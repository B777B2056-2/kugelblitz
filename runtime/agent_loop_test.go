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
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/infra"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCfg(provider coretypes.ILMProvider) config.Config {
	return config.Config{
		Model:           config.ModelConfig{Provider: provider, StreamMode: false},
		Runtime:         config.RuntimeConfig{MaxStateMachineCycles: 30},
		ContextCompress: config.ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 0},
		TargetDrift:     config.TargetDriftConfig{ReviewInterval: 12, MaxFailuresBeforeReview: 5},
	}
}

// MockProvider implements coretypes.ILMProvider for tests.
type MockProvider struct {
	GenerateFn func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error)
}

func (m *MockProvider) Generate(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
	if m.GenerateFn != nil {
		return m.GenerateFn(ctx, params)
	}
	return nil, nil
}

// mustNewAgentLoop constructs an AgentLoop, failing the test if initialization
// (e.g. long-term memory) fails.
func mustNewAgentLoop(t *testing.T, cfg config.Config, opts ...AgentLoopOption) *AgentLoop {
	t.Helper()
	var (
		loop *AgentLoop
		err  error
	)
	loop, err = NewAgentLoop(cfg, opts...)
	require.NoError(t, err)
	return loop
}

func TestAgentLoop_AutoDreamDisabled_NoScheduler(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	working.ResetPlans()
	cfg := testCfg(&MockProvider{})
	cfg.AutoDream = config.AutoDreamConfig{} // zero value → disabled
	loop := mustNewAgentLoop(t, cfg)
	assert.Nil(t, loop.dreamScheduler, "auto dream disabled must not build a scheduler")
}

func TestAgentLoop_AutoDreamEnabled_BuildsScheduler(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	working.ResetPlans()
	cfg := testCfg(&MockProvider{})
	cfg.AutoDream = config.AutoDreamConfig{Enabled: true}
	loop := mustNewAgentLoop(t, cfg)
	assert.NotNil(t, loop.dreamScheduler, "auto dream enabled must build a scheduler")
}

func TestDreamInterval_DefaultOnZero(t *testing.T) {
	assert.Equal(t, 30*time.Minute, dreamInterval(0, 30*time.Minute))
	assert.Equal(t, time.Second, dreamInterval(1, 30*time.Minute))
}

func TestPlanner_ContextError_TriggersRetry(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	working.ResetPlans()
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			callCount++
			if callCount == 1 {
				return nil, coretypes.ErrContextLengthExceeded
			}
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})
			return &msg, nil
		},
	}

	planner := mustNewAgentLoop(t, testCfg(provider))
	_, err := planner.execute(context.Background(), coretypes.AgentInput{Text: "test goal"})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, callCount, 2, "should have retried after compress")
}

func TestPlanner_NonContextError_NoRetry(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	working.ResetPlans()
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			return nil, errors.New("some other error")
		},
	}

	planner := mustNewAgentLoop(t, testCfg(provider))
	_, err := planner.execute(context.Background(), coretypes.AgentInput{Text: "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "some other error")
}

func TestPlanner_SecondCallSeesHistory(t *testing.T) {

	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "result"})
			return &msg, nil
		},
	}

	planner := mustNewAgentLoop(t, testCfg(provider))

	_, err := planner.execute(context.Background(), coretypes.AgentInput{Text: "goal 1"})
	require.NoError(t, err)

	_, err = planner.execute(context.Background(), coretypes.AgentInput{Text: "goal 2"})
	require.NoError(t, err)
}

func TestWorkerAgent_ExecuteTask_Simple(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "task completed"})
			msg.Usage = &coretypes.Usage{TotalTokens: 10, InputTokens: 5, OutputTokens: 5}
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
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
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
	planner := mustNewAgentLoop(t, testCfg(nil))
	planner.Cancel()
	// Cancel is idempotent; no error to check.
}

