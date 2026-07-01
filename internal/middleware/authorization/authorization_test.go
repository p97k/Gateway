package authorization_test

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
	"github.com/nbe-group/apigateway/internal/middleware/authorization"
	"github.com/nbe-group/apigateway/internal/router"
	"github.com/nbe-group/apigateway/pkg/jwt"
)

func init() { gin.SetMode(gin.TestMode) }

const (
	secret = "test-secret"
	issuer = "auth"
	aud    = "api"
)

// adminRoute requires the admin role.
var adminRoute = []config.RouteConfig{{Prefix: "/api/admin", Service: "svc", Auth: true, Roles: []string{"admin"}}}

func buildChain(t *testing.T) *gin.Engine {
	t.Helper()
	v, err := jwt.New(jwt.Config{Algorithm: "HS256", Secret: secret, Issuer: issuer, Audience: aud})
	require.NoError(t, err)
	matcher := router.NewMatcher(adminRoute)
	r := gin.New()
	r.NoRoute(
		matcher.Resolver(),
		auth.New(v, nil),
		authorization.New(nil),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)
	return r
}

func token(t *testing.T, role string) string {
	t.Helper()
	tok, err := jwt.SignHS256(secret, jwt.NewClaims("u", role, issuer, aud, time.Hour, time.Now()))
	require.NoError(t, err)
	return "Bearer " + tok
}

func TestAuthz_AllowedRole(t *testing.T) {
	r := buildChain(t)
	w := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	rq.Header.Set("Authorization", token(t, "admin"))
	r.ServeHTTP(w, rq)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthz_ForbiddenRole(t *testing.T) {
	r := buildChain(t)
	w := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	rq.Header.Set("Authorization", token(t, "customer"))
	r.ServeHTTP(w, rq)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "AUTH_006")
}
