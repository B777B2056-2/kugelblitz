// Kugelblitz Web UI — HTTP server with SSE streaming, plan panel,
// human-in-the-loop, token monitoring, and settings editor.
//
// Configuration is stored in ~/.kugelblitz/kugelblitz.yaml and can be
// edited via the Web UI at runtime. No CLI flags required for API keys.
//
// Usage:
//
//	kugelblitz-ui                  # uses ~/.kugelblitz/kugelblitz.yaml
//	kugelblitz-ui -addr :9090      # custom port
//	kugelblitz-ui -workspace /path # custom workspace
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/observability"
)

func main() {
	addr := flag.String("addr", ":8088", "Listen address")
	workspaceDir := flag.String("workspace", "", "Workspace directory (default: ~/.kugelblitz)")
	flag.Parse()

	if *workspaceDir != "" {
		core.GetWorkspace().SetDir(*workspaceDir)
	}
	_ = core.GetWorkspace().MkdirAll()

	// Initialize logging: stderr + file
	initLogging("webui")

	// Load config from kugelblitz.yaml (auto-creates with defaults if missing)
	cfg := LoadConfig()

	// Initialize OTel tracing (noop if disabled or unconfigured)
	shutdown, err := observability.InitTracer(context.Background(), cfg.Observability)
	if err != nil {
		core.Warn("otel init failed", "err", err)
	}
	defer shutdown()

	srv := NewServer()
	core.Info("kugelblitz-ui starting", "workspace", core.GetWorkspace().Dir())
	if cfg.Model.APIKey == "" {
		core.Warn("api key not configured, set via Settings UI or kugelblitz.yaml")
	} else {
		core.Info("provider configured", "provider", cfg.Model.ProviderName, "model", cfg.Model.Model)
	}
	if err := srv.ListenAndServe(*addr); err != nil {
		core.Error("server listen failed", "err", err)
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
