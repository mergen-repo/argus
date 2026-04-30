---
story: STORY-065
title: Observability & Tracing Standardization
phase: 10
effort: L
complexity: Medium-High
planner: amil-planner
plan_version: 1
zero_deferral: true
waves: 4
tasks: 18
acs: 11
---

# Implementation Plan: STORY-065 — Observability & Tracing Standardization

## Goal
Close Argus's observability gap in a single story by wiring OpenTelemetry tracing end-to-end (HTTP → pgx → NATS), migrating the custom Prometheus text output to `client_golang` with a proper metric taxonomy, adding `tenant_id` to logs + metric labels, committing six Grafana dashboards and nine Prometheus alert rules, and exposing operator health + circuit breaker state as gauges — zero deferrals, old code paths deleted not shadowed.

## Phase 10 Zero-Deferral Charter
- Every AC (AC-1..AC-11) closes in THIS story. No "TODO observability", no `fmt.Sprintf` metric remnants, no placeholder dashboard stubs.
- Prometheus migration is **delete-and-replace**: the hand-written text format handler (`internal/api/metrics/prometheus.go`) is removed from the tree, not kept behind a feature flag. Analytics collector + pusher + `/api/v1/system/metrics` JSON endpoint (WebSocket realtime push for dashboard) are **preserved** — they serve a different audience (admin UI realtime).
- Each Grafana dashboard is a real 4–6 panel deliverable with working PromQL — no empty placeholder JSON.
- Alert rules must pass `promtool check rules infra/prometheus/alerts.yml`.
- All tests run with `-race`.

## Architecture Context

### Scope (cross-cutting)
| Component | Layer | Files |
|-----------|-------|-------|
| OTel SDK bootstrap | `internal/observability` (new pkg) | `observability/otel.go`, `observability/otel_test.go` |
| Prometheus registry + metric definitions | `internal/observability/metrics` (new pkg) | `metrics/metrics.go`, `metrics/metrics_test.go` |
| HTTP instrumentation (otelhttp) | `internal/gateway` | `router.go`, `logging.go`, `http_metrics.go` (new), `http_metrics_test.go` (new) |
| DB instrumentation (otelpgx) | `internal/store` | `postgres.go`, `postgres_test.go` |
| NATS instrumentation (traceparent header) | `internal/bus` | `nats.go`, `nats_test.go` (new), all subscriber sites in `cmd/argus/main.go` |
| Redis instrumentation | `internal/cache` | `redis.go`, `redis_metrics.go` (new) |
| AAA metrics migration | `internal/aaa/radius`, `internal/observability/metrics` | `server.go`, `metrics/aaa_recorder.go` (new) |
| Operator health + breaker gauges | `internal/operator` | `health.go`, `circuit_breaker.go` |
| Job metrics | `internal/job` | `runner.go` (metric hook), adapter for duration/outcome |
| Config env vars | `internal/config` | `config.go`, `config_test.go` |
| Main wiring + graceful shutdown flush | `cmd/argus/main.go` | `main.go` |
| Route `/metrics` migration | `internal/gateway` | `router.go`, removal of `internal/api/metrics/prometheus.go` |
| Grafana dashboards | `infra/grafana/dashboards/` (new) | 6 JSON files |
| Prometheus alert rules | `infra/prometheus/alerts.yml` (new) | YAML |
| Docker Compose scrape config | `infra/prometheus/prometheus.yml` (new) | YAML |

### Data Flow — Full request with tracing + metrics
```
Incoming HTTP
    │
    ▼
otelhttp.NewHandler (root span created: http.method, http.route, http.status_code)
    │
    ▼
chi router
    │ correlation middleware: X-Request-ID UUID → context + response header + span attr
    │ CORS, security headers, rate limiter
    │ ZerologRequestLogger (captures timing + tenant_id from ctx post-handler)
    │ JWTAuth: sets ctx[tenant_id,user_id,role] AND span.SetAttributes(tenant.id, user.id)
    │ Handler
    ▼
Store (pgxpool with otelpgx Tracer)
    │ child span: db.system=postgresql, db.operation=SELECT, db.statement (sanitized), db.name=argus
    │ if duration > 100ms → span attribute argus.db.slow=true
    ▼
EventBus.PublishMsg (bus/nats.go)
    │ new: build nats.Msg with Header[traceparent] injected via otel TextMapPropagator
    │ child span: messaging.system=nats, messaging.destination=<subject>, messaging.operation=publish
    ▼
Consumer side (Subscribe wrapper)
    │ extract traceparent → otel.GetTextMapPropagator().Extract → new child span
    │ pass span ctx through handler

Exit:
    Response writer captures status, body size → ZerologRequestLogger logs
       (correlation_id, tenant_id, method, path, status, duration_ms, bytes)
    Prometheus middleware observes argus_http_request_duration_seconds and increments
       argus_http_requests_total{method,route,status,tenant_id}
```

### Dependency Decisions (IMPORTANT)
1. **otelpgx, not otelsql.** Argus uses `pgxpool.Pool` (pgx v5 native), not `database/sql`. `github.com/XSAM/otelsql` wraps `database/sql` and would require rewriting every store file. Use `github.com/exaring/otelpgx` which implements the pgx v5 `pgx.QueryTracer` interface and plugs into `pgxpool.Config.ConnConfig.Tracer`. Slow-query threshold applied via a wrapper QueryTracer that sets `argus.db.slow=true` when duration > 100ms.
2. **Prometheus metrics live in their OWN package** `internal/observability/metrics`, not in the existing `internal/analytics/metrics` package. The existing `analytics/metrics` (Collector, Pusher, types) serves a different purpose: Redis-based realtime aggregation pushed via WebSocket to the admin UI. It is **preserved**. Only `internal/api/metrics/prometheus.go` (the hand-written text handler) is deleted.
3. **AAA MetricsRecorder interface is extended, not replaced.** A new Prometheus-backed `MetricsRecorder` implementation is added. The RADIUS server is wired to a composite recorder that delegates to both (Redis collector for WS, Prometheus for scrape). The existing `collector.RecordAuth` stays untouched.
4. **otelhttp wraps the full chi router**, set at the top of `NewRouterWithDeps` by returning `otelhttp.NewHandler(chiRouter, "argus.http")` — but since chi routes are dynamic, route template extraction needs `chimiddleware.RouteContext` after routing. Approach: use `otelhttp` at the server boundary AND add an inner middleware `PrometheusHTTPMetrics` that, after `ServeHTTP`, reads `chi.RouteContext(r.Context()).RoutePattern()` to get the normalized route (e.g. `/api/v1/sims/{id}`), avoiding high-cardinality path labels.
5. **Default Prometheus registry + Go collectors.** Use a custom `*prometheus.Registry` (not `DefaultRegisterer`) so tests can create isolated registries. Register `collectors.NewGoCollector()` and `collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})` explicitly in `metrics.NewRegistry()`.
6. **tenant_id cardinality.** `tenant_id` label on counter/histogram metrics is acceptable because Argus is multi-operator single-tenant per DB row with a small-to-medium number of tenants (target fleet ~100s, not millions). Still, a defensive cap is applied: if `tenant_id` from context is empty/nil UUID, the label is set to `"unknown"` (not `""`), and an env var `OBS_METRICS_TENANT_LABEL_ENABLED=true` (default true) allows emergency disable without redeploy.

### Go Modules to Add (go.mod)
```
go.opentelemetry.io/otel                              v1.29.0
go.opentelemetry.io/otel/sdk                          v1.29.0
go.opentelemetry.io/otel/trace                        v1.29.0
go.opentelemetry.io/otel/exporters/otlp/otlptrace     v1.29.0
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.29.0
go.opentelemetry.io/otel/semconv/v1.26.0              v1.29.0
go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.54.0
github.com/exaring/otelpgx                            v0.8.0
github.com/prometheus/client_golang                   v1.20.4
```
Developer runs `go get` + `go mod tidy`; exact patch versions may float — the plan pins minor versions to match OTel v1.29 (Aug 2024) line, compatible with Go 1.25.

### Existing State to Preserve
- `internal/analytics/metrics/collector.go` — Redis-based counter aggregator used by WebSocket realtime dashboard pusher. **KEEP**.
- `internal/analytics/metrics/pusher.go` — 1s WS push loop. **KEEP**.
- `internal/analytics/metrics/types.go` — `SystemMetrics`, `RealtimePayload` structs. **KEEP**.
- `internal/api/metrics/handler.go` — `GetSystemMetrics` JSON envelope handler at `/api/v1/system/metrics`. **KEEP** (still used by admin UI System Health screen — STORY-033).
- `internal/api/metrics/handler_test.go` — tests for JSON envelope. **KEEP** the JSON test; **UPDATE** to remove assertions on `Prometheus()` method + delete tests that import the custom text format.
- `internal/gateway/correlation.go` — already generates UUID, sets `X-Request-ID`. **KEEP** unchanged; add span attribute propagation inside otelhttp middleware.

