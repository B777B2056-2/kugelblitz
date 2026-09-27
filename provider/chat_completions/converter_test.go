//nolint:staticcheck
package chat_completions

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newConverter() *Converter { return NewConverter() }

// --- ConvertMessages ---

func TestConvertMessages_SystemMessage(t *testing.T) {
	c := newConverter()
	msgs := []coretypes.Message{
		{ID: "s1", Role: constants.RoleSystem, Content: coretypes.TextContent{Text: "system prompt"}},
	}
	result, err := c.ConvertMessages(msgs)
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestConvertMessages_UserTextMessage(t *testing.T) {
	c := newConverter()
	msgs := []coretypes.Message{coretypes.NewUserMessage(coretypes.TextContent{Text: "hello"})}
	result, err := c.ConvertMessages(msgs)
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestConvertMessages_AssistantTextMessage(t *testing.T) {
	c := newConverter()
	msgs := []coretypes.Message{coretypes.NewAssistantMessage(coretypes.TextContent{Text: "response"})}
	result, err := c.ConvertMessages(msgs)
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestConvertMessages_ToolCallWithReasoning(t *testing.T) {
	c := newConverter()
	msg := coretypes.Message{
		ID:   "a1",
		Role: constants.RoleAssistant,
		Content: coretypes.CompositeContent{
			Parts: []coretypes.Content{
				coretypes.ReasoningContent{Reasoning: "I need to search"},
				coretypes.ToolCallContent{Details: []coretypes.ToolCallDetail{
					{ID: "tc-1", ToolName: "search", Args: map[string]any{"q": "test"}},
				}},
			},
		},
	}
	result, err := c.ConvertMessages([]coretypes.Message{msg})
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestConvertMessages_ToolResultMessage(t *testing.T) {
	c := newConverter()
	msgs := []coretypes.Message{
		coretypes.NewToolMessage([]coretypes.ToolCallResult{
			{ToolCallID: "tc-1", ToolName: "search", Outputs: map[string]any{"result": "found"}},
		}),
	}
	result, err := c.ConvertMessages(msgs)
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestConvertMessages_EmptyList(t *testing.T) {
	c := newConverter()
	result, err := c.ConvertMessages([]coretypes.Message{})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestConvertMessages_UnknownRole(t *testing.T) {
	c := newConverter()
	_, err := c.ConvertMessages([]coretypes.Message{{Role: constants.RoleType("invalid")}})
	assert.Error(t, err)
}

// --- ConvertTools ---

func TestConvertTools_SingleTool(t *testing.T) {
	c := newConverter()
	tools := []coretypes.ToolDefinition{
		{Name: "search", Description: "Search", JSONSchema: map[string]any{"type": "object"}},
	}
	result, err := c.ConvertTools(tools)
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestConvertTools_Empty(t *testing.T) {
	c := newConverter()
	result, err := c.ConvertTools([]coretypes.ToolDefinition{})
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestIsStrictCompliant(t *testing.T) {
	tests := []struct {
		name   string
		schema map[string]any
		want   bool
	}{
		{
			name: "compliant object with required",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"drift":  map[string]any{"type": "boolean"},
					"reason": map[string]any{"type": "string"},
				},
				"required":             []any{"drift", "reason"},
				"additionalProperties": false,
			},
			want: true,
		},
		{
			name:   "missing additionalProperties",
			schema: map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}, "required": []any{"a"}},
			want:   false,
		},
		{
			name:   "additionalProperties true",
			schema: map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}, "required": []any{"a"}, "additionalProperties": true},
			want:   false,
		},
		{
			name:   "non-object type",
			schema: map[string]any{"type": "string"},
			want:   false,
		},
		{
			name:   "property missing from required",
			schema: map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}, "b": map[string]any{"type": "string"}}, "required": []any{"a"}, "additionalProperties": false},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isStrictCompliant(tt.schema))
		})
	}
}

