package internals

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/skills"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillUse_ActivatesSkill(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())

	skill := &skills.Skill{
		Name:   "reviewer",
		Prompt: "# Reviewer\nYou review code.",
		Tools:  []string{"file_read"},
	}

	tool := &SkillUse{active: &skills.Skill{}, skills: []*skills.Skill{skill}}

	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "s1", ToolName: "skill_use",
		Args: map[string]any{"name": "reviewer"},
	})

	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, true, result.Outputs["activated"])
	assert.Equal(t, "reviewer", result.Outputs["name"])
	assert.Contains(t, result.Outputs["prompt"], "You review code")
}

func TestSkillUse_NotFound(t *testing.T) {
	tool := &SkillUse{active: &skills.Skill{}, skills: nil}

	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "s1", ToolName: "skill_use",
		Args: map[string]any{"name": "nonexistent"},
	})
	assert.NotNil(t, result.Outputs["error"])
}

func TestSkillUse_NoArgsDeactivates(t *testing.T) {
	skill := &skills.Skill{Name: "active", Prompt: "p"}
	tool := &SkillUse{active: skill, skills: []*skills.Skill{skill}}

	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "s1", ToolName: "skill_use",
		Args: map[string]any{},
	})
	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, true, result.Outputs["deactivated"])
	assert.Equal(t, "", tool.active.Name)
}

func TestSkillUse_AvailableTools(t *testing.T) {
	skill := &skills.Skill{
		Name:   "deployer",
		Prompt: "You deploy.",
		Tools:  []string{"file_write", "dir_create"},
	}
	tool := &SkillUse{active: &skills.Skill{}, skills: []*skills.Skill{skill}}

	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "s1", ToolName: "skill_use",
		Args: map[string]any{"name": "deployer"},
	})

	require.Nil(t, result.Outputs["error"])
	tools, ok := result.Outputs["skill_tools"].([]string)
	require.True(t, ok)
	assert.Len(t, tools, 2)
	assert.Contains(t, tools, "file_write")
	assert.Contains(t, tools, "dir_create")
}

func TestSkillUse_Deactivate(t *testing.T) {
	skill := &skills.Skill{Name: "r", Prompt: "p"}
	tool := &SkillUse{active: skill, skills: []*skills.Skill{skill}}

	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "s1", ToolName: "skill_use",
		Args: map[string]any{},
	})
	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, true, result.Outputs["deactivated"])
	assert.Equal(t, "", tool.active.Name)
}
