// Package server is the gateway's composition root. It constructs every
// component from configuration, wires the middleware chain in the canonical
// order, and owns the HTTP server lifecycle (start + graceful shutdown).
//
// Keeping all wiring here (rather than scattered across packages) makes the
// dependency graph explicit and gives a single place to read the request
// pipeline top-to-bottom.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.uber.org/zap"

	"github.com/nbe-group/apigateway/internal/config"
	"github.com/nbe-group/apigateway/internal/health"
	"github.com/nbe-group/apigateway/internal/metrics"
	"github.com/nbe-group/apigateway/internal/middleware/ratelimit"
	"github.com/nbe-group/apigateway/internal/observability"
	"github.com/nbe-group/apigateway/internal/proxy"
	"github.com/nbe-group/apigateway/internal/service_registry"
	apptransport "github.com/nbe-group/apigateway/internal/transport"
	"github.com/nbe-group/apigateway/pkg/jwt"
)

// Server bundles the constructed gateway and its long-lived dependencies so the
// lifecycle (Run/shutdown) can release them cleanly.
type Server struct {
	cfg       *config.Config
	logger    *zap.Logger
	httpSrv   *http.Server
	limiter   *ratelimit.Limiter
	checker   *health.Checker
	traceStop observability.Shutdown
}

// Dependencies groups the concrete adapters built during construction. They are
// returned together so tests can construct a Server with substitutes.
type Dependencies struct {
	Logger    *zap.Logger
	Registry  prometheus.Registerer
	Gatherer  prometheus.Gatherer
	Metrics   *metrics.Metrics
	Verifier  jwt.Verifier
	Services  *service_registry.StaticRegistry
	Transport http.RoundTripper
}

// New builds a fully-wired Server from config.
func New(ctx context.Context, cfg *config.Config) (*Server, error) {
	logger, err := observability.NewLogger(cfg.Logging)
	if err != nil {
		return nil, fmt.Errorf("init logger: %w", err)
	}

	traceStop, err := observability.Setup(ctx, cfg.Tracing)
	if err != nil {
		logger.Warn("tracing setup failed; continuing without tracing", zap.Error(err))
		traceStop = func(context.Context) error { return nil }
	}

	// Prometheus registry (injectable, not the global default).
	promReg := prometheus.NewRegistry()
	promReg.MustRegister(collectors.NewGoCollector())
	promReg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := metrics.New(promReg)

	verifier, err := jwt.New(jwt.Config{
		Algorithm: cfg.JWT.Algorithm,
		Secret:    cfg.JWT.Secret,
		PublicKey: cfg.JWT.PublicKey,
		Issuer:    cfg.JWT.Issuer,
		Audience:  cfg.JWT.Audience,
	})
	if err != nil {
		return nil, fmt.Errorf("init jwt verifier: %w", err)
	}

	services := make(map[string]string, len(cfg.Services))
	for name, sc := range cfg.Services {
		services[name] = sc.URL
	}
	registry, err := service_registry.NewStatic(services)
	if err != nil {
		return nil, fmt.Errorf("init service registry: %w", err)
	}

	rt := apptransport.New(apptransport.DefaultOptions())
	limiter := ratelimit.New(cfg.RateLimit, m)
	rproxy := proxy.New(registry, rt, m, logger)
	checker := health.NewChecker(registry, registry, 10*time.Second)

	deps := Dependencies{
		Logger:    logger,
		Registry:  promReg,
		Gatherer:  promReg,
		Metrics:   m,
		Verifier:  verifier,
		Services:  registry,
		Transport: rt,
	}

	engine := BuildEngine(cfg, deps, limiter, rproxy, checker)

	srv := &Server{
		cfg:       cfg,
		logger:    logger,
		limiter:   limiter,
		checker:   checker,
		traceStop: traceStop,
		httpSrv: &http.Server{
			Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
			Handler:      engine,
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
			IdleTimeout:  cfg.Server.IdleTimeout,
		},
	}
	return srv, nil
}

// Run starts the HTTP server and the health checker, then blocks until ctx is
// cancelled (e.g. SIGINT/SIGTERM), at which point it shuts everything down
// gracefully within the configured timeout.
func (s *Server) Run(ctx context.Context) error {
	s.checker.Start(ctx)

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("gateway listening",
			zap.String("addr", s.httpSrv.Addr),
			zap.Int("routes", len(s.cfg.Routes)),
		)
		if err := s.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.logger.Info("shutdown signal received")
	}

	return s.shutdown()
}

func (s *Server) shutdown() error {
	timeout := s.cfg.Server.ShutdownTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	s.checker.Stop()
	s.limiter.Stop()

	err := s.httpSrv.Shutdown(shutdownCtx)
	if traceErr := s.traceStop(shutdownCtx); traceErr != nil {
		s.logger.Warn("tracer shutdown error", zap.Error(traceErr))
	}
	_ = s.logger.Sync()
	if err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}
	s.logger.Info("gateway stopped cleanly")
	return nil
}