func TestOnToolResult_CountsFails(t *testing.T) {
	stepCount := 0
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			callCount++
			if callCount == 1 {
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{{ID: "t1", ToolName: "test"}},
				})
				return &msg, nil
			}
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})
			return &msg, nil
		},
	}

	agent := infra.NewReactAgent(provider, false)
	agent.SetOnToolResult(func(results []coretypes.ToolCallResult, step int) bool {
		stepCount++
		assert.Equal(t, stepCount, step)
		return true
	})

	_, err := agent.Execute(
		context.Background(),
		coretypes.NewUserMessage(coretypes.TextContent{Text: "sys"}),
		[]coretypes.Message{coretypes.NewUserMessage(coretypes.TextContent{Text: "hi"})},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, stepCount, "OnToolResult should fire once for the tool call")
}

func TestOnToolResult_TracksConsecutiveFails(t *testing.T) {
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			callCount++
			if callCount <= 2 {
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{{ID: "t1", ToolName: "test"}},
				})
				return &msg, nil
			}
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})
			return &msg, nil
		},
	}

	var capturedFails []int
	agent := infra.NewReactAgent(provider, false)
	agent.SetOnToolResult(func(results []coretypes.ToolCallResult, step int) bool {
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
		coretypes.NewUserMessage(coretypes.TextContent{Text: "sys"}),
		[]coretypes.Message{coretypes.NewUserMessage(coretypes.TextContent{Text: "hi"})})
}

func TestOnToolResult_AbortOnFalse(t *testing.T) {
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			callCount++
			msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
				Details: []coretypes.ToolCallDetail{{ID: "t1", ToolName: "test"}},
			})
			return &msg, nil
		},
	}

	agent := infra.NewReactAgent(provider, false)
	fireCount := 0
	agent.SetOnToolResult(func(results []coretypes.ToolCallResult, step int) bool {
		fireCount++
		return false
	})

	_, err := agent.Execute(
		context.Background(),
		coretypes.NewUserMessage(coretypes.TextContent{Text: "sys"}),
		[]coretypes.Message{coretypes.NewUserMessage(coretypes.TextContent{Text: "hi"})},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, fireCount, "OnToolResult should fire only once before abort")
	assert.GreaterOrEqual(t, callCount, 1)
}

func TestPlanner_Execute_CompressThenReview(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	working.ResetPlans()
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			callCount++
			if callCount == 1 {
				return nil, coretypes.ErrContextLengthExceeded
			}
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})
			return &msg, nil
		},
	}

	planner := mustNewAgentLoop(t, testCfg(provider))
	_, err := planner.execute(context.Background(), coretypes.AgentInput{Text: "test goal"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, callCount, 2)
}

func TestPlanner_LLMUsageCallback_NilSafe(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})
			return &msg, nil
		},
	}
	planner := mustNewAgentLoop(t, testCfg(provider))
	_, err := planner.execute(context.Background(), coretypes.AgentInput{Text: "test"})
	require.NoError(t, err)
}

func TestPlanner_LLMUsageCallback_FiresWithIdentity(t *testing.T) {

	var reports []coretypes.Usage
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			callCount++
			if callCount <= 1 {
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{{ID: "t1", ToolName: "test_tool"}},
				})
				msg.Usage = &coretypes.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}
				if params.EventHandler != nil {
					params.EventHandler.OnUsageUpdated(*msg.Usage)
				}
				return &msg, nil
			}
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "final"})
			return &msg, nil
		},
	}

	core.RegisterTool(coretypes.ToolDefinition{Name: "test_tool"},
		func(ctx context.Context, detail coretypes.ToolCallDetail) coretypes.ToolCallResult {
			return coretypes.ToolCallResult{ToolCallID: detail.ID, Outputs: map[string]any{"ok": true}}
		})

	planner := mustNewAgentLoop(t, testCfg(provider))
	planner.RegisterEventHooks(core.AgentEventHooks{
		OnUsageUpdated: func(id constants.AgentIdentity, usage coretypes.Usage) {
			reports = append(reports, usage)
		},
	})
	_, err := planner.execute(context.Background(), coretypes.AgentInput{Text: "test"})
	require.NoError(t, err)

	assert.NotEmpty(t, reports)
}

