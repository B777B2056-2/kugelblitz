package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Workspace manages the framework's working directory.
// Default: ~/.kugelblitz
type Workspace struct {
	dir string
	mu  sync.RWMutex
}

var globalWorkspace = &Workspace{}

func init() {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	globalWorkspace.dir = filepath.Join(home, ".kugelblitz")
}

// GetWorkspace returns the global Workspace singleton.
func GetWorkspace() *Workspace { return globalWorkspace }

// Dir returns the workspace root directory.
func (w *Workspace) Dir() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.dir
}

// SetDir changes the workspace root.
func (w *Workspace) SetDir(dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dir = dir
}

// SessionsDir returns the sessions subdirectory path.
func (w *Workspace) SessionsDir() string {
	return filepath.Join(w.Dir(), "sessions")
}

// PlansDir returns the plans subdirectory path.
func (w *Workspace) PlansDir() string {
	return filepath.Join(w.Dir(), "plans")
}

// MemoryFile returns the MEMORY.md file path.
func (w *Workspace) MemoryFile() string {
	return filepath.Join(w.Dir(), "MEMORY.md")
}

// SessionPath returns the full path for a session JSON file.
func (w *Workspace) SessionPath(sessionID string) string {
	return filepath.Join(w.Dir(), "sessions", sessionID+".json")
}

// PlanPath returns the full path for a plan JSON file.
func (w *Workspace) PlanPath(planID string) string {
	return filepath.Join(w.Dir(), "plans", planID+".json")
}

// CheckpointPath returns the full path for a plan checkpoint file.
func (w *Workspace) CheckpointPath(planID string, version int) string {
	return filepath.Join(w.Dir(), "checkpoints", planID, fmt.Sprintf("%04d.json", version))
}

// MkdirAll creates the workspace directory tree.
func (w *Workspace) MkdirAll() error {
	dirs := []string{w.Dir(), w.SessionsDir(), w.PlansDir()}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}
