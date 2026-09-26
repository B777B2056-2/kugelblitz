// Package config provides the unified configuration for AgentLoop and Kernel.
// It defines configuration structs and defaults. File I/O for kugelblitz.yaml
// is handled by cmd/common — this package has no filesystem or YAML dependency.
package config

import (
	"fmt"

	"github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/provider"
)

// ModelConfig groups LLM provider and thinking configuration.
type ModelConfig struct {
	Provider types.ILMProvider // concrete provider instance (set at runtime)

	ProviderName    string `json:"provider_name"`
	Model           string `json:"model"`
	BaseURL         string `json:"base_url"`
	APIKey          string `json:"api_key"`
	StreamMode      bool   `json:"stream_mode"`
	EnableThinking  bool   `json:"enable_thinking"`
	ReasoningEffort string `json:"reasoning_effort"`
}

// ContextCompressConfig groups session memory compression parameters.
type ContextCompressConfig struct {
	MaxAttempts           int `json:"compress_max_attempts"`
	MaxToolResultChars    int `json:"compress_max_tool_result_chars"`
	KeepLastN             int `json:"compress_keep_last_n"`
	MinMessagesToCompress int `json:"compress_min_messages"`
}

// TargetDriftConfig groups reviewer / drift detection parameters.
type TargetDriftConfig struct {
	ReviewInterval          int `json:"review_interval"`
	MaxFailuresBeforeReview int `json:"max_failures_before_review"`
}

// RuntimeConfig groups state machine runtime parameters.
type RuntimeConfig struct {
	MaxStateMachineCycles int `json:"max_state_machine_cycles"`

	// ForceMode skips the Intent (set_work_mode) classification phase and starts
	// the state machine directly in the given mode.
	//   "" or "auto" — default: the LLM classifies the request via set_work_mode.
	//   "plan"       — always enter plan mode (PlanStateInit).
	//   "simple"     — always enter direct execution (PlanStateDirect).
	ForceMode string `json:"force_mode"`
}

// MCPServerConfig defines how to connect to a single MCP server.
// yaml tags are retained because cmd/common serializes this type
// via yaml.Marshal when writing mcp_servers to kugelblitz.yaml.
type MCPServerConfig struct {
	Command string            `yaml:"command" json:"command"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// MultimodalConfig controls how the framework handles multimodal media (image, audio, video).
type MultimodalConfig struct {
	// ImageModel is the model used for image understanding (description generation).
	// When nil, falls back to the main model (Config.Model).
	ImageModel *ModelConfig `json:"image_model,omitempty"`

	// AudioModel is the model used for audio understanding (description generation).
	// When nil, falls back to the main model (Config.Model).
	AudioModel *ModelConfig `json:"audio_model,omitempty"`

	// AutoDescribeMedia controls whether an LLM is automatically called to generate
	// a text description when media enters SessionMemory.
	// false (default): only a metadata summary is produced (e.g. "[image: image/png 1920×1080]").
	// true: the model generates a natural-language description of the media content.
	AutoDescribeMedia bool `json:"auto_describe_media"`
}

// ObservabilityConfig groups OpenTelemetry tracing parameters.
// When Enabled is false (default), OTel is not initialized and all spans are no-op.
// The OTel SDK will also read standard environment variables
// (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_HEADERS) as fallback.
type ObservabilityConfig struct {
	Enabled     bool   `json:"otel_enabled"`      // enable OTel tracing
	Endpoint    string `json:"otel_endpoint"`     // OTLP/HTTP endpoint
	AuthHeader  string `json:"otel_auth_header"`  // "Basic base64(pk:sk)" for Langfuse
	ServiceName string `json:"otel_service_name"` // default "kugelblitz"
}

// AutoDreamConfig controls background memory consolidation (dreaming).
// Intervals are in seconds; zero means "use the default".
type AutoDreamConfig struct {
	Enabled          bool `json:"auto_dream_enabled"`            // default true via DefaultConfig
	CheckIntervalSec int  `json:"auto_dream_check_interval_sec"` // poll every N sec (default 1800 = 30m)
	CooldownSec      int  `json:"auto_dream_cooldown_sec"`       // min between dreams (default 21600 = 6h)
	IdleThresholdSec int  `json:"auto_dream_idle_threshold_sec"` // min idle before dream (default 300 = 5m)
}

// Config is the top-level configuration for AgentLoop + Kernel.
type Config struct {
	Model           ModelConfig                `json:"model"`
	Runtime         RuntimeConfig              `json:"runtime"`
	ContextCompress ContextCompressConfig      `json:"context_compress"`
	TargetDrift     TargetDriftConfig          `json:"target_drift"`
	Multimodal      MultimodalConfig           `json:"multimodal"`
	Observability   ObservabilityConfig        `json:"observability"`
	AutoDream       AutoDreamConfig            `json:"auto_dream"`
	MCP             map[string]MCPServerConfig `json:"mcp_servers"`
}

// NewProvider creates a types.ILMProvider from name + credentials.
// Supported provider names: "deepseek", "openai".
func NewProvider(name, apiKey, baseURL, model string) (types.ILMProvider, error) {
	switch name {
	case "deepseek":
		return provider.DeepSeek(apiKey, baseURL, model), nil
	case "openai":
		return provider.OpenAI(apiKey, baseURL, model), nil
	default:
		return nil, fmt.Errorf("unknown provider %q (supported: deepseek, openai)", name)
	}
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Model: ModelConfig{
			ProviderName:    "deepseek",
			Model:           "deepseek-v4-flash",
			BaseURL:         "https://api.deepseek.com",
			APIKey:          "",
			StreamMode:      true,
			EnableThinking:  true,
			ReasoningEffort: types.ReasoningEffortHigh,
		},
		Runtime:         RuntimeConfig{MaxStateMachineCycles: 30},
		ContextCompress: ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 4000, KeepLastN: 20, MinMessagesToCompress: 10},
		TargetDrift:     TargetDriftConfig{ReviewInterval: 12, MaxFailuresBeforeReview: 5},
		Observability:   ObservabilityConfig{Enabled: false, ServiceName: "kugelblitz"},
		AutoDream:       AutoDreamConfig{Enabled: true, CheckIntervalSec: 1800, CooldownSec: 21600, IdleThresholdSec: 300},
	}
}
