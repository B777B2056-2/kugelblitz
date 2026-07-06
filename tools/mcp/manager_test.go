package mcp

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestManager creates a Manager connected to an in-memory MCP test server
// with a simple echo tool.
func newTestManager(t *testing.T) *Manager {
	t.Helper()

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

	t1, t2 := mcp.NewInMemoryTransports()

	go func() {
		_, err := srv.Connect(context.Background(), t1, nil)
		require.NoError(t, err)
	}()

	m := &Manager{
		servers:  map[string]config.MCPServerConfig{"test": {Command: "test"}},
		sessions: make(map[string]entry),
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "kugelblitz", Version: "0.1.0"}, nil)
	session, err := client.Connect(context.Background(), t2, nil)
	require.NoError(t, err)
	m.sessions["test"] = entry{session: session, client: client}

	return m
}

func TestManager_DiscoverAndRegisterTools(t *testing.T) {

	m := newTestManager(t)
	defer func() { _ = m.Shutdown(context.Background()) }()

	err := m.discoverAndRegister(context.Background(), "test", m.sessions["test"].session)
	require.NoError(t, err)

	defs := core.ListToolDefinitions()
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	assert.True(t, names["mcp_test_echo"], "expected mcp:test_echo to be registered")
}

func TestManager_CallMCPTool(t *testing.T) {

	m := newTestManager(t)
	defer func() { _ = m.Shutdown(context.Background()) }()

	err := m.discoverAndRegister(context.Background(), "test", m.sessions["test"].session)
	require.NoError(t, err)

	result := core.CallTool(context.Background(), core.ToolCallDetail{
		ID:       "call_1",
		ToolName: "mcp_test_echo",
		Args:     map[string]any{"message": "hello"},
	})

	assert.Nil(t, result.Outputs["error"])
	assert.Contains(t, result.Outputs["text_0"], "echo: hello")
}

func TestManager_CallMCPTool_Error(t *testing.T) {
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
	go func() { _, _ = srv.Connect(context.Background(), t1, nil) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "kugelblitz", Version: "0.1.0"}, nil)
	session, err := client.Connect(context.Background(), t2, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	m := &Manager{
		servers:  map[string]config.MCPServerConfig{"errsrv": {Command: "err"}},
		sessions: map[string]entry{"errsrv": {session: session, client: client}},
	}

	err = m.discoverAndRegister(context.Background(), "errsrv", session)
	require.NoError(t, err)

	result := core.CallTool(context.Background(), core.ToolCallDetail{
		ID:       "call_2",
		ToolName: "mcp_errsrv_failing",
		Args:     map[string]any{},
	})

	assert.NotNil(t, result.Outputs["error"])
	assert.Contains(t, result.Outputs["error"].(string), "deliberate failure")
}

func TestNewManager_Validate(t *testing.T) {
	_, err := NewManager(map[string]config.MCPServerConfig{})
	assert.Error(t, err)

	_, err = NewManager(map[string]config.MCPServerConfig{"test": {Command: ""}})
	assert.Error(t, err)

	_, err = NewManager(map[string]config.MCPServerConfig{"test": {Command: "echo"}})
	assert.NoError(t, err)
}

func TestMCPToolName(t *testing.T) {
	assert.Equal(t, "mcp_github_search", mcpToolName("github", "search"))
	assert.Equal(t, "mcp_slack_post", mcpToolName("slack", "post"))
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

	def := convertToolDef("mcp_test_greet", mcpTool, "test")
	assert.Equal(t, "mcp_test_greet", def.Name)
	assert.Contains(t, def.Description, "Greet someone")
	assert.NotNil(t, def.JSONSchema["properties"])
	assert.Contains(t, def.JSONSchema["required"].([]string), "name")
}

func TestManager_Shutdown(t *testing.T) {
	m := newTestManager(t)
	err := m.Shutdown(context.Background())
	assert.NoError(t, err)
	assert.Empty(t, m.sessions)
}
