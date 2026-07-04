package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- Transport ----

func TestTransport_ReadMessage(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}` + "\n"
	rw := &testReadWriter{readBuf: bytes.NewBufferString(input), writeBuf: new(bytes.Buffer)}
	tr := NewTransport(rw)

	msg, err := tr.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "2.0", msg.JSONRPC)
	assert.Equal(t, "initialize", msg.Method)
	assert.True(t, msg.IsRequest())
	assert.False(t, msg.IsNotification())
	assert.False(t, msg.IsResponse())
}

func TestTransport_ReadMessage_Notification(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"abc"}}` + "\n"
	rw := &testReadWriter{readBuf: bytes.NewBufferString(input), writeBuf: new(bytes.Buffer)}
	tr := NewTransport(rw)

	msg, err := tr.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "session/update", msg.Method)
	assert.True(t, msg.IsNotification())
	assert.False(t, msg.IsRequest())
}

func TestTransport_WriteMessage(t *testing.T) {
	rw := &testReadWriter{readBuf: new(bytes.Buffer), writeBuf: new(bytes.Buffer)}
	tr := NewTransport(rw)

	id := json.RawMessage([]byte("1"))
	resp, err := NewResponse(id, map[string]any{"ok": true})
	require.NoError(t, err)

	err = tr.WriteMessage(resp)
	require.NoError(t, err)

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"jsonrpc":"2.0"`)
	assert.Contains(t, output, `"result":`)
	assert.Contains(t, output, `"ok":true`)
}

func TestTransport_SendNotification(t *testing.T) {
	rw := &testReadWriter{readBuf: new(bytes.Buffer), writeBuf: new(bytes.Buffer)}
	tr := NewTransport(rw)

	err := tr.SendNotification("session/update", map[string]any{
		"sessionId": "s1",
		"update":    map[string]string{"type": "agent_message_chunk", "text": "hello"},
	})
	require.NoError(t, err)

	output := rw.writeBuf.String()
	assert.Contains(t, output, `"method":"session/update"`)
	assert.Contains(t, output, `"sessionId":"s1"`)
	assert.Contains(t, output, `"hello"`)
	// Notification should have no id
	assert.NotContains(t, output, `"id"`)
}

// ---- JSONRPCMessage helpers ----

func TestIsRequest(t *testing.T) {
	id := json.RawMessage([]byte("1"))
	msg := &JSONRPCMessage{JSONRPC: "2.0", ID: &id, Method: "initialize"}
	assert.True(t, msg.IsRequest())
	assert.False(t, msg.IsNotification())
	assert.False(t, msg.IsResponse())
}

func TestIsNotification(t *testing.T) {
	msg := &JSONRPCMessage{JSONRPC: "2.0", Method: "session/update"}
	assert.True(t, msg.IsNotification())
	assert.False(t, msg.IsRequest())
	assert.False(t, msg.IsResponse())
}

func TestIsResponse(t *testing.T) {
	id := json.RawMessage([]byte("1"))
	result := json.RawMessage([]byte(`"ok"`))
	msg := &JSONRPCMessage{JSONRPC: "2.0", ID: &id, Result: result}
	assert.True(t, msg.IsResponse())
	assert.False(t, msg.IsRequest())
	assert.False(t, msg.IsNotification())
}

func TestNewResponse(t *testing.T) {
	id := json.RawMessage([]byte("1"))
	resp, err := NewResponse(id, map[string]string{"status": "ok"})
	require.NoError(t, err)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.NotNil(t, resp.ID)
	assert.NotNil(t, resp.Result)
	assert.Contains(t, string(resp.Result), "ok")
	assert.Empty(t, resp.Method)
}

func TestNewErrorResponse(t *testing.T) {
	id := json.RawMessage([]byte("1"))
	resp := NewErrorResponse(id, -32601, "Method not found", "no such method")
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
	assert.Equal(t, "Method not found", resp.Error.Message)
	assert.Empty(t, resp.Method)
}

func TestNewNotification(t *testing.T) {
	msg, err := NewNotification("session/update", map[string]string{"key": "val"})
	require.NoError(t, err)
	assert.Equal(t, "2.0", msg.JSONRPC)
	assert.Equal(t, "session/update", msg.Method)
	assert.Nil(t, msg.ID)
	assert.Contains(t, string(msg.Params), "val")
}

func TestNewResponse_MarshalError(t *testing.T) {
	// Passing a channel (non-marshalable) should return an error
	id := json.RawMessage([]byte("1"))
	_, err := NewResponse(id, make(chan int))
	assert.Error(t, err)
}

func TestNewNotification_MarshalError(t *testing.T) {
	_, err := NewNotification("test", make(chan int))
	assert.Error(t, err)
}

func TestIsResponse_ErrorOnly(t *testing.T) {
	id := json.RawMessage([]byte("1"))
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Error:   &JSONRPCError{Code: -1, Message: "fail"},
	}
	assert.True(t, msg.IsResponse())
	assert.False(t, msg.IsRequest())
	assert.False(t, msg.IsNotification())
}

// ---- Transport error paths ----

func TestTransport_ReadMessage_MalformedJSON(t *testing.T) {
	input := "not valid json\n"
	rw := &testReadWriter{readBuf: bytes.NewBufferString(input), writeBuf: new(bytes.Buffer)}
	tr := NewTransport(rw)

	_, err := tr.ReadMessage()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse error")
}

func TestTransport_ReadMessage_EOF(t *testing.T) {
	rw := &testReadWriter{readBuf: bytes.NewBufferString(""), writeBuf: new(bytes.Buffer)}
	tr := NewTransport(rw)

	_, err := tr.ReadMessage()
	assert.Error(t, err)
}

// ---- Server construction tests ----

func TestServer_NewServer_Defaults(t *testing.T) {
	agent := newMockAgent()
	prov := &mockProvider{}
	srv := NewServer(agent, prov)
	assert.NotNil(t, srv)
	assert.NotNil(t, srv.transport)
	assert.NotNil(t, srv.sessions)
	assert.NotNil(t, srv.handler)
	assert.NotNil(t, srv.transport)
}

func TestServer_NewServer_WithCapabilities(t *testing.T) {
	agent := newMockAgent()
	prov := &mockProvider{}
	caps := AgentCapabilities{
		PromptCapabilities:   PromptCapabilities{Image: false, Stream: true},
		MCPCapabilities:      &MCPCapabilities{Proxy: true},
	}
	srv := NewServer(agent, prov, WithCapabilities(caps))
	assert.False(t, srv.capabilities.PromptCapabilities.Image)
	assert.True(t, srv.capabilities.MCPCapabilities.Proxy)
}

func TestServer_NewServer_WithToolFilter(t *testing.T) {
	agent := newMockAgent()
	prov := &mockProvider{}
	srv := NewServer(agent, prov, WithToolFilter("tool_a", "tool_b"))
	assert.Len(t, srv.toolFilter, 2)
	assert.Equal(t, "tool_a", srv.toolFilter[0])
}

func TestServer_NewServer_WithEnableThinking(t *testing.T) {
	agent := newMockAgent()
	prov := &mockProvider{}
	srv := NewServer(agent, prov, WithEnableThinking(true))
	assert.True(t, srv.enableThinking)
}

func TestServer_NewServer_LogsViaCoreLogger(t *testing.T) {
	agent := newMockAgent()
	prov := &mockProvider{}
	srv := NewServer(agent, prov)
	assert.NotNil(t, srv)
	// Server uses core.Logger — no crash on Shutdown
	assert.NoError(t, srv.Shutdown(context.Background()))
}

func TestServer_NewServer_WithIO(t *testing.T) {
	agent := newMockAgent()
	prov := &mockProvider{}
	in := bytes.NewBufferString("")
	out := new(bytes.Buffer)
	srv := NewServer(agent, prov, WithIO(in, out))
	assert.NotNil(t, srv.transport)
}

func TestServer_Shutdown(t *testing.T) {
	agent := newMockAgent()
	prov := &mockProvider{}
	srv := NewServer(agent, prov)
	// Shutdown should not panic and should be idempotent
	assert.NoError(t, srv.Shutdown(context.Background()))
	assert.NoError(t, srv.Shutdown(context.Background()))
}

func TestServer_Run_ContextCancellation(t *testing.T) {
	agent := newMockAgent()
	prov := &mockProvider{}

	in := bytes.NewBufferString("")
	out := new(bytes.Buffer)
	srv := NewServer(agent, prov, WithIO(in, out))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Run should return promptly when ctx is already cancelled
	err := srv.Run(ctx)
	assert.NoError(t, err)
}

func TestServer_Run_MalformedMessage(t *testing.T) {
	agent := newMockAgent()
	prov := &mockProvider{}

	in := bytes.NewBufferString("this is not json\n")
	out := new(bytes.Buffer)
	srv := NewServer(agent, prov, WithIO(in, out))

	ctx := context.Background()
	err := srv.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read error")
}

func TestServer_Run_EOF(t *testing.T) {
	agent := newMockAgent()
	prov := &mockProvider{}

	in := bytes.NewBufferString("")
	out := new(bytes.Buffer)
	srv := NewServer(agent, prov, WithIO(in, out))

	ctx := context.Background()
	// Run should return nil when stdin is empty (EOF)
	err := srv.Run(ctx)
	assert.NoError(t, err)
}

// ---- Server Integration ----

func TestServer_FullFlow(t *testing.T) {
	// Build mock agent
	mockAgent := newMockAgent()
	mockAgent.executeFn = func(ctx context.Context, sys core.Message, userMsgs []core.Message) ([]core.Message, error) {
		return []core.Message{
			core.NewAssistantMessage(core.TextContent{Text: "I can help with that!"}),
		}, nil
	}

	input := buildACPInput(
		initializeMsg("1"),
		sessionNewMsg("2", "/test/project"),
	)
	readBuf := bytes.NewBufferString(input)
	writeBuf := new(bytes.Buffer)

	tr := NewTransport(&testReadWriter{readBuf: readBuf, writeBuf: writeBuf})

	handler := &Handler{
		transport: tr,
		sessions:  NewSessionManager(),
		agent:     mockAgent,
		provider:  &mockProvider{},
		serverInfo: ServerInfo{Name: "kugelblitz", Version: "0.1.0"},
	}

	ctx := context.Background()

	// 1. Initialize
	msg1, err := tr.ReadMessage()
	require.NoError(t, err)
	err = handler.Dispatch(ctx, msg1)
	require.NoError(t, err)
	output1 := writeBuf.String()
	assert.Contains(t, output1, `"protocolVersion":1`)
	writeBuf.Reset()

	// Extract session ID from session/new response to use in prompt
	msg2, err := tr.ReadMessage()
	require.NoError(t, err)
	err = handler.Dispatch(ctx, msg2)
	require.NoError(t, err)
	output2 := writeBuf.String()
	assert.Contains(t, output2, `"sessionId"`)
	writeBuf.Reset()

	// Parse session ID from response
	var sessionResp struct {
		Result SessionNewResult `json:"result"`
	}
	json.Unmarshal([]byte(output2), &sessionResp)
	sessionID := sessionResp.Result.SessionID
	require.NotEmpty(t, sessionID)

	// 3. Prompt
	promptJSON := fmt.Sprintf(`{"jsonrpc":"2.0","id":"3","method":"session/prompt","params":{"sessionId":"%s","prompt":[{"type":"text","text":"Hello agent"}]}}`+"\n", sessionID)
	readBuf2 := bytes.NewBufferString(promptJSON)
	tr2 := NewTransport(&testReadWriter{readBuf: readBuf2, writeBuf: writeBuf})
	handler.transport = tr2

	msg3, err := tr2.ReadMessage()
	require.NoError(t, err)
	err = handler.Dispatch(ctx, msg3)
	require.NoError(t, err)
	output3 := writeBuf.String()
	assert.Contains(t, output3, `"stopReason"`)
	assert.Contains(t, output3, StopReasonEndTurn)
}

func initializeMsg(id string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":"%s","method":"initialize","params":{"protocolVersion":1,"clientInfo":{"name":"test-editor","version":"1.0"},"clientCapabilities":{"fs":{}}}}`+"\n", id)
}

func sessionNewMsg(id, cwd string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":"%s","method":"session/new","params":{"cwd":"%s"}}`+"\n", id, cwd)
}

func sessionPromptMsg(id, sessionID, text string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":"%s","method":"session/prompt","params":{"sessionId":"%s","prompt":[{"type":"text","text":"%s"}]}}`+"\n", id, sessionID, text)
}

func buildACPInput(messages ...string) string {
	result := ""
	for _, m := range messages {
		result += m
	}
	return result
}

// ---- test helpers ----

// testReadWriter implements io.ReadWriter for testing transport.
type testReadWriter struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
}

func (rw *testReadWriter) Read(p []byte) (int, error)  { return rw.readBuf.Read(p) }
func (rw *testReadWriter) Write(p []byte) (int, error) { return rw.writeBuf.Write(p) }
