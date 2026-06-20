package provider

import (
	"context"

	"kugelblitz/core"
)

// APIFormat abstracts a specific API wire protocol (Chat Completions, Responses, etc.).
// Implementations handle the serialization, HTTP transport, and response parsing
// for a given API type, independent of which provider is being called.
type APIFormat interface {
	Generate(ctx context.Context, params core.GenerateParams) (*core.Message, error)
}
