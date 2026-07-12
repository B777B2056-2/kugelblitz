package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/B777B2056-2/kugelblitz/cmd/common"
	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/core"
)

// ServerConfig is the wire format for GET/PUT /api/settings/config.
// It mirrors config.Config with no Provider instance.
type ServerConfig struct {
	ProviderName               string                            `json:"provider_name"`
	Model                      string                            `json:"model"`
	BaseURL                    string                            `json:"base_url"`
	APIKey                     string                            `json:"api_key"`
	StreamMode                 bool                              `json:"stream_mode"`
	EnableThinking             bool                              `json:"enable_thinking"`
	ReasoningEffort            string                            `json:"reasoning_effort"`
	MaxStateMachineCycles      int                               `json:"max_state_machine_cycles"`
	CompressMaxAttempts        int                               `json:"compress_max_attempts"`
	CompressMaxToolResultChars int                               `json:"compress_max_tool_result_chars"`
	CompressKeepLastN          int                               `json:"compress_keep_last_n"`
	CompressMinMessages        int                               `json:"compress_min_messages"`
	ReviewInterval             int                               `json:"review_interval"`
	MaxFailuresBeforeReview    int                               `json:"max_failures_before_review"`
	MCPServers                 map[string]config.MCPServerConfig `json:"mcp_servers"`

	// Multimodal
	ImageProviderName string `json:"image_provider_name,omitempty"`
	ImageModel        string `json:"image_model,omitempty"`
	ImageBaseURL      string `json:"image_base_url,omitempty"`
	ImageAPIKey       string `json:"image_api_key,omitempty"`
	AudioProviderName string `json:"audio_provider_name,omitempty"`
	AudioModel        string `json:"audio_model,omitempty"`
	AudioBaseURL      string `json:"audio_base_url,omitempty"`
	AudioAPIKey       string `json:"audio_api_key,omitempty"`
	AutoDescribeMedia bool   `json:"auto_describe_media"`

	// Observability (OTel)
	OtelEnabled     bool   `json:"otel_enabled"`
	OtelEndpoint    string `json:"otel_endpoint,omitempty"`
	OtelAuthHeader  string `json:"otel_auth_header,omitempty"`
	OtelServiceName string `json:"otel_service_name,omitempty"`
}

var (
	currentConfig config.Config
	configMu      sync.RWMutex
	configLoaded  bool
)

// configFilePath returns the path to kugelblitz.yaml in the workspace.
func configFilePath() string {
	return filepath.Join(core.GetWorkspace().Dir(), "kugelblitz.yaml")
}

// LoadConfig reads kugelblitz.yaml via common.Load.
func LoadConfig() config.Config {
	configMu.Lock()
	defer configMu.Unlock()

	if configLoaded {
		return currentConfig
	}
	configLoaded = true

	cfg, err := common.Load(configFilePath())
	if err != nil {
		cfg = config.DefaultConfig()
	}
	currentConfig = cfg
	return cfg
}

// GetConfig returns the current configuration (thread-safe).
func GetConfig() config.Config {
	configMu.RLock()
	defer configMu.RUnlock()
	return currentConfig
}

// SaveConfig persists cfg to kugelblitz.yaml.
func SaveConfig(cfg config.Config) error {
	configMu.Lock()
	defer configMu.Unlock()
	if err := common.Save(configFilePath(), cfg); err != nil {
		return err
	}
	currentConfig = cfg
	return nil
}

// reloadConfigFromFile re-reads kugelblitz.yaml into memory.
func reloadConfigFromFile() {
	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := common.Load(configFilePath())
	if err != nil {
		return
	}
	currentConfig = cfg
}

