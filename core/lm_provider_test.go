package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// testEventHandler implements ModelEventHandler for testing.
type testEventHandler struct {
	thinkingChunks []string
	replyChunks    []string
	toolCalls      []ToolCallDetail
	finishReasons  []string
	usages         []Usage
	errors         []error
}

func (h *testEventHandler) OnThinkingChunk(chunk string) {
	h.thinkingChunks = append(h.thinkingChunks, chunk)
}
func (h *testEventHandler) OnReplyChunk(chunk string) { h.replyChunks = append(h.replyChunks, chunk) }
func (h *testEventHandler) OnBlockThinking(reasoning string) {
	h.thinkingChunks = append(h.thinkingChunks, reasoning)
}
func (h *testEventHandler) OnBlockReply(text string) { h.replyChunks = append(h.replyChunks, text) }
func (h *testEventHandler) OnFunctionCall(detail ToolCallDetail) {
	h.toolCalls = append(h.toolCalls, detail)
}
func (h *testEventHandler) OnFinished(reason string) {
	h.finishReasons = append(h.finishReasons, reason)
}
func (h *testEventHandler) OnUsageUpdated(usage Usage) { h.usages = append(h.usages, usage) }
func (h *testEventHandler) OnError(err error)          { h.errors = append(h.errors, err) }

var _ ModelEventHandler = (*testEventHandler)(nil)

func TestModelEventHandler_InterfaceImplementation(t *testing.T) {
	h := &testEventHandler{}
	h.OnReplyChunk("hello")
	h.OnReplyChunk(" world")
	h.OnFinished("stop")

	assert.Equal(t, []string{"hello", " world"}, h.replyChunks)
	assert.Equal(t, []string{"stop"}, h.finishReasons)
}

func TestModelEventHandler_ChunkCallbacksOptional(t *testing.T) {
	h := &testEventHandler{}
	h.OnThinkingChunk("think")
	assert.Len(t, h.thinkingChunks, 1)
	assert.Empty(t, h.replyChunks)
}

func TestGenerateParams_ZeroValue(t *testing.T) {
	params := GenerateParams{}
	assert.False(t, params.Stream)
	assert.Nil(t, params.Messages)
	assert.Nil(t, params.Tools)
	assert.Nil(t, params.EventHandler)
}

func TestGenerateParams_WithBlockMode(t *testing.T) {
	h := &testEventHandler{}
	params := GenerateParams{
		Messages:     []Message{NewUserMessage(TextContent{Text: "hi"})},
		Stream:       false,
		EventHandler: h,
	}
	assert.False(t, params.Stream)
	assert.NotNil(t, params.EventHandler)
}

func TestGenerateParams_WithStreamMode(t *testing.T) {
	h := &testEventHandler{}
	params := GenerateParams{
		Messages:     []Message{NewUserMessage(TextContent{Text: "hi"})},
		Tools:        []ToolDefinition{{Name: "tool1"}},
		Stream:       true,
		EventHandler: h,
	}
	assert.True(t, params.Stream)
	assert.NotNil(t, params.EventHandler)
	assert.Len(t, params.Tools, 1)
}

func TestGenerateParams_WithThinkingEnabled(t *testing.T) {
	enabled := true
	params := GenerateParams{
		Messages:        []Message{NewUserMessage(TextContent{Text: "hi"})},
		EnableThinking:  &enabled,
		ReasoningEffort: ReasoningEffortHigh,
	}
	assert.NotNil(t, params.EnableThinking)
	assert.True(t, *params.EnableThinking)
	assert.Equal(t, ReasoningEffortHigh, params.ReasoningEffort)
}

func TestGenerateParams_WithThinkingDisabled(t *testing.T) {
	disabled := false
	params := GenerateParams{
		EnableThinking: &disabled,
	}
	assert.NotNil(t, params.EnableThinking)
	assert.False(t, *params.EnableThinking)
}

func TestGenerateParams_ThinkingDefaultNil(t *testing.T) {
	params := GenerateParams{}
	assert.Nil(t, params.EnableThinking)
	assert.Empty(t, params.ReasoningEffort)
}

type mockProvider struct {
	generateFn func(ctx context.Context, params GenerateParams) (*Message, error)
}

func (m *mockProvider) Generate(ctx context.Context, params GenerateParams) (*Message, error) {
	if m.generateFn != nil {
		return m.generateFn(ctx, params)
	}
	return nil, nil
}

var _ ILMProvider = (*mockProvider)(nil)

func TestILMProvider_InterfaceSatisfied(t *testing.T) {
	p := &mockProvider{}
	assert.NotNil(t, p)
}

func TestMockProvider_Generate_Delegates(t *testing.T) {
	expected := NewAssistantMessage(TextContent{Text: "response"})
	p := &mockProvider{
		generateFn: func(ctx context.Context, params GenerateParams) (*Message, error) {
			return &expected, nil
		},
	}
	result, err := p.Generate(context.Background(), GenerateParams{})
	assert.NoError(t, err)
	assert.Equal(t, "response", result.Content.(TextContent).Text)
}
