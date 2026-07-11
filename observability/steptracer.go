package observability

import (
	"context"
	"fmt"
	"strings"

	"github.com/B777B2056-2/kugelblitz/core"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// StepTracer collects LLM call data via ModelEventHandler callbacks.
// It builds a hierarchical trace using OTel spans:
//
//	Trace "planner: <goal>"
//	  ├── Span "react.step" #1
//	  │   ├── Span "step-1-llm" (generation kind=internal)
//	  │   ├── Span "tool:plan_create"
//	  │   └── Span "tool:task_insert"
//	  ├── Span "react.step" #2
//	  │   └── ...
type StepTracer struct {
	tracer          trace.Tracer
	currentStepSpan trace.Span
	stepNum         int

	thinkBuf     strings.Builder
	replyBuf     strings.Builder
	toolNames    []string
	pendingTools []core.ToolCallDetail
	usage        core.Usage
}

func NewStepTracer() *StepTracer {
	return &StepTracer{}
}

// SetTrace starts the first react.step span. Must be called before the ReAct loop.
// ctx goes first per Go convention; the returned context carries the span.
func (st *StepTracer) SetTrace(ctx context.Context, tracer trace.Tracer, goal string) (context.Context, trace.Span) {
	st.tracer = tracer
	st.stepNum = 1
	ctx, st.currentStepSpan = tracer.Start(ctx, "react.step",
		trace.WithAttributes(attribute.Int("step", 1), attribute.String("goal", goal)),
	)
	return ctx, st.currentStepSpan
}

func (st *StepTracer) EventHandler() core.ModelEventHandler {
	return &stepTracerHandler{st: st}
}

// LastUsage returns the accumulated LLM usage since the last StepSpan/reset.
func (st *StepTracer) LastUsage() core.Usage { return st.usage }

// StepSpan finalizes the completed step and prepares for the next.
func (st *StepTracer) StepSpan(ctx context.Context, step int, results []core.ToolCallResult) trace.Span {
	// 1. Create generation for the completed LLM call
	genName := fmt.Sprintf("step-%d-llm", step)
	_, gen := st.tracer.Start(ctx, genName, trace.WithSpanKind(trace.SpanKindInternal))
	st.applyGenAttrs(gen)
	gen.End()

	// 2. Create tool spans under the current step
	hasErr := false
	for i, r := range results {
		if _, isErr := r.Outputs["error"]; isErr {
			hasErr = true
		}
		opts := []trace.SpanStartOption{trace.WithAttributes(
			attribute.String("output", fmt.Sprint(r.Outputs)),
		)}
		if i < len(st.pendingTools) {
			opts = append(opts, trace.WithAttributes(
				attribute.String("input", fmt.Sprint(st.pendingTools[i].Args)),
			))
		}
		_, toolSpan := st.tracer.Start(ctx, "tool:"+r.ToolName, opts...)
		if _, isErr := r.Outputs["error"]; isErr {
			toolSpan.SetAttributes(attribute.String("status", "error"))
		}
		toolSpan.End()
	}

	st.reset()

	oldSpan := st.currentStepSpan
	if hasErr {
		oldSpan.SetAttributes(attribute.String("status", "error"))
	}
	oldSpan.End()

	st.stepNum++
	_, st.currentStepSpan = st.tracer.Start(ctx, "react.step",
		trace.WithAttributes(attribute.Int("step", st.stepNum)),
	)
	return oldSpan
}

// Flush creates the final generation and ends the current step span.
func (st *StepTracer) Flush() {
	if st.currentStepSpan == nil {
		return
	}
	if st.thinkBuf.Len() > 0 || st.replyBuf.Len() > 0 {
		genName := fmt.Sprintf("step-%d-llm", st.stepNum)
		// Use a background context since the caller's context may be cancelled
		_, gen := st.tracer.Start(context.Background(), genName, trace.WithSpanKind(trace.SpanKindInternal))
		st.applyGenAttrs(gen)
		gen.End()
	}
	st.currentStepSpan.End()
	st.reset()
}

func (st *StepTracer) applyGenAttrs(span trace.Span) {
	thinking := st.thinkBuf.String()
	reply := st.replyBuf.String()
	span.SetAttributes(
		attribute.String("input", thinking),
		attribute.String("output", reply),
		attribute.StringSlice("tool_calls", st.toolNames),
		attribute.Int64("tokens_in", st.usage.InputTokens),
		attribute.Int64("tokens_out", st.usage.OutputTokens),
		attribute.Int64("tokens_total", st.usage.TotalTokens),
	)
	if thinking != "" {
		span.SetAttributes(attribute.String("thinking", thinking))
	}
}

func (st *StepTracer) reset() {
	st.thinkBuf.Reset()
	st.replyBuf.Reset()
	st.toolNames = nil
	st.pendingTools = nil
	st.usage = core.Usage{}
}

// ---- ModelEventHandler ----

type stepTracerHandler struct {
	st *StepTracer
}

func (h *stepTracerHandler) OnThinkingChunk(chunk string)     { h.st.thinkBuf.WriteString(chunk) }
func (h *stepTracerHandler) OnReplyChunk(chunk string)        { h.st.replyBuf.WriteString(chunk) }
func (h *stepTracerHandler) OnBlockThinking(reasoning string) { h.st.thinkBuf.WriteString(reasoning) }
func (h *stepTracerHandler) OnBlockReply(text string)         { h.st.replyBuf.WriteString(text) }
func (h *stepTracerHandler) OnError(err error)                {}

func (h *stepTracerHandler) OnFunctionCall(detail core.ToolCallDetail) {
	h.st.toolNames = append(h.st.toolNames, detail.ToolName)
	if h.st.currentStepSpan != nil {
		h.st.pendingTools = append(h.st.pendingTools, detail)
	}
}

func (h *stepTracerHandler) OnFinished(reason string) {}
func (h *stepTracerHandler) OnUsageUpdated(usage core.Usage) {
	h.st.usage.InputTokens += usage.InputTokens
	h.st.usage.OutputTokens += usage.OutputTokens
	h.st.usage.TotalTokens += usage.TotalTokens
}
