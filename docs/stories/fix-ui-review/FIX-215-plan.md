# Implementation Plan: FIX-215 — SLA Historical Reports + PDF Export + Drill-down

## Goal
Deliver a monthly historical SLA view (6–12 months) on `/sla` with per-month/per-operator drill-down, streaming PDF export of `sla_monthly`, breach computation from `operator_health_logs` (≥5 min continuous down), per-operator editable `sla_uptime_target`, 24-month retention, and 12 months of seeded SLA history — without blocking on FIX-248.

## Summary
L story, 10 tasks, 2 high-complexity (Tasks 2, 8). Split into 4 waves.
- Wave 1 (serial): Task 1 (DB audit + retention migration + seed audit).
- Wave 2 (parallel): Task 2 (store: monthly rollup + breach detection), Task 3 (seed generator for 12 months history), Task 4 (SLA target editable on operator).
- Wave 3 (parallel): Task 5 (API: history + month detail + operator-month breach endpoints), Task 6 (PDF streaming endpoint + provider), Task 7 (gateway routes + reports processor link).
- Wave 4 (parallel): Task 8 (FE rewrite `/sla` to historical view + drill-down), Task 9 (FE Operator Detail → Protocols/SLA tab target editor), Task 10 (backend + browser tests).

New endpoints: 4 (`GET /sla/history`, `GET /sla/months/:year/:month`, `GET /sla/operators/:operatorId/months/:year/:month/breaches`, `GET /sla/pdf`). New migration: 1 (per-operator latency threshold + index). New FE page surfaces: `/sla` (rewrite), month-detail drawer, operator-breach drawer. Operator detail tab gains SLA target editor.

Pre-Validation: all embedded sections referenced by `Context refs`; ≥5 tasks; 3 high-complexity; API/DB/UI specs embedded; patterns referenced per task; tests map to each AC.

---

## Scope Decisions

### AC-4 PDF Export — **Option (a): Ship now with synchronous streaming endpoint**
Rationale:
- `internal/report/pdf.go` + `StoreProvider.SLAMonthly` + `ReportSLAMonthly` type are already wired; only the HTTP surface is missing.
- FIX-248 (Local FS + signed-URL) is pending and unrelated to rendering. Blocking AC-4 on FIX-248 would leave user-visible scope broken past this wave.
- Synchronous streaming keeps the story self-contained. Report generation for a single month ≤ a few MB and < 1 s; no need for job indirection.

**PDF endpoint shape — parameter-driven, NOT row-id-driven:**
`SLAReportProcessor` (`internal/job/sla_report.go:127-177`) inserts only per-operator rows (`operator_id` populated; tenant-wide `NULL` rows are not produced). Therefore a `/sla-reports/:id/pdf` route cannot serve "whole-month" PDFs.

Resolution: expose TWO PDF routes, both using `report.Engine.Generate` with `Filters` built from query params (no required DB row lookup):
- `GET /api/v1/sla/pdf?year=YYYY&month=MM` → month-wide PDF covering every operator that has a `sla_reports` row for that month (tenant-scoped).
- `GET /api/v1/sla/pdf?year=YYYY&month=MM&operator_id=UUID` → single-operator month PDF.

`Content-Disposition: attachment; filename="sla-YYYY-MM-{operator_code|all}.pdf"`. Auth = same tenant-scoped middleware as `/sla-reports`. Errors follow standard envelope.

The old row-id endpoint `/sla-reports/:id/pdf` is **not introduced** — it would force the processor to also emit `operator_id=NULL` rows, doubling write volume with no benefit since the month bucket already maps 1:1 to `(year, month)` in the UI.

When FIX-248 lands, the reports handler will swap to producing artifacts in LocalFS + signed URLs; `/api/v1/sla/pdf` can be kept as a convenience alias or deprecated then — note in ROUTEMAP Tech Debt, not a blocker.

### Seed Data — **in-scope mandatory, no defer**
Per `feedback_no_defer_seed`: extend `migrations/seed/003_comprehensive_seed.sql` so that every time `make db-seed` runs, `sla_reports` contains 12 monthly rows per operator (12 × N_operators). Use realistic generators (uptime 98.5–99.99 %, MTTR 60–1800 s, incidents 0–4, latency_p95 70–320 ms). Also seed matching `operator_health_logs` samples for the most recent 2 months so breach queries return non-empty rows during UAT. No separate backfill script — seed is single source of truth.

### Breach Source — **operator_health_logs time-series, computed on demand (no stored breach table)**
- Source table: `operator_health_logs(operator_id, checked_at, status, latency_ms, circuit_state)` (TBL-23, hypertable).
- Definition: a breach is a maximal run of `status = 'down'` OR `latency_ms > sla_latency_threshold_ms` (default 500 ms) with duration ≥ 5 minutes, gaps between consecutive samples ≤ 2× polling interval (≤ 120 s).
- Computation lives in `internal/store/sla.go::BreachesForOperatorMonth(ctx, operatorID, year, month, latencyThresholdMs)` — a window-based SQL using `LAG` + `session_id`-style gap detection (pattern mirrors the existing MTTR CTE in `OperatorStore.AggregateHealthForSLA`).
- Aggregate `breach_minutes` + `incident_count` are also persisted per-month in `sla_reports.details` JSON so historical queries don't re-scan operator_health_logs beyond its 90-day retention.

### SLA Target Storage — **existing `operators.sla_uptime_target` column, editable via Operator Detail API**
- `operators.sla_uptime_target DECIMAL(5,2) DEFAULT 99.90` already exists (seen in `migrations/20260320000002_core_schema.up.sql`).
- No new column. Add a latency breach threshold: `operators.sla_latency_threshold_ms INTEGER NOT NULL DEFAULT 500` (new migration).
- PATCH endpoint: `PATCH /api/v1/operators/:id` already widens to accept `sla_uptime_target`, extend to accept `sla_latency_threshold_ms`. Validation: uptime ∈ [50.00, 100.00]; latency ∈ [50, 60000]. Audit log entry on change (pattern: existing operator updates already emit `audit.operator.updated`).

