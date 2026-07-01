package service_registry

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"sync"

	apperrors "github.com/nbe-group/apigateway/internal/errors"
)

// StaticRegistry is a ServiceRegistry backed by static configuration. Each
// service currently maps to exactly one upstream URL, but the internal model
// already stores a slice of instances so adding multiple instances per service
// (and a balancing policy) is a localized change.
type StaticRegistry struct {
	mu        sync.RWMutex
	instances map[string][]*Instance
}

// compile-time assertion that StaticRegistry satisfies the interface.
var _ ServiceRegistry = (*StaticRegistry)(nil)

// NewStatic builds a registry from a name->URL map (as produced from config).
func NewStatic(services map[string]string) (*StaticRegistry, error) {
	reg := &StaticRegistry{instances: make(map[string][]*Instance, len(services))}
	for name, raw := range services {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("service %q: invalid url %q: %w", name, raw, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("service %q: url %q must include scheme and host", name, raw)
		}
		reg.instances[name] = []*Instance{{Service: name, URL: u, Healthy: true}}
	}
	return reg, nil
}

// Resolve returns the first healthy instance for the service. With a single
// instance this is trivial; the health filter is what a load balancer would
// hook into.
func (r *StaticRegistry) Resolve(_ context.Context, service string) (*Instance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	insts, ok := r.instances[service]
	if !ok {
		return nil, apperrors.ErrServiceUnknown.WithMessage("service %q is not registered", service)
	}
	for _, inst := range insts {
		if inst.Healthy {
			return inst, nil
		}
	}
	return nil, apperrors.ErrServiceNoTarget.WithMessage("no healthy instance for service %q", service)
}

// Instances returns a copy of the known instances for a service.
func (r *StaticRegistry) Instances(_ context.Context, service string) ([]*Instance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	insts, ok := r.instances[service]
	if !ok {
		return nil, apperrors.ErrServiceUnknown.WithMessage("service %q is not registered", service)
	}
	// Return value snapshots, not the live pointers: SetHealth mutates the
	// stored instances under the write lock, so handing out shared pointers
	// would let callers read Healthy concurrently with a write (data race).
	out := make([]*Instance, len(insts))
	for i, inst := range insts {
		cp := *inst
		out[i] = &cp
	}
	return out, nil
}

// Services returns all registered service names, sorted for stable output.
func (r *StaticRegistry) Services() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.instances))
	for name := range r.instances {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SetHealth updates the health flag of all instances of a service. Used by the
// background health checker (see internal/health).
func (r *StaticRegistry) SetHealth(service string, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, inst := range r.instances[service] {
		inst.Healthy = healthy
	}
}
