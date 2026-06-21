package persist

import (
	"path/filepath"
	"sync"
)

// Manager orchestrates persistence of domain objects (Plans, Sessions).
type Manager struct {
	persister Persister
}

// NewManager creates a Manager with the given Persister backend.
func NewManager(p Persister) *Manager {
	return &Manager{persister: p}
}

// NewFileManager is a convenience constructor using FilePersister.
func NewFileManager(root string) *Manager {
	return NewManager(NewFilePersister(root))
}

// Persister returns the underlying Persister (for Exists checks, etc.).
func (m *Manager) Persister() Persister {
	return m.persister
}

// SavePlan persists a Plan JSON under "plans/{id}".
func (m *Manager) SavePlan(id string, data []byte) error {
	return m.persister.Save(filepath.Join("plans", id), data)
}

// LoadPlan reads a Plan JSON from "plans/{id}".
func (m *Manager) LoadPlan(id string) ([]byte, error) {
	return m.persister.Load(filepath.Join("plans", id))
}

// SaveSession persists session JSONL under "sessions/{id}".
func (m *Manager) SaveSession(id string, data []byte) error {
	return m.persister.Save(filepath.Join("sessions", id), data)
}

// LoadSession reads session JSONL from "sessions/{id}".
func (m *Manager) LoadSession(id string) ([]byte, error) {
	return m.persister.Load(filepath.Join("sessions", id))
}

// Delete removes a key from the underlying persister.
func (m *Manager) Delete(key string) error {
	return m.persister.Delete(key)
}

// ListPlans returns all persisted plan IDs.
func (m *Manager) ListPlans() ([]string, error) {
	return m.persister.List("plans")
}

// ListSessions returns all persisted session IDs.
func (m *Manager) ListSessions() ([]string, error) {
	return m.persister.List("sessions")
}

// ---- Global singleton ----

var (
	globalManager *Manager
	globalOnce    sync.Once
)

// GetManager returns the global Manager singleton (FilePersister at ".kugelblitz").
func GetManager() *Manager {
	globalOnce.Do(func() {
		globalManager = NewFileManager(".kugelblitz")
	})
	return globalManager
}

// SetManager replaces the global Manager (for testing or custom backends).
func SetManager(m *Manager) {
	globalManager = m
}
