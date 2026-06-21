package skills

import (
	"os"
	"path/filepath"
	"testing"

	"kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSkill(t *testing.T, name, yamlFrontmatter, prompt string) {
	dir := filepath.Join(core.GetWorkspace().SkillsDir(), name)
	os.MkdirAll(dir, 0755)
	content := "---\n" + yamlFrontmatter + "\n---\n\n" + prompt
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644)
}

func TestLoadSkill_FullFrontmatter(t *testing.T) {
	old := core.GetWorkspace().Dir()
	core.GetWorkspace().SetDir(t.TempDir())
	defer core.GetWorkspace().SetDir(old)

	writeSkill(t, "code-reviewer",
		`name: code-reviewer
description: Review code for bugs and style
tools:
  - file_read
  - file_write`,
		"# Code Reviewer\n\nYou are a code reviewer. Report bugs clearly.")

	skill, err := Load("code-reviewer")
	require.NoError(t, err)
	assert.Equal(t, "code-reviewer", skill.Name)
	assert.Equal(t, "Review code for bugs and style", skill.Description)
	assert.Equal(t, []string{"file_read", "file_write"}, skill.Tools)
	assert.Contains(t, skill.Prompt, "You are a code reviewer")
}

func TestLoadSkill_MinimalFrontmatter(t *testing.T) {
	old := core.GetWorkspace().Dir()
	core.GetWorkspace().SetDir(t.TempDir())
	defer core.GetWorkspace().SetDir(old)

	writeSkill(t, "minimal", `name: minimal`, "Just do the thing.")

	skill, err := Load("minimal")
	require.NoError(t, err)
	assert.Equal(t, "minimal", skill.Name)
	assert.Empty(t, skill.Tools)
	assert.Equal(t, "Just do the thing.", skill.Prompt)
}

func TestLoadSkill_NotFound(t *testing.T) {
	old := core.GetWorkspace().Dir()
	core.GetWorkspace().SetDir(t.TempDir())
	defer core.GetWorkspace().SetDir(old)

	_, err := Load("nonexistent")
	assert.Error(t, err)
}

func TestLoadSkill_MissingName(t *testing.T) {
	old := core.GetWorkspace().Dir()
	core.GetWorkspace().SetDir(t.TempDir())
	defer core.GetWorkspace().SetDir(old)

	writeSkill(t, "noname", ``, "Some content")

	_, err := Load("noname")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestListSkills(t *testing.T) {
	old := core.GetWorkspace().Dir()
	core.GetWorkspace().SetDir(t.TempDir())
	defer core.GetWorkspace().SetDir(old)

	writeSkill(t, "alpha", `name: alpha`, "a")
	writeSkill(t, "beta", `name: beta`, "b")

	names, err := List()
	require.NoError(t, err)
	assert.Contains(t, names, "alpha")
	assert.Contains(t, names, "beta")
	assert.Len(t, names, 2)
}

func TestSkill_Context(t *testing.T) {
	skill := &Skill{
		Name:   "reviewer",
		Prompt: "You are a reviewer. Check for bugs.",
	}
	ctx := skill.Context()
	assert.Equal(t, "You are a reviewer. Check for bugs.", ctx)
}