### Existing State to DELETE
- `internal/api/metrics/prometheus.go` — entire file removed.
- Corresponding test cases in `internal/api/metrics/handler_test.go` that assert `# HELP argus_…\n` text output — remove the TEXT-format test block, keep JSON-format tests.
- In `cmd/argus/main.go`: `deps.MetricsHandler.Prometheus` → replaced with `promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{...})`.

---

## Environment Variables (add to `internal/config/config.go`)

| Env Var | Default | Purpose |
|---------|---------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `` (tracing disabled if empty) | OTLP gRPC collector URL, e.g. `localhost:4317` |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` | Send OTLP without TLS (dev/compose) |
| `OTEL_SERVICE_NAME` | `argus` | Populates resource `service.name` |
| `OTEL_SERVICE_VERSION` | `dev` | Populates resource `service.version` (CI injects git SHA) |
| `OTEL_DEPLOYMENT_ENVIRONMENT` | `development` | Populates resource `deployment.environment` |
| `OTEL_TRACES_SAMPLER_RATIO` | `1.0` | Parent-based TraceIDRatioBased sampler (1.0=all, 0.1=10%) |
| `METRICS_ENABLED` | `true` | Master switch for Prometheus handler registration |
| `METRICS_TENANT_LABEL_ENABLED` | `true` | Emergency kill-switch to drop tenant_id label if cardinality explodes |
| `METRICS_HISTOGRAM_HTTP_BUCKETS` | `0.001,0.005,0.01,0.025,0.05,0.1,0.25,0.5,1,2.5,5,10` | HTTP latency buckets (s) |

Config loading: add `OTelConfig` nested struct or inline fields; validate via `Validate()`: if `OTEL_TRACES_SAMPLER_RATIO` outside [0,1] → error.

---

## Prometheus Metric Registry — Full Definition

All metrics live in `internal/observability/metrics/metrics.go`. The package exports a `Registry` type wrapping `*prometheus.Registry` plus typed accessors.

### Metric Definitions (all 15 ACs met + required extras)

```go
// AC-6: the full metric set
type Registry struct {
    Reg *prometheus.Registry

    HTTPRequestsTotal      *prometheus.CounterVec   // method, route, status, tenant_id
    HTTPRequestDuration    *prometheus.HistogramVec // method, route, tenant_id

    AAAAuthRequestsTotal   *prometheus.CounterVec   // protocol, operator_id, result, tenant_id
    AAAAuthLatency         *prometheus.HistogramVec // protocol, operator_id, tenant_id
    ActiveSessions         *prometheus.GaugeVec     // tenant_id, operator_id

    DBQueryDuration        *prometheus.HistogramVec // operation, table
    DBPoolConnections      *prometheus.GaugeVec     // state (idle|in_use|waiting)

    NATSPublishedTotal     *prometheus.CounterVec   // subject
    NATSConsumedTotal      *prometheus.CounterVec   // subject
    NATSPendingMessages    *prometheus.GaugeVec     // subject

    RedisOpsTotal          *prometheus.CounterVec   // op, result
    RedisCacheHitsTotal    *prometheus.CounterVec   // cache
    RedisCacheMissesTotal  *prometheus.CounterVec   // cache

    JobRunsTotal           *prometheus.CounterVec   // job_type, result
    JobDuration            *prometheus.HistogramVec // job_type

    OperatorHealth         *prometheus.GaugeVec     // operator_id  (0=down,1=degraded,2=healthy)
    CircuitBreakerState    *prometheus.GaugeVec     // operator_id, state (closed|open|half_open)
}
```

### Label-Set Rationale
- **HTTPRequestsTotal** labels `{method, route, status, tenant_id}`: `route` is `chi.RouteContext(r).RoutePattern()`, NOT raw path (avoids explosion). Cardinality bound: ~30 routes × 5 methods × 10 statuses × ~100 tenants ≈ 150k series upper bound, acceptable.
- **AAAAuthRequestsTotal** labels `{protocol, operator_id, result, tenant_id}`: `protocol ∈ {radius,diameter,5g-sba}`, `result ∈ {success,reject,error}`.
- **DBQueryDuration** labels `{operation, table}`: extracted from SQL by otelpgx or our wrapper — `operation ∈ {SELECT,INSERT,UPDATE,DELETE}`. Table derived via regex on SQL text.
- **OperatorHealth** value encoding: gauge value `0=down, 1=degraded, 2=healthy` — simpler PromQL than a state-labeled gauge for alerts.
- **CircuitBreakerState** uses per-state 0/1 gauge so `argus_circuit_breaker_state{state="open"} == 1` works directly.

### Histogram Buckets
- HTTP: `0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10` (s)
- AAA auth: `0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5` (s)
- DB query: `0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5` (s)
- Job duration: `0.5, 1, 5, 15, 30, 60, 120, 300, 600, 1800` (s)

---

## OTel Init — Structural Skeleton

File: `internal/observability/otel.go`

```go
package observability