// toServerConfig converts config.Config to the JSON wire format.
func toServerConfig(cfg config.Config) ServerConfig {
	sc := ServerConfig{
		ProviderName:               cfg.Model.ProviderName,
		Model:                      cfg.Model.Model,
		BaseURL:                    cfg.Model.BaseURL,
		APIKey:                     cfg.Model.APIKey,
		StreamMode:                 cfg.Model.StreamMode,
		EnableThinking:             cfg.Model.EnableThinking,
		ReasoningEffort:            cfg.Model.ReasoningEffort,
		MaxStateMachineCycles:      cfg.Runtime.MaxStateMachineCycles,
		CompressMaxAttempts:        cfg.ContextCompress.MaxAttempts,
		CompressMaxToolResultChars: cfg.ContextCompress.MaxToolResultChars,
		CompressKeepLastN:          cfg.ContextCompress.KeepLastN,
		CompressMinMessages:        cfg.ContextCompress.MinMessagesToCompress,
		ReviewInterval:             cfg.TargetDrift.ReviewInterval,
		MaxFailuresBeforeReview:    cfg.TargetDrift.MaxFailuresBeforeReview,
		MCPServers:                 cfg.MCP,
	}

	// Multimodal
	if cfg.Multimodal.ImageModel != nil {
		sc.ImageProviderName = cfg.Multimodal.ImageModel.ProviderName
		sc.ImageModel = cfg.Multimodal.ImageModel.Model
		sc.ImageBaseURL = cfg.Multimodal.ImageModel.BaseURL
		sc.ImageAPIKey = cfg.Multimodal.ImageModel.APIKey
	}
	if cfg.Multimodal.AudioModel != nil {
		sc.AudioProviderName = cfg.Multimodal.AudioModel.ProviderName
		sc.AudioModel = cfg.Multimodal.AudioModel.Model
		sc.AudioBaseURL = cfg.Multimodal.AudioModel.BaseURL
		sc.AudioAPIKey = cfg.Multimodal.AudioModel.APIKey
	}
	sc.AutoDescribeMedia = cfg.Multimodal.AutoDescribeMedia

	// Observability
	sc.OtelEnabled = cfg.Observability.Enabled
	sc.OtelEndpoint = cfg.Observability.Endpoint
	sc.OtelAuthHeader = cfg.Observability.AuthHeader
	sc.OtelServiceName = cfg.Observability.ServiceName

	return sc
}

// fromServerConfig converts the JSON wire format back to config.Config.
func fromServerConfig(sc ServerConfig, existingCfg config.Config) config.Config {
	cfg := config.DefaultConfig()

	cfg.Model.ProviderName = sc.ProviderName
	cfg.Model.Model = sc.Model
	cfg.Model.BaseURL = sc.BaseURL
	cfg.Model.StreamMode = sc.StreamMode
	cfg.Model.EnableThinking = sc.EnableThinking
	cfg.Model.ReasoningEffort = sc.ReasoningEffort

	cfg.Model.APIKey = sc.APIKey

	cfg.Runtime.MaxStateMachineCycles = sc.MaxStateMachineCycles
	cfg.ContextCompress.MaxAttempts = sc.CompressMaxAttempts
	cfg.ContextCompress.MaxToolResultChars = sc.CompressMaxToolResultChars
	cfg.ContextCompress.KeepLastN = sc.CompressKeepLastN
	cfg.ContextCompress.MinMessagesToCompress = sc.CompressMinMessages
	cfg.TargetDrift.ReviewInterval = sc.ReviewInterval
	cfg.TargetDrift.MaxFailuresBeforeReview = sc.MaxFailuresBeforeReview
	cfg.MCP = sc.MCPServers

	// Observability
	cfg.Observability.Enabled = sc.OtelEnabled
	cfg.Observability.Endpoint = sc.OtelEndpoint
	cfg.Observability.AuthHeader = sc.OtelAuthHeader
	cfg.Observability.ServiceName = sc.OtelServiceName

	// Multimodal — preserve existing config unless explicitly overridden.
	cfg.Multimodal.AutoDescribeMedia = sc.AutoDescribeMedia
	cfg.Multimodal.ImageModel = existingCfg.Multimodal.ImageModel
	cfg.Multimodal.AudioModel = existingCfg.Multimodal.AudioModel

	if sc.ImageProviderName != "" || sc.ImageModel != "" {
		im := &config.ModelConfig{
			ProviderName: sc.ImageProviderName,
			Model:        sc.ImageModel,
			BaseURL:      sc.ImageBaseURL,
			APIKey:       sc.ImageAPIKey,
		}
		if ip, err := config.NewProvider(im.ProviderName, im.APIKey, im.BaseURL, im.Model); err == nil {
			im.Provider = ip
		}
		cfg.Multimodal.ImageModel = im
	}

	if sc.AudioProviderName != "" || sc.AudioModel != "" {
		am := &config.ModelConfig{
			ProviderName: sc.AudioProviderName,
			Model:        sc.AudioModel,
			BaseURL:      sc.AudioBaseURL,
			APIKey:       sc.AudioAPIKey,
		}
		if ap, err := config.NewProvider(am.ProviderName, am.APIKey, am.BaseURL, am.Model); err == nil {
			am.Provider = ap
		}
		cfg.Multimodal.AudioModel = am
	}

	// Re-create provider
	p, _ := config.NewProvider(cfg.Model.ProviderName, cfg.Model.APIKey, cfg.Model.BaseURL, cfg.Model.Model)
	cfg.Model.Provider = p
	return cfg
}

// handleGetConfig GET /api/settings/config
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	reloadConfigFromFile()
	cfg := toServerConfig(GetConfig())
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, cfg)
}

// handlePutConfig PUT /api/settings/config
func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var sc ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&sc); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	cfg := fromServerConfig(sc, GetConfig())
	if err := SaveConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
