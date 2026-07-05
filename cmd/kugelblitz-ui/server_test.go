package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return NewServer()
}

// ── Session CRUD ──

func TestHandleCreateSession(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/session", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotEmpty(t, resp["session_id"])
}

func TestHandleListSessions(t *testing.T) {
	srv := newTestServer(t)

	// Create a session first
	createReq := httptest.NewRequest("POST", "/api/session", nil)
	createRec := httptest.NewRecorder()
	srv.mux.ServeHTTP(createRec, createReq)

	req := httptest.NewRequest("GET", "/api/session", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var entries []SessionListEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&entries))
	assert.GreaterOrEqual(t, len(entries), 1)
}

func TestHandleGetSession(t *testing.T) {
	srv := newTestServer(t)

	// Create a session
	createReq := httptest.NewRequest("POST", "/api/session", nil)
	createRec := httptest.NewRecorder()
	srv.mux.ServeHTTP(createRec, createReq)
	var created map[string]string
	_ = json.NewDecoder(createRec.Body).Decode(&created)

	req := httptest.NewRequest("GET", "/api/session/"+created["session_id"], nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGetSession_NotFound(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/session/nonexistent", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetSession_MissingID(t *testing.T) {
	srv := newTestServer(t)
	// Go 1.22+ routing: /api/session/{id} requires non-empty {id}
	req := httptest.NewRequest("GET", "/api/session/", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleDeleteSession(t *testing.T) {
	srv := newTestServer(t)

	createReq := httptest.NewRequest("POST", "/api/session", nil)
	createRec := httptest.NewRecorder()
	srv.mux.ServeHTTP(createRec, createReq)
	var created map[string]string
	_ = json.NewDecoder(createRec.Body).Decode(&created)

	delReq := httptest.NewRequest("DELETE", "/api/session/"+created["session_id"], nil)
	delRec := httptest.NewRecorder()
	srv.mux.ServeHTTP(delRec, delReq)
	assert.Equal(t, http.StatusOK, delRec.Code)

	// Verify deleted
	getReq := httptest.NewRequest("GET", "/api/session/"+created["session_id"], nil)
	getRec := httptest.NewRecorder()
	srv.mux.ServeHTTP(getRec, getReq)
	assert.Equal(t, http.StatusNotFound, getRec.Code)
}

// ── Chat validation ──

func TestHandleChat_MissingGoal(t *testing.T) {
	srv := newTestServer(t)
	body := `{"goal":""}`
	req := httptest.NewRequest("POST", "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleChat_InvalidJSON(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/chat", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ── HITL ──

func TestHandleHITL_MissingSessionID(t *testing.T) {
	srv := newTestServer(t)
	// Go 1.22+ routing: /api/hitl/{session_id} requires non-empty path
	body := `{"response":"yes"}`
	req := httptest.NewRequest("POST", "/api/hitl/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	// No matching route → 405 Method Not Allowed
	assert.NotEqual(t, http.StatusOK, rec.Code)
}

func TestHandleHITL_SessionNotFound(t *testing.T) {
	srv := newTestServer(t)
	body := `{"response":"yes"}`
	req := httptest.NewRequest("POST", "/api/hitl/nonexistent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleHITL_NotWaiting(t *testing.T) {
	srv := newTestServer(t)

	// Create a session — it's not in HITL state
	session := srv.sessions.Create()

	body := `{"response":"yes"}`
	req := httptest.NewRequest("POST", "/api/hitl/"+session.ID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestHandleHITL_MissingResponse(t *testing.T) {
	srv := newTestServer(t)

	session := srv.sessions.Create()
	// Simulate HITL waiting state
	session.hitlCh = make(chan string, 1)
	session.hitlWaiting = true

	body := `{"response":""}`
	req := httptest.NewRequest("POST", "/api/hitl/"+session.ID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleHITL_Success(t *testing.T) {
	srv := newTestServer(t)

	session := srv.sessions.Create()
	session.hitlCh = make(chan string, 1)
	session.hitlWaiting = true

	go func() {
		// Consume the response from the channel
		<-session.hitlCh
	}()

	body := `{"response":"yes, proceed"}`
	req := httptest.NewRequest("POST", "/api/hitl/"+session.ID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleHITL_InvalidJSON(t *testing.T) {
	srv := newTestServer(t)

	session := srv.sessions.Create()
	session.hitlWaiting = true

	req := httptest.NewRequest("POST", "/api/hitl/"+session.ID, strings.NewReader("bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ── HITL Status ──

func TestHandleHITLStatus_NotFound(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/hitl/nonexistent/status", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleHITLStatus_NotWaiting(t *testing.T) {
	srv := newTestServer(t)
	session := srv.sessions.Create()

	req := httptest.NewRequest("GET", "/api/hitl/"+session.ID+"/status", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	assert.Equal(t, false, resp["waiting"])
}

func TestHandleHITLStatus_Waiting(t *testing.T) {
	srv := newTestServer(t)
	session := srv.sessions.Create()
	session.hitlWaiting = true
	session.hitlInfo = &HitlInfo{Question: "proceed?", Reason: "need approval"}

	req := httptest.NewRequest("GET", "/api/hitl/"+session.ID+"/status", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	assert.Equal(t, true, resp["waiting"])
	assert.NotNil(t, resp["question"])
}

// ── Cancel ──

func TestHandleCancel_SessionNotFound(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/cancel/nonexistent", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleCancel_NoActiveAgent(t *testing.T) {
	srv := newTestServer(t)
	session := srv.sessions.Create()

	req := httptest.NewRequest("POST", "/api/cancel/"+session.ID, nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestHandleCancel_Success(t *testing.T) {
	srv := newTestServer(t)
	session := srv.sessions.Create()
	cancelCalled := false
	session.cancelFn = func() { cancelCalled = true }

	req := httptest.NewRequest("POST", "/api/cancel/"+session.ID, nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, cancelCalled, "cancel function should be called")
}

// ── writeJSON helper ──

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusTeapot, map[string]string{"tea": "earl grey"})

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	assert.Equal(t, "earl grey", resp["tea"])
}

// ── CORS / content-type check ──

func TestSessionEndpoints_ContentType(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("POST", "/api/session", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

// ── Static file serving ──

func TestStaticFiles_ServeIndex(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)
	// Should serve something (200, not 404)
	assert.NotEqual(t, http.StatusNotFound, rec.Code)
}
