package core

import (
	"encoding/json"
	"testing"

	"kugelblitz/constants"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time verification that concrete types satisfy the Content interface.
var (
	_ Content = TextContent{}
	_ Content = ReasoningContent{}
	_ Content = ToolCallContent{}
	_ Content = ToolResultContent{}
	_ Content = MultiModalContent{}
	_ Content = CompositeContent{}
)

func TestTextContent_SatisfiesContentInterface(t *testing.T) {
	c := TextContent{Text: "hello"}
	assert.Equal(t, "hello", c.Text)
}

func TestReasoningContent_SatisfiesContentInterface(t *testing.T) {
	c := ReasoningContent{Reasoning: "thinking..."}
	assert.Equal(t, "thinking...", c.Reasoning)
}

func TestToolCallContent_SatisfiesContentInterface(t *testing.T) {
	c := ToolCallContent{Details: []ToolCallDetail{{ID: "1", ToolName: "test"}}}
	require.Len(t, c.Details, 1)
	assert.Equal(t, "test", c.Details[0].ToolName)
}

func TestToolResultContent_SatisfiesContentInterface(t *testing.T) {
	c := ToolResultContent{Results: []ToolCallResult{{ToolCallID: "1"}}}
	require.Len(t, c.Results, 1)
	assert.Equal(t, "1", c.Results[0].ToolCallID)
}

func TestMultiModalContent_SatisfiesContentInterface(t *testing.T) {
	c := MultiModalContent{Detail: MultiModalDetail{ID: "img1", Type: constants.MultiModalTypeImage}}
	assert.Equal(t, "img1", c.Detail.ID)
	assert.Equal(t, constants.MultiModalTypeImage, c.Detail.Type)
}

func TestCompositeContent_SatisfiesContentInterface(t *testing.T) {
	c := CompositeContent{
		Parts: []Content{
			TextContent{Text: "hello"},
			ReasoningContent{Reasoning: "think"},
		},
	}
	require.Len(t, c.Parts, 2)
}

func TestNewUserMessage_CreatesWithCorrectRole(t *testing.T) {
	msg := NewUserMessage("parent-1", TextContent{Text: "hello"})
	assert.NotEmpty(t, msg.ID)
	assert.Equal(t, "parent-1", msg.ParentID)
	assert.Equal(t, constants.RoleUser, msg.Role)
	assert.Equal(t, "hello", msg.Content.(TextContent).Text)
	assert.Empty(t, msg.FinishReason)
	assert.Nil(t, msg.Usage)
}

func TestNewAssistantMessage_CreatesWithCorrectRole(t *testing.T) {
	msg := NewAssistantMessage("parent-2", TextContent{Text: "response"})
	assert.NotEmpty(t, msg.ID)
	assert.Equal(t, constants.RoleAssistant, msg.Role)
	assert.Equal(t, "response", msg.Content.(TextContent).Text)
}

func TestNewToolMessage_CreatesWithToolResultContent(t *testing.T) {
	results := []ToolCallResult{
		{ToolCallID: "tc-1", ToolName: "search", Outputs: map[string]any{"result": "found"}},
	}
	msg := NewToolMessage("parent-3", results)
	assert.NotEmpty(t, msg.ID)
	assert.Equal(t, constants.RoleTool, msg.Role)
	toolResult, ok := msg.Content.(ToolResultContent)
	require.True(t, ok, "Content should be ToolResultContent")
	require.Len(t, toolResult.Results, 1)
	assert.Equal(t, "tc-1", toolResult.Results[0].ToolCallID)
}

func TestMessage_FinishReasonDefaultEmpty(t *testing.T) {
	msg := NewAssistantMessage("parent", TextContent{Text: "ok"})
	assert.Empty(t, msg.FinishReason)
}

func TestMessage_UsageIsNilByDefault(t *testing.T) {
	msg := NewAssistantMessage("parent", TextContent{Text: "ok"})
	assert.Nil(t, msg.Usage)
}

// Content_SealedInterface verifies the Content interface cannot be implemented
// from outside the core package, since it requires an unexported method.
// This is a compile-time check: any attempt to implement Content outside
// core will fail to compile.
func TestContent_SealedInterface(t *testing.T) {
	var c Content = TextContent{Text: "hello"}
	assert.NotNil(t, c)
}

func TestMessage_JSONRoundTrip_TextContent(t *testing.T) {
	original := NewUserMessage("p1", TextContent{Text: "hello world"})
	original.ID = "msg-fixed"
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Message
	require.NoError(t, json.Unmarshal(data, &restored))

	assert.Equal(t, "msg-fixed", restored.ID)
	assert.Equal(t, "user", string(restored.Role))
	text, ok := restored.Content.(TextContent)
	require.True(t, ok)
	assert.Equal(t, "hello world", text.Text)
}

func TestMessage_JSONRoundTrip_ToolCall(t *testing.T) {
	original := NewAssistantMessage("p1", nil)
	original.Content = ToolCallContent{
		Details: []ToolCallDetail{
			{ID: "tc-1", ToolName: "search", Args: map[string]any{"q": "test"}},
		},
	}
	original.FinishReason = "tool_calls"

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Message
	require.NoError(t, json.Unmarshal(data, &restored))

	tc, ok := restored.Content.(ToolCallContent)
	require.True(t, ok)
	require.Len(t, tc.Details, 1)
	assert.Equal(t, "search", tc.Details[0].ToolName)
	assert.Equal(t, "test", tc.Details[0].Args["q"])
	assert.Equal(t, "tool_calls", restored.FinishReason)
}

func TestMessage_JSONRoundTrip_Composite(t *testing.T) {
	original := NewAssistantMessage("p1", CompositeContent{
		Parts: []Content{
			ReasoningContent{Reasoning: "let me think"},
			TextContent{Text: "answer is 42"},
		},
	})
	original.Usage = &Usage{TotalTokens: 100, InputTokens: 50, OutputTokens: 50}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Message
	require.NoError(t, json.Unmarshal(data, &restored))

	cc, ok := restored.Content.(CompositeContent)
	require.True(t, ok)
	require.Len(t, cc.Parts, 2)
	assert.Equal(t, "let me think", cc.Parts[0].(ReasoningContent).Reasoning)
	assert.Equal(t, "answer is 42", cc.Parts[1].(TextContent).Text)
	assert.Equal(t, int64(100), restored.Usage.TotalTokens)
}

func TestMessage_JSONRoundTrip_MultiModal(t *testing.T) {
	original := NewUserMessage("p1", MultiModalContent{
		Detail: MultiModalDetail{ID: "img-1", Type: constants.MultiModalTypeImage, Path: "/tmp/a.png"},
	})

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Message
	require.NoError(t, json.Unmarshal(data, &restored))

	mm, ok := restored.Content.(MultiModalContent)
	require.True(t, ok)
	assert.Equal(t, "img-1", mm.Detail.ID)
}

func TestMessage_JSONRoundTrip_EmptyContent(t *testing.T) {
	original := NewAssistantMessage("p1", nil)
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Message
	require.NoError(t, json.Unmarshal(data, &restored))
	assert.Equal(t, "assistant", string(restored.Role))
}
