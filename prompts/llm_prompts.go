package prompts

import (
	"fmt"
	"strings"

	"github.com/B777B2056-2/kugelblitz/core/types"
)

// FormatMessages renders a message history as the compact text body consumed by
// the TypeSummarize template. It mirrors the message-dump format previously
// inlined in BuildSummarizePrompt: each message becomes "[i] role: <content>".
func FormatMessages(messages []types.Message) string {
	var sb strings.Builder
	for i, msg := range messages {
		fmt.Fprintf(&sb, "[%d] %s: ", i, msg.Role)
		switch ct := msg.Content.(type) {
		case types.TextContent:
			sb.WriteString(truncateStr(ct.Text, 500))
		case types.ToolCallContent:
			var names []string
			for _, d := range ct.Details {
				names = append(names, d.ToolName)
			}
			fmt.Fprintf(&sb, "[tool calls: %s]", strings.Join(names, ", "))
		case types.ToolResultContent:
			fmt.Fprintf(&sb, "[tool results: %d]", len(ct.Results))
		case types.CompositeContent:
			sb.WriteString("[composite]")
		default:
			sb.WriteString("[content]")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
