package tools

import (
	"context"
	"fmt"
	"github.com/B777B2056-2/kugelblitz/core"
)

// Tool is the abstract interface for all tools.
// Implement Definition() to describe the tool to the LLM,
// and Execute() to perform the actual work.
type Tool interface {
	Definition() core.ToolDefinition
	Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult
}

// Register registers a tool with the global ToolRegistry.
func Register(t Tool) {
	core.RegisterTool(t.Definition(), t.Execute)
}

// RegisterAll registers multiple tools at once.
func RegisterAll(tools ...Tool) {
	for _, t := range tools {
		Register(t)
	}
}

// ErrorResult is a helper to create an error ToolCallResult.
func ErrorResult(toolCallID, toolName string, err error) core.ToolCallResult {
	return core.ToolCallResult{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Outputs: map[string]any{
			"error": err.Error(),
		},
	}
}

// SuccessResult is a helper to create a success ToolCallResult with outputs.
func SuccessResult(toolCallID, toolName string, outputs map[string]any) core.ToolCallResult {
	return core.ToolCallResult{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Outputs:    outputs,
	}
}

// OkResult is a convenience helper that returns {"ok": true}.
func OkResult(toolCallID, toolName string) core.ToolCallResult {
	return SuccessResult(toolCallID, toolName, map[string]any{"ok": true})
}

// Arg extracts a string argument from the tool call, or returns an error.
func Arg(detail core.ToolCallDetail, key string) (string, error) {
	v, ok := detail.Args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument: %s", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", key)
	}
	return s, nil
}
