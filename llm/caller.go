// Package llm provides a single-shot LLM caller that unifies the repeated
// "build prompt → provider.Generate → type-assert → decode → record span"
// boilerplate spread across memory, infra and tools. A Caller wraps any
// coretypes.ILMProvider and decodes the response into one of a small set of
// output shapes selected by Mode.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Mode selects how a response is decoded after a Generate call.
type Mode int

const (
	// ModeText returns the raw response text (empty if the response has no text).
	ModeText Mode = iota
	// ModeBool maps a YES/NO text response to a bool (case-insensitive "YES").
	ModeBool
	// ModeJSON extracts the first balanced JSON object or array from the text
	// and unmarshals it into Request.Target (a non-nil pointer).
	ModeJSON
	// ModeToolCall forces a structured tool call via Request.Tool and returns
	// the arguments of the first matching call.
	ModeToolCall
)

// Caller invokes a provider for a single-shot task and decodes the result.
type Caller struct {
	provider coretypes.ILMProvider
	tracer   trace.Tracer // nil = no span
}

// NewCaller creates a Caller backed by the given provider. A nil tracer
// disables span creation.
func NewCaller(provider coretypes.ILMProvider, tracer trace.Tracer) *Caller {
	return &Caller{provider: provider, tracer: tracer}
}

// SetProvider replaces the underlying provider, for runtime model switching
// (e.g. a multimodal model selected per input). The tracer is unchanged.
func (c *Caller) SetProvider(provider coretypes.ILMProvider) { c.provider = provider }

// ErrNoToolCall is returned by Call for ModeToolCall when the response contains
// no tool call matching the requested Tool. Callers can distinguish this
// "model didn't cooperate" case from transport errors via errors.Is.
var ErrNoToolCall = errors.New("llm: no matching tool call received")

// Request describes a single-shot call.
type Request struct {
	// Prompt is wrapped in a user message when Messages is nil.
	Prompt string
	// Messages holds prebuilt input (e.g. multimodal). Takes precedence over Prompt.
	Messages []coretypes.Message
	Mode     Mode
	// SpanName starts a span with this name when non-empty and a tracer is set.
	SpanName  string
	SpanAttrs []attribute.KeyValue
	// Target is the destination pointer for ModeJSON.
	Target any
	// Tool is the tool definition for ModeToolCall.
	Tool *coretypes.ToolDefinition
	// EventHandler, if set, is passed through to the provider (block-mode hooks).
	EventHandler coretypes.ModelEventHandler
}

// Result holds the decoded output of a call. It is always non-nil; on a
// transport error Usage is still populated when the provider returned a
// partial response.
type Result struct {
	Text  string         // ModeText (raw)
	Bool  bool           // ModeBool
	Args  map[string]any // ModeToolCall
	Usage *coretypes.Usage
}

// Call runs the request and decodes the response. The returned error is a
// transport error (Generate failed) or a decode error; the Result is always
// non-nil so callers can still read Usage on error.
func (c *Caller) Call(ctx context.Context, req Request) (*Result, error) {
	messages := req.Messages
	if messages == nil {
		messages = []coretypes.Message{coretypes.NewUserMessage(coretypes.TextContent{Text: req.Prompt})}
	}

	params := coretypes.GenerateParams{
		Messages:     messages,
		Stream:       false,
		EventHandler: req.EventHandler,
	}
	if req.Mode == ModeToolCall && req.Tool != nil {
		params.Tools = []coretypes.ToolDefinition{*req.Tool}
	}

	var span trace.Span
	if c.tracer != nil && req.SpanName != "" {
		ctx, span = c.tracer.Start(ctx, req.SpanName, trace.WithAttributes(req.SpanAttrs...))
		defer span.End()
	}

	resp, err := c.provider.Generate(ctx, params)
	if err != nil {
		if span != nil {
			span.RecordError(err)
		}
		res := &Result{}
		if resp != nil {
			res.Usage = resp.Usage
		}
		return res, err
	}

	if resp.Usage != nil && span != nil {
		span.SetAttributes(
			attribute.Int64("tokens_in", resp.Usage.InputTokens),
			attribute.Int64("tokens_out", resp.Usage.OutputTokens),
			attribute.Int64("tokens_total", resp.Usage.TotalTokens),
		)
	}

	res := &Result{Usage: resp.Usage}
	switch req.Mode {
	case ModeText:
		res.Text = extractText(resp.Content)
	case ModeBool:
		res.Bool = isYes(extractText(resp.Content))
	case ModeJSON:
		if req.Target == nil {
			return res, fmt.Errorf("llm: ModeJSON requires a non-nil Target")
		}
		raw, err := extractJSON(extractText(resp.Content))
		if err != nil {
			return res, err
		}
		if err := json.Unmarshal(raw, req.Target); err != nil {
			return res, fmt.Errorf("llm: json decode: %w", err)
		}
	case ModeToolCall:
		name := ""
		if req.Tool != nil {
			name = req.Tool.Name
		}
		args, err := toolArgs(resp.Content, name)
		if err != nil {
			return res, err
		}
		res.Args = args
	}
	return res, nil
}

// extractText returns the concatenated text parts of a response content.
func extractText(content coretypes.Content) string {
	switch c := content.(type) {
	case coretypes.TextContent:
		return c.Text
	case coretypes.CompositeContent:
		var sb strings.Builder
		for _, p := range c.Parts {
			if tc, ok := p.(coretypes.TextContent); ok {
				sb.WriteString(tc.Text)
			}
		}
		return sb.String()
	}
	return ""
}

// isYes reports whether a response text is an affirmative YES answer.
func isYes(text string) bool {
	return strings.Contains(strings.ToUpper(strings.TrimSpace(text)), "YES")
}

// extractJSON finds the first balanced JSON value (object or array) in text and
// returns its raw bytes. It picks whichever delimiter — '{' or '[' — appears
// first, so it correctly selects an outer array ([{...}]) over its nested object.
func extractJSON(text string) ([]byte, error) {
	for i := 0; i < len(text); i++ {
		var open, close byte
		switch text[i] {
		case '{':
			open, close = '{', '}'
		case '[':
			open, close = '[', ']'
		default:
			continue
		}
		if end := matchDelim(text, i, open, close); end >= 0 {
			return []byte(text[i : end+1]), nil
		}
	}
	return nil, fmt.Errorf("llm: no JSON object or array found in response")
}

// matchDelim scans forward from start to find the matching close delimiter,
// accounting for nesting and string literals. Returns -1 when unbalanced.
func matchDelim(text string, start int, open, close byte) int {
	depth := 0
	inStr := false
	for i := start; i < len(text); i++ {
		c := text[i]
		if inStr {
			switch c {
			case '\\':
				i++
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// toolArgs returns the arguments of the first tool call matching name.
func toolArgs(content coretypes.Content, name string) (map[string]any, error) {
	for _, d := range toolCalls(content) {
		if d.ToolName == name {
			return d.Args, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrNoToolCall, name)
}

// toolCalls returns all tool-call details embedded in a response content.
func toolCalls(content coretypes.Content) []coretypes.ToolCallDetail {
	switch c := content.(type) {
	case coretypes.ToolCallContent:
		return c.Details
	case coretypes.CompositeContent:
		var details []coretypes.ToolCallDetail
		for _, p := range c.Parts {
			if tc, ok := p.(coretypes.ToolCallContent); ok {
				details = append(details, tc.Details...)
			}
		}
		return details
	}
	return nil
}
