package infra

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockProvider implements core.ILMProvider for testing ReactAgent.
type MockProvider struct {
	GenerateFn func(ctx context.Context, params core.GenerateParams) (*core.Message, error)
}

func (m *MockProvider) Generate(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
	if m.GenerateFn != nil {
		return m.GenerateFn(ctx, params)
	}
	return nil, nil
}

func TestReactAgent_Execute_SimpleTextResponse(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			msg := core.NewAssistantMessage(core.TextContent{Text: "hello world"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "hi"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 1)

	textContent, ok := messages[0].Content.(core.TextContent)
	require.True(t, ok, "expected TextContent, got %T", messages[0].Content)
	assert.Equal(t, "hello world", textContent.Text)
}

func TestReactAgent_Execute_SingleToolCall(t *testing.T) {

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
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				msg := core.NewAssistantMessage(nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "get_weather", Args: map[string]any{"city": "NYC"}},
					},
				}
				return &msg, nil
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "The weather is 72°F"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "what's the weather?"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 3)
	assert.Equal(t, 2, callCount)

	toolCallContent, ok := messages[0].Content.(core.ToolCallContent)
	require.True(t, ok)
	assert.Equal(t, "get_weather", toolCallContent.Details[0].ToolName)

	textContent, ok := messages[2].Content.(core.TextContent)
	require.True(t, ok)
	assert.Equal(t, "The weather is 72°F", textContent.Text)
}

func TestReactAgent_Execute_MultiTurnToolCalls(t *testing.T) {

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
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			switch callCount {
			case 1:
				msg := core.NewAssistantMessage(nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{{ID: "tc-1", ToolName: "step1", Args: nil}},
				}
				return &msg, nil
			case 2:
				msg := core.NewAssistantMessage(nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{{ID: "tc-2", ToolName: "step2", Args: nil}},
				}
				return &msg, nil
			default:
				msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
				return &msg, nil
			}
		},
	}

	agent := NewReactAgent(provider, false)
	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "go"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 5)
	assert.Equal(t, 3, callCount)
}

func TestReactAgent_Execute_ProviderError(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			return nil, errors.New("api error")
		},
	}

	agent := NewReactAgent(provider, false)
	_, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "hi"})},
	)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api error")
}

func TestReactAgent_Execute_ToolNotFound(t *testing.T) {

	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				msg := core.NewAssistantMessage(nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "nonexistent", Args: nil},
					},
				}
				return &msg, nil
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "I couldn't find that tool"})
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "hi"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 3)
}

func TestReactAgent_Execute_BlockMode_CallsEventHandler(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			if params.EventHandler != nil {
				params.EventHandler.OnFinished("stop")
				params.EventHandler.OnUsageUpdated(core.Usage{TotalTokens: 42})
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "response"})
			msg.FinishReason = "stop"
			msg.Usage = &core.Usage{TotalTokens: 42}
			return &msg, nil
		},
	}

	handler := &testEventHandler{}
	agent := NewReactAgent(provider, false)
	agent.RegisterEventHooks(core.AgentEventHooks{
		OnReplyChunk:    func(id constants.AgentIdentity, chunk string) { handler.OnReplyChunk(chunk) },
		OnThinkingChunk: func(id constants.AgentIdentity, chunk string) { handler.OnThinkingChunk(chunk) },
		OnBlockReply:    func(id constants.AgentIdentity, text string) { handler.OnBlockReply(text) },
		OnBlockThinking: func(id constants.AgentIdentity, reasoning string) { handler.OnBlockThinking(reasoning) },
		OnFunctionCall:  func(id constants.AgentIdentity, detail core.ToolCallDetail) { handler.OnFunctionCall(detail) },
		OnModelFinished: func(id constants.AgentIdentity, reason string) { handler.OnFinished(reason) },
		OnUsageUpdated:  func(id constants.AgentIdentity, usage core.Usage) { handler.OnUsageUpdated(usage) },
		OnError:         func(id constants.AgentIdentity, err error) { handler.OnError(err) },
	})

	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "hi"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Empty(t, handler.replyChunks)
	assert.Empty(t, handler.thinkingChunks)
	assert.Equal(t, []string{"stop"}, handler.finishReasons)
	assert.Equal(t, int64(42), handler.usages[0].TotalTokens)
}

