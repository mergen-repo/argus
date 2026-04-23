# Implementation Plan: FIX-221 — Dashboard Polish (Heatmap Tooltip, IP Pool KPI Clarity)

## Goal
Expose raw byte totals on the Dashboard 7x24 traffic heatmap (tooltip shows "12.4 MB @ Mon 14:00") and clarify the IP Pool Usage KPI card semantics with an "avg across all pools" label and a "Top pool: <name> <pct>%" subtitle.

## Scope Discipline
- **In scope:**
  - Backend: `internal/api/dashboard/handler.go` (DTO additions), `internal/store/cdr.go` (heatmap query — expose raw bytes alongside normalized value), `internal/store/ippool.go` (new helper for top-pool summary).
  - Frontend: `web/src/pages/dashboard/index.tsx` (`TrafficHeatmap` and `IP Pool Usage` `KPICard` — both are inline components in this file; story's "`web/src/components/dashboard/...`" paths DO NOT EXIST — see DEV-295), `web/src/types/dashboard.ts`.
- **Out of scope — DEFERRED to future FIX:**
  - Dashboard MSISDN display (already addressed in FIX-220 for analytics), other dashboard panels, KPI sparkline color logic, any cache-invalidation changes.
  - Extraction of the IP-Pool KPI into its own component (story hinted at `ip-pool-kpi.tsx` but the project uses a single generic `KPICard` — refactoring is not scoped here; instead we upgrade `KPICard` to support an optional subtitle).
- **Story-spec file-path correction:**
  - Spec listed `web/src/components/dashboard/traffic-heatmap.tsx` and `web/src/components/dashboard/ip-pool-kpi.tsx`. These **do not exist**. The dashboard page is fully inline in `web/src/pages/dashboard/index.tsx`. See DEV-295 for the decision to edit inline rather than extract.
- **Unit tests DEFERRED** to D-091 per AUTOPILOT policy (FIX-220 precedent). Manual browser smoke per Test Plan.

## Findings Addressed
- **F-05 (primary):** Dashboard 7x24 heatmap has no value tooltip; IP Pool KPI card label ambiguous (does it mean total? average? top pool?).

## Architecture Context

### Components Involved
- **Backend (Go)**
  - `internal/store/cdr.go:955-1006` — `GetTrafficHeatmap7x24(ctx, tenantID) ([][]float64, error)` returns 7×24 normalized `[0,1]` matrix. Max-scaling loses the raw byte totals needed for the tooltip. Must be extended (new return type or parallel return value) to expose `raw_bytes`.
  - `internal/store/ippool.go:94-111` — `TenantPoolUsage(ctx, tenantID) (float64, error)` returns average utilization. Add new helper `TopPoolUsage(ctx, tenantID) (*TopPoolSummary, error)` returning `{ pool_id, name, usage_pct }` for the single most-utilized pool.
  - `internal/api/dashboard/handler.go:138-142, 162-529` — `trafficHeatmapCell` DTO adds `raw_bytes int64`; `dashboardDTO` adds optional `top_ip_pool *topIPPoolDTO`; goroutine at line 382-395 (IP pool) + goroutine at line 423-442 (heatmap) extended to populate the new fields.
- **Frontend (React)**
  - `web/src/pages/dashboard/index.tsx:423-524` — `TrafficHeatmap` inline component. Tooltip DIV already exists (line 499-506) but shows `{value} req/s` from the normalized float. Replace with "`<formattedBytes>` @ `<Day>` `<HH>`:00".
  - `web/src/pages/dashboard/index.tsx:1124-1133` — `IP Pool Usage` `KPICard` call. Add `subtitle="Top pool: <name> <pct>%"` prop and update `title` to include the clarifier.
  - `web/src/pages/dashboard/index.tsx:121-212` — `KPICard` inline component. Extend `KPICardProps` with optional `subtitle?: string` and render it under the value when present.
  - `web/src/types/dashboard.ts` — extend `TrafficHeatmapCell` with `raw_bytes: number`; add `TopIPPool` interface; extend `DashboardData` with optional `top_ip_pool`.
  - `web/src/lib/format.ts:65-70` — existing `formatBytes(n)`. Reuse. No changes.

### Data Flow

```
Browser (dashboard page)
  ↓ GET /api/v1/dashboard          (useDashboard hook)
internal/api/dashboard/handler.go (8 parallel goroutines)
  ├─ heatmap goroutine: store.cdrStore.GetTrafficHeatmap7x24WithRaw(tenant)  ← CHANGED (or dual return)
  │     returns matrix [7][24]float64 + raw [7][24]int64
  ├─ ippool goroutine: store.ippoolStore.TenantPoolUsage(tenant)             (existing)
  │                    store.ippoolStore.TopPoolUsage(tenant)                ← NEW
  └─ … other 6 goroutines
DTO envelope → Redis cache (30s) → response
Frontend:
  useDashboard() query → DashboardData
  <TrafficHeatmap data={data.traffic_heatmap} />           ← reads raw_bytes, shows in tooltip
  <KPICard title="Pool Utilization (avg across all pools)"
           value={m.ip_pool_usage_pct}
           subtitle={data.top_ip_pool && `Top pool: ${name} ${pct}%`} />
```

### API Specification — existing endpoint, additive fields only

`GET /api/v1/dashboard` — response envelope `{ status: "success", data: DashboardDTO }`.

New/changed fields in `data`:

```json
{
  "traffic_heatmap": [
    {
      "day": 0,                    // 0=Mon..6=Sun (unchanged)
      "hour": 14,                  // 0..23 (unchanged)
      "value": 0.73,               // existing: normalized 0..1 for color
      "raw_bytes": 13026148352     // NEW — AC-2: unsumed total_bytes_in+out for this bucket
    }
  ],
  "top_ip_pool": {                 // NEW — AC-3. Omitted when tenant has 0 active pools.
    "id": "uuid",
    "name": "Demo M2M",
    "usage_pct": 45.2
  }
}
```

All other fields unchanged. Additive — no frontend breakage if `raw_bytes` absent during rolling deploy (FE defaults to 0 → tooltip shows "0 B @ Mon 14:00", acceptable transient state).

### Database Schema (ACTUAL — no migration needed)

> Source: `internal/store/cdr.go:955` (SQL already sums `total_bytes_in + total_bytes_out`) + `internal/store/ippool.go:94` (already reads `used_addresses`, `total_addresses` from `ip_pools`). **Zero schema changes.**

Existing tables used:
- `cdrs_hourly` — columns already queried: `bucket`, `tenant_id`, `total_bytes_in`, `total_bytes_out`. The existing `GetTrafficHeatmap7x24` already computes `SUM(total_bytes_in + total_bytes_out)` per cell — it just throws the raw sum away after normalization. Fix: return both.
- `ip_pools` — columns: `id`, `tenant_id`, `name`, `state`, `used_addresses`, `total_addresses`. New query:
  ```sql
  SELECT id, name, used_addresses::float / NULLIF(total_addresses, 0) * 100 AS pct
  FROM ip_pools
  WHERE tenant_id = $1 AND state = 'active' AND total_addresses > 0
  ORDER BY pct DESC NULLS LAST
  LIMIT 1
  ```

### Screen Mockup (SCR-001 Dashboard — heatmap tooltip detail)

```
┌─ Traffic Pattern — Last 7 Days ─────────────────────────────────┐
│                                                                  │
│          00 03 06 09 12 15 18 21                                │
│    Mon   □  □  ▣  ▥  ▮  ▮  ▥  ▣    ┌─────────────────────────┐ │
│    Tue   □  ▣  ▥  ▮  ▮  ▥  ▣  □    │ 12.4 MB @ Mon 14:00     │ │
│    Wed   ▣  ▥  ▮  ▮  ▥  ▣  □  □    └─────────────────────────┘ │
│    ...                                 (hovered cell is outlined)│
│                                                                  │
│                              Low ▯▯▯▯▯ High                     │
└──────────────────────────────────────────────────────────────────┘
```

```
┌─ IP Pool Utilization (avg across all pools) ─┐
│                                               │
│       45.2%                     ↑ +2.1        │
│       ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━   │
│       Top pool: Demo M2M 45%                  │
└───────────────────────────────────────────────┘
```

### Design Token Map (UI tasks ONLY — MANDATORY)

> Read from `docs/FRONTEND.md` + current dashboard usage. Tokens are CSS custom properties in `index.css`; Tailwind classes map to them.

#### Color Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Primary text | `text-text-primary` | `text-[#e2e8f0]`, `text-white` |
| Secondary text | `text-text-secondary` | `text-[#94a3b8]`, `text-gray-400` |
| Tertiary / muted text | `text-text-tertiary` | `text-[#64748b]`, `text-gray-500` |
| Accent (cyan live) | `text-accent` / `bg-accent` | `text-[#00d4ff]`, `text-cyan-500` |
| Elevated panel bg (tooltip box) | `bg-bg-elevated` | `bg-white`, `bg-[#0f1419]` |
| Border | `border-border` | `border-[#1e293b]`, `border-gray-700` |

#### Typography Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Mono numeric / tooltip | `font-mono text-[11px]` (matches existing heatmap tooltip) | arbitrary sizes |
| KPI title | `text-[10px] uppercase tracking-[1.5px] font-medium` (existing `KPICard` pattern) | `text-xs` |
| KPI subtitle | `text-[10px] font-mono text-text-tertiary` (match existing sparkline labels) | `text-xs text-gray-400` |

#### Radius / Shadow
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Tooltip box | `rounded-[var(--radius-sm)]` `shadow-lg` (existing pattern) | `rounded-md`, custom shadow |

#### Existing Components to REUSE (DO NOT recreate)
| Component | Path | Use For |
|-----------|------|---------|
| `<KPICard>` (inline) | `web/src/pages/dashboard/index.tsx:135` | IP Pool Usage card — extend with `subtitle` prop |
| `<TrafficHeatmap>` (inline) | `web/src/pages/dashboard/index.tsx:423` | Tooltip content edit only — keep existing hover-tracking + positioned DIV pattern (multi-line beats the text-only `<Tooltip>` primitive here) |
| `formatBytes` | `web/src/lib/format.ts:65` | Heatmap tooltip byte formatting — already in dashboard imports (line 24) |
| `DAYS` constant | `web/src/pages/dashboard/index.tsx:28` | Weekday labels — reuse, no re-definition |

> **Do NOT** introduce a new tooltip primitive, a new components/dashboard subdirectory, a new formatter, or a new typography scale. All required primitives already exist.

## Prerequisites
- [x] FIX-220 merged (14198f4 — FIX-219; FIX-220 merged in 20ce0cf earlier today) — no code conflicts expected.
- [x] `formatBytes` helper present (used by FIX-220; line 65 of `web/src/lib/format.ts`).
- [x] Dashboard caching (Redis `dashboard:<tenant>` 30s TTL) — additive DTO keys are cache-safe; old cached responses during deploy will briefly lack `raw_bytes`/`top_ip_pool`. Risk #1 below.

## Story-Specific Compliance Rules
- **API:** Standard envelope `{ status, data }` preserved — additive fields only. No breaking changes.
- **DB:** No migration. Existing columns sufficient.
- **UI:** Reuse `formatBytes`, `DAYS`, inline `KPICard`/`TrafficHeatmap`. Zero hardcoded hex/px. `KPICard` subtitle prop must be optional (backward-compat for the other 7 KPI cards).
- **Business:** Heatmap raw-bytes is a derived display; no alerting/thresholds change. "Top pool" is the single most-utilized active pool; suspended/terminated pools excluded (matches `TenantPoolUsage` filter).
- **ADR:** N/A — cosmetic + additive data only.

## Bug Pattern Warnings
No matching bug patterns. (`docs/brainstorming/bug-patterns.md` — none of the catalogued patterns (SQL injection, N+1, stale cache on mutate, ASCII-Turkish) intersect this story: read-only query extension, additive DTO, inline component tooltip text.)

## Tech Debt (from ROUTEMAP)
No open Tech Debt items target FIX-221. (Recent D-106..D-113 are all FIX-220 scoped and resolved; D-091 (deferred unit tests) is cross-cutting and handled per AUTOPILOT policy, not per-story.)

## Mock Retirement
No mock retirement — backend endpoints are live; no `src/mocks/` directory in project.

## Tasks

> Waves are parallelizable; tasks within a wave have no cross-dependencies.
> - Wave 1 (parallel): Task 1, Task 2  (backend store extensions)
> - Wave 2 (parallel-after-W1): Task 3  (backend handler DTO wiring — depends on W1 both)
> - Wave 3 (parallel-after-W2): Task 4, Task 5, Task 6  (frontend types + heatmap tooltip + KPI subtitle)

### Task 1: Extend `GetTrafficHeatmap7x24` to return raw byte totals

- **Files:** Modify `internal/store/cdr.go` (around line 955-1006).
- **Depends on:** — (none — wave 1)
- **Complexity:** low
- **Pattern ref:** Read `internal/store/cdr.go:955-1006` — follow same `pgx.Query` → scan → build-matrix pattern. Preserve the 7×24 matrix shape.
- **Context refs:** "Architecture Context > Components Involved", "Database Schema", "API Specification".
- **What:**
  - Introduce a new return type OR a parallel return value. Preferred: new method `GetTrafficHeatmap7x24WithRaw(ctx, tenantID) (normalized [7][24]float64, raw [7][24]int64, err error)` OR a sibling type:
    ```go
    type TrafficHeatmapCell struct { Day, Hour int; Normalized float64; RawBytes int64 }
    func (s *CDRStore) GetTrafficHeatmap7x24V2(ctx, tenantID) ([]TrafficHeatmapCell, error)
    ```
  - Pick whichever minimizes handler churn — the Developer decides. The existing `GetTrafficHeatmap7x24` may be kept as a thin wrapper that discards raw to preserve any other callers (check with `grep -rn "GetTrafficHeatmap7x24" internal/` before touching — if it's only used by `dashboard/handler.go`, replace in place).
  - The single SQL query already computes `SUM(total_bytes_in + total_bytes_out)` per (dow, hour). Do not change SQL semantics — only preserve the raw `total` alongside normalization.
- **Verify:** `go build ./...` passes. `go vet ./internal/store/...` passes. Existing test `TestCDRStore_GetTrafficHeatmap7x24` (cdr_test.go:329) still passes (either because you kept the wrapper or because you updated the test to call the new signature; if you update, keep the invariant "values in [0,1]").

### Task 2: Add `TopPoolUsage` to `IPPoolStore`

- **Files:** Modify `internal/store/ippool.go` (append near `TenantPoolUsage` at line 94).
- **Depends on:** — (none — wave 1)
- **Complexity:** low
- **Pattern ref:** Read `internal/store/ippool.go:94-111` (`TenantPoolUsage`) — follow same `QueryRow` + `Scan` + zero-division guard pattern.
- **Context refs:** "Architecture Context > Components Involved", "Database Schema".
- **What:**
  - New struct:
    ```go
    type TopPoolSummary struct {
        ID       uuid.UUID
        Name     string
        UsagePct float64
    }
    ```
  - New method:
    ```go
    // TopPoolUsage returns the single most-utilized active IP pool for the tenant.
    // Returns (nil, nil) when the tenant has zero active pools with total_addresses > 0.
    func (s *IPPoolStore) TopPoolUsage(ctx context.Context, tenantID uuid.UUID) (*TopPoolSummary, error)
    ```
  - Query: `SELECT id, name, (used_addresses::float / total_addresses::float) * 100 AS pct FROM ip_pools WHERE tenant_id = $1 AND state = 'active' AND total_addresses > 0 ORDER BY pct DESC NULLS LAST LIMIT 1`.
  - On `pgx.ErrNoRows` → return `(nil, nil)`, NOT an error. Empty-tenant case is normal.
- **Verify:** `go build ./...` passes. `go vet` passes. No SQL injection (parameterized). Manually reason: ip_pools has `state` and `total_addresses` columns (confirmed — `TenantPoolUsage` already filters on both).

### Task 3: Wire heatmap raw bytes + top pool into `GetDashboard` DTO

- **Files:** Modify `internal/api/dashboard/handler.go`.
- **Depends on:** Task 1, Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/dashboard/handler.go:138-160` (DTO definitions) and `:382-442` (the two affected goroutines). Follow existing goroutine + mutex pattern — do NOT add a new goroutine; extend the two existing ones.
- **Context refs:** "Architecture Context > Data Flow", "API Specification", "Architecture Context > Components Involved".
- **What:**
  - Extend `trafficHeatmapCell` DTO:
    ```go
    type trafficHeatmapCell struct {
        Day       int     `json:"day"`
        Hour      int     `json:"hour"`
        Value     float64 `json:"value"`
        RawBytes  int64   `json:"raw_bytes"`
    }
    ```
  - Add DTO:
    ```go
    type topIPPoolDTO struct {
        ID       string  `json:"id"`
        Name     string  `json:"name"`
        UsagePct float64 `json:"usage_pct"`
    }
    ```
  - Add field to `dashboardDTO`: `TopIPPool *topIPPoolDTO `json:"top_ip_pool,omitempty"`` (`omitempty` — FE tolerates absence).
  - In the heatmap goroutine (`:423-442`): switch to Task 1's API. Populate `RawBytes` on each cell from the returned raw value.
  - In the ippool goroutine (`:382-395`): after the existing `TenantPoolUsage` call, call `h.ippoolStore.TopPoolUsage(ctx, tenantID)`. Mutex-locked assign to `resp.TopIPPool` (only when result is non-nil). On error, log warn + continue — do NOT fail the dashboard.
  - NO Redis cache key change — additive fields invalidate naturally after the 30s TTL. If existing cached responses are a concern, the Developer may bump the cache key suffix (e.g., `dashboard:v2:<tenant>`) — but given 30s TTL and low risk, simpler to just let stale entries age out (noted in Risk #1).
- **Verify:** `go build ./...` passes. `go vet` passes. `go test ./internal/api/dashboard/...` passes (update `handler_test.go` assertions only if they explicitly check the cell shape; additive JSON fields won't break existing assertions unless a test uses `DisallowUnknownFields`). `curl -H "Authorization: Bearer <token>" http://localhost:8084/api/v1/dashboard | jq '.data.traffic_heatmap[0]'` shows `raw_bytes`; `.data.top_ip_pool` present (or null if no active pool).

### Task 4: Extend frontend `Dashboard` types

- **Files:** Modify `web/src/types/dashboard.ts`.
- **Depends on:** Task 3 (type shape must match backend)
- **Complexity:** low
- **Pattern ref:** Read `web/src/types/dashboard.ts:64-68` — existing `TrafficHeatmapCell`. Follow same `export interface` style.
- **Context refs:** "API Specification".
- **What:**
  - Extend `TrafficHeatmapCell`:
    ```ts
    export interface TrafficHeatmapCell {
      day: number
      hour: number
      value: number
      raw_bytes: number        // NEW — AC-2. Tolerate missing during rolling deploy via `?? 0` at read site.
    }
    ```
  - Add new interface:
    ```ts
    export interface TopIPPool {
      id: string
      name: string
      usage_pct: number
    }
    ```
  - Extend `DashboardData`: `top_ip_pool?: TopIPPool | null` (matches `omitempty`).
- **Verify:** `cd web && bun run tsc --noEmit` passes (0 errors).

### Task 5: Update `TrafficHeatmap` tooltip to show "<bytes> @ <Day> <HH>:00"

- **Files:** Modify `web/src/pages/dashboard/index.tsx` (component block line 423-524).
- **Depends on:** Task 4
- **Complexity:** low
- **Pattern ref:** Read `web/src/pages/dashboard/index.tsx:423-524` — keep the existing `hoveredCell` state machine and absolutely-positioned tooltip DIV. Only edit the tooltip content (line 499-506) and the `setHoveredCell` payload (line 490).
- **Context refs:** "Screen Mockup", "Design Token Map", "Architecture Context > Components Involved".
- **What:**
  - Change `hoveredCell` state type to carry `rawBytes`:
    `useState<{ day: number; hour: number; value: number; rawBytes: number } | null>(null)`
  - At line 478, read the raw value too: from the `data` array lookup, or extend the `grid` Map to `Map<string, { value: number; rawBytes: number }>`. Prefer the Map variant — one pass builds both keys.
  - On mouse-enter: `setHoveredCell({ day: dayIdx, hour, value, rawBytes: cell.raw_bytes ?? 0 })`.
  - Replace tooltip content (existing line 500-506) with:
    ```tsx
    <div className="absolute top-0 right-0 bg-bg-elevated border border-border rounded-[var(--radius-sm)] px-2.5 py-1.5 text-[11px] font-mono pointer-events-none z-20 shadow-lg">
      <span className="text-accent font-semibold">{formatBytes(hoveredCell.rawBytes)}</span>
      <span className="mx-1.5 text-text-tertiary">@</span>
      <span className="text-text-secondary">{DAYS[hoveredCell.day]} {hoveredCell.hour.toString().padStart(2, '0')}:00</span>
    </div>
    ```
  - **Remove** the stale "req/s" suffix — the cell value is bytes, not req/s. (Secondary cleanup — the old tooltip showed a normalized 0..1 float labelled "req/s" which was doubly wrong.)
  - **Tokens:** `bg-bg-elevated`, `border-border`, `text-accent`, `text-text-secondary`, `text-text-tertiary`, `rounded-[var(--radius-sm)]`, `shadow-lg`, `font-mono text-[11px]` — all already used in the file. Zero new hex/px.
- **Verify:**
  - `cd web && bun run tsc --noEmit` passes.
  - `cd web && bun run build` succeeds.
  - `grep -E '#[0-9a-fA-F]{3,6}' web/src/pages/dashboard/index.tsx | grep -v '//'` → no NEW hex introduced (pre-existing rgba(0,212,255,..) in `cellColor` is allowed — not in scope).
  - Manual: open `/dashboard` → hover any heatmap cell → tooltip reads e.g. `12.4 MB @ Mon 14:00` (1024-base units match existing `formatBytes`: `B / KB / MB / GB / TB`).

### Task 6: Add `subtitle` prop to `KPICard` and apply to IP Pool Usage

- **Files:** Modify `web/src/pages/dashboard/index.tsx` (KPICard at :121-212, and the IP Pool Usage call site at :1124-1133).
- **Depends on:** Task 4
- **Complexity:** low
- **Pattern ref:** Read `web/src/pages/dashboard/index.tsx:121-212` — follow the existing `KPICardProps` shape and `CardContent` JSX. Add the subtitle UNDER the Sparkline (or between AnimatedCounter and Sparkline — Developer's visual call; prefer below sparkline to match mockup).
- **Context refs:** "Screen Mockup", "Design Token Map", "Architecture Context > Components Involved".
- **What:**
  - Extend `KPICardProps`:
    ```ts
    subtitle?: string  // optional small-text line rendered below the sparkline
    ```
  - In the card body (inside `CardContent`), AFTER the `<Sparkline ... />`:
    ```tsx
    {subtitle && (
      <p className="mt-1.5 text-[10px] font-mono text-text-tertiary truncate">
        {subtitle}
      </p>
    )}
    ```
  - Rendering rule: only render when `subtitle` is a non-empty string. The other 7 KPI cards omit the prop → no visual change.
  - Update the IP Pool Usage card call site (line ~1124):
    ```tsx
    <KPICard
      title="Pool Utilization (avg across all pools)"                   // CHANGED — keeps AC-3 clarifier IN the title
      subtitle={
        data.top_ip_pool
          ? `Top pool: ${data.top_ip_pool.name} ${data.top_ip_pool.usage_pct.toFixed(0)}%`
          : undefined                                                    // no subtitle when no pools; title alone satisfies AC-3
      }
      value={m.ip_pool_usage_pct}
      formatter={(n) => `${n.toFixed(1)}%`}
      ...
    />
    ```
    Rationale: AC-3 requires BOTH the label clarifier AND the "Top pool" subtitle **simultaneously**. Dropping "(avg across all pools)" from the title exactly when a top pool is shown would read as if the big percentage IS the top pool's 45%, when it is actually the fleet average. Therefore: title always carries the full "(avg across all pools)" clarifier; subtitle renders `Top pool: ...` only when `top_ip_pool` is non-null, and renders nothing when null (zero-pool tenant — title alone conveys the intent). The 10px-uppercase title will wrap to two lines on narrow viewports; this is accepted over losing the clarifier. If wrap-truncation becomes a visual issue, the Developer may apply `className="leading-tight"` to the title — but MUST NOT abbreviate.
  - **Tokens:** only existing classes (`text-[10px]`, `font-mono`, `text-text-tertiary`, `mt-1.5`, `truncate`). No new hex/px.
- **Verify:**
  - `cd web && bun run tsc --noEmit` passes.
  - `cd web && bun run build` succeeds.
  - Manual: `/dashboard` → `IP Pool Usage` card → title reads `POOL UTILIZATION (AVG ACROSS ALL POOLS)` (uppercase via CSS); when pools exist, subtitle reads `Top pool: Demo M2M 45%`; when no pools, no subtitle rendered. Other 7 KPI cards are visually unchanged (no stray subtitle line).

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1: Heatmap tooltip shows "<bytes> @ <Day> <HH>:00" on hover | Task 5 | Task 5 Verify (manual hover) |
| AC-2: Backend `/dashboard` `traffic_heatmap` includes `raw_bytes` per cell | Task 1, Task 3 | Task 3 Verify (`curl | jq`) |
| AC-3: IP Pool KPI: "Pool Utilization (avg across all pools)" + "Top pool: <name> <pct>%" | Task 2, Task 3, Task 6 | Task 6 Verify (manual inspection) |

## Test Plan (deferred unit tests — manual smoke only)

Per AUTOPILOT policy (D-091 open for all FIX-21x), no new Go unit tests or Playwright scripts in this story. Manual:

1. `make up` → wait for healthy → `/login` admin.
2. Open `/dashboard`. Verify:
   - Heatmap renders (already worked pre-FIX).
   - Hover cell → tooltip text matches regex `^\d+(\.\d+)? (B|KB|MB|GB|TB) @ (Mon|Tue|Wed|Thu|Fri|Sat|Sun) \d{2}:00$` (units match `formatBytes` — 1024-base, bare `KB/MB/…` not `KiB/MiB`).
   - IP Pool Usage card title reads "Pool Utilization"; subtitle reads "Top pool: <name> <N>%" (if any pools exist) or "avg across all pools" (if none).
3. Curl smoke:
   ```bash
   curl -s -H "Authorization: Bearer $(…)" http://localhost:8084/api/v1/dashboard \
     | jq '.data.traffic_heatmap[0], .data.top_ip_pool'
   ```
   First line must include `"raw_bytes":`. Second must be either object with `id/name/usage_pct` OR `null`.

## Risks & Mitigations

1. **Redis cache — stale payloads during rolling deploy.** Dashboard responses are cached for 30s; the first 30s post-deploy may serve payloads WITHOUT `raw_bytes`/`top_ip_pool`. FE tolerates both — tooltip shows "0 B" briefly, subtitle shows fallback text. Mitigation: accept 30s degraded window; OR (optional) bump cache key in Task 3 (`dashboard:v2:<tenant>`). Developer may choose — default is "accept the window" per KISS.
2. **`GetTrafficHeatmap7x24` in-place rename breaks other callers.** Mitigation: Developer MUST `grep -rn "GetTrafficHeatmap7x24" internal/` before changing signature. Only `dashboard/handler.go` is expected; confirm before rename, else keep the old function as a wrapper.
3. **`TopPoolUsage` on tenant with hundreds of pools.** Query uses `LIMIT 1` + an index-friendly `ORDER BY` on computed `pct` — acceptable at dashboard poll cadence (30s cache). No index change needed.
4. **Visual layout: subtitle adds ~14px height to ONE KPI card.** Since other 7 cards omit the prop, the grid row height will equal the tallest card — one subtitle row adds ~14px to the whole row. Acceptable per FIX-220 precedent (analytics tooltip extension accepted similar trade-off). If visual review flags it, remove subtitle for null `top_ip_pool` case (keep fallback inline with title). Noted, not blocked.
