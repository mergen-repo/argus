# Deliverable: STORY-030 — Bulk Operations (State Change, Policy Assign, Operator Switch)

## Summary

Implemented bulk operations on SIM segments: bulk state change, bulk policy assignment, and bulk eSIM operator switch. All operations run as background jobs via SVC-09 with partial success handling, undo capability, distributed per-SIM locking, progress tracking via NATS/WebSocket, and CSV error report export.

## Files Changed

### New Files
| File | Purpose |
|------|---------|
| `internal/api/sim/bulk_handler.go` | HTTP handler — 3 bulk operation endpoints |
| `internal/api/sim/bulk_handler_test.go` | Handler validation tests |
| `internal/job/bulk_types.go` | Shared types: BulkOpError, payload/undo structs, BulkResult |
| `internal/job/bulk_state_change.go` | Bulk state change processor — forward + undo modes |
| `internal/job/bulk_policy_assign.go` | Bulk policy assign processor — forward + undo modes |
| `internal/job/bulk_esim_switch.go` | Bulk eSIM operator switch processor — forward + undo modes |
| `internal/job/bulk_state_change_test.go` | Processor payload serialization tests |

### Modified Files
| File | Change |
|------|--------|
| `internal/store/segment.go` | Added ListMatchingSIMIDs, ListMatchingSIMIDsWithDetails |
| `internal/gateway/router.go` | Registered 3 bulk routes with role guards |
| `cmd/argus/main.go` | Replaced 3 stub processors with real implementations |
| `internal/api/job/handler.go` | CSV error report handles BulkOpError format |

## API Endpoints
| Ref | Method | Path | Auth | Description |
|-----|--------|------|------|-------------|
| API-064 | POST | `/api/v1/sims/bulk/state-change` | sim_manager+ | Bulk state change on segment |
| API-065 | POST | `/api/v1/sims/bulk/policy-assign` | policy_editor+ | Bulk policy assignment on segment |
| API-066 | POST | `/api/v1/sims/bulk/operator-switch` | tenant_admin | Bulk eSIM operator switch on segment |

## Key Implementation Details
- Batch size: 100 (configurable)
- Per-SIM distributed lock: `argus:lock:sim:{id}` with 30s TTL
- Progress published to NATS every batch
- Undo records stored in job result JSONB for revert capability
- Physical SIMs skipped during operator switch (eSIM-only)
- CSV error report with sim_id, iccid, error_code, error_message columns

## Test Coverage
- 13 new tests across handler and processor files
- 797 total tests passing, 0 failures, 0 regressions
- All 15 acceptance criteria covered