### Historical Retention — **24-month minimum for `sla_reports` (no-retention-policy by default; monthly rollups are small)**
- `sla_reports` is a plain table (not hypertable). With one row per operator per month, 10 operators × 24 months = 240 rows/tenant. Retention is effectively unlimited by default.
- Add a migration note guaranteeing no cleanup cron drops `sla_reports < NOW() - INTERVAL '24 months'`. This is a policy guarantee, not an active job. `operator_health_logs` keeps its 90-day retention (compliant, because monthly rollups are already persisted with per-month breach data in `sla_reports.details`).
- Documentation line added to `docs/architecture/db/_index.md` under TBL-17 (sla_reports): "Retention: 24 months minimum (compliance); no automated cleanup."

---

## Architecture Context

### Components Involved

| Layer | Component | Role | File path |
|---|---|---|---|
| DB | `sla_reports` (TBL-17) | Persisted monthly SLA rollups | `migrations/20260412000001_sla_reports.up.sql` (exists) |
| DB | `operator_health_logs` (TBL-23, hypertable) | Raw health time-series, source for breach computation | `migrations/20260320000002_core_schema.up.sql` (exists) |
| DB | `operators` (TBL-05) | `sla_uptime_target` column (exists); add `sla_latency_threshold_ms` | `migrations/20260320000002_core_schema.up.sql` (exists) + new migration |
| DB (new) | Column migration | `operators.sla_latency_threshold_ms` + `sla_reports.details` schema note | `migrations/20260422000001_sla_latency_threshold.up.sql` (NEW) |
| Seed | Monthly SLA history generator | 12 months × N operators synthetic rows | `migrations/seed/003_comprehensive_seed.sql` (extend) |
| Store | `SLAReportStore` | Reads + history rollup | `internal/store/sla_report.go` (extend) |
| Store (new) | `SLAHistoryStore` methods | `HistoryByMonth`, `MonthDetail`, `BreachesForOperatorMonth`, `UpsertMonthlyRollup` | `internal/store/sla_report.go` (extend same file) |
| Store | `OperatorStore.AggregateHealthForSLA` | Existing aggregate, extended to accept latency threshold + return breaches | `internal/store/operator.go` (extend) |
| Store (new) | `OperatorStore.UpdateSLATargets` | Update `sla_uptime_target` + `sla_latency_threshold_ms` | `internal/store/operator.go` (extend) |
| API | `slaapi.Handler` | History + month detail + breach detail + PDF endpoint | `internal/api/sla/handler.go` (extend) |
| Report | `StoreProvider.SLAMonthly` | Provide SLAData from store; accept month+operator filter | `internal/report/store_provider.go` (extend) |
| Report | PDF builder (existing `buildPDF`/`ReportSLAMonthly`) | Render artifact | `internal/report/pdf.go` (no change) |
| API (existing) | Operator update PATCH | Accept `sla_uptime_target` + `sla_latency_threshold_ms` | `internal/api/operator/handler.go` (extend) |
| Gateway | `router.go` | Route registration | `internal/gateway/router.go` (extend) |
| Job (unchanged) | `SLAReportProcessor` | Already rolls up per-operator monthly rows | `internal/job/sla_report.go` (minor: use month boundaries instead of rolling 24h on monthly cron) |
| FE page | `SLAHistoricalPage` | Rewrite of `/sla` to show 6/12-month history | `web/src/pages/sla/index.tsx` (rewrite) |
| FE drawer | `MonthDetailDrawer` | Per-month drill-down with operator list | `web/src/pages/sla/month-detail.tsx` (NEW) |
| FE drawer | `OperatorBreachDrawer` | Per-operator month breach timeline | `web/src/pages/sla/operator-breach.tsx` (NEW) |
| FE hook | `useSLAHistory`, `useSLAMonthDetail`, `useSLAOperatorBreaches` | TanStack Query hooks | `web/src/hooks/use-sla.ts` (NEW) |
| FE hook | `useUpdateOperatorSLA` | Mutation | `web/src/hooks/use-sla.ts` (NEW) |
| FE page | Operator Detail `Protocols/SLA` tab | SLA target editor form | `web/src/pages/operators/detail.tsx` (extend) |
| FE sidebar | (no change, `/sla` already registered) | — | `web/src/components/layout/sidebar.tsx` |

### Data Flow (Historical View)

1. User opens `/sla` → FE calls `GET /api/v1/sla/history?months=6&year=2026` (default: last 6 rolling months, or `year=N` for calendar year).
2. Backend reads `sla_reports` grouped by `(operator_id, DATE_TRUNC('month', window_start))`, joined with `operators` for name/target. Returns per-operator per-month uptime + incident counts. If an expected month is missing for an operator → `null` summary flagged "not generated".
3. User clicks month card → `GET /api/v1/sla/months/:year/:month` returns full operator × month table for that one month.
4. User clicks operator row → `GET /api/v1/sla/operators/:operatorId/months/:year/:month/breaches` returns breach events (start, end, duration_sec, cause ("down" | "latency_exceeded"), samples_count) computed from `operator_health_logs`.
5. User clicks "Export PDF" on a month → opens `GET /api/v1/sla-reports/:id/pdf` (where `id` = sla_reports.id for the monthly row). Browser download.

### Event model
None in this story (no NATS publishers/consumers added). FIX-212 envelope already covers `sla.report.generated` (emitted by `SLAReportProcessor`). This story does not extend the event surface.

---

## API Specs

### `GET /api/v1/sla/history`
**Query params (all optional)**:
- `months` INT (1–24, default 6) — rolling months back from current month
- `year` INT (2020–current, optional) — if set, returns Jan–Dec (or Jan–current month if year=now)
- `operator_id` UUID (optional) — filter to one operator

**Response (`{status, data, meta?}`):**
```
{
  "status": "success",
  "data": {
    "months": [
      {
        "year": 2026, "month": 3, "label": "March 2026",
        "overall": {
          "uptime_pct": 99.82, "incident_count": 4, "breach_minutes": 71,
          "latency_p95_ms": 142, "sessions_total": 482113
        },
        "operators": [
          { "operator_id":"...", "operator_name":"Turkcell", "operator_code":"TUR01",
            "sla_uptime_target": 99.90,
            "uptime_pct": 99.95, "incident_count": 0, "breach_minutes": 0,
            "latency_p95_ms": 118, "mttr_sec": 0,
            "status": "on_track",
            "report_id": "uuid-of-sla_reports-row"   // nullable; row identity for cache/key purposes only — PDF link uses year+month+operator_id params, not this id
          }
        ]
      }
    ],
    "summary": {
      "months_count": 6,
      "overall_avg_uptime_pct": 99.78,
      "best_month": "March 2026",
      "worst_month": "January 2026"
    }
  }
}
```

