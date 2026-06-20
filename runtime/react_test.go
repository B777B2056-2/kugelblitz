package runtime

import (
	"context"
	"errors"
	"testing"

	"kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider implements core.ILMProvider for testing ReactAgent.
type mockProvider struct {
	generateFn func(ctx context.Context, params core.GenerateParams) (*core.Message, error)
}

func (m *mockProvider) Generate(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
	if m.generateFn != nil {
		return m.generateFn(ctx, params)
	}
	return nil, nil
}

func TestReactAgent_Execute_SimpleTextResponse(t *testing.T) {
	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			msg := core.NewAssistantMessage("msg-1", core.TextContent{Text: "hello world"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage("root", core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage("root", core.TextContent{Text: "hi"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 1)

	textContent, ok := messages[0].Content.(core.TextContent)
	require.True(t, ok, "expected TextContent, got %T", messages[0].Content)
	assert.Equal(t, "hello world", textContent.Text)
}

func TestReactAgent_Execute_SingleToolCall(t *testing.T) {
	core.GetToolRegistry().Reset()
	core.RegisterTool(
		core.ToolDefinition{Name: "get_weather", Description: "Get weather"},
		func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{
				ToolCallID: detail.ID,
				ToolName:   detail.ToolName,
				Outputs:    map[string]any{"temp": 72},
			}
		},
	)

	callCount := 0
	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				msg := core.NewAssistantMessage("msg-1", nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "get_weather", Args: map[string]any{"city": "NYC"}},
					},
				}
				return &msg, nil
			}
			msg := core.NewAssistantMessage("msg-2", core.TextContent{Text: "The weather is 72°F"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage("root", core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage("root", core.TextContent{Text: "what's the weather?"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 2)
	assert.Equal(t, 2, callCount)

	toolCallContent, ok := messages[0].Content.(core.ToolCallContent)
	require.True(t, ok)
	assert.Equal(t, "get_weather", toolCallContent.Details[0].ToolName)

	textContent, ok := messages[1].Content.(core.TextContent)
	require.True(t, ok)
	assert.Equal(t, "The weather is 72°F", textContent.Text)
}

func TestReactAgent_Execute_MultiTurnToolCalls(t *testing.T) {
	core.GetToolRegistry().Reset()
	core.RegisterTool(
		core.ToolDefinition{Name: "step1", Description: "First step"},
		func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{ToolCallID: detail.ID, ToolName: detail.ToolName, Outputs: map[string]any{"done": true}}
		},
	)
	core.RegisterTool(
		core.ToolDefinition{Name: "step2", Description: "Second step"},
		func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{ToolCallID: detail.ID, ToolName: detail.ToolName, Outputs: map[string]any{"done": true}}
		},
	)

	callCount := 0
	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			switch callCount {
			case 1:
				msg := core.NewAssistantMessage("m1", nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{{ID: "tc-1", ToolName: "step1", Args: nil}},
				}
				return &msg, nil
			case 2:
				msg := core.NewAssistantMessage("m2", nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{{ID: "tc-2", ToolName: "step2", Args: nil}},
				}
				return &msg, nil
			default:
				msg := core.NewAssistantMessage("m3", core.TextContent{Text: "done"})
				return &msg, nil
			}
		},
	}

	agent := NewReactAgent(provider, false)
	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage("root", core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage("root", core.TextContent{Text: "go"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 3)
	assert.Equal(t, 3, callCount)
}

func TestReactAgent_Execute_ProviderError(t *testing.T) {
	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			return nil, errors.New("api error")
		},
	}

	agent := NewReactAgent(provider, false)
	_, err := agent.Execute(
		context.Background(),
		core.NewUserMessage("root", core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage("root", core.TextContent{Text: "hi"})},
	)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api error")
}

func TestReactAgent_Execute_ToolNotFound(t *testing.T) {
	core.GetToolRegistry().Reset()
	// Don't register any tools

	callCount := 0
	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				msg := core.NewAssistantMessage("msg-1", nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "nonexistent", Args: nil},
					},
				}
				return &msg, nil
			}
			msg := core.NewAssistantMessage("msg-2", core.TextContent{Text: "I couldn't find that tool"})
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage("root", core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage("root", core.TextContent{Text: "hi"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 2)
}

func TestReactAgent_Execute_BlockMode_CallsEventHandler(t *testing.T) {
	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			// Simulate block mode: only non-chunk callbacks fire
			if params.EventHandler != nil {
				params.EventHandler.OnFinished("stop")
				params.EventHandler.OnUsageUpdated(core.Usage{TotalTokens: 42})
			}
			msg := core.NewAssistantMessage("parent", core.TextContent{Text: "response"})
			msg.FinishReason = "stop"
			msg.Usage = &core.Usage{TotalTokens: 42}
			return &msg, nil
		},
	}

	handler := &testEventHandler{}
	agent := NewReactAgent(provider, false)
	agent.RegisterEventHooks(core.AgentEventHooks{
		ModelEventHandler: handler,
	})

	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage("root", core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage("root", core.TextContent{Text: "hi"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 1)
	// Block mode: chunk callbacks NOT called
	assert.Empty(t, handler.replyChunks)
	assert.Empty(t, handler.thinkingChunks)
	// Block mode: these ARE called
	assert.Equal(t, []string{"stop"}, handler.finishReasons)
	assert.Equal(t, int64(42), handler.usages[0].TotalTokens)
}

func TestReactAgent_Execute_StreamMode_CallsEventHandler(t *testing.T) {
	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			if params.EventHandler != nil {
				params.EventHandler.OnReplyChunk("Hello")
				params.EventHandler.OnReplyChunk(" World")
				params.EventHandler.OnFinished("stop")
			}
			msg := core.NewAssistantMessage("parent", core.TextContent{Text: "Hello World"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	handler := &testEventHandler{}
	agent := NewReactAgent(provider, true)
	agent.RegisterEventHooks(core.AgentEventHooks{
		ModelEventHandler: handler,
	})

	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage("root", core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage("root", core.TextContent{Text: "hi"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, []string{"Hello", " World"}, handler.replyChunks)
	assert.Equal(t, []string{"stop"}, handler.finishReasons)
}

func TestReactAgent_RegisterEventHooks_StoresCorrectly(t *testing.T) {
	agent := NewReactAgent(nil, false)
	hooks := core.AgentEventHooks{
		OnToolCallEnd: func(result core.ToolCallResult) {},
	}
	agent.RegisterEventHooks(hooks)
	assert.NotNil(t, agent.eventHooks.OnToolCallEnd)
}

func TestReactAgent_Interrupt_SendsSignal(t *testing.T) {
	agent := NewReactAgent(nil, false)
	err := agent.Interrupt(context.Background())
	assert.NoError(t, err)

	select {
	case <-agent.abortSignal:
		// Expected
	default:
		t.Error("expected abort signal to be sent")
	}
}

func TestReactAgent_WithTools_FiltersVisibleTools(t *testing.T) {
	core.GetToolRegistry().Reset()
	core.RegisterTool(core.ToolDefinition{Name: "tool_a", Description: "A"}, nil)
	core.RegisterTool(core.ToolDefinition{Name: "tool_b", Description: "B"}, nil)
	core.RegisterTool(core.ToolDefinition{Name: "tool_c", Description: "C"}, nil)

	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			// Verify only whitelisted tools are passed
			assert.Len(t, params.Tools, 2)
			names := make(map[string]bool)
			for _, d := range params.Tools {
				names[d.Name] = true
			}
			assert.True(t, names["tool_a"])
			assert.True(t, names["tool_c"])
			assert.False(t, names["tool_b"])

			msg := core.NewAssistantMessage("m", core.TextContent{Text: "done"})
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	agent.WithTools("tool_a", "tool_c")

	_, err := agent.Execute(
		context.Background(),
		core.NewUserMessage("r", core.TextContent{Text: "sys"}),
		[]core.Message{core.NewUserMessage("r", core.TextContent{Text: "hi"})},
	)
	require.NoError(t, err)
}

func TestReactAgent_WithTools_EmptyResetsToAll(t *testing.T) {
	core.GetToolRegistry().Reset()
	core.RegisterTool(core.ToolDefinition{Name: "tool_x", Description: "X"}, nil)

	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			assert.Len(t, params.Tools, 1) // sees all after reset
			msg := core.NewAssistantMessage("m", core.TextContent{Text: "ok"})
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	agent.WithTools("some_other").WithTools() // clear filter
	_, err := agent.Execute(
		context.Background(),
		core.NewUserMessage("r", core.TextContent{Text: "sys"}),
		[]core.Message{core.NewUserMessage("r", core.TextContent{Text: "hi"})},
	)
	require.NoError(t, err)
}

func TestReactAgent_WithTools_ChainedCalls(t *testing.T) {
	agent := NewReactAgent(nil, false)
	agent.WithTools("a", "b").WithTools("c")
	assert.Len(t, agent.toolNames, 3)
	assert.Equal(t, []string{"a", "b", "c"}, agent.toolNames)
}

func TestReactAgent_DefaultSeesAllTools(t *testing.T) {
	core.GetToolRegistry().Reset()
	core.RegisterTool(core.ToolDefinition{Name: "t1", Description: "T1"}, nil)
	core.RegisterTool(core.ToolDefinition{Name: "t2", Description: "T2"}, nil)

	provider := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			assert.Len(t, params.Tools, 2) // sees all
			msg := core.NewAssistantMessage("m", core.TextContent{Text: "ok"})
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false) // no WithTools call
	_, err := agent.Execute(
		context.Background(),
		core.NewUserMessage("r", core.TextContent{Text: "sys"}),
		[]core.Message{core.NewUserMessage("r", core.TextContent{Text: "hi"})},
	)
	require.NoError(t, err)
}

// testEventHandler implements core.ModelEventHandler for testing.
type testEventHandler struct {
	thinkingChunks []string
	replyChunks    []string
	toolCalls      []core.ToolCallDetail
	finishReasons  []string
	usages         []core.Usage
	errors         []error
}

func (h *testEventHandler) OnThinkingChunk(chunk string)           { h.thinkingChunks = append(h.thinkingChunks, chunk) }
func (h *testEventHandler) OnReplyChunk(chunk string)              { h.replyChunks = append(h.replyChunks, chunk) }
func (h *testEventHandler) OnFunctionCall(detail core.ToolCallDetail) { h.toolCalls = append(h.toolCalls, detail) }
func (h *testEventHandler) OnFinished(reason string)               { h.finishReasons = append(h.finishReasons, reason) }
func (h *testEventHandler) OnUsageUpdated(usage core.Usage)        { h.usages = append(h.usages, usage) }
func (h *testEventHandler) OnError(err error)                      { h.errors = append(h.errors, err) }
