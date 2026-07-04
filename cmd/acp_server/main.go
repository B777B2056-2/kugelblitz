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
//	acp_server -v                  # verbose logging
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/B777B2056-2/kugelblitz/acp"
	"github.com/B777B2056-2/kugelblitz/cmd/common"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/runtime"
)

func main() {
	workspaceDir := flag.String("workspace", "", "Workspace directory (default: ~/.kugelblitz)")
	verbose := flag.Bool("v", false, "Verbose logging to stderr")
	flag.Parse()

	if *workspaceDir != "" {
		core.GetWorkspace().SetDir(*workspaceDir)
	}
	_ = core.GetWorkspace().MkdirAll()

	if !*verbose {
		core.SetLogger(core.DiscardLogger())
	}

	// Load config from kugelblitz.yaml (same as Web UI)
	cfg, err := common.Load(filepath.Join(core.GetWorkspace().Dir(), "kugelblitz.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// AgentLoop wires up MCP, skills, LTM, session — same as Web UI.
	loop := runtime.NewAgentLoop(cfg)
	srv := acp.NewServer(loop.Agent(), cfg.Model.Provider)

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
