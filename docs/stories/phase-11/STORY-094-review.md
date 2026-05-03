# Post-Story Review: STORY-094 — SIM-Device Binding Model + Policy DSL Extension

> Date: 2026-05-01

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-095 | Consumes D-187 `simAllowlistStore` (dormant in 094, production consumer required here); must replace `device.imei_in_pool` placeholder evaluator with real pool lookup; migration timestamps must exceed 20260507000004. | UPDATED — handoff notes appended |
| STORY-096 | Inherits `sims` binding columns + `SetDeviceBinding` store as write target for enforcement; consumes D-184 1M-SIM bench (re-targeted here from 094); `IMEIHistoryStore.Append` stub must be fully implemented here. | UPDATED — handoff notes appended |
| STORY-097 | Inherits D-183 PEIRaw re-target (definitive target is now this story); D-182 fully resolved (Diameter S6a listener wired in 094); `ClearBoundIMEI` API-329 surface available; change-detection reads `imei_history` rows written by 096. | UPDATED — handoff notes appended |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/ROUTEMAP.md` | STORY-094 status `[~] IN PROGRESS` → `[x] DONE 2026-05-01`; D-182 OPEN → ✓ RESOLVED (Gate missed marking); D-183 target `STORY-094 / STORY-097` → `STORY-097`; D-184 target `STORY-094` → `STORY-096` | UPDATED |
| `docs/GLOSSARY.md` | Added 3 terms: Notify-Request (S6a), Update-Location-Request (S6a), Bound IMEI | UPDATED |
| `docs/USERTEST.md` | Appended `## STORY-094:` section (14 manual test scenarios) | UPDATED |
| `docs/brainstorming/bug-patterns.md` | Added PAT-006 RECURRENCE [STORY-094 Gate F-A2] (bulk worker silent field clear); added PAT-031 NEW [STORY-094 Gate F-LEAD-1] (JSON pointer tri-state decoder) | UPDATED |
| `docs/architecture/ERROR_CODES.md` | Added `INVALID_BINDING_MODE`, `INVALID_IMEI`, `INVALID_BINDING_STATUS` (table rows + Go constants) | UPDATED |
| `docs/architecture/DSL_GRAMMAR.md` | Already fully documented (lines 43-55, 147-156, 298-335). | NO_CHANGE |
| `docs/architecture/api/_index.md` | API-327..330+336 already registered by Phase 11 architect dispatch. Total = 276 (unchanged by this story; 4 endpoints were pre-registered). | NO_CHANGE |
| `docs/architecture/db/_index.md` | TBL-59, TBL-60 already registered; TBL-10 binding column note already present. | NO_CHANGE |
| `docs/architecture/CONFIG.md` | No new env vars shipped. | NO_CHANGE |
| `docs/ARCHITECTURE.md` | Backend-only story; no architectural change. | NO_CHANGE |
| `docs/SCREENS.md` | Backend-only story; no screen changes. | NO_CHANGE |
| `docs/FRONTEND.md` | Backend-only story. | NO_CHANGE |
| `docs/FUTURE.md` | No new future items identified. | NO_CHANGE |
| `docs/stories/phase-11/STORY-095-imei-pool-management.md` | Appended STORY-094 Handoff Notes | UPDATED |
| `docs/stories/phase-11/STORY-096-binding-enforcement.md` | Appended STORY-094 Handoff Notes | UPDATED |
| `docs/stories/phase-11/STORY-097-imei-change-detection.md` | Appended STORY-094 Handoff Notes | UPDATED |
| `docs/brainstorming/decisions.md` | VAL-042..VAL-047 already present (Gate Lead added them). | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- DSL_GRAMMAR.md lines 43-55 and 147-156 match Plan §DSL Extension and AC-11/12 exactly.
- API-327/328/330/336 in api/_index.md match Plan §API Specifications byte-for-byte (post F-A5 amendment).
- TBL-10 binding column note in db/_index.md matches Migration 1 SQL exactly.
- Error codes INVALID_BINDING_MODE / INVALID_IMEI / INVALID_BINDING_STATUS were absent from ERROR_CODES.md pre-review; now added.

