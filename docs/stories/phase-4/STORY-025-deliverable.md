# Deliverable: STORY-025 — Policy Staged Rollout (Canary)

## Summary

Implemented staged policy rollout with canary deployment pattern: 1% → 10% → 100% with full rollback capability. Each stage selects SIMs, updates policy assignments (TBL-15), sends CoA for active sessions, and publishes progress via NATS/WebSocket. Rollback reverts all migrated SIMs to previous version with mass CoA.

## What Was Built

### Rollout Service (internal/policy/rollout/service.go)
- `StartRollout()`: Create rollout record (TBL-16), execute first stage (1%)
- `ExecuteStage()`: Select SIMs, update TBL-15, send CoA, publish progress
- `AdvanceRollout()`: Move to next stage (10% → 100%)
- `RollbackRollout()`: Revert all migrated SIMs, mass CoA, mark rolled_back
- `GetProgress()`: Current stage, migrated/total counts, errors
- `sendCoAForSIM()`: CoA dispatch per active session
- `publishProgress()`: NATS "policy.rollout_progress" event
- `resolveTenantID()`: Tenant resolution from rollout context
- Session provider and CoA dispatcher interfaces with adapters

### Rollout Store Extensions (internal/store/policy.go)
- CreateRollout, GetRolloutByID, SelectSIMsForStage (FOR UPDATE SKIP LOCKED)
- AssignSIMsToVersion, RevertRolloutAssignments, CompleteRollout, RollbackRollout
- GetTenantIDForRollout, GetPolicyIDForRollout
- 15 rollout store methods total

### API Handlers (internal/api/policy/handler.go)
- `StartRollout()` (API-096): POST /api/v1/policy-versions/:id/rollout
- `AdvanceRollout()` (API-097): POST /api/v1/policy-rollouts/:id/advance
- `RollbackRollout()` (API-098): POST /api/v1/policy-rollouts/:id/rollback
- `GetRollout()` (API-099): GET /api/v1/policy-rollouts/:id
- Standard envelope responses, audit logging, stage validation

### Async Job Processor (internal/job/rollout.go)
- `RolloutStageProcessor` for large stages (>100K SIMs)
- Implements `job.Processor` interface

### Routing & Wiring
- 4 new routes under RequireRole("policy_editor")
- Session/CoA adapter types for interface bridging
- WS hub subscription to rollout progress events
- Job processor registration

### Tests
- 10 service tests + 3 job processor tests + 12 handler tests = 25 new tests
- Full suite: 1008 tests passing, zero regressions

## Gate Fixes Applied
1. **CRITICAL**: ExecuteStage passed uuid.Nil as tenantID — no SIMs would be selected in production. Fixed with resolveTenantID() + GetTenantIDForRollout()
2. **Minor**: rolloutResponse.PolicyID never populated — added GetPolicyIDForRollout()
3. **Minor**: rolloutResponse missing errors field from API-099 spec — added Errors []string

## Architecture References Fulfilled
- API-096: POST /api/v1/policy-versions/:id/rollout (start staged rollout)
- API-097: POST /api/v1/policy-rollouts/:id/advance (advance stage)
- API-098: POST /api/v1/policy-rollouts/:id/rollback (rollback)
- API-099: GET /api/v1/policy-rollouts/:id (get progress)
- TBL-15: policy_assignments (SIM ↔ policy version mapping)
- TBL-16: policy_rollouts (rollout tracking with stages)
- FLW-03: Policy Staged Rollout flow
- SVC-05 + SVC-04 + SVC-02: Policy Engine + CoA + WebSocket integration

## Files Changed
```
internal/policy/rollout/service.go      (modified)
internal/policy/rollout/service_test.go (new)
internal/job/rollout.go                 (new)
internal/job/rollout_test.go            (new)
internal/store/policy.go                (modified)
internal/api/policy/handler.go          (modified)
internal/api/policy/handler_test.go     (modified)
internal/gateway/router.go              (modified)
cmd/argus/main.go                       (modified)
```

## Dependencies Unblocked
- STORY-046 (Frontend Policy Editor) — rollout controls UI can now use these APIs
