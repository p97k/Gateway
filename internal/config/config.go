// Package config defines the gateway's configuration schema and loading logic.
//
// All tunable behavior is configuration-driven so the gateway binary is
// generic: routes, services, auth requirements, and rate limits all come from
// YAML (overridable by environment variables). This is what makes the gateway
// reusable across environments without recompilation.
package config

import "time"

// Config is the root configuration document.
type Config struct {
	Server    ServerConfig             `mapstructure:"server"`
	JWT       JWTConfig                `mapstructure:"jwt"`
	Routes    []RouteConfig            `mapstructure:"routes"`
	Services  map[string]ServiceConfig `mapstructure:"services"`
	RateLimit RateLimitConfig          `mapstructure:"rate_limit"`
	Logging   LoggingConfig            `mapstructure:"logging"`
	Tracing   TracingConfig            `mapstructure:"tracing"`
	Metrics   MetricsConfig            `mapstructure:"metrics"`
}

// ServerConfig controls the gateway's own HTTP listener.
type ServerConfig struct {
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}

// JWTConfig configures token validation. Algorithm selects the signing scheme;
// only HS256 is wired today, but the field exists so RS256 can be enabled by
// configuration once the verifier supports it (see pkg/jwt).
type JWTConfig struct {
	Algorithm string `mapstructure:"algorithm"`  // HS256 (default) | RS256
	Secret    string `mapstructure:"secret"`     // HMAC secret for HS256
	PublicKey string `mapstructure:"public_key"` // PEM public key for RS256 (future)
	Issuer    string `mapstructure:"issuer"`
	Audience  string `mapstructure:"audience"`
}

// RouteConfig describes one routing rule. The first route whose Prefix matches
// the request path wins (longest-prefix ordering is applied at load time).
type RouteConfig struct {
	Prefix      string   `mapstructure:"prefix"`       // e.g. /api/products
	Service     string   `mapstructure:"service"`      // key into Services
	Auth        bool     `mapstructure:"auth"`         // require a valid JWT
	Roles       []string `mapstructure:"roles"`        // allowed roles (empty = any authenticated)
	StripPrefix bool     `mapstructure:"strip_prefix"` // strip Prefix before forwarding
}

// ServiceConfig describes a registered upstream service.
type ServiceConfig struct {
	URL string `mapstructure:"url"`
}

// RateLimitConfig groups the three rate-limiting scopes. Each scope can be
// independently enabled; a request must pass every enabled scope.
type RateLimitConfig struct {
	Global  LimitConfig `mapstructure:"global"`
	PerIP   LimitConfig `mapstructure:"per_ip"`
	PerUser LimitConfig `mapstructure:"per_user"`
}

// LimitConfig is a single token-bucket configuration. RPS is the sustained
// refill rate (tokens/second) and Burst is the bucket capacity.
type LimitConfig struct {
	Enabled bool    `mapstructure:"enabled"`
	RPS     float64 `mapstructure:"rps"`
	Burst   int     `mapstructure:"burst"`
}

// LoggingConfig controls the zap logger.
type LoggingConfig struct {
	Level  string `mapstructure:"level"`  // debug|info|warn|error
	Format string `mapstructure:"format"` // json|console
}

// TracingConfig controls OpenTelemetry. When Endpoint is empty an in-process
// (no-network) tracer is still installed so spans show up in tests/logs.
type TracingConfig struct {
	Enabled      bool    `mapstructure:"enabled"`
	ServiceName  string  `mapstructure:"service_name"`
	Endpoint     string  `mapstructure:"endpoint"`      // OTLP/HTTP collector endpoint
	SamplerRatio float64 `mapstructure:"sampler_ratio"` // 0..1
}

// MetricsConfig controls the Prometheus endpoint.
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
}
