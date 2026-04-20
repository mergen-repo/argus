# FIX-232: Rollout UI Active State — Progress Bar, Advance/Rollback/Abort Buttons, WS Push, Endpoint Path Fix

## Problem Statement
When a policy rollout is active (`state=in_progress`), the UI Rollout tab still shows the initial "Direct Assign / Staged Rollout" selection cards as if no rollout exists. No progress bar, no advance button, no rollback, no abort. Backend has `/policy-rollouts/{id}/advance` endpoint but frontend calls wrong path `/rollouts/{id}/advance` → 404. No WS push for progress — UI needs manual refresh.

Verified:
- `v3 Direct Assign` rollout state=in_progress for days, UI still shows "Create rollout" cards
- `POST /api/v1/rollouts/{id}/advance` → 404
- `POST /api/v1/policy-rollouts/{id}/advance` → 200 (correct path)

## User Story
As a policy admin, when a rollout is active I want to see its progress, advance/rollback it, and see live updates without refreshing — so I can safely monitor and control rollout execution.

## Architecture Reference
- Backend: `internal/policy/rollout/service.go` (state), router `/policy-rollouts/*` endpoints
- Event: `argus.events.policy.rollout.progressed` NATS subject (FIX-212 envelope)
- FE: `web/src/pages/policies/editor.tsx` Rollout tab + new components

## Findings Addressed
F-145 (endpoint path mismatch), F-146 (UI doesn't reflect state; advance/rollback buttons missing)

## Acceptance Criteria
- [ ] **AC-1:** Rollout tab detects active rollout state via `useRollout(versionID)` hook. If `rollout.state IN ('pending','in_progress')`: render active-rollout panel. If `completed|failed|rolled_back|null`: render selection cards.
- [ ] **AC-2:** Active-rollout panel shows:
  - Rollout ID + started_at + duration so far
  - Strategy: Direct or Staged (stages visualization: 1% → 10% → 100% with current stage highlighted)
  - Progress bar: `migrated_sims / total_sims (N%)`
  - Per-stage status: ✓ completed, ▶ in_progress, ○ pending
  - CoA ack counter: "3/5 CoA acked · 1 pending · 1 failed"
  - ETA (if stage completing: linear extrapolation)
- [ ] **AC-3:** Actions toolbar:
  - **Advance to Next Stage** (Staged only, if current stage completed): `POST /api/v1/policy-rollouts/{id}/advance` — confirm dialog
  - **Rollback**: `POST /api/v1/policy-rollouts/{id}/rollback` — confirm dialog with warning
  - **Abort**: `POST /api/v1/policy-rollouts/{id}/abort` (NEW endpoint — stops without reverting)
  - **View Migrated SIMs**: link to `/sims?rollout_id=X` (cohort filter FIX-233)
- [ ] **AC-4:** FE HTTP client path corrected — `/policy-rollouts/{id}/{action}`, NOT `/rollouts/...`. Update `web/src/hooks/use-rollout.ts` and call sites.
- [ ] **AC-5:** WS subscription `policy.rollout.progressed` (consumes FIX-212 envelope) — updates panel live without refetch. Backend publisher in `ExecuteStage` already exists; FE subscriber consumes.
- [ ] **AC-6:** New backend endpoint `POST /api/v1/policy-rollouts/{id}/abort`:
  - Sets `state='aborted'` + `aborted_at=NOW()`
  - Does NOT revert assignments (already-migrated SIMs stay on new policy)
  - Audit event `policy_rollout.aborted` with actor + reason
  - Use case: admin realizes policy is fine but rollout was mistakenly staged — abort without rollback.
- [ ] **AC-7:** "Rollback" behavior documented: reverts all policy_assignments for rollout to previous_version_id, fires CoA with old policy, sets `state='rolled_back'`. Destructive — confirm dialog explicit.
- [ ] **AC-8:** Progress polling fallback — if WS disconnects, polls every 5s.
- [ ] **AC-9:** Error surfacing — failed stages show error reason + retry button (if recoverable).
- [ ] **AC-10:** Abort endpoint regression-tests: (a) abort in_progress rollout → state=aborted, (b) abort completed rollout → 400 "cannot abort completed", (c) abort rolled_back → 400.
- [ ] **AC-11:** Rollout panel in expanded view (side drawer) links out to: CDR Explorer filtered to rollout SIMs (FIX-214), Sessions page filtered to rollout cohort (FIX-233), Audit log entries for this rollout.

## Files to Touch
- **Backend:**
  - `internal/api/policy/handler.go::AbortRollout` (NEW) + route in `internal/gateway/router.go`
  - `internal/policy/rollout/service.go::AbortRollout` (NEW)
- **Frontend:**
  - `web/src/pages/policies/editor.tsx` — conditional active-rollout panel
  - `web/src/components/policy/rollout-active-panel.tsx` (NEW)
  - `web/src/components/policy/rollout-selection-cards.tsx` (existing — conditional render)
  - `web/src/hooks/use-rollout.ts` — endpoint path fix + WS subscribe
  - `web/src/lib/ws-client.ts` (if exists) — subject `policy.rollout.progressed` wired
- **Docs:** `docs/architecture/api/_index.md` — document abort endpoint

## Risks & Regression
- **Risk 1 — Multi-tab concurrent action:** Two admins click Advance simultaneously. Mitigation: backend idempotent — if already at next stage, return current state (not error).
- **Risk 2 — WS payload drift:** Relies on FIX-212 envelope. Must deploy together or with backward shim.
- **Risk 3 — Rollback storm:** User panics and rolls back mid-rollout; all migrated SIMs ping-pong. Mitigation: Rollback confirm dialog shows count of affected SIMs; CoA throttled.
- **Risk 4 — Endpoint path fix breaks existing FE callers:** Must grep all `rollouts/` references and update.

## Test Plan
- Unit: `AbortRollout` state transitions in 3 scenarios
- Integration: start rollout → advance 2 stages → rollback → verify assignments reverted
- Integration: start → abort → verify state=aborted, migrated SIMs stay on new policy
- Browser: live rollout → progress bar updates without refresh; advance button disabled mid-stage, enabled when complete
- Browser: WS disconnect (block port 8081) → polling fallback activates within 10s

## Plan Reference
Priority: P0 · Effort: L · Wave: 3 · Depends: FIX-212 (event envelope), FIX-230 (DSL match — rollout must produce correct data first)
