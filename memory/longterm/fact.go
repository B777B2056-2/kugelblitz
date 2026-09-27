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

	memorytypes "github.com/B777B2056-2/kugelblitz/memory/types"
	"github.com/B777B2056-2/kugelblitz/persist"
)

// confidenceDecayPerDay is the daily decay factor.
// confidence *= 0.95^(days_since_update)
const confidenceDecayPerDay = 0.95

// LongTermMemory stores items in MEMORY.md — the authoritative source for
// pure declarative items (user preferences, project items, lessons learned).
// For non-fact memories (episodic, patterns), use EpisodicMemory backed by ChromaDB.
type LongTermMemory struct {
	items []memorytypes.MemoryItem
	index map[string]int // key → slice index for O(1) lookup by section+key
	mu    sync.RWMutex

	mdStore *persist.MarkdownPersist
	path    string // filesystem path to MEMORY.md
	graph   *GraphStore
	judge   func(oldValue, newValue string) bool // LLM semantic-equivalence (optional)
}

// Option configures a LongTermMemory at construction.
type Option func(*LongTermMemory)

// WithSemanticJudge injects an LLM-based semantic-equivalence judge, used by
// Store's conflict resolution when string comparison is inconclusive. A nil
// judge (the default) disables the LLM path and falls back to string matching.
func WithSemanticJudge(fn func(oldValue, newValue string) bool) Option {
	return func(ltm *LongTermMemory) { ltm.judge = fn }
}

// WithGraph attaches a GraphStore for entity-relationship extraction.
func WithGraph(g *GraphStore) Option {
	return func(ltm *LongTermMemory) { ltm.graph = g }
}

// NewLongTermMemory loads items from MEMORY.md via the given MarkdownPersist.
func NewLongTermMemory(mdStore *persist.MarkdownPersist, opts ...Option) (*LongTermMemory, error) {
	ltm := &LongTermMemory{
		mdStore: mdStore,
		path:    "MEMORY.md",
		index:   make(map[string]int),
	}
	for _, opt := range opts {
		opt(ltm)
	}
	if err := ltm.load(); err != nil {
		return nil, err
	}
	return ltm, nil
}

// Graph returns the associated GraphStore (may be nil).
func (ltm *LongTermMemory) Graph() *GraphStore { return ltm.graph }

// StoreMarkdown writes a markdown document (e.g. DREAMS.md) through the
// underlying MarkdownPersist, keeping mdStore encapsulated from callers.
func (ltm *LongTermMemory) StoreMarkdown(ctx context.Context, path string, data []byte) error {
	return ltm.mdStore.Store(ctx, path, data)
}

// LoadMarkdown reads a markdown document (e.g. DREAMS.md) through the
// underlying MarkdownPersist, mirroring StoreMarkdown.
func (ltm *LongTermMemory) LoadMarkdown(ctx context.Context, path string) ([]byte, error) {
	return ltm.mdStore.Load(ctx, path)
}

// MarkdownExists reports whether a markdown document exists.
func (ltm *LongTermMemory) MarkdownExists(ctx context.Context, path string) bool {
	return ltm.mdStore.Exists(ctx, path)
}

// indexKey builds the normalized index key for O(1) lookup.
func (ltm *LongTermMemory) indexKey(section, key string) string {
	return ltm.normalize(section) + "\x00" + key
}

// rebuildIndex rebuilds the in-memory index from the items slice.
// Must be called with mu held.
func (ltm *LongTermMemory) rebuildIndex() {
	ltm.index = make(map[string]int, len(ltm.items))
	for i := range ltm.items {
		ltm.index[ltm.indexKey(ltm.items[i].Section, ltm.items[i].Key)] = i
	}
}

// ---- CRUD ----

// Store upserts a fact with confidence-based conflict resolution.
// Returns the winning fact and whether a conflict existed.
func (ltm *LongTermMemory) Store(section, key, value string) (winner memorytypes.MemoryItem, conflict *memorytypes.MemoryItem, _ error) {
	ltm.mu.Lock()
	defer ltm.mu.Unlock()
	now := time.Now()

	section = ltm.normalize(section)
	idxKey := ltm.indexKey(section, key)
	curIdx, exists := ltm.index[idxKey]

	newFact := memorytypes.MemoryItem{Section: section, Key: key, Value: value, Version: 1, Confidence: 1.0, UpdatedAt: now}

	if !exists {
		ltm.index[idxKey] = len(ltm.items)
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
		return c, &memorytypes.MemoryItem{Section: section, Key: key, Value: value, Version: c.Version + 1, Confidence: 1.0}, nil
	}
}

