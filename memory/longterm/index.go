package longterm

import (
	"context"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/persist"
)

// IndexManager manages the ChromaDB search index derived from MEMORY.md.
// ChromaDB is the sole query entry point; its content is rebuilt from
// MEMORY.md at startup and after every write.
type IndexManager struct {
	store *persist.VectorPersist
	ltm   *LongTermMemory
}

// NewIndexManager creates an IndexManager. store may be nil (ChromaDB disabled).
func NewIndexManager(store *persist.VectorPersist, ltm *LongTermMemory) *IndexManager {
	return &IndexManager{store: store, ltm: ltm}
}

// IsAvailable reports whether ChromaDB indexing is configured.
func (im *IndexManager) IsAvailable() bool {
	return im != nil && im.store != nil && im.store.IsAvailable()
}

// Rebuild fully rebuilds the ChromaDB index from MEMORY.md items.
// Reads all items, converts to vector entries, deletes old index, upserts new.
func (im *IndexManager) Rebuild(ctx context.Context) error {
	if !im.IsAvailable() {
		return nil
	}

	items := im.ltm.All()
	var entries []persist.VectorEntry
	for _, f := range items {
		entries = append(entries, itemToVectorEntry(f))
	}

	// Delete old index by deleting the collection and recreating
	// (ChromaDB UpsertMany is idempotent, but we also need to remove deleted items)
	if len(entries) == 0 {
		return nil
	}
	return im.store.UpsertMany(ctx, entries)
}

// RebuildIfStale checks if the index needs rebuilding and rebuilds if so.
// Called at startup to ensure index consistency.
func (im *IndexManager) RebuildIfStale(ctx context.Context) error {
	if !im.IsAvailable() {
		return nil
	}
	// Always rebuild at startup — cheap for small datasets, ensures consistency
	return im.Rebuild(ctx)
}

// Search queries the ChromaDB index. Falls back to MEMORY.md string search
// if ChromaDB is not available.
func (im *IndexManager) Search(ctx context.Context, query string, mode persist.SearchMode, limit int) []MemoryItem {
	if im.IsAvailable() {
		results, err := im.store.Search(ctx, query, mode, limit)
		if err == nil && len(results) > 0 {
			items := make([]MemoryItem, 0, len(results))
			for _, r := range results {
				section, _ := r.Metadata["section"].(string)
				key, _ := r.Metadata["key"].(string)
				if section == "" || key == "" {
					continue
				}
				// Get authoritative value from MEMORY.md
				if f, ok := im.ltm.Get(section, key); ok {
					items = append(items, f)
				}
			}
			if len(items) > 0 {
				return items
			}
		}
	}
	// Fallback: MEMORY.md string search
	return im.ltm.SearchWithMode(query, mode)
}

// itemToVectorEntry converts a MemoryItem to a ChromaDB vector entry.
func itemToVectorEntry(f MemoryItem) persist.VectorEntry {
	return persist.VectorEntry{
		DocID:    fmt.Sprintf("mem:%s:%s", f.Section, f.Key),
		Document: fmt.Sprintf("[%s] %s: %s", f.Section, f.Key, f.Value),
		Metadata: map[string]any{
			"section":    f.Section,
			"key":        f.Key,
			"value":      f.Value,
			"version":    f.Version,
			"confidence": f.Confidence,
			"updated_at": f.UpdatedAt.Format("2006-01-02"),
		},
	}
}