func TestConvertTools_StrictOnlyWhenCompliant(t *testing.T) {
	c := newConverter()
	compliant := coretypes.ToolDefinition{
		Name: "compliant", Description: "C",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"q": map[string]any{"type": "string"},
			},
			"required":             []any{"q"},
			"additionalProperties": false,
		},
	}
	nonCompliant := coretypes.ToolDefinition{
		Name: "noncompliant", Description: "N",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"q": map[string]any{"type": "string"},
			},
		},
	}

	result, err := c.ConvertTools([]coretypes.ToolDefinition{compliant, nonCompliant})
	require.NoError(t, err)
	require.Len(t, result, 2)

	// Compliant schema → Strict is set (valid) and true.
	strict0 := result[0].OfFunction.Function.Strict
	require.True(t, strict0.Valid())
	assert.True(t, strict0.Value)

	// Non-compliant schema → Strict is omitted (invalid / not present).
	assert.False(t, result[1].OfFunction.Function.Strict.Valid())
}

// --- ParseResponse ---

func TestParseResponse_TextMessage(t *testing.T) {
	c := newConverter()
	raw := openai.ChatCompletionMessage{Content: "hello world"}
	result, err := c.ParseResponse(context.Background(), "p1", raw)
	require.NoError(t, err)

	text, ok := result.Content.(coretypes.TextContent)
	require.True(t, ok)
	assert.Equal(t, "hello world", text.Text)
}

func TestParseResponse_ToolCalls(t *testing.T) {
	c := newConverter()
	raw := openai.ChatCompletionMessage{
		ToolCalls: []openai.ChatCompletionMessageToolCallUnion{
			{ID: "tc-1", Function: openai.ChatCompletionMessageFunctionToolCallFunction{Name: "search", Arguments: `{"q":"t"}`}},
		},
	}
	result, err := c.ParseResponse(context.Background(), "p1", raw)
	require.NoError(t, err)

	toolContent, ok := result.Content.(coretypes.ToolCallContent)
	require.True(t, ok)
	assert.Equal(t, "search", toolContent.Details[0].ToolName)
}

// --- ParseStreamChunk ---

func TestParseStreamChunk_TextDelta(t *testing.T) {
	c := newConverter()
	raw := openai.ChatCompletionChunk{
		Choices: []openai.ChatCompletionChunkChoice{
			{Delta: openai.ChatCompletionChunkChoiceDelta{Content: "hello"}},
		},
	}
	result, err := c.ParseStreamChunk(context.Background(), "p1", raw)
	require.NoError(t, err)
	text, ok := result.Content.(coretypes.TextContent)
	require.True(t, ok)
	assert.Equal(t, "hello", text.Text)
}

func TestParseStreamChunk_EmptyChoices(t *testing.T) {
	c := newConverter()
	result, err := c.ParseStreamChunk(context.Background(), "p1", openai.ChatCompletionChunk{})
	require.NoError(t, err)
	assert.Nil(t, result)
}

// TestParseStreamChunk_UsageOnlyChunk guards the include_usage terminal chunk:
// streaming APIs (DeepSeek/OpenAI) emit a final chunk with usage but no choices.
// It must still surface a message carrying Usage so token tracking works.
func TestParseStreamChunk_UsageOnlyChunk(t *testing.T) {
	c := newConverter()
	raw := openai.ChatCompletionChunk{
		Usage: openai.CompletionUsage{
			TotalTokens:      70,
			PromptTokens:     31,
			CompletionTokens: 39,
		},
	}
	result, err := c.ParseStreamChunk(context.Background(), "p1", raw)
	require.NoError(t, err)
	require.NotNil(t, result, "usage-only chunk must yield a message carrying usage")
	require.NotNil(t, result.Usage)
	assert.Equal(t, int64(70), result.Usage.TotalTokens)
	assert.Equal(t, int64(31), result.Usage.InputTokens)
	assert.Equal(t, int64(39), result.Usage.OutputTokens)
}

