# Post-Story Review: FIX-204 — Analytics group_by NULL Scan Bug + APN Orphan Sessions

> Date: 2026-04-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-207 | Orphan session detector in `internal/job/orphan_session.go` (FIX-204) and FIX-207 "Session/CDR Data Integrity" both target active sessions with missing FK references. FIX-207 planner should evaluate absorbing or extending the detector (e.g., add negative-duration and cross-pool-IP checks to the same job) to avoid a second independent sweep goroutine. | REPORT ONLY — FIX-207 planner to decide at plan phase |
| FIX-208 | `GetBreakdowns` sentinel harmonized from `'unknown'` → `'__unassigned__'`. FIX-208 "Cross-Tab Data Aggregation Unify" consumes the same breakdowns API surface — the single consistent sentinel simplifies the unification. No contract change needed. | NO_CHANGE |
| FIX-220 | `group_by=operator_id` alias (accepted by `validGroupBy`) passes `"operator_id"` to `resolveGroupKeyName`, whose `__unassigned__` switch does not have an `"operator_id"` case → falls through to `"Unassigned"` instead of `"Unknown Operator"`. Gate accepted as non-blocking pre-existing UX imperfection. Deferred to FIX-220 (Analytics Polish). | REPORT ONLY — defer to D-061 (see Tech Debt below) |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/architecture/CONFIG.md` | Added `ORPHAN_SESSION_CHECK_INTERVAL` row to Background Jobs table | UPDATED |
| `docs/brainstorming/decisions.md` | Added DEV-259 (sentinel harmonization + orphan detector wiring) | UPDATED |
| `docs/brainstorming/bug-patterns.md` | Added PAT-009 (nullable FK columns in analytics aggregations) | UPDATED |
| `docs/USERTEST.md` | Added `## FIX-204` section (4 manual test scenarios) | UPDATED |
| `docs/ROUTEMAP.md` | No changes (FIX-204 row already updated to IN PROGRESS / Review by gate) | NO_CHANGE |
| ARCHITECTURE | No structural changes — analytics store and job packages unchanged in signature | NO_CHANGE |
| SCREENS | No changes — analytics page shape unchanged (sentinel is a data value) | NO_CHANGE |
| FRONTEND | No changes — `resolveGroupLabel` is consistent with existing design conventions | NO_CHANGE |
| FUTURE | No changes | NO_CHANGE |
| Makefile | No changes | NO_CHANGE |
| CLAUDE.md | No changes | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- `ORPHAN_SESSION_CHECK_INTERVAL` gap closed — CONFIG.md now matches implementation.
- Old `'unknown'` sentinel no longer appears in `usage_analytics.go` (gate grep confirmed). Other files (`cost_analytics.go:180`, `sim.go:1130`, `search/handler.go:225`) use `'unknown'` in unrelated query branches (cost rat breakdown, SIM RAT stats, operator health status) — these are OUT of FIX-204 scope and do not share the analytics/usage response surface.
- FE `resolveGroupLabel` function is a pure helper — no design-token violations, no hardcoded hex.

## Decision Tracing

- Decisions checked: DEV-259 (new, FIX-204), plus PAT-009 (new, FIX-204). No prior-cycle decisions targeted FIX-204.
- Plan sentinel-harmonization decision (`'unknown'` → `'__unassigned__'`): applied in `GetBreakdowns` line 268 — PASS.
- Plan rat_type-handler-bypass decision (FE-only translation for rat_type): applied at handler line 304 guard + FE `resolveGroupLabel` — PASS.
- Orphaned (approved but not applied): 0.

## USERTEST Completeness

- Entry exists: YES (added in this review cycle)
- Type: UI scenarios (4 scenarios: group_by=apn/operator/rat_type, orphan detector logs)

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting FIX-204 in ROUTEMAP Tech Debt: 0 (plan documented no pre-existing tech debt targets).
- New item identified by this review:

| ID | Description | Target | Status |
|----|-------------|--------|--------|
| D-061 | `validGroupBy` alias forms (`operator_id`, `apn_id`) pass through to `resolveGroupKeyName` without matching the `__unassigned__` sentinel case for alias keys — returns generic "Unassigned" instead of "Unknown Operator". Pre-existing UX imperfection, not a regression from FIX-204. | FIX-220 | OPEN |

## Mock Status

Not applicable — this is a backend bug fix + FE label translation. No mock files involved.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | `ORPHAN_SESSION_CHECK_INTERVAL` env var not documented in `docs/architecture/CONFIG.md` | NON-BLOCKING | FIXED | Added to Background Jobs table in CONFIG.md (between `JOB_TIMEOUT_CHECK_INTERVAL` and `JOB_LOCK_TTL`). |
| 2 | `validGroupBy` alias keys (`operator_id`, `apn_id`) produce wrong sentinel label in `resolveGroupKeyName` (falls to "Unassigned" instead of "Unknown Operator") | NON-BLOCKING | DEFERRED D-061 | Pre-existing alias acceptance; the switch only covers canonical forms ("operator", "apn"). Deferred to FIX-220 (Analytics Polish). |
| 3 | `'unknown'` sentinel remains in `cost_analytics.go`, `sim.go`, `search/handler.go` | NON-BLOCKING | NO_ACTION | Confirmed out of FIX-204 scope — these are unrelated query branches not consumed by the `GET /analytics/usage` response surface. Gate correctly scoped the sweep to `usage_analytics.go`. |

## Project Health

- Stories completed in UI Review track: FIX-201, FIX-202, FIX-203, FIX-204 (4 done, Wave 1 ongoing)
- Current phase: UI Review Remediation — Wave 1
- Next story: FIX-205 (Token Refresh Auto-retry on 401)
- Blockers: None — FIX-204 PASS, 3338/3338 tests pass, typecheck clean
