package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nbe-group/apigateway/internal/health"
	"github.com/nbe-group/apigateway/internal/service_registry"
)

func init() { gin.SetMode(gin.TestMode) }

func TestChecker_MarksHealthyAndUnhealthy(t *testing.T) {
	var up atomic.Bool
	up.Store(true)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if up.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer backend.Close()

	reg, err := service_registry.NewStatic(map[string]string{"svc": backend.URL})
	require.NoError(t, err)

	checker := health.NewChecker(reg, reg, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checker.Start(ctx)
	defer checker.Stop()

	// Initially healthy.
	require.Eventually(t, func() bool {
		_, err := reg.Resolve(ctx, "svc")
		return err == nil
	}, time.Second, 10*time.Millisecond)

	// Flip the backend down; the checker should flip the registry.
	up.Store(false)
	require.Eventually(t, func() bool {
		_, err := reg.Resolve(ctx, "svc")
		return err != nil
	}, time.Second, 10*time.Millisecond)
}

func TestChecker_Handler(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	reg, err := service_registry.NewStatic(map[string]string{"svc": backend.URL})
	require.NoError(t, err)

	r := gin.New()
	r.GET("/health", health.NewChecker(reg, reg, time.Minute).Handler())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "svc")
}

func TestLivenessHandler(t *testing.T) {
	r := gin.New()
	r.GET("/live", health.LivenessHandler())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/live", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}
