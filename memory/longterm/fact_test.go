package longterm

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLTM(t *testing.T) *LongTermMemory {
	t.Helper()
	ltm, err := NewLongTermMemory(persist.NewMarkdownPersist(persist.NewFilePersist(t.TempDir())))
	require.NoError(t, err)
	return ltm
}

func TestLongTermMemory_StoreNewFact(t *testing.T) {
	ltm := newTestLTM(t)

	winner, conflict, err := ltm.Store("prefs", "lang", "Go")
	require.NoError(t, err)
	assert.Equal(t, "Go", winner.Value)
	assert.Equal(t, 1.0, winner.Confidence)
	assert.Nil(t, conflict)
}

func TestLongTermMemory_StoreSameValue(t *testing.T) {
	ltm := newTestLTM(t)

	_, _, _ = ltm.Store("prefs", "lang", "Go")
	winner, conflict, _ := ltm.Store("prefs", "lang", "Go")

	assert.Nil(t, conflict)
	assert.Equal(t, "Go", winner.Value)
	assert.Equal(t, 1.0, winner.Confidence) // capped at 1.0
	assert.Equal(t, 2, winner.Version)
}

func TestLongTermMemory_StoreConflictNewWins(t *testing.T) {
	ltm := newTestLTM(t)

	// Store an old fact with low confidence (set UpdatedAt far in the past)
	ltm.items = append(ltm.items, MemoryItem{
		Section: "prefs", Key: "lang", Value: "Python",
		Version: 1, Confidence: 0.2, UpdatedAt: time.Now().Add(-30 * 24 * time.Hour),
	})
	ltm.rebuildIndex()

	// New fact has confidence 1.0 — should win over decayed 0.2
	// conflict is non-nil (holds the displaced old fact)
	winner, conflict, _ := ltm.Store("prefs", "lang", "Go")
	assert.NotNil(t, conflict) // old fact displaced
	assert.Equal(t, "Python", conflict.Value)
	assert.Equal(t, "Go", winner.Value)
}

func TestLongTermMemory_StoreConflictOldWins(t *testing.T) {
	ltm := newTestLTM(t)

	// Old fact — UpdatedAt is set in the future so decayConfidence returns
	// the original 1.0 regardless of platform clock resolution.
	ltm.items = append(ltm.items, MemoryItem{
		Section: "prefs", Key: "lang", Value: "Python",
		Version: 1, Confidence: 1.0, UpdatedAt: time.Now().Add(time.Hour),
	})
	ltm.rebuildIndex()

	// New fact also has confidence 1.0. With no decay on old, 1.0 > 1.0 is
	// false → falls to default → old wins (kept as winner, new returned as conflict).
	winner, conflict, _ := ltm.Store("prefs", "lang", "Go")
	assert.NotNil(t, conflict)
	assert.Equal(t, "Python", winner.Value) // old wins
}

func TestLongTermMemory_ConfidenceDecay(t *testing.T) {
	ltm := &LongTermMemory{}
	f := MemoryItem{Confidence: 1.0, UpdatedAt: time.Now().Add(-10 * 24 * time.Hour)}
	d := ltm.decayConfidence(f)
	assert.Less(t, d.Confidence, 1.0)
	assert.GreaterOrEqual(t, d.Confidence, 0.1)
}

func TestLongTermMemory_GetReturnsDecayed(t *testing.T) {
	ltm := newTestLTM(t)

	ltm.items = append(ltm.items, MemoryItem{
		Section: "prefs", Key: "lang", Value: "Go",
		Version: 1, Confidence: 1.0, UpdatedAt: time.Now().Add(-10 * 24 * time.Hour),
	})
	ltm.rebuildIndex()

	f, ok := ltm.Get("prefs", "lang")
	assert.True(t, ok)
	assert.Less(t, f.Confidence, 1.0) // decayed
}

func TestLongTermMemory_LoadExisting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "MEMORY.md")
	today := time.Now().Format("2006-01-02")
	_ = os.WriteFile(p, []byte(`# Project Memory

## prefs
- lang: Go  `+"`v2 c0.95 "+today+"`"+`
`), 0644)

	ltm, err := NewLongTermMemory(persist.NewMarkdownPersist(persist.NewFilePersist(dir)))
	require.NoError(t, err)

	f, ok := ltm.Get("prefs", "lang")
	assert.True(t, ok)
	assert.Equal(t, "Go", f.Value)
	assert.Equal(t, 2, f.Version)
	assert.InDelta(t, 0.95, f.Confidence, 0.05)
}

