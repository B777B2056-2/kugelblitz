package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
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
	seq    atomic.Int64
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

// nextID returns a unique ID with the given prefix.
// Uses nanosecond timestamp + atomic counter to prevent collisions
// even when called in rapid succession.
func (o *LangfuseObserver) nextID(prefix string) string {
	n := o.seq.Add(1)
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), n)
}

func (o *LangfuseObserver) enabled() bool {
	return o.config.PublicKey != "" && o.config.SecretKey != "" && o.config.Host != ""
}

func (o *LangfuseObserver) StartTrace(ctx context.Context, name, goal string) (context.Context, core.TraceSpan) {
	if !o.enabled() {
		return ctx, &noopTraceSpan{}
	}
	traceID := o.nextID("trace")
	ts := time.Now().UTC().Format(time.RFC3339)

	body, _ := json.Marshal(map[string]any{
		"id":        traceID,
		"name":      name,
		"input":     goal,
		"startTime": ts,
		"metadata":  map[string]any{"source": "kugelblitz"},
	})
	o.queue("trace-create", traceID, ts, body)

	return ctx, &langfuseSpan{
		obs:       o,
		traceID:   traceID,
		spanType:  "trace",
		startTime: ts,
	}
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

	// Separate trace-create events — Langfuse v2 requires the trace to exist
	// before spans/generations reference it.
	var traces, rest []ingestionEvent
	for _, e := range batch {
		if e.Type == "trace-create" {
			traces = append(traces, e)
		} else {
			rest = append(rest, e)
		}
	}

	// Flush trace-create first
	if len(traces) > 0 {
		if err := o.send(ctx, traces); err != nil {
			return err
		}
	}
	// Then flush spans/generations
	if len(rest) > 0 {
		if err := o.send(ctx, rest); err != nil {
			return err
		}
	}
	return nil
}

func (o *LangfuseObserver) send(ctx context.Context, batch []ingestionEvent) error {
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
		body, _ := io.ReadAll(resp.Body)
		log.Printf("langfuse: flush %d: %s", resp.StatusCode, string(body))
		return fmt.Errorf("langfuse: status %d", resp.StatusCode)
	}
	log.Printf("langfuse: flushed %d events OK", len(batch))
	return nil
}

// ---- Span implementations ----

type langfuseSpan struct {
	obs       *LangfuseObserver
	traceID   string
	spanID    string
	spanType  string // "trace" | "span" | "generation"
	startTime string
}

func (s *langfuseSpan) parentObservationID() string {
	if s.spanID != "" {
		return s.spanID
	}
	// trace-level span → parent is the trace itself, but Langfuse uses traceId
	// for top-level observations; return empty so they attach to the trace root.
	return s.traceID
}

func (s *langfuseSpan) StartGeneration(attrs map[string]any) core.Span {
	if !s.obs.enabled() {
		return &noopTraceSpan{}
	}
	id := s.obs.nextID("gen")
	ts := time.Now().UTC().Format(time.RFC3339)

	name := "llm-generation"
	if n, ok := attrs["name"].(string); ok && n != "" {
		name = n
	}

	body := map[string]any{
		"id":                   id,
		"name":                 name,
		"traceId":              s.traceID,
		"parentObservationId":  s.parentObservationID(),
		"startTime":            ts,
	}

	meta := map[string]any{}
	for k, v := range attrs {
		switch k {
		case "input", "output":
			body[k] = v
		case "tool_calls", "thinking":
			meta[k] = v
		case "name":
			// already handled above
		default:
			// tokens_in/out/total handled by usage
		}
	}
	if len(meta) > 0 {
		body["metadata"] = meta
	}
	if _, ok := attrs["tokens_in"]; ok {
		body["usage"] = map[string]any{
			"promptTokens":     attrs["tokens_in"],
			"completionTokens": attrs["tokens_out"],
			"totalTokens":      attrs["tokens_total"],
		}
	}
	b, _ := json.Marshal(body)
	s.obs.queue("generation-create", id, ts, b)
	return &langfuseSpan{
		obs:       s.obs,
		traceID:   s.traceID,
		spanID:    id,
		spanType:  "generation",
		startTime: ts,
	}
}

func (s *langfuseSpan) StartSpan(name string, attrs map[string]any) core.Span {
	if !s.obs.enabled() {
		return &noopTraceSpan{}
	}
	id := s.obs.nextID("span")
	ts := time.Now().UTC().Format(time.RFC3339)

	body := map[string]any{
		"id":                  id,
		"name":                name,
		"traceId":             s.traceID,
		"parentObservationId": s.parentObservationID(),
		"startTime":           ts,
	}

	// Move input/output to top-level body fields, rest to metadata
	meta := map[string]any{}
	for k, v := range attrs {
		switch k {
		case "input", "output":
			body[k] = v
		default:
			meta[k] = v
		}
	}
	if len(meta) > 0 {
		body["metadata"] = meta
	}
	b, _ := json.Marshal(body)
	s.obs.queue("span-create", id, ts, b)

	return &langfuseSpan{
		obs:       s.obs,
		traceID:   s.traceID,
		spanID:    id,
		spanType:  "span",
		startTime: ts,
	}
}

func (s *langfuseSpan) SetAttributes(attrs map[string]any) {
	if !s.obs.enabled() {
		return
	}
	ts := time.Now().UTC().Format(time.RFC3339)

	var eventType, eventID string
	body := map[string]any{}

	switch s.spanType {
	case "trace":
		eventType = "trace-update"
		eventID = s.traceID + "-update"
		body["id"] = s.traceID
	case "generation":
		eventType = "generation-update"
		eventID = s.spanID + "-update"
		body["id"] = s.spanID
		body["traceId"] = s.traceID
	case "span":
		eventType = "span-update"
		eventID = s.spanID + "-update"
		body["id"] = s.spanID
		body["traceId"] = s.traceID
	}

	for k, v := range attrs {
		body[k] = v
	}
	b, _ := json.Marshal(body)
	s.obs.queue(eventType, eventID, ts, b)
}

func (s *langfuseSpan) RecordError(err error) {
	s.SetAttributes(map[string]any{
		"statusMessage": err.Error(),
		"level":         "ERROR",
	})
}

func (s *langfuseSpan) End() {
	if !s.obs.enabled() {
		return
	}
	ts := time.Now().UTC().Format(time.RFC3339)

	var eventType, eventID string
	body := map[string]any{
		"endTime": ts,
	}

	switch s.spanType {
	case "trace":
		eventType = "trace-update"
		eventID = s.traceID + "-end"
		body["id"] = s.traceID
	case "generation":
		eventType = "generation-update"
		eventID = s.spanID + "-end"
		body["id"] = s.spanID
		body["traceId"] = s.traceID
	case "span":
		eventType = "span-update"
		eventID = s.spanID + "-end"
		body["id"] = s.spanID
		body["traceId"] = s.traceID
	default:
		return
	}

	b, _ := json.Marshal(body)
	s.obs.queue(eventType, eventID, ts, b)
}

type noopTraceSpan struct{}

func (n *noopTraceSpan) StartSpan(_ string, _ map[string]any) core.Span { return n }
func (n *noopTraceSpan) StartGeneration(_ map[string]any) core.Span      { return n }
func (n *noopTraceSpan) SetAttributes(_ map[string]any)                  {}
func (n *noopTraceSpan) RecordError(_ error)                             {}
func (n *noopTraceSpan) End()                                            {}
