package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"

	"github.com/nbe-group/apigateway/internal/config"
	"github.com/nbe-group/apigateway/internal/middleware/ratelimit"
	"github.com/nbe-group/apigateway/internal/models"
	"github.com/nbe-group/apigateway/internal/transport"
)

func init() { gin.SetMode(gin.TestMode) }

// drive sends n requests through a one-handler engine and returns how many got 200.
func drive(t *testing.T, mw gin.HandlerFunc, n int) (allowed, limited int) {
	t.Helper()
	r := gin.New()
	r.Use(mw)
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	for i := 0; i < n; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:5555"
		r.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			allowed++
		} else if w.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	return
}

func TestGlobalLimit_BurstThenReject(t *testing.T) {
	l := ratelimit.New(config.RateLimitConfig{
		Global: config.LimitConfig{Enabled: true, RPS: 0.0001, Burst: 3},
	}, nil)
	defer l.Stop()

	allowed, limited := drive(t, l.PreAuth(), 10)
	assert.Equal(t, 3, allowed, "burst capacity should allow exactly 3")
	assert.Equal(t, 7, limited)
}

func TestPerIPLimit(t *testing.T) {
	l := ratelimit.New(config.RateLimitConfig{
		PerIP: config.LimitConfig{Enabled: true, RPS: 0.0001, Burst: 2},
	}, nil)
	defer l.Stop()

	allowed, limited := drive(t, l.PreAuth(), 5)
	assert.Equal(t, 2, allowed)
	assert.Equal(t, 3, limited)
}

func TestPerUserLimit(t *testing.T) {
	l := ratelimit.New(config.RateLimitConfig{
		PerUser: config.LimitConfig{Enabled: true, RPS: 0.0001, Burst: 2},
	}, nil)
	defer l.Stop()

	r := gin.New()
	// Seed an authenticated principal, then apply the per-user limiter.
	r.Use(func(c *gin.Context) {
		transport.SetClaims(c, &models.Claims{RegisteredClaims: gojwt.RegisteredClaims{Subject: "user-42"}})
		c.Next()
	})
	r.Use(l.PerUser())
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	var allowed, limited int
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		switch w.Code {
		case http.StatusOK:
			allowed++
		case http.StatusTooManyRequests:
			limited++
		}
	}
	assert.Equal(t, 2, allowed)
	assert.Equal(t, 3, limited)
}

func TestPerUser_AnonymousNotLimited(t *testing.T) {
	l := ratelimit.New(config.RateLimitConfig{
		PerUser: config.LimitConfig{Enabled: true, RPS: 0.0001, Burst: 1},
	}, nil)
	defer l.Stop()

	allowed, limited := drive(t, l.PerUser(), 5) // no claims set => not per-user limited
	assert.Equal(t, 5, allowed)
	assert.Equal(t, 0, limited)
}

func TestDisabledLimit_AllowsAll(t *testing.T) {
	l := ratelimit.New(config.RateLimitConfig{}, nil)
	defer l.Stop()

	allowed, limited := drive(t, l.PreAuth(), 50)
	assert.Equal(t, 50, allowed)
	assert.Equal(t, 0, limited)
}
