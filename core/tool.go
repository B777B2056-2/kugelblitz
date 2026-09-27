package core

import (
	"context"
	"fmt"
	"sort"
	"sync"

	types "github.com/B777B2056-2/kugelblitz/core/types"
)

type registryEntry struct {
	fn  types.ToolCallFunc
	def types.ToolDefinition
}

// ToolRegistry stores tool implementations and their definitions.
// The global singleton is obtained via GetToolRegistry.
type ToolRegistry struct {
	tools         map[string]registryEntry
	internalNames map[string]bool // framework-internal tool names (excluded from CustomToolNames)
	mu            sync.RWMutex
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
			tools:         make(map[string]registryEntry),
			internalNames: make(map[string]bool),
		}
	})
	return globalToolRegistry
}

// ResetToolRegistry resets the global singleton so the next GetToolRegistry
// call rebuilds a fresh, empty registry. Intended for tests that need isolation
// from tools registered by earlier tests or package init(). Callers that need
// the built-in tools must re-register them (e.g. internals.RegisterAll).
func ResetToolRegistry() {
	globalToolRegistryOnce = sync.Once{}
	globalToolRegistry = nil
}

// Register adds or replaces a tool. It stores both the implementation
// and its definition (used when building provider requests).
func (tr *ToolRegistry) Register(def types.ToolDefinition, fn types.ToolCallFunc) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.tools[def.Name] = registryEntry{fn: fn, def: def}
}

// Call looks up and executes a tool by name. If the tool is not found,
// it returns a types.ToolCallResult with an error output.
func (tr *ToolRegistry) Call(ctx context.Context, detail types.ToolCallDetail) types.ToolCallResult {
	tr.mu.RLock()
	entry, ok := tr.tools[detail.ToolName]
	tr.mu.RUnlock()
	if !ok {
		return types.ToolCallResult{
			ToolCallID: detail.ID,
			ToolName:   detail.ToolName,
			Outputs:    MakeErrorToolOutputs(fmt.Errorf("tool not found: %s", detail.ToolName)),
		}
	}
	return entry.fn(ctx, detail)
}

// ListDefinitions returns all registered tool definitions, suitable for
// passing to GenerateParams.Tools when calling a provider.
func (tr *ToolRegistry) ListDefinitions() []types.ToolDefinition {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	defs := make([]types.ToolDefinition, 0, len(tr.tools))
	for _, entry := range tr.tools {
		defs = append(defs, entry.def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

// IsTerminating returns true if the named tool should end the ReAct loop
// immediately after execution (without feeding the result back to the LLM).
func (tr *ToolRegistry) IsTerminating(name string) bool {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	entry, ok := tr.tools[name]
	return ok && entry.def.Terminating
}

// MarkAsInternal marks the given tool names as framework-internal.
func (tr *ToolRegistry) MarkAsInternal(names ...string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	for _, name := range names {
		tr.internalNames[name] = true
	}
}

// RegisterCustomTool registers a user-defined tool with name-conflict detection.
func (tr *ToolRegistry) RegisterCustomTool(def types.ToolDefinition, fn types.ToolCallFunc) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if _, exists := tr.tools[def.Name]; exists {
		return fmt.Errorf("tool %q already registered", def.Name)
	}
	tr.tools[def.Name] = registryEntry{fn: fn, def: def}
	return nil
}

// CustomToolNames returns the names of all registered tools that are not internal.
func (tr *ToolRegistry) CustomToolNames() []string {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	var result []string
	for name := range tr.tools {
		if !tr.internalNames[name] {
			result = append(result, name)
		}
	}
	return result
}

// RegisterTool registers a tool on the global ToolRegistry.
func RegisterTool(def types.ToolDefinition, fn types.ToolCallFunc) {
	GetToolRegistry().Register(def, fn)
}

// RegisterCustomTool registers a user-defined tool on the global ToolRegistry
// with name-conflict detection. Returns an error if the name already exists.
func RegisterCustomTool(def types.ToolDefinition, fn types.ToolCallFunc) error {
	return GetToolRegistry().RegisterCustomTool(def, fn)
}

// CallTool calls a tool on the global ToolRegistry.
func CallTool(ctx context.Context, detail types.ToolCallDetail) types.ToolCallResult {
	return GetToolRegistry().Call(ctx, detail)
}

// ListToolDefinitions returns all tool definitions from the global ToolRegistry.
func ListToolDefinitions() []types.ToolDefinition {
	return GetToolRegistry().ListDefinitions()
}

// MakeErrorToolOutputs creates an outputs map containing an error message.
func MakeErrorToolOutputs(err error) map[string]any {
	return map[string]any{
		"error": err.Error(),
	}
}