### `GET /api/v1/sla/months/:year/:month`
Params: `year` (path, 2020–current), `month` (path, 1–12). Returns the same shape as one month entry above, with additional `operators[].downtime_minutes` and `operators[].breaches_count`.

### `GET /api/v1/sla/operators/:operatorId/months/:year/:month/breaches`
**Response:**
```
{
  "status": "success",
  "data": {
    "operator": { "id":"...", "name":"Turkcell", "code":"TUR01",
                  "sla_uptime_target": 99.90, "sla_latency_threshold_ms": 500 },
    "month": { "year": 2026, "month": 3 },
    "breaches": [
      { "started_at":"2026-03-14T02:13:00Z", "ended_at":"2026-03-14T02:22:00Z",
        "duration_sec": 540, "cause": "down", "samples_count": 9,
        "affected_sessions_est": 87 }
    ],
    "totals": { "breaches_count": 3, "downtime_seconds": 1840,
                "affected_sessions_est": 412 }
  }
}
```
`affected_sessions_est` = approximation: `sessions_total × (duration_sec / month_seconds)` for each breach — documented caveat in response `meta`.

### `GET /api/v1/sla/pdf`
Streaming. Tenant-scoped. Query params:
- `year` INT (required, 2020–current)
- `month` INT (required, 1–12)
- `operator_id` UUID (optional; when omitted → month-wide PDF covering all operators)

Uses `Engine.Generate(ctx, Request{Type: ReportSLAMonthly, Format: FormatPDF, TenantID: tid, Filters: {"year": Y, "month": M, "operator_id": optID}})`.
- Content-Type: `application/pdf`
- Content-Disposition: `attachment; filename="sla-<year>-<month>-<operator_code|all>.pdf"`
- 404 if no `sla_reports` rows match (year, month, tenant [, operator_id]).

Builder behavior: `StoreProvider.SLAMonthly` (extended in Task 6) reads `sla_reports` rows by `(tenant_id, DATE_TRUNC('month', window_start) = make_date(year, month, 1))` and optional operator filter, then renders into `SLAData.Rows` via `pdf.go`'s existing `renderTabularPDF` path (no builder changes required — `buildPDF` already routes `ReportSLAMonthly` at line 56).

### `PATCH /api/v1/operators/:id` (EXTEND)
New optional body fields:
```
{
  "sla_uptime_target": 99.95,
  "sla_latency_threshold_ms": 400
}
```
Validation: `sla_uptime_target ∈ [50.00, 100.00]`, `sla_latency_threshold_ms ∈ [50, 60000]`. Emit audit event `operator.updated` with diff.

---

## DB Schema

### Existing (authoritative — from migrations)

`operators` (TBL-05, `migrations/20260320000002_core_schema.up.sql`):
- `id UUID PRIMARY KEY`, `name`, `code`, `mcc`, `mnc`, `adapter_type`, …
- **`sla_uptime_target DECIMAL(5,2) DEFAULT 99.90`** (already exists)
- `state VARCHAR(20) NOT NULL DEFAULT 'active'`, `created_at`, `updated_at`

`operator_health_logs` (TBL-23, `migrations/20260320000002_core_schema.up.sql`, hypertable via `20260320000003_timescaledb_hypertables.up.sql`, retention 90 days):
```sql
CREATE TABLE operator_health_logs (
    id BIGSERIAL,
    operator_id UUID NOT NULL,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status VARCHAR(20) NOT NULL,
    latency_ms INTEGER,
    error_message TEXT,
    circuit_state VARCHAR(20) NOT NULL
);
CREATE INDEX idx_op_health_operator_time ON operator_health_logs (operator_id, checked_at DESC);
```

`sla_reports` (TBL-17, `migrations/20260412000001_sla_reports.up.sql`):
```sql
CREATE TABLE sla_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID REFERENCES operators(id),     -- NULL = tenant-wide aggregate
    window_start TIMESTAMPTZ NOT NULL,
    window_end   TIMESTAMPTZ NOT NULL,
    uptime_pct       NUMERIC(5,2)  NOT NULL,
    latency_p95_ms   INTEGER       NOT NULL DEFAULT 0,
    incident_count   INTEGER       NOT NULL DEFAULT 0,
    mttr_sec         INTEGER       NOT NULL DEFAULT 0,
    sessions_total   BIGINT        NOT NULL DEFAULT 0,
    error_count      INTEGER       NOT NULL DEFAULT 0,
    details          JSONB         NOT NULL DEFAULT '{}'::jsonb,
    generated_at     TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT sla_window_valid CHECK (window_end > window_start)
);
```
Indexes exist: `idx_sla_reports_tenant_time`, `idx_sla_reports_operator`, `idx_sla_reports_generated_at`.

### New migration — `migrations/20260422000001_sla_latency_threshold.up.sql`

```sql
-- FIX-215: per-operator latency threshold for SLA breach computation
ALTER TABLE operators
  ADD COLUMN IF NOT EXISTS sla_latency_threshold_ms INTEGER NOT NULL DEFAULT 500;

-- Partial index for monthly queries on sla_reports
CREATE INDEX IF NOT EXISTS idx_sla_reports_operator_month
  ON sla_reports (operator_id, DATE_TRUNC('month', window_start) DESC)
  WHERE operator_id IS NOT NULL;

COMMENT ON COLUMN operators.sla_latency_threshold_ms
  IS 'FIX-215: per-operator latency ceiling in ms; latencies above this trigger SLA breach contribution';
```
Down: drop column + index (with IF EXISTS).

### `sla_reports.details` JSON convention (documented, not enforced by schema)
```
{
  "breach_minutes": 12,
  "breaches": [{"started_at":"...","ended_at":"...","duration_sec":540,"cause":"down"}],
  "latency_threshold_ms": 500,
  "uptime_target": 99.90
}
```
Written by `SLAReportProcessor` when persisting monthly rollups (Task 2). Read by history endpoint to avoid re-scanning operator_health_logs beyond 90-day retention.

---

## Business Rules

