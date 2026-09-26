package persist

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newChromaStore spins up an httptest server that answers Chroma's collection
// check (GET) with 200 and delegates POST endpoints to the given handler.
func newChromaStore(t *testing.T, post http.HandlerFunc) *ChromaStore {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		post(w, r)
	}))
	t.Cleanup(ts.Close)
	c, err := NewChromaStore(ts.URL, "test")
	require.NoError(t, err)
	return c
}

// ---- P1: JSONL Append surfaces a Load failure instead of silently
// clearing existing data. A non-IsNotExist Load error must abort the append.

func TestJSONLAppend_LoadErrorPropagates(t *testing.T) {
	fp := NewFilePersist(t.TempDir())
	jp := NewJSONLPersist(fp)

	// Make the target path a directory so Load returns "is a directory"
	// (not os.IsNotExist) and the append must surface it.
	require.NoError(t, os.MkdirAll(filepath.Join(fp.root, "blocked"), 0755))

	err := jp.Append(context.Background(), "blocked", []JSONLEvent{{Type: "msg"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load existing")
}

// ---- P2: JSONL Append is serialized so concurrent appends lose no events.

func TestJSONLAppend_ConcurrentNoLostEvents(t *testing.T) {
	fp := NewFilePersist(t.TempDir())
	jp := NewJSONLPersist(fp)

	const workers, perWorker = 20, 10
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				payload, _ := json.Marshal(map[string]int{"w": w, "i": i})
				evt := JSONLEvent{Type: "msg", Payload: payload}
				if err := jp.Append(context.Background(), "log.jsonl", []JSONLEvent{evt}); err != nil {
					t.Errorf("append w=%d i=%d: %v", w, i, err)
				}
			}
		}(w)
	}
	wg.Wait()

	events, err := jp.ReadAll("log.jsonl")
	require.NoError(t, err)
	assert.Len(t, events, workers*perWorker)
}

// ---- P3: NewChromaStoreOrNil degrades to nil (rather than panicking or
// returning a broken store) when Chroma initialization fails.

func TestNewChromaStoreOrNil_InitErrorReturnsNil(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	t.Setenv("CHROMA_URL", ts.URL)

	assert.Nil(t, NewChromaStoreOrNil())
}

// ---- P4: Chroma Exists reflects actual presence (no inverted logic).

func TestChromaStore_Exists_ReflectsPresence(t *testing.T) {
	c := newChromaStore(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			QueryTexts []string `json:"query_texts"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.QueryTexts) > 0 && body.QueryTexts[0] == "present" {
			_, _ = w.Write([]byte(`{"documents":[["found"]],"distances":[[0.1]]}`))
		} else {
			_, _ = w.Write([]byte(`{"documents":[],"distances":[]}`))
		}
	})

	assert.True(t, c.Exists(context.Background(), "present"))
	assert.False(t, c.Exists(context.Background(), "absent"))
}

// ---- P5: Store and Delete use the key as the document ID so that a Delete
// actually removes what a Store created.

func TestChromaStore_StoreDeleteUseKeyAsDocID(t *testing.T) {
	var upsertID, deleteID string
	c := newChromaStore(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/upsert"):
			if len(body.IDs) > 0 {
				upsertID = body.IDs[0]
			}
		case strings.HasSuffix(r.URL.Path, "/delete"):
			if len(body.IDs) > 0 {
				deleteID = body.IDs[0]
			}
		}
		w.WriteHeader(http.StatusOK)
	})

	require.NoError(t, c.Store(context.Background(), "doc-123", []byte("hello")))
	require.NoError(t, c.Delete(context.Background(), "doc-123"))

	assert.Equal(t, "doc-123", upsertID)
	assert.Equal(t, "doc-123", deleteID)
}

// ---- P6: Search accepts non-semantic modes and falls back gracefully.

func TestChromaStore_Search_NonSemanticModeFallsBack(t *testing.T) {
	c := newChromaStore(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"documents":[["doc-a"]],"distances":[[0.2]]}`))
	})

	results, err := c.Search("q", SearchBM25, 3)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "doc-a", results[0].Document)
}

// ---- P9: FilePersist.List is safe under concurrent Store.

func TestFilePersist_ConcurrentListAndStore(t *testing.T) {
	fp := NewFilePersist(t.TempDir())
	ctx := context.Background()
	requireNoError(t, fp.Store(ctx, filepath.Join("sessions", "a.jsonl"), []byte("x")))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = fp.Store(ctx, fmt.Sprintf("sessions/s%d.jsonl", idx), []byte("y"))
		}(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = fp.List(ctx, "sessions")
		}()
	}
	wg.Wait()

	keys, err := fp.List(ctx, "sessions")
	require.NoError(t, err)
	assert.Len(t, keys, 21)
}

// ---- P14: FilePersist.Store is atomic — no temp files leak and content is
// intact after a successful write.

func TestFilePersist_Store_LeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	fp := NewFilePersist(dir)

	require.NoError(t, fp.Store(context.Background(), "data.json", []byte("payload")))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp-", "atomic store must not leave temp files")
	}

	data, err := fp.Load(context.Background(), "data.json")
	require.NoError(t, err)
	assert.Equal(t, "payload", string(data))
}
