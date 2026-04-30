# Implementation Plan: FIX-220 — Analytics Polish (MSISDN, IN/OUT Split, Tooltip, Delta Cap, Capitalization)

## Goal
Polish the Usage Analytics page (`/analytics`) so the Per-SIM breakdown exposes MSISDN + IMSI + IN/OUT byte split, the Traffic Over Time chart tooltip is information-rich, delta % values are capped/humanized, and breakdown labels/empty-states read cleanly.

## Scope Discipline
- **In scope:** `web/src/pages/dashboard/analytics.tsx` + its backing DTOs/queries (`internal/api/analytics/handler.go`, `internal/store/usage_analytics.go`).
- **Out of scope (flag as follow-up FIX, do not touch here):**
  - `analytics-cost.tsx` (has its own `DeltaBadge` and `TopExpensiveSIM` — same delta-cap bug but NOT in this story's ACs).
  - `analytics-anomalies.tsx`.
  - Backend anomaly / cost surfaces.
- **AC-14 Export CSV is DEFERRED.** FIX-236 (10M-scale readiness / streaming export pattern) has a story file but has NOT shipped. AC-14 will be re-addressed inside FIX-236 or as a follow-up. Noted in Risks.
- **Unit tests DEFERRED to D-091** per dispatch directive. No test tasks in this plan; a Test Plan section records the deferral.

## Findings Addressed (corrected mapping — story file had mislabels)
| Story says | Actual review ID | Summary |
|-----------|-------------------|---------|
| F-23 (MSISDN missing) | **F-29** | MSISDN/IMSI columns missing from Top Consumers |
| F-26 (IN/OUT merged) | **F-30** | Top Consumers Usage is single number; IN/OUT split required |
| F-29 (tooltip sparse) | **F-32** | Traffic Over Time chart tooltip has no rich context |
| F-30 (delta uncapped) | **F-23** | KPI deltas show +19221.6%, unreadable |
| F-32 (capitalization) | **F-26** | "apn Breakdown" lowercase header |
| F-33 (formatting) | **F-33** | ICCID hash-like; no readable identifier |
| Additional (F-25) | **F-25** | `unique_sims = 0` per time_series bucket (required for AC-4 multi-series tooltip) |
| Additional (F-31) | **F-31** | Operator / APN cells not clickable — satisfied via EntityLink |

## Architecture Context

### Components Involved
- **Backend (Go)**
  - `internal/store/usage_analytics.go` — `TopConsumer` struct, `GetTopConsumers`, `GetTimeSeries`, `buildTimeSeriesQuery`.
  - `internal/api/analytics/handler.go` — `topConsumerDTO`, `timeSeriesDTO`, `GetUsage` handler, `enrichTopConsumer` helper.
  - Model source: `internal/store/sim.go` — `Sim.MSISDN *string`, `Sim.IMSI string`.
- **Frontend (React)**
  - `web/src/pages/dashboard/analytics.tsx` — page component (KPIs, chart, Top Consumers, Breakdowns).
  - `web/src/types/analytics.ts` — `TopConsumer`, `TimeSeriesPoint`, `UsageComparison` TS types.
  - `web/src/lib/format.ts` — shared formatters (`formatBytes`, `formatNumber`).
  - `web/src/components/shared/entity-link.tsx` — existing EntityLink component (FIX-219).
  - New: `web/src/lib/delta.ts` (or extension of `format.ts`) — `formatDeltaPct` with cap.
  - New: `web/src/lib/rat.ts` (or addition to format.ts) — `humanizeRatType`, `humanizeGroupDim`.
  - New: `web/src/components/analytics/two-way-traffic.tsx` — `<TwoWayTraffic in out />`.
  - New: `web/src/components/analytics/usage-chart-tooltip.tsx` — custom recharts `content` component.

### Data Flow (per request)
1. User opens `/analytics` or changes a filter chip.
2. `useUsageAnalytics(filters)` calls `GET /api/v1/analytics/usage?period=...&group_by=...&compare=true`.
3. Handler orchestrates: `GetTimeSeries`, `GetTotals`, `GetBreakdowns` (x3 dims), `GetTopConsumers(20)`, optional previous-period `GetTotals` for comparison.
4. `enrichTopConsumer` joins each sim_id to `sim` / `operator` / `apn` / `ip_address` — MSISDN/IMSI added in this FIX.
5. Response returned under standard envelope `{ status, data, meta? }`.
6. FE: `chartData` bucketed; custom tooltip reads the current bucket point; `TwoWayTraffic` renders bytes_in/bytes_out side-by-side.

### API Specification — existing endpoint, additive fields only
`GET /api/v1/analytics/usage?period={1h|24h|7d|30d|custom}&group_by={operator|apn|rat_type}&operator_id=&apn_id=&rat_type=&compare=true`

Response `data` object (only fields that CHANGE in this FIX):

```json
{
  "time_series": [
    {
      "ts": "2026-04-22T10:00:00Z",
      "total_bytes": 12345,
      "bytes_in": 10000,          // NEW — AC-4
      "bytes_out": 2345,           // NEW — AC-4
      "sessions": 12,
      "auths": 12,
      "unique_sims": 8,            // FIX: currently always 0 per bucket (F-25)
      "group_key": "Turkcell"
    }
  ],
  "top_consumers": [
    {
      "sim_id": "uuid",
      "iccid": "8990...",
      "imsi": "2860...",           // NEW — AC-1 / F-29
      "msisdn": "+9053...",        // NEW — AC-1 / F-29 (nullable, may be omitted)
      "operator_id": "uuid",       // NEW — needed for EntityLink (F-31)
      "operator_name": "Turkcell",
      "apn_id": "uuid",            // NEW — needed for EntityLink (F-31)
      "apn_name": "iot.m2m",
      "ip_address": "10.0.0.42",
      "bytes_in": 10000,           // NEW — AC-1 / F-30
      "bytes_out": 2345,           // NEW — AC-1 / F-30
      "total_bytes": 12345,
      "sessions": 12,
      "avg_duration_sec": 320      // NEW — AC-3 (nullable)
    }
  ]
}
```

Empty strings / null operator/apn IDs are allowed (orphan SIM — render `—` via EntityLink orphan rule per FRONTEND.md AC-9).

### Database Schema (ACTUAL — no migration needed)

> Source: `internal/store/sim.go` + existing CDR column layout observed in `internal/store/usage_analytics.go`. No schema change in this FIX.

- `sims` table (via `sim.go`): `id`, `tenant_id`, `operator_id`, `apn_id`, `iccid`, `imsi`, `msisdn`, ... — all three identity columns already exist.
- `cdrs` / `cdrs_hourly` / `cdrs_daily` tables: `bytes_in bigint`, `bytes_out bigint`, `duration_sec` — referenced throughout `cost_analytics.go` and `usage_analytics.go`. No migration.
- `unique_sims` fix for `GetTimeSeries`: SQL already selects `COUNT(DISTINCT sim_id) AS unique_sims` per bucket (see line 126 & 135 of `usage_analytics.go`). If the returned value is 0, it is likely a scanning/column-order mismatch in the aggregator — the Developer must verify with `psql` + fix either SQL or Scan target. If `cdrs_hourly`/`cdrs_daily` materialized views only carry pre-aggregated totals without SIM id, per-bucket distinct SIM cannot be recomputed — in that case the Developer MUST return the existing per-bucket value from the `cdrs` path (period `1h` bucket = `1 minute` already aggregates `cdrs` directly) and, for hourly/daily views, fall back to `NULL`/omit the field (AC-4 tolerates missing "distinct SIMs" line — display nothing when null). Document chosen approach in a DEV decision.

### Screen Context — ASCII

```
┌────────────────────────────────────────────────────────────────────────┐
│ Analytics — Usage                                   [TimeframeSelector]│
├────────────────────────────────────────────────────────────────────────┤
│ [Group:APN] [Metric:Bytes] [Operator] [APN] [RAT]                      │
├────────────────────────────────────────────────────────────────────────┤
│ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐                                    │
│ │Bytes │ │Sessi │ │Auths │ │Uniq  │   ← KPI cards w/ capped DeltaBadge │
│ │12 GB │ │ 458  │ │ 50   │ │ 26   │                                    │
│ │↑42.5%│ │>999% │ │—     │ │+3.1% │                                    │
│ └──────┘ └──────┘ └──────┘ └──────┘                                    │
├────────────────────────────────────────────────────────────────────────┤
│ Traffic Over Time                                             [Export] │
│   [Area chart with custom tooltip]                                      │
│   ┌──────────────────────────────────────────┐                         │
│   │ Mon 14:00 (2h ago)                       │ ← hover tooltip         │
│   │ ↓ 8.2 GB  ↑ 320 MB                       │                         │
│   │ Total 8.5 GB   Δ +12.4%                  │                         │
│   │ Sessions 42   Auths 42   Unique SIMs 28  │                         │
│   │ Top: Turkcell — 320 sessions             │ ← when group_by active  │
│   └──────────────────────────────────────────┘                         │
├────────────────────────────────────────────────────────────────────────┤
│ Top Consumers                                                           │
│ # | ICCID | IMSI | MSISDN | Operator | APN | IN | OUT | Total | Sess.. │
│ 1 | …7016 | 286… | +9053… | Turkcell | iot | 10G| 2G  | 12 GB |  42    │
│ 2 | …8044 |  —   |   —    | Vodafone |  —  |  8G| 1G  |  9 GB |  36    │
├────────────────────────────────────────────────────────────────────────┤
│ APN Breakdown        Operator Breakdown       RAT Type Breakdown       │
│   (Title-Cased!)        (Title-Cased!)           (Title-Cased!)        │
└────────────────────────────────────────────────────────────────────────┘
```

Empty state copy (AC-10):
> "No data for the selected filter ({from} to {to}). Try expanding the date range or clearing the Operator filter."

Group-by empty state (AC-11):
> "No groupings found — all values in '__unassigned__' bucket. Configure APN mappings to see breakdown."

### Design Token Map (FRONTEND.md reference — use ONLY these class names)

#### Color Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-white`, `text-gray-900` |
| Secondary text | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-500` |
| Tertiary / muted | `text-text-tertiary` | `text-[#4A4A65]`, `text-gray-400` |
| Accent (links, positive metric) | `text-accent` / `bg-accent` / `bg-accent-dim` | `text-blue-500`, `bg-blue-500/20` |
| Success (delta ↑ when metric good, IN arrow) | `text-success` / `bg-success-dim` | `text-green-500` |
| Danger (delta ↓ when metric good) | `text-danger` / `bg-danger-dim` | `text-red-500` |
| Warning | `text-warning` / `bg-warning-dim` | `text-yellow-500` |
| Info / OUT arrow | `text-info` / `bg-info-dim` | `text-cyan-500` |
| Card background | `bg-bg-surface` | `bg-white`, `bg-[#1a1a2e]` |
| Tooltip / elevated bg | `bg-bg-elevated` | `bg-black/80`, `bg-gray-900` |
| Divider / border | `border-border` / `border-border/50` | `border-gray-200`, `border-[#e2e8f0]` |

Chart series color vars (already used in analytics.tsx, keep as-is):
- `var(--color-accent)`, `var(--color-success)`, `var(--color-warning)`, `var(--color-purple)`, `var(--color-danger)`, `var(--color-cyan)`, `var(--color-info)`, `var(--color-orange)`.

#### Typography Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page title | `text-[16px] font-semibold text-text-primary` (established in analytics.tsx — keep) | `text-2xl`, `text-[24px]` |
| KPI metric value | `font-mono text-[22px] font-bold text-text-primary` (established) | `text-xl`, `text-[24px]` |
| KPI label | `text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium` (established) | `text-xs text-gray-500` |
| Table body text | `text-xs text-text-secondary` / `text-xs font-mono` for IDs | `text-sm`, `text-[13px]` |
| Tooltip content text | `text-xs text-text-primary` + `font-mono` for numbers | `text-sm text-white` |
| Delta badge | `text-xs font-mono` | `text-[11px]` |

#### Spacing / Elevation
| Usage | Token Class |
|-------|-------------|
| Card | `rounded-xl` + existing `<Card>` (shadcn) |
| Tooltip border-radius | `rounded-md` (matches existing `var(--radius-sm)` in `tooltipStyle` const) |
| Section gap | `space-y-4` (established) |

#### Existing Components to REUSE (DO NOT recreate, NEVER raw HTML)
| Component | Path | Use For |
|-----------|------|---------|
| `<Card>`, `<CardHeader>`, `<CardContent>`, `<CardTitle>` | `web/src/components/ui/card` | KPI / chart / table container — ALWAYS |
| `<Table>`, `<TableHeader>`, `<TableBody>`, `<TableRow>`, `<TableHead>`, `<TableCell>` | `web/src/components/ui/table` | Top Consumers table — ALWAYS, never raw `<table>` |
| `<Button>` | `web/src/components/ui/button` | Retry/Refresh/Export icon buttons |
| `<Skeleton>` | `web/src/components/ui/skeleton` | Loading placeholders |
| `<EntityLink>` | `web/src/components/shared/entity-link.tsx` | ICCID cell → sim link; operator cell → operator link; APN cell → APN link (F-31). Orphan rule: when entityId empty, renders "—" |
| `<TimeframeSelector>` | `web/src/components/ui/timeframe-selector.tsx` | Already in use — do not touch |
| `<AnimatedCounter>` | `web/src/components/ui/animated-counter` | KPI numbers — already in use |
| `formatBytes`, `formatNumber`, `formatDuration` | `web/src/lib/format.ts` | Numeric formatting — always prefer these over ad-hoc |

New components/helpers introduced in this FIX:
| Component | Path | Purpose |
|-----------|------|---------|
| `formatDeltaPct(current, previous)` | `web/src/lib/format.ts` (extend existing) | Returns `{ text: string, tone: 'positive'|'negative'|'neutral'|'null' }` following AC-6 rules |
| `humanizeRatType`, `humanizeGroupDim` | `web/src/lib/format.ts` (extend) | `lte_m → 'LTE-M'`, `nr_5g → '5G NR'`, `apn → 'APN'`, `rat_type → 'RAT Type'`, `operator → 'Operator'` |
| `<TwoWayTraffic in={} out={} />` | `web/src/components/analytics/two-way-traffic.tsx` | Renders `↓{formatBytes(in)} ↑{formatBytes(out)}`; down arrow in `text-success`, up arrow in `text-info`; font-mono, inline-flex |
| `<UsageChartTooltip period metric groupBy groupContributors />` | `web/src/components/analytics/usage-chart-tooltip.tsx` | Custom recharts `content={...}` — multi-series when groupBy active, delta vs previous bucket, time absolute+relative, IN/OUT arrows, sessions/auths/uniq_sims rows |

## Delta % Rules (AC-6, AC-7) — canonical table for implementers

| Condition | Display | Tone |
|-----------|---------|------|
| `previous === 0` AND `current > 0` | `"↑"` (icon only, no number) | `neutral` (no color) |
| `previous === 0` AND `current === 0` | `"—"` | `null` |
| `delta === 0` (current === previous) | `"0%"` | `neutral` |
| `-100 <= delta <= 999` | sign + `delta.toFixed(1) + '%'` | positive/negative per sign & metric polarity |
| `delta > 999` | `">999% ↑"` | `positive` (or `negative` for inverse-polarity metrics) |
| `delta < -100` | `"—"` (defensive; impossible mathematically) | `null` |
| `delta` undefined / NaN | `"—"` | `null` |

Tone → color mapping (AC-7):
- Metric polarity `"increase is good"` (bytes, sessions, auths, sims): `positive → text-success`, `negative → text-danger`.
- Metric polarity `"decrease is good"` (errors, cost per MB — not in this FIX, only structure the helper to accept a polarity param for future use): inverse colors. Default polarity = `increase-is-good`. No AC requires inverse right now.
- `neutral` → `text-text-tertiary`; `null` → `text-text-tertiary`.

## Tasks

### Task 1 — Backend: extend `TopConsumer` store struct + query
- **Files:** Modify `internal/store/usage_analytics.go` (1 file)
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/cost_analytics.go` `GetTopExpensiveSIMs` for the established `JOIN sims` pattern used in analytics store.
- **Context refs:** Architecture Context > Components Involved; API Specification; Database Schema.
- **What:**
  - Extend `TopConsumer` struct with `BytesIn int64`, `BytesOut int64`, `AvgDurationSec *int64` (nullable if no duration data), `ICCID string`, `IMSI string`, `MSISDN *string`, `OperatorID *uuid.UUID`, `APNID *uuid.UUID`, `APNName string`, `OperatorName string`, `IPAddress string`.
  - Rewrite `GetTopConsumers` SQL to `SELECT c.sim_id, SUM(bytes_in), SUM(bytes_out), SUM(bytes_in+bytes_out), COUNT(DISTINCT session_id), AVG(duration_sec), s.iccid, s.imsi, s.msisdn, s.operator_id, s.apn_id FROM cdrs c JOIN sims s ON s.id = c.sim_id WHERE ... GROUP BY c.sim_id, s.iccid, s.imsi, s.msisdn, s.operator_id, s.apn_id ORDER BY SUM(bytes_in+bytes_out) DESC LIMIT ...`. Scan into extended struct.
  - Keep the existing `enrichTopConsumer` in handler for operator/apn/ip resolution (don't duplicate in SQL — this preserves tenant-scoped ACL and existing caching surface).
- **Verify:** `go test ./internal/store/...` passes (no test added, but existing suite must not regress). `go build ./...` succeeds.

### Task 2 — Backend: `TimeSeries` bytes_in/bytes_out + unique_sims fix
- **Files:** Modify `internal/store/usage_analytics.go` (same file; same task if edits fit ≤50 lines — split if larger)
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/usage_analytics.go` `buildTimeSeriesQuery` already handles bucket-by-view switching — follow the same SELECT list extension.
- **Context refs:** API Specification; Database Schema (unique_sims note).
- **What:**
  - Extend `UsageTimePoint` struct with `BytesIn int64`, `BytesOut int64`.
  - Update `buildTimeSeriesQuery` SELECT list for both `cdrs` and `cdrs_hourly/daily` branches to include `SUM(bytes_in) AS bytes_in, SUM(bytes_out) AS bytes_out`.
  - Investigate `unique_sims = 0` per bucket (F-25). In `cdrs` path, ensure `COUNT(DISTINCT sim_id)` is actually the column being scanned (check column order vs `Scan(...)` call). For `cdrs_hourly`/`cdrs_daily` aggregate views: if they don't carry `sim_id`, return the per-bucket value as 0 and **document in code comment** that hourly/daily views lack per-bucket distinct-SIM resolution — UI tooltip (AC-4) will hide the "Unique SIMs" line when 0/null. If the aggregate views DO have it (check column schema via `\d cdrs_hourly`), fix the query. Developer: run `psql -c "\d cdrs_hourly"` before choosing.
- **Verify:** `curl localhost:8080/api/v1/analytics/usage?period=1h | jq '.data.time_series[0]'` shows `bytes_in`, `bytes_out` and (for period=1h) non-zero `unique_sims`. Manual check.

### Task 3 — Backend: DTO + handler enrichment for Top Consumers & TimeSeries
- **Files:** Modify `internal/api/analytics/handler.go` (1 file)
- **Depends on:** Task 1, Task 2
- **Complexity:** medium
- **Pattern ref:** Same file — follow existing `enrichTopConsumer` shape; add fields, don't restructure.
- **Context refs:** API Specification; Architecture Context > Data Flow.
- **What:**
  - Extend `topConsumerDTO` with `json:"imsi,omitempty"`, `json:"msisdn,omitempty"`, `json:"bytes_in"`, `json:"bytes_out"`, `json:"avg_duration_sec,omitempty"`, `json:"operator_id,omitempty"`, `json:"apn_id,omitempty"`. Use `omitempty` only for nullable values; bytes_in/bytes_out always emit.
  - Extend `timeSeriesDTO` with `json:"bytes_in"`, `json:"bytes_out"`.
  - In `GetUsage`:
    - Populate `BytesIn/BytesOut/AvgDurationSec` from `TopConsumer` store values directly.
    - In `enrichTopConsumer`: set `dto.IMSI = sim.IMSI`; if `sim.MSISDN != nil && *sim.MSISDN != ""` then `dto.MSISDN = *sim.MSISDN` else leave empty; set `dto.OperatorID = sim.OperatorID.String()`; if `sim.APNID != nil` set `dto.APNID = (*sim.APNID).String()`.
    - Populate `BytesIn/BytesOut` in timeSeriesDTO from the store point struct.
- **Verify:** `curl /api/v1/analytics/usage?period=24h | jq '.data.top_consumers[0] | {iccid, imsi, msisdn, bytes_in, bytes_out, operator_id, apn_id}'` returns all fields. `go build ./...` green.

### Task 4 — Frontend: TS types + shared formatters
- **Files:** Modify `web/src/types/analytics.ts`; Modify `web/src/lib/format.ts` (2 files)
- **Depends on:** Task 3 (types mirror DTO)
- **Complexity:** low
- **Pattern ref:** Existing `web/src/lib/format.ts` formatters — follow same exported-function style (no default export).
- **Context refs:** API Specification; Delta % Rules; Design Token Map > new components/helpers.
- **What:**
  - `TopConsumer`: add `imsi?: string`, `msisdn?: string`, `bytes_in: number`, `bytes_out: number`, `avg_duration_sec?: number`, `operator_id?: string`, `apn_id?: string`.
  - `TimeSeriesPoint`: add `bytes_in: number`, `bytes_out: number`.
  - `format.ts`:
    - `formatDeltaPct(current: number, previous: number, polarity?: 'up-good'|'down-good'): { text: string; tone: 'positive'|'negative'|'neutral'|'null' }` — implement the full table in "Delta % Rules". Default polarity `'up-good'`.
    - `humanizeRatType(rat: string): string` — `{ nb_iot: 'NB-IoT', lte_m: 'LTE-M', lte: 'LTE', nr_5g: '5G NR' }[rat] ?? rat.toUpperCase()`.
    - `humanizeGroupDim(dim: string): string` — `{ apn: 'APN', operator: 'Operator', rat_type: 'RAT Type' }[dim] ?? dim`.
  - **DO NOT** add JSX here — format.ts is logic-only.
- **Verify:** TypeScript compiles (`npm --prefix web run typecheck`).

### Task 5 — Frontend: `<TwoWayTraffic>` presentational atom
- **Files:** Create `web/src/components/analytics/two-way-traffic.tsx` (1 file)
- **Depends on:** Task 4
- **Complexity:** low
- **Pattern ref:** Read `web/src/components/shared/entity-link.tsx` for the minimal atom style (cn + Tailwind + lucide icon). Keep similar shape — no internal state.
- **Context refs:** Design Token Map > Color Tokens; Existing Components to REUSE.
- **What:**
  - Props: `{ in: number; out: number; className?: string }`.
  - Render `<span className="inline-flex items-center gap-2 font-mono text-xs">`:
    - IN group: `<ArrowDown className="h-3 w-3 text-success" />{formatBytes(in)}`.
    - OUT group: `<ArrowUp className="h-3 w-3 text-info" />{formatBytes(out)}`.
  - Use lucide icons `ArrowDown`, `ArrowUp` (already available in project).
  - Tooltip on hover (via existing `<Tooltip>` from `@/components/ui/tooltip`) with full "In: 8.2 GB · Out: 320 MB · Total: 8.5 GB" content.
- **Verify:** Import in a stub page and render — visually identical to ASCII mockup.

### Task 6 — Frontend: custom `<UsageChartTooltip>`
- **Files:** Create `web/src/components/analytics/usage-chart-tooltip.tsx` (1 file)
- **Depends on:** Task 4
- **Complexity:** medium
- **Pattern ref:** recharts `content` prop accepts a React component receiving `{ active, payload, label }`. Follow the pattern used anywhere in the app (grep for `content={` in `web/src/pages/dashboard/*.tsx` before starting; if none, establish pattern).
- **Context refs:** API Specification (TimeSeriesPoint fields); Delta % Rules; Design Token Map.
- **What:**
  - Props: `{ period: string; metric: UsageMetric; groupBy: UsageGroupBy; allData: TimeSeriesPoint[]; groupKeys: string[] }`.
  - Receives recharts render-props `active, payload, label`.
  - When `active && payload?.length`:
    - Header: `{formatTimestamp(ts, period)} ({timeAgo(ts)})`.
    - If `groupBy` inactive: one row with `<TwoWayTraffic in out />`, total, delta vs previous bucket (using `formatDeltaPct(curr.total_bytes, prev.total_bytes)`), sessions, auths, unique_sims (hide row when 0 or null).
    - If `groupBy` active: iterate payload (one per series) → series color dot + name + formatted value; **after** series list, render a "Top: {series with max value} — {formatNumber(value)}" line as the top-contributor hint (AC-4).
  - Wrap in `<div role="tooltip">` (AC-5 accessible). The tooltip itself remains visual-only — recharts positions it; aria is on the chart container.
  - Use `bg-bg-elevated border border-border rounded-md p-2 shadow-lg text-xs text-text-primary font-mono` for the panel.
  - **No hex colors, no arbitrary px values.**
- **Verify:** Mount in `analytics.tsx` via `<Tooltip content={<UsageChartTooltip ... />} />` — hover shows all rows for non-grouped, all series for grouped.

### Task 7 — Frontend: wire it into `analytics.tsx` (delta cap, column set, empty states, capitalization)
- **Files:** Modify `web/src/pages/dashboard/analytics.tsx` (1 file)
- **Depends on:** Task 4, Task 5, Task 6
- **Complexity:** medium
- **Pattern ref:** Self-reference — keep the existing JSX scaffold; add fields and swap components.
- **Context refs:** Screen Context ASCII; Design Token Map; Delta % Rules; Components to REUSE.
- **What:**
  - **`DeltaBadge` replacement (AC-6/7):** Rewrite `DeltaBadge` to accept the previous/current pair (or the existing `delta` number + `previous` for the "↑ when prev=0" branch). Use `formatDeltaPct` and apply tone → class mapping (`text-success` / `text-danger` / `text-text-tertiary`). Render the emitted `text`; when tone `neutral` with icon-only `"↑"`, render `<TrendingUp className="h-3 w-3" />` with no number. KPI cards pass current totals and `data.comparison.previous_totals.*` (already in API).
  - **KPI cards:** update `DeltaBadge` props to pass both current and previous for accurate "prev = 0" detection. (Currently only passes `delta_pct` from backend.)
  - **Top Consumers table columns (AC-1/2/3):** replace with `# | ICCID | IMSI | MSISDN | Operator | APN | IN | OUT | Total | Sessions | Avg Duration`. Use:
    - ICCID cell: `<EntityLink entityType="sim" entityId={tc.sim_id} label={tc.iccid} truncate />` — retain existing.
    - IMSI cell: `<span className="font-mono text-xs text-text-secondary" title={tc.imsi}>{tc.imsi ? tc.imsi.slice(-7) : '—'}</span>`.
    - MSISDN cell: `{tc.msisdn ? <span className="font-mono text-xs">{tc.msisdn}</span> : <span className="text-text-tertiary">—</span>}`.
    - Operator cell (F-31): `<EntityLink entityType="operator" entityId={tc.operator_id ?? ''} label={tc.operator_name ?? ''} />`. Orphan rule auto-renders "—" when both empty.
    - APN cell (F-31): `<EntityLink entityType="apn" entityId={tc.apn_id ?? ''} label={tc.apn_name ?? ''} />`.
    - IN cell: `{formatBytes(tc.bytes_in)}`. OUT cell: `{formatBytes(tc.bytes_out)}`. Total cell: `{formatBytes(tc.total_bytes)}`.
    - Avg Duration cell: `{tc.avg_duration_sec != null ? formatDuration(tc.avg_duration_sec) : '—'}`.
  - **Remove the ip_address column** (not in AC-1; keep row slim — ICCID EntityLink's hover card covers debug need). If Developer disagrees, flag — but AC-1 explicitly enumerates columns.
  - **Capitalization (AC-8, F-26):** replace `<CardTitle className="capitalize">{dim.replace(/_/g, ' ')} Breakdown</CardTitle>` with `<CardTitle>{humanizeGroupDim(dim)} Breakdown</CardTitle>`.
  - **RAT humanization (AC-9):** in breakdown item row + chart legend + tooltip, when `groupBy === 'rat_type'` apply `humanizeRatType(item.key)` before rendering. Also pass through `resolveGroupLabel` for `__unassigned__`.
  - **Empty state (AC-10):** replace `EmptyState`'s hard-coded `"No data for selected period"` with a message that includes the current `from`/`to` (pass `data?.from`, `data?.to` or the computed window) and an actionable suggestion. New props: `{ from?: string; to?: string; hasFilter: boolean }`. When any of `operatorId/apnId/ratType` active → "Try expanding the date range or clearing the {Active Filter}". Else → "Try expanding the date range."
  - **Group-by empty state (AC-11):** when `groupBy` active and `groupKeys.length === 0`, render the "No groupings found — all values in '__unassigned__' bucket. Configure APN mappings to see breakdown." message inside the chart card.
  - **Chart tooltip:** replace the existing `<Tooltip contentStyle={tooltipStyle} formatter={...} />` with `<Tooltip content={<UsageChartTooltip period={period} metric={metric} groupBy={groupBy} allData={data?.time_series ?? []} groupKeys={groupKeys} />} />`.
  - **AC-14 Export CSV:** mark as deferred via an inline code comment `// FIX-220: Export CSV deferred to FIX-236 streaming pattern — no button rendered.` Do not add a non-functional button.
- **Tokens:** Use ONLY classes from Design Token Map — zero hardcoded hex/px. Run `grep -nE '#[0-9a-fA-F]{3,6}|\btext-\[[0-9]+px\]|\bp-\[[0-9]+px\]' web/src/pages/dashboard/analytics.tsx web/src/components/analytics/*.tsx` — must return ZERO matches.
- **Components:** Reuse atoms/molecules from Component Reuse table — NEVER raw `<button>`, `<table>`, `<input>` elements.
- **Note:** Invoke `frontend-design` skill before writing JSX for the tooltip + table redesign — professional polish required.
- **Verify:**
  - `npm --prefix web run typecheck` passes.
  - Hot-reload `/analytics`: Top Consumers shows all 10 columns; tooltip on chart hover is rich; all 3 breakdown cards use Title Case; empty state has the new copy; delta badge never exceeds ">999%".
  - `grep -nE '#[0-9a-fA-F]{3,6}' web/src/pages/dashboard/analytics.tsx web/src/components/analytics/*.tsx` → zero matches.

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|----------------|-------------|
| AC-1 Per-SIM columns (ICCID, IMSI, MSISDN, Operator, APN, IN, OUT, Total, Sessions, Avg Duration) | Task 1, 3, 4, 7 | Manual — table row inspection |
| AC-2 Byte columns humanized | Task 7 (reuses `formatBytes`) | Visual check |
| AC-3 Sessions + Avg Duration present | Task 1, 3, 7 | Table inspection |
| AC-4 Rich chart tooltip (ts, value, delta, top contributor, multi-series) | Task 2, 3, 6, 7 | Hover on chart |
| AC-5 Tooltip accessible | Task 6 (`role="tooltip"`) | aXe scan manually |
| AC-6 Delta rules (cap, "—", "↑") | Task 4, 7 | KPI cards when baseline ≈ 0 |
| AC-7 Delta color coding | Task 4, 7 | Red/green/gray inspection |
| AC-8 Title-case headers | Task 4, 7 (`humanizeGroupDim`) | "APN Breakdown" etc. |
| AC-9 Enum humanized (`lte_m → LTE-M`, `nr_5g → 5G NR`) | Task 4, 7 (`humanizeRatType`) | RAT breakdown + chart legend |
| AC-10 Actionable empty state | Task 7 | Apply filter with no results |
| AC-11 Group-by zero-groups empty state | Task 7 | Group by APN on fresh tenant |
| AC-12 `group_by=apn` works end-to-end | Pre-existing FIX-204 fix | Verified by Dev in browser |
| AC-13 `group_by=operator|rat_type|apn` render | Task 7 (humanizer covers labels) | Toggle all 3 |
| AC-14 Export CSV | **DEFERRED** to FIX-236 | Flagged in Risks |

## Story-Specific Compliance Rules
- **API:** Response stays on standard envelope `{ status, data }`. New fields are additive; existing consumers must not break.
- **DB:** No migration. `sims.msisdn` / `sims.imsi` / `cdrs.bytes_in` / `cdrs.bytes_out` already exist.
- **UI:** Design Token Map above is mandatory — Developer MUST NOT introduce new hex codes or arbitrary `px` values. EntityLink orphan rule (FRONTEND.md) applied for orphan operator/apn.
- **Business:** Delta polarity defaults to "up-is-good" for byte/session/auth/SIMs KPIs (matches FRONTEND color semantics: success = up, danger = down).
- **ADR:** None directly impacted. This is a FE + DTO-extension story.

## Bug Pattern Warnings
- **PAT-007 (implied from F-25):** Aggregate-view queries that drop `sim_id` cannot recompute `COUNT(DISTINCT sim_id)` — always run the SELECT list against the raw CDR table for per-bucket distinct cardinality, or accept the value as omitted for pre-aggregated views. Developer MUST verify the `cdrs_hourly`/`cdrs_daily` view definitions before assuming the `COUNT(DISTINCT sim_id)` column is populated.
- **PAT-012 (FRONTEND):** Hard-coded hex colors creep in during "quick polish" passes. Grep rule stated in Task 7 Verify — run it before considering the task done.
- **PAT-X (this story):** Delta % without zero-baseline handling causes UI breakage. The `formatDeltaPct` helper is the single source of truth — every callsite must use it. If the Developer finds another page still doing `delta.toFixed(1) + '%'` inline, it belongs to a follow-up FIX (out of scope here).

## Tech Debt (from ROUTEMAP)
No tech debt items tagged FIX-220 in `docs/ROUTEMAP.md`'s Tech Debt table.

## Mock Retirement
No mocks; backend API is already live. No mock retirement.

## Test Plan (DEFERRED to D-091)
Unit tests deferred per orchestrator dispatch. When D-091 activates, targets:
- `formatDeltaPct` edge cases: `(100, 0)`, `(0, 0)`, `(0, 100)`, `(10, 10)`, `(1e9, 1)`, `(-50, 100)`, `(NaN, 10)`, `(undefined, 10)`.
- `humanizeRatType`, `humanizeGroupDim` round-trip vs known keys + unknown keys.
- `<TwoWayTraffic>` snapshot for `(0, 0)`, small/large byte values.
- `<UsageChartTooltip>` render with and without groupBy.
- Store: `GetTopConsumers` returns MSISDN when present, handles nil MSISDN.
- Handler: `topConsumerDTO` omits `msisdn` key when SIM MSISDN is nil.

Manual smoke for this FIX:
- [ ] `/analytics` Top Consumers shows MSISDN column; orphan SIM shows `—`.
- [ ] Hover chart point shows rich tooltip; works with group_by.
- [ ] Apply filter with no results → actionable empty state copy.
- [ ] `group_by=apn` renders three breakdown cards, all Title-Case.
- [ ] KPI card with +999% shows `">999% ↑"`.

## Risks & Mitigations
- **R1: `cdrs_hourly`/`cdrs_daily` may not have `sim_id` → `unique_sims` stays 0 for those periods.** Mitigation: tooltip hides the "Unique SIMs" row when null/0. Code comment in Task 2 records the choice so future devs know.
- **R2: `GetTopConsumers` JOIN with `sims` could degrade performance at 10M SIMs.** Mitigation: `sim_id` is indexed (PK in `sims`). Limit 20 bounds the outer query. If measured regression — fall back to post-query enrichment (current pattern) and only pull `msisdn/imsi` via `enrichTopConsumer`. Developer may choose either approach in Task 1 and document in DEV-decision; both satisfy AC-1.
- **R3: AC-14 Export CSV is unshippable here.** Mitigation: deferred to FIX-236; Risks + Plan explicit.
- **R4: MSISDN column breaks narrow-screen width.** Mitigation: apply `hidden md:table-cell` to IMSI + MSISDN columns (shadcn Table supports this — existing pattern in `web/src/pages/cdrs/index.tsx`). ICCID always visible; EntityLink hover card on the row offers IMSI/MSISDN when hidden.
- **R5: Backend `cdrs` → `sims` JOIN on tenants with 10M SIMs may benefit from `WITH` CTE or `LATERAL`.** Out of scope for this FIX; log in D-091 follow-up.

## Wave Decomposition
- **Wave 1 (backend foundation, parallel-safe at store level):** Task 1, Task 2.
- **Wave 2 (DTO wiring, blocked by Wave 1):** Task 3.
- **Wave 3 (FE primitives, parallel-safe):** Task 4, Task 5, Task 6 — all independent once Task 3's types are known; Task 4 must precede 5/6 by one dispatch.
- **Wave 4 (FE integration, blocked by 4/5/6):** Task 7.

Practical dispatch order for M-sized story: (1, 2) → 3 → 4 → (5, 6) → 7 = 4 synchronous rounds.

## Quality Gate Self-Check
- [x] Minimum substance (M story, ≥ 60 lines, ≥ 3 tasks — 7 tasks, well over)
- [x] Required sections: Goal, Architecture Context, Tasks, Acceptance Criteria Mapping, Risks
- [x] Embedded specs: API field list, DB column source, screen ASCII, token map
- [x] No cross-layer imports planned
- [x] Standard envelope noted
- [x] Context refs point to sections that exist in this plan
- [x] No implementation code in tasks (specs + pattern refs only)
- [x] Design Token Map populated with exact class names
- [x] Component Reuse table populated
- [x] Every UI task references token map + component reuse
- [x] Each task has Pattern ref
- [x] Each task has Depends on + Complexity
- [x] Tasks ≤ 1-2 files each (Task 7 is 1 file, largest)
- [x] Deferred tests noted (D-091)
- [x] AC-14 deferral explicit
- [x] Finding-ID mislabel corrected in Findings Addressed section
- [x] Out-of-scope pages (analytics-cost, analytics-anomalies) explicitly excluded
