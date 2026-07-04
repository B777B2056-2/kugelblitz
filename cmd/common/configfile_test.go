package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyRaw_FlatYAML_MCPServers(t *testing.T) {
	raw := map[string]any{
		"provider_name": "deepseek",
		"model":         "deepseek-v4-flash",
		"mcp_servers": map[string]any{
			"test": map[string]any{
				"command": "python3",
				"args":    []any{"server.py"},
			},
		},
	}

	cfg := config.DefaultConfig()
	applyRaw(raw, &cfg)

	require.NotNil(t, cfg.MCP)
	assert.Contains(t, cfg.MCP, "test")
	assert.Equal(t, "python3", cfg.MCP["test"].Command)
	assert.Equal(t, []string{"server.py"}, cfg.MCP["test"].Args)
}

func TestApplyRaw_NestedYAML_MCPServers(t *testing.T) {
	raw := map[string]any{
		"model": map[string]any{
			"provider_name": "openai",
			"model":         "gpt-4",
		},
		"runtime": map[string]any{
			"max_state_machine_cycles": 50,
		},
		"mcp_servers": map[string]any{
			"github": map[string]any{
				"command": "npx",
				"args":    []any{"-y", "@modelcontextprotocol/server-github"},
				"env": map[string]any{
					"GITHUB_TOKEN": "ghp_test",
				},
			},
		},
	}

	cfg := config.DefaultConfig()
	applyRaw(raw, &cfg)

	assert.Equal(t, "openai", cfg.Model.ProviderName)
	assert.Equal(t, "gpt-4", cfg.Model.Model)
	assert.Equal(t, 50, cfg.Runtime.MaxStateMachineCycles)

	require.NotNil(t, cfg.MCP)
	assert.Contains(t, cfg.MCP, "github")
	assert.Equal(t, "npx", cfg.MCP["github"].Command)
	assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-github"}, cfg.MCP["github"].Args)
	assert.Equal(t, "ghp_test", cfg.MCP["github"].Env["GITHUB_TOKEN"])
}

func TestApplyRaw_NestedYAML_FlatKeysTakePriority(t *testing.T) {
	raw := map[string]any{
		"provider_name": "deepseek",
		"model": map[string]any{
			"provider_name": "openai",
			"model":         "gpt-4",
		},
	}

	cfg := config.DefaultConfig()
	applyRaw(raw, &cfg)

	// Nested values overwrite flat keys (backward compat)
	assert.Equal(t, "openai", cfg.Model.ProviderName)
	assert.Equal(t, "gpt-4", cfg.Model.Model)
}

func TestSave_FlatFormat(t *testing.T) {
	dir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.Model.ProviderName = "deepseek"
	cfg.Model.Model = "deepseek-v4-flash"
	cfg.MCP = map[string]config.MCPServerConfig{
		"test": {
			Command: "python3",
			Args:    []string{"server.py"},
		},
	}

	path := filepath.Join(dir, "kugelblitz.yaml")
	err := Save(path, cfg)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	yamlStr := string(data)

	assert.NotContains(t, yamlStr, "\nmodel:\n", "should not have nested model key")
	assert.Contains(t, yamlStr, "provider_name:")
	assert.Contains(t, yamlStr, "model:")
	assert.Contains(t, yamlStr, "mcp_servers:")
	assert.Contains(t, yamlStr, "command:")
}

func TestRoundTrip_MCPServers(t *testing.T) {
	dir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.Model.ProviderName = "deepseek"
	cfg.Model.Model = "deepseek-v4-flash"
	cfg.Model.APIKey = "sk-test"
	cfg.MCP = map[string]config.MCPServerConfig{
		"test": {
			Command: "python3",
			Args:    []string{"server.py"},
		},
	}

	path := filepath.Join(dir, "kugelblitz.yaml")

	// Save
	err := Save(path, cfg)
	require.NoError(t, err)

	// Load
	loaded, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "deepseek", loaded.Model.ProviderName)
	assert.Equal(t, "deepseek-v4-flash", loaded.Model.Model)
	assert.Equal(t, "sk-test", loaded.Model.APIKey)
	assert.Equal(t, true, loaded.Model.StreamMode)

	require.NotNil(t, loaded.MCP)
	assert.Contains(t, loaded.MCP, "test")
	assert.Equal(t, "python3", loaded.MCP["test"].Command)
	assert.Equal(t, []string{"server.py"}, loaded.MCP["test"].Args)
}

func TestRoundTrip_EmptyMCP(t *testing.T) {
	dir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.Model.ProviderName = "openai"
	cfg.MCP = nil

	path := filepath.Join(dir, "kugelblitz.yaml")

	err := Save(path, cfg)
	require.NoError(t, err)

	loaded, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "openai", loaded.Model.ProviderName)
	assert.Nil(t, loaded.MCP)
}
