package main

import (
	"github.com/B777B2056-2/kugelblitz/constants"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/google/uuid"
)

// ContentBlocksToMessages converts ACP content blocks into Kugelblitz core.Messages.
// Each content block becomes a user message (since ACP sessions receive user prompts).
func ContentBlocksToMessages(blocks []ContentBlock) ([]coretypes.Message, error) {
	if len(blocks) == 0 {
		return nil, nil
	}

	msgs := make([]coretypes.Message, 0, len(blocks))
	for _, block := range blocks {
		content := contentBlockToCoreContent(block)
		if content == nil {
			continue
		}
		msgs = append(msgs, coretypes.Message{
			ID:      uuid.New().String(),
			Role:    constants.RoleUser,
			Content: content,
		})
	}
	return msgs, nil
}

// contentBlockToCoreContent converts a single ACP content block to a coretypes.Content.
func contentBlockToCoreContent(block ContentBlock) coretypes.Content {
	switch block.Type {
	case ContentBlockTypeText:
		return coretypes.TextContent{Text: block.Text}

	case ContentBlockTypeImage:
		mimeType := block.MimeType
		mmType := constants.MultiModalTypeImage
		if mimeType != "" {
			mmType = mimeToMultiModalType(mimeType)
		}
		return coretypes.MultiModalContent{
			Detail: coretypes.MultiModalDetail{
				Type:   mmType,
				Base64: block.Data,
			},
		}

	case ContentBlockTypeResourceLink:
		return coretypes.MultiModalContent{
			Detail: coretypes.MultiModalDetail{
				Type: constants.MultiModalTypeUnknown,
				Path: block.URI,
			},
		}

	case ContentBlockTypeResource:
		if block.Resource != nil {
			return coretypes.TextContent{Text: block.Resource.Text}
		}
		return coretypes.TextContent{Text: block.Text}

	default:
		// Fallback: treat unknown types as text
		return coretypes.TextContent{Text: block.Text}
	}
}

// MessagesToContentBlocks converts Kugelblitz core.Messages back into ACP content blocks.
// This is used when replaying session history during session/load.
func MessagesToContentBlocks(msgs []coretypes.Message) []ContentBlock {
	if len(msgs) == 0 {
		return nil
	}

	blocks := make([]ContentBlock, 0, len(msgs))
	for _, msg := range msgs {
		block := coreContentToContentBlock(msg.Content)
		if block != nil {
			blocks = append(blocks, *block)
		}
	}
	return blocks
}

// coreContentToContentBlock converts a coretypes.Content to an ACP content block.
func coreContentToContentBlock(c coretypes.Content) *ContentBlock {
	if c == nil {
		return nil
	}

	switch ct := c.(type) {
	case coretypes.TextContent:
		return &ContentBlock{
			Type: ContentBlockTypeText,
			Text: ct.Text,
		}

	case coretypes.ReasoningContent:
		// Reasoning is internal — don't expose as a content block
		return nil

	case coretypes.ToolCallContent:
		// Tool calls are sent via session/update notifications, not content blocks
		return nil

	case coretypes.ToolResultContent:
		// Tool results are sent via session/update notifications, not content blocks
		return nil

	case coretypes.MultiModalContent:
		switch ct.Detail.Type {
		case constants.MultiModalTypeImage:
			return &ContentBlock{
				Type:     ContentBlockTypeImage,
				Data:     ct.Detail.Base64,
				MimeType: multiModalToMime(ct.Detail.Type),
			}
		default:
			return &ContentBlock{
				Type: ContentBlockTypeResourceLink,
				URI:  ct.Detail.Path,
				Name: ct.Detail.Path,
			}
		}

	case coretypes.CompositeContent:
		// Return the first text part (composite combines multiple parts)
		for _, part := range ct.Parts {
			if tc, ok := part.(coretypes.TextContent); ok {
				return &ContentBlock{
					Type: ContentBlockTypeText,
					Text: tc.Text,
				}
			}
		}
		return nil

	default:
		return nil
	}
}

// TextToStreamChunks splits agent text output into ACP agent_message_chunk
// notifications using the ACP v1 format.
func TextToStreamChunks(text string) []AgentMessageChunk {
	if text == "" {
		return nil
	}
	return []AgentMessageChunk{
		NewAgentMessageChunk(text),
	}
}

// ToolCallToNotification converts a coretypes.ToolCallDetail to an ACP v1 tool_call notification.
// Deprecated: use NewToolCallNotification for direct construction.
func ToolCallToNotification(detail coretypes.ToolCallDetail) ToolCallNotification {
	return NewToolCallNotification(detail.ID, detail.ToolName, detail.Args)
}

// ToolResultToNotification converts a coretypes.ToolCallResult to an ACP v1 tool_call_update notification.
// Deprecated: use NewToolCallUpdateNotification for direct construction.
func ToolResultToNotification(result coretypes.ToolCallResult) ToolCallUpdateNotification {
	status := ToolCallStatusCompleted
	if _, hasErr := result.Outputs["error"]; hasErr {
		status = ToolCallStatusError
	}
	return NewToolCallUpdateNotification(result.ToolCallID, status, result.Outputs)
}

// ---- internal helpers ----

// mimeToMultiModalType maps a MIME type string to a MultiModalType.
// Handles common image, video, audio, and document types.
func mimeToMultiModalType(mimeType string) constants.MultiModalType {
	switch {
	case len(mimeType) >= 6 && mimeType[:6] == "image/":
		return constants.MultiModalTypeImage
	case len(mimeType) >= 6 && mimeType[:6] == "video/":
		return constants.MultiModalTypeVideo
	case len(mimeType) >= 6 && mimeType[:6] == "audio/":
		return constants.MultiModalTypeAudio
	case mimeType == "application/pdf":
		return constants.MultiModalTypePDF
	case mimeType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" ||
		mimeType == "application/msword":
		return constants.MultiModalTypeWord
	case mimeType == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" ||
		mimeType == "application/vnd.ms-excel":
		return constants.MultiModalTypeExcel
	default:
		return constants.MultiModalTypeUnknown
	}
}

// multiModalToMime maps a MultiModalType to its default MIME type.
func multiModalToMime(mmType constants.MultiModalType) string {
	switch mmType {
	case constants.MultiModalTypeImage:
		return "image/png"
	case constants.MultiModalTypeVideo:
		return "video/mp4"
	case constants.MultiModalTypeAudio:
		return "audio/mpeg"
	case constants.MultiModalTypePDF:
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
