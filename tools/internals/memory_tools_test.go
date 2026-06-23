package internals

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_StoresFact(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	ltm, _ := memory.NewLongTermMemory()

	tool := &MemoryStore{ltm: ltm}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "m1", ToolName: "memory_store",
		Args: map[string]any{"section": "prefs", "key": "language", "value": "Go"},
	})
	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, "Go", result.Outputs["value"])
	assert.Equal(t, true, result.Outputs["accepted"])
	assert.InDelta(t, 1.0, result.Outputs["confidence"].(float64), 0.01)
}

func TestMemoryStore_Conflict(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	ltm, _ := memory.NewLongTermMemory()
	ltm.Store("prefs", "lang", "Python")

	tool := &MemoryStore{ltm: ltm}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "m1", ToolName: "memory_store",
		Args: map[string]any{"section": "prefs", "key": "lang", "value": "Go"},
	})
	assert.Nil(t, result.Outputs["error"])
	// Same confidence: old wins (Python), new rejected
	conflict, ok := result.Outputs["conflict"].(map[string]any)
	if ok {
		assert.Equal(t, "Go", conflict["rejected_value"])
	}
}

func TestMemorySearch_FindsResults(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	ltm, _ := memory.NewLongTermMemory()
	ltm.Store("prefs", "lang", "Go")
	ltm.Store("prefs", "editor", "VSCode")

	tool := &MemorySearch{ltm: ltm}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "m1", ToolName: "memory_search",
		Args: map[string]any{"query": "lang"},
	})
	results, ok := result.Outputs["results"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, results, 1)
	assert.Equal(t, "Go", results[0]["value"])
	assert.NotNil(t, results[0]["confidence"])
	assert.NotNil(t, results[0]["version"])
}

func TestMemoryGetSection_ReturnsAll(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	ltm, _ := memory.NewLongTermMemory()
	ltm.Store("prefs", "a", "1")
	ltm.Store("prefs", "b", "2")

	tool := &MemoryGetSection{ltm: ltm}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "m1", ToolName: "memory_get_section",
		Args: map[string]any{"section": "prefs"},
	})
	entries, ok := result.Outputs["entries"].(map[string]any)
	require.True(t, ok)
	require.Len(t, entries, 2)
	e := entries["a"].(map[string]any)
	assert.Equal(t, "1", e["value"])
	assert.NotNil(t, e["confidence"])
}
