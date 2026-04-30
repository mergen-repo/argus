# FIX-220 Gate Scout — Analysis Pass

Scope: `internal/store/usage_analytics.go`, `internal/api/analytics/handler.go`, `web/src/pages/dashboard/analytics.tsx`, `web/src/components/analytics/two-way-traffic.tsx`, `web/src/components/analytics/usage-chart-tooltip.tsx`, `web/src/types/analytics.ts`, `web/src/lib/format.ts`. Out-of-scope: `analytics-cost.tsx`, `analytics-anomalies.tsx`.

<SCOUT-ANALYSIS-FINDINGS>

## Inventories

### Field Inventory
| Field | Source | Store | DTO/API | FE Type | FE Render |
|-------|--------|-------|---------|---------|-----------|
| iccid | AC-1 | TopConsumer.ICCID | topConsumerDTO.ICCID | TopConsumer.iccid | EntityLink label |
| imsi | AC-1 | TopConsumer.IMSI | topConsumerDTO.IMSI | TopConsumer.imsi | `.slice(-7)` mono cell |
| msisdn | AC-1 | TopConsumer.MSISDN *string | topConsumerDTO.MSISDN omitempty | TopConsumer.msisdn? | mono cell, "—" when null |
| operator_id | AC-1, F-31 | TopConsumer.OperatorID *uuid | topConsumerDTO.OperatorID omitempty | TopConsumer.operator_id? | EntityLink entity |
| operator_name | AC-1 | (handler enrichTopConsumer) | topConsumerDTO.OperatorName | TopConsumer.operator_name? | EntityLink label |
| apn_id | AC-1, F-31 | TopConsumer.APNID *uuid | topConsumerDTO.APNID omitempty | TopConsumer.apn_id? | EntityLink entity |
| apn_name | AC-1 | (handler enrichTopConsumer) | topConsumerDTO.APNName | TopConsumer.apn_name? | EntityLink label |
| bytes_in | AC-1, AC-2 | TopConsumer.BytesIn, UsageTimePoint.BytesIn | topConsumerDTO.BytesIn, timeSeriesDTO.BytesIn | TopConsumer.bytes_in, TimeSeriesPoint.bytes_in | TwoWayTraffic |
| bytes_out | AC-1, AC-2 | TopConsumer.BytesOut, UsageTimePoint.BytesOut | topConsumerDTO.BytesOut, timeSeriesDTO.BytesOut | TopConsumer.bytes_out, TimeSeriesPoint.bytes_out | TwoWayTraffic |
| total_bytes | AC-1 | existing | existing | existing | formatBytes |
| sessions | AC-3 | existing | existing | existing | formatNumber |
| avg_duration_sec | AC-3 | TopConsumer.AvgDurationSec *float64 | topConsumerDTO.AvgDurationSec omitempty | TopConsumer.avg_duration_sec? | formatDuration, "—" when null |
| unique_sims | AC-4 | UsageTimePoint.UniqueSims | timeSeriesDTO.UniqueSims | TimeSeriesPoint.unique_sims | hidden when 0 in tooltip |

### Endpoint Inventory
| Method | Path | Source | Impl Status |
|--------|------|--------|-------------|
| GET | /api/v1/analytics/usage | existing AC-1..13 | extended — all new fields populate in DTO loop (handler.go:310-357). envelope preserved. |

### Workflow Inventory
| AC | Step | Chain Status |
|----|------|--------------|
| AC-1 | User loads /analytics, sees Top Consumers row | OK — store JOIN sims, enrich overrides from live SIM, EntityLink renders columns |
| AC-2 | Bytes rendered human-readable | OK — `formatBytes` via TwoWayTraffic + total cell |
| AC-3 | Sessions + Avg Duration columns | OK — present; Avg Duration uses formatDuration, "—" when nil |
| AC-4 | Hover on chart point → rich tooltip | OK for cdrs path; degraded (no unique SIMs) for hourly/daily aggregated paths (documented) |
| AC-5 | Tooltip keyboard/aria | PARTIAL — `role="tooltip"` set on panel, but recharts shows on hover only (no focus trigger); keyboard focus not wired |
| AC-6 | Delta capping rules | OK — `formatDeltaPct` covers all 7 branches |
| AC-7 | Color coding via polarity | OK — TONE_CLASS map applied |
| AC-8 | Title Case headers | OK — `humanizeGroupDim` replaces `.capitalize + underscore` |
| AC-9 | Enum humanization (lte_m → LTE-M) | OK in breakdown + tooltip via `humanizeRatType`; chart legend uses `resolveGroupLabel` → humanizer applied |
| AC-10 | Actionable empty state | PARTIAL — copy uses a date RANGE (`from – to`), story AC specifies "2026-04-01 to 2026-04-19" date style is fine; actionable hint present; see F-A6 |
| AC-11 | Group-by zero-groups msg | OK — inline message inside chart card at lines 429-433 |
| AC-12 | group_by=apn | OK (pre-existing FIX-204 fix) |
| AC-13 | group_by variants | OK — `resolveGroupLabel` handles each |
| AC-14 | Export CSV | DEFERRED to FIX-236 (documented) — PNG export (FIX-234) preserved |

