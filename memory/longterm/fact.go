// Package longterm provides long-term memory storage for the kugelblitz agent framework.
//
// It manages two distinct types of long-term memory:
//   - All (MEMORY.md): pure structured items (section/key/value), human-readable,
//     authoritative source for declarative knowledge.
//   - Episodic memories (ChromaDB): non-fact memories such as episodic records,
//     lessons learned, and behavioral patterns, stored as vector embeddings for
//     semantic retrieval.
//
// The two stores have zero overlap in content — no reconciliation is needed.
package longterm

import (
	"context"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/B777B2056-2/kugelblitz/persist"
)

// MemoryItem is a single versioned entry in long-term memory.
// Confidence decays exponentially over time; new items start at 1.0.
// When a conflict occurs (same section+key, different value),
// the version with higher confidence wins.
type MemoryItem struct {
	Section    string
	Key        string
	Value      string
	Version    int       // starts at 1
	Confidence float64   // 0.0–1.0, decays over time
	UpdatedAt  time.Time // last update timestamp
}

// confidenceDecayPerDay is the daily decay factor.
// confidence *= 0.95^(days_since_update)
const confidenceDecayPerDay = 0.95

// LongTermMemory stores items in MEMORY.md — the authoritative source for
// pure declarative items (user preferences, project items, lessons learned).
// For non-fact memories (episodic, patterns), use EpisodicMemory backed by ChromaDB.
type LongTermMemory struct {
	items []MemoryItem
	mu    sync.RWMutex

	mdStore *persist.MarkdownPersist
	path    string // filesystem path to MEMORY.md
	graph   *GraphStore
}

// NewLongTermMemory loads items from MEMORY.md via the given MarkdownPersist.
func NewLongTermMemory(mdStore *persist.MarkdownPersist) (*LongTermMemory, error) {
	ltm := &LongTermMemory{
		mdStore: mdStore,
		path:    "MEMORY.md",
	}
	if err := ltm.load(); err != nil {
		return nil, err
	}
	return ltm, nil
}

// Graph returns the associated GraphStore (may be nil).
func (ltm *LongTermMemory) Graph() *GraphStore { return ltm.graph }

// SetGraph attaches a GraphStore for entity-relationship extraction.
func (ltm *LongTermMemory) SetGraph(g *GraphStore) { ltm.graph = g }

// ---- CRUD ----

// Store upserts a fact with confidence-based conflict resolution.
// Returns the winning fact and whether a conflict existed.
func (ltm *LongTermMemory) Store(section, key, value string) (winner MemoryItem, conflict *MemoryItem, _ error) {
	ltm.mu.Lock()
	defer ltm.mu.Unlock()
	now := time.Now()

	section = ltm.normalize(section)
	var curIdx = -1
	for i := range ltm.items {
		if ltm.normalize(ltm.items[i].Section) == section && ltm.items[i].Key == key {
			curIdx = i
			break
		}
	}

	newFact := MemoryItem{Section: section, Key: key, Value: value, Version: 1, Confidence: 1.0, UpdatedAt: now}

	if curIdx < 0 {
		ltm.items = append(ltm.items, newFact)
		_ = ltm.write()
		return newFact, nil, nil
	}

	existing := ltm.items[curIdx]
	decayed := ltm.decayConfidence(existing)

	isSame := existing.Value == value || ltm.isSemanticMatch(existing.Value, value)

	switch {
	case isSame:
		if existing.Value != value {
			existing.Value = value
		}
		existing.Confidence = math.Min(1.0, math.Max(decayed.Confidence, newFact.Confidence)+0.1)
		existing.UpdatedAt = now
		existing.Version++
		ltm.items[curIdx] = existing
		_ = ltm.write()
		return existing, nil, nil

	case newFact.Confidence > decayed.Confidence:
		newFact.Version = existing.Version + 1
		conflictCopy := ltm.items[curIdx]
		ltm.items[curIdx] = newFact
		_ = ltm.write()
		return newFact, &conflictCopy, nil

	default:
		c := existing
		return c, &MemoryItem{Section: section, Key: key, Value: value, Version: c.Version + 1, Confidence: 1.0}, nil
	}
}

// BulkStore atomically writes multiple items to MEMORY.md.
func (ltm *LongTermMemory) BulkStore(items []MemoryItem) error {
	ltm.mu.Lock()
	defer ltm.mu.Unlock()

	now := time.Now()
	for _, newFact := range items {
		newFact.UpdatedAt = now
		newFact.Section = ltm.normalize(newFact.Section)

		curIdx := -1
		for i := range ltm.items {
			if ltm.normalize(ltm.items[i].Section) == newFact.Section && ltm.items[i].Key == newFact.Key {
				curIdx = i
				break
			}
		}
		if curIdx < 0 {
			ltm.items = append(ltm.items, newFact)
		} else {
			newFact.Version = ltm.items[curIdx].Version + 1
			ltm.items[curIdx] = newFact
		}
	}
	return ltm.write()
}

// Get returns the current value for a key (with decayed confidence).
func (ltm *LongTermMemory) Get(section, key string) (MemoryItem, bool) {
	ltm.mu.RLock()
	defer ltm.mu.RUnlock()

	section = ltm.normalize(section)
	for _, f := range ltm.items {
		if ltm.normalize(f.Section) == section && f.Key == key {
			d := ltm.decayConfidence(f)
			return d, true
		}
	}
	return MemoryItem{}, false
}