// TestParseStreamChunk_UsageWithEmptyContentChunk guards DeepSeek's include_usage
// terminal chunk: it has a choice with empty content and finish_reason, and the
// usage attached to the same chunk. Usage must survive despite empty content.
func TestParseStreamChunk_UsageWithEmptyContentChunk(t *testing.T) {
	c := newConverter()
	raw := openai.ChatCompletionChunk{
		Choices: []openai.ChatCompletionChunkChoice{
			{FinishReason: "stop", Delta: openai.ChatCompletionChunkChoiceDelta{Content: ""}},
		},
		Usage: openai.CompletionUsage{
			TotalTokens:      77,
			PromptTokens:     31,
			CompletionTokens: 46,
		},
	}
	result, err := c.ParseStreamChunk(context.Background(), "p1", raw)
	require.NoError(t, err)
	require.NotNil(t, result, "terminal chunk with empty content must still surface usage")
	require.NotNil(t, result.Usage)
	assert.Equal(t, int64(77), result.Usage.TotalTokens)
	assert.Equal(t, "stop", result.FinishReason)
}

func TestParseStreamChunk_WithFinishReason(t *testing.T) {
	c := newConverter()
	raw := openai.ChatCompletionChunk{
		Choices: []openai.ChatCompletionChunkChoice{
			{FinishReason: "stop", Delta: openai.ChatCompletionChunkChoiceDelta{Content: "final"}},
		},
	}
	result, err := c.ParseStreamChunk(context.Background(), "p1", raw)
	require.NoError(t, err)
	assert.Equal(t, "stop", result.FinishReason)
}

