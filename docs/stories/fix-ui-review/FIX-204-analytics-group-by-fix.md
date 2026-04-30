# FIX-204: Analytics group_by=apn NULL Scan Bug + APN Orphan Sessions

## Problem Statement
`GET /analytics/usage?group_by=apn` returns **500 INTERNAL_ERROR**. App log:
```
ERR get time series error="store: scan usage time point: 
  can't scan into dest[5] (col: group_key): cannot scan NULL into *string"
```
Root cause: SQL query projects `apn_id::text AS group_key` but some session/usage rows have `apn_id = NULL` (orphan sessions, default APN fallbacks). Go scan destination `*string` doesn't tolerate NULL.

## User Story
As an analyst, I want Analytics `Group by: APN` to work without crashing so I can see per-APN traffic breakdown.

## Architecture Reference
- Backend: `internal/store/analytics.go` usage time series query
- Handler: `internal/api/analytics/handler.go:166-178, :386 resolveGroupKeyName`

## Findings Addressed
F-22 (null scan crash), F-28 (group_by implementation gap)

## Acceptance Criteria
- [ ] **AC-1:** SQL query wraps with `COALESCE(apn_id::text, '__unassigned__') AS group_key` — no NULLs escape to Go layer.
- [ ] **AC-2:** Handler `resolveGroupKeyName` recognizes `__unassigned__` sentinel → returns display "Unassigned APN" (i18n-friendly).
- [ ] **AC-3:** Same NULL protection applied to `group_by=operator`, `group_by=rat_type` — audit all group_by columns for nullability.
- [ ] **AC-4:** Scan destination changed from `*string` to `sql.NullString` OR string with COALESCE fallback (pick COALESCE for simpler code path).
- [ ] **AC-5:** Orphan session cleanup: backend data-integrity job scans `sessions` for NULL apn_id on active sessions (which should always have APN) — logs warning + optional auto-repair to default APN.
- [ ] **AC-6:** Response structure: time_series entries for group_key="__unassigned__" show "Unassigned APN" in FE; aggregates in total row at bottom.
- [ ] **AC-7:** Unit test `TestUsageTimeSeries_NullGroupKey` — fixture with 1 session having NULL apn_id, verify no crash + response contains "Unassigned" bucket.
- [ ] **AC-8:** Performance regression check — COALESCE overhead < 5% on 1M row query.

## Files to Touch
- `internal/store/analytics.go` (or equivalent) — COALESCE in query
- `internal/api/analytics/handler.go` — handle sentinel in resolveGroupKeyName
- `internal/api/analytics/handler_test.go` — NULL scan test
- `internal/job/data_integrity.go` (new or existing) — orphan session detector

## Risks & Regression
- **Risk 1 — Other nullable group columns:** AC-3 audit other groupings.
- **Risk 2 — Breaking existing non-null data:** Behavior identical for populated rows; COALESCE only affects nil path.
- **Risk 3 — Cardinality issue:** "__unassigned__" bucket may grow large if many orphans. AC-5 detector surfaces volume.

## Test Plan
- Unit: NULL scan test + orphan detection
- Integration: `?group_by=apn` returns 200 with unassigned bucket
- Browser: Analytics page "Group by APN" works without error

## Plan Reference
Priority: P0 · Effort: S · Wave: 1