// BulkStore atomically writes multiple items to MEMORY.md.
func (ltm *LongTermMemory) BulkStore(items []memorytypes.MemoryItem) error {
	ltm.mu.Lock()
	defer ltm.mu.Unlock()

	now := time.Now()
	for _, newFact := range items {
		newFact.UpdatedAt = now
		newFact.Section = ltm.normalize(newFact.Section)
		idxKey := ltm.indexKey(newFact.Section, newFact.Key)

		if curIdx, exists := ltm.index[idxKey]; exists {
			newFact.Version = ltm.items[curIdx].Version + 1
			ltm.items[curIdx] = newFact
		} else {
			ltm.index[idxKey] = len(ltm.items)
			ltm.items = append(ltm.items, newFact)
		}
	}
	return ltm.write()
}

// Get returns the current value for a key (with decayed confidence).
func (ltm *LongTermMemory) Get(section, key string) (memorytypes.MemoryItem, bool) {
	ltm.mu.RLock()
	defer ltm.mu.RUnlock()

	if idx, ok := ltm.index[ltm.indexKey(section, key)]; ok {
		return ltm.decayConfidence(ltm.items[idx]), true
	}
	return memorytypes.MemoryItem{}, false
}

// Remove permanently deletes a fact.
func (ltm *LongTermMemory) Remove(section, key string) error {
	ltm.mu.Lock()
	defer ltm.mu.Unlock()

	idxKey := ltm.indexKey(section, key)
	idx, ok := ltm.index[idxKey]
	if !ok {
		return nil
	}

	// swap-remove: move last element to the deleted position, update index
	lastIdx := len(ltm.items) - 1
	if idx != lastIdx {
		ltm.items[idx] = ltm.items[lastIdx]
		ltm.index[ltm.indexKey(ltm.items[idx].Section, ltm.items[idx].Key)] = idx
	}
	ltm.items = ltm.items[:lastIdx]
	delete(ltm.index, idxKey)

	return ltm.write()
}

// GetSection returns all items in a section with decayed confidence.
func (ltm *LongTermMemory) GetSection(section string) []memorytypes.MemoryItem {
	ltm.mu.RLock()
	defer ltm.mu.RUnlock()

	var result []memorytypes.MemoryItem
	section = ltm.normalize(section)
	for _, f := range ltm.items {
		if ltm.normalize(f.Section) == section {
			result = append(result, ltm.decayConfidence(f))
		}
	}
	return result
}

// All returns a copy of all items without confidence decay.
// Decay is computed lazily on Get/GetSection to avoid O(n) math.Pow on every call.
func (ltm *LongTermMemory) All() []memorytypes.MemoryItem {
	ltm.mu.RLock()
	defer ltm.mu.RUnlock()

	result := make([]memorytypes.MemoryItem, len(ltm.items))
	copy(result, ltm.items)
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
func (ltm *LongTermMemory) Search(query string) []memorytypes.MemoryItem {
	return ltm.SearchWithMode(query, persist.SearchBM25)
}

// SearchWithMode queries items. BM25 and Hybrid do string matching;
// Semantic mode is handled by EpisodicMemory (ChromaDB).
func (ltm *LongTermMemory) SearchWithMode(query string, mode persist.SearchMode) []memorytypes.MemoryItem {
	ltm.mu.RLock()
	defer ltm.mu.RUnlock()

	q := strings.ToLower(query)
	var results []memorytypes.MemoryItem
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

func (ltm *LongTermMemory) decayConfidence(f memorytypes.MemoryItem) memorytypes.MemoryItem {
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
	ltm.rebuildIndex()
	return nil
}

func (ltm *LongTermMemory) write() error {
	var entries []persist.MarkdownEntry
	for _, f := range ltm.items {
		entries = append(entries, itemToMarkdown(f))
	}
	return ltm.mdStore.WriteAll(context.Background(), ltm.path, entries)
}

func itemToMarkdown(f memorytypes.MemoryItem) persist.MarkdownEntry {
	return persist.MarkdownEntry{
		Section:    f.Section,
		Key:        f.Key,
		Value:      f.Value,
		Source:     f.Source,
		Version:    f.Version,
		Confidence: f.Confidence,
		UpdatedAt:  f.UpdatedAt,
	}
}

func markdownToItem(e persist.MarkdownEntry) memorytypes.MemoryItem {
	return memorytypes.MemoryItem{
		Section:    e.Section,
		Key:        e.Key,
		Value:      e.Value,
		Source:     e.Source,
		Version:    e.Version,
		Confidence: e.Confidence,
		UpdatedAt:  e.UpdatedAt,
	}
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
	if ltm.judge != nil {
		return ltm.judge(la, lb)
	}
	return false
}

// normalize lowercases and trims a string for case-insensitive comparison.
func (ltm *LongTermMemory) normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
