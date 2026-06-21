package core

import (
	"os"
	"strings"
)

// contextFiles lists the user-authored context files in load order.
var contextFiles = []struct {
	name  string
	label string
}{
	{"AGENTS.md", "Agent Capabilities"},
	{"IDENTITY.md", "Agent Identity"},
	{"SOUL.md", "Agent Personality"},
	{"USER.md", "User Profile"},
}

// LoadAgentContext reads AGENTS.md, IDENTITY.md, SOUL.md, and USER.md
// from the workspace directory. Missing or empty files are silently skipped.
// Returns a formatted string suitable for prepending to a system prompt.
func LoadAgentContext() string {
	dir := GetWorkspace().Dir()
	var sb strings.Builder

	for _, cf := range contextFiles {
		data, err := os.ReadFile(dir + "/" + cf.name)
		if err != nil || len(data) == 0 {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(content)
	}

	return sb.String()
}
