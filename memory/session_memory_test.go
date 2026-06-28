package memory

import (
	"context"
	"sync"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/persist"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionMemory_AppendAndGetHistory(t *testing.T) {
	mem := newSessionMemory("test-session")

	msg1 := core.NewUserMessage("root", core.TextContent{Text: "hello"})
	msg2 := core.NewAssistantMessage("root", core.TextContent{Text: "world"})

	mem.AppendMessage(msg1)
	mem.AppendMessage(msg2)

	history := mem.GetHistoryMessages()
	require.Len(t, history, 2)
	assert.Equal(t, "hello", history[0].Content.(core.TextContent).Text)
	assert.Equal(t, "world", history[1].Content.(core.TextContent).Text)
}

func TestSessionMemory_AppendMessages(t *testing.T) {
	mem := newSessionMemory("test")
	mem.AppendMessages([]core.Message{
		core.NewUserMessage("r", core.TextContent{Text: "a"}),
		core.NewUserMessage("r", core.TextContent{Text: "b"}),
	})
	assert.Len(t, mem.GetHistoryMessages(), 2)
}

func TestSessionMemory_GetHistoryIncludesSummary(t *testing.T) {
	mem := newSessionMemory("test")
	mem.AppendMessage(core.NewUserMessage("r", core.TextContent{Text: "msg"}))

	// Artificially set a summary
	mem.summary = "prior context"

	history := mem.GetHistoryMessages()
	require.Len(t, history, 2) // summary + msg
	assert.Equal(t, "system", string(history[0].Role))
	assert.Contains(t, history[0].Content.(core.TextContent).Text, "prior context")
	assert.Equal(t, "msg", history[1].Content.(core.TextContent).Text)
}

func TestSessionMemory_Compress_NoopWhenFewMessages(t *testing.T) {
	mem := newSessionMemory("test")
	mem.AppendMessage(core.NewUserMessage("r", core.TextContent{Text: "only one"}))

	// Compressor is nil — but Compress should return early (total <= KeepLastN)
	_, err := mem.Compress(context.Background(), nil, CompressConfig{
		KeepLastN:             10,
		MinMessagesToCompress: 5,
	})
	assert.NoError(t, err) // no-op, no panic
}

func TestSessionMemory_Compress_NoopWhenOldTooFew(t *testing.T) {
	mem := newSessionMemory("test")
	for i := 0; i < 12; i++ {
		mem.AppendMessage(core.NewUserMessage("r", core.TextContent{Text: "msg"}))
	}

	// 12 total, KeepLastN=10 → 2 old, MinMessagesToCompress=5 → skip
	_, err := mem.Compress(context.Background(), nil, CompressConfig{
		KeepLastN:             10,
		MinMessagesToCompress: 5,
	})
	assert.NoError(t, err)
	assert.Len(t, mem.GetHistoryMessages(), 12) // unchanged
}

func TestDefaultCompressConfig(t *testing.T) {
	cfg := DefaultCompressConfig()
	assert.Equal(t, 10, cfg.KeepLastN)
	assert.Equal(t, 5, cfg.MinMessagesToCompress)
}

func TestManager_ReloadAfterRestart(t *testing.T) {
	oldPM := persist.GetManager()
	persist.SetManager(persist.NewFileManager(t.TempDir()))
	defer persist.SetManager(oldPM)

	// Simulate: create session, add messages, persist, then "restart"
	mgr := GetSessionMemoryManager()
	id := mgr.CreateSessionMemory()
	mem, _ := mgr.GetSessionMemory(id)
	mem.AppendMessage(core.NewUserMessage("r", core.TextContent{Text: "persisted msg"}))
	mem.summary = "pre-restart context"
	mem.Persist()

	// "Restart": clear the in-memory map
	mgr.SessionMemoryMap = sync.Map{}

	// GetSessionMemory should reload from disk
	reloaded, ok := mgr.GetSessionMemory(id)
	require.True(t, ok)
	require.NotNil(t, reloaded)
	assert.Equal(t, "pre-restart context", reloaded.summary)

	history := reloaded.GetHistoryMessages()
	require.Len(t, history, 2) // summary + 1 msg
	assert.Contains(t, history[1].Content.(core.TextContent).Text, "persisted msg")
}

func TestManager_CreateAndGet(t *testing.T) {
	mgr := GetSessionMemoryManager()
	id := mgr.CreateSessionMemory()
	assert.NotEmpty(t, id)

	mem, ok := mgr.GetSessionMemory(id)
	require.True(t, ok)
	assert.NotNil(t, mem)
}

func TestManager_GetNonexistent(t *testing.T) {
	mgr := GetSessionMemoryManager()
	_, ok := mgr.GetSessionMemory("nonexistent")
	assert.False(t, ok)
}

