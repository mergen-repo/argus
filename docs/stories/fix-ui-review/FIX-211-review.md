# Post-Story Review: FIX-211 — Severity Taxonomy Unification

> Date: 2026-04-21

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-209 (Unified alerts table) | Must adopt canonical 5-value CHECK constraint `chk_alerts_severity CHECK (severity IN ('critical','high','medium','low','info'))`. ERROR_CODES.md §Severity Taxonomy already documents this requirement verbatim. Dependency is correctly listed in ROUTEMAP. | NO_CHANGE (already documented in ERROR_CODES.md §Cross-reference for FIX-209) |
| FIX-210 (Alert dedup + state machine) | No severity schema work — operates on the alerts table that FIX-209 creates. Dedup logic will consume canonical severity values. No updates needed. | NO_CHANGE |
| FIX-213 (Live Event Stream UX) | Story spec already references "severity (5-level)" in AC-1 and AC-2. The `<SeverityBadge>` shared component and `SEVERITY_FILTER_OPTIONS` from `web/src/lib/severity.ts` are available for adoption. No story-file edits needed. | NO_CHANGE |
| FIX-215 (SLA Historical Reports) | No severity dependency identified in story spec. | NO_CHANGE |
| FIX-229 (Alert Enhancements) | No severity dependency identified in story spec. Depends on FIX-209 which depends on FIX-211 — indirect ordering correct. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/USERTEST.md` | Added `## FIX-211` section with 2 scenarios: (1) alerts severity filter + 400 rejection; (2) notification preferences 5-level threshold | UPDATED |
| `docs/GLOSSARY.md` | Added `Severity Level` term to "Argus Platform Terms" section (5-value enum, ordinals, Go+TS source refs, FIX-211 attribution) | UPDATED |
| `docs/GLOSSARY.md` | Fixed `Pool Utilization` entry: "warning (80%)" → "medium (80%)" to align with canonical taxonomy | UPDATED |
| `docs/PRODUCT.md` | Fixed line 238 "warning alert" → "medium-severity alert" to align with canonical taxonomy | UPDATED |
| `docs/architecture/ERROR_CODES.md` | Already updated by Task 7 in story; `INVALID_SEVERITY` row in Validation Errors table + `## Severity Taxonomy` section complete and accurate | NO_CHANGE (verified) |
| `docs/FRONTEND.md` | No severity colour section exists; canonical token map lives in ERROR_CODES.md §Severity Taxonomy — sufficient. No addition needed. | NO_CHANGE |
| `docs/SCREENS.md` | SCR-169 (Incidents) and SCR-183 (Alerts) reference severity generically; no bucket-count claims to update. | NO_CHANGE |
| `docs/ROUTEMAP.md` | FIX-209 dependency on FIX-211 accurate (line 364). FIX-211 status still `[~] IN PROGRESS (Dev)` — will be flipped to `[x] DONE` in the Commit step. | NO_CHANGE (Commit step handles) |
| `docs/brainstorming/decisions.md` | No DEV-NNN decisions pre-tagged to FIX-211 found (plan used no pre-committed DEV IDs). No new implicit decisions to capture — the HARD validation decision was already documented in the plan and ERROR_CODES.md. | NO_CHANGE |
| `Makefile` | No new services, scripts, or targets added. | NO_CHANGE |
| `CLAUDE.md` | No Docker URL/port changes. | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 2 (both fixed)
  1. `docs/PRODUCT.md:238` — "warning alert" used old-taxonomy label; corrected to "medium-severity alert"
  2. `docs/GLOSSARY.md:147` — Pool Utilization entry used "warning (80%)"; corrected to "medium (80%)"
- `docs/GLOSSARY.md` was missing the "Severity Level" domain term introduced by FIX-211 — added.
- PAT-006 (missed construction site) verified clean: `rg '"warning"|"error"' internal/ --type go | grep -iE 'severity'` returns only DSL parser hits (`internal/policy/dsl/parser.go`, `internal/api/policy/handler.go`, `internal/policy/dryrun/service.go`) which are in-scope exclusions (DSL parse-error severity, not event severity).

## Decision Tracing

- Decisions checked: FIX-211 plan carried no pre-assigned DEV-NNN IDs for story-specific decisions (unlike FIX-205/206/207/208 which pre-committed DEV IDs)
- Orphaned (approved but not applied): 0
- The one major implicit decision (HARD validation, no toggle) is documented in `docs/architecture/ERROR_CODES.md §Validation policy` — no DEV entry required.

## USERTEST Completeness

- Entry exists: YES (written in this review)
- Type: UI scenarios + curl API scenarios covering AC-4 (400 rejection), AC-5 (5-level UI), AC-8 (threshold suppression)

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 0 (confirmed by plan: "Consulted docs/ROUTEMAP.md Tech Debt table: no OPEN items are targeted at FIX-211")
- Already ✓ RESOLVED by Gate: 0 (none targeted)
- Resolved by Reviewer: 0
- NOT addressed (CRITICAL): 0

## Mock Status

- N/A — no `src/mocks/` directory; Argus is backend-first.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | `docs/PRODUCT.md:238` used "warning alert" (old-taxonomy label) | NON-BLOCKING | FIXED | Updated to "medium-severity alert" to align with canonical severity taxonomy established by FIX-211 |
| 2 | `docs/GLOSSARY.md` Pool Utilization entry (line 147) used "warning (80%)" | NON-BLOCKING | FIXED | Updated to "medium (80%)" to align with canonical taxonomy |
| 3 | `docs/GLOSSARY.md` missing "Severity Level" domain term | NON-BLOCKING | FIXED | Added full entry to "Argus Platform Terms" section with 5-value enum, ordinals, Go/TS source refs, FIX-211 attribution |
| 4 | `docs/USERTEST.md` had no FIX-211 section | NON-BLOCKING | FIXED | Written in this review cycle per reviewer-prompt §12 — 2 scenarios (filter/validation + notification preferences 5-level) |

## Project Health

- Stories completed: Wave 1 (FIX-201..207) 6/6 DONE; Wave 2 FIX-208 DONE; FIX-211 DONE (pending Commit step ROUTEMAP update); FIX-209/210 PENDING
- Current phase: UI Review Remediation — Wave 2 (Alert Architecture)
- Next story: FIX-209 (Unified alerts table) — now unblocked; FIX-211 dependency satisfied
- Blockers: None. FIX-210 has no listed dependencies (can run in parallel with FIX-209).
