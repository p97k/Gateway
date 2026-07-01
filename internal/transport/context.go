// Package transport holds cross-cutting HTTP transport concerns: the canonical
// header names the gateway injects, request-context propagation helpers, and
// the tuned http.RoundTripper used by the reverse proxy.
//
// Centralizing the context keys and header names here prevents the classic
// stringly-typed bug where one middleware writes "request_id" and another reads
// "requestID". Every layer goes through these helpers.
package transport

import (
	"github.com/gin-gonic/gin"

	"github.com/nbe-group/apigateway/internal/models"
)

// Canonical headers propagated to upstream services.
const (
	HeaderRequestID = "X-Request-Id"
	HeaderUserID    = "X-User-Id"
	HeaderUserRole  = "X-User-Role"
)

// gin context keys. Unexported typed wrappers would be ideal, but gin's context
// store is keyed by string; we use a private prefix to avoid collisions with
// handler-set values.
const (
	ctxKeyRequestID = "gateway.request_id"
	ctxKeyClaims    = "gateway.claims"
	ctxKeyRoute     = "gateway.route"
)

// SetRequestID stores the correlation id on the request context.
func SetRequestID(c *gin.Context, id string) { c.Set(ctxKeyRequestID, id) }

// RequestID returns the correlation id, or "" if none was set.
func RequestID(c *gin.Context) string {
	if v, ok := c.Get(ctxKeyRequestID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// SetClaims stores the authenticated principal on the request context.
func SetClaims(c *gin.Context, claims *models.Claims) { c.Set(ctxKeyClaims, claims) }

// Claims returns the authenticated principal, or nil for anonymous requests.
func Claims(c *gin.Context) *models.Claims {
	if v, ok := c.Get(ctxKeyClaims); ok {
		if claims, ok := v.(*models.Claims); ok {
			return claims
		}
	}
	return nil
}

// SetRouteName stores the matched route's service name for logging/metrics.
func SetRouteName(c *gin.Context, name string) { c.Set(ctxKeyRoute, name) }

// RouteName returns the matched service name, or "" if unrouted.
func RouteName(c *gin.Context) string {
	if v, ok := c.Get(ctxKeyRoute); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
