package internals

import (
	"context"
	"os"

	"kugelblitz/core"
	"kugelblitz/tools"
)

// FileWrite writes content to a file, creating it if it doesn't exist.
type FileWrite struct{}

func (t *FileWrite) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "file_write",
		Description: "Write content to a file at the given path. Overwrites existing files. Creates parent directories if needed.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the file to write",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Content to write to the file",
				},
			},
			"required": []string{"path", "content"},
		},
	}
}

func (t *FileWrite) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	path, err := tools.Arg(detail, "path")
	if err != nil {
		return tools.ErrorResult(detail.ID, "file_write", err)
	}
	content, err := tools.Arg(detail, "content")
	if err != nil {
		return tools.ErrorResult(detail.ID, "file_write", err)
	}

	// Ensure parent directories exist
	if err := os.MkdirAll(dirOf(path), 0755); err != nil {
		return tools.ErrorResult(detail.ID, "file_write", err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return tools.ErrorResult(detail.ID, "file_write", err)
	}

	return tools.SuccessResult(detail.ID, "file_write", map[string]any{
		"path":     path,
		"bytes":    len(content),
		"consumed": len(content),
	})
}
