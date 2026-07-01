package router

import (
	"github.com/gin-gonic/gin"

	apperrors "github.com/nbe-group/apigateway/internal/errors"
	"github.com/nbe-group/apigateway/internal/response"
	"github.com/nbe-group/apigateway/internal/transport"
)

// Resolver returns the middleware that matches the request path to a route and
// stores it on the context. It runs early — before auth/authz — because those
// stages need to know the route's policy (auth required? which roles?).
//
// If no route matches, the request is rejected here with 404 and the rest of
// the chain is skipped, so downstream stages can safely assume a matched route
// exists.
func (m *Matcher) Resolver() gin.HandlerFunc {
	return func(c *gin.Context) {
		route, ok := m.Match(c.Request.URL.Path)
		if !ok {
			response.Error(c, apperrors.ErrRouteNotFound.WithMessage(
				"no route matches %q", c.Request.URL.Path))
			return
		}
		setMatchedRoute(c, route)
		// Record the target service early so logs/metrics/traces have it even
		// if a later stage (auth/authz/rate-limit) short-circuits the request.
		transport.SetRouteName(c, route.Service)
		c.Next()
	}
}
