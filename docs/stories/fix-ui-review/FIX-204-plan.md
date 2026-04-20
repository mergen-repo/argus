# Fix Plan: FIX-204 — Analytics group_by=apn NULL Scan Bug + APN Orphan Sessions

## Goal
Eliminate the 500 crash on `GET /analytics/usage?group_by=apn` caused by scanning a SQL NULL into Go `string`, and apply consistent `__unassigned__` sentinel protection across all group_by columns (operator, apn, rat_type).

---

## Bug Description
`GET /analytics/usage?group_by=apn` returns 500. Log:
```
ERR get time series error="store: scan usage time point:
  can't scan into dest[5] (col: group_key): cannot scan NULL into *string"
```

Some `cdrs` / `cdrs_hourly` / `cdrs_daily` rows have `apn_id = NULL` (nullable per schema: sessions without assigned APN). SQL query builds `apn_id::text AS group_key` with no NULL guard. `UsageTimePoint.GroupKey string` cannot receive NULL from pgx.

---

## Root Cause

| Location | Line | Problem |
|----------|------|---------|
| `internal/store/usage_analytics.go:151` | `selectCols += fmt.Sprintf(`, %s::text AS group_key`, col)` | No COALESCE — NULL columns project to SQL NULL, pgx panics scanning into `string` |
| `internal/api/analytics/handler.go:386-408` | `resolveGroupKeyName` | Does not handle `__unassigned__` sentinel — it tries `uuid.Parse("__unassigned__")` which fails, returns sentinel as-is (acceptable but needs explicit branch) |
| `internal/store/usage_analytics.go:259` | `GetBreakdowns` uses `COALESCE(%s::text, 'unknown')` | Inconsistent sentinel — `'unknown'` vs proposed `'__unassigned__'` |

**Sentinel decision:** Harmonize to `'__unassigned__'` everywhere. The existing `'unknown'` in `GetBreakdowns` is ambiguous (could collide with a real RAT type value like `"unknown"`). Change it too.

---

## Affected Files

| File | Change | Reason |
|------|--------|--------|
| `internal/store/usage_analytics.go` | Modify | COALESCE in GetTimeSeries line 151 + update GetBreakdowns sentinel |
| `internal/api/analytics/handler.go` | Modify | resolveGroupKeyName sentinel branch for `__unassigned__` |
| `internal/api/analytics/handler_test.go` | Modify | Add NULL scan regression test |
| `internal/job/orphan_session.go` | Create | Orphan session detector job (AC-5) |
| `internal/job/orphan_session_test.go` | Create | Tests for orphan job |

---

## Architecture Context

### Components Involved

- **UsageAnalyticsStore** (`internal/store/usage_analytics.go`): SVC-07 analytics store. Builds dynamic SQL for time series, totals, breakdowns queries. All group_by columns (operator_id, apn_id, rat_type) are nullable in source tables.
- **Analytics Handler** (`internal/api/analytics/handler.go`): HTTP handler. Resolves UUID group keys to human-readable names via `resolveGroupKeyName`. Skips resolution for rat_type entirely (`groupBy != "rat_type"` guard at line 304).
- **Job Runner** (`internal/job/`): Background job infrastructure. Pattern: each job is a standalone `.go` file implementing a function with `context.Context` + store refs. Registration in `scheduler.go`.

### Data Flow (GetTimeSeries path)
```
GET /api/v1/analytics/usage?group_by=apn
  → Handler.GetUsage
  → UsageAnalyticsStore.GetTimeSeries(params)
    → ResolvePeriod → selects view: cdrs | cdrs_hourly | cdrs_daily
    → groupByColumn("apn") → "apn_id"
    → SQL: SELECT ..., apn_id::text AS group_key FROM cdrs_hourly ...
    → pgx row.Scan(&tp.GroupKey)  ← CRASH if apn_id IS NULL
  → handler maps GroupKey → resolveGroupKeyName (uuid.Parse sentinel check)
  → JSON response: time_series[].group_key
```

### Nullable Columns (from migrations)

