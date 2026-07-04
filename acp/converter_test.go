package acp

import (
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentBlocksToMessages_TextOnly(t *testing.T) {
	blocks := []ContentBlock{
		{Type: ContentBlockTypeText, Text: "Hello, what can you do?"},
	}
	msgs, err := ContentBlocksToMessages(blocks)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	assert.Equal(t, constants.RoleUser, msgs[0].Role)
	assert.NotEmpty(t, msgs[0].ID)

	tc, ok := msgs[0].Content.(core.TextContent)
	require.True(t, ok)
	assert.Equal(t, "Hello, what can you do?", tc.Text)
}

func TestContentBlocksToMessages_MultipleText(t *testing.T) {
	blocks := []ContentBlock{
		{Type: ContentBlockTypeText, Text: "First message."},
		{Type: ContentBlockTypeText, Text: "Second message."},
	}
	msgs, err := ContentBlocksToMessages(blocks)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, "First message.", msgs[0].Content.(core.TextContent).Text)
	assert.Equal(t, "Second message.", msgs[1].Content.(core.TextContent).Text)
}

func TestContentBlocksToMessages_Image(t *testing.T) {
	blocks := []ContentBlock{
		{Type: ContentBlockTypeImage, Data: "base64data", MimeType: "image/png"},
	}
	msgs, err := ContentBlocksToMessages(blocks)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	mmc, ok := msgs[0].Content.(core.MultiModalContent)
	require.True(t, ok)
	assert.Equal(t, constants.MultiModalTypeImage, mmc.Detail.Type)
	assert.Equal(t, "base64data", mmc.Detail.Base64)
}

func TestContentBlocksToMessages_ResourceLink(t *testing.T) {
	blocks := []ContentBlock{
		{Type: ContentBlockTypeResourceLink, URI: "file:///tmp/doc.md", Name: "doc.md"},
	}
	msgs, err := ContentBlocksToMessages(blocks)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	mmc, ok := msgs[0].Content.(core.MultiModalContent)
	require.True(t, ok)
	assert.Equal(t, "file:///tmp/doc.md", mmc.Detail.Path)
}

func TestContentBlocksToMessages_Empty(t *testing.T) {
	msgs, err := ContentBlocksToMessages(nil)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestContentBlocksToMessages_UnknownType(t *testing.T) {
	blocks := []ContentBlock{
		{Type: "unknown_type", Text: "foo"},
	}
	msgs, err := ContentBlocksToMessages(blocks)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	// Unknown types are treated as text
	tc, ok := msgs[0].Content.(core.TextContent)
	require.True(t, ok)
	assert.Equal(t, "foo", tc.Text)
}

func TestMessagesToContentBlocks_TextContent(t *testing.T) {
	msgs := []core.Message{
		core.NewAssistantMessage(core.TextContent{Text: "I can help!"}),
	}
	blocks := MessagesToContentBlocks(msgs)
	require.Len(t, blocks, 1)
	assert.Equal(t, ContentBlockTypeText, blocks[0].Type)
	assert.Equal(t, "I can help!", blocks[0].Text)
}

func TestMessagesToContentBlocks_MultiModal(t *testing.T) {
	msgs := []core.Message{
		{
			ID:   "m1",
			Role: constants.RoleAssistant,
			Content: core.MultiModalContent{
				Detail: core.MultiModalDetail{
					Type:   constants.MultiModalTypeImage,
					Path:   "/tmp/img.png",
					Base64: "abc123",
				},
			},
		},
	}
	blocks := MessagesToContentBlocks(msgs)
	require.Len(t, blocks, 1)
	assert.Equal(t, ContentBlockTypeImage, blocks[0].Type)
	assert.Equal(t, "abc123", blocks[0].Data)
}

func TestMessagesToContentBlocks_Empty(t *testing.T) {
	blocks := MessagesToContentBlocks(nil)
	assert.Empty(t, blocks)
}

func TestTextToStreamChunks(t *testing.T) {
	chunks := TextToStreamChunks("Hello world")
	assert.Len(t, chunks, 1)
	assert.Equal(t, UpdateTypeAgentMessageChunk, chunks[0].SessionUpdate)
	assert.Equal(t, "Hello world", chunks[0].Content.Text)
}

func TestToolCallToNotification(t *testing.T) {
	detail := core.ToolCallDetail{
		ID:       "call_1",
		ToolName: "read_file",
		Args:     map[string]any{"path": "/tmp/test.txt"},
	}
	notif := ToolCallToNotification(detail)
	assert.Equal(t, UpdateTypeToolCall, notif.SessionUpdate)
	assert.Equal(t, "read_file", notif.Title)
	assert.Equal(t, "call_1", notif.ToolCallID)
	assert.Equal(t, ToolCallStatusRunning, notif.Status)
	assert.Equal(t, "/tmp/test.txt", notif.Input["path"])
}

func TestToolResultToNotification_Completed(t *testing.T) {
	result := core.ToolCallResult{
		ToolCallID: "call_1",
		ToolName:   "read_file",
		Outputs:    map[string]any{"content": "file contents here"},
	}
	notif := ToolResultToNotification(result)
	assert.Equal(t, UpdateTypeToolCallUpdate, notif.SessionUpdate)
	assert.Equal(t, "call_1", notif.ToolCallID)
	assert.Equal(t, ToolCallStatusCompleted, notif.Status)
	require.Len(t, notif.Content, 1)
	assert.Contains(t, notif.Content[0].Text, "file contents here")
}

func TestToolResultToNotification_Error(t *testing.T) {
	result := core.ToolCallResult{
		ToolCallID: "call_err",
		ToolName:   "bad_tool",
		Outputs:    map[string]any{"error": "something went wrong"},
	}
	notif := ToolResultToNotification(result)
	assert.Equal(t, ToolCallStatusError, notif.Status)
}

// ---- Additional converter coverage ----

func TestContentBlocksToMessages_Resource(t *testing.T) {
	blocks := []ContentBlock{
		{
			Type: ContentBlockTypeResource,
			Resource: &ResourceContent{
				URI:  "file:///tmp/doc.md",
				Text: "file contents",
			},
		},
	}
	msgs, err := ContentBlocksToMessages(blocks)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	tc, ok := msgs[0].Content.(core.TextContent)
	require.True(t, ok)
	assert.Equal(t, "file contents", tc.Text)
}

func TestContentBlocksToMessages_ImageVideoAudio(t *testing.T) {
	tests := []struct {
		name     string
		mime     string
		expected constants.MultiModalType
	}{
		{"video/mp4", "video/mp4", constants.MultiModalTypeVideo},
		{"audio/mpeg", "audio/mpeg", constants.MultiModalTypeAudio},
		{"application/pdf", "application/pdf", constants.MultiModalTypePDF},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", constants.MultiModalTypeWord},
		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", constants.MultiModalTypeExcel},
		{"application/octet-stream", "application/octet-stream", constants.MultiModalTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := []ContentBlock{
				{Type: ContentBlockTypeImage, Data: "xyz", MimeType: tt.mime},
			}
			msgs, err := ContentBlocksToMessages(blocks)
			require.NoError(t, err)
			mmc, ok := msgs[0].Content.(core.MultiModalContent)
			require.True(t, ok)
			assert.Equal(t, tt.expected, mmc.Detail.Type)
		})
	}
}

