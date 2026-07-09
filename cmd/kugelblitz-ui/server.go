package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/B777B2056-2/kugelblitz/core"
)

//go:embed static/*
var staticFiles embed.FS

// Server is the HTTP server for the Kugelblitz Web UI.
type Server struct {
	mux      *http.ServeMux
	sessions *SessionManager
}

// NewServer creates and configures a new Server.
func NewServer() *Server {
	storageDir := core.GetWorkspace().Dir() + "/webui/sessions"
	s := &Server{
		mux:      http.NewServeMux(),
		sessions: NewSessionManager(storageDir),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Static files
	staticFS, _ := fs.Sub(staticFiles, "static")
	s.mux.Handle("GET /", http.FileServer(http.FS(staticFS)))

	// Session management
	s.mux.HandleFunc("POST /api/session", s.handleCreateSession)
	s.mux.HandleFunc("GET /api/session", s.handleListSessions)
	s.mux.HandleFunc("GET /api/session/{id}", s.handleGetSession)
	s.mux.HandleFunc("DELETE /api/session/{id}", s.handleDeleteSession)

	// Chat (SSE streaming)
	s.mux.HandleFunc("POST /api/chat", s.handleChat)

	// Human-in-the-loop
	s.mux.HandleFunc("POST /api/hitl/{session_id}", s.handleHITL)
	s.mux.HandleFunc("GET /api/hitl/{session_id}/status", s.handleHITLStatus)

	// Cancel running agent
	s.mux.HandleFunc("POST /api/cancel/{session_id}", s.handleCancel)

	// Settings — config
	s.mux.HandleFunc("GET /api/settings/config", s.handleGetConfig)
	s.mux.HandleFunc("PUT /api/settings/config", s.handlePutConfig)

	// Settings — multimodal availability
	s.mux.HandleFunc("GET /api/settings/multimodal", s.handleGetMultimodalConfig)

	// Settings — files
	s.mux.HandleFunc("GET /api/settings/files", s.handleSettingsFiles)
	s.mux.HandleFunc("GET /api/settings/file/{name}", s.handleSettingsGetFile)
	s.mux.HandleFunc("PUT /api/settings/file/{name}", s.handleSettingsPutFile)
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	core.Info("server starting", "addr", addr)
	return http.ListenAndServe(addr, s.mux)
}

// ── Session handlers ──

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	session := s.sessions.Create()
	writeJSON(w, http.StatusCreated, map[string]string{
		"session_id": session.ID,
	})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	entries := s.sessions.List()
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	ss, err := s.sessions.LoadHistory(id)
	if err != nil {
		core.Warn("session not found", "id", id)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	writeJSON(w, http.StatusOK, ss)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.sessions.Delete(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ── Helper ──

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