```sql
-- Source: migrations/20260320000002_core_schema.up.sql
-- cdrs table:
apn_id UUID,           -- nullable (session may have no APN)
rat_type VARCHAR(10),  -- nullable
operator_id UUID NOT NULL  -- NOT NULL — but still wrap for safety per AC-3

-- sessions table:
apn_id UUID,           -- nullable
rat_type VARCHAR(10),  -- nullable

-- Source: migrations/20260323000003_cdrs_daily_dimensions.up.sql
-- cdrs_daily continuous aggregate includes: apn_id, rat_type, operator_id
-- (same nullability as source cdrs table)
```

### Key SQL Fix Location
`internal/store/usage_analytics.go` lines 149-153:
```go
if p.GroupBy != "" {
    col := groupByColumn(p.GroupBy)
    selectCols += fmt.Sprintf(`, %s::text AS group_key`, col)   // ← BUG
    groupCols += fmt.Sprintf(`, %s`, col)
}
```
Fix: wrap with `COALESCE(%s::text, '__unassigned__') AS group_key`.

Also `GetBreakdowns` line ~259: `COALESCE(%s::text, 'unknown')` → change `'unknown'` to `'__unassigned__'`.

### rat_type bypass in handler
`handler.go:304`: `if gk != "" && groupBy != "rat_type"` skips `resolveGroupKeyName` for rat_type entirely. Therefore `__unassigned__` for rat_type reaches the FE unprocessed. FE label mapping (T3) is the ONLY translation point for rat_type sentinel. Do NOT add rat_type to `resolveGroupKeyName`.

---

## Tasks

