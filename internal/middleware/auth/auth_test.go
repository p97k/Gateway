package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nbe-group/apigateway/internal/config"
	"github.com/nbe-group/apigateway/internal/middleware/auth"
	"github.com/nbe-group/apigateway/internal/router"
	"github.com/nbe-group/apigateway/internal/transport"
	"github.com/nbe-group/apigateway/pkg/jwt"
)

func init() { gin.SetMode(gin.TestMode) }

const (
	secret = "test-secret"
	issuer = "auth"
	aud    = "api"
)

// buildChain wires resolver -> auth -> terminal handler, mirroring production.
func buildChain(t *testing.T, routes []config.RouteConfig) (*gin.Engine, *capturedClaims) {
	t.Helper()
	v, err := jwt.New(jwt.Config{Algorithm: "HS256", Secret: secret, Issuer: issuer, Audience: aud})
	require.NoError(t, err)

	captured := &capturedClaims{}
	matcher := router.NewMatcher(routes)
	r := gin.New()
	r.NoRoute(matcher.Resolver(), auth.New(v, nil), func(c *gin.Context) {
		if cl := transport.Claims(c); cl != nil {
			captured.userID = cl.UserID()
			captured.role = cl.Role
		}
		c.Status(http.StatusOK)
	})
	return r, captured
}

type capturedClaims struct {
	userID string
	role   string
}

func bearer(t *testing.T, sub, role string, ttl time.Duration) string {
	t.Helper()
	tok, err := jwt.SignHS256(secret, jwt.NewClaims(sub, role, issuer, aud, ttl, time.Now()))
	require.NoError(t, err)
	return "Bearer " + tok
}

func do(r *gin.Engine, path, authHeader string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	r.ServeHTTP(w, req)
	return w
}

var protectedRoutes = []config.RouteConfig{{Prefix: "/api", Service: "svc", Auth: true}}
var publicRoutes = []config.RouteConfig{{Prefix: "/api", Service: "svc", Auth: false}}

func TestAuth_ValidToken(t *testing.T) {
	r, captured := buildChain(t, protectedRoutes)
	w := do(r, "/api/x", bearer(t, "u1", "customer", time.Hour))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "u1", captured.userID)
	assert.Equal(t, "customer", captured.role)
}

func TestAuth_MissingToken(t *testing.T) {
	r, _ := buildChain(t, protectedRoutes)
	w := do(r, "/api/x", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "AUTH_001")
}

func TestAuth_ExpiredToken(t *testing.T) {
	r, _ := buildChain(t, protectedRoutes)
	tok, err := jwt.SignHS256(secret, jwt.NewClaims("u", "c", issuer, aud, -time.Minute, time.Now().Add(-time.Hour)))
	require.NoError(t, err)
	w := do(r, "/api/x", "Bearer "+tok)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "AUTH_003")
}

func TestAuth_NonBearerScheme(t *testing.T) {
	r, _ := buildChain(t, protectedRoutes)
	w := do(r, "/api/x", "Basic abc")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuth_PublicRouteSkipsAuth(t *testing.T) {
	r, _ := buildChain(t, publicRoutes)
	w := do(r, "/api/x", "")
	assert.Equal(t, http.StatusOK, w.Code)
}
