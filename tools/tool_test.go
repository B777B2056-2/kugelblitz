package tools

import (
	"context"
	"errors"
	"testing"

	"kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testTool struct{}

func (t *testTool) Definition() core.ToolDefinition {
	return core.ToolDefinition{Name: "test", Description: "A test tool"}
}

func (t *testTool) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	return OkResult(detail.ID, "test")
}

func TestRegister_AddsToGlobalRegistry(t *testing.T) {
	core.GetToolRegistry().Reset()

	Register(&testTool{})

	defs := core.ListToolDefinitions()
	require.Len(t, defs, 1)
	assert.Equal(t, "test", defs[0].Name)
}

func TestRegisterAll_MultipleTools(t *testing.T) {
	core.GetToolRegistry().Reset()

	t1 := &testTool{}
	t2 := &toolWithName{name: "other", desc: "Another tool"}
	RegisterAll(t1, t2)

	defs := core.ListToolDefinitions()
	require.Len(t, defs, 2)

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	assert.True(t, names["test"])
	assert.True(t, names["other"])
}

type toolWithName struct {
	name, desc string
}

func (t *toolWithName) Definition() core.ToolDefinition {
	return core.ToolDefinition{Name: t.name, Description: t.desc}
}

func (t *toolWithName) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	return OkResult(detail.ID, t.name)
}

func TestErrorResult(t *testing.T) {
	r := ErrorResult("id1", "mytool", errors.New("something went wrong"))
	assert.Equal(t, "id1", r.ToolCallID)
	assert.Equal(t, "mytool", r.ToolName)
	assert.Equal(t, "something went wrong", r.Outputs["error"])
}

func TestSuccessResult(t *testing.T) {
	r := SuccessResult("id1", "mytool", map[string]any{"key": "value"})
	assert.Equal(t, "id1", r.ToolCallID)
	assert.Equal(t, "mytool", r.ToolName)
	assert.Equal(t, "value", r.Outputs["key"])
}

func TestOkResult(t *testing.T) {
	r := OkResult("id1", "mytool")
	assert.Equal(t, true, r.Outputs["ok"])
}

func TestArg_Valid(t *testing.T) {
	detail := core.ToolCallDetail{
		ID:   "id1",
		Args: map[string]any{"path": "/tmp/test.txt"},
	}
	v, err := Arg(detail, "path")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/test.txt", v)
}

func TestArg_Missing(t *testing.T) {
	detail := core.ToolCallDetail{
		ID:   "id1",
		Args: map[string]any{},
	}
	_, err := Arg(detail, "path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument")
}

func TestArg_NotString(t *testing.T) {
	detail := core.ToolCallDetail{
		ID:   "id1",
		Args: map[string]any{"path": 42},
	}
	_, err := Arg(detail, "path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a string")
}

// Compile-time check: testTool implements Tool
var _ Tool = (*testTool)(nil)
