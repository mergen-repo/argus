# FIX-202: SIM List & Dashboard DTO — Operator Name Resolution Everywhere

## Problem Statement

SIM DTO returns `operator_id` (UUID) but missing `operator_name, operator_code`, causing UI to display raw UUID prefixes ("20000000") instead of human-readable "Turkcell". Same pattern across Dashboard, Sessions, Violations, eSIM, Notifications. Root cause: handlers don't enrich DTOs with joined names from `operators`/`apns` tables.

**Verified:**
```
GET /api/v1/sims?limit=1 → {operator_id: "...", apn_name: "XYZ Private", operator_name: MISSING}
```

## User Story
As an operator, I want every SIM reference, session, violation, and event in Argus to show the operator/APN/policy names (not UUIDs), so I can identify entities without lookup.

## Architecture Reference
- Backend: `internal/api/sim/handler.go` (DTO), `internal/api/dashboard/handler.go`, `internal/api/session/handler.go`
- Store: name resolution via JOIN in store layer (not per-request handler enrichment — F-300 performance)

## Findings Addressed
F-14 (global name resolution), F-21 (session events UUID), F-82 (SIM DTO), F-97 (dashboard), F-84 (violations), F-102 (notifications UUID refs)

## Acceptance Criteria
- [ ] **AC-1:** SIM DTO adds: `operator_name, operator_code, policy_name, policy_version_number, policy_version_id`. Nullable for orphan/unassigned.
- [ ] **AC-2:** Store `sim.List` uses JOIN (sims + operators + apns + policy_versions) — single query, no N+1 (F-300 prevention).
- [ ] **AC-3:** Dashboard `operatorHealthDTO` adds `code` (turkcell/vodafone_tr), `latency_ms`, `active_sessions`, `auth_rate`, `last_health_check`, `sla_target` (F-03 scope).
- [ ] **AC-4:** Session DTO adds `policy_name, policy_version, operator_name` (already has some).
- [ ] **AC-5:** Violation DTO adds `iccid, policy_name, policy_version, operator_name, apn_name`.
- [ ] **AC-6:** eSIM DTO adds `operator_name`, `operator_code`.
- [ ] **AC-7:** Notification body entity references carry `{entity_type, entity_id, display_name}` for FE link rendering.
- [ ] **AC-8:** Orphan entity handling: if operator_id references non-existent row (F-83), DTO returns `operator_name: null` + FE shows "(Unknown)" with warning icon. Does NOT crash.
- [ ] **AC-9:** Backend enrichment centralized in store query — single JOIN; handler just serializes. No `handler.enrichSomeDTO()` per-row lookups (F-300).
- [ ] **AC-10:** Performance — SIM list endpoint p95 < 100ms for 50-item page (down from likely 500ms+ with enrichment N+1).

## Files to Touch
- `internal/store/sim.go` — add LEFT JOIN operators/apns/policy_versions to List query
- `internal/api/sim/handler.go` — remove enrichSessionDTO-style per-row calls
- `internal/api/dashboard/handler.go` — widen operatorHealthDTO
- `internal/api/session/handler.go` — same
- `internal/api/violation/handler.go` — same
- `internal/api/esim/handler.go` — same
- `web/src/types/*.ts` — add new fields
- `web/src/pages/*/` — UI render updated columns (operator chip with name)

## Risks & Regression
- **Risk 1 — JOIN performance:** Verify indices `sims.operator_id`, `sims.apn_id`, `sims.policy_version_id` exist. Add if missing.
- **Risk 2 — Orphan rows crash:** AC-8 — LEFT JOIN tolerates missing parent; FE null-safe rendering.
- **Risk 3 — Test fixtures break:** Existing mocks may return DTO without new fields; test updates required.

## Test Plan
- Unit: store query returns enriched fields per JOIN
- Integration: 500 SIM fixture, verify all 6 enriched fields populated
- Browser: SIMs list shows "Turkcell" (not UUID), Dashboard cards show health metrics, no F-14 UUIDs anywhere
- Load: 10K SIM list fetch < 500ms p95

## Plan Reference
- Plan: `docs/reviews/ui-review-remediation-plan.md` → P0 Backend Contract
- Priority: P0 · Effort: M · Wave: 1
