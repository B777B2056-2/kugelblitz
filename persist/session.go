package persist

import (
	"bytes"
	"encoding/json"
	"fmt"

	"kugelblitz/core"
)

// ---- JSONL event format ----
//
// Sessions are stored as JSONL — one JSON object per line:
//
//	{"type":"init","session_id":"xxx"}
//	{"type":"summary","summary":"prior context..."}
//	{"type":"msg","message":{...full core.Message JSON...}}

type sessionEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	Summary   string          `json:"summary,omitempty"`
}

// SaveSessionJSONL serializes a session's messages and summary as JSONL,
// then persists via the global Manager.
func SaveSessionJSONL(sessionID string, summary string, messages []core.Message) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	if err := enc.Encode(sessionEvent{Type: "init", SessionID: sessionID}); err != nil {
		return fmt.Errorf("session persist: %w", err)
	}
	if summary != "" {
		if err := enc.Encode(sessionEvent{Type: "summary", Summary: summary}); err != nil {
			return fmt.Errorf("session persist: %w", err)
		}
	}
	for _, msg := range messages {
		rawMsg, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("session persist: marshal message: %w", err)
		}
		if err := enc.Encode(sessionEvent{Type: "msg", Message: rawMsg}); err != nil {
			return fmt.Errorf("session persist: encode message: %w", err)
		}
	}

	return GetManager().SaveSession(sessionID, buf.Bytes())
}

// LoadSessionJSONL loads a session's JSONL data and reconstructs the
// summary and message list. Returns nil if not found.
func LoadSessionJSONL(sessionID string) (summary string, messages []core.Message, _ error) {
	data, err := GetManager().LoadSession(sessionID)
	if err != nil {
		return "", nil, nil // not found
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	for dec.More() {
		var evt sessionEvent
		if err := dec.Decode(&evt); err != nil {
			return "", nil, fmt.Errorf("session load: %w", err)
		}
		switch evt.Type {
		case "summary":
			summary = evt.Summary
		case "msg":
			var msg core.Message
			if err := json.Unmarshal(evt.Message, &msg); err != nil {
				return "", nil, fmt.Errorf("session load: unmarshal message: %w", err)
			}
			messages = append(messages, msg)
		}
	}

	return summary, messages, nil
}