func TestPlanner_LLMUsageCallback_NoCallback_NoPanic(t *testing.T) {

	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			callCount++
			if callCount <= 1 {
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{{ID: "t1", ToolName: "test_tool"}},
				})
				return &msg, nil
			}
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})
			return &msg, nil
		},
	}

	core.RegisterTool(coretypes.ToolDefinition{Name: "test_tool"},
		func(ctx context.Context, detail coretypes.ToolCallDetail) coretypes.ToolCallResult {
			return coretypes.ToolCallResult{ToolCallID: detail.ID, Outputs: map[string]any{"ok": true}}
		})

	planner := mustNewAgentLoop(t, testCfg(provider))
	msgs, err := planner.execute(context.Background(), coretypes.AgentInput{Text: "test"})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(msgs), 1)
}

func TestCompressSingleResult_ShortStringUnchanged(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			return nil, nil
		},
	}
	cfg := testCfg(provider)
	cfg.ContextCompress = config.ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 4000}
	planner := mustNewAgentLoop(t, cfg)

	r := coretypes.ToolCallResult{ToolCallID: "1", ToolName: "test", Outputs: map[string]any{
		"text": "short string",
		"num":  42,
	}}
	planner.sessionMem.CompressToolResult(context.Background(), planner.planner.Compressor(), planner.cfg.ContextCompress.MaxToolResultChars, &r)

	assert.Equal(t, "short string", r.Outputs["text"])
	assert.Equal(t, 42, r.Outputs["num"])
}

func TestCompressSingleResult_LongStringCompressed(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "compressed summary"})
			return &msg, nil
		},
	}
	cfg := testCfg(provider)
	cfg.ContextCompress = config.ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 50}
	planner := mustNewAgentLoop(t, cfg)

	longText := strings.Repeat("abcdefghij", 10)
	r := coretypes.ToolCallResult{ToolCallID: "1", ToolName: "file_read", Outputs: map[string]any{
		"content": longText,
		"path":    "/short.txt",
	}}
	planner.sessionMem.CompressToolResult(context.Background(), planner.planner.Compressor(), planner.cfg.ContextCompress.MaxToolResultChars, &r)

	assert.Equal(t, "compressed summary", r.Outputs["content"])
	assert.Equal(t, "/short.txt", r.Outputs["path"])
}

func TestCompressSingleResult_SkipsErrors(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			return nil, nil
		},
	}
	cfg := testCfg(provider)
	cfg.ContextCompress = config.ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 10}
	planner := mustNewAgentLoop(t, cfg)

	longText := strings.Repeat("x", 100)
	r := coretypes.ToolCallResult{ToolCallID: "1", ToolName: "test", Outputs: map[string]any{
		"error":   "something went wrong",
		"details": longText,
	}}
	planner.sessionMem.CompressToolResult(context.Background(), planner.planner.Compressor(), planner.cfg.ContextCompress.MaxToolResultChars, &r)

	assert.Equal(t, "something went wrong", r.Outputs["error"])
	assert.Equal(t, longText, r.Outputs["details"])
}

func TestCompressSingleResult_DisabledWhenZero(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			t.Error("provider should not be called when compression is disabled")
			return nil, nil
		},
	}
	cfg := testCfg(provider)
	cfg.ContextCompress = config.ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 0}
	planner := mustNewAgentLoop(t, cfg)

	longText := strings.Repeat("x", 10000)
	r := coretypes.ToolCallResult{ToolCallID: "1", ToolName: "test", Outputs: map[string]any{
		"content": longText,
	}}
	planner.sessionMem.CompressToolResult(context.Background(), planner.planner.Compressor(), planner.cfg.ContextCompress.MaxToolResultChars, &r)

	assert.Equal(t, longText, r.Outputs["content"])
}

