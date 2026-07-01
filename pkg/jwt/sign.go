package jwt

import (
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/nbe-group/apigateway/internal/models"
)

// SignHS256 mints an HS256-signed token. It exists primarily to support tests
// and local development (and a potential dev-only token endpoint); the gateway
// itself only ever verifies tokens, it does not issue them in production.
func SignHS256(secret string, claims models.Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// NewClaims is a small convenience for constructing claims with sane registered
// fields populated. exp is always set to now+ttl, so a negative ttl yields an
// already-expired token (handy for exercising the expiry path in tests).
func NewClaims(subject, role, issuer, audience string, ttl time.Duration, now time.Time) models.Claims {
	rc := jwt.RegisteredClaims{
		Subject:   subject,
		Issuer:    issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	if audience != "" {
		rc.Audience = jwt.ClaimStrings{audience}
	}
	return models.Claims{Role: role, RegisteredClaims: rc}
}
