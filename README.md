# Production-Grade API Gateway in Go

A learning-oriented but production-shaped **API Gateway** that sits between
clients and backend services and implements the core capabilities of gateways
like Kong, Spring Cloud Gateway, APISIX and Envoy:

> **Authentication · Authorization · Routing · Reverse Proxying · Rate Limiting ·
> Structured Logging · Metrics · Distributed Tracing · Request Validation ·
> Request-Context Propagation**

The gateway contains **no business logic**. It is a generic, configuration-driven
edge that authenticates, authorizes, rate-limits, observes, and forwards.

```
            ┌─────────────────────────── API Gateway ───────────────────────────┐
 Client ───▶│ requestid ▸ metrics ▸ logging ▸ tracing ▸ route-match ▸ rate-limit │───▶ Product Service
            │ ▸ authenticate ▸ rate-limit(user) ▸ authorize ▸ reverse-proxy      │───▶ Order Service
            └────────────────────────────────────────────────────────────────────┘───▶ User Service
```

---

## Table of Contents

1. [Architecture](#architecture)
2. [Project Layout](#project-layout)
3. [Request Lifecycle](#request-lifecycle)
4. [Authentication Flow](#authentication-flow)
5. [Authorization Flow](#authorization-flow)
6. [Reverse Proxy Flow](#reverse-proxy-flow)
7. [Rate Limiting](#rate-limiting)
8. [Observability](#observability-logging-metrics-tracing)
9. [Configuration](#configuration)
10. [Error Handling](#error-handling)
11. [Running Locally](#running-locally)
12. [Running with Docker](#running-with-docker)
13. [Testing](#testing)
14. [Key Design Decisions](#key-design-decisions)
15. [Future Extensions](#future-extensions)

---

## Architecture

The codebase follows **Clean Architecture** and **SOLID** principles. The guiding
rule is the **Dependency Rule**: source dependencies point *inward*, toward
stable abstractions. Concrete infrastructure (JWT scheme, service discovery,
rate-limit storage) sits at the edges behind interfaces, so it can be swapped
without touching the core.

```
                       ┌──────────────────────────────────────────┐
                       │                 cmd/gateway                │  composition entrypoint
                       └───────────────────────┬────────────────────┘
                                                │
                       ┌────────────────────────▼────────────────────┐
                       │              internal/server                  │  composition root:
                       │   (builds the engine + middleware chain)      │  wires everything
                       └───┬───────────────┬───────────────┬──────────┘
                           │               │               │
        ┌──────────────────▼──┐   ┌────────▼─────────┐   ┌─▼──────────────────┐
        │   middleware/*       │   │   router         │   │   proxy            │
        │ requestid, logging,  │   │ prefix matching  │   │ httputil reverse   │
        │ tracing, ratelimit,  │   │ + matched-route  │   │ proxy + header     │
        │ auth, authorization  │   │   context        │   │ injection          │
        └──────┬───────────────┘   └───────┬──────────┘   └────────┬───────────┘
               │ depend on interfaces only  │                       │
        ┌──────▼────────────────────────────▼───────────────────────▼──────────┐
        │                          Abstractions (interfaces)                     │
        │   jwt.Verifier   service_registry.ServiceRegistry   metrics.Metrics    │
        └──────┬───────────────────────┬──────────────────────────┬─────────────┘
               │                        │                          │
        ┌──────▼──────┐        ┌────────▼─────────┐       ┌────────▼─────────┐
        │ HS256 / RS256│        │ StaticRegistry   │       │ Prometheus impl  │
        │  verifier    │        │ (Consul/K8s...)  │       │                  │
        └──────────────┘        └──────────────────┘       └──────────────────┘

        models · errors · response · transport   ← dependency-free core types
```

**Why these seams matter**

| Interface | Implemented today | Drop-in future implementations (no caller changes) |
|---|---|---|
| `jwt.Verifier` | HS256 (`hmacProvider`) | RS256 (already wired), ES256, JWKS rotation |
| `service_registry.ServiceRegistry` | `StaticRegistry` (config) | Consul, Kubernetes, etcd, multi-instance + load balancing |
| rate-limit store | in-memory token buckets | Redis (distributed limiting) behind the same middleware |

---

## Project Layout

```
cmd/
  gateway/        gateway entrypoint (loads config, runs server)
  mockservice/    one configurable backend → product/order/user services
  token/          dev helper to mint demo HS256 JWTs
internal/
  config/         YAML + env config schema, loading, validation
  middleware/
    requestid/    correlation id generation/propagation
    logging/      structured zap access logging
    tracing/      OpenTelemetry root span ("Gateway Request")
    ratelimit/    token-bucket limiting (global / per-IP / per-user)
    auth/         JWT authentication
    authorization/ role-based access control
  router/         config-driven longest-prefix matching + matched-route context
  proxy/          net/http/httputil reverse proxy + header injection
  service_registry/ ServiceRegistry interface + StaticRegistry
  metrics/        Prometheus collectors + recording middleware
  observability/  zap logger + OTel tracer setup
  health/         /health endpoint + background upstream health checker
  models/         shared dependency-free types (Claims)
  response/       standard JSON success/error envelopes
  errors/         typed error catalog (stable codes)
  transport/      header names, request-context helpers, tuned RoundTripper
  server/         composition root: engine assembly + lifecycle
pkg/
  jwt/            reusable JWT verifier abstraction (HS256 + RS256)
configs/          config.yaml (local) + config.docker.yaml
deployments/      prometheus + grafana provisioning
tests/            black-box integration tests (full engine)
```

---

## Request Lifecycle

Every request flows through this pipeline, wired in
[`internal/server/engine.go`](internal/server/engine.go):

```
        ┌────────────────────────────────────────────────────────────────────────┐
        │  1. RequestID    generate/propagate X-Request-Id                         │
        │  2. Metrics      start latency timer (wraps everything below)            │
        │  3. Logging      capture start time; emit one structured line at the end │
        │  4. Tracing      extract inbound trace; open "Gateway Request" span      │
        │  ───────────────────────────────────────────────────────────────────── │
        │  5. Route match  resolve path → route config (404 if none)               │
        │  6. Rate limit   global + per-IP buckets   (pre-auth: DoS protection)    │
        │  7. Authenticate validate JWT IF route.auth   → store claims on context  │
        │  8. Rate limit   per-user bucket          (post-auth: needs identity)    │
        │  9. Authorize    role check IF route.roles    → 403 on mismatch          │
        │ 10. Reverse proxy resolve upstream, inject headers, forward, stream back │
        └────────────────────────────────────────────────────────────────────────┘
                                          │
                                          ▼
                              Backend Service (8081/8082/8083)
```

A short explanation of two ordering decisions that are easy to get wrong:

- **Route matching runs *before* auth/authz** (step 5). Authentication and
  authorization are *per-route* policies (`auth: true`, `roles: [...]`), so the
  pipeline must know which route matched before it can apply them. The actual
  *proxying* is still the terminal step (10).
- **Rate limiting is split around authentication.** The global + per-IP buckets
  run *before* auth (step 6) because their job is to protect the edge from
  anonymous floods — you must reject before doing expensive work. The per-user
  bucket runs *after* auth (step 8) because the user identity only exists once
  the token is validated.

---

## Authentication Flow

Implemented in [`internal/middleware/auth`](internal/middleware/auth/auth.go),
backed by the [`pkg/jwt`](pkg/jwt/jwt.go) verifier abstraction.

```
   Request ──▶ matched route.auth == true ?
                   │ no → continue (public route)
                   │ yes
                   ▼
        Authorization: Bearer <token>
                   │ missing / wrong scheme → 401 AUTH_001 / AUTH_002
                   ▼
        jwt.Verifier.Verify(token)
           ├─ signature  (HS256 today; RS256 ready)   → invalid → 401 AUTH_002
           ├─ expiration (exp required)               → expired → 401 AUTH_003
           ├─ issuer     (iss == config)              → bad iss → 401 AUTH_004
           └─ audience   (aud == config)              → bad aud → 401 AUTH_005
                   │ ok
                   ▼
        decode claims { sub, role }  →  store on request context
                   ▼
                continue
```

**Claims** are `{ "sub": "123", "role": "customer" }`, decoded into
[`models.Claims`](internal/models/claims.go) and stored on the request context
via [`transport.SetClaims`](internal/transport/context.go). They are later read
by authorization and by the proxy (for header injection).

**Algorithm extensibility (HS256 → RS256).** The verifier depends on a private
`keyProvider` strategy: `hmacProvider` (HS256) and `rsaProvider` (RS256) are both
implemented. The parser is locked to the expected signing method
(`jwt.WithValidMethods`), which defends against **algorithm-confusion** and
**`alg: none`** attacks. Switching to RS256 is purely configuration:

```yaml
jwt:
  algorithm: RS256
  public_key: |
    -----BEGIN PUBLIC KEY-----
    ...
    -----END PUBLIC KEY-----
```

---

## Authorization Flow

Implemented in [`internal/middleware/authorization`](internal/middleware/authorization/authorization.go).
Authorization is **route-level and configuration-driven**:

```yaml
routes:
  - prefix: /api/admin
    service: user-service
    auth: true
    roles: [admin]          # only role=admin may pass
```

```
   Request ──▶ matched route has roles ?
                   │ no  → continue (any authenticated user allowed)
                   │ yes
                   ▼
        claims.role ∈ route.roles ?
                   ├─ yes → continue
                   └─ no  → 403 AUTH_006 (authorization failure metric++)
```

The config validator guarantees that any route with `roles` also has
`auth: true`, so a roles-restricted route can never be reached anonymously.

---

## Reverse Proxy Flow

Implemented in [`internal/proxy`](internal/proxy/proxy.go) using
`net/http/httputil.ReverseProxy`.

```
   matched route.service ("product-service")
            │
            ▼
   ServiceRegistry.Resolve(service) ──▶ healthy *url.URL (e.g. http://product-service:8081)
            │                                │ unknown service → 502 RTE_002
            │                                │ no healthy inst → 503 RTE_003
            ▼
   Build outbound request (Rewrite):
       • scheme/host         ← upstream
       • path                ← incoming path (optionally strip_prefix)
       • query string        ← preserved
       • method, body        ← preserved
       • X-Forwarded-* set
       • INJECT  X-Request-Id, X-User-Id, X-User-Role
       • traceparent injected automatically by instrumented transport
            │
            ▼
   Stream upstream response (status + headers + body) back to client unchanged
       • transport failure   → 502 PRX_001
       • upstream timeout     → 504 PRX_002
       • client disconnect    → silently dropped (no error response)
```

A **single** `ReverseProxy` instance is shared across requests (it is
concurrency-safe); per-request data (target, identity headers) is passed via the
request context, avoiding a proxy allocation per call. The tuned
[`transport`](internal/transport/transport.go) provides connection pooling and
wraps the `RoundTripper` in OpenTelemetry instrumentation.

---

## Rate Limiting

Token-bucket limiting (via `golang.org/x/time/rate`) with three independent
scopes — a request must pass **every** enabled scope.

| Scope | Key | Where it runs | Purpose |
|---|---|---|---|
| `global` | the whole gateway | pre-auth | total throughput ceiling |
| `per_ip` | client IP | pre-auth | anonymous flood / DoS protection |
| `per_user` | `sub` claim | post-auth | fair-use per authenticated user |

Each scope has a sustained rate (`rps`, the bucket refill) and a `burst` (the
bucket capacity). Exceeding a bucket returns **429 `RATE_001`** with a
`Retry-After` header, and increments `gateway_rate_limit_rejections_total{scope}`.
Per-key buckets are evicted after 10 minutes of inactivity so memory stays
bounded on a public edge.

---

## Observability (Logging, Metrics, Tracing)

**Structured logging** ([`zap`](internal/middleware/logging/logging.go)) — one
JSON line per request:

```json
{"level":"info","ts":"2026-06-15T12:00:00Z","msg":"request","request_id":"abc",
 "method":"GET","path":"/api/products","status":200,"latency_ms":45,
 "service":"product-service","user_id":"123","user_role":"customer"}
```

**Metrics** ([Prometheus](internal/metrics/metrics.go)) exposed at `/metrics`:

| Metric | Type | Labels |
|---|---|---|
| `gateway_requests_total` | counter | method, service, status |
| `gateway_request_duration_seconds` | histogram | method, service, status |
| `gateway_errors_total` | counter | service, code |
| `gateway_rate_limit_rejections_total` | counter | scope |
| `gateway_authentication_failures_total` | counter | reason (error code) |
| `gateway_authorization_failures_total` | counter | service |
| `gateway_upstream_errors_total` | counter | service |

> Cardinality is controlled by labeling on the **matched service**, never the raw
> path — so path parameters and query strings can't blow up the metric series.

**Tracing** ([OpenTelemetry](internal/observability/tracing.go)) — a span tree
per request:

```
Gateway Request (server span)
├── Authentication
├── Authorization
└── Proxy Request ──(W3C traceparent)──▶ upstream service spans
```

Inbound `traceparent` is continued; outbound trace context is injected
automatically by the instrumented transport, giving end-to-end traces. With no
collector configured spans are still created (visible via the `X-Trace-Id`
response header); set `tracing.endpoint` to export to an OTLP/HTTP collector.

---

## Configuration

YAML with environment overrides (prefix `GATEWAY_`, dots → underscores, e.g.
`GATEWAY_JWT_SECRET`). See [`configs/config.yaml`](configs/config.yaml).

```yaml
server:
  port: 8080

jwt:
  algorithm: HS256
  secret: "secret123"        # inject via GATEWAY_JWT_SECRET in prod
  issuer: "auth-service"
  audience: "api"

routes:
  - prefix: /api/products
    service: product-service
    auth: true
  - prefix: /api/orders
    service: order-service
    auth: true
    roles: [customer, admin]
  - prefix: /api/admin
    service: user-service
    auth: true
    roles: [admin]

services:
  product-service: { url: http://localhost:8081 }
  order-service:   { url: http://localhost:8082 }
  user-service:    { url: http://localhost:8083 }

rate_limit:
  global:   { enabled: true, rps: 1000, burst: 2000 }
  per_ip:   { enabled: true, rps: 50,   burst: 100 }
  per_user: { enabled: true, rps: 20,   burst: 40 }

logging: { level: info, format: json }
tracing: { enabled: true, service_name: api-gateway, endpoint: "", sampler_ratio: 1.0 }
metrics: { enabled: true, path: /metrics }
```

Configuration is **validated at startup** (port range, JWT algorithm/secret,
that every route references a defined service, that `roles` implies `auth`, etc.)
so misconfiguration fails fast instead of at runtime. Routes are sorted
longest-prefix-first at load, so ordering in the file is irrelevant.

---

## Error Handling

All gateway-originated responses use one envelope (see
[`internal/response`](internal/response/response.go)):

```json
{ "success": false, "message": "Unauthorized", "code": "AUTH_001" }
```

Codes are stable and namespaced ([`internal/errors`](internal/errors/errors.go)):

| Prefix | Meaning | Examples |
|---|---|---|
| `AUTH_*` | auth / authz | `AUTH_001` missing token … `AUTH_006` forbidden |
| `RATE_*` | rate limiting | `RATE_001` too many requests |
| `RTE_*` | routing | `RTE_001` no route, `RTE_002` unknown service |
| `PRX_*` | upstream | `PRX_001` unavailable, `PRX_002` timeout |
| `GW_*` | internal | `GW_001` internal error |

Proxied (upstream) responses are passed through **untouched** — the gateway only
applies its envelope to errors it originates.

---

## Running Locally

Requires Go 1.25+.

```bash
# 1. Start the three mock backends (separate terminals, or background them)
SERVICE_NAME=product-service PORT=8081 go run ./cmd/mockservice &
SERVICE_NAME=order-service   PORT=8082 go run ./cmd/mockservice &
SERVICE_NAME=user-service    PORT=8083 go run ./cmd/mockservice &

# 2. Start the gateway
make run            # or: go run ./cmd/gateway -config configs/config.yaml

# 3. Mint a token and call through the gateway
TOKEN=$(make token ROLE=customer)          # or: go run ./cmd/token -role customer
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/products/42

# Admin-only route
ADMIN=$(go run ./cmd/token -role admin)
curl -H "Authorization: Bearer $ADMIN" http://localhost:8080/api/admin/anything

# No token → 401
curl -i http://localhost:8080/api/products

# Gateway introspection
curl http://localhost:8080/health
curl http://localhost:8080/metrics
```

---

## Running with Docker

```bash
docker compose up --build
```

Brings up the **gateway**, **product/order/user services**, **Prometheus**, and
**Grafana**:

| Component | URL |
|---|---|
| Gateway | http://localhost:8080 |
| Prometheus | http://localhost:9090 |
| Grafana (anon admin) | http://localhost:3000 → dashboard **API Gateway** |

```bash
TOKEN=$(go run ./cmd/token -role customer)
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/products
```

The Grafana datasource and an **API Gateway** dashboard (request rate, p95
latency, 5xx rate, rate-limit rejections, auth/authz failures) are provisioned
automatically from [`deployments/grafana`](deployments/grafana).

---

## Testing

```bash
make test         # all tests
make test-race    # with the race detector
make cover        # total coverage report
```

Coverage is **> 80%** (≈88%). The suite includes:

- **JWT tests** — valid/expired/bad-issuer/bad-audience/bad-signature/malformed,
  `alg: none` rejection, and a full **RS256 round-trip** + algorithm-confusion guard.
- **Config tests** — load, env override, longest-prefix sorting, validation failures.
- **Routing tests** — exact, sub-path, boundary-aware, wildcard, no-match.
- **Middleware tests** — request-id, authentication, authorization, rate limiting
  (global / per-IP / per-user / disabled).
- **Proxy tests** — request fidelity, header injection, prefix stripping, upstream-down.
- **Service registry tests** — resolve, unknown service, health gating.
- **Server lifecycle test** — construct → run (ephemeral port) → graceful shutdown.
- **Integration tests** ([`tests/`](tests/integration_test.go)) — the fully
  assembled engine against live `httptest` upstreams: 401/403/200 paths, identity
  propagation, 404, `/health`, `/metrics`.

---

## Key Design Decisions

- **Interfaces at every infrastructure seam** (JWT, service registry, metrics) so
  the gateway is open for extension, closed for modification (OCP) and the core
  depends on abstractions, not details (DIP).
- **Typed error catalog + single response envelope** — consistent client contract,
  no leaking of internal error strings, stable codes for dashboards/alerts.
- **Matched-route on the context** — route matching happens once; auth, authz and
  proxy all read the same decision instead of re-deriving it (single source of truth).
- **Shared, instrumented `RoundTripper`** — connection pooling (the dominant
  perf lever for an edge fanning onto few upstreams) and automatic trace propagation.
- **Injectable Prometheus registry** — tests assert on exact metric values with a
  fresh registry instead of the global default.
- **Composition root in `internal/server`** — the entire dependency graph is wired
  in one readable place; `cmd/gateway` stays a thin shell.
- **Graceful shutdown** — SIGINT/SIGTERM cancels a root context; in-flight requests
  drain within `shutdown_timeout`, background workers and the tracer flush cleanly.

---

## Future Extensions

The architecture was explicitly shaped to accommodate these without core rewrites:

| Feature | Where it plugs in |
|---|---|
| **RS256 JWT** | already implemented in `pkg/jwt` — enable via config |
| **JWKS / key rotation** | new `keyProvider` in `pkg/jwt` |
| **Consul / Kubernetes discovery** | new `ServiceRegistry` implementation |
| **Redis rate limiting** | alternative store behind the ratelimit middleware |
| **Load balancing** | `Instance` model is already a slice; add a selection policy in `Resolve` |
| **Circuit breaker / retries** | wrap the proxy `RoundTripper` |
| **Response caching / transformation** | `ReverseProxy.ModifyResponse` hook |
| **WebSocket proxying** | `ReverseProxy` upgrades transparently; add config opt-in |
| **Canary / Blue-Green** | weighted selection across instances in the registry |
| **Health-aware routing** | the background health checker already flips `Instance.Healthy` |

---

## License

Provided as an educational reference implementation.