### UI Component Inventory
| Component | Location | Arch Ref | Impl Status |
|-----------|----------|----------|-------------|
| Card/CardHeader/CardContent/CardTitle | KPI, chart, tables | @/components/ui/card | OK |
| Table/TableHeader/TableBody/TableRow/TableHead/TableCell | Top Consumers | @/components/ui/table | OK |
| EntityLink | ICCID/Operator/APN cells | @/components/shared/entity-link | OK (F-31 fulfilled) |
| TwoWayTraffic (new) | Table row + chart tooltip | @/components/analytics/two-way-traffic | OK |
| UsageChartTooltip (new) | Recharts content | @/components/analytics/usage-chart-tooltip | OK |
| Skeleton | Loading | @/components/ui/skeleton | OK |
| AnimatedCounter | KPI values | @/components/ui/animated-counter | OK |
| TimeframeSelector | Header | @/components/ui/timeframe-selector | OK (untouched) |
| DropdownMenu | PillFilter | @/components/ui/dropdown-menu | OK |
| Tooltip (shared) | TwoWayTraffic hover | @/components/ui/tooltip | OK (string content only — limitation; no a11y focus) |

### AC Summary
| # | Criterion | Status | Gaps |
|---|-----------|--------|------|
| AC-1 | Columns incl MSISDN/IMSI/IN/OUT | PASS | — |
| AC-2 | formatBytes on byte cells | PASS | — |
| AC-3 | Sessions + Avg Duration | PASS | — |
| AC-4 | Rich tooltip | PASS | unique_sims omitted for cdrs_hourly/cdrs_daily paths — documented |
| AC-5 | Accessible tooltip | PARTIAL | only `role="tooltip"` on panel; no keyboard activation; recharts tooltip is mouseover-only (see F-A4) |
| AC-6 | Delta cap rules | PASS | — |
| AC-7 | Color coding | PASS | — |
| AC-8 | Title Case headers | PASS | — |
| AC-9 | Enum humanization | PASS | Breakdown double-applies humanizer — no-op but redundant (see F-A5) |
| AC-10 | Empty state actionable | PARTIAL | hint is generic ("active filter") not named ("Operator/APN/RAT") — see F-A6 |
| AC-11 | Group zero-groups | PASS | — |
| AC-12 | group_by=apn | PASS (FIX-204) | — |
| AC-13 | 3 group_by variants | PASS | — |
| AC-14 | Export CSV | DEFERRED | FIX-236 (acknowledged) |

## Findings

### F-A1 | MEDIUM | gap
- Title: Live SIM enrichment unconditionally overrides store-joined MSISDN/IMSI with blank when SIM row missing fields.
- Location: `internal/api/analytics/handler.go:384-392`
- Description: `enrichTopConsumer` executes `dto.ICCID = sim.ICCID; dto.IMSI = sim.IMSI; dto.OperatorID = sim.OperatorID.String()` unconditionally. If the live `sim_store.GetByID` succeeds but the live `Sim.IMSI`/`ICCID` values are empty strings (edge case: SIM row exists but fields were cleared), the store-joined values already set at handler.go:338-352 get overwritten with blanks. Same risk for MSISDN: the guarded `if sim.MSISDN != nil && *sim.MSISDN != ""` preserves a prior MSISDN, but the preceding line 385 writes `dto.IMSI = sim.IMSI` without a non-empty guard. Inconsistent with MSISDN handling a few lines below.
- Fixable: YES
- Suggested fix: Apply the same non-empty guard to ICCID/IMSI/OperatorID — only override when the live value is non-empty/non-nil. Alternatively skip the override altogether since the store JOIN already fetched the canonical value (simpler, and removes a whole sim GET per top-consumer row — see F-A9 perf).

