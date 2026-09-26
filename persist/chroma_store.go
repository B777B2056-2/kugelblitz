package persist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"
)

// ChromaStore implements VectorStore via ChromaDB's HTTP API v2.
type ChromaStore struct {
	baseURL    string
	collection string
	client     *http.Client
}

// NewChromaStore creates a ChromaDB-backed VectorStore for the given collection.
func NewChromaStore(baseURL, collection string) (*ChromaStore, error) {
	c := &ChromaStore{
		baseURL:    strings.TrimRight(baseURL, "/"),
		collection: collection,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
	if err := c.ensureCollection(); err != nil {
		return nil, fmt.Errorf("chroma: %w", err)
	}
	return c, nil
}

// NewChromaStoreOrNil returns a ChromaStore if CHROMA_URL is set, nil otherwise.
func NewChromaStoreOrNil() *ChromaStore {
	url := os.Getenv("CHROMA_URL")
	if url == "" {
		return nil
	}
	c, err := NewChromaStore(url, "kugelblitz_memory")
	if err != nil {
		core.Warn("chroma: init failed, vector search disabled", "err", err)
		return nil
	}
	return c
}

// postJSON marshals body and POSTs it to the given path, surfacing marshal
// errors instead of silently sending an empty body (P7).
func (c *ChromaStore) postJSON(path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("chroma marshal: %w", err)
	}
	return c.client.Post(c.baseURL+path, "application/json", bytes.NewReader(data))
}

func (c *ChromaStore) ensureCollection() error {
	resp, err := c.client.Get(c.baseURL + "/api/v2/collections/" + c.collection)
	if err == nil && resp.StatusCode == 200 {
		_ = resp.Body.Close()
		return nil
	}
	if resp != nil {
		_ = resp.Body.Close()
	}

	resp, err = c.postJSON("/api/v2/collections", map[string]any{"name": c.collection})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create collection %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// distanceToScore converts a Chroma distance (0 = identical, larger = more
// dissimilar) into a similarity score in [0, 1]. Clamps negatives and maps
// arbitrary distances onto a bounded, monotonic scale (P8).
func distanceToScore(d float64) float64 {
	if d < 0 {
		d = 0
	}
	return 1.0 / (1.0 + d)
}

// Add inserts documents into the collection (legacy, prefer UpsertMany).
func (c *ChromaStore) Add(documents []string, metadatas []map[string]any) error {
	ids := make([]string, len(documents))
	for i := range ids {
		ids[i] = fmt.Sprintf("doc-%d-%d", time.Now().UnixNano(), i)
	}

	body := map[string]any{
		"ids":       ids,
		"documents": documents,
	}
	if len(metadatas) > 0 {
		body["metadatas"] = metadatas
	}

	resp, err := c.postJSON(fmt.Sprintf("/api/v2/collections/%s/add", c.collection), body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chroma add %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// Search queries the collection.
func (c *ChromaStore) Search(query string, mode SearchMode, limit int) ([]SearchResult, error) {
	if mode != SearchSemantic {
		// The basic /query endpoint only does embedding (semantic) search.
		// BM25/hybrid require a newer Chroma API; surface the fallback rather
		// than silently returning identical results (P6).
		core.Warn("chroma: search mode not supported, falling back to semantic", "mode", mode)
	}
	body := map[string]any{
		"query_texts": []string{query},
		"n_results":   limit,
		"include":     []string{"documents", "metadatas", "distances"},
	}
	resp, err := c.postJSON(fmt.Sprintf("/api/v2/collections/%s/query", c.collection), body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("chroma query %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Documents [][]string         `json:"documents"`
		Metadatas [][]map[string]any `json:"metadatas"`
		Distances [][]float64        `json:"distances"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var results []SearchResult
	if len(result.Documents) > 0 {
		for i, doc := range result.Documents[0] {
			r := SearchResult{Document: doc, Score: 1.0}
			if len(result.Distances) > 0 && i < len(result.Distances[0]) {
				r.Score = distanceToScore(result.Distances[0][i])
			}
			if len(result.Metadatas) > 0 && i < len(result.Metadatas[0]) {
				r.Metadata = result.Metadatas[0][i]
			}
			results = append(results, r)
		}
	}
	return results, nil
}

// UpsertMany batch-upserts documents with explicit IDs for idempotent writes.
func (c *ChromaStore) UpsertMany(entries []VectorEntry) error {
	if len(entries) == 0 {
		return nil
	}
	ids := make([]string, len(entries))
	docs := make([]string, len(entries))
	metas := make([]map[string]any, len(entries))
	for i, e := range entries {
		ids[i] = e.DocID
		docs[i] = e.Document
		metas[i] = e.Metadata
	}

	body := map[string]any{
		"ids":       ids,
		"documents": docs,
	}
	if len(metas) > 0 {
		body["metadatas"] = metas
	}

	resp, err := c.postJSON(fmt.Sprintf("/api/v2/collections/%s/upsert", c.collection), body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chroma upsert %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// DeleteDocument removes a single document by ID.
// ---- IPersist implementation (doc-level operations) ----

// Store adds a single document as JSON. The key is used as the document ID so
// that Delete(key) can locate it (P5).
func (c *ChromaStore) Store(ctx context.Context, key string, data []byte) error {
	return c.UpsertMany([]VectorEntry{{
		DocID:    key,
		Document: string(data),
		Metadata: map[string]any{"_key": key},
	}})
}

// Load is not directly supported for ChromaDB — use Search instead.
func (c *ChromaStore) Load(ctx context.Context, key string) ([]byte, error) {
	results, err := c.Search(key, SearchSemantic, 1)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("chroma: not found: %s", key)
	}
	return []byte(results[0].Document), nil
}

// Delete removes a document by key (uses DeleteDocument internally).
func (c *ChromaStore) Delete(ctx context.Context, key string) error {
	return c.DeleteDocument(key)
}

// List is not supported for ChromaDB.
func (c *ChromaStore) List(ctx context.Context, prefix string) ([]string, error) {
	return nil, nil
}

// Exists checks document existence via search.
func (c *ChromaStore) Exists(ctx context.Context, key string) bool {
	_, err := c.Load(ctx, key)
	return err == nil
}

func (c *ChromaStore) DeleteDocument(docID string) error {
	body := map[string]any{
		"ids": []string{docID},
	}
	resp, err := c.postJSON(fmt.Sprintf("/api/v2/collections/%s/delete", c.collection), body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chroma delete %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
