package core

// This file re-exports the pure value types and contracts that now live in the
// leaf package core/types. The aliases below keep every existing `core.X`
// reference compiling unchanged while consumers migrate to import core/types
// directly. Once no package depends on core solely for value types, these
// aliases (and the wrapper functions) can be removed.

import (
	"github.com/B777B2056-2/kugelblitz/core/types"
)

// ── Value types ──

type Usage = types.Usage
type MultiModalDetail = types.MultiModalDetail
type ToolCallDetail = types.ToolCallDetail
type ToolCallResult = types.ToolCallResult
type Content = types.Content
type TextContent = types.TextContent
type ReasoningContent = types.ReasoningContent
type ToolCallContent = types.ToolCallContent
type ToolResultContent = types.ToolResultContent
type MultiModalContent = types.MultiModalContent
type CompositeContent = types.CompositeContent
type Message = types.Message
type AgentInput = types.AgentInput

// ── Tool contracts ──

type ToolCallFunc = types.ToolCallFunc
type ToolDefinition = types.ToolDefinition

// ── Provider contracts ──

type GenerateParams = types.GenerateParams
type ModelEventHandler = types.ModelEventHandler
type ILMProvider = types.ILMProvider

// ── Reasoning effort levels ──

const (
	ReasoningEffortNone    = types.ReasoningEffortNone
	ReasoningEffortMinimal = types.ReasoningEffortMinimal
	ReasoningEffortLow     = types.ReasoningEffortLow
	ReasoningEffortMedium  = types.ReasoningEffortMedium
	ReasoningEffortHigh    = types.ReasoningEffortHigh
	ReasoningEffortXHigh   = types.ReasoningEffortXHigh
	ReasoningEffortMax     = types.ReasoningEffortMax
)

// ── Sentinel errors ──

var (
	ErrContextLengthExceeded = types.ErrContextLengthExceeded
	ErrMaxStepsExceeded      = types.ErrMaxStepsExceeded
	ErrMaxCyclesExceeded     = types.ErrMaxCyclesExceeded
)

// ── Constructors / helpers ──

// NewSystemMessage creates a new system message with the given content.
func NewSystemMessage(content Content) Message { return types.NewSystemMessage(content) }

// NewUserMessage creates a new user message with the given content.
func NewUserMessage(content Content) Message { return types.NewUserMessage(content) }

// NewAssistantMessage creates a new assistant message with the given content.
func NewAssistantMessage(content Content) Message { return types.NewAssistantMessage(content) }

// NewToolMessage creates a tool message containing tool call results.
func NewToolMessage(results []ToolCallResult) Message { return types.NewToolMessage(results) }

// ExtractToolResult extracts a typed value from the first tool result matching toolName.
func ExtractToolResult[T any](messages []Message, toolName, key string) (T, bool) {
	return types.ExtractToolResult[T](messages, toolName, key)
}

// CombineModelEventHandlers returns a handler that delegates to all given handlers in order.
func CombineModelEventHandlers(handlers ...ModelEventHandler) ModelEventHandler {
	return types.CombineModelEventHandlers(handlers...)
}
