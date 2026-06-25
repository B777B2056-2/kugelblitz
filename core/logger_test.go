package core

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetLogger_Custom(t *testing.T) {
	original := globalLogger
	defer func() { globalLogger = original }()

	custom := &captureLogger{}
	SetLogger(custom)

	Info("hello", "key", "val")
	assert.Equal(t, "hello", custom.lastMsg)
	assert.Equal(t, []any{"key", "val"}, custom.lastArgs)

	Debug("debug msg")
	Error("error msg")
	Warn("warn msg")
	assert.Equal(t, 4, custom.count)
}

func TestSetLogger_NilRestoresDefault(t *testing.T) {
	original := globalLogger
	defer func() { globalLogger = original }()

	SetLogger(nil)
	assert.NotNil(t, globalLogger)
}

func TestGetLogger(t *testing.T) {
	original := globalLogger
	defer func() { globalLogger = original }()

	custom := &captureLogger{}
	SetLogger(custom)
	assert.Equal(t, custom, GetLogger())
}

func TestDiscardLogger(t *testing.T) {
	l := DiscardLogger()
	// Should not panic
	l.Debug("test")
	l.Info("test")
	l.Warn("test")
	l.Error("test")
}

func TestNopLogger(t *testing.T) {
	var l NopLogger
	l.Debug("test")
	l.Info("test")
	l.Warn("test")
	l.Error("test")
}

func TestSlogLogger_Output(t *testing.T) {
	var buf bytes.Buffer
	l := &slogLogger{logger: newSlogLoggerWriter(&buf)}
	l.Info("connected", "server", "github", "tools", 5)
	output := buf.String()
	assert.Contains(t, output, "connected")
	assert.Contains(t, output, "server=github")
	assert.Contains(t, output, "tools=5")
}

func TestSlogLogger_Levels(t *testing.T) {
	var buf bytes.Buffer
	l := &slogLogger{logger: newSlogLoggerWriter(&buf)}

	l.Debug("debug msg", "a", 1)
	l.Info("info msg", "b", 2)
	l.Warn("warn msg", "c", 3)
	l.Error("error msg", "d", 4)

	output := buf.String()
	require.Contains(t, output, "INFO")
	require.Contains(t, output, "WARN")
	require.Contains(t, output, "ERROR")
}

// captureLogger records calls for testing.
type captureLogger struct {
	lastMsg  string
	lastArgs []any
	count     int
}

func (c *captureLogger) Debug(msg string, args ...any) { c.lastMsg = msg; c.lastArgs = args; c.count++ }
func (c *captureLogger) Info(msg string, args ...any)  { c.lastMsg = msg; c.lastArgs = args; c.count++ }
func (c *captureLogger) Warn(msg string, args ...any)  { c.lastMsg = msg; c.lastArgs = args; c.count++ }
func (c *captureLogger) Error(msg string, args ...any) { c.lastMsg = msg; c.lastArgs = args; c.count++ }

var _ Logger = (*captureLogger)(nil)

func newSlogLoggerWriter(w *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, nil))
}
