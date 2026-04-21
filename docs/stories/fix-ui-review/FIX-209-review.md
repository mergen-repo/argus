# Post-Story Review: FIX-209 — Unified `alerts` Table + Operator/Infra Alert Persistence

> Date: 2026-04-21

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-210 | Directly inherits `dedup_key VARCHAR(255)` column + `suppressed` state reserved in TBL-53; `UpdateState` correctly rejects `suppressed` so FIX-210 must add a separate `SuppressAlert` store method. `idx_alerts_dedup` partial index already in place. D-076 (3-enum drift) targets FIX-210. | REPORT ONLY — no plan edit |
| FIX-212 | `parseAlertPayload` sentinel `systemTenantID` (`00000000-0000-0000-0000-000000000000`) must be removed once FIX-212 normalizes all publisher envelopes with mandatory `tenant_id`. D-075 already tracks this dependency. | REPORT ONLY — no plan edit |
| FIX-213 | Live Event Stream UX consumes unified alerts via `alertStore.ListByTenant`. No structural changes needed; FIX-213 reads the same endpoint (API-313) and should inherit the Source filter chip pattern from FIX-209 FE. | REPORT ONLY — no plan edit |
| FIX-229 | Alert Feature Enhancements (Mute All UX, Export format, Similar count) depends on the unified alerts table. D-073 (CSV export stale endpoint) is the active tech debt item targeting this story. | REPORT ONLY — no plan edit |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| docs/USERTEST.md | Added FIX-209 section — 4 scenarios (alerts list Source filter + invalid source 400, detail ack/resolve + Escalate gating, dashboard Recent Alerts + Source chip, retention job observation) | UPDATED |
| docs/GLOSSARY.md | Added 4 terms: Alert, Alert State, Alert Source, Unified Alerts Table | UPDATED |
| docs/ARCHITECTURE.md | Added to file tree: `notification/` SVC-08 refactor note, `api/alert/` handler, `store/alert.go`, `job/alerts_retention.go`; added AlertsRetentionJob bullet under Backup Infrastructure section | UPDATED |
| docs/SCREENS.md | SCR-183 Notes updated with FIX-209 annotation (unified multi-source feed, Source chip, TBL-53 backing); SCR-010 updated with AlertFeed mount note | UPDATED |
| docs/brainstorming/decisions.md | Added DEV-270 (Option B tolerant persist + systemTenantID sentinel), DEV-271 (Escalate gated to source=sim + anomaly_id), DEV-272 (AlertFeed dashboard mount + alertStore swap) | UPDATED |
| .env.example | Added `ALERTS_RETENTION_DAYS=180` under new `── Alerts Retention (FIX-209) ──` section | UPDATED |
| docs/ROUTEMAP.md | FIX-209 row flipped to `[x] DONE (2026-04-21)`; Change Log entry added | UPDATED |
| CLAUDE.md | Story pointer advanced to FIX-210; Step set to Plan | UPDATED |
| docs/architecture/ERROR_CODES.md | Verified: Alerts Taxonomy section + ALERT_NOT_FOUND row + state table corrections already present from Gate F-A5 fix | NO_CHANGE |
| docs/architecture/CONFIG.md | Verified: ALERTS_RETENTION_DAYS row present from Gate Task 7 | NO_CHANGE |
| docs/architecture/api/_index.md | Verified: API-313/314/315 rows present from Gate Task 7 | NO_CHANGE |
| docs/architecture/db/_index.md | Verified: TBL-53 row present from Gate Task 7 | NO_CHANGE |
| docs/PRODUCT.md | F-043 already references "alert feed"; no new feature category needed | NO_CHANGE |
| docs/FUTURE.md | FTR-001 AI Anomaly Engine already references alert pipeline; no new future items | NO_CHANGE |
| Makefile | No new targets required (alerts retention is an in-process cron, not a CLI target) | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- ERROR_CODES.md Alerts Taxonomy section verified coherent (4-state machine described, FIX-210 suppressed reserved, publisher tolerance paragraph present).
- CONFIG.md ALERTS_RETENTION_DAYS row verified (default 180, min 30, FIX-209 tagged).
- TBL-53 in db/_index.md verified consistent with migration `20260422000001_alerts_table.up.sql`.
- API-313/314/315 in api/_index.md verified consistent with handler implementation.

## Decision Tracing

- Decisions checked: 3 (DEV-270, DEV-271, DEV-272 — all written this review)
- Orphaned (approved but not applied): 0
- Gate Escalated Issues: None (advisor confirmed Option B before substantive work — no plan/scout conflict escalated to user)

## USERTEST Completeness

- Entry exists: YES (written this review cycle)
- Type: Mixed UI + backend scenarios (4 scenarios covering list filter, detail state machine, dashboard panel, retention job config observation)

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting FIX-209 at start of story: 0 (ROUTEMAP Tech Debt table had no pre-existing items targeting FIX-209)
- Items created by Gate for FIX-209: 4 (D-073, D-074, D-075, D-076)
- Verified present in ROUTEMAP: 4 ✓
- NOT addressed (CRITICAL): 0

## Mock Status

N/A — not a Frontend-First project (no `src/mocks/` directory).

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | USERTEST.md had no FIX-209 section | NON-BLOCKING | FIXED | Added 4 scenarios per task spec |
| 2 | GLOSSARY.md had no Alert, Alert State, Alert Source, or Unified Alerts Table terms | NON-BLOCKING | FIXED | Added 4 terms to SRE Operations Terms section |
| 3 | ARCHITECTURE.md file tree did not reference `api/alert/`, `store/alert.go`, `job/alerts_retention.go`; SVC-08 notification entry had no FIX-209 annotation | NON-BLOCKING | FIXED | Added tree entries + AlertsRetentionJob bullet under Backup Infrastructure |
| 4 | SCREENS.md SCR-183 (Alerts List) and SCR-010 (Dashboard) had no FIX-209 annotations | NON-BLOCKING | FIXED | Updated both rows with FIX-209 feature notes |
| 5 | decisions.md had no entries capturing FIX-209 architectural decisions | NON-BLOCKING | FIXED | Added DEV-270, DEV-271, DEV-272 |
| 6 | `.env.example` missing `ALERTS_RETENTION_DAYS` (Check #7 — env var documented in CONFIG.md but not in .env.example) | NON-BLOCKING | FIXED | Added `ALERTS_RETENTION_DAYS=180` with comment |
| 7 | CLAUDE.md story pointer still pointed to FIX-209 after DONE | NON-BLOCKING | FIXED | Advanced to FIX-210, Step set to Plan |

## Project Health

- Stories completed (UI Review Remediation track): FIX-201..209, FIX-211 = 10 stories DONE
- Current phase: UI Review Remediation [IN PROGRESS] — Wave 2
- Next story: FIX-210 (Alert Deduplication + State Machine)
- Blockers: None — FIX-209 unblocks FIX-210 (dedup_key + suppressed state reserved)
