package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// SearchMode specifies the retrieval strategy.
type SearchMode string

const (
	SearchHybrid   SearchMode = "hybrid"   // semantic + keyword combined
	SearchBM25     SearchMode = "bm25"     // keyword exact match (fallback: string search)
	SearchSemantic SearchMode = "semantic" // vector embedding search via ChromaDB
)

// SearchResult from any vector store.
type SearchResult struct {
	Document string
	Metadata map[string]any
	Score    float64
}

// VectorStore abstracts a search backend.
type VectorStore interface {
	Search(query string, mode SearchMode, limit int) ([]SearchResult, error)
	Add(documents []string, metadatas []map[string]any) error
	Sync(facts []Fact) error
}

// ---- ChromaDB HTTP client ----

type ChromaClient struct {
	baseURL    string
	collection string
	client     *http.Client
	mu         sync.Mutex
}

// NewChromaClientOrNil returns a client if CHROMA_URL is set, nil otherwise.
func NewChromaClientOrNil() *ChromaClient {
	url := os.Getenv("CHROMA_URL")
	if url == "" {
		return nil
	}
	c, _ := NewChromaClient(url, "kugelblitz_memory")
	return c
}

func NewChromaClient(baseURL, collection string) (*ChromaClient, error) {
	c := &ChromaClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		collection: collection,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
	if err := c.ensureCollection(); err != nil {
		return nil, fmt.Errorf("chroma: %w", err)
	}
	return c, nil
}

func (c *ChromaClient) ensureCollection() error {
	// Check if collection exists
	resp, err := c.client.Get(c.baseURL + "/api/v2/collections/" + c.collection)
	if err == nil && resp.StatusCode == 200 {
		resp.Body.Close()
		return nil
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Create collection
	body := map[string]any{"name": c.collection}
	data, _ := json.Marshal(body)
	resp, err = c.client.Post(c.baseURL+"/api/v2/collections", "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create collection %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *ChromaClient) Add(documents []string, metadatas []map[string]any) error {
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
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chroma add %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *ChromaClient) Search(query string, mode SearchMode, limit int) ([]SearchResult, error) {
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
	defer resp.Body.Close()
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
				r.Score = 1.0 - result.Distances[0][i] // convert distance → similarity
			}
			if len(result.Metadatas) > 0 && i < len(result.Metadatas[0]) {
				r.Metadata = result.Metadatas[0][i]
			}
			results = append(results, r)
		}
	}
	return results, nil
}

// Sync replaces all documents in the collection with the current facts.
func (c *ChromaClient) Sync(facts []Fact) error {
	// Delete and recreate for simplicity
	c.mu.Lock()
	defer c.mu.Unlock()

	delURL := fmt.Sprintf("%s/api/v2/collections/%s", c.baseURL, c.collection)
	req, _ := http.NewRequest("DELETE", delURL, nil)
	resp, err := c.client.Do(req)
	if err == nil {
		resp.Body.Close()
	}

	if err := c.ensureCollection(); err != nil {
		return err
	}

	var docs []string
	var metas []map[string]any
	for _, f := range facts {
		docs = append(docs, fmt.Sprintf("[%s] %s: %s", f.Section, f.Key, f.Value))
		metas = append(metas, map[string]any{"section": f.Section, "key": f.Key, "value": f.Value})
	}
	if len(docs) == 0 {
		return nil
	}
	return c.Add(docs, metas)
}