func TestReactAgent_Execute_StreamMode_CallsEventHandler(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			if params.EventHandler != nil {
				params.EventHandler.OnReplyChunk("Hello")
				params.EventHandler.OnReplyChunk(" World")
				params.EventHandler.OnFinished("stop")
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "Hello World"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	handler := &testEventHandler{}
	agent := NewReactAgent(provider, true)
	agent.RegisterEventHooks(core.AgentEventHooks{
		OnReplyChunk:    func(id constants.AgentIdentity, chunk string) { handler.OnReplyChunk(chunk) },
		OnThinkingChunk: func(id constants.AgentIdentity, chunk string) { handler.OnThinkingChunk(chunk) },
		OnBlockReply:    func(id constants.AgentIdentity, text string) { handler.OnBlockReply(text) },
		OnBlockThinking: func(id constants.AgentIdentity, reasoning string) { handler.OnBlockThinking(reasoning) },
		OnFunctionCall:  func(id constants.AgentIdentity, detail core.ToolCallDetail) { handler.OnFunctionCall(detail) },
		OnModelFinished: func(id constants.AgentIdentity, reason string) { handler.OnFinished(reason) },
		OnUsageUpdated:  func(id constants.AgentIdentity, usage core.Usage) { handler.OnUsageUpdated(usage) },
		OnError:         func(id constants.AgentIdentity, err error) { handler.OnError(err) },
	})

	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "hi"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, []string{"Hello", " World"}, handler.replyChunks)
	assert.Equal(t, []string{"stop"}, handler.finishReasons)
}

func TestReactAgent_RegisterEventHooks_StoresCorrectly(t *testing.T) {
	agent := NewReactAgent(nil, false)
	hooks := core.AgentEventHooks{
		OnToolCallEnd: func(id constants.AgentIdentity, result core.ToolCallResult) {},
	}
	agent.RegisterEventHooks(hooks)
	assert.NotNil(t, agent.EventHooks.OnToolCallEnd)
}

func TestReactAgent_Interrupt_SendsSignal(t *testing.T) {
	agent := NewReactAgent(nil, false)
	err := agent.Interrupt(context.Background())
	assert.NoError(t, err)

	select {
	case <-agent.abortSignal:
	default:
		t.Error("expected abort signal to be sent")
	}
}

func TestReactAgent_WithTools_FiltersVisibleTools(t *testing.T) {

	core.RegisterTool(core.ToolDefinition{Name: "tool_a", Description: "A"}, nil)
	core.RegisterTool(core.ToolDefinition{Name: "tool_b", Description: "B"}, nil)
	core.RegisterTool(core.ToolDefinition{Name: "tool_c", Description: "C"}, nil)

	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			assert.Len(t, params.Tools, 2)
			names := make(map[string]bool)
			for _, d := range params.Tools {
				names[d.Name] = true
			}
			assert.True(t, names["tool_a"])
			assert.True(t, names["tool_c"])
			assert.False(t, names["tool_b"])

			msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	agent.WithTools("tool_a", "tool_c")

	_, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "sys"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "hi"})},
	)
	require.NoError(t, err)
}

func TestReactAgent_WithTools_EmptyResetsToAll(t *testing.T) {

	core.RegisterTool(core.ToolDefinition{Name: "tool_x", Description: "X"}, nil)

	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			assert.GreaterOrEqual(t, len(params.Tools), 1)
			msg := core.NewAssistantMessage(core.TextContent{Text: "ok"})
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	agent.WithTools("some_other").WithTools()
	_, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "sys"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "hi"})},
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

	core.RegisterTool(core.ToolDefinition{Name: "t1", Description: "T1"}, nil)
	core.RegisterTool(core.ToolDefinition{Name: "t2", Description: "T2"}, nil)

	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			assert.GreaterOrEqual(t, len(params.Tools), 2)
			msg := core.NewAssistantMessage(core.TextContent{Text: "ok"})
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	_, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "sys"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "hi"})},
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

