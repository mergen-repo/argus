# Gate Report: STORY-025 — Policy Staged Rollout (Canary)

**Date:** 2026-03-21
**Phase:** 4
**Result:** PASS (with fixes applied)

## Summary

Staged policy rollout implementation delivering canary deployment (1% -> 10% -> 100%) with rollback capability, CoA for active sessions, NATS progress events, WebSocket push, and async job support for large stages. Backend-only story (Pass 6 SKIPPED).

## Test Results

| Suite | Tests | Result |
|-------|-------|--------|
| `internal/policy/rollout/...` | 10 | ALL PASS |
| `internal/job/... -run Rollout` | 3 | ALL PASS |
| `internal/api/policy/...` | 36 (12 new rollout) | ALL PASS |
| Full suite (`go test ./...`) | 1008 | ALL PASS |
| Build (`go build ./...`) | - | CLEAN |

## Pass 1: Requirements Tracing (15 ACs)

| # | Acceptance Criterion | Status | Implementation |
|---|---------------------|--------|----------------|
| 1 | POST activate activates immediately (100%) | DONE (STORY-023) | `handler.go:ActivateVersion` |
| 2 | POST rollout starts staged rollout at 1% | PASS | `service.go:StartRollout` -> `handler.go:StartRollout` |
| 3 | Rollout record in TBL-16 with stages JSON | PASS | `store/policy.go:CreateRollout` (INSERT policy_rollouts) |
| 4 | Stage 1: 1% SIMs selected, TBL-15 updated, CoA sent | PASS | `service.go:ExecuteStage` -> `SelectSIMsForStage` -> `AssignSIMsToVersion` -> `sendCoAForSIM` |
| 5 | POST advance moves to next stage | PASS | `service.go:AdvanceRollout` -> `handler.go:AdvanceRollout` |
| 6 | Advance requires explicit user action | PASS | Separate POST endpoint, no auto-advance logic |
| 7 | Concurrent versions during rollout | PASS | `policy_assignments` tracks per-SIM version; `sims.policy_version_id` updated individually |
| 8 | Policy eval uses SIM-specific version from TBL-15 | PASS (by design) | Existing policy evaluator resolve_policy step |
| 9 | POST rollback reverts all SIMs | PASS | `service.go:RollbackRollout` -> `RevertRolloutAssignments` |
| 10 | Rollback triggers mass CoA | PASS | Batch CoA in `RollbackRollout` via `sendCoAForSIM` per SIM |
| 11 | GET rollout returns progress | PASS | `service.go:GetProgress` -> `handler.go:GetRollout` |
| 12 | NATS: publish rollout_progress | PASS | `bus.SubjectPolicyRolloutProgress`, published after each batch and stage completion |
| 13 | WebSocket: push progress | PASS | `SubjectPolicyRolloutProgress` in WS hub NATS subscription list (`main.go:198`) |
| 14 | CoA failure logged but continues | PASS | `sendCoAForSIM` logs warn and continues; `coa_status` set to "failed" |
| 15 | Only one active rollout per policy | PASS | `GetActiveRolloutForPolicy` check + `CreateRollout` tx-level COUNT check |

**Coverage: 15/15 ACs satisfied**

## Pass 2: Compliance

| Check | Status | Notes |
|-------|--------|-------|
| Standard envelope `{status, data}` | PASS | All 4 handlers use `apierr.WriteSuccess` / `apierr.WriteError` |
| tenant_id scoping | PASS | `GetRolloutByIDWithTenant` joins through `policy_versions -> policies` for tenant check; `SelectSIMsForStage` uses `s.tenant_id = $1` |
| Audit logging | PASS | `createAuditEntry` called for start, advance, rollback (3 state-changing endpoints); GetRollout is read-only |
| Naming conventions | PASS | Go camelCase, DB snake_case, routes kebab-case |
| Layer separation | PASS | Store (CRUD) -> Service (business logic) -> Handler (HTTP) -> Router (registration) |
| Error codes | PASS | `VERSION_NOT_DRAFT`, `ROLLOUT_IN_PROGRESS`, `ROLLOUT_COMPLETED`, `ROLLOUT_ROLLED_BACK`, `STAGE_IN_PROGRESS` |
| ADR-001 compliance | PASS | All code in `internal/` packages |
| HTTP status codes | PASS | 201 (start), 200 (advance/rollback/get), 404, 422 as specified |

## Pass 2.5: Security

| Check | Status | Notes |
|-------|--------|-------|
| SQL injection | PASS | All queries use parameterized placeholders ($1, $2, etc.) |
| Auth middleware | PASS | All 4 routes under `RequireRole("policy_editor")` in router.go |
| Input validation | PASS | UUID format validation, stage percentages (1-100, ascending, last=100), JSON body parsing |
| Nil-safe CoA | PASS | `sendCoAForSIM` checks `sessionProvider == nil || coaDispatcher == nil` |
| Nil-safe event bus | PASS | `publishProgressWithState` checks `eventBus == nil` |

## Pass 3: Tests

- **Rollout service tests (10):** Constructor, setter, CoA with nil providers, CoA with no sessions, CoA with sessions, progress nil bus, stage JSON round-trip, event serialization, default stages
- **Job processor tests (3):** Type() value, payload unmarshal, missing fields handling
- **Handler tests (12 new rollout):** Invalid version ID, no service, invalid stages (4 sub-tests), invalid rollout ID (advance/rollback/get), no service (advance/rollback/get), toRolloutResponse with/without optionals

