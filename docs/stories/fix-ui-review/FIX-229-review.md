# Post-Story Review: FIX-229 — Alert Feature Enhancements (Mute UX, Export, Similar Clustering, Retention)

> Date: 2026-04-25

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-241 (Global API nil-slice `WriteList` helper) | `ListSimilar` handler uses `make([]alertDTO, 0, len(similar))` — nil-slice safe. Export endpoints return binary blobs, not JSON lists — `WriteList` helper does not apply. NO_CHANGE needed. | NO_CHANGE |
| FIX-248 (Reports Subsystem Refactor) | D-135 explicitly records that FIX-229 alert export collector is the migration target. FIX-248 plan already aware via ROUTEMAP Tech Debt. | NO_CHANGE |
| FIX-237 (M2M Event Taxonomy + Notification Redesign) | Depends on FIX-212 (event envelope), not FIX-229. Alert suppression does not affect notification dispatch logic. | NO_CHANGE |
| FIX-244 (Violations lifecycle UI — acknowledge/remediate) | Alert ack/resolve patterns unchanged by FIX-229 (those are PATCH /alerts/{id} state transitions, untouched). FIX-244 is purely violations, orthogonal. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/architecture/api/_index.md` | Added API-319..325 (7 rows): suppressions CRUD, similar-alerts, export tri-format. Total updated 249→256. | UPDATED |
| `docs/architecture/ERROR_CODES.md` | Added `ALERT_NO_DATA` (404), `SUPPRESSION_NOT_FOUND` (404), `DUPLICATE` (409) to prose table and Go constants block. | UPDATED |
| `docs/SCREENS.md` | SCR-172 annotated with FIX-229 row-expand subtabs + deeplink. SCR-183 annotated with MutePanel + UnmuteDialog + Export dropdown. SCR-195 added for `/settings/alert-rules`. Total 83→84. | UPDATED |
| `docs/brainstorming/decisions.md` | DEV-333..DEV-344 added (12 plan-era pinned decisions). | UPDATED |
| `docs/ROUTEMAP.md` | FIX-229 row `[~] IN PROGRESS (Dev)` → `[x] DONE (2026-04-25)`. Decision Log entry added. | UPDATED |
| `docs/USERTEST.md` | FIX-229 section added — 5 AC scenarios in Turkish. | UPDATED |
| `docs/architecture/db/_index.md` | TBL-55 `alert_suppressions` already present (pre-existing, correct). | NO_CHANGE |
| `docs/ARCHITECTURE.md` | FIX-229 extends existing alert/report subsystems inline — no new SVC or top-level structural change required. | NO_CHANGE |
| `docs/GLOSSARY.md` | No new domain terms beyond what FIX-209/210 already introduced (alert, suppression, dedup_key). | NO_CHANGE |
| `docs/FRONTEND.md` | No new design tokens or pattern library entries. Radio atom follows existing Checkbox wrapper pattern — already documented. | NO_CHANGE |
| `docs/FUTURE.md` | No new extension points revealed. | NO_CHANGE |
| `Makefile` | No new targets or env vars. | NO_CHANGE |
| `CLAUDE.md` | No Docker URL/port changes. | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- `SERVICE_UNAVAILABLE` (503) was already present in ERROR_CODES.md at line 277/388 — NOT re-added. Verified correct.
- `CodeDuplicate = "DUPLICATE"` in apierr.go distinct from `CodeDuplicateProfile = "DUPLICATE_PROFILE"` — both exist without conflict.
- TBL-55 `alert_suppressions` in db/_index.md line 65 verified present and correct.

## Decision Tracing

- Decisions checked: 12 (DEV-333..DEV-344 from plan Pinned Decisions)
- DEV-333..DEV-344 were NOT logged in decisions.md before this review (tail ended at DEV-332). Added in this review cycle.
- All 12 decisions verified as implemented per Gate report findings F-A1..F-A10, F-B1..F-B6, F-U1..F-U4 (PASS).
- Orphaned (approved but not applied): 0

## USERTEST Completeness

- Entry exists: YES (added this review cycle)
- Type: UI scenarios — 5 scenario groups covering all 5 ACs

## Tech Debt Pickup (from ROUTEMAP)

| D-ID | Source | Status | Notes |
|------|--------|--------|-------|
| D-132 | FIX-229 plan §Tech Debt | DEFERRED | Alerts PDF export synchronous `report.Engine.Build` — deferred to FIX-248 |
| D-133 | FIX-229 plan §Tech Debt | DEFERRED | Backfill UPDATE on POST /alerts/suppressions — deferred |
| D-134 | FIX-229 DEV-336 R3 | DEFERRED | AlertsRetentionProcessor single-query DELETE — deferred |
| D-135 | FIX-229 Gate F-A6 | DEFERRED | Export pagination 100-row RTT loop — deferred to FIX-248 |
| D-136 | FIX-229 Gate F-A8 | DEFERRED | Similar-alerts overflow "+N more" no exact count — deferred to FIX-24x |

- Items targeting THIS story: 0 (D-132..D-136 are items created BY this story, targeting future stories)
- Pre-existing items resolved by this story: 0
- NOT addressed (CRITICAL): 0

## Mock Status

- `web/src/mocks/` directory: not applicable (this project does not use mocks directory pattern)
- `useExport('analytics/anomalies')` bug retired — replaced by `useAlertExport` (verified in Gate fix #15/DEV-343). RESOLVED.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | API-319..325 missing from `api/_index.md` — 6 new endpoints (suppressions CRUD, similar, export×3) had no index entries | NON-BLOCKING | FIXED | Added API-319..325, total counter updated 249→256 |
| 2 | `ALERT_NO_DATA`, `SUPPRESSION_NOT_FOUND`, `DUPLICATE` missing from `ERROR_CODES.md` prose table and constants block | NON-BLOCKING | FIXED | Added all 3 to both table and Go constants block |
| 3 | DEV-333..DEV-344 (12 plan decisions) not logged in `decisions.md` | NON-BLOCKING | FIXED | Added all 12 with 2026-04-25 date and ACCEPTED status |
| 4 | SCR-195 (`/settings/alert-rules`) missing from `SCREENS.md`; SCR-172 and SCR-183 unannotated for FIX-229 changes | NON-BLOCKING | FIXED | SCR-195 added; SCR-172 and SCR-183 annotated with FIX-229 changes |
| 5 | USERTEST.md had no FIX-229 section | NON-BLOCKING | FIXED | 5 AC scenario groups added in Turkish |
| 6 | Gate deferred items D-135 and D-136 logged with `D-NNN` placeholder IDs — ROUTEMAP already assigned D-135/D-136 | NON-BLOCKING | RESOLVED | D-135 and D-136 confirmed correctly assigned in ROUTEMAP lines 734-735 |

## Project Health

- Stories completed: FIX-201..FIX-229 in UI Review Remediation track (Wave 1..7 complete)
- Current phase: UI Review Remediation Wave 7 complete
- Next story: FIX-230 (Rollout DSL Match Integration — P0, Wave 2.5)
- Blockers: None introduced by FIX-229
