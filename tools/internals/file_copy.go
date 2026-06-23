package internals

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/tools"
)

// FileCopy copies or moves a file from source to destination.
type FileCopy struct{}

func (t *FileCopy) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "file_copy",
		Description: "Copy or move a file from source to destination. Set 'move' to true to move instead of copy. Overwrites destination if it exists.",
		JsonSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source":      map[string]any{"type": "string", "description": "Source file path"},
				"destination": map[string]any{"type": "string", "description": "Destination file path"},
				"move":        map[string]any{"type": "boolean", "description": "If true, move (cut) instead of copy. Default: false."},
			},
			"required": []string{"source", "destination"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source":      map[string]any{"type": "string", "description": "Source file path"},
				"destination": map[string]any{"type": "string", "description": "Destination file path"},
				"action":      map[string]any{"type": "string", "description": "copied or moved"},
			},
		},
	}
}

func (t *FileCopy) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	src, err := tools.Arg(detail, "source")
	if err != nil {
		return tools.ErrorResult(detail.ID, "file_copy", err)
	}
	dst, err := tools.Arg(detail, "destination")
	if err != nil {
		return tools.ErrorResult(detail.ID, "file_copy", err)
	}

	move := false
	if v, ok := detail.Args["move"]; ok {
		if b, ok := v.(bool); ok {
			move = b
		}
	}

	if err := os.MkdirAll(dirOf(dst), 0755); err != nil {
		return tools.ErrorResult(detail.ID, "file_copy", err)
	}

	action := "copied"
	if move {
		action = "moved"
		if err := os.Rename(src, dst); err != nil {
			if err := copyFile(src, dst); err != nil {
				return tools.ErrorResult(detail.ID, "file_copy", err)
			}
			os.Remove(src)
		}
	} else {
		if err := copyFile(src, dst); err != nil {
			return tools.ErrorResult(detail.ID, "file_copy", err)
		}
	}

	return tools.SuccessResult(detail.ID, "file_copy", map[string]any{
		"source":      src,
		"destination": dst,
		"action":      action,
	})
}

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	info, err := s.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("source is a directory, use dir_copy instead")
	}

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return err
	}
	return d.Close()
}
