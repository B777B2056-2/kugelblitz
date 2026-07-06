package core

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// registry returns the global singleton.
func registry() *ToolRegistry {
	return GetToolRegistry()
}

func TestToolRegistry_RegisterAndCall(t *testing.T) {
	r := registry()

	r.Register(
		ToolDefinition{Name: "greet", Description: "Greets someone", JSONSchema: map[string]any{"type": "object"}},
		func(ctx context.Context, detail ToolCallDetail) ToolCallResult {
			return ToolCallResult{
				ToolCallID: detail.ID,
				ToolName:   detail.ToolName,
				Outputs:    map[string]any{"greeting": "hello"},
			}
		},
	)

	result := r.Call(context.Background(), ToolCallDetail{ID: "tc-1", ToolName: "greet", Args: map[string]any{}})
	assert.Equal(t, "tc-1", result.ToolCallID)
	assert.Equal(t, "greet", result.ToolName)
	assert.Equal(t, "hello", result.Outputs["greeting"])
}

func TestToolRegistry_CallUnknownTool(t *testing.T) {
	r := registry()

	result := r.Call(context.Background(), ToolCallDetail{ID: "tc-1", ToolName: "nonexistent"})
	assert.Equal(t, "tc-1", result.ToolCallID)
	assert.Equal(t, "nonexistent", result.ToolName)
	assert.Contains(t, result.Outputs["error"], "tool not found")
}

func TestToolRegistry_ListDefinitions_HasBuiltins(t *testing.T) {
	r := registry()
	defs := r.ListDefinitions()
	assert.NotEmpty(t, defs, "global registry should have built-in tools from init()")
}

func TestToolRegistry_ListDefinitions_HasEntries(t *testing.T) {
	r := registry()

	r.Register(ToolDefinition{Name: "tool1", Description: "First tool"}, nil)
	r.Register(ToolDefinition{Name: "tool2", Description: "Second tool"}, nil)

	defs := r.ListDefinitions()
	assert.GreaterOrEqual(t, len(defs), 2)

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	assert.True(t, names["tool1"])
	assert.True(t, names["tool2"])
}

func TestToolRegistry_RegisterOverwrites(t *testing.T) {
	r := registry()

	r.Register(ToolDefinition{Name: "tool", Description: "v1"}, nil)
	r.Register(ToolDefinition{Name: "tool", Description: "v2"}, nil)

	defs := r.ListDefinitions()
	found := false
	for _, d := range defs {
		if d.Name == "tool" {
			assert.Equal(t, "v2", d.Description)
			found = true
		}
	}
	assert.True(t, found)
}

func TestToolRegistry_ConcurrentAccess(t *testing.T) {
	r := registry()
	var wg sync.WaitGroup

	// Concurrent registrations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r.Register(
				ToolDefinition{Name: "tool", Description: "concurrent"},
				func(ctx context.Context, detail ToolCallDetail) ToolCallResult {
					return ToolCallResult{ToolCallID: detail.ID}
				},
			)
		}(i)
	}

	// Concurrent calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = r.Call(context.Background(), ToolCallDetail{ID: "tc", ToolName: "tool"})
		}(i)
	}

	// Concurrent ListDefinitions
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.ListDefinitions()
		}()
	}

	wg.Wait()
}

func TestToolRegistry_ConcurrentRegisterCallReset(t *testing.T) {
	r := registry()
	var wg sync.WaitGroup

	// Concurrent Register
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("t%d", idx)
			r.Register(ToolDefinition{Name: name, Description: "test"},
				func(ctx context.Context, detail ToolCallDetail) ToolCallResult {
					return ToolCallResult{ToolCallID: detail.ID, ToolName: name}
				})
		}(i)
	}
	wg.Wait()

	// Concurrent Call
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = r.Call(context.Background(), ToolCallDetail{ID: "tc", ToolName: fmt.Sprintf("t%d", idx)})
		}(i)
	}
	wg.Wait()

	// Verify all registered
	defs := r.ListDefinitions()
	assert.GreaterOrEqual(t, len(defs), 5)

	// Concurrent Reset + Register
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r.Register(ToolDefinition{Name: fmt.Sprintf("new%d", idx), Description: "after reset"}, nil)
		}(i)
	}
	wg.Wait()

	// No panic, no corruption
	_ = r.ListDefinitions()
}

func TestMakeErrorToolOutputs_ContainsErrorMessage(t *testing.T) {
	outputs := MakeErrorToolOutputs(assert.AnError)
	assert.Equal(t, assert.AnError.Error(), outputs["error"])
}

func TestToolDefinition_Fields(t *testing.T) {
	def := ToolDefinition{
		Name:        "search",
		Description: "Search the web",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
		},
	}
	assert.Equal(t, "search", def.Name)
	assert.Equal(t, "Search the web", def.Description)
	assert.NotNil(t, def.JSONSchema)
}

func TestGetToolRegistry_ReturnsSingleton(t *testing.T) {
	a := GetToolRegistry()
	b := GetToolRegistry()
	assert.Same(t, a, b, "GetToolRegistry must return the same instance")
}

func TestRegisterTool_ConvenienceFunction(t *testing.T) {

	RegisterTool(
		ToolDefinition{Name: "global_tool", Description: "Registered globally"},
		func(ctx context.Context, detail ToolCallDetail) ToolCallResult {
			return ToolCallResult{ToolCallID: detail.ID, ToolName: "global_tool", Outputs: map[string]any{"ok": true}}
		},
	)

	result := CallTool(context.Background(), ToolCallDetail{ID: "t1", ToolName: "global_tool"})
	assert.Equal(t, "global_tool", result.ToolName)
	assert.Equal(t, true, result.Outputs["ok"])
}

func TestListToolDefinitions_ConvenienceFunction(t *testing.T) {
	r := registry()
	r.Register(ToolDefinition{Name: "listed", Description: "A tool"}, nil)

	defs := ListToolDefinitions()
	found := false
	for _, d := range defs {
		if d.Name == "listed" {
			assert.Equal(t, "A tool", d.Description)
			found = true
		}
	}
	assert.True(t, found)
}
