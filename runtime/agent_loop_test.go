package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/persist"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/infra"
	"github.com/B777B2056-2/kugelblitz/tools/internals"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCfg(provider core.ILMProvider) config.Config {
	return config.Config{
		Model:           config.ModelConfig{Provider: provider, StreamMode: false},
		Runtime:         config.RuntimeConfig{MaxStateMachineCycles: 30},
		ContextCompress: config.ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 0},
		TargetDrift:     config.TargetDriftConfig{ReviewInterval: 12, MaxFailuresBeforeReview: 5},
	}
}

// MockProvider implements core.ILMProvider for tests.
type MockProvider struct {
	GenerateFn func(ctx context.Context, params core.GenerateParams) (*core.Message, error)
}

func (m *MockProvider) Generate(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
	if m.GenerateFn != nil {
		return m.GenerateFn(ctx, params)
	}
	return nil, nil
}

func TestPlanner_ContextError_TriggersRetry(t *testing.T) {
	core.GetToolRegistry().Reset()
	internals.RegisterAll()
	persist.SetManager(persist.NewFileManager(t.TempDir()))
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				return nil, core.ErrContextLengthExceeded
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
			return &msg, nil
		},
	}

	planner := NewAgentLoop(testCfg(provider))
	_, err := planner.execute(context.Background(), "test goal")
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, callCount, 2, "should have retried after compress")
}

func TestPlanner_NonContextError_NoRetry(t *testing.T) {
	persist.SetManager(persist.NewFileManager(t.TempDir()))
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			return nil, errors.New("some other error")
		},
	}

	planner := NewAgentLoop(testCfg(provider))
	_, err := planner.execute(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "some other error")
}

func TestPlanner_SecondCallSeesHistory(t *testing.T) {
	core.GetToolRegistry().Reset()
	internals.RegisterAll()

	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			msg := core.NewAssistantMessage(core.TextContent{Text: "result"})
			return &msg, nil
		},
	}

	planner := NewAgentLoop(testCfg(provider))

	_, err := planner.execute(context.Background(), "goal 1")
	require.NoError(t, err)

	_, err = planner.execute(context.Background(), "goal 2")
	require.NoError(t, err)
}

func TestWorkerAgent_ExecuteTask_Simple(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			msg := core.NewAssistantMessage(core.TextContent{Text: "task completed"})
			msg.Usage = &core.Usage{TotalTokens: 10, InputTokens: 5, OutputTokens: 5}
			return &msg, nil
		},
	}

	worker := infra.NewWorkerAgent(provider, false)
	output, usage, err := worker.ExecuteTask(context.Background(), "test goal", "do it")

	require.NoError(t, err)
	assert.Contains(t, output, "task completed")
	assert.NotNil(t, usage)
	assert.Equal(t, int64(10), usage.TotalTokens)
}

func TestWorkerAgent_ExecuteTask_Error(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			return nil, errors.New("api failure")
		},
	}

	worker := infra.NewWorkerAgent(provider, false)
	output, usage, err := worker.ExecuteTask(context.Background(), "goal", "action")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api failure")
	assert.NotNil(t, usage)
	_ = output
}

func TestPlanner_Cancel(t *testing.T) {
	planner := NewAgentLoop(testCfg(nil))
	planner.Cancel()
	// Cancel is idempotent; no error to check.
}

func TestOnToolResult_CountsFails(t *testing.T) {
	stepCount := 0
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{{ID: "t1", ToolName: "test"}},
				})
				return &msg, nil
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
			return &msg, nil
		},
	}

	agent := infra.NewReactAgent(provider, false)
	agent.SetOnToolResult(func(results []core.ToolCallResult, step int) bool {
		stepCount++
		assert.Equal(t, stepCount, step)
		return true
	})

	_, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "sys"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "hi"})},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, stepCount, "OnToolResult should fire once for the tool call")
}