func (h *testEventHandler) OnThinkingChunk(chunk string) {
	h.thinkingChunks = append(h.thinkingChunks, chunk)
}
func (h *testEventHandler) OnReplyChunk(chunk string) { h.replyChunks = append(h.replyChunks, chunk) }
func (h *testEventHandler) OnBlockThinking(reasoning string) {
	h.thinkingChunks = append(h.thinkingChunks, reasoning)
}
func (h *testEventHandler) OnBlockReply(text string) { h.replyChunks = append(h.replyChunks, text) }
func (h *testEventHandler) OnFunctionCall(detail core.ToolCallDetail) {
	h.toolCalls = append(h.toolCalls, detail)
}
func (h *testEventHandler) OnFinished(reason string) {
	h.finishReasons = append(h.finishReasons, reason)
}
func (h *testEventHandler) OnUsageUpdated(usage core.Usage) { h.usages = append(h.usages, usage) }
func (h *testEventHandler) OnError(err error)               { h.errors = append(h.errors, err) }

func TestEnableHumanInTheLoop_SetsUpLocalTool(t *testing.T) {
	agent := NewReactAgent(nil, false)
	agent.EnableHumanInTheLoop()

	require.NotNil(t, agent.humanLoop)
	assert.NotNil(t, agent.humanLoop.localTools["ask_human"])
	assert.NotNil(t, agent.humanLoop.localDefs["ask_human"])
	assert.Len(t, agent.humanLoop.responseCh, 0)
}

func TestEnableHumanInTheLoop_Idempotent(t *testing.T) {
	agent := NewReactAgent(nil, false)
	agent.EnableHumanInTheLoop()
	first := agent.humanLoop
	agent.EnableHumanInTheLoop()
	assert.Same(t, first, agent.humanLoop, "second call should be no-op")
}

func TestVisibleTools_IncludesLocalToolAfterEnable(t *testing.T) {

	core.RegisterTool(core.ToolDefinition{Name: "global_tool", Description: "A global tool"}, nil)

	agent := NewReactAgent(nil, false)
	agent.EnableHumanInTheLoop()

	defs := agent.visibleTools()
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	assert.True(t, names["global_tool"], "should see global tools")
	assert.True(t, names["ask_human"], "should see local ask_human tool")
}

func TestCallTool_LocalToolOverridesGlobal(t *testing.T) {

	core.RegisterTool(
		core.ToolDefinition{Name: "ask_human", Description: "global fake"},
		func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{
				ToolCallID: detail.ID,
				ToolName:   "ask_human",
				Outputs:    map[string]any{"response": "from global"},
			}
		},
	)

	agent := NewReactAgent(nil, false)
	agent.EnableHumanInTheLoop()

	_, hasLocal := agent.humanLoop.localTools["ask_human"]
	assert.True(t, hasLocal, "local ask_human should take precedence over global")
}

