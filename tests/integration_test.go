// Package tests holds black-box integration tests that exercise the fully
// assembled gateway engine (the real middleware chain) against live httptest
// upstreams — the closest thing to running the gateway for real.
package tests

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/nbe-group/apigateway/internal/config"
	"github.com/nbe-group/apigateway/internal/health"
	"github.com/nbe-group/apigateway/internal/metrics"
	"github.com/nbe-group/apigateway/internal/middleware/ratelimit"
	"github.com/nbe-group/apigateway/internal/proxy"
	"github.com/nbe-group/apigateway/internal/server"
	"github.com/nbe-group/apigateway/internal/service_registry"
	apptransport "github.com/nbe-group/apigateway/internal/transport"
	"github.com/nbe-group/apigateway/pkg/jwt"
)

const (
	secret = "itest-secret"
	issuer = "auth"
	aud    = "api"
)

func init() { gin.SetMode(gin.TestMode) }

type harness struct {
	server  *httptest.Server
	backend *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Echo identity headers so we can assert propagation.
		w.Header().Set("X-Saw-User", r.Header.Get(apptransport.HeaderUserID))
		w.Header().Set("X-Saw-Role", r.Header.Get(apptransport.HeaderUserRole))
		w.Header().Set("X-Saw-Request", r.Header.Get(apptransport.HeaderRequestID))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(backend.Close)

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 0},
		JWT:    config.JWTConfig{Algorithm: "HS256", Secret: secret, Issuer: issuer, Audience: aud},
		Routes: []config.RouteConfig{
			{Prefix: "/api/admin", Service: "svc", Auth: true, Roles: []string{"admin"}},
			{Prefix: "/api/orders", Service: "svc", Auth: true, Roles: []string{"customer", "admin"}},
			{Prefix: "/api/products", Service: "svc", Auth: true},
			{Prefix: "/api/public", Service: "svc", Auth: false},
		},
		Services: map[string]config.ServiceConfig{"svc": {URL: backend.URL}},
		Metrics:  config.MetricsConfig{Enabled: true, Path: "/metrics"},
	}

	reg, err := service_registry.NewStatic(map[string]string{"svc": backend.URL})
	require.NoError(t, err)
	promReg := prometheus.NewRegistry()
	m := metrics.New(promReg)
	verifier, err := jwt.New(jwt.Config{Algorithm: "HS256", Secret: secret, Issuer: issuer, Audience: aud})
	require.NoError(t, err)
	rt := apptransport.New(apptransport.DefaultOptions())

	deps := server.Dependencies{
		Logger:    zap.NewNop(),
		Registry:  promReg,
		Gatherer:  promReg,
		Metrics:   m,
		Verifier:  verifier,
		Services:  reg,
		Transport: rt,
	}
	limiter := ratelimit.New(cfg.RateLimit, m)
	t.Cleanup(limiter.Stop)
	rproxy := proxy.New(reg, rt, m, zap.NewNop())
	checker := health.NewChecker(reg, reg, time.Minute)

	engine := server.BuildEngine(cfg, deps, limiter, rproxy, checker)
	gw := httptest.NewServer(engine)
	t.Cleanup(gw.Close)
	return &harness{server: gw, backend: backend}
}

// response is a small wrapper carrying the parts the assertions need.
type response struct {
	code   int
	body   string
	header http.Header
}

func (h *harness) do(method, path, token string) response {
	req, err := http.NewRequest(method, h.server.URL+path, nil)
	if err != nil {
		panic(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return response{code: resp.StatusCode, body: string(body), header: resp.Header}
}

func mint(t *testing.T, sub, role string) string {
	t.Helper()
	tok, err := jwt.SignHS256(secret, jwt.NewClaims(sub, role, issuer, aud, time.Hour, time.Now()))
	require.NoError(t, err)
	return tok
}

func TestIntegration_ProtectedRoute_NoToken_401(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodGet, "/api/products/1", "")
	assert.Equal(t, http.StatusUnauthorized, w.code)
}

func TestIntegration_ProtectedRoute_ValidToken_Proxies(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodGet, "/api/products/1", mint(t, "user-1", "customer"))
	assert.Equal(t, http.StatusOK, w.code)
	assert.JSONEq(t, `{"ok":true}`, w.body)
	// Identity propagation reached the backend.
	assert.Equal(t, "user-1", w.header.Get("X-Saw-User"))
	assert.Equal(t, "customer", w.header.Get("X-Saw-Role"))
	assert.NotEmpty(t, w.header.Get("X-Saw-Request"))
}

func TestIntegration_AdminRoute_ForbiddenForCustomer_403(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodGet, "/api/admin/x", mint(t, "user-1", "customer"))
	assert.Equal(t, http.StatusForbidden, w.code)
}

func TestIntegration_AdminRoute_AllowedForAdmin(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodGet, "/api/admin/x", mint(t, "admin-1", "admin"))
	assert.Equal(t, http.StatusOK, w.code)
}

func TestIntegration_PublicRoute_NoTokenNeeded(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodGet, "/api/public/info", "")
	assert.Equal(t, http.StatusOK, w.code)
}

func TestIntegration_UnknownRoute_404(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodGet, "/nope", "")
	assert.Equal(t, http.StatusNotFound, w.code)
	assert.Contains(t, w.body, "RTE_001")
}

func TestIntegration_HealthEndpoint(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodGet, "/health", "")
	assert.Equal(t, http.StatusOK, w.code)
	assert.Contains(t, w.body, "\"status\":\"ok\"")
}

func TestIntegration_MetricsEndpoint(t *testing.T) {
	h := newHarness(t)
	// Generate some traffic first.
	h.do(http.MethodGet, "/api/products/1", mint(t, "u", "customer"))
	w := h.do(http.MethodGet, "/metrics", "")
	assert.Equal(t, http.StatusOK, w.code)
	assert.Contains(t, w.body, "gateway_requests_total")
}