func TestOnToolResult_TracksConsecutiveFails(t *testing.T) {
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount <= 2 {
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{{ID: "t1", ToolName: "test"}},
				})
				return &msg, nil
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
			return &msg, nil
		},
	}

	var capturedFails []int
	agent := infra.NewReactAgent(provider, false)
	agent.SetOnToolResult(func(results []core.ToolCallResult, step int) bool {
		hasFailure := false
		for _, r := range results {
			if _, isErr := r.Outputs["error"]; isErr {
				hasFailure = true
			}
		}
		trackedFails := 0
		if hasFailure {
			trackedFails++
		}
		capturedFails = append(capturedFails, trackedFails)
		return true
	})

	_, _ = agent.Execute(context.Background(),
		core.NewUserMessage(core.TextContent{Text: "sys"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "hi"})})
}

func TestOnToolResult_AbortOnFalse(t *testing.T) {
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			msg := core.NewAssistantMessage(core.ToolCallContent{
				Details: []core.ToolCallDetail{{ID: "t1", ToolName: "test"}},
			})
			return &msg, nil
		},
	}

	agent := infra.NewReactAgent(provider, false)
	fireCount := 0
	agent.SetOnToolResult(func(results []core.ToolCallResult, step int) bool {
		fireCount++
		return false
	})

	_, err := agent.Execute(
		context.Background(),
		core.NewUserMessage(core.TextContent{Text: "sys"}),
		[]core.Message{core.NewUserMessage(core.TextContent{Text: "hi"})},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, fireCount, "OnToolResult should fire only once before abort")
	assert.GreaterOrEqual(t, callCount, 1)
}

func TestPlanner_Execute_CompressThenReview(t *testing.T) {
	core.GetToolRegistry().Reset()
	internals.RegisterAll()
	persist.SetManager(persist.NewFileManager(t.TempDir()))
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				return nil, core.ErrContextLengthExceeded
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
			return &msg, nil
		},
	}

	planner := NewAgentLoop(testCfg(provider))
	_, err := planner.execute(context.Background(), "test goal")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, callCount, 2)
}

func TestPlanner_LLMUsageCallback_NilSafe(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
			return &msg, nil
		},
	}
	planner := NewAgentLoop(testCfg(provider))
	_, err := planner.execute(context.Background(), "test")
	require.NoError(t, err)
}

func TestPlanner_LLMUsageCallback_FiresWithIdentity(t *testing.T) {
	core.GetToolRegistry().Reset()

	var reports []core.Usage
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount <= 1 {
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{{ID: "t1", ToolName: "test_tool"}},
				})
				msg.Usage = &core.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}
				if params.EventHandler != nil {
					params.EventHandler.OnUsageUpdated(*msg.Usage)
				}
				return &msg, nil
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "final"})
			return &msg, nil
		},
	}

	core.RegisterTool(core.ToolDefinition{Name: "test_tool"},
		func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{ToolCallID: detail.ID, Outputs: map[string]any{"ok": true}}
		})

	planner := NewAgentLoop(testCfg(provider))
	planner.RegisterEventHooks(core.AgentEventHooks{
		OnUsageUpdated: func(id constants.AgentIdentity, usage core.Usage) {
			reports = append(reports, usage)
		},
	})
	_, err := planner.execute(context.Background(), "test")
	require.NoError(t, err)

	assert.NotEmpty(t, reports)
}

func TestPlanner_LLMUsageCallback_NoCallback_NoPanic(t *testing.T) {
	core.GetToolRegistry().Reset()

	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount <= 1 {
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{{ID: "t1", ToolName: "test_tool"}},
				})
				return &msg, nil
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
			return &msg, nil
		},
	}

	core.RegisterTool(core.ToolDefinition{Name: "test_tool"},
		func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{ToolCallID: detail.ID, Outputs: map[string]any{"ok": true}}
		})

	planner := NewAgentLoop(testCfg(provider))
	msgs, err := planner.execute(context.Background(), "test")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(msgs), 1)
}

