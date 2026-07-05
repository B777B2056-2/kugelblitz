package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Batch 1: SessionManager CRUD ──

func TestSessionManager_Create(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	s := sm.Create()
	assert.NotEmpty(t, s.ID, "should assign an ID")
	assert.Len(t, s.ID, 8, "ID should be 8 chars")
	assert.NotNil(t, s.hitlCh)
}

func TestSessionManager_Get_Exists(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	s := sm.Create()
	got := sm.Get(s.ID)
	require.NotNil(t, got)
	assert.Equal(t, s.ID, got.ID)
}

func TestSessionManager_Get_NotFound(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	assert.Nil(t, sm.Get("nonexistent"))
}

func TestSessionManager_Delete(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	s := sm.Create()
	sm.Delete(s.ID)
	assert.Nil(t, sm.Get(s.ID), "should be removed from memory")
}

func TestSessionManager_GetOrCreate_New(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	// GetOrCreate with an unknown ID creates a new session (with auto-generated ID)
	s := sm.GetOrCreate("unknown")
	require.NotNil(t, s)
	assert.NotEmpty(t, s.ID, "should auto-assign an ID")
}

func TestSessionManager_GetOrCreate_Existing(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	// First, create a session to get a known ID
	known := sm.Create()
	// Then GetOrCreate with that ID should return the SAME instance
	got := sm.GetOrCreate(known.ID)
	assert.Same(t, known, got, "should return the same instance for known ID")
}

func TestSessionManager_GetOrCreate_AutoCreate(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	s := sm.GetOrCreate("")
	require.NotNil(t, s)
	assert.NotEmpty(t, s.ID)
}

func TestSessionManager_List_SortedByUpdatedAt(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	a := sm.Create()
	time.Sleep(10 * time.Millisecond)
	b := sm.Create()

	entries := sm.List()
	require.GreaterOrEqual(t, len(entries), 2)
	// b created after a → b should be first (newest first)
	assert.Equal(t, b.ID, entries[0].ID)
	_ = a
}

func TestSessionManager_List_IncludesGoal(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	s := sm.Create()
	s.Goal = "build a web app"
	sm.ArchiveTurn(s)

	entries := sm.List()
	require.GreaterOrEqual(t, len(entries), 1)
	var found bool
	for _, e := range entries {
		if e.ID == s.ID {
			assert.Equal(t, "build a web app", e.Goal)
			found = true
		}
	}
	assert.True(t, found)
}

func TestSessionManager_LoadHistory(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	s := sm.Create()
	s.Goal = "test"
	s.turnMessages = []StoredMessage{{Role: "user", Content: "hello"}}
	sm.ArchiveTurn(s)

	history, err := sm.LoadHistory(s.ID)
	require.NoError(t, err)
	assert.Equal(t, "test", history.Goal)
	require.Len(t, history.Turns, 1)
	assert.Equal(t, "hello", history.Turns[0].Messages[0].Content)
}

func TestSessionManager_LoadHistory_NotFound(t *testing.T) {
	sm := NewSessionManager(t.TempDir())
	_, err := sm.LoadHistory("nonexistent")
	assert.Error(t, err)
}

func TestSessionManager_PersistenceAcrossInstances(t *testing.T) {
	dir := t.TempDir()

	sm1 := NewSessionManager(dir)
	s1 := sm1.Create()
	s1.Goal = "persist me"
	s1.turnMessages = []StoredMessage{{Role: "user", Content: "hi"}}
	sm1.ArchiveTurn(s1)

	// New instance loads persisted
	sm2 := NewSessionManager(dir)
	history, err := sm2.LoadHistory(s1.ID)
	require.NoError(t, err)
	assert.Equal(t, "persist me", history.Goal)
	require.Len(t, history.Turns, 1)
}

// ── Batch 2: HITL + Token + Plan ──

func newTestSession() *ChatSession {
	return &ChatSession{ID: "test-session", hitlCh: make(chan string, 1)}
}

func TestChatSession_RespondHITL_WhileWaiting(t *testing.T) {
	s := newTestSession()
	s.hitlWaiting = true
	assert.True(t, s.RespondHITL("yes"))
	assert.False(t, s.hitlWaiting)
}

func TestChatSession_RespondHITL_NotWaiting(t *testing.T) {
	s := newTestSession()
	assert.False(t, s.RespondHITL("yes"))
}

func TestChatSession_RespondHITL_ChannelFull(t *testing.T) {
	s := newTestSession()
	s.hitlWaiting = true
	s.hitlCh <- "blocking" // fill channel
	assert.False(t, s.RespondHITL("overflow"), "should not block when channel is full")
}

func TestChatSession_IsHITLWaiting(t *testing.T) {
	s := newTestSession()
	assert.False(t, s.IsHITLWaiting())
	s.hitlWaiting = true
	assert.True(t, s.IsHITLWaiting())
}

func TestChatSession_AddTokenReport(t *testing.T) {
	s := newTestSession()
	s.addTokenReport(TokenReport{Identity: "main", Input: 10, Output: 5, Reason: 2, Total: 17})
	assert.Equal(t, int64(17), s.tokenTotal.Total)
	assert.Equal(t, 1, len(s.tokenReports))
	assert.Equal(t, "main", s.tokenReports[0].Identity)
}

func TestChatSession_AddTokenReport_Accumulates(t *testing.T) {
	s := newTestSession()
	s.addTokenReport(TokenReport{Identity: "main", Input: 10, Output: 5, Total: 15})
	s.addTokenReport(TokenReport{Identity: "worker", Input: 20, Output: 8, Total: 28})
	assert.Equal(t, 2, len(s.tokenReports))
	assert.Greater(t, s.tokenTotal.Total, int64(15))
}

func TestChatSession_AddTurnPlan_Upsert(t *testing.T) {
	s := newTestSession()
	s.addTurnPlan(StoredPlan{PlanID: "p1", Name: "Plan A"})
	s.addTurnPlan(StoredPlan{PlanID: "p1", Name: "Plan A v2"})
	assert.Len(t, s.turnPlans, 1, "should replace, not append")
	assert.Equal(t, "Plan A v2", s.turnPlans[0].Name)
}

func TestChatSession_AddTurnPlan_Append(t *testing.T) {
	s := newTestSession()
	s.addTurnPlan(StoredPlan{PlanID: "p1", Name: "Plan A"})
	s.addTurnPlan(StoredPlan{PlanID: "p2", Name: "Plan B"})
	assert.Len(t, s.turnPlans, 2)
}
