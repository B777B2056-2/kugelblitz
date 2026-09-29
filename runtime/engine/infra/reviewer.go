package infra

import (
	"context"
	"errors"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/events"
	"github.com/B777B2056-2/kugelblitz/llm"
	"github.com/B777B2056-2/kugelblitz/prompts"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/types"
)

// Reviewer checks for goal drift using a dedicated tool call.
type Reviewer struct {
	caller *llm.Caller
	bus    *events.Bus
}

// ReviewResult reports a goal-drift review outcome.
// Aliased from the leaf types package so callers may reference either name.
type ReviewResult = types.ReviewResult

func NewReviewer(caller *llm.Caller) *Reviewer {
	return &Reviewer{caller: caller, bus: events.NewBus()}
}

// SetBus attaches the shared per-loop bus this reviewer emits into.
func (r *Reviewer) SetBus(bus *events.Bus) {
	if bus == nil {
		bus = events.NewBus()
	}
	r.bus = bus
}

// SetProvider swaps the underlying LLM provider at runtime (e.g. per-input
// multimodal model selection). A nil caller is a no-op (test stubs).
func (r *Reviewer) SetProvider(p coretypes.ILMProvider) {
	if r.caller != nil {
		r.caller.SetProvider(p)
	}
}

// Review does a single tool-call generation via the unified caller.
func (r *Reviewer) Review(ctx context.Context, originalGoal, planSummary, recentActivity string) ReviewResult {
	text, err := prompts.DefaultFactory.Render(prompts.TypeReview, prompts.ReviewParams{
		OriginalGoal: originalGoal, PlanSummary: planSummary, RecentActivity: recentActivity,
	})
	if err != nil {
		return ReviewResult{Drift: false, Reason: "prompt render: " + err.Error()}
	}
	userMsg := coretypes.NewUserMessage(coretypes.TextContent{Text: text})

	res, err := r.caller.Call(ctx, llm.Request{
		Messages: []coretypes.Message{userMsg},
		Mode:     llm.ModeToolCall,
		Tool: &coretypes.ToolDefinition{
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
		},
		SpanName:     "reviewer.check",
		EventHandler: core.NewBusModelHandler(r.bus, constants.AgentReviewer, ""),
	})
	if err != nil {
		var usage *coretypes.Usage
		if res != nil {
			usage = res.Usage
		}
		if errors.Is(err, llm.ErrNoToolCall) {
			return ReviewResult{Drift: false, Reason: "no reviewer_report call received", Usage: usage}
		}
		return ReviewResult{Drift: false, Reason: "reviewer error: " + err.Error(), Usage: usage}
	}

	drift, driftOK := res.Args["drift"].(bool)
	if !driftOK {
		return ReviewResult{Drift: false, Reason: "reviewer_report: drift field missing or non-bool", Usage: res.Usage}
	}
	reason, _ := res.Args["reason"].(string)
	suggestion, _ := res.Args["suggestion"].(string)
	return ReviewResult{Drift: drift, Reason: reason, Suggestion: suggestion, Usage: res.Usage}
}
