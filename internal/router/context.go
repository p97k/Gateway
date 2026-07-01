package router

import (
	"github.com/gin-gonic/gin"

	"github.com/nbe-group/apigateway/internal/config"
)

const ctxKeyMatchedRoute = "gateway.matched_route"

// setMatchedRoute stores the resolved route on the request context.
func setMatchedRoute(c *gin.Context, route *config.RouteConfig) {
	c.Set(ctxKeyMatchedRoute, route)
}

// MatchedRoute returns the route resolved by the resolver middleware, or nil.
// Auth, authz and the proxy handler read this to make their decisions, which
// keeps route matching a single source of truth rather than re-deriving it.
func MatchedRoute(c *gin.Context) *config.RouteConfig {
	if v, ok := c.Get(ctxKeyMatchedRoute); ok {
		if r, ok := v.(*config.RouteConfig); ok {
			return r
		}
	}
	return nil
}
