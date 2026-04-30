# STORY-065: Observability & Tracing Standardization

## User Story
As an SRE on-call for a 10M+ SIM platform, I want distributed tracing from every HTTP request down to DB and NATS, standard Prometheus metrics from a real client library, tenant-labeled logs and metrics, and ready-to-import Grafana dashboards with Prometheus alert rules, so that I can diagnose latency spikes, multi-tenant issues, and upstream failures in minutes instead of hours.

## Description
Current state: no distributed tracing (no OpenTelemetry), metrics are hand-written Prometheus text format via `fmt.Sprintf`, `tenant_id` is not in log lines or metric labels, no Grafana dashboard or alert rules exist in the repo, and DB/NATS/Redis operations have no first-class metrics. For a 10M SIM platform, this is unworkable. This story closes the observability gap comprehensively.

## Architecture Reference
- Services: ALL (cross-cutting)
- Packages: cmd/argus/main.go, internal/gateway (middleware chain), internal/analytics/metrics, internal/store (DB instrumentation), internal/bus (NATS instrumentation), internal/cache (Redis instrumentation)
- Source: Phase 10 production ops audit (6-agent scan 2026-04-11)
- New deps: `go.opentelemetry.io/otel`, `github.com/prometheus/client_golang/prometheus`, `github.com/XSAM/otelsql` (or equivalent DB instrumentation)

## Screen Reference
- Invisible to end users, but surfaces in Grafana dashboards and Prometheus alerting.
- SCR-120 (System Health) may gain a link to Grafana.

## Acceptance Criteria
- [ ] AC-1: **OpenTelemetry SDK wired.** `cmd/argus/main.go` initializes OTel tracer provider with OTLP exporter (gRPC) pointing to `OTEL_EXPORTER_OTLP_ENDPOINT` env var. Resource attributes include `service.name=argus`, `service.version=<git-sha>`, `deployment.environment=<env>`. Graceful shutdown flushes spans.
- [ ] AC-2: **HTTP instrumentation.** Chi router wraps with `otelhttp.NewHandler`. Every incoming request gets a root span with attributes: `http.method`, `http.route`, `http.status_code`, `correlation_id`, `tenant_id` (added after auth middleware extracts it), `user_id`.
- [ ] AC-3: **DB instrumentation.** pgx connection pool opened via `otelsql` wrapper so every `QueryRow`/`Query`/`Exec` produces a child span with `db.statement`, `db.operation`, `db.name`, `db.system=postgresql`. Slow query threshold 100ms flagged in span attribute.
- [ ] AC-4: **NATS instrumentation.** NATS publish/subscribe wrapped to inject trace context into message headers and extract on consume. `internal/bus/nats.go` `Publish()` helper adds `traceparent` header; consumers create child spans from it.
- [ ] AC-5: **Prometheus client_golang migration.** Delete custom `fmt.Sprintf("argus_auth_requests_per_second %d\n", ...)` code. Replace with real `CounterVec`/`HistogramVec`/`GaugeVec` from `prometheus/client_golang`. Handler at `/metrics` uses `promhttp.Handler()`. Default Go runtime metrics (`go_goroutines`, `go_memstats_*`, `process_*`, `promhttp_metric_handler_requests_total`) emitted automatically.
- [ ] AC-6: **Core metric set defined** with proper label sets:
  - `argus_http_requests_total{method,route,status,tenant_id}` — Counter
  - `argus_http_request_duration_seconds{method,route,tenant_id}` — Histogram, buckets tuned for 1ms–10s
  - `argus_aaa_auth_requests_total{protocol,operator_id,result,tenant_id}` — Counter
  - `argus_aaa_auth_latency_seconds{protocol,operator_id,tenant_id}` — Histogram
  - `argus_active_sessions{tenant_id,operator_id}` — Gauge
  - `argus_db_query_duration_seconds{operation,table}` — Histogram
  - `argus_db_pool_connections{state="idle|in_use|waiting"}` — Gauge
  - `argus_nats_published_total{subject}` / `argus_nats_consumed_total{subject}` — Counter
  - `argus_nats_pending_messages{subject}` — Gauge
  - `argus_redis_ops_total{op,result}` — Counter
  - `argus_redis_cache_hits_total{cache}` / `argus_redis_cache_misses_total{cache}` — Counter
  - `argus_job_runs_total{job_type,result}` — Counter
  - `argus_job_duration_seconds{job_type}` — Histogram
  - `argus_operator_health{operator_id}` — Gauge (0=down,1=degraded,2=healthy)
  - `argus_circuit_breaker_state{operator_id,state="closed|open|half_open"}` — Gauge