- **BR-1 (breach definition):** A breach is a continuous run (samples gap ≤ 120 s) of `status = 'down'` OR `latency_ms > operators.sla_latency_threshold_ms` with total `duration_sec ≥ 300`. Duration = `MAX(checked_at) - MIN(checked_at)` within the run.
- **BR-2 (uptime formula):** `uptime_pct = 100.0 * (samples where status='healthy' AND (latency_ms IS NULL OR latency_ms ≤ threshold)) / total_samples`. If zero samples in window → `NULL` reported as "no data"; status = `insufficient_data`; does NOT contribute to summary avg.
- **BR-3 (status classification):** `on_track` if `uptime_pct ≥ sla_uptime_target`; `at_risk` if within 0.5 p.p. below target; `breached` if further below target. Color: green / yellow / red.
- **BR-4 (retention guarantee):** `sla_reports` rows are preserved for ≥ 24 months. No cleanup cron operates on this table. Documented in db index.
- **BR-5 (target editable by operator_manager+ only):** PATCH on `operators` already enforces this; the new fields inherit same role check.
- **BR-6 (PDF tenant-scope):** `GET /sla-reports/:id/pdf` refuses if `tenant_id` on the row ≠ request tenant (standard 404, not 403, to avoid enumeration).
- **BR-7 (backward compat):** Existing `GET /sla-reports` endpoint stays unchanged; new endpoints sit under `/sla/...` to avoid FE query breakage mid-migration.

---

## Design Token Map (FE — UI tasks use these EXACT class names)

#### Color Tokens (from `docs/FRONTEND.md`)
| Usage | Token / Class | NEVER Use |
|---|---|---|
| Page background | `bg-[var(--bg-primary)]` (already inherited from `BaseLayout`) | `bg-black`, `bg-[#0f0f15]` |
| Card surface | `bg-[var(--bg-surface)]` | `bg-white`, `bg-gray-900` |
| Elevated dropdown / drawer | `bg-[var(--bg-elevated)]` | arbitrary hex |
| Hover row | `hover:bg-[var(--bg-hover)]` | `hover:bg-gray-800` |
| Border | `border-[var(--border)]` | `border-gray-700` |
| Subtle divider | `border-[var(--border-subtle)]` | `border-gray-800` |
| Primary text | `text-[var(--text-primary)]` | `text-white`, `text-gray-100` |
| Secondary label | `text-[var(--text-secondary)]` | `text-gray-400` |
| Tertiary/muted | `text-[var(--text-tertiary)]` | `text-gray-500` |
| Accent (CTA / link / selected) | `text-[var(--accent)]` / `bg-[var(--accent-dim)]` | `text-cyan-400`, `bg-cyan-500/10` |
| On-track (success) | `text-[var(--success)]` / `bg-[var(--success-dim)]` | `text-green-400` |
| At-risk (warning) | `text-[var(--warning)]` / `bg-[var(--warning-dim)]` | `text-yellow-400` |
| Breached (danger) | `text-[var(--danger)]` / `bg-[var(--danger-dim)]` | `text-red-400` |
| Secondary accent (operator column highlight) | `text-[var(--purple)]` | `text-violet-500` |

#### Radii / Shadows
| Usage | Class |
|---|---|
| Buttons / badges / nav items | `rounded-[6px]` (radius-sm) |
| Cards / panels / tables | `rounded-[10px]` (radius-md) |
| Modals / drawers | `rounded-[14px]` (radius-lg) |

#### Components to Reuse (glob hits under `web/src/components/ui/`)
- `Card`, `CardHeader`, `CardTitle`, `CardContent` — monthly summary cards
- `Badge` — status chips (variants `success` | `warning` | `danger`)
- `Skeleton` — loading
- `Select` — year selector, operator filter
- `Button` — actions
- `SlidePanel` (from `slide-panel.tsx`) — month detail drawer + operator breach drawer (reuses FIX-214 pattern)
- `TimeframeSelector` (`timeframe-selector.tsx`) — optional for rolling windows
- `Breadcrumb` — already present in `pages/sla/index.tsx`; keep
- `AnimatedCounter` — preserve existing SLA overall number animation
- `EntityLink` (`components/shared/entity-link.tsx`) — clickable operator cells (FIX-219 pattern)
- `Sparkline` (`components/ui/sparkline.tsx`) — 6-month uptime sparkline per operator row

#### Typography
- Page title: `text-heading-lg font-bold` (per previous FIX stories)
- Card title: `text-heading-md font-semibold`
- KPI number: `text-3xl tabular-nums font-semibold` (consistent with dashboard)
- Body: `text-body-md`
- Caption / timestamp: `text-xs text-[var(--text-secondary)]`

---

## Screen Mockups (text wireframes)

### `/sla` (historical view — replaces current snapshot)

