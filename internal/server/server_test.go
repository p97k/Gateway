package server_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nbe-group/apigateway/internal/config"
	"github.com/nbe-group/apigateway/internal/server"
)

// pickPort reserves a free TCP port and returns it (the listener is closed, so
// there is a small race, but it is fine for a single test process).
func pickPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func itoa(i int) string { return strconv.Itoa(i) }

// TestServer_NewRunShutdown exercises the full lifecycle: construct from config,
// run (binding an ephemeral port), serve one request, then shut down via context
// cancellation.
func TestServer_NewRunShutdown(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: pickPort(t), ShutdownTimeout: 2 * time.Second},
		JWT:      config.JWTConfig{Algorithm: "HS256", Secret: "s", Issuer: "i", Audience: "a"},
		Routes:   []config.RouteConfig{{Prefix: "/api/public", Service: "svc", Auth: false}},
		Services: map[string]config.ServiceConfig{"svc": {URL: backend.URL}},
		Logging:  config.LoggingConfig{Level: "error", Format: "json"},
		Tracing:  config.TracingConfig{Enabled: false},
		Metrics:  config.MetricsConfig{Enabled: true, Path: "/metrics"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	srv, err := server.New(ctx, cfg)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()

	base := "http://127.0.0.1:" + itoa(cfg.Server.Port)
	// Wait for the listener to come up, then hit a public route and /health.
	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/health")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 3*time.Second, 25*time.Millisecond)

	resp, err := http.Get(base + "/api/public/info")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	cancel() // trigger graceful shutdown
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down in time")
	}
}

func TestServer_New_InvalidJWTConfig(t *testing.T) {
	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8080},
		JWT:      config.JWTConfig{Algorithm: "RS256", PublicKey: "not-a-key"},
		Services: map[string]config.ServiceConfig{},
		Logging:  config.LoggingConfig{Level: "error"},
	}
	_, err := server.New(context.Background(), cfg)
	assert.Error(t, err)
}
