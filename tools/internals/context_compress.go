package internals

import (
	"context"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/core"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/tools"
)

// RegisterContextCompressTool registers the context_compress tool and stores the
// instance so BindContextCompress can wire its runtime function later.
func RegisterContextCompressTool() {
	t := &ContextCompress{}
	registeredContextCompress = t
	tools.Register(t)
	core.GetToolRegistry().MarkAsInternal(t.Definition().Name)
}

// registeredContextCompress holds the ContextCompress instance created by
// RegisterContextCompressTool. BindContextCompress sets its function at runtime.
var registeredContextCompress *ContextCompress

// BindContextCompress binds the compression function to the registered
// ContextCompress tool. The function is invoked with the tool call's own context.
func BindContextCompress(fn func(ctx context.Context) (*coretypes.Usage, error)) {
	if registeredContextCompress != nil {
		registeredContextCompress.compressFn = fn
	}
}

// ContextCompress lets the agent manually compress the current conversation
// history into a summary, freeing context-window space mid-turn.
type ContextCompress struct {
	compressFn func(ctx context.Context) (*coretypes.Usage, error)
}

func (t *ContextCompress) Definition() coretypes.ToolDefinition {
	return coretypes.ToolDefinition{
		Name: "context_compress",
		Description: "Compress the current conversation history into a compact summary " +
			"to free context-window space. Recent messages are preserved. Call this when " +
			"the conversation is running long.",
		JSONSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"compressed":    map[string]any{"type": "boolean", "description": "true if the history was compressed."},
				"total_tokens":  map[string]any{"type": "integer", "description": "Total tokens used by the summarization call (if any)."},
				"input_tokens":  map[string]any{"type": "integer", "description": "Input tokens used by the summarization call (if any)."},
				"output_tokens": map[string]any{"type": "integer", "description": "Output tokens used by the summarization call (if any)."},
			},
		},
	}
}

func (t *ContextCompress) Execute(ctx context.Context, detail coretypes.ToolCallDetail) coretypes.ToolCallResult {
	if t.compressFn == nil {
		return tools.ErrorResult(detail.ID, "context_compress", fmt.Errorf("context compression not configured"))
	}
	usage, err := t.compressFn(ctx)
	if err != nil {
		return tools.ErrorResult(detail.ID, "context_compress", err)
	}
	outputs := map[string]any{"compressed": true}
	if usage != nil {
		outputs["total_tokens"] = usage.TotalTokens
		outputs["input_tokens"] = usage.InputTokens
		outputs["output_tokens"] = usage.OutputTokens
	}
	return tools.SuccessResult(detail.ID, "context_compress", outputs)
}
