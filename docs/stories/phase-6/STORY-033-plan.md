# STORY-033 Plan: Built-In Observability & Real-Time Metrics

## Overview
Add a built-in observability layer with Redis-based metrics collection, REST API endpoint (API-181), Prometheus-compatible /metrics endpoint, and WebSocket real-time push. All metrics are per-operator and aggregated, with system health status derivation.

## Architecture Decisions
- **Metrics Collector** (`internal/analytics/metrics/collector.go`): Central service that records auth events and computes aggregated metrics from Redis. Uses sliding-window Redis keys with TTLs to prevent unbounded memory growth.
- **Latency tracking**: Reuses `SLATracker` pattern (sorted set + ZRemRangeByScore) but with 60s sliding window for real-time metrics, separate from SLA's 1-hour window.
- **Auth counters**: Redis INCR with 1s TTL windows for per-second rate; rolling 60s counters for error rate calculation.
- **Session count**: Queries DB via existing `RadiusSessionStore.CountActive()` (accurate, already indexed).
- **WebSocket push**: 1s ticker goroutine in metrics collector broadcasts via `hub.BroadcastAll("metrics.realtime", ...)`.
- **Prometheus**: Simple text/plain handler outputting OpenMetrics format — no external dependency needed.
- **Per-operator**: All counters keyed by operator_id; aggregation sums across operators.

## Key Pattern: Redis Keys
| Purpose | Key Pattern | Type | TTL |
|---------|------------|------|-----|
| Auth total (1s window) | `metrics:auth:total:{epoch_sec}` | INCR | 5s |
| Auth success (1s window) | `metrics:auth:success:{epoch_sec}` | INCR | 5s |
| Auth failure (1s window) | `metrics:auth:failure:{epoch_sec}` | INCR | 5s |
| Auth total per-op (1s) | `metrics:auth:total:{operator_id}:{epoch_sec}` | INCR | 5s |
| Auth success per-op (1s) | `metrics:auth:success:{operator_id}:{epoch_sec}` | INCR | 5s |
| Auth failure per-op (1s) | `metrics:auth:failure:{operator_id}:{epoch_sec}` | INCR | 5s |
| Auth latency (60s window) | `metrics:latency:global` | ZSET(score=timestamp, member=latency_ms:nano_suffix) | 120s |
| Auth latency per-op | `metrics:latency:{operator_id}` | ZSET | 120s |

## Tasks

### Task 1: Metrics Collector Core (`internal/analytics/metrics/collector.go`)
**Files:** `internal/analytics/metrics/collector.go`
- `Collector` struct: holds `*redis.Client`, `zerolog.Logger`
- `RecordAuth(ctx, operatorID uuid.UUID, success bool, latencyMs int)` — INCR counters + ZADD latency
- `GetMetrics(ctx) (SystemMetrics, error)` — reads all Redis keys, computes aggregates
- `GetOperatorMetrics(ctx, operatorID) (OperatorMetrics, error)`
- Sliding window prune on latency sorted sets (ZRemRangeByScore older than 60s)
- All Redis keys have TTL to prevent unbounded growth

### Task 2: Metrics Data Types (`internal/analytics/metrics/types.go`)
**Files:** `internal/analytics/metrics/types.go`
- `LatencyPercentiles` struct: P50, P95, P99
- `OperatorMetrics` struct: AuthPerSec, AuthErrorRate, Latency, OperatorID, OperatorCode
- `SystemMetrics` struct: AuthPerSec, AuthErrorRate, Latency, ActiveSessions, ByOperator map, SystemStatus
- `SystemStatus` type: "healthy", "degraded", "critical"
- Thresholds: error_rate > 5% = degraded, > 20% = critical

### Task 3: Session Counter Interface (`internal/analytics/metrics/collector.go` addition)
**Files:** `internal/analytics/metrics/collector.go`
- `SessionCounter` interface: `CountActive(ctx) (int64, error)`
- `Collector.SetSessionCounter(sc SessionCounter)` — setter for injection
- `GetMetrics` uses this to fetch active session count
- This interface is satisfied by existing `RadiusSessionStore` and `session.Manager`

### Task 4: RADIUS Server Integration — Instrument Auth Handlers
**Files:** `internal/aaa/radius/server.go`
- Add `metricsCollector` field to `Server` struct
- Add `SetMetricsCollector(mc MetricsRecorder)` method
- `MetricsRecorder` interface: `RecordAuth(ctx, operatorID uuid.UUID, success bool, latencyMs int)`
- In `handleDirectAuth`: after Accept/Reject, call `mc.RecordAuth(ctx, op.ID, true/false, latencyMs)`
- In `handleEAPAuth` (success/failure paths): same instrumentation
- Latency = `time.Since(startTime).Milliseconds()`

