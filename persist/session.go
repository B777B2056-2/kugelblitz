package persist

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/B777B2056-2/kugelblitz/core"
)

func SaveSessionJSONL(sessionID string, summary string, messages []core.Message) error {
	mgr := GetManager()
	var events []JSONLEvent
	initPayload, _ := json.Marshal(map[string]string{"session_id": sessionID})
	events = append(events, JSONLEvent{Type: "init", Payload: initPayload})
	if summary != "" {
		sumPayload, _ := json.Marshal(map[string]string{"summary": summary})
		events = append(events, JSONLEvent{Type: "summary", Payload: sumPayload})
	}
	for _, msg := range messages {
		msgPayload, _ := json.Marshal(msg)
		events = append(events, JSONLEvent{Type: "msg", Payload: msgPayload})
	}
	return mgr.JSONL().WriteAll(context.Background(), filepath.Join("memory", "sessions", sessionID+".jsonl"), events)
}

func LoadSessionJSONL(sessionID string) (summary string, messages []core.Message, _ error) {
	mgr := GetManager()
	events, err := mgr.JSONL().ReadAll(filepath.Join("memory", "sessions", sessionID+".jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, nil
		}
		return "", nil, err
	}
	for _, evt := range events {
		switch evt.Type {
		case "summary":
			var s struct {
				Summary string `json:"summary"`
			}
			if err := json.Unmarshal(evt.Payload, &s); err != nil {
				return "", nil, fmt.Errorf("load session: unmarshal summary: %w", err)
			}
			summary = s.Summary
		case "msg":
			var msg core.Message
			if err := json.Unmarshal(evt.Payload, &msg); err != nil {
				return "", nil, fmt.Errorf("load session: unmarshal message: %w", err)
			}
			messages = append(messages, msg)
		}
	}
	return summary, messages, nil
}

func ListSessions() ([]string, error) {
	mgr := GetManager()
	keys, err := mgr.JSONL().List(context.Background(), filepath.Join("memory", "sessions"))
	if err != nil {
		return nil, err
	}
	// List returns full prefixed paths; strip to bare session IDs (P11).
	var ids []string
	for _, k := range keys {
		ids = append(ids, filepath.Base(k))
	}
	return ids, nil
}

func DeleteSession(sessionID string) error {
	mgr := GetManager()
	return mgr.JSONL().Delete(context.Background(), filepath.Join("memory", "sessions", sessionID+".jsonl"))
}
