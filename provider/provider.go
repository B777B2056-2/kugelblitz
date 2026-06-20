package provider

import (
	"context"

	"kugelblitz/core"
)

// Provider combines a provider configuration with an API format to implement
// the core.ILMProvider interface. It is the main entry point for applications.
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
func (p *Provider) Generate(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
	return p.format.Generate(ctx, params)
}

// Compile-time check: Provider implements core.ILMProvider.
var _ core.ILMProvider = (*Provider)(nil)
