package infra

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/events"
	"github.com/B777B2056-2/kugelblitz/observability"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/worker"
)

// OnToolResult is called after each tool execution in the ReAct loop.
// step = current loop iteration count. Return false to abort the loop.
type OnToolResult func(results []coretypes.ToolCallResult, step int) bool

// humanLoopState groups all human-in-the-loop state into a single struct.
// It is nil when HITL is not enabled.
type humanLoopState struct {
	localTools map[string]coretypes.ToolCallFunc   // instance‑local tools (e.g. ask_human)
	localDefs  map[string]coretypes.ToolDefinition // definitions for local tools
	responseCh chan string                         // buffers one human response
	isWaiting  atomic.Bool                         // true while WaitForHuman is blocking
}

type ReactAgent struct {
	provider         coretypes.ILMProvider
	providerMu       sync.RWMutex
	toolRegistry     *core.ToolRegistry
	StreamMode       bool
	bus              *events.Bus // shared per-loop bus; events are emitted here
	instance         string      // disambiguates concurrent agents on a shared bus
	unsub            func()      // combined unsubscribe for RegisterEventHooks (overwrite semantics)
	agentIdentity    constants.AgentIdentity
	abortSignal      chan struct{}
	EnableThinking   *bool
	ReasoningEffort  string
	toolNames        []string                   // nil=all tools; non-nil=whitelist
	visibleCache     []coretypes.ToolDefinition // cached filtered tool list; invalidated by WithTools
	stepCount        int                        // ReAct loop iterations
	maxSteps         int                        // max ReAct iterations; 0 = unlimited
	OnToolResult     OnToolResult               // per-tool-execution callback
	stepTracer       *observability.StepTracer  // per-step trace instrumentation
	humanLoop        *humanLoopState
	humanToolFactory worker.HumanToolFactory // builds the local ask_human tool; nil = omit
	pauseGate        worker.PauseGate        // shared gate; nil=no pausing; WaitIfPaused blocks tool calls
}

func NewReactAgent(provider coretypes.ILMProvider, streamMode bool) *ReactAgent {
	return &ReactAgent{
		provider:     provider,
		toolRegistry: core.GetToolRegistry(),
		StreamMode:   streamMode,
		bus:          events.NewBus(),
		abortSignal:  make(chan struct{}, 1),
	}
}

func (a *ReactAgent) SetThinking(enabled bool, effort string) {
	a.EnableThinking = &enabled
	a.ReasoningEffort = effort
}

func (a *ReactAgent) WithTools(names ...string) *ReactAgent {
	a.visibleCache = nil // invalidate cache
	if len(names) == 0 {
		a.toolNames = nil
	} else {
		// Replace (not accumulate) the tool set so repeated calls do not leak
		// earlier selections into later ones (B16).
		a.toolNames = append([]string(nil), names...)
	}
	return a
}

func (a *ReactAgent) SetAgentIdentity(agentIdentity constants.AgentIdentity) {
	a.agentIdentity = agentIdentity
}

// SetStepTracer attaches a StepTracer for per-step OTel instrumentation.
func (a *ReactAgent) SetStepTracer(st *observability.StepTracer) { a.stepTracer = st }

// SetProvider replaces the LLM provider used for subsequent ExecuteWithTools calls.
func (a *ReactAgent) SetProvider(p coretypes.ILMProvider) {
	a.providerMu.Lock()
	a.provider = p
	a.providerMu.Unlock()
}

// SetBus attaches the shared per-loop bus this agent emits into. A nil bus is
// replaced by a fresh private bus so standalone agents (e.g. tests) still work.
func (a *ReactAgent) SetBus(bus *events.Bus) {
	if bus == nil {
		bus = events.NewBus()
	}
	a.bus = bus
}

// SetInstance tags events emitted by this agent with a unique instance ID, used
// to tell concurrent DAG workers apart on a shared bus.
func (a *ReactAgent) SetInstance(instance string) { a.instance = instance }

func (a *ReactAgent) RegisterEventHooks(hooks core.AgentEventHooks) {
	if a.unsub != nil {
		a.unsub()
	}
	a.unsub = hooks.Subscribe(a.bus)
}

func (a *ReactAgent) WithPauseGate(g worker.PauseGate) *ReactAgent {
	a.pauseGate = g
	return a
}

func (a *ReactAgent) SetOnToolResult(fn OnToolResult) { a.OnToolResult = fn }

// SetHumanToolFactory injects the factory used to build the local ask_human
// tool when EnableHumanInTheLoop is called. When nil, ask_human is omitted.
func (a *ReactAgent) SetHumanToolFactory(f worker.HumanToolFactory) { a.humanToolFactory = f }

// SetMaxSteps caps the number of ReAct loop iterations. 0 (default) = unlimited.
func (a *ReactAgent) SetMaxSteps(n int) *ReactAgent { a.maxSteps = n; return a }

func (a *ReactAgent) Execute(ctx context.Context, systemMessage coretypes.Message, userMessages []coretypes.Message) ([]coretypes.Message, error) {
	return a.ExecuteWithTools(ctx, systemMessage, userMessages, nil)
}

