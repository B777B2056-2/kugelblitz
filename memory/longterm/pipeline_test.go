package longterm

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWritePipeline_Run_ExtractsAndStoresFacts(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	t.Cleanup(func() { core.GetWorkspace().SetDir("") })
	ltm, _ := NewLongTermMemory(persist.NewMarkdownPersist(persist.NewFilePersist("")))
	provider := &mockExtractProvider{
		response: `[{"section":"prefs","key":"lang","value":"Go","source_evidence":"user said","suggested_confidence":0.9}]`,
	}
	pipeline := NewWritePipeline(provider, ltm, nil, 0.15)
	result, err := pipeline.Run(context.Background(), &ExtractionContext{UserMessage: "I use Go"})
	require.NoError(t, err)
	assert.Equal(t, 1, result.ItemsExtracted)
	assert.Equal(t, 1, result.ItemsStored)
	f, ok := ltm.Get("prefs", "lang")
	assert.True(t, ok)
	assert.Equal(t, "Go", f.Value)
}

func TestWritePipeline_Run_ConflictCreatesPending(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	t.Cleanup(func() { core.GetWorkspace().SetDir("") })
	ltm, _ := NewLongTermMemory(persist.NewMarkdownPersist(persist.NewFilePersist("")))
	ltm.Store("prefs", "lang", "Python")
	provider := &mockExtractProvider{
		response: `[{"section":"prefs","key":"lang","value":"Go","source_evidence":"","suggested_confidence":0.95}]`,
	}
	pipeline := NewWritePipeline(provider, ltm, nil, 0.15)
	result, err := pipeline.Run(context.Background(), &ExtractionContext{UserMessage: "Switch to Go"})
	require.NoError(t, err)
	assert.Greater(t, result.NeedsHuman, 0)
	assert.NotEmpty(t, ltm.PendingConflicts())
}

func TestWritePipeline_Run_EmptyConversation(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	t.Cleanup(func() { core.GetWorkspace().SetDir("") })
	ltm, _ := NewLongTermMemory(persist.NewMarkdownPersist(persist.NewFilePersist("")))
	provider := &mockExtractProvider{response: `[]`}
	pipeline := NewWritePipeline(provider, ltm, nil, 0.15)
	result, err := pipeline.Run(context.Background(), &ExtractionContext{})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ItemsStored)
}
