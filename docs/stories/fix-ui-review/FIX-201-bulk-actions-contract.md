# FIX-201: Bulk Actions Contract Fix — Accept `sim_ids` Array

## Problem Statement

Frontend bulk action flows (SIM state-change, policy-assign, operator-switch) send `{sim_ids: [uuid1, uuid2, ...]}` arrays in request body, but backend bulk handlers reject with `422 VALIDATION_ERROR: "segment_id is required"` — they only accept pre-defined segments, not ad-hoc SIM ID lists. This is a complete FE/BE contract mismatch that **breaks every bulk action** in SIM management UI.

**Empirical evidence:**
```
curl -X POST /api/v1/sims/bulk/state-change {"sim_ids":["<uuid>"]} 
→ 422 {"code":"VALIDATION_ERROR","message":"segment_id is required"}
```

FE code (`web/src/pages/sims/index.tsx`) clearly sends `sim_ids` array from row selection. BE handler (`internal/api/sim/bulk_handler.go`) validates only `segment_id`.

## User Story

As a platform operator, I want to select arbitrary SIMs from the list (via checkboxes or "all matching filter") and apply bulk state changes, policy assignments, or operator switches directly, so that I can perform ad-hoc operations without pre-creating a segment.

## Architecture Reference

- **Backend:** `internal/api/sim/bulk_handler.go` (lines 284-340 bulk policy assign, similar for state-change + operator-switch)
- **Frontend:** `web/src/pages/sims/index.tsx` (bulk action bar + mutation hooks `useBulkStateChange`, `useBulkPolicyAssign`)
- **Endpoints affected:**
  - `POST /api/v1/sims/bulk/state-change`
  - `POST /api/v1/sims/bulk/policy-assign`
  - `POST /api/v1/sims/bulk/operator-switch`
- **Scale consideration:** 10M+ SIMs — also need "all matching filter" selection pattern (not just explicit ID list). See FIX-236.

## Screen Reference
- SCR-080 (SIM Cards List) + bulk action bar
- Also affects: SIM Detail bulk context (single-SIM operations may reuse same endpoint)

## Findings Addressed

- **F-103** — Bulk state-change endpoint rejects sim_ids
- **F-104** — Bulk policy-assign endpoint rejects sim_ids
- **F-105** — Bulk operator-switch endpoint rejects sim_ids
- **F-106** — Bulk action bar sticky positioning issue (UI — included in this story)
- **F-107** — FE ↔ BE contract documentation mismatch
- **F-108** — Hot-path enforcement: after assignment, CoA must fire

## Acceptance Criteria

- [ ] **AC-1:** `POST /sims/bulk/state-change` accepts request body: `{sim_ids: [uuid...], new_state: string, reason?: string}`. Backward compat: also accepts legacy `{segment_id: uuid, new_state: string, reason?: string}` for existing integrations. Response: `{job_id: uuid, total_sims: int}` with 202 Accepted status.

- [ ] **AC-2:** `POST /sims/bulk/policy-assign` accepts `{sim_ids: [uuid...], policy_version_id: uuid, reason?: string}` + legacy `segment_id` shape.

- [ ] **AC-3:** `POST /sims/bulk/operator-switch` accepts `{sim_ids: [uuid...], target_operator_id: uuid, reason?: string}` + legacy shape.

- [ ] **AC-4:** Validation — exactly one of `sim_ids` OR `segment_id` must be provided. Both absent: 400 with message "one of sim_ids or segment_id is required". Both present: 400 with message "sim_ids and segment_id are mutually exclusive".

- [ ] **AC-5:** `sim_ids` array validation: minimum 1 entry, maximum 10000 per request (larger batches use segment_id or filter-based selection in FIX-236). Each UUID format-validated. Validation error lists offending indices.

- [ ] **AC-6:** Tenant isolation — handler verifies ALL `sim_ids` belong to caller's tenant. Any cross-tenant SIM ID: 403 Forbidden with listed violations. No silent skip.

- [ ] **AC-7:** Bulk job persistence: 1 job row in `jobs` table per request with `total_items = len(sim_ids)`, `type` matching action (`bulk_state_change`/`bulk_policy_assign`/`bulk_operator_switch`). Processor iterates sim_ids, updates `processed_items` after each SIM.

- [ ] **AC-8:** Audit trail — 1 audit entry per SIM action (not per batch). Enables per-SIM accountability. Entry includes: action, sim_id, before_value, after_value, actor, reason, bulk_job_id (for grouping).

- [ ] **AC-9:** Hot-path enforcement (F-108) — after bulk policy-assign: queue CoA dispatch for each SIM's active sessions. Job processor calls existing CoA dispatcher; result tracked via `policy_assignments.coa_status`. Fire-and-forget acceptable for first pass (improves in FIX-234).

- [ ] **AC-10:** FE bulk action bar (`web/src/pages/sims/index.tsx`): bar sticks at bottom when row(s) selected, becomes non-sticky when selection cleared. Z-index above table rows, shadow separator. Responsive — mobile scrolls separately. F-106 addressed.

- [ ] **AC-11:** FE optimistic update — when bulk mutation fires, selected rows show "processing" spinner until job complete. On completion, row data refetches. On error, toast with failure count + drill-down to job errors.

- [ ] **AC-12:** Performance — 10K sim_ids request processed in < 30s (target p95). Processor batches inserts (100 per transaction) to reduce lock contention.

- [ ] **AC-13:** Bulk job detail shows `failed_items` count and error_report JSONB populated with per-SIM failures ({sim_id, error_code, error_message}). UI can surface failed items (future FIX-195 scope).

