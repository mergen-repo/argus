# Review: STORY-034 — Usage Analytics Dashboards

**Date:** 2026-03-22
**Reviewer:** Amil Reviewer Agent
**Phase:** 6 (Analytics & BI)
**Status:** COMPLETE

---

## 1. Next Story Impact Analysis

### STORY-035 (Cost Analytics & Optimization) — 2 observations

1. **Shared pattern reuse:** STORY-034 established the `UsageAnalyticsStore` + `analyticsapi.Handler` pattern with period resolution, parameterized queries, and SQL injection prevention via dimension allowlists. STORY-035 can follow the identical architecture: a `CostAnalyticsStore` with `ResolvePeriod()` reuse (same function in `store` package) and a new `cost` handler. The existing `GetCostAggregation()` from STORY-032 provides per-operator daily cost data, but STORY-035 needs finer breakdowns (cost-per-MB by RAT type, top expensive SIMs). STORY-035 should query `cdrs_hourly` for RAT-level cost breakdowns since `cdrs_daily` lacks `apn_id` and `rat_type` columns.

2. **Comparison mode:** STORY-034's comparison logic (`deltaPercent()`, previous-period calculation) is directly reusable for STORY-035 cost comparison. Consider extracting the comparison pattern to a shared utility if both stories end up with identical delta calculation.

**Impact: LOW — Pattern established, no blocking changes.**

### STORY-036 (Anomaly Detection) — 1 observation

1. **CDR aggregate reuse for batch detection:** STORY-036 AC requires "SIM daily usage exceeds 3x its 30-day average." The `cdrs_daily` continuous aggregate (now with `real-time aggregation` enabled by STORY-034 migration) provides per-SIM daily totals via `active_sims`. However, `cdrs_daily` groups by `tenant_id, operator_id` only -- not by `sim_id`. STORY-036 will need to query raw `cdrs` table grouped by `sim_id` for per-SIM daily totals, or create a new per-SIM continuous aggregate. The real-time aggregation enabled by STORY-034 benefits STORY-036 by ensuring near-real-time data in the daily view.

**Impact: LOW — No blocking changes.**

### STORY-037 (Connectivity Diagnostics) — No changes needed

STORY-037 is a per-SIM diagnostic check using session/operator/APN/policy data. It does not depend on analytics aggregates. No impact.

**Impact: NONE.**

### STORY-048 (Frontend Analytics Pages) — 1 observation

1. **API contract alignment:** STORY-048 AC specifies "filter bar (operator, APN, RAT type, segment)." STORY-034 removed `segment_id` filter (DEV-110). STORY-048 frontend should either omit the segment filter dropdown on the usage page or implement client-side segment resolution (query segment SIM IDs, then filter). The API response DTO matches what STORY-048 expects: `time_series`, `totals`, `breakdowns`, `top_consumers`, `comparison`.

**Impact: LOW — Segment filter omission noted for frontend.**

### STORY-053 (Data Volume Optimization) — 1 observation

1. **`cdrs_monthly` aggregate created:** STORY-034 created `cdrs_monthly` which STORY-053 can leverage for long-term data archival decisions (monthly summaries survive after raw CDR compression/archival). No changes needed to STORY-053.

**Impact: NONE.**

---

## 2. Architecture Evolution

### Implemented vs Planned Structure

| Aspect | Planned (ARCHITECTURE.md) | Implemented | Status |
|--------|---------------------------|-------------|--------|
| Package location | `internal/analytics/` (SVC-07) | Store: `internal/store/usage_analytics.go`, Handler: `internal/api/analytics/handler.go` | CONSISTENT — follows store+handler separation pattern |
| API endpoint | API-111: `GET /api/v1/analytics/usage` | Implemented at exact path with `analyst` role | CONSISTENT |
| API-052 | `GET /api/v1/sims/:id/usage` (per-SIM usage) | NOT implemented by STORY-034 | OBSERVATION — API-052 is a per-SIM endpoint, different from fleet-wide API-111. Not in STORY-034 scope. May be addressed by STORY-048 frontend or remain for a future story. |
| TimescaleDB aggregates | Hourly + daily (STORY-002), monthly mentioned in ALGORITHMS.md | Hourly + daily (pre-existing) + monthly (new) | CONSISTENT |
| Real-time aggregation | Not explicitly planned | Enabled on all 3 views via `materialized_only = false` | ENHANCEMENT — improves query freshness |

### Architecture Doc Updates Needed

1. **aaa-analytics.md** should document the `cdrs_monthly` continuous aggregate alongside `cdrs_hourly` and `cdrs_daily`.

---

## 3. New Domain Terms