import (
    "context"
    "time"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/propagation"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/resource"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type Config struct {
    ServiceName    string
    ServiceVersion string
    Environment    string
    OTLPEndpoint   string   // "" disables tracing
    Insecure       bool
    SamplerRatio   float64
}

// Init builds a TracerProvider, registers it as global, sets TraceContext
// propagator, and returns a shutdown func. If OTLPEndpoint is empty, returns
// a no-op provider and a no-op shutdown.
func Init(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
    // 1. Build resource with service.name/version, deployment.environment
    // 2. If endpoint empty → return noop tracer provider + noop shutdown
    // 3. Build OTLP gRPC exporter with dial options
    // 4. Build batcher TracerProvider with TraceIDRatioBased sampler
    // 5. otel.SetTracerProvider(tp) + otel.SetTextMapPropagator(propagation.TraceContext{})
    // 6. Return shutdown that flushes in <=5s
}
```

Graceful shutdown wiring in `main.go`:
```go
otelShutdown, err := observability.Init(ctx, observability.Config{...cfg})
if err != nil { log.Fatal(...) }
defer func() {
    ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
    defer c()
    _ = otelShutdown(ctx)
}()
```

---

## otelpgx Wiring — Structural Skeleton

File: `internal/store/postgres.go` — modify `NewPostgres`:

```go
import (
    "github.com/exaring/otelpgx"
    "github.com/jackc/pgx/v5/tracelog"
)

func NewPostgres(ctx context.Context, dsn string, maxConns, maxIdleConns int32, connMaxLife time.Duration) (*Postgres, error) {
    cfg, err := pgxpool.ParseConfig(dsn)
    // ... existing config ...

    // NEW: attach otelpgx tracer for spans + our slow-query decorator
    cfg.ConnConfig.Tracer = newCompositeTracer(
        otelpgx.NewTracer(
            otelpgx.WithTrimSQLInSpanName(),
            otelpgx.WithIncludeQueryParameters(false), // PII safety
        ),
        &slowQueryTracer{threshold: 100 * time.Millisecond},
    )
    // ... NewWithConfig(ctx, cfg)
}
```

`slowQueryTracer` implements `pgx.QueryTracer` and adds `argus.db.slow=true` span attribute when `duration > 100ms`. Full impl: ~40 LOC, Developer writes.

Also: in task 12, wire DB pool gauge updater — a goroutine that every 10s calls `pool.Stat()` and updates `DBPoolConnections` gauge with idle/in_use/waiting counts.

---

## NATS traceparent — Structural Skeleton

File: `internal/bus/nats.go` — modify publish + subscribe sides.

### Publish side
```go
func (eb *EventBus) Publish(ctx context.Context, subject string, payload interface{}) error {
    data, err := json.Marshal(payload)
    if err != nil { return err }

    msg := &nats.Msg{
        Subject: subject,
        Data:    data,
        Header:  nats.Header{},
    }
    // Inject W3C traceparent into msg.Header via otel propagator
    otel.GetTextMapPropagator().Inject(ctx, natsHeaderCarrier(msg.Header))

    _, err = eb.js.PublishMsg(ctx, msg)
    // metric: obs.NATSPublishedTotal.WithLabelValues(subject).Inc()
    return err
}
```

Where `natsHeaderCarrier` is a small adapter implementing `propagation.TextMapCarrier` on top of `nats.Header`.

### Subscribe side — signature compatibility
Current handler signature `func(subject string, data []byte)` is called from ~8 sites in `cmd/argus/main.go` via adapter structs. To add traceparent extraction without breaking every site:

**Approach A (chosen):** Wrap at the bus layer. `Subscribe`/`QueueSubscribe` wrappers extract traceparent from `msg.Header`, open a new child span using `otel.GetTextMapPropagator().Extract(ctx, ...)`, and invoke the user handler with context available via a new internal helper. Since the `MessageHandler` signature is `func(subject, data []byte)` — no context — we introduce a new optional handler type `MessageHandlerCtx func(ctx context.Context, subject string, data []byte)` and a new `SubscribeCtx` method. Existing subscribers keep working; new metric+trace-aware subscribers migrate as they need.

**Minimum migration requirement for STORY-065:** `SubscribeCtx`/`QueueSubscribeCtx` must exist and the metric `argus_nats_consumed_total{subject}` must be incremented from the bus-level wrapper regardless of which API the caller uses. Span creation happens in both paths; only the ctx propagation to the handler differs.

### Pending messages gauge
Add a periodic job (every 15s) in `main.go` that iterates JetStream consumers via `js.Stream(StreamEvents).Consumers()` and updates `NATSPendingMessages{subject}` gauge with `Info.NumPending`. Task 12 scope.

---

## HTTP Instrumentation — Middleware Order

File: `internal/gateway/router.go`, `NewRouterWithDeps`:

```go
func NewRouterWithDeps(deps RouterDeps) http.Handler {
    r := chi.NewRouter()

    r.Use(RecoveryWithZerolog(deps.Logger))
    r.Use(CorrelationID())
    r.Use(chimiddleware.RealIP)
    // ... existing sec/cors/ratelimit ...
    r.Use(ZerologRequestLogger(deps.Logger))
    r.Use(PrometheusHTTPMetrics(deps.MetricsReg)) // NEW: must run AFTER routing so RoutePattern is set
    // ... routes ...

    // Wrap the whole chi router in otelhttp at the server boundary
    return otelhttp.NewHandler(
        r,
        "argus.http",
        otelhttp.WithPropagators(otel.GetTextMapPropagator()),
        otelhttp.WithSpanNameFormatter(func(op string, r *http.Request) string {
            return r.Method + " " + r.URL.Path // best-effort, chi RoutePattern adds post-routing
        }),
    )
}
```

Additional: a tiny span-enrichment middleware (registered right after `JWTAuth` in authenticated groups) reads `ctx.Value(apierr.TenantIDKey)` + `UserIDKey` and calls `trace.SpanFromContext(ctx).SetAttributes(attribute.String("tenant.id", ...), attribute.String("user.id", ...))`. Same middleware also reads `RoutePattern()` and sets `http.route` attribute. This middleware must run AFTER JWTAuth so claims are available — plan adds it to the same router.Use chain after the auth in every authenticated Group, OR plan exposes it as an `AuthSpanEnricher()` middleware applied alongside `JWTAuth(...)`.

**Simpler: make it part of the PrometheusHTTPMetrics middleware** — it runs at the end of the chain, reads tenant_id from ctx, records metric + enriches current span. Single middleware, two jobs.

### PrometheusHTTPMetrics — skeleton
File: `internal/gateway/http_metrics.go`
```go
func PrometheusHTTPMetrics(reg *metrics.Registry) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()
            rc := &responseCapture{ResponseWriter: w, status: 200}

            next.ServeHTTP(rc, r)

            route := chi.RouteContext(r.Context()).RoutePattern()
            if route == "" { route = "unmatched" }
            tenant := tenantLabel(r.Context())
            status := strconv.Itoa(rc.status)

            reg.HTTPRequestsTotal.WithLabelValues(r.Method, route, status, tenant).Inc()
            reg.HTTPRequestDuration.WithLabelValues(r.Method, route, tenant).Observe(time.Since(start).Seconds())

            if span := trace.SpanFromContext(r.Context()); span.IsRecording() {
                span.SetAttributes(
                    attribute.String("http.route", route),
                    attribute.String("tenant.id", tenant),
                    attribute.Int("http.status_code", rc.status),
                )
                if uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID); ok {
                    span.SetAttributes(attribute.String("user.id", uid.String()))
                }
            }
        })
    }
}

func tenantLabel(ctx context.Context) string {
    if tid, ok := ctx.Value(apierr.TenantIDKey).(uuid.UUID); ok && tid != uuid.Nil {
        return tid.String()
    }
    return "unknown"
}
```

### Logging — tenant_id enrichment (AC-7)
File: `internal/gateway/logging.go`, existing `ZerologRequestLogger`. Augment the `event.` builder (after `next.ServeHTTP`):
```go
event.
    Str("correlation_id", correlationID).
    Str("method", r.Method).
    Str("path", r.URL.Path).
    Int("status", rc.status).
    Int64("duration_ms", duration.Milliseconds()).
    Int("bytes", rc.bytes).
    Str("remote_addr", r.RemoteAddr).
    Str("tenant_id", tenantLabel(r.Context())).   // NEW
    Msg("http request")
