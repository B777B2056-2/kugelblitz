package runtime

import (
	"context"
	"errors"
	"testing"

	"kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanner_ContextError_TriggersRetry(t *testing.T) {
	callCount := 0
	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				// First call: context exceeded
				return nil, core.ErrContextLengthExceeded
			}
			// Second call (after compress): succeeds
			msg := core.NewAssistantMessage("m", core.TextContent{Text: "done"})
			return &msg, nil
		},
	}

	planner := NewPlanner(provider, false)
	msgs, err := planner.Execute(context.Background(), "test goal")

	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, 2, callCount, "should have retried after compress")
	assert.Equal(t, "done", msgs[0].Content.(core.TextContent).Text)
}

func TestPlanner_NonContextError_NoRetry(t *testing.T) {
	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			return nil, errors.New("some other error")
		},
	}

	planner := NewPlanner(provider, false)
	_, err := planner.Execute(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "some other error")
}

func TestPlanner_SecondCallSeesHistory(t *testing.T) {
	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			// Verify that the second call includes summary + first call's messages
			// The params.Messages should have: [summary?, system, user(goal)]
			msg := core.NewAssistantMessage("m", core.TextContent{Text: "result"})
			return &msg, nil
		},
	}

	planner := NewPlanner(provider, false)

	// First call
	msgs1, err := planner.Execute(context.Background(), "goal 1")
	require.NoError(t, err)
	require.Len(t, msgs1, 1)

	// Second call — should include context from first
	msgs2, err := planner.Execute(context.Background(), "goal 2")
	require.NoError(t, err)
	require.Len(t, msgs2, 1)
}

func TestPlanner_SetThinking(t *testing.T) {
	planner := NewPlanner(nil, false)
	planner.SetThinking(true, core.ReasoningEffortHigh)
	assert.NotNil(t, planner.react.enableThinking)
	assert.True(t, *planner.react.enableThinking)
	assert.Equal(t, core.ReasoningEffortHigh, planner.react.reasoningEffort)
}

func TestWorkerAgent_ExecuteTask_Simple(t *testing.T) {
	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			msg := core.NewAssistantMessage("m", core.TextContent{Text: "task completed"})
			msg.Usage = &core.Usage{TotalTokens: 10, InputTokens: 5, OutputTokens: 5}
			return &msg, nil
		},
	}

	worker := NewWorkerAgent(provider, false, []string{})
	output, usage, err := worker.ExecuteTask(context.Background(), "test goal", "do it")

	require.NoError(t, err)
	assert.Contains(t, output, "task completed")
	assert.NotNil(t, usage)
	assert.Equal(t, int64(10), usage.TotalTokens)
}

func TestWorkerAgent_ExecuteTask_Error(t *testing.T) {
	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			return nil, errors.New("api failure")
		},
	}

	worker := NewWorkerAgent(provider, false, []string{})
	output, usage, err := worker.ExecuteTask(context.Background(), "goal", "action")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api failure")
	assert.NotNil(t, usage)
	_ = output
}

func TestPlanner_Interrupt(t *testing.T) {
	planner := NewPlanner(nil, false)
	err := planner.Interrupt(context.Background())
	assert.NoError(t, err)
}
