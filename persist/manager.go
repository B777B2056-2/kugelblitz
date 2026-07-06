package persist

import (
	"sync"

	"github.com/B777B2056-2/kugelblitz/core"
)

// Manager provides access to all persistence instances.
type Manager struct {
	markdown *MarkdownPersist
	jsonl    *JSONLPersist
	vector   *VectorPersist
	dir      string
}

// NewManager creates a Manager with the given persist implementations.
func NewManager(dir string, md *MarkdownPersist, jl *JSONLPersist, vs *VectorPersist) *Manager {
	return &Manager{
		dir:      dir,
		markdown: md,
		jsonl:    jl,
		vector:   vs,
	}
}

// Markdown returns the MarkdownPersist for MEMORY.md operations.
func (m *Manager) Markdown() *MarkdownPersist { return m.markdown }

// JSONL returns the JSONLPersist for session/plan/checkpoint files.
func (m *Manager) JSONL() *JSONLPersist { return m.jsonl }

// Vector returns the VectorPersist for ChromaDB operations (nil if not configured).
func (m *Manager) Vector() *VectorPersist { return m.vector }

// Dir returns the workspace root directory.
func (m *Manager) Dir() string { return m.dir }

// ---- Global singleton ----

var (
	globalManager *Manager
	globalOnce    sync.Once
)

// GetManager returns the global Manager singleton.
func GetManager() *Manager {
	globalOnce.Do(func() {
		dir := core.GetWorkspace().Dir()
		fp := NewFilePersist(dir)
		globalManager = NewManager(
			dir,
			NewMarkdownPersist(fp),
			NewJSONLPersist(fp),
			NewVectorPersist(fp, NewChromaStoreOrNil()),
		)
	})
	return globalManager
}
