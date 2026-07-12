package main

import (
	"testing"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToServerConfig_PassesAPIKeysThroughUnmasked(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Model.APIKey = "sk-this-is-a-very-long-api-key-12345678"
	cfg.Multimodal.ImageModel = &config.ModelConfig{
		ProviderName: "openai",
		Model:        "gpt-4o",
		APIKey:       "sk-image-key-abcdefgh",
	}
	cfg.Multimodal.AudioModel = &config.ModelConfig{
		ProviderName: "deepseek",
		Model:        "deepseek-audio",
		APIKey:       "sk-audio-key-1234abcd",
	}

	sc := toServerConfig(cfg)

	// API keys should pass through unchanged — no masking
	assert.Equal(t, "sk-this-is-a-very-long-api-key-12345678", sc.APIKey)
	assert.Equal(t, "sk-image-key-abcdefgh", sc.ImageAPIKey)
	assert.Equal(t, "sk-audio-key-1234abcd", sc.AudioAPIKey)
}

func TestToServerConfig_NilMultimodalReturnsEmpty(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Model.APIKey = "sk-main-key"
	cfg.Multimodal.ImageModel = nil
	cfg.Multimodal.AudioModel = nil

	sc := toServerConfig(cfg)

	assert.Equal(t, "sk-main-key", sc.APIKey)
	assert.Equal(t, "", sc.ImageAPIKey)
	assert.Equal(t, "", sc.AudioAPIKey)
}

func TestFromServerConfig_RoundTrip(t *testing.T) {
	existingCfg := config.DefaultConfig()
	existingCfg.Model.APIKey = "sk-real-master-key-1234abcd"
	existingCfg.Multimodal.ImageModel = &config.ModelConfig{
		ProviderName: "openai",
		Model:        "gpt-4o",
		APIKey:       "sk-real-image-key-5678efgh",
	}
	existingCfg.Multimodal.AudioModel = &config.ModelConfig{
		ProviderName: "deepseek",
		Model:        "deepseek-audio",
		APIKey:       "sk-real-audio-key-9012ijkl",
	}

	// Round-trip via toServerConfig → fromServerConfig
	sc := toServerConfig(existingCfg)
	result := fromServerConfig(sc, existingCfg)

	assert.Equal(t, "sk-real-master-key-1234abcd", result.Model.APIKey)
	require.NotNil(t, result.Multimodal.ImageModel)
	assert.Equal(t, "sk-real-image-key-5678efgh", result.Multimodal.ImageModel.APIKey)
	require.NotNil(t, result.Multimodal.AudioModel)
	assert.Equal(t, "sk-real-audio-key-9012ijkl", result.Multimodal.AudioModel.APIKey)
}

func TestFromServerConfig_NewAPIKeyAccepted(t *testing.T) {
	existingCfg := config.DefaultConfig()
	existingCfg.Model.APIKey = "sk-old-key"

	// User types a new key in the frontend
	sc := ServerConfig{
		ProviderName: "openai",
		Model:        "gpt-4",
		BaseURL:      "https://api.openai.com/v1",
		APIKey:       "sk-new-real-key",
	}

	result := fromServerConfig(sc, existingCfg)
	assert.Equal(t, "sk-new-real-key", result.Model.APIKey)
}

func TestFromServerConfig_OtherFieldsPreserved(t *testing.T) {
	existingCfg := config.DefaultConfig()
	existingCfg.Model.APIKey = "sk-keep-me"
	existingCfg.Model.ProviderName = "deepseek"
	existingCfg.Model.Model = "deepseek-v4-flash"
	existingCfg.Model.StreamMode = true
	existingCfg.Runtime.MaxStateMachineCycles = 42
	existingCfg.ContextCompress.MaxAttempts = 5

	sc := toServerConfig(existingCfg)
	result := fromServerConfig(sc, existingCfg)

	assert.Equal(t, "sk-keep-me", result.Model.APIKey)
	assert.Equal(t, "deepseek", result.Model.ProviderName)
	assert.Equal(t, "deepseek-v4-flash", result.Model.Model)
	assert.Equal(t, true, result.Model.StreamMode)
	assert.Equal(t, 42, result.Runtime.MaxStateMachineCycles)
	assert.Equal(t, 5, result.ContextCompress.MaxAttempts)
}
