// Package common provides shared utilities for cmd/ programs.
package common

import (
	"os"

	"github.com/B777B2056-2/kugelblitz/config"
	"gopkg.in/yaml.v3"
)

// Load reads a kugelblitz.yaml file from path and returns a fully-populated Config
// with a concrete Provider. Missing keys retain their defaults — critical for
// bool fields that default to true. A missing or unreadable file returns the
// default config unchanged.
func Load(path string) (config.Config, error) {
	cfg := config.DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err // no file → use defaults
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg, err
	}
	applyRaw(raw, &cfg)

	p, err := config.NewProvider(cfg.Model.ProviderName, cfg.Model.APIKey, cfg.Model.BaseURL, cfg.Model.Model)
	if err != nil {
		return cfg, err
	}
	cfg.Model.Provider = p

	// Resolve image model provider (if configured)
	if cfg.Multimodal.ImageModel != nil && cfg.Multimodal.ImageModel.ProviderName != "" {
		im := cfg.Multimodal.ImageModel
		ip, err := config.NewProvider(im.ProviderName, im.APIKey, im.BaseURL, im.Model)
		if err == nil {
			im.Provider = ip
		}
	}

	// Resolve audio model provider (if configured)
	if cfg.Multimodal.AudioModel != nil && cfg.Multimodal.AudioModel.ProviderName != "" {
		am := cfg.Multimodal.AudioModel
		ap, err := config.NewProvider(am.ProviderName, am.APIKey, am.BaseURL, am.Model)
		if err == nil {
			am.Provider = ap
		}
	}

	return cfg, nil
}