### F-A2 | MEDIUM | performance
- Title: N+1 pattern — `enrichTopConsumer` issues up to 20 × (GetByID sim + GetByID operator + GetByID apn + GetIPAddressByID) round-trips per /analytics/usage call.
- Location: `internal/api/analytics/handler.go:353-357, 379-421`
- Description: Top consumers loop calls `h.enrichTopConsumer` which does 1..4 separate DB round-trips per row (sim, operator, apn, ip). For limit=20 that is up to 80 queries on top of the store's JOIN. The JOIN in `GetTopConsumers` already returns `s.iccid, s.imsi, s.msisdn, s.operator_id, s.apn_id` — the only truly missing bits are `operator.name`, `apn.display_name/name`, and optional `ip_address`. Risk: response latency spikes, especially under tenants with larger top-consumer fan-out or cache miss.
- Fixable: YES
- Suggested fix: Either (a) batch-fetch operators and APNs by the union of IDs in a single `IN (...)` query and build local maps (preferred; simplest pattern in `cost_analytics.go` cost-per-mb flow), or (b) extend the store JOIN to include `operators.name` and `apns.name/display_name` directly. Plan Risk R2 anticipated this; Team Lead may elect to keep as follow-up if 20-row LIMIT is acceptable today, but the finding must be flagged.

### F-A3 | MEDIUM | compliance
- Title: `<Tooltip>` wrapper only accepts `content: string`; TwoWayTraffic is a fine use but the shared primitive is not accessible (no `role`, no `aria-describedby`, mouse-only).
- Location: `web/src/components/ui/tooltip.tsx:1-42`, used in `two-way-traffic.tsx:22`
- Description: The shared Tooltip atom renders a div with no `role="tooltip"`, no `aria-*` wiring, and shows only on `onMouseEnter`/`Leave` — no focus handlers. This is a pre-existing limitation but propagates into FIX-220's new TwoWayTraffic atom and partially affects AC-5 (accessibility). Not a regression from this story, but flagged because the story adds a new consumer.
- Fixable: YES (scope-limited)
- Suggested fix: Add `onFocus`/`onBlur` handlers and `role="tooltip"` + `aria-describedby` to the shared `Tooltip` atom — or scope the fix to this story by using shadcn's Radix tooltip primitive for TwoWayTraffic. Team Lead decides whether to widen scope or defer to a FE a11y follow-up FIX.

### F-A4 | MEDIUM | gap
- Title: AC-5 "tooltip keyboard focusable" not satisfied — recharts tooltip is pointer-only.
- Location: `web/src/components/analytics/usage-chart-tooltip.tsx:55, 104`
- Description: `role="tooltip"` is applied to the panel, but recharts `<Tooltip content={...} />` fires only on cursor hover over data points. Keyboard users cannot activate per-bucket data reveal. AC-5 requires "aria, keyboard focusable." This is a product of recharts — not a writing bug, but the AC is not technically met.
- Fixable: YES (architectural trade-off)
- Suggested fix: Either (a) add a data table fallback (visually hidden, screen-reader accessible) listing per-bucket values, (b) add an on-chart data-point focus handler (recharts supports `onMouseEnter` per-dot; `tabIndex` + keyboard nav would need custom Area layer), or (c) down-scope AC-5 to "role=tooltip + non-decorative semantics" and mark keyboard access as follow-up. Recommend (a) — cheapest and satisfies WCAG without re-engineering recharts.

### F-A5 | LOW | compliance
- Title: Redundant double-humanization in RAT breakdown row.
- Location: `web/src/pages/dashboard/analytics.tsx:591-593`
- Description: `dim === 'rat_type' ? humanizeRatType(resolveGroupLabel(dim, item.key)) : resolveGroupLabel(dim, item.key)`. `resolveGroupLabel` at line 94 already invokes `humanizeRatType(key)` when `groupBy === 'rat_type'`. Outer `humanizeRatType` is therefore applied to an already-humanized string (e.g., `humanizeRatType('LTE-M')` → `map['LTE-M'] ?? 'LTE-M'.toUpperCase()` → `'LTE-M'`). No-op in practice but code-smell and breaks if keys change.
- Fixable: YES
- Suggested fix: Remove the outer `humanizeRatType` wrap — `resolveGroupLabel(dim, item.key)` already does the right thing. Keeps a single source-of-truth.