func TestWaitForHuman_FiresCallback(t *testing.T) {
	agent := NewReactAgent(nil, false)
	agent.EnableHumanInTheLoop()

	var cbReason, cbPrompt string
	var cbWg sync.WaitGroup
	cbWg.Add(1)

	agent.RegisterEventHooks(core.AgentEventHooks{
		OnWaitForHumanAction: func(id constants.AgentIdentity, reason, prompt string) {
			cbReason = reason
			cbPrompt = prompt
			cbWg.Done()
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go func() {
		_, _ = agent.WaitForHuman(ctx, "need_approval", "proceed?")
	}()

	cbWg.Wait()

	assert.Equal(t, "need_approval", cbReason)
	assert.Equal(t, "proceed?", cbPrompt)
	assert.True(t, agent.humanLoop.isWaiting.Load())
}

func TestResumeWithHumanResponse_UnblocksWaitForHuman(t *testing.T) {
	agent := NewReactAgent(nil, false)
	agent.EnableHumanInTheLoop()

	var wg sync.WaitGroup
	wg.Add(1)
	var result string
	var resultErr error

	go func() {
		defer wg.Done()
		result, resultErr = agent.WaitForHuman(context.Background(), "r", "p")
	}()

	time.Sleep(50 * time.Millisecond)
	assert.True(t, agent.humanLoop.isWaiting.Load())

	err := agent.ResumeWithHumanResponse(context.Background(), "yes, go ahead")
	require.NoError(t, err)

	wg.Wait()
	assert.NoError(t, resultErr)
	assert.Equal(t, "yes, go ahead", result)
	assert.False(t, agent.humanLoop.isWaiting.Load())
}

func TestResumeWithHumanResponse_ErrorWhenNotEnabled(t *testing.T) {
	agent := NewReactAgent(nil, false)
	err := agent.ResumeWithHumanResponse(context.Background(), "response")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestResumeWithHumanResponse_ErrorWhenNotWaiting(t *testing.T) {
	agent := NewReactAgent(nil, false)
	agent.EnableHumanInTheLoop()

	err := agent.ResumeWithHumanResponse(context.Background(), "response")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not waiting")
}

func TestWaitForHuman_ContextCanceled(t *testing.T) {
	agent := NewReactAgent(nil, false)
	agent.EnableHumanInTheLoop()

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	var resultErr error

	go func() {
		defer wg.Done()
		_, resultErr = agent.WaitForHuman(ctx, "r", "p")
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	wg.Wait()
	assert.Error(t, resultErr)
	assert.Equal(t, context.Canceled, resultErr)
}

func TestWaitForHuman_ErrorWhenNotEnabled(t *testing.T) {
	agent := NewReactAgent(nil, false)
	_, err := agent.WaitForHuman(context.Background(), "r", "p")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestWaitForHuman_OnWaitForHumanActionCanBeNil(t *testing.T) {
	agent := NewReactAgent(nil, false)
	agent.EnableHumanInTheLoop()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = agent.WaitForHuman(ctx, "r", "p")
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()
}

func TestReactAgent_Execute_MultipleSequentialAskHuman(t *testing.T) {

	callCount := 0
	mockProv := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			switch callCount {
			case 1:
				msg := core.NewAssistantMessage(nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "ask_human", Args: map[string]any{
							"question": "Proceed with step 1?",
							"reason":   "first check",
						}},
					},
				}
				return &msg, nil
			case 2:
				msg := core.NewAssistantMessage(nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-2", ToolName: "ask_human", Args: map[string]any{
							"question": "Which method, A or B?",
							"reason":   "need choice",
						}},
					},
				}
				return &msg, nil
			default:
				msg := core.NewAssistantMessage(core.TextContent{Text: "Done."})
				msg.FinishReason = "stop"
				return &msg, nil
			}
		},
	}

	agent := NewReactAgent(mockProv, false)
	agent.EnableHumanInTheLoop()

	var waitReasons []string
	var waitPrompts []string
	agent.RegisterEventHooks(core.AgentEventHooks{
		OnWaitForHumanAction: func(id constants.AgentIdentity, reason, prompt string) {
			waitReasons = append(waitReasons, reason)
			waitPrompts = append(waitPrompts, prompt)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var messages []core.Message
	var execErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		messages, execErr = agent.Execute(ctx,
			core.NewUserMessage(core.TextContent{Text: "system"}),
			[]core.Message{core.NewUserMessage(core.TextContent{Text: "multi-step"})},
		)
	}()

	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 20; i++ {
		if agent.humanLoop.isWaiting.Load() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.True(t, agent.humanLoop.isWaiting.Load(), "should be waiting for first input")
	require.NoError(t, agent.ResumeWithHumanResponse(ctx, "yes, go ahead"))

	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 20; i++ {
		if agent.humanLoop.isWaiting.Load() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.True(t, agent.humanLoop.isWaiting.Load(), "should be waiting for second input")
	require.NoError(t, agent.ResumeWithHumanResponse(ctx, "choose A"))

	wg.Wait()
	require.NoError(t, execErr)
	assert.Equal(t, 3, callCount)
	assert.Len(t, messages, 5)

	assert.Equal(t, []string{"first check", "need choice"}, waitReasons)
	assert.Equal(t, []string{"Proceed with step 1?", "Which method, A or B?"}, waitPrompts)
}

func TestReactAgent_OnToolCallEndFiresForAskHuman(t *testing.T) {

	callCount := 0
	mockProv := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				msg := core.NewAssistantMessage(nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "ask_human", Args: map[string]any{
							"question": "OK?",
						}},
					},
				}
				return &msg, nil
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	agent := NewReactAgent(mockProv, false)
	agent.EnableHumanInTheLoop()

	var toolCallEndResults []core.ToolCallResult
	agent.RegisterEventHooks(core.AgentEventHooks{
		OnToolCallEnd: func(id constants.AgentIdentity, r core.ToolCallResult) {
			toolCallEndResults = append(toolCallEndResults, r)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = agent.Execute(ctx,
			core.NewUserMessage(core.TextContent{Text: "system"}),
			[]core.Message{core.NewUserMessage(core.TextContent{Text: "check"})},
		)
	}()

	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 20; i++ {
		if agent.humanLoop.isWaiting.Load() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	_ = agent.ResumeWithHumanResponse(ctx, "approved")

	wg.Wait()

	require.Len(t, toolCallEndResults, 1)
	assert.Equal(t, "ask_human", toolCallEndResults[0].ToolName)
	assert.Equal(t, map[string]any{"response": "approved"}, toolCallEndResults[0].Outputs)
}

func TestReactAgent_ParallelToolsWithAskHuman(t *testing.T) {

	core.RegisterTool(
		core.ToolDefinition{Name: "side_effect", Description: "A side effect tool"},
		func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{
				ToolCallID: detail.ID,
				ToolName:   "side_effect",
				Outputs:    map[string]any{"done": true},
			}
		},
	)

	callCount := 0
	mockProv := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				msg := core.NewAssistantMessage(nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "side_effect", Args: map[string]any{}},
						{ID: "tc-2", ToolName: "ask_human", Args: map[string]any{
							"question": "Continue?",
						}},
					},
				}
				return &msg, nil
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	agent := NewReactAgent(mockProv, false)
	agent.EnableHumanInTheLoop()
	agent.WithTools("side_effect", "ask_human")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	var messages []core.Message
	go func() {
		defer wg.Done()
		messages, _ = agent.Execute(ctx,
			core.NewUserMessage(core.TextContent{Text: "system"}),
			[]core.Message{core.NewUserMessage(core.TextContent{Text: "go"})},
		)
	}()

	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 20; i++ {
		if agent.humanLoop.isWaiting.Load() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.True(t, agent.humanLoop.isWaiting.Load(), "ask_human should be blocking")

	_ = agent.ResumeWithHumanResponse(ctx, "yes")

	wg.Wait()
	require.Len(t, messages, 3)
	tcc, ok := messages[0].Content.(core.ToolCallContent)
	require.True(t, ok)
	assert.Len(t, tcc.Details, 2)
	txt, ok := messages[2].Content.(core.TextContent)
	require.True(t, ok)
	assert.Equal(t, "done", txt.Text)
}

func TestHumanLoopWaiting_ReturnsTrueWhileWaiting(t *testing.T) {
	agent := NewReactAgent(nil, false)
	assert.False(t, agent.HumanLoopWaiting())

	agent.EnableHumanInTheLoop()
	assert.False(t, agent.HumanLoopWaiting())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = agent.WaitForHuman(ctx, "r", "p")
	}()

	time.Sleep(50 * time.Millisecond)
	assert.True(t, agent.HumanLoopWaiting())

	cancel()
	wg.Wait()
	assert.False(t, agent.HumanLoopWaiting())
}

func TestReactAgent_WithTools_FiltersLocalTool(t *testing.T) {

	core.RegisterTool(core.ToolDefinition{Name: "tool_a", Description: "A"}, nil)

	agent := NewReactAgent(nil, false)
	agent.EnableHumanInTheLoop()
	agent.WithTools("tool_a")

	defs := agent.visibleTools()
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	assert.True(t, names["tool_a"], "tool_a should be visible")
	assert.False(t, names["ask_human"], "ask_human should be filtered out when not whitelisted")
}

func TestReactAgent_Execute_WithAskHumanIntegration(t *testing.T) {

	callCount := 0
	mockProv := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				msg := core.NewAssistantMessage(nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "ask_human", Args: map[string]any{
							"question": "Should I delete the file?",
							"reason":   "need approval",
						}},
					},
				}
				return &msg, nil
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "Got it, won't delete."})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	agent := NewReactAgent(mockProv, false)
	agent.EnableHumanInTheLoop()

	var onWaitCalled bool
	agent.RegisterEventHooks(core.AgentEventHooks{
		OnWaitForHumanAction: func(id constants.AgentIdentity, reason, prompt string) {
			onWaitCalled = true
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var messages []core.Message
	var execErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		messages, execErr = agent.Execute(ctx,
			core.NewUserMessage(core.TextContent{Text: "system"}),
			[]core.Message{core.NewUserMessage(core.TextContent{Text: "do it"})},
		)
	}()

	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 20; i++ {
		if agent.humanLoop.isWaiting.Load() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	require.True(t, onWaitCalled, "OnWaitForHumanAction should have been called")
	require.True(t, agent.humanLoop.isWaiting.Load(), "agent should be waiting")

	err := agent.ResumeWithHumanResponse(ctx, "No, do not delete.")
	require.NoError(t, err)

	wg.Wait()
	require.NoError(t, execErr)
	require.Len(t, messages, 3)
	assert.Equal(t, 2, callCount)

	textContent, ok := messages[2].Content.(core.TextContent)
	require.True(t, ok)
	assert.Equal(t, "Got it, won't delete.", textContent.Text)
}

// ---- Full ReAct cycle tests ----

func TestReactAgent_FullReActCycle_CallbackOrder(t *testing.T) {

	core.RegisterTool(
		core.ToolDefinition{Name: "get_weather", Description: "Get weather"},
		func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{
				ToolCallID: detail.ID, ToolName: detail.ToolName,
				Outputs: map[string]any{"temp": 72},
			}
		},
	)

	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				// Simulate what real provider does: fire OnFunctionCall for each tool call
				if params.EventHandler != nil {
					params.EventHandler.OnFunctionCall(core.ToolCallDetail{ID: "tc-1", ToolName: "get_weather", Args: map[string]any{"city": "NYC"}})
				}
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "get_weather", Args: map[string]any{"city": "NYC"}},
					},
				})
				return &msg, nil
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "The weather is 72F"})
			msg.FinishReason = "stop"
			if params.EventHandler != nil {
				params.EventHandler.OnFinished("stop")
			}
			return &msg, nil
		},
	}

	var callOrder []string
	agent := NewReactAgent(provider, false)
	agent.SetAgentIdentity(constants.AgentMain)
	agent.RegisterEventHooks(core.AgentEventHooks{
		OnFunctionCall: func(id constants.AgentIdentity, detail core.ToolCallDetail) {
			assert.Equal(t, constants.AgentMain, id)
			callOrder = append(callOrder, "function_call")
		},
		OnToolCallEnd: func(id constants.AgentIdentity, result core.ToolCallResult) {
			assert.Equal(t, constants.AgentMain, id)
			callOrder = append(callOrder, "tool_call_end")
		},
		OnModelFinished: func(id constants.AgentIdentity, reason string) {
			assert.Equal(t, constants.AgentMain, id)
			callOrder = append(callOrder, "model_finished")
		},
	})

	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "what's the weather?"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 3)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, []string{"function_call", "tool_call_end", "model_finished"}, callOrder)
}

