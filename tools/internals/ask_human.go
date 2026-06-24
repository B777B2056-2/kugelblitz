package internals

import (
	"context"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/tools"
)

// AskHumanTool pauses the agent loop and waits for a human response.
// It is NOT registered globally — each agent registers it locally via
// EnableHumanInTheLoop so the tool can hold a reference to the agent's HumanGate.
type AskHumanTool struct {
	Gate core.HumanGate
}

func (t *AskHumanTool) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name: "ask_human",
		Description: "Ask the human user a question and wait for their response. " +
			"Use this when you need clarification, need approval before proceeding, " +
			"or require additional information only the user can provide. " +
			"The agent will pause until the human responds.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "The question to ask the human user.",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Brief reason why you need to ask — helps the human understand context.",
				},
			},
			"required": []string{"question"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"response": map[string]any{
					"type":        "string",
					"description": "The human's response.",
				},
			},
		},
	}
}

func (t *AskHumanTool) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	question, err := tools.Arg(detail, "question")
	if err != nil {
		return tools.ErrorResult(detail.ID, "ask_human", err)
	}
	reason, _ := tools.Arg(detail, "reason")

	response, err := t.Gate.WaitForHuman(ctx, reason, question)
	if err != nil {
		return tools.ErrorResult(detail.ID, "ask_human", err)
	}
	return tools.SuccessResult(detail.ID, "ask_human", map[string]any{
		"response": response,
	})
}