// ExecuteWithTools runs the ReAct loop with an optional per-call tool whitelist.
// When tools is nil, uses the instance-level toolNames (set by WithTools). When non-nil,
// overrides for this call only. Pass an empty slice to allow no tools.
func (a *ReactAgent) ExecuteWithTools(ctx context.Context, systemMessage coretypes.Message, userMessages []coretypes.Message, tools []string) ([]coretypes.Message, error) {
	inputMessages := append([]coretypes.Message{systemMessage}, userMessages...)
	var assistantMessages []coretypes.Message

	// Override tools only when explicitly provided (nil = use instance config)
	if tools != nil {
		originalTools := a.toolNames
		originalCache := a.visibleCache
		a.toolNames = tools
		a.visibleCache = nil
		defer func() {
			a.toolNames = originalTools
			a.visibleCache = originalCache
		}()
	}

	a.stepCount = 0
	for {
		a.stepCount++
		if a.maxSteps > 0 && a.stepCount > a.maxSteps {
			return stripDanglingToolCalls(assistantMessages), coretypes.ErrMaxStepsExceeded
		}

		select {
		case <-a.abortSignal:
			return stripDanglingToolCalls(assistantMessages), nil
		case <-ctx.Done():
			return stripDanglingToolCalls(assistantMessages), ctx.Err()
		default:
		}

		params := coretypes.GenerateParams{
			Messages:        inputMessages,
			Tools:           a.visibleTools(),
			Stream:          a.StreamMode,
			EventHandler:    a.modelEventHandler(),
			EnableThinking:  a.EnableThinking,
			ReasoningEffort: a.ReasoningEffort,
		}

		a.providerMu.RLock()
		p := a.provider
		a.providerMu.RUnlock()
		assistantMessage, err := p.Generate(ctx, params)
		if err != nil {
			return assistantMessages, err
		}
		assistantMessages = append(assistantMessages, *assistantMessage)

		// Ensure OnUsageUpdated fires for every LLM call. Streaming
		// providers dispatch per-chunk; Block() does not. Always fire
		// from the final message to cover both paths.
		if assistantMessage.Usage != nil {
			if eh := a.modelEventHandler(); eh != nil {
				eh.OnUsageUpdated(*assistantMessage.Usage)
			}
		}

		details := extractToolCalls(assistantMessage.Content)
		if len(details) == 0 {
			return assistantMessages, nil
		}

		toolCallResults := a.executeTools(ctx, details)

		if a.stepTracer != nil {
			a.stepTracer.StepSpan(ctx, a.stepCount, toolCallResults)
		}

		toolMsg := coretypes.NewToolMessage(toolCallResults)
		assistantMessages = append(assistantMessages, toolMsg)

		// Let external observer inspect results and optionally abort
		if a.OnToolResult != nil && !a.OnToolResult(toolCallResults, a.stepCount) {
			return assistantMessages, nil
		}

		if needEarlyTerminating(toolCallResults) {
			return assistantMessages, nil
		}

		inputMessages = append(inputMessages, *assistantMessage, toolMsg)
	}
}

// stripDanglingToolCalls removes the last assistant message if it has tool_calls
// but no corresponding tool results (e.g. after abort/cancel during execution).
func stripDanglingToolCalls(messages []coretypes.Message) []coretypes.Message {
	if len(messages) == 0 {
		return messages
	}
	last := messages[len(messages)-1]
	if last.Role != constants.RoleAssistant {
		return messages
	}
	details := extractToolCalls(last.Content)
	if len(details) > 0 {
		return messages[:len(messages)-1]
	}
	return messages
}

// needEarlyTerminating returns true if any terminating tool in the
// batch executed without error. In that case the caller should stop the
// ReAct loop without feeding results back to the LLM.
func needEarlyTerminating(results []coretypes.ToolCallResult) bool {
	for _, r := range results {
		if core.GetToolRegistry().IsTerminating(r.ToolName) {
			if _, isErr := r.Outputs["error"]; !isErr {
				return true
			}
		}
	}
	return false
}

func extractToolCalls(content coretypes.Content) []coretypes.ToolCallDetail {
	if content == nil {
		return nil
	}
	switch ct := content.(type) {
	case coretypes.ToolCallContent:
		return ct.Details
	case coretypes.CompositeContent:
		var details []coretypes.ToolCallDetail
		for _, part := range ct.Parts {
			if tc, ok := part.(coretypes.ToolCallContent); ok {
				details = append(details, tc.Details...)
			}
		}
		return details
	default:
		return nil
	}
}

func (a *ReactAgent) executeTools(ctx context.Context, details []coretypes.ToolCallDetail) []coretypes.ToolCallResult {
	results := make([]coretypes.ToolCallResult, len(details))
	for i, detail := range details {
		result := a.callTool(ctx, detail)
		results[i] = result
		events.Emit(a.bus, events.ToolCallEnd{Identity: a.agentIdentity, Instance: a.instance, Result: result})
	}
	return results
}

