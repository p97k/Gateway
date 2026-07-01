package config

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Load reads configuration from the given file path, applies environment
// variable overrides, validates it, and returns a normalized Config.
//
// Environment overrides use the prefix GATEWAY_ and underscore-separated keys,
// e.g. GATEWAY_SERVER_PORT=9090 or GATEWAY_JWT_SECRET=xxxx. This lets secrets
// stay out of the YAML file in production (12-factor style).
func Load(path string) (*Config, error) {
	v := viper.New()
	setDefaults(v)

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config %q: %w", path, err)
		}
	}

	// Environment overrides. GATEWAY_SERVER_PORT -> server.port
	v.SetEnvPrefix("GATEWAY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.read_timeout", 15*time.Second)
	v.SetDefault("server.write_timeout", 30*time.Second)
	v.SetDefault("server.idle_timeout", 60*time.Second)
	v.SetDefault("server.shutdown_timeout", 10*time.Second)

	v.SetDefault("jwt.algorithm", "HS256")

	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")

	v.SetDefault("tracing.enabled", true)
	v.SetDefault("tracing.service_name", "api-gateway")
	v.SetDefault("tracing.sampler_ratio", 1.0)

	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.path", "/metrics")
}

// normalize applies derived adjustments that should happen regardless of how
// the config was sourced: defaulting the algorithm and ordering routes so the
// most specific (longest) prefix is matched first.
func (c *Config) normalize() {
	if c.JWT.Algorithm == "" {
		c.JWT.Algorithm = "HS256"
	}
	c.JWT.Algorithm = strings.ToUpper(c.JWT.Algorithm)

	// Longest-prefix-first: prevents a broad rule like /api from shadowing a
	// more specific /api/admin rule regardless of YAML ordering.
	sort.SliceStable(c.Routes, func(i, j int) bool {
		return len(c.Routes[i].Prefix) > len(c.Routes[j].Prefix)
	})
}

// Validate enforces the invariants the rest of the gateway relies on, failing
// fast at startup instead of surfacing confusing runtime errors.
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port %d out of range", c.Server.Port)
	}

	switch c.JWT.Algorithm {
	case "HS256":
		if c.requiresAuth() && c.JWT.Secret == "" {
			return fmt.Errorf("jwt.secret is required for HS256 when any route has auth: true")
		}
	case "RS256":
		if c.JWT.PublicKey == "" {
			return fmt.Errorf("jwt.public_key is required for RS256")
		}
	default:
		return fmt.Errorf("unsupported jwt.algorithm %q (supported: HS256, RS256)", c.JWT.Algorithm)
	}

	for i, r := range c.Routes {
		if r.Prefix == "" {
			return fmt.Errorf("routes[%d]: prefix is required", i)
		}
		if !strings.HasPrefix(r.Prefix, "/") {
			return fmt.Errorf("routes[%d]: prefix %q must start with '/'", i, r.Prefix)
		}
		if r.Service == "" {
			return fmt.Errorf("routes[%d] (%s): service is required", i, r.Prefix)
		}
		if _, ok := c.Services[r.Service]; !ok {
			return fmt.Errorf("routes[%d] (%s): service %q is not defined under services", i, r.Prefix, r.Service)
		}
		if len(r.Roles) > 0 && !r.Auth {
			return fmt.Errorf("routes[%d] (%s): roles set but auth is false (roles require authentication)", i, r.Prefix)
		}
	}

	for name, s := range c.Services {
		if s.URL == "" {
			return fmt.Errorf("services[%s]: url is required", name)
		}
	}
	return nil
}

func (c *Config) requiresAuth() bool {
	for _, r := range c.Routes {
		if r.Auth {
			return true
		}
	}
	return false
}
