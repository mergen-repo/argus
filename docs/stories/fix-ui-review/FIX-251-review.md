# Post-Story Review: FIX-251 — Stale "An unexpected error occurred" toasts on /sims

> **RETROACTIVE REVIEW — filed 2026-04-30 to backfill missing artifact from 2026-04-26 closure (commit 211f0cc).**
> The original lite-review (step-log STEP_4) covered: bug-patterns.md (PAT-006 RECURRENCE #3), decisions.md (DEV-389), USERTEST.md (4 scenarios), ROUTEMAP (FIX-251 DONE + activity log), CLAUDE.md update. This retroactive pass verifies all those still in place and runs the remaining 14-check protocol.

> Date: 2026-04-30

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-253 | NO_CHANGE — already closed 2026-04-26; T2 signature change adopted PAT-006 RECURRENCE defensively (cross-ref logged in FIX-253 commit). | NO_CHANGE |
| FIX-228 / FIX-229 / FIX-230..234 | NO_CHANGE — distinct scope (auth, alerts export, etc.). | NO_CHANGE |
| All Wave 9 stories (FIX-243/244/239/236/248) | NO_CHANGE — already closed; PAT-006 discipline already internalised in those works. | NO_CHANGE |
| Future store-table stories | POTENTIAL pickup — D-181 systemic audit may surface drift in `sim.go`/`policy.go`/`cdr.go`/`ippool.go` etc. before next column-add ships. | INFORMATIONAL |

## Documents Updated (verified in place)

| Document | Change | Status |
|----------|--------|--------|
| `docs/brainstorming/bug-patterns.md` | PAT-006 RECURRENCE #3 entry (line 31, 2026-04-26) cross-ref to FIX-201 + FIX-215 | VERIFIED |
| `docs/brainstorming/decisions.md` | DEV-389 (line 642) — silentPaths-rejected rationale + backend-fix decision, ACCEPTED | VERIFIED |
| `docs/USERTEST.md` | FIX-251 section (line 4741) — 4 Turkish scenarios with backend-fix narrative | VERIFIED |
| `docs/ROUTEMAP.md` | FIX-251 marked DONE (line 423) + activity log row 2026-04-26 (line 512) | VERIFIED |
| `CLAUDE.md` | FIX-251 listed in Prior closures (lines 133, 134) | VERIFIED |
| `docs/stories/fix-ui-review/FIX-251-gate.md` | RETROACTIVE GATE (this review cycle) | NEW |
| `docs/stories/fix-ui-review/FIX-251-review.md` | RETROACTIVE REVIEW (this file) | NEW |
| `docs/ROUTEMAP.md → ## Tech Debt` | D-181 added (systemic inline-scan-vs-helper audit) | UPDATED |

## 14-Check Protocol

| # | Check | Status | Detail |
|---|-------|--------|--------|
| 1 | Next-story impact | PASS | All next stories closed independently 2026-04-26..27; no upstream changes from FIX-251. |
| 2 | Architecture evolution | PASS | No architecture change — fix preserves existing `scanOperator` helper pattern. ARCHITECTURE.md unchanged. |
| 3 | New domain terms | PASS | No new terms. GLOSSARY.md unchanged. |
| 4 | Screen updates | PASS | No screen change — BE fix only. SCREENS.md unchanged. |
| 5 | FUTURE.md relevance | PASS | No new future opportunities; existing roadmap unaffected. |
| 6 | New decisions | PASS | DEV-389 logged 2026-04-26 (verified line 642 of decisions.md). |
| 7 | Makefile / .env consistency | PASS | No new services, scripts, env vars. Makefile unchanged. |
| 8 | CLAUDE.md consistency | PASS | No Docker port/URL change. CLAUDE.md FIX-251 closure note in place. |
| 9 | Cross-doc consistency | PASS | DEV-389 narrative in decisions.md aligns with PAT-006 #3 in bug-patterns.md aligns with FIX-251 ROUTEMAP row aligns with commit 211f0cc message. No contradictions. |
| 10 | Story updates report-only | PASS | No upcoming story spec needs updating. FIX-253 already absorbed defensive pattern in its own scope. |
| 11 | Decision tracing | PASS | DEV-389 ACCEPTED → applied: (a) silentPaths NOT extended (`web/src/lib/api.ts:93` unchanged); (b) inline scans patched (operator.go:341-349 + 470-478 with `&o.SLALatencyThresholdMs`); (c) PAT-006 RECURRENCE #3 logged in bug-patterns. All three apply. |
| 12 | USERTEST completeness | PASS | `docs/USERTEST.md` § FIX-251 (line 4741) has 4 Turkish backend-fix scenarios; story is BE-only so no UI scenarios required (note in italic explains plan pivot). |
| 13 | Tech Debt pickup | PASS | No D-NNN targets FIX-251 in ROUTEMAP. New D-181 ADDED this review cycle (systemic audit, future test-hardening). |
| 14 | Mock sweep | N/A | No `src/mocks/` in this project (verified by `ls web/src/mocks/ 2>/dev/null` → not found). N/A. |

## Cross-Doc Consistency

- Contradictions found: 0
- DEV-389 narrative ↔ commit message ↔ PAT-006 RECURRENCE #3 entry ↔ FIX-251 ROUTEMAP row ↔ CLAUDE.md prior-closures line: all consistent.

## Decision Tracing

- Decisions checked: 1 (DEV-389)
- Orphaned (approved but not applied): 0
- DEV-389 status: ACCEPTED → 100% applied (silentPaths rejected, BE fix landed, regression tests added).

## USERTEST Completeness

- Entry exists: YES (line 4741)
- Type: 4 backend-fix Turkish scenarios + plan-pivot note (italic) explaining FE→BE redirect

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story before review: 0
- Already ✓ RESOLVED by Gate: 0
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0
- New deferred this review: **D-181** (systemic inline-scan-vs-helper audit across 18 store files — non-blocking; routed to future test-hardening / refactor story)

## Mock Status

- N/A — no `src/mocks/` in this Frontend-First repo (project is BE-driven; FE consumes real API)

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | FIX-251-gate.md and FIX-251-review.md missing from 2026-04-26 closure | NON-BLOCKING | FIXED | Created retroactively 2026-04-30 (this artifact + companion gate). Step-log appended with STEP_3 GATE + STEP_4 REVIEW retroactive entries. |
| 2 | Systemic inline-scan-vs-helper drift risk in 5+ other store files (`sim.go`, `policy.go`, `cdr.go`, `ippool.go`, `session_radius.go`, `notification.go`) — same shape as PAT-006 #3 root cause | LOW | DEFERRED → D-181 | Routed to ROUTEMAP Tech Debt; future test-hardening or refactor story should add `TestColumnsAndScanCountConsistency` per table OR refactor inline list scans to delegate to the `scanXxx` helper. Non-blocking — FIX-251 spec scope was the operator-table immediate fix, not a refactor. |

## Project Health

- Stories completed at 2026-04-30: as per CLAUDE.md — Wave 9 P1 5/5 DONE; FIX-251 closed 2026-04-26.
- Current phase: UI Review Remediation IN PROGRESS — Wave 10 P2 PENDING (per CLAUDE.md "Story: — (Wave 9 5/5 DONE — UI Review Remediation track Wave 10 PENDING")
- Next story: per ROUTEMAP — Wave 10 P2 batch
- Blockers: None (D-181 is informational, not a blocker)