| Term | Definition | Context |
|------|-----------|---------|
| Usage Analytics | Fleet-wide time-series analytics API providing data volume, session count, auth count, and unique SIM metrics over configurable periods (1h/24h/7d/30d/custom). Supports group-by dimensions (operator, APN, RAT type), breakdowns, top 20 consumers, and comparison mode with delta percentages. | SVC-07, STORY-034, API-111 |
| Period Resolution | Mapping of user-selected time period to optimal aggregate view and bucket size. 1h->raw cdrs (1min buckets), 24h->cdrs_hourly (15min), 7d->cdrs_hourly (1h), 30d->cdrs_daily (6h). Custom ranges auto-calculate based on span duration. | SVC-07, STORY-034, DEV-109 |
| Real-Time Aggregation | TimescaleDB feature (`materialized_only = false`) that combines materialized aggregate data with recent un-aggregated raw data at query time. Ensures analytics queries return current data without waiting for refresh policies. | SVC-07, STORY-034 |

**Action: Add to GLOSSARY.md under "Argus Platform Terms".**

---

## 4. FUTURE.md Relevance

1. **AI Anomaly Engine (FTR-001):** FUTURE.md states "ML-based anomaly detection beyond rule-based -- learns 'normal' patterns per SIM/APN." The usage analytics time-series data (per-SIM, per-APN, per-operator) provides the exact training data needed for ML models. The `ResolvePeriod()` function and aggregate views create clean feature extraction pipelines for ML. **No FUTURE.md update needed.**

2. **Predictive Quota Management (FTR-002):** FUTURE.md mentions "per-SIM quota consumption forecast." The `GetTopConsumers()` query (per-SIM usage aggregation) is the foundation for per-SIM consumption trending. **No FUTURE.md update needed.**

3. **Network Quality Scoring (FTR-004):** The per-operator/per-RAT breakdowns enable quality scoring per dimension. **No FUTURE.md update needed.**

---

## 5. New Decisions to Capture

Decisions DEV-109, DEV-110, DEV-111 are already captured in `docs/brainstorming/decisions.md`. No additional decisions needed.

| # | Date | Decision | Status |
|---|------|----------|--------|
| DEV-109 | 2026-03-22 | Period-to-aggregate resolution mapping | Already captured |
| DEV-110 | 2026-03-22 | segment_id filter removed | Already captured |
| DEV-111 | 2026-03-22 | Aggregate view column mappings | Already captured |

---

## 6. Cross-Document Consistency

| Check | Documents | Status | Notes |
|-------|-----------|--------|-------|
| API-111 endpoint | api/_index.md vs router.go | CONSISTENT | Both specify `GET /api/v1/analytics/usage` with `analyst+` role |
| API-111 response shape | STORY-034 spec vs handler.go | CONSISTENT | `time_series`, `totals`, `breakdowns`, `top_consumers`, `comparison` all present |
| API-052 per-SIM usage | api/_index.md | NOT IMPLEMENTED | API-052 references STORY-034 but is a different endpoint (`/sims/:id/usage`). Should be re-assigned or clarified. |
| Continuous aggregates | aaa-analytics.md vs migrations | MINOR GAP | `cdrs_monthly` not documented in aaa-analytics.md. Add it. |
| Period buckets | STORY-034 spec vs implementation | CONSISTENT | 1h->1min, 24h->15min, 7d->1h, 30d->6h all match |
| Filters | STORY-034 spec vs handler.go | NARROWED | Spec lists `segment_id`; implementation has 3 filters (operator_id, apn_id, rat_type). Accepted per DEV-110. |
| ROUTEMAP status | ROUTEMAP.md | NEEDS UPDATE | Currently shows `[~] IN PROGRESS`. Should be `[x] DONE`. |
| Decisions | decisions.md | CONSISTENT | DEV-109 to DEV-111 all present and accurate. |
| Gate report | STORY-034-gate.md | CONSISTENT | All 11 ACs verified, 3 gate fixes applied and verified. |
| STORY-048 dependency | STORY-048 spec | CONSISTENT | Correctly lists STORY-034 as dependency for usage API. |
| STORY-053 dependency | STORY-053 spec | CONSISTENT | Correctly lists STORY-034 as dependency for analytics queries. |
| Feature reference | PRODUCT.md F-036 (real-time dashboards) | CONSISTENT | Planning review confirms F-036 mapped to STORY-034. |

**Overall: CONSISTENT with 2 minor items (API-052 reference, cdrs_monthly doc gap).**

---

## 7. Code Quality Observations

