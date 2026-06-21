package runtime

import (
	"context"
	"sync"

	"kugelblitz/core"
)

// OnToolResult is called after each tool execution in the ReAct loop.
// step = current loop iteration count. Return false to abort the loop.
type OnToolResult func(results []core.ToolCallResult, step int) bool

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

func (a *ReactAgent) SetOnToolResult(fn OnToolResult) { a.onToolResult = fn }

func (a *ReactAgent) Execute(ctx context.Context, systemMessage core.Message, userMessages []core.Message) ([]core.Message, error) {
	inputMessages := append([]core.Message{systemMessage}, userMessages...)
	var assistantMessages []core.Message

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

		inputMessages = append(inputMessages, *assistantMessage)
		toolMsg := core.NewToolMessage(assistantMessage.ID, toolCallResults)
		inputMessages = append(inputMessages, toolMsg)
	}
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
	if len(details) == 1 {
		result := a.toolRegistry.Call(ctx, details[0])
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

func (a *ReactAgent) Interrupt(ctx context.Context) error {
	select {
	case a.abortSignal <- struct{}{}:
	default:
	}
	return nil
}
