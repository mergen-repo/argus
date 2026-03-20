# STORY-011 Deliverable: SIM CRUD & State Machine

## Summary

Full SIM lifecycle management implementing 9 API endpoints, complete state machine with 7 valid transitions, cursor-based pagination with 9 filter parameters, IP allocation on activation, audit logging for all state changes, and auto-purge scheduling.

## Acceptance Criteria Status

| # | Criteria | Status |
|---|---------|--------|
| 1 | POST /api/v1/sims creates SIM in ORDERED state | DONE |
| 2 | POST /api/v1/sims/:id/activate → ORDERED→ACTIVE, allocates IP, assigns default policy | DONE |
| 3 | POST /api/v1/sims/:id/suspend → ACTIVE→SUSPENDED, retains IP | DONE |
| 4 | POST /api/v1/sims/:id/resume → SUSPENDED→ACTIVE | DONE |
| 5 | POST /api/v1/sims/:id/terminate → ACTIVE/SUSPENDED→TERMINATED, schedules IP reclaim + purge | DONE |
| 6 | POST /api/v1/sims/:id/report-lost → ACTIVE→STOLEN_LOST | DONE |
| 7 | Invalid transitions return 422 INVALID_STATE_TRANSITION | DONE |
| 8 | Every state transition creates sim_state_history entry | DONE |
| 9 | GET /api/v1/sims supports combo search (ICCID, IMSI, MSISDN, operator, APN, state, RAT) | DONE |
| 10 | GET /api/v1/sims uses cursor-based pagination | DONE |
| 11 | GET /api/v1/sims/:id returns full detail | DONE |
| 12 | ICCID and IMSI globally unique (enforced by DB unique index) | DONE |
| 13 | purge_at = terminated_at + tenant.purge_retention_days | DONE |
| 14 | Audit log entry for every state change | DONE |

## Files Changed

| File | Change |
|------|--------|
| `internal/store/sim.go` | NEW — SIM store: 23-column struct, CRUD, state machine (7 transitions), cursor pagination, IP allocation |
| `internal/api/sim/handler.go` | NEW — 9 HTTP endpoints with validation, operator/APN existence checks, audit logging |
| `internal/store/sim_test.go` | NEW — 20+ tests: struct fields, state machine (7 valid, 18 invalid transitions), validation |
| `internal/api/sim/handler_test.go` | NEW — Handler tests: query parsing, response conversion, SIM/RAT type validation |
| `internal/gateway/router.go` | MODIFIED — SIM routes registered (sim_manager+, tenant_admin for terminate) |
| `cmd/argus/main.go` | MODIFIED — SIM store + handler wired into DI |
| `internal/apierr/apierr.go` | MODIFIED — Added ICCID_EXISTS, IMSI_EXISTS, INVALID_STATE_TRANSITION error codes |
| `internal/job/import.go` | MODIFIED — Updated for new SIMStore.Create signature (tenantID parameter) |
| `internal/store/stubs.go` | DELETED — Stub types superseded by real SIM implementation |

## Architecture References Fulfilled

- SVC-03: Core API — SIM endpoints
- API-040 to API-052: SIM CRUD + state transitions + history
- TBL-10 (sims): 23 columns mapped
- TBL-11 (sim_state_history): Full history tracking
- TBL-09 (ip_addresses): IP allocation on activation

## Gate Results

- Gate Status: PASS
- Fixes Applied: 5 (2 security — SQL injection parameterization, 1 performance — updated_at timestamps, 2 test — validation coverage)
- Escalated: 0

## Test Coverage

- Store tests: struct fields, nil handling, state machine validation (7 valid + 18 invalid transitions)
- Handler tests: query parsing, response conversion, SIM/RAT type validation
- Full suite: 29/29 packages pass
