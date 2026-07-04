package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "deepseek", cfg.Model.ProviderName)
	assert.Equal(t, "deepseek-v4-flash", cfg.Model.Model)
	assert.Equal(t, "https://api.deepseek.com", cfg.Model.BaseURL)
	assert.True(t, cfg.Model.StreamMode)
	assert.True(t, cfg.Model.EnableThinking)
	assert.Equal(t, "high", cfg.Model.ReasoningEffort)
	assert.Equal(t, 30, cfg.Runtime.MaxStateMachineCycles)
	assert.Nil(t, cfg.MCP)
}

func TestNewProvider_DeepSeek(t *testing.T) {
	p, err := NewProvider("deepseek", "sk-test", "https://api.deepseek.com", "deepseek-chat")
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewProvider_OpenAI(t *testing.T) {
	p, err := NewProvider("openai", "sk-test", "https://api.openai.com/v1", "gpt-4")
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewProvider_Unknown(t *testing.T) {
	_, err := NewProvider("unknown", "", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

func TestMCPServerConfig_JSONTags(t *testing.T) {
	cfg := MCPServerConfig{
		Command: "python3",
		Args:    []string{"server.py"},
		Env:     map[string]string{"KEY": "val"},
	}
	assert.Equal(t, "python3", cfg.Command)
	assert.Equal(t, []string{"server.py"}, cfg.Args)
	assert.Equal(t, "val", cfg.Env["KEY"])
}
