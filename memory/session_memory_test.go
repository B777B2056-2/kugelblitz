package memory

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/B777B2056-2/kugelblitz/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionMemory_AppendAndGetHistory(t *testing.T) {
	mem := newSessionMemory("test-session")

	msg1 := coretypes.NewUserMessage(coretypes.TextContent{Text: "hello"})
	msg2 := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "world"})

	mem.AppendMessage(msg1)
	mem.AppendMessage(msg2)

	history := mem.GetHistoryMessages()
	require.Len(t, history, 2)
	assert.Equal(t, "hello", history[0].Content.(coretypes.TextContent).Text)
	assert.Equal(t, "world", history[1].Content.(coretypes.TextContent).Text)
}

func TestSessionMemory_AppendMessages(t *testing.T) {
	mem := newSessionMemory("test")
	mem.AppendMessages([]coretypes.Message{
		coretypes.NewUserMessage(coretypes.TextContent{Text: "a"}),
		coretypes.NewUserMessage(coretypes.TextContent{Text: "b"}),
	})
	assert.Len(t, mem.GetHistoryMessages(), 2)
}

func TestSessionMemory_GetHistoryIncludesSummary(t *testing.T) {
	mem := newSessionMemory("test")
	mem.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: "msg"}))

	// Artificially set a summary
	mem.summary = "prior context"

	history := mem.GetHistoryMessages()
	require.Len(t, history, 2) // summary + msg
	assert.Equal(t, "system", string(history[0].Role))
	assert.Contains(t, history[0].Content.(coretypes.TextContent).Text, "prior context")
	assert.Equal(t, "msg", history[1].Content.(coretypes.TextContent).Text)
}

func TestSessionMemory_Compress_NoopWhenFewMessages(t *testing.T) {
	mem := newSessionMemory("test")
	mem.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: "only one"}))

	// Compressor is nil — but Compress should return early (total <= KeepLastN)
	_, err := mem.Compress(context.Background(), nil, 10, 5)
	assert.NoError(t, err) // no-op, no panic
}

func TestSessionMemory_Compress_NoopWhenOldTooFew(t *testing.T) {
	mem := newSessionMemory("test")
	for i := 0; i < 12; i++ {
		mem.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: "msg"}))
	}

	// 12 total, KeepLastN=10 → 2 old, MinMessagesToCompress=5 → skip
	_, err := mem.Compress(context.Background(), nil, 10, 5)
	assert.NoError(t, err)
	assert.Len(t, mem.GetHistoryMessages(), 12) // unchanged
}

func TestManager_ReloadAfterRestart(t *testing.T) {
	oldDir := core.GetWorkspace().Dir()
	core.GetWorkspace().SetDir(t.TempDir())
	defer core.GetWorkspace().SetDir(oldDir)

	// Simulate: create session, add messages, persist, then "restart"
	mgr := GetSessionMemoryManager()
	id := utils.GenerateSessionID()
	mem := mgr.CreateSessionMemory(id)
	mem.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: "persisted msg"}))
	mem.summary = "pre-restart context"
	_ = mem.Persist()

	// "Restart": clear the in-memory map
	mgr.SessionMemoryMap = sync.Map{}

	// GetSessionMemory should reload from disk
	reloaded, ok := mgr.GetSessionMemory(id)
	require.True(t, ok)
	require.NotNil(t, reloaded)
	assert.Equal(t, "pre-restart context", reloaded.summary)

	history := reloaded.GetHistoryMessages()
	require.Len(t, history, 2) // summary + 1 msg
	assert.Contains(t, history[1].Content.(coretypes.TextContent).Text, "persisted msg")
}

func TestManager_CreateAndGet(t *testing.T) {
	mgr := GetSessionMemoryManager()
	id := utils.GenerateSessionID()
	_ = mgr.CreateSessionMemory(id)
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
	oldDir := core.GetWorkspace().Dir()
	core.GetWorkspace().SetDir(t.TempDir())
	defer core.GetWorkspace().SetDir(oldDir)

	mem := newSessionMemory("persist-test")
	mem.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: "hello"}))
	mem.AppendMessage(coretypes.NewAssistantMessage(coretypes.TextContent{Text: "world"}))
	mem.summary = "test context"

	err := mem.Persist()
	require.NoError(t, err)

	loaded, err := LoadSessionMemory("persist-test")
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, "test context", loaded.summary)
	history := loaded.GetHistoryMessages()
	require.Len(t, history, 3) // summary + hello + world
	assert.Contains(t, history[1].Content.(coretypes.TextContent).Text, "hello")
	assert.Contains(t, history[2].Content.(coretypes.TextContent).Text, "world")
}

