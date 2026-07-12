package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setup(t *testing.T) (*tracetest.SpanRecorder, *StepTracer) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)

	tracer := otel.Tracer("kugelblitz")
	// Mimic agent_loop.go: create a root span so Flush gen spans have a valid
	// parent instead of becoming orphan root spans.
	ctx, rootSpan := tracer.Start(context.Background(), "planner-root")
	defer rootSpan.End() // End after all assertions in sub-tests

	pi := NewStepTracer()
	pi.SetTrace(ctx, tracer, "test goal")
	return sr, pi
}

func TestStepTracer_LLMUsage(t *testing.T) {
	sr, pi := setup(t)

	h := pi.EventHandler()
	h.OnThinkingChunk("thinking...")
	h.OnReplyChunk("reply")
	h.OnUsageUpdated(core.Usage{InputTokens: 1000, OutputTokens: 200, TotalTokens: 1200})

	pi.StepSpan(context.Background(), 1, []core.ToolCallResult{})
	pi.Flush()

	spans := sr.Ended()
	var gen sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "step-1-llm" {
			gen = s
			break
		}
	}
	require.NotNil(t, gen, "should have step-1-llm generation")
	// Verify usage ended up in the span attributes
	assert.NotEmpty(t, gen.Attributes())
}

func TestStepTracer_ToolSpans(t *testing.T) {
	sr, pi := setup(t)

	h := pi.EventHandler()
	h.OnThinkingChunk("planning")
	h.OnFunctionCall(core.ToolCallDetail{ID: "1", ToolName: "plan_create", Args: map[string]any{"name": "my-plan"}})
	h.OnReplyChunk("done")
	h.OnUsageUpdated(core.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150})

	pi.StepSpan(context.Background(), 1, []core.ToolCallResult{
		{ToolCallID: "1", ToolName: "plan_create", Outputs: map[string]any{"plan_id": "plan-abc"}},
	})
	pi.Flush()

	spans := sr.Ended()
	var toolSpan sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "tool:plan_create" {
			toolSpan = s
			break
		}
	}
	require.NotNil(t, toolSpan, "should have tool:plan_create span")
	assert.NotEmpty(t, toolSpan.Attributes(), "tool span should have attributes")
}

func TestStepTracer_MultipleSteps(t *testing.T) {
	sr, pi := setup(t)

	h := pi.EventHandler()

	// Step 1
	h.OnThinkingChunk("step1 think")
	h.OnFunctionCall(core.ToolCallDetail{ID: "1", ToolName: "file_read", Args: map[string]any{"path": "a.go"}})
	h.OnReplyChunk("step1 reply")
	h.OnUsageUpdated(core.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
	pi.StepSpan(context.Background(), 1, []core.ToolCallResult{
		{ToolCallID: "1", ToolName: "file_read", Outputs: map[string]any{"content": "hello"}},
	})

	// Step 2
	h.OnThinkingChunk("step2 think")
	h.OnReplyChunk("step2 reply")
	h.OnUsageUpdated(core.Usage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30})
	pi.StepSpan(context.Background(), 2, []core.ToolCallResult{})

	pi.Flush()

	spans := sr.Ended()
	stepCount := 0
	genCount := 0
	for _, s := range spans {
		if s.Name() == "react.step" {
			stepCount++
		}
		if s.Name() == "step-1-llm" || s.Name() == "step-2-llm" {
			genCount++
		}
	}
	assert.Equal(t, 3, stepCount) // SetTrace(1) + StepSpan(1→2) + StepSpan(2→3)
	assert.Equal(t, 2, genCount)
}

func TestStepTracer_NoOpWhenNoData(t *testing.T) {
	sr, pi := setup(t)
	pi.Flush()

	spans := sr.Ended()
	genCount := 0
	for _, s := range spans {
		if s.Name() == "step-1-llm" {
			genCount++
		}
	}
	assert.Equal(t, 0, genCount)
}

