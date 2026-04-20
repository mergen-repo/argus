# FIX-233: SIM List Policy Column + Rollout Cohort Filter

## Problem Statement
SIM List page shows: ICCID, IMSI, MSISDN, IP, State, Operator, APN, IP Pool, RAT, Created. **No Policy column.** No filter by policy version or rollout cohort. Observed user need: "I rolled out v4 to 1% canary — I want to see those 2 SIMs in isolation to inspect their session behavior." Impossible today.

Additionally, backend `GET /sims?policy_version_id=X` silently ignores the filter (F-149 verified) — tests with 3 different UUIDs return same 50 rows.

## User Story
As a policy admin, I want to see each SIM's active policy+version in the list, and filter the list by rollout cohort (all SIMs migrated in stage 1% of rollout X), so I can isolate and investigate the canary cohort.

## Architecture Reference
- Backend: `internal/api/sim/handler.go::ListSIMsParams` — add `PolicyVersionID`, `RolloutID`, `RolloutStagePct`
- Store: query extensions
- FE: list column + filter chips

## Findings Addressed
F-147 (policy column missing), F-148 (dual-source — post-FIX-231 resolved), F-149 (backend filter ignored — code-verified)

## Acceptance Criteria
- [ ] **AC-1:** Backend `ListSIMsParams` struct adds:
  ```go
  PolicyVersionID *uuid.UUID  // filter by specific version
  PolicyID        *uuid.UUID  // filter by policy (any version)
  RolloutID       *uuid.UUID  // filter to cohort of rollout
  RolloutStagePct *int        // combined with RolloutID — only SIMs in specific stage
  ```
- [ ] **AC-2:** Handler query params:
  - `?policy_version_id=<uuid>`
  - `?policy_id=<uuid>`
  - `?rollout_id=<uuid>&rollout_stage_pct=1` (combined for cohort)
- [ ] **AC-3:** Store `sim.List` query JOINs `policy_assignments` (post-FIX-231 canonical) + filter application.
- [ ] **AC-4:** Invalid UUID → 400 BadRequest (fix F-149 silent fallback).
- [ ] **AC-5:** SIM DTO (from FIX-202) exposes `policy_name, policy_version, policy_version_id, rollout_id?, rollout_stage_pct?, coa_status?`.
- [ ] **AC-6:** FE list adds "Policy" column — renders "Demo Premium v3" (clickable → policy detail). Nullable SIMs: "—".
- [ ] **AC-7:** FE filter bar:
  - **Policy** dropdown — multi-select from active policies (sub-menu: "All versions" / "v1" / "v2" / "v3").
  - **Rollout Cohort** (advanced) — dropdown of active rollouts; when selected, reveals stage filter (1% / 10% / 100% / All migrated / Pending).
- [ ] **AC-8:** URL deep-linking: `?policy_version_id=X&rollout_id=Y&rollout_stage_pct=1` → filters apply on page load.
- [ ] **AC-9:** "View cohort" button on Rollout UI panel (FIX-232 AC-3) navigates here with pre-filled filters.
- [ ] **AC-10:** Performance — adding policy JOIN shouldn't degrade list query p95 beyond 150ms (verify with index on `policy_assignments.sim_id`).

## Files to Touch
- `internal/api/sim/handler.go` — ListSIMsParams, query param parse, validation
- `internal/store/sim.go` — JOIN + filter application
- `internal/api/sim/handler_test.go` — filter test cases
- `web/src/pages/sims/index.tsx` — column + filter chips
- `web/src/hooks/use-sims.ts` — filter query params

## Risks & Regression
- **Risk 1 — Policy column breaks table width on narrow screens:** Responsive column show/hide (policy hidden on <1280px with tooltip).
- **Risk 2 — Query performance with JOIN + complex filter:** Indices on `policy_assignments.sim_id`, `policy_version_id`, `rollout_id`. Explain plan < 100ms.
- **Risk 3 — Cohort filter over-queries backend:** Debounce filter changes 300ms; paginate server-side.
- **Risk 4 — Stale data — rollout progresses while UI filter active:** Re-fetch on WS event `policy.rollout.progressed`.

## Test Plan
- Unit: filter parser rejects invalid UUIDs
- Integration: seed rollout, request `?rollout_id=X&rollout_stage_pct=1` → correct 2 SIMs
- Browser: SIM list shows Policy column with version; cohort filter isolates canary SIMs

## Plan Reference
Priority: P1 · Effort: M · Wave: 4 · Depends: FIX-230 (DSL match — cohort correctness), FIX-231 (canonical policy source), FIX-202 (DTO enrichment)
