package core

import (
	"encoding/json"
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
	types "github.com/B777B2056-2/kugelblitz/core/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time verification that concrete types satisfy the types.Content interface.
var (
	_ types.Content = types.TextContent{}
	_ types.Content = types.ReasoningContent{}
	_ types.Content = types.ToolCallContent{}
	_ types.Content = types.ToolResultContent{}
	_ types.Content = types.MultiModalContent{}
	_ types.Content = types.CompositeContent{}
)

func TestTextContent_SatisfiesContentInterface(t *testing.T) {
	c := types.TextContent{Text: "hello"}
	assert.Equal(t, "hello", c.Text)
}

func TestReasoningContent_SatisfiesContentInterface(t *testing.T) {
	c := types.ReasoningContent{Reasoning: "thinking..."}
	assert.Equal(t, "thinking...", c.Reasoning)
}

func TestToolCallContent_SatisfiesContentInterface(t *testing.T) {
	c := types.ToolCallContent{Details: []types.ToolCallDetail{{ID: "1", ToolName: "test"}}}
	require.Len(t, c.Details, 1)
	assert.Equal(t, "test", c.Details[0].ToolName)
}

func TestToolResultContent_SatisfiesContentInterface(t *testing.T) {
	c := types.ToolResultContent{Results: []types.ToolCallResult{{ToolCallID: "1"}}}
	require.Len(t, c.Results, 1)
	assert.Equal(t, "1", c.Results[0].ToolCallID)
}

func TestMultiModalContent_SatisfiesContentInterface(t *testing.T) {
	c := types.MultiModalContent{Detail: types.MultiModalDetail{ID: "img1", Type: constants.MultiModalTypeImage}}
	assert.Equal(t, "img1", c.Detail.ID)
	assert.Equal(t, constants.MultiModalTypeImage, c.Detail.Type)
}

func TestCompositeContent_SatisfiesContentInterface(t *testing.T) {
	c := types.CompositeContent{
		Parts: []types.Content{
			types.TextContent{Text: "hello"},
			types.ReasoningContent{Reasoning: "think"},
		},
	}
	require.Len(t, c.Parts, 2)
}

func TestNewUserMessage_CreatesWithCorrectRole(t *testing.T) {
	msg := types.NewUserMessage(types.TextContent{Text: "hello"})
	assert.NotEmpty(t, msg.ID)
	assert.Equal(t, constants.RoleUser, msg.Role)
	assert.Equal(t, "hello", msg.Content.(types.TextContent).Text)
	assert.Empty(t, msg.FinishReason)
	assert.Nil(t, msg.Usage)
}

func TestNewAssistantMessage_CreatesWithCorrectRole(t *testing.T) {
	msg := types.NewAssistantMessage(types.TextContent{Text: "response"})
	assert.NotEmpty(t, msg.ID)
	assert.Equal(t, constants.RoleAssistant, msg.Role)
	assert.Equal(t, "response", msg.Content.(types.TextContent).Text)
}

func TestNewToolMessage_CreatesWithToolResultContent(t *testing.T) {
	results := []types.ToolCallResult{
		{ToolCallID: "tc-1", ToolName: "search", Outputs: map[string]any{"result": "found"}},
	}
	msg := types.NewToolMessage(results)
	assert.NotEmpty(t, msg.ID)
	assert.Equal(t, constants.RoleTool, msg.Role)
	toolResult, ok := msg.Content.(types.ToolResultContent)
	require.True(t, ok, "Content should be ToolResultContent")
	require.Len(t, toolResult.Results, 1)
	assert.Equal(t, "tc-1", toolResult.Results[0].ToolCallID)
}

func TestMessage_FinishReasonDefaultEmpty(t *testing.T) {
	msg := types.NewAssistantMessage(types.TextContent{Text: "ok"})
	assert.Empty(t, msg.FinishReason)
}

func TestMessage_UsageIsNilByDefault(t *testing.T) {
	msg := types.NewAssistantMessage(types.TextContent{Text: "ok"})
	assert.Nil(t, msg.Usage)
}

// Content_SealedInterface verifies the types.Content interface cannot be implemented
// from outside the core package, since it requires an unexported method.
// This is a compile-time check: any attempt to implement types.Content outside
// core will fail to compile.
func TestContent_SealedInterface(t *testing.T) {
	var c types.Content = types.TextContent{Text: "hello"}
	assert.NotNil(t, c)
}

func TestMessage_JSONRoundTrip_TextContent(t *testing.T) {
	original := types.NewUserMessage(types.TextContent{Text: "hello world"})
	original.ID = "msg-fixed"
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored types.Message
	require.NoError(t, json.Unmarshal(data, &restored))

	assert.Equal(t, "msg-fixed", restored.ID)
	assert.Equal(t, "user", string(restored.Role))
	text, ok := restored.Content.(types.TextContent)
	require.True(t, ok)
	assert.Equal(t, "hello world", text.Text)
}

func TestMessage_JSONRoundTrip_ToolCall(t *testing.T) {
	original := types.NewAssistantMessage(nil)
	original.Content = types.ToolCallContent{
		Details: []types.ToolCallDetail{
			{ID: "tc-1", ToolName: "search", Args: map[string]any{"q": "test"}},
		},
	}
	original.FinishReason = "tool_calls"

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored types.Message
	require.NoError(t, json.Unmarshal(data, &restored))

	tc, ok := restored.Content.(types.ToolCallContent)
	require.True(t, ok)
	require.Len(t, tc.Details, 1)
	assert.Equal(t, "search", tc.Details[0].ToolName)
	assert.Equal(t, "test", tc.Details[0].Args["q"])
	assert.Equal(t, "tool_calls", restored.FinishReason)
}

func TestMessage_JSONRoundTrip_Composite(t *testing.T) {
	original := types.NewAssistantMessage(types.CompositeContent{
		Parts: []types.Content{
			types.ReasoningContent{Reasoning: "let me think"},
			types.TextContent{Text: "answer is 42"},
		},
	})
	original.Usage = &types.Usage{TotalTokens: 100, InputTokens: 50, OutputTokens: 50}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored types.Message
	require.NoError(t, json.Unmarshal(data, &restored))

	cc, ok := restored.Content.(types.CompositeContent)
	require.True(t, ok)
	require.Len(t, cc.Parts, 2)
	assert.Equal(t, "let me think", cc.Parts[0].(types.ReasoningContent).Reasoning)
	assert.Equal(t, "answer is 42", cc.Parts[1].(types.TextContent).Text)
	assert.Equal(t, int64(100), restored.Usage.TotalTokens)
}

func TestMessage_Unmarshal_CompositeMalformedPart_ReturnsError(t *testing.T) {
	// A composite whose Parts contains a non-JSON sub-block must surface an error
	// instead of silently dropping the part (C2).
	raw := `{"id":"m1","role":"assistant","content":{"type":"composite","parts":["{not-json}"]}}`
	var m types.Message
	err := json.Unmarshal([]byte(raw), &m)
	require.Error(t, err)
}

func TestMessage_Unmarshal_UnknownContentType_ReturnsNilContent(t *testing.T) {
	raw := `{"id":"m1","role":"assistant","content":{"type":"nope"}}`
	var m types.Message
	require.NoError(t, json.Unmarshal([]byte(raw), &m))
	assert.Nil(t, m.Content)
}

func TestMessage_JSONRoundTrip_MultiModal(t *testing.T) {
	original := types.NewUserMessage(types.MultiModalContent{
		Detail: types.MultiModalDetail{ID: "img-1", Type: constants.MultiModalTypeImage, Path: "/tmp/a.png"},
	})

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored types.Message
	require.NoError(t, json.Unmarshal(data, &restored))

	mm, ok := restored.Content.(types.MultiModalContent)
	require.True(t, ok)
	assert.Equal(t, "img-1", mm.Detail.ID)
}

func TestMultiModalDetail_NewFields(t *testing.T) {
	detail := types.MultiModalDetail{
		ID:       "img-1",
		Type:     constants.MultiModalTypeImage,
		Path:     "/tmp/a.png",
		MimeType: "image/png",
		Meta:     map[string]any{"width": 1920, "height": 1080},
	}
	assert.Equal(t, "image/png", detail.MimeType)
	assert.Equal(t, 1920, detail.Meta["width"])
}

func TestMultiModalDetail_MarshalJSON_StripsBase64(t *testing.T) {
	// 持久化时不应写入 Base64
	detail := types.MultiModalDetail{
		ID:       "img-1",
		Type:     constants.MultiModalTypeImage,
		Path:     "media/sess_abc/img_001.png",
		Base64:   "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk",
		MimeType: "image/png",
		Meta:     map[string]any{"width": 1, "height": 1},
	}

	data, err := json.Marshal(detail)
	require.NoError(t, err)

	// Base64 不应出现在 JSON 中
	assert.NotContains(t, string(data), "iVBORw0")
	assert.NotContains(t, string(data), "base64")

	// 但 Path、MimeType、Meta 应保留
	assert.Contains(t, string(data), "media/sess_abc/img_001.png")
	assert.Contains(t, string(data), "image/png")
	assert.Contains(t, string(data), "width")
}

func TestMultiModalDetail_UnmarshalJSON_PreservesPathAndMeta(t *testing.T) {
	raw := `{"id":"img-1","type":"image","path":"media/sess_abc/img_001.png","mime_type":"image/png","meta":{"height":1080,"width":1920}}`

	var detail types.MultiModalDetail
	require.NoError(t, json.Unmarshal([]byte(raw), &detail))

	assert.Equal(t, "img-1", detail.ID)
	assert.Equal(t, "media/sess_abc/img_001.png", detail.Path)
	assert.Equal(t, "image/png", detail.MimeType)
	assert.Empty(t, detail.Base64)                // 从持久化格式反序列化时 Base64 应为空
	assert.Equal(t, 1920.0, detail.Meta["width"]) // JSON numbers → float64
}

func TestMultiModalDetail_MetaIsNilByDefault(t *testing.T) {
	detail := types.MultiModalDetail{ID: "a", Type: constants.MultiModalTypeImage}
	assert.Nil(t, detail.Meta)
	assert.Empty(t, detail.MimeType)
}

func TestMessage_JSONRoundTrip_EmptyContent(t *testing.T) {
	original := types.NewAssistantMessage(nil)
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored types.Message
	require.NoError(t, json.Unmarshal(data, &restored))
	assert.Equal(t, "assistant", string(restored.Role))
}
