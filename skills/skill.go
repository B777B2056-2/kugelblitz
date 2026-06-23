package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/B777B2056-2/kugelblitz/core"

	"gopkg.in/yaml.v3"
)

// Skill represents a loaded skill from a SKILL.md file.
type Skill struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools"`
	Prompt      string   // everything after --- (raw markdown)
}

// Context returns the skill prompt suitable for injecting into system prompt.
func (s *Skill) Context() string {
	return s.Prompt
}

// Load reads workspace/skills/{name}/SKILL.md and parses its frontmatter.
func Load(name string) (*Skill, error) {
	path := filepath.Join(core.GetWorkspace().SkillsDir(), name, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load skill %q: %w", name, err)
	}
	fm, prompt, err := parseFrontmatter(string(data))
	if err != nil {
		return nil, fmt.Errorf("load skill %q: %w", name, err)
	}
	var skill Skill
	if err := yaml.Unmarshal([]byte(fm), &skill); err != nil {
		return nil, fmt.Errorf("load skill %q: invalid YAML: %w", name, err)
	}
	if skill.Name == "" {
		return nil, fmt.Errorf("load skill %q: name is required", name)
	}
	skill.Prompt = strings.TrimSpace(prompt)
	return &skill, nil
}

// List returns names of all available skills.
func List() ([]string, error) {
	entries, err := os.ReadDir(core.GetWorkspace().SkillsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

func parseFrontmatter(content string) (frontmatter, prompt string, _ error) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return "", content, nil
	}
	rest := content[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", content, nil
	}
	return strings.TrimSpace(rest[:end]), strings.TrimSpace(rest[end+4:]), nil
}
