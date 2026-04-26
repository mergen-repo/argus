# Post-Story Review: FIX-231 — Policy Version State Machine + Dual-Source Fix

**Date:** 2026-04-26
**Reviewer:** Argus Reviewer Agent (AUTOPILOT)
**Gate result:** PASS (3581 tests PASS / 0 FAIL; web-build PASS; db-seed PASS)

---

## 1. Requirements Tracing

All 11 ACs implemented per gate. Summary:

| AC | Description | Implementation | Status |
|----|-------------|----------------|--------|
| AC-1 | `rolling_out` state during rollout stage | service.go StartRollout, AdvanceRollout | PASS |
| AC-2 | Trigger `trg_sims_policy_version_sync` propagates to `sims.policy_version_id` | migration 20260427000001 | PASS |
| AC-3 | State transition validation (no invalid jumps) | service.go service-layer guards; chk_policy_versions_state CHECK already exists | PASS |
| AC-4 | `CompleteRollout` atomic (rollout→completed, version→active, prior→superseded, policy.current_version_id) | store/policy.go F-A1 CRITICAL fix: supersede first, then activate | PASS |
| AC-5 | Single active rollout per policy — `policy_active_rollout` partial unique index | migration 20260427000001; CreateRollout maps 23505 → ErrRolloutInProgress (422) | PASS |
| AC-6 | Single active version per policy — `policy_active_version` partial unique index | migration 20260427000001; fail-fast guard before index creation | PASS |
| AC-7 | Reconciliation migration handles dual-source drift | migration 20260427000002 (Phase 1 backfill + Phase 2 reconcile) | PASS |
| AC-8 | Stuck rollout reaper cron job | internal/job/stuck_rollout_reaper.go; */5 * * * *; ARGUS_STUCK_ROLLOUT_GRACE_MINUTES knob | PASS |
| AC-9 | `policy_rollouts.policy_id` column + FK + backfill | migration 20260427000001; DEV-345 | PASS |
| AC-10 | Versions tab timeline UI (chips, state colors, hover tooltip, a11y) | web/src/components/policy/versions-tab.tsx; F-U1 a11y fix | PASS |
| AC-11 | 1-SIM-1-policy doctrine doc | docs/PRODUCT.md §Policy Model Doctrine; idx_policy_assignments_sim UNIQUE existing | PASS |

---

## 2. Test Coverage

| Check | Result |
|-------|--------|
| `go test ./... -short -count=1 -race` | 3581 PASS / 0 FAIL across 109 packages |
| DB-gated FIX-231 tests (policy_active_version index intact) | 6/6 store + 2/2 rollout + reaper PASS |
| `make test-db` (new target — auto-detects argus-postgres port) | PASS |
| F-A1 CRITICAL: DROP INDEX workaround removed from 2 test files | PASS — tests exercise real schema |
| `make web-build` | PASS (2.77s) |
| `make db-seed` | PASS |

---

## 3. Architecture / Documentation Checks

### 3.1 ERROR_CODES.md — ROLLOUT_IN_PROGRESS

**Status: NO_CHANGE (pre-existing)**
`ROLLOUT_IN_PROGRESS` (422) confirmed present at line 196 with full example and `CodeRolloutInProgress` Go constant. No action needed.

### 3.2 db/_index.md — TBL-14/15/16

**Status: FIXED by reviewer**
Added FIX-231 constraint notes to TBL-14 (`policy_active_version` partial unique), TBL-15 (canonical source, trigger), and TBL-16 (`policy_id` column + `policy_active_rollout` partial unique).

### 3.3 db/policy.md — TBL-14/15/16

**Status: FIXED by reviewer**
- TBL-14: Added `policy_active_version` partial unique index row + `chk_policy_versions_state` constraint note.
- TBL-15: Added "canonical source of truth" header + `trg_sims_policy_version_sync` trigger row in Triggers section.
- TBL-16: Added `policy_id UUID FK→policies.id NOT NULL` column row; added `idx_policy_rollouts_policy` + `policy_active_rollout` partial unique to Indexes section.

### 3.4 CONFIG.md — ARGUS_STUCK_ROLLOUT_GRACE_MINUTES + cron table

**Status: FIXED by reviewer**
- Added `ARGUS_STUCK_ROLLOUT_GRACE_MINUTES` env var row (int, default 10, range [5,120]) to the Jobs section.
- Added `stuck_rollout_reaper | */5 * * * * | stuck_rollout_reaper | FIX-231` row to the cron table.

### 3.5 .env.example

**Status: FIXED by reviewer**
Added `ARGUS_STUCK_ROLLOUT_GRACE_MINUTES=10` under the `# Stuck Rollout Reaper (FIX-231)` comment, adjacent to ALERT_COOLDOWN_MINUTES.

### 3.6 decisions.md — DEV-345..DEV-353

**Status: FIXED by reviewer**
All 9 story-specific decisions added as DEV-345..DEV-353 with dates 2026-04-27, all ACCEPTED. Topics: policy_rollouts.policy_id column, single-writer trigger, DB CHECK already present, CompleteRollout UPDATE order, migration sequence, reconciliation both-directions, grace period env-knob, fail-fast guard, 1-SIM-1-policy doctrine.

### 3.7 PRODUCT.md — Policy Model Doctrine

