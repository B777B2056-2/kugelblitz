package internals

import (
	"context"
	"runtime"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/tools"

	"github.com/stretchr/testify/assert"
)

func TestShellExec_SimpleCommand(t *testing.T) {
	tool := &ShellExec{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "shell_exec",
		Args: map[string]any{"command": "echo hello"},
	})

	assert.Nil(t, result.Outputs["error"])
	assert.Equal(t, 0, result.Outputs["exit_code"])
	assert.Contains(t, result.Outputs["stdout"], "hello")
}

func TestShellExec_CommandNotFound(t *testing.T) {
	tool := &ShellExec{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "shell_exec",
		Args: map[string]any{"command": "nonexistent_command_xyz_123"},
	})

	// Should have non-zero exit code or an error
	exitCode, _ := result.Outputs["exit_code"].(int)
	if result.Outputs["error"] == nil {
		assert.NotEqual(t, 0, exitCode, "expected non-zero exit code for unknown command")
	}
}

func TestShellExec_Stderr(t *testing.T) {
	tool := &ShellExec{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "shell_exec",
		Args: map[string]any{"command": "echo error >&2"},
	})

	assert.Nil(t, result.Outputs["error"])
	assert.Contains(t, result.Outputs["stderr"], "error")
}

func TestShellExec_WorkingDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pwd not available on Windows")
	}
	tool := &ShellExec{}
	result := tool.Execute(context.Background(), core.ToolCallDetail{
		ID: "t1", ToolName: "shell_exec",
		Args: map[string]any{"command": "pwd", "cwd": "/tmp"},
	})

	assert.Nil(t, result.Outputs["error"])
	assert.Contains(t, result.Outputs["stdout"], "/tmp")
}

func TestShellExec_Definition(t *testing.T) {
	tool := &ShellExec{}
	def := tool.Definition()
	assert.Equal(t, "shell_exec", def.Name)
	assert.NotEmpty(t, def.Description)
}

var _ tools.Tool = (*ShellExec)(nil)