### F-A6 | LOW | gap
- Title: AC-10 empty state hint not filter-specific.
- Location: `web/src/pages/dashboard/analytics.tsx:162-175`
- Description: When `hasFilter` is true the hint says "Try expanding the date range or clearing the active filter" — it doesn't name which filter (Operator / APN / RAT). Story AC-10 example: "clearing the Operator filter". Minor UX polish.
- Fixable: YES
- Suggested fix: Pass concrete `operatorId`/`apnId`/`ratType` truthy list and format: e.g. `"clearing the {Operator, APN} filter"` (list of active names). Low-cost, already have the state in parent scope.

### F-A7 | LOW | compliance
- Title: `deltaPercent` helper in handler.go duplicates frontend logic with different semantics.
- Location: `internal/api/analytics/handler.go:458-466`
- Description: Backend `deltaPercent(current, previous)` returns `100.0` when `previous===0 && current>0` — does NOT cap at 999% and does NOT implement the "↑ icon only" branch. Frontend `formatDeltaPct` ignores the backend value entirely (KPI cards pass `current + previous`, lines 347, 360, 373, 393). Dead-code-ish path: the returned `bytes_delta_pct` / `sessions_delta_pct` etc. are never read by the analytics page today.
- Fixable: YES (low severity — no visible bug)
- Suggested fix: Either drop the `XxxDelta float64` fields from `comparisonDTO` (the FE uses `previous_totals` directly) or align semantics. Risk of breaking other consumers — grep showed no FE reader for `bytes_delta_pct` in the analytics page, but cost/anomaly pages (out of scope) may. Recommend leaving as-is with a code comment noting the FE bypass; remove in a dedicated cleanup FIX.

### F-A8 | LOW | performance
- Title: `chartData` `bucketMap` key collisions when time_series carries per-group rows.
- Location: `web/src/pages/dashboard/analytics.tsx:256-276`
- Description: The grouped branch uses `p.ts` as the map key and pivots per-group values into a single object. Each `p` row's `bytes_in`/`bytes_out` (new fields) are NOT propagated into the pivoted bucket — only `[p.group_key] = p[metric]`. That's correct for the stacked Area chart, but UsageChartTooltip's non-grouped branch (line 39-89) looks up `allData.find((p) => p.ts === ts)` against the raw `data?.time_series` (not the pivoted `chartData`), so the grouped path falls through to the grouped-payload branch where `bytes_in`/`bytes_out` aren't used. No bug — but a reviewer needs to understand that TwoWayTraffic is only shown in non-grouped tooltip, consistent with spec. Low-severity heads-up.
- Fixable: n/a (not a bug)
- Suggested fix: None; optionally add a comment in `usage-chart-tooltip.tsx` explaining the two code paths.

### F-A9 | MEDIUM | performance
- Title: `GetTopConsumers` JOIN path still fine but `GROUP BY` includes every non-aggregate column; scalability beyond LIMIT=20.
- Location: `internal/store/usage_analytics.go:352-370`
- Description: SELECT includes `s.iccid, s.imsi, s.msisdn, s.operator_id, s.apn_id`, so GROUP BY must list all of them. With `LIMIT 20` and an indexed `c.sim_id`, plan stays hash-aggregate bounded. At scale (10M SIMs + 100M CDRs/day), Plan Risk R2 anticipated this and suggested falling back to post-query enrichment. Current impl is acceptable for FIX-220's scope. Flag as observable, not a blocker.
- Fixable: YES (future work)
- Suggested fix: Add a measurement in the D-091 follow-up; if p95 of `GetTopConsumers` exceeds a threshold (e.g., 500ms at 10M SIMs), migrate to post-query enrichment (Task 1 is explicit: Developer may choose either approach). No immediate change.