```
┌ Breadcrumb: Home / SLA ──────────────── [Year: 2026 ▼] [Rolling 6m|12m] [PDF Export ⏷] ┐
│                                                                                         │
│ KPIs:  [Avg Uptime 99.82%]  [Total Breaches 11]  [Downtime 4h 32m]  [Best: Mar 99.95%] │
│                                                                                         │
│ Month strip (scrollable right-to-left, last 6 or 12 months):                            │
│  ┌Apr 26┐┌Mar 26┐┌Feb 26┐┌Jan 26┐┌Dec 25┐┌Nov 25┐                                        │
│  │99.78 ││99.95 ││99.71 ││99.55 ││99.88 ││99.90 │  ← each = card, colored by status    │
│  │ BREA ││ ON   ││ AT-R ││ BREA ││ ON   ││ ON   │                                        │
│  └──────┘└──────┘└──────┘└──────┘└──────┘└──────┘                                        │
│                                                                                         │
│ Operator × Month matrix (table, expanded for current view):                             │
│  Operator │ Target │ Apr │ Mar │ Feb │ Jan │ Dec │ Nov │ 6M Trend                       │
│  Turkcell │ 99.90  │99.95│99.99│99.82│99.45│99.96│99.98│ ▁▃▂▅▂▁  [PDF] ← hover        │
│  Vodafone │ 99.90  │99.67│99.90│99.60│99.38│99.80│99.81│ ▂▁▃▅▂▂  [PDF]                │
│  Türk Tel │ 99.50  │99.55│99.75│99.88│99.60│99.70│99.72│ ▂▁▁▃▂▂  [PDF]                │
│                                                                                         │
│ Click month card → MonthDetailDrawer.  Click operator cell → OperatorBreachDrawer.      │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

### MonthDetailDrawer (SlidePanel, right side)

```
┌ March 2026 · SLA Detail ─────────────────────── [×] ┐
│ Overall Uptime 99.82%   Incidents 4   Downtime 71m  │
│                                                      │
│ Operator         │ Uptime │ Breaches │ Downtime │ → │
│ Turkcell TUR01   │ 99.95% │    0     │    0m    │ > │
│ Vodafone VOD01   │ 99.67% │    2     │   38m    │ > │  ← click = OperatorBreachDrawer
│ Türk Tel  TT01   │ 99.82% │    1     │   12m    │ > │
│                                                      │
│          [Download month PDF]  ← /sla/pdf?year=Y&month=M │
└─────────────────────────────────────────────────────┘
```

### OperatorBreachDrawer (SlidePanel, nested)

```
┌ Turkcell · March 2026 · Breaches ───────────────── [×] ┐
│ Target 99.90%  Latency ceiling 500 ms                  │
│ Totals: 3 breaches · 30m 40s downtime · ~412 sessions  │
│                                                         │
│ Timeline:                                               │
│ 1. 14 Mar 02:13 → 02:22  (9m)  down       87 sessions │
│ 2. 21 Mar 08:40 → 08:46  (6m)  latency>500 142 sess.  │
│ 3. 27 Mar 19:05 → 19:20  (15m) down        183 sess.  │
│                                                         │
│  [Download operator-month PDF] ← /sla/pdf?year=&month=&operator_id=  │
└────────────────────────────────────────────────────────┘
```

### Operator Detail → Protocols/SLA tab (editable)

New section inside existing Protocols tab:
```
│ SLA Targets                                                             │
│  Uptime target:            [99.90] %   (50.00–100.00)                   │
│  Latency ceiling:          [  500] ms  (50–60000)                        │
│                                                [Reset defaults] [Save]  │
│  Last changed: 2 days ago by admin@argus.io                              │
```
Save → PATCH `/operators/:id` → optimistic toast; audit log entry created server-side.

---

## Tasks

### Task 1: DB + seed audit, retention policy note, latency column migration

- **Files (modify/create):**
  - Create `migrations/20260422000001_sla_latency_threshold.up.sql` (see DB Schema > new migration)
  - Create `migrations/20260422000001_sla_latency_threshold.down.sql` (reverse ALTER + drop index)
  - Modify `docs/architecture/db/_index.md` — add under TBL-17 retention note ("24 months minimum, no cleanup cron")
- **Goal:** Add `sla_latency_threshold_ms`; verify `sla_reports` + `operator_health_logs` match spec; document retention guarantee.
- **Depends on:** none
- **Complexity:** low
- **Pattern ref:** `migrations/20260421000003_cdrs_explorer_indexes.up.sql` (FIX-214 pattern — idempotent CREATE INDEX IF NOT EXISTS with COMMENT)
- **AC covered:** AC-5 (partial: schema), AC-7 (documented)
- **Context refs:** `## DB Schema`, `## Business Rules` (BR-4)
- **Tests:** Run `make db-migrate up && make db-migrate down && make db-migrate up` — roundtrip clean. No Go tests needed.

### Task 2: Store layer — monthly rollup + breach detection + SLA target update

- **Files (modify):**
  - `internal/store/sla_report.go` — add `HistoryByMonth(ctx, tenantID, yearOrRollingN, operatorID) ([]MonthSummary, error)`, `MonthDetail(ctx, tenantID, year, month) (*MonthDetail, error)`, `UpsertMonthlyRollup(ctx, SLAReportRow) error` (idempotent upsert on `(tenant_id, operator_id, window_start, window_end)`).
  - `internal/store/operator.go` — add `BreachesForOperatorMonth(ctx, operatorID uuid.UUID, year, month int, latencyThresholdMs int) ([]Breach, error)` using LAG+gap CTE (mirror the MTTR CTE at lines 612-645). Add `UpdateSLATargets(ctx, operatorID, uptimeTarget float64, latencyThresholdMs int) error` with audit log hook.
- **Output types to add (in `sla_report.go`):**
  ```go
  type MonthSummary struct { Year, Month int; Overall OperatorMonthAgg; Operators []OperatorMonthAgg }
  type OperatorMonthAgg struct { OperatorID uuid.UUID; OperatorName, OperatorCode string;
    UptimePct float64; IncidentCount int; BreachMinutes int; LatencyP95Ms int;
    MTTRSec int; SessionsTotal int64; SLAUptimeTarget float64; ReportID *uuid.UUID }
  type Breach struct { StartedAt, EndedAt time.Time; DurationSec int; Cause string; SamplesCount int }
  ```
- **Depends on:** Task 1 (needs `sla_latency_threshold_ms`)
- **Complexity:** **high** (SQL CTE for breach runs; tenant_id scoping; idempotent upsert; unit tests with synthetic `operator_health_logs` fixtures)
- **Pattern ref:** `internal/store/operator.go:595-649` (existing `AggregateHealthForSLA` CTE); `internal/store/cdr.go` for pagination patterns; `internal/store/sla_report.go:92-162` for ListByTenant scaffold.
- **AC covered:** AC-2 (data source), AC-3 (breach list), AC-6 (definition), AC-5 (target update)
- **Context refs:** `## DB Schema`, `## API Specs`, `## Business Rules` (BR-1, BR-2, BR-5)
- **Tests:** `internal/store/sla_report_test.go` + `internal/store/operator_breach_test.go` — table-driven with `pgxtest` fixtures:
  - breach detection: 3-min run (not breach), 5-min run (breach), latency-only breach, mixed cause, gap > 120s (two breaches)
  - monthly rollup idempotency (insert twice → one row)
  - target update roundtrip + audit log row asserted

### Task 3: Seed generator — 12 months of synthetic SLA rows + matching health logs

- **Files (modify):** `migrations/seed/003_comprehensive_seed.sql`
- **Behavior:**
  - Append a `DO $$ … $$` block that, for each `(tenant, operator_grant)`, generates 12 `sla_reports` rows (`window_start` = first of month, `window_end` = first of next month) with randomized but plausible values.
  - `details` JSONB populated per Task 2 convention (breach_minutes, breaches[] sample, latency_threshold_ms, uptime_target).
  - Also extend existing `operator_health_logs` seed (line 805) so that current month + previous month have 1 sample/min for at least 2 operators (~90k rows, manageable) — enables live breach queries.
  - Idempotent: guard with `WHERE NOT EXISTS (SELECT 1 FROM sla_reports LIMIT 1)`.