## Decision Tracing

- Decisions checked: VAL-042, VAL-043, VAL-044, VAL-045, VAL-046, VAL-047 (all present in decisions.md lines 306-311)
- Orphaned (approved but not applied): 0
- Plan-era decisions DEV-409, DEV-410, DEV-412 verified reflected in migration SQL (NULL default, composite-PK FK comment, DSL pool predicate).

## USERTEST Completeness

- Entry exists: YES (appended by this review)
- Type: Backend/API-level scenarios (14 scenarios: GET/PATCH/history/bulk/DSL/Diameter/regression)
- Note in prior STORY-093 section (line 5743) referenced "D-182 → STORY-094" — this is now outdated wording since D-182 is RESOLVED; the note is informational only and does not block.

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 3 (D-182, D-183, D-184)
- Already ✓ RESOLVED by Gate: 0 (Gate omitted marking D-182 as resolved despite Task 7 shipping S6a listener)
- Resolved by Reviewer (Gate missed marking): 1 → D-182 updated to `✓ RESOLVED [STORY-094 Task 7]`
- NOT addressed (CRITICAL): 0
- Re-targeted by plan (Reviewer confirmed): D-183 → STORY-097 (OPEN); D-184 → STORY-096 (OPEN)

## Mock Status

- N/A — backend-only story; `web/src/mocks/` has no mocks for these endpoints (UI controls land in STORY-097).

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | D-182 OPEN in ROUTEMAP despite Task 7 shipping the S6a Notify/ULR listener (Gate omitted marking) | NON-BLOCKING | FIXED | ROUTEMAP D-182 status updated to `✓ RESOLVED [STORY-094 Task 7]` by Reviewer |
| 2 | D-183 target stale (`STORY-094 / STORY-097`) — STORY-094 plan decision was NO fold-in | NON-BLOCKING | FIXED | ROUTEMAP D-183 target updated to `STORY-097` |
| 3 | D-184 target stale (`STORY-094`) — 1M-SIM bench not runnable without enforcement on hot path | NON-BLOCKING | FIXED | ROUTEMAP D-184 target updated to `STORY-096` |
| 4 | STORY-094 status `[~] IN PROGRESS` not updated to DONE in ROUTEMAP | NON-BLOCKING | FIXED | ROUTEMAP line 514 updated to `[x] DONE 2026-05-01` |
| 5 | Error codes INVALID_BINDING_MODE, INVALID_IMEI, INVALID_BINDING_STATUS missing from ERROR_CODES.md | NON-BLOCKING | FIXED | Three rows added to SIM Errors table + Go constants added |
| 6 | GLOSSARY missing: Bound IMEI, Notify-Request (S6a), Update-Location-Request (S6a) | NON-BLOCKING | FIXED | Three terms added after Terminal-Information AVP entry |
| 7 | USERTEST.md had no STORY-094 section | NON-BLOCKING | FIXED | 14 manual test scenarios appended |
| 8 | PAT-006 RECURRENCE (F-A2 bulk worker field-clear shape) not annotated in bug-patterns.md | NON-BLOCKING | FIXED | PAT-006 RECURRENCE [STORY-094 Gate F-A2] added |
| 9 | PAT-031 (F-LEAD-1 JSON tri-state decoder) — new pattern, not yet in bug-patterns.md | NON-BLOCKING | FIXED | PAT-031 NEW added to bug-patterns.md |
| 10 | STORY-095/096/097 lacked STORY-094 handoff notes | NON-BLOCKING | FIXED | Handoff sections appended to all three downstream story files |

## Project Health

- Stories completed: STORY-094 is the 4th closed story in Phase 11 (following STORY-093, 092, 091). Phase 11 P0 total: 4 stories (STORY-091..094) of 8 P0 stories done.
- Current phase: Phase 11 — IMEI / Device Binding Epic
- Next story: STORY-095 (IMEI Pool Management)
- Blockers: None. STORY-095 can begin; all prerequisites (TBL-59/60/binding columns, `simAllowlistStore`, DSL `device.imei_in_pool` parser) are landed.
