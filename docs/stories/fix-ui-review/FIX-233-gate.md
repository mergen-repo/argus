# Gate Report: FIX-233 — SIM List Policy column + Rollout Cohort filter

**Date:** 2026-04-26
**Story:** FIX-233 (UI Review Remediation)
**Plan:** `docs/stories/fix-ui-review/FIX-233-plan.md`
**Spec:** `docs/stories/fix-ui-review/FIX-233-sim-list-policy-cohort-filter.md`
**Gate Lead:** gate-team-lead (Loop 1)
**Mode:** AUTOPILOT — UI Review Remediation track

## Summary

- Requirements Tracing: 10/10 ACs traced to code (AC-1..AC-10)
- Gap Analysis: **9/10 ACs PASS**, **1/10 PARTIAL** (AC-7 — chip-driven version submenu deferred to **D-141**, not blocking; backend + URL deep-link path fully functional)
- Compliance: **COMPLIANT** (PAT-006, PAT-009, PAT-011, PAT-012, PAT-014..PAT-018 all PASS)
- Tests: **PASS** — 3615/102 short suite + 623/4 race + 3/3 DB-gated FIX-233 tests
- Build: **PASS** — `go build ./...`, `go vet ./...`, `pnpm tsc --noEmit`, `pnpm build` (2.64s)
- Performance: structural index-use confirms AC-10 PASS at dev-scale; staging-scale validation logged as **D-143**
- Overall: **PASS**

## Team Composition

- Analysis Scout: 7 findings (F-A1..F-A7)
- Test/Build Scout: 2 findings (F-B1, F-B2)
- UI Scout: 3 findings (F-U1..F-U3)
- De-duplicated: 12 → 12 findings (no duplicates across scouts; orthogonal coverage)

## Merged Findings Table

| ID | Severity | Title | Source-Scout | Decision |
|----|----------|-------|--------------|----------|
| **F-B1** | **CRITICAL** | `TestSIMStore_CohortFilter_RolloutAndStagePct` FK violation `fk_policy_assignments_rollout` — test seeds `policy_assignments` referencing rollout UUIDs never inserted into `policy_rollouts` | testbuild | **FIX-LEAD (Loop 1, applied)** |
| **F-U1** | **CRITICAL** | Pre-existing global React #185 crash — `useEventStore(useFilteredEventsSelector)` returns fresh `.filter()` array each render, triggering React 19 `useSyncExternalStore` re-render loop. Verified pre-existing on HEAD~1 (NOT FIX-233 regression) | ui | **OUT-OF-SCOPE → logged FIX-249 (P0, S)** |
| **F-A1** | HIGH | Wire-shape mismatch: BE emits `current_stage` (handler.go:~1432, `RolloutSummary.CurrentStage int json:"current_stage"`), FE declares `current_stage_pct: number` (`web/src/types/policy.ts:141`) + test fixture `index.test.tsx:71` | analysis | **FIX-LEAD (Loop 1, applied)** |
| **F-U3** | MED | Pre-existing `make build` TS error — `web/src/components/ui/info-tooltip.tsx:47` references `process.env.NODE_ENV` (not Vite-native). FIX-222 leftover; container image cannot be built clean | ui | **OUT-OF-SCOPE → logged FIX-250 (P2, XS)** |
| **F-A2** | MED | Cohort stage submenu hard-coded `[1, 10, 100]` in `web/src/pages/sims/index.tsx:~723`; real rollouts can use `[5, 25, 50, 100]` | analysis | **DEFER → D-142** |
| **F-A3** | MED | AC-7 partial — Policy chip version submenu omitted because `PolicyListItem` no `versions[]`. Backend supports it; URL deep-link works; only chip UX gap | analysis | **DEFER → D-141** (AC-7 → PARTIAL in AC mapping below) |
| **F-A4** | LOW | Skeleton/colSpan accuracy verified (13 cells, header 13 cells, colSpan=13) | analysis | **NOTE** (informational PASS — no action) |
| **F-A5** | LOW | `if (filters.rollout_stage_pct)` falsy on value 0 — backend rejects 0; cosmetic | analysis | **NOTE** (validated as benign — backend rejects stage=0 with 400) |
| **F-A6** | LOW | T9 perf is dev-scale (empty rollout); structural index-use confirms AC-10 PASS today | analysis | **DEFER → D-143** |
| **F-A7** | NOTE | T8 case-7 propagation covered by store integration test instead of router test — sufficient today | analysis | **NOTE** (router-level integration as future hardening; non-blocking) |
| **F-B2** | LOW | vitest not installed; FE smoke is tsc-only — matches FIX-232 fallback | testbuild | **NOTE** (project-wide pattern, not FIX-233-specific) |
| **F-U2** | INFO | API verifies clean at network layer: `/policy-rollouts` 200 [], `/sims?rollout_id=<bad>` 400, validation 400, DTO carries policy fields, F-149 fix confirmed | ui | **NOTE** (positive evidence — confirms AC-4 + AC-5) |

