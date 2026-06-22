package observability

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureObserver wraps a LangfuseObserver and records all events for inspection.
type captureObserver struct {
	*LangfuseObserver
	srv    *httptest.Server
	events []map[string]any
}

func newCaptureObserver(t *testing.T) *captureObserver {
	t.Helper()
	c := &captureObserver{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		json.Unmarshal(body, &payload)
		batch, _ := payload["batch"].([]any)
		for _, e := range batch {
			c.events = append(c.events, e.(map[string]any))
		}
		w.WriteHeader(200)
	}))
	c.LangfuseObserver = NewLangfuseObserver(LangfuseConfig{
		Host: c.srv.URL, PublicKey: "pk", SecretKey: "sk",
	})
	return c
}

func (c *captureObserver) Close() {
	c.srv.Close()
}

func (c *captureObserver) eventsByType(typ string) []map[string]any {
	var out []map[string]any
	for _, e := range c.events {
		if e["type"] == typ {
			out = append(out, e)
		}
	}
	return out
}

func (c *captureObserver) generationUsages() []map[string]any {
	var usages []map[string]any
	for _, e := range c.eventsByType("generation-create") {
		body, ok := e["body"].(map[string]any)
		if !ok {
			continue
		}
		if u, ok := body["usage"].(map[string]any); ok {
			usages = append(usages, u)
		}
	}
	return usages
}

func TestPlannerInstrument_LLMUsageFlowsIntoGeneration(t *testing.T) {
	// The planner's own LLM usage (via OnUsageUpdated) lands in the generation.
	c := newCaptureObserver(t)
	defer c.Close()

	_, trace := c.StartTrace(context.Background(), "session-1", "test goal")
	pi := NewPlannerInstrument(trace)
	pi.SetTrace(trace)

	handler := pi.EventHandler()
	handler.OnThinkingChunk("thinking...")
	handler.OnReplyChunk("reply")
	handler.OnUsageUpdated(core.Usage{InputTokens: 1000, OutputTokens: 200, TotalTokens: 1200})

	sp := pi.StepSpan(1, []core.ToolCallResult{})
	sp.End()

	trace.End()
	c.Flush(context.Background())

	usages := c.generationUsages()
	require.Len(t, usages, 1, "should have 1 generation with usage")
	u := usages[0]
	assert.Equal(t, float64(1000), u["promptTokens"])
	assert.Equal(t, float64(200), u["completionTokens"])
	assert.Equal(t, float64(1200), u["totalTokens"])
}

func TestPlannerInstrument_ToolSpansHaveInputOutput(t *testing.T) {
	c := newCaptureObserver(t)
	defer c.Close()

	_, trace := c.StartTrace(context.Background(), "session-2", "test goal")
	pi := NewPlannerInstrument(trace)
	pi.SetTrace(trace)

	handler := pi.EventHandler()
	handler.OnThinkingChunk("planning")
	handler.OnFunctionCall(core.ToolCallDetail{ID: "1", ToolName: "plan_create", Args: map[string]any{"name": "my-plan"}})
	handler.OnReplyChunk("done")
	handler.OnUsageUpdated(core.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150})

	sp := pi.StepSpan(1, []core.ToolCallResult{
		{ToolCallID: "1", ToolName: "plan_create", Outputs: map[string]any{"plan_id": "plan-abc"}},
	})
	sp.End()

	trace.End()
	c.Flush(context.Background())

	var toolSpan map[string]any
	for _, e := range c.eventsByType("span-create") {
		body, ok := e["body"].(map[string]any)
		if !ok {
			continue
		}
		if name, _ := body["name"].(string); name == "tool:plan_create" {
			toolSpan = body
			break
		}
	}
	require.NotNil(t, toolSpan, "should have tool:plan_create span")
	assert.NotNil(t, toolSpan["input"], "tool span should have input (LLM args)")
	assert.NotNil(t, toolSpan["output"], "tool span should have output (execution result)")
}

func TestPlannerInstrument_HierarchyStructure(t *testing.T) {
	// Verify the full hierarchy: react.step > generation + tool spans
	c := newCaptureObserver(t)
	defer c.Close()

	_, trace := c.StartTrace(context.Background(), "session-3", "test")
	pi := NewPlannerInstrument(trace)
	pi.SetTrace(trace)

	h := pi.EventHandler()
	h.OnThinkingChunk("think")
	h.OnFunctionCall(core.ToolCallDetail{ID: "1", ToolName: "tool_a", Args: map[string]any{"x": 1}})
	h.OnFunctionCall(core.ToolCallDetail{ID: "2", ToolName: "tool_b", Args: map[string]any{"y": 2}})
	h.OnReplyChunk("reply")
	h.OnUsageUpdated(core.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})

	sp := pi.StepSpan(1, []core.ToolCallResult{
		{ToolCallID: "1", ToolName: "tool_a", Outputs: map[string]any{"ok": true}},
		{ToolCallID: "2", ToolName: "tool_b", Outputs: map[string]any{"result": "done"}},
	})
	sp.End()

	trace.End()
	c.Flush(context.Background())

	// Build parent-child map
	spanIDs := map[string]string{} // spanID -> name
	parentMap := map[string]string{} // spanID -> parentID
	for _, e := range c.eventsByType("span-create") {
		body, _ := e["body"].(map[string]any)
		id, _ := body["id"].(string)
		name, _ := body["name"].(string)
		spanIDs[id] = name
		if p, ok := body["parentObservationId"].(string); ok {
			parentMap[id] = p
		}
	}

	// Find tool spans and verify they share the same parent (react.step)
	var stepParent string
	for id, name := range spanIDs {
		if name == "tool:tool_a" || name == "tool:tool_b" {
			if stepParent == "" {
				stepParent = parentMap[id]
			} else {
				assert.Equal(t, stepParent, parentMap[id],
					"tool spans should share the same parent react.step")
			}
		}
	}
	assert.NotEmpty(t, stepParent)

	// Verify the parent of tools is a react.step span
	parentName, ok := spanIDs[stepParent]
	assert.True(t, ok, "tool parent should exist in spans")
	assert.Equal(t, "react.step", parentName)
}