### Task 1: SQL COALESCE fix in UsageAnalyticsStore
- **Files:** Modify `internal/store/usage_analytics.go`
- **Depends on:** — (first task, no dependency)
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/usage_analytics.go` — existing `groupByColumn()` and `GetBreakdowns` COALESCE pattern.
- **Context refs:** "Root Cause", "Architecture Context > Nullable Columns", "Architecture Context > Key SQL Fix Location", "Architecture Context > Data Flow"
- **What:**
  1. Line 151 — Change `selectCols += fmt.Sprintf(`, %s::text AS group_key`, col)` to `selectCols += fmt.Sprintf(`, COALESCE(%s::text, '__unassigned__') AS group_key`, col)`. This fixes GetTimeSeries for all three aggregate branches (cdrs / cdrs_hourly / cdrs_daily) because the COALESCE is applied at string-building time for all branches.
  2. In `GetBreakdowns` (~line 259) — change `COALESCE(%s::text, 'unknown')` to `COALESCE(%s::text, '__unassigned__')`. Harmonize sentinel; prevents FE from receiving inconsistent strings across the two response surfaces.
  3. No type changes needed to `UsageTimePoint.GroupKey string` — COALESCE ensures the DB never returns NULL to Go.
- **Verify:** `go build ./internal/store/...` passes. `go test ./internal/store/...` passes.

### Task 2: Handler resolveGroupKeyName sentinel handling
- **Files:** Modify `internal/api/analytics/handler.go`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `internal/api/analytics/handler.go:386-408` — existing `resolveGroupKeyName` switch.
- **Context refs:** "Root Cause", "Architecture Context > rat_type bypass in handler"
- **What:**
  In `resolveGroupKeyName`, add an early-return guard BEFORE `uuid.Parse`:
  ```
  if key == "__unassigned__" {
      // translate by groupBy context
      switch groupBy {
      case "apn":   return "Unassigned APN"
      case "operator": return "Unknown Operator"
      default:      return "Unassigned"
      }
  }
  ```
  This prevents `uuid.Parse("__unassigned__")` from running and returns the display label. Keep this handler-side; do NOT add rat_type handling here (rat_type bypasses this function — FE handles it in T3).
  Also fix the calller at line 304: `if gk != "" && groupBy != "rat_type"` — leave this guard as-is; it's intentional.
- **Verify:** `go build ./internal/api/analytics/...` passes. `go test ./internal/api/analytics/...` passes.

### Task 3: FE label mapping for `__unassigned__` sentinel
- **Files:** Modify `web/src/pages/dashboard/analytics.tsx`
- **Depends on:** Task 1 (backend sentinel is defined)
- **Complexity:** low
- **Pattern ref:** Read `web/src/pages/dashboard/analytics.tsx` lines 214-240 — existing `group_key` usage in `buildChartData` and key de-dup logic.
- **Context refs:** "Architecture Context > rat_type bypass in handler", "Bug Description"
- **What:**
  Add a `resolveGroupLabel(groupBy: string, key: string): string` utility function in `analytics.tsx`:
  - If `key === '__unassigned__'`: return context-specific label — `"Unassigned APN"` for `groupBy==="apn"`, `"Unknown Operator"` for `groupBy==="operator"`, `"Unknown RAT"` for `groupBy==="rat_type"`.
  - Otherwise return `key` as-is.
  Apply this function wherever `p.group_key` is used as a chart series key or legend label (the `buildChartData` area at lines ~214-240). Do NOT change the `group_key` field name in the TypeScript type `TimeSeriesPoint` — it must remain `group_key?: string` as defined in `web/src/types/analytics.ts`.
  rat_type note: the backend never translates rat_type keys; the `__unassigned__` sentinel for rat_type reaches the FE raw. FE must handle it here.
- **Verify:** `cd /Users/btopcu/workspace/argus && npx tsc --noEmit -p web/tsconfig.json` — zero TS errors in `analytics.tsx`.

### Task 4: Regression test — NULL scan
- **Files:** Modify `internal/api/analytics/handler_test.go`, optionally add to `internal/store/usage_analytics_test.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** low
- **Pattern ref:** Read `internal/api/analytics/handler_test.go` — existing mock-based test structure. Read `internal/store/usage_analytics_test.go` — existing store unit tests.
- **Context refs:** "Root Cause", "Architecture Context > Data Flow", "Acceptance Criteria Mapping"
- **What:**
  Add `TestUsageTimeSeries_NullGroupKey` in `internal/api/analytics/handler_test.go`:
  - Build a mock/stub `UsageAnalyticsStore` that returns a `UsageTimePoint{GroupKey: ""}` (simulating what happens when COALESCE maps NULL → `"__unassigned__"` and then the handler maps it to "Unassigned APN").
  - Alternatively add a pure-Go unit test in `usage_analytics_test.go` testing `groupByColumn` and the COALESCE SQL fragment string assembly: verify the produced SQL contains `COALESCE(apn_id::text, '__unassigned__')`.
  - Add `TestResolveGroupKeyName_Sentinel` in `handler_test.go`: call `h.resolveGroupKeyName(ctx, "apn", "__unassigned__", tenantID)` and assert it returns `"Unassigned APN"`.
  - Add `TestResolveGroupKeyName_OperatorSentinel`: `group_by=operator`, `__unassigned__` → `"Unknown Operator"`.
  - AC-8 perf note: add a comment in the test file noting that COALESCE performance is a planner concern — assert `EXPLAIN` text contains "Filter" but do NOT build a 1M-row benchmark; this is a P0 fix story.
- **Verify:** `go test ./internal/api/analytics/... ./internal/store/...` — all pass.

