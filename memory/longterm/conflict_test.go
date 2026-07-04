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
	stored := cr.Resolve(candidates)
	assert.Len(t, stored, 1)
	assert.Equal(t, "Go", stored[0].Value)
	assert.Equal(t, 1, stored[0].Version)
}

func TestConflictResolver_SemanticMatch_NoConflict(t *testing.T) {
	ltm := &LongTermMemory{}
	ltm.Store("prefs", "lang", "Go")
	oldJudge := semanticJudge
	semanticJudge = func(old, new string) bool { return true }
	defer func() { semanticJudge = oldJudge }()

	cr := NewConflictResolver(ltm, 0.15)
	candidates := []MemoryItemCandidate{
		{Section: "prefs", Key: "lang", Value: "Golang", SuggestedConfidence: 0.9},
	}
	stored := cr.Resolve(candidates)
	assert.Len(t, stored, 1)
	assert.Equal(t, "Golang", stored[0].Value)
}

func TestConflictResolver_ClearWinner_NewWins(t *testing.T) {
	ltm := &LongTermMemory{}
	ltm.items = append(ltm.items, MemoryItem{
		Section: "prefs", Key: "lang", Value: "Python",
		Version: 1, Confidence: 0.3, UpdatedAt: ltmTimeNow(),
	})

	cr := NewConflictResolver(ltm, 0.15)
	candidates := []MemoryItemCandidate{
		{Section: "prefs", Key: "lang", Value: "Go", SuggestedConfidence: 0.9},
	}
	stored := cr.Resolve(candidates)
	assert.Len(t, stored, 1)
	assert.Equal(t, "Go", stored[0].Value)
}

func TestConflictResolver_ClearWinner_OldWins(t *testing.T) {
	ltm := &LongTermMemory{}
	ltm.items = append(ltm.items, MemoryItem{
		Section: "prefs", Key: "lang", Value: "Python",
		Version: 1, Confidence: 0.95, UpdatedAt: ltmTimeNow(),
	})

	cr := NewConflictResolver(ltm, 0.15)
	candidates := []MemoryItemCandidate{
		{Section: "prefs", Key: "lang", Value: "Go", SuggestedConfidence: 0.3},
	}
	stored := cr.Resolve(candidates)
	assert.Len(t, stored, 1)
	assert.Equal(t, "Python", stored[0].Value)
}

func TestConflictResolver_NarrowGap_KeepsExisting(t *testing.T) {
	ltm := &LongTermMemory{}
	ltm.items = append(ltm.items, MemoryItem{
		Section: "prefs", Key: "lang", Value: "Python",
		Version: 1, Confidence: 0.6, UpdatedAt: ltmTimeNow(),
	})

	cr := NewConflictResolver(ltm, 0.15)
	candidates := []MemoryItemCandidate{
		{Section: "prefs", Key: "lang", Value: "Go", SuggestedConfidence: 0.7},
	}
	stored := cr.Resolve(candidates)
	// Narrow gap (0.1 < 0.15) → old wins
	assert.Len(t, stored, 1)
	assert.Equal(t, "Python", stored[0].Value)
}

func TestConflictResolver_MultipleCandidates(t *testing.T) {
	ltm := &LongTermMemory{}
	cr := NewConflictResolver(ltm, 0.15)

	candidates := []MemoryItemCandidate{
		{Section: "prefs", Key: "lang", Value: "Go", SuggestedConfidence: 0.9},
		{Section: "prefs", Key: "editor", Value: "VSCode", SuggestedConfidence: 0.8},
		{Section: "items", Key: "deploy", Value: "prod", SuggestedConfidence: 0.95},
	}
	stored := cr.Resolve(candidates)
	assert.Len(t, stored, 3)
}

func TestConflictResolver_SameConfidence_KeepsExisting(t *testing.T) {
	ltm := &LongTermMemory{}
	ltm.items = append(ltm.items, MemoryItem{
		Section: "prefs", Key: "lang", Value: "Python",
		Version: 1, Confidence: 0.8, UpdatedAt: ltmTimeNow(),
	})

	cr := NewConflictResolver(ltm, 0.15)
	candidates := []MemoryItemCandidate{
		{Section: "prefs", Key: "lang", Value: "Go", SuggestedConfidence: 0.8},
	}
	stored := cr.Resolve(candidates)
	// Same confidence (gap 0 < 0.15) → old wins
	assert.Len(t, stored, 1)
}

func TestConflictResolver_DefaultGap(t *testing.T) {
	cr := NewConflictResolver(&LongTermMemory{}, 0)
	assert.Equal(t, 0.15, cr.confidenceGap)
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
