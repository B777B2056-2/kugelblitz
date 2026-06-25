package mcp

import (
	"context"
	"os"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestManager creates a Manager connected to an in-memory MCP test server
// with a simple echo tool.
func newTestManager(t *testing.T) *Manager {
	t.Helper()

	// Build test server with one tool
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)

	tool := &mcp.Tool{
		Name:        "echo",
		Description: "Echoes back the input",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{
					"type":        "string",
					"description": "Message to echo",
				},
			},
			"required": []string{"message"},
		},
	}
	mcp.AddTool(srv, tool, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		Message string `json:"message"`
	}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "echo: " + args.Message},
			},
		}, nil, nil
	})

	// Create in-memory transport pair
	t1, t2 := mcp.NewInMemoryTransports()

	// Connect server side (in background)
	go func() {
		_, err := srv.Connect(context.Background(), t1, nil)
		require.NoError(t, err)
	}()

	// Create Manager with a test config that uses our transport
	m := &Manager{
		config: &Config{
			MCPServers: map[string]ServerConfig{
				"test": {Command: "test"},
			},
		},
		sessions: make(map[string]entry),
	}

	// Connect client side using the paired transport
	client := mcp.NewClient(&mcp.Implementation{Name: "kugelblitz", Version: "0.1.0"}, nil)
	session, err := client.Connect(context.Background(), t2, nil)
	require.NoError(t, err)
	m.sessions["test"] = entry{session: session, client: client}

	return m
}

func TestManager_DiscoverAndRegisterTools(t *testing.T) {
	// Reset tool registry
	core.GetToolRegistry().Reset()

	m := newTestManager(t)
	defer m.Shutdown(context.Background())

	// Discover and register tools for the test server
	err := m.discoverAndRegister(context.Background(), "test", m.sessions["test"].session)
	require.NoError(t, err)

	// Verify tool is registered
	defs := core.ListToolDefinitions()
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	assert.True(t, names["mcp:test_echo"], "expected mcp:test_echo to be registered")
}

func TestManager_CallMCPTool(t *testing.T) {
	core.GetToolRegistry().Reset()

	m := newTestManager(t)
	defer m.Shutdown(context.Background())

	err := m.discoverAndRegister(context.Background(), "test", m.sessions["test"].session)
	require.NoError(t, err)

	// Call the tool through the ToolRegistry
	result := core.CallTool(context.Background(), core.ToolCallDetail{
		ID:       "call_1",
		ToolName: "mcp:test_echo",
		Args:     map[string]any{"message": "hello"},
	})

	assert.Nil(t, result.Outputs["error"], "expected no error, got: %v", result.Outputs["error"])
	assert.Contains(t, result.Outputs["text_0"], "echo: hello")
}

func TestManager_CallMCPTool_Error(t *testing.T) {
	// Build server with a tool that always errors
	srv := mcp.NewServer(&mcp.Implementation{Name: "errsrv", Version: "1.0"}, nil)
	errorTool := &mcp.Tool{
		Name:        "failing",
		Description: "Always fails",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}
	mcp.AddTool(srv, errorTool, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{Text: "deliberate failure"},
			},
		}, nil, nil
	})

	t1, t2 := mcp.NewInMemoryTransports()
	go srv.Connect(context.Background(), t1, nil)

	client := mcp.NewClient(&mcp.Implementation{Name: "kugelblitz", Version: "0.1.0"}, nil)
	session, err := client.Connect(context.Background(), t2, nil)
	require.NoError(t, err)
	defer session.Close()

	core.GetToolRegistry().Reset()
	m := &Manager{
		config:   &Config{MCPServers: map[string]ServerConfig{"errsrv": {Command: "err"}}},
		sessions: map[string]entry{"errsrv": {session: session, client: client}},
	}

	err = m.discoverAndRegister(context.Background(), "errsrv", session)
	require.NoError(t, err)

	result := core.CallTool(context.Background(), core.ToolCallDetail{
		ID:       "call_2",
		ToolName: "mcp:errsrv_failing",
		Args:     map[string]any{},
	})

	assert.NotNil(t, result.Outputs["error"])
	assert.Contains(t, result.Outputs["error"].(string), "deliberate failure")
}

func TestConfig_LoadAndValidate(t *testing.T) {
	// Test validation
	cfg := &Config{MCPServers: map[string]ServerConfig{}}
	assert.Error(t, cfg.Validate())

	cfg.MCPServers["test"] = ServerConfig{Command: ""}
	assert.Error(t, cfg.Validate())

	cfg.MCPServers["test"] = ServerConfig{Command: "echo"}
	assert.NoError(t, cfg.Validate())
}

func TestLoadFromWorkspace(t *testing.T) {
	dir := t.TempDir()
	ws := &core.Workspace{}
	ws.SetDir(dir)

	content := `mcpServers:
  github:
    command: npx
    args:
      - "-y"
      - "@modelcontextprotocol/server-github"
    env:
      GITHUB_TOKEN: ghp_xxx
  postgres:
    command: npx
    args:
      - "-y"
      - "@modelcontextprotocol/server-postgres"
      - "postgresql://localhost/mydb"
`
	require.NoError(t, os.WriteFile(dir+"/mcp.yaml", []byte(content), 0644))

	cfg, err := LoadFromWorkspace(ws)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.MCPServers, 2)

	assert.Equal(t, "npx", cfg.MCPServers["github"].Command)
	assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-github"}, cfg.MCPServers["github"].Args)
	assert.Equal(t, "ghp_xxx", cfg.MCPServers["github"].Env["GITHUB_TOKEN"])

	assert.Equal(t, "npx", cfg.MCPServers["postgres"].Command)
	assert.Len(t, cfg.MCPServers["postgres"].Args, 3)
}

func TestLoadFromWorkspace_NoFile(t *testing.T) {
	dir := t.TempDir()
	ws := &core.Workspace{}
	ws.SetDir(dir)

	cfg, err := LoadFromWorkspace(ws)
	assert.NoError(t, err)
	assert.Nil(t, cfg, "no config file means no MCP servers")
}

func TestLoadFromWorkspace_NilWorkspace(t *testing.T) {
	// Should fall back to global workspace (may or may not have mcp.yaml)
	cfg, err := LoadFromWorkspace(nil)
	assert.NoError(t, err)
	// Just verify it doesn't panic; cfg may be nil or not
	_ = cfg
}

func TestMCPToolName(t *testing.T) {
	assert.Equal(t, "mcp:github_search", mcpToolName("github", "search"))
	assert.Equal(t, "mcp:slack_post", mcpToolName("slack", "post"))
}

func TestConvertToolDef(t *testing.T) {
	mcpTool := &mcp.Tool{
		Name:        "greet",
		Description: "Greet someone",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
			"required": []string{"name"},
		},
	}

	def := convertToolDef("mcp:test_greet", mcpTool, "test")
	assert.Equal(t, "mcp:test_greet", def.Name)
	assert.Contains(t, def.Description, "Greet someone")
	assert.NotNil(t, def.JsonSchema["properties"])
	assert.Contains(t, def.JsonSchema["required"].([]string), "name")
}

func TestManager_Shutdown(t *testing.T) {
	m := newTestManager(t)
	err := m.Shutdown(context.Background())
	assert.NoError(t, err)
	assert.Empty(t, m.sessions)
}
