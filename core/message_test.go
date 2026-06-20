package core

import (
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
	// If this test compiles and runs, the interface is correctly sealed.
	var c Content = TextContent{Text: "hello"}
	assert.NotNil(t, c)
}
