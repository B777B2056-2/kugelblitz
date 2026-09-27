package persist

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetManager re-points the global persist manager at a fresh temp dir so the
// session/plan/checkpoint API functions can be exercised in isolation.
func resetManager(t *testing.T) {
	t.Helper()
	core.GetWorkspace().SetDir(t.TempDir())
	globalOnce = sync.Once{}
	globalManager = nil
	_ = GetManager()
}

// ---- P7: Chroma request-body marshal errors are surfaced ----

func TestChromaStore_PostJSON_MarshalError(t *testing.T) {
	c := &ChromaStore{baseURL: "http://example.com", client: &http.Client{}}
	_, err := c.postJSON("/x", map[string]any{"bad": make(chan int)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal")
}

// ---- P8: distance-to-score mapping is bounded and non-negative ----

func TestDistanceToScore(t *testing.T) {
	assert.Equal(t, 1.0, distanceToScore(0))
	assert.Equal(t, 0.5, distanceToScore(1))
	assert.Equal(t, 1.0, distanceToScore(-5)) // negative distances clamp to 0
	for _, d := range []float64{0, 0.5, 2, 100, 100000} {
		s := distanceToScore(d)
		assert.Greater(t, s, 0.0)
		assert.LessOrEqual(t, s, 1.0)
	}
}

// ---- P10: ListCheckpoints parses basenames, not full paths ----

func TestListCheckpoints_ReturnsVersions(t *testing.T) {
	resetManager(t)
	require.NoError(t, SaveCheckpointJSON("p1", 1, map[string]any{"v": 1}))
	require.NoError(t, SaveCheckpointJSON("p1", 2, map[string]any{"v": 2}))

	versions, err := ListCheckpoints("p1")
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, versions)
}

// ---- P11: ListSessions returns bare IDs ----

func TestListSessions_ReturnsBareIDs(t *testing.T) {
	resetManager(t)
	require.NoError(t, SaveSessionJSONL("abc", "summary", nil))
	require.NoError(t, SaveSessionJSONL("def", "", nil))

	ids, err := ListSessions()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"abc", "def"}, ids)
}

// ---- P12: LoadPlanJSON returns a clean error when no events exist ----

func TestLoadPlanJSON_NoEvents_ReturnsCleanError(t *testing.T) {
	resetManager(t)
	var dst map[string]any
	err := LoadPlanJSON("missing", &dst)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "%!w")
}

// ---- P13: LoadSessionJSONL surfaces unmarshal failures ----

func TestLoadSessionJSONL_InvalidMessage_ReturnsError(t *testing.T) {
	resetManager(t)
	// A msg payload that is not a valid Message object.
	bad := []byte(`{"type":"msg","payload":123}` + "\n")
	require.NoError(t, GetManager().JSONL().Store(
		context.Background(), filepath.Join("memory", "sessions", "bad.jsonl"), bad))

	_, _, err := LoadSessionJSONL("bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "message")
}

// ---- P15: formatMarkdown merges case-insensitive sections ----

func TestFormatMarkdown_MergesCaseInsensitiveSections(t *testing.T) {
	ts := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	entries := []MarkdownEntry{
		{Section: "Facts", Key: "a", Value: "1", Version: 1, Confidence: 0.9, UpdatedAt: ts},
		{Section: "facts", Key: "b", Value: "2", Version: 1, Confidence: 0.8, UpdatedAt: ts},
	}
	out := string(formatMarkdown(entries))
	assert.Contains(t, out, "- a: 1")
	assert.Contains(t, out, "- b: 2")
}
