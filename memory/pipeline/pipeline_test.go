package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestPipeline_RunsStepsInOrder(t *testing.T) {
	var order []string
	p := New(
		Step{Name: "a", Run: func(ctx context.Context) error { order = append(order, "a"); return nil }},
		Step{Name: "b", Run: func(ctx context.Context) error { order = append(order, "b"); return nil }},
		Step{Name: "c", Run: func(ctx context.Context) error { order = append(order, "c"); return nil }},
	)

	require.NoError(t, p.Run(context.Background()))
	assert.Equal(t, []string{"a", "b", "c"}, order)
}

func TestPipeline_StopsOnFirstError(t *testing.T) {
	var order []string
	wantErr := errors.New("boom")
	p := New(
		Step{Name: "a", Run: func(ctx context.Context) error { order = append(order, "a"); return nil }},
		Step{Name: "b", Run: func(ctx context.Context) error { order = append(order, "b"); return wantErr }},
		Step{Name: "c", Run: func(ctx context.Context) error { order = append(order, "c"); return nil }},
	)

	err := p.Run(context.Background())
	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, []string{"a", "b"}, order, "step c must not run after b errors")
}

func TestPipeline_EmptyIsNoop(t *testing.T) {
	require.NoError(t, New().Run(context.Background()))
}

func TestPipeline_NilTracerDoesNotPanic(t *testing.T) {
	p := New(Step{Name: "a", Run: func(ctx context.Context) error { return nil }})
	require.NoError(t, p.Run(context.Background()))
}

func TestPipeline_NoopTracerDoesNotPanic(t *testing.T) {
	p := New(Step{Name: "a", Run: func(ctx context.Context) error { return nil }}).
		WithTracer(noop.NewTracerProvider().Tracer("test"))
	require.NoError(t, p.Run(context.Background()))
}
