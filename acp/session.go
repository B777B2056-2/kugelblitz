package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/utils"
)

// Session represents an ACP conversation session.
type Session struct {
	ID        string          `json:"id"`
	Cwd       string          `json:"cwd"`
	Messages  []core.Message  `json:"messages"`
	CreatedAt time.Time       `json:"created_at"`
	Agent     core.IAgent     `json:"-"`
	cancelFn  context.CancelFunc
}

// sessionData is the JSON-serializable portion of a Session.
type sessionData struct {
	ID        string         `json:"id"`
	Cwd       string         `json:"cwd"`
	Messages  []core.Message `json:"messages"`
	CreatedAt time.Time      `json:"created_at"`
}

// SessionManager manages ACP session lifecycle and persistence.
// It uses core.Workspace to persist sessions to disk.
type SessionManager struct {
	sessions  map[string]*Session
	mu        sync.RWMutex
	workspace *core.Workspace
}

// NewSessionManager creates a new SessionManager using the given workspace
// for session file storage.
func NewSessionManager(ws *core.Workspace) *SessionManager {
	if ws == nil {
		ws = core.GetWorkspace()
	}
	return &SessionManager{
		sessions:  make(map[string]*Session),
		workspace: ws,
	}
}

// Create creates a new session with the given cwd and agent.
// If cwd is empty, the workspace directory is used as default.
func (sm *SessionManager) Create(cwd string, agent core.IAgent) (*Session, error) {
	if cwd == "" {
		cwd = sm.workspace.Dir()
	}

	session := &Session{
		ID:        utils.GenerateSessionID(),
		Cwd:       cwd,
		Messages:  nil,
		CreatedAt: time.Now(),
		Agent:     agent,
	}

	sm.mu.Lock()
	sm.sessions[session.ID] = session
	sm.mu.Unlock()

	if err := sm.persist(session); err != nil {
		return nil, fmt.Errorf("session: persist error: %w", err)
	}

	return session, nil
}

// Load retrieves an existing session by ID. It first checks the in-memory map,
// then falls back to loading from disk via the workspace.
func (sm *SessionManager) Load(sessionID string) (*Session, error) {
	sm.mu.RLock()
	session, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()
	if ok {
		return session, nil
	}

	return sm.restore(sessionID)
}

// List returns summary info for all active sessions (both in-memory and on disk).
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

	// Also scan disk for persisted sessions not currently in memory
	diskSessions, _ := sm.workspaceSessionIDs()
	for _, id := range diskSessions {
		if _, ok := sm.sessions[id]; !ok {
			if sd, err := sm.loadData(id); err == nil {
				infos = append(infos, SessionInfo{
					SessionID: sd.ID,
					Cwd:       sd.Cwd,
					CreatedAt: sd.CreatedAt.Format(time.RFC3339),
				})
			}
		}
	}

	return infos
}

// Delete removes a session from memory and disk.
// Returns an error if the session does not exist in either memory or on disk.
func (sm *SessionManager) Delete(sessionID string) error {
	sm.mu.Lock()
	_, inMem := sm.sessions[sessionID]
	delete(sm.sessions, sessionID)
	sm.mu.Unlock()

	path := sm.workspace.SessionPath(sessionID)
	err := os.Remove(path)
	if os.IsNotExist(err) && !inMem {
		return fmt.Errorf("session: not found: %s", sessionID)
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("session: delete error: %w", err)
	}
	return nil
}

// AppendMessage adds a message to a session's history and persists the session.
func (sm *SessionManager) AppendMessage(sessionID string, msg core.Message) error {
	sm.mu.Lock()
	session, ok := sm.sessions[sessionID]
	if !ok {
		sm.mu.Unlock()
		return fmt.Errorf("session: not found: %s", sessionID)
	}
	session.Messages = append(session.Messages, msg)
	sm.mu.Unlock()

	return sm.persist(session)
}

// Cancel cancels the active execution for a session. It is a no-op if no
// execution is in progress.
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

// persist writes the session's JSON-serializable data to the workspace.
func (sm *SessionManager) persist(s *Session) error {
	data := sessionData{
		ID:        s.ID,
		Cwd:       s.Cwd,
		Messages:  s.Messages,
		CreatedAt: s.CreatedAt,
	}

	b, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// Ensure sessions directory exists
	if err := os.MkdirAll(sm.workspace.SessionsDir(), 0755); err != nil {
		return err
	}

	path := sm.workspace.SessionPath(s.ID)
	return os.WriteFile(path, b, 0644)
}

// restore reads session data from disk. The returned session will have a nil Agent.
func (sm *SessionManager) restore(sessionID string) (*Session, error) {
	data, err := sm.loadData(sessionID)
	if err != nil {
		return nil, fmt.Errorf("session: not found: %s", sessionID)
	}

	return &Session{
		ID:        data.ID,
		Cwd:       data.Cwd,
		Messages:  data.Messages,
		CreatedAt: data.CreatedAt,
		Agent:     nil, // caller must set the Agent after loading
	}, nil
}

// loadData reads and unmarshals session data from disk.
func (sm *SessionManager) loadData(sessionID string) (*sessionData, error) {
	path := sm.workspace.SessionPath(sessionID)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var data sessionData
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// workspaceSessionIDs scans the sessions directory for existing session files.
func (sm *SessionManager) workspaceSessionIDs() ([]string, error) {
	dir := sm.workspace.SessionsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var ids []string
	for _, entry := range entries {
		if !entry.IsDir() && len(entry.Name()) > 5 && entry.Name()[len(entry.Name())-5:] == ".json" {
			ids = append(ids, entry.Name()[:len(entry.Name())-5])
		}
	}
	return ids, nil
}
