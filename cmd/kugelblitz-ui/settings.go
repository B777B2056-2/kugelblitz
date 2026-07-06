package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/B777B2056-2/kugelblitz/core"
)

// SettingsFile describes an editable config file.
type SettingsFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// editableFiles lists the files exposed via the settings API.
var editableFiles = []SettingsFile{
	{Name: "kugelblitz.yaml", Path: "kugelblitz.yaml"},
	{Name: "MEMORY.md", Path: "MEMORY.md"},
	{Name: "DREAMS.md", Path: "DREAMS.md"},
	{Name: "MEMORY_GRAPH.md", Path: "memory/longterm/memory_graph.jsonl"},
	{Name: "AGENTS.md", Path: "AGENTS.md"},
	{Name: "IDENTITY.md", Path: "IDENTITY.md"},
	{Name: "SOUL.md", Path: "SOUL.md"},
	{Name: "USER.md", Path: "USER.md"},
	{Name: ".mcp.json", Path: ".mcp.json"},
}

func (s *Server) settingsDir() string {
	return core.GetWorkspace().Dir()
}

// handleSettingsFiles lists all editable config files.
func (s *Server) handleSettingsFiles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, editableFiles)
}

// handleSettingsGetFile reads a config file's content.
func (s *Server) handleSettingsGetFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	sf := findSettingsFile(name)
	if sf == nil {
		core.Warn("file not in editable list", "name", name)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}

	fullPath := filepath.Join(s.settingsDir(), sf.Path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]string{
				"name":    sf.Name,
				"path":    sf.Path,
				"content": "",
			})
			return
		}
		core.Error("settings read error", "name", sf.Name, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"name":    sf.Name,
		"path":    sf.Path,
		"content": string(data),
	})
}

// handleSettingsPutFile saves a config file.
func (s *Server) handleSettingsPutFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	sf := findSettingsFile(name)
	if sf == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	fullPath := filepath.Join(s.settingsDir(), sf.Path)

	// Ensure parent directory exists
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := os.WriteFile(fullPath, []byte(req.Content), 0644); err != nil {
		core.Error("settings save error", "name", sf.Name, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	core.Info("settings file saved", "name", sf.Name)

	// Reload runtime config if kugelblitz.yaml was saved
	if strings.EqualFold(sf.Name, "kugelblitz.yaml") {
		reloadConfigFromFile()
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func findSettingsFile(name string) *SettingsFile {
	for i := range editableFiles {
		if strings.EqualFold(editableFiles[i].Name, name) {
			return &editableFiles[i]
		}
	}
	return nil
}