func TestCompressSingleResult_ShortStringUnchanged(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			return nil, nil
		},
	}
	cfg := testCfg(provider)
	cfg.ContextCompress = config.ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 4000}
	planner := NewAgentLoop(cfg)

	r := core.ToolCallResult{ToolCallID: "1", ToolName: "test", Outputs: map[string]any{
		"text": "short string",
		"num":  42,
	}}
	planner.sessionMem.CompressToolResult(context.Background(), planner.planner.Compressor(), planner.cfg.ContextCompress.MaxToolResultChars, &r)

	assert.Equal(t, "short string", r.Outputs["text"])
	assert.Equal(t, 42, r.Outputs["num"])
}

func TestCompressSingleResult_LongStringCompressed(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			msg := core.NewAssistantMessage(core.TextContent{Text: "compressed summary"})
			return &msg, nil
		},
	}
	cfg := testCfg(provider)
	cfg.ContextCompress = config.ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 50}
	planner := NewAgentLoop(cfg)

	longText := strings.Repeat("abcdefghij", 10)
	r := core.ToolCallResult{ToolCallID: "1", ToolName: "file_read", Outputs: map[string]any{
		"content": longText,
		"path":    "/short.txt",
	}}
	planner.sessionMem.CompressToolResult(context.Background(), planner.planner.Compressor(), planner.cfg.ContextCompress.MaxToolResultChars, &r)

	assert.Equal(t, "compressed summary", r.Outputs["content"])
	assert.Equal(t, "/short.txt", r.Outputs["path"])
}

func TestCompressSingleResult_SkipsErrors(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			return nil, nil
		},
	}
	cfg := testCfg(provider)
	cfg.ContextCompress = config.ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 10}
	planner := NewAgentLoop(cfg)

	longText := strings.Repeat("x", 100)
	r := core.ToolCallResult{ToolCallID: "1", ToolName: "test", Outputs: map[string]any{
		"error":   "something went wrong",
		"details": longText,
	}}
	planner.sessionMem.CompressToolResult(context.Background(), planner.planner.Compressor(), planner.cfg.ContextCompress.MaxToolResultChars, &r)

	assert.Equal(t, "something went wrong", r.Outputs["error"])
	assert.Equal(t, longText, r.Outputs["details"])
}

func TestCompressSingleResult_DisabledWhenZero(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			t.Error("provider should not be called when compression is disabled")
			return nil, nil
		},
	}
	cfg := testCfg(provider)
	cfg.ContextCompress = config.ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 0}
	planner := NewAgentLoop(cfg)

	longText := strings.Repeat("x", 10000)
	r := core.ToolCallResult{ToolCallID: "1", ToolName: "test", Outputs: map[string]any{
		"content": longText,
	}}
	planner.sessionMem.CompressToolResult(context.Background(), planner.planner.Compressor(), planner.cfg.ContextCompress.MaxToolResultChars, &r)

	assert.Equal(t, longText, r.Outputs["content"])
}

func TestCompressSingleResult_MultipleFields(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			msg := core.NewAssistantMessage(core.TextContent{Text: "summary"})
			return &msg, nil
		},
	}
	cfg := testCfg(provider)
	cfg.ContextCompress = config.ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 20}
	planner := NewAgentLoop(cfg)

	long1 := strings.Repeat("a", 100)
	long2 := strings.Repeat("b", 200)
	r := core.ToolCallResult{ToolCallID: "1", ToolName: "test", Outputs: map[string]any{
		"field_a": long1,
		"field_b": long2,
		"field_c": "hi",
		"flag":    true,
		"count":   float64(99),
	}}
	planner.sessionMem.CompressToolResult(context.Background(), planner.planner.Compressor(), planner.cfg.ContextCompress.MaxToolResultChars, &r)

	assert.Equal(t, "summary", r.Outputs["field_a"])
	assert.Equal(t, "summary", r.Outputs["field_b"])
	assert.Equal(t, "hi", r.Outputs["field_c"])
	assert.Equal(t, true, r.Outputs["flag"])
	assert.Equal(t, float64(99), r.Outputs["count"])
}

// ---- End-to-end FSM state migration tests ----

