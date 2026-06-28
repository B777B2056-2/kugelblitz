package longterm

import (
	"fmt"
	"math"
	"time"
)

// ConflictDecision encodes the result of a conflict resolution.
type ConflictDecision string

const (
	ConflictAcceptNew    ConflictDecision = "accept_new"
	ConflictKeepExisting ConflictDecision = "keep_existing"
	ConflictNeedsHuman   ConflictDecision = "needs_human"
)

// PendingConflict is a conflict that needs human review.
type PendingConflict struct {
	Section      string  `json:"section"`
	Key          string  `json:"key"`
	OldValue     string  `json:"old_value"`
	OldConfidence float64 `json:"old_confidence"`
	NewValue     string  `json:"new_value"`
	NewConfidence float64 `json:"new_confidence"`
	Reason       string  `json:"reason"`
}

// ConflictResolver resolves conflicts between extracted items and existing LTM items.
// When confidence gap is narrow (< confidenceGap), conflicts are deferred for human review.
type ConflictResolver struct {
	ltm           *LongTermMemory
	confidenceGap float64 // when |new - old| < this, defer to human (default 0.15)
}

// NewConflictResolver creates a resolver for the given LTM.
func NewConflictResolver(ltm *LongTermMemory, confidenceGap float64) *ConflictResolver {
	if confidenceGap <= 0 {
		confidenceGap = 0.15
	}
	return &ConflictResolver{ltm: ltm, confidenceGap: confidenceGap}
}

// ResolveResult captures the outcome of resolving a single fact candidate against existing LTM.
type ResolveResult struct {
	Candidate   MemoryItemCandidate
	Decision    ConflictDecision
	Winner      MemoryItem      // The winning fact (for accept_new or keep_existing)
	OldFact     *MemoryItem     // The displaced old fact, if new wins
	PendingConflict *PendingConflict // Set if decision is needs_human
}

// Resolve processes a batch of fact candidates against existing LTM items.
// Returns resolved items (ready to store) and pending conflicts (need human).
func (cr *ConflictResolver) Resolve(candidates []MemoryItemCandidate) ([]MemoryItem, []PendingConflict) {
	var stored []MemoryItem
	var pending []PendingConflict

	for _, c := range candidates {
		result := cr.resolveOne(c)
		switch result.Decision {
		case ConflictAcceptNew, ConflictKeepExisting:
			stored = append(stored, result.Winner)
		case ConflictNeedsHuman:
			pending = append(pending, *result.PendingConflict)
		}
	}
	return stored, pending
}

// resolveOne resolves a single fact candidate.
func (cr *ConflictResolver) resolveOne(c MemoryItemCandidate) ResolveResult {
	// Look up existing fact
	existing, exists := cr.ltm.Get(c.Section, c.Key)

	if !exists {
		// No existing fact — accept directly
		now := time.Now()
		return ResolveResult{
			Candidate: c,
			Decision:  ConflictAcceptNew,
			Winner: MemoryItem{
				Section:    c.Section,
				Key:        c.Key,
				Value:      c.Value,
				Version:    1,
				Confidence: clampConfidence(c.SuggestedConfidence),
				UpdatedAt:  now,
			},
		}
	}

	// Semantic match check — same fact, different wording?
	if cr.ltm.isSemanticMatch(existing.Value, c.Value) {
		// Same meaning — bump confidence, use newer phrasing
		existing.Confidence = math.Min(1.0, existing.Confidence+0.1)
		existing.Value = c.Value // prefer newer phrasing
		existing.Version++
		existing.UpdatedAt = time.Now()
		return ResolveResult{
			Candidate: c,
			Decision:  ConflictAcceptNew,
			Winner:    existing,
		}
	}

	// Different values — compare confidences
	newConf := clampConfidence(c.SuggestedConfidence)
	oldDecayed := existing.Confidence

	gap := math.Abs(newConf - oldDecayed)

	switch {
	case gap >= cr.confidenceGap && newConf > oldDecayed:
		// Clear winner: new fact
		now := time.Now()
		newFact := MemoryItem{
			Section:    c.Section,
			Key:        c.Key,
			Value:      c.Value,
			Version:    existing.Version + 1,
			Confidence: newConf,
			UpdatedAt:  now,
		}
		return ResolveResult{
			Candidate: c,
			Decision:  ConflictAcceptNew,
			Winner:    newFact,
			OldFact:   &existing,
		}

	case gap >= cr.confidenceGap && oldDecayed >= newConf:
		// Clear winner: old fact
		return ResolveResult{
			Candidate: c,
			Decision:  ConflictKeepExisting,
			Winner:    existing,
		}

	default:
		// Narrow gap — need human
		return ResolveResult{
			Candidate: c,
			Decision:  ConflictNeedsHuman,
			PendingConflict: &PendingConflict{
				Section:       c.Section,
				Key:           c.Key,
				OldValue:      existing.Value,
				OldConfidence: oldDecayed,
				NewValue:      c.Value,
				NewConfidence: newConf,
				Reason:        fmt.Sprintf("Confidence gap %.2f < threshold %.2f", gap, cr.confidenceGap),
			},
		}
	}
}

func clampConfidence(c float64) float64 {
	if c <= 0 {
		return 0.5 // default for unset
	}
	if c > 1.0 {
		return 1.0
	}
	return c
}
