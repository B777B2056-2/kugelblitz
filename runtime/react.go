package runtime

import (
	"context"
	"sync"

	"kugelblitz/core"
)

// ReactAgent implements the ReAct (Reasoning + Acting) pattern.
// It loops: LLM thinks → acts (tool calls) → observes (results) → thinks again.
type ReactAgent struct {
	provider        core.ILMProvider
	toolRegistry    *core.ToolRegistry
	streamMode      bool
	eventHooks      core.AgentEventHooks
	abortSignal     chan struct{}
	enableThinking  *bool
	reasoningEffort string
	toolNames       []string // nil=all tools; non-nil=whitelist
}

// NewReactAgent creates a new ReAct agent with the given provider.
// Tools are resolved via the global [core.GetToolRegistry] singleton;
// register tools with [core.RegisterTool] before calling Execute.
func NewReactAgent(provider core.ILMProvider, streamMode bool) *ReactAgent {
	return &ReactAgent{
		provider:     provider,
		toolRegistry: core.GetToolRegistry(),
		streamMode:   streamMode,
		abortSignal:  make(chan struct{}, 1),
	}
}

// SetThinking configures thinking mode for all subsequent Execute calls.
// enabled controls whether the model spends tokens on internal reasoning.
// effort controls reasoning intensity: "low", "medium", "high", "xhigh", "max".
// Use [core.ReasoningEffortHigh] etc. for the effort value.
func (a *ReactAgent) SetThinking(enabled bool, effort string) {
	a.enableThinking = &enabled
	a.reasoningEffort = effort
}

// WithTools restricts the agent to only see the named tools.
// Call multiple times to accumulate; pass no names to clear the filter (see all).
// If never called, all registered tools are visible.
func (a *ReactAgent) WithTools(names ...string) *ReactAgent {
	if len(names) == 0 {
		a.toolNames = nil
	} else {
		a.toolNames = append(a.toolNames, names...)
	}
	return a
}

// RegisterEventHooks stores hooks for per-request use.
// StreamHandler is passed to the provider via GenerateParams on each call.
func (a *ReactAgent) RegisterEventHooks(hooks core.AgentEventHooks) {
	a.eventHooks = hooks
}

// Execute runs the ReAct loop.
// It appends system + user messages, then loops:
//  1. Call the LLM via provider
//  2. If no tool calls, return all assistant messages
//  3. Execute tool calls via the registry
//  4. Append assistant + tool messages to history, loop
func (a *ReactAgent) Execute(ctx context.Context, systemMessage core.Message, userMessages []core.Message) ([]core.Message, error) {
	inputMessages := append([]core.Message{systemMessage}, userMessages...)
	var assistantMessages []core.Message

	for {
		// Check for abort or context cancellation
		select {
		case <-a.abortSignal:
			return assistantMessages, nil
		case <-ctx.Done():
			return assistantMessages, ctx.Err()
		default:
		}

		// Build params from current state
		params := core.GenerateParams{
			Messages:        inputMessages,
			Tools:           a.visibleTools(),
			Stream:          a.streamMode,
			EventHandler:    a.eventHooks.ModelEventHandler,
			EnableThinking:  a.enableThinking,
			ReasoningEffort: a.reasoningEffort,
		}

		// 1. thinking — get LLM response
		assistantMessage, err := a.provider.Generate(ctx, params)
		if err != nil {
			return assistantMessages, err
		}
		assistantMessages = append(assistantMessages, *assistantMessage)

		// 2. action — check if there are tool calls to execute
		details := extractToolCalls(assistantMessage.Content)
		if len(details) == 0 {
			return assistantMessages, nil
		}

		// Execute tool calls concurrently when multiple are present
		toolCallResults := a.executeTools(ctx, details)

		// 3. observation — append both assistant + tool result to conversation history
		inputMessages = append(inputMessages, *assistantMessage)
		toolMsg := core.NewToolMessage(assistantMessage.ID, toolCallResults)
		inputMessages = append(inputMessages, toolMsg)
	}
}

// extractToolCalls extracts ToolCallDetail from any Content type that
// may contain tool calls (ToolCallContent directly, or CompositeContent).
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

// executeTools runs tool calls concurrently when multiple are present.
func (a *ReactAgent) executeTools(ctx context.Context, details []core.ToolCallDetail) []core.ToolCallResult {
	if len(details) == 1 {
		// Single tool — no goroutine overhead
		result := a.toolRegistry.Call(ctx, details[0])
		if a.eventHooks.OnToolCallEnd != nil {
			a.eventHooks.OnToolCallEnd(result)
		}
		return []core.ToolCallResult{result}
	}

	results := make([]core.ToolCallResult, len(details))
	var wg sync.WaitGroup
	var cbMu sync.Mutex // ensures only one callback runs at a time

	for i, detail := range details {
		wg.Add(1)
		go func(idx int, d core.ToolCallDetail) {
			defer wg.Done()
			result := a.toolRegistry.Call(ctx, d)
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

// visibleTools returns the tool definitions this agent can see.
// If toolNames is nil, all registered tools are visible; otherwise only
// the tools whose names are in the whitelist.
func (a *ReactAgent) visibleTools() []core.ToolDefinition {
	all := a.toolRegistry.ListDefinitions()
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
	return filtered
}

// Interrupt signals the agent to stop at the next loop iteration.
func (a *ReactAgent) Interrupt(ctx context.Context) error {
	select {
	case a.abortSignal <- struct{}{}:
	default:
		// Channel already has a pending signal, skip
	}
	return nil
}
