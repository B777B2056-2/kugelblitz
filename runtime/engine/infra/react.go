package infra

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/observability"
	"github.com/B777B2056-2/kugelblitz/tools/internals"
)

// OnToolResult is called after each tool execution in the ReAct loop.
// step = current loop iteration count. Return false to abort the loop.
type OnToolResult func(results []core.ToolCallResult, step int) bool

// humanLoopState groups all human-in-the-loop state into a single struct.
// It is nil when HITL is not enabled.
type humanLoopState struct {
	localTools map[string]core.ToolCallFunc   // instance‑local tools (e.g. ask_human)
	localDefs  map[string]core.ToolDefinition // definitions for local tools
	responseCh chan string                    // buffers one human response
	isWaiting  atomic.Bool                    // true while WaitForHuman is blocking
}

type ReactAgent struct {
	provider        core.ILMProvider
	providerMu      sync.RWMutex
	toolRegistry    *core.ToolRegistry
	StreamMode      bool
	EventHooks      core.AgentEventHooks
	agentIdentity   constants.AgentIdentity
	abortSignal     chan struct{}
	EnableThinking  *bool
	ReasoningEffort string
	toolNames       []string                  // nil=all tools; non-nil=whitelist
	visibleCache    []core.ToolDefinition     // cached filtered tool list; invalidated by WithTools
	stepCount       int                       // ReAct loop iterations
	OnToolResult    OnToolResult              // per-tool-execution callback
	stepTracer      *observability.StepTracer // per-step trace instrumentation
	humanLoop       *humanLoopState
	pauseGate       *sync.RWMutex // shared gate; nil=no pausing; RLock blocks tool calls
}

func NewReactAgent(provider core.ILMProvider, streamMode bool) *ReactAgent {
	return &ReactAgent{
		provider:     provider,
		toolRegistry: core.GetToolRegistry(),
		StreamMode:   streamMode,
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
		a.toolNames = append(a.toolNames, names...)
	}
	return a
}

func (a *ReactAgent) SetAgentIdentity(agentIdentity constants.AgentIdentity) {
	a.agentIdentity = agentIdentity
}

// SetStepTracer attaches a StepTracer for per-step OTel instrumentation.
func (a *ReactAgent) SetStepTracer(st *observability.StepTracer) { a.stepTracer = st }

// SetProvider replaces the LLM provider used for subsequent ExecuteWithTools calls.
func (a *ReactAgent) SetProvider(p core.ILMProvider) {
	a.providerMu.Lock()
	a.provider = p
	a.providerMu.Unlock()
}

func (a *ReactAgent) GetAgentIdentity() constants.AgentIdentity {
	return a.agentIdentity
}

func (a *ReactAgent) RegisterEventHooks(hooks core.AgentEventHooks) {
	a.EventHooks = hooks
}

func (a *ReactAgent) WithPauseGate(g *sync.RWMutex) *ReactAgent {
	a.pauseGate = g
	return a
}

func (a *ReactAgent) SetOnToolResult(fn OnToolResult) { a.OnToolResult = fn }

func (a *ReactAgent) Execute(ctx context.Context, systemMessage core.Message, userMessages []core.Message) ([]core.Message, error) {
	return a.ExecuteWithTools(ctx, systemMessage, userMessages, nil)
}

// ExecuteWithTools runs the ReAct loop with an optional per-call tool whitelist.
// When tools is nil, uses the instance-level toolNames (set by WithTools). When non-nil,
// overrides for this call only. Pass an empty slice to allow no tools.
func (a *ReactAgent) ExecuteWithTools(ctx context.Context, systemMessage core.Message, userMessages []core.Message, tools []string) ([]core.Message, error) {
	inputMessages := append([]core.Message{systemMessage}, userMessages...)
	var assistantMessages []core.Message

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

		select {
		case <-a.abortSignal:
			return stripDanglingToolCalls(assistantMessages), nil
		case <-ctx.Done():
			return stripDanglingToolCalls(assistantMessages), ctx.Err()
		default:
		}

		params := core.GenerateParams{
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

		toolMsg := core.NewToolMessage(toolCallResults)
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
func stripDanglingToolCalls(messages []core.Message) []core.Message {
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
func needEarlyTerminating(results []core.ToolCallResult) bool {
	for _, r := range results {
		if core.GetToolRegistry().IsTerminating(r.ToolName) {
			if _, isErr := r.Outputs["error"]; !isErr {
				return true
			}
		}
	}
	return false
}

func extractToolCalls(content core.Content) []core.ToolCallDetail {
	if content == nil {
		return nil
	}
	switch ct := content.(type) {
	case core.ToolCallContent:
		return ct.Details
	case core.CompositeContent:
		var details []core.ToolCallDetail
		for _, part := range ct.Parts {
			if tc, ok := part.(core.ToolCallContent); ok {
				details = append(details, tc.Details...)
			}
		}
		return details
	default:
		return nil
	}
}

func (a *ReactAgent) executeTools(ctx context.Context, details []core.ToolCallDetail) []core.ToolCallResult {
	results := make([]core.ToolCallResult, len(details))
	for i, detail := range details {
		result := a.callTool(ctx, detail)
		results[i] = result
		if a.EventHooks.OnToolCallEnd != nil {
			a.EventHooks.OnToolCallEnd(a.agentIdentity, result)
		}
	}
	return results
}

func (a *ReactAgent) visibleTools() []core.ToolDefinition {
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
	filtered := make([]core.ToolDefinition, 0, len(a.toolNames))
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
func (a *ReactAgent) EnableHumanInTheLoop() *ReactAgent {
	if a.humanLoop != nil {
		return a // already enabled
	}
	a.visibleCache = nil // invalidate cache: local ask_human tool will be added
	a.humanLoop = &humanLoopState{
		localTools: make(map[string]core.ToolCallFunc),
		localDefs:  make(map[string]core.ToolDefinition),
		responseCh: make(chan string, 1),
	}
	a.registerLocalAskHuman()
	return a
}

func (a *ReactAgent) registerLocalAskHuman() {
	askTool := &internals.AskHumanTool{Gate: a}
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
	if a.EventHooks.OnWaitForHumanAction != nil {
		a.EventHooks.OnWaitForHumanAction(a.agentIdentity, reason, prompt)
	}
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
// Before executing, it checks the shared DAG pause gate (sync.RWMutex).
// When another worker enters HITL, the DAG executor write-locks this mutex
// (PauseMu.Lock). RLock blocks until the write-lock is released, so every
// tool call becomes a synchronization checkpoint:
//
//	Normal:  write-lock NOT held → RLock passes instantly → RUnlock → proceed
//	Paused:  write-lock IS held  → RLock blocks until Resume() → proceed
//
// We don't need to hold the read lock during tool execution — a single
// RLock/RUnlock round-trip is enough to detect whether the DAG is paused.
func (a *ReactAgent) callTool(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	if a.pauseGate != nil {
		a.pauseGate.RLock()
		a.pauseGate.RUnlock() //nolint:staticcheck // DAG pause checkpoint, see doc above
	}
	if a.humanLoop != nil {
		if fn, ok := a.humanLoop.localTools[detail.ToolName]; ok {
			return fn(ctx, detail)
		}
	}
	return a.toolRegistry.Call(ctx, detail)
}

// modelEventHandler returns a ModelEventHandler for the provider by creating
// a bridge from the AgentEventHooks callback fields.
func (a *ReactAgent) modelEventHandler() core.ModelEventHandler {
	return a.EventHooks.AsModelEventHandler(a.agentIdentity)
}
