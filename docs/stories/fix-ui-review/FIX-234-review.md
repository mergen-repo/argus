# Post-Story Review: FIX-234 — CoA Status Enum Extension + Idle SIM Handling + UI Counters

> Date: 2026-04-26

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-251 | P3 stale-toast on /sims — NO impact from FIX-234 (FE-only toast fix) | NO_CHANGE |
| FIX-252 | P2 SIM activate 500 — NO impact from FIX-234 (IP pool backend bug) | NO_CHANGE |
| FIX-237 | Phase 2 P0 event taxonomy — POTENTIAL: if taxonomy-redesign introduces new categorical state fields, the VARCHAR(20)+CHECK+Go-const pattern (DEV-378/379) is directly reusable | POTENTIAL |
| FIX-241 | Phase 2 P0 nil-slice safety — NO_CHANGE | NO_CHANGE |
| FIX-242 | Phase 2 P0 session detail extended DTO — POTENTIAL: D-145 (`coa_failure_reason`/`coa_sent_at` DTO extension) is a candidate to fold here if FIX-242 expands the SIM/session DTO surface | POTENTIAL |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| decisions.md | Added DEV-378..DEV-385 (8 entries) | UPDATED |
| bug-patterns.md | Added PAT-022 (VARCHAR+CHECK vs ENUM) | UPDATED |
| docs/architecture/db/policy.md | TBL-15: rewrote coa_status row description (6-state canonical set); added new partial index; migration reference | UPDATED |
| docs/architecture/db/_index.md | TBL-15 row: annotated FIX-234 constraint + 6-state set | UPDATED |
| docs/architecture/api/_index.md | API-099 + API-326 annotated with `coa_counts` optional field | UPDATED |
| docs/SCREENS.md | SCR-062 extended with 6-state breakdown; SCR-021 extended with CoA Status InfoRow | UPDATED |
| docs/GLOSSARY.md | Added "CoA Status" + "Idle SIM" terms | UPDATED |
| docs/USERTEST.md | Added FIX-234 section — 9 AC scenarios in Turkish | UPDATED |
| docs/ROUTEMAP.md | FIX-234 marked DONE; activity log row appended; D-144/D-145/D-146 added to Tech Debt | UPDATED |
| CLAUDE.md | Story → FIX-252, Step → Plan | UPDATED |
| docs/FRONTEND.md | No new state-token section needed | NO_CHANGE |
| docs/ARCHITECTURE.md | Backend-only story additions; no scale changes | NO_CHANGE |

## 14-Check Summary

| # | Check | Status | Notes |
|---|-------|--------|-------|
| 1 | Story spec accuracy | REPORT ONLY | Spec accurate; AC-6 partially fulfilled — DTO extension REJECTED by orchestrator, tooltip-only fallback. Deviation documented as DEV-384. |
| 2 | Plan ↔ implementation drift | PASS | (a) T6 DTO rejection logged DEV-384; (b) T4 N+1 accepted logged DEV-385+D-144; (c) T3b severity ordinal corrected 2→4 logged DEV-383; (d) T3 extractTenantAndSIM copied with renamed local; (e) Wave 1 split into 5 sub-waves due to internal dependency edges. All documented. |
| 3 | API index | UPDATED | API-099 + API-326 now carry `coa_counts` optional field annotation. |
| 4 | DB index + db/policy.md | UPDATED | TBL-15: stale "pending/sent/acked/failed" → canonical 6-state set; CHECK constraint + new partial index added; migration referenced. |
| 5 | Error codes | NO_CHANGE | No new error codes introduced. |
| 6 | SCREENS.md | UPDATED | SCR-062 + SCR-021 annotated. |
| 7 | FRONTEND.md | NO_CHANGE | No new state-token mapping section required; `text-warning` / `text-info` already documented in token table (FIX-228 entry). |
| 8 | GLOSSARY.md | UPDATED | "CoA Status" + "Idle SIM" added. |
| 9 | decisions.md | UPDATED | DEV-378..385 appended. |
| 10 | bug-patterns.md | UPDATED | PAT-022 added (VARCHAR+CHECK constraint vs native ENUM). |
| 11 | USERTEST.md | UPDATED | 9 scenario groups (9 ACs) in Turkish. |
| 12 | ROUTEMAP | UPDATED | FIX-234 DONE; activity log row; D-144/145/146 in Tech Debt. |
| 13 | CLAUDE.md | UPDATED | Story → FIX-252, Step → Plan. |
| 14 | Story Impact | REPORTED | 5 stories analyzed: 3 NO_CHANGE, 2 POTENTIAL. |

## Cross-Doc Consistency

- Contradictions found: 0
- TBL-15 stale 4-value enum description corrected to 6-state canonical set (DEV-379).

## Decision Tracing

- Decisions checked: DEV-378..385 (8 new entries)
- Orchestrator overrides: T6 DTO rejection + T4 N+1 acceptance + T3b severity ordinal correction — all honored in implementation (gate verified) and now logged.
- Orphaned approved decisions: 0

## USERTEST Completeness

- Entry exists: YES (9 scenarios appended)
- Type: UI + backend + DB + metrics scenarios

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 0 pre-existing
- New items added by gate: D-144, D-145, D-146 → added to ROUTEMAP Tech Debt

## Mock Status

- N/A (not a Frontend-First mock project)

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | TBL-15 `coa_status` description stale ("pending, sent, acked, failed") | NON-BLOCKING | FIXED | db/policy.md row updated to canonical 6-state set; DEV-379 logged |
| 2 | AC-6 DTO extension scope reduction | NON-BLOCKING | DEFERRED D-145 | T6 backend DTO extension REJECTED by orchestrator; static tooltip fallback in place; D-145 targets future session-detail extended DTO story |
| 3 | T4 list endpoint N+1 | NON-BLOCKING | DEFERRED D-144 | `GetCoAStatusCountsByRollout` called per-row in `ListRollouts` loop (handler.go:1528); acceptable at cap-50; batch alternative deferred to Phase 2 hardening |
| 4 | Live browser smoke not exercised in Gate | NON-BLOCKING | DEFERRED D-146 | Visual surfaces deferred to USERTEST manual verification; code-level PAT-018 zero + tsc PASS considered sufficient for autopilot PASS |

## Project Health

- Stories completed: UI Review Remediation track, FIX-234 done
- Current phase: UI Review Remediation [IN PROGRESS]
- Next story: FIX-252 (P2 sim activate 500 IP pool backend bug)
- Blockers: None
