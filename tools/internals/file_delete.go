package internals

import (
	"context"
	"os"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/tools"
)

// FileDelete deletes a file at the given path.
type FileDelete struct{}

func (t *FileDelete) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "file_delete",
		Description: "Delete a file at the given path. Returns an error if the file does not exist.",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the file to delete",
				},
			},
			"required": []string{"path"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":   map[string]any{"type": "string", "description": "Path of the deleted file"},
				"action": map[string]any{"type": "string", "description": "Always \"deleted\" on success"},
			},
		},
	}
}

func (t *FileDelete) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	path, err := tools.Arg(detail, "path")
	if err != nil {
		return tools.ErrorResult(detail.ID, "file_delete", err)
	}

	if err := os.Remove(path); err != nil {
		return tools.ErrorResult(detail.ID, "file_delete", err)
	}

	return tools.SuccessResult(detail.ID, "file_delete", map[string]any{
		"path":   path,
		"action": "deleted",
	})
}