```
Also add a helper `LoggerWith(ctx, base)` in `internal/gateway/logging.go` that returns `base.With().Str("tenant_id", tenantLabel(ctx)).Str("correlation_id", corrID(ctx)).Logger()` — handlers that log inside their own code path call this to inherit fields. Not mandatory for test pass, but plan includes it for future-proofing and PAT-002 (duplicated helper drift).

---

## Grafana Dashboards — PromQL Spec (6 files)

Developer generates JSON at dev time. For each dashboard, the plan specifies exact panel titles + PromQL queries. Dashboards use `{{tenant_id}}` templated variable where relevant.

### 1. `infra/grafana/dashboards/argus-overview.json` — 6 panels
| Panel | Type | PromQL |
|-------|------|--------|
| Request Rate (req/s) | timeseries | `sum(rate(argus_http_requests_total[1m]))` |
| Error Rate (%) | timeseries | `sum(rate(argus_http_requests_total{status=~"5.."}[5m])) / sum(rate(argus_http_requests_total[5m]))` |
| p95 Latency (s) | timeseries | `histogram_quantile(0.95, sum by (le) (rate(argus_http_request_duration_seconds_bucket[5m])))` |
| p99 Latency (s) | timeseries | `histogram_quantile(0.99, sum by (le) (rate(argus_http_request_duration_seconds_bucket[5m])))` |
| Active Sessions | stat | `sum(argus_active_sessions)` |
| Goroutines / Memory | timeseries | `go_goroutines` and `process_resident_memory_bytes` (2 series) |

### 2. `argus-aaa.json` — 5 panels
| Panel | PromQL |
|-------|--------|
| Auth Req/s per Protocol | `sum by (protocol) (rate(argus_aaa_auth_requests_total[1m]))` |
| Auth Success Rate (%) | `sum(rate(argus_aaa_auth_requests_total{result="success"}[5m])) / sum(rate(argus_aaa_auth_requests_total[5m]))` |
| Auth Latency p50/p95/p99 | `histogram_quantile(0.50/0.95/0.99, sum by (le,protocol) (rate(argus_aaa_auth_latency_seconds_bucket[5m])))` |
| Operator Health | `argus_operator_health` (value mapped 0=down,1=degraded,2=healthy) |
| Circuit Breaker States | `sum by (state) (argus_circuit_breaker_state == 1)` |

### 3. `argus-database.json` — 5 panels
| Panel | PromQL |
|-------|--------|
| Pool Connections (stacked) | `argus_db_pool_connections` by state |
| Query Duration p95 by operation | `histogram_quantile(0.95, sum by (le,operation) (rate(argus_db_query_duration_seconds_bucket[5m])))` |
| Queries per Second | `sum by (operation) (rate(argus_db_query_duration_seconds_count[1m]))` |
| Pool Waits | `argus_db_pool_connections{state="waiting"}` |
| Top 5 Slowest Tables | `topk(5, histogram_quantile(0.95, sum by (le,table) (rate(argus_db_query_duration_seconds_bucket[5m]))))` |

### 4. `argus-messaging.json` — 6 panels
| Panel | PromQL |
|-------|--------|
| NATS Publish Rate by Subject | `sum by (subject) (rate(argus_nats_published_total[1m]))` |
| NATS Consume Rate by Subject | `sum by (subject) (rate(argus_nats_consumed_total[1m]))` |
| NATS Pending (consumer lag) | `argus_nats_pending_messages` |
| Redis Ops/s by Op | `sum by (op) (rate(argus_redis_ops_total[1m]))` |
| Redis Cache Hit Rate (%) | `sum(rate(argus_redis_cache_hits_total[5m])) / (sum(rate(argus_redis_cache_hits_total[5m])) + sum(rate(argus_redis_cache_misses_total[5m])))` |
| Redis Evictions | `rate(redis_evicted_keys_total[5m])` (via redis_exporter sidecar — documented as external) |

### 5. `argus-tenant.json` — templated by `$tenant_id` — 5 panels
| Panel | PromQL |
|-------|--------|
| Per-Tenant Request Rate | `sum by (tenant_id) (rate(argus_http_requests_total{tenant_id=~"$tenant_id"}[1m]))` |
| Per-Tenant Error Rate | `sum by (tenant_id) (rate(argus_http_requests_total{tenant_id=~"$tenant_id",status=~"5.."}[5m]))` |
| Per-Tenant Active Sessions | `sum by (tenant_id) (argus_active_sessions{tenant_id=~"$tenant_id"})` |
| Per-Tenant Auth Req/s | `sum by (tenant_id) (rate(argus_aaa_auth_requests_total{tenant_id=~"$tenant_id"}[1m]))` |
| Per-Tenant p95 Latency | `histogram_quantile(0.95, sum by (le,tenant_id) (rate(argus_http_request_duration_seconds_bucket{tenant_id=~"$tenant_id"}[5m])))` |

Variable definition:
```json
"templating": { "list": [{
  "name": "tenant_id",
  "type": "query",
  "query": "label_values(argus_http_requests_total, tenant_id)"
}]}
```

### 6. `argus-jobs.json` — 4 panels
| Panel | PromQL |
|-------|--------|
| Job Throughput by Type | `sum by (job_type) (rate(argus_job_runs_total[5m]))` |
| Job Failure Rate | `sum by (job_type) (rate(argus_job_runs_total{result="failure"}[5m]))` |
| Job Duration p95 | `histogram_quantile(0.95, sum by (le,job_type) (rate(argus_job_duration_seconds_bucket[10m])))` |
| Job Queue Depth | `argus_nats_pending_messages{subject=~"argus.jobs.*"}` |

**Dashboard JSON structure (template Developer follows):**
```json
{
  "title": "Argus Overview",
  "uid": "argus-overview",
  "tags": ["argus","observability"],
  "timezone": "browser",
  "schemaVersion": 38,
  "time": {"from": "now-1h", "to": "now"},
  "refresh": "10s",
  "templating": {"list": []},
  "panels": [
    {
      "id": 1,
      "title": "Request Rate (req/s)",
      "type": "timeseries",
      "targets": [{"expr": "sum(rate(argus_http_requests_total[1m]))", "refId": "A"}],
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 0}
    }
    /* … 5 more … */
  ]
}
```
Each panel has unique `id`, `gridPos` laid out 2-column, `targets[].expr` from the table above, `datasource: {"type":"prometheus","uid":"prometheus"}`.

---

## Prometheus Alert Rules — Full YAML Skeleton

File: `infra/prometheus/alerts.yml`

```yaml
groups:
  - name: argus.rules
    interval: 30s
    rules:
      - alert: ArgusHighErrorRate
        expr: |
          sum(rate(argus_http_requests_total{status=~"5.."}[5m]))
            / sum(rate(argus_http_requests_total[5m])) > 0.05
        for: 5m
        labels:
          severity: critical
          service: argus
        annotations:
          summary: "Argus 5xx error rate above 5% ({{ $value | humanizePercentage }})"
          runbook: "https://runbook.example/argus-high-error-rate"

      - alert: ArgusAuthLatencyHigh
        expr: |
          histogram_quantile(0.99,
            sum by (le) (rate(argus_aaa_auth_latency_seconds_bucket[5m]))
          ) > 0.5
        for: 10m
        labels: {severity: warning, service: argus}
        annotations:
          summary: "AAA auth p99 latency above 500ms"

      - alert: ArgusOperatorDown
        expr: argus_operator_health == 0
        for: 2m
        labels: {severity: critical, service: argus}
        annotations:
          summary: "Operator {{ $labels.operator_id }} is DOWN"

      - alert: ArgusCircuitBreakerOpen
        expr: argus_circuit_breaker_state{state="open"} == 1
        for: 5m
        labels: {severity: warning, service: argus}
        annotations:
          summary: "Circuit breaker OPEN for operator {{ $labels.operator_id }}"

      - alert: ArgusDBPoolExhausted
        expr: argus_db_pool_connections{state="waiting"} > 0
        for: 1m
        labels: {severity: critical, service: argus}
        annotations:
          summary: "DB pool has {{ $value }} waiting connections"

      - alert: ArgusNATSConsumerLag
        expr: argus_nats_pending_messages > 10000
        for: 5m
        labels: {severity: warning, service: argus}
        annotations:
          summary: "NATS pending messages for {{ $labels.subject }} above 10k"

      - alert: ArgusJobFailureRate
        expr: |
          sum by (job_type) (rate(argus_job_runs_total{result="failure"}[10m])) > 0.1
        for: 10m
        labels: {severity: warning, service: argus}
        annotations:
          summary: "Job {{ $labels.job_type }} failing at >10% rate"

      - alert: ArgusRedisEvictionStorm
        expr: rate(redis_evicted_keys_total[5m]) > 100
        for: 5m
        labels: {severity: warning, service: argus}
        annotations:
          summary: "Redis eviction rate high: {{ $value }}/s"

      - alert: ArgusDiskSpaceLow
        expr: |
          (node_filesystem_avail_bytes{mountpoint="/"}
            / node_filesystem_size_bytes{mountpoint="/"}) < 0.15
        for: 5m
        labels: {severity: warning, service: argus}
        annotations:
          summary: "Disk free below 15% on {{ $labels.instance }}"
```

Validation: task runs `promtool check rules infra/prometheus/alerts.yml` — must exit 0.

---

## Prometheus Scrape Config

File: `infra/prometheus/prometheus.yml` (new)
```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

rule_files:
  - /etc/prometheus/alerts.yml

scrape_configs:
  - job_name: argus
    static_configs:
      - targets: ['argus:8080']
    metrics_path: /metrics

  - job_name: node
    static_configs:
      - targets: ['node-exporter:9100']

  - job_name: redis
    static_configs:
      - targets: ['redis-exporter:9121']
```
This file is delivered as part of the story but wiring into `deploy/docker-compose.yml` (adding Prometheus + Grafana + node-exporter + redis-exporter services) is SCOPED IN: STORY-065 ships a compose overlay so operators can `docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.obs.yml up` and get a working stack. This is necessary to satisfy AC-8 (dashboards must be importable in real Grafana during E2E validation).

File: `deploy/docker-compose.obs.yml` — adds `prometheus`, `grafana`, `otel-collector`, `node-exporter`, `redis-exporter` services with volume mounts to `infra/`.

---

## Prerequisites
- [x] STORY-063 complete — real health probes fed into dashboards
- [x] Go 1.25.9 available (go.mod)
- [x] Decision DEV-137.3 (SUPERSEDED) — gap analysis that drove this story
- [x] PAT-002 awareness — shared helpers (tenantLabel, corrID) extracted once, not duplicated

## Tech Debt (from ROUTEMAP)
No open tech debt items explicitly targeting STORY-065 in `docs/ROUTEMAP.md` — the story itself IS the tech debt resolver for DEV-137.3.

## Bug Pattern Warnings
- **PAT-002** (duplicated helpers): `tenantLabel(ctx)` / `corrID(ctx)` helpers MUST live in one place (`internal/gateway/context_helpers.go` or equivalent) and be imported — do not re-implement inline in each middleware.
- **PAT-001** (BR test drift): Not applicable — no business rules changed.

## Mock Retirement
No mock retirement — this story does not touch frontend mocks.

---

## Tasks (18 tasks across 4 waves)

> **Wave strategy:** Wave 1 establishes foundation modules (config, OTel init, metric registry, /metrics handler) that all later waves depend on. Wave 2 instruments each subsystem in parallel. Wave 3 wires callsites and adds the infra artifacts. Wave 4 is tests + docker compose overlay + final verification.

### Wave 1 — Foundation (tasks can run in parallel)

### Task 1: Add OTel + Prometheus + otelpgx dependencies
- **Files:** Modify `go.mod`, `go.sum`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `go.mod` existing requires; use `go get` to add modules, then `go mod tidy`
- **Context refs:** "Go Modules to Add (go.mod)"
- **What:** Run `go get go.opentelemetry.io/otel@v1.29.0 go.opentelemetry.io/otel/sdk@v1.29.0 go.opentelemetry.io/otel/trace@v1.29.0 go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.29.0 go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@v0.54.0 github.com/exaring/otelpgx@v0.8.0 github.com/prometheus/client_golang@v1.20.4`. Then `go mod tidy`. Verify no replace directives needed. If a version is unavailable, use the latest compatible version printed by `go mod tidy` and document the bump in the plan.
- **Verify:** `go build ./...` compiles; `go mod verify` passes; `grep "go.opentelemetry.io/otel" go.mod` → at least 5 lines

### Task 2: Config — add OTEL_* and METRICS_* env vars
- **Files:** Modify `internal/config/config.go`, `internal/config/config_test.go`, Modify `.env.example`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/config/config.go` lines 1-150 — add to the `Config` struct in the same style (envconfig tags)
- **Context refs:** "Environment Variables" section
- **What:** Add all 9 env vars from the table. Default `OTELExporterOTLPEndpoint=""`, `OTELSamplerRatio=1.0`, `METRICSTenantLabelEnabled=true`, etc. Extend `Config.Validate()` to check `OTELSamplerRatio` in `[0,1]`. Extend `.env.example` with commented samples. Update `config_test.go` with one happy-path and one boundary test (`sampler_ratio=1.5` → error).
- **Verify:** `go test ./internal/config/... -race` passes

