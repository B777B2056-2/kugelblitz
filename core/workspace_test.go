package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkspace_DefaultDir(t *testing.T) {
	ws := GetWorkspace()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".kugelblitz")
	assert.Equal(t, expected, ws.Dir())
}

func TestWorkspace_SetDir(t *testing.T) {
	old := GetWorkspace().Dir()
	defer GetWorkspace().SetDir(old)

	GetWorkspace().SetDir("/custom/path")
	assert.Equal(t, "/custom/path", GetWorkspace().Dir())
}

func TestWorkspace_MemoryDir(t *testing.T) {
	old := GetWorkspace().Dir()
	defer GetWorkspace().SetDir(old)

	GetWorkspace().SetDir("/ws")
	assert.Equal(t, filepath.Join("/ws", "memory"), GetWorkspace().MemoryDir())
}

func TestWorkspace_SessionsDir(t *testing.T) {
	old := GetWorkspace().Dir()
	defer GetWorkspace().SetDir(old)

	GetWorkspace().SetDir("/ws")
	assert.Equal(t, filepath.Join("/ws", "memory", "sessions"), GetWorkspace().SessionsDir())
}

func TestWorkspace_PlansDir(t *testing.T) {
	old := GetWorkspace().Dir()
	defer GetWorkspace().SetDir(old)

	GetWorkspace().SetDir("/ws")
	assert.Equal(t, filepath.Join("/ws", "memory", "plans"), GetWorkspace().PlansDir())
}

func TestWorkspace_MemoryFile(t *testing.T) {
	old := GetWorkspace().Dir()
	defer GetWorkspace().SetDir(old)

	GetWorkspace().SetDir("/ws")
	assert.Equal(t, filepath.Join("/ws", "MEMORY.md"), GetWorkspace().MemoryFile())
}

func TestWorkspace_SessionPath(t *testing.T) {
	old := GetWorkspace().Dir()
	defer GetWorkspace().SetDir(old)

	GetWorkspace().SetDir("/ws")
	assert.Equal(t, filepath.Join("/ws", "memory", "sessions", "abc.jsonl"), GetWorkspace().SessionPath("abc"))
}

func TestWorkspace_PlanPath(t *testing.T) {
	old := GetWorkspace().Dir()
	defer GetWorkspace().SetDir(old)

	GetWorkspace().SetDir("/ws")
	assert.Equal(t, filepath.Join("/ws", "memory", "plans", "abc", "plan.jsonl"), GetWorkspace().PlanPath("abc"))
}

func TestWorkspace_CheckpointPath(t *testing.T) {
	old := GetWorkspace().Dir()
	defer GetWorkspace().SetDir(old)

	GetWorkspace().SetDir("/ws")
	assert.Equal(t, filepath.Join("/ws", "memory", "plans", "p1", "checkpoints", "0001.jsonl"), GetWorkspace().CheckpointPath("p1", 1))
}

func TestWorkspace_MkdirAll(t *testing.T) {
	old := GetWorkspace().Dir()
	defer GetWorkspace().SetDir(old)

	dir := filepath.Join(t.TempDir(), "kugelblitz-test")
	GetWorkspace().SetDir(dir)

	require.NoError(t, GetWorkspace().MkdirAll())

	_, err := os.Stat(dir)
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "memory"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "memory", "sessions"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "memory", "plans"))
	assert.NoError(t, err)
}

func TestWorkspace_SessionPath_RejectsTraversal(t *testing.T) {
	old := GetWorkspace().Dir()
	defer GetWorkspace().SetDir(old)

	GetWorkspace().SetDir("/ws")
	path := GetWorkspace().SessionPath("../../etc/passwd")
	assert.Equal(t, filepath.Join("/ws", "memory", "sessions", "passwd.jsonl"), path)
	// The result must stay inside the sessions directory.
	assert.True(t, strings.HasPrefix(path, filepath.Join("/ws", "memory", "sessions")))
}

func TestWorkspace_PlanPath_RejectsTraversal(t *testing.T) {
	old := GetWorkspace().Dir()
	defer GetWorkspace().SetDir(old)

	GetWorkspace().SetDir("/ws")
	path := GetWorkspace().PlanPath("../..//secret")
	assert.Equal(t, filepath.Join("/ws", "memory", "plans", "secret", "plan.jsonl"), path)
}

func TestSafeID(t *testing.T) {
	assert.Equal(t, "abc", safeID("abc"))
	assert.Equal(t, "passwd", safeID("../../etc/passwd"))
	assert.Equal(t, "b", safeID("a/b"))
	assert.Equal(t, "", safeID(".."))
	assert.Equal(t, "", safeID("."))
	assert.Equal(t, "", safeID(""))
}

func TestWorkspace_WindowsPathNormalization(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	old := GetWorkspace().Dir()
	defer GetWorkspace().SetDir(old)

	GetWorkspace().SetDir(`C:\Users\test\.kugelblitz`)
	assert.Contains(t, GetWorkspace().SessionsDir(), `C:\Users\test\.kugelblitz\memory\sessions`)
}
