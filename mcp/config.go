package mcp

import (
	"fmt"
	"os"

	"github.com/B777B2056-2/kugelblitz/core"
	"gopkg.in/yaml.v3"
)

// Config holds the configuration for MCP servers.
// Multiple servers can be defined under mcpServers.
type Config struct {
	MCPServers map[string]ServerConfig `yaml:"mcpServers"`
}

// ServerConfig defines how to connect to a single MCP server.
type ServerConfig struct {
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
}

// LoadFromWorkspace reads the MCP configuration from the workspace directory.
// The file must be at <workspace>/mcp.yaml. Returns nil, nil if the file
// does not exist (no MCP servers configured).
func LoadFromWorkspace(ws *core.Workspace) (*Config, error) {
	if ws == nil {
		ws = core.GetWorkspace()
	}

	path := ws.Dir() + "/mcp.yaml"
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no config = no MCP servers
		}
		return nil, fmt.Errorf("mcp: read %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("mcp: parse %s: %w", path, err)
	}

	return &cfg, nil
}

// Validate checks that the configuration has at least one server and that
// each server has a command.
func (c *Config) Validate() error {
	if c == nil || len(c.MCPServers) == 0 {
		return fmt.Errorf("mcp: no servers configured")
	}
	for name, srv := range c.MCPServers {
		if srv.Command == "" {
			return fmt.Errorf("mcp: server %q has no command", name)
		}
	}
	return nil
}