**Status: NO_CHANGE (pre-existing)**
`## Policy Model Doctrine — 1 SIM = 1 Policy` confirmed at line 371. Developer completed Task 7 correctly. No action needed.

### 3.8 GLOSSARY.md

**Status: FIXED by reviewer**
- `Version State Machine (Policy)`: updated to describe DB-enforced single-active-version invariant (FIX-231) + reversed UPDATE order in CompleteRollout.
- `Staged Rollout`: updated to describe `policy_active_rollout` partial unique index + 23505 → ErrRolloutInProgress mapping.
- Added new term: `Stuck Rollout Reaper` (job, cron, grace knob, bus event).
- Added new term: `sims_policy_version_sync (Trigger)` (sole-writer contract, AFTER INSERT/UPDATE/DELETE on policy_assignments).

### 3.9 USERTEST.md — FIX-231 section

**Status: FIXED by reviewer**
Added `## FIX-231: Policy Version State Machine + Dual-Source Fix` section with:
- Backend/infra note for ACs 1-9 and AC-11 (points to `make test` + DB index probes).
- Turkish UI scenario for AC-10 (versions tab timeline): 10-step test covering timeline render, state color tokens, hover tooltips, empty state, keyboard accessibility (Tab/Enter/Space), and PAT-018 design token audit via DevTools.

### 3.10 ROUTEMAP.md — FIX-231 row + downstream + Change Log

**Status: FIXED by reviewer**
- FIX-231 row flipped from `[~] IN PROGRESS (Dev)` → `[x] DONE (2026-04-26)`.
- FIX-230/233/242 confirmed as downstream dependents. D-137/D-138 confirmed already present in Tech Debt (no additions needed from reviewer).
- Change Log row added (2026-04-26 | REVIEW | FIX-231 review completed...).

### 3.11 CLAUDE.md — story pointer

**Status: FIXED by reviewer**
Story pointer advanced from FIX-231 (Dev) → FIX-230 (Plan). FIX-230 is the next unblocked P0 story (depends on FIX-231 which is now DONE).

---

## 4. Tech Debt Cross-Reference

| ROUTEMAP ID | Finding | Description | Status |
|-------------|---------|-------------|--------|
| D-137 | F-A11 | `sims_policy_version_sync` trigger writes `updated_at = NOW()` even on no-op same-value updates; minor write amplification on partitioned sims table. Future fix: add `IF OLD.policy_version_id IS DISTINCT FROM NEW.policy_version_id` guard. | DEFERRED |
| D-138 | F-B2 | `TestSimsPolicyVersionSync_BulkInsert` rolls back its 1000-row insert transaction; a sibling test that COMMITs and asserts post-commit invariants would strengthen coverage. | DEFERRED |

---

## 5. Gate Findings Summary

16 findings from gate (CRITICAL: 1, HIGH: 2+, MEDIUM: 4, LOW: 7). All 14 fixes applied in gate. Key fix:

**F-A1 CRITICAL** (`CompleteRollout` UPDATE order): reversed supersede-before-activate to prevent 23505 collision on `policy_active_version`. Two test files had `DROP INDEX IF EXISTS policy_active_version` workarounds that were masking the bug — both removed.

---

## 6. Story Impact

| Story | Impact |
|-------|--------|
| FIX-230 (Rollout DSL Match Integration) | Now unblocked. Depends on FIX-231 (DONE). |
| FIX-233 (SIM List Policy column + Rollout Cohort filter) | Now unblocked. Depends on FIX-230, FIX-231. |
| FIX-242 (Policy version state UI — detail panel chip) | Now unblocked. Depends on FIX-231. |
| FIX-232 (Rollout UI Active State) | Depends on FIX-212, FIX-230 — not yet unblocked. |

---

## 7. Checklist (14-point)

| # | Check | Result |
|---|-------|--------|
| 1 | AC traceability (all ACs implemented) | PASS — 11/11 |
| 2 | Test coverage (unit + integration) | PASS — 3581 tests |
| 3 | No new tech debt introduced without ROUTEMAP entry | PASS — D-137/D-138 already logged |
| 4 | ERROR_CODES.md updated for new codes | NO_CHANGE — ROLLOUT_IN_PROGRESS pre-existing |
| 5 | CONFIG.md updated for new env vars | FIXED — ARGUS_STUCK_ROLLOUT_GRACE_MINUTES + cron row |
| 6 | .env.example updated | FIXED — ARGUS_STUCK_ROLLOUT_GRACE_MINUTES=10 |
| 7 | db/ schema docs updated | FIXED — policy.md TBL-14/15/16 + _index.md rows |
| 8 | decisions.md updated (DEV-NNN) | FIXED — DEV-345..DEV-353 |
| 9 | PRODUCT.md doctrine updated | NO_CHANGE — pre-existing §Policy Model Doctrine |
| 10 | USERTEST.md section added | FIXED — FIX-231 section with AC-10 Turkish UI scenario |
| 11 | GLOSSARY.md updated | FIXED — 2 terms updated + 2 new terms |
| 12 | ROUTEMAP status flipped + Change Log | FIXED — DONE (2026-04-26) + Change Log row |
| 13 | CLAUDE.md story pointer advanced | FIXED — FIX-231 → FIX-230 (Plan) |
| 14 | Build clean (go build + web-build + db-seed) | PASS |

**Overall: PASS — all 14 checks complete.**