### Task 3: Observability package — OTel SDK init
- **Files:** Create `internal/observability/otel.go`, Create `internal/observability/otel_test.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** First of its kind — follow structure shown in "OTel Init — Structural Skeleton". Use `noop.NewTracerProvider()` from `go.opentelemetry.io/otel/trace/noop` when endpoint empty.
- **Context refs:** "Go Modules to Add", "OTel Init — Structural Skeleton"
- **What:** Implement `Init(ctx, Config) (shutdown func, error)` exactly as skeleton. Build `resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceNameKey.String(cfg.ServiceName), ...)`. Builder otlptracegrpc exporter. BatchSpanProcessor with 5s export timeout. TraceIDRatioBased sampler wrapped in ParentBased. Set global tracer provider + `otel.SetTextMapPropagator(propagation.TraceContext{})`. Return shutdown that calls `tp.Shutdown(ctx)`. Tests: (a) `Init` with empty endpoint returns noop shutdown that succeeds, (b) `Init` with invalid endpoint does not panic, (c) after `Init`, `otel.GetTracerProvider()` is non-nil, (d) tracer produces spans via `tp.Tracer("x").Start(...)`.
- **Verify:** `go test ./internal/observability/... -race -v`

### Task 4: Observability — Prometheus metric registry
- **Files:** Create `internal/observability/metrics/metrics.go`, Create `internal/observability/metrics/metrics_test.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** First of its kind for this project. Follow the Registry struct skeleton exactly. Use `prometheus.NewRegistry()` (not default), register `collectors.NewGoCollector()` and `collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})`.
- **Context refs:** "Prometheus Metric Registry — Full Definition", "Histogram Buckets"
- **What:** Create `NewRegistry() *Registry` building all 15 metric vectors with exact names, help strings, labels, and bucket values from the plan. Each vector registered via `reg.MustRegister(...)`. Also export `func (r *Registry) Handler() http.Handler` returning `promhttp.HandlerFor(r.Reg, promhttp.HandlerOpts{EnableOpenMetrics: true, Registry: r.Reg})`. Tests: (a) `NewRegistry` succeeds without panic, (b) each metric name is unique, (c) `Handler()` GET returns 200 with `argus_http_requests_total` help line, `go_goroutines` line, and `process_resident_memory_bytes` line, (d) incrementing a counter visible via handler output.
- **Verify:** `go test ./internal/observability/metrics/... -race -v` — assert output contains `# HELP argus_http_requests_total`, `go_goroutines`, `process_cpu_seconds_total`.

### Task 5: Delete old custom Prometheus text format handler
- **Files:** Delete `internal/api/metrics/prometheus.go`, Modify `internal/api/metrics/handler_test.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/api/metrics/handler_test.go` — identify which test functions reference `Prometheus()` method or assert text-format output, delete those functions (keep ones that test `GetSystemMetrics` JSON).
- **Context refs:** "Existing State to DELETE"
- **What:** `git rm internal/api/metrics/prometheus.go`. In `handler_test.go`, delete `TestHandler_Prometheus*` or similarly named functions, keep `TestHandler_GetSystemMetrics*`. The `Handler` struct in `handler.go` loses `Prometheus` method — verify no references via grep.
- **Verify:** `grep -r "MetricsHandler.Prometheus\|handler.Prometheus" internal/ cmd/` → only matches in `router.go` (Task 9 will fix); `go build ./internal/api/metrics/...` passes

### Wave 2 — Instrumentation (tasks in parallel after Wave 1)

### Task 6: gateway — tenantLabel helper + logging tenant_id
- **Files:** Create `internal/gateway/context_helpers.go`, Modify `internal/gateway/logging.go`, Modify `internal/gateway/logging_test.go` (create if absent)
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/gateway/correlation.go` — tiny helper file pattern
- **Context refs:** "Logging — tenant_id enrichment (AC-7)", "Bug Pattern Warnings (PAT-002)"
- **What:** Create `context_helpers.go` exporting `TenantLabel(ctx context.Context) string` (returns `uuid.String()` or `"unknown"`) and `LoggerWith(ctx, base zerolog.Logger) zerolog.Logger` (adds `tenant_id`+`correlation_id`). Modify `ZerologRequestLogger` to add `.Str("tenant_id", TenantLabel(r.Context()))` to the event builder BEFORE `.Msg(...)`. Add a test `TestZerologRequestLogger_IncludesTenantID` using `zerolog.TestWriter` or similar that passes an authenticated ctx and asserts log line contains `tenant_id`.
- **Verify:** `go test ./internal/gateway/... -race -run TestZerologRequestLogger` passes

### Task 7: gateway — PrometheusHTTPMetrics middleware + otelhttp wrap
- **Files:** Create `internal/gateway/http_metrics.go`, Create `internal/gateway/http_metrics_test.go`, Modify `internal/gateway/router.go`
- **Depends on:** Task 4, Task 6
- **Complexity:** high
- **Pattern ref:** Read `internal/gateway/logging.go` for middleware+`responseCapture` pattern. Follow `PrometheusHTTPMetrics — skeleton` in plan.
- **Context refs:** "HTTP Instrumentation — Middleware Order", "PrometheusHTTPMetrics — skeleton"
- **What:** Implement `PrometheusHTTPMetrics(reg *metrics.Registry) func(http.Handler) http.Handler` that wraps with responseCapture, reads `chi.RouteContext(r.Context()).RoutePattern()`, increments counter + observes histogram, and enriches the current span with tenant_id/user_id/route/status. Modify `NewRouterWithDeps` to: (1) accept a new `MetricsReg *metrics.Registry` field in `RouterDeps`, (2) call `r.Use(PrometheusHTTPMetrics(deps.MetricsReg))` after `ZerologRequestLogger`, (3) wrap the final returned chi router with `otelhttp.NewHandler(r, "argus.http", otelhttp.WithPropagators(otel.GetTextMapPropagator()))` and return `http.Handler` instead of `*chi.Mux`. Change the function signature to `func NewRouterWithDeps(deps RouterDeps) http.Handler`. Update the single caller (`NewRouter`) accordingly. Tests: integration test sends a GET to a mounted fake route, scrapes the registry via `reg.Handler()`, asserts counter incremented for that route pattern with expected labels.
- **Verify:** `go test ./internal/gateway/... -race -run TestPrometheusHTTPMetrics` passes. `grep "otelhttp.NewHandler" internal/gateway/router.go` finds 1 match.

### Task 8: store — otelpgx tracer + slow-query wrapper + pool gauge
- **Files:** Modify `internal/store/postgres.go`, Create `internal/store/tracer_slow.go`, Create `internal/store/postgres_test.go` (extend existing if present)
- **Depends on:** Task 1, Task 4
- **Complexity:** high
- **Pattern ref:** Read current `internal/store/postgres.go` — extend `NewPostgres` without changing its public signature. `slowQueryTracer` implements `pgx.QueryTracer`: `TraceQueryStart` stashes start time on ctx, `TraceQueryEnd` calculates duration and (a) observes `DBQueryDuration`, (b) sets `argus.db.slow=true` span attribute if > 100ms.
- **Context refs:** "otelpgx Wiring — Structural Skeleton", "Dependency Decisions #1"
- **What:** Create composite tracer `newCompositeTracer(tracers ...pgx.QueryTracer)` that fans out each callback to all wrapped tracers. `NewPostgres` now takes an extra optional arg (or uses a functional option) to receive a `*metrics.Registry`; easier approach: new constructor `NewPostgresWithMetrics(ctx, dsn, ..., reg *metrics.Registry) (*Postgres, error)` that calls the existing one and sets `cfg.ConnConfig.Tracer`. Keep `NewPostgres` unchanged for backward compat but have `main.go` (Task 15) call the new constructor. Implement `slowQueryTracer` that pulls table+operation from SQL via `regexp.MustCompile`(`(?i)^\s*(SELECT|INSERT|UPDATE|DELETE)\s+(?:FROM|INTO)?\s*"?(\w+)`) and labels accordingly. Add `StartPoolGauge(ctx, reg, interval)` goroutine that every 10s reads `pool.Stat()` and sets `DBPoolConnections{state=idle|in_use|waiting}` gauge. Tests: (a) `slowQueryTracer` records histogram observation via a stub registry, (b) query >100ms sets span attribute, (c) `StartPoolGauge` updates gauge after first tick, (d) regex extracts table name for each operation.
- **Verify:** `go test ./internal/store/... -race -run Tracer|PoolGauge` passes

