package internals

import (
	"context"
	"testing"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/memory/longterm"
	memorytypes "github.com/B777B2056-2/kugelblitz/memory/types"
	"github.com/B777B2056-2/kugelblitz/persist"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLTMMemtools(t *testing.T) *longterm.LongTermMemory {
	t.Helper()
	ltm, err := longterm.NewLongTermMemory(persist.NewMarkdownPersist(persist.NewFilePersist(t.TempDir())))
	require.NoError(t, err)
	return ltm
}

func TestMemoryStore_StoresFact(t *testing.T) {
	ltm := newTestLTMMemtools(t)

	tool := &MemoryStore{ltm: ltm}
	result := tool.Execute(context.Background(), coretypes.ToolCallDetail{
		ID: "m1", ToolName: "memory_store",
		Args: map[string]any{"section": "prefs", "key": "language", "value": "Go"},
	})
	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, "Go", result.Outputs["value"])
	assert.Equal(t, true, result.Outputs["accepted"])
	assert.InDelta(t, 1.0, result.Outputs["confidence"].(float64), 0.01)
}

func TestMemoryStore_Conflict(t *testing.T) {
	ltm := newTestLTMMemtools(t)
	_, _, _ = ltm.Store("prefs", "lang", "Python")

	tool := &MemoryStore{ltm: ltm}
	result := tool.Execute(context.Background(), coretypes.ToolCallDetail{
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
	ltm := newTestLTMMemtools(t)
	_, _, _ = ltm.Store("prefs", "lang", "Go")
	_, _, _ = ltm.Store("prefs", "editor", "VSCode")

	tool := &MemorySearch{ltm: ltm}
	result := tool.Execute(context.Background(), coretypes.ToolCallDetail{
		ID: "m1", ToolName: "memory_search",
		Args: map[string]any{"query": "lang"},
	})
	results, ok := result.Outputs["results"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, results, 1)
	assert.Equal(t, "Go", results[0]["value"])
	assert.NotNil(t, results[0]["confidence"])
	assert.NotNil(t, results[0]["version"])

	count, ok := result.Outputs["count"].(int)
	require.True(t, ok)
	assert.Equal(t, 1, count)
}

func TestMemoryGetSection_ReturnsAll(t *testing.T) {
	ltm := newTestLTMMemtools(t)
	_, _, _ = ltm.Store("prefs", "a", "1")
	_, _, _ = ltm.Store("prefs", "b", "2")

	tool := &MemoryGetSection{ltm: ltm}
	result := tool.Execute(context.Background(), coretypes.ToolCallDetail{
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

// ---- consumer-side interface proof ----

// fakeMemoryStore implements MemoryStoreBackend with an in-memory map. It
// proves the memory tools depend only on the interface + memory/types, not on
// the concrete *longterm.LongTermMemory.
type fakeMemoryStore struct {
	items map[string]memorytypes.MemoryItem
}

func (f *fakeMemoryStore) Store(section, key, value string) (memorytypes.MemoryItem, *memorytypes.MemoryItem, error) {
	item := memorytypes.MemoryItem{Section: section, Key: key, Value: value, Confidence: 1.0}
	f.items[section+"/"+key] = item
	return item, nil, nil
}

func (f *fakeMemoryStore) GetSection(section string) []memorytypes.MemoryItem {
	var out []memorytypes.MemoryItem
	for _, it := range f.items {
		if it.Section == section {
			out = append(out, it)
		}
	}
	return out
}

func (f *fakeMemoryStore) Remove(section, key string) error {
	delete(f.items, section+"/"+key)
	return nil
}

func (f *fakeMemoryStore) ListSections() map[string]int {
	m := map[string]int{}
	for _, it := range f.items {
		m[it.Section]++
	}
	return m
}

func (f *fakeMemoryStore) Stats() (int, int, float64) {
	return len(f.items), len(f.ListSections()), 0
}

func (f *fakeMemoryStore) SearchWithMode(query string, mode persist.SearchMode) []memorytypes.MemoryItem {
	return nil
}

func (f *fakeMemoryStore) Graph() *longterm.GraphStore { return nil }

func TestMemoryStore_DependsOnlyOnInterface(t *testing.T) {
	fake := &fakeMemoryStore{items: map[string]memorytypes.MemoryItem{}}
	tool := &MemoryStore{ltm: fake}

	result := tool.Execute(context.Background(), coretypes.ToolCallDetail{
		ID: "m1", ToolName: "memory_store",
		Args: map[string]any{"section": "prefs", "key": "language", "value": "Go"},
	})

	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, "Go", result.Outputs["value"])
	assert.Equal(t, true, result.Outputs["accepted"])
	assert.InDelta(t, 1.0, result.Outputs["confidence"].(float64), 0.01)
}
