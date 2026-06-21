package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"kugelblitz/core"
)

// LangfuseConfig holds connection parameters for a Langfuse instance.
type LangfuseConfig struct {
	Host      string // e.g. "http://localhost:3000" or "https://cloud.langfuse.com"
	PublicKey string
	SecretKey string
}

// LangfuseObserver implements core.Observer by sending traces to Langfuse.
// Events are batched and flushed via Flush() or periodically.
type LangfuseObserver struct {
	config LangfuseConfig
	client *http.Client
	batch  []ingestionEvent
	mu     sync.Mutex
}

type ingestionEvent struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Timestamp string          `json:"timestamp"`
	Body      json.RawMessage `json:"body"`
}

func NewLangfuseObserver(cfg LangfuseConfig) *LangfuseObserver {
	return &LangfuseObserver{
		config: cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (o *LangfuseObserver) enabled() bool {
	return o.config.PublicKey != "" && o.config.SecretKey != "" && o.config.Host != ""
}

func (o *LangfuseObserver) StartTrace(ctx context.Context, name, goal string) (context.Context, core.TraceSpan) {
	if !o.enabled() {
		return ctx, &noopTraceSpan{}
	}
	id := fmt.Sprintf("trace-%d", time.Now().UnixNano())
	ts := time.Now().UTC().Format(time.RFC3339)

	body, _ := json.Marshal(map[string]any{
		"name":   name,
		"input":  goal,
		"metadata": map[string]any{"source": "kugelblitz"},
	})
	o.queue("trace-create", id, ts, body)

	return ctx, &langfuseSpan{obs: o, traceID: id}
}

func (o *LangfuseObserver) queue(eventType, id, ts string, body json.RawMessage) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.batch = append(o.batch, ingestionEvent{Type: eventType, ID: id, Timestamp: ts, Body: body})
}

// Flush sends all queued events to Langfuse.
func (o *LangfuseObserver) Flush(ctx context.Context) error {
	o.mu.Lock()
	batch := o.batch
	o.batch = nil
	o.mu.Unlock()

	if len(batch) == 0 || !o.enabled() {
		return nil
	}

	payload, _ := json.Marshal(map[string]any{"batch": batch})
	url := o.config.Host + "/api/public/ingestion"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.SetBasicAuth(o.config.PublicKey, o.config.SecretKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		log.Printf("langfuse: flush error: %v", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		log.Printf("langfuse: flush %d", resp.StatusCode)
	}
	return nil
}

// ---- Span implementations ----

type langfuseSpan struct {
	obs     *LangfuseObserver
	traceID string
	spanID  string
}

func (s *langfuseSpan) StartSpan(name string, attrs map[string]any) core.Span {
	if !s.obs.enabled() {
		return &noopTraceSpan{}
	}
	id := fmt.Sprintf("span-%d", time.Now().UnixNano())
	ts := time.Now().UTC().Format(time.RFC3339)

	body := map[string]any{
		"name":    name,
		"traceId": s.traceID,
	}
	if len(attrs) > 0 {
		body["metadata"] = attrs
	}
	b, _ := json.Marshal(body)
	s.obs.queue("span-create", id, ts, b)

	return &langfuseSpan{obs: s.obs, traceID: s.traceID, spanID: id}
}

func (s *langfuseSpan) SetAttributes(attrs map[string]any) {
	if !s.obs.enabled() {
		return
	}
	b, _ := json.Marshal(map[string]any{"metadata": attrs})
	s.obs.queue("observation-update", s.getSpanID(), "", b)
}

func (s *langfuseSpan) RecordError(err error) {
	s.SetAttributes(map[string]any{"error": err.Error()})
}

func (s *langfuseSpan) End() {}

func (s *langfuseSpan) getSpanID() string {
	if s.spanID != "" {
		return s.spanID
	}
	return s.traceID
}

type noopTraceSpan struct{}

func (n *noopTraceSpan) StartSpan(_ string, _ map[string]any) core.Span { return n }
func (n *noopTraceSpan) SetAttributes(_ map[string]any)                 {}
func (n *noopTraceSpan) RecordError(_ error)                            {}
func (n *noopTraceSpan) End()                                           {}
