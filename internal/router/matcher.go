// Package router resolves an incoming request path to a configured route, and
// exposes the matched route to later stages (auth, authz, proxy) via the
// request context.
//
// Gin's own router is path-parameter based; the gateway instead needs
// configuration-driven longest-prefix matching ("/api/products" -> product-
// service). That logic lives in Matcher so it is unit-testable in isolation
// from the HTTP layer.
package router

import (
	"strings"

	"github.com/nbe-group/apigateway/internal/config"
)

// Matcher performs longest-prefix route matching. Routes are expected to be
// pre-sorted longest-first (config.normalize does this), so the first match is
// also the most specific.
type Matcher struct {
	routes []config.RouteConfig
}

// NewMatcher builds a Matcher over a copy of the given routes.
func NewMatcher(routes []config.RouteConfig) *Matcher {
	cp := make([]config.RouteConfig, len(routes))
	copy(cp, routes)
	return &Matcher{routes: cp}
}

// Match returns the route whose prefix matches path, and true, or (nil, false).
//
// Matching is boundary-aware: prefix "/api/order" does NOT match "/api/orders".
// A trailing "/*" or "*" in the configured prefix is treated as a wildcard and
// stripped before comparison, so "/api/admin/*" matches "/api/admin" and
// anything beneath it.
func (m *Matcher) Match(path string) (*config.RouteConfig, bool) {
	for i := range m.routes {
		prefix := normalizePrefix(m.routes[i].Prefix)
		if matchPrefix(path, prefix) {
			return &m.routes[i], true
		}
	}
	return nil, false
}

func normalizePrefix(prefix string) string {
	prefix = strings.TrimSuffix(prefix, "*")
	prefix = strings.TrimSuffix(prefix, "/")
	return prefix
}

func matchPrefix(path, prefix string) bool {
	if prefix == "" {
		return true // matches everything (a catch-all route)
	}
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+"/")
}
