package core

import (
	"io"
	"log/slog"
	"os"
)

// Logger is the logging interface used throughout Kugelblitz.
// Implementations can wrap slog, zap, zerolog, or any other backend.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// globalLogger is the singleton logger instance. Set via SetLogger.
var globalLogger Logger = newSlogLogger(os.Stderr, slog.LevelInfo)

// SetLogger replaces the global logger. Pass nil to restore the default.
func SetLogger(l Logger) {
	if l == nil {
		globalLogger = newSlogLogger(os.Stderr, slog.LevelInfo)
	} else {
		globalLogger = l
	}
}

// GetLogger returns the global logger.
func GetLogger() Logger { return globalLogger }

// Convenience functions using the global logger.

func Debug(msg string, args ...any) { globalLogger.Debug(msg, args...) }
func Info(msg string, args ...any)  { globalLogger.Info(msg, args...) }
func Warn(msg string, args ...any)  { globalLogger.Warn(msg, args...) }
func Error(msg string, args ...any) { globalLogger.Error(msg, args...) }

// --- Default slog-based implementation ---

type slogLogger struct {
	logger *slog.Logger
}

func newSlogLogger(w *os.File, level slog.Level) *slogLogger {
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	return &slogLogger{logger: slog.New(handler)}
}

func (l *slogLogger) Debug(msg string, args ...any) { l.logger.Debug(msg, toSlogArgs(args)...) }
func (l *slogLogger) Info(msg string, args ...any)  { l.logger.Info(msg, toSlogArgs(args)...) }
func (l *slogLogger) Warn(msg string, args ...any)  { l.logger.Warn(msg, toSlogArgs(args)...) }
func (l *slogLogger) Error(msg string, args ...any) { l.logger.Error(msg, toSlogArgs(args)...) }

// toSlogArgs converts our key-value pair variadic args to slog.Attr.
func toSlogArgs(args []any) []any {
	// slog accepts variadic ...any as key-value pairs directly
	return args
}

// --- Helpers for tests and special cases ---

// DiscardLogger returns a Logger that discards all output.
func DiscardLogger() Logger {
	return &slogLogger{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// NopLogger is a no-op implementation of Logger.
type NopLogger struct{}

func (NopLogger) Debug(string, ...any) {}
func (NopLogger) Info(string, ...any)  {}
func (NopLogger) Warn(string, ...any)  {}
func (NopLogger) Error(string, ...any) {}

// Compile-time check
var _ Logger = (*slogLogger)(nil)
var _ Logger = NopLogger{}
