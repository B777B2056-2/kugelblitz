package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer redirects the global workspace to a fresh temp dir and resets
// the config globals, so endpoint tests never touch ~/.kugelblitz. Both
// globals are restored via t.Cleanup so tests stay order-independent.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	oldDir := core.GetWorkspace().Dir()
	core.GetWorkspace().SetDir(t.TempDir())

	configMu.Lock()
	oldCfg, oldLoaded := currentConfig, configLoaded
	currentConfig = config.DefaultConfig()
	configLoaded = true
	configMu.Unlock()

	t.Cleanup(func() {
		core.GetWorkspace().SetDir(oldDir)
		configMu.Lock()
		currentConfig, configLoaded = oldCfg, oldLoaded
		configMu.Unlock()
	})

	return NewServer()
}

// doRequest is a tiny helper for exercising a single HTTP route.
func doRequest(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	return rec
}

// ── Settings files ──

func TestSettingsFiles_List(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, "GET", "/api/settings/files", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var files []SettingsFile
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&files))
	require.GreaterOrEqual(t, len(files), 1)

	names := map[string]bool{}
	for _, f := range files {
		names[f.Name] = true
	}
	assert.True(t, names["kugelblitz.yaml"], "kugelblitz.yaml should be editable")
	assert.True(t, names["MEMORY.md"], "MEMORY.md should be editable")
	assert.True(t, names["DREAMS.md"], "DREAMS.md should be editable")
}

func TestSettingsFile_Get_NotEditable(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, "GET", "/api/settings/file/nope.txt", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSettingsFile_Get_MissingReturnsEmpty(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, "GET", "/api/settings/file/MEMORY.md", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "MEMORY.md", resp["name"])
	assert.Equal(t, "", resp["content"], "missing file should yield empty content, not 404")
}

func TestSettingsFile_PutThenGet_RoundTrip(t *testing.T) {
	srv := newTestServer(t)
	content := "# Test Memory\n- fact one\n- fact two\n"

	put := doRequest(t, srv, "PUT", "/api/settings/file/MEMORY.md", `{"content":`+mustJSON(t, content)+`}`)
	require.Equal(t, http.StatusOK, put.Code)

	get := doRequest(t, srv, "GET", "/api/settings/file/MEMORY.md", "")
	require.Equal(t, http.StatusOK, get.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(get.Body).Decode(&resp))
	assert.Equal(t, content, resp["content"])
}

func TestSettingsFile_Put_NotEditable(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, "PUT", "/api/settings/file/nope.txt", `{"content":"x"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSettingsFile_Put_CreatesParentDir(t *testing.T) {
	srv := newTestServer(t)
	// MEMORY_GRAPH.md lives under memory/longterm/, which may not exist yet.
	rec := doRequest(t, srv, "PUT", "/api/settings/file/MEMORY_GRAPH.md", `{"content":"{}"}`)
	require.Equal(t, http.StatusOK, rec.Code)

	full := filepath.Join(core.GetWorkspace().Dir(), "memory", "longterm", "memory_graph.jsonl")
	data, err := os.ReadFile(full)
	require.NoError(t, err, "PUT should create parent dirs and write the file")
	assert.Equal(t, "{}", string(data))
}

func TestSettingsFile_Put_InvalidJSON(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, "PUT", "/api/settings/file/MEMORY.md", "not json")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ── Settings config ──

func TestSettingsConfig_Get(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, "GET", "/api/settings/config", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var sc ServerConfig
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&sc))
}

func TestSettingsConfig_PutThenGet_RoundTrip(t *testing.T) {
	srv := newTestServer(t)
	body := `{"provider_name":"openai","model":"gpt-4o","api_key":"sk-test-123"}`

	put := doRequest(t, srv, "PUT", "/api/settings/config", body)
	require.Equal(t, http.StatusOK, put.Code)

	get := doRequest(t, srv, "GET", "/api/settings/config", "")
	require.Equal(t, http.StatusOK, get.Code)
	var sc ServerConfig
	require.NoError(t, json.NewDecoder(get.Body).Decode(&sc))
	assert.Equal(t, "openai", sc.ProviderName)
	assert.Equal(t, "gpt-4o", sc.Model)
	assert.Equal(t, "sk-test-123", sc.APIKey)
}

func TestSettingsConfig_Put_InvalidJSON(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, "PUT", "/api/settings/config", "not json")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ── Settings multimodal ──

func TestSettingsMultimodal(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, "GET", "/api/settings/multimodal", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]bool
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.False(t, resp["image_available"], "default config has no image model")
	assert.False(t, resp["audio_available"], "default config has no audio model")
}

// ── Static assets ──

func TestStaticFiles_ServeAssets(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{"/", "/chat.js", "/style.css", "/settings.js"} {
		rec := doRequest(t, srv, "GET", path, "")
		assert.Equal(t, http.StatusOK, rec.Code, "GET %s", path)
		assert.NotEmpty(t, rec.Body.String(), "GET %s should return non-empty content", path)
	}
}

func TestStaticFiles_UnknownPathIs404(t *testing.T) {
	srv := newTestServer(t)
	rec := doRequest(t, srv, "GET", "/no/such/file.js", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// mustJSON encodes v as a JSON string literal for embedding in a request body.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