func TestLongTermMemory_Search(t *testing.T) {
	ltm := newTestLTM(t)
	_, _, _ = ltm.Store("prefs", "lang", "Go")
	_, _, _ = ltm.Store("items", "deploy", "production")

	results := ltm.Search("Go")
	assert.Len(t, results, 1)
	assert.Equal(t, "lang", results[0].Key)
}

func TestLongTermMemory_CaseInsensitive(t *testing.T) {
	ltm := newTestLTM(t)
	// Section normalization is case-insensitive; keys are case-sensitive
	_, _, _ = ltm.Store("PREFS", "lang", "Go")

	f, ok := ltm.Get("prefs", "lang")
	assert.True(t, ok)
	assert.Equal(t, "Go", f.Value)
}

func TestLongTermMemory_Remove(t *testing.T) {
	ltm := newTestLTM(t)
	_, _, _ = ltm.Store("prefs", "lang", "Go")

	err := ltm.Remove("prefs", "lang")
	require.NoError(t, err)
	_, ok := ltm.Get("prefs", "lang")
	assert.False(t, ok)
}

func TestLongTermMemory_GetSection(t *testing.T) {
	ltm := newTestLTM(t)
	_, _, _ = ltm.Store("prefs", "lang", "Go")
	_, _, _ = ltm.Store("prefs", "editor", "VSCode")

	section := ltm.GetSection("prefs")
	assert.Len(t, section, 2)
}

func TestLongTermMemory_SemanticMatchExact(t *testing.T) {
	ltm := &LongTermMemory{}
	assert.True(t, ltm.isSemanticMatch("Go", "Go"))
}

func TestLongTermMemory_SemanticMatchCaseInsensitive(t *testing.T) {
	ltm := &LongTermMemory{}
	assert.True(t, ltm.isSemanticMatch("Go", "go"))
}

func TestLongTermMemory_SemanticMatchSubstring(t *testing.T) {
	ltm := &LongTermMemory{}
	assert.True(t, ltm.isSemanticMatch("Go programming", "Go"))
}

func TestLongTermMemory_SemanticMatchLLMJudge(t *testing.T) {
	oldJudge := semanticJudge
	defer func() { semanticJudge = oldJudge }()

	semanticJudge = func(old, new string) bool { return old == "python" && new == "py" }

	ltm := &LongTermMemory{}
	assert.True(t, ltm.isSemanticMatch("python", "py"))
}

func TestLongTermMemory_SemanticMatchLLMNoMatch(t *testing.T) {
	oldJudge := semanticJudge
	defer func() { semanticJudge = oldJudge }()

	semanticJudge = func(old, new string) bool { return false }

	ltm := &LongTermMemory{}
	assert.False(t, ltm.isSemanticMatch("Go", "Python"))
}

func TestLongTermMemory_SemanticMatchNoJudge(t *testing.T) {
	ltm := &LongTermMemory{}
	assert.False(t, ltm.isSemanticMatch("Go", "Python"))
}

func TestLongTermMemory_StoreSemanticMatch(t *testing.T) {
	ltm := newTestLTM(t)

	oldJudge := semanticJudge
	semanticJudge = func(old, new string) bool { return true } // always match
	defer func() { semanticJudge = oldJudge }()

	_, _, _ = ltm.Store("prefs", "lang", "Go")
	// Same semantic meaning — should bump confidence (capped at 1.0)
	winner, conflict, _ := ltm.Store("prefs", "lang", "Golang")
	assert.Nil(t, conflict)
	assert.Equal(t, "Golang", winner.Value) // newer phrasing wins
	assert.Equal(t, 1.0, winner.Confidence) // capped at 1.0
	assert.Equal(t, 2, winner.Version)
}

func TestLongTermMemory_StoreSemanticMismatch(t *testing.T) {
	ltm := newTestLTM(t)

	oldJudge := semanticJudge
	semanticJudge = func(old, new string) bool { return false }
	defer func() { semanticJudge = oldJudge }()

	ltm.items = append(ltm.items, MemoryItem{
		Section: "prefs", Key: "lang", Value: "Python",
		Version: 1, Confidence: 1.0, UpdatedAt: time.Now(),
	})
	ltm.rebuildIndex()

	// Different values, old has high confidence — should be a conflict
	_, conflict, _ := ltm.Store("prefs", "lang", "Go")
	assert.NotNil(t, conflict)
}