### Task 9: bus — NATS traceparent injection + extraction
- **Files:** Modify `internal/bus/nats.go`, Create `internal/bus/nats_trace_test.go`
- **Depends on:** Task 1, Task 3
- **Complexity:** high
- **Pattern ref:** First of its kind. Follow "NATS traceparent — Structural Skeleton" exactly. Use `propagation.TextMapCarrier` interface adapter.
- **Context refs:** "NATS traceparent — Structural Skeleton", "Dependency Decisions #3"
- **What:** Add `natsHeaderCarrier` type wrapping `nats.Header` with `Get/Set/Keys` methods. Modify `EventBus.Publish` and `EventBus.PublishRaw` to build `nats.Msg` with header + `otel.GetTextMapPropagator().Inject(ctx, carrier)`, then call `eb.js.PublishMsg(ctx, msg)`. Open a publish span via `tracer.Start(ctx, "nats.publish", trace.WithAttributes(semconv.MessagingSystem("nats"), semconv.MessagingDestinationName(subject), semconv.MessagingOperationPublish))`. Add new method `(eb *EventBus) SubscribeCtx(subject string, h func(context.Context, string, []byte)) (*nats.Subscription, error)` + `QueueSubscribeCtx`. Wrapper extracts header via `otel.GetTextMapPropagator().Extract(ctx, carrier)` and opens `tracer.Start(ctx, "nats.consume", ...)` child span before calling handler. Keep old `Subscribe`/`QueueSubscribe` signatures intact (they now also extract + open consume span internally, discarding ctx before calling old handler) — this keeps all existing main.go adapters working without changes. Both code paths increment `NATSPublishedTotal`/`NATSConsumedTotal` on the provided registry. Because `EventBus` struct has no metrics ref today, add `(eb *EventBus) SetMetrics(reg *metrics.Registry)`. Tests: (a) PublishMsg propagates traceparent from ctx (spin up in-proc NATS via nats-server embedded or use a fake `jetstream.JetStream` double), (b) Subscribe wrapper extracts traceparent, (c) metrics incremented on both sides.
- **Verify:** `go test ./internal/bus/... -race -run Trace` passes. Manual: `grep -r "eb.js.Publish(" internal/bus/` → 0 matches (all replaced with `PublishMsg`).

### Task 10: cache — Redis metrics hooks
- **Files:** Create `internal/cache/redis_metrics.go`, Create `internal/cache/redis_metrics_test.go`
- **Depends on:** Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/cache/redis.go`. The `go-redis/v9` client exposes `AddHook(redis.Hook)` — implement a hook that increments `RedisOpsTotal{op,result}` on every ProcessHook callback.
- **Context refs:** "Prometheus Metric Registry — Full Definition (RedisOpsTotal, RedisCacheHitsTotal, RedisCacheMissesTotal)"
- **What:** Implement `type metricsHook struct{ reg *metrics.Registry }` with `DialHook`, `ProcessHook`, `ProcessPipelineHook` methods per `redis.Hook` interface. On `ProcessHook`, increment `RedisOpsTotal.WithLabelValues(cmd.Name(), resultLabel(err))`. Add `RegisterRedisMetrics(client *redis.Client, reg *metrics.Registry)` that calls `client.AddHook(...)`. Cache hit/miss counters — these are more semantic; document that callsites that check cache (e.g. `simcache.go`, `name_cache.go`) call `reg.RedisCacheHitsTotal.WithLabelValues("sim").Inc()` / `RedisCacheMissesTotal` directly; this task adds the infrastructure but does NOT instrument every callsite (Task 11 does). Tests: `TestMetricsHook_IncrementsOnProcess` using `miniredis` (already in go.sum) — perform a SET, GET, verify counter metrics.
- **Verify:** `go test ./internal/cache/... -race -run Metrics` passes

### Task 11: aaa + operator — Prometheus-backed recorder + breaker/health gauges
- **Files:** Create `internal/observability/metrics/aaa_recorder.go`, Create `internal/observability/metrics/aaa_recorder_test.go`, Modify `internal/aaa/radius/server.go`, Modify `internal/operator/health.go`, Modify `internal/operator/circuit_breaker.go`
- **Depends on:** Task 4
- **Complexity:** high
- **Pattern ref:** Read `internal/analytics/metrics/collector.go` `RecordAuth` — new recorder has the SAME interface so it drops into `radius.Server.SetMetricsRecorder` transparently. Also read `internal/aaa/radius/server.go:32` for the `MetricsRecorder` interface.
- **Context refs:** "AAA MetricsRecorder interface is extended, not replaced", "AC-11 Operator health + circuit breaker as gauges"
- **What:**
  1. Create `PromAAARecorder` implementing the existing `MetricsRecorder` interface (`RecordAuth(ctx, opID, success, latencyMs)`). Takes a `*metrics.Registry`, a `protocol string`, and on each call increments `AAAAuthRequestsTotal{protocol, operator_id, result, tenant_id}` (result = success|failure, tenant_id extracted from ctx via `TenantLabel`) and observes `AAAAuthLatency`.
  2. Create `CompositeMetricsRecorder` that fans out to multiple recorders (used to wire BOTH the existing Redis collector AND the new Prom recorder into `radius.Server`).
  3. In `internal/operator/health.go` `checkOperator`: after computing `status`, set `metrics.OperatorHealth.WithLabelValues(opID.String()).Set(healthValue(status))` where healthValue maps down=0/degraded=1/healthy=2. Add a `SetMetrics(reg *metrics.Registry)` method on `HealthChecker` and wire in Task 15.
  4. In `internal/operator/circuit_breaker.go`: add optional `SetMetricsHook(func(state CircuitState))` so `RecordSuccess`/`RecordFailure`/`State` transition points can notify. Easier: make `CircuitBreaker` hold a `transitionHook func(opID uuid.UUID, state CircuitState)` set from `HealthChecker` which uses it to update `CircuitBreakerState` gauge (set 1 for current state, 0 for others).
  5. Tests: PromAAARecorder increments counter with expected labels; composite fan-out delivers to both; Circuit breaker transition triggers hook; OperatorHealth gauge set on status change.
- **Verify:** `go test ./internal/observability/metrics/... ./internal/aaa/radius/... ./internal/operator/... -race -run Recorder|Health|Breaker` passes

### Task 12: job — JobRunner metric hook
- **Files:** Modify `internal/job/job.go` (Runner), Create/Modify `internal/job/job_test.go` for metric assertions
- **Depends on:** Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/job.go` — find where a processor's `Process` is called and where success/failure is resolved. Wrap with timing.
- **Context refs:** "Prometheus Metric Registry > JobRunsTotal/JobDuration"
- **What:** Add `(r *Runner) SetMetrics(reg *metrics.Registry)`. In the dispatch path, record `start := time.Now()`; after the processor returns or errors, increment `JobRunsTotal.WithLabelValues(jobType, resultLabel(err))` and observe `JobDuration.WithLabelValues(jobType)`. Add small unit test using a stub processor that succeeds and one that fails; assert counter and histogram observe.
- **Verify:** `go test ./internal/job/... -race -run JobMetrics` passes