func TestPersistAndLoad_FullFidelity(t *testing.T) {
	oldDir := core.GetWorkspace().Dir()
	core.GetWorkspace().SetDir(t.TempDir())
	defer core.GetWorkspace().SetDir(oldDir)

	mem := newSessionMemory("fidelity-test")

	// Text content
	mem.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: "do something"}))

	// Tool call content
	toolMsg := coretypes.NewAssistantMessage(nil)
	toolMsg.Content = coretypes.ToolCallContent{
		Details: []coretypes.ToolCallDetail{
			{ID: "tc-1", ToolName: "search", Args: map[string]any{"query": "test"}},
			{ID: "tc-2", ToolName: "calculate", Args: map[string]any{"expr": "1+1"}},
		},
	}
	mem.AppendMessage(toolMsg)

	// Tool result content
	resultMsg := coretypes.NewToolMessage([]coretypes.ToolCallResult{
		{ToolCallID: "tc-1", ToolName: "search", Outputs: map[string]any{"result": "found"}},
	})
	mem.AppendMessage(resultMsg)

	// Composite content
	compMsg := coretypes.NewAssistantMessage(nil)
	compMsg.Content = coretypes.CompositeContent{
		Parts: []coretypes.Content{
			coretypes.ReasoningContent{Reasoning: "thinking..."},
			coretypes.TextContent{Text: "the answer is 42"},
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
	tc, ok := history[2].Content.(coretypes.ToolCallContent)
	require.True(t, ok, "expected ToolCallContent, got %T", history[2].Content)
	assert.Equal(t, "search", tc.Details[0].ToolName)
	assert.Equal(t, "test", tc.Details[0].Args["query"])

	// Verify composite
	cc, ok := history[4].Content.(coretypes.CompositeContent)
	require.True(t, ok, "expected CompositeContent, got %T", history[4].Content)
	require.Len(t, cc.Parts, 2)
	assert.Equal(t, "thinking...", cc.Parts[0].(coretypes.ReasoningContent).Reasoning)
	assert.Equal(t, "the answer is 42", cc.Parts[1].(coretypes.TextContent).Text)
}

func TestPersist_ThenDeleteFile(t *testing.T) {
	oldDir := core.GetWorkspace().Dir()
	core.GetWorkspace().SetDir(t.TempDir())
	defer core.GetWorkspace().SetDir(oldDir)

	mem := newSessionMemory("tmp-session")
	mem.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: "hi"}))
	require.NoError(t, mem.Persist())

	// Remove the persisted data
	_ = persist.DeleteSession("tmp-session")

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
	id1 := utils.GenerateSessionID()
	id2 := utils.GenerateSessionID()
	mem1 := mgr.CreateSessionMemory(id1)
	mem2 := mgr.CreateSessionMemory(id2)
	assert.NotEqual(t, id1, id2)
	assert.NotSame(t, mem1, mem2)

	mem1.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: "a"}))
	mem2.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: "b"}))

	assert.Len(t, mem1.GetHistoryMessages(), 1)
	assert.Len(t, mem2.GetHistoryMessages(), 1)
}

func TestSessionMemory_ConcurrentReadDuringCompress(t *testing.T) {
	sm := newSessionMemory("test")
	for i := 0; i < 100; i++ {
		sm.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: fmt.Sprintf("msg-%d", i)}))
	}

	var wg sync.WaitGroup
	// Simulate compress: truncate messages and set summary
	wg.Add(1)
	go func() {
		defer wg.Done()
		sm.mu.Lock()
		sm.summary = "compressed"
		sm.historyMessages = sm.historyMessages[50:]
		sm.mu.Unlock()
	}()

	// Concurrent reads
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sm.GetHistoryMessages()
		}()
	}

	// Concurrent appends
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sm.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: fmt.Sprintf("new-%d", idx)}))
		}(i)
	}

	wg.Wait()
	msgs := sm.GetHistoryMessages()
	assert.NotEmpty(t, msgs)
	// Summary should be set by the simulated compress
	assert.Equal(t, "compressed", sm.Summary())
}

func TestSessionMemory_ConcurrentAppendAndPersist(t *testing.T) {
	sm := newSessionMemory("test")
	for i := 0; i < 10; i++ {
		sm.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: fmt.Sprintf("base-%d", i)}))
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sm.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: fmt.Sprintf("appended-%d", idx)}))
		}(i)
	}
	wg.Wait()

	msgs := sm.GetHistoryMessages()
	assert.GreaterOrEqual(t, len(msgs), 20, "should have base + appended messages")
}

func TestResetSessionMemoryManager_Isolates(t *testing.T) {
	ResetSessionMemoryManager()
	smm := GetSessionMemoryManager()
	smm.CreateSessionMemory("reset-probe-session")
	_, ok := smm.GetSessionMemory("reset-probe-session")
	assert.True(t, ok)

	ResetSessionMemoryManager()
	smm2 := GetSessionMemoryManager()
	_, ok = smm2.GetSessionMemory("reset-probe-session")
	assert.False(t, ok, "reset manager should not remember prior sessions")
}
