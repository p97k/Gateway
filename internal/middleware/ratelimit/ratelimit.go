// Package ratelimit implements token-bucket rate limiting with three scopes:
// global, per-IP and per-user, backed by golang.org/x/time/rate.
//
// Design note on ordering: identity is only known AFTER authentication, so the
// limiter is split into two middlewares from the same Limiter:
//
//	PreAuth() -> global + per-IP   (runs before auth: protects anonymous edge
//	             from floods / DoS, the main reason per-IP limiting exists)
//	PerUser() -> per-user          (runs after auth: needs the resolved user id)
//
// This keeps each bucket meaningful while respecting when its key is available.
// Swapping the in-memory buckets for Redis (distributed limiting) is a matter
// of providing an alternative store behind the same middleware methods.
package ratelimit

import (
	"github.com/gin-gonic/gin"

	"github.com/nbe-group/apigateway/internal/config"
	apperrors "github.com/nbe-group/apigateway/internal/errors"
	"github.com/nbe-group/apigateway/internal/metrics"
	"github.com/nbe-group/apigateway/internal/response"
	"github.com/nbe-group/apigateway/internal/transport"
)

// Scope labels for metrics.
const (
	scopeGlobal = "global"
	scopeIP     = "ip"
	scopeUser   = "user"
)

// Limiter holds the three scope buckets.
type Limiter struct {
	global  *bucket          // single shared bucket
	perIP   *keyedBuckets    // one bucket per client IP
	perUser *keyedBuckets    // one bucket per user id
	metrics *metrics.Metrics // optional; may be nil in tests
}

// New builds a Limiter from config. metrics may be nil.
func New(cfg config.RateLimitConfig, m *metrics.Metrics) *Limiter {
	l := &Limiter{metrics: m}
	if cfg.Global.Enabled {
		l.global = newBucket(cfg.Global.RPS, cfg.Global.Burst)
	}
	if cfg.PerIP.Enabled {
		l.perIP = newKeyedBuckets(cfg.PerIP.RPS, cfg.PerIP.Burst)
	}
	if cfg.PerUser.Enabled {
		l.perUser = newKeyedBuckets(cfg.PerUser.RPS, cfg.PerUser.Burst)
	}
	return l
}

// PreAuth enforces the global and per-IP buckets. Place before authentication.
func (l *Limiter) PreAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if l.global != nil && !l.global.allow() {
			l.reject(c, scopeGlobal)
			return
		}
		if l.perIP != nil && !l.perIP.allow(c.ClientIP()) {
			l.reject(c, scopeIP)
			return
		}
		c.Next()
	}
}

// PerUser enforces the per-user bucket. Place after authentication. Anonymous
// requests (no claims) are not per-user limited here — they were already
// covered by the per-IP bucket upstream.
func (l *Limiter) PerUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		if l.perUser != nil {
			if claims := transport.Claims(c); claims != nil && claims.UserID() != "" {
				if !l.perUser.allow(claims.UserID()) {
					l.reject(c, scopeUser)
					return
				}
			}
		}
		c.Next()
	}
}

func (l *Limiter) reject(c *gin.Context, scope string) {
	if l.metrics != nil {
		l.metrics.RateLimitReject.WithLabelValues(scope).Inc()
	}
	// Retry-After is advisory; clients/SDKs use it for backoff.
	c.Header("Retry-After", "1")
	response.Error(c, apperrors.ErrRateLimited.WithMessage("rate limit exceeded (%s)", scope))
}

// Stop releases background resources (cleanup goroutines) held by the keyed
// buckets. Call on shutdown.
func (l *Limiter) Stop() {
	if l.perIP != nil {
		l.perIP.stop()
	}
	if l.perUser != nil {
		l.perUser.stop()
	}
}
