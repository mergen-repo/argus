# Post-Story Review: STORY-097 — IMEI Change Detection & Re-pair Workflow

> Date: 2026-05-04

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-098 | Native Syslog Forwarder (RFC 3164/5424) — independent track; no shared code, tables, or events with STORY-097. No change. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/brainstorming/bug-patterns.md` | Added PAT-026 RECURRENCE [STORY-097 Gate F-A2]: 8-layer catalog sweep annotation for new notification subjects caught at Gate pre-merge | UPDATED |
| `docs/GLOSSARY.md` | Added 3 new terms: Grace Countdown, Severity Scaling (Binding), Force Re-verify (deferred) | UPDATED |
| `docs/USERTEST.md` | Added UT-097-13..16 covering post-Gate fixes F-A1 / F-A2 / F-A3 / F-A4 | UPDATED |
| `docs/SCREENS.md` | Updated SCR-021f description: 4 shipped panels enumerated, D-194 deferred surfaces noted | UPDATED |
| `docs/ROUTEMAP.md` | STORY-097 row: `[~] IN PROGRESS` → `[x] DONE`, Step `Plan` → `—`, Completed `—` → `2026-05-04`; Last updated bumped to 5/6 stories | UPDATED |
| `docs/brainstorming/decisions.md` | VAL-059..067 all present; no additions needed | NO_CHANGE |
| `docs/architecture/api/_index.md` | API-329 already registered; no change needed | NO_CHANGE |
| `docs/architecture/db/_index.md` | No new tables; no change needed | NO_CHANGE |
| `docs/architecture/CONFIG.md` | `ARGUS_BINDING_GRACE_WINDOW` already present at line 211 | NO_CHANGE |
| `docs/architecture/ERROR_CODES.md` | No new error codes; SIM_NOT_FOUND + INSUFFICIENT_ROLE are pre-existing | NO_CHANGE |
| `docs/architecture/DSL_GRAMMAR.md` | `device.imei` + `device.binding_status` unchanged by this story; STORY-094/095/096 wired them | NO_CHANGE |
| `docs/ARCHITECTURE.md` | No architectural changes introduced by STORY-097 | NO_CHANGE |
| `docs/FUTURE.md` | No new future opportunities surfaced | NO_CHANGE |
| `Makefile` | No new services or targets added | NO_CHANGE |
| `CLAUDE.md` | No port/URL changes | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- API-329 registered in `docs/architecture/api/_index.md` at line 589; endpoint matches plan spec.
- ADR-004 binding modes: FE `BindingMode` union corrected at Gate F-A3 to 7 canonical values; matches `Binding Mode` GLOSSARY entry and SCR-021f dropdown spec.
- AC-5 wording amended at Gate F-A5 to reconcile VAL-066; spec now matches code truth.

## Decision Tracing

- Decisions checked: VAL-059, VAL-060, VAL-061, VAL-062, VAL-063, VAL-064, VAL-065, VAL-066, VAL-067
- All 9 found in `docs/brainstorming/decisions.md` lines 323–331
- Orphaned (approved but not applied): 0
- VAL-067 (re-pair reason radio deferred): correctly captured; AC-3 confirmed not to require it; aligned with Gate report disposition

## USERTEST Completeness

- Entry exists: YES — `## STORY-097:` at line 6106
- Type: UI + backend mixed scenarios (12 pre-Gate + 4 post-Gate)
- Pre-Gate coverage (UT-097-01..12): Re-pair UI, idempotency, RBAC, grace badge, grace scanner dedup, IMEI history pagination, protocol filter, date filter, NULL mode history, severity scaling, lookup (D-188), PEIRaw (D-183)
- Post-Gate coverage added (UT-097-13..16):
  - UT-097-13: F-A1 — `imei.changed` subject verified in DB notifications table for mismatch events (incl. soft mode + blacklist negative case)
  - UT-097-14: F-A2 — `/api/v1/events/catalog` returns all 3 new subjects with correct severity and source
  - UT-097-15: F-A3 — `BindingMode` label rendering verified for all 7 values + unknown fallback
  - UT-097-16: F-A4 — re-pair notification payload verified to contain `iccid`, `actor_user_id`, `previous_bound_imei`; `severity` field absent

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting STORY-097: D-183, D-188, D-193, D-194
- D-183 ✓ RESOLVED by Gate: `SessionContext.PEIRaw` + `ExtractPEIRaw` sibling helper — code verified in `internal/aaa/sba/`
- D-188 ✓ RESOLVED by Gate: API-335 Lookup wire — `data.bound_sims` + `data.history` populated; USERTEST updated at UT-097-11
- D-193 NEW (tech debt added): tenant-scoped grace window infrastructure — PENDING, targeting follow-up story
- D-194 NEW (tech debt added): SCR-021f allowlist sub-table + Force Re-verify button — PENDING, targeting STORY-094/095 follow-up
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0

## Mock Status

- No `src/mocks/` directory; this project does not use mock-file-based API interception for STORY-097 endpoints
- N/A

## PAT-026 RECURRENCE Annotation

- Entry added: PAT-026 RECURRENCE [STORY-097 Gate F-A2] — 8-layer event catalog sweep for new notification subjects; 3 subjects registered in `catalog.go` + `tiers.go` + `publisherSourceMap`; caught by Gate scout pre-merge (positive outcome vs FIX-238 caught at runtime)
- Location: `docs/brainstorming/bug-patterns.md` line 44 (inserted before existing STORY-095 RECURRENCE entry)

## Issues

No issues found.

## Build Verification (Reviewer)

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go vet ./...` | PASS (0 issues) |
| `go test -count=1 ./...` | PASS — 4129 tests, 111 packages |
| `npx tsc --noEmit` | PASS — 0 type errors |
| `npm run build` | PASS — built in 3.07s |

## Project Health

- Stories completed: 5/6 (83%) — Phase 11 Enterprise Readiness Pack
- Current phase: Phase 11
- Next story: STORY-098 — Native Syslog Forwarder (RFC 3164/5424)
- Blockers: None
