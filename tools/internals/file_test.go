package internals

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/tools"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tmpDir(t *testing.T) string {
	d, err := os.MkdirTemp("", "kugelblitz-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func TestFileRead_ReadsContent(t *testing.T) {
	dir := tmpDir(t)
	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0644))

	tool := &FileRead{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "file_read",
		Args: map[string]any{"path": path},
	})

	assert.Equal(t, "hello world", result.Outputs["content"])
}

func TestFileRead_FileNotFound(t *testing.T) {
	tool := &FileRead{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "file_read",
		Args: map[string]any{"path": "/nonexistent/file.txt"},
	})
	assert.NotNil(t, result.Outputs["error"])
	assert.NotEmpty(t, result.Outputs["error"])
}

func TestFileWrite_WritesContent(t *testing.T) {
	dir := tmpDir(t)
	path := filepath.Join(dir, "output.txt")

	tool := &FileWrite{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "file_write",
		Args: map[string]any{"path": path, "content": "hello go"},
	})

	assert.Equal(t, true, result.Outputs["consumed"] != nil)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello go", string(data))
}

func TestFileWrite_CreatesParentDirs(t *testing.T) {
	dir := tmpDir(t)
	path := filepath.Join(dir, "nested", "sub", "file.txt")

	tool := &FileWrite{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "file_write",
		Args: map[string]any{"path": path, "content": "deep"},
	})
	assert.Nil(t, result.Outputs["error"])

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "deep", string(data))
}

func TestFileCopy_CopiesFile(t *testing.T) {
	dir := tmpDir(t)
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	require.NoError(t, os.WriteFile(src, []byte("original"), 0644))

	tool := &FileCopy{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "file_copy",
		Args: map[string]any{"source": src, "destination": dst},
	})

	assert.Equal(t, "copied", result.Outputs["action"])
	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "original", string(data))
}

func TestFileCopy_MovesFile(t *testing.T) {
	dir := tmpDir(t)
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	require.NoError(t, os.WriteFile(src, []byte("move me"), 0644))

	tool := &FileCopy{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "file_copy",
		Args: map[string]any{"source": src, "destination": dst, "move": true},
	})

	assert.Equal(t, "moved", result.Outputs["action"])
	_, err := os.Stat(src)
	assert.True(t, os.IsNotExist(err), "source should not exist after move")
}

func TestDirCreate_CreatesDirectory(t *testing.T) {
	dir := tmpDir(t)
	newDir := filepath.Join(dir, "newdir")

	tool := &DirCreate{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "dir_create",
		Args: map[string]any{"path": newDir},
	})

	assert.Equal(t, true, result.Outputs["ok"])
	info, err := os.Stat(newDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestDirCopy_CopiesDirectory(t *testing.T) {
	dir := tmpDir(t)
	src := filepath.Join(dir, "srcdir")
	dst := filepath.Join(dir, "dstdir")
	require.NoError(t, os.MkdirAll(src, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "a.txt"), []byte("A"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "sub"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("B"), 0644))

	tool := &DirCopy{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "dir_copy",
		Args: map[string]any{"source": src, "destination": dst},
	})

	assert.Equal(t, "copied", result.Outputs["action"])
	data, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "A", string(data))
	data, err = os.ReadFile(filepath.Join(dst, "sub", "b.txt"))
	require.NoError(t, err)
	assert.Equal(t, "B", string(data))
}

func TestRegisterAll_RegistersAllTools(t *testing.T) {
	core.GetToolRegistry().Reset()
	RegisterAll()

	defs := core.ListToolDefinitions()
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	assert.True(t, names["file_read"])
	assert.True(t, names["file_write"])
	assert.True(t, names["file_copy"])
	assert.True(t, names["dir_create"])
	assert.True(t, names["dir_copy"])
	assert.Len(t, names, 15)
}

// Compile-time checks
var _ tools.Tool = (*FileRead)(nil)
var _ tools.Tool = (*FileWrite)(nil)
var _ tools.Tool = (*FileDelete)(nil)
var _ tools.Tool = (*FileCopy)(nil)
var _ tools.Tool = (*DirCreate)(nil)
var _ tools.Tool = (*DirCopy)(nil)
var _ tools.Tool = (*ShellExec)(nil)
var _ tools.Tool = (*PlanCreate)(nil)
var _ tools.Tool = (*PlanQuery)(nil)
var _ tools.Tool = (*PlanStatusUpdate)(nil)
var _ tools.Tool = (*TaskInsert)(nil)
var _ tools.Tool = (*TaskDelete)(nil)
var _ tools.Tool = (*TaskQuery)(nil)
var _ tools.Tool = (*TaskStatusUpdate)(nil)
