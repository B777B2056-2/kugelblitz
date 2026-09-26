package infra

import (
	"context"
	"errors"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/stretchr/testify/require"
)

// TestReactAgent_MaxSteps_BoundsInfiniteLoop verifies that a provider which
// always requests a tool call is stopped by the step limit instead of looping
// forever (B4).
func TestReactAgent_MaxSteps_BoundsInfiniteLoop(t *testing.T) {
	core.RegisterTool(
		coretypes.ToolDefinition{Name: "loop_tool", Description: "always called"},
		func(ctx context.Context, detail coretypes.ToolCallDetail) coretypes.ToolCallResult {
			return coretypes.ToolCallResult{ToolCallID: detail.ID, ToolName: detail.ToolName, Outputs: map[string]any{"ok": true}}
		},
	)

	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			callCount++
			msg := coretypes.NewAssistantMessage(nil)
			msg.Content = coretypes.ToolCallContent{
				Details: []coretypes.ToolCallDetail{
					{ID: "tc", ToolName: "loop_tool", Args: map[string]any{}},
				},
			}
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	agent.SetMaxSteps(5)

	_, err := agent.Execute(
		context.Background(),
		coretypes.NewUserMessage(coretypes.TextContent{Text: "system"}),
		[]coretypes.Message{coretypes.NewUserMessage(coretypes.TextContent{Text: "go"})},
	)

	require.Error(t, err)
	require.True(t, errors.Is(err, coretypes.ErrMaxStepsExceeded), "expected ErrMaxStepsExceeded, got %v", err)
	require.Equal(t, 5, callCount, "expected exactly maxSteps LLM calls")
}

// TestReactAgent_MaxSteps_ZeroMeansUnlimited ensures the default (0) does not
// impose a limit, preserving existing behavior for the main planner agent.
func TestReactAgent_MaxSteps_ZeroMeansUnlimited(t *testing.T) {
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			callCount++
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false) // maxSteps == 0 (default)

	_, err := agent.Execute(
		context.Background(),
		coretypes.NewUserMessage(coretypes.TextContent{Text: "system"}),
		[]coretypes.Message{coretypes.NewUserMessage(coretypes.TextContent{Text: "hi"})},
	)

	require.NoError(t, err)
	require.Equal(t, 1, callCount)
}
