// Package proxy implements the reverse-proxy terminal handler using
// net/http/httputil.ReverseProxy.
//
// Responsibilities:
//   - Resolve the matched route's service to an upstream instance (via the
//     ServiceRegistry abstraction).
//   - Forward the request faithfully: method, path (optionally prefix-stripped),
//     query string, headers and body are preserved.
//   - Inject the gateway's identity/correlation headers downstream:
//     X-Request-Id, X-User-Id, X-User-Role.
//   - Open a "Proxy Request" span; the instrumented transport propagates trace
//     context to the upstream automatically.
//   - Translate upstream transport failures into the standard error envelope.
//
// A single ReverseProxy instance is shared across requests (it is concurrency-
// safe). Per-request data — target URL, outgoing path, identity headers — is
// passed through the request context, so we avoid allocating a proxy per call.
package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"

	"github.com/nbe-group/apigateway/internal/config"
	apperrors "github.com/nbe-group/apigateway/internal/errors"
	"github.com/nbe-group/apigateway/internal/metrics"
	"github.com/nbe-group/apigateway/internal/observability"
	"github.com/nbe-group/apigateway/internal/response"
	"github.com/nbe-group/apigateway/internal/router"
	"github.com/nbe-group/apigateway/internal/service_registry"
	"github.com/nbe-group/apigateway/internal/transport"
)

// SpanProxyRequest is the name of the proxy span.
const SpanProxyRequest = "Proxy Request"

// Proxy forwards requests to upstream services.
type Proxy struct {
	registry service_registry.ServiceRegistry
	rp       *httputil.ReverseProxy
	metrics  *metrics.Metrics
	logger   *zap.Logger
}

// target carries everything Rewrite/ErrorHandler need for a single request.
type target struct {
	url       *url.URL
	outPath   string
	service   string
	requestID string
	userID    string
	role      string
}

type ctxKey struct{}

// New constructs a Proxy. rt is the (instrumented) RoundTripper; logger and
// metrics may be nil.
func New(reg service_registry.ServiceRegistry, rt http.RoundTripper, m *metrics.Metrics, logger *zap.Logger) *Proxy {
	if logger == nil {
		logger = zap.NewNop()
	}
	p := &Proxy{registry: reg, metrics: m, logger: logger}

	p.rp = &httputil.ReverseProxy{
		Transport:      rt,
		Rewrite:        p.rewrite,
		ErrorHandler:   p.errorHandler,
		ModifyResponse: p.modifyResponse,
	}
	return p
}

// Handler returns the gin terminal handler that proxies the request.
func (p *Proxy) Handler() gin.HandlerFunc {
	tracer := observability.Tracer()

	return func(c *gin.Context) {
		route := router.MatchedRoute(c)
		if route == nil {
			// Should not happen — the resolver runs first — but never panic.
			response.Error(c, apperrors.ErrRouteNotFound)
			return
		}

		instance, err := p.registry.Resolve(c.Request.Context(), route.Service)
		if err != nil {
			response.Error(c, err)
			return
		}

		ctx, span := tracer.Start(c.Request.Context(), SpanProxyRequest)
		defer span.End()

		tg := &target{
			url:       instance.URL,
			outPath:   outboundPath(c.Request.URL.Path, route),
			service:   route.Service,
			requestID: transport.RequestID(c),
		}
		if claims := transport.Claims(c); claims != nil {
			tg.userID = claims.UserID()
			tg.role = claims.Role
		}

		ctx = context.WithValue(ctx, ctxKey{}, tg)
		p.rp.ServeHTTP(c.Writer, c.Request.WithContext(ctx))

		if c.Writer.Status() >= 500 {
			span.SetStatus(codes.Error, "upstream error")
		}
	}
}

// rewrite mutates the outbound request: sets the upstream target, rewrites the
// path, preserves the query, and injects identity/correlation headers.
func (p *Proxy) rewrite(pr *httputil.ProxyRequest) {
	tg, ok := pr.In.Context().Value(ctxKey{}).(*target)
	if !ok {
		return
	}

	pr.Out.URL.Scheme = tg.url.Scheme
	pr.Out.URL.Host = tg.url.Host
	pr.Out.Host = tg.url.Host
	pr.Out.URL.Path = singleJoiningSlash(tg.url.Path, tg.outPath)
	// RawQuery is already copied from the inbound request onto Out, so query
	// parameters are preserved automatically.

	// Standard X-Forwarded-* headers so the upstream knows the real client.
	pr.SetXForwarded()

	// Gateway-injected identity & correlation headers.
	pr.Out.Header.Set(transport.HeaderRequestID, tg.requestID)
	if tg.userID != "" {
		pr.Out.Header.Set(transport.HeaderUserID, tg.userID)
	}
	if tg.role != "" {
		pr.Out.Header.Set(transport.HeaderUserRole, tg.role)
	}
}

// modifyResponse stamps the correlation id on the response so clients always
// see it even on proxied responses.
func (p *Proxy) modifyResponse(resp *http.Response) error {
	if tg, ok := resp.Request.Context().Value(ctxKey{}).(*target); ok {
		resp.Header.Set(transport.HeaderRequestID, tg.requestID)
	}
	return nil
}

// errorHandler converts upstream transport failures into the standard error
// envelope and records metrics. ReverseProxy calls this when the upstream is
// unreachable, times out, or the client disconnects.
func (p *Proxy) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	// Client cancelled / disconnected: nothing to send, don't log as an error.
	if errors.Is(err, context.Canceled) {
		return
	}

	service := ""
	if tg, ok := r.Context().Value(ctxKey{}).(*target); ok {
		service = tg.service
	}
	if p.metrics != nil {
		p.metrics.UpstreamErrors.WithLabelValues(orUnknown(service)).Inc()
	}

	apiErr := apperrors.ErrUpstreamUnavailable
	if errors.Is(err, context.DeadlineExceeded) {
		apiErr = apperrors.ErrUpstreamTimeout
	}
	p.logger.Warn("upstream request failed",
		zap.String("service", service),
		zap.String("error", err.Error()),
	)
	response.WriteError(w, apiErr)
}

// outboundPath computes the path forwarded upstream, honoring strip_prefix.
// When stripping, the configured prefix is removed so the upstream sees its own
// local path (e.g. gateway /api/products/42 -> product-service /42).
func outboundPath(inPath string, route *config.RouteConfig) string {
	if !route.StripPrefix {
		return inPath
	}
	prefix := strings.TrimSuffix(strings.TrimSuffix(route.Prefix, "*"), "/")
	trimmed := strings.TrimPrefix(inPath, prefix)
	if trimmed == "" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	return trimmed
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		if a == "" {
			return b
		}
		return a + "/" + b
	}
	return a + b
}
