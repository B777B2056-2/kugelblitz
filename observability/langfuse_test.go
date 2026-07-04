package observability

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func langfuseEnabled() bool {
	return os.Getenv("LANGFUSE_HOST") != "" || os.Getenv("LANGFUSE_PUBLIC_KEY") != ""
}

func TestLangfuseObserver_StartTrace(t *testing.T) {
	obs := NewLangfuseObserver(LangfuseConfig{
		Host: "http://localhost:3000", PublicKey: "pk-test", SecretKey: "sk-test",
	})
	_, span := obs.StartTrace(context.Background(), "test-trace", "verify")
	require.NotNil(t, span)
	child := span.StartSpan("react.step", map[string]any{"step": 1})
	child.End()
	span.End()
}

func TestLangfuseObserver_NoopWhenDisabled(t *testing.T) {
	obs := NewLangfuseObserver(LangfuseConfig{})
	_, span := obs.StartTrace(context.Background(), "t", "")
	child := span.StartSpan("test", nil)
	child.End()
	span.End()
}

func TestLangfuseObserver_BatchFormat(t *testing.T) {
	obs := NewLangfuseObserver(LangfuseConfig{
		Host: "http://localhost:3000", PublicKey: "pk", SecretKey: "sk",
	})
	_, span := obs.StartTrace(context.Background(), "trace-name", "test goal")
	child := span.StartSpan("tool:plan_create", map[string]any{"plan_id": "p1"})
	child.End()
	span.End()

	obs.mu.Lock()
	batch := obs.batch
	obs.mu.Unlock()

	assert.Greater(t, len(batch), 0)

	// First event should be trace-create
	assert.Equal(t, "trace-create", batch[0].Type)
	assert.NotEmpty(t, batch[0].ID)
	assert.Contains(t, string(batch[0].Body), "trace-name")

	// Find the span-create event and verify parentObservationId
	var foundSpan bool
	for _, e := range batch {
		if e.Type == "span-create" {
			foundSpan = true
			var body map[string]any
			_ = json.Unmarshal(e.Body, &body)
			assert.NotEmpty(t, body["parentObservationId"], "span should have parentObservationId")
			assert.NotEmpty(t, body["startTime"], "span should have startTime")
			break
		}
	}
	assert.True(t, foundSpan, "should contain a span-create event")

	// Verify End() sends an update event with endTime
	var foundEnd bool
	for _, e := range batch {
		if e.Type == "span-update" {
			var body map[string]any
			_ = json.Unmarshal(e.Body, &body)
			if _, ok := body["endTime"]; ok {
				foundEnd = true
				break
			}
		}
	}
	assert.True(t, foundEnd, "should contain a span-update event with endTime")
}

func TestLangfuseObserver_HTTPPayload(t *testing.T) {
	var allEvents []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		for _, e := range payload["batch"].([]any) {
			allEvents = append(allEvents, e.(map[string]any))
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	obs := NewLangfuseObserver(LangfuseConfig{
		Host: srv.URL, PublicKey: "pk", SecretKey: "sk",
	})

	_, span := obs.StartTrace(context.Background(), "planner: deploy app", "deploy app")
	child := span.StartSpan("react.step", map[string]any{"step": 1})
	child.End()
	span.End()
	_ = obs.Flush(context.Background())

	require.GreaterOrEqual(t, len(allEvents), 3) // trace-create, span-create, span-update, trace-update

	// Verify trace-create
	var traceCreate map[string]any
	for _, e := range allEvents {
		if e["type"] == "trace-create" {
			traceCreate = e
			break
		}
	}
	require.NotNil(t, traceCreate, "should have trace-create event")
	body := traceCreate["body"].(map[string]any)
	assert.Equal(t, "planner: deploy app", body["name"])
	assert.NotEmpty(t, body["startTime"])

	// Verify span-create has parentObservationId
	var spanCreateBody map[string]any
	for _, e := range allEvents {
		if e["type"] == "span-create" {
			b, _ := json.Marshal(e["body"])
			_ = json.Unmarshal(b, &spanCreateBody)
			break
		}
	}
	require.NotNil(t, spanCreateBody, "should have span-create event")
	assert.NotEmpty(t, spanCreateBody["parentObservationId"])

	// Verify update event has endTime
	var foundEndTime bool
	for _, e := range allEvents {
		if e["type"] == "span-update" {
			var b map[string]any
			raw, _ := json.Marshal(e["body"])
			_ = json.Unmarshal(raw, &b)
			if _, ok := b["endTime"]; ok {
				foundEndTime = true
				break
			}
		}
	}
	assert.True(t, foundEndTime, "span-update should contain endTime")
}

func TestLangfuseObserver_NestedHierarchy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	obs := NewLangfuseObserver(LangfuseConfig{
		Host: srv.URL, PublicKey: "pk", SecretKey: "sk",
	})

	_, trace := obs.StartTrace(context.Background(), "planner: test", "test goal")

	// Simulate step 1: create step span, then a generation and tool calls under it
	step1 := trace.StartSpan("react.step", map[string]any{"step": 1})
	gen1 := step1.StartGeneration(map[string]any{
		"name": "step-1-llm", "output": "thinking...",
	})
	gen1.End()
	tool1 := step1.StartSpan("tool:plan_create", map[string]any{"input": "{}"})
	tool1.End()
	step1.End()

	// Simulate step 2
	step2 := trace.StartSpan("react.step", map[string]any{"step": 2})
	gen2 := step2.StartGeneration(map[string]any{
		"name": "step-2-llm", "output": "done",
	})
	gen2.End()
	step2.End()

	trace.End()

	// Collect events
	obs.mu.Lock()
	batch := obs.batch
	obs.mu.Unlock()

	// Build a map of id -> body for analysis
	eventsByType := make(map[string][]map[string]any)
	for _, e := range batch {
		var body map[string]any
		_ = json.Unmarshal(e.Body, &body)
		eventsByType[e.Type] = append(eventsByType[e.Type], body)
	}

	// Verify generations have parentObservationId pointing to their step span
	genCreates := eventsByType["generation-create"]
	assert.Len(t, genCreates, 2, "should have 2 generation-create events")
	for _, gen := range genCreates {
		assert.NotEmpty(t, gen["parentObservationId"], "generation should have parentObservationId")
		assert.Contains(t, gen["parentObservationId"].(string), "span-",
			"generation's parent should be a span")
	}
}

