package persist

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// JSONLEvent is a single line in a JSONL file.
type JSONLEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// JSONLPersist implements IPersist and adds JSONL-specific append/read methods.
type JSONLPersist struct {
	backend IPersist
}

// NewJSONLPersist creates a JSONLPersist backed by the given IPersist.
func NewJSONLPersist(backend IPersist) *JSONLPersist {
	return &JSONLPersist{backend: backend}
}

// Backend returns the underlying IPersist.
func (j *JSONLPersist) Backend() IPersist { return j.backend }

// ---- IPersist implementation ----

func (j *JSONLPersist) Store(ctx context.Context, key string, data []byte) error {
	return j.backend.Store(ctx, key, data)
}
func (j *JSONLPersist) Load(ctx context.Context, key string) ([]byte, error) {
	return j.backend.Load(ctx, key)
}
func (j *JSONLPersist) Delete(ctx context.Context, key string) error {
	return j.backend.Delete(ctx, key)
}
func (j *JSONLPersist) List(ctx context.Context, prefix string) ([]string, error) {
	return j.backend.List(ctx, prefix)
}
func (j *JSONLPersist) Exists(ctx context.Context, key string) bool {
	return j.backend.Exists(ctx, key)
}

// ---- Extended methods ----

// Append adds JSONL events to the end of a file.
func (j *JSONLPersist) Append(ctx context.Context, path string, events []JSONLEvent) error {
	existing, _ := j.backend.Load(ctx, path)
	var all []byte
	if len(existing) > 0 {
		all = existing
		if all[len(all)-1] != '\n' {
			all = append(all, '\n')
		}
	}
	for _, evt := range events {
		data, err := json.Marshal(evt)
		if err != nil {
			return fmt.Errorf("jsonl append: %w", err)
		}
		all = append(all, data...)
		all = append(all, '\n')
	}
	return j.backend.Store(ctx, path, all)
}

// ReadAll reads all JSONL events from a file.
func (j *JSONLPersist) ReadAll(path string) ([]JSONLEvent, error) {
	data, err := j.backend.Load(context.Background(), path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var events []JSONLEvent
	for dec.More() {
		var evt JSONLEvent
		if err := dec.Decode(&evt); err != nil {
			return nil, fmt.Errorf("jsonl read: %w", err)
		}
		events = append(events, evt)
	}
	return events, nil
}

// WriteAll overwrites a file with JSONL events.
func (j *JSONLPersist) WriteAll(ctx context.Context, path string, events []JSONLEvent) error {
	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	enc := json.NewEncoder(bw)
	for _, evt := range events {
		if err := enc.Encode(evt); err != nil {
			return fmt.Errorf("jsonl write: %w", err)
		}
	}
	_ = bw.Flush()
	return j.backend.Store(ctx, path, buf.Bytes())
}
