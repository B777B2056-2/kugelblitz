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
	"flag"
	"log"
	"os"

	"github.com/B777B2056-2/kugelblitz/core"
)

func main() {
	addr := flag.String("addr", ":8088", "Listen address")
	workspaceDir := flag.String("workspace", "", "Workspace directory (default: ~/.kugelblitz)")
	flag.Parse()

	if *workspaceDir != "" {
		core.GetWorkspace().SetDir(*workspaceDir)
	}
	_ = core.GetWorkspace().MkdirAll()

	// Load config from kugelblitz.yaml (auto-creates with defaults if missing)
	cfg := LoadConfig()

	srv := NewServer()
	log.Printf("[main] kugelblitz-ui starting workspace=%s", core.GetWorkspace().Dir())
	if cfg.Model.APIKey == "" {
		log.Printf("[main] WARNING: api key not configured, set via Settings UI or kugelblitz.yaml")
	} else {
		log.Printf("[main] provider=%q model=%q", cfg.Model.ProviderName, cfg.Model.Model)
	}
	if err := srv.ListenAndServe(*addr); err != nil {
		log.Printf("[main] FATAL: %v", err)
		os.Exit(1)
	}
}