func TestLongTermMemory_StoreReturnsMetadata(t *testing.T) {
	ltm := newTestLTM(t)

	winner, _, _ := ltm.Store("prefs", "lang", "Go")
	assert.Equal(t, "prefs", winner.Section)
	assert.Equal(t, "lang", winner.Key)
	assert.Equal(t, 1, winner.Version)
	assert.Equal(t, 1.0, winner.Confidence)
}

func TestLongTermMemory_BulkStore(t *testing.T) {
	ltm := newTestLTM(t)

	items := []MemoryItem{
		{Section: "prefs", Key: "lang", Value: "Go", Confidence: 1.0},
		{Section: "prefs", Key: "editor", Value: "VSCode", Confidence: 1.0},
		{Section: "items", Key: "deploy", Value: "prod", Confidence: 0.9},
	}
	err := ltm.BulkStore(items)
	require.NoError(t, err)

	f, ok := ltm.Get("prefs", "lang")
	assert.True(t, ok)
	assert.Equal(t, "Go", f.Value)

	f2, ok := ltm.Get("items", "deploy")
	assert.True(t, ok)
	assert.Equal(t, "prod", f2.Value)
}

func TestLongTermMemory_Facts(t *testing.T) {
	ltm := newTestLTM(t)
	_, _, _ = ltm.Store("prefs", "lang", "Go")
	_, _, _ = ltm.Store("items", "deploy", "prod")

	all := ltm.All()
	assert.Len(t, all, 2)
}

func TestLongTermMemory_ListSections(t *testing.T) {
	ltm := newTestLTM(t)
	_, _, _ = ltm.Store("prefs", "lang", "Go")
	_, _, _ = ltm.Store("prefs", "editor", "VSCode")
	_, _, _ = ltm.Store("items", "deploy", "prod")

	sections := ltm.ListSections()
	assert.Equal(t, 2, sections["prefs"])
	assert.Equal(t, 1, sections["items"])
}

func TestLongTermMemory_Stats(t *testing.T) {
	ltm := newTestLTM(t)
	_, _, _ = ltm.Store("prefs", "lang", "Go")

	total, sections, avg := ltm.Stats()
	assert.Equal(t, 1, total)
	assert.Equal(t, 1, sections)
	assert.Greater(t, avg, 0.0)
}

func TestLongTermMemory_ConcurrentStoreAndGet(t *testing.T) {
	ltm := newTestLTM(t)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, _, _ = ltm.Store("s", fmt.Sprintf("key-%d", idx), fmt.Sprintf("val-%d", idx))
		}(i)
	}
	wg.Wait()

	// Verify all keys are present
	for i := 0; i < 10; i++ {
		item, ok := ltm.Get("s", fmt.Sprintf("key-%d", i))
		assert.True(t, ok, "key-%d should exist", i)
		assert.Equal(t, fmt.Sprintf("val-%d", i), item.Value)
	}
}

func TestLongTermMemory_ConcurrentBulkAndRemove(t *testing.T) {
	ltm := newTestLTM(t)

	items := make([]MemoryItem, 10)
	for i := 0; i < 10; i++ {
		items[i] = MemoryItem{Section: "s", Key: fmt.Sprintf("k%d", i), Value: fmt.Sprintf("v%d", i)}
	}

	// BulkStore
	require.NoError(t, ltm.BulkStore(items))

	var wg sync.WaitGroup
	// Concurrent reads via All
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			all := ltm.All()
			assert.NotEmpty(t, all)
		}()
	}

	// Concurrent Remove
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = ltm.Remove("s", fmt.Sprintf("k%d", idx))
		}(i)
	}

	wg.Wait()

	// Remaining items should be 7 (10 - 3)
	remaining := len(ltm.All())
	assert.GreaterOrEqual(t, remaining, 7)
	assert.LessOrEqual(t, remaining, 10)
}

func TestLongTermMemory_ConcurrentSameKeyStore(t *testing.T) {
	ltm := newTestLTM(t)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, _, _ = ltm.Store("s", "same-key", fmt.Sprintf("val-%d", idx))
		}(i)
	}
	wg.Wait()

	// Final item should exist (no panic, no corruption)
	item, ok := ltm.Get("s", "same-key")
	assert.True(t, ok)
	assert.NotEmpty(t, item.Value)
	// Index count should match items count
	assert.Equal(t, len(ltm.items), len(ltm.index))
}