### Task 5: Orphan session detector job
- **Files:** Create `internal/job/orphan_session.go`, `internal/job/orphan_session_test.go`
- **Depends on:** Task 1 (sentinel semantics established)
- **Complexity:** low
- **Pattern ref:** Read `internal/job/ip_reclaim.go` — same job function pattern: standalone func + struct, context-aware, returns error. Read `internal/job/scheduler.go` to understand job registration.
- **Context refs:** "Architecture Context > Components Involved", "Acceptance Criteria Mapping"
- **What:**
  Create `OrphanSessionDetector` job in `internal/job/orphan_session.go`:
  - Query: `SELECT COUNT(*) FROM sessions WHERE apn_id IS NULL AND session_state = 'active' AND tenant_id = $1`
  - If count > 0: log `WARN` with tenant_id and count. Optionally publish a NATS event `data.integrity.orphan_sessions_detected` with JSON payload `{tenant_id, count, ts}`.
  - No auto-repair in this story — AC-5 says "logs warning + optional auto-repair"; plan warning-only. Auto-repair is a destructive operation and out of scope for P0.
  - Register the job in scheduler with interval `30m` (configurable via ENV `ORPHAN_SESSION_CHECK_INTERVAL`).
  - Test: `TestOrphanSessionDetector_LogsWarning` — stub DB returning count=5, assert log contains "orphan sessions detected", assert no error returned.
  - Test: `TestOrphanSessionDetector_NoOrphans` — stub returning count=0, assert no log output, no error.
- **Verify:** `go test ./internal/job/...` passes. `go build ./...` passes.

---

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1: COALESCE `apn_id::text` in SQL | Task 1 | Task 4 TestUsageTimeSeries_NullGroupKey |
| AC-2: resolveGroupKeyName `__unassigned__` → "Unassigned APN" | Task 2 | Task 4 TestResolveGroupKeyName_Sentinel |
| AC-3: Same NULL protection for operator, rat_type | Task 1 (single COALESCE site covers all), Task 3 (rat_type FE) | Task 4 TestResolveGroupKeyName_OperatorSentinel |
| AC-4: Scan destination uses COALESCE (not sql.NullString) | Task 1 | Task 4 |
| AC-5: Orphan session detector job | Task 5 | Task 5 tests |
| AC-6: FE shows "Unassigned APN" for group_key=`__unassigned__` | Task 3 | Manual browser check |
| AC-7: Unit test TestUsageTimeSeries_NullGroupKey | Task 4 | `go test` |
| AC-8: COALESCE overhead < 5% | Task 4 (comment + planner assertion, no benchmark harness) | N/A — planner-level concern |

---

## Story-Specific Compliance Rules

- **API:** Standard envelope `{ status, data }` — no changes to response shape; group_key `__unassigned__` is a data value, not a structural change.
- **DB:** No migration needed — COALESCE is a query-level fix. Columns already nullable per existing migrations. Both `GetTimeSeries` and `GetBreakdowns` are query-only changes.
- **Sentinel harmonization:** `GetBreakdowns` currently emits `'unknown'` — MUST change to `'__unassigned__'` in Task 1 to avoid two different sentinel strings in the same API response.
- **rat_type handling:** `resolveGroupKeyName` intentionally does NOT handle rat_type (handler bypasses it). FE is sole translation point for rat_type `__unassigned__`.
- **Job registration:** New job must be registered in `internal/job/scheduler.go` using the same pattern as existing jobs.

---

## Bug Pattern Warnings

- **PAT-006 [FIX-201]:** When fixing `GetBreakdowns` sentinel, grep ALL call sites of `GetBreakdowns` and verify downstream consumers (e.g., breakdowns map in handler.go:218-261) handle `__unassigned__` key correctly (they use `key` string as-is → FE side must map it; no handler-level mapping for breakdown keys).
- No other matching patterns for this story's scope.

---

## Tech Debt

No tech debt items targeted at FIX-204 in ROUTEMAP.

---

## Mock Retirement

No mock files affected — this is a backend query bug. FE changes are in production code (`analytics.tsx`), not mocks.

---

## Regression Risk

- **Low:** COALESCE only affects NULL-path rows. Non-null existing data is unaffected (COALESCE returns the original value when non-null).
- **Medium:** Changing `GetBreakdowns` sentinel from `'unknown'` to `'__unassigned__'` changes the JSON `key` field for those rows. Any client expecting `"unknown"` will see `"__unassigned__"` after the fix. FE analytics page consumes this as `breakdowns[dim][].key` — update FE label mapping (Task 3) to also handle breakdown keys.
- **Existing tests that must still pass:** `TestResolvePeriod_Presets`, `TestResolvePeriod_Custom` (no change to those code paths).