- **Depends on:** Task 1 (for `sla_latency_threshold_ms` existence)
- **Complexity:** medium
- **Pattern ref:** existing `operator_health_logs` seed block at `migrations/seed/003_comprehensive_seed.sql:805-833` (uses `generate_series` + modulo randomization).
- **AC covered:** Risk 1 mitigation, AC-1 (data available), AC-2, AC-3
- **Context refs:** `## Scope Decisions > Seed Data`, `## Business Rules`
- **Tests:** `make db-seed` end-to-end from a fresh DB; then `psql -c "SELECT COUNT(*), COUNT(DISTINCT DATE_TRUNC('month', window_start)) FROM sla_reports"` returns `(≥ N_operators*12, ≥12)`.

### Task 4: Operator update API — accept new SLA fields + audit log

- **Files (modify):**
  - `internal/api/operator/handler.go` — extend `UpdateOperator` (or equivalent PATCH handler) to accept + validate `sla_uptime_target` and `sla_latency_threshold_ms`.
  - `internal/model/operator.go` (if DTO struct exists there) — add fields.
  - Reuse existing audit pattern (event type `operator.updated`).
- **Validation:** `sla_uptime_target ∈ [50, 100]`; `sla_latency_threshold_ms ∈ [50, 60000]`.
- **Depends on:** Task 2 (`UpdateSLATargets` store method)
- **Complexity:** low
- **Pattern ref:** `internal/api/operator/handler.go` existing update handler; `internal/api/sim/handler.go` for PATCH/audit pattern.
- **AC covered:** AC-5
- **Context refs:** `## API Specs > PATCH /api/v1/operators/:id`, `## Business Rules > BR-5`
- **Tests:** handler test with valid + invalid (out-of-range) + forbidden (role < operator_manager) cases.

### Task 5: SLA API — history + month detail + breach detail (with retention-aware fallback)

- **Files (modify):** `internal/api/sla/handler.go` — add `History`, `MonthDetail`, `OperatorMonthBreaches` handlers. Tenant context extraction mirrors existing `List`/`Get` (lines 27-98). Pagination NOT needed (bounded by `months ≤ 24`).
- **Validation:** `year ∈ [2020, now().Year()]`; `month ∈ [1,12]`; `months ∈ [1,24]`; `operator_id` valid UUID when present.
- **Envelope:** standard `{status, data}`. `meta` carries caveat note on `affected_sessions_est` plus a `meta.breach_source` flag (`"live"` | `"persisted"`).
- **Retention fallback (EXPLICIT):** `operator_health_logs` retention is 90 days (`20260320000003_timescaledb_hypertables.up.sql:49`). In `OperatorMonthBreaches`:
  - Compute `monthEnd = first-of-next-month UTC`; if `monthEnd < NOW() - 91d` → **skip** live CTE; load breaches from the `sla_reports.details.breaches` JSON array for that `(tenant_id, operator_id, window_start)` row. Set `meta.breach_source = "persisted"`.
  - Else → run `store.BreachesForOperatorMonth` live; set `meta.breach_source = "live"`.
  - If neither path yields data (no `sla_reports` row AND window is beyond retention) → 404 with envelope `error.code = "sla_month_not_available"`.
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** `internal/api/sla/handler.go:27-126` (existing handler style); `internal/api/cdr/handler.go` (FIX-214) for multi-endpoint handler struct.
- **AC covered:** AC-1, AC-2, AC-3
- **Context refs:** `## API Specs`, `## Business Rules`
- **Tests:** `internal/api/sla/handler_test.go` — 6+ cases: happy path, year scope, rolling months scope, invalid year, invalid month, cross-tenant 404.

### Task 6: PDF streaming endpoint + provider filter hook

- **Files (modify):**
  - `internal/api/sla/handler.go` — add `DownloadPDF(w, r)` handler: parses `year`, `month`, optional `operator_id` from query; calls `engine.Generate(ctx, Request{Type: ReportSLAMonthly, Format: FormatPDF, TenantID: tid, Filters: {"year": Y, "month": M, "operator_id": optID}})`; verifies result non-empty (Rows > 0) else 404; streams `artifact.Bytes` with Content-Type/Disposition headers (filename pattern `sla-YYYY-MM-{operator_code|all}.pdf`, resolve operator code via `OperatorStore.GetByID` for display).
  - `internal/report/store_provider.go` — extend `SLAMonthly` to accept `year`, `month`, `operator_id` filters; when supplied, query `sla_reports` rows by `(tenant_id, window_start = make_date(year, month, 1), [operator_id = ?])` (not a 30-day rolling window). Preserve legacy filter-less behavior for backward compat with existing `/reports` entry point.
  - Wire `Engine` dependency into `slaapi.Handler` (add `engine *report.Engine` field + constructor param; update `router.go` deps builder and `cmd/argus/main.go` wiring).
- **Depends on:** Task 5 (handler scaffold), Task 2 (store reads)
- **Complexity:** medium
- **Pattern ref:** `internal/api/reports/handler.go` uses `engine.Generate` already — mirror its streaming write pattern; `internal/api/cdr/handler.go:ExportCSV` for streaming response writing.
- **AC covered:** AC-4
- **Context refs:** `## Scope Decisions > AC-4`, `## API Specs > GET /api/v1/sla/pdf`
- **Tests:** handler test (happy path month-wide, happy path per-operator, missing params 400, empty month 404, cross-tenant 404, `Content-Disposition` asserted).

### Task 7: Gateway route registration

- **Files (modify):** `internal/gateway/router.go` — under existing `SLAHandler` block (line 764), add:
  - `r.Get("/api/v1/sla/history", deps.SLAHandler.History)`
  - `r.Get("/api/v1/sla/months/{year}/{month}", deps.SLAHandler.MonthDetail)`
  - `r.Get("/api/v1/sla/operators/{operatorId}/months/{year}/{month}/breaches", deps.SLAHandler.OperatorMonthBreaches)`
  - `r.Get("/api/v1/sla/pdf", deps.SLAHandler.DownloadPDF)`
  - `cmd/argus/main.go` — wire `report.Engine` into `slaapi.NewHandler` (thread the existing `Engine` already built for reports).
