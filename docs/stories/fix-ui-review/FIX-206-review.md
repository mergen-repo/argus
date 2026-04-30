# Post-Story Review: FIX-206 — Orphan Operator IDs Cleanup + FK Constraints + Seed Fix

> Date: 2026-04-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-207 | Session/CDR Data Integrity — scope confirmed as CHECK constraints + IMSI format (duration ≥ 0, IP pool validation). Does NOT include sessions.operator_id or cdrs.operator_id FK adds — those are correctly deferred as D-062/D-063. FIX-207 ACs are unaffected by FIX-206's scope-cut. | NO_CHANGE |
| FIX-208 | Cross-Tab Data Aggregation Unify depends on FIX-206 (operator orphans caused null-operator math errors). With FIX-206 DONE and all 200 orphan SIMs remapped, the null-operator aggregation bug is eliminated at source. FIX-208 can proceed as planned. | NO_CHANGE |
| FIX-231 | Policy Version State Machine — listed in dispatch as unblocked by FIX-206 (referential integrity foundation). No ACs changed; dependency satisfied. | NO_CHANGE |
| FIX-202 (completed) | ListEnriched LEFT JOIN + COALESCE pattern preserved for operator-delete race path (`simEnrichedJoin` comment at line 1327: "LEFT JOINs are required for AC-8 orphan safety — INNER would hide orphan rows"). FIX-202's orphan-safe resolver still handles the race between `operatorStore.GetByID` handler check and an in-flight operator delete. FIX-206 eliminates the seed-induced structural orphans but does not remove the need for the LEFT JOIN in production races. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| docs/architecture/api/_index.md | API-042 (POST /sims) row updated: added 400 `INVALID_REFERENCE` error path documentation — FK-violation defense-in-depth, primary path 404 `NOT_FOUND`, added FIX-206 story reference. | UPDATED |
| docs/ROUTEMAP.md | FIX-206 status flipped `[~] IN PROGRESS (Dev)` → `[x] DONE (2026-04-20)`; Change Log REVIEW row added (2026-04-20). | UPDATED |
| CLAUDE.md | Active Session Story advanced from FIX-206 → FIX-207; Step: Plan. | UPDATED |
| docs/architecture/db/sim-apn.md | TBL-10 Foreign Keys section added (3 FKs: fk_sims_operator RESTRICT, fk_sims_apn SET NULL, fk_sims_ip_address SET NULL). Already done by developer in Task 6. | NO_CHANGE |
| docs/architecture/ERROR_CODES.md | `INVALID_REFERENCE` row added under Validation Errors + `CodeInvalidReference` constant in Go constants ledger. Already done by Gate fix #2. | NO_CHANGE |
| docs/USERTEST.md | FIX-206 section added (4 scenarios: AC-4 fresh-volume zero orphans, AC-2/3 FK constraints installed, AC-7 HTTP 400 INVALID_REFERENCE, AC-2 RESTRICT blocks operator delete). Already done by Gate fix #6. | NO_CHANGE |
| docs/brainstorming/decisions.md | DEV-264..DEV-267 appended (all dated 2026-04-20): sims FK direction, Migration A deterministic remap, Migration B plain ADD CONSTRAINT, handler/store FK violation translation. Already done by developer. | NO_CHANGE |
| docs/ARCHITECTURE.md | No changes needed — migration list is illustrative (sample files), not exhaustive. | NO_CHANGE |
| docs/PRODUCT.md | No changes needed — operator-delete business rule ("reassign-before-delete") is enforced structurally by the FK; no product narrative change required. | NO_CHANGE |
| internal/store/errors.go | PAT-006 reminder comment added naming D-062/D-063/D-064/D-065 as follow-up FK stories. Already done by Gate fix #5. | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- db/_index.md TBL-10 relations column (`→ TBL-01, → TBL-05, → TBL-07, → TBL-15`) — FK arrows now enforced by real DB constraints. Column is a summary reference, not a constraint-type indicator; no update required.
- ARCHITECTURE.md: no FK inventory section; migration tree is illustrative. No contradiction.
- PRODUCT.md: operator-delete semantic (FK RESTRICT = "reassign before delete") is implicit in the FK choice. No business-rule text contradicted.

## Decision Tracing