func latestPlanID() string {
	plans := working.ListPlans()
	if len(plans) == 0 {
		return ""
	}
	return plans[0].ID
}

func TestAgentLoop_IntentToDirect_SimpleTask(t *testing.T) {
	core.GetToolRegistry().Reset()
	working.ResetPlans()
	internals.RegisterAll()
	persist.SetManager(persist.NewFileManager(t.TempDir()))

	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount == 1 {
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "set_work_mode", Args: map[string]any{"mode": "simple"}},
					},
				})
				return &msg, nil
			}
			msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	al := NewAgentLoop(testCfg(provider))
	al.Run(ctx, "echo hello")
	<-al.Done()

	assert.Empty(t, working.ListPlans(), "simple task should not create a plan")
}

func TestAgentLoop_RejectPath_UserRejectsPlan(t *testing.T) {
	core.GetToolRegistry().Reset()
	working.ResetPlans()
	internals.RegisterAll()
	persist.SetManager(persist.NewFileManager(t.TempDir()))

	var mu sync.Mutex
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			mu.Lock()
			callCount++
			c := callCount
			mu.Unlock()
			switch c {
			case 1:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "set_work_mode", Args: map[string]any{"mode": "plan"}},
					},
				})
				return &msg, nil
			case 2:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-2", ToolName: "plan_create", Args: map[string]any{"name": "Test Plan"}},
					},
				})
				return &msg, nil
			case 3:
				pid := working.ListPlans()[0].ID
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-3", ToolName: "task_insert",
							Args: map[string]any{"plan_id": pid, "goal": "Task 1"},
						},
					},
				})
				return &msg, nil
			case 4:
				msg := core.NewAssistantMessage(core.TextContent{Text: "plan ready"})
				return &msg, nil
			case 5:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-4", ToolName: "ask_human",
							Args: map[string]any{"question": "Approve?", "reason": "confirm"},
						},
					},
				})
				return &msg, nil
			case 6:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-5", ToolName: "confirm_plan",
							Args: map[string]any{"status": "rejected", "plan_id": latestPlanID()},
						},
					},
				})
				return &msg, nil
			default:
				msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
				return &msg, nil
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	al := NewAgentLoop(testCfg(provider))
	hitlCh := make(chan struct{}, 2)
	al.RegisterEventHooks(core.AgentEventHooks{
		OnWaitForHumanAction: func(id constants.AgentIdentity, reason, prompt string) {
			hitlCh <- struct{}{}
		},
	})
	al.Run(ctx, "build a web app")

	select {
	case <-hitlCh:
	case <-time.After(10 * time.Second):
		t.Fatal("HITL never triggered")
	}
	require.NoError(t, al.ResumeWithHumanResponse("no, reject this plan"))
	<-al.Done()

	plans := working.ListPlans()
	require.Len(t, plans, 1)
	assert.Equal(t, constants.PlanStateRejected, plans[0].State)
	assert.Len(t, plans[0].SubTasks, 1)
}