- **Depends on:** Task 5, Task 6
- **Complexity:** low
- **Pattern ref:** `internal/gateway/router.go` existing `/api/v1/sla-reports` block (line 764-770); `/api/v1/cdrs/*` block (609-613).
- **AC covered:** AC-1…4 (exposure)
- **Context refs:** `## API Specs`
- **Tests:** integration via `make test` picks up Go router assembly compile; E2E hits in Task 10 cover wire correctness.

### Task 8: FE `/sla` page rewrite — historical view, month strip, operator matrix

- **Files (modify + create):**
  - Rewrite `web/src/pages/sla/index.tsx` (605 lines → ~350 lines of focused historical view; old snapshot code fully replaced)
  - Create `web/src/hooks/use-sla.ts` with: `useSLAHistory({months, year, operatorId})`, `useSLAMonthDetail(year, month)`, `useSLAOperatorBreaches(operatorId, year, month)`, `useUpdateOperatorSLA()`.
  - Create `web/src/pages/sla/month-detail.tsx` (SlidePanel component used by index)
  - Create `web/src/pages/sla/operator-breach.tsx` (SlidePanel, rendered nested)
  - Create `web/src/types/sla.ts` (shared types: `SLAMonthSummary`, `SLAOperatorMonthAgg`, `SLABreach`, `SLAHistoryResponse`)
  - Add "Export PDF" anchor `<a href="/api/v1/sla/pdf?year=Y&month=M" download>` on each month card (month-wide PDF) AND `<a href="/api/v1/sla/pdf?year=Y&month=M&operator_id=UUID" download>` per operator-month cell/row (uses the already-authenticated session — same pattern as CDR export in FIX-214).
  - Preserve existing Breadcrumb, overall KPI cards, `AnimatedCounter` usage (port numbers from new endpoint).
- **Depends on:** Task 7
- **Complexity:** **high** (largest UI surface; careful replacement to avoid regressions on `/sla` entry point)
- **Pattern ref:** `web/src/pages/cdrs/index.tsx` + `web/src/pages/cdrs/session-timeline.tsx` (FIX-214) — same list + drawer pattern; `web/src/hooks/use-cdrs.ts` for TanStack hook shape; `web/src/components/ui/slide-panel.tsx` for drawer mechanics; `web/src/pages/sla/index.tsx` current for Breadcrumb + KPI card style to preserve.
- **AC covered:** AC-1, AC-2, AC-3, AC-4 (download links)
- **Context refs:** `## Screen Mockups`, `## Design Token Map`, `## API Specs`
- **Tests:** Browser (Playwright) in Task 10; hook-level RTL test for `useSLAHistory` caching.

### Task 9: Operator Detail — Protocols/SLA tab target editor

- **Files (modify):**
  - `web/src/pages/operators/detail.tsx` — inside Protocols (or existing SLA) tab, add an `SLATargetsSection` with two `Input` fields, Save button, optimistic `useUpdateOperatorSLA` mutation, success/error `Toast`.
  - Reuse `useOperatorSWR`/`useOperator` (whichever exists) to read current values; local form state for editing.
- **Validation (client):** same as server; show inline error on blur.
- **Depends on:** Task 4 (API), Task 8 (hook file created)
- **Complexity:** low-medium
- **Pattern ref:** `web/src/pages/operators/detail.tsx` existing Protocols tab layout; `web/src/components/ui/input.tsx`; any existing `Form` pattern in SIM detail (`web/src/pages/sims/detail.tsx`).
- **AC covered:** AC-5 (UI)
- **Context refs:** `## Screen Mockups > Operator Detail`, `## Design Token Map`, `## API Specs > PATCH /operators/:id`
- **Tests:** RTL test for input validation + mutation fire; browser click-through in Task 10.

### Task 10: Tests — backend + browser + make db-seed clean run

- **Files (create/modify):**
  - `internal/api/sla/handler_test.go` — extend with history/month/breaches/pdf cases
  - `internal/store/sla_report_test.go` — monthly rollup + idempotency + breach detection
  - `internal/store/operator_breach_test.go` (new) — breach CTE cases (see Task 2)
  - `internal/job/sla_report_test.go` — month-boundary payload path (ensure existing test still passes after Task 2 upsert)
  - E2E: `web/tests/sla-historical.spec.ts` (Playwright) — load `/sla`, assert 6 month cards, open month drawer, click operator row, see breach list, click Export PDF link → 200 `application/pdf`
  - Smoke: `make db-seed` + assertion script (`scripts/verify_sla_seed.sh` — run `psql -c` to assert row counts, exit non-zero on fail)
- **Depends on:** all prior tasks
- **Complexity:** medium
- **Pattern ref:** `internal/api/cdr/handler_test.go` (FIX-214 test style); `web/tests/cdr-explorer.spec.ts` if present; existing `internal/report/engine_test.go` fake provider for PDF render assertion.
- **AC covered:** all (validation of)
- **Context refs:** `## API Specs`, `## Business Rules`, `## Scope Decisions`
- **Outputs:** `go test ./...` green; Playwright suite green; `make db-seed` clean.

---

## Acceptance Criteria Mapping

| AC | Tasks | Verification |
|---|---|---|
| AC-1 (6-month summary cards, selectable year) | 5, 8, 10 | Playwright: 6 cards visible on default load; Year select shows 2026 → shows Jan–Apr; switch year → re-queries. |
| AC-2 (per-month drill-down) | 5, 8, 10 | Playwright: click month card → drawer lists operators with uptime/incidents/breaches. |
| AC-3 (per-operator month breach detail) | 2, 5, 8, 10 | Playwright: click operator row → breach timeline with start/end/cause/sessions. |
| AC-4 (PDF export) | 6, 7, 8, 10 | Playwright: click PDF link → response content-type `application/pdf`, filename pattern correct. |
| AC-5 (editable SLA target per operator) | 1, 2, 4, 9, 10 | Unit: store update + audit row; RTL: input validation; Playwright: save + reload shows new value. |
| AC-6 (breach = ≥5min down or latency>threshold) | 1, 2, 3, 10 | Store test: fixtures prove 3-min run rejected, 5-min accepted, latency-only accepted, gap splits into two. |
| AC-7 (24-month retention) | 1 | Migration merged; docs updated; no cleanup cron exists (verified by `grep -r sla_reports internal/job`). |

---

## Files to Touch

