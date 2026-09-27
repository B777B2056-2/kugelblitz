package internals

import (
	"context"
	"errors"
	"testing"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextCompress_Execute_ReturnsUsage(t *testing.T) {
	tool := &ContextCompress{
		compressFn: func(ctx context.Context) (*coretypes.Usage, error) {
			return &coretypes.Usage{TotalTokens: 15, InputTokens: 9, OutputTokens: 6}, nil
		},
	}

	result := tool.Execute(context.Background(), coretypes.ToolCallDetail{
		ID: "c1", ToolName: "context_compress", Args: map[string]any{},
	})

	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, true, result.Outputs["compressed"])
	assert.Equal(t, int64(15), result.Outputs["total_tokens"])
	assert.Equal(t, int64(9), result.Outputs["input_tokens"])
	assert.Equal(t, int64(6), result.Outputs["output_tokens"])
}

func TestContextCompress_Execute_NoBinding(t *testing.T) {
	tool := &ContextCompress{}

	result := tool.Execute(context.Background(), coretypes.ToolCallDetail{
		ID: "c1", ToolName: "context_compress", Args: map[string]any{},
	})

	assert.NotNil(t, result.Outputs["error"])
}

func TestContextCompress_Execute_PropagatesError(t *testing.T) {
	tool := &ContextCompress{
		compressFn: func(ctx context.Context) (*coretypes.Usage, error) {
			return nil, errors.New("boom")
		},
	}

	result := tool.Execute(context.Background(), coretypes.ToolCallDetail{
		ID: "c1", ToolName: "context_compress", Args: map[string]any{},
	})

	assert.Equal(t, "boom", result.Outputs["error"])
}

func TestBindContextCompress_SetsOnRegisteredInstance(t *testing.T) {
	// Set the package-level instance directly rather than calling
	// RegisterContextCompressTool, which mutates the global ToolRegistry and
	// would pollute the registry-count assertions in other tests.
	registeredContextCompress = &ContextCompress{}
	defer func() { registeredContextCompress = nil }()

	called := false
	BindContextCompress(func(ctx context.Context) (*coretypes.Usage, error) {
		called = true
		return nil, nil
	})

	require.NotNil(t, registeredContextCompress.compressFn, "BindContextCompress should set the bound function")
	_ = registeredContextCompress.Execute(context.Background(), coretypes.ToolCallDetail{
		ID: "c1", ToolName: "context_compress", Args: map[string]any{},
	})
	assert.True(t, called, "bound function should be invoked on Execute")
}
