package persist

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FilePersist implements IPersist using the local filesystem.
// Each key is stored as a .json file under the root directory.
type FilePersist struct {
	root string
	mu   sync.Mutex
}

// NewFilePersist creates a FilePersist rooted at the given directory.
func NewFilePersist(root string) *FilePersist {
	return &FilePersist{root: root}
}

func (fp *FilePersist) fullPath(key string) string {
	return filepath.Join(fp.root, key)
}

// Store writes data to the key's file. Directories are created as needed.
func (fp *FilePersist) Store(_ context.Context, key string, data []byte) error {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	path := fp.fullPath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("file persist store: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// Load reads the key's file contents.
func (fp *FilePersist) Load(_ context.Context, key string) ([]byte, error) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return os.ReadFile(fp.fullPath(key))
}

// Exists reports whether the key's file exists.
func (fp *FilePersist) Exists(_ context.Context, key string) bool {
	_, err := os.Stat(fp.fullPath(key))
	return err == nil
}

// Delete removes the key's file.
func (fp *FilePersist) Delete(_ context.Context, key string) error {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if err := os.Remove(fp.fullPath(key)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("file persist delete: %w", err)
	}
	return nil
}

// List returns all keys under the given prefix.
func (fp *FilePersist) List(_ context.Context, prefix string) ([]string, error) {
	dir := filepath.Join(fp.root, prefix)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("file persist list: %w", err)
	}
	var keys []string
	for _, e := range entries {
		if e.IsDir() {
			keys = append(keys, filepath.Join(prefix, e.Name()))
		} else {
			// Strip extension for flat files
			name := e.Name()
			if strings.HasSuffix(name, ".jsonl") {
				name = name[:len(name)-6]
			}
			keys = append(keys, filepath.Join(prefix, name))
		}
	}
	return keys, nil
}
