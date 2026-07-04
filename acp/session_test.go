package acp

import (
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionManager_Create(t *testing.T) {
	sm := NewSessionManager()
	s := sm.Create("/home/test", nil)
	assert.NotEmpty(t, s.ID)
	assert.Equal(t, "/home/test", s.Cwd)
	assert.NotNil(t, s.CreatedAt)
}

func TestSessionManager_Create_DefaultCwd(t *testing.T) {
	sm := NewSessionManager()
	s := sm.Create("", nil)
	// Empty cwd is valid — caller should set it explicitly
	assert.NotNil(t, s)
}

func TestSessionManager_Load_Found(t *testing.T) {
	sm := NewSessionManager()
	s := sm.Create("/tmp", nil)
	loaded, err := sm.Load(s.ID)
	require.NoError(t, err)
	assert.Equal(t, s.ID, loaded.ID)
}

func TestSessionManager_Load_NotFound(t *testing.T) {
	sm := NewSessionManager()
	_, err := sm.Load("nonexistent")
	assert.Error(t, err)
}

func TestSessionManager_Delete(t *testing.T) {
	sm := NewSessionManager()
	s := sm.Create("/tmp", nil)
	require.NoError(t, sm.Delete(s.ID))
	_, err := sm.Load(s.ID)
	assert.Error(t, err)
}

func TestSessionManager_Delete_NotFound(t *testing.T) {
	sm := NewSessionManager()
	err := sm.Delete("nonexistent")
	assert.Error(t, err)
}

func TestSessionManager_AppendMessage(t *testing.T) {
	sm := NewSessionManager()
	s := sm.Create("/tmp", nil)
	msg := core.NewUserMessage(core.TextContent{Text: "hello"})
	require.NoError(t, sm.AppendMessage(s.ID, msg))
	assert.Len(t, s.Messages, 1)
	assert.Equal(t, "hello", s.Messages[0].Content.(core.TextContent).Text)
}

func TestSessionManager_AppendMessage_NotFound(t *testing.T) {
	sm := NewSessionManager()
	err := sm.AppendMessage("nonexistent", core.Message{})
	assert.Error(t, err)
}

func TestSessionManager_List(t *testing.T) {
	sm := NewSessionManager()
	sm.Create("/a", nil)
	sm.Create("/b", nil)
	assert.Len(t, sm.List(), 2)
}

func TestSessionManager_Cancel(t *testing.T) {
	sm := NewSessionManager()
	s := sm.Create("/tmp", nil)
	called := false
	s.cancelFn = func() { called = true }
	require.NoError(t, sm.Cancel(s.ID))
	assert.True(t, called)
}

func TestSessionManager_Cancel_NoopWhenNoCancelFn(t *testing.T) {
	sm := NewSessionManager()
	s := sm.Create("/tmp", nil)
	require.NoError(t, sm.Cancel(s.ID))
}

func TestSessionManager_SetCancelFunc(t *testing.T) {
	sm := NewSessionManager()
	s := sm.Create("/tmp", nil)
	called := false
	require.NoError(t, sm.SetCancelFunc(s.ID, func() { called = true }))
	_ = sm.Cancel(s.ID)
	assert.True(t, called)
}

func TestSessionManager_SetCancelFunc_NotFound(t *testing.T) {
	sm := NewSessionManager()
	err := sm.SetCancelFunc("nonexistent", func() {})
	assert.Error(t, err)
}
