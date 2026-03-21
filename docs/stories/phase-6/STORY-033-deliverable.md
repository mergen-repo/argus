# Deliverable: STORY-033 — Built-In Observability & Real-Time Metrics

## Summary

Implemented built-in observability layer with Redis-based real-time metrics. Auth rate, error rate, latency percentiles (p50/p95/p99), and session count tracked in Redis with TTL-bounded keys. Metrics pushed via WebSocket at 1-second intervals. REST endpoint for full metric snapshot. Optional Prometheus/OpenMetrics endpoint. RADIUS server instrumented with MetricsRecorder interface.

## Files Changed

### New Files
| File | Purpose |
|------|---------|
| `internal/analytics/metrics/types.go` | SystemMetrics, OperatorMetrics, LatencyPercentiles, SystemStatus |
| `internal/analytics/metrics/collector.go` | Redis-based metrics collector with auth counters, latency sorted sets |
| `internal/analytics/metrics/pusher.go` | WebSocket pusher — broadcasts metrics.realtime every 1s |
| `internal/analytics/metrics/collector_test.go` | 9 collector tests |
| `internal/analytics/metrics/pusher_test.go` | 1 pusher test |
| `internal/api/metrics/handler.go` | GET /api/v1/system/metrics (super_admin) |
| `internal/api/metrics/prometheus.go` | GET /metrics (OpenMetrics format, no auth) |
| `internal/api/metrics/handler_test.go` | 3 handler tests |

### Modified Files
| File | Change |
|------|--------|
| `internal/aaa/radius/server.go` | MetricsRecorder interface, auth instrumentation |
| `internal/gateway/router.go` | Metrics routes registered |
| `cmd/argus/main.go` | Wired collector, pusher, handler, RADIUS instrumentation |

## API Endpoints
| Ref | Method | Path | Auth | Description |
|-----|--------|------|------|-------------|
| API-181 | GET | `/api/v1/system/metrics` | super_admin | Full metrics snapshot with by_operator breakdown |
| — | GET | `/metrics` | none | Prometheus/OpenMetrics format |

## Test Coverage
- 13 new tests across 3 test files
- All 43 packages pass, 0 regressions
- All 11 acceptance criteria covered