func TestLangfuseObserver_EndSendsUpdate(t *testing.T) {
	obs := NewLangfuseObserver(LangfuseConfig{
		Host: "http://localhost:3000", PublicKey: "pk", SecretKey: "sk",
	})

	_, span := obs.StartTrace(context.Background(), "test", "goal")
	span.End() // trace.End() should queue a trace-update with endTime

	obs.mu.Lock()
	batch := obs.batch
	obs.mu.Unlock()

	var foundTraceEnd bool
	for _, e := range batch {
		if e.Type == "trace-update" {
			var body map[string]any
			_ = json.Unmarshal(e.Body, &body)
			if _, ok := body["endTime"]; ok {
				foundTraceEnd = true
			}
		}
	}
	assert.True(t, foundTraceEnd, "trace End() should send trace-update with endTime")
}

func TestLangfuseObserver_SetAttributesUsesCorrectType(t *testing.T) {
	obs := NewLangfuseObserver(LangfuseConfig{
		Host: "http://localhost:3000", PublicKey: "pk", SecretKey: "sk",
	})

	_, trace := obs.StartTrace(context.Background(), "test", "goal")
	trace.SetAttributes(map[string]any{"output": "done"})

	child := trace.StartSpan("react.step", map[string]any{"step": 1})
	child.SetAttributes(map[string]any{"status": "error"})

	gen := child.StartGeneration(map[string]any{"name": "test-gen"})
	gen.SetAttributes(map[string]any{"output": "result"})

	obs.mu.Lock()
	batch := obs.batch
	obs.mu.Unlock()

	hasTraceUpdate := false
	hasSpanUpdate := false
	hasGenUpdate := false
	for _, e := range batch {
		switch e.Type {
		case "trace-update":
			hasTraceUpdate = true
		case "span-update":
			hasSpanUpdate = true
		case "generation-update":
			hasGenUpdate = true
		}
	}
	assert.True(t, hasTraceUpdate, "trace SetAttributes should use trace-update")
	assert.True(t, hasSpanUpdate, "span SetAttributes should use span-update")
	assert.True(t, hasGenUpdate, "generation SetAttributes should use generation-update")
}

func TestLangfuseObserver_RecordError(t *testing.T) {
	obs := NewLangfuseObserver(LangfuseConfig{
		Host: "http://localhost:3000", PublicKey: "pk", SecretKey: "sk",
	})

	_, trace := obs.StartTrace(context.Background(), "test", "goal")
	child := trace.StartSpan("react.step", nil)
	child.RecordError(assert.AnError)

	obs.mu.Lock()
	batch := obs.batch
	defer obs.mu.Unlock()

	var foundErrorUpdate bool
	for _, e := range batch {
		if e.Type == "span-update" {
			var body map[string]any
			_ = json.Unmarshal(e.Body, &body)
			if msg, ok := body["statusMessage"]; ok && msg == assert.AnError.Error() {
				foundErrorUpdate = true
			}
		}
	}
	assert.True(t, foundErrorUpdate, "RecordError should send span-update with statusMessage")
}

func TestLangfuseObserver_Generations(t *testing.T) {
	if !langfuseEnabled() {
		t.Skip("Langfuse not configured")
	}
	obs := NewLangfuseObserver(LangfuseConfig{
		Host:      os.Getenv("LANGFUSE_HOST"),
		PublicKey: os.Getenv("LANGFUSE_PUBLIC_KEY"),
		SecretKey: os.Getenv("LANGFUSE_SECRET_KEY"),
	})
	_, span := obs.StartTrace(context.Background(), "planner: deploy app", "deploy app")
	gen := span.StartGeneration(map[string]any{"name": "test-gen", "output": "test"})
	gen.End()
	span.End()
	_ = obs.Flush(context.Background())
}
