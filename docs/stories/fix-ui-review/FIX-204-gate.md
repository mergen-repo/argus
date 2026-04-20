# Gate Report: FIX-204 — Analytics group_by NULL Scan Bug + APN Orphan Sessions

## Summary
- Requirements Tracing: ACs 8/8 addressed (AC-1 SQL COALESCE, AC-2 handler sentinel, AC-3 all three group columns covered, AC-4 scan via COALESCE, AC-5 detector warning-only, AC-6 FE label, AC-7 pure-Go SQL assertion, AC-8 planner-cheap pass-through accepted)
- Gap Analysis: 8/8 acceptance criteria PASS
- Compliance: COMPLIANT
- Tests: 3338/3338 PASS across 102 packages (up from 3326 baseline → +12 new tests from Gate fixes)
- Test Coverage: AC-7 now has direct pure-Go SQL assembly regression (`TestBuildTimeSeriesQuery_COALESCESentinel` + 3 subtests), AC-2 has handler sentinel subtests (3), AC-5 has detector behavior tests (4) + env-override tests (6)
- Performance: no new perf issues; COALESCE pass-through on non-null rows, planner-cheap (AC-8 accepted as inspectable guarantee — no 1M-row harness per plan)
- Build: PASS (Go build + vet) ; TS typecheck PASS
- Overall: **PASS**

## Team Composition
- Analysis Scout (inline): 1 observation (F-A5 operator_id alias edge case — pre-existing, scope-consistent, not a regression)
- Test/Build Scout (inline): 0 findings (build + vet + full suite + race all PASS)
- UI Scout (inline): 0 findings (no new hex/console/arbitrary-px introduced; TS typecheck clean)
- De-duplicated: 1 → 1 finding (accepted as non-blocking; see Passed Items)

Note: Per lead-prompt.md, subagents cannot nest-dispatch Task calls. Scout work was performed inline by the Team Lead against the three scout charters (Analysis / Test-Build / UI). The single-writer invariant still holds (only Gate Lead wrote source files in this phase).

## Fixes Applied
| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Config/Plan-adherence | `internal/job/orphan_session.go` | Added `resolveOrphanSessionInterval()` reading `ORPHAN_SESSION_CHECK_INTERVAL` env var (Go duration) with 30m default; parses, rejects non-positive and unparseable values | 6 env-override subtests PASS + orig 4 detector tests PASS w/ -race |
| 2 | Test coverage (AC-7) | `internal/store/usage_analytics.go` | Extracted pure `buildTimeSeriesQuery(p) (string, []interface{})` helper from `GetTimeSeries` — no behavior change, enables unit-test of SQL assembly without live DB | Full suite PASS (3338) |
| 3 | Test coverage (AC-7) | `internal/store/usage_analytics_test.go` | Added `TestBuildTimeSeriesQuery_COALESCESentinel` (3 subtests: apn/operator/rat_type asserting `COALESCE(col::text, '__unassigned__')` substring + legacy `'unknown'` absence) and `TestBuildTimeSeriesQuery_NoGroupByOmitsCOALESCE` | 4 new subtests PASS |
| 4 | Test coverage (config) | `internal/job/orphan_session_test.go` | Added `TestResolveOrphanSessionInterval` (6 subtests: default_when_unset, override_valid_15m, override_valid_1h, reject_invalid_falls_back, reject_zero_falls_back, reject_negative_falls_back) using `t.Setenv` | 6 subtests PASS |

### Pre-Gate state (from Dev phase, verified by Gate)
| File | Change | Verified |
|------|--------|----------|
| `internal/store/usage_analytics.go:151` | `COALESCE(%s::text, '__unassigned__') AS group_key` on GetTimeSeries | grep 0 legacy `'unknown'` in file, build+vet PASS |
| `internal/store/usage_analytics.go:259` | `COALESCE(%s::text, '__unassigned__') AS key` on GetBreakdowns (sentinel harmonized from `'unknown'`) | grep clean |
| `internal/api/analytics/handler.go:386-396` | `resolveGroupKeyName` early-return for `__unassigned__` sentinel → per-groupBy label | 3 subtests PASS |
| `internal/api/analytics/handler_test.go:366-388` | `TestResolveGroupKeyName_UnassignedSentinel` (apn / operator / rat_type subtests) | PASS |
| `web/src/pages/dashboard/analytics.tsx:74-85,422,448,521-522` | `resolveGroupLabel(groupBy, key)` helper + 4 call sites (Area name, legend strip, breakdown title attr, breakdown text) | TS typecheck PASS, 0 hex, 0 console.log |
| `internal/job/orphan_session.go` (new) | `OrphanSessionDetector` with Start/Stop/Run lifecycle, cross-tenant scan with per-tenant log output, warning-only (no auto-repair per AC-5 scope) | 4 behavior tests PASS incl -race |
| `internal/job/orphan_session_test.go` (new) | LogsWarning / NoOrphans / QueryError / MultiTenant | PASS |
| `cmd/argus/main.go` | Detector `Start` wired at init, `Stop` wired in `gracefulShutdown` order alongside `TimeoutDetector` | build PASS |

## Escalated Issues (architectural / business decisions)
None. No HIGH/CRITICAL findings.

## Deferred Items (tracked in ROUTEMAP → Tech Debt)
None new. Plan-documented scope cuts (warning-only AC-5, no 1M-row benchmark AC-8, DB-gated NullGroupKey test omitted) are all covered by alternative evidence (code-inspectable COALESCE + pure-Go SQL assembly test; env-configurable detector interval; handler-level sentinel tests).

