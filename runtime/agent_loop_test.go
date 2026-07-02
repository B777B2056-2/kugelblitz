package runtime

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentLoop_Run(t *testing.T) {
	core.GetToolRegistry().Reset()
	core.RegisterTool(
		core.ToolDefinition{Name: "file_read", Description: "Read a file"},
		func(ctx context.Context, d core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{ToolCallID: d.ID, ToolName: d.ToolName, Outputs: map[string]any{"content": "ok"}}
		},
	)

	callCount := 0
	prov := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			msg := core.NewAssistantMessage("p1", core.TextContent{Text: "done"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	loop := NewAgentLoop(prov, core.AgentEventHooks{})
	loop.Run(context.Background(), "read the README", "")
	loop.WaitDone()

	// Intent → Direct path uses the state machine (always uses Planner now).
	assert.GreaterOrEqual(t, callCount, 1)
}

func TestAgentLoop_Resume_ErrorWhenNotRunning(t *testing.T) {
	loop := NewAgentLoop(nil, core.AgentEventHooks{})
	err := loop.Resume("test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestAgentLoop_ThinkingConfig(t *testing.T) {
	core.GetToolRegistry().Reset()

	callCount := 0
	prov := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			require.NotNil(t, params.EnableThinking)
			assert.True(t, *params.EnableThinking)
			msg := core.NewAssistantMessage("p1", core.TextContent{Text: "done"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	loop := NewAgentLoop(prov, core.AgentEventHooks{},
		WithThinking(true, "high"),
	)
	loop.Run(context.Background(), "test", "")
	loop.WaitDone()

	assert.GreaterOrEqual(t, callCount, 1)
}

func TestAgentLoop_UsageCallback(t *testing.T) {
	core.GetToolRegistry().Reset()

	var reports []core.LLMUsageReport
	callCount := 0
	prov := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			msg := core.NewAssistantMessage("p1", core.TextContent{Text: "done"})
			msg.FinishReason = "stop"
			msg.Usage = &core.Usage{TotalTokens: 42}
			return &msg, nil
		},
	}

	loop := NewAgentLoop(prov, core.AgentEventHooks{},
		WithUsageCallback(func(r core.LLMUsageReport) { reports = append(reports, r) }),
	)
	loop.Run(context.Background(), "test", "")
	loop.WaitDone()

	assert.GreaterOrEqual(t, callCount, 1)
}

func TestAgentLoop_ProviderError(t *testing.T) {
	core.GetToolRegistry().Reset()

	eh := &testEventHandler{}
	prov := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			return nil, assert.AnError
		},
	}

	loop := NewAgentLoop(prov, core.AgentEventHooks{
		ModelEventHandler: eh,
	})
	loop.Run(context.Background(), "do something", "")
	loop.WaitDone()

	require.NotEmpty(t, eh.errors)
}