func TestPersistAndLoad_RoundTrip(t *testing.T) {
	// Use temp dir to avoid cluttering project
	oldPM := persist.GetManager()
	persist.SetManager(persist.NewFileManager(t.TempDir()))
	defer persist.SetManager(oldPM)

	mem := newSessionMemory("persist-test")
	mem.AppendMessage(core.NewUserMessage("r", core.TextContent{Text: "hello"}))
	mem.AppendMessage(core.NewAssistantMessage("r", core.TextContent{Text: "world"}))
	mem.summary = "test context"

	err := mem.Persist()
	require.NoError(t, err)

	loaded, err := LoadSessionMemory("persist-test")
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, "test context", loaded.summary)
	history := loaded.GetHistoryMessages()
	require.Len(t, history, 3) // summary + hello + world
	assert.Contains(t, history[1].Content.(core.TextContent).Text, "hello")
	assert.Contains(t, history[2].Content.(core.TextContent).Text, "world")
}

func TestPersistAndLoad_FullFidelity(t *testing.T) {
	oldPM := persist.GetManager()
	persist.SetManager(persist.NewFileManager(t.TempDir()))
	defer persist.SetManager(oldPM)

	mem := newSessionMemory("fidelity-test")

	// Text content
	mem.AppendMessage(core.NewUserMessage("r", core.TextContent{Text: "do something"}))

	// Tool call content
	toolMsg := core.NewAssistantMessage("r", nil)
	toolMsg.Content = core.ToolCallContent{
		Details: []core.ToolCallDetail{
			{ID: "tc-1", ToolName: "search", Args: map[string]any{"query": "test"}},
			{ID: "tc-2", ToolName: "calculate", Args: map[string]any{"expr": "1+1"}},
		},
	}
	mem.AppendMessage(toolMsg)

	// Tool result content
	resultMsg := core.NewToolMessage("r", []core.ToolCallResult{
		{ToolCallID: "tc-1", ToolName: "search", Outputs: map[string]any{"result": "found"}},
	})
	mem.AppendMessage(resultMsg)

	// Composite content
	compMsg := core.NewAssistantMessage("r", nil)
	compMsg.Content = core.CompositeContent{
		Parts: []core.Content{
			core.ReasoningContent{Reasoning: "thinking..."},
			core.TextContent{Text: "the answer is 42"},
		},
	}
	mem.AppendMessage(compMsg)
	mem.summary = "persisted context"

	err := mem.Persist()
	require.NoError(t, err)

	loaded, err := LoadSessionMemory("fidelity-test")
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, "persisted context", loaded.summary)
	history := loaded.GetHistoryMessages()
	require.Len(t, history, 5) // summary + 4 messages

	// Verify tool call
	tc, ok := history[2].Content.(core.ToolCallContent)
	require.True(t, ok, "expected ToolCallContent, got %T", history[2].Content)
	assert.Equal(t, "search", tc.Details[0].ToolName)
	assert.Equal(t, "test", tc.Details[0].Args["query"])

	// Verify composite
	cc, ok := history[4].Content.(core.CompositeContent)
	require.True(t, ok, "expected CompositeContent, got %T", history[4].Content)
	require.Len(t, cc.Parts, 2)
	assert.Equal(t, "thinking...", cc.Parts[0].(core.ReasoningContent).Reasoning)
	assert.Equal(t, "the answer is 42", cc.Parts[1].(core.TextContent).Text)
}

func TestPersist_ThenDeleteFile(t *testing.T) {
	oldPM := persist.GetManager()
	persist.SetManager(persist.NewFileManager(t.TempDir()))
	defer persist.SetManager(oldPM)

	mem := newSessionMemory("tmp-session")
	mem.AppendMessage(core.NewUserMessage("r", core.TextContent{Text: "hi"}))
	require.NoError(t, mem.Persist())

	// Remove the persisted data
	persist.DeleteSession("tmp-session")

	loaded, err := LoadSessionMemory("tmp-session")
	assert.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestLoadSessionMemory_NonExistent(t *testing.T) {
	mem, err := LoadSessionMemory("no-such-session")
	assert.NoError(t, err)
	assert.Nil(t, mem)
}

func TestManager_MultipleSessions(t *testing.T) {
	mgr := GetSessionMemoryManager()
	id1 := mgr.CreateSessionMemory()
	id2 := mgr.CreateSessionMemory()
	assert.NotEqual(t, id1, id2)

	mem1, _ := mgr.GetSessionMemory(id1)
	mem2, _ := mgr.GetSessionMemory(id2)
	assert.NotSame(t, mem1, mem2)

	mem1.AppendMessage(core.NewUserMessage("r", core.TextContent{Text: "a"}))
	mem2.AppendMessage(core.NewUserMessage("r", core.TextContent{Text: "b"}))

	assert.Len(t, mem1.GetHistoryMessages(), 1)
	assert.Len(t, mem2.GetHistoryMessages(), 1)
}