### Wave 3 — Wiring, callsites, infra artifacts

### Task 13: Grafana dashboards — 6 JSON files
- **Files:** Create `infra/grafana/dashboards/argus-overview.json`, `argus-aaa.json`, `argus-database.json`, `argus-messaging.json`, `argus-tenant.json`, `argus-jobs.json`; Create `infra/grafana/provisioning/datasources/prometheus.yml`; Create `infra/grafana/provisioning/dashboards/argus.yml`
- **Depends on:** Task 4 (metric names must be stable)
- **Complexity:** medium
- **Pattern ref:** First of its kind. Follow the Dashboard JSON template in plan; panel schemas from the "Grafana Dashboards — PromQL Spec" tables. Grafana schemaVersion 38 (Grafana 10.x).
- **Context refs:** "Grafana Dashboards — PromQL Spec (6 files)"
- **What:** For each of the 6 dashboards, produce a complete JSON file with: `title`, `uid`, `tags`, `schemaVersion: 38`, `time`, `refresh: 10s`, `templating.list` (empty for all except tenant), `panels[]` with unique ids, gridPos laid out 2-column (w=12 each, rows of h=8), Prometheus datasource reference `{"type":"prometheus","uid":"prometheus"}`, and `targets[].expr` from the PromQL spec tables. Provisioning YAML registers the folder + datasource so Grafana auto-loads on boot. Each dashboard MUST have at least 4 panels (overview=6, aaa=5, database=5, messaging=6, tenant=5, jobs=4).
- **Verify:** Each JSON file parses via `jq . infra/grafana/dashboards/*.json` → exit 0. Each file contains `"datasource"` key and at least 4 `"targets"` arrays. Grep for `argus_http_requests_total` in overview, `argus_aaa_auth_requests_total` in aaa, etc.

### Task 14: Prometheus alerts.yml + scrape config + docker-compose overlay
- **Files:** Create `infra/prometheus/alerts.yml`, Create `infra/prometheus/prometheus.yml`, Create `deploy/docker-compose.obs.yml`
- **Depends on:** Task 4
- **Complexity:** medium
- **Pattern ref:** Read existing `deploy/docker-compose.yml` for service structure + network
- **Context refs:** "Prometheus Alert Rules — Full YAML Skeleton", "Prometheus Scrape Config"
- **What:** Emit the full `alerts.yml` per plan skeleton (9 rules). Emit `prometheus.yml` with global scrape_interval 15s, rule_files entry, scrape targets for argus/node/redis exporters. Emit `docker-compose.obs.yml` adding `prometheus`, `grafana`, `otel-collector` (otel/opentelemetry-collector-contrib), `node-exporter`, `redis-exporter` services with volume mounts, healthchecks, attached to the existing `argus-net` network. Grafana reads provisioning from `infra/grafana/provisioning/`. otel-collector config is a minimal pipeline: OTLP gRPC receiver 4317 → batch → OTLP HTTP exporter to stdout+Tempo (Tempo left as optional, documented as `TEMPO_ENDPOINT` env).
- **Verify:** `promtool check rules infra/prometheus/alerts.yml` exits 0 (document in task verify — if promtool unavailable locally, CI catches). `docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.obs.yml config` parses without error.

### Task 15: main.go — wire everything + graceful flush
- **Files:** Modify `cmd/argus/main.go`
- **Depends on:** Task 2, Task 3, Task 4, Task 7, Task 8, Task 9, Task 10, Task 11, Task 12
- **Complexity:** high
- **Pattern ref:** Read `cmd/argus/main.go` lines 76-860 — add init + wiring immediately after logger setup and before DB init.
- **Context refs:** "Data Flow — Full request with tracing + metrics", "Existing State to DELETE" (for router change)
- **What:**
  1. After logger/pprof setup (~line 110), call `metricsReg := metrics.NewRegistry()` and `otelShutdown, err := observability.Init(ctx, observability.Config{...cfg})`; defer shutdown with 5s context.
  2. Replace `store.NewPostgres(...)` with `store.NewPostgresWithMetrics(ctx, ..., metricsReg)`.
  3. After pool init, start `store.StartPoolGauge(ctx, pg.Pool, metricsReg, 10*time.Second)` goroutine; wire stop via ctx cancel on shutdown.
  4. After `bus.NewNATS`, call `eventBus.SetMetrics(metricsReg)`.
  5. After `cache.NewRedis`, call `cache.RegisterRedisMetrics(rdb.Client, metricsReg)`.
  6. After `healthChecker := operator.NewHealthChecker(...)`, call `healthChecker.SetMetrics(metricsReg)`.
  7. After `jobRunner := job.NewRunner(...)`, call `jobRunner.SetMetrics(metricsReg)`.
  8. When wiring RADIUS: build `composite := metrics.NewCompositeRecorder(metricsCollector, metrics.NewPromAAARecorder(metricsReg, "radius"))` and call `radiusServer.SetMetricsRecorder(composite)`. Same for diameter and SBA if they have a recorder interface (check; if not, leave them wired to the Prom recorder only where applicable).
  9. Pass `metricsReg` into `RouterDeps.MetricsReg`.
  10. Add a `/metrics` route registration: replace `r.Get("/metrics", deps.MetricsHandler.Prometheus)` (in router.go) with `r.Handle("/metrics", deps.MetricsReg.Handler())`. This is done in Task 7 but main.go must pass the field through.
  11. Start NATS pending messages poller goroutine (every 15s iterate `js.Stream(StreamEvents).Consumer(...)` → Info.NumPending → `NATSPendingMessages.WithLabelValues(subject).Set(...)`). Stop on shutdown.
  12. Graceful shutdown order: before closing NATS/Redis/DB, call `otelShutdown(ctx)` to flush in-flight spans.
- **Verify:** `go build ./cmd/argus && ./argus --help` works (no panic at init). Manual smoke test: start service with `OTEL_EXPORTER_OTLP_ENDPOINT=""` → should start fine (tracing noop). `curl localhost:8080/metrics` → 200 with `argus_http_requests_total`, `go_goroutines`.

### Task 16: router.go — finalize /metrics route migration
- **Files:** Modify `internal/gateway/router.go`, Modify `internal/gateway/router_test.go`
- **Depends on:** Task 5, Task 7, Task 15
- **Complexity:** low
- **Pattern ref:** Read `internal/gateway/router.go:496-504`
- **Context refs:** "Existing State to DELETE"
- **What:** Remove `r.Get("/metrics", deps.MetricsHandler.Prometheus)` line. Replace with `if deps.MetricsReg != nil { r.Handle("/metrics", deps.MetricsReg.Handler()) }`. `/api/v1/system/metrics` JSON endpoint (line 500) REMAINS unchanged — still served by `deps.MetricsHandler.GetSystemMetrics`. Add `MetricsReg *metrics.Registry` field to `RouterDeps` struct. Update `router_test.go` to assert (a) GET `/metrics` returns 200 and body contains `# HELP argus_http_requests_total`, (b) GET `/api/v1/system/metrics` still returns JSON envelope.
- **Verify:** `go test ./internal/gateway/... -race -run TestRouter` passes. `grep -r "MetricsHandler.Prometheus" /Users/btopcu/workspace/argus/` → 0 matches.

### Wave 4 — Tests + final verification

### Task 17: Integration test — end-to-end trace + metrics
- **Files:** Create `internal/observability/integration_test.go` (build tag `//go:build integration`)
- **Depends on:** Tasks 6–16
- **Complexity:** medium
- **Pattern ref:** First of its kind. Use `httptest.NewServer` + `otelhttp` + in-memory exporter from `go.opentelemetry.io/otel/sdk/trace/tracetest`.
- **Context refs:** "Test Scenarios" from story
- **What:** Spin up a minimal chi router using `NewRouterWithDeps` with a fake auth middleware that injects a tenant UUID into ctx. Build a `tracetest.InMemoryExporter` and replace the global tracer with a TracerProvider using it. Send authenticated GET to `/metrics` and to a fake `/api/v1/test` route. Assertions: (a) `/metrics` body contains `argus_http_requests_total`, `go_goroutines`, `process_resident_memory_bytes`, (b) after the GET, `argus_http_requests_total{method="GET",route="/api/v1/test",status="200",tenant_id="<uuid>"}` counter >= 1, (c) in-memory exporter captured ≥1 span with attribute `http.route=/api/v1/test` and `tenant.id=<uuid>`, (d) correlation_id from X-Request-ID header appears in the span attributes. Tag the test file `//go:build integration` so it runs only in CI target `make test-integration`.
- **Verify:** `go test -tags integration ./internal/observability/... -race -v` passes

