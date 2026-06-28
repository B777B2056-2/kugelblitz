package persist

import (
	"context"
	"fmt"
)

// SearchMode specifies the retrieval strategy for vector search.
type SearchMode string

const (
	SearchHybrid   SearchMode = "hybrid"
	SearchBM25     SearchMode = "bm25"
	SearchSemantic SearchMode = "semantic"
)

// SearchResult from a vector store query.
type SearchResult struct {
	Document string
	Metadata map[string]any
	Score    float64
}

// VectorEntry is a document to upsert into a vector store.
type VectorEntry struct {
	DocID    string
	Document string
	Metadata map[string]any
}

// VectorPersist implements IPersist and adds vector search/upsert methods.
// It wraps a backend that supports vector operations (e.g. ChromaDB).
type VectorPersist struct {
	backend IPersist
	store   *ChromaStore // specialized vector backend
}

// NewVectorPersist creates a VectorPersist. chroma may be nil (ChromaDB disabled).
func NewVectorPersist(backend IPersist, chroma *ChromaStore) *VectorPersist {
	return &VectorPersist{backend: backend, store: chroma}
}

// IsAvailable reports whether a vector backend is configured.
func (v *VectorPersist) IsAvailable() bool { return v != nil && v.store != nil }

// ---- IPersist implementation ----

func (v *VectorPersist) Store(ctx context.Context, key string, data []byte) error {
	if v.backend == nil {
		return nil
	}
	return v.backend.Store(ctx, key, data)
}
func (v *VectorPersist) Load(ctx context.Context, key string) ([]byte, error) {
	if v.backend == nil {
		return nil, fmt.Errorf("vector store not available")
	}
	return v.backend.Load(ctx, key)
}
func (v *VectorPersist) Delete(ctx context.Context, key string) error {
	if v.backend == nil {
		return nil
	}
	return v.backend.Delete(ctx, key)
}
func (v *VectorPersist) List(ctx context.Context, prefix string) ([]string, error) {
	if v.backend == nil {
		return nil, nil
	}
	return v.backend.List(ctx, prefix)
}
func (v *VectorPersist) Exists(ctx context.Context, key string) bool {
	if v.backend == nil {
		return false
	}
	return v.backend.Exists(ctx, key)
}

// ---- Extended methods ----

// Search performs a vector similarity search via ChromaDB.
func (v *VectorPersist) Search(ctx context.Context, query string, mode SearchMode, limit int) ([]SearchResult, error) {
	if v.store == nil {
		return nil, nil
	}
	return v.store.Search(query, mode, limit)
}

// UpsertMany batch-upserts documents into the vector store.
func (v *VectorPersist) UpsertMany(ctx context.Context, entries []VectorEntry) error {
	if v.store == nil || len(entries) == 0 {
		return nil
	}
	return v.store.UpsertMany(entries)
}

// DeleteDocument removes a single document by ID.
func (v *VectorPersist) DeleteDocument(ctx context.Context, docID string) error {
	if v.store == nil {
		return nil
	}
	return v.store.DeleteDocument(docID)
}
