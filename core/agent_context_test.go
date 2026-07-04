package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFiles(ws *Workspace, files map[string]string) {
	_ = ws.MkdirAll()
	for name, content := range files {
		_ = os.WriteFile(filepath.Join(ws.Dir(), name), []byte(content), 0644)
	}
}

func TestLoadAgentContext_AllFilesPresent(t *testing.T) {
	old := GetWorkspace().Dir()
	GetWorkspace().SetDir(t.TempDir())
	defer GetWorkspace().SetDir(old)

	writeFiles(GetWorkspace(), map[string]string{
		"AGENTS.md":   "# Agents\nYou have file tools.",
		"IDENTITY.md": "# Identity\nYou are Kugelblitz, a Go agent.",
		"SOUL.md":     "# Soul\nBe concise and helpful.",
		"USER.md":     "# User\nPrefers Go language.",
	})

	ctx := LoadAgentContext()

	assert.Contains(t, ctx, "You have file tools")
	assert.Contains(t, ctx, "Kugelblitz")
	assert.Contains(t, ctx, "concise and helpful")
	assert.Contains(t, ctx, "Prefers Go language")
}

func TestLoadAgentContext_PartialFiles(t *testing.T) {
	old := GetWorkspace().Dir()
	GetWorkspace().SetDir(t.TempDir())
	defer GetWorkspace().SetDir(old)

	writeFiles(GetWorkspace(), map[string]string{
		"AGENTS.md": "# Agents\nTools available.",
	})

	ctx := LoadAgentContext()

	assert.Contains(t, ctx, "Tools available")
	assert.NotContains(t, ctx, "IDENTITY")
	assert.NotContains(t, ctx, "SOUL")
	assert.NotContains(t, ctx, "USER")
}

func TestLoadAgentContext_NoFiles(t *testing.T) {
	old := GetWorkspace().Dir()
	GetWorkspace().SetDir(t.TempDir())
	defer GetWorkspace().SetDir(old)

	ctx := LoadAgentContext()
	assert.Empty(t, ctx)
}

func TestLoadAgentContext_Order(t *testing.T) {
	old := GetWorkspace().Dir()
	GetWorkspace().SetDir(t.TempDir())
	defer GetWorkspace().SetDir(old)

	writeFiles(GetWorkspace(), map[string]string{
		"AGENTS.md":   "AGENT",
		"IDENTITY.md": "ID",
	})

	ctx := LoadAgentContext()
	agentIdx := assertIndexBefore(t, ctx, "AGENT", "ID")
	assert.True(t, agentIdx >= 0)
}

func TestLoadAgentContext_EmptyFileSkipped(t *testing.T) {
	old := GetWorkspace().Dir()
	GetWorkspace().SetDir(t.TempDir())
	defer GetWorkspace().SetDir(old)

	writeFiles(GetWorkspace(), map[string]string{
		"AGENTS.md":   "",
		"IDENTITY.md": "# ID\nI am here.",
	})

	ctx := LoadAgentContext()
	assert.NotContains(t, ctx, "AGENTS.md")
	assert.Contains(t, ctx, "I am here")
}

func assertIndexBefore(t *testing.T, s, first, second string) int {
	i1 := indexOf(s, first)
	i2 := indexOf(s, second)
	require.Greater(t, i2, i1, "%q should appear before %q", first, second)
	return i1
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
