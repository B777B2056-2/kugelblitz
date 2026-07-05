package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Transport handles JSON-RPC 2.0 message framing over an io.ReadWriter.
// Messages are newline-delimited JSON (one JSON object per line).
type Transport struct {
	reader *bufio.Reader
	writer *bufio.Writer
	mu     sync.Mutex // write lock
}

// NewTransport creates a new Transport over the given ReadWriter.
func NewTransport(rw io.ReadWriter) *Transport {
	return &Transport{
		reader: bufio.NewReader(rw),
		writer: bufio.NewWriter(rw),
	}
}

// ReadMessage reads and parses the next JSON-RPC message from the input.
func (t *Transport) ReadMessage() (*JSONRPCMessage, error) {
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("acp: read error: %w", err)
	}
	var msg JSONRPCMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, fmt.Errorf("acp: parse error: %w", err)
	}
	return &msg, nil
}

// WriteMessage marshals and writes a JSON-RPC message as a single line.
func (t *Transport) WriteMessage(msg *JSONRPCMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("acp: marshal error: %w", err)
	}
	if _, err := t.writer.Write(data); err != nil {
		return fmt.Errorf("acp: write error: %w", err)
	}
	if err := t.writer.WriteByte('\n'); err != nil {
		return fmt.Errorf("acp: write error: %w", err)
	}
	return t.writer.Flush()
}

// SendNotification is a convenience method that creates and sends a
// JSON-RPC notification (method with params, no id).
func (t *Transport) SendNotification(method string, params any) error {
	msg, err := NewNotification(method, params)
	if err != nil {
		return err
	}
	return t.WriteMessage(msg)
}