// Remove permanently deletes a fact.
func (ltm *LongTermMemory) Remove(section, key string) error {
	ltm.mu.Lock()
	defer ltm.mu.Unlock()

	section = ltm.normalize(section)
	for i, f := range ltm.items {
		if ltm.normalize(f.Section) == section && f.Key == key {
			ltm.items = append(ltm.items[:i], ltm.items[i+1:]...)
			_ = ltm.write()
			return nil
		}
	}
	return nil
}

// GetSection returns all items in a section with decayed confidence.
func (ltm *LongTermMemory) GetSection(section string) []MemoryItem {
	ltm.mu.RLock()
	defer ltm.mu.RUnlock()

	var result []MemoryItem
	section = ltm.normalize(section)
	for _, f := range ltm.items {
		if ltm.normalize(f.Section) == section {
			result = append(result, ltm.decayConfidence(f))
		}
	}
	return result
}

// All returns a copy of all items.
func (ltm *LongTermMemory) All() []MemoryItem {
	ltm.mu.RLock()
	defer ltm.mu.RUnlock()

	result := make([]MemoryItem, len(ltm.items))
	for i, f := range ltm.items {
		result[i] = ltm.decayConfidence(f)
	}
	return result
}

// ListSections returns all section names with fact counts.
func (ltm *LongTermMemory) ListSections() map[string]int {
	ltm.mu.RLock()
	defer ltm.mu.RUnlock()

	counts := make(map[string]int)
	for _, f := range ltm.items {
		counts[f.Section]++
	}
	return counts
}

// Stats returns aggregate statistics for items.
func (ltm *LongTermMemory) Stats() (total int, sections int, avgConfidence float64) {
	ltm.mu.RLock()
	defer ltm.mu.RUnlock()

	total = len(ltm.items)
	seen := make(map[string]bool)
	var sum float64
	for _, f := range ltm.items {
		seen[f.Section] = true
		sum += ltm.decayConfidence(f).Confidence
	}
	sections = len(seen)
	if total > 0 {
		avgConfidence = sum / float64(total)
	}
	return
}

// Search queries items by keyword.
func (ltm *LongTermMemory) Search(query string) []MemoryItem {
	return ltm.SearchWithMode(query, persist.SearchBM25)
}

// SearchWithMode queries items. BM25 and Hybrid do string matching;
// Semantic mode is handled by EpisodicMemory (ChromaDB).
func (ltm *LongTermMemory) SearchWithMode(query string, mode persist.SearchMode) []MemoryItem {
	ltm.mu.RLock()
	defer ltm.mu.RUnlock()

	q := strings.ToLower(query)
	var results []MemoryItem
	for _, f := range ltm.items {
		if strings.Contains(strings.ToLower(f.Section), q) ||
			strings.Contains(strings.ToLower(f.Key), q) ||
			strings.Contains(strings.ToLower(f.Value), q) {
			results = append(results, ltm.decayConfidence(f))
		}
	}
	return results
}

// ---- Confidence decay ----

func (ltm *LongTermMemory) decayConfidence(f MemoryItem) MemoryItem {
	if f.Confidence <= 0 {
		return f
	}
	days := time.Since(f.UpdatedAt).Hours() / 24
	if days <= 0 {
		return f
	}
	f.Confidence = math.Max(0.1, f.Confidence*math.Pow(confidenceDecayPerDay, days))
	return f
}

// ---- File I/O (delegates to persist.MarkdownStore) ----

func (ltm *LongTermMemory) load() error {
	entries, err := ltm.mdStore.ReadAll(ltm.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // first run, no MEMORY.md yet
		}
		return err
	}
	for _, e := range entries {
		ltm.items = append(ltm.items, markdownToItem(e))
	}
	return nil
}

func (ltm *LongTermMemory) write() error {
	if ltm.mdStore == nil {
		return nil // read-only instance (e.g. tests)
	}
	var entries []persist.MarkdownEntry
	for _, f := range ltm.items {
		entries = append(entries, itemToMarkdown(f))
	}
	return ltm.mdStore.WriteAll(context.Background(), ltm.path, entries)
}

func itemToMarkdown(f MemoryItem) persist.MarkdownEntry {
	return persist.MarkdownEntry{
		Section:    f.Section,
		Key:        f.Key,
		Value:      f.Value,
		Version:    f.Version,
		Confidence: f.Confidence,
		UpdatedAt:  f.UpdatedAt,
	}
}

func markdownToItem(e persist.MarkdownEntry) MemoryItem {
	return MemoryItem{
		Section:    e.Section,
		Key:        e.Key,
		Value:      e.Value,
		Version:    e.Version,
		Confidence: e.Confidence,
		UpdatedAt:  e.UpdatedAt,
	}
}

// semanticJudge is set externally via SetSemanticJudge for LLM-based comparison.
var semanticJudge func(oldValue, newValue string) bool

// SetSemanticJudge configures LLM-based semantic comparison.
func SetSemanticJudge(fn func(oldValue, newValue string) bool) {
	semanticJudge = fn
}

// isSemanticMatch returns true if two values are semantically equivalent.
func (ltm *LongTermMemory) isSemanticMatch(a, b string) bool {
	if a == b {
		return true
	}
	la, lb := strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	if la == lb {
		return true
	}
	if strings.Contains(la, lb) || strings.Contains(lb, la) {
		return true
	}
	if semanticJudge != nil {
		return semanticJudge(la, lb)
	}
	return false
}

// normalize lowercases and trims a string for case-insensitive comparison.
func (ltm *LongTermMemory) normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