- [ ] **AC-14:** `docs/architecture/api/bulk-actions.md` updated — new request schema with examples. `docs/architecture/MIDDLEWARE.md` notes: bulk endpoints behind rate limit (1 req/sec per tenant).

## Pre-conditions / Dependencies

- **Blocks:** Any story that relies on working bulk actions (FIX-236 scale readiness depends on this as foundation).
- **Depends on:** None for the core contract fix. Optional: FIX-206 (orphan cleanup) improves tenant isolation correctness.

## Files to Touch

**Backend (Go):**
- `internal/api/sim/bulk_handler.go` — add `sim_ids` branch to all 3 handlers
- `internal/api/sim/bulk_handler_test.go` — test both shapes (sim_ids + segment_id), mutual exclusion, tenant isolation, limits
- `internal/job/bulk_state_change.go` — processor handles sim_ids payload
- `internal/job/bulk_policy_assign.go` — processor + CoA dispatch
- `internal/job/bulk_operator_switch.go` — processor
- `internal/store/sim.go` — bulk update helpers (batched inserts)
- `internal/audit/service.go` — per-SIM audit writes with bulk_job_id grouping

**Frontend (TS):**
- `web/src/pages/sims/index.tsx` — verify sticky bulk bar (F-106), optimistic updates
- `web/src/hooks/use-sims.ts` — verify request shape matches backend
- `web/src/types/sim.ts` — update BulkActionRequest type
- `web/src/components/sims/bulk-action-bar.tsx` — sticky positioning CSS

**Documentation:**
- `docs/architecture/api/_index.md` — update 3 endpoint entries
- `docs/architecture/api/bulk-actions.md` (NEW or update) — full contract

## Risks & Regression Prevention

### Risk 1: Breaking existing segment-based integrations
- **Scenario:** External scripts or cron jobs use `segment_id`. Removing legacy path breaks them.
- **Mitigation:** AC-1..AC-3 keep legacy shape working. Dual-shape acceptance validated by tests.

### Risk 2: Tenant isolation bypass via spoofed sim_ids
- **Scenario:** Malicious API caller lists another tenant's sim_ids.
- **Mitigation:** AC-6 explicit per-SIM tenant check BEFORE any mutation. Fail fast with listed violations.

### Risk 3: Job lock contention under 10K SIM batch
- **Scenario:** Single transaction locks all 10K rows → blocks concurrent reads.
- **Mitigation:** AC-12 batched processing (100 SIMs per transaction). Processor resumes from last processed_items on restart (idempotent).

### Risk 4: Audit log flood (10K entries per batch)
- **Scenario:** Single bulk action generates 10K audit rows → audit_logs table grows rapidly.
- **Mitigation:** Accepted trade-off — per-SIM audit REQUIRED for compliance (SOX, ISO27001). Hash-chain audit (F-203 positive) already handles volume. Retention policy (90 days for action audit) covers growth.

### Risk 5: CoA storm for bulk policy-assign (10K SIMs)
- **Scenario:** 10K CoAs fired simultaneously to operators' RADIUS servers — potential throttling/rejection from operator side.
- **Mitigation:** AC-9 rate-limited CoA dispatcher (future FIX-234 refines). First pass: best-effort, log failures.

### Risk 6: UI selection lost on filter change
- **Scenario:** User selects 50 rows, changes filter → list reloads → selection cleared → user confused.
- **Mitigation:** AC-10 UI state: selection map keyed by SIM ID. Filter change re-evaluates visible selections, shows "50 selected (3 visible, 47 hidden by filter)" indicator.

### Risk 7: Latent FE handlers still using old FE shape
- **Scenario:** Multiple FE call sites build payload; some updated, others not.
- **Mitigation:** Grep `web/src/` for bulk mutation call sites; update all in single commit.

## Test Plan

### Unit Tests
- `bulk_handler_test.go`: 
  - TestBulkStateChange_SimIdsArray_Accepted
  - TestBulkStateChange_SegmentId_Accepted (backward compat)
  - TestBulkStateChange_BothProvided_400
  - TestBulkStateChange_NeitherProvided_400
  - TestBulkStateChange_EmptyArray_400
  - TestBulkStateChange_OverLimit_400
  - TestBulkStateChange_CrossTenantSimId_403
- Same 7 tests × 3 endpoints = 21 cases

### Integration Tests
- Submit 100-SIM bulk state-change → job created → processor runs → all 100 updated → 100 audit entries
- Submit bulk policy-assign → policy_assignments rows created → CoA dispatcher queued (mock)

### Browser Regression (dev-browser)
- `/sims` page: select 3 rows → bulk action bar appears → select "Suspend" → confirm → job toast → rows update to "suspended" state
- Filter change while selected: selection preserved per AC-10 (selection map)

### Load Test
- 10K sim_ids batch: job completes < 30s (p95), processed_items monotonically advances

### Regression Test Suite
- `make test` (all Go tests)
- `cd web && npm run typecheck`

## Rollout

- **Size:** L (3-4 days)
- **Deploy risk:** MEDIUM (critical path for SIM management UI)
- **Feature flag:** Not needed (backward compat via dual-shape)
- **Rollback plan:** Revert commits; FE falls back to legacy segment_id path (breaks arbitrary selection but preserves segment bulk).

## Plan Reference
- **Plan:** `docs/reviews/ui-review-remediation-plan.md` → "P0 — Backend Contract + Data Integrity"
- **Priority:** P0
- **Effort:** L
- **Wave:** Wave 1 (Critical — bulk blockers)
