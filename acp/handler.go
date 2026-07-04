package acp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
)

// Handler processes ACP JSON-RPC method calls and routes them to the
// appropriate handler methods. It is the bridge between the wire protocol
// and the Kugelblitz agent runtime.
type Handler struct {
	transport  *Transport
	sessions   *SessionManager
	agent      core.IAgent
	provider   core.ILMProvider
	serverInfo ServerInfo
}

// Dispatch routes a JSON-RPC request to the correct handler method and
// writes the response (or error) back to the transport.
func (h *Handler) Dispatch(ctx context.Context, msg *JSONRPCMessage) error {
	if !msg.IsRequest() {
		if msg.IsNotification() {
			return h.handleNotification(ctx, msg)
		}
		return nil
	}

	id := *msg.ID

	switch msg.Method {
	case "initialize":
		h.invoke(ctx, id, msg.Params, func(p json.RawMessage) (any, error) {
			return h.handleInitialize(ctx, p)
		})

	case "session/new":
		h.invoke(ctx, id, msg.Params, func(p json.RawMessage) (any, error) {
			var params SessionNewParams
			if err := json.Unmarshal(p, &params); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			return h.handleSessionNew(ctx, params)
		})

	case "session/prompt":
		h.invoke(ctx, id, msg.Params, func(p json.RawMessage) (any, error) {
			var params SessionPromptParams
			if err := json.Unmarshal(p, &params); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			return h.handleSessionPrompt(ctx, params)
		})

	case "session/cancel":
		h.invoke(ctx, id, msg.Params, func(p json.RawMessage) (any, error) {
			var params SessionCancelParams
			if err := json.Unmarshal(p, &params); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			return nil, h.handleSessionCancel(ctx, params)
		})

	case "session/load":
		h.invoke(ctx, id, msg.Params, func(p json.RawMessage) (any, error) {
			var params SessionLoadParams
			if err := json.Unmarshal(p, &params); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			return h.handleSessionLoad(ctx, params)
		})

	case "session/list":
		h.invoke(ctx, id, msg.Params, func(_ json.RawMessage) (any, error) {
			return h.handleSessionList(ctx)
		})

	case "session/delete":
		h.invoke(ctx, id, msg.Params, func(p json.RawMessage) (any, error) {
			var params SessionDeleteParams
			if err := json.Unmarshal(p, &params); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			return nil, h.handleSessionDelete(ctx, params)
		})

	default:
		h.writeError(id, ErrCodeMethodNotFound, fmt.Sprintf("Method not found: %s", msg.Method), nil)
	}

	return nil
}

// invoke runs a handler function and writes the response or error.
func (h *Handler) invoke(_ context.Context, id json.RawMessage, rawParams json.RawMessage, fn func(json.RawMessage) (any, error)) {
	result, err := fn(rawParams)
	if err != nil {
		h.writeError(id, ErrCodeInternalError, err.Error(), nil)
		return
	}
	if result != nil {
		resp, marshalErr := NewResponse(id, result)
		if marshalErr != nil {
			h.writeError(id, ErrCodeInternalError, marshalErr.Error(), nil)
			return
		}
		_ = h.transport.WriteMessage(resp)
	}
}

// handleNotification processes JSON-RPC notifications (no response expected).
func (h *Handler) handleNotification(_ context.Context, _ *JSONRPCMessage) error {
	return nil
}

