package runtime

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/B777B2056-2/kugelblitz/core"
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
	toolRegistry    *core.ToolRegistry
	streamMode      bool
	eventHooks      core.AgentEventHooks
	abortSignal     chan struct{}
	enableThinking  *bool
	reasoningEffort string
	toolNames       []string     // nil=all tools; non-nil=whitelist
	stepCount       int          // ReAct loop iterations
	onToolResult    OnToolResult // per-tool-execution callback
	humanLoop       *humanLoopState
	pauseGate       *sync.RWMutex // shared gate; nil=no pausing; RLock blocks tool calls
}

func NewReactAgent(provider core.ILMProvider, streamMode bool) *ReactAgent {
	return &ReactAgent{
		provider:     provider,
		toolRegistry: core.GetToolRegistry(),
		streamMode:   streamMode,
		abortSignal:  make(chan struct{}, 1),
	}
}

func (a *ReactAgent) SetThinking(enabled bool, effort string) {
	a.enableThinking = &enabled
	a.reasoningEffort = effort
}

func (a *ReactAgent) WithTools(names ...string) *ReactAgent {
	if len(names) == 0 {
		a.toolNames = nil
	} else {
		a.toolNames = append(a.toolNames, names...)
	}
	return a
}

func (a *ReactAgent) RegisterEventHooks(hooks core.AgentEventHooks) {
	a.eventHooks = hooks
}

func (a *ReactAgent) WithPauseGate(g *sync.RWMutex) *ReactAgent {
	a.pauseGate = g
	return a
}

func (a *ReactAgent) SetOnToolResult(fn OnToolResult) { a.onToolResult = fn }

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
		a.toolNames = tools
		defer func() { a.toolNames = originalTools }()
	}

	a.stepCount = 0
	for {
		a.stepCount++

		select {
		case <-a.abortSignal:
			return assistantMessages, nil
		case <-ctx.Done():
			return assistantMessages, ctx.Err()
		default:
		}

		params := core.GenerateParams{
			Messages:        inputMessages,
			Tools:           a.visibleTools(),
			Stream:          a.streamMode,
			EventHandler:    a.eventHooks.ModelEventHandler,
			EnableThinking:  a.enableThinking,
			ReasoningEffort: a.reasoningEffort,
		}

		assistantMessage, err := a.provider.Generate(ctx, params)
		if err != nil {
			return assistantMessages, err
		}
		assistantMessages = append(assistantMessages, *assistantMessage)

		details := extractToolCalls(assistantMessage.Content)
		if len(details) == 0 {
			return assistantMessages, nil
		}

		toolCallResults := a.executeTools(ctx, details)

		// Let external observer inspect results and optionally abort
		if a.onToolResult != nil && !a.onToolResult(toolCallResults, a.stepCount) {
			return assistantMessages, nil
		}

		if needEarlyTerminating(toolCallResults) {
			toolMsg := core.NewToolMessage(assistantMessage.ID, toolCallResults)
			assistantMessages = append(assistantMessages, toolMsg)
			return assistantMessages, nil
		}

		inputMessages = append(inputMessages, *assistantMessage)

		toolMsg := core.NewToolMessage(assistantMessage.ID, toolCallResults)
		inputMessages = append(inputMessages, toolMsg)
		assistantMessages = append(assistantMessages, toolMsg)
	}
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

// extractToolResults extracts all ToolCallResults from a message's content,
// handling both ToolResultContent and CompositeContent wrappers.
func extractToolResults(content core.Content) []core.ToolCallResult {
	if content == nil {
		return nil
	}
	switch ct := content.(type) {
	case core.ToolResultContent:
		return ct.Results
	case core.CompositeContent:
		var results []core.ToolCallResult
		for _, part := range ct.Parts {
			if tr, ok := part.(core.ToolResultContent); ok {
				results = append(results, tr.Results...)
			}
		}
		return results
	default:
		return nil
	}
}

func (a *ReactAgent) executeTools(ctx context.Context, details []core.ToolCallDetail) []core.ToolCallResult {
	if len(details) == 1 {
		result := a.callTool(ctx, details[0])
		if a.eventHooks.OnToolCallEnd != nil {
			a.eventHooks.OnToolCallEnd(result)
		}
		return []core.ToolCallResult{result}
	}

	results := make([]core.ToolCallResult, len(details))
	var wg sync.WaitGroup
	var cbMu sync.Mutex

	for i, detail := range details {
		wg.Add(1)
		go func(idx int, d core.ToolCallDetail) {
			defer wg.Done()
			result := a.callTool(ctx, d)
			results[idx] = result
			if a.eventHooks.OnToolCallEnd != nil {
				cbMu.Lock()
				a.eventHooks.OnToolCallEnd(result)
				cbMu.Unlock()
			}
		}(i, detail)
	}

	wg.Wait()
	return results
}

func (a *ReactAgent) visibleTools() []core.ToolDefinition {
	all := a.toolRegistry.ListDefinitions()
	// Append local tool definitions
	if a.humanLoop != nil {
		for _, def := range a.humanLoop.localDefs {
			all = append(all, def)
		}
	}
	// Debug: log all globally registered tools
	{
		names := make([]string, len(all))
		for i, d := range all {
			names[i] = d.Name
		}
		whitelist := a.toolNames
		if whitelist == nil {
			whitelist = []string{"<all>"}
		}
	}
	if a.toolNames == nil {
		return all
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
	// Debug: log filtered tools
	{
		names := make([]string, len(filtered))
		for i, d := range filtered {
			names[i] = d.Name
		}
	}
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
	if a.eventHooks.OnWaitForHumanAction != nil {
		a.eventHooks.OnWaitForHumanAction(reason, prompt)
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
func (a *ReactAgent) callTool(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	if a.pauseGate != nil {
		a.pauseGate.RLock()
		a.pauseGate.RUnlock()
	}
	if a.humanLoop != nil {
		if fn, ok := a.humanLoop.localTools[detail.ToolName]; ok {
			return fn(ctx, detail)
		}
	}
	return a.toolRegistry.Call(ctx, detail)
}
