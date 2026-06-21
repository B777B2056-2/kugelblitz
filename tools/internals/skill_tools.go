package internals

import (
	"context"
	"fmt"

	"kugelblitz/core"
	"kugelblitz/skills"
	"kugelblitz/tools"
)

// SkillUse activates a skill by name. The active skill's prompt is injected
// into the LLM's context. Call with no args to deactivate.
type SkillUse struct {
	active *skills.Skill   // currently active skill
	skills []*skills.Skill // all available skills
}

func (t *SkillUse) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "skill_use",
		Description: "Activate a skill by name to take on a specialized role (e.g. 'code-reviewer', 'researcher'). Call with no name to deactivate. Returns the skill's prompt, description, and available tools.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Skill name to activate (omit to deactivate)"},
			},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"activated":    map[string]any{"type": "boolean", "description": "Whether a skill was activated"},
				"deactivated":  map[string]any{"type": "boolean", "description": "Whether skill was deactivated"},
				"name":         map[string]any{"type": "string", "description": "Skill name"},
				"prompt":       map[string]any{"type": "string", "description": "Skill prompt to follow"},
				"skill_tools":  map[string]any{"type": "array", "description": "Tools the skill recommends"},
			},
		},
	}
}

func (t *SkillUse) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	name, _ := tools.Arg(detail, "name")

	// Deactivate
	if name == "" {
		t.active.Name = ""
		return tools.SuccessResult(detail.ID, "skill_use", map[string]any{
			"deactivated": true,
			"message":     "Skill deactivated",
		})
	}

	// Activate
	for _, s := range t.skills {
		if s.Name == name {
			*t.active = *s
			return tools.SuccessResult(detail.ID, "skill_use", map[string]any{
				"activated":   true,
				"name":        s.Name,
				"description": s.Description,
				"prompt":      s.Prompt,
				"skill_tools": s.Tools,
			})
		}
	}

	return tools.ErrorResult(detail.ID, "skill_use", fmt.Errorf("skill %q not found", name))
}

// ActiveSkill returns the currently active skill (or nil).
func (t *SkillUse) ActiveSkill() *skills.Skill { return t.active }

// RegisterSkillTool registers skill_use with the global ToolRegistry.
func RegisterSkillTool(skillList []*skills.Skill, active *skills.Skill) *SkillUse {
	s := &SkillUse{skills: skillList, active: active}
	tools.Register(s)
	return s
}