func TestParseStreamChunk_EmptyDelta(t *testing.T) {
	c := newConverter()
	raw := openai.ChatCompletionChunk{
		Choices: []openai.ChatCompletionChunkChoice{{Delta: openai.ChatCompletionChunkChoiceDelta{}}},
	}
	result, err := c.ParseStreamChunk(context.Background(), "p1", raw)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// --- reasoning_content parsing ---

func TestParseReasoningFromRaw_Present(t *testing.T) {
	assert.Equal(t, "thinking...", parseReasoningFromRaw(`{"reasoning_content":"thinking..."}`))
}

func TestParseReasoningFromRaw_Absent(t *testing.T) {
	assert.Empty(t, parseReasoningFromRaw(`{"content":"hello"}`))
}

func TestParseReasoningFromRaw_Empty(t *testing.T) {
	assert.Empty(t, parseReasoningFromRaw(""))
}

func TestParseReasoningFromChunkRaw_Present(t *testing.T) {
	raw := `{"choices":[{"delta":{"reasoning_content":"thinking..."}}]}`
	assert.Equal(t, "thinking...", parseReasoningFromChunkRaw(raw))
}

func TestParseReasoningFromChunkRaw_Absent(t *testing.T) {
	assert.Empty(t, parseReasoningFromChunkRaw(`{"choices":[{"delta":{"content":"hello"}}]}`))
}

// --- Multimodal conversion ---

func TestConvertMessages_UserImageMessage(t *testing.T) {
	c := newConverter()
	msgs := []coretypes.Message{
		coretypes.NewUserMessage(coretypes.MultiModalContent{
			Detail: coretypes.MultiModalDetail{
				ID:       "img-1",
				Type:     constants.MultiModalTypeImage,
				Base64:   "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk",
				MimeType: "image/png",
			},
		}),
	}
	result, err := c.ConvertMessages(msgs)
	require.NoError(t, err)
	require.Len(t, result, 1)

	// Should produce a user message with image content parts
	param := result[0]
	require.NotNil(t, param.OfUser)
	userMsg := param.OfUser

	// Content should be an array of content parts
	parts := userMsg.Content.OfArrayOfContentParts
	require.NotEmpty(t, parts)

	// First part should be image_url
	imagePart := parts[0]
	require.NotNil(t, imagePart.OfImageURL)
	assert.Contains(t, imagePart.OfImageURL.ImageURL.URL, "data:image/png;base64,iVBORw0KGgo")
}

func TestConvertMessages_UserAudioMessage(t *testing.T) {
	c := newConverter()
	msgs := []coretypes.Message{
		coretypes.NewUserMessage(coretypes.MultiModalContent{
			Detail: coretypes.MultiModalDetail{
				ID:       "aud-1",
				Type:     constants.MultiModalTypeAudio,
				Base64:   "ZGF0YQ==", // "data" in base64
				MimeType: "audio/wav",
			},
		}),
	}
	result, err := c.ConvertMessages(msgs)
	require.NoError(t, err)
	require.Len(t, result, 1)

	param := result[0]
	require.NotNil(t, param.OfUser)
	parts := param.OfUser.Content.OfArrayOfContentParts
	require.NotEmpty(t, parts)

	// First part should be input_audio
	audioPart := parts[0]
	require.NotNil(t, audioPart.OfInputAudio)
	assert.Equal(t, "ZGF0YQ==", audioPart.OfInputAudio.InputAudio.Data)
	assert.Equal(t, "wav", audioPart.OfInputAudio.InputAudio.Format)
}

func TestConvertMessages_UserCompositeTextAndImage(t *testing.T) {
	c := newConverter()
	msgs := []coretypes.Message{
		coretypes.NewUserMessage(coretypes.CompositeContent{
			Parts: []coretypes.Content{
				coretypes.TextContent{Text: "请描述这张图片"},
				coretypes.MultiModalContent{
					Detail: coretypes.MultiModalDetail{
						ID:       "img-1",
						Type:     constants.MultiModalTypeImage,
						Base64:   "iVBORw0KGgo=",
						MimeType: "image/png",
					},
				},
			},
		}),
	}
	result, err := c.ConvertMessages(msgs)
	require.NoError(t, err)
	require.Len(t, result, 1)

	param := result[0]
	require.NotNil(t, param.OfUser)
	parts := param.OfUser.Content.OfArrayOfContentParts
	require.Len(t, parts, 2)

	// First part: text
	textPart := parts[0]
	require.NotNil(t, textPart.OfText)
	assert.Equal(t, "请描述这张图片", textPart.OfText.Text)

	// Second part: image
	imagePart := parts[1]
	require.NotNil(t, imagePart.OfImageURL)
}

func TestConvertMessages_UserVideoMessage(t *testing.T) {
	c := newConverter()
	msgs := []coretypes.Message{
		coretypes.NewUserMessage(coretypes.MultiModalContent{
			Detail: coretypes.MultiModalDetail{
				ID:       "vid-1",
				Type:     constants.MultiModalTypeVideo,
				Base64:   "iVBORw0KGgo=",
				MimeType: "video/mp4",
			},
		}),
	}
	result, err := c.ConvertMessages(msgs)
	require.NoError(t, err)
	require.Len(t, result, 1)

	// Video → image_url (first frame)
	param := result[0]
	require.NotNil(t, param.OfUser)
	parts := param.OfUser.Content.OfArrayOfContentParts
	require.NotEmpty(t, parts)
	assert.NotNil(t, parts[0].OfImageURL)
}

func TestConvertMessages_UnsupportedMediaType(t *testing.T) {
	c := newConverter()
	msgs := []coretypes.Message{
		coretypes.NewUserMessage(coretypes.MultiModalContent{
			Detail: coretypes.MultiModalDetail{
				ID:   "pdf-1",
				Type: constants.MultiModalTypePDF,
			},
		}),
	}
	_, err := c.ConvertMessages(msgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported media type")
}

func TestAudioFormat(t *testing.T) {
	assert.Equal(t, "wav", audioFormat("audio/wav"))
	assert.Equal(t, "mpeg", audioFormat("audio/mpeg"))
	assert.Equal(t, "mp4", audioFormat("audio/mp4"))
	assert.Equal(t, "webm", audioFormat("audio/webm"))
	assert.Equal(t, "wav", audioFormat("image/unknown")) // fallback
}
