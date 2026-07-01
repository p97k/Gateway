// Package metrics defines the gateway's Prometheus instrumentation.
//
// All collectors live behind a single Metrics struct that is injected into the
// middleware that records them. Registering against an injectable
// prometheus.Registerer (rather than the global default) keeps the package
// testable — each test can use a fresh registry and assert on exact values.
package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics bundles every collector the gateway exposes.
type Metrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	ErrorsTotal     *prometheus.CounterVec
	RateLimitReject *prometheus.CounterVec
	AuthFailures    *prometheus.CounterVec
	AuthzFailures   *prometheus.CounterVec
	UpstreamErrors  *prometheus.CounterVec
}

// Label name constants keep cardinality decisions in one place. Note: we label
// by matched route/service, NOT by raw path, to avoid unbounded cardinality
// from path parameters and query strings.
const (
	labelMethod  = "method"
	labelService = "service"
	labelStatus  = "status"
	labelScope   = "scope" // rate-limit scope: global|ip|user
	labelReason  = "reason"
)

// New constructs and registers all collectors against reg.
func New(reg prometheus.Registerer) *Metrics {
	factory := promFactory{reg}
	return &Metrics{
		RequestsTotal: factory.counterVec(prometheus.CounterOpts{
			Name: "gateway_requests_total",
			Help: "Total number of HTTP requests processed by the gateway.",
		}, []string{labelMethod, labelService, labelStatus}),

		RequestDuration: factory.histogramVec(prometheus.HistogramOpts{
			Name:    "gateway_request_duration_seconds",
			Help:    "End-to-end request latency observed by the gateway.",
			Buckets: prometheus.DefBuckets,
		}, []string{labelMethod, labelService, labelStatus}),

		ErrorsTotal: factory.counterVec(prometheus.CounterOpts{
			Name: "gateway_errors_total",
			Help: "Total gateway-originated errors, labeled by error code.",
		}, []string{labelService, "code"}),

		RateLimitReject: factory.counterVec(prometheus.CounterOpts{
			Name: "gateway_rate_limit_rejections_total",
			Help: "Requests rejected by rate limiting, labeled by scope.",
		}, []string{labelScope}),

		AuthFailures: factory.counterVec(prometheus.CounterOpts{
			Name: "gateway_authentication_failures_total",
			Help: "Failed authentication attempts, labeled by reason (error code).",
		}, []string{labelReason}),

		AuthzFailures: factory.counterVec(prometheus.CounterOpts{
			Name: "gateway_authorization_failures_total",
			Help: "Failed authorization checks, labeled by service.",
		}, []string{labelService}),

		UpstreamErrors: factory.counterVec(prometheus.CounterOpts{
			Name: "gateway_upstream_errors_total",
			Help: "Errors talking to upstream services, labeled by service.",
		}, []string{labelService}),
	}
}

// ObserveRequest records the per-request count and latency in one call.
func (m *Metrics) ObserveRequest(method, service string, status int, seconds float64) {
	svc := orUnknown(service)
	st := strconv.Itoa(status)
	m.RequestsTotal.WithLabelValues(method, svc, st).Inc()
	m.RequestDuration.WithLabelValues(method, svc, st).Observe(seconds)
}

// ObserveError increments the error counter for a gateway error code.
func (m *Metrics) ObserveError(service, code string) {
	if code == "" {
		return
	}
	m.ErrorsTotal.WithLabelValues(orUnknown(service), code).Inc()
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// promFactory is a tiny helper to register collectors against an injectable
// registerer while keeping construction terse.
type promFactory struct{ reg prometheus.Registerer }

func (f promFactory) counterVec(opts prometheus.CounterOpts, labels []string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(opts, labels)
	f.reg.MustRegister(c)
	return c
}

func (f promFactory) histogramVec(opts prometheus.HistogramOpts, labels []string) *prometheus.HistogramVec {
	h := prometheus.NewHistogramVec(opts, labels)
	f.reg.MustRegister(h)
	return h
}
