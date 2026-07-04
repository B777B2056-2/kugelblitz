package core

import (
	"encoding/json"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/utils"
)

// Usage tracks token usage for a provider response.
type Usage struct {
	TotalTokens     int64 `json:"total_tokens"`
	InputTokens     int64 `json:"input_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
}

// MultiModalDetail describes non-text media content.
type MultiModalDetail struct {
	ID     string                   `json:"id"`
	Type   constants.MultiModalType `json:"type"`
	Path   string                   `json:"path,omitempty"`
	Base64 string                   `json:"base64,omitempty"`
}

// ToolCallDetail represents a single tool call requested by the LLM.
type ToolCallDetail struct {
	ID       string         `json:"id"`
	ToolName string         `json:"tool_name"`
	Args     map[string]any `json:"args"`
}

// ToolCallResult represents the result of executing a tool call.
type ToolCallResult struct {
	ToolCallID string         `json:"tool_call_id"`
	ToolName   string         `json:"tool_name"`
	Outputs    map[string]any `json:"outputs"`
}

// Content is the sealed interface for all message content types.
type Content interface {
	contentType()
}

// TextContent represents simple text content in a message.
type TextContent struct {
	Text string `json:"text"`
}

func (TextContent) contentType() {}

// ReasoningContent represents thinking/reasoning text from the LLM.
type ReasoningContent struct {
	Reasoning string `json:"reasoning"`
}

func (ReasoningContent) contentType() {}

// ToolCallContent represents one or more tool call requests from the LLM.
type ToolCallContent struct {
	Details []ToolCallDetail `json:"details"`
}

func (ToolCallContent) contentType() {}

// ToolResultContent represents the results of executed tool calls.
type ToolResultContent struct {
	Results []ToolCallResult `json:"results"`
}

func (ToolResultContent) contentType() {}

// MultiModalContent represents non-text media (images, audio, video, files).
type MultiModalContent struct {
	Detail MultiModalDetail `json:"detail"`
}

func (MultiModalContent) contentType() {}

// CompositeContent holds multiple ordered content parts.
type CompositeContent struct {
	Parts []Content `json:"parts"`
}

func (CompositeContent) contentType() {}

// Message represents a single message in a conversation.
type Message struct {
	ID           string
	Role         constants.RoleType
	Content      Content
	FinishReason string `json:"finish_reason,omitempty"`
	Usage        *Usage `json:"usage,omitempty"`
}

// ---- Message JSON serialization ----

// contentWrapper is the JSON representation of a Content value.
// The Type field acts as a discriminator for reconstructing the concrete type.
type contentWrapper struct {
	Type      string            `json:"type"`
	Text      string            `json:"text,omitempty"`
	Reasoning string            `json:"reasoning,omitempty"`
	Details   []ToolCallDetail  `json:"details,omitempty"`
	Results   []ToolCallResult  `json:"results,omitempty"`
	Detail    *MultiModalDetail `json:"detail,omitempty"`
	Parts     []json.RawMessage `json:"parts,omitempty"`
}

// messageJSON is the full JSON representation of a Message.
type messageJSON struct {
	ID           string         `json:"id"`
	Role         string         `json:"role"`
	Content      contentWrapper `json:"content"`
	FinishReason string         `json:"finish_reason,omitempty"`
	Usage        *Usage         `json:"usage,omitempty"`
}

// MarshalJSON serializes a Message to JSON with full Content fidelity.
func (m Message) MarshalJSON() ([]byte, error) {
	mj := messageJSON{
		ID:           m.ID,
		Role:         string(m.Role),
		FinishReason: m.FinishReason,
		Usage:        m.Usage,
	}
	mj.Content = marshalContent(m.Content)
	return json.Marshal(mj)
}

// UnmarshalJSON deserializes a Message from JSON, reconstructing the Content type.
func (m *Message) UnmarshalJSON(data []byte) error {
	var mj messageJSON
	if err := json.Unmarshal(data, &mj); err != nil {
		return err
	}
	m.ID = mj.ID
	m.Role = constants.RoleType(mj.Role)
	m.FinishReason = mj.FinishReason
	m.Usage = mj.Usage
	m.Content = unmarshalContent(mj.Content)
	return nil
}

// marshalContent converts any Content to a type-discriminated JSON wrapper.
func marshalContent(c Content) contentWrapper {
	switch ct := c.(type) {
	case TextContent:
		return contentWrapper{Type: "text", Text: ct.Text}
	case ReasoningContent:
		return contentWrapper{Type: "reasoning", Reasoning: ct.Reasoning}
	case ToolCallContent:
		return contentWrapper{Type: "tool_call", Details: ct.Details}
	case ToolResultContent:
		return contentWrapper{Type: "tool_result", Results: ct.Results}
	case MultiModalContent:
		return contentWrapper{Type: "multi_modal", Detail: &ct.Detail}
	case CompositeContent:
		parts := make([]json.RawMessage, len(ct.Parts))
		for i, p := range ct.Parts {
			w := marshalContent(p)
			b, _ := json.Marshal(w)
			parts[i] = b
		}
		return contentWrapper{Type: "composite", Parts: parts}
	default:
		return contentWrapper{Type: "unknown"}
	}
}

// unmarshalContent reconstructs the concrete Content type from a wrapper.
func unmarshalContent(w contentWrapper) Content {
	switch w.Type {
	case "text":
		return TextContent{Text: w.Text}
	case "reasoning":
		return ReasoningContent{Reasoning: w.Reasoning}
	case "tool_call":
		return ToolCallContent{Details: w.Details}
	case "tool_result":
		return ToolResultContent{Results: w.Results}
	case "multi_modal":
		if w.Detail != nil {
			return MultiModalContent{Detail: *w.Detail}
		}
		return MultiModalContent{}
	case "composite":
		parts := make([]Content, len(w.Parts))
		for i, raw := range w.Parts {
			var cw contentWrapper
			if err := json.Unmarshal(raw, &cw); err == nil {
				parts[i] = unmarshalContent(cw)
			}
		}
		return CompositeContent{Parts: parts}
	default:
		return nil
	}
}

// ToolResults returns all ToolCallResults from the message content.
func (m Message) ToolResults() []ToolCallResult {
	switch ct := m.Content.(type) {
	case ToolResultContent:
		return ct.Results
	case CompositeContent:
		var results []ToolCallResult
		for _, part := range ct.Parts {
			if tr, ok := part.(ToolResultContent); ok {
				results = append(results, tr.Results...)
			}
		}
		return results
	}
	return nil
}

// ExtractToolResult extracts a typed value from the first tool result matching toolName.
func ExtractToolResult[T any](messages []Message, toolName, key string) (T, bool) {
	for _, msg := range messages {
		for _, r := range msg.ToolResults() {
			if r.ToolName == toolName {
				if v, ok := r.Outputs[key].(T); ok {
					return v, true
				}
			}
		}
	}
	var zero T
	return zero, false
}

// NewSystemMessage creates a new system message with the given content.
func NewSystemMessage(content Content) Message {
	return Message{
		ID:      utils.GenerateMessageID(),
		Role:    constants.RoleSystem,
		Content: content,
	}
}

// NewUserMessage creates a new user message with the given content.
func NewUserMessage(content Content) Message {
	return Message{
		ID:      utils.GenerateMessageID(),
		Role:    constants.RoleUser,
		Content: content,
	}
}

// NewAssistantMessage creates a new assistant message with the given content.
func NewAssistantMessage(content Content) Message {
	return Message{
		ID:      utils.GenerateMessageID(),
		Role:    constants.RoleAssistant,
		Content: content,
	}
}

// NewToolMessage creates a tool message containing tool call results.
func NewToolMessage(results []ToolCallResult) Message {
	return Message{
		ID:   utils.GenerateMessageID(),
		Role: constants.RoleTool,
		Content: ToolResultContent{
			Results: results,
		},
	}
}

// Ensure json.Marshal/Unmarshal work on the helper types at compile time.
var (
	_ json.Marshaler   = (*Message)(nil)
	_ json.Unmarshaler = (*Message)(nil)
)
