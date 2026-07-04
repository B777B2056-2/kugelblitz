package core

import "context"

// Observer is the top-level observability hook. Each Planner.Execute call
// creates a new TraceSpan.
type Observer interface {
	// Name returns a short identifier for this observer (e.g. "langfuse", "datadog").
	Name() string
	StartTrace(ctx context.Context, name, goal string) (context.Context, TraceSpan)
}

// TraceSpan represents a single Planner execution.
type TraceSpan interface {
	Span
}

// Span represents a single operation within a trace (LLM call, tool call, etc.).
// Spans can be nested: StartSpan/StartGeneration create child observations.
type Span interface {
	StartSpan(name string, attrs map[string]any) Span
	StartGeneration(attrs map[string]any) Span
	SetAttributes(attrs map[string]any)
	RecordError(err error)
	End()
}

// NoopObserver is a default observer that does nothing.
type NoopObserver struct{}

type noopSpan struct{}

func (NoopObserver) Name() string { return "noop" }
func (NoopObserver) StartTrace(ctx context.Context, name, goal string) (context.Context, TraceSpan) {
	return ctx, &noopSpan{}
}
func (*noopSpan) StartSpan(_ string, _ map[string]any) Span { return &noopSpan{} }
func (*noopSpan) StartGeneration(_ map[string]any) Span     { return &noopSpan{} }
func (*noopSpan) SetAttributes(_ map[string]any)            {}
func (*noopSpan) End()                                      {}
func (*noopSpan) RecordError(_ error)                       {}
