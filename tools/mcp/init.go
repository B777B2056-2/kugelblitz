package mcp

import (
	"context"
	"sync"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/core"
)

var (
	globalMgr  *Manager
	globalOnce sync.Once
)

// Init connects to all MCP servers and registers their tools globally.
// Idempotent — only connects once per process. An empty or nil config is a
// no-op. Returns the manager so callers can Shutdown.
func Init(ctx context.Context, servers map[string]config.MCPServerConfig) *Manager {
	globalOnce.Do(func() {
		if len(servers) == 0 {
			return
		}
		mgr, err := NewManager(servers)
		if err != nil {
			core.Warn("mcp: skip init", "err", err)
			return
		}
		if err := mgr.ConnectAll(ctx); err != nil {
			core.Warn("mcp: connect failed", "err", err)
		}
		globalMgr = mgr
	})
	return globalMgr
}
