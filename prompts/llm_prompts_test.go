package prompts

import (
	"testing"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/stretchr/testify/assert"
)

// These tests relocate the prompt-content assertions that previously lived in
// memory/compressor_test.go against BuildSummarizePrompt. The message-dump
// behavior now lives in FormatMessages, and the summary-template branching lives
// in TypeSummarize, so each is asserted at its new home.

func TestSummarizeTemplate_NoExistingSummary(t *testing.T) {
	msgs := []coretypes.Message{
		coretypes.NewUserMessage(coretypes.TextContent{Text: "hello"}),
		coretypes.NewAssistantMessage(coretypes.TextContent{Text: "world"}),
	}
	prompt, err := DefaultFactory.Render(TypeSummarize, SummarizeParams{
		Messages: FormatMessages(msgs),
	})
	assert.NoError(t, err)
	assert.Contains(t, prompt, "Summarize the following conversation")
	assert.Contains(t, prompt, "hello")
	assert.Contains(t, prompt, "world")
	assert.NotContains(t, prompt, "EXISTING SUMMARY")
	assert.NotContains(t, prompt, "previous summary")
}

func TestSummarizeTemplate_WithExistingSummary(t *testing.T) {
	msgs := []coretypes.Message{
		coretypes.NewUserMessage(coretypes.TextContent{Text: "new info"}),
	}
	existing := "User likes Go programming."
	prompt, err := DefaultFactory.Render(TypeSummarize, SummarizeParams{
		ExistingSummary: existing,
		Messages:        FormatMessages(msgs),
	})
	assert.NoError(t, err)
	assert.Contains(t, prompt, "EXISTING SUMMARY")
	assert.Contains(t, prompt, existing)
	assert.Contains(t, prompt, "CONSOLIDATED")
	assert.Contains(t, prompt, "PREFER the new information")
	assert.Contains(t, prompt, "new info")
}

func TestFormatMessages_ToolCalls(t *testing.T) {
	msgs := []coretypes.Message{
		{
			Role: "assistant",
			Content: coretypes.ToolCallContent{
				Details: []coretypes.ToolCallDetail{
					{ID: "t1", ToolName: "search"},
					{ID: "t2", ToolName: "calculate"},
				},
			},
		},
	}
	assert.Contains(t, FormatMessages(msgs), "[tool calls: search, calculate]")
}

func TestFormatMessages_ToolResults(t *testing.T) {
	msgs := []coretypes.Message{
		{
			Role: "tool",
			Content: coretypes.ToolResultContent{
				Results: []coretypes.ToolCallResult{
					{ToolCallID: "t1"},
					{ToolCallID: "t2"},
					{ToolCallID: "t3"},
				},
			},
		},
	}
	assert.Contains(t, FormatMessages(msgs), "[tool results: 3]")
}
