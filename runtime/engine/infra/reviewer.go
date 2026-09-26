package infra

import (
	"context"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/prompts"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Reviewer checks for goal drift using a dedicated tool call.
type Reviewer struct {
	Provider core.ILMProvider
	Hooks    core.AgentEventHooks
	tracer   trace.Tracer
}

// ReviewResult reports a goal-drift review outcome.
// Aliased from the leaf types package so callers may reference either name.
type ReviewResult = types.ReviewResult

func NewReviewer(provider core.ILMProvider, tracer trace.Tracer) *Reviewer {
	return &Reviewer{Provider: provider, tracer: tracer}
}

func (r *Reviewer) SetHooks(hooks core.AgentEventHooks) { r.Hooks = hooks }

func (r *Reviewer) SetProvider(p core.ILMProvider) { r.Provider = p }

// Review does a single Generate call with a reviewer_report tool.
func (r *Reviewer) Review(ctx context.Context, originalGoal, planSummary, recentActivity string) ReviewResult {
	ctx, span := r.tracer.Start(ctx, "reviewer.check")
	defer span.End()

	text, err := prompts.DefaultFactory.Render(prompts.TypeReview, prompts.ReviewParams{
		OriginalGoal: originalGoal, PlanSummary: planSummary, RecentActivity: recentActivity,
	})
	if err != nil {
		return ReviewResult{Drift: false, Reason: "prompt render: " + err.Error()}
	}
	userMsg := core.NewUserMessage(core.TextContent{Text: text})
	params := core.GenerateParams{
		Messages: []core.Message{userMsg},
		Tools: []core.ToolDefinition{{
			Name:        "reviewer_report",
			Description: "Report your goal-alignment assessment.",
			JSONSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"drift":      map[string]any{"type": "boolean", "description": "true if execution has drifted from the original goal"},
					"reason":     map[string]any{"type": "string", "description": "Brief explanation"},
					"suggestion": map[string]any{"type": "string", "description": "Suggested action if drift detected"},
				},
				"required": []string{"drift", "reason"},
			},
		}},
		Stream:       false,
		EventHandler: r.Hooks.AsModelEventHandler(constants.AgentReviewer),
	}

	result, err := r.Provider.Generate(ctx, params)
	if err != nil {
		span.RecordError(err)
		var usage *core.Usage
		if result != nil {
			usage = result.Usage
		}
		return ReviewResult{Drift: false, Reason: "reviewer error: " + err.Error(), Usage: usage}
	}

	if result.Usage != nil {
		span.SetAttributes(
			attribute.Int64("tokens_in", result.Usage.InputTokens),
			attribute.Int64("tokens_out", result.Usage.OutputTokens),
		)
	}

	if tc, ok := result.Content.(core.ToolCallContent); ok {
		for _, d := range tc.Details {
			if d.ToolName == "reviewer_report" {
				drift, driftOK := d.Args["drift"].(bool)
				if !driftOK {
					return ReviewResult{Drift: false, Reason: "reviewer_report: drift field missing or non-bool", Usage: result.Usage}
				}
				reason, _ := d.Args["reason"].(string)
				suggestion, _ := d.Args["suggestion"].(string)
				span.SetAttributes(attribute.Bool("drift_detected", drift))
				span.SetAttributes(attribute.String("drift_reason", reason))
				return ReviewResult{Drift: drift, Reason: reason, Suggestion: suggestion, Usage: result.Usage}
			}
		}
	}

	return ReviewResult{Drift: false, Reason: "no reviewer_report call received", Usage: result.Usage}
}
