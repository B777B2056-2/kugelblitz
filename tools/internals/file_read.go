package internals

import (
	"context"
	"os"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/tools"
)

// FileRead reads the contents of a file.
type FileRead struct{}

func (t *FileRead) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "file_read",
		Description: "Read the contents of a file at the given path.",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the file to read",
				},
			},
			"required": []string{"path"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path that was read"},
				"content": map[string]any{"type": "string", "description": "File content as a string"},
				"size":    map[string]any{"type": "integer", "description": "File size in bytes"},
			},
		},
	}
}

func (t *FileRead) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	path, err := tools.Arg(detail, "path")
	if err != nil {
		return tools.ErrorResult(detail.ID, "file_read", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return tools.ErrorResult(detail.ID, "file_read", err)
	}

	return tools.SuccessResult(detail.ID, "file_read", map[string]any{
		"path":    path,
		"content": string(data),
		"size":    len(data),
	})
}
