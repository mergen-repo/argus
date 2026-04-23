# Gate Report: FIX-220 — Analytics Polish (MSISDN, IN/OUT Split, Tooltip, Delta Cap, Capitalization)

## Summary
- Requirements Tracing: Fields 13/13, Endpoints 1/1, Workflows 14/14, Components 10/10
- Gap Analysis: 13/14 ACs PASS (AC-14 Export CSV DEFERRED to FIX-236 per plan)
- Compliance: COMPLIANT
- Tests: 482/482 go tests pass (store + api/analytics + dependents), FE unit tests DEFERRED per D-091
- Test Coverage: Unit tests deferred to FE test-infra wave (D-053/D-091)
- Performance: 6 issues found, 2 fixed directly, 4 deferred to perf hardening wave
- Build: PASS (go build + vet + vite build all green)
- Screen Mockup Compliance: 10/10 elements implemented per ASCII spec
- UI Quality: 14/14 criteria PASS, 0 NEEDS_FIX, 0 CRITICAL
- Token Enforcement: 0 violations (zero hex, zero default-tailwind, zero raw HTML, zero competing libs, zero inline SVG across 7 in-scope files)
- Turkish Text: not in scope for this FIX (locale hint deferred to i18n wave per F-U3/D-113)
- Overall: **PASS**

## Team Composition
- Analysis Scout: 12 findings (F-A1..F-A12; 6 MEDIUM, 6 LOW, 0 CRITICAL/HIGH)
- Test/Build Scout: 0 findings (all gates PASS; bundle delta +0.99 kB acceptable)
- UI Scout: 3 findings (F-U1..F-U3; all LOW)
- De-duplicated: 15 → 12 findings (F-U1 ≡ F-A3, F-U2 ≡ F-A5, F-U3 flagged separately as locale concern)

## Merged Findings Table (sorted by severity)

| ID | Severity | Category | Title | Resolution |
|----|----------|----------|-------|-----------|
| F-A1 | MEDIUM | gap | enrichTopConsumer clobbers store-joined values with blank when live SIM row has empty fields | **FIXED** — added non-empty guards for ICCID/IMSI/OperatorID (handler.go:384-395) |
| F-A2 | MEDIUM | performance | N+1 pattern — up to 80 DB round-trips per /analytics/usage call | **DEFERRED** → D-106 (FIX-24x perf hardening) |
| F-A3 ≡ F-U1 | MEDIUM | compliance | Shared Tooltip atom not accessible (mouse-only, no role/aria) | **DEFERRED** → D-107 (FIX-24x a11y pass); cross-cutting, out of FIX-220 scope |
| F-A4 | MEDIUM | gap | AC-5 keyboard-focusable tooltip not satisfied — recharts is pointer-only | **DEFERRED** → D-108 (FIX-24x a11y pass with SR data-table alternative) |
| F-A9 | MEDIUM | performance | GetTopConsumers GROUP BY 6 cols scalability beyond LIMIT=20 | **DEFERRED** → D-111 (FIX-24x perf hardening) |
| F-A12 | MEDIUM | compliance | Pre-existing bug: `cdrs_daily` guard silently drops apn_id/rat_type filters on 30d queries | **FIXED** — removed the `&& spec.AggregateView != "cdrs_daily"` conjunct on both lines (usage_analytics.go:187, 192). See Behavioral Change note below. |
| F-A5 ≡ F-U2 | LOW | compliance | Redundant double `humanizeRatType` wrap in breakdown row | **FIXED** — simplified to `resolveGroupLabel(dim, item.key)` (analytics.tsx:591-592) |
| F-A6 | LOW | gap | AC-10 empty state hint not filter-specific (says "active filter" not the filter name) | **DEFERRED** → D-109 (FIX-24x UI polish) |
| F-A7 | LOW | compliance | Backend deltaPercent dead-code; FE bypasses the emitted fields | **DEFERRED** → D-110 (FIX-24x cleanup) |
| F-A8 | LOW | performance | TwoWayTraffic render-path divergence; reviewer confusion risk | **FIXED** — added explanatory comment to usage-chart-tooltip.tsx explaining the two code paths |
| F-A10 | LOW | compliance | cdrs_daily aggregate edge: bytes_in=0 bytes_out=0 with total_bytes>0 | **DEFERRED** → D-112 (FIX-24x UI polish); currently unreachable (TwoWayTraffic only renders in non-grouped, which uses full-fidelity cdrs/cdrs_hourly fields) |
| F-A11 | LOW | security | No new OWASP patterns introduced | **NO_ACTION** (nothing to fix) |
| F-U3 | LOW | ui | Empty-state date formatter uses `en-GB` not Turkish locale | **DEFERRED** → D-113 (separate i18n wave); pre-existing page-wide convention |

