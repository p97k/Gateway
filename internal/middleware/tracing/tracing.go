// Package tracing provides the middleware that opens the root "Gateway Request"
// span for each request and seeds the request context with trace state.
//
// Inbound trace context (W3C traceparent) is extracted first, so the gateway
// continues an existing distributed trace when the caller is itself
// instrumented. Downstream stages (auth, authz, proxy) open child spans off the
// context this middleware installs.
package tracing

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/nbe-group/apigateway/internal/observability"
	"github.com/nbe-group/apigateway/internal/transport"
)

// SpanGatewayRequest is the name of the root span.
const SpanGatewayRequest = "Gateway Request"

// New returns the tracing middleware.
func New() gin.HandlerFunc {
	tracer := observability.Tracer()
	propagator := otel.GetTextMapPropagator()

	return func(c *gin.Context) {
		// Continue any inbound trace.
		ctx := propagator.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		ctx, span := tracer.Start(ctx, SpanGatewayRequest,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(c.Request.Method),
				semconv.URLPath(c.Request.URL.Path),
			),
		)
		defer span.End()

		// Make the span/context available to downstream handlers.
		c.Request = c.Request.WithContext(ctx)
		// Surface the trace id as a response header for easy correlation.
		if sc := span.SpanContext(); sc.HasTraceID() {
			c.Header("X-Trace-Id", sc.TraceID().String())
		}

		c.Next()

		status := c.Writer.Status()
		span.SetAttributes(
			semconv.HTTPResponseStatusCode(status),
			attribute.String("gateway.service", transport.RouteName(c)),
		)
		if status >= 500 {
			span.SetStatus(codes.Error, "upstream/gateway error")
		}
	}
}
