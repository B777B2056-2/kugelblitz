// ACP (Agent Client Protocol) Server.
//
// Starts a Kugelblitz agent as an ACP-compatible server over stdin/stdout.
// Any ACP-compatible editor (Zed, JetBrains, VS Code with extension, Neovim)
// can use this as its AI coding agent backend.
//
// Configuration is read from ~/.kugelblitz/kugelblitz.yaml (same file as the
// Web UI).
//
// Usage:
//
//	acp_server                     # uses ~/.kugelblitz/kugelblitz.yaml
//	acp_server -workspace /path    # custom workspace
//	acp_server                     # logs to stderr + ~/.kugelblitz/acp_server/acp_server.log
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/B777B2056-2/kugelblitz/cmd/common"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/runtime"
)

func main() {
	workspaceDir := flag.String("workspace", "", "Workspace directory (default: ~/.kugelblitz)")
	flag.Parse()

	if *workspaceDir != "" {
		core.GetWorkspace().SetDir(*workspaceDir)
	}
	_ = core.GetWorkspace().MkdirAll()

	initLogging("acp_server")

	// Load config from kugelblitz.yaml (same as Web UI)
	cfg, err := common.Load(filepath.Join(core.GetWorkspace().Dir(), "kugelblitz.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// AgentLoop wires up MCP, skills, LTM, session — same as Web UI.
	loop := runtime.NewAgentLoop(cfg)
	srv := NewServer(loop.Agent(), cfg.Model.Provider)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	core.Info("acp_server starting",
		"provider", cfg.Model.ProviderName, "model", cfg.Model.Model,
		"stream", cfg.Model.StreamMode)

	if err := srv.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// initLogging configures the global logger to write to both stderr and a log file
// under ~/.kugelblitz/<subdir>/<subdir>.log.
func initLogging(subdir string) {
	logDir := filepath.Join(core.GetWorkspace().Dir(), subdir)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: cannot create log dir %s: %v\n", logDir, err)
		return
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, subdir+".log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: cannot open log file: %v\n", err)
		return
	}
	handler := slog.NewTextHandler(io.MultiWriter(os.Stderr, logFile),
		&slog.HandlerOptions{Level: slog.LevelInfo})
	core.SetLogger(slog.New(handler))
}
