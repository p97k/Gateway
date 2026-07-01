package server

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nbe-group/apigateway/internal/config"
	"github.com/nbe-group/apigateway/internal/health"
	"github.com/nbe-group/apigateway/internal/middleware/auth"
	"github.com/nbe-group/apigateway/internal/middleware/authorization"
	"github.com/nbe-group/apigateway/internal/middleware/logging"
	"github.com/nbe-group/apigateway/internal/middleware/ratelimit"
	"github.com/nbe-group/apigateway/internal/middleware/requestid"
	"github.com/nbe-group/apigateway/internal/middleware/tracing"
	"github.com/nbe-group/apigateway/internal/proxy"
	"github.com/nbe-group/apigateway/internal/router"
)

// BuildEngine assembles the gin engine and wires the middleware pipeline.
//
// The pipeline is the literal request lifecycle the gateway implements:
//
//	requestid → metrics → logging → tracing → [route match]
//	  → rate-limit(global,ip) → authenticate → rate-limit(user)
//	  → authorize → reverse-proxy → upstream
//
// Observability middleware (request id, metrics, logging, tracing) is global so
// it also covers the /health and /metrics endpoints. The gateway pipeline
// itself (matching → policy → proxy) is attached to NoRoute, so any path not
// explicitly handled by the gateway's own endpoints flows through it and is
// matched against the configured routes.
func BuildEngine(
	cfg *config.Config,
	deps Dependencies,
	limiter *ratelimit.Limiter,
	rproxy *proxy.Proxy,
	checker *health.Checker,
) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	// gin.Recovery converts panics into 500s instead of crashing the process.
	engine.Use(gin.Recovery())

	// --- Global observability (applies to every endpoint) ---------------
	engine.Use(requestid.New())
	engine.Use(deps.Metrics.Middleware())
	engine.Use(logging.New(deps.Logger))
	engine.Use(tracing.New())

	// --- Gateway management endpoints ------------------------------------
	engine.GET("/health", checker.Handler())
	engine.GET("/live", health.LivenessHandler())
	if cfg.Metrics.Enabled {
		path := cfg.Metrics.Path
		if path == "" {
			path = "/metrics"
		}
		engine.GET(path, gin.WrapH(promhttp.HandlerFor(deps.Gatherer, promhttp.HandlerOpts{})))
	}

	// --- The gateway proxy pipeline (everything else) --------------------
	matcher := router.NewMatcher(cfg.Routes)
	engine.NoRoute(
		matcher.Resolver(),                    // 1. resolve route (404 if none)
		limiter.PreAuth(),                     // 2. global + per-IP rate limit
		auth.New(deps.Verifier, deps.Metrics), // 3. authenticate (if route requires)
		limiter.PerUser(),                     // 4. per-user rate limit
		authorization.New(deps.Metrics),       // 5. authorize (if route restricts roles)
		rproxy.Handler(),                      // 6. reverse-proxy to upstream
	)

	return engine
}
