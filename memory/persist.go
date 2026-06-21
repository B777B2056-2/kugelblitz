package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"kugelblitz/core"
)

// ---- JSONL format ----
//
// Each session is stored as one .jsonl file per session ID.
// Every line is a complete, self-describing JSON object:
//
//	{"type":"init","session_id":"xxx"}
//	{"type":"summary","summary":"prior context..."}
//	{"type":"msg","message":{...full core.Message JSON...}}
//
// Message lines use core.Message.MarshalJSON, preserving all content types:
// text, reasoning, tool_call, tool_result, multi_modal, and composite.

type persistEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	Summary   string          `json:"summary,omitempty"`
}

// ---- Config ----

type PersistConfig struct {
	Dir string
}

func DefaultPersistConfig() PersistConfig {
	return PersistConfig{Dir: filepath.Join(".kugelblitz", "sessions")}
}

var (
	persistDir   = DefaultPersistConfig().Dir
	persistDirMu sync.Mutex
)

func SetPersistDir(dir string) {
	persistDirMu.Lock()
	defer persistDirMu.Unlock()
	persistDir = dir
}

func getPersistDir() string {
	persistDirMu.Lock()
	defer persistDirMu.Unlock()
	return persistDir
}

func sessionFilePath(sessionID string) string {
	return filepath.Join(getPersistDir(), sessionID+".jsonl")
}

// ---- Save ----

// Persist writes the session's full state to a JSONL file.
// All message content types are preserved via core.Message.MarshalJSON.
func (s *SessionMemory) Persist() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := sessionFilePath(s.sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("persist: mkdir: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("persist: create: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)

	// Header
	if err := enc.Encode(persistEvent{Type: "init", SessionID: s.sessionID}); err != nil {
		return fmt.Errorf("persist: %w", err)
	}

	// Summary
	if s.summary != "" {
		if err := enc.Encode(persistEvent{Type: "summary", Summary: s.summary}); err != nil {
			return fmt.Errorf("persist: %w", err)
		}
	}

	// Messages — full fidelity via core.Message.MarshalJSON
	for _, msg := range s.historyMessages {
		rawMsg, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("persist: marshal message: %w", err)
		}
		if err := enc.Encode(persistEvent{Type: "msg", Message: rawMsg}); err != nil {
			return fmt.Errorf("persist: encode message: %w", err)
		}
	}

	return nil
}

// ---- Load ----

// LoadSessionMemory loads a session from its JSONL file.
// Returns nil if the file does not exist.
func LoadSessionMemory(sessionID string) (*SessionMemory, error) {
	path := sessionFilePath(sessionID)

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load: %w", err)
	}
	defer f.Close()

	mem := newSessionMemory(sessionID)
	dec := json.NewDecoder(f)

	for dec.More() {
		var evt persistEvent
		if err := dec.Decode(&evt); err != nil {
			return nil, fmt.Errorf("load: %w", err)
		}

		switch evt.Type {
		case "summary":
			mem.summary = evt.Summary
		case "msg":
			var msg core.Message
			if err := json.Unmarshal(evt.Message, &msg); err != nil {
				return nil, fmt.Errorf("load: unmarshal message: %w", err)
			}
			mem.historyMessages = append(mem.historyMessages, msg)
		}
	}

	return mem, nil
}
