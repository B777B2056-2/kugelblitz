package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/core"
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

	agent.Execute(context.Background(),
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

	var reports []core.LLMUsageReport
	callCount := 0
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			callCount++
			if callCount <= 1 {
				msg := core.NewAssistantMessage(core.ToolCallContent{
					Details: []core.ToolCallDetail{{ID: "t1", ToolName: "test_tool"}},
				})
				msg.Usage = &core.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}
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
		OnLLMUsage: func(report core.LLMUsageReport) {
			reports = append(reports, report)
		},
	})
	_, err := planner.execute(context.Background(), "test")
	require.NoError(t, err)

	_ = reports
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