### Task 18: Docs update + final Quality Gate grep checks
- **Files:** Modify `docs/architecture/CONFIG.md` (add OTEL_*/METRICS_* env vars), Modify `README.md` (add observability section link), Modify `docs/ROUTEMAP.md` (update STORY-065 row — handled by Amil post-proc, but task notes the dependency)
- **Depends on:** All prior tasks
- **Complexity:** low
- **Pattern ref:** Read current `docs/architecture/CONFIG.md` for table format
- **Context refs:** "Environment Variables" section
- **What:** Append a new "Observability" section to `CONFIG.md` with all 9 env vars, defaults, description. README: add a short "Observability" section pointing to `infra/grafana/dashboards/` and `infra/prometheus/alerts.yml` and documenting `docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.obs.yml up` quickstart. Run final lint: `grep -r 'fmt.Sprintf.*argus_' internal/` → 0 matches; `grep -r 'argus_.*\\\\n' internal/` → 0 matches outside tests; `go vet ./...` → 0 issues.
- **Verify:** All greps return as expected. `go vet ./...` and `go build ./...` pass.

---

## Acceptance Criteria Mapping
| AC | Description | Implemented In | Verified By |
|----|-------------|---------------|-------------|
| AC-1 | OpenTelemetry SDK + OTLP gRPC + graceful flush | Task 3, Task 15 | Task 17 (span export) |
| AC-2 | HTTP otelhttp + tenant_id/user_id on spans | Task 7, Task 15 | Task 17 (span attrs) |
| AC-3 | DB otelpgx + slow query flag | Task 8, Task 15 | Task 8 unit tests |
| AC-4 | NATS traceparent inject/extract | Task 9, Task 15 | Task 9 unit tests |
| AC-5 | Prometheus client_golang migration | Task 4, Task 5, Task 7, Task 16 | Task 17 (grep + scrape) |
| AC-6 | 15 metrics with label sets | Task 4, Task 7, Task 8, Task 10, Task 11, Task 12 | Task 4 unit, Task 17 integration |
| AC-7 | tenant_id in logs | Task 6 | Task 6 unit test |
| AC-8 | 6 Grafana dashboards | Task 13 | Task 13 JSON parse checks |
| AC-9 | 9 Prometheus alert rules | Task 14 | promtool check rules |
| AC-10 | correlation_id propagation request→log→span→downstream | Task 6, Task 7, Task 9 | Task 17 (in-memory exporter) |
| AC-11 | Operator health + breaker as gauges | Task 11, Task 15 | Task 11 unit tests |

## Story-Specific Compliance Rules
- **API:** `/metrics` endpoint is NOT a standard-envelope JSON endpoint — it is Prometheus text (and optionally OpenMetrics). `/api/v1/system/metrics` JSON envelope remains unchanged and still required.
- **DB:** No schema changes. `pgxpool.Pool` public API unchanged; only its `Tracer` field is now set.
- **Go convention:** All new packages use project-standard `package foo; import (...)` ordering; tests use `package foo_test` where appropriate; `-race` must pass.
- **Config:** New env vars documented in `CONFIG.md` (PAT-002 equivalent — single source of truth).
- **Observability:** Slow-query threshold 100ms is hardcoded per AC-3; make it a const not magic number.
- **ADR compliance:** No new ADRs required. Existing ADRs on logging (zerolog), on correlation_id header, and on tenant scoping remain unchanged.

---

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| otelpgx version incompatibility with pgx v5.8.0 | Task 1 runs `go mod tidy`; Developer picks latest compatible version and notes in commit msg. Fallback: wrap `pool.Acquire` with a custom `pgx.QueryTracer` of our own — ~80 LOC extra in `tracer_slow.go`. |
| NATS handler signature change breaks existing subscribers | Task 9 keeps old `Subscribe`/`QueueSubscribe` signatures working (just adds metrics + span internally). New `SubscribeCtx` is additive. Zero call-site changes required outside main.go wiring. |
| Metric cardinality explosion from `tenant_id` on HTTP counter | `METRICS_TENANT_LABEL_ENABLED=false` env var kill switch. If disabled, `tenantLabel()` returns `"all"`. Documented in CONFIG.md. |
| Grafana JSON schema version drift | Pin `schemaVersion: 38` matching Grafana 10.x (what docker-compose.obs.yml provisions). |
| otelhttp wrapping the full router breaks the chi `Mux` return type | Task 7 returns `http.Handler` from `NewRouterWithDeps` instead of `*chi.Mux`. The single caller uses `http.Handler` already (for `srv.Handler`). |
| Alertmanager not in scope | `alerts.yml` is just rules; actual Alertmanager routing is STORY-066. Document this in the file header comment. |
| Developer forgets to wire one of the `SetMetrics(reg)` calls | Task 17 integration test hits a real route and asserts counter incremented — will fail loudly if wiring missing. |

---

## Self-Validation Quality Gate (Planner — PRE-WRITE)

**Substance (story effort L → ≥100 lines, ≥5 tasks):** Plan has 18 tasks, ~560+ lines — PASS.

**Required sections:**
- [x] Goal
- [x] Architecture Context
- [x] Tasks (18 numbered)
- [x] Acceptance Criteria Mapping

**Embedded specs:**
- [x] No database schema changes — N/A
- [x] API: /metrics endpoint format and fields specified (Prometheus text + OpenMetrics)
- [x] Environment variables embedded (9 vars)
- [x] Metric definitions embedded (15 metrics with labels + buckets)
- [x] Code skeletons embedded for OTel init, otelpgx wrap, NATS traceparent, HTTP middleware
- [x] Grafana dashboard panel structure + all PromQL queries embedded
- [x] Alert rules YAML skeleton embedded (all 9 rules)

**Task complexity cross-check (L story → ≥1 high):** 5 high-complexity tasks (Task 7, 8, 9, 11, 15) — PASS.

**Context refs validation:**
- All referenced sections exist in this plan: "Go Modules to Add", "Environment Variables", "Prometheus Metric Registry", "OTel Init — Structural Skeleton", "otelpgx Wiring — Structural Skeleton", "NATS traceparent — Structural Skeleton", "HTTP Instrumentation — Middleware Order", "PrometheusHTTPMetrics — skeleton", "Logging — tenant_id enrichment (AC-7)", "Grafana Dashboards — PromQL Spec", "Prometheus Alert Rules — Full YAML Skeleton", "Dependency Decisions", "Existing State to DELETE", "Bug Pattern Warnings" — all present.

**Architecture compliance:**
- [x] No cross-layer imports planned (observability is its own package, dependency direction: main → observability, gateway/store/bus/cache → observability/metrics — acceptable: metrics package is infra-level like logger)
- [x] Dependency direction correct
- [x] Naming matches project conventions (`internal/observability`, `internal/observability/metrics`)

**Go backend compliance:**
- [x] `-race` flag in all test verify steps
- [x] No new DB migrations required
- [x] Existing public APIs preserved (otelpgx added as optional field on pool config)
- [x] Graceful shutdown sequence updated

**Task decomposition:**
- [x] Each task touches 1–3 files ideally — task 15 (main.go only, but large edit — acceptable because main.go is the wiring file and splitting would create dependency tangles)
- [x] Tasks ordered by dependency — Wave 1 foundation, Wave 2 instrumentation, Wave 3 wiring/infra, Wave 4 tests
- [x] Tests in same task as code where feasible (6 of 18 tasks include their own unit tests)
- [x] Independent tasks parallelizable: Wave 2 has 7 tasks (6–12) that can run in parallel after Wave 1

**Test compliance:**
- [x] Test task exists for each AC (see Acceptance Criteria Mapping table)
- [x] Integration test (Task 17) covers end-to-end span + metric scrape
- [x] All tests run with `-race`

**Self-containment:**
- [x] API specs embedded (Prometheus format)
- [x] Config env vars embedded with defaults + validation
- [x] Metric definitions embedded (no "see metrics.go")
- [x] Dashboard PromQL embedded (no "see Grafana docs")
- [x] Alert YAML embedded
- [x] Every task's Context refs point to sections present in this plan

**Zero-deferral compliance:** AC-1..AC-11 all mapped to implementation tasks. No deferral, no TODO. PASS.

Self-validation: **PASS**. Writing plan now.
