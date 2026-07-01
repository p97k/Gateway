package transport

import (
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Options tunes the upstream HTTP transport.
type Options struct {
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	DialTimeout         time.Duration
	TLSHandshakeTimeout time.Duration
	// ResponseHeaderTimeout bounds how long we wait for an upstream's response
	// headers; it is the proxy's effective per-request upstream timeout.
	ResponseHeaderTimeout time.Duration
}

// DefaultOptions returns production-sane connection-pool settings. Generous
// keep-alive pools matter for a gateway: it fans many client requests onto a
// small set of upstreams, so connection reuse is the dominant performance lever.
func DefaultOptions() Options {
	return Options{
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		DialTimeout:           5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
}

// New builds an *http.Transport with the given options, wrapped in OpenTelemetry
// instrumentation so each upstream call becomes a child span and trace context
// is injected into outbound headers automatically (W3C traceparent). Returning
// it as an http.RoundTripper keeps callers decoupled from the concrete type.
func New(opts Options) http.RoundTripper {
	base := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   opts.DialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          opts.MaxIdleConns,
		MaxIdleConnsPerHost:   opts.MaxIdleConnsPerHost,
		IdleConnTimeout:       opts.IdleConnTimeout,
		TLSHandshakeTimeout:   opts.TLSHandshakeTimeout,
		ResponseHeaderTimeout: opts.ResponseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return otelhttp.NewTransport(base)
}
