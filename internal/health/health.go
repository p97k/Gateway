// Package health provides the gateway's own liveness endpoint and an optional
// background checker that probes upstream services and reflects their status
// into the service registry.
//
// Marking unhealthy upstreams lets the registry's Resolve skip them — the seam
// a real load balancer would use to avoid routing to dead instances.
package health

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/nbe-group/apigateway/internal/response"
	"github.com/nbe-group/apigateway/internal/service_registry"
)

// HealthSetter is implemented by registries that can have their instance health
// updated externally (e.g. StaticRegistry). Dynamic registries that learn
// health from their backing system (Consul, K8s) simply won't provide one.
type HealthSetter interface {
	SetHealth(service string, healthy bool)
}

// Checker periodically probes each service's /health endpoint.
type Checker struct {
	registry service_registry.ServiceRegistry
	setter   HealthSetter
	client   *http.Client
	interval time.Duration
	path     string

	done    chan struct{}
	stopOne sync.Once
}

// NewChecker builds a Checker. setter may be nil (then probes still run and are
// observable via logs/handlers but won't flip registry health).
func NewChecker(reg service_registry.ServiceRegistry, setter HealthSetter, interval time.Duration) *Checker {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &Checker{
		registry: reg,
		setter:   setter,
		client:   &http.Client{Timeout: 3 * time.Second},
		interval: interval,
		path:     "/health",
		done:     make(chan struct{}),
	}
}

// Start launches the probe loop until Stop is called or ctx is cancelled.
func (c *Checker) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		c.probeAll(ctx) // probe immediately on start
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.done:
				return
			case <-ticker.C:
				c.probeAll(ctx)
			}
		}
	}()
}

// Stop halts the probe loop.
func (c *Checker) Stop() { c.stopOne.Do(func() { close(c.done) }) }

func (c *Checker) probeAll(ctx context.Context) {
	for _, service := range c.registry.Services() {
		instances, err := c.registry.Instances(ctx, service)
		if err != nil {
			continue
		}
		healthy := false
		for _, inst := range instances {
			if c.probe(ctx, inst.URL.String()+c.path) {
				healthy = true
				break
			}
		}
		if c.setter != nil {
			c.setter.SetHealth(service, healthy)
		}
	}
}

func (c *Checker) probe(ctx context.Context, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// Handler returns the gateway's own /health handler. It reports the gateway as
// up and lists the registered services with their last-known health, so an
// operator (or an orchestrator probe) gets a useful at-a-glance view.
func (c *Checker) Handler() gin.HandlerFunc {
	return func(g *gin.Context) {
		services := map[string]string{}
		for _, name := range c.registry.Services() {
			insts, err := c.registry.Instances(g.Request.Context(), name)
			status := "unknown"
			if err == nil {
				status = "down"
				for _, inst := range insts {
					if inst.Healthy {
						status = "up"
						break
					}
				}
			}
			services[name] = status
		}
		response.OK(g, http.StatusOK, gin.H{
			"status":   "ok",
			"services": services,
		})
	}
}

// LivenessHandler is a minimal always-200 handler for orchestrator liveness
// probes that should not depend on upstream health.
func LivenessHandler() gin.HandlerFunc {
	return func(g *gin.Context) {
		response.OK(g, http.StatusOK, gin.H{"status": "ok"})
	}
}
