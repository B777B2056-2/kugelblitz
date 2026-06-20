package core

import (
	"context"
	"fmt"
	"sync"
)

// ToolCallFunc is the function signature for tool call implementations.
type ToolCallFunc func(context.Context, ToolCallDetail) ToolCallResult

// ToolDefinition describes a tool that can be called by the LLM.
type ToolDefinition struct {
	Name        string
	Description string
	JsonSchema  map[string]any
}

type registryEntry struct {
	fn  ToolCallFunc
	def ToolDefinition
}

// ToolRegistry stores tool implementations and their definitions.
// The global singleton is obtained via GetToolRegistry.
type ToolRegistry struct {
	tools map[string]registryEntry
	mu    sync.RWMutex
}

var (
	globalToolRegistry     *ToolRegistry
	globalToolRegistryOnce sync.Once
)

// GetToolRegistry returns the global singleton ToolRegistry.
// The instance is lazily initialized on first call — safe for concurrent use.
func GetToolRegistry() *ToolRegistry {
	globalToolRegistryOnce.Do(func() {
		globalToolRegistry = &ToolRegistry{
			tools: make(map[string]registryEntry),
		}
	})
	return globalToolRegistry
}

// Register adds or replaces a tool. It stores both the implementation
// and its definition (used when building provider requests).
func (tr *ToolRegistry) Register(def ToolDefinition, fn ToolCallFunc) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.tools[def.Name] = registryEntry{fn: fn, def: def}
}

// Call looks up and executes a tool by name. If the tool is not found,
// it returns a ToolCallResult with an error output.
func (tr *ToolRegistry) Call(ctx context.Context, detail ToolCallDetail) ToolCallResult {
	tr.mu.RLock()
	entry, ok := tr.tools[detail.ToolName]
	tr.mu.RUnlock()
	if !ok {
		return ToolCallResult{
			ToolCallID: detail.ID,
			ToolName:   detail.ToolName,
			Outputs:    MakeErrorToolOutputs(fmt.Errorf("tool not found: %s", detail.ToolName)),
		}
	}
	return entry.fn(ctx, detail)
}

// ListDefinitions returns all registered tool definitions, suitable for
// passing to GenerateParams.Tools when calling a provider.
func (tr *ToolRegistry) ListDefinitions() []ToolDefinition {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	defs := make([]ToolDefinition, 0, len(tr.tools))
	for _, entry := range tr.tools {
		defs = append(defs, entry.def)
	}
	return defs
}

// Reset clears all registered tools. Primarily intended for testing.
func (tr *ToolRegistry) Reset() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.tools = make(map[string]registryEntry)
}

// RegisterTool registers a tool on the global ToolRegistry.
func RegisterTool(def ToolDefinition, fn ToolCallFunc) {
	GetToolRegistry().Register(def, fn)
}

// CallTool calls a tool on the global ToolRegistry.
func CallTool(ctx context.Context, detail ToolCallDetail) ToolCallResult {
	return GetToolRegistry().Call(ctx, detail)
}

// ListToolDefinitions returns all tool definitions from the global ToolRegistry.
func ListToolDefinitions() []ToolDefinition {
	return GetToolRegistry().ListDefinitions()
}

// MakeErrorToolOutputs creates an outputs map containing an error message.
func MakeErrorToolOutputs(err error) map[string]any {
	return map[string]any{
		"error": err.Error(),
	}
}
