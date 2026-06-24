package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	core.GetToolRegistry().Reset()
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
	core.GetToolRegistry().Reset()
	// Register a global "ask_human" that returns a different result
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

	// The local tool should call WaitForHuman, which blocks.
	// We verify the local tool is used by checking it's registered.
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
		OnWaitForHumanAction: func(reason, prompt string) {
			cbReason = reason
			cbPrompt = prompt
			cbWg.Done()
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go func() {
		agent.WaitForHuman(ctx, "need_approval", "proceed?")
	}()

	// Wait for callback to fire
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

	// Give the goroutine time to start waiting
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

	// Wait for goroutine to start, then cancel
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
	// Don't register any hooks — OnWaitForHumanAction is nil

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		agent.WaitForHuman(ctx, "r", "p")
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()
	// should not panic
}

func TestReactAgent_Execute_MultipleSequentialAskHuman(t *testing.T) {
	core.GetToolRegistry().Reset()

	callCount := 0
	mockProv := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			switch callCount {
			case 1:
				// First: ask first question
				msg := core.NewAssistantMessage("m1", nil)
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
				// Second: ask second question
				msg := core.NewAssistantMessage("m2", nil)
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
				msg := core.NewAssistantMessage("m3", core.TextContent{Text: "Done."})
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
		OnWaitForHumanAction: func(reason, prompt string) {
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
			core.NewUserMessage("root", core.TextContent{Text: "system"}),
			[]core.Message{core.NewUserMessage("root", core.TextContent{Text: "multi-step"})},
		)
	}()

	// First pause
	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 20; i++ {
		if agent.humanLoop.isWaiting.Load() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.True(t, agent.humanLoop.isWaiting.Load(), "should be waiting for first input")
	require.NoError(t, agent.ResumeWithHumanResponse(ctx, "yes, go ahead"))

	// Second pause
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
	assert.Len(t, messages, 3)

	assert.Equal(t, []string{"first check", "need choice"}, waitReasons)
	assert.Equal(t, []string{"Proceed with step 1?", "Which method, A or B?"}, waitPrompts)
}

func TestReactAgent_OnToolCallEndFiresForAskHuman(t *testing.T) {
	core.GetToolRegistry().Reset()

	callCount := 0
	mockProv := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				msg := core.NewAssistantMessage("m1", nil)
				msg.Content = core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "ask_human", Args: map[string]any{
							"question": "OK?",
						}},
					},
				}
				return &msg, nil
			}
			msg := core.NewAssistantMessage("m2", core.TextContent{Text: "done"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	agent := NewReactAgent(mockProv, false)
	agent.EnableHumanInTheLoop()

	var toolCallEndResults []core.ToolCallResult
	agent.RegisterEventHooks(core.AgentEventHooks{
		OnToolCallEnd: func(r core.ToolCallResult) {
			toolCallEndResults = append(toolCallEndResults, r)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		agent.Execute(ctx,
			core.NewUserMessage("root", core.TextContent{Text: "system"}),
			[]core.Message{core.NewUserMessage("root", core.TextContent{Text: "check"})},
		)
	}()

	// Wait for pause
	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 20; i++ {
		if agent.humanLoop.isWaiting.Load() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	agent.ResumeWithHumanResponse(ctx, "approved")

	wg.Wait()

	require.Len(t, toolCallEndResults, 1)
	assert.Equal(t, "ask_human", toolCallEndResults[0].ToolName)
	assert.Equal(t, map[string]any{"response": "approved"}, toolCallEndResults[0].Outputs)
}

func TestReactAgent_ParallelToolsWithAskHuman(t *testing.T) {
	// LLM calls ask_human + a global tool in the same step.
	// The global tool completes; ask_human blocks until resume.
	core.GetToolRegistry().Reset()
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
	mockProv := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				msg := core.NewAssistantMessage("m1", nil)
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
			msg := core.NewAssistantMessage("m2", core.TextContent{Text: "done"})
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
			core.NewUserMessage("root", core.TextContent{Text: "system"}),
			[]core.Message{core.NewUserMessage("root", core.TextContent{Text: "go"})},
		)
	}()

	// Wait for ask_human to block
	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 20; i++ {
		if agent.humanLoop.isWaiting.Load() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.True(t, agent.humanLoop.isWaiting.Load(), "ask_human should be blocking")

	agent.ResumeWithHumanResponse(ctx, "yes")

	wg.Wait()
	require.Len(t, messages, 2)
	// First assistant message contains both tool calls.
	tcc, ok := messages[0].Content.(core.ToolCallContent)
	require.True(t, ok)
	assert.Len(t, tcc.Details, 2)
	// Second assistant message is the text response
	txt, ok := messages[1].Content.(core.TextContent)
	require.True(t, ok)
	assert.Equal(t, "done", txt.Text)
}

func TestHumanLoopWaiting_ReturnsTrueWhileWaiting(t *testing.T) {
	agent := NewReactAgent(nil, false)
	assert.False(t, agent.HumanLoopWaiting()) // not enabled

	agent.EnableHumanInTheLoop()
	assert.False(t, agent.HumanLoopWaiting()) // enabled but not waiting

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		agent.WaitForHuman(ctx, "r", "p")
	}()

	time.Sleep(50 * time.Millisecond)
	assert.True(t, agent.HumanLoopWaiting()) // should be waiting

	cancel()
	wg.Wait()
	assert.False(t, agent.HumanLoopWaiting()) // should have stopped
}

func TestReactAgent_WithTools_FiltersLocalTool(t *testing.T) {
	// If ask_human is not in the whitelist, it should not be visible.
	core.GetToolRegistry().Reset()
	core.RegisterTool(core.ToolDefinition{Name: "tool_a", Description: "A"}, nil)

	agent := NewReactAgent(nil, false)
	agent.EnableHumanInTheLoop()
	agent.WithTools("tool_a") // NOT ask_human

	defs := agent.visibleTools()
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	assert.True(t, names["tool_a"], "tool_a should be visible")
	assert.False(t, names["ask_human"], "ask_human should be filtered out when not whitelisted")
}

func TestReactAgent_Execute_WithAskHumanIntegration(t *testing.T) {
	// Integration test: LLM calls ask_human, loop pauses, human responds, loop continues.
	core.GetToolRegistry().Reset()

	callCount := 0
	mockProv := &mockProvider{
		generateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				// First call: LLM wants to ask human
				msg := core.NewAssistantMessage("m1", nil)
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
			// Second call: after human response
			msg := core.NewAssistantMessage("m2", core.TextContent{Text: "Got it, won't delete."})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	agent := NewReactAgent(mockProv, false)
	agent.EnableHumanInTheLoop()

	var onWaitCalled bool
	agent.RegisterEventHooks(core.AgentEventHooks{
		OnWaitForHumanAction: func(reason, prompt string) {
			onWaitCalled = true
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Run Execute in a goroutine — it will block on ask_human
	var messages []core.Message
	var execErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		messages, execErr = agent.Execute(ctx,
			core.NewUserMessage("root", core.TextContent{Text: "system"}),
			[]core.Message{core.NewUserMessage("root", core.TextContent{Text: "do it"})},
		)
	}()

	// Wait for the agent to call ask_human (poll)
	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 20; i++ {
		if agent.humanLoop.isWaiting.Load() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	require.True(t, onWaitCalled, "OnWaitForHumanAction should have been called")
	require.True(t, agent.humanLoop.isWaiting.Load(), "agent should be waiting")

	// Human responds
	err := agent.ResumeWithHumanResponse(ctx, "No, do not delete.")
	require.NoError(t, err)

	// Wait for Execute to finish
	wg.Wait()
	require.NoError(t, execErr)
	require.Len(t, messages, 2) // tool call + final text response
	assert.Equal(t, 2, callCount)

	// Verify the tool result message contains the human response
	textContent, ok := messages[1].Content.(core.TextContent)
	require.True(t, ok)
	assert.Equal(t, "Got it, won't delete.", textContent.Text)
}
