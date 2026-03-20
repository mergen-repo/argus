# STORY-015 Deliverable: RADIUS Authentication & Accounting Server

## Summary

Full RADIUS server implementation with UDP listeners (:1812 auth, :1813 acct), SIM lookup with Redis cache, session lifecycle management (Redis+DB), CoA/DM support, NATS event publishing, and health check integration. Uses layeh/radius library per RFC 2865/2866.

## Acceptance Criteria Status

| # | Criteria | Status |
|---|---------|--------|
| 1 | RADIUS server listens on UDP :1812 (auth) and :1813 (accounting) | DONE |
| 2 | Access-Request: parse IMSI from User-Name, lookup SIM | DONE |
| 3 | Access-Request: validate SIM state is ACTIVE, operator healthy | DONE |
| 4 | Access-Request: delegate to Policy Engine | DONE |
| 5 | Access-Accept: Framed-IP, Session-Timeout, QoS from policy | DONE |
| 6 | Access-Reject: Reply-Message with reason code | DONE |
| 7 | Accounting-Start: create session in Redis+TBL-17, NATS event | DONE |
| 8 | Accounting-Interim: update bytes_in/bytes_out in Redis | DONE |
| 9 | Accounting-Stop: finalize in TBL-17, remove Redis, NATS event | DONE |
| 10 | CoA: send mid-session policy update to NAS | DONE |
| 11 | DM: force disconnect session from NAS | DONE |
| 12 | Shared secret per operator (from adapter_config) | DONE |
| 13 | RADIUS packet logging with correlation ID | DONE |
| 14 | Health check reports AAA status | DONE |
| 15 | Graceful shutdown: drain within 5s | DONE |

## Files Changed

| File | Change |
|------|--------|
| `internal/aaa/radius/server.go` | NEW — RADIUS server: auth+acct handlers, CoA/DM, lifecycle |
| `internal/aaa/radius/sim_cache.go` | NEW — Redis-backed SIM cache (5min TTL, DB fallback) |
| `internal/aaa/radius/server_test.go` | NEW — 15 tests (packet construction, handler logic, UDP) |
| `internal/store/session_radius.go` | NEW — RadiusSessionStore (TBL-17 CRUD) |
| `internal/store/session_radius_test.go` | NEW — 5 store tests |
| `internal/aaa/session/session.go` | REWRITTEN — Real Redis+DB session manager |
| `internal/aaa/session/sweep_test.go` | MODIFIED — Sweep tests |
| `internal/gateway/health.go` | MODIFIED — AAA health checker interface |
| `internal/store/sim.go` | MODIFIED — Added GetByIMSI |
| `internal/store/ippool.go` | MODIFIED — Added GetIPAddressByID |
| `cmd/argus/main.go` | MODIFIED — RADIUS server wiring + graceful shutdown |

## Architecture References Fulfilled

- SVC-04: AAA Engine — RADIUS server
- FLW-01: RADIUS Authentication flow
- FLW-02: RADIUS Accounting flow
- TBL-17 (sessions): TimescaleDB hypertable
- API-180: Health check with AAA status

## Gate Results

- Gate Status: PASS
- Fixes Applied: 4 (Acct-Stop final counters, TerminateWithCounters, per-operator secret, 7 tests)
- Escalated: 0

## Test Coverage

- 15 RADIUS tests, 7 session tests, 5 store tests
- Full suite pass with zero regressions