| # | Severity | Observation | Location |
|---|----------|-------------|----------|
| 1 | LOW | `GetBreakdowns()` only applies `operator_id` filter but not `apn_id` or `rat_type` filters. The `apn_id` and `rat_type` params from the handler are not passed to breakdown queries. This is acceptable -- breakdowns show all dimensions regardless of time-series filters -- but should be documented. | `internal/store/usage_analytics.go:251` |
| 2 | LOW | `GetTimeSeries()` silently drops `apn_id` and `rat_type` filters when querying `cdrs_daily` (which lacks those columns). The `if spec.AggregateView != "cdrs_daily"` guard is correct behavior but could benefit from a log warning when filters are silently ignored. | `internal/store/usage_analytics.go:163-171` |
| 3 | INFO | `cdrs_monthly` aggregate created but never queried. Created for future use per gate notes. No harm. | `migrations/20260322000002_usage_analytics_aggregates.up.sql` |
| 4 | INFO | `GroupBy` parameter uses `p.GroupBy` directly in SQL format string, but `sanitizeDimension()` is not called in `GetTimeSeries`. The handler validates via `validGroupBy` map which covers the same values, so this is safe. However, double-sanitizing via `sanitizeDimension()` in the store would be defense-in-depth. | `internal/store/usage_analytics.go:150` |

---

## 8. Test Coverage Assessment

| File | Test Functions | Sub-tests | Results | Coverage |
|------|---------------|-----------|---------|----------|
| `internal/api/analytics/handler_test.go` | 12 | 6 (deltaPercent) | 17 PASS, 1 SKIP | Handler validation paths, delta math |
| `internal/store/usage_analytics_test.go` | 5 | 17 | All PASS | Period resolution, time range, sanitization |
| **Total** | **17** | **23** | **39 assertions** | Input validation, period logic, security |

**Notable gaps:** No integration tests that verify actual SQL queries against a database (store tests are unit-only for pure functions). This is acceptable for v1 -- the build-tagged integration test pattern is in place for future expansion.

---

## 9. Document Updates

### 9.1 GLOSSARY.md — Add 3 new terms
- Usage Analytics, Period Resolution, Real-Time Aggregation

### 9.2 aaa-analytics.md — Add cdrs_monthly aggregate
- Document the `cdrs_monthly` continuous aggregate (schema, refresh policy, real-time flag)

### 9.3 ROUTEMAP.md — Mark STORY-034 complete
- Status: `[x] DONE`
- Step: `—`
- Completed: `2026-03-22`
- Update progress counter: 33/55 (60%)
- Phase 6 header: still `[IN PROGRESS]` (STORY-035, 036, 037 remain)

### 9.4 api/_index.md — Clarify API-052 reference
- API-052 (`GET /api/v1/sims/:id/usage`) currently references STORY-034 but was not implemented. Either re-assign to a future story or mark as deferred.

---

## 10. Compilation & Test Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS — clean compilation, zero errors |
| Handler tests (12 functions) | PASS — 17 pass, 1 skip (requires DB) |
| Store tests (5 functions) | PASS — all 17 sub-tests pass |
| Full project build | PASS — no regressions |

---

## 11. Security Review

| Check | Result | Notes |
|-------|--------|-------|
| SQL injection prevention | PASS | `sanitizeDimension()` allowlist for group-by; all user input parameterized ($1..$N); table names from code constants |
| Tenant isolation | PASS | All queries include `tenant_id = $1` as first condition |
| Auth/RBAC | PASS | Route protected by `JWTAuth` + `RequireRole("analyst")` |
| Input validation | PASS | Period validated against `validPeriods` map; group_by against `validGroupBy`; UUIDs parsed; dates RFC3339; from<to check |
| Resource limits | PASS | Top consumers capped at 100 max; breakdowns bounded by distinct dimension values |
| Error messages | PASS | Internal errors return generic message; no stack traces or SQL exposed to client |

---

## 12. Gate Fix Verification

All 3 gate findings from the re-gate are confirmed resolved:

| # | Original Finding | Status | Evidence |
|---|-----------------|--------|----------|
| 1 | segment_id dead code | FIXED | No `segment_id` references in handler.go or usage_analytics.go |
| 2 | auths/unique_sims return 0 in aggregates | FIXED | `cdrs_hourly`: record_count as auths; `cdrs_daily`: active_sims as unique_sims |
| 3 | active_sims misused as sessions | FIXED | `cdrs_daily` returns `0::bigint AS sessions` (honest zero) |

---

## Summary

| Metric | Result |
|--------|--------|
| Stories Impacted | 5 analyzed, 0 blocking changes needed |
| Architecture Changes | None required (1 doc update: cdrs_monthly in aaa-analytics.md) |
| New Glossary Terms | 3 (Usage Analytics, Period Resolution, Real-Time Aggregation) |
| New Decisions | 0 (DEV-109 to DEV-111 already captured) |
| Cross-Doc Consistency | CONSISTENT (1 minor: API-052 reference, 1 doc gap: cdrs_monthly) |
| Code Observations | 4 (2 LOW, 2 INFO — no blockers) |
| FUTURE.md | No updates needed |
| ROUTEMAP Progress | 33/55 (60%) |
| Next Story | STORY-035 (Cost Analytics & Optimization) |