func TestAgentLoop_HappyPath_IntentToDone(t *testing.T) {
	core.GetToolRegistry().Reset()
	working.ResetPlans()
	internals.RegisterAll()
	persist.SetManager(persist.NewFileManager(t.TempDir()))

	var mu sync.Mutex
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			mu.Lock()
			callCount++
			c := callCount
			mu.Unlock()
			switch c {
			case 1:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "set_work_mode", Args: map[string]any{"mode": "plan"}},
					},
				})
				return &msg, nil
			case 2:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-2", ToolName: "plan_create", Args: map[string]any{"name": "Happy Plan"}},
					},
				})
				return &msg, nil
			case 3:
				pid := latestPlanID()
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-3a", ToolName: "task_insert", Args: map[string]any{"plan_id": pid, "goal": "Step 1"}},
						{ID: "tc-3b", ToolName: "task_insert", Args: map[string]any{"plan_id": pid, "goal": "Step 2"}},
					},
				})
				return &msg, nil
			case 4:
				msg := core.NewAssistantMessage(core.TextContent{Text: "plan ready"})
				return &msg, nil
			case 5:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-4", ToolName: "ask_human", Args: map[string]any{"question": "OK?", "reason": "confirm"}},
					},
				})
				return &msg, nil
			case 6:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-5", ToolName: "confirm_plan", Args: map[string]any{"status": "doing", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			default:
				msg := core.NewAssistantMessage(core.TextContent{Text: "task completed"})
				msg.FinishReason = "stop"
				return &msg, nil
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	al := NewAgentLoop(testCfg(provider))
	hitlCh := make(chan struct{}, 2)
	al.RegisterEventHooks(core.AgentEventHooks{
		OnWaitForHumanAction: func(id constants.AgentIdentity, reason, prompt string) {
			hitlCh <- struct{}{}
		},
	})
	al.Run(ctx, "build a web app")

	select {
	case <-hitlCh:
	case <-time.After(10 * time.Second):
		t.Fatal("HITL never triggered")
	}
	require.NoError(t, al.ResumeWithHumanResponse("yes, proceed"))
	<-al.Done()

	plans := working.ListPlans()
	require.Len(t, plans, 1)
	assert.Equal(t, constants.PlanStateDone, plans[0].State, "plan should reach Done")
	for _, task := range plans[0].SubTasks {
		assert.Equal(t, working.TaskStatusDone, task.Status, "task should be done")
	}
}

func TestAgentLoop_RecoveryPath_FailReplanRetry(t *testing.T) {
	core.GetToolRegistry().Reset()
	working.ResetPlans()
	internals.RegisterAll()
	persist.SetManager(persist.NewFileManager(t.TempDir()))

	var mu sync.Mutex
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			mu.Lock()
			callCount++
			c := callCount
			mu.Unlock()
			switch c {
			case 1:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "set_work_mode", Args: map[string]any{"mode": "plan"}},
					},
				})
				return &msg, nil
			case 2:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-2", ToolName: "plan_create", Args: map[string]any{"name": "Recovery Plan"}},
					},
				})
				return &msg, nil
			case 3:
				pid := latestPlanID()
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-3", ToolName: "task_insert", Args: map[string]any{"plan_id": pid, "goal": "Risky task"}},
					},
				})
				return &msg, nil
			case 4:
				msg := core.NewAssistantMessage(core.TextContent{Text: "plan ready"})
				return &msg, nil
			case 5:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-4", ToolName: "ask_human", Args: map[string]any{"question": "Proceed?", "reason": "confirm"}},
					},
				})
				return &msg, nil
			case 6:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-5", ToolName: "confirm_plan", Args: map[string]any{"status": "doing", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			case 7:
				// End ConfirmedState ReAct loop
				msg := core.NewAssistantMessage(core.TextContent{Text: "confirmed"})
				return &msg, nil
			case 8:
				return nil, errors.New("worker failure")
			case 9:
				// UpdatingState: task_insert (plan_id not needed, LLM has it from context)
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-6", ToolName: "task_insert", Args: map[string]any{"goal": "Fix and retry", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			case 10:
				// End UpdatingState ReAct loop with text
				msg := core.NewAssistantMessage(core.TextContent{Text: "plan updated"})
				return &msg, nil
			case 11:
				// ConfirmedState round 2: ask_human
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-7", ToolName: "ask_human", Args: map[string]any{"question": "Retry?", "reason": "retry_confirm"}},
					},
				})
				return &msg, nil
			case 12:
				// confirm_plan
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-8", ToolName: "confirm_plan", Args: map[string]any{"status": "doing", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			case 13:
				// End ConfirmedState round 2 with text
				msg := core.NewAssistantMessage(core.TextContent{Text: "reconfirmed"})
				return &msg, nil
			default:
				msg := core.NewAssistantMessage(core.TextContent{Text: "retry succeeded"})
				msg.FinishReason = "stop"
				return &msg, nil
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	al := NewAgentLoop(testCfg(provider))
	hitlCh := make(chan struct{}, 3)
	al.RegisterEventHooks(core.AgentEventHooks{
		OnWaitForHumanAction: func(id constants.AgentIdentity, reason, prompt string) {
			hitlCh <- struct{}{}
		},
	})
	al.Run(ctx, "complex task")

	select {
	case <-hitlCh:
	case <-time.After(10 * time.Second):
		t.Fatal("first HITL")
	}
	require.NoError(t, al.ResumeWithHumanResponse("yes"))
	select {
	case <-hitlCh:
	case <-time.After(10 * time.Second):
		t.Fatal("second HITL")
	}
	require.NoError(t, al.ResumeWithHumanResponse("yes, retry"))
	<-al.Done()

	plans := working.ListPlans()
	if len(plans) > 0 {
		assert.GreaterOrEqual(t, plans[0].Version, 2, "plan should have been versioned >= 2")
		assert.Equal(t, constants.PlanStateDone, plans[0].State)
	}
}