### Backend
- NEW: `migrations/20260422000001_sla_latency_threshold.up.sql` / `.down.sql`
- MOD: `migrations/seed/003_comprehensive_seed.sql`
- MOD: `internal/store/sla_report.go`
- MOD: `internal/store/operator.go`
- NEW: `internal/store/operator_breach_test.go`
- MOD: `internal/store/sla_report_test.go`
- MOD: `internal/api/sla/handler.go`
- MOD: `internal/api/sla/handler_test.go`
- MOD: `internal/api/operator/handler.go` (+ existing handler test file)
- MOD: `internal/report/store_provider.go` (SLAMonthly filter extension)
- MOD: `internal/gateway/router.go`
- MOD: `cmd/argus/main.go` (wire Engine into slaapi handler)
- MOD: `docs/architecture/db/_index.md` (TBL-17 retention note)

### Frontend
- REWRITE: `web/src/pages/sla/index.tsx`
- NEW: `web/src/pages/sla/month-detail.tsx`
- NEW: `web/src/pages/sla/operator-breach.tsx`
- NEW: `web/src/hooks/use-sla.ts`
- NEW: `web/src/types/sla.ts`
- MOD: `web/src/pages/operators/detail.tsx` (Protocols/SLA tab section)
- NEW: `web/tests/sla-historical.spec.ts`

### Scripts
- NEW: `scripts/verify_sla_seed.sh`

---

## Risks & Regression

- **R1 (Seed data volume):** extended `operator_health_logs` seed (1 sample/min × 60 days × 2 operators ≈ 173k rows) adds ~20–30 MB to fresh DB. Acceptable for dev; gate behind `NOT EXISTS` check so repeat `db-seed` is cheap.
- **R2 (Breach CTE performance):** CTE on hypertable chunks for a single operator-month scans ≤ 45k rows (90-day retention × 1/min). Index `idx_op_health_operator_time` covers the scan; target `< 200ms` at dev volume. If prod slower, fall back to pre-computed `details.breaches` in `sla_reports` (already persisted per Task 2).
- **R3 (PDF route accidentally public):** ensure `DownloadPDF` sits inside the tenant-scoped middleware group (verified via matching existing `/api/v1/sla-reports/{id}` registration line 769 which is already under the authenticated group).
- **R4 (Existing `/sla` snapshot consumers):** the old FE page called `/sla-reports?from=...&to=...`. We keep that endpoint untouched; only rewrite the FE that called it. Any external consumer relying on `/sla-reports` list keeps working.
- **R5 (FIX-248 collision):** when FIX-248 ships signed-URL reports, the `/api/v1/sla/pdf` streaming route must be marked for deprecation in `docs/architecture/REPORTS.md` (add Tech Debt entry to ROUTEMAP: "FIX-215 streaming PDF endpoint to be replaced by FIX-248 signed URL once merged").
- **R6 (Seed idempotency):** the `NOT EXISTS` guard uses `sla_reports LIMIT 1` — ensure prior partial state doesn't skip when only some months exist. Use tuple-based guard on `(tenant_id, operator_id, window_start)` upsert semantics inside the DO block.
- **R7 (Audit log path for target update):** `PATCH /operators/:id` must emit one audit event, not two (one per field). Verify by asserting single row in audit test.
- **R8 (Timezone ambiguity):** monthly boundaries are UTC. Document in API response `meta.timezone = "UTC"`. FE renders labels with `Intl.DateTimeFormat(..., { timeZone: 'UTC' })` to match.
- **Regression — existing snapshot `/sla` users:** we port the KPI/AnimatedCounter aesthetic; do not delete the Breadcrumb or Select imports. Old page-level refetch button kept.

---

## Test Plan Summary

- **Unit (Go):**
  - `internal/store/sla_report_test.go` — HistoryByMonth / MonthDetail / UpsertMonthlyRollup idempotency
  - `internal/store/operator_breach_test.go` — breach cases (see Task 2)
  - `internal/api/sla/handler_test.go` — all 4 new endpoints + PDF streaming
  - `internal/api/operator/handler_test.go` — new field validation + audit
- **Integration:** `make db-migrate up/down/up` roundtrip clean; `make db-seed` clean + row count assertions via `scripts/verify_sla_seed.sh`; `go test ./... -race` green.
- **Browser (Playwright):** `web/tests/sla-historical.spec.ts` — load, month strip count, operator matrix, drawer open, breach drawer, PDF download header assertion.
- **Manual smoke:** year switcher, operator filter, target editor (valid + invalid), PDF opens in browser viewer.

---

## Pre-Validation & Quality Gate (self-validated)

- [x] **Minimum substance (L story):** plan ≥100 lines ✓ (≈ 350 lines), tasks ≥ 5 ✓ (10 tasks), ≥1 high ✓ (Tasks 2 and 8 marked `high`).
- [x] **Required sections:** Goal, Architecture Context, Data Flow, API Specs, DB Schema, Business Rules, Screen Mockups, Design Token Map, Tasks, AC Mapping, Files to Touch, Risks, Test Plan — all present.
- [x] **Self-containment:** API shapes embedded; DB DDL embedded with migration source noted; screen wireframes embedded; business rules stated inline; no "see ARCHITECTURE.md" references that aren't also inlined.
- [x] **DB schema source:** Source noted for existing tables (`migrations/20260320000002_core_schema.up.sql`, `migrations/20260412000001_sla_reports.up.sql`, `migrations/20260320000003_timescaledb_hypertables.up.sql`); new migration identified.
- [x] **Task Context refs:** every task's `Context refs` lists sections that exist in this plan (verified).
- [x] **Pattern refs per task:** every task that creates or extends a file has a `Pattern ref` to an existing project file.
- [x] **API compliance:** standard envelope `{status, data, meta?}`; correct HTTP methods; validation step called out; error responses specified.
- [x] **DB compliance:** migration task first (Task 1); idempotent CREATE/ALTER; down migration required; tenant scoping preserved everywhere.
- [x] **UI compliance:** Design Token Map section present (UI story), tokens extracted from `docs/FRONTEND.md`, reusable components glob'd and listed.
- [x] **Scope decisions explicit:** AC-4 (Option a), Seed (in-scope), Breach source, Target storage, Retention — all decided and justified.
- [x] **Complexity cross-check:** L story with 2 tasks marked `high` (Tasks 2 and 8) ≥ 1 required — PASS.

**Quality Gate result: PASS.**