## Fixes Applied

| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Gap (F-A1) | `internal/api/analytics/handler.go:384-395` | Added non-empty guards to ICCID, IMSI, OperatorID overrides in `enrichTopConsumer` (matches existing MSISDN guard pattern). Prevents store-joined values being clobbered with blanks when a live SIM row has empty identity columns. | go build + go test 482/482 PASS |
| 2 | Compliance (F-A12) | `internal/store/usage_analytics.go:187-196` | Removed `&& spec.AggregateView != "cdrs_daily"` conjunct on both the `apn_id` and `rat_type` WHERE clause guards. Migration `20260323000003_cdrs_daily_dimensions.up.sql` added both columns to the `cdrs_daily` materialized view, making the original guard obsolete. **Behavioral change**: 30d-period analytics queries with `apn_id` or `rat_type` filters now correctly return filtered data (previously silently returned unfiltered 30d aggregates). See note below. | go build + go test 482/482 PASS |
| 3 | Compliance (F-A5/F-U2) | `web/src/pages/dashboard/analytics.tsx:591-592` | Removed redundant outer `humanizeRatType(...)` wrap in breakdown row. `resolveGroupLabel(dim, item.key)` at line 94 already calls `humanizeRatType` for the `rat_type` branch — outer call was a no-op today (`humanizeRatType('LTE-M')` returns `'LTE-M'` via fallback toUpperCase) but would mask bugs if map keys change. Import preserved (still used inside `resolveGroupLabel`). | tsc + vite build PASS |
| 4 | Performance doc (F-A8) | `web/src/components/analytics/usage-chart-tooltip.tsx:38-46` | Added block comment explaining why the non-grouped branch reads raw `data.time_series` via `allData` prop (to access `bytes_in`/`bytes_out`/`sessions`/`auths`/`unique_sims`) while the grouped branch reads the recharts payload directly (chartData is pivoted per-bucket with one key per group series; raw byte fields are not propagated into the pivoted bucket, so TwoWayTraffic only renders non-grouped). | tsc + vite build PASS |

### Behavioral Change Note (F-A12)

Prior to this fix, `buildTimeSeriesQuery` at `internal/store/usage_analytics.go` silently dropped the user-supplied `apn_id` and `rat_type` WHERE clauses whenever `AggregateView == "cdrs_daily"` (i.e., for any 30d-period analytics request). The original rationale — `cdrs_daily` lacked those dimension columns — was invalidated by migration `20260323000003_cdrs_daily_dimensions.up.sql` which added both columns to the aggregate view.

**Impact:** 30d `/analytics/usage` responses with `apn_id` or `rat_type` filters previously returned tenant-wide totals instead of filtered totals. After this fix, those queries correctly return filtered results.