// handleInitialize handles the initialize method.
func (h *Handler) handleInitialize(_ context.Context, p json.RawMessage) (any, error) {
	var params InitializeParams
	if err := json.Unmarshal(p, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	return InitializeResult{
		ProtocolVersion: ProtocolVersion,
		ServerInfo:      h.serverInfo,
		Capabilities: AgentCapabilities{
			PromptCapabilities: PromptCapabilities{
				Image:  true,
				Stream: true,
			},
		},
	}, nil
}

// handleSessionNew creates a new ACP session.
func (h *Handler) handleSessionNew(_ context.Context, params SessionNewParams) (any, error) {
	agent := h.agent
	session := h.sessions.Create(params.Cwd, agent)
	return SessionNewResult{SessionID: session.ID}, nil
}

// handleSessionPrompt runs the agent on the user prompt and streams
// progress notifications back to the client.
func (h *Handler) handleSessionPrompt(ctx context.Context, params SessionPromptParams) (any, error) {
	session, err := h.sessions.Load(params.SessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %s", params.SessionID)
	}

	promptCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := h.sessions.SetCancelFunc(params.SessionID, cancel); err != nil {
		return nil, err
	}

	userMessages, err := ContentBlocksToMessages(params.Prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to convert prompt: %w", err)
	}

	systemMsg := core.Message{
		ID:   "system",
		Role: "system",
		Content: core.TextContent{
			Text: fmt.Sprintf("You are an AI coding agent. Working directory: %s", session.Cwd),
		},
	}

	// Register event hooks to stream progress to the client
	sid := params.SessionID
	hooks := core.AgentEventHooks{
		OnReplyChunk: func(id constants.AgentIdentity, chunk string) {
			_ = h.transport.SendNotification("session/update", SessionUpdateParams{
				SessionID: sid, Update: NewAgentMessageChunk(chunk),
			})
		},
		OnBlockReply: func(id constants.AgentIdentity, text string) {
			_ = h.transport.SendNotification("session/update", SessionUpdateParams{
				SessionID: sid, Update: NewAgentMessageChunk(text),
			})
		},
		OnFunctionCall: func(id constants.AgentIdentity, detail core.ToolCallDetail) {
			notif := NewToolCallNotification(detail.ID, detail.ToolName, detail.Args)
			_ = h.transport.SendNotification("session/update", SessionUpdateParams{
				SessionID: sid, Update: notif,
			})
		},
		OnToolCallEnd: func(id constants.AgentIdentity, result core.ToolCallResult) {
			status := ToolCallStatusCompleted
			if _, hasErr := result.Outputs["error"]; hasErr {
				status = ToolCallStatusError
			}
			notif := NewToolCallUpdateNotification(result.ToolCallID, status, result.Outputs)
			_ = h.transport.SendNotification("session/update", SessionUpdateParams{
				SessionID: params.SessionID,
				Update:    notif,
			})
		},
	}

	session.Agent.RegisterEventHooks(hooks)

	assistantMessages, err := session.Agent.Execute(promptCtx, systemMsg, userMessages)
	if err != nil {
		_ = h.transport.SendNotification("session/update", SessionUpdateParams{
			SessionID: params.SessionID,
			Update:    NewAgentMessageChunk(fmt.Sprintf("Error: %v", err)),
		})
		return SessionPromptResult{StopReason: StopReasonCancelled}, nil
	}

	for _, msg := range userMessages {
		_ = h.sessions.AppendMessage(params.SessionID, msg)
	}
	for _, msg := range assistantMessages {
		_ = h.sessions.AppendMessage(params.SessionID, msg)
	}

	return SessionPromptResult{StopReason: StopReasonEndTurn}, nil
}

// handleSessionCancel cancels the active prompt execution in a session.
func (h *Handler) handleSessionCancel(_ context.Context, params SessionCancelParams) error {
	return h.sessions.Cancel(params.SessionID)
}

// handleSessionLoad loads an existing session and replays its history.
func (h *Handler) handleSessionLoad(_ context.Context, params SessionLoadParams) (any, error) {
	session, err := h.sessions.Load(params.SessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %s", params.SessionID)
	}

	blocks := MessagesToContentBlocks(session.Messages)
	for _, block := range blocks {
		_ = h.transport.SendNotification("session/update", SessionUpdateParams{
			SessionID: params.SessionID,
			Update:    block,
		})
	}

	return SessionLoadResult{SessionID: session.ID}, nil
}

// handleSessionList returns information about all sessions.
func (h *Handler) handleSessionList(_ context.Context) (any, error) {
	return h.sessions.List(), nil
}

// handleSessionDelete deletes a session.
func (h *Handler) handleSessionDelete(_ context.Context, params SessionDeleteParams) error {
	return h.sessions.Delete(params.SessionID)
}

// writeError sends a JSON-RPC error response.
func (h *Handler) writeError(id json.RawMessage, code int, message string, data any) {
	resp := NewErrorResponse(id, code, message, data)
	_ = h.transport.WriteMessage(resp)
}
