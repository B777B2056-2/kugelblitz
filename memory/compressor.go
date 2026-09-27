package memory

import (
	"context"
	"fmt"
	"strings"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/llm"
	"github.com/B777B2056-2/kugelblitz/prompts"
	"go.opentelemetry.io/otel/attribute"
)

// Summarizer summarizes a message history into a compact summary. SessionMemory
// depends on this interface (not the concrete *Compressor) so it can be swapped
// for a stub in tests and so higher layers don't carry the concrete type.
type Summarizer interface {
	Summarize(ctx context.Context, messages []coretypes.Message, existingSummary string) (string, *coretypes.Usage, error)
}

// FieldSummarizer summarizes a single oversized tool-result field. SessionMemory
// depends on this interface (not the concrete *Compressor) for symmetry with
// Summarizer, so CompressToolResult and Compress both consume interfaces.
type FieldSummarizer interface {
	SummarizeToolResultField(ctx context.Context, toolName, fieldKey, raw string) (string, error)
}

// Compressor handles conversation summarization via a unified LLM caller.
// It satisfies Summarizer.
type Compressor struct {
	caller *llm.Caller
}

// NewCompressor creates a Compressor backed by the given LLM caller.
func NewCompressor(caller *llm.Caller) *Compressor {
	return &Compressor{caller: caller}
}

// Summarize sends messages to the LLM and returns a summary string and token usage.
func (c *Compressor) Summarize(ctx context.Context, messages []coretypes.Message, existingSummary string) (string, *coretypes.Usage, error) {
	prompt, err := prompts.DefaultFactory.Render(prompts.TypeSummarize, prompts.SummarizeParams{
		ExistingSummary: existingSummary,
		Messages:        prompts.FormatMessages(messages),
	})
	if err != nil {
		return "", nil, fmt.Errorf("compressor: render: %w", err)
	}

	res, err := c.caller.Call(ctx, llm.Request{
		Prompt:    prompt,
		Mode:      llm.ModeText,
		SpanName:  "compress.summarize",
		SpanAttrs: []attribute.KeyValue{attribute.Int("messages", len(messages))},
	})
	if err != nil {
		return "", res.Usage, fmt.Errorf("compressor: generate: %w", err)
	}
	return res.Text, res.Usage, nil
}

// SummarizeToolResultField summarizes a single oversized tool result field via the LLM.
func (c *Compressor) SummarizeToolResultField(ctx context.Context, toolName, fieldKey, raw string) (string, error) {
	prompt, err := prompts.DefaultFactory.Render(prompts.TypeCompressTool, prompts.CompressToolParams{
		ToolName: toolName, FieldKey: fieldKey, OrigLen: len(raw), Raw: raw,
	})
	if err != nil {
		return "", fmt.Errorf("compressor: render: %w", err)
	}

	res, err := c.caller.Call(ctx, llm.Request{
		Prompt:   prompt,
		Mode:     llm.ModeText,
		SpanName: "compress.field",
		SpanAttrs: []attribute.KeyValue{
			attribute.String("tool", toolName),
			attribute.String("field", fieldKey),
			attribute.Int("raw_len", len(raw)),
		},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Text), nil
}

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
