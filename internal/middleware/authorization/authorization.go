// Package authorization provides route-level, role-based access control.
//
// Authorization is configuration-driven: each route may list the roles allowed
// to reach it (config.RouteConfig.Roles). This middleware runs after
// authentication, compares the authenticated principal's role against the
// matched route's allow-list, and rejects with 403 on mismatch.
//
// Keeping the policy in configuration (rather than code) means new
// access rules ship without recompiling the gateway, and the decision logic
// here stays a single, well-tested function.
package authorization

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	apperrors "github.com/nbe-group/apigateway/internal/errors"
	"github.com/nbe-group/apigateway/internal/metrics"
	"github.com/nbe-group/apigateway/internal/observability"
	"github.com/nbe-group/apigateway/internal/response"
	"github.com/nbe-group/apigateway/internal/router"
	"github.com/nbe-group/apigateway/internal/transport"
)

// SpanAuthorization is the name of the authorization span.
const SpanAuthorization = "Authorization"

// New returns the authorization middleware. metrics may be nil.
func New(m *metrics.Metrics) gin.HandlerFunc {
	tracer := observability.Tracer()

	return func(c *gin.Context) {
		route := router.MatchedRoute(c)
		// No route, or no role restriction: nothing to authorize. (A route can
		// require authentication without restricting roles — any authenticated
		// user is then allowed.)
		if route == nil || len(route.Roles) == 0 {
			c.Next()
			return
		}

		_, span := tracer.Start(c.Request.Context(), SpanAuthorization)
		defer span.End()

		claims := transport.Claims(c)
		if claims == nil {
			// Defensive: a roles-restricted route must also be auth:true (the
			// config validator enforces this), so claims should exist. If not,
			// treat as unauthorized rather than panicking.
			deny(c, span, m, route.Service, apperrors.ErrMissingToken)
			return
		}

		if !roleAllowed(claims.Role, route.Roles) {
			deny(c, span, m, route.Service, apperrors.ErrForbidden.WithMessage(
				"role %q is not permitted to access this resource", claims.Role))
			return
		}

		span.SetStatus(codes.Ok, "authorized")
		c.Next()
	}
}

// roleAllowed reports whether role is in the allow-list.
func roleAllowed(role string, allowed []string) bool {
	for _, r := range allowed {
		if r == role {
			return true
		}
	}
	return false
}

func deny(c *gin.Context, span trace.Span, m *metrics.Metrics, service string, err error) {
	if m != nil {
		m.AuthzFailures.WithLabelValues(service).Inc()
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, apperrors.From(err).Code)
	response.Error(c, err)
}