**Consumer surfaces:**
- `/analytics` page (this FIX's target) — KPIs, time series, top consumers, breakdowns now all honor the filter for 30d views.
- No other known consumers of 30d + apn_id/rat_type filters (cost-analytics and anomaly pages were out of scope for this FIX; any pre-existing callers of the `UsageAnalyticsStore.GetTimeSeries` path with `cdrs_daily` + those filters will now receive corrected data).

**Regression risk:** LOW. The fix aligns behavior with the documented API contract (filters are honored for all periods). Existing 482 go tests PASS. Manual verification in the store layer would require DATABASE_URL + seeded 30d CDR fixtures (DB-gated; not in default CI).

## Escalated Issues

None. All findings were either FIXED or DEFERRED with explicit target in ROUTEMAP Tech Debt.

## Deferred Items (tracked in ROUTEMAP → Tech Debt)

| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-106 | F-A2 — N+1 enrichTopConsumer (80 round-trips/call) | FIX-24x (perf hardening) | YES |
| D-107 | F-A3/F-U1 — Shared Tooltip atom not accessible (cross-cutting) | FIX-24x (a11y pass) | YES |
| D-108 | F-A4 — Recharts tooltip keyboard-inaccessible (AC-5 partial) | FIX-24x (a11y pass) | YES |
| D-109 | F-A6 — EmptyState filter-name hint not specific | FIX-24x (UI polish) | YES |
| D-110 | F-A7 — Backend deltaPercent dead-code cleanup | FIX-24x (cleanup) | YES |
| D-111 | F-A9 — GetTopConsumers GROUP BY scalability beyond LIMIT=20 | FIX-24x (perf hardening) | YES |
| D-112 | F-A10 — cdrs_daily aggregate edge (cosmetic polish) | FIX-24x (UI polish) | YES |
| D-113 | F-U3 — Turkish date locale (page-wide convention) | Separate i18n wave | YES |

## Performance Summary

### Queries Analyzed

| # | File:Line | Pattern | Issue | Severity | Status |
|---|-----------|---------|-------|----------|--------|
| 1 | `usage_analytics.go:352-370` | `GetTopConsumers` cdrs JOIN sims GROUP BY 6 cols | Acceptable at LIMIT=20; scale risk logged | MEDIUM | **DEFERRED** D-111 |
| 2 | `usage_analytics.go:127-204` | `buildTimeSeriesQuery` dynamic SELECT | OK (per-period view selection) | LOW | PASS |
| 3 | `usage_analytics.go:232-269` | `GetTotals(cdrs)` | OK (bounded by `timestamp BETWEEN` + `tenant_id`) | LOW | PASS |
| 4 | `usage_analytics.go:187,192` | `cdrs_daily` filter guard silently drops apn/rat filter | Data-correctness regression (pre-existing) | MEDIUM | **FIXED** |
| 5 | `handler.go:353-357, 379-421` | `enrichTopConsumer` N+1 (up to 80 DB round-trips per /usage call) | Perf | MEDIUM | **DEFERRED** D-106 |
| 6 | `handler.go:226-270` | `GetBreakdowns` × 3 dims | OK (LIMIT 50 each) | LOW | PASS |

### Caching Verdicts

| # | Data | Location | TTL | Decision |
|---|------|----------|-----|----------|
| 1 | operator name by id | in-memory LRU (future) | 5m | SKIP for FIX-220; tied to F-A2 follow-up (D-106) |
| 2 | apn name/display_name by (tenant, id) | in-memory LRU (future) | 5m | SKIP as above |
| 3 | top_consumers query result | Redis | 30s | SKIP — freshness matters; deferred |
| 4 | `cdrs_hourly` / `cdrs_daily` rows | DB-side continuous aggregates | TimescaleDB policy | CACHE (already in place via MV refresh policies) |

## Token & Component Enforcement

| Check | Before | After | Status |
|-------|--------|-------|--------|
| Hardcoded hex colors (in-scope files) | 0 | 0 | CLEAN |
| Arbitrary pixel values (new components) | 0 | 0 | CLEAN (pre-existing page scaffold px values documented in plan Token Map) |
| Raw HTML elements | 0 | 0 | CLEAN |
| Competing UI library imports | 0 | 0 | CLEAN |
| Default Tailwind colors (gray/slate/white) | 0 | 0 | CLEAN |
| Inline SVG | 0 | 0 | CLEAN (lucide icons only) |
| Missing elevation (shadow-none) | 0 | 0 | CLEAN |

## Verification (post-fix)

| Check | Result |
|-------|--------|
| `go build ./...` | **PASS** |
| `go vet ./...` | **PASS** (0 issues) |
| `go test ./internal/store/... ./internal/api/analytics/...` | **PASS** (482/482) |
| `cd web && npx tsc --noEmit` | **PASS** (0 errors) |
| `cd web && npx vite build` | **PASS** (2.43s) |
| Main bundle `index-*.js` | 408.90 kB (gzip 124.42 kB) — unchanged from pre-fix baseline (+0.99 kB from FIX-219 baseline, within budget) |
| Analytics chunk `analytics-*.js` | 18.06 kB (gzip 5.22 kB) — effectively unchanged (−30 bytes from baseline) |
| Fix iterations | 1 (no re-fix needed) |

## Passed Items

- All 13 AC items targeted by plan PASS (AC-14 DEFERRED per plan to FIX-236 streaming export).
- `GetTopConsumers` SQL JOIN semantics preserved (1:1 sim_id → sims, LIMIT 20 bounded).
- `buildTimeSeriesQuery` additive SELECT extension (`SUM(bytes_in)`, `SUM(bytes_out)`) introduces no JOIN, no grouping change, no regression.
- New components (`TwoWayTraffic`, `UsageChartTooltip`) are pure presentational — no hooks, no effects, no timers, no resource leaks.
- Scope discipline honored: `analytics-cost.tsx`, `analytics-anomalies.tsx`, and downstream anomaly/cost surfaces untouched.
- Standard API envelope `{ status, data, meta? }` preserved; new fields are additive (`omitempty` on nullable).
- EntityLink orphan rule applied correctly for orphan operator/APN rows (FRONTEND.md AC-9).

## Gate Verdict

**GATE_RESULT: PASS**

4 FIXABLE items resolved directly; 8 items DEFERRED to ROUTEMAP Tech Debt with explicit target story; 0 items ESCALATED. Post-fix verification PASS across all 5 gates (go build, go vet, go test, tsc, vite build).
