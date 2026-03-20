# STORY-017 Deliverable: Session Management & Concurrent Control

## Summary

Session management layer with filtered listing, stats aggregation, force disconnect (CoA/DM), bulk disconnect as background job, concurrent session control with oldest eviction, idle/hard timeouts, and Redis session cache.

## Acceptance Criteria Status

| # | Criteria | Status |
|---|---------|--------|
| 1 | GET /api/v1/sessions with filters + cursor pagination | DONE |
| 2 | GET /api/v1/sessions cursor-based pagination | DONE |
| 3 | GET /api/v1/sessions/stats (total, by_operator, by_apn, avg) | DONE |
| 4 | POST /api/v1/sessions/:id/disconnect (CoA/DM) | DONE |
| 5 | POST /api/v1/sessions/bulk/disconnect (segment-based) | DONE |
| 6 | Concurrent session control (max_sessions_per_sim) | DONE |
| 7 | Idle timeout auto-disconnect | DONE |
| 8 | Hard timeout auto-disconnect | DONE |
| 9 | Redis session cache with TTL | DONE |
| 10 | NATS session events (started/ended) | DONE |
| 11 | Real-time duration/usage tracking via Redis | DONE |
| 12 | Force disconnect audit log | DONE |
| 13 | Bulk disconnect as job if >100 | DONE |

## Files Changed

| File | Change |
|------|--------|
| `internal/store/session_radius.go` | MODIFIED — ListActiveFiltered, GetActiveStats, CountActiveForSIM, GetOldestActiveForSIM |
| `internal/aaa/session/session.go` | MODIFIED — ListActive, Stats, CheckConcurrentLimit, SessionFilter extended |
| `internal/api/session/handler.go` | MODIFIED — Enhanced List with min_duration/min_usage, Stats handler |
| `internal/aaa/radius/server.go` | MODIFIED — Concurrent session control in handleAcctStart |
| `internal/gateway/router.go` | MODIFIED — 4 session routes registered |
| `cmd/argus/main.go` | MODIFIED — Session handler, timeout sweeper, bulk disconnect wiring |
| `internal/job/bulk_disconnect.go` | NEW — BulkDisconnectProcessor (job.Processor) |
| `internal/aaa/session/session_test.go` | NEW — 6 tests |
| `internal/job/bulk_disconnect_test.go` | NEW — 4 tests |
| `internal/gateway/router_test.go` | MODIFIED — Session route tests |

## Gate Results

- Gate Status: PASS
- Fixes Applied: 6 (CRITICAL: tenant_id scoping on ListActive/Stats, performance: SCAN key filter)
- Escalated: 0

## Test Coverage

- 24 story tests (session manager, bulk disconnect, routes)
- Full suite: 30/30 packages pass
