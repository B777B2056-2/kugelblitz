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
	mu   sync.RWMutex
}

// NewFilePersist creates a FilePersist rooted at the given directory.
func NewFilePersist(root string) *FilePersist {
	return &FilePersist{root: root}
}

// fullPath resolves key under the persist root, refusing any key that would
// escape it via path traversal (C3).
func (fp *FilePersist) fullPath(key string) (string, error) {
	root := filepath.Clean(fp.root)
	path := filepath.Join(root, filepath.Clean(key))
	if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return "", fmt.Errorf("file persist: key %q escapes root", key)
	}
	return path, nil
}

// Store writes data to the key's file. Directories are created as needed.
// The write is atomic (temp file + rename) so a crash cannot leave a truncated
// file behind (P14).
func (fp *FilePersist) Store(_ context.Context, key string, data []byte) error {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	path, err := fp.fullPath(key)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("file persist store: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("file persist store: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after successful rename

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("file persist store: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("file persist store: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("file persist store: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("file persist store: rename: %w", err)
	}
	return nil
}

// Load reads the key's file contents.
func (fp *FilePersist) Load(_ context.Context, key string) ([]byte, error) {
	fp.mu.RLock()
	defer fp.mu.RUnlock()
	path, err := fp.fullPath(key)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// Exists reports whether the key's file exists.
func (fp *FilePersist) Exists(_ context.Context, key string) bool {
	fp.mu.RLock()
	defer fp.mu.RUnlock()
	path, err := fp.fullPath(key)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Delete removes the key's file.
func (fp *FilePersist) Delete(_ context.Context, key string) error {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	path, err := fp.fullPath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("file persist delete: %w", err)
	}
	return nil
}

// List returns all keys under the given prefix.
func (fp *FilePersist) List(_ context.Context, prefix string) ([]string, error) {
	fp.mu.RLock()
	defer fp.mu.RUnlock()

	dir, err := fp.fullPath(prefix)
	if err != nil {
		return nil, err
	}
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
			name := strings.TrimSuffix(e.Name(), ".jsonl")
			keys = append(keys, filepath.Join(prefix, name))
		}
	}
	return keys, nil
}
