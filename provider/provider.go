package provider

import (
	"context"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
)

// Provider combines a provider configuration with an API format to implement
// the coretypes.ILMProvider interface. It is the main entry point for applications.
//
// Use the preset functions (OpenAI, DeepSeek) or construct manually:
//
//	p := provider.New(provider.Config{...}, chat_completions.NewFormat(...))
type Provider struct {
	Config
	format APIFormat
}

// New creates a Provider from configuration and an API format.
func New(cfg Config, format APIFormat) *Provider {
	return &Provider{
		Config: cfg,
		format: format,
	}
}

// Generate delegates to the underlying API format.
// Provider-specific extensions (e.g., auth headers) should be applied
// via the format's request builder before this call.
func (p *Provider) Generate(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
	return p.format.Generate(ctx, params)
}

// Compile-time check: Provider implements coretypes.ILMProvider.
var _ coretypes.ILMProvider = (*Provider)(nil)
