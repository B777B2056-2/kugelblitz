package core

import (
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentInput_IsTextOnly_Empty(t *testing.T) {
	input := AgentInput{Text: "hello"}
	assert.True(t, input.IsTextOnly())
}

func TestAgentInput_IsTextOnly_WithMedia(t *testing.T) {
	input := AgentInput{
		Text: "describe this",
		Media: []MultiModalDetail{
			{ID: "img-1", Type: constants.MultiModalTypeImage},
		},
	}
	assert.False(t, input.IsTextOnly())
}

func TestAgentInput_IsTextOnly_NilMedia(t *testing.T) {
	input := AgentInput{Text: "hello", Media: nil}
	assert.True(t, input.IsTextOnly())
}

func TestAgentInput_BuildUserMessage_TextOnly(t *testing.T) {
	input := AgentInput{Text: "hello world"}
	msg := input.BuildUserMessage()

	assert.NotEmpty(t, msg.ID)
	assert.Equal(t, constants.RoleUser, msg.Role)

	tc, ok := msg.Content.(TextContent)
	require.True(t, ok, "expected TextContent for text-only input")
	assert.Equal(t, "hello world", tc.Text)
}

func TestAgentInput_BuildUserMessage_WithSingleImage(t *testing.T) {
	input := AgentInput{
		Text: "描述这张图",
		Media: []MultiModalDetail{
			{ID: "img-1", Type: constants.MultiModalTypeImage, MimeType: "image/png"},
		},
	}
	msg := input.BuildUserMessage()

	assert.Equal(t, constants.RoleUser, msg.Role)

	cc, ok := msg.Content.(CompositeContent)
	require.True(t, ok, "expected CompositeContent when media present")
	require.Len(t, cc.Parts, 2)

	// First part: text
	textPart, ok := cc.Parts[0].(TextContent)
	require.True(t, ok)
	assert.Equal(t, "描述这张图", textPart.Text)

	// Second part: media
	mmPart, ok := cc.Parts[1].(MultiModalContent)
	require.True(t, ok)
	assert.Equal(t, "img-1", mmPart.Detail.ID)
	assert.Equal(t, constants.MultiModalTypeImage, mmPart.Detail.Type)
}

func TestAgentInput_BuildUserMessage_WithMultipleMedia(t *testing.T) {
	input := AgentInput{
		Text: "对比这两张图和这段音频",
		Media: []MultiModalDetail{
			{ID: "img-1", Type: constants.MultiModalTypeImage},
			{ID: "img-2", Type: constants.MultiModalTypeImage},
			{ID: "aud-1", Type: constants.MultiModalTypeAudio},
		},
	}
	msg := input.BuildUserMessage()

	cc, ok := msg.Content.(CompositeContent)
	require.True(t, ok)
	require.Len(t, cc.Parts, 4) // 1 text + 3 media
}

func TestAgentInput_Validate_EmptyText(t *testing.T) {
	input := AgentInput{Text: ""}
	assert.Error(t, input.Validate())
}

func TestAgentInput_Validate_EmptyTextWithMedia(t *testing.T) {
	input := AgentInput{
		Media: []MultiModalDetail{
			{ID: "img-1", Type: constants.MultiModalTypeImage},
		},
	}
	assert.Error(t, input.Validate())
}

func TestAgentInput_Validate_Ok(t *testing.T) {
	input := AgentInput{Text: "hello"}
	assert.NoError(t, input.Validate())
}

func TestAgentInput_Validate_OkWithMedia(t *testing.T) {
	input := AgentInput{
		Text: "描述这张图",
		Media: []MultiModalDetail{
			{ID: "img-1", Type: constants.MultiModalTypeImage},
		},
	}
	assert.NoError(t, input.Validate())
}

func TestAgentInput_Validate_WhitespaceOnly(t *testing.T) {
	input := AgentInput{Text: "   "}
	assert.Error(t, input.Validate())
}

func TestAgentInput_Validate_MixedMediaTypes(t *testing.T) {
	input := AgentInput{
		Text: "compare these",
		Media: []MultiModalDetail{
			{ID: "img-1", Type: constants.MultiModalTypeImage},
			{ID: "aud-1", Type: constants.MultiModalTypeAudio},
		},
	}
	assert.Error(t, input.Validate())
	assert.Contains(t, input.Validate().Error(), "mix")
}

func TestAgentInput_Validate_MultipleSameType(t *testing.T) {
	// Multiple images are allowed
	input := AgentInput{
		Text: "compare images",
		Media: []MultiModalDetail{
			{ID: "img-1", Type: constants.MultiModalTypeImage},
			{ID: "img-2", Type: constants.MultiModalTypeImage},
		},
	}
	assert.NoError(t, input.Validate())
}
