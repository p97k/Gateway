// Package observability wires OpenTelemetry tracing for the gateway.
//
// It installs a global TracerProvider and a W3C TraceContext propagator so that
// (a) middleware can open spans for each pipeline stage and (b) the proxy's
// instrumented transport automatically injects `traceparent` into upstream
// requests — giving end-to-end distributed traces across the gateway and the
// backend services.
package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/nbe-group/apigateway/internal/config"
)

// TracerName is the instrumentation scope used for gateway-authored spans.
const TracerName = "github.com/nbe-group/apigateway"

// Shutdown flushes and stops the tracer provider. Always call it (deferred) on
// process exit so buffered spans are exported.
type Shutdown func(context.Context) error

// Setup configures global tracing from config and returns a shutdown function.
//
// Behavior:
//   - Disabled  -> a no-op shutdown; the global no-op tracer remains, so span
//     calls are cheap and safe everywhere.
//   - No endpoint -> a real SDK provider WITHOUT an exporter. Spans are created
//     and sampled (useful in tests and for in-process trace IDs) but not
//     shipped anywhere.
//   - Endpoint set -> spans are batched and exported to an OTLP/HTTP collector.
func Setup(ctx context.Context, cfg config.TracingConfig) (Shutdown, error) {
	noop := func(context.Context) error { return nil }
	if !cfg.Enabled {
		return noop, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName(cfg)),
		),
	)
	if err != nil {
		return noop, fmt.Errorf("build otel resource: %w", err)
	}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(samplerRatio(cfg)))),
	}

	if cfg.Endpoint != "" {
		exporter, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(cfg.Endpoint),
			otlptracehttp.WithInsecure(), // collectors are typically on a trusted network; use TLS in prod
		)
		if err != nil {
			return noop, fmt.Errorf("create otlp exporter: %w", err)
		}
		opts = append(opts, sdktrace.WithBatcher(exporter))
	}

	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	// W3C TraceContext + Baggage so trace ids flow across the wire both ways.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(shutdownCtx context.Context) error {
		ctx, cancel := context.WithTimeout(shutdownCtx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(ctx)
	}, nil
}

// Tracer returns the gateway's named tracer.
func Tracer() trace.Tracer { return otel.Tracer(TracerName) }

func serviceName(cfg config.TracingConfig) string {
	if cfg.ServiceName != "" {
		return cfg.ServiceName
	}
	return "api-gateway"
}

func samplerRatio(cfg config.TracingConfig) float64 {
	if cfg.SamplerRatio <= 0 {
		return 1.0
	}
	return cfg.SamplerRatio
}
