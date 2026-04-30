# Post-Story Review: STORY-061 — eSIM Model Evolution

> Date: 2026-04-12

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-064 | DB hardening story already mentions `esim_profiles` tenant-scope in AC-2 (`GetBySimID` and other store methods scope on tenantID via JOIN sims). The new multi-profile schema (profile_id column, 3 new indexes) is compatible — STORY-064's planned index/RLS work applies cleanly. No story file changes needed. | NO_CHANGE |
| STORY-070 | Frontend real-data wiring: eSIM tab is now live with multi-profile support. STORY-070 should wire `useESimListBySim` and the Create/Delete hooks if any mock wiring exists. No mocks directory exists in this project, so no explicit mock retirement needed. | NO_CHANGE |
| STORY-075 | Cross-entity context story: SIM detail enrichment now includes multi-profile eSIM tab. Profile state (available/enabled/disabled) is relevant context to surface in cross-entity views. No story file edit needed; the tab already exists. | NO_CHANGE |
| STORY-062 | D-003 (stale SCR IDs) already targets this story. SCR-072/SCR-073 referenced in STORY-061 story file do not exist in SCREENS.md. Covered by existing D-003. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/architecture/db/sim-apn.md` (TBL-12) | Added profile_id column; updated profile_state to 4 states + CHECK constraint; changed default to 'available'; replaced UNIQUE(sim_id) with 3 new indexes; added migration reference and multi-profile note | UPDATED |
| `docs/architecture/api/_index.md` | Added API-075 (POST /esim-profiles) and API-076 (DELETE /esim-profiles/:id); updated API-072 description (accepts available state); updated API-074 description (old→available, IP release, policy clear); eSIM section header 5→7; footer total 111→113 | UPDATED |
| `docs/ARCHITECTURE.md` | Header scale: 111 APIs → 113 APIs | UPDATED |
| `docs/GLOSSARY.md` | Updated "eSIM Profile State Machine" (4 states, available default, partial unique, switch→available); updated "Profile Switch (eSIM)" (old→available per DEV-164, IP release, policy clear, NATS event); added "Profile Load" term; added "Multi-profile eSIM" term | UPDATED |
| `docs/USERTEST.md` | Added STORY-061 section (14 scenarios: eSIM tab UI flow, SCR-070 delete, altyapi DB constraint checks) | UPDATED |
| `docs/ROUTEMAP.md` | STORY-061 marked DONE (2026-04-12); counter 6/22 → 7/22; current story updated to STORY-064; change log entry added | UPDATED |
| `docs/architecture/ERROR_CODES.md` | All 5 new codes confirmed present (PROFILE_LIMIT_EXCEEDED, CANNOT_DELETE_ENABLED_PROFILE, DUPLICATE_PROFILE, PROFILE_NOT_AVAILABLE, IP_RELEASE_FAILED) | NO_CHANGE |
| `docs/brainstorming/decisions.md` | DEV-164 (old→available on switch) and DEV-165 (max 8 profiles) confirmed present at lines 379-380 | NO_CHANGE |
| `docs/architecture/CONFIG.md` | No new env vars introduced by STORY-061 | NO_CHANGE |
| `docs/CLAUDE.md` | No Docker URL/port changes | NO_CHANGE |
| Makefile | No new targets | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 6 (all fixed — see Issues table)
- ERROR_CODES.md: All 5 new eSIM error codes present and consistent with Go constants in `internal/apierr/apierr.go`. PASS.
- api/_index.md: SESSION_DISCONNECT_FAILED (API-074) referenced correctly. PASS.
- decisions.md: DEV-164 and DEV-165 both ACCEPTED, both verified implemented in gate (esim.go:382, handler.go:580). PASS.

## Decision Tracing

- Decisions checked: 2 (DEV-164, DEV-165)
- Orphaned (approved but not applied): 0
- DEV-164: `store.Switch` sets source state to `'available'` (esim.go:382). PASS.
- DEV-165: `handler.Create` checks `count >= 8`, returns 422 PROFILE_LIMIT_EXCEEDED (handler.go:580). PASS.

## USERTEST Completeness

- Entry exists: YES (added this review)
- Type: UI scenarios (10 scenarios for eSIM tab + SCR-070) + altyapi (4 DB-level checks)
- Count: 14 scenarios total

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 0
- Already RESOLVED by Gate: N/A
- Resolved by Reviewer: 0
- NOT addressed (CRITICAL): 0

## Mock Status

- No `web/src/mocks/` directory exists in this project. N/A.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | TBL-12 schema in `db/sim-apn.md` completely stale: missing `profile_id` column, wrong default (`disabled` vs `available`), only 3 states listed (missing `available`), incorrect UNIQUE index, missing 3 new indexes and CHECK constraint | NON-BLOCKING | FIXED | Updated TBL-12 entry with all new columns, correct 4-state model, partial unique indexes (idx_esim_profiles_sim_enabled, idx_esim_profiles_sim_profile, idx_esim_profiles_sim_state), CHECK constraint, and migration reference |
| 2 | `api/_index.md` eSIM section lists 5 endpoints but STORY-061 added 2 new: POST /api/v1/esim-profiles (API-075) and DELETE /api/v1/esim-profiles/:id (API-076) | NON-BLOCKING | FIXED | Added API-075 and API-076 rows; section header updated 5→7; footer updated 111→113 |
| 3 | `ARCHITECTURE.md` header still says "111 APIs" after 2 new endpoints added | NON-BLOCKING | FIXED | Updated to "113 APIs" |
| 4 | GLOSSARY "eSIM Profile State Machine": describes 3-state model (disabled/enabled/deleted), default `disabled`, no mention of partial unique; contradicts implemented 4-state model | NON-BLOCKING | FIXED | Updated entry to describe 4 states, `available` default, partial unique index, CHECK constraint, correct switch→available transition |
| 5 | GLOSSARY "Profile Switch (eSIM)": says old profile transitions to `disabled`; contradicts DEV-164 (old→available on switch) and implemented behavior in esim.go:382; also missing IP release and policy clear steps | NON-BLOCKING | FIXED | Updated to reflect available-transition per DEV-164, added IP release + policy clear + NATS event details |
| 6 | USERTEST.md missing STORY-061 section entirely; story is UI-bearing (eSIM tab, Load Profile, Enable/Switch/Delete actions) | NON-BLOCKING | FIXED | Added 14-scenario STORY-061 section covering UI flow (SCR-021 eSIM tab, SCR-070) and altyapi constraint checks |
| 7 | STORY-061 story file references SCR-072/SCR-073 which do not exist in SCREENS.md (eSIM tab is a SIM detail sub-view, standalone page is SCR-070) | NON-BLOCKING | DEFERRED D-003 | Already tracked as D-003 (stale SCR IDs in story files) targeting STORY-062. Per reviewer protocol, story files are not edited post-closure. |

## Project Health

- Stories completed: 7/22 Phase 10 (7/77 overall in Phase 10 scope; all 55 dev phase stories complete)
- Current phase: Phase 10 — Cleanup & Production Hardening
- Next story: STORY-064 (Database Hardening & Partition Automation)
- Blockers: None
