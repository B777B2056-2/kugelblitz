package types

import "context"

// ToolCallFunc is the function signature for tool call implementations.
type ToolCallFunc func(context.Context, ToolCallDetail) ToolCallResult

// ToolDefinition describes a tool that can be called by the LLM.
type ToolDefinition struct {
	Name         string
	Description  string
	JSONSchema   map[string]any // input parameter schema (JSON Schema format)
	OutputSchema map[string]any // output/return value schema (optional; nil = no schema)
	Terminating  bool           // if true, ReAct loop stops immediately after calling this tool
}
