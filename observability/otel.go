package observability

import (
	"context"
	"fmt"
	"os"

	"github.com/B777B2056-2/kugelblitz/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// InitTracer creates and registers a global OTel TracerProvider.
// If cfg.Enabled is false, OTel stays in no-op mode (the SDK default).
// Environment variables OTEL_EXPORTER_OTLP_ENDPOINT / OTEL_EXPORTER_OTLP_HEADERS
// serve as fallback when config fields are empty.
func InitTracer(ctx context.Context, cfg config.ObservabilityConfig) (_ func(), err error) {
	if !cfg.Enabled {
		return func() {}, nil
	}

	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	if endpoint == "" {
		return nil, fmt.Errorf("otel: endpoint not configured")
	}

	opts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(endpoint)}
	if cfg.AuthHeader != "" {
		opts = append(opts, otlptracehttp.WithHeaders(map[string]string{
			"Authorization": cfg.AuthHeader,
		}))
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otel: create exporter: %w", err)
	}

	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "kugelblitz"
	}

	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, fmt.Errorf("otel: create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return func() { _ = tp.Shutdown(ctx) }, nil
}