func (a *ReactAgent) visibleTools() []coretypes.ToolDefinition {
	// No whitelist → return all tools (global + local)
	if a.toolNames == nil {
		all := a.toolRegistry.ListDefinitions()
		if a.humanLoop != nil {
			for _, def := range a.humanLoop.localDefs {
				all = append(all, def)
			}
		}
		return all
	}
	// Have whitelist → use cache
	if a.visibleCache != nil {
		return a.visibleCache
	}
	all := a.toolRegistry.ListDefinitions()
	if a.humanLoop != nil {
		for _, def := range a.humanLoop.localDefs {
			all = append(all, def)
		}
	}
	allow := make(map[string]bool, len(a.toolNames))
	for _, n := range a.toolNames {
		allow[n] = true
	}
	filtered := make([]coretypes.ToolDefinition, 0, len(a.toolNames))
	for _, def := range all {
		if allow[def.Name] {
			filtered = append(filtered, def)
		}
	}
	a.visibleCache = filtered
	return filtered
}

func (a *ReactAgent) Interrupt(ctx context.Context) error {
	select {
	case a.abortSignal <- struct{}{}:
	default:
	}
	return nil
}

// EnableHumanInTheLoop activates human-in-the-loop support by registering the
// ask_human tool locally on this agent. Must be called before Execute.
// The ask_human tool is only registered when a HumanToolFactory has been set
// via SetHumanToolFactory (done by the composition root).
func (a *ReactAgent) EnableHumanInTheLoop() *ReactAgent {
	if a.humanLoop != nil {
		return a // already enabled
	}
	a.visibleCache = nil // invalidate cache: local ask_human tool will be added
	a.humanLoop = &humanLoopState{
		localTools: make(map[string]coretypes.ToolCallFunc),
		localDefs:  make(map[string]coretypes.ToolDefinition),
		responseCh: make(chan string, 1),
	}
	a.registerLocalAskHuman()
	return a
}

func (a *ReactAgent) registerLocalAskHuman() {
	if a.humanToolFactory == nil {
		return
	}
	askTool := a.humanToolFactory(a)
	def := askTool.Definition()
	a.humanLoop.localDefs[def.Name] = def
	a.humanLoop.localTools[def.Name] = askTool.Execute
}

// WaitForHuman implements core.HumanGate. It fires OnWaitForHumanAction and
// blocks until ResumeWithHumanResponse is called or ctx is cancelled.
func (a *ReactAgent) WaitForHuman(ctx context.Context, reason, prompt string) (string, error) {
	if a.humanLoop == nil {
		return "", fmt.Errorf("human-in-the-loop not enabled")
	}
	events.Emit(a.bus, events.WaitForHumanAction{Identity: a.agentIdentity, Instance: a.instance, Reason: reason, Prompt: prompt})
	a.humanLoop.isWaiting.Store(true)
	defer a.humanLoop.isWaiting.Store(false)

	select {
	case response := <-a.humanLoop.responseCh:
		return response, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// ResumeWithHumanResponse unblocks a pending WaitForHuman call with the given
// response. It returns an error if HITL is not enabled or the agent is not
// currently waiting.
func (a *ReactAgent) ResumeWithHumanResponse(ctx context.Context, response string) error {
	if a.humanLoop == nil {
		return fmt.Errorf("human-in-the-loop not enabled")
	}
	if !a.humanLoop.isWaiting.Load() {
		return fmt.Errorf("agent is not waiting for human input")
	}
	select {
	case a.humanLoop.responseCh <- response:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// HumanLoopWaiting reports whether the agent is currently blocked in
// WaitForHuman, waiting for a human response via ResumeWithHumanResponse.
func (a *ReactAgent) HumanLoopWaiting() bool {
	return a.humanLoop != nil && a.humanLoop.isWaiting.Load()
}

// callTool resolves a tool call: local tools first, then the global registry.
//
// Before executing, it checks the shared DAG pause gate. When another worker
// enters HITL, the gate is paused and WaitIfPaused blocks until Resume, so
// every tool call becomes a synchronization checkpoint:
//
//	Normal: gate not paused → WaitIfPaused returns instantly → proceed
//	Paused: gate paused    → WaitIfPaused blocks until Resume → proceed
//
// We don't need to hold the gate during tool execution — a single check is
// enough to detect whether the DAG is currently paused.
func (a *ReactAgent) callTool(ctx context.Context, detail coretypes.ToolCallDetail) coretypes.ToolCallResult {
	if a.pauseGate != nil {
		a.pauseGate.WaitIfPaused()
	}
	if a.humanLoop != nil {
		if fn, ok := a.humanLoop.localTools[detail.ToolName]; ok {
			return fn(ctx, detail)
		}
	}
	return a.toolRegistry.Call(ctx, detail)
}

// modelEventHandler returns a ModelEventHandler that emits each provider callback
// as a typed event on the bus, tagged with this agent's identity and instance.
func (a *ReactAgent) modelEventHandler() coretypes.ModelEventHandler {
	return core.NewBusModelHandler(a.bus, a.agentIdentity, a.instance)
}
