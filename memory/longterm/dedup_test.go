package longterm

import (
	"testing"

	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/stretchr/testify/assert"
)

func newTestLTMDedup(t *testing.T) *LongTermMemory {
	t.Helper()
	ltm, _ := NewLongTermMemory(persist.NewMarkdownPersist(persist.NewFilePersist(t.TempDir())))
	return ltm
}

func TestDeduplicator_DedupFacts_NoDuplicate(t *testing.T) {
	ltm := newTestLTMDedup(t)
	dedup := NewDeduplicator(ltm)
	items := []MemoryItem{
		{Section: "prefs", Key: "lang", Value: "Go"},
		{Section: "prefs", Key: "editor", Value: "VSCode"},
	}
	result := dedup.DedupItems(items)
	assert.Len(t, result.Accepted, 2)
	assert.Equal(t, 0, result.Rejected)
}

func TestDeduplicator_DedupFacts_ExistingDuplicate(t *testing.T) {
	ltm := newTestLTMDedup(t)
	_, _, _ = ltm.Store("prefs", "lang", "Go")
	dedup := NewDeduplicator(ltm)
	result := dedup.DedupItems([]MemoryItem{{Section: "prefs", Key: "lang", Value: "Go"}})
	assert.Empty(t, result.Accepted)
	assert.Equal(t, 1, result.Rejected)
}

func TestDeduplicator_DedupFacts_BatchDuplicate(t *testing.T) {
	ltm := newTestLTMDedup(t)
	dedup := NewDeduplicator(ltm)
	items := []MemoryItem{
		{Section: "prefs", Key: "lang", Value: "Go"},
		{Section: "prefs", Key: "lang", Value: "Golang"},
	}
	result := dedup.DedupItems(items)
	assert.Len(t, result.Accepted, 1)
	assert.Equal(t, 1, result.Rejected)
}

func TestTextOverlap_Identical(t *testing.T) {
	assert.Equal(t, 1.0, textOverlap("hello world", "hello world"))
}

func TestTextOverlap_Partial(t *testing.T) {
	overlap := textOverlap("hello world foo", "hello world bar")
	assert.Greater(t, overlap, 0.4)
	assert.Less(t, overlap, 1.0)
}

func TestTextOverlap_NoOverlap(t *testing.T) {
	assert.Equal(t, 0.0, textOverlap("hello", "world"))
}