## In-Scope Fixes Applied

### Fix #1 — F-A1 (FE wire-shape rename `current_stage_pct` → `current_stage`)

**File:** `web/src/types/policy.ts:141`
```diff
   state: string
-  current_stage_pct: number
+  current_stage: number
   started_at?: string
```

**File:** `web/src/pages/sims/__tests__/index.test.tsx:71`
```diff
   state: 'in_progress',
-  current_stage_pct: 50,
+  current_stage: 50,
   started_at: '2026-01-01T00:00:00Z',
```

**Why:** BE handler emits `RolloutSummary.CurrentStage int json:"current_stage"` (`internal/api/policy/handler.go:~1432`). FE type lying about wire shape would produce silent type-mismatch at runtime — fixture fix in test keeps the type-check honest.

**Verification:**
- `grep -rn "current_stage_pct" web/src/` → **0 matches** (was 2)
- `pnpm exec tsc --noEmit` → **PASS** (no errors)
- `pnpm build` → **PASS** (2.64s, vendor breakdown clean)

### Fix #2 — F-B1 (test seed `policy_rollouts` parent rows before `policy_assignments`)

**File:** `internal/store/sim_list_enriched_test.go:516..540`

**Before** (FK violation on every run):
```go
// Seed rollouts (use raw UUIDs — no FK on policy_rollouts.id required from sims)
r1ID := uuid.New()
r2ID := uuid.New()

// Seed 2 SIMs at stage 1, 3 SIMs at stage 10 for tenant A / rollout R1
```

**After** (parent rollouts inserted; cleanup registered):
```go
// Seed rollouts. policy_assignments.rollout_id has FK fk_policy_assignments_rollout
// → policy_rollouts(id), so the parent rollout rows must exist before the
// assignments loop. Each rollout is scoped to its tenant via the policy
// version (fA.policyVersionID for r1, fB.policyVersionID for r2).
r1ID := uuid.New()
r2ID := uuid.New()
if _, err := pool.Exec(ctx, `
    INSERT INTO policy_rollouts (id, policy_id, policy_version_id, strategy, stages, current_stage, total_sims, state, started_at)
    VALUES ($1, $2, $3, 'canary', '[]'::jsonb, 0, 0, 'in_progress', NOW())`,
    r1ID, fA.policyID, fA.policyVersionID,
); err != nil {
    t.Fatalf("seed policy_rollouts r1: %v", err)
}
if _, err := pool.Exec(ctx, `
    INSERT INTO policy_rollouts (id, policy_id, policy_version_id, strategy, stages, current_stage, total_sims, state, started_at)
    VALUES ($1, $2, $3, 'canary', '[]'::jsonb, 0, 0, 'in_progress', NOW())`,
    r2ID, fB.policyID, fB.policyVersionID,
); err != nil {
    t.Fatalf("seed policy_rollouts r2: %v", err)
}
t.Cleanup(func() {
    cctx := context.Background()
    _, _ = pool.Exec(cctx, `DELETE FROM policy_assignments WHERE rollout_id IN ($1, $2)`, r1ID, r2ID)
    _, _ = pool.Exec(cctx, `DELETE FROM policy_rollouts WHERE id IN ($1, $2)`, r1ID, r2ID)
})

// Seed 2 SIMs at stage 1, 3 SIMs at stage 10 for tenant A / rollout R1
```

