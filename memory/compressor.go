package memory

import (
	"context"
	"fmt"
	"strings"

	"kugelblitz/core"
)

// Compressor handles conversation summarization via an LLM provider.
// It extracts key facts, decisions, and context from old messages
// to produce a compact summary that replaces them.
type Compressor struct {
	provider core.ILMProvider
}

// NewCompressor creates a Compressor backed by the given LLM provider.
func NewCompressor(provider core.ILMProvider) *Compressor {
	return &Compressor{provider: provider}
}

// Summarize sends messages to the LLM and returns a summary string and token usage.
// existingSummary is a prior summary to carry forward (may be empty).
func (c *Compressor) Summarize(ctx context.Context, messages []core.Message, existingSummary string) (string, *core.Usage, error) {
	prompt := buildSummarizePrompt(messages, existingSummary)

	userMsg := core.NewUserMessage("compressor", core.TextContent{Text: prompt})
	params := core.GenerateParams{
		Messages: []core.Message{userMsg},
		Stream:   false,
	}

	result, err := c.provider.Generate(ctx, params)
	if err != nil {
		return "", nil, fmt.Errorf("compressor: generate: %w", err)
	}

	if tc, ok := result.Content.(core.TextContent); ok {
		return tc.Text, result.Usage, nil
	}
	return "", result.Usage, fmt.Errorf("compressor: unexpected response type: %T", result.Content)
}

// buildSummarizePrompt creates a prompt asking the LLM to produce a unified,
// conflict-resolved summary that consolidates the existing summary with new messages.
func buildSummarizePrompt(messages []core.Message, existingSummary string) string {
	var sb strings.Builder

	if existingSummary != "" {
		sb.WriteString("You are maintaining a running summary of an ongoing conversation.\n\n")
		sb.WriteString("EXISTING SUMMARY:\n")
		sb.WriteString(existingSummary)
		sb.WriteString("\n\n")
		sb.WriteString("Below are NEW messages that continue the conversation. ")
		sb.WriteString("Produce a single CONSOLIDATED summary that:\n")
		sb.WriteString("- Incorporates key facts from both the existing summary and the new messages\n")
		sb.WriteString("- If new information contradicts the existing summary, PREFER the new information\n")
		sb.WriteString("- Removes outdated or superseded facts\n")
		sb.WriteString("- Is concise (under 500 words)\n\n")
	} else {
		sb.WriteString("Summarize the following conversation segment. ")
		sb.WriteString("Extract key facts, decisions, tool call results, and important context. ")
		sb.WriteString("Be concise but complete (under 500 words).\n\n")
	}

	sb.WriteString("--- Messages ---\n\n")
	for i, msg := range messages {
		fmt.Fprintf(&sb, "[%d] %s: ", i, msg.Role)
		switch ct := msg.Content.(type) {
		case core.TextContent:
			sb.WriteString(truncate(ct.Text, 500))
		case core.ToolCallContent:
			var names []string
			for _, d := range ct.Details {
				names = append(names, d.ToolName)
			}
			fmt.Fprintf(&sb, "[tool calls: %s]", strings.Join(names, ", "))
		case core.ToolResultContent:
			fmt.Fprintf(&sb, "[tool results: %d]", len(ct.Results))
		case core.CompositeContent:
			sb.WriteString("[composite]")
		default:
			sb.WriteString("[content]")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n--- End of messages ---\n")
	sb.WriteString("Provide the consolidated summary (under 500 words):")
	return sb.String()
}

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