## Passed Items
- **AC-1 (SQL COALESCE GetTimeSeries):** `internal/store/usage_analytics.go:151` emits `COALESCE(%s::text, '__unassigned__') AS group_key`. Covers all three aggregate views (cdrs / cdrs_hourly / cdrs_daily) via string-builder reuse. Verified by `TestBuildTimeSeriesQuery_COALESCESentinel`.
- **AC-2 (handler sentinel):** `resolveGroupKeyName` recognizes `__unassigned__` and returns "Unassigned APN" / "Unknown Operator" / "Unassigned" per group_by context. Verified by `TestResolveGroupKeyName_UnassignedSentinel`.
- **AC-3 (all group columns):** single COALESCE site in `buildTimeSeriesQuery` covers operator_id, apn_id, rat_type. `GetBreakdowns` parallel fix at line 259. Verified by parameterised test across all three columns.
- **AC-4 (scan path):** COALESCE at SQL layer eliminates NULL-scan crash; `UsageTimePoint.GroupKey string` is safe because Postgres never returns NULL post-COALESCE. Verified by code inspection + end-to-end build.
- **AC-5 (orphan detector):** `OrphanSessionDetector` running on 30m (configurable via ENV) ticker; multi-tenant scan with per-tenant warning logs + aggregate total log. No auto-repair per plan scope (destructive op out of P0). Wired into `gracefulShutdown`.
- **AC-6 (FE label):** `resolveGroupLabel` helper in `analytics.tsx:74-85` applied at 4 call sites (Area name legend, explicit legend strip, breakdown title tooltip, breakdown row text). `dataKey={key}` preserves raw group_key for Recharts identity; display label is translation layer only. TS typecheck clean.
- **AC-7 (regression test):** pure-Go SQL-fragment assertion (`TestBuildTimeSeriesQuery_COALESCESentinel`) covers the exact source of the pre-fix 500 crash — guards against any future refactor removing COALESCE. Handler-level sentinel coverage via `TestResolveGroupKeyName_UnassignedSentinel`.
- **AC-8 (performance):** COALESCE on a nullable column is a single-row expression evaluation, planner-cheap (no index invalidation, no extra round trip). No 1M-row benchmark built per plan scope ("planner-level concern"). Accepted as inspectable guarantee. If perf concern surfaces post-launch, it can revisit as POST-GA D-item.

### Scout lens coverage
**Analysis Scout:** SQL correctness (both sites covered) · sentinel consistency BE↔FE (no legacy `'unknown'` in `usage_analytics.go`) · orphan detector lifecycle (Start/Stop/wg/stopCh matches `TimeoutDetector` pattern) · `session_state` column verified correct in migrations · tenant scoping (multi-tenant scan is the correct choice for a system-level background job — improves on plan's per-tenant loop) · `operator_id`/`apn_id` alias edge case in `validGroupBy` noted (when user passes `group_by=operator_id`, the sentinel falls through to "Unassigned" instead of "Unknown Operator" — pre-existing, minor UX imperfection, not a regression from this story).

**Test/Build Scout:** `go build ./...` PASS · `go vet ./...` PASS · full suite 3338/3338 PASS across 102 packages · `-race` flag on orphan detector subset PASS · no flakes observed on repeated runs.

**UI Scout:** No new hex colors introduced by FIX-204 edits · 0 `console.*` calls · `resolveGroupLabel` is pure + TS-safe · `dataKey={key}` preserves chart identity (display separation via `name=`) · TS typecheck (`npx tsc --noEmit` in web/) PASS · arbitrary-px in `analytics.tsx` at lines 119/120/265/316/319/327/330/338/341/349/352/368/388 are pre-existing from earlier stories and are OUT of FIX-204 scope — noted for possible future design-token pass.

## Verification
- Tests after fixes: 3338/3338 PASS (102 packages)
- Build after fixes: PASS (go build + vet, TS typecheck)
- Token enforcement on edited FE file: 0 new hex, 0 console.log, TS clean
- Fix iterations: 1 (advisor gap closure — env configurability + pure-Go SQL assertion)

## Scope Cuts — Documented Acceptance
- **AC-5 auto-repair omitted:** plan explicitly warning-only; destructive repair deferred as out-of-scope for P0 bug fix.
- **AC-7 DB-gated `TestUsageTimeSeries_NullGroupKey` omitted:** replaced by pure-Go SQL assembly assertion (`TestBuildTimeSeriesQuery_COALESCESentinel`) — covers the exact pre-fix failure pattern without requiring live Postgres, runs in default CI.
- **AC-8 1M-row benchmark omitted:** COALESCE on nullable column is planner-cheap; accepted as code-inspectable guarantee. No harness built per plan "planner-level concern" clause.

## Dev-concern Reconciliation
1. **T1 legacy sentinel:** Confirmed 0 `'unknown'` remaining in `internal/store/usage_analytics.go`. Other files (`cost_analytics.go`, `sim.go`, `search/handler.go`) are OUT of FIX-204 scope — different endpoints/functions.
2. **T5 scheduler pattern:** Dev correctly identified `scheduler.Register` doesn't exist (scheduler is cron-expression-based via `AddEntry`). Standalone goroutine pattern mirrors `TimeoutDetector` — approved.
3. **T3 FE call sites:** 4 confirmed at lines 422, 448, 521 (title attr), 522 (text). Coverage complete.
4. **T4 DB-gated test:** Gate upgraded dev's "sufficient" framing to explicit pure-Go SQL assertion (see Fixes #2 + #3). AC-7 now has direct regression coverage.
5. **AC-5 ENV configurability gap closed in Gate (Fix #1 + #4):** `ORPHAN_SESSION_CHECK_INTERVAL` is now honoured with robust input validation (rejects non-positive and unparseable values).
