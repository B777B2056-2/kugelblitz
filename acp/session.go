package acp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/utils"
)

// Session represents an ACP conversation session.
type Session struct {
	ID        string        `json:"id"`
	Cwd       string        `json:"cwd"`
	Messages  []core.Message `json:"messages"`
	CreatedAt time.Time     `json:"created_at"`
	Agent     core.IAgent   `json:"-"`
	cancelFn  context.CancelFunc
}

// SessionManager manages ACP session lifecycle in memory only.
// Session persistence is handled by memory.SessionMemory.
type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewSessionManager creates a new in-memory SessionManager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// Create creates a new session with the given cwd and agent.
func (sm *SessionManager) Create(cwd string, agent core.IAgent) *Session {
	session := &Session{
		ID:        utils.GenerateSessionID(),
		Cwd:       cwd,
		CreatedAt: time.Now(),
		Agent:     agent,
	}

	sm.mu.Lock()
	sm.sessions[session.ID] = session
	sm.mu.Unlock()

	return session
}

// Load retrieves an existing session by ID from the in-memory map.
func (sm *SessionManager) Load(sessionID string) (*Session, error) {
	sm.mu.RLock()
	session, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session: not found: %s", sessionID)
	}
	return session, nil
}

// List returns summary info for all active sessions.
func (sm *SessionManager) List() []SessionInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	infos := make([]SessionInfo, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		infos = append(infos, SessionInfo{
			SessionID: s.ID,
			Cwd:       s.Cwd,
			CreatedAt: s.CreatedAt.Format(time.RFC3339),
		})
	}
	return infos
}

// Delete removes a session from memory.
func (sm *SessionManager) Delete(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.sessions[sessionID]; !ok {
		return fmt.Errorf("session: not found: %s", sessionID)
	}
	delete(sm.sessions, sessionID)
	return nil
}

// AppendMessage adds a message to a session's history.
func (sm *SessionManager) AppendMessage(sessionID string, msg core.Message) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session: not found: %s", sessionID)
	}
	session.Messages = append(session.Messages, msg)
	return nil
}

// Cancel cancels the active execution for a session.
func (sm *SessionManager) Cancel(sessionID string) error {
	sm.mu.RLock()
	session, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session: not found: %s", sessionID)
	}
	if session.cancelFn != nil {
		session.cancelFn()
	}
	return nil
}

// SetCancelFunc sets the cancel function for the session's active execution.
func (sm *SessionManager) SetCancelFunc(sessionID string, fn context.CancelFunc) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session: not found: %s", sessionID)
	}
	session.cancelFn = fn
	return nil
}