func TestStepTracer_OnErrorRecordsOnStepSpan(t *testing.T) {
	sr, pi := setup(t)

	h := pi.EventHandler()
	h.OnThinkingChunk("thinking...")
	h.OnError(errors.New("LLM rate limit exceeded"))
	h.OnReplyChunk("partial reply")
	h.OnUsageUpdated(core.Usage{InputTokens: 50, OutputTokens: 10, TotalTokens: 60})

	pi.StepSpan(context.Background(), 1, []core.ToolCallResult{})
	pi.Flush()

	spans := sr.Ended()

	// Find the react.step span for step 1 and verify it has error status
	var stepSpan sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "react.step" {
			attrs := s.Attributes()
			for _, a := range attrs {
				if a.Key == "step" && a.Value.AsInt64() == 1 {
					stepSpan = s
					break
				}
			}
		}
	}
	require.NotNil(t, stepSpan, "should have react.step span for step 1")

	// Check that the span has error status
	assert.Equal(t, codes.Error, stepSpan.Status().Code,
		"step span should have error status after OnError")
	assert.Contains(t, stepSpan.Status().Description, "LLM rate limit exceeded")

	// Check that error events were recorded
	foundErr := false
	for _, evt := range stepSpan.Events() {
		if evt.Name == "exception" {
			foundErr = true
			break
		}
	}
	assert.True(t, foundErr, "step span should have exception event from RecordError")
}

func TestStepTracer_ToolErrorSetStatus(t *testing.T) {
	sr, pi := setup(t)

	h := pi.EventHandler()
	h.OnThinkingChunk("calling tool...")
	h.OnFunctionCall(core.ToolCallDetail{ID: "1", ToolName: "shell_exec", Args: map[string]any{"command": "rm -rf /"}})
	h.OnReplyChunk("tool failed")
	h.OnUsageUpdated(core.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})

	pi.StepSpan(context.Background(), 1, []core.ToolCallResult{
		{ToolCallID: "1", ToolName: "shell_exec", Outputs: map[string]any{"error": "permission denied"}},
	})
	pi.Flush()

	spans := sr.Ended()

	var toolSpan sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "tool:shell_exec" {
			toolSpan = s
			break
		}
	}
	require.NotNil(t, toolSpan, "should have tool:shell_exec span")

	// Tool span should have error status via SetStatus + RecordError
	assert.Equal(t, codes.Error, toolSpan.Status().Code,
		"tool span should have error status code")
	assert.Contains(t, toolSpan.Status().Description, "permission denied")

	// Should also have the custom status attribute for backward compat
	foundStatusAttr := false
	for _, a := range toolSpan.Attributes() {
		if a.Key == "status" && a.Value.AsString() == "error" {
			foundStatusAttr = true
		}
	}
	assert.True(t, foundStatusAttr, "tool span should have status=error attribute")

	// Should have an exception event
	foundExc := false
	for _, evt := range toolSpan.Events() {
		if evt.Name == "exception" {
			foundExc = true
			break
		}
	}
	assert.True(t, foundExc, "tool span should have exception event from RecordError")
}

func TestStepTracer_FlushGenSpanHasParent(t *testing.T) {
	sr, pi := setup(t)

	h := pi.EventHandler()
	h.OnThinkingChunk("final thinking...")
	h.OnReplyChunk("final answer")
	h.OnUsageUpdated(core.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150})

	// Simulate the ReAct loop ending without a StepSpan call (no tool calls).
	// In real usage this happens when the LLM returns text with no tool_calls.
	pi.Flush()

	spans := sr.Ended()

	// Find the gen span created by Flush
	var genSpan sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "step-1-llm" {
			genSpan = s
			break
		}
	}
	require.NotNil(t, genSpan, "Flush should create step-1-llm gen span")

	// The gen span should have a parent (not be a root span).
	// It should hang under the root trace span (planner-level), not be orphaned.
	assert.True(t, genSpan.Parent().IsValid(),
		"Flush gen span should have a valid parent — not be an orphan root span")
}

func TestStepTracer_LastErrClearedAfterStepSpan(t *testing.T) {
	sr, pi := setup(t)

	h := pi.EventHandler()

	// Step 1: has an error
	h.OnThinkingChunk("step 1")
	h.OnError(errors.New("first error"))
	h.OnReplyChunk("reply")
	h.OnUsageUpdated(core.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
	pi.StepSpan(context.Background(), 1, []core.ToolCallResult{})

	// Step 2: no error — should be clean
	h.OnThinkingChunk("step 2 clean")
	h.OnReplyChunk("clean reply")
	h.OnUsageUpdated(core.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
	pi.StepSpan(context.Background(), 2, []core.ToolCallResult{})

	pi.Flush()

	spans := sr.Ended()

	// Find step 2 span — should be clean (no error)
	for _, s := range spans {
		if s.Name() == "react.step" {
			hasStep2 := false
			for _, a := range s.Attributes() {
				if a.Key == "step" && a.Value.AsInt64() == 2 {
					hasStep2 = true
					break
				}
			}
			if hasStep2 {
				assert.Equal(t, codes.Unset, s.Status().Code,
					"step 2 span should have no error after lastErr was cleared")
				assert.Empty(t, s.Events(), "step 2 span should have no events")
			}
		}
	}
}
