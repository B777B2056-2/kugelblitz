package memory

import (
	"os"
	"testing"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withTempWorkspace(t *testing.T) func() {
	ws := core.GetWorkspace()
	old := ws.Dir()
	ws.SetDir(t.TempDir())
	return func() { ws.SetDir(old) }
}

func TestLongTermMemory_StoreNewFact(t *testing.T) {
	defer withTempWorkspace(t)()
	ltm, _ := NewLongTermMemory()

	winner, conflict, err := ltm.Store("prefs", "language", "Go")
	require.NoError(t, err)
	assert.Equal(t, "Go", winner.Value)
	assert.Equal(t, 1, winner.Version)
	assert.InDelta(t, 1.0, winner.Confidence, 0.01)
	assert.Nil(t, conflict)
}

func TestLongTermMemory_StoreSameValue(t *testing.T) {
	defer withTempWorkspace(t)()
	ltm, _ := NewLongTermMemory()

	ltm.Store("facts", "v", "1.0")
	winner, conflict, _ := ltm.Store("facts", "v", "1.0")

	assert.Equal(t, "1.0", winner.Value)
	assert.Greater(t, winner.Version, 1)
	assert.Nil(t, conflict)
}

func TestLongTermMemory_StoreConflictNewWins(t *testing.T) {
	defer withTempWorkspace(t)()
	ltm, _ := NewLongTermMemory()

	// Store old fact, then artificially decay it
	ltm.Store("facts", "v", "old")
	// Simulate time passing by setting low confidence
	ltm.mu.Lock()
	ltm.facts[0].Confidence = 0.1
	ltm.facts[0].UpdatedAt = time.Now().Add(-30 * 24 * time.Hour)
	ltm.mu.Unlock()

	// New fact should win (confidence 1.0 vs 0.1)
	winner, conflict, _ := ltm.Store("facts", "v", "new")
	assert.Equal(t, "new", winner.Value)
	assert.NotNil(t, conflict)
	assert.Equal(t, "old", conflict.Value)
}

func TestLongTermMemory_StoreConflictOldWins(t *testing.T) {
	defer withTempWorkspace(t)()
	ltm, _ := NewLongTermMemory()

	// Store old fact, then artificially boost its confidence
	ltm.Store("facts", "v", "old")
	ltm.mu.Lock()
	ltm.facts[0].Confidence = 1.5 // higher than any fresh fact (1.0)
	ltm.mu.Unlock()

	winner, conflict, _ := ltm.Store("facts", "v", "new")
	assert.Equal(t, "old", winner.Value)
	assert.NotNil(t, conflict)
	assert.Equal(t, "new", conflict.Value)
}

func TestLongTermMemory_ConfidenceDecay(t *testing.T) {
	ltm := &LongTermMemory{}
	f := Fact{Confidence: 1.0, UpdatedAt: time.Now().Add(-10 * 24 * time.Hour)}
	d := ltm.decayConfidence(f)
	assert.Less(t, d.Confidence, 1.0)
	assert.Greater(t, d.Confidence, 0.4)
}

func TestLongTermMemory_GetReturnsDecayed(t *testing.T) {
	defer withTempWorkspace(t)()
	ltm, _ := NewLongTermMemory()

	ltm.Store("prefs", "lang", "Go")
	// Simulate old fact
	ltm.mu.Lock()
	ltm.facts[0].UpdatedAt = time.Now().Add(-30 * 24 * time.Hour)
	ltm.mu.Unlock()

	f, ok := ltm.Get("prefs", "lang")
	assert.True(t, ok)
	assert.Less(t, f.Confidence, 1.0)
}

func TestLongTermMemory_LoadExisting(t *testing.T) {
	defer withTempWorkspace(t)()

	content := `# Project Memory

## prefs
- lang: Go  ` + "`v2 c0.95 2026-06-20`" + `

## facts
- deploy: production  ` + "`v1 c1.0 2026-06-21`" + `
`
	require.NoError(t, os.WriteFile(memoryPath(), []byte(content), 0644))

	ltm, _ := NewLongTermMemory()
	f, ok := ltm.Get("prefs", "lang")
	assert.True(t, ok)
	assert.Equal(t, "Go", f.Value)
	assert.Equal(t, 2, f.Version)
	assert.Greater(t, f.Confidence, 0.0) // may be decayed

	f, ok = ltm.Get("facts", "deploy")
	assert.True(t, ok)
	assert.Equal(t, "production", f.Value)
	assert.GreaterOrEqual(t, f.Version, 1)
}

func TestLongTermMemory_Search(t *testing.T) {
	defer withTempWorkspace(t)()
	ltm, _ := NewLongTermMemory()

	ltm.Store("prefs", "lang", "Go")
	ltm.Store("prefs", "editor", "VSCode")

	results := ltm.Search("Go")
	require.Len(t, results, 1)
	assert.Equal(t, "Go", results[0].Value)
}

func TestLongTermMemory_CaseInsensitive(t *testing.T) {
	defer withTempWorkspace(t)()
	ltm, _ := NewLongTermMemory()

	ltm.Store("MySection", "key", "value")
	f, ok := ltm.Get("mysection", "key")
	assert.True(t, ok)
	assert.Equal(t, "value", f.Value)
}

func TestLongTermMemory_Remove(t *testing.T) {
	defer withTempWorkspace(t)()
	ltm, _ := NewLongTermMemory()

	ltm.Store("facts", "k1", "v1")
	ltm.Store("facts", "k2", "v2")
	require.NoError(t, ltm.Remove("facts", "k1"))

	_, ok := ltm.Get("facts", "k1")
	assert.False(t, ok)
}

func TestLongTermMemory_GetSection(t *testing.T) {
	defer withTempWorkspace(t)()
	ltm, _ := NewLongTermMemory()

	ltm.Store("prefs", "lang", "Go")
	ltm.Store("prefs", "os", "linux")

	facts := ltm.GetSection("prefs")
	require.Len(t, facts, 2)
}

func TestIsSemanticMatch_Exact(t *testing.T) {
	ltm := &LongTermMemory{}
	assert.True(t, ltm.isSemanticMatch("Go", "Go"))
}

func TestIsSemanticMatch_Normalized(t *testing.T) {
	ltm := &LongTermMemory{}
	assert.True(t, ltm.isSemanticMatch(" Go ", "go"))
	assert.True(t, ltm.isSemanticMatch("PRODUCTION", "production"))
}

func TestIsSemanticMatch_Substring(t *testing.T) {
	ltm := &LongTermMemory{}
	assert.True(t, ltm.isSemanticMatch("用户偏好Go语言", "Go"))
	assert.True(t, ltm.isSemanticMatch("Golang", "Golang语言"))
}

func TestIsSemanticMatch_LLMJudge(t *testing.T) {
	oldJudge := semanticJudge
	defer func() { semanticJudge = oldJudge }()

	SetSemanticJudge(func(old, new string) bool {
		return true // mock: always says YES
	})

	ltm := &LongTermMemory{}
	assert.True(t, ltm.isSemanticMatch("Go", "Golang"))
}

func TestIsSemanticMatch_LLMJudgeNoMatch(t *testing.T) {
	oldJudge := semanticJudge
	defer func() { semanticJudge = oldJudge }()

	SetSemanticJudge(func(old, new string) bool {
		return false
	})

	ltm := &LongTermMemory{}
	assert.False(t, ltm.isSemanticMatch("Go", "Python"))
}

func TestIsSemanticMatch_NoMatch(t *testing.T) {
	ltm := &LongTermMemory{}
	assert.False(t, ltm.isSemanticMatch("Go", "Python"))
}

func TestStore_SemanticMatchSkipsConflict(t *testing.T) {
	defer withTempWorkspace(t)()
	ltm, _ := NewLongTermMemory()

	oldJudge := semanticJudge
	semanticJudge = func(old, new string) bool { return true } // always match
	defer func() { semanticJudge = oldJudge }()

	ltm.Store("prefs", "lang", "Go")
	winner, conflict, _ := ltm.Store("prefs", "lang", "Golang语言")

	assert.Equal(t, "Golang语言", winner.Value) // treated as same, confidence bumped
	assert.Nil(t, conflict, "should not flag conflict for semantic match")
}

func TestStore_NoSemanticMatch_FlagsConflict(t *testing.T) {
	defer withTempWorkspace(t)()
	ltm, _ := NewLongTermMemory()

	oldJudge := semanticJudge
	semanticJudge = func(old, new string) bool { return false }
	defer func() { semanticJudge = oldJudge }()

	ltm.Store("prefs", "lang", "Go")
	// Manually lower confidence so new can win
	ltm.mu.Lock()
	ltm.facts[0].Confidence = 0.1
	ltm.mu.Unlock()

	winner, conflict, _ := ltm.Store("prefs", "lang", "Python")
	assert.Equal(t, "Python", winner.Value)
	assert.NotNil(t, conflict)
	assert.Equal(t, "Go", conflict.Value)
}

func TestLongTermMemory_StoreReturnsMetadata(t *testing.T) {
	defer withTempWorkspace(t)()
	ltm, _ := NewLongTermMemory()

	winner, _, _ := ltm.Store("facts", "v", "1.0")
	assert.NotZero(t, winner.UpdatedAt)
}
