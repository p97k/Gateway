// Package auth provides the authentication middleware. It validates the JWT on
// routes that require it, decodes the principal, and stores the claims on the
// request context for downstream authorization and header propagation.
//
// It depends only on the jwt.Verifier interface, so the signing scheme (HS256
// today, RS256 later) is irrelevant to this layer — Dependency Inversion in
// action.
package auth

import (
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	apperrors "github.com/nbe-group/apigateway/internal/errors"
	"github.com/nbe-group/apigateway/internal/metrics"
	"github.com/nbe-group/apigateway/internal/observability"
	"github.com/nbe-group/apigateway/internal/response"
	"github.com/nbe-group/apigateway/internal/router"
	"github.com/nbe-group/apigateway/internal/transport"
	"github.com/nbe-group/apigateway/pkg/jwt"
)

const bearerPrefix = "Bearer "

// SpanAuthentication is the name of the auth span.
const SpanAuthentication = "Authentication"

// New returns the authentication middleware. metrics may be nil.
func New(verifier jwt.Verifier, m *metrics.Metrics) gin.HandlerFunc {
	tracer := observability.Tracer()

	return func(c *gin.Context) {
		route := router.MatchedRoute(c)
		// Public route: nothing to authenticate. Note we still continue the
		// chain so optional downstream stages run.
		if route == nil || !route.Auth {
			c.Next()
			return
		}

		ctx, span := tracer.Start(c.Request.Context(), SpanAuthentication)
		defer span.End()
		c.Request = c.Request.WithContext(ctx)

		token, err := extractBearerToken(c)
		if err != nil {
			fail(c, span, m, err)
			return
		}

		claims, err := verifier.Verify(token)
		if err != nil {
			fail(c, span, m, err)
			return
		}

		transport.SetClaims(c, claims)
		span.SetStatus(codes.Ok, "authenticated")
		c.Next()
	}
}

func extractBearerToken(c *gin.Context) (string, error) {
	header := c.GetHeader("Authorization")
	if header == "" {
		return "", apperrors.ErrMissingToken
	}
	if !strings.HasPrefix(header, bearerPrefix) {
		return "", apperrors.ErrInvalidToken.WithMessage("Authorization header must use the Bearer scheme")
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, bearerPrefix))
	if token == "" {
		return "", apperrors.ErrMissingToken
	}
	return token, nil
}

func fail(c *gin.Context, span trace.Span, m *metrics.Metrics, err error) {
	apiErr := apperrors.From(err)
	if m != nil {
		m.AuthFailures.WithLabelValues(apiErr.Code).Inc()
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, apiErr.Code)
	response.Error(c, err)
}
