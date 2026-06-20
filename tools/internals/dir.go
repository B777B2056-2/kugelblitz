package internals

import (
	"context"
	"os"
	"path/filepath"

	"kugelblitz/core"
	"kugelblitz/tools"
)

// DirCreate creates a directory (including parent directories).
type DirCreate struct{}

func (t *DirCreate) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "dir_create",
		Description: "Create a directory at the given path. Creates parent directories as needed (like mkdir -p).",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the directory to create",
				},
			},
			"required": []string{"path"},
		},
	}
}

func (t *DirCreate) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	path, err := tools.Arg(detail, "path")
	if err != nil {
		return tools.ErrorResult(detail.ID, "dir_create", err)
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		return tools.ErrorResult(detail.ID, "dir_create", err)
	}

	return tools.OkResult(detail.ID, "dir_create")
}

// DirCopy copies or moves an entire directory recursively.
type DirCopy struct{}

func (t *DirCopy) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "dir_copy",
		Description: "Copy or move a directory and all its contents from source to destination. Set 'move' to true to move instead of copy.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source": map[string]any{
					"type":        "string",
					"description": "Source directory path",
				},
				"destination": map[string]any{
					"type":        "string",
					"description": "Destination directory path",
				},
				"move": map[string]any{
					"type":        "boolean",
					"description": "If true, move (cut) instead of copy. Default: false.",
				},
			},
			"required": []string{"source", "destination"},
		},
	}
}

func (t *DirCopy) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	src, err := tools.Arg(detail, "source")
	if err != nil {
		return tools.ErrorResult(detail.ID, "dir_copy", err)
	}
	dst, err := tools.Arg(detail, "destination")
	if err != nil {
		return tools.ErrorResult(detail.ID, "dir_copy", err)
	}

	move := false
	if v, ok := detail.Args["move"]; ok {
		if b, ok := v.(bool); ok {
			move = b
		}
	}

	action := "copied"
	if move {
		action = "moved"
		if err := os.Rename(src, dst); err != nil {
			// Cross-device move: copy then delete
			if err := copyDir(src, dst); err != nil {
				return tools.ErrorResult(detail.ID, "dir_copy", err)
			}
			os.RemoveAll(src)
		}
	} else {
		if err := copyDir(src, dst); err != nil {
			return tools.ErrorResult(detail.ID, "dir_copy", err)
		}
	}

	return tools.SuccessResult(detail.ID, "dir_copy", map[string]any{
		"source":      src,
		"destination": dst,
		"action":      action,
	})
}

// copyDir recursively copies a directory from src to dst.
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}