### Task 5: Metrics WebSocket Pusher (`internal/analytics/metrics/pusher.go`)
**Files:** `internal/analytics/metrics/pusher.go`
- `Pusher` struct: holds `*Collector`, `*ws.Hub`, `zerolog.Logger`, `stopCh chan struct{}`
- `Start()` — spawns goroutine with 1s ticker
- Each tick: `collector.GetMetrics(ctx)` -> `hub.BroadcastAll("metrics.realtime", payload)`
- `Stop()` — closes stopCh, waits for goroutine
- Payload: `{auth_per_sec, error_rate, latency_p50, latency_p95, active_sessions, timestamp}`

### Task 6: REST API Handler (`internal/api/metrics/handler.go`)
**Files:** `internal/api/metrics/handler.go`
- `Handler` struct: holds `*metrics.Collector`, `zerolog.Logger`
- `GetSystemMetrics(w, r)` — calls `collector.GetMetrics(ctx)`, returns standard envelope
- Response: `{status: "success", data: {auth_per_sec, auth_error_rate, latency: {p50, p95, p99}, active_sessions, by_operator: {...}, system_status}}`

### Task 7: Prometheus Endpoint (`internal/api/metrics/prometheus.go`)
**Files:** `internal/api/metrics/prometheus.go`
- `PrometheusHandler(w, r)` method on Handler
- Outputs OpenMetrics text format:
  - `argus_auth_requests_per_second`
  - `argus_auth_error_rate`
  - `argus_latency_p50_ms`, `_p95_ms`, `_p99_ms`
  - `argus_active_sessions`
  - Per-operator labels: `{operator_id="..."}`
- Content-Type: `text/plain; version=0.0.4; charset=utf-8`

### Task 8: Router & Main Wiring
**Files:** `internal/gateway/router.go`, `cmd/argus/main.go`
- Add `MetricsHandler *metricsapi.Handler` to `RouterDeps`
- Route: `GET /api/v1/system/metrics` (super_admin)
- Route: `GET /metrics` (no auth — Prometheus scraping)
- In `main.go`:
  - Create `metrics.NewCollector(rdb.Client, log.Logger)`
  - Set session counter: `collector.SetSessionCounter(sessionMgr)` (or radiusSessionStore)
  - Instrument RADIUS server: `radiusServer.SetMetricsCollector(collector)`
  - Create `metrics.NewPusher(collector, wsHub, log.Logger)` and start/stop
  - Create handler and inject into router deps

### Task 9: Tests (`internal/analytics/metrics/collector_test.go`, `metrics_test.go`)
**Files:** `internal/analytics/metrics/collector_test.go`, `internal/analytics/metrics/pusher_test.go`, `internal/api/metrics/handler_test.go`
- TestRecordAuth_IncrementsCounters — record 100 auths, verify auth_per_sec ≈ 100
- TestRecordAuth_ErrorRate — 10 failures / 100 total = 0.10
- TestLatencyPercentiles — known latency set, verify p50/p95/p99
- TestSystemStatus_Healthy/Degraded/Critical — verify threshold logic
- TestPrometheus_Format — verify text output contains expected metric names
- TestPusher_BroadcastsMetrics — mock hub, verify BroadcastAll called with correct event type

## Dependency Order
```
Task 2 (types) → Task 1+3 (collector) → Task 4 (RADIUS instrumentation)
                                       → Task 5 (pusher)
                                       → Task 6 (REST handler)
                                       → Task 7 (prometheus)
                                       → Task 8 (wiring)
                                       → Task 9 (tests)
```

## Wave Plan
- **Wave 1:** Task 2 (types) + Task 1 + Task 3 (collector with session counter)
- **Wave 2:** Task 4 (RADIUS), Task 5 (pusher), Task 6 (REST handler), Task 7 (prometheus)
- **Wave 3:** Task 8 (wiring)
- **Wave 4:** Task 9 (tests)

## Files Created/Modified
| File | Action |
|------|--------|
| `internal/analytics/metrics/types.go` | CREATE |
| `internal/analytics/metrics/collector.go` | CREATE |
| `internal/analytics/metrics/pusher.go` | CREATE |
| `internal/api/metrics/handler.go` | CREATE |
| `internal/api/metrics/prometheus.go` | CREATE |
| `internal/aaa/radius/server.go` | MODIFY |
| `internal/gateway/router.go` | MODIFY |
| `cmd/argus/main.go` | MODIFY |
| `internal/analytics/metrics/collector_test.go` | CREATE |
| `internal/analytics/metrics/pusher_test.go` | CREATE |
| `internal/api/metrics/handler_test.go` | CREATE |
