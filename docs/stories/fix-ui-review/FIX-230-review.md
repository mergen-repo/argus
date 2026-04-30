# Post-Story Review: FIX-230 — Rollout DSL Match Integration

> Date: 2026-04-26

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-232 | Rollout UI Active State consumes `total_sims` from rollout DTO. Field name unchanged; value is now accurate (DSL-cohort count, not all-tenant count). Progress bar percentages and completion detection will be correct without any FE change. | NO_CHANGE |
| FIX-233 | SIM List Policy column + Rollout Cohort filter depends on FIX-230 + FIX-231. FIX-230 delivers accurate `total_sims` and DSL-filtered cohort in the rollout pipeline. FE reads the same rollout DTO field — no schema change. | NO_CHANGE |
| FIX-243 | Policy DSL realtime validate endpoint — D-139 deferred here targets FIX-243 for typed-wrapper `dsl.SQLPredicate` struct (defense-in-depth refactor). That story should add `SQLPredicate{sql string; args []any}` constructable only via `ToSQLPredicate`. | DEFERRED (D-139) |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/brainstorming/decisions.md` | Added DEV-354..DEV-356 (DSL→SQL whitelist contract, affected_sim_count caching + explicit-zero semantics, fail-closed corrupted-DSL behaviour) | UPDATED |
| `docs/architecture/DSL_GRAMMAR.md` | Added §"Predicate Execution (SQL Backend)" — whitelisted fields table, parameterization rules, empty-MATCH→TRUE, fail-closed contract, usage pattern | UPDATED |
| `docs/USERTEST.md` | Added `## FIX-230` section — backend note + 7 API-level scenarios (affected_sim_count, rollout total_sims, 1-of-7 migration, unknown field 422, SQL injection parametrization) | UPDATED |
| `docs/ROUTEMAP.md` | FIX-230 status updated `[~] IN PROGRESS (Dev)` → `[x] DONE (2026-04-26)`; changelog row added | UPDATED |
| `docs/GLOSSARY.md` | `SIM Fleet Filters` entry updated — FIX-230 replaces `buildFiltersFromMatch()` with `dsl.ToSQLPredicate`; `CountWithPredicate` + `SelectSIMsForStage` references added | UPDATED |
| `docs/ARCHITECTURE.md` | `dsl/` directory comment extended with `sql_predicate.go (ToSQLPredicate — FIX-230)` | UPDATED |
| `CLAUDE.md` | Story pointer advanced from FIX-230 to FIX-232; Step set to Plan | UPDATED |
| `docs/SCREENS.md` | N/A — backend-only story | NO_CHANGE |
| `docs/FRONTEND.md` | N/A — backend-only story | NO_CHANGE |
| `docs/FUTURE.md` | No new future opportunities identified | NO_CHANGE |
| `Makefile` | No new targets or services | NO_CHANGE |
| `.env.example` | No new env vars | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- GLOSSARY `SIM Fleet Filters` was still referencing the old `buildFiltersFromMatch()` path — corrected to reflect FIX-230's canonical `dsl.ToSQLPredicate` path.
- ARCHITECTURE.md `dsl/` directory had no mention of the SQL predicate backend — annotated.

## Decision Tracing

- Decisions checked: 3 (DEV-354..DEV-356 — planner-numbered, not yet written to decisions.md pre-review)
- Orphaned (approved but not applied): 0
  - DEV-354 (whitelist contract): `internal/policy/dsl/sql_predicate.go` implements the 5-field whitelist + unknown-field error + $N-only values. VERIFIED.
  - DEV-355 (affected_sim_count caching + explicit-zero): `internal/policy/rollout/service.go` checks `AffectedSIMCount == nil` as cache-miss trigger (F-A1 fix); handler writes `meta.warnings: ["affected_sim_count_pending"]` on transient count failure (F-A2 fix). VERIFIED.
  - DEV-356 (fail-closed corrupted DSL): `compiledMatchFromVersion` inspects both `err` and `errs` diagnostics; any error-severity diagnostic → returns non-nil error (F-A6 fix). VERIFIED.

## USERTEST Completeness

- Entry exists: YES (added this review cycle)
- Type: Backend note + API-level scenario (7 steps covering all critical ACs)

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 0 (plan confirmed: "No tech debt items target FIX-230 specifically")
- Already ✓ RESOLVED by Gate: 0
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0

## Mock Status

- N/A — backend-only story; `src/mocks/` not applicable.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | DEV-354..DEV-356 missing from decisions.md — planner-numbered in step-log but not written | NON-BLOCKING | FIXED | Added 3 rows to decisions.md immediately after DEV-353 |
| 2 | DSL_GRAMMAR.md had no documentation of the SQL predicate execution contract | NON-BLOCKING | FIXED | Added §"Predicate Execution (SQL Backend)" section (~35 lines) covering whitelist, rules, fail-closed, usage pattern |
| 3 | FIX-230 absent from USERTEST.md | NON-BLOCKING | FIXED | Added backend note + 7 API-level test scenarios |
| 4 | GLOSSARY.md SIM Fleet Filters entry referenced obsolete `buildFiltersFromMatch()` — still described old in-memory filter builder, not the new SQL predicate path | NON-BLOCKING | FIXED | Updated entry to reference `dsl.ToSQLPredicate`, `CountWithPredicate`, `SelectSIMsForStage` |
| 5 | D-139 ROUTEMAP row status — gate confirmed deferred to FIX-243; ROUTEMAP row correctly shows OPEN | NON-BLOCKING | DEFERRED D-139 | Tech debt registry correctly shows OPEN; review.md cross-reference uses DEFERRED per orchestrator convention |

## Project Health

- Stories completed: FIX-201..FIX-215, FIX-217..FIX-231 (approx. 28/44 UI Review stories = ~64%)
- Current phase: UI Review Remediation — Wave 2.5+3
- Next story: FIX-232 (Rollout UI Active State)
- Blockers: None — FIX-232 dependencies (FIX-212 + FIX-230) both DONE
