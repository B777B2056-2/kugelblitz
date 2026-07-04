package longterm

import (
	"math"
	"time"
)

// ConflictDecision encodes the result of a conflict resolution.
type ConflictDecision string

const (
	ConflictAcceptNew    ConflictDecision = "accept_new"
	ConflictKeepExisting ConflictDecision = "keep_existing"
)

// ConflictResolver resolves conflicts between extracted items and existing LTM items.
type ConflictResolver struct {
	ltm           *LongTermMemory
	confidenceGap float64
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
	Candidate MemoryItemCandidate
	Decision  ConflictDecision
	Winner    MemoryItem // The winning fact
	OldFact   *MemoryItem
}

// Resolve processes a batch of fact candidates against existing LTM items.
// When confidence gap is narrow, the existing fact is kept.
func (cr *ConflictResolver) Resolve(candidates []MemoryItemCandidate) []MemoryItem {
	var stored []MemoryItem
	for _, c := range candidates {
		result := cr.resolveOne(c)
		switch result.Decision {
		case ConflictAcceptNew, ConflictKeepExisting:
			stored = append(stored, result.Winner)
		}
	}
	return stored
}

func (cr *ConflictResolver) resolveOne(c MemoryItemCandidate) ResolveResult {
	existing, exists := cr.ltm.Get(c.Section, c.Key)

	if !exists {
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

	// Semantic match — same fact, different wording
	if cr.ltm.isSemanticMatch(existing.Value, c.Value) {
		existing.Confidence = math.Min(1.0, existing.Confidence+0.1)
		existing.Value = c.Value
		existing.Version++
		existing.UpdatedAt = time.Now()
		return ResolveResult{
			Candidate: c,
			Decision:  ConflictAcceptNew,
			Winner:    existing,
		}
	}

	// Different values — compare confidences; narrow gap keeps existing
	newConf := clampConfidence(c.SuggestedConfidence)
	oldDecayed := existing.Confidence
	gap := math.Abs(newConf - oldDecayed)

	if gap >= cr.confidenceGap && newConf > oldDecayed {
		now := time.Now()
		return ResolveResult{
			Candidate: c,
			Decision:  ConflictAcceptNew,
			Winner: MemoryItem{
				Section:    c.Section,
				Key:        c.Key,
				Value:      c.Value,
				Version:    existing.Version + 1,
				Confidence: newConf,
				UpdatedAt:  now,
			},
			OldFact: &existing,
		}
	}

	// Old wins or gap too narrow — keep existing
	return ResolveResult{
		Candidate: c,
		Decision:  ConflictKeepExisting,
		Winner:    existing,
	}
}

func clampConfidence(c float64) float64 {
	if c <= 0 {
		return 0.5
	}
	if c > 1.0 {
		return 1.0
	}
	return c
}
