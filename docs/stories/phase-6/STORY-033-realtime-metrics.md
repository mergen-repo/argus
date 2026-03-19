# STORY-033: Built-In Observability & Real-Time Metrics

## User Story
As a system administrator, I want real-time metrics for authentication rate, latency percentiles, error rate, and session count, so that I can monitor system health and performance.

## Description
Built-in observability layer: Redis-based counters for auth/s, session count, error rate. Latency percentiles (p50, p95, p99) tracked via HyperLogLog or T-Digest in Redis. Metrics pushed to connected portal clients via WebSocket at 1-second intervals. System health dashboard shows all metrics. No external metrics stack required (Prometheus optional via /metrics endpoint).

## Architecture Reference
- Services: SVC-07 (Analytics Engine — internal/analytics/metrics), SVC-02 (WebSocket), SVC-04 (AAA — metrics source)
- API Endpoints: API-181
- Source: docs/architecture/api/_index.md (System Health section)

## Screen Reference
- SCR-120: System Health — auth/s gauge, latency chart, error rate, session count, service status

## Acceptance Criteria
- [ ] Auth rate: Redis INCR with 1s TTL window, exposed as auth_requests_per_second
- [ ] Auth success/failure counters: separate Redis counters, calculate error_rate
- [ ] Latency tracking: record auth latency per request in Redis sorted set (sliding window 60s)
- [ ] Latency percentiles: p50, p95, p99 calculated from sliding window
- [ ] Session count: Redis SCARD on active sessions set
- [ ] GET /api/v1/system/metrics returns: auth_per_sec, auth_error_rate, latency_p50/p95/p99, active_sessions, by_operator breakdown
- [ ] WebSocket: push metrics.realtime event every 1 second to subscribed clients
- [ ] metrics.realtime payload: {auth_per_sec, error_rate, latency_p50, latency_p95, active_sessions, timestamp}
- [ ] Optional Prometheus endpoint: GET /metrics (OpenMetrics format)
- [ ] Per-operator metrics: auth rate, error rate, latency per operator
- [ ] Metric retention: Redis keys with TTL, no unbounded memory growth
- [ ] System health: aggregate status (healthy/degraded/critical) based on metric thresholds

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-181 | GET | /api/v1/system/metrics | — | `{auth_per_sec,auth_error_rate,latency:{p50,p95,p99},active_sessions,by_operator:{},system_status}` | JWT(super_admin) | 200 |

## Dependencies
- Blocked by: STORY-015 (RADIUS — generates auth metrics), STORY-017 (sessions — session count)
- Blocks: STORY-043 (frontend dashboard uses metrics)

## Test Scenarios
- [ ] 100 auth requests in 1s → auth_per_sec ≈ 100
- [ ] 10 failures out of 100 → error_rate ≈ 0.10
- [ ] Auth latencies [1ms, 2ms, 5ms, 10ms, 50ms] → p50=5ms, p95=50ms
- [ ] Active sessions count matches Redis session set cardinality
- [ ] WebSocket client receives metrics.realtime event every 1s
- [ ] Metrics per operator: Operator A auth/s separate from Operator B
- [ ] Prometheus /metrics endpoint returns valid OpenMetrics format
- [ ] System status "degraded" when error_rate > 5%
- [ ] System status "critical" when error_rate > 20%

## Effort Estimate
- Size: L
- Complexity: Medium