## Pass 4: Performance

| Check | Status | Notes |
|-------|--------|-------|
| Batch CoA | PASS | `batchSize = 1000` constant, SIMs processed in batches |
| Async threshold | PASS | `asyncThreshold = 100000`, stages > threshold create background job |
| SELECT random() with LIMIT | PASS | `ORDER BY random() LIMIT $N FOR UPDATE SKIP LOCKED` |
| Index usage | PASS | `idx_policy_assignments_sim` (unique), `idx_policy_assignments_rollout`, `idx_policy_rollouts_state` |
| Transaction safety | PASS | `CreateRollout`, `CompleteRollout`, `RollbackRollout`, `AssignSIMsToVersion`, `RevertRolloutAssignments` all use BEGIN/COMMIT with defer Rollback |
| SELECT FOR UPDATE | PASS | Used in `SelectSIMsForStage` with SKIP LOCKED, `CreateRollout`, `CompleteRollout`, `RollbackRollout` |

## Pass 5: Build

```
go build ./... -> CLEAN (no errors, no warnings)
go test ./... -> 1008 tests, ALL PASS, 0 failures
```

## Pass 6: Frontend

SKIPPED (backend-only story, frontend in STORY-046)

## Issues Found & Fixed

### FIX-1 (Critical): ExecuteStage passed uuid.Nil as tenantID
- **File:** `internal/policy/rollout/service.go`
- **Problem:** `SelectSIMsForStage` was called with `uuid.Nil` as tenantID, meaning `WHERE s.tenant_id = $1` would match no SIMs in production.
- **Fix:** Added `resolveTenantID()` helper that queries `GetTenantIDForRollout()` (new store method, joins through policy_rollouts -> policy_versions -> policies). ExecuteStage now resolves correct tenantID before SIM selection.

### FIX-2 (Minor): rolloutResponse missing PolicyID population
- **File:** `internal/api/policy/handler.go`
- **Problem:** `rolloutResponse.PolicyID` field existed but was never populated in `toRolloutResponse()`. API-099 spec requires `policy_id` in response.
- **Fix:** `GetRollout` and `StartRollout` handlers now resolve `policy_id` via `GetPolicyIDForRollout()` (new store method).

### FIX-3 (Minor): rolloutResponse missing errors field
- **File:** `internal/api/policy/handler.go`
- **Problem:** API-099 spec defines `errors: []` in response, but `rolloutResponse` struct had no `Errors` field.
- **Fix:** Added `Errors []string` field initialized as empty slice in `toRolloutResponse()`.

### New Store Methods Added
- `GetTenantIDForRollout(ctx, rolloutID)` - resolves tenant_id via join chain
- `GetPolicyIDForRollout(ctx, rolloutID)` - resolves policy_id via join

## Files Changed

| File | Change Type | Key Changes |
|------|------------|-------------|
| `internal/policy/rollout/service.go` | Modified | Rollout service with StartRollout, ExecuteStage, AdvanceRollout, RollbackRollout, GetProgress, CoA dispatch, NATS events, async job support |
| `internal/policy/rollout/service_test.go` | New | 10 unit tests for service, mocks for SessionProvider/CoADispatcher |
| `internal/job/rollout.go` | New | RolloutStageProcessor implementing Processor interface |
| `internal/job/rollout_test.go` | New | 3 unit tests for processor Type and payload unmarshal |
| `internal/api/policy/handler.go` | Modified | 4 new handler methods (StartRollout, AdvanceRollout, RollbackRollout, GetRollout), response types, error mapping |
| `internal/api/policy/handler_test.go` | Modified | 12 new handler tests for rollout endpoints |
| `internal/gateway/router.go` | Modified | 4 new routes under policy_editor role |
| `internal/store/policy.go` | Modified | Rollout CRUD (13 methods), types, error sentinels, new helper methods |
| `internal/bus/nats.go` | Modified | `SubjectPolicyRolloutProgress` constant |
| `cmd/argus/main.go` | Modified | rolloutSvc creation, rolloutStageProc registration, session/CoA adapter wiring, WS hub NATS subscription |

## Architecture Compliance

- **Modular monolith (ADR-001):** All code in `internal/` packages
- **Store pattern:** Follows existing PolicyStore patterns (scan helpers, error sentinels, transactions)
- **Handler pattern:** Follows existing handler patterns (chi.URLParam, apierr helpers, audit entries)
- **Job processor pattern:** Implements `Processor` interface matching existing processors
- **Import cycle prevention:** `SessionProvider` and `CoADispatcher` interfaces defined in rollout package; adapters in main.go
- **Event bus pattern:** Uses `bus.SubjectPolicyRolloutProgress` for NATS events
- **RADIUS nil-safety:** rolloutSvc created with nil session/CoA providers, wired later via SetSessionProvider/SetCoADispatcher when RADIUS is configured

## Verdict

**PASS** -- All 15 acceptance criteria satisfied, 1008 tests passing, clean build, 3 issues found and fixed (1 critical tenant scoping bug in SIM selection, 2 minor API response completeness issues). No regressions.
