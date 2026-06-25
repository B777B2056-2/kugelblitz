package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolPrefix is prepended to MCP tool names to avoid conflicts with built-in tools.
const ToolPrefix = "mcp:"

// entry holds a connected MCP session and its client.
type entry struct {
	session *mcp.ClientSession
	client  *mcp.Client
}

// Manager connects to MCP servers, discovers their tools, and registers them
// in the global ToolRegistry. Each server's tools are prefixed to avoid
// collisions: "mcp:<server>_<tool>".
type Manager struct {
	config   *Config
	sessions map[string]entry
	mu       sync.Mutex
}

// NewManager creates a Manager from the given Config.
func NewManager(cfg *Config) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Manager{
		config:   cfg,
		sessions: make(map[string]entry),
	}, nil
}

// ConnectAll connects to all configured MCP servers, discovers their tools,
// and registers them in the global ToolRegistry.
func (m *Manager) ConnectAll(ctx context.Context) error {
	for name, srvCfg := range m.config.MCPServers {
		if err := m.connectServer(ctx, name, srvCfg); err != nil {
			return fmt.Errorf("mcp: server %q: %w", name, err)
		}
	}
	return nil
}

// connectServer connects to a single MCP server via subprocess and registers its tools.
func (m *Manager) connectServer(ctx context.Context, name string, cfg ServerConfig) error {
	core.Info("MCP: connecting", "server", name, "command", cfg.Command)

	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "kugelblitz",
		Version: "0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	if err := m.discoverAndRegister(ctx, name, session); err != nil {
		session.Close()
		return err
	}

	m.mu.Lock()
	m.sessions[name] = entry{session: session, client: client}
	m.mu.Unlock()
	return nil
}

// discoverAndRegister lists tools from the given session and registers them
// in the global ToolRegistry.
func (m *Manager) discoverAndRegister(ctx context.Context, name string, session *mcp.ClientSession) error {
	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	core.Info("MCP: tools discovered", "server", name, "count", len(tools.Tools))

	reg := core.GetToolRegistry()
	for _, tool := range tools.Tools {
		regName := mcpToolName(name, tool.Name)
		def := convertToolDef(regName, tool, name)
		srvName := name
		mcpTool := tool
		mcpSession := session

		fn := func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return callMCPTool(ctx, mcpSession, srvName, mcpTool.Name, detail)
		}
		reg.Register(def, fn)
		core.Debug("MCP: tool registered", "name", regName)
	}
	return nil
}

// Shutdown closes all MCP server connections.
func (m *Manager) Shutdown(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, e := range m.sessions {
		core.Debug("MCP: disconnecting", "server", name)
		if err := e.session.Close(); err != nil {
			core.Warn("MCP: close error", "server", name, "err", err)
		}
	}
	m.sessions = make(map[string]entry)
	return nil
}

// mcpToolName builds the prefixed tool name: "mcp:<server>_<tool>"
func mcpToolName(server, tool string) string {
	return ToolPrefix + server + "_" + tool
}

// convertToolDef converts an MCP Tool to a Kugelblitz ToolDefinition.
func convertToolDef(regName string, tool *mcp.Tool, serverName string) core.ToolDefinition {
	desc := tool.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %s from server %s", tool.Name, serverName)
	}

	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	if is, ok := tool.InputSchema.(map[string]any); ok {
		if props, ok := is["properties"]; ok {
			schema["properties"] = props
		}
		if req, ok := is["required"]; ok {
			schema["required"] = req
		}
		if typ, ok := is["type"]; ok {
			schema["type"] = typ
		}
	}

	return core.ToolDefinition{
		Name:        regName,
		Description: desc,
		JsonSchema:  schema,
	}
}

// callMCPTool forwards a tool call to the MCP server and converts the result.
func callMCPTool(ctx context.Context, session *mcp.ClientSession, serverName, toolName string, detail core.ToolCallDetail) core.ToolCallResult {
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: detail.Args,
	})
	if err != nil {
		core.Warn("MCP: tool call failed", "server", serverName, "tool", toolName, "err", err)
		return core.ToolCallResult{
			ToolCallID: detail.ID,
			ToolName:   detail.ToolName,
			Outputs:    core.MakeErrorToolOutputs(fmt.Errorf("mcp call %s/%s: %w", serverName, toolName, err)),
		}
	}

	if result.IsError {
		errText := ""
		for _, c := range result.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				errText += tc.Text
			}
		}
		core.Warn("MCP: tool returned error", "server", serverName, "tool", toolName, "msg", errText)
		return core.ToolCallResult{
			ToolCallID: detail.ID,
			ToolName:   detail.ToolName,
			Outputs:    core.MakeErrorToolOutputs(fmt.Errorf("mcp %s/%s: %s", serverName, toolName, errText)),
		}
	}

	outputs := map[string]any{}
	for i, c := range result.Content {
		switch ct := c.(type) {
		case *mcp.TextContent:
			outputs[fmt.Sprintf("text_%d", i)] = ct.Text
		case *mcp.ImageContent:
			outputs[fmt.Sprintf("image_%d", i)] = map[string]any{
				"mime_type": ct.MIMEType,
				"data":      string(ct.Data),
			}
		default:
			if b, err := json.Marshal(c); err == nil {
				outputs[fmt.Sprintf("item_%d", i)] = string(b)
			}
		}
	}

	return core.ToolCallResult{
		ToolCallID: detail.ID,
		ToolName:   detail.ToolName,
		Outputs:    outputs,
	}
}