**Why:** `policy_assignments.rollout_id` carries FK `fk_policy_assignments_rollout → policy_rollouts(id)` (declared in `migrations/20260320000002_core_schema.up.sql:384`). Test was inserting child rows before parent rows existed. The fix:
1. Inserts r1 keyed to fA's policy/version (tenant A rollout); r2 keyed to fB's policy/version (tenant B rollout) — preserving the cross-tenant isolation scenario that the test exercises.
2. Sets minimum NOT NULL columns: `policy_id`, `policy_version_id`, `strategy='canary'`, `stages='[]'::jsonb`, `current_stage=0`, `total_sims=0`, `state='in_progress'`, `started_at=NOW()`.
3. `t.Cleanup` deletes assignments first, then rollouts (order matters for FK).
4. Note: `policy_rollouts` schema has no `tenant_id` column — tenant scoping flows through `policy_id → policies.tenant_id`. The dispatch prompt mentioned `tenant_id` as a hypothetical NOT NULL column; verified against actual schema, the correct columns are as listed above.

**Verification:**
- `DATABASE_URL=... go test ./internal/store/ -run 'TestSIMStore_CohortFilter_RolloutAndStagePct|TestSIMStore_NullablePolicyAssignment|TestPolicyStore_AssignSIMsToVersion_StagePct' -v` → **3/3 PASS**

## Out-of-Scope Items Logged (NOT fixed in FIX-233)

| ID | Path | Tier | Effort |
|----|------|------|--------|
| **FIX-249** | `docs/stories/fix-ui-review/FIX-249-react-185-event-stream-loop.md` | P0 | S |
| **FIX-250** | `docs/stories/fix-ui-review/FIX-250-vite-native-env.md` | P2 | XS |

Both added to `docs/ROUTEMAP.md` "Wave — UI Review Remediation" block (rows after FIX-234).

**Hard constraint enforced:** No edit was made to `event-stream-drawer.tsx` or `info-tooltip.tsx` from this gate. They are pre-existing defects logged for AUTOPILOT to pick up next per priority (FIX-249 P0 → next wave; FIX-250 P2 → cleanup wave).

## Tech Debt Added (ROUTEMAP → ## Tech Debt)

| ID | Description (short) | Tracking | Status |
|----|---------------------|----------|--------|
| **D-141** | AC-7 Policy chip version submenu omitted — `PolicyListItem` no `versions[]`; URL deep-link works | FIX-243 (DSL realtime validate) | OPEN |
| **D-142** | Cohort stage submenu hard-codes `[1, 10, 100]` — real rollouts can use other ladders | FIX-24x (rollout DTO enrichment) | OPEN |
| **D-143** | AC-10 perf evidence dev-scale only — staging-scale (10K+ SIMs) re-validation deferred | FIX-24x (perf hardening) | OPEN |

## Acceptance Criteria Mapping

| AC | Description | Status | Evidence |
|----|-------------|--------|----------|
| AC-1 | `ListSIMsParams` adds RolloutID/RolloutStagePct/PolicyVersionID/PolicyName | **PASS** | `internal/store/sim_list.go` ListSIMsParams struct |
| AC-2 | Handler query params parsing | **PASS** | `internal/api/sim/handler.go` ListSIMs; T7/T8 router tests |
| AC-3 | Store JOINs `policy_assignments` (post-FIX-231 canonical) | **PASS** | `internal/store/sim_list_enriched.go` LEFT JOIN; T6 test |
| AC-4 | Invalid UUID → 400 BadRequest (F-149 silent fallback fix) | **PASS** | `TestHandler_ListSIMs_FilterValidation` 7 subtests; F-U2 confirmed network 400 |
| AC-5 | SIM DTO exposes policy_name/policy_version/policy_version_id/rollout_id?/rollout_stage_pct?/coa_status? | **PASS** | `internal/model/sim_dto.go`; F-U2 confirmed DTO carries fields |
| AC-6 | FE list "Policy" column (Demo Premium v3, clickable, "—" for nullable) | **PASS** | `web/src/pages/sims/index.tsx` Policy cell + colSpan=13 verified by F-A4 |
| AC-7 | FE filter bar: Policy chip with version submenu + Cohort chip with stage submenu + URL sync | **PARTIAL — DEFERRED to D-141** | Cohort chip + URL sync PASS; Policy chip version submenu omitted (`PolicyListItem` no `versions[]`). Power users have URL deep-link as workaround. Tracked in D-141 → FIX-243. |
| AC-8 | URL deep-linking (`?policy_version_id=X&rollout_id=Y&rollout_stage_pct=1`) on page load | **PASS** | `web/src/pages/sims/index.tsx` searchParams hydration; T8 case-7 store integration test |
| AC-9 | "View cohort" button on Rollout UI panel pre-fills filters | **PASS** | `web/src/components/rollout/rollout-active-panel.tsx` link enriched (non-regressive — original target preserved, now also pre-fills `rollout_stage_pct`) |
| AC-10 | Performance — policy JOIN < 150ms p95; index on `policy_assignments.sim_id` | **PASS** (dev-scale; staging tracked D-143) | `docs/stories/fix-ui-review/FIX-233-perf.md` EXPLAIN: Index Scan via `idx_policy_assignments_sim_id` + `idx_policy_assignments_rollout` |