- [ ] AC-7: **tenant_id label in logs.** `internal/gateway/logging.go` middleware adds `tenant_id` to logger context after auth extraction. All subsequent log lines (via zerolog context) carry `tenant_id`. Grep test: no log produced inside authenticated handler lacks `tenant_id` field.
- [ ] AC-8: **Grafana dashboards committed** at `infra/grafana/dashboards/`:
  - `argus-overview.json` — request rate, error rate, p95/p99 latency, active sessions, goroutines, memory
  - `argus-aaa.json` — auth requests/sec per protocol, auth latency percentiles, operator health, circuit breaker states
  - `argus-database.json` — pool utilization, query duration, slow queries, connection waits
  - `argus-messaging.json` — NATS publish/consume rates, pending, slow consumers; Redis ops/s, hit rate, evictions
  - `argus-tenant.json` — per-tenant request rate, error rate, active SIMs, cost (templated by tenant_id label)
  - `argus-jobs.json` — job throughput, duration percentiles, failure rate, queue depth
- [ ] AC-9: **Prometheus alert rules** committed at `infra/prometheus/alerts.yml`:
  - `ArgusHighErrorRate`: rate(argus_http_requests_total{status=~"5.."}[5m]) > 0.05
  - `ArgusAuthLatencyHigh`: histogram_quantile(0.99, argus_aaa_auth_latency_seconds) > 0.5
  - `ArgusOperatorDown`: argus_operator_health == 0 for 2m
  - `ArgusCircuitBreakerOpen`: argus_circuit_breaker_state{state="open"} == 1 for 5m
  - `ArgusDBPoolExhausted`: argus_db_pool_connections{state="waiting"} > 0 for 1m
  - `ArgusNATSConsumerLag`: argus_nats_pending_messages > 10000 for 5m
  - `ArgusJobFailureRate`: rate(argus_job_runs_total{result="failure"}[10m]) > 0.1
  - `ArgusRedisEvictionStorm`: rate(redis_evicted_keys_total[5m]) > 100
  - `ArgusDiskSpaceLow`: node_filesystem_avail_bytes / node_filesystem_size_bytes < 0.15
- [ ] AC-10: **Correlation ID propagation** verified: request → log → span → downstream service. Test: trigger request, find correlation_id in logs, find same ID as span attribute, verify trace appears in Jaeger/Tempo UI.
- [ ] AC-11: **Operator health + circuit breaker** exposed as Prometheus gauges (not just logged). SoR engine, adapter, breaker all emit state changes through metric updates.

## Dependencies
- Blocked by: STORY-063 (health endpoint real probes feed into dashboards)
- Blocks: STORY-066 (alert rules depend on metrics existing), STORY-067 (dashboards referenced by runbook)

## Test Scenarios
- [ ] Integration: Hit `/api/v1/sims` → request produces trace in OTLP collector, DB span is child, NATS publish (if any) is child.
- [ ] Integration: `curl /metrics` → output includes `argus_http_requests_total` counter with tenant_id label, Go runtime metrics present, no custom `fmt.Sprintf` format remnants.
- [ ] Integration: Load test 1000 req/s for 30s → Grafana dashboard shows populated panels within 15s scrape interval.
- [ ] Alert test: Simulate operator down → `ArgusOperatorDown` alert fires after 2m, resolves after recovery.
- [ ] Unit: Log output from authenticated handler contains `tenant_id=<uuid>` field.
- [ ] Build: `go test ./... -race` green with new otel/prom deps.

## Effort Estimate
- Size: L
- Complexity: Medium-High (broad touch, but each change is mechanical)