func TestCompressSingleResult_MultipleFields(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "summary"})
			return &msg, nil
		},
	}
	cfg := testCfg(provider)
	cfg.ContextCompress = config.ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 20}
	planner := mustNewAgentLoop(t, cfg)

	long1 := strings.Repeat("a", 100)
	long2 := strings.Repeat("b", 200)
	r := coretypes.ToolCallResult{ToolCallID: "1", ToolName: "test", Outputs: map[string]any{
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
	core.GetWorkspace().SetDir(t.TempDir())
	working.ResetPlans()

	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			callCount++
			if callCount == 1 {
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-1", ToolName: "set_work_mode", Args: map[string]any{"mode": "simple"}},
					},
				})
				return &msg, nil
			}
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	al := mustNewAgentLoop(t, testCfg(provider))
	al.Run(ctx, coretypes.AgentInput{Text: "echo hello"})
	<-al.Done()

	assert.Empty(t, working.ListPlans(), "simple task should not create a plan")
}

// toolNames extracts the tool definition names offered in a Generate call.
func toolNames(params coretypes.GenerateParams) []string {
	var names []string
	for _, td := range params.Tools {
		names = append(names, td.Name)
	}
	return names
}

func TestAgentLoop_ForceModeSimple_SkipsIntent(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	working.ResetPlans()

	var firstTools []string
	var firstSystemPrompt string
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			if firstTools == nil {
				firstTools = toolNames(params)
				if len(params.Messages) > 0 {
					if tc, ok := params.Messages[0].Content.(coretypes.TextContent); ok {
						firstSystemPrompt = tc.Text
					}
				}
			}
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	cfg := testCfg(provider)
	cfg.Runtime.ForceMode = "simple"
	al := mustNewAgentLoop(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	al.Run(ctx, coretypes.AgentInput{Text: "echo hello"})
	<-al.Done()

	assert.Empty(t, working.ListPlans(), "force_mode=simple should not create a plan")
	assert.NotContains(t, firstSystemPrompt, "intent recognition", "intent prompt should be skipped")
	assert.Contains(t, firstTools, "shell_exec", "direct mode should expose execution tools")
	assert.NotContains(t, firstTools, "set_work_mode", "set_work_mode should not be offered when intent is skipped")
}

func TestAgentLoop_ForceModePlan_SkipsIntent(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	working.ResetPlans()

	var firstTools []string
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			if firstTools == nil {
				firstTools = toolNames(params)
			}
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}

	cfg := testCfg(provider)
	cfg.Runtime.ForceMode = "plan"
	al := mustNewAgentLoop(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	al.Run(ctx, coretypes.AgentInput{Text: "build a web app"})
	<-al.Done()

	assert.Contains(t, firstTools, "plan_create", "plan mode should offer plan_create directly")
	assert.NotContains(t, firstTools, "set_work_mode", "set_work_mode should not be offered when intent is skipped")
}

func TestAgentLoop_RejectPath_UserRejectsPlan(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	working.ResetPlans()

	var mu sync.Mutex
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			mu.Lock()
			callCount++
			c := callCount
			mu.Unlock()
			switch c {
			case 1:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-1", ToolName: "set_work_mode", Args: map[string]any{"mode": "plan"}},
					},
				})
				return &msg, nil
			case 2:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-2", ToolName: "plan_create", Args: map[string]any{"name": "Test Plan"}},
					},
				})
				return &msg, nil
			case 3:
				pid := working.ListPlans()[0].ID
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-3", ToolName: "task_insert",
							Args: map[string]any{"plan_id": pid, "goal": "Task 1"},
						},
					},
				})
				return &msg, nil
			case 4:
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "plan ready"})
				return &msg, nil
			case 5:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-4", ToolName: "ask_human",
							Args: map[string]any{"question": "Approve?", "reason": "confirm"},
						},
					},
				})
				return &msg, nil
			case 6:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-5", ToolName: "confirm_plan",
							Args: map[string]any{"status": "rejected", "plan_id": latestPlanID()},
						},
					},
				})
				return &msg, nil
			default:
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})
				return &msg, nil
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	al := mustNewAgentLoop(t, testCfg(provider))
	hitlCh := make(chan struct{}, 2)
	al.RegisterEventHooks(core.AgentEventHooks{
		OnWaitForHumanAction: func(id constants.AgentIdentity, reason, prompt string) {
			hitlCh <- struct{}{}
		},
	})
	al.Run(ctx, coretypes.AgentInput{Text: "build a web app"})

	select {
	case <-hitlCh:
	case <-time.After(10 * time.Second):
		t.Fatal("HITL never triggered")
	}
	require.NoError(t, al.ResumeWithHumanResponse("no, reject this plan"))
	<-al.Done()

	plans := working.ListPlans()
	require.NotEmpty(t, plans)
	p := plans[len(plans)-1]
	assert.Equal(t, constants.PlanStateRejected, p.State)
	assert.Len(t, p.SubTasks, 1)
}

