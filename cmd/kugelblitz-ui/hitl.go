package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// handleHITL processes a human-in-the-loop response.
func (s *Server) handleHITL(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}

	session := s.sessions.Get(sessionID)
	if session == nil {
		log.Printf("[hitl] unknown session %q", sessionID)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	if !session.IsHITLWaiting() {
		log.Printf("[hitl] session %q not waiting", sessionID)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "session is not waiting for human input"})
		return
	}

	var req struct {
		ToolCallID string `json:"tool_call_id,omitempty"`
		Response   string `json:"response"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.Response == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "response is required"})
		return
	}

	if !session.RespondHITL(req.Response) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to deliver response"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleHITLStatus checks if a session is waiting for human input.
func (s *Server) handleHITLStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	session := s.sessions.Get(sessionID)
	if session == nil {
		log.Printf("[hitl] status for unknown session %q", sessionID)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"waiting":  session.hitlWaiting,
		"question": session.hitlInfo,
	})
}

// handleCancel cancels a running AgentLoop for the given session.
func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	session := s.sessions.Get(sessionID)
	if session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	session.mu.Lock()
	if session.cancelFn == nil {
		session.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "no active agent to cancel"})
		return
	}
	session.cancelFn()
	session.cancelFn = nil
	session.mu.Unlock()

	log.Printf("[hitl] cancelled session %q", sessionID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}
