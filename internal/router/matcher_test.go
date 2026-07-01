package router_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nbe-group/apigateway/internal/config"
	"github.com/nbe-group/apigateway/internal/router"
)

func testRoutes() []config.RouteConfig {
	// Longest-first, as config.normalize would produce.
	return []config.RouteConfig{
		{Prefix: "/api/products", Service: "product"},
		{Prefix: "/api/orders", Service: "order"},
		{Prefix: "/api/admin", Service: "admin"},
	}
}

func TestMatch_ExactPrefix(t *testing.T) {
	m := router.NewMatcher(testRoutes())
	r, ok := m.Match("/api/products")
	require.True(t, ok)
	assert.Equal(t, "product", r.Service)
}

func TestMatch_SubPath(t *testing.T) {
	m := router.NewMatcher(testRoutes())
	r, ok := m.Match("/api/products/42")
	require.True(t, ok)
	assert.Equal(t, "product", r.Service)
}

func TestMatch_BoundaryAware(t *testing.T) {
	// "/api/order" must NOT match the "/api/orders" route's sibling, and a
	// path like "/api/productsX" must not match "/api/products".
	m := router.NewMatcher([]config.RouteConfig{{Prefix: "/api/products", Service: "product"}})
	_, ok := m.Match("/api/productsX")
	assert.False(t, ok)
}

func TestMatch_NoMatch(t *testing.T) {
	m := router.NewMatcher(testRoutes())
	_, ok := m.Match("/nope")
	assert.False(t, ok)
}

func TestMatch_Wildcard(t *testing.T) {
	m := router.NewMatcher([]config.RouteConfig{{Prefix: "/api/admin/*", Service: "admin"}})
	r, ok := m.Match("/api/admin/users/1")
	require.True(t, ok)
	assert.Equal(t, "admin", r.Service)

	_, ok = m.Match("/api/admin")
	assert.True(t, ok, "wildcard prefix should also match the base path")
}