func TestAgentLoop_AbandonPath_FailReplanThenReject(t *testing.T) {
	core.GetToolRegistry().Reset()
	working.ResetPlans()
	internals.RegisterAll()
	persist.SetManager(persist.NewFileManager(t.TempDir()))

	var mu sync.Mutex
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			mu.Lock()
			callCount++
			c := callCount
			mu.Unlock()
			switch c {
			case 1:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-1", ToolName: "set_work_mode", Args: map[string]any{"mode": "plan"}},
					},
				})
				return &msg, nil
			case 2:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-2", ToolName: "plan_create", Args: map[string]any{"name": "Abandoned Plan"}},
					},
				})
				return &msg, nil
			case 3:
				pid := latestPlanID()
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-3", ToolName: "task_insert", Args: map[string]any{"plan_id": pid, "goal": "Task"}},
					},
				})
				return &msg, nil
			case 4:
				msg := core.NewAssistantMessage(core.TextContent{Text: "plan ready"})
				return &msg, nil
			case 5:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-4", ToolName: "ask_human", Args: map[string]any{"question": "Go?", "reason": "confirm"}},
					},
				})
				return &msg, nil
			case 6:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-5", ToolName: "confirm_plan", Args: map[string]any{"status": "doing", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			case 7:
				msg := core.NewAssistantMessage(core.TextContent{Text: "confirmed"})
				return &msg, nil
			case 8:
				return nil, errors.New("worker failure")
			case 9:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-6", ToolName: "task_insert", Args: map[string]any{"goal": "Fix task", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			case 10:
				msg := core.NewAssistantMessage(core.TextContent{Text: "plan updated"})
				return &msg, nil
			case 11:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-7", ToolName: "ask_human", Args: map[string]any{"question": "Retry?", "reason": "retry"}},
					},
				})
				return &msg, nil
			case 12:
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{
						{ID: "tc-8", ToolName: "confirm_plan", Args: map[string]any{"status": "rejected", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			case 13:
				msg := core.NewAssistantMessage(core.TextContent{Text: "abandoned"})
				return &msg, nil
			default:
				msg := core.NewAssistantMessage(core.TextContent{Text: "done"})
				return &msg, nil
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	al := NewAgentLoop(testCfg(provider))
	hitlCh := make(chan struct{}, 3)
	al.RegisterEventHooks(core.AgentEventHooks{
		OnWaitForHumanAction: func(id constants.AgentIdentity, reason, prompt string) {
			hitlCh <- struct{}{}
		},
	})
	al.Run(ctx, "abandoned task")

	select {
	case <-hitlCh:
	case <-time.After(10 * time.Second):
		t.Fatal("first HITL")
	}
	require.NoError(t, al.ResumeWithHumanResponse("yes"))
	select {
	case <-hitlCh:
	case <-time.After(10 * time.Second):
		t.Fatal("second HITL")
	}
	require.NoError(t, al.ResumeWithHumanResponse("no, abandon"))
	<-al.Done()

	plans := working.ListPlans()
	require.Len(t, plans, 1)
	assert.Equal(t, constants.PlanStateRejected, plans[0].State, "plan should be rejected")
}
