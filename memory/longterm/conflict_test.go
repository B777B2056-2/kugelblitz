package longterm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConflictResolver_NoConflict_NewFact(t *testing.T) {
	ltm := &LongTermMemory{}
	cr := NewConflictResolver(ltm, 0.15)

	candidates := []MemoryItemCandidate{
		{Section: "prefs", Key: "lang", Value: "Go", SuggestedConfidence: 0.9},
	}
	stored, pending := cr.Resolve(candidates)
	assert.Len(t, stored, 1)
	assert.Equal(t, "Go", stored[0].Value)
	assert.Equal(t, 1, stored[0].Version)
	assert.Empty(t, pending)
}

func TestConflictResolver_SemanticMatch_NoConflict(t *testing.T) {
	ltm := &LongTermMemory{}
	ltm.Store("prefs", "lang", "Go")
	// Simulate semantic match
	oldJudge := semanticJudge
	semanticJudge = func(old, new string) bool { return true }
	defer func() { semanticJudge = oldJudge }()

	cr := NewConflictResolver(ltm, 0.15)
	candidates := []MemoryItemCandidate{
		{Section: "prefs", Key: "lang", Value: "Golang", SuggestedConfidence: 0.9},
	}
	stored, pending := cr.Resolve(candidates)
	assert.Len(t, stored, 1)
	assert.Equal(t, "Golang", stored[0].Value) // newer phrasing wins
	assert.Empty(t, pending)
}

func TestConflictResolver_ClearWinner_NewWins(t *testing.T) {
	ltm := &LongTermMemory{}
	// Store old fact with low confidence
	ltm.items = append(ltm.items, MemoryItem{
		Section: "prefs", Key: "lang", Value: "Python",
		Version: 1, Confidence: 0.3, UpdatedAt: ltmTimeNow(),
	})

	cr := NewConflictResolver(ltm, 0.15)
	candidates := []MemoryItemCandidate{
		{Section: "prefs", Key: "lang", Value: "Go", SuggestedConfidence: 0.9},
	}
	stored, pending := cr.Resolve(candidates)
	assert.Len(t, stored, 1)
	assert.Equal(t, "Go", stored[0].Value) // new wins
	assert.Empty(t, pending)
}

func TestConflictResolver_ClearWinner_OldWins(t *testing.T) {
	ltm := &LongTermMemory{}
	// Store old fact with very high confidence
	ltm.items = append(ltm.items, MemoryItem{
		Section: "prefs", Key: "lang", Value: "Python",
		Version: 1, Confidence: 0.95, UpdatedAt: ltmTimeNow(),
	})

	cr := NewConflictResolver(ltm, 0.15)
	candidates := []MemoryItemCandidate{
		{Section: "prefs", Key: "lang", Value: "Go", SuggestedConfidence: 0.3},
	}
	stored, pending := cr.Resolve(candidates)
	assert.Len(t, stored, 1)
	assert.Equal(t, "Python", stored[0].Value) // old wins
	assert.Empty(t, pending)
}

func TestConflictResolver_CloseGap_NeedsHuman(t *testing.T) {
	ltm := &LongTermMemory{}
	// Old: 0.6, New: 0.7 — gap 0.1 < 0.15
	ltm.items = append(ltm.items, MemoryItem{
		Section: "prefs", Key: "lang", Value: "Python",
		Version: 1, Confidence: 0.6, UpdatedAt: ltmTimeNow(),
	})

	cr := NewConflictResolver(ltm, 0.15)
	candidates := []MemoryItemCandidate{
		{Section: "prefs", Key: "lang", Value: "Go", SuggestedConfidence: 0.7},
	}
	stored, pending := cr.Resolve(candidates)
	assert.Empty(t, stored)
	assert.Len(t, pending, 1)
	assert.Equal(t, "Python", pending[0].OldValue)
	assert.Equal(t, "Go", pending[0].NewValue)
}

func TestConflictResolver_MultipleCandidates(t *testing.T) {
	ltm := &LongTermMemory{}
	cr := NewConflictResolver(ltm, 0.15)

	candidates := []MemoryItemCandidate{
		{Section: "prefs", Key: "lang", Value: "Go", SuggestedConfidence: 0.9},
		{Section: "prefs", Key: "editor", Value: "VSCode", SuggestedConfidence: 0.8},
		{Section: "items", Key: "deploy", Value: "prod", SuggestedConfidence: 0.95},
	}
	stored, pending := cr.Resolve(candidates)
	assert.Len(t, stored, 3)
	assert.Empty(t, pending)
}

func TestConflictResolver_SameConfidence_NeedsHuman(t *testing.T) {
	ltm := &LongTermMemory{}
	ltm.items = append(ltm.items, MemoryItem{
		Section: "prefs", Key: "lang", Value: "Python",
		Version: 1, Confidence: 0.8, UpdatedAt: ltmTimeNow(),
	})

	cr := NewConflictResolver(ltm, 0.15)
	candidates := []MemoryItemCandidate{
		{Section: "prefs", Key: "lang", Value: "Go", SuggestedConfidence: 0.8},
	}
	stored, pending := cr.Resolve(candidates)
	// Gap = 0 → < 0.15 → needs human
	assert.Empty(t, stored)
	assert.Len(t, pending, 1)
}

func TestConflictResolver_DefaultGap(t *testing.T) {
	cr := NewConflictResolver(&LongTermMemory{}, 0)
	assert.Equal(t, 0.15, cr.confidenceGap) // defaults to 0.15
}

func TestResolveResult_AcceptNewWins(t *testing.T) {
	cr := NewConflictResolver(&LongTermMemory{}, 0.15)
	result := cr.resolveOne(MemoryItemCandidate{
		Section: "prefs", Key: "lang", Value: "Go", SuggestedConfidence: 0.9,
	})
	assert.Equal(t, ConflictAcceptNew, result.Decision)
	assert.Equal(t, "Go", result.Winner.Value)
	require.NotNil(t, result.Winner)
	assert.Equal(t, 1, result.Winner.Version)
}

func ltmTimeNow() time.Time {
	return time.Now()
}
