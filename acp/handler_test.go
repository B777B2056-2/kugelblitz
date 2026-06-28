package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestHandler creates a Handler wired to a test transport and workspace.
func newTestHandler(t *testing.T) (*Handler, *testReadWriter, *SessionManager) {
	t.Helper()
	sm := NewSessionManager()
	rw := &testReadWriter{readBuf: new(bytes.Buffer), writeBuf: new(bytes.Buffer)}
	tr := NewTransport(rw)

	h := &Handler{
		transport: tr,
		sessions:  sm,
		agent:     newMockAgent(),
		provider:  &mockProvider{},
		serverInfo: ServerInfo{
			Name:    "kugelblitz",
			Version: "0.1.0",
		},
	}
	return h, rw, sm
}

func TestHandler_Dispatch_Initialize(t *testing.T) {
	h, rw, _ := newTestHandler(t)

	params := InitializeParams{
		ProtocolVersion: 1,
		ClientInfo:      ClientInfo{Name: "test-editor", Version: "1.0"},
		ClientCapabilities: ClientCapabilities{
			FS: true,
		},
	}
	paramsBytes, _ := json.Marshal(params)
	id := json.RawMessage([]byte(`"1"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "initialize",
		Params:  paramsBytes,
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"result"`)
	assert.Contains(t, output, `"protocolVersion"`)
}

func TestHandler_Dispatch_SessionNew(t *testing.T) {
	h, rw, _ := newTestHandler(t)

	params := SessionNewParams{Cwd: "/home/user/project"}
	paramsBytes, _ := json.Marshal(params)
	id := json.RawMessage([]byte(`"2"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/new",
		Params:  paramsBytes,
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"sessionId"`)
}

func TestHandler_Dispatch_SessionPrompt(t *testing.T) {
	h, rw, sm := newTestHandler(t)

	// Set up mock agent to return a simple response
	mockAgent := newMockAgent()
	mockAgent.executeFn = func(ctx context.Context, sys core.Message, userMsgs []core.Message) ([]core.Message, error) {
		return []core.Message{
			core.NewAssistantMessage("root", core.TextContent{Text: "Hello from agent!"}),
		}, nil
	}

	// Create session with mock agent
	session := sm.Create("/proj", mockAgent)

	params := SessionPromptParams{
		SessionID: session.ID,
		Prompt:    []ContentBlock{{Type: ContentBlockTypeText, Text: "Hi"}},
	}
	paramsBytes, _ := json.Marshal(params)
	id := json.RawMessage([]byte(`"3"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/prompt",
		Params:  paramsBytes,
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)

	output := rw.writeBuf.String()
	// Should contain the stop reason response
	assert.Contains(t, output, `"stopReason"`)
}

func TestHandler_Dispatch_SessionCancel(t *testing.T) {
	h, _, sm := newTestHandler(t)

	session := sm.Create("/proj", newMockAgent())

	params := SessionCancelParams{SessionID: session.ID}
	paramsBytes, _ := json.Marshal(params)
	id := json.RawMessage([]byte(`"4"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/cancel",
		Params:  paramsBytes,
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)
}

func TestHandler_Dispatch_SessionList(t *testing.T) {
	h, rw, sm := newTestHandler(t)

	sm.Create("/proj1", newMockAgent())
	sm.Create("/proj2", newMockAgent())

	id := json.RawMessage([]byte(`"5"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/list",
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"sessionId"`)
}

func TestHandler_Dispatch_SessionDelete(t *testing.T) {
	h, _, sm := newTestHandler(t)

	session := sm.Create("/proj", newMockAgent())

	params := SessionDeleteParams{SessionID: session.ID}
	paramsBytes, _ := json.Marshal(params)
	id := json.RawMessage([]byte(`"6"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/delete",
		Params:  paramsBytes,
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)

	// Verify deleted
	_, err = sm.Load(session.ID)
	assert.Error(t, err)
}

func TestHandler_Dispatch_MethodNotFound(t *testing.T) {
	h, rw, _ := newTestHandler(t)

	id := json.RawMessage([]byte(`"99"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "unknown/method",
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"error"`)
	assert.Contains(t, output, "Method not found")
}

func TestHandler_Dispatch_InvalidParams(t *testing.T) {
	h, rw, _ := newTestHandler(t)

	id := json.RawMessage([]byte(`"100"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "initialize",
		Params:  json.RawMessage([]byte(`{"protocolVersion":"not_a_number"}`)),
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"error"`)
}

func TestHandler_Prompt_StreamingNotifications(t *testing.T) {
	h, rw, sm := newTestHandler(t)

	mockAgent := newMockAgent()
	// Simulate a tool call flow — the agent returns tool calls then text
	callCount := 0
	mockAgent.executeFn = func(ctx context.Context, sys core.Message, userMsgs []core.Message) ([]core.Message, error) {
		callCount++
		if callCount == 1 {
			return []core.Message{
				{
					ID:   "m1",
					Role: "assistant",
					Content: core.ToolCallContent{
						Details: []core.ToolCallDetail{
							{ID: "tc_1", ToolName: "read_file", Args: map[string]any{"path": "/tmp/test.txt"}},
						},
					},
				},
			}, nil
		}
		return []core.Message{
			core.NewAssistantMessage("m1", core.TextContent{Text: "File contents: hello"}),
		}, nil
	}

	session := sm.Create("/proj", mockAgent)

	params := SessionPromptParams{
		SessionID: session.ID,
		Prompt:    []ContentBlock{{Type: ContentBlockTypeText, Text: "Read file"}},
	}
	paramsBytes, _ := json.Marshal(params)
	id := json.RawMessage([]byte(`"7"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/prompt",
		Params:  paramsBytes,
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)

	output := rw.writeBuf.String()
	// The handler should return a proper stopReason
	assert.Contains(t, output, `"stopReason"`)
	assert.Contains(t, output, StopReasonEndTurn)
}

// mockProvider implements core.ILMProvider for testing.
type mockProvider struct {
	generateFn func(ctx context.Context, params core.GenerateParams) (*core.Message, error)
}

func (m *mockProvider) Generate(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
	if m.generateFn != nil {
		return m.generateFn(ctx, params)
	}
	return &core.Message{
		ID:      "resp",
		Role:    "assistant",
		Content: core.TextContent{Text: "mock response"},
	}, nil
}

var _ core.ILMProvider = (*mockProvider)(nil)

// ---- Handler error paths ----

func TestHandler_Dispatch_SessionPrompt_NotFound(t *testing.T) {
	h, rw, _ := newTestHandler(t)

	params := SessionPromptParams{
		SessionID: "nonexistent",
		Prompt:    []ContentBlock{{Type: ContentBlockTypeText, Text: "Hi"}},
	}
	paramsBytes, _ := json.Marshal(params)
	id := json.RawMessage([]byte(`"err_1"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/prompt",
		Params:  paramsBytes,
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"error"`)
	assert.Contains(t, output, "session not found")
}

func TestHandler_Dispatch_SessionCancel_NotFound(t *testing.T) {
	h, rw, _ := newTestHandler(t)

	params := SessionCancelParams{SessionID: "nonexistent"}
	paramsBytes, _ := json.Marshal(params)
	id := json.RawMessage([]byte(`"err_2"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/cancel",
		Params:  paramsBytes,
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"error"`)
}

func TestHandler_Dispatch_SessionLoad(t *testing.T) {
	h, rw, sm := newTestHandler(t)

	// Create a session with messages so Load can replay them
	s := sm.Create("/proj", newMockAgent())
	sm.AppendMessage(s.ID, core.NewUserMessage("root", core.TextContent{Text: "hello"}))

	params := SessionLoadParams{SessionID: s.ID}
	paramsBytes, _ := json.Marshal(params)
	id := json.RawMessage([]byte(`"load_1"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/load",
		Params:  paramsBytes,
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)

	output := rw.writeBuf.String()
	// Response should contain the sessionId and the replayed update notifications
	assert.Contains(t, output, `"session/update"`)
	assert.Contains(t, output, s.ID)
}

func TestHandler_Dispatch_SessionLoad_NotFound(t *testing.T) {
	h, rw, _ := newTestHandler(t)

	params := SessionLoadParams{SessionID: "nonexistent"}
	paramsBytes, _ := json.Marshal(params)
	id := json.RawMessage([]byte(`"load_err"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/load",
		Params:  paramsBytes,
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"error"`)
}

func TestHandler_Dispatch_SessionDelete_NotFound(t *testing.T) {
	h, rw, _ := newTestHandler(t)

	params := SessionDeleteParams{SessionID: "nonexistent"}
	paramsBytes, _ := json.Marshal(params)
	id := json.RawMessage([]byte(`"del_err"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/delete",
		Params:  paramsBytes,
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"error"`)
}

func TestHandler_Dispatch_Notification(t *testing.T) {
	h, _, _ := newTestHandler(t)

	// A notification has no id — should be handled without response
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  "some/notification",
		Params:  json.RawMessage([]byte(`{}`)),
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)
}

func TestHandler_Dispatch_NonRequest(t *testing.T) {
	h, _, _ := newTestHandler(t)

	// A message with a result but no method (response-like, not expected from client)
	// Should be silently ignored
	result := json.RawMessage([]byte(`"ok"`))
	id := json.RawMessage([]byte(`"999"`))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Result:  result,
	}

	err := h.Dispatch(context.Background(), msg)
	require.NoError(t, err)
}

// ---- acpEventHandler tests ----

func TestACPEventHandler_OnReplyChunk(t *testing.T) {
	rw := &testReadWriter{readBuf: new(bytes.Buffer), writeBuf: new(bytes.Buffer)}
	tr := NewTransport(rw)

	handler := &acpEventHandler{transport: tr, sessionID: "s1"}
	handler.OnReplyChunk("hello world")

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"session/update"`)
	assert.Contains(t, output, `"s1"`)
	assert.Contains(t, output, `"sessionUpdate":"agent_message_chunk"`)
	assert.Contains(t, output, "hello world")
}

func TestACPEventHandler_OnFunctionCall(t *testing.T) {
	rw := &testReadWriter{readBuf: new(bytes.Buffer), writeBuf: new(bytes.Buffer)}
	tr := NewTransport(rw)

	handler := &acpEventHandler{transport: tr, sessionID: "s2"}
	handler.OnFunctionCall(core.ToolCallDetail{
		ID:       "tc_1",
		ToolName: "read_file",
		Args:     map[string]any{"path": "/tmp/test.txt"},
	})

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"sessionUpdate":"tool_call"`)
	assert.Contains(t, output, `"read_file"`)
	assert.Contains(t, output, `"tc_1"`)
}

func TestACPEventHandler_OnError(t *testing.T) {
	rw := &testReadWriter{readBuf: new(bytes.Buffer), writeBuf: new(bytes.Buffer)}
	tr := NewTransport(rw)

	handler := &acpEventHandler{transport: tr, sessionID: "s3"}
	handler.OnError(assert.AnError)

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"session/update"`)
	assert.Contains(t, output, "assert.AnError")
}

func TestACPEventHandler_OnThinkingChunk(t *testing.T) {
	rw := &testReadWriter{readBuf: new(bytes.Buffer), writeBuf: new(bytes.Buffer)}
	tr := NewTransport(rw)

	handler := &acpEventHandler{transport: tr, sessionID: "s4"}
	// OnThinkingChunk should not send anything (internal only)
	handler.OnThinkingChunk("internal thought")

	output := rw.writeBuf.String()
	assert.Empty(t, output)
}

func TestACPEventHandler_OnFinished(t *testing.T) {
	rw := &testReadWriter{readBuf: new(bytes.Buffer), writeBuf: new(bytes.Buffer)}
	tr := NewTransport(rw)

	handler := &acpEventHandler{transport: tr, sessionID: "s5"}
	handler.OnFinished("stop")

	output := rw.writeBuf.String()
	// OnFinished does not send a notification (handled by prompt response)
	assert.Empty(t, output)
}

func TestACPEventHandler_OnUsageUpdated(t *testing.T) {
	rw := &testReadWriter{readBuf: new(bytes.Buffer), writeBuf: new(bytes.Buffer)}
	tr := NewTransport(rw)

	handler := &acpEventHandler{transport: tr, sessionID: "s6"}
	handler.OnUsageUpdated(core.Usage{InputTokens: 100, OutputTokens: 50})

	output := rw.writeBuf.String()
	// OnUsageUpdated doesn't send a notification currently
	assert.Empty(t, output)
}

// ---- Shared test helpers ----

type mockAgent struct {
	executeFn  func(ctx context.Context, systemMsg core.Message, userMsgs []core.Message) ([]core.Message, error)
	interruptFn func(ctx context.Context) error
}

func (m *mockAgent) RegisterEventHooks(hooks core.AgentEventHooks) {}
func (m *mockAgent) Execute(ctx context.Context, systemMsg core.Message, userMsgs []core.Message) ([]core.Message, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, systemMsg, userMsgs)
	}
	return nil, nil
}
func (m *mockAgent) Interrupt(ctx context.Context) error {
	if m.interruptFn != nil {
		return m.interruptFn(ctx)
	}
	return nil
}
func (m *mockAgent) ResumeWithHumanResponse(ctx context.Context, response string) error { return nil }
func (m *mockAgent) HumanLoopWaiting() bool                                            { return false }

var _ core.IAgent = (*mockAgent)(nil)

func newMockAgent() *mockAgent { return &mockAgent{} }
