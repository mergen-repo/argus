# Post-Story Review: STORY-098 — Native Syslog Forwarder (RFC 3164/5424)

> Date: 2026-05-05

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| Phase 11 Phase Gate | Final dev story complete (6/6). Phase Gate is the next action — no upcoming dev stories in Phase 11. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| ARCHITECTURE.md | Fixed audit-action name `destination_removed` → `destination_deleted`; added missing 5th action `delivery_failed`; bumped TBL count 60→61; bumped API count 269→279; corrected endpoint range to API-001..348; updated Log Forwarding endpoint count to "API-337..341" | UPDATED |
| GLOSSARY.md | Fixed TBL-59 → TBL-61 in "Syslog" + "Syslog Destination" Context columns; rewrote "Syslog Destination" definition (removed wrong `tls_config JSONB`, documented 3 separate PEM text columns + `filter_categories`, `severity_floor`, etc.); rewrote "Syslog Filter Rule" (was wrong TBL-60 — TBL-60 is `sim_imei_allowlist`; rewritten to describe column-level filtering on TBL-61) | UPDATED |
| docs/architecture/api/_index.md | Added API-346 (Test-connection), API-347 (SetEnabled), API-348 (Delete) — 3 endpoints missing from Phase 11 architect dispatch pre-registration; corrected section header "2 endpoints" → "5 endpoints"; removed stale "DELETE variant out of v1 scope" note from API-338; updated Total 276→279; added changelog entry | UPDATED |
| decisions.md | VAL-068..075 (Dev) + VAL-076..078 (Gate) already present | NO_CHANGE |
| SCREENS.md | SCR-198 already registered | NO_CHANGE |
| FRONTEND.md | No changes | NO_CHANGE |
| FUTURE.md | No new syslog extension point needed (D-197 NATS-refresh is tech debt, not a FUTURE roadmap item) | NO_CHANGE |
| Makefile | No new targets added by STORY-098 | NO_CHANGE |
| CLAUDE.md | Step = "Review" (correct for current state; orchestrator updates after Commit) | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 4 (all fixed — see Issues table)
  1. ARCHITECTURE.md audit-action list used `destination_removed`; code/catalog/USERTEST use `destination_deleted`
  2. ARCHITECTURE.md audit-action list missing `delivery_failed` (5th action added by Gate F-A1)
  3. GLOSSARY.md "Syslog Destination" entry referenced `tls_config JSONB` (pre-plan placeholder); actual schema has 3 separate text columns
  4. GLOSSARY.md "Syslog Filter Rule" referenced TBL-60 — TBL-60 is `sim_imei_allowlist`, not a filter-rules table
- API index gap: 3 shipped endpoints (Test, SetEnabled, Delete) unregistered in api/_index.md — registered as API-346..348

## Decision Tracing

- Decisions checked: VAL-068..075 (Dev, 8 entries) + VAL-076..078 (Gate, 3 entries) = 11 STORY-098 decisions
- All present in decisions.md with correct ACCEPTED status
- All reflected in implementation (code verified against Gate finding disposition table)
- Orphaned (approved but not applied): 0

## USERTEST Completeness

- Entry exists: YES — `## STORY-098:` section at line 6234
- Type: 12 UI + API + wire-format scenarios in Turkish (UT-098-01 through UT-098-12)
- Status: PASS

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 0 (no prior tech-debt row had Target = STORY-098)
- Items created BY this story: D-195 (IANA PEN), D-196 (KMS encrypt tls_client_key_pem), D-197 (NATS-event-driven destination refresh) — all OPEN with correct future targets
- Already ✓ RESOLVED by Gate: N/A
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0

## Mock Status

- N/A — Argus is a full-stack live project, no `src/mocks/` directory

## Issues

> Every issue MUST have a Resolution. NEVER write an issue without one.

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | ARCHITECTURE.md audit-action `log_forwarding.destination_removed` — code, catalog, and USERTEST all use `destination_deleted` | NON-BLOCKING | FIXED | Line 580 corrected; action name now matches `internal/api/events/catalog.go:519` and `internal/api/settings/log_forwarding.go:637` |
| 2 | ARCHITECTURE.md missing 5th audit action `log_forwarding.delivery_failed` — added to catalog by Gate F-A1 | NON-BLOCKING | FIXED | Line 580 extended; 5 actions now listed |
| 3 | ARCHITECTURE.md Reference ID Registry: `TBL-NN | 60 | TBL-01 to TBL-60` — STORY-098 introduced TBL-61 (`syslog_destinations`) | NON-BLOCKING | FIXED | Row bumped to 61 / TBL-01..61; API count updated 269→279 / range to API-001..348 |
| 4 | GLOSSARY.md "Syslog" + "Syslog Destination" context column shows `TBL-59` — TBL-59 is `imei_history`; `syslog_destinations` is TBL-61 | NON-BLOCKING | FIXED | Both Context columns corrected to TBL-61; db/_index.md already correctly registered TBL-61 |
| 5 | GLOSSARY.md "Syslog Destination" definition references `tls_config JSONB` — pre-plan placeholder; actual TBL-61 schema has 3 separate text columns (`tls_ca_pem`, `tls_client_cert_pem`, `tls_client_key_pem`) | NON-BLOCKING | FIXED | Definition rewritten to match migration schema |
| 6 | GLOSSARY.md "Syslog Filter Rule" references `TBL-60` — TBL-60 is `sim_imei_allowlist` (STORY-094/095). STORY-098 has no separate filter-rules table; filtering is column-level on TBL-61 | NON-BLOCKING | FIXED | Entry rewritten to describe `filter_categories TEXT[]` + `filter_min_severity` + `severity_floor` columns on TBL-61 |
| 7 | api/_index.md Log Forwarding section registered only 2 endpoints (API-337/338); STORY-098 shipped 5: also Test (`POST /test`), SetEnabled (`POST /{id}/enabled`), Delete (`DELETE /{id}`) | NON-BLOCKING | FIXED | API-346, API-347, API-348 added; section header corrected to "5 endpoints"; Total 276→279; changelog entry appended |

## Project Health

- Stories completed: 98/98 Dev stories in scope through Phase 11 (6/6 Phase 11 stories DONE)
- Current phase: Phase 11 — Enterprise Readiness Pack (final dev story closed)
- Next action: Phase 11 Phase Gate
- Blockers: None