func TestMessagesToContentBlocks_ReasoningContent(t *testing.T) {
	msgs := []core.Message{
		{
			ID:   "m1",
			Role: constants.RoleAssistant,
			Content: core.ReasoningContent{
				Reasoning: "internal thought",
			},
		},
	}
	blocks := MessagesToContentBlocks(msgs)
	// Reasoning content should be filtered out (not exposed as content block)
	assert.Empty(t, blocks)
}

func TestMessagesToContentBlocks_ToolCallContent(t *testing.T) {
	msgs := []core.Message{
		{
			ID:   "m1",
			Role: constants.RoleAssistant,
			Content: core.ToolCallContent{
				Details: []core.ToolCallDetail{
					{ID: "tc_1", ToolName: "read_file", Args: map[string]any{"path": "/f"}},
				},
			},
		},
	}
	// Tool calls are sent via notifications, not content blocks
	blocks := MessagesToContentBlocks(msgs)
	assert.Empty(t, blocks)
}

func TestMessagesToContentBlocks_ToolResultContent(t *testing.T) {
	msgs := []core.Message{
		{
			ID:   "m1",
			Role: constants.RoleTool,
			Content: core.ToolResultContent{
				Results: []core.ToolCallResult{
					{ToolCallID: "tc_1", ToolName: "read_file", Outputs: map[string]any{"ok": true}},
				},
			},
		},
	}
	blocks := MessagesToContentBlocks(msgs)
	assert.Empty(t, blocks)
}

func TestMessagesToContentBlocks_CompositeContent(t *testing.T) {
	msgs := []core.Message{
		{
			ID:   "m1",
			Role: constants.RoleAssistant,
			Content: core.CompositeContent{
				Parts: []core.Content{
					core.TextContent{Text: "First part"},
					core.TextContent{Text: "Second part"},
				},
			},
		},
	}
	blocks := MessagesToContentBlocks(msgs)
	require.Len(t, blocks, 1)
	assert.Equal(t, ContentBlockTypeText, blocks[0].Type)
	assert.Equal(t, "First part", blocks[0].Text)
}

func TestMessagesToContentBlocks_NilContent(t *testing.T) {
	msgs := []core.Message{
		{ID: "m1", Role: constants.RoleAssistant, Content: nil},
	}
	blocks := MessagesToContentBlocks(msgs)
	assert.Empty(t, blocks)
}

func TestTextToStreamChunks_Empty(t *testing.T) {
	chunks := TextToStreamChunks("")
	assert.Nil(t, chunks)
}

func TestMimeToMultiModalType_AllPaths(t *testing.T) {
	tests := []struct {
		mime     string
		expected constants.MultiModalType
	}{
		{"image/png", constants.MultiModalTypeImage},
		{"image/jpeg", constants.MultiModalTypeImage},
		{"video/mp4", constants.MultiModalTypeVideo},
		{"audio/mpeg", constants.MultiModalTypeAudio},
		{"application/pdf", constants.MultiModalTypePDF},
		{"application/msword", constants.MultiModalTypeWord},
		{"application/vnd.ms-excel", constants.MultiModalTypeExcel},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", constants.MultiModalTypeWord},
		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", constants.MultiModalTypeExcel},
		{"text/plain", constants.MultiModalTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			got := mimeToMultiModalType(tt.mime)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestMultiModalToMime_AllPaths(t *testing.T) {
	tests := []struct {
		mmType   constants.MultiModalType
		expected string
	}{
		{constants.MultiModalTypeImage, "image/png"},
		{constants.MultiModalTypeVideo, "video/mp4"},
		{constants.MultiModalTypeAudio, "audio/mpeg"},
		{constants.MultiModalTypePDF, "application/pdf"},
		{constants.MultiModalTypeUnknown, "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(string(tt.mmType), func(t *testing.T) {
			got := multiModalToMime(tt.mmType)
			assert.Equal(t, tt.expected, got)
		})
	}
}
