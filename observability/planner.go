package observability

import (
	"fmt"
	"strings"

	"github.com/B777B2056-2/kugelblitz/core"
)

// PlannerInstrument collects LLM call data via ModelEventHandler callbacks.
// It creates a hierarchical trace structure:
//
//	Trace "planner: <goal>"
//	  ├── Span "react.step" #1
//	  │   ├── Generation "step-1-llm"
//	  │   ├── Span "tool:plan_create"
//	  │   └── Span "tool:task_insert"
//	  ├── Span "react.step" #2
//	  │   ├── Generation "step-2-llm"
//	  │   └── Span "tool:worker_spawn"
//	  └── Span "react.step" #3 (final)
//	      └── Generation "step-3-llm"
type PlannerInstrument struct {
	trace           core.TraceSpan
	currentStepSpan core.Span // the react.step span for the current step
	stepNum         int

	thinkBuf       strings.Builder
	replyBuf       strings.Builder
	toolNames      []string
	pendingTools   []core.ToolCallDetail // tool calls awaiting execution results
	usage          core.Usage
}

func NewPlannerInstrument(trace core.TraceSpan) *PlannerInstrument {
	pi := &PlannerInstrument{
		trace:           trace,
		currentStepSpan: nil,
		stepNum:         0,
	}
	return pi
}

// SetTrace wires the trace and creates the first react.step span.
// Must be called before the ReAct loop starts.
func (pi *PlannerInstrument) SetTrace(t core.TraceSpan) {
	pi.trace = t
	pi.stepNum = 1
	pi.currentStepSpan = t.StartSpan("react.step", map[string]any{"step": 1})
}

func (pi *PlannerInstrument) EventHandler() core.ModelEventHandler {
	return &plannerHandler{pi: pi}
}

// LastUsage returns the accumulated LLM usage since the last StepSpan/reset.
func (pi *PlannerInstrument) LastUsage() core.Usage { return pi.usage }

// StepSpan finalizes the completed step and prepares for the next:
//  1. Creates a generation for the LLM call data (under the current step span).
//  2. Returns the current step span so the caller can set attributes and end it.
//  3. Creates a new react.step span for the next iteration (tool calls from
//     the next LLM response will attach to it).
func (pi *PlannerInstrument) StepSpan(step int, results []core.ToolCallResult) core.Span {
	// 1. Build generation for the completed step
	genAttrs := pi.buildGenAttrs()
	genAttrs["name"] = fmt.Sprintf("step-%d-llm", step)
	gen := pi.currentStepSpan.StartGeneration(genAttrs)
	gen.End()

	// 2. Create tool spans now (after generation) so they appear after it in Langfuse.
	//    Each tool span gets both the LLM's args (input) and the execution result (output).
	hasErr := false
	for i, r := range results {
		if _, isErr := r.Outputs["error"]; isErr {
			hasErr = true
		}
		attrs := map[string]any{"output": r.Outputs}
		if i < len(pi.pendingTools) {
			attrs["input"] = pi.pendingTools[i].Args
		}
		sp := pi.currentStepSpan.StartSpan("tool:"+r.ToolName, attrs)
		if _, isErr := r.Outputs["error"]; isErr {
			sp.SetAttributes(map[string]any{"status": "error"})
		}
		sp.End()
	}

	// 3. Reset accumulators for the next step
	pi.reset()

	// 4. Return current step span (caller will End it)
	//    and create the next step span for upcoming LLM call
	oldSpan := pi.currentStepSpan
	if hasErr {
		oldSpan.SetAttributes(map[string]any{"status": "error"})
	}

	pi.stepNum++
	pi.currentStepSpan = pi.trace.StartSpan("react.step", map[string]any{"step": pi.stepNum})
	return oldSpan
}

// Flush creates the final generation and ends the current step span.
// Called when the ReAct loop exits (no more tool calls).
func (pi *PlannerInstrument) Flush() {
	if pi.currentStepSpan == nil {
		return
	}
	// Only create a generation if there's data to flush
	if pi.thinkBuf.Len() > 0 || pi.replyBuf.Len() > 0 {
		genAttrs := pi.buildGenAttrs()
		genAttrs["name"] = fmt.Sprintf("step-%d-llm", pi.stepNum)
		gen := pi.currentStepSpan.StartGeneration(genAttrs)
		gen.End()
	}
	pi.currentStepSpan.End()
	pi.reset()
}

func (pi *PlannerInstrument) buildGenAttrs() map[string]any {
	thinking := pi.thinkBuf.String()
	reply := pi.replyBuf.String()
	attrs := map[string]any{
		"input":        thinking,
		"output":       reply,
		"tool_calls":   pi.toolNames,
		"tokens_in":    pi.usage.InputTokens,
		"tokens_out":   pi.usage.OutputTokens,
		"tokens_total": pi.usage.TotalTokens,
	}
	if thinking != "" {
		attrs["thinking"] = thinking
	}
	return attrs
}

func (pi *PlannerInstrument) reset() {
	pi.thinkBuf.Reset()
	pi.replyBuf.Reset()
	pi.toolNames = nil
	pi.pendingTools = nil
	pi.usage = core.Usage{}
}

// ---- ModelEventHandler ----

type plannerHandler struct {
	pi *PlannerInstrument
}

func (h *plannerHandler) OnThinkingChunk(chunk string) { h.pi.thinkBuf.WriteString(chunk) }
func (h *plannerHandler) OnReplyChunk(chunk string)    { h.pi.replyBuf.WriteString(chunk) }
func (h *plannerHandler) OnError(err error)            {}

func (h *plannerHandler) OnFunctionCall(detail core.ToolCallDetail) {
	h.pi.toolNames = append(h.pi.toolNames, detail.ToolName)
	// Defer tool span creation to StepSpan — store the call detail so we can
	// create the span after the generation (correct order in Langfuse).
	if h.pi.currentStepSpan != nil {
		h.pi.pendingTools = append(h.pi.pendingTools, detail)
	}
}

func (h *plannerHandler) OnFinished(reason string) {}
func (h *plannerHandler) OnUsageUpdated(usage core.Usage) {
	h.pi.usage.InputTokens += usage.InputTokens
	h.pi.usage.OutputTokens += usage.OutputTokens
	h.pi.usage.TotalTokens += usage.TotalTokens
}
