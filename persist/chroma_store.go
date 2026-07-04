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
	"sync"
	"time"
)

// ChromaStore implements VectorStore via ChromaDB's HTTP API v2.
type ChromaStore struct {
	baseURL    string
	collection string
	client     *http.Client
	mu         sync.Mutex
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
	c, _ := NewChromaStore(url, "kugelblitz_memory")
	return c
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

	body := map[string]any{"name": c.collection}
	data, _ := json.Marshal(body)
	resp, err = c.client.Post(c.baseURL+"/api/v2/collections", "application/json", bytes.NewReader(data))
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

// Add inserts documents into the collection (legacy, prefer UpsertMany).
func (c *ChromaStore) Add(documents []string, metadatas []map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

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

	data, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/api/v2/collections/%s/add", c.baseURL, c.collection)
	resp, err := c.client.Post(url, "application/json", bytes.NewReader(data))
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
	c.mu.Lock()
	defer c.mu.Unlock()

	body := map[string]any{
		"query_texts": []string{query},
		"n_results":   limit,
		"include":     []string{"documents", "metadatas", "distances"},
	}
	data, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/api/v2/collections/%s/query", c.baseURL, c.collection)
	resp, err := c.client.Post(url, "application/json", bytes.NewReader(data))
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
				r.Score = 1.0 - result.Distances[0][i]
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
	c.mu.Lock()
	defer c.mu.Unlock()

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

	data, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/api/v2/collections/%s/upsert", c.baseURL, c.collection)
	resp, err := c.client.Post(url, "application/json", bytes.NewReader(data))
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

// Store adds a single document as JSON.
func (c *ChromaStore) Store(ctx context.Context, key string, data []byte) error {
	return c.Add([]string{string(data)}, []map[string]any{{"_key": key}})
}

// Load is not directly supported for ChromaDB — use Search instead.
func (c *ChromaStore) Load(ctx context.Context, key string) ([]byte, error) {
	results, err := c.Search(key, SearchSemantic, 1)
	if err != nil || len(results) == 0 {
		return nil, err
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
	c.mu.Lock()
	defer c.mu.Unlock()

	body := map[string]any{
		"ids": []string{docID},
	}
	data, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/api/v2/collections/%s/delete", c.baseURL, c.collection)
	resp, err := c.client.Post(url, "application/json", bytes.NewReader(data))
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
