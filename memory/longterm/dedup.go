package longterm

import "strings"

// DedupResult is the output of fact deduplication.
type DedupResult struct {
	Accepted []MemoryItem // All after dedup (ready to store)
	Rejected int          // All dropped (near-duplicates)
}

// Deduplicator performs semantic dedup on extracted items against
// existing MEMORY.md items and within the current batch.
type Deduplicator struct {
	ltm *LongTermMemory
}

// NewDeduplicator creates a deduplicator for the given LTM.
func NewDeduplicator(ltm *LongTermMemory) *Deduplicator {
	return &Deduplicator{ltm: ltm}
}

// DedupItems deduplicates items against existing LTM items and
// within the batch. Returns accepted items ready to store.
func (d *Deduplicator) DedupItems(items []MemoryItem) *DedupResult {
	var accepted []MemoryItem
	rejected := 0

	for _, f := range items {
		if d.isExistingDuplicate(f) {
			rejected++
			continue
		}
		if d.isBatchDuplicate(f, accepted) {
			rejected++
			continue
		}
		accepted = append(accepted, f)
	}
	return &DedupResult{Accepted: accepted, Rejected: rejected}
}

// isExistingDuplicate checks if a fact is semantically equivalent to an existing one.
func (d *Deduplicator) isExistingDuplicate(f MemoryItem) bool {
	existing, exists := d.ltm.Get(f.Section, f.Key)
	if !exists {
		return false
	}
	if existing.Value == f.Value {
		return true
	}
	return d.ltm.isSemanticMatch(existing.Value, f.Value)
}

// isBatchDuplicate checks for duplicates within the current batch.
func (d *Deduplicator) isBatchDuplicate(f MemoryItem, accepted []MemoryItem) bool {
	for _, a := range accepted {
		if a.Section == f.Section && a.Key == f.Key {
			return true
		}
		if a.Section == f.Section && a.Key != f.Key {
			if d.ltm.isSemanticMatch(a.Value, f.Value) {
				return true
			}
		}
	}
	return false
}

// textOverlap computes Jaccard-like similarity between two strings.
func textOverlap(a, b string) float64 {
	wordsA := strings.Fields(strings.ToLower(a))
	wordsB := strings.Fields(strings.ToLower(b))
	if len(wordsA) == 0 && len(wordsB) == 0 {
		return 1.0
	}
	set := make(map[string]bool)
	for _, w := range wordsA {
		set[w] = true
	}
	intersection := 0
	for _, w := range wordsB {
		if set[w] {
			intersection++
		}
	}
	union := len(set)
	for _, w := range wordsB {
		if !set[w] {
			set[w] = true
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
