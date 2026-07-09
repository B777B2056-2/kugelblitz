//nolint:staticcheck
package chat_completions

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newConverter() *Converter { return NewConverter() }

// --- ConvertMessages ---

func TestConvertMessages_SystemMessage(t *testing.T) {
	c := newConverter()
	msgs := []core.Message{
		{ID: "s1", Role: constants.RoleSystem, Content: core.TextContent{Text: "system prompt"}},
	}
	result, err := c.ConvertMessages(msgs)
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestConvertMessages_UserTextMessage(t *testing.T) {
	c := newConverter()
	msgs := []core.Message{core.NewUserMessage(core.TextContent{Text: "hello"})}
	result, err := c.ConvertMessages(msgs)
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestConvertMessages_AssistantTextMessage(t *testing.T) {
	c := newConverter()
	msgs := []core.Message{core.NewAssistantMessage(core.TextContent{Text: "response"})}
	result, err := c.ConvertMessages(msgs)
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestConvertMessages_ToolCallWithReasoning(t *testing.T) {
	c := newConverter()
	msg := core.Message{
		ID:   "a1",
		Role: constants.RoleAssistant,
		Content: core.CompositeContent{
			Parts: []core.Content{
				core.ReasoningContent{Reasoning: "I need to search"},
				core.ToolCallContent{Details: []core.ToolCallDetail{
					{ID: "tc-1", ToolName: "search", Args: map[string]any{"q": "test"}},
				}},
			},
		},
	}
	result, err := c.ConvertMessages([]core.Message{msg})
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestConvertMessages_ToolResultMessage(t *testing.T) {
	c := newConverter()
	msgs := []core.Message{
		core.NewToolMessage([]core.ToolCallResult{
			{ToolCallID: "tc-1", ToolName: "search", Outputs: map[string]any{"result": "found"}},
		}),
	}
	result, err := c.ConvertMessages(msgs)
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestConvertMessages_EmptyList(t *testing.T) {
	c := newConverter()
	result, err := c.ConvertMessages([]core.Message{})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestConvertMessages_UnknownRole(t *testing.T) {
	c := newConverter()
	_, err := c.ConvertMessages([]core.Message{{Role: constants.RoleType("invalid")}})
	assert.Error(t, err)
}

// --- ConvertTools ---

func TestConvertTools_SingleTool(t *testing.T) {
	c := newConverter()
	tools := []core.ToolDefinition{
		{Name: "search", Description: "Search", JSONSchema: map[string]any{"type": "object"}},
	}
	result, err := c.ConvertTools(tools)
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestConvertTools_Empty(t *testing.T) {
	c := newConverter()
	result, err := c.ConvertTools([]core.ToolDefinition{})
	require.NoError(t, err)
	assert.Nil(t, result)
}

// --- ParseResponse ---

func TestParseResponse_TextMessage(t *testing.T) {
	c := newConverter()
	raw := openai.ChatCompletionMessage{Content: "hello world"}
	result, err := c.ParseResponse(context.Background(), "p1", raw)
	require.NoError(t, err)

	text, ok := result.Content.(core.TextContent)
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

	toolContent, ok := result.Content.(core.ToolCallContent)
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
	text, ok := result.Content.(core.TextContent)
	require.True(t, ok)
	assert.Equal(t, "hello", text.Text)
}

func TestParseStreamChunk_EmptyChoices(t *testing.T) {
	c := newConverter()
	result, err := c.ParseStreamChunk(context.Background(), "p1", openai.ChatCompletionChunk{})
	require.NoError(t, err)
	assert.Nil(t, result)
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
	msgs := []core.Message{
		core.NewUserMessage(core.MultiModalContent{
			Detail: core.MultiModalDetail{
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
	msgs := []core.Message{
		core.NewUserMessage(core.MultiModalContent{
			Detail: core.MultiModalDetail{
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
	msgs := []core.Message{
		core.NewUserMessage(core.CompositeContent{
			Parts: []core.Content{
				core.TextContent{Text: "请描述这张图片"},
				core.MultiModalContent{
					Detail: core.MultiModalDetail{
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
	msgs := []core.Message{
		core.NewUserMessage(core.MultiModalContent{
			Detail: core.MultiModalDetail{
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
	msgs := []core.Message{
		core.NewUserMessage(core.MultiModalContent{
			Detail: core.MultiModalDetail{
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