func TestAgentLoop_HappyPath_IntentToDone(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	working.ResetPlans()

	var mu sync.Mutex
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			mu.Lock()
			callCount++
			c := callCount
			mu.Unlock()
			switch c {
			case 1:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-1", ToolName: "set_work_mode", Args: map[string]any{"mode": "plan"}},
					},
				})
				return &msg, nil
			case 2:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-2", ToolName: "plan_create", Args: map[string]any{"name": "Happy Plan"}},
					},
				})
				return &msg, nil
			case 3:
				pid := latestPlanID()
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-3a", ToolName: "task_insert", Args: map[string]any{"plan_id": pid, "goal": "Step 1"}},
						{ID: "tc-3b", ToolName: "task_insert", Args: map[string]any{"plan_id": pid, "goal": "Step 2"}},
					},
				})
				return &msg, nil
			case 4:
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "plan ready"})
				return &msg, nil
			case 5:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-4", ToolName: "ask_human", Args: map[string]any{"question": "OK?", "reason": "confirm"}},
					},
				})
				return &msg, nil
			case 6:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-5", ToolName: "confirm_plan", Args: map[string]any{"status": "doing", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			default:
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "task completed"})
				msg.FinishReason = "stop"
				return &msg, nil
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	al := mustNewAgentLoop(t, testCfg(provider))
	hitlCh := make(chan struct{}, 2)
	al.RegisterEventHooks(core.AgentEventHooks{
		OnWaitForHumanAction: func(id constants.AgentIdentity, reason, prompt string) {
			hitlCh <- struct{}{}
		},
	})
	al.Run(ctx, coretypes.AgentInput{Text: "build a web app"})

	select {
	case <-hitlCh:
	case <-time.After(10 * time.Second):
		t.Fatal("HITL never triggered")
	}
	require.NoError(t, al.ResumeWithHumanResponse("yes, proceed"))
	<-al.Done()

	plans := working.ListPlans()
	require.NotEmpty(t, plans)
	// Find our plan (last created)
	p := plans[len(plans)-1]
	assert.Equal(t, constants.PlanStateDone, p.State, "plan should reach Done")
	for _, task := range p.SubTasks {
		assert.Equal(t, working.TaskStatusDone, task.Status, "task should be done")
	}
}

