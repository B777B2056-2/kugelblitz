package prompts

import (
	"fmt"
	"strings"

	"github.com/B777B2056-2/kugelblitz/core"
)

// ---- Compressor prompts ----

// BuildSummarizePrompt creates a prompt asking the LLM to produce a consolidated summary.
func BuildSummarizePrompt(messages []core.Message, existingSummary string) string {
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
			sb.WriteString(truncateStr(ct.Text, 500))
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

// BuildCompressFieldPrompt creates a prompt for summarizing a single tool result field.
func BuildCompressFieldPrompt(toolName, fieldKey string, rawLen int, raw string) string {
	return fmt.Sprintf(
		`Summarize this tool result field. Keep key items, IDs, file paths, error messages, and numbers.
Be concise but don't drop critical data. Output ONLY the summary, no preamble.

Tool: %s
Field: %s
Original length: %d chars

Content:
%s`, toolName, fieldKey, rawLen, raw,
	)
}

// ---- Dream prompts ----

// BuildMemoryScorePrompt creates a prompt for scoring memory items.
func BuildMemoryScorePrompt(itemsJSON string) string {
	return fmt.Sprintf(
		`You are a memory scoring system. Rate each memory item from 1-10:
- 1-3: low value (one-time event, outdated, already well-known)
- 4-6: moderate (useful but not critical)
- 7-10: high value (recurring theme, important preference, actionable insight)

Consider: recency (high confidence = recent), frequency (high version = updated often), graph connections (high degree = well-connected entity).

Output ONLY valid JSON:
{"scores": [{"section":"...","key":"...","score":N,"reason":"brief justification"}]}

Items:
%s`, itemsJSON,
	)
}

// BuildMemoryReflectionPrompt creates a prompt for reflecting on high-value memories (REM).
func BuildMemoryReflectionPrompt(itemsDesc string) string {
	return fmt.Sprintf(
		`You are a memory reflection system. Analyze these high-value memories and extract cross-cutting insights.

Output ONLY valid JSON:
{"insights": [{"section":"insights","key":"short_label","value":"detailed insight"}], "summary":"one-sentence summary of what the user is focused on"}

High-value memories:
%s`, itemsDesc,
	)
}

// ---- Helpers ----

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
