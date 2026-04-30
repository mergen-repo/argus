# FIX-231: Policy Version State Machine — Atomic Transitions + Dual-Source Fix

## Problem Statement
Two deeply coupled bugs in policy versioning:

**Bug 1 — Version state machine kopuk:** `policy_versions.state` transitions (`draft → rolling_out → active → superseded`) not atomically enforced. Observed: v2 "active" but 0 SIMs; v3 "rolling_out" but completed; concurrent rollouts allowed.

**Bug 2 — Dual source of truth:** `sims.policy_version_id` + `policy_assignments` both track "active policy per SIM" but desync:
```
iccid ...000107: sims.policy_version_id=v1, policy_assignments.policy_version_id=v5 (DIFFERENT!)
```
F-125 root cause (list 0 vs detail 10 SIM counts) — each tab queries different table.

## User Story
As a policy administrator, I want policy version state transitions to be atomic with rollout completion, and a single canonical source for "active policy per SIM", so the system behavior is predictable and data is trustworthy.

## Architecture Reference
- Backend: `internal/policy/rollout/service.go::CompleteRollout`, `internal/store/policy.go` schema
- DB: `sims.policy_version_id` + `policy_assignments` reconciliation

## Findings Addressed
F-144 (previous_version_id broken), F-146a (dual source), F-146b (multi-policy decision)

## Acceptance Criteria
- [ ] **AC-1:** Schema decision: `policy_assignments` becomes **canonical** single source. `sims.policy_version_id` kept as **read-optimized denormalized pointer** with trigger guarantee.
- [ ] **AC-2:** Trigger `sims_policy_version_sync`: After INSERT/UPDATE/DELETE on `policy_assignments`, update `sims.policy_version_id` to match. Single-writer contract.
- [ ] **AC-3:** State machine CHECK constraint on `policy_versions.state`: valid transitions enforced via Go service layer (DB CHECK for enum values).
- [ ] **AC-4:** `CompleteRollout` atomic transaction:
  1. `UPDATE policy_rollouts SET state='completed', completed_at=NOW() WHERE id=X`
  2. `UPDATE policy_versions SET state='active', activated_at=NOW() WHERE id=X` (target version)
  3. `UPDATE policy_versions SET state='superseded' WHERE policy_id=X AND state='active' AND id!=target` (previous active)
  4. All-or-nothing via transaction.
- [ ] **AC-5:** Unique constraint: at most ONE `policy_rollouts` with `state IN ('pending', 'in_progress')` per `policy_id` (prevent concurrent rollouts). `CREATE UNIQUE INDEX policy_active_rollout ON policy_rollouts(policy_id) WHERE state IN ('pending','in_progress')`.
- [ ] **AC-6:** Unique constraint: at most ONE `policy_versions` with `state='active'` per `policy_id`. Enforces single active version.
- [ ] **AC-7:** Reconciliation migration: scan existing `sims.policy_version_id` vs `policy_assignments` → pick canonical per SIM (priority: policy_assignments if exists; else sims row). Fix mismatches, log migration actions.
- [ ] **AC-8:** Stuck rollout detector (cron job): if `policy_rollouts.state='in_progress'` AND `migrated_sims == total_sims` AND no recent update → force-complete via CompleteRollout. Recovers pre-migration zombie rows.
- [ ] **AC-9:** RADIUS hot path reads `sims.policy_version_id` (fast) — guaranteed consistent via trigger. No JOIN needed in auth path.
- [ ] **AC-10:** Version state chart in Policy Detail UI — visual state machine showing timeline of transitions.
- [ ] **AC-11:** Multi-policy decision (F-146b) — **kept 1 SIM = 1 policy** (single canonical). Future multi-layer policies (base + override) out of scope — documented in `docs/PRODUCT.md`.

## Files to Touch
- `migrations/YYYYMMDDHHMMSS_policy_state_machine.up.sql` — triggers, constraints, indices
- `migrations/YYYYMMDDHHMMSS_reconcile_policy_assignments.up.sql` — data migration (AC-7)
- `internal/policy/rollout/service.go::CompleteRollout` — atomic tx
- `internal/policy/rollout/service.go::StartRollout` — validation per AC-5
- `internal/store/policy.go` — trigger-aware helpers
- `internal/job/stuck_rollout_reaper.go` (NEW)
- `docs/PRODUCT.md` — 1 SIM = 1 policy doctrine

## Risks & Regression
- **Risk 1 — Trigger performance:** Every policy_assignments write triggers sims row update. At 10K bulk rollout = 10K trigger calls. Mitigation: bulk assign uses single UPDATE with join (not per-row INSERT) — statement-level trigger.
- **Risk 2 — Reconciliation picks wrong canonical:** AC-7 logs every decision; manual review spot-check.
- **Risk 3 — Stuck detector false positive:** AC-8 grace period (10min since last update) before force-complete.
- **Risk 4 — Concurrent rollout prevention breaks existing workflow:** If two admins try to rollout simultaneously, second fails with clear error. Accepted trade-off for correctness.

## Test Plan
- Migration dry-run on prod clone: reconciliation counts sim rows migrated
- Integration: concurrent StartRollout for same policy → second 422
- Integration: CompleteRollout → atomic 3-row transition verified
- Trigger regression: bulk insert 1000 assignments → all sims rows synced in < 1s

## Plan Reference
Priority: P0 · Effort: XL · Wave: 2.5 · Depends: FIX-206 (orphan cleanup — prerequisite for referential integrity)
