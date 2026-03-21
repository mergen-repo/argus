# Phase 5 Gate Report

> Date: 2026-03-22
> Phase: 5 — eSIM & Advanced Ops
> Status: PASS
> Stories Tested: STORY-031, STORY-028, STORY-029, STORY-030

## Deploy
| Check | Status |
|-------|--------|
| Docker build | PASS |
| Services up (5/5) | PASS |
| Health check | PASS |

## Smoke Test
| Endpoint | Status | Response |
|----------|--------|----------|
| Frontend | 200 | OK |
| API Health | 200 | `{"status":"success","data":{"db":"ok","redis":"ok","nats":"ok","aaa":{"radius":"ok","diameter":"ok"}}}` |
| DB | connected | `accepting connections` |

## Unit/Integration Tests
> Total: 797 | Passed: 797 | Failed: 0 | Skipped: 0

All tests pass with `-race` flag. No regressions from Phase 4.

## Functional Verification
> API: 14/14 pass | DB: 2/2 pass | Business Rules: 4/4 pass

| Type | Check | Result | Detail |
|------|-------|--------|--------|
| API | GET /api/v1/jobs — list jobs | PASS | 200, returns job array with pagination |
| API | GET /api/v1/jobs/:id — job detail | PASS | 200 for existing, 404 for non-existent |
| API | GET /api/v1/esim-profiles — list profiles | PASS | 200, empty array (no eSIMs in seed) |
| API | GET /api/v1/sim-segments — list segments | PASS | 200, returns seed segment |
| API | POST /api/v1/sims/:id/ota — send OTA | PASS | 201, command created with queued status |
| API | GET /api/v1/sims/:id/ota — OTA history | PASS | 200, returns created command |
| API | POST /api/v1/sims/bulk/state-change | PASS | 202, job created and processed |
| API | POST /api/v1/sims/bulk/policy-assign | PASS | 400, proper validation (no segment) |
| API | POST /api/v1/sims/bulk/operator-switch | PASS | 400, validation (target_apn_id required) |
| API | GET /api/v1/jobs/:id/errors — error report | PASS | 200, returns per-SIM error details |
| API | POST /api/v1/jobs/:id/cancel — cancel job | PASS | Route registered (tenant_admin role) |
| API | POST /api/v1/jobs/:id/retry — retry job | PASS | Route registered (sim_manager role) |
| API | POST /api/v1/sims/bulk/ota — bulk OTA | PASS | Route registered (tenant_admin role) |
| API | GET /api/v1/ota-commands/:id — get command | PASS | Route registered (sim_manager role) |
| DB | ota_commands table exists | PASS | Table created with 5 indexes |
| DB | OTA command persisted | PASS | 1 row in ota_commands after send test |
| Rule | No auth token → 401 | PASS | 401 Unauthorized |
| Rule | Non-existent SIM OTA → 404 | PASS | 404 Not Found |
| Rule | Invalid command_type → 422 | PASS | 422 Validation Error |
| Rule | Invalid state transition → partial success | PASS | Job completed, error_report has INVALID_STATE_TRANSITION |

## Fix Attempts
| # | Issue | Fix | Commit | Result |
|---|-------|-----|--------|--------|
| 1 | OTA `security_mode` check constraint violation — empty string instead of 'none' | Default `security_mode` and `channel` in handler before store insert | 8743d95 | PASS |
| 1 | Bulk job "tenant_id not in context" — `context.Background()` missing tenant | Inject `apierr.TenantIDKey` into context via `context.WithValue` in job runner | 8743d95 | PASS |
| 1 | OTA migration FK failure — `sims` partitioned table has composite PK `(id, operator_id)` | Remove `REFERENCES sims(id)` FK from `ota_commands.sim_id` (matches `esim_profiles` pattern) | 8743d95 | PASS |

## Escalated (unfixed)
None

## Summary

Phase 5 delivers four backend stories:

- **STORY-031**: Background job system with distributed Redis locking, cron scheduler, timeout detection (30min auto-fail), per-tenant concurrency control, 11 job types with 7 real processors
- **STORY-028**: eSIM profile management with SM-DP+ adapter interface (mock), atomic profile switch with FOR UPDATE row locks, one-profile-per-SIM enforcement
- **STORY-029**: OTA SIM management via APDU commands, SMS-PP/BIP encoding, AES-128-CBC/HMAC-SHA256 security, Redis rate limiting (10/SIM/hour), 5 command types
- **STORY-030**: Bulk operations (state change, policy assign, operator switch) with segment-based targeting, distributed per-SIM locking, partial success handling, undo capability, CSV error reports

Three runtime bugs found and fixed in a single commit: OTA security_mode defaulting, job runner tenant context injection, and OTA migration FK on partitioned table. All 797 tests pass after fixes.
