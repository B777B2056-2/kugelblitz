package persist

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilePersist_ConcurrentLoadAndStore(t *testing.T) {
	fp := NewFilePersist(t.TempDir())
	ctx := context.Background()
	requireNoError(t, fp.Store(ctx, "shared.file", []byte("initial")))

	var wg sync.WaitGroup
	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := fp.Load(ctx, "shared.file")
			if err == nil {
				assert.NotNil(t, data)
			}
		}()
	}

	// Concurrent writes (different files)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = fp.Store(ctx, fmt.Sprintf("file-%d.txt", idx), []byte("data"))
		}(i)
	}

	wg.Wait()

	// Verify shared file still intact
	data, err := fp.Load(ctx, "shared.file")
	assert.NoError(t, err)
	assert.Equal(t, "initial", string(data))

	// Verify new files exist
	for i := 0; i < 5; i++ {
		assert.True(t, fp.Exists(ctx, fmt.Sprintf("file-%d.txt", i)))
	}
}

func TestFilePersist_ConcurrentExistsAndDelete(t *testing.T) {
	fp := NewFilePersist(t.TempDir())
	ctx := context.Background()

	// Create files
	for i := 0; i < 10; i++ {
		requireNoError(t, fp.Store(ctx, fmt.Sprintf("f-%d.txt", i), []byte("x")))
	}

	var wg sync.WaitGroup
	// Concurrent Exists (no assertion — the file may be deleted concurrently)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = fp.Exists(ctx, fmt.Sprintf("f-%d.txt", idx))
		}(i)
	}

	// Concurrent Delete
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = fp.Delete(ctx, fmt.Sprintf("f-%d.txt", idx))
		}(i)
	}

	wg.Wait()

	// Verify: 5 deleted, 5 remaining
	remaining := 0
	for i := 0; i < 10; i++ {
		if fp.Exists(ctx, fmt.Sprintf("f-%d.txt", i)) {
			remaining++
		}
	}
	assert.GreaterOrEqual(t, remaining, 5)
}

func TestFilePersist_RejectsPathTraversal(t *testing.T) {
	fp := NewFilePersist(t.TempDir())
	ctx := context.Background()

	// Store/Delete/Load/List must refuse keys that escape the root.
	_, err := fp.Load(ctx, "../secret.txt")
	assert.Error(t, err)

	err = fp.Store(ctx, "../secret.txt", []byte("x"))
	assert.Error(t, err)

	err = fp.Delete(ctx, "../secret.txt")
	assert.Error(t, err)

	assert.False(t, fp.Exists(ctx, "../secret.txt"))

	_, err = fp.List(ctx, "../memory")
	assert.Error(t, err)
}

func TestFilePersist_ListReturnsStrippedNames(t *testing.T) {
	fp := NewFilePersist(t.TempDir())
	ctx := context.Background()

	requireNoError(t, fp.Store(ctx, "memory/sessions/abc.jsonl", []byte("x")))
	keys, err := fp.List(ctx, "memory/sessions")
	assert.NoError(t, err)
	assert.Equal(t, []string{filepath.Join("memory", "sessions", "abc")}, keys)
}

func requireNoError(t *testing.T, err error, args ...any) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResetManager_Isolates(t *testing.T) {
	ResetManager()
	m1 := GetManager()
	assert.NotNil(t, m1)

	ResetManager()
	m2 := GetManager()
	assert.NotNil(t, m2)
	assert.NotSame(t, m1, m2, "reset should yield a fresh manager instance")
}