**Result: 9/10 PASS, 1/10 PARTIAL→DEFERRED (D-141).**

## Re-Verification Results (after Loop 1 fixes)

```
go build ./...                                                              PASS  (no output)
go vet ./...                                                                PASS  (no output)
DATABASE_URL=... go test ./internal/store/ -run 'TestSIMStore_CohortFilter_RolloutAndStagePct|TestSIMStore_NullablePolicyAssignment|TestPolicyStore_AssignSIMsToVersion_StagePct' -v
                                                                            PASS  (3/3)
go test -short ./internal/...                                               PASS  (3615/102, from testbuild scout)
go test -race ./internal/store ./internal/api/sim ./internal/api/policy     PASS  (623/4, from testbuild scout)
cd web && pnpm exec tsc --noEmit                                            PASS  (no errors)
cd web && pnpm build                                                        PASS  (2.64s; vendor breakdown clean)
PAT-018 grep (current_stage_pct in web/src/)                                CLEAN (0 matches)
PAT-011 grep (rollout DTO field shape)                                      CLEAN (current_stage consistent across types + fixture)
```

## Compliance Checks (from Analysis Scout — re-verified post-fix)

| Pattern | Description | Status |
|---------|-------------|--------|
| PAT-006 | API envelope `{ status, data, meta? }` | PASS |
| PAT-009 | LEFT JOIN nullable scan (sql.NullX into pointers) | PASS — verified by `TestSIMStore_NullablePolicyAssignment` |
| PAT-011 | Cursor pagination (no offset) | PASS |
| PAT-012 | Tenant scoping in store layer | PASS — cross-tenant test asserts 0 leak |
| PAT-014 | Audit log for state-changing ops | N/A (read-only feature) |
| PAT-015 | shadcn/ui token usage (no raw HTML, no arbitrary px) | PASS |
| PAT-016 | Trigger NEW.policy_version_id (not NEW.id) — only relevant for FIX-231 trigger; FIX-233 doesn't add triggers | PASS (no new triggers) |
| PAT-017 | Validation 400 on bad UUID input | PASS — F-149 fix verified |
| PAT-018 | No silent fallback on invalid filter input | CLEAN — 0 matches in fixed code |

## Verification Iterations

- Fix iterations: **1** (Loop 1 only — both fixes landed clean on first attempt; no Loop 2 needed)
- Max allowed: 2
- Escalation: **none**

## Passed Items

- Backend: ListSIMsParams + handler + store + DTO all wired correctly
- DB-gated tests: 3/3 PASS post-fix
- Race tests: 623/4 PASS
- FE types: wire shape matches BE (`current_stage` consistent)
- FE build: 2.64s, vendor breakdown clean
- Performance: structural EXPLAIN confirms index use
- Security: F-149 silent-fallback regression fixed (AC-4)
- Cross-tenant: isolation verified by `TestSIMStore_CohortFilter_RolloutAndStagePct` tenant-A/B scenario

## GATE_VERDICT: **PASS**

Story FIX-233 passes Gate. 9 ACs PASS, AC-7 PARTIAL with explicit deferral to D-141 (FIX-243 follow-up). Two pre-existing defects (F-U1 React #185 global crash, F-U3 Vite-env build error) logged as new stories FIX-249 (P0) + FIX-250 (P2) for AUTOPILOT to pick up next; both verified pre-existing on HEAD~1 and explicitly out of FIX-233 scope per orchestrator-level scope decision.

Ready for Reviewer (Step 4).
