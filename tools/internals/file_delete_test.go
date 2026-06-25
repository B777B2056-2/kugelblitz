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

func TestFileDelete_DeletesFile(t *testing.T) {
	dir := tmpDir(t)
	path := filepath.Join(dir, "to_delete.txt")
	require.NoError(t, os.WriteFile(path, []byte("delete me"), 0644))

	tool := &FileDelete{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "file_delete",
		Args: map[string]any{"path": path},
	})

	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, path, result.Outputs["path"])
	assert.Equal(t, "deleted", result.Outputs["action"])

	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestFileDelete_NotFound(t *testing.T) {
	tool := &FileDelete{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "file_delete",
		Args: map[string]any{"path": "/nonexistent/file.txt"},
	})
	assert.NotNil(t, result.Outputs["error"])
}

func TestFileDelete_Definition(t *testing.T) {
	tool := &FileDelete{}
	def := tool.Definition()
	assert.Equal(t, "file_delete", def.Name)
	assert.NotEmpty(t, def.Description)
}

var _ tools.Tool = (*FileDelete)(nil)