### F-A10 | LOW | compliance
- Title: `bytes_in=0 && bytes_out=0` edge for aggregated `cdrs_daily` rows will render "—" in TwoWayTraffic, which conflates "aggregate view limitation" with "no traffic".
- Location: `web/src/components/analytics/two-way-traffic.tsx:14-16`
- Description: For 30d period, `buildTimeSeriesQuery` returns `bytes_in=0, bytes_out=0` in the cdrs_daily branch because the view lacks per-direction columns (documented choice). If TwoWayTraffic were ever used in that tooltip path, it would show "—". Today it isn't (UsageChartTooltip shows TwoWayTraffic only in non-grouped branch, and non-grouped tooltip receives cdrs/cdrs_hourly full-fidelity fields). Still — the limitation should be documented in UsageChartTooltip or hide the TwoWayTraffic row for aggregated-only periods.
- Fixable: YES (cosmetic)
- Suggested fix: In UsageChartTooltip non-grouped branch, when `curr.bytes_in === 0 && curr.bytes_out === 0 && curr.total_bytes > 0`, render the Total row but skip TwoWayTraffic (or show "IN/OUT: aggregated"). Minor polish.

### F-A11 | LOW | security
- Title: No new OWASP patterns introduced.
- Location: n/a
- Description: Grep for SQL string concat (`query ... + req`), `dangerouslySetInnerHTML`, path traversal, hardcoded secrets, `Math.random()`, CORS wildcard in scope files: all clean. Store uses parameterized `$N` placeholders throughout. DTO field additions are strings (safe). EntityLink preserves existing XSS-safe text rendering. No changes to auth/access-control layer.
- Fixable: n/a
- Suggested fix: None.

### F-A12 | LOW | compliance
- Title: Plan-excluded `apn_id/rat_type` filter no-op guard on `cdrs_daily` is outdated.
- Location: `internal/store/usage_analytics.go:187-191`
- Description: `buildTimeSeriesQuery` skips `apn_id` and `rat_type` WHERE clauses when `AggregateView == "cdrs_daily"`. Migration `20260323000003_cdrs_daily_dimensions.up.sql` added both columns to `cdrs_daily`. The guard silently drops user-supplied filters on 30d queries — returning unfiltered data. Pre-existing condition (not introduced by FIX-220), but co-located with the FIX-220 query changes. Worth flagging because a data-correctness bug hiding here may embarrass FIX-220 in UAT.
- Fixable: YES
- Suggested fix: Remove the `&& spec.AggregateView != "cdrs_daily"` conjunct on both lines 187 and 192. Verify via integration test that 30d + apn_id filter returns filtered time series. Optional: do in a separate bugfix story (e.g. piggyback on FIX-220 as a Pass-2 compliance fix).

## Non-Fixable (Escalate)

(none)

## Performance Summary

### Queries Analyzed
| # | File:Line | Pattern | Issue | Severity |
|---|-----------|---------|-------|----------|
| 1 | store/usage_analytics.go:352-370 | cdrs JOIN sims (GROUP BY) | Acceptable at LIMIT=20; scale risk logged | MEDIUM |
| 2 | store/usage_analytics.go:127-204 | Time-series dynamic SELECT | Uses per-period view; OK | LOW |
| 3 | store/usage_analytics.go:232-269 | GetTotals(cdrs) | Full scan bounded by `timestamp BETWEEN` + tenant_id; OK | LOW |
| 4 | store/usage_analytics.go:187,192 | `cdrs_daily` filter guards drop apn/rat filter | Data-correctness regression; pre-existing | MEDIUM (F-A12) |
| 5 | api/analytics/handler.go:353-357 → 379-421 | enrichTopConsumer N+1 | Up to 80 DB round-trips per /usage call | MEDIUM (F-A2) |
| 6 | api/analytics/handler.go:226-270 | GetBreakdowns × 3 dims | 3 separate roundtrips; bounded (LIMIT 50 each); OK | LOW |

### Caching Verdicts
| # | Data | Location | TTL | Decision |
|---|------|----------|-----|----------|
| 1 | operator name by id | in-memory LRU (future) | 5m | SKIP for FIX-220 — hot path but scope-narrow; flag for FIX follow-up tied to F-A2 |
| 2 | apn name/display_name by (tenant,id) | in-memory LRU (future) | 5m | SKIP as above |
| 3 | top_consumers query result | Redis | 30s | SKIP — data freshness matters; tenant-scoped cache would add complexity; defer |
| 4 | cdrs_hourly / cdrs_daily rows | DB-side continuous aggregates | TimescaleDB policy | CACHE (already in place via materialized view refresh policies) |

</SCOUT-ANALYSIS-FINDINGS>
