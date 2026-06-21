package memory

import (
	"context"
	"fmt"

	"kugelblitz/core"
	"kugelblitz/utils"
	"sync"
)

// ---- Config ----

// CompressConfig controls how compression is performed.
type CompressConfig struct {
	// KeepLastN is the number of most recent messages to keep uncompressed.
	// Messages beyond this window are consolidated into a summary.
	KeepLastN int

	// MinMessagesToCompress is the minimum number of old messages needed
	// before compression is triggered. Prevents summarizing tiny histories.
	MinMessagesToCompress int
}

// DefaultCompressConfig returns sensible defaults.
func DefaultCompressConfig() CompressConfig {
	return CompressConfig{
		KeepLastN:             10,
		MinMessagesToCompress: 5,
	}
}

// ---- SessionMemory ----

type SessionMemory struct {
	sessionID       string
	historyMessages []core.Message
	summary         string // accumulated summary from previous compressions
	mu              sync.RWMutex
}

func newSessionMemory(sessionID string) *SessionMemory {
	return &SessionMemory{
		sessionID: sessionID,
	}
}

func (s *SessionMemory) AppendMessages(messages []core.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.historyMessages = append(s.historyMessages, messages...)
}

func (s *SessionMemory) AppendMessage(message core.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.historyMessages = append(s.historyMessages, message)
}

// GetHistoryMessages returns all messages in the session.
// If a summary exists from prior compressions, it is prepended as a system message.
func (s *SessionMemory) GetHistoryMessages() []core.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	base := make([]core.Message, len(s.historyMessages))
	copy(base, s.historyMessages)

	if s.summary != "" {
		sumMsg := core.NewUserMessage(s.sessionID, core.TextContent{Text: s.summary})
		sumMsg.Role = "system"
		return append([]core.Message{sumMsg}, base...)
	}
	return base
}

// Compress delegates to a Compressor to summarize old messages and
// replaces them with a compact summary. Recent messages (last KeepLastN)
// are preserved.
func (s *SessionMemory) Compress(ctx context.Context, c *Compressor, cfg CompressConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := len(s.historyMessages)
	if total <= cfg.KeepLastN {
		return nil
	}

	splitAt := total - cfg.KeepLastN
	old := s.historyMessages[:splitAt]
	recent := s.historyMessages[splitAt:]

	if len(old) < cfg.MinMessagesToCompress {
		return nil
	}

	newSummary, err := c.Summarize(ctx, old, s.summary)
	if err != nil {
		return fmt.Errorf("compress: %w", err)
	}

	s.summary = newSummary // already consolidated by the LLM
	s.historyMessages = make([]core.Message, len(recent))
	copy(s.historyMessages, recent)

	// Auto-persist: summaries are expensive (LLM call), don't lose them
	return s.Persist()
}

// ---- Manager ----

var (
	SessionMemoryManagerOnce sync.Once
	SessionMemoryManagerInst *SessionMemoryManager
)

type SessionMemoryManager struct {
	SessionMemoryMap sync.Map
}

func GetSessionMemoryManager() *SessionMemoryManager {
	SessionMemoryManagerOnce.Do(func() {
		SessionMemoryManagerInst = &SessionMemoryManager{}
	})
	return SessionMemoryManagerInst
}

func (smm *SessionMemoryManager) CreateSessionMemory() string {
	sessionID := utils.GenerateSessionID()
	SessionMemory := newSessionMemory(sessionID)
	smm.SessionMemoryMap.Store(sessionID, SessionMemory)
	return sessionID
}

func (smm *SessionMemoryManager) GetSessionMemory(sessionID string) (*SessionMemory, bool) {
	// In-memory hit
	if obj, ok := smm.SessionMemoryMap.Load(sessionID); ok {
		return obj.(*SessionMemory), true
	}

	// Try disk — session may have been persisted before restart
	mem, err := LoadSessionMemory(sessionID)
	if err != nil || mem == nil {
		return nil, false
	}

	smm.SessionMemoryMap.Store(sessionID, mem)
	return mem, true
}