func TestAgentLoop_RecoveryPath_FailReplanRetry(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())
	working.ResetPlans()

	var mu sync.Mutex
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			mu.Lock()
			callCount++
			c := callCount
			mu.Unlock()
			switch c {
			case 1:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-1", ToolName: "set_work_mode", Args: map[string]any{"mode": "plan"}},
					},
				})
				return &msg, nil
			case 2:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-2", ToolName: "plan_create", Args: map[string]any{"name": "Recovery Plan"}},
					},
				})
				return &msg, nil
			case 3:
				pid := latestPlanID()
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-3", ToolName: "task_insert", Args: map[string]any{"plan_id": pid, "goal": "Risky task"}},
					},
				})
				return &msg, nil
			case 4:
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "plan ready"})
				return &msg, nil
			case 5:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-4", ToolName: "ask_human", Args: map[string]any{"question": "Proceed?", "reason": "confirm"}},
					},
				})
				return &msg, nil
			case 6:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-5", ToolName: "confirm_plan", Args: map[string]any{"status": "doing", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			case 7:
				// End ConfirmedState ReAct loop
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "confirmed"})
				return &msg, nil
			case 8:
				return nil, errors.New("worker failure")
			case 9:
				// UpdatingState: task_insert (plan_id not needed, LLM has it from context)
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-6", ToolName: "task_insert", Args: map[string]any{"goal": "Fix and retry", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			case 10:
				// End UpdatingState ReAct loop with text
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "plan updated"})
				return &msg, nil
			case 11:
				// ConfirmedState round 2: ask_human
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-7", ToolName: "ask_human", Args: map[string]any{"question": "Retry?", "reason": "retry_confirm"}},
					},
				})
				return &msg, nil
			case 12:
				// confirm_plan
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-8", ToolName: "confirm_plan", Args: map[string]any{"status": "doing", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			case 13:
				// End ConfirmedState round 2 with text
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "reconfirmed"})
				return &msg, nil
			default:
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "retry succeeded"})
				msg.FinishReason = "stop"
				return &msg, nil
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	al := mustNewAgentLoop(t, testCfg(provider))
	hitlCh := make(chan struct{}, 3)
	al.RegisterEventHooks(core.AgentEventHooks{
		OnWaitForHumanAction: func(id constants.AgentIdentity, reason, prompt string) {
			hitlCh <- struct{}{}
		},
	})
	al.Run(ctx, coretypes.AgentInput{Text: "complex task"})

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
	core.GetWorkspace().SetDir(t.TempDir())
	working.ResetPlans()

	var mu sync.Mutex
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			mu.Lock()
			callCount++
			c := callCount
			mu.Unlock()
			switch c {
			case 1:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-1", ToolName: "set_work_mode", Args: map[string]any{"mode": "plan"}},
					},
				})
				return &msg, nil
			case 2:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-2", ToolName: "plan_create", Args: map[string]any{"name": "Abandoned Plan"}},
					},
				})
				return &msg, nil
			case 3:
				pid := latestPlanID()
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-3", ToolName: "task_insert", Args: map[string]any{"plan_id": pid, "goal": "Task"}},
					},
				})
				return &msg, nil
			case 4:
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "plan ready"})
				return &msg, nil
			case 5:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-4", ToolName: "ask_human", Args: map[string]any{"question": "Go?", "reason": "confirm"}},
					},
				})
				return &msg, nil
			case 6:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-5", ToolName: "confirm_plan", Args: map[string]any{"status": "doing", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			case 7:
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "confirmed"})
				return &msg, nil
			case 8:
				return nil, errors.New("worker failure")
			case 9:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-6", ToolName: "task_insert", Args: map[string]any{"goal": "Fix task", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			case 10:
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "plan updated"})
				return &msg, nil
			case 11:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-7", ToolName: "ask_human", Args: map[string]any{"question": "Retry?", "reason": "retry"}},
					},
				})
				return &msg, nil
			case 12:
				msg := coretypes.NewAssistantMessage(coretypes.ToolCallContent{
					Details: []coretypes.ToolCallDetail{
						{ID: "tc-8", ToolName: "confirm_plan", Args: map[string]any{"status": "rejected", "plan_id": latestPlanID()}},
					},
				})
				return &msg, nil
			case 13:
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "abandoned"})
				return &msg, nil
			default:
				msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})
				return &msg, nil
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	al := mustNewAgentLoop(t, testCfg(provider))
	hitlCh := make(chan struct{}, 3)
	al.RegisterEventHooks(core.AgentEventHooks{
		OnWaitForHumanAction: func(id constants.AgentIdentity, reason, prompt string) {
			hitlCh <- struct{}{}
		},
	})
	al.Run(ctx, coretypes.AgentInput{Text: "abandoned task"})

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
	require.NotEmpty(t, plans)
	p := plans[len(plans)-1]
	assert.Equal(t, constants.PlanStateRejected, p.State, "plan should be rejected")
}
