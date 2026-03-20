# STORY-014 Deliverable: MSISDN Number Pool Management

## Summary

MSISDN pool management with CSV import, list with state filtering, assign to SIM, global uniqueness, and grace period release on SIM termination. Route registration and DI wiring completed the pre-existing store/handler implementation.

## Acceptance Criteria Status

| # | Criteria | Status |
|---|---------|--------|
| 1 | POST /api/v1/msisdn-pool/import accepts CSV of MSISDNs per operator | DONE |
| 2 | GET /api/v1/msisdn-pool lists numbers with state (available/assigned/reserved) | DONE |
| 3 | POST /api/v1/msisdn-pool/:id/assign assigns MSISDN to SIM | DONE |
| 4 | MSISDN globally unique across all tenants/operators | DONE |
| 5 | Released on SIM termination (after grace period) | DONE |

## Files Changed

| File | Change |
|------|--------|
| `internal/gateway/router.go` | MODIFIED — 3 MSISDN routes registered with RBAC |
| `cmd/argus/main.go` | MODIFIED — MSISDNStore + MSISDNHandler wired into DI |
| `internal/gateway/router_test.go` | MODIFIED — Route registration tests |
| `internal/store/msisdn.go` | MODIFIED — Release with grace period (reserved state + reserved_until) |
| `internal/store/sim.go` | MODIFIED — SIM terminate now releases assigned MSISDN |
| `internal/api/msisdn/handler.go` | MODIFIED — HasMore in ListMeta |
| `internal/store/msisdn_test.go` | MODIFIED — 3 new tests (struct fields, partial success, error JSON) |
| `internal/api/msisdn/handler_test.go` | MODIFIED — 7 new tests (import/assign validation, CSV, state, JSON) |

## Architecture References Fulfilled

- SVC-03: Core API — MSISDN pool endpoints
- API-160 to API-162: Import, List, Assign
- TBL-24 (msisdn_pool): Pool management with state machine

## Gate Results

- Gate Status: PASS
- Fixes Applied: 5 (grace period release, MSISDN release on SIM terminate, HasMore, 10 new tests)
- Escalated: 0

## Test Coverage

- 10 new tests across store and handler packages
- Full suite: 29/29 packages pass
