package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/google/uuid"
)

// ══════════════════════════════════════════════════
// Persistent session storage (webui/sessions/)
// ══════════════════════════════════════════════════

// StoredSession is the Web UI's own session record, persisted to disk.
type StoredSession struct {
	ID         string       `json:"id"`
	Goal       string       `json:"goal"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
	Turns      []StoredTurn `json:"turns"`
	TotalUsage StoredUsage  `json:"total_usage"`
}

// StoredTurn represents a single AgentLoop execution (one POST /api/chat).
type StoredTurn struct {
	Goal     string          `json:"goal"`
	Messages []StoredMessage `json:"messages"`
	Plans    []StoredPlan    `json:"plans"`
	Usage    StoredUsage     `json:"usage"`
	EndedAt  time.Time       `json:"ended_at"`
}

// StoredMessage is a simplified message for UI rendering.
type StoredMessage struct {
	Role      string `json:"role"`                 // user | assistant | think | tool_call | tool_result | system | error
	Content   string `json:"content"`              // rendered markdown/HTML
	MediaType string `json:"media_type,omitempty"` // "image" | "audio"
	MediaPath string `json:"media_path,omitempty"` // file path reference
	MimeType  string `json:"mime_type,omitempty"`  // "image/png", "audio/mp3"
	ToolName  string `json:"tool_name,omitempty"`
	ToolArgs  any    `json:"tool_args,omitempty"`
	ToolOut   any    `json:"tool_out,omitempty"`
}

// StoredPlan records a plan that existed during a turn.
type StoredPlan struct {
	PlanID string       `json:"plan_id"`
	Name   string       `json:"name"`
	Status string       `json:"status"`
	Tasks  []StoredTask `json:"tasks"`
}

// StoredTask is a task within a stored plan.
type StoredTask struct {
	ID     string `json:"id"`
	Goal   string `json:"goal"`
	Status string `json:"status"`
}

// StoredUsage records token usage.
type StoredUsage struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Total     int64 `json:"total"`
}

// ══════════════════════════════════════════════════
// ChatSession (in-memory, active)
// ══════════════════════════════════════════════════

// ChatSession holds the state for a single active chat session.
type ChatSession struct {
	ID                 string
	FrameworkSessionID string // AgentLoop.SessionID, populated after first turn
	Goal               string

	// HITL
	hitlCh      chan string
	hitlWaiting bool
	hitlInfo    *HitlInfo

	// Token tracking (accumulates across turns)
	tokenReports []TokenReport
	tokenTotal   TokenTotals

	// Turn accumulator — built during SSE streaming
	turnMessages []StoredMessage
	turnPlans    []StoredPlan
	turnUsage    StoredUsage

	// Current plan state (derived from tool results, not memory/working)
	currentPlan *UIPlanState

	// Lifecycle
	cancelFn func()
	mu       sync.Mutex
}

// UIPlanState tracks the current plan within an active turn.
type UIPlanState struct {
	PlanID string
	Name   string
	Status string
	Tasks  map[string]*UIPlanTask
}

// UIPlanTask is a single task in the current plan.
type UIPlanTask struct {
	ID     string
	Goal   string
	Status string
}

// ══════════════════════════════════════════════════
// SessionManager
// ══════════════════════════════════════════════════

// SessionManager manages chat sessions with its own persistent storage.
type SessionManager struct {
	mu         sync.RWMutex
	sessions   map[string]*ChatSession
	storageDir string
}

// NewSessionManager creates a SessionManager and loads persisted sessions.
func NewSessionManager(storageDir string) *SessionManager {
	sm := &SessionManager{
		sessions:   make(map[string]*ChatSession),
		storageDir: storageDir,
	}
	_ = os.MkdirAll(storageDir, 0755)

	// Load persisted sessions into the in-memory map (lightweight: only metadata)
	entries, err := os.ReadDir(storageDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			id := e.Name()[:len(e.Name())-5] // strip .json
			if id == "" {
				continue
			}
			ss, err := sm.loadStoredSession(id)
			if err != nil {
				continue
			}
			sm.sessions[id] = &ChatSession{
				ID:     ss.ID,
				Goal:   ss.Goal,
				hitlCh: make(chan string, 1),
			}
		}
	}

	core.Info("session manager ready", "count", len(sm.sessions))
	return sm
}

// sessionPath returns the file path for a session ID.
func (sm *SessionManager) sessionPath(id string) string {
	return filepath.Join(sm.storageDir, id+".json")
}

func (sm *SessionManager) loadStoredSession(id string) (*StoredSession, error) {
	data, err := os.ReadFile(sm.sessionPath(id))
	if err != nil {
		return nil, err
	}
	var ss StoredSession
	if err := json.Unmarshal(data, &ss); err != nil {
		return nil, err
	}
	return &ss, nil
}

func (sm *SessionManager) saveStoredSession(ss *StoredSession) error {
	data, err := json.MarshalIndent(ss, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sm.sessionPath(ss.ID), data, 0644)
}

// ═══ Public API ═══

// Create creates a new session and persists an empty record.
func (sm *SessionManager) Create() *ChatSession {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	id := uuid.New().String()[:8]
	s := &ChatSession{
		ID:     id,
		hitlCh: make(chan string, 1),
	}
	sm.sessions[id] = s

	ss := &StoredSession{
		ID:        id,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_ = sm.saveStoredSession(ss)

	core.Debug("session created", "id", id)
	return s
}

// Get returns the session with the given ID, or nil.
func (sm *SessionManager) Get(id string) *ChatSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[id]
}

// List returns all sessions sorted by most-recently-updated first.
func (sm *SessionManager) List() []SessionListEntry {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make([]SessionListEntry, 0, len(sm.sessions))
	for id := range sm.sessions {
		ss, err := sm.loadStoredSession(id)
		if err != nil {
			result = append(result, SessionListEntry{ID: id, Goal: sm.sessions[id].Goal})
			continue
		}
		result = append(result, SessionListEntry{
			ID:           ss.ID,
			Goal:         ss.Goal,
			CreatedAt:    ss.CreatedAt,
			UpdatedAt:    ss.UpdatedAt,
			MessageCount: ss.messageCount(),
			TotalTokens:  ss.TotalUsage.Total,
			TurnCount:    len(ss.Turns),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result
}

// LoadHistory returns the full stored session including all messages.
func (sm *SessionManager) LoadHistory(id string) (*StoredSession, error) {
	sm.mu.RLock()
	if _, ok := sm.sessions[id]; !ok {
		sm.mu.RUnlock()
		return nil, fmt.Errorf("session %q not found", id)
	}
	sm.mu.RUnlock()
	return sm.loadStoredSession(id)
}

// Delete removes a session from memory and disk.
func (sm *SessionManager) Delete(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, id)
	_ = os.Remove(sm.sessionPath(id))
	core.Info("session deleted", "id", id)
}

// GetOrCreate returns the session by ID, or creates a new one.
func (sm *SessionManager) GetOrCreate(id string) *ChatSession {
	s := sm.Get(id)
	if s == nil {
		s = sm.Create()
	}
	return s
}

// ArchiveTurn appends a completed turn to the persisted session.
func (sm *SessionManager) ArchiveTurn(session *ChatSession) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	ss, err := sm.loadStoredSession(session.ID)
	if err != nil {
		ss = &StoredSession{
			ID:        session.ID,
			CreatedAt: time.Now(),
		}
	}

	turn := StoredTurn{
		Goal:     session.Goal,
		Messages: session.turnMessages,
		Plans:    session.turnPlans,
		Usage:    session.turnUsage,
		EndedAt:  time.Now(),
	}

	ss.Goal = session.Goal
	ss.UpdatedAt = time.Now()
	ss.Turns = append(ss.Turns, turn)
	ss.TotalUsage.Input += session.turnUsage.Input
	ss.TotalUsage.Output += session.turnUsage.Output
	ss.TotalUsage.Reasoning += session.turnUsage.Reasoning
	ss.TotalUsage.Total += session.turnUsage.Total

	_ = sm.saveStoredSession(ss)
}

// ═══ SessionListEntry (API response) ═══

type SessionListEntry struct {
	ID           string    `json:"id"`
	Goal         string    `json:"goal,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
	TotalTokens  int64     `json:"total_tokens"`
	TurnCount    int       `json:"turn_count"`
}

