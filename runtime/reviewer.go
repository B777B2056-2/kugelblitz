package runtime

import (
	"context"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/core"
)

// Reviewer checks for goal drift using a dedicated tool call.
type Reviewer struct {
	provider core.ILMProvider
}

type ReviewResult struct {
	Drift      bool
	Reason     string
	Suggestion string
	Usage      *core.Usage // token usage from the review LLM call
}

func NewReviewer(provider core.ILMProvider) *Reviewer {
	return &Reviewer{provider: provider}
}

// Review does a single Generate call with a reviewer_report tool to get
// structured drift assessment via function calling.
func (r *Reviewer) Review(ctx context.Context, originalGoal, planSummary, recentActivity string) ReviewResult {
	prompt := fmt.Sprintf(`You are a goal-alignment reviewer.

ORIGINAL GOAL: %s

CURRENT PLAN STATE: %s

RECENT ACTIVITY: %s

Analyze whether the current plan execution has drifted from the original goal.
Check for: irrelevant tasks, scope creep, repetitive failures, context shift.
You MUST call reviewer_report with your assessment.`, originalGoal, planSummary, recentActivity)

	userMsg := core.NewUserMessage("reviewer", core.TextContent{Text: prompt})
	params := core.GenerateParams{
		Messages: []core.Message{userMsg},
		Tools: []core.ToolDefinition{{
			Name:        "reviewer_report",
			Description: "Report your goal-alignment assessment.",
			JsonSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"drift":      map[string]any{"type": "boolean", "description": "true if execution has drifted from the original goal"},
					"reason":     map[string]any{"type": "string", "description": "Brief explanation"},
					"suggestion": map[string]any{"type": "string", "description": "Suggested action if drift detected"},
				},
				"required": []string{"drift", "reason"},
			},
		}},
		Stream: false,
	}

	result, err := r.provider.Generate(ctx, params)
	if err != nil {
		var usage *core.Usage
		if result != nil {
			usage = result.Usage
		}
		return ReviewResult{Drift: false, Reason: "reviewer error: " + err.Error(), Usage: usage}
	}

	if tc, ok := result.Content.(core.ToolCallContent); ok {
		for _, d := range tc.Details {
			if d.ToolName == "reviewer_report" {
				drift, _ := d.Args["drift"].(bool)
				reason, _ := d.Args["reason"].(string)
				suggestion, _ := d.Args["suggestion"].(string)
				return ReviewResult{Drift: drift, Reason: reason, Suggestion: suggestion, Usage: result.Usage}
			}
		}
	}

	return ReviewResult{Drift: false, Reason: "no reviewer_report call received", Usage: result.Usage}
}
