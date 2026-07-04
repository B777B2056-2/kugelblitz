package infra

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/stretchr/testify/assert"
)

func TestReviewer_Review_NoDrift(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			msg := core.NewAssistantMessage(core.ToolCallContent{
				Details: []core.ToolCallDetail{{
					ID: "tc-1", ToolName: "reviewer_report",
					Args: map[string]any{"drift": false, "reason": "All tasks aligned with goal"},
				}},
			})
			return &msg, nil
		},
	}
	reviewer := NewReviewer(provider)
	result := reviewer.Review(context.Background(), "deploy", "plan v5", "step")

	assert.False(t, result.Drift)
	assert.Equal(t, "All tasks aligned with goal", result.Reason)
}

func TestReviewer_Review_Drift(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			msg := core.NewAssistantMessage(core.ToolCallContent{
				Details: []core.ToolCallDetail{{
					ID: "tc-1", ToolName: "reviewer_report",
					Args: map[string]any{
						"drift":      true,
						"reason":     "Plan expanded to 8 unrelated tasks",
						"suggestion": "rollback to v4 and replan",
					},
				}},
			})
			return &msg, nil
		},
	}
	reviewer := NewReviewer(provider)
	result := reviewer.Review(context.Background(), "deploy", "plan v5, 8 tasks", "step")

	assert.True(t, result.Drift)
	assert.Contains(t, result.Reason, "8 unrelated tasks")
	assert.Equal(t, "rollback to v4 and replan", result.Suggestion)
}

func TestReviewer_Review_ProviderError(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			return nil, assert.AnError
		},
	}
	reviewer := NewReviewer(provider)
	result := reviewer.Review(context.Background(), "goal", "plan", "trigger")
	assert.False(t, result.Drift)
	assert.Contains(t, result.Reason, "reviewer error")
}

func TestReviewer_Review_PlainTextFallback(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			msg := core.NewAssistantMessage(core.TextContent{Text: "Everything looks good"})
			return &msg, nil
		},
	}
	reviewer := NewReviewer(provider)
	result := reviewer.Review(context.Background(), "goal", "plan", "trigger")
	assert.False(t, result.Drift)
	assert.Contains(t, result.Reason, "no reviewer_report call")
}
