package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/prompts"
)

// Compressor handles conversation summarization via an LLM provider.
// It extracts key facts, decisions, and context from old messages
// to produce a compact summary that replaces them.
type Compressor struct {
	provider core.ILMProvider
}

// NewCompressor creates a Compressor backed by the given LLM provider.
func NewCompressor(provider core.ILMProvider) *Compressor {
	return &Compressor{provider: provider}
}

// Summarize sends messages to the LLM and returns a summary string and token usage.
// existingSummary is a prior summary to carry forward (may be empty).
func (c *Compressor) Summarize(ctx context.Context, messages []core.Message, existingSummary string) (string, *core.Usage, error) {
	prompt := prompts.BuildSummarizePrompt(messages, existingSummary)

	userMsg := core.NewUserMessage(core.TextContent{Text: prompt})
	params := core.GenerateParams{
		Messages: []core.Message{userMsg},
		Stream:   false,
	}

	result, err := c.provider.Generate(ctx, params)
	if err != nil {
		return "", nil, fmt.Errorf("compressor: generate: %w", err)
	}

	if tc, ok := result.Content.(core.TextContent); ok {
		return tc.Text, result.Usage, nil
	}
	return "", result.Usage, fmt.Errorf("compressor: unexpected response type: %T", result.Content)
}

// SummarizeToolResultField summarizes a single oversized tool result field via the LLM.
func (c *Compressor) SummarizeToolResultField(ctx context.Context, toolName, fieldKey, raw string) (string, error) {
	prompt := prompts.BuildCompressFieldPrompt(toolName, fieldKey, len(raw), raw)

	msg := core.NewUserMessage(core.TextContent{Text: prompt})
	params := core.GenerateParams{
		Messages: []core.Message{msg},
		Stream:   false,
	}
	result, err := c.provider.Generate(ctx, params)
	if err != nil {
		return "", err
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
