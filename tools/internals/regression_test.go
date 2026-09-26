package internals

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slowCommand returns a platform command that runs far longer than 1s so the
// shell_exec timeout path can be exercised deterministically. On Windows a
// cmd-internal busy loop is used (rather than ping, which spawns a child that
// keeps the stdout pipe open after cmd is killed).
func slowCommand() string {
	if runtime.GOOS == "windows" {
		return `for /l %i in (1,1,1000000000) do rem`
	}
	return "sleep 30"
}

// ---- T1: a timed-out command surfaces an error instead of reporting success.

func TestShellExec_TimeoutReturnsError(t *testing.T) {
	result := (&ShellExec{}).Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "shell_exec",
		Args: map[string]any{"command": slowCommand(), "timeout": 1},
	})

	errVal, ok := result.Outputs["error"].(string)
	require.True(t, ok, "timeout must surface an error output")
	assert.Contains(t, errVal, "timed out")
}

// ---- T3: output truncation cuts on rune boundaries, never splitting UTF-8.

func TestTruncateString_PreservesUTF8(t *testing.T) {
	s := strings.Repeat("你好", 3000) // 6000 runes, 18000 bytes
	out := truncateString(s, 4000)

	assert.True(t, utf8.ValidString(out), "truncation must not split a rune")
	assert.Contains(t, out, "(truncated")
	assert.Contains(t, out, "6000 total chars")
}

func TestTruncateString_UnderLimitUnchanged(t *testing.T) {
	assert.Equal(t, "short", truncateString("short", 4000))
}

// ---- T8: wrong-typed optional args are surfaced, not silently discarded.

func TestShellExec_CwdTypeMismatch(t *testing.T) {
	result := (&ShellExec{}).Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "shell_exec",
		Args: map[string]any{"command": "echo hi", "cwd": 123},
	})
	_, ok := result.Outputs["error"].(string)
	assert.True(t, ok, "non-string cwd must surface an error")
}

func TestShellExec_TimeoutTypeMismatch(t *testing.T) {
	result := (&ShellExec{}).Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "shell_exec",
		Args: map[string]any{"command": "echo hi", "timeout": "lots"},
	})
	_, ok := result.Outputs["error"].(string)
	assert.True(t, ok, "non-int timeout must surface an error")
}

func TestAskHuman_ReasonTypeMismatch(t *testing.T) {
	result := (&AskHumanTool{}).Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "ask_human",
		Args: map[string]any{"question": "why?", "reason": 123},
	})
	_, ok := result.Outputs["error"].(string)
	assert.True(t, ok, "non-string reason must surface an error")
}

// ---- T4/T5: move removes the source on success.

func TestFileCopy_Move_RemovesSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "b.txt")
	require.NoError(t, os.WriteFile(src, []byte("hello"), 0644))

	result := (&FileCopy{}).Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "file_copy",
		Args: map[string]any{"source": src, "destination": dst, "move": true},
	})

	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, "moved", result.Outputs["action"])
	assert.NoFileExists(t, src, "move must remove the source file")
	assert.FileExists(t, dst)
}

func TestDirCopy_Move_RemovesSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	require.NoError(t, os.MkdirAll(src, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "f.txt"), []byte("x"), 0644))

	result := (&DirCopy{}).Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "dir_copy",
		Args: map[string]any{"source": src, "destination": dst, "move": true},
	})

	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, "moved", result.Outputs["action"])
	assert.NoDirExists(t, src, "move must remove the source directory")
	assert.FileExists(t, filepath.Join(dst, "f.txt"))
}

// ---- T4/T5: a move fallback whose source-removal fails must surface the
// error, not report success while leaving the source behind. The read-only
// parent directory forces os.Rename to fail while copyFile/copyDir succeed.

func TestFileCopy_MoveFallback_RemoveErrorSurfaced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only parent-directory semantics differ on Windows")
	}
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "a.txt")
	require.NoError(t, os.WriteFile(src, []byte("hello"), 0644))
	require.NoError(t, os.Chmod(srcDir, 0555))
	defer os.Chmod(srcDir, 0755)

	dst := filepath.Join(t.TempDir(), "b.txt")
	result := (&FileCopy{}).Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "file_copy",
		Args: map[string]any{"source": src, "destination": dst, "move": true},
	})

	errVal, ok := result.Outputs["error"].(string)
	require.True(t, ok, "remove failure must surface an error")
	assert.Contains(t, errVal, "remove source")
}

func TestDirCopy_MoveFallback_RemoveErrorSurfaced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only parent-directory semantics differ on Windows")
	}
	srcParent := t.TempDir()
	src := filepath.Join(srcParent, "src")
	require.NoError(t, os.MkdirAll(src, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "f.txt"), []byte("x"), 0644))
	require.NoError(t, os.Chmod(srcParent, 0555))
	defer os.Chmod(srcParent, 0755)

	dst := filepath.Join(t.TempDir(), "dst")
	result := (&DirCopy{}).Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "dir_copy",
		Args: map[string]any{"source": src, "destination": dst, "move": true},
	})

	errVal, ok := result.Outputs["error"].(string)
	require.True(t, ok, "remove failure must surface an error")
	assert.Contains(t, errVal, "remove source")
}