func (ss *StoredSession) messageCount() int {
	n := 0
	for _, t := range ss.Turns {
		n += len(t.Messages)
	}
	return n
}

// ═══ HITL helpers ═══

func (s *ChatSession) RespondHITL(response string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hitlWaiting {
		return false
	}
	select {
	case s.hitlCh <- response:
		s.hitlWaiting = false
		s.hitlInfo = nil
		return true
	default:
		return false
	}
}

func (s *ChatSession) IsHITLWaiting() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hitlWaiting
}

// ═══ Token tracking ═══

// TokenTotals holds aggregated token counts.
type TokenTotals struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Total     int64 `json:"total"`
}

func (s *ChatSession) addTokenReport(report TokenReport) {
	s.mu.Lock()
	defer s.mu.Unlock()

	isIntent := len(s.tokenReports) == 0

	s.tokenTotal.Output += report.Output
	if isIntent {
		s.tokenTotal.Input += report.Input
	} else {
		s.tokenTotal.Output += report.Input
	}
	s.tokenTotal.Reasoning += report.Reason
	s.tokenTotal.Total += report.Total

	s.turnUsage.Output += report.Output
	if isIntent {
		s.turnUsage.Input += report.Input
	} else {
		s.turnUsage.Output += report.Input
	}
	s.turnUsage.Reasoning += report.Reason
	s.turnUsage.Total += report.Total

	s.tokenReports = append(s.tokenReports, report)
}

// ═══ Turn message builders ═══

func (s *ChatSession) addTurnMessage(msg StoredMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnMessages = append(s.turnMessages, msg)
}

func (s *ChatSession) addTurnPlan(plan StoredPlan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Replace plan with same ID
	for i, p := range s.turnPlans {
		if p.PlanID == plan.PlanID {
			s.turnPlans[i] = plan
			return
		}
	}
	s.turnPlans = append(s.turnPlans, plan)
}
