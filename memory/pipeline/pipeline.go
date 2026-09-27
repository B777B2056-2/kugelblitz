// Package pipeline provides a minimal ordered-step primitive for memory
// consolidation flows. It unifies naming, error short-circuiting and optional
// span observation without imposing a generic data-flow abstraction — step
// state flows through closures, keeping typed stages readable.
package pipeline

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// Step is a named processing step. Returning an error aborts the pipeline.
type Step struct {
	Name string
	Run  func(ctx context.Context) error
}

// Pipeline runs steps in order. A nil tracer disables span creation.
type Pipeline struct {
	steps  []Step
	tracer trace.Tracer
}

// New builds a pipeline from the given steps.
func New(steps ...Step) *Pipeline {
	return &Pipeline{steps: steps}
}

// WithTracer attaches a tracer for per-step span observation. It returns p so
// callers can chain construction.
func (p *Pipeline) WithTracer(t trace.Tracer) *Pipeline {
	p.tracer = t
	return p
}

// Run executes steps sequentially, short-circuiting on the first error. With a
// tracer set, each step runs inside a span named after the step.
func (p *Pipeline) Run(ctx context.Context) error {
	for _, s := range p.steps {
		if err := p.runStep(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pipeline) runStep(ctx context.Context, s Step) error {
	if p.tracer == nil {
		return s.Run(ctx)
	}
	ctx, span := p.tracer.Start(ctx, s.Name)
	defer span.End()
	if err := s.Run(ctx); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}
