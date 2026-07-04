package internals

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/tools"
)

// ShellExec executes a shell command and returns stdout, stderr, and exit code.
// Supports cwd and timeout options.
type ShellExec struct{}

func (t *ShellExec) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "shell_exec",
		Description: "Execute a shell command and return the output. Supports optional cwd and timeout (seconds, default 30). Max output: 4000 chars each for stdout/stderr.",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command to execute",
				},
				"cwd": map[string]any{
					"type":        "string",
					"description": "Working directory for the command (optional)",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Timeout in seconds (optional, default 30, max 120)",
				},
			},
			"required": []string{"command"},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"stdout":    map[string]any{"type": "string", "description": "Standard output"},
				"stderr":    map[string]any{"type": "string", "description": "Standard error"},
				"exit_code": map[string]any{"type": "integer", "description": "Exit code (0 = success)"},
			},
		},
	}
}

func (t *ShellExec) Execute(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	command, err := tools.Arg(detail, "command")
	if err != nil {
		return tools.ErrorResult(detail.ID, "shell_exec", err)
	}

	cwd, _ := tools.Arg(detail, "cwd")

	timeout := 30 * time.Second
	if timeoutSec, err := tools.OptionalInt(detail, "timeout", 30); err != nil {
		return tools.ErrorResult(detail.ID, "shell_exec", err)
	} else if timeoutSec != 30 {
		if timeoutSec < 1 {
			timeoutSec = 1
		}
		if timeoutSec > 120 {
			timeoutSec = 120
		}
		timeout = time.Duration(timeoutSec) * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}

	if cwd != "" {
		cmd.Dir = cwd
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() != nil {
			return tools.ErrorResult(detail.ID, "shell_exec",
				fmt.Errorf("command timed out after %v", timeout))
		} else {
			return tools.ErrorResult(detail.ID, "shell_exec", err)
		}
	}

	stdoutStr := truncateString(strings.TrimSpace(stdout.String()), 4000)
	stderrStr := truncateString(strings.TrimSpace(stderr.String()), 4000)

	return tools.SuccessResult(detail.ID, "shell_exec", map[string]any{
		"stdout":    stdoutStr,
		"stderr":    stderrStr,
		"exit_code": exitCode,
	})
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + fmt.Sprintf("\n... (truncated, %d total chars)", len(s))
}