// Save writes cfg to path in flat-key YAML format (matching applyRaw), not the
// nested struct YAML that yaml.Marshal(&cfg) would produce.
func Save(path string, cfg config.Config) error {
	out := map[string]any{
		"provider_name":                  cfg.Model.ProviderName,
		"model":                          cfg.Model.Model,
		"base_url":                       cfg.Model.BaseURL,
		"api_key":                        cfg.Model.APIKey,
		"stream_mode":                    cfg.Model.StreamMode,
		"enable_thinking":                cfg.Model.EnableThinking,
		"reasoning_effort":               cfg.Model.ReasoningEffort,
		"max_state_machine_cycles":       cfg.Runtime.MaxStateMachineCycles,
		"compress_max_attempts":          cfg.ContextCompress.MaxAttempts,
		"compress_max_tool_result_chars": cfg.ContextCompress.MaxToolResultChars,
		"compress_keep_last_n":           cfg.ContextCompress.KeepLastN,
		"compress_min_messages":          cfg.ContextCompress.MinMessagesToCompress,
		"review_interval":                cfg.TargetDrift.ReviewInterval,
		"max_failures_before_review":     cfg.TargetDrift.MaxFailuresBeforeReview,
		"auto_describe_media":            cfg.Multimodal.AutoDescribeMedia,
		"otel_enabled":                    cfg.Observability.Enabled,
		"otel_endpoint":                   cfg.Observability.Endpoint,
		"otel_auth_header":                cfg.Observability.AuthHeader,
		"otel_service_name":               cfg.Observability.ServiceName,
	}
	if cfg.Multimodal.ImageModel != nil {
		out["image_provider_name"] = cfg.Multimodal.ImageModel.ProviderName
		out["image_model"] = cfg.Multimodal.ImageModel.Model
		out["image_base_url"] = cfg.Multimodal.ImageModel.BaseURL
		out["image_api_key"] = cfg.Multimodal.ImageModel.APIKey
	}
	if cfg.Multimodal.AudioModel != nil {
		out["audio_provider_name"] = cfg.Multimodal.AudioModel.ProviderName
		out["audio_model"] = cfg.Multimodal.AudioModel.Model
		out["audio_base_url"] = cfg.Multimodal.AudioModel.BaseURL
		out["audio_api_key"] = cfg.Multimodal.AudioModel.APIKey
	}
	if len(cfg.MCP) > 0 {
		out["mcp_servers"] = cfg.MCP
	}

	data, err := yaml.Marshal(out)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// applyRaw merges parsed YAML keys into cfg. Only keys present in the YAML
// are applied — missing keys retain their default values.
//
// Supports two YAML formats:
//   - Flat (preferred): top-level keys like provider_name, model, mcp_servers, ...
//   - Nested (legacy): keys grouped under model:, runtime:, context_compress:, etc.
//     This was produced by older versions that used yaml.Marshal(&cfg) directly.
func applyRaw(raw map[string]any, cfg *config.Config) {
	// Backward-compat: if "model" is a nested map, flatten its keys.
	if m, ok := raw["model"].(map[string]any); ok {
		for k, v := range m {
			raw[k] = v
		}
	}
	if m, ok := raw["runtime"].(map[string]any); ok {
		for k, v := range m {
			raw[k] = v
		}
	}
	if m, ok := raw["context_compress"].(map[string]any); ok {
		for k, v := range m {
			raw[k] = v
		}
	}
	if m, ok := raw["target_drift"].(map[string]any); ok {
		for k, v := range m {
			raw[k] = v
		}
	}

	// — Model —
	if v, ok := raw["provider_name"].(string); ok && v != "" {
		cfg.Model.ProviderName = v
	}
	if v, ok := raw["model"].(string); ok && v != "" {
		cfg.Model.Model = v
	}
	if v, ok := raw["base_url"].(string); ok && v != "" {
		cfg.Model.BaseURL = v
	}
	if v, ok := raw["api_key"].(string); ok && v != "" {
		cfg.Model.APIKey = v
	}
	if _, ok := raw["stream_mode"]; ok {
		cfg.Model.StreamMode = toBool(raw["stream_mode"])
	}
	if _, ok := raw["enable_thinking"]; ok {
		cfg.Model.EnableThinking = toBool(raw["enable_thinking"])
	}
	if v, ok := raw["reasoning_effort"].(string); ok && v != "" {
		cfg.Model.ReasoningEffort = v
	}

	// — Runtime —
	if v, ok := raw["max_state_machine_cycles"]; ok {
		if n := toInt(v); n != 0 {
			cfg.Runtime.MaxStateMachineCycles = n
		}
	}

	// — Context Compress —
	if v, ok := raw["compress_max_attempts"]; ok {
		if n := toInt(v); n != 0 {
			cfg.ContextCompress.MaxAttempts = n
		}
	}
	if v, ok := raw["compress_max_tool_result_chars"]; ok {
		if n := toInt(v); n != 0 {
			cfg.ContextCompress.MaxToolResultChars = n
		}
	}
	if v, ok := raw["compress_keep_last_n"]; ok {
		if n := toInt(v); n != 0 {
			cfg.ContextCompress.KeepLastN = n
		}
	}
	if v, ok := raw["compress_min_messages"]; ok {
		if n := toInt(v); n != 0 {
			cfg.ContextCompress.MinMessagesToCompress = n
		}
	}

	// — Target Drift —
	if v, ok := raw["review_interval"]; ok {
		if n := toInt(v); n != 0 {
			cfg.TargetDrift.ReviewInterval = n
		}
	}
	if v, ok := raw["max_failures_before_review"]; ok {
		if n := toInt(v); n != 0 {
			cfg.TargetDrift.MaxFailuresBeforeReview = n
		}
	}

	// — Multimodal —
	if _, ok := raw["auto_describe_media"]; ok {
		cfg.Multimodal.AutoDescribeMedia = toBool(raw["auto_describe_media"])
	}

	// — Observability (OTel) —
	if _, ok := raw["otel_enabled"]; ok {
		cfg.Observability.Enabled = toBool(raw["otel_enabled"])
	}
	if v, ok := raw["otel_endpoint"].(string); ok && v != "" {
		cfg.Observability.Endpoint = v
	}
	if v, ok := raw["otel_auth_header"].(string); ok && v != "" {
		cfg.Observability.AuthHeader = v
	}
	if v, ok := raw["otel_service_name"].(string); ok && v != "" {
		cfg.Observability.ServiceName = v
	}

	// Image model (optional — only populated when explicitly configured)
	if hasAnyKey(raw, "image_provider_name", "image_model", "image_base_url", "image_api_key") {
		im := &config.ModelConfig{}
		if v, ok := raw["image_provider_name"].(string); ok && v != "" {
			im.ProviderName = v
		}
		if v, ok := raw["image_model"].(string); ok && v != "" {
			im.Model = v
		}
		if v, ok := raw["image_base_url"].(string); ok && v != "" {
			im.BaseURL = v
		}
		if v, ok := raw["image_api_key"].(string); ok && v != "" {
			im.APIKey = v
		}
		cfg.Multimodal.ImageModel = im
	}

	// Audio model (optional — only populated when explicitly configured)
	if hasAnyKey(raw, "audio_provider_name", "audio_model", "audio_base_url", "audio_api_key") {
		am := &config.ModelConfig{}
		if v, ok := raw["audio_provider_name"].(string); ok && v != "" {
			am.ProviderName = v
		}
		if v, ok := raw["audio_model"].(string); ok && v != "" {
			am.Model = v
		}
		if v, ok := raw["audio_base_url"].(string); ok && v != "" {
			am.BaseURL = v
		}
		if v, ok := raw["audio_api_key"].(string); ok && v != "" {
			am.APIKey = v
		}
		cfg.Multimodal.AudioModel = am
	}

	// — MCP —
	if v, ok := raw["mcp_servers"].(map[string]any); ok {
		cfg.MCP = parseMCPServers(v)
	}
}

func hasAnyKey(raw map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := raw[k]; ok {
			return true
		}
	}
	return false
}

func parseMCPServers(raw map[string]any) map[string]config.MCPServerConfig {
	result := make(map[string]config.MCPServerConfig, len(raw))
	for name, srv := range raw {
		sm, ok := srv.(map[string]any)
		if !ok {
			continue
		}
		sc := config.MCPServerConfig{}
		if cmd, ok := sm["command"].(string); ok {
			sc.Command = cmd
		}
		if args, ok := sm["args"].([]any); ok {
			for _, a := range args {
				if s, ok := a.(string); ok {
					sc.Args = append(sc.Args, s)
				}
			}
		}
		if env, ok := sm["env"].(map[string]any); ok {
			sc.Env = make(map[string]string)
			for k, v := range env {
				if vs, ok := v.(string); ok {
					sc.Env[k] = vs
				}
			}
		}
		result[name] = sc
	}
	return result
}

func toBool(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true" || val == "yes" || val == "1"
	default:
		return false
	}
}

func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	default:
		return 0
	}
}
