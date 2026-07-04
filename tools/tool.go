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

// RequiredString extracts a required non-empty string argument.
func RequiredString(detail core.ToolCallDetail, key string) (string, error) {
	v, ok := detail.Args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument: %s", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string, got %T", key, v)
	}
	if s == "" {
		return "", fmt.Errorf("argument %q must not be empty", key)
	}
	return s, nil
}

// OptionalString extracts an optional string argument, returning "" if missing.
func OptionalString(detail core.ToolCallDetail, key string) string {
	v, ok := detail.Args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// RequiredInt extracts a required integer argument (handles JSON float64 decoding).
func RequiredInt(detail core.ToolCallDetail, key string) (int, error) {
	v, ok := detail.Args[key]
	if !ok {
		return 0, fmt.Errorf("missing required argument: %s", key)
	}
	switch n := v.(type) {
	case float64:
		if n != float64(int(n)) {
			return 0, fmt.Errorf("argument %q must be a whole number, got %v", key, n)
		}
		return int(n), nil
	case int:
		return n, nil
	default:
		return 0, fmt.Errorf("argument %q must be an integer, got %T", key, v)
	}
}

// OptionalInt extracts an optional integer, returning defaultVal if missing.
func OptionalInt(detail core.ToolCallDetail, key string, defaultVal int) (int, error) {
	v, ok := detail.Args[key]
	if !ok {
		return defaultVal, nil
	}
	switch n := v.(type) {
	case float64:
		if n != float64(int(n)) {
			return 0, fmt.Errorf("argument %q must be a whole number, got %v", key, n)
		}
		return int(n), nil
	case int:
		return n, nil
	default:
		return 0, fmt.Errorf("argument %q must be an integer, got %T", key, v)
	}
}

// OptionalBool extracts an optional boolean, returning defaultVal if missing.
func OptionalBool(detail core.ToolCallDetail, key string, defaultVal bool) bool {
	v, ok := detail.Args[key]
	if !ok {
		return defaultVal
	}
	b, ok := v.(bool)
	if !ok {
		return defaultVal
	}
	return b
}

// In validates that a string value is one of the allowed set.
func In(value string, allowed ...string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("value %q is not valid; must be one of: %v", value, allowed)
}

// BoundedInt validates that an integer is within [min, max].
func BoundedInt(key string, value, min, max int) error {
	if value < min || value > max {
		return fmt.Errorf("argument %q must be between %d and %d, got %d", key, min, max, value)
	}
	return nil
}
