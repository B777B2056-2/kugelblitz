package infra

import (
	"context"
	"testing"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
)

func TestReviewer_Review_NoDrift(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
				Details: []coretypes.ToolCallDetail{{
					ID: "tc-1", ToolName: "reviewer_report",
					Args: map[string]any{"drift": false, "reason": "All tasks aligned with goal"},
				}},
			})
			return &msg, nil
		},
	}
	reviewer := NewReviewer(provider, otel.Tracer("test"))
	result := reviewer.Review(context.Background(), "deploy", "plan v5", "step")

	assert.False(t, result.Drift)
	assert.Equal(t, "All tasks aligned with goal", result.Reason)
}

func TestReviewer_Review_Drift(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
				Details: []coretypes.ToolCallDetail{{
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
	reviewer := NewReviewer(provider, otel.Tracer("test"))
	result := reviewer.Review(context.Background(), "deploy", "plan v5, 8 tasks", "step")

	assert.True(t, result.Drift)
	assert.Contains(t, result.Reason, "8 unrelated tasks")
	assert.Equal(t, "rollback to v4 and replan", result.Suggestion)
}

func TestReviewer_Review_ProviderError(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			return nil, assert.AnError
		},
	}
	reviewer := NewReviewer(provider, otel.Tracer("test"))
	result := reviewer.Review(context.Background(), "goal", "plan", "trigger")
	assert.False(t, result.Drift)
	assert.Contains(t, result.Reason, "reviewer error")
}

// ---- B14: a reviewer_report call missing or mis-typing the drift field must
// fail closed (Drift=false) with an explicit reason, not fail open.

func TestReviewer_Review_MissingDriftField(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
				Details: []coretypes.ToolCallDetail{{
					ID: "tc-1", ToolName: "reviewer_report",
					Args: map[string]any{"reason": "no drift field present"},
				}},
			})
			return &msg, nil
		},
	}
	reviewer := NewReviewer(provider, otel.Tracer("test"))
	result := reviewer.Review(context.Background(), "goal", "plan", "trigger")
	assert.False(t, result.Drift)
	assert.Contains(t, result.Reason, "drift field missing")
}

func TestReviewer_Review_NonBoolDriftField(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
				Details: []coretypes.ToolCallDetail{{
					ID: "tc-1", ToolName: "reviewer_report",
					Args: map[string]any{"drift": "yes", "reason": "drift is a string"},
				}},
			})
			return &msg, nil
		},
	}
	reviewer := NewReviewer(provider, otel.Tracer("test"))
	result := reviewer.Review(context.Background(), "goal", "plan", "trigger")
	assert.False(t, result.Drift)
	assert.Contains(t, result.Reason, "drift field missing")
}

func TestReviewer_Review_PlainTextFallback(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "Everything looks good"})
			return &msg, nil
		},
	}
	reviewer := NewReviewer(provider, otel.Tracer("test"))
	result := reviewer.Review(context.Background(), "goal", "plan", "trigger")
	assert.False(t, result.Drift)
	assert.Contains(t, result.Reason, "no reviewer_report call")
}
