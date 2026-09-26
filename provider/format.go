package provider

import (
	"context"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
)

// APIFormat abstracts a specific API wire protocol (Chat Completions, Responses, etc.).
// Implementations handle the serialization, HTTP transport, and response parsing
// for a given API type, independent of which provider is being called.
type APIFormat interface {
	Generate(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error)
}
