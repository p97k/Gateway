// Package service_registry abstracts how the gateway resolves a logical
// service name (e.g. "product-service") to a concrete upstream URL.
//
// The ServiceRegistry interface is the seam that lets the gateway evolve from
// static configuration to dynamic discovery (Consul, Kubernetes EndpointSlices,
// etcd) without any change to the proxy or router. Today only StaticRegistry
// exists; future implementations satisfy the same contract.
package service_registry

import (
	"context"
	"net/url"
)

// Instance is a single resolved upstream endpoint. Modeling resolution as a
// list of instances (rather than a single URL) is deliberate: it leaves room
// for load balancing and health-aware selection without an interface change.
type Instance struct {
	Service string
	URL     *url.URL
	Healthy bool
}

// ServiceRegistry resolves a service name to a usable upstream instance.
type ServiceRegistry interface {
	// Resolve returns one instance to route to. Implementations own the
	// selection policy (round-robin, least-conn, random, health-aware).
	Resolve(ctx context.Context, service string) (*Instance, error)

	// Instances returns all known instances for a service, primarily for
	// health checking and observability.
	Instances(ctx context.Context, service string) ([]*Instance, error)

	// Services lists all registered service names.
	Services() []string
}
