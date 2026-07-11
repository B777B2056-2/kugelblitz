package observability

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setup(t *testing.T) (*tracetest.SpanRecorder, *StepTracer) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)

	tracer := otel.Tracer("kugelblitz")
	pi := NewStepTracer()
	pi.SetTrace(context.Background(), tracer, "test goal")
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
