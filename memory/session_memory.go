package memory

import (
	"context"
	"fmt"
	"sync"
	"unicode/utf8"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/persist"
)

// ---- Config ----

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

// SessionID returns the unique identifier for this session.
func (s *SessionMemory) SessionID() string { return s.sessionID }

// Summary returns the current compression summary (empty if never compressed).
func (s *SessionMemory) Summary() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.summary
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
		sumMsg := core.NewUserMessage(core.TextContent{Text: s.summary})
		sumMsg.Role = "system"
		return append([]core.Message{sumMsg}, base...)
	}
	return base
}

// Compress delegates to a Compressor to summarize old messages and
// replaces them with a compact summary. Recent messages (last KeepLastN)
// are preserved. Returns the LLM token usage from the summarization call.
func (s *SessionMemory) Compress(ctx context.Context, c *Compressor, keepLastN, minToCompress int) (*core.Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := len(s.historyMessages)
	if total <= keepLastN {
		return nil, nil
	}

	splitAt := total - keepLastN
	old := s.historyMessages[:splitAt]
	recent := s.historyMessages[splitAt:]

	if len(old) < minToCompress {
		return nil, nil
	}

	newSummary, usage, err := c.Summarize(ctx, old, s.summary)
	if err != nil {
		return usage, fmt.Errorf("compress: %w", err)
	}

	s.summary = newSummary // already consolidated by the LLM
	s.historyMessages = make([]core.Message, len(recent))
	copy(s.historyMessages, recent)

	// Auto-persist: summaries are expensive (LLM call), don't lose them
	return usage, s.Persist()
}

// CompressToolResult compresses oversized string fields in a tool result via the LLM.
// Fields exceeding maxChars are summarized in-place. Error fields are never compressed.
func (s *SessionMemory) CompressToolResult(
	ctx context.Context, c *Compressor, maxChars int, result *core.ToolCallResult,
) {
	if maxChars <= 0 {
		return
	}
	if _, isErr := result.Outputs["error"]; isErr {
		return
	}
	for k, v := range result.Outputs {
		raw, ok := v.(string)
		if !ok || utf8.RuneCountInString(raw) <= maxChars {
			continue
		}
		summary, err := c.SummarizeToolResultField(ctx, result.ToolName, k, raw)
		if err != nil {
			continue
		}
		result.Outputs[k] = summary
	}
}

// Persist saves the session to disk via the persist package.
func (s *SessionMemory) Persist() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return persist.SaveSessionJSONL(s.sessionID, s.summary, s.historyMessages)
}

// LoadSessionMemory loads a session from disk.
// Returns nil if the session file does not exist.
func LoadSessionMemory(sessionID string) (*SessionMemory, error) {
	summary, messages, err := persist.LoadSessionJSONL(sessionID)
	if err != nil || (summary == "" && len(messages) == 0) {
		return nil, err
	}
	mem := newSessionMemory(sessionID)
	mem.summary = summary
	mem.historyMessages = messages
	return mem, nil
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

// CreateSessionMemory returns the session for the given ID, creating it
// if it does not already exist.
func (smm *SessionMemoryManager) CreateSessionMemory(sessionID string) *SessionMemory {
	if mem, ok := smm.GetSessionMemory(sessionID); ok {
		return mem
	}
	mem := newSessionMemory(sessionID)
	smm.SessionMemoryMap.Store(sessionID, mem)
	return mem
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
