package mcp

import (
	"context"
	"sync"
	"time"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/core"
)

// connectTimeout bounds the connection attempt so a hung MCP server does not
// block agent startup forever (B21).
const connectTimeout = 30 * time.Second

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
		connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
		defer cancel()
		if err := mgr.ConnectAll(connectCtx); err != nil {
			core.Warn("mcp: connect failed", "err", err)
		}
		globalMgr = mgr
	})
	return globalMgr
}

// ShutdownGlobal closes all MCP server connections managed by the global
// singleton, if any. Safe to call when Init was a no-op or never called.
func ShutdownGlobal(ctx context.Context) error {
	if globalMgr == nil {
		return nil
	}
	return globalMgr.Shutdown(ctx)
}

// ResetGlobal resets the global MCP manager singleton so the next Init call
// reconnects from scratch. Callers should ShutdownGlobal first to close any
// open connections. Intended for tests.
func ResetGlobal() {
	globalOnce = sync.Once{}
	globalMgr = nil
}
