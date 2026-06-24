package acp

import (
	"encoding/json"
	"fmt"
)

// JSONRPCMessage is a generic JSON-RPC 2.0 message envelope.
// It can represent a request, a response, or an error response.
type JSONRPCMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *JSONRPCError    `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC 2.0 error codes.
const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603
)

// IsRequest reports whether this message is a JSON-RPC request (has a method).
func (m *JSONRPCMessage) IsRequest() bool {
	return m.Method != "" && m.ID != nil
}

// IsNotification reports whether this message is a JSON-RPC notification
// (has a method but no id).
func (m *JSONRPCMessage) IsNotification() bool {
	return m.Method != "" && m.ID == nil
}

// IsResponse reports whether this message is a JSON-RPC response (has a result
// or error but no method).
func (m *JSONRPCMessage) IsResponse() bool {
	return m.Method == "" && (m.Result != nil || m.Error != nil)
}

// NewResponse creates a successful JSON-RPC response for the given request id.
func NewResponse(id json.RawMessage, result any) (*JSONRPCMessage, error) {
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Result:  resultBytes,
	}, nil
}

// NewErrorResponse creates a JSON-RPC error response for the given request id.
func NewErrorResponse(id json.RawMessage, code int, message string, data any) *JSONRPCMessage {
	var rawData json.RawMessage
	if data != nil {
		b, _ := json.Marshal(data)
		rawData = b
	}
	return &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    rawData,
		},
	}
}

// NewNotification creates a JSON-RPC notification (no id).
func NewNotification(method string, params any) (*JSONRPCMessage, error) {
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return &JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsBytes,
	}, nil
}

// ---- ACP Protocol Constants ----

const ProtocolVersion = 1

const (
	StopReasonEndTurn   = "end_turn"
	StopReasonCancelled = "cancelled"
	StopReasonMaxTokens = "max_tokens"
)

// ---- ACP Content Blocks ----

// ContentBlockType is the type discriminator for ACP content blocks.
type ContentBlockType string

const (
	ContentBlockTypeText         ContentBlockType = "text"
	ContentBlockTypeImage        ContentBlockType = "image"
	ContentBlockTypeResourceLink ContentBlockType = "resource_link"
	ContentBlockTypeResource     ContentBlockType = "resource"
)

// ContentBlock is an ACP message content block (text, image, resource_link, resource).
type ContentBlock struct {
	Type     ContentBlockType `json:"type"`
	Text     string           `json:"text,omitempty"`

	// Image fields
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`

	// Resource link fields
	URI         string `json:"uri,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Size        int64  `json:"size,omitempty"`

	// Embedded resource
	Resource *ResourceContent `json:"resource,omitempty"`
}

// ResourceContent represents an embedded resource in a content block.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// ---- ACP Initialize Types ----

// InitializeParams is sent by the client for capability negotiation.
type InitializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientInfo         ClientInfo         `json:"clientInfo"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
}

// ClientInfo describes the editor/client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClientCapabilities describes what the client supports.
// The FS and Terminal fields accept both boolean and object forms
// (ACP clients vary in how they encode these capabilities).
type ClientCapabilities struct {
	FS       FlexibleFlag `json:"fs,omitempty"`
	Terminal FlexibleFlag `json:"terminal,omitempty"`
}

// FlexibleFlag is a boolean that unmarshals from either a JSON bool (true/false)
// or a JSON object ({}) — both are valid in ACP.
type FlexibleFlag bool

// UnmarshalJSON implements json.Unmarshaler for FlexibleFlag.
func (f *FlexibleFlag) UnmarshalJSON(data []byte) error {
	// Try as boolean first
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		*f = FlexibleFlag(b)
		return nil
	}
	// Otherwise try as object — any non-null object means true
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err == nil {
		*f = true
		return nil
	}
	*f = false
	return nil
}

// MarshalJSON implements json.Marshaler for FlexibleFlag.
func (f FlexibleFlag) MarshalJSON() ([]byte, error) {
	return json.Marshal(bool(f))
}

// InitializeResult is the agent's response to initialize.
type InitializeResult struct {
	ProtocolVersion int              `json:"protocolVersion"`
	ServerInfo      ServerInfo       `json:"serverInfo"`
	Capabilities    AgentCapabilities `json:"capabilities"`
}

// ServerInfo describes the agent.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// AgentCapabilities describes what the agent supports.
type AgentCapabilities struct {
	PromptCapabilities   PromptCapabilities   `json:"promptCapabilities"`
	MCPCapabilities      *MCPCapabilities     `json:"mcpCapabilities,omitempty"`
	TerminalCapabilities *TerminalCapabilities `json:"terminalCapabilities,omitempty"`
}

// PromptCapabilities describes the agent's prompt handling.
type PromptCapabilities struct {
	Image  bool `json:"image"`
	Stream bool `json:"stream"`
}

// MCPCapabilities indicates MCP server support.
type MCPCapabilities struct {
	Proxy bool `json:"proxy"`
}

// TerminalCapabilities indicates terminal support.
type TerminalCapabilities struct{}

