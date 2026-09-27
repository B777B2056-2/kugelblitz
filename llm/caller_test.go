package llm

import (
	"context"
	"errors"
	"testing"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

// mockProvider is a function-field fake satisfying coretypes.ILMProvider.
type mockProvider struct {
	generateFn func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error)
}

func (m *mockProvider) Generate(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
	return m.generateFn(ctx, params)
}

func textMessage(text string) *coretypes.Message {
	return &coretypes.Message{Content: coretypes.TextContent{Text: text}}
}

func newTestCaller(fn func(context.Context, coretypes.GenerateParams) (*coretypes.Message, error)) *Caller {
	return NewCaller(&mockProvider{generateFn: fn}, nil)
}

func TestModeText_ReturnsRawText(t *testing.T) {
	c := newTestCaller(func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
		return textMessage("hello world"), nil
	})

	res, err := c.Call(context.Background(), Request{Prompt: "hi", Mode: ModeText})
	require.NoError(t, err)
	assert.Equal(t, "hello world", res.Text)
}

func TestModeText_BuildsUserMessageFromPrompt(t *testing.T) {
	var captured coretypes.GenerateParams
	c := newTestCaller(func(_ context.Context, p coretypes.GenerateParams) (*coretypes.Message, error) {
		captured = p
		return textMessage("ok"), nil
	})

	_, err := c.Call(context.Background(), Request{Prompt: "summarize this", Mode: ModeText})
	require.NoError(t, err)
	require.Len(t, captured.Messages, 1)
	tc, ok := captured.Messages[0].Content.(coretypes.TextContent)
	require.True(t, ok, "prompt must be wrapped in a text user message")
	assert.Equal(t, "summarize this", tc.Text)
}

func TestModeText_PrefersMessagesOverPrompt(t *testing.T) {
	var captured coretypes.GenerateParams
	c := newTestCaller(func(_ context.Context, p coretypes.GenerateParams) (*coretypes.Message, error) {
		captured = p
		return textMessage("ok"), nil
	})

	prebuilt := []coretypes.Message{coretypes.NewUserMessage(coretypes.MultiModalContent{})}
	_, err := c.Call(context.Background(), Request{Prompt: "ignored", Messages: prebuilt, Mode: ModeText})
	require.NoError(t, err)
	assert.Equal(t, prebuilt, captured.Messages)
}

func TestModeBool_YesNo(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"YES", true},
		{"No.", false},
		{"The answer is yes", true},
		{"NO", false},
	}
	for _, tt := range tests {
		c := newTestCaller(func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
			return textMessage(tt.text), nil
		})
		res, err := c.Call(context.Background(), Request{Prompt: "q", Mode: ModeBool})
		require.NoError(t, err)
		assert.Equal(t, tt.want, res.Bool, "text %q", tt.text)
	}
}

func TestModeJSON_Object(t *testing.T) {
	c := newTestCaller(func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
		return textMessage("result: {\"a\": 1, \"b\": \"x\"} done"), nil
	})

	var out struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	_, err := c.Call(context.Background(), Request{Prompt: "q", Mode: ModeJSON, Target: &out})
	require.NoError(t, err)
	assert.Equal(t, 1, out.A)
	assert.Equal(t, "x", out.B)
}

func TestModeJSON_ObjectInMarkdownFence(t *testing.T) {
	c := newTestCaller(func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
		return textMessage("```json\n{\"items\":[{\"k\":\"v\"}]}\n```"), nil
	})

	var out struct {
		Items []map[string]string `json:"items"`
	}
	_, err := c.Call(context.Background(), Request{Prompt: "q", Mode: ModeJSON, Target: &out})
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	assert.Equal(t, "v", out.Items[0]["k"])
}

func TestModeJSON_Array(t *testing.T) {
	c := newTestCaller(func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
		return textMessage("candidates: [{\"section\":\"s\"}] end"), nil
	})

	var out []map[string]string
	_, err := c.Call(context.Background(), Request{Prompt: "q", Mode: ModeJSON, Target: &out})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "s", out[0]["section"])
}

func TestModeJSON_NoJSONReturnsError(t *testing.T) {
	c := newTestCaller(func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
		return textMessage("no json here"), nil
	})

	var out map[string]any
	_, err := c.Call(context.Background(), Request{Prompt: "q", Mode: ModeJSON, Target: &out})
	assert.Error(t, err)
}

