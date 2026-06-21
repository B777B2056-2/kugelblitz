package persist

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Persister abstracts key-value persistence to disk.
// Backends: file system (default), S3, database, etc.
type Persister interface {
	Save(key string, data []byte) error
	Load(key string) ([]byte, error)
	Exists(key string) bool
	Delete(key string) error
	List(prefix string) ([]string, error)
}

// ---- FilePersister ----

type FilePersister struct {
	root string
	mu   sync.Mutex
}

func NewFilePersister(root string) *FilePersister {
	return &FilePersister{root: root}
}

func (fp *FilePersister) fullPath(key string) string {
	return filepath.Join(fp.root, key+".json")
}

func (fp *FilePersister) Save(key string, data []byte) error {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	path := fp.fullPath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("filesystem save: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func (fp *FilePersister) Load(key string) ([]byte, error) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return os.ReadFile(fp.fullPath(key))
}

func (fp *FilePersister) Exists(key string) bool {
	_, err := os.Stat(fp.fullPath(key))
	return err == nil
}

func (fp *FilePersister) Delete(key string) error {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if err := os.Remove(fp.fullPath(key)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("filesystem delete: %w", err)
	}
	return nil
}

func (fp *FilePersister) List(prefix string) ([]string, error) {
	dir := filepath.Join(fp.root, prefix)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("filesystem list: %w", err)
	}
	var keys []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			keys = append(keys, filepath.Join(prefix, strings.TrimSuffix(e.Name(), ".json")))
		}
	}
	return keys, nil
}