func TestReactAgent_MultiTurnWithErrors(t *testing.T) {

	core.RegisterTool(
		core.ToolDefinition{Name: "get_weather", Description: "Get weather"},
		func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{
				ToolCallID: detail.ID, ToolName: detail.ToolName,
				Outputs: map[string]any{"temp": 72},
			}
		},
	)

	callCount := 0
	var errorHooksFired int
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			switch callCount {
			case 1:
				if params.EventHandler != nil {
					params.EventHandler.OnFunctionCall(core.ToolCallDetail{ID: "tc-err", ToolName: "nonexistent"})
				}
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-err", ToolName: "nonexistent", Args: nil},
					},
				})
				return &msg, nil
			case 2:
				if params.EventHandler != nil {
					params.EventHandler.OnFunctionCall(core.ToolCallDetail{ID: "tc-ok", ToolName: "get_weather", Args: map[string]any{"city": "NYC"}})
				}
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-ok", ToolName: "get_weather", Args: map[string]any{"city": "NYC"}},
					},
				})
				return &msg, nil
			default:
				msg := core.NewAssistantMessage(core.TextContent{Text: "Weather: 72F"})
				msg.FinishReason = "stop"
				if params.EventHandler != nil {
					params.EventHandler.OnFinished("stop")
				}
				return &msg, nil
			}
		},
	}

	var functionCalls int
	agent := NewReactAgent(provider, false)
	agent.RegisterEventHooks(core.AgentEventHooks{
		OnFunctionCall: func(id constants.AgentIdentity, detail core.ToolCallDetail) {
			functionCalls++
		},
		OnError: func(id constants.AgentIdentity, err error) {
			errorHooksFired++
		},
	})

	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "weather?"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 5)
	assert.Equal(t, 3, callCount)
	assert.Equal(t, 2, functionCalls)
	assert.Equal(t, 0, errorHooksFired, "tool not found should not trigger OnError")
}
func TestReactAgent_StreamMode_FullCallbackChain(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			if params.EventHandler != nil {
				params.EventHandler.OnReplyChunk("Hel")
				params.EventHandler.OnReplyChunk("lo World")
				params.EventHandler.OnFinished("stop")
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "Hello World"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	handler := &testEventHandler{}
	agent := NewReactAgent(provider, true)
	agent.RegisterEventHooks(core.AgentEventHooks{
		OnReplyChunk:    func(id constants.AgentIdentity, chunk string) { handler.OnReplyChunk(chunk) },
		OnBlockReply:    func(id constants.AgentIdentity, text string) { handler.OnBlockReply(text) },
		OnModelFinished: func(id constants.AgentIdentity, reason string) { handler.OnFinished(reason) },
	})

	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "hi"})},
	)

	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, []string{"Hel", "lo World"}, handler.replyChunks)
	assert.Equal(t, []string{"stop"}, handler.finishReasons)
	assert.Empty(t, handler.thinkingChunks, "OnBlockReply should not fire in stream mode")
}

