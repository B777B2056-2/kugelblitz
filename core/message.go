package core

import (
	"kugelblitz/constants"
	"kugelblitz/utils"
)

// Usage tracks token usage for a provider response.
type Usage struct {
	TotalTokens     int64
	InputTokens     int64
	CachedTokens    int64
	ReasoningTokens int64
	OutputTokens    int64
}

// MultiModalDetail describes non-text media content.
type MultiModalDetail struct {
	ID     string
	Type   constants.MultiModalType
	Path   string
	Base64 string
}

// ToolCallDetail represents a single tool call requested by the LLM.
type ToolCallDetail struct {
	ID       string
	ToolName string
	Args     map[string]any
}

// ToolCallResult represents the result of executing a tool call.
type ToolCallResult struct {
	ToolCallID string
	ToolName   string
	Outputs    map[string]any
}

// Content is the sealed interface for all message content types.
// It can only be implemented by types in the core package.
type Content interface {
	contentType()
}

// TextContent represents simple text content in a message.
type TextContent struct {
	Text string
}

func (TextContent) contentType() {}

// ReasoningContent represents thinking/reasoning text from the LLM.
type ReasoningContent struct {
	Reasoning string
}

func (ReasoningContent) contentType() {}

// ToolCallContent represents one or more tool call requests from the LLM.
type ToolCallContent struct {
	Details []ToolCallDetail
}

func (ToolCallContent) contentType() {}

// ToolResultContent represents the results of executed tool calls.
type ToolResultContent struct {
	Results []ToolCallResult
}

func (ToolResultContent) contentType() {}

// MultiModalContent represents non-text media (images, audio, video, files).
type MultiModalContent struct {
	Detail MultiModalDetail
}

func (MultiModalContent) contentType() {}

// CompositeContent holds multiple ordered content parts.
// Used when a single message contains mixed content types (e.g., text + tool calls).
type CompositeContent struct {
	Parts []Content
}

func (CompositeContent) contentType() {}

// Message represents a single message in a conversation.
type Message struct {
	ID           string
	ParentID     string
	Role         constants.RoleType
	Content      Content
	FinishReason string // set only on assistant responses
	Usage        *Usage // set only on assistant responses
}

// NewUserMessage creates a new user message with the given content.
func NewUserMessage(parentID string, content Content) Message {
	return Message{
		ID:       utils.GenerateMessageID(),
		ParentID: parentID,
		Role:     constants.RoleUser,
		Content:  content,
	}
}

// NewAssistantMessage creates a new assistant message with the given content.
func NewAssistantMessage(parentID string, content Content) Message {
	return Message{
		ID:       utils.GenerateMessageID(),
		ParentID: parentID,
		Role:     constants.RoleAssistant,
		Content:  content,
	}
}

// NewToolMessage creates a tool message containing tool call results.
func NewToolMessage(parentID string, results []ToolCallResult) Message {
	return Message{
		ID:       utils.GenerateMessageID(),
		ParentID: parentID,
		Role:     constants.RoleTool,
		Content: ToolResultContent{
			Results: results,
		},
	}
}
