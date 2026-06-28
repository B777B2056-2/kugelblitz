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

// MemoryDir returns the memory subdirectory path.
func (w *Workspace) MemoryDir() string {
	return filepath.Join(w.Dir(), "memory")
}

// SessionsDir returns the sessions subdirectory path under memory/.
func (w *Workspace) SessionsDir() string {
	return filepath.Join(w.Dir(), "memory", "sessions")
}

// PlansDir returns the plans subdirectory path under memory/.
func (w *Workspace) PlansDir() string {
	return filepath.Join(w.Dir(), "memory", "plans")
}

// SkillsDir returns the skills subdirectory path.
func (w *Workspace) SkillsDir() string {
	return filepath.Join(w.Dir(), "skills")
}

// MemoryFile returns the MEMORY.md file path (remains at workspace root).
func (w *Workspace) MemoryFile() string {
	return filepath.Join(w.Dir(), "MEMORY.md")
}

// SessionPath returns the full path for a session JSONL file.
func (w *Workspace) SessionPath(sessionID string) string {
	return filepath.Join(w.Dir(), "memory", "sessions", sessionID+".jsonl")
}

// PlanPath returns the full path for a plan JSONL file.
func (w *Workspace) PlanPath(planID string) string {
	return filepath.Join(w.Dir(), "memory", "plans", planID, "plan.jsonl")
}

// CheckpointPath returns the full path for a plan checkpoint file.
func (w *Workspace) CheckpointPath(planID string, version int) string {
	return filepath.Join(w.Dir(), "memory", "plans", planID, "checkpoints", fmt.Sprintf("%04d.jsonl", version))
}

// MkdirAll creates the workspace directory tree.
func (w *Workspace) MkdirAll() error {
	dirs := []string{w.Dir(), w.MemoryDir(), w.SessionsDir(), w.PlansDir()}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}