func TestReactAgent_UsageReportedPerCall(t *testing.T) {

	core.RegisterTool(
		core.ToolDefinition{Name: "tool_a", Description: "A"},
		func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{ToolCallID: detail.ID, ToolName: detail.ToolName, Outputs: map[string]any{"ok": true}}
		},
	)

	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{{ID: "t1", ToolName: "tool_a", Args: nil}},
				})
				msg.Usage = &core.Usage{TotalTokens: 10}
				return &msg, nil
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
			msg.FinishReason = "stop"
			msg.Usage = &core.Usage{TotalTokens: 20}
			return &msg, nil
		},
	}

	var usages []core.Usage
	agent := NewReactAgent(provider, false)
	agent.RegisterEventHooks(core.AgentEventHooks{
		OnUsageUpdated: func(id constants.AgentIdentity, usage core.Usage) {
			usages = append(usages, usage)
		},
	})

	_, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "go"})},
	)

	require.NoError(t, err)
	require.Len(t, usages, 2)
	assert.Equal(t, int64(10), usages[0].TotalTokens)
	assert.Equal(t, int64(20), usages[1].TotalTokens)
}

func TestReactAgent_Interrupt_DuringLoop(t *testing.T) {

	core.RegisterTool(
		core.ToolDefinition{Name: "slow_tool", Description: "Slow"},
		func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{ToolCallID: detail.ID, ToolName: detail.ToolName, Outputs: map[string]any{"ok": true}}
		},
	)

	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			msg := core.NewAssistantMessage(core.ToolCallContent{
				Details: []core.ToolCallDetail{{ID: "t1", ToolName: "slow_tool", Args: nil}},
			})
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	agent.SetOnToolResult(func(results []core.ToolCallResult, step int) bool {
		_ = agent.Interrupt(context.Background())
		return true
	})

	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "go"})},
	)

	require.NoError(t, err)
	// Interrupt fires during OnToolResult (after tool exec), so tool_call + tool_result
	// are both present. The loop exits at the top of the next iteration.
	require.Len(t, messages, 2)
	assert.Equal(t, constants.RoleAssistant, messages[0].Role)
	assert.Equal(t, constants.RoleTool, messages[1].Role)
}

func TestReactAgent_TerminatingTool_StopsLoop(t *testing.T) {

	core.RegisterTool(
		core.ToolDefinition{Name: "submit_answer", Description: "Submit final answer", Terminating: true},
		func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{
				ToolCallID: detail.ID, ToolName: detail.ToolName,
				Outputs: map[string]any{"answer": "42"},
			}
		},
	)

	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "submit_answer", Args: map[string]any{"value": "42"}},
					},
				})
				return &msg, nil
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "should not be called"})
			return &msg, nil
		},
	}

	agent := NewReactAgent(provider, false)
	messages, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "system"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "what is 6*7?"})},
	)

	require.NoError(t, err)
	// Terminating tool returns after tool exec; messages include tool_call + tool_result
	require.Len(t, messages, 2)
	assert.Equal(t, 1, callCount, "terminating tool should stop loop without second LLM call")
	assert.Equal(t, constants.RoleAssistant, messages[0].Role)
	assert.Equal(t, constants.RoleTool, messages[1].Role)
}