func TestModeJSON_NilTargetReturnsError(t *testing.T) {
	c := newTestCaller(func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
		return textMessage("{\"a\":1}"), nil
	})

	_, err := c.Call(context.Background(), Request{Prompt: "q", Mode: ModeJSON})
	assert.Error(t, err)
}

func TestModeToolCall_ReturnsMatchingArgs(t *testing.T) {
	call := coretypes.ToolCallContent{Details: []coretypes.ToolCallDetail{{
		ID: "1", ToolName: "reviewer_report", Args: map[string]any{"drift": true, "reason": "off"},
	}}}
	c := newTestCaller(func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
		return &coretypes.Message{Content: call}, nil
	})

	res, err := c.Call(context.Background(), Request{
		Prompt: "q", Mode: ModeToolCall, Tool: &coretypes.ToolDefinition{Name: "reviewer_report"},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"drift": true, "reason": "off"}, res.Args)
}

func TestModeToolCall_NoMatchReturnsError(t *testing.T) {
	call := coretypes.ToolCallContent{Details: []coretypes.ToolCallDetail{{
		ID: "1", ToolName: "other", Args: map[string]any{},
	}}}
	c := newTestCaller(func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
		return &coretypes.Message{Content: call}, nil
	})

	_, err := c.Call(context.Background(), Request{
		Prompt: "q", Mode: ModeToolCall, Tool: &coretypes.ToolDefinition{Name: "reviewer_report"},
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoToolCall), "no-match must be distinguishable via ErrNoToolCall")
}

func TestSetProvider_SwapsProvider(t *testing.T) {
	first := newTestCaller(func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
		return textMessage("first"), nil
	})
	second := newTestCaller(func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
		return textMessage("second"), nil
	})

	c := NewCaller(nil, nil)
	c.SetProvider(first.provider)

	res, err := c.Call(context.Background(), Request{Prompt: "q", Mode: ModeText})
	require.NoError(t, err)
	assert.Equal(t, "first", res.Text)

	c.SetProvider(second.provider)
	res, err = c.Call(context.Background(), Request{Prompt: "q", Mode: ModeText})
	require.NoError(t, err)
	assert.Equal(t, "second", res.Text)
}

func TestModeToolCall_SendsToolDefinition(t *testing.T) {
	var captured coretypes.GenerateParams
	c := newTestCaller(func(_ context.Context, p coretypes.GenerateParams) (*coretypes.Message, error) {
		captured = p
		return &coretypes.Message{Content: coretypes.ToolCallContent{Details: []coretypes.ToolCallDetail{{
			ToolName: "t", Args: map[string]any{},
		}}}}, nil
	})

	_, err := c.Call(context.Background(), Request{
		Prompt: "q", Mode: ModeToolCall, Tool: &coretypes.ToolDefinition{Name: "t"},
	})
	require.NoError(t, err)
	require.Len(t, captured.Tools, 1)
	assert.Equal(t, "t", captured.Tools[0].Name)
}

func TestTransportError_ReturnsUsage(t *testing.T) {
	wantUsage := &coretypes.Usage{TotalTokens: 10, InputTokens: 6, OutputTokens: 4}
	c := newTestCaller(func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
		return &coretypes.Message{Usage: wantUsage}, assert.AnError
	})

	res, err := c.Call(context.Background(), Request{Prompt: "q", Mode: ModeText})
	require.ErrorIs(t, err, assert.AnError)
	require.NotNil(t, res)
	assert.Equal(t, wantUsage, res.Usage)
}

func TestNilTracer_DoesNotPanic(t *testing.T) {
	c := NewCaller(&mockProvider{
		generateFn: func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
			return textMessage("ok"), nil
		},
	}, nil)

	res, err := c.Call(context.Background(), Request{Prompt: "q", Mode: ModeText, SpanName: "ignored"})
	require.NoError(t, err)
	assert.Equal(t, "ok", res.Text)
}

func TestNoopTracer_DoesNotPanic(t *testing.T) {
	c := NewCaller(&mockProvider{
		generateFn: func(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
			return textMessage("ok"), nil
		},
	}, noop.NewTracerProvider().Tracer("test"))

	res, err := c.Call(context.Background(), Request{Prompt: "q", Mode: ModeText, SpanName: "span"})
	require.NoError(t, err)
	assert.Equal(t, "ok", res.Text)
}
