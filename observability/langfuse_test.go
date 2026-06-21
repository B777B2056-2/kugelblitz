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
		Host:      "http://localhost:3000",
		PublicKey: "pk-test",
		SecretKey: "sk-test",
	})

	_, span := obs.StartTrace(context.Background(), "test-trace", "verify deployment")
	require.NotNil(t, span)

	child := span.StartSpan("react.step", map[string]any{"step": 1})
	child.SetAttributes(map[string]any{"tokens": 42})
	child.End()

	span.SetAttributes(map[string]any{"total_steps": 3})
	span.End()
}

func TestLangfuseObserver_NoopWhenDisabled(t *testing.T) {
	obs := NewLangfuseObserver(LangfuseConfig{})
	_, span := obs.StartTrace(context.Background(), "t", "")
	assert.NotNil(t, span)

	child := span.StartSpan("test", nil)
	child.End()
	span.End()
	// should not panic or send
}

func TestLangfuseObserver_BatchFormat(t *testing.T) {
	obs := NewLangfuseObserver(LangfuseConfig{
		Host: "http://localhost:3000", PublicKey: "pk", SecretKey: "sk",
	})

	_, span := obs.StartTrace(context.Background(), "trace-name", "test goal")
	child := span.StartSpan("tool:plan_create", map[string]any{"plan_id": "p1"})
	child.SetAttributes(map[string]any{"status": "init"})
	child.End()
	span.End()

	obs.mu.Lock()
	batch := obs.batch
	obs.mu.Unlock()

	assert.Greater(t, len(batch), 0)

	// Verify trace-create event
	assert.Equal(t, "trace-create", batch[0].Type)
	assert.NotEmpty(t, batch[0].ID)
	assert.Contains(t, string(batch[0].Body), "trace-name")

	// Verify span-create event
	assert.Equal(t, "span-create", batch[1].Type)
	assert.Contains(t, string(batch[1].Body), "plan_create")

	// Verify observation-update event
	assert.Equal(t, "observation-update", batch[2].Type)
}

func TestLangfuseObserver_HTTPPayload(t *testing.T) {
	// Mock Langfuse server to verify payload format
	received := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/public/ingestion", r.URL.Path)
		assert.Equal(t, "Basic cGs6c2s=", r.Header.Get("Authorization")) // pk:sk
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.WriteHeader(200)
	}))
	defer srv.Close()

	obs := NewLangfuseObserver(LangfuseConfig{
		Host: srv.URL, PublicKey: "pk", SecretKey: "sk",
	})

	_, span := obs.StartTrace(context.Background(), "planner-execute", "deploy app")
	child := span.StartSpan("react.step", map[string]any{"step": 1, "tokens": 42})
	child.End()
	span.End()
	obs.Flush(context.Background())

	var payload map[string]any
	require.NoError(t, json.Unmarshal(<-received, &payload))

	batch := payload["batch"].([]any)
	assert.GreaterOrEqual(t, len(batch), 2)

	// Verify trace-create
	t0 := batch[0].(map[string]any)
	assert.Equal(t, "trace-create", t0["type"])
	assert.NotEmpty(t, t0["id"])
	body0 := t0["body"].(map[string]any)
	assert.Equal(t, "planner-execute", body0["name"])

	// Verify span-create
	t1 := batch[1].(map[string]any)
	assert.Equal(t, "span-create", t1["type"])
	body1 := t1["body"].(map[string]any)
	assert.Equal(t, "react.step", body1["name"])
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

	ctx, span := obs.StartTrace(context.Background(), "planner-execute", "deploy app")
	gen := span.StartSpan("react.step", map[string]any{
		"step":   1,
		"tokens_in":  int64(100),
		"tokens_out": int64(50),
	})
	gen.End()
	span.End()

	obs.Flush(ctx)
}