// ---- ACP Session Types ----

// SessionNewParams is sent to create a new session.
type SessionNewParams struct {
	Cwd       string          `json:"cwd"`
	MCPConfig json.RawMessage `json:"mcpServers,omitempty"`
}

// SessionNewResult is returned after session creation.
type SessionNewResult struct {
	SessionID string `json:"sessionId"`
}

// SessionPromptParams is sent to prompt the agent.
type SessionPromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// SessionPromptResult is returned when the agent finishes processing.
type SessionPromptResult struct {
	StopReason string `json:"stopReason"`
}

// SessionCancelParams is sent to cancel an active prompt.
type SessionCancelParams struct {
	SessionID string `json:"sessionId"`
}

// SessionLoadParams is sent to load an existing session.
type SessionLoadParams struct {
	SessionID string `json:"sessionId"`
}

// SessionLoadResult is returned after loading a session.
type SessionLoadResult struct {
	SessionID string `json:"sessionId"`
}

// SessionDeleteParams is sent to delete a session.
type SessionDeleteParams struct {
	SessionID string `json:"sessionId"`
}

// SessionInfo contains summary info about a session.
type SessionInfo struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	CreatedAt string `json:"createdAt"`
}

// ---- ACP Update Notification Types ----

const (
	UpdateTypeAgentMessageChunk = "agent_message_chunk"
	UpdateTypeToolCall          = "tool_call"
	UpdateTypeToolCallUpdate    = "tool_call_update"
	UpdateTypePlan              = "plan"
)

// SessionUpdateParams is the params for a session/update notification.
// The ACP spec uses "sessionUpdate" as the discriminator field in the update object,
// and wraps text content in a "content" field.
type SessionUpdateParams struct {
	SessionID string `json:"sessionId"`
	Update    any    `json:"update"`
}

// TextContentBlock is a simple text content block for use in notifications.
type TextContentBlock struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// AgentMessageChunk is a streaming text chunk from the agent (ACP v1 format).
type AgentMessageChunk struct {
	SessionUpdate string           `json:"sessionUpdate"` // "agent_message_chunk"
	Content       TextContentBlock `json:"content"`
}

// NewAgentMessageChunk creates an AgentMessageChunk with the correct discriminator.
func NewAgentMessageChunk(text string) AgentMessageChunk {
	return AgentMessageChunk{
		SessionUpdate: UpdateTypeAgentMessageChunk,
		Content: TextContentBlock{
			Type: "text",
			Text: text,
		},
	}
}

// ToolCallNotification notifies the client that a tool call is starting.
type ToolCallNotification struct {
	SessionUpdate string         `json:"sessionUpdate"` // "tool_call"
	ToolCallID    string         `json:"toolCallId"`
	Title         string         `json:"title"`
	Status        string         `json:"status"` // "running"
	Input         map[string]any `json:"input,omitempty"`
}

// NewToolCallNotification creates a ToolCallNotification in ACP v1 format.
func NewToolCallNotification(id, name string, input map[string]any) ToolCallNotification {
	return ToolCallNotification{
		SessionUpdate: UpdateTypeToolCall,
		ToolCallID:    id,
		Title:         name,
		Status:        ToolCallStatusRunning,
		Input:         input,
	}
}

// ToolCallUpdateNotification notifies the client of a tool call result.
type ToolCallUpdateNotification struct {
	SessionUpdate string             `json:"sessionUpdate"` // "tool_call_update"
	ToolCallID    string             `json:"toolCallId"`
	Status        string             `json:"status"` // "running", "completed", "error"
	Content       []TextContentBlock `json:"content,omitempty"`
}

// NewToolCallUpdateNotification creates a tool result update in ACP v1 format.
func NewToolCallUpdateNotification(id, status string, outputs map[string]any) ToolCallUpdateNotification {
	var content []TextContentBlock
	if outputs != nil {
		for k, v := range outputs {
			if s, ok := v.(string); ok {
				content = append(content, TextContentBlock{Type: "text", Text: fmt.Sprintf("%s: %s", k, s)})
			}
		}
		if len(content) == 0 {
			// Fallback: serialize as JSON
			if b, err := json.Marshal(outputs); err == nil {
				content = []TextContentBlock{{Type: "text", Text: string(b)}}
			}
		}
	}
	return ToolCallUpdateNotification{
		SessionUpdate: UpdateTypeToolCallUpdate,
		ToolCallID:    id,
		Status:        status,
		Content:       content,
	}
}

const (
	ToolCallStatusRunning   = "running"
	ToolCallStatusCompleted = "completed"
	ToolCallStatusError    = "error"
)

// PlanNotification notifies the client about the agent's plan.
type PlanNotification struct {
	Type string `json:"type"` // "plan"
	Plan string `json:"plan"`
}

// ---- ACP Permission Types ----

// PermissionRequest represents an agent asking for user permission.
type PermissionRequest struct {
	SessionID string `json:"sessionId"`
	ToolName  string `json:"toolName"`
	ToolID    string `json:"toolId"`
	Reason    string `json:"reason"`
	Args      map[string]any `json:"args"`
}
