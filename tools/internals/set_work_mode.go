package internals

import (
	"context"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/tools"
)

// SetWorkMode is the Phase 1 intent-recognition tool. The Planner calls it
// to choose between plan mode and direct (simple) execution.
type SetWorkMode struct{}

func (t *SetWorkMode) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "set_work_mode",
		Terminating: true,
		Description: "Set the execution mode. Use 'plan' for complex multi-step tasks " +
			"that require planning and task decomposition. Use 'simple' for " +
			"straightforward single-step tasks that can be done directly.",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mode": map[string]any{
					"type":        "string",
					"description": "Execution mode: 'plan' or 'simple'",
				},
			},
			"required": []string{"mode"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mode": map[string]any{"type": "string"},
			},
		},
	}
}

func (t *SetWorkMode) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	mode, err := tools.Arg(detail, "mode")
	if err != nil {
		return tools.ErrorResult(detail.ID, "set_work_mode", err)
	}
	if err := tools.In(mode, "plan", "simple"); err != nil {
		return tools.ErrorResult(detail.ID, "set_work_mode", err)
	}
	return tools.SuccessResult(detail.ID, "set_work_mode", map[string]any{"mode": mode})
}