- Decisions checked: DEV-264, DEV-265, DEV-266, DEV-267
- DEV-264 (FK direction FROM partitioned INTO non-partitioned) — verified: Migration B uses standard FK syntax on `sims` parent. PAT-004 clarification correct. PASS.
- DEV-265 (Migration A deterministic REMAP, suspend as defense-in-depth, fresh-volume short-circuit) — verified: migration file uses remap logic + suspend fallback + pre-flight count check. PASS.
- DEV-266 (Migration B plain ADD CONSTRAINT, NOT VALID split blocked by PG16) — verified: Migration B file uses plain `ADD CONSTRAINT` + DO block WARNING at >100k rows (D-065 pointer). PASS.
- DEV-267 (handler/store FK violation translation to INVALID_REFERENCE) — verified: `internal/store/errors.go` exports `ErrInvalidReference`, `InvalidReferenceError`, `asInvalidReference`; `internal/store/sim.go` Create routes through it; `internal/api/sim/handler.go` Create maps `*InvalidReferenceError` → HTTP 400 `INVALID_REFERENCE`. PASS.
- Orphaned decisions (approved but not applied): 0

## USERTEST Completeness

- Entry exists: YES — `docs/USERTEST.md` line 2875
- Type: Backend/DB story — 4 manual DB verification scenarios (bash psql commands + expected output)
- Scenarios cover: AC-4 fresh-volume orphan check, AC-2/3 FK install check, AC-7 HTTP error path, AC-2 RESTRICT delete-block. All correct.
- Scenario 3 (AC-7) correctly documents that handler-layer 404 is the primary path; defensive FK 400 is race-only, validated by integration tests.

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 0 pre-existing D-items targeted FIX-206 specifically.
- New D-items created by this story: 5 (D-062..D-066), all written to ROUTEMAP by Gate fix #7 and developer Task 6.
- Status check:
  - D-062 (sessions hypertable FK): OPEN — correctly deferred. FIX-207 scope confirmed clear.
  - D-063 (cdrs hypertable FK): OPEN — correctly deferred.
  - D-064 (operator_health_logs hypertable FK): OPEN — correctly deferred.
  - D-065 (Migration B prod 10M-row cutover runbook): OPEN — pre-prod blocker per gate E-1. WARNING emitted at migrate-time for >100k rows.
  - D-066 (13 pre-existing test failures): OPEN — correctly tracked. Unrelated to sims/FKs.
- Items resolved this story: 0 pre-existing D-items.
- NOT addressed (CRITICAL): 0

## Mock Status

N/A — backend-only story, no frontend mocks. `src/mocks/` not applicable.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | API-042 (POST /sims) in `docs/architecture/api/_index.md` missing the new `INVALID_REFERENCE` error path. Prior FIX-202/203/205 reviews fixed API index rows for new error paths — same pattern applies here. | NON-BLOCKING | FIXED | Updated API-042 row to document 400 `INVALID_REFERENCE` as defensive error path + added FIX-206 story reference. |
| 2 | ROUTEMAP FIX-206 row still shows `[~] IN PROGRESS (Dev)` — gate PASS was confirmed but status not flipped. | NON-BLOCKING | FIXED | Flipped to `[x] DONE (2026-04-20)`. |
| 3 | ROUTEMAP Change Log missing FIX-206 REVIEW entry. | NON-BLOCKING | FIXED | Added REVIEW row (2026-04-20) with review summary, impacted files, and pre-prod blocker callout. |
| 4 | CLAUDE.md Active Session still points at FIX-206/Step:Plan — must advance to next story. | NON-BLOCKING | FIXED | Advanced to Story: FIX-207, Step: Plan. |
| 5 | E-1 Production 10M-row Migration B cutover (D-065) is a pre-prod blocker — risk that this gets silently deprioritized as FIX track accelerates. | MEDIUM | DEFERRED D-065 | Already in ROUTEMAP D-065 (OPEN, target: pre-prod infrastructure). Reviewer callout in Change Log row ensures visibility. Per-partition runbook (detach → NOT VALID + VALIDATE per partition → re-attach) must be authored and tested against a prod-clone before any production deploy of FIX-206 migration. |

## Project Health

- UI Review Remediation stories completed: 6/44 (FIX-201..FIX-206; 14%)
- Current wave: Wave 1 — P0 Backend Contract + Data Foundation
- Wave 1 status: 6/7 stories done (FIX-207 PENDING, depends on FIX-206 which is now DONE)
- Next story: FIX-207 — Session/CDR Data Integrity
- Blockers: D-065 pre-prod infrastructure (production deploy only; development unblocked)
