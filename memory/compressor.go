package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/prompts"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Compressor handles conversation summarization via an LLM provider.
type Compressor struct {
	provider core.ILMProvider
	tracer   trace.Tracer
}

// NewCompressor creates a Compressor backed by the given LLM provider.
func NewCompressor(provider core.ILMProvider, tracer trace.Tracer) *Compressor {
	return &Compressor{provider: provider, tracer: tracer}
}

// Summarize sends messages to the LLM and returns a summary string and token usage.
func (c *Compressor) Summarize(ctx context.Context, messages []core.Message, existingSummary string) (string, *core.Usage, error) {
	ctx, span := c.tracer.Start(ctx, "compress.summarize",
		trace.WithAttributes(attribute.Int("messages", len(messages))),
	)
	defer span.End()

	prompt := prompts.BuildSummarizePrompt(messages, existingSummary)

	userMsg := core.NewUserMessage(core.TextContent{Text: prompt})
	params := core.GenerateParams{
		Messages: []core.Message{userMsg},
		Stream:   false,
	}

	result, err := c.provider.Generate(ctx, params)
	if err != nil {
		span.RecordError(err)
		return "", nil, fmt.Errorf("compressor: generate: %w", err)
	}

	if result.Usage != nil {
		span.SetAttributes(
			attribute.Int64("tokens_in", result.Usage.InputTokens),
			attribute.Int64("tokens_out", result.Usage.OutputTokens),
			attribute.Int64("tokens_total", result.Usage.TotalTokens),
		)
	}

	if tc, ok := result.Content.(core.TextContent); ok {
		span.SetAttributes(attribute.Int("summary_len", len(tc.Text)))
		return tc.Text, result.Usage, nil
	}
	return "", result.Usage, fmt.Errorf("compressor: unexpected response type: %T", result.Content)
}

// SummarizeToolResultField summarizes a single oversized tool result field via the LLM.
func (c *Compressor) SummarizeToolResultField(ctx context.Context, toolName, fieldKey, raw string) (string, error) {
	ctx, span := c.tracer.Start(ctx, "compress.field",
		trace.WithAttributes(
			attribute.String("tool", toolName),
			attribute.String("field", fieldKey),
			attribute.Int("raw_len", len(raw)),
		),
	)
	defer span.End()

	prompt := prompts.BuildCompressFieldPrompt(toolName, fieldKey, len(raw), raw)

	msg := core.NewUserMessage(core.TextContent{Text: prompt})
	params := core.GenerateParams{
		Messages: []core.Message{msg},
		Stream:   false,
	}
	result, err := c.provider.Generate(ctx, params)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	if result.Usage != nil {
		span.SetAttributes(
			attribute.Int64("tokens_in", result.Usage.InputTokens),
			attribute.Int64("tokens_out", result.Usage.OutputTokens),
		)
	}

	if tc, ok := result.Content.(core.TextContent); ok {
		return strings.TrimSpace(tc.Text), nil
	}
	return "", fmt.Errorf("compressor: unexpected response type: %T", result.Content)
}

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
