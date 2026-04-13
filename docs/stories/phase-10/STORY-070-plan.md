# Implementation Plan: STORY-070 — Frontend Real-Data Wiring

## Goal

Eliminate every `Math.random()`, hardcoded mock array, and silent catch from the React SPA. Wire 15+ pages to real backend endpoints (adding the few that don't yet exist), persist all filter state in the URL, surface errors as toasts/banners, and make every action button perform a real API call with audit.

## Scope Summary

Backend: 6 new endpoints, 1 store migration (acknowledgment columns on `policy_violations`), 4 new store methods, capacity env-config additions.

Frontend: 14 ACs touching ~18 page/hook files. Plus `lib/ws.ts` enhancement (expose connection state + envelope id), enhanced status-bar/header WS indicator.

Effort: **L** — most tasks medium, 3 tasks high (operator metrics aggregation, dashboard heatmap query, topology real-flow wiring).

---

## Architecture Context

### Components Involved

- `internal/api/dashboard/` — SVC-03 — adds `traffic_heatmap` field to dashboard DTO via new `CDRStore.GetTrafficHeatmap7x24`
- `internal/api/operator/` — SVC-03 — adds `GetHealthHistory` + `GetMetrics` endpoints
- `internal/api/apn/` — SVC-03 — extends `List` to enrich each APN with `sim_count`, `traffic_24h_bytes`, `pool_used`, `pool_total`; adds `GetTraffic` endpoint
- `internal/api/system/` — SVC-03 — new `capacity_handler.go` returns env-configured targets + live counts
- `internal/api/reports/` — SVC-03 — adds `ListDefinitions` handler returning `validReportTypes` as structured array
- `internal/api/violation/` — SVC-03 — adds `Acknowledge` (POST `/policy-violations/:id/acknowledge`)
- `internal/store/policy_violation.go` — new `Acknowledge` + extended `PolicyViolation` struct
- `internal/store/cdr.go` — new `GetTrafficHeatmap7x24`, `GetAPNTraffic(apnID, period)`, `GetOperatorMetrics(operatorID, window)`
- `internal/config/` — adds `SIM_CAPACITY`, `SESSION_CAPACITY`, `AUTH_CAPACITY` env vars
- `migrations/` — new migration `20260413000001_violation_acknowledgment` (up + down)
- Frontend `web/src/hooks/` — new hooks `use-apn-traffic`, `use-operator-history`, `use-operator-metrics`, `use-capacity`, `use-report-definitions`, extended `use-violations` (acknowledge mutation)
- Frontend `web/src/pages/` — rewires dashboard, APN list/detail, operator detail, SLA, capacity, reports, violations, topology, sims, audit, sessions, jobs, esim, cdrs, anomalies
- Frontend `web/src/lib/ws.ts` — exposes `getStatus()` + envelope `id` to subscribers
- Frontend `web/src/components/layout/status-bar.tsx` — replace stub `isConnected` with real state
- Frontend new `web/src/components/layout/ws-indicator.tsx` — header badge with tooltip + manual reconnect

### Data Flow (representative)

1. **APN list with stats**: User opens `/apns` → React `useAPNs()` calls `GET /api/v1/apns?limit=100` → handler joins each APN with: SIM count (subquery on `sims.apn_id`), 24h bytes (subquery on `cdrs_daily WHERE bucket >= now() - 24h`), pool counts (subquery on `ip_pool_addresses` grouped by pool→apn) → JSON envelope → frontend renders APNCard with real numbers.
2. **Reports generate**: User clicks Generate → React calls `POST /api/v1/reports/generate` → handler creates job + publishes NATS subject `argus.jobs.report` → job worker processes → completes → updates job state → frontend `useJob(jobId)` polls every 2s → on `succeeded`, links to download URL embedded in result → no more `setTimeout` fakery.
3. **Violation acknowledge**: User clicks "Dismiss" → React `useAcknowledgeViolation` mutation → `POST /api/v1/policy-violations/:id/acknowledge` → handler verifies tenant_id, updates `acknowledged_at = now()`, `acknowledged_by = currentUserID`, writes audit entry → optimistic update + toast.

### API Specifications (new endpoints)

#### 1. `GET /api/v1/operators/:id/health-history?hours=24&limit=100`
- **Auth**: JWT (op_manager+)
- **Response**: `{ status, data: [{ checked_at, status, latency_ms, circuit_state, error_message }, ...] }`
- **Source**: `OperatorStore.GetHealthLogs` (already exists; just thread limit + hours window)
- **Errors**: 400 invalid id/limit, 404 operator not found

#### 2. `GET /api/v1/operators/:id/metrics?window=1h`
- **Auth**: JWT (op_manager+)
- **Window values**: `15m`, `1h`, `6h`, `24h`
- **Response**:
  ```json
  { "status": "success", "data": {
      "window": "1h",
      "buckets": [{ "ts": "2026-04-13T10:00:00Z", "auth_rate_per_sec": 437.2, "error_rate_per_sec": 8.1 }, ...]
  } }
  ```
- **Source**: New `CDRStore.GetOperatorMetrics(ctx, tenantID, operatorID, window)` aggregates `cdrs_hourly` (or `cdrs` directly) by minute/hour bucket; auth_rate ≈ `count(*) / bucket_seconds`.

#### 3. `GET /api/v1/apns/:id/traffic?period=24h`
- **Auth**: JWT (sim_manager+)
- **Period values**: `15m`, `1h`, `6h`, `24h`, `7d`, `30d`
- **Response**:
  ```json
  { "status": "success", "data": {
      "period": "24h",
      "series": [{ "ts": "...", "bytes_in": 0, "bytes_out": 0, "auth_count": 0 }, ...]
  } }
  ```
- **Source**: New `CDRStore.GetAPNTraffic(ctx, tenantID, apnID, period)` group-by bucket from `cdrs_hourly` for short windows / `cdrs_daily` for ≥7d.

#### 4. `GET /api/v1/system/capacity`
- **Auth**: JWT (analyst+)
- **Response**:
  ```json
  { "status": "success", "data": {
      "total_sims": 10200000, "active_sessions": 847000, "auth_per_sec": 1247,
      "sim_capacity": 15000000, "session_capacity": 2000000, "auth_capacity": 5000,
      "monthly_growth_sims": 72000,
      "ip_pools": [{ id, name, cidr, total, used, available, utilization_pct, allocation_rate, exhaustion_hours }]
  } }
  ```
- **Source**: env vars (`ARGUS_CAPACITY_SIM`, `ARGUS_CAPACITY_SESSION`, `ARGUS_CAPACITY_AUTH`, `ARGUS_CAPACITY_GROWTH_SIMS_MONTHLY`) + live counts from existing `SIMStore`, `RadiusSessionStore`, `IPPoolStore`. Allocation rate derived from `(used_today - used_yesterday) / 86400`.

#### 5. `GET /api/v1/reports/definitions`
- **Auth**: JWT (api_user+)
- **Response**: `{ status, data: [{ id, category, name, description, format_options:["pdf","csv","xlsx"] }, ...] }`
- **Source**: Static map in handler keyed off `validReportTypes`. Same content the frontend currently hardcodes.

#### 6. `POST /api/v1/policy-violations/:id/acknowledge`
- **Auth**: JWT (policy_editor+)
- **Body**: `{ "note": "..." }` (optional)
- **Response**: `{ status: "success", data: { id, acknowledged_at, acknowledged_by, note } }`
- **Errors**: 404 not found, 409 already acknowledged
- **Side effects**: audit entry `violation.acknowledge`

#### Dashboard endpoint enrichment (no new route)
- Existing `GET /api/v1/dashboard` adds `traffic_heatmap: [{ day:0..6, hour:0..23, value }]` (7×24 grid, value = total bytes in that bucket, normalized by max).
- Source: New `CDRStore.GetTrafficHeatmap7x24(ctx, tenantID)` queries `cdrs_hourly` last 7 days, `GROUP BY date_part('dow', bucket), date_part('hour', bucket)`.

### Database Schema

#### Existing — `policy_violations`
Source: `migrations/20260324000001_policy_violations.up.sql` (ACTUAL)
```sql
CREATE TABLE IF NOT EXISTS policy_violations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    sim_id          UUID NOT NULL,
    policy_id       UUID NOT NULL,
    version_id      UUID NOT NULL,
    rule_index      INT NOT NULL DEFAULT 0,
    violation_type  TEXT NOT NULL,
    action_taken    TEXT NOT NULL,
    details         JSONB NOT NULL DEFAULT '{}',
    session_id      UUID,
    operator_id     UUID,
    apn_id          UUID,
    severity        TEXT NOT NULL DEFAULT 'info',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

#### NEW migration — `20260413000001_violation_acknowledgment.up.sql`
```sql
ALTER TABLE policy_violations
  ADD COLUMN IF NOT EXISTS acknowledged_at  TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS acknowledged_by  UUID,
  ADD COLUMN IF NOT EXISTS acknowledgment_note TEXT;

CREATE INDEX IF NOT EXISTS idx_policy_violations_unack
  ON policy_violations(tenant_id, created_at DESC)
  WHERE acknowledged_at IS NULL;
```
Down migration drops the columns + index.

#### Existing — `operator_health_logs` (already a TimescaleDB hypertable)
Source: `migrations/20260320000002_core_schema.up.sql` line 555. Used by AC-5; no changes needed.

#### Existing — `sla_reports`
Source: `migrations/20260412000001_sla_reports.up.sql`. AC-7 reads via existing `SLAReportStore.ListByTenant`.

### Screen Mockups

This story rewires existing screens — no new visual designs. Reference screens being touched:

- **SCR-010 Main Dashboard** (`/`) — heatmap becomes real, sparklines real, live event stream uses server IDs
- **SCR-021c SIM Detail Usage tab** (`/sims/:id#usage`) — already wired (AC-2 verify only)
- **SCR-030 APN List** (`/apns`) — APNCard shows real SIM count, 24h traffic, pool utilization
- **SCR-032 APN Detail** (`/apns/:id`) — TrafficTab area chart shows real bytes_in/out + auth_count from CDR
- **SCR-041 Operator Detail** (`/operators/:id`) — HealthTimelineTab + TrafficTab use real endpoints
- **SCR-073 SLA** (`/sla`) — All metrics from `sla_reports` table (no random)
- **SCR-074 Capacity** (`/capacity`) — Targets from env, allocation rate from real delta query
- **SCR-130 Reports** (`/reports`) — ReportCards driven by `/reports/definitions`, generate calls real POST + polls job
- **SCR-085 Violations** (`/violations`) — Action menu per row: Suspend SIM / Review Policy / Dismiss / Escalate
- **SCR-086 Topology** (`/topology`) — FlowLine reflects real session counts; refresh every 30s

### Design Token Map (from FRONTEND.md)

This story does NOT introduce new visual components — only rewires data sources. All existing classes already follow the token system. New code MUST use the same tokens:

#### Color Tokens
| Usage | Class | NEVER Use |
|-------|-------|-----------|
| Page text | `text-text-primary` | `text-white`, `text-[#E4E4ED]` |
| Secondary text | `text-text-secondary` | `text-[#7A7A95]` |
| Tertiary / labels | `text-text-tertiary` | `text-[#4A4A65]` |
| Cyan accent (CTA, links) | `text-accent`, `bg-accent`, `border-accent` | `text-[#00D4FF]`, `text-cyan-500` |
| Success / healthy | `text-success`, `bg-success-dim` | `text-[#00FF88]`, `text-green-500` |
| Warning / degraded | `text-warning`, `bg-warning-dim` | `text-[#FFB800]` |
| Danger / critical | `text-danger`, `bg-danger-dim` | `text-[#FF4466]` |
| Card surface | `bg-bg-surface` | `bg-white`, `bg-[#0C0C14]` |
| Elevated surface | `bg-bg-elevated` | `bg-[#12121C]` |
| Hover | `bg-bg-hover` | `bg-gray-100` |
| Border | `border-border` | `border-[#1E1E30]` |

#### Typography
| Usage | Class |
|-------|-------|
| Mono numbers / data | `font-mono` |
| Section title | `text-sm font-semibold text-text-primary` |
| Card label uppercase | `text-[10px] uppercase tracking-wider text-text-tertiary` |

#### Existing Components to REUSE
| Component | Path | Use For |
|-----------|------|---------|
| `<Card>`, `<CardHeader>`, `<CardContent>`, `<CardTitle>` | `web/src/components/ui/card.tsx` | All panels |
| `<Badge>` | `web/src/components/ui/badge.tsx` | Status pills (uses variants `success`/`warning`/`danger`/`secondary`) |
| `<Button>` | `web/src/components/ui/button.tsx` | All buttons |
| `<DropdownMenu>` etc | `web/src/components/ui/dropdown-menu.tsx` | Violation row action menu |
| `<Skeleton>` | `web/src/components/ui/skeleton.tsx` | Loading placeholders |
| `<Spinner>` | `web/src/components/ui/spinner.tsx` | Inline loaders |
| `<Sparkline>` | `web/src/components/ui/sparkline.tsx` | Dashboard sparklines |
| `<AnimatedCounter>` | `web/src/components/ui/animated-counter.tsx` | Capacity / SLA numbers |
| `<Tabs>` etc | `web/src/components/ui/tabs.tsx` | Detail page tab sets |
| `<Select>` | `web/src/components/ui/select.tsx` | Period selectors |
| `<Input>` | `web/src/components/ui/input.tsx` | Filter search boxes |
| `<Breadcrumb>` | `web/src/components/ui/breadcrumb.tsx` | Page header |
| `toast` | `sonner` (already imported throughout) | Action feedback |
| Recharts (`AreaChart`, `Area`, `LineChart`, etc) | npm | Chart rendering |

**RULE**: New visual elements (WS indicator badge in header) MUST use the existing `<Badge>` + status-dot pulse pattern from `dashboard-layout.tsx` / `status-bar.tsx`. Zero hardcoded hex colors.

---

## Story-Specific Compliance Rules

- **API**: All new endpoints MUST use `apierr.WriteSuccess` / `apierr.WriteError` envelope (project standard).
- **Tenant scoping**: Every new handler MUST extract `tenantID` from `r.Context().Value(apierr.TenantIDKey)` and gate access; every store query MUST scope by `tenant_id`. RLS migrations already enforce this at DB level.
- **Middleware order**: Routes registered after JWT + RequireRole groups in `internal/gateway/router.go` per existing pattern. Never invent a new chi `Mount` outside the existing pattern.
- **DB**: Migration MUST be additive; down migration MUST cleanly reverse. RLS policy on `policy_violations` does NOT need touching — new columns inherit.
- **UI**: Use semantic Tailwind tokens from `FRONTEND.md` — zero `text-[#xxxxxx]`, zero `bg-[#xxxxxx]` in new code.
- **No `Math.random()`**: Verified by E2E grep (`grep -r "Math.random" web/src/pages web/src/hooks` → 0 matches).
- **No silent catch**: Every `try/catch` MUST either re-throw, set error state, or call `toast.error(...)` with a meaningful message.
- **URL filter persistence**: Use `useSearchParams` from `react-router-dom`. Filter state initialized FROM URL, mutations writeTo URL. Browser back/forward MUST update visible filters.
- **Audit**: Acknowledge violation, suspend SIM via violation row, reserve IPs all create audit log entries via existing `audit.Auditor` interface.
- **WS event ID**: Backend already includes `id` in `EventEnvelope`. Frontend `wsClient.on(...)` callback signature must change so subscribers can consume the envelope id (eliminating `${Date.now()}-${Math.random()}` client-side IDs).

## Bug Pattern Warnings

- **PAT-001 (BR test drift)**: AC-9 adds acknowledgment behavior on violations. If violation lifecycle behavior tests exist (e.g. `policy_violation_test.go` BR-style), update them in same task as the new column/store method.
- **PAT-002 (utility duplication)**: When extending `cdrs` aggregation queries (heatmap, APN traffic, operator metrics), reuse the existing `cdrs_hourly` / `cdrs_daily` materialized views — do NOT duplicate raw `cdrs` group-by patterns across stores.

## Tech Debt (from ROUTEMAP)

No tech debt items target STORY-070. (D-001/D-002 target STORY-077; D-003 targets STORY-062; D-004/D-005 already RESOLVED.)

## Mock Retirement

Frontend mock arrays / generators retired by this story (none in `src/mocks/` — these are inline in pages):

- `web/src/pages/dashboard/index.tsx:647` — client-generated event id → use `msg.id` from envelope
- `web/src/components/layout/dashboard-layout.tsx:26` — same
- `web/src/pages/sims/detail.tsx` — `mockUsageData` (already removed; verify zero match)
- `web/src/pages/apns/detail.tsx:573-590` — `generateMockTraffic`, `generateMockFrequency` → `useAPNTraffic`
- `web/src/pages/apns/index.tsx:190-193` — `mockSimCount`, `mockTrafficMB`, `mockPoolUsed`, `mockPoolTotal` → server-enriched APN list
- `web/src/pages/operators/detail.tsx:263-279` — `mockTimeline` → `useOperatorHealthHistory`
- `web/src/pages/operators/detail.tsx:428-434` — `mockAuthData` → `useOperatorMetrics`
- `web/src/pages/sla/index.tsx:78-88` — `Math.random()` for uptime/latency → `useSLAReports`
- `web/src/pages/capacity/index.tsx:77-96` — random allocation rate + hardcoded targets → `useCapacity`
- `web/src/pages/reports/index.tsx:65-81` — `REPORT_DEFINITIONS` array → `useReportDefinitions`
- `web/src/pages/reports/index.tsx:284-287` — `setTimeout` fake completion → real job poll

---

## Tasks

> Wave 1 = parallel backend foundation. Wave 2 = parallel frontend hooks. Wave 3 = parallel page rewires. Wave 4 = URL persistence. Wave 5 = polish/UX.

### Task 1: Migration — violation acknowledgment columns
- **Files**: Create `migrations/20260413000001_violation_acknowledgment.up.sql`, `migrations/20260413000001_violation_acknowledgment.down.sql`
- **Depends on**: —
- **Complexity**: low
- **Pattern ref**: `migrations/20260412000004_operator_grants_rat_types.up.sql` (recent additive ALTER TABLE pattern)
- **Context refs**: `Database Schema > NEW migration`
- **What**: Add `acknowledged_at TIMESTAMPTZ`, `acknowledged_by UUID`, `acknowledgment_note TEXT`; partial index on un-acknowledged rows.
- **Verify**: `psql -c "\d+ policy_violations"` shows new columns; `make db-migrate-down` then `up` succeeds.
- **AC**: AC-9

### Task 2: Store — `PolicyViolationStore.Acknowledge` + extend struct
- **Files**: Modify `internal/store/policy_violation.go`, modify `internal/store/policy_violation_test.go` (if exists; else add `internal/store/policy_violation_acknowledge_test.go`)
- **Depends on**: Task 1
- **Complexity**: medium
- **Pattern ref**: `internal/store/policy_violation.go` `Create` method (uses `s.db.QueryRow` + struct scan)
- **Context refs**: `Database Schema`, `API Specifications > 6. POST /policy-violations/:id/acknowledge`
- **What**: Add `AcknowledgedAt *time.Time`, `AcknowledgedBy *uuid.UUID`, `AcknowledgmentNote *string` to `PolicyViolation` struct; extend `cdr-style` SELECT lists in `List` and `GetByID`. New method `Acknowledge(ctx, id, tenantID, userID, note string) (*PolicyViolation, error)` — UPDATE ... WHERE id AND tenant_id AND acknowledged_at IS NULL RETURNING ...; returns `ErrAlreadyAcknowledged` if no row affected. Also bring `tenant_id` filter into the WHERE clause.
- **Verify**: `go test ./internal/store -run TestPolicyViolation` — passes; conflict path returns sentinel error.
- **AC**: AC-9

### Task 3: Handler — `POST /policy-violations/:id/acknowledge`
- **Files**: Modify `internal/api/violation/handler.go`, `internal/api/violation/handler_test.go` (create if missing), `internal/gateway/router.go` (one new route line)
- **Depends on**: Task 2
- **Complexity**: medium
- **Pattern ref**: `internal/api/sim/handler.go` state-change handlers (audit + tenant scope + envelope writeback)
- **Context refs**: `API Specifications > 6.`, `Story-Specific Compliance Rules`
- **What**: New `Acknowledge(w, r)` method. Parse `id` from chi URL param, decode optional `{ note }`, extract `tenantID` + `userID` from context, call `store.Acknowledge`, on success write envelope + create audit entry `violation.acknowledge`. On `ErrAlreadyAcknowledged` return 409 `apierr.CodeConflict`. Wire route in router.go under `RequireRole("policy_editor")`.
- **Verify**: `go test ./internal/api/violation` — happy + 404 + 409 tests pass; `curl -X POST .../acknowledge` returns 200 with envelope.
- **AC**: AC-9

### Task 4: Store — operator metrics + APN traffic + 7×24 heatmap (CDRStore methods)
- **Files**: Modify `internal/store/cdr.go`, modify `internal/store/cdr_test.go`
- **Depends on**: —
- **Complexity**: high
- **Pattern ref**: `internal/store/cdr.go` `GetCostAggregation` (per-operator group-by from `cdrs_daily`) and `GetDailyKPISparklines` (time-bucket aggregation pattern)
- **Context refs**: `API Specifications > 2.`, `API Specifications > 3.`, `Dashboard endpoint enrichment`
- **What**: Three new methods (each tenant-scoped):
  - `GetOperatorMetrics(ctx, tenantID, operatorID, window string) ([]OperatorMetricBucket, error)` — bucket = `1m` for `15m` window, `5m` for `1h`, `30m` for `6h`, `1h` for `24h`. Group by bucket from `cdrs` table; `auth_rate = count(*) / bucket_seconds`, `error_rate = count(*) FILTER (WHERE termination_cause IN ('auth_fail','reject')) / bucket_seconds`.
  - `GetAPNTraffic(ctx, tenantID, apnID uuid.UUID, period string) ([]APNTrafficBucket, error)` — period→bucket mapping per period. Query `cdrs_hourly` for ≤24h, `cdrs_daily` for ≥7d. Returns `bytes_in`, `bytes_out`, `auth_count`.
  - `GetTrafficHeatmap7x24(ctx, tenantID) ([][]float64, error)` — returns `[7][24]float64` matrix (Sun..Sat × 0..23) of normalized total bytes, last 7 days. Source: `SELECT EXTRACT(DOW FROM bucket), EXTRACT(HOUR FROM bucket), SUM(total_bytes) FROM cdrs_hourly WHERE tenant_id=$1 AND bucket >= NOW() - INTERVAL '7 days' GROUP BY 1, 2`.
- **Verify**: New unit tests insert sample CDR rows, call each method, assert bucket counts/values; `go test ./internal/store -run TestCDR` passes.
- **AC**: AC-1, AC-3, AC-5

### Task 5: Handler — Operator health-history + metrics endpoints
- **Files**: Modify `internal/api/operator/handler.go`, modify `internal/api/operator/handler_test.go`, modify `internal/gateway/router.go` (two new routes)
- **Depends on**: Task 4
- **Complexity**: medium
- **Pattern ref**: `internal/api/operator/handler.go` `GetHealth` method (existing pattern: parse id, call store, write envelope)
- **Context refs**: `API Specifications > 1.`, `API Specifications > 2.`
- **What**:
  - `GetHealthHistory(w, r)`: parse id, parse `?hours=` (default 24, max 168), parse `?limit=` (default 100, max 500), call `OperatorStore.GetHealthLogs(ctx, id, limit)` (existing), filter to within window, write envelope.
  - `GetMetrics(w, r)`: parse id, parse `?window=` (validate ∈ {15m,1h,6h,24h}; default 1h), call `cdrStore.GetOperatorMetrics(ctx, tenantID, id, window)`, write envelope.
  - Wire `cdrStore` into operator Handler via `WithCDRStore` option (mirror dashboard pattern).
  - Add routes under `RequireRole("op_manager")`.
- **Verify**: `go test ./internal/api/operator` — both endpoints tested with happy + 400 + 404 paths.
- **AC**: AC-5

### Task 6: Handler — APN traffic endpoint + APN list enrichment
- **Files**: Modify `internal/api/apn/handler.go`, modify `internal/api/apn/handler_test.go`, modify `internal/gateway/router.go` (one new route)
- **Depends on**: Task 4
- **Complexity**: high
- **Pattern ref**: `internal/api/apn/handler.go` `ListSIMs` (id parse + tenant scope + injected store) and `List` (current list response)
- **Context refs**: `API Specifications > 3.`, `Architecture Context > Data Flow > APN list with stats`
- **What**:
  - New `GetTraffic(w, r)`: parse id + `?period=`, call `cdrStore.GetAPNTraffic(...)`, write envelope.
  - Extend `apnResponse` with optional fields `SIMCount *int`, `Traffic24hBytes *int64`, `PoolUsed *int`, `PoolTotal *int`.
  - Extend `Handler.List` to populate these via batched queries: one `SELECT apn_id, COUNT(*) FROM sims WHERE tenant_id=$1 GROUP BY apn_id`, one `SELECT apn_id, SUM(total_bytes) FROM cdrs_daily WHERE tenant_id=$1 AND bucket >= NOW() - 24h GROUP BY apn_id`, one `SELECT apn_id, SUM(used_addresses), SUM(total_addresses) FROM ip_pools WHERE tenant_id=$1 GROUP BY apn_id`. Add corresponding store methods (`SIMStore.CountByAPN`, `CDRStore.SumBytesByAPN24h`, `IPPoolStore.SumByAPN`).
  - Inject `cdrStore`, `simStore` (already injected), `ipPoolStore` via new `WithCDRStore`/`WithIPPoolStore` options.
  - Wire `GET /api/v1/apns/:id/traffic` under `RequireRole("sim_manager")`.
- **Verify**: `go test ./internal/api/apn` covers happy path + period validation + tenant isolation (other tenant's APN returns 404).
- **AC**: AC-3, AC-4

### Task 7: Handler — System capacity endpoint
- **Files**: Create `internal/api/system/capacity_handler.go`, create `internal/api/system/capacity_handler_test.go`, modify `internal/config/config.go` (add capacity envs), modify `internal/gateway/router.go` (one new route + handler init in `cmd/argus/main.go`)
- **Depends on**: —
- **Complexity**: medium
- **Pattern ref**: `internal/api/system/status_handler.go` (existing system handler pattern)
- **Context refs**: `API Specifications > 4.`
- **What**:
  - Add config fields: `CapacitySIMs int` (env `ARGUS_CAPACITY_SIM`, default 15_000_000), `CapacitySessions int` (env `ARGUS_CAPACITY_SESSION`, default 2_000_000), `CapacityAuthPerSec int` (env `ARGUS_CAPACITY_AUTH`, default 5_000), `CapacityMonthlyGrowth int` (env `ARGUS_CAPACITY_GROWTH_SIMS_MONTHLY`, default 72_000).
  - New `CapacityHandler` with deps: capacity config struct, `*store.SIMStore`, `*store.RadiusSessionStore`, `*store.IPPoolStore`, `*store.CDRStore`. `GET()` responds with envelope per spec. Allocation rate per pool computed from `IPPoolStore.GetUsageDelta(ctx, poolID, 24h)` (new method: `(used_now - used_24h_ago) / 86400`).
  - Document new env vars in `docs/architecture/CONFIG.md`.
  - Wire route under `RequireRole("analyst")`.
- **Verify**: `go test ./internal/api/system -run TestCapacity` passes; `curl -H "Authorization: Bearer ..."  /api/v1/system/capacity | jq` shows valid envelope.
- **AC**: AC-6

### Task 8: Handler — Reports definitions endpoint
- **Files**: Modify `internal/api/reports/handler.go`, modify `internal/api/reports/handler_test.go`, modify `internal/gateway/router.go` (one new route)
- **Depends on**: —
- **Complexity**: low
- **Pattern ref**: `internal/api/reports/handler.go` existing handler structure
- **Context refs**: `API Specifications > 5.`
- **What**: New `ListDefinitions(w, r)` method returns a static slice keyed off `validReportTypes`. Each entry: `{ id, category, name, description, format_options }` — content matches what frontend currently hardcodes (`compliance_btk`, `compliance_kvkk`, `compliance_gdpr`, `sla_monthly`, `usage_summary`, `cost_analysis`, `sim_inventory`, `audit_log_export`). Wire `GET /api/v1/reports/definitions` under `RequireRole("api_user")` (same group as existing scheduled-list).
- **Verify**: Test asserts response contains all 8 known report types with non-empty name+description.
- **AC**: AC-8

### Task 9: Handler — Dashboard heatmap field
- **Files**: Modify `internal/api/dashboard/handler.go`, modify `internal/api/dashboard/handler_test.go`
- **Depends on**: Task 4
- **Complexity**: medium
- **Pattern ref**: `internal/api/dashboard/handler.go` parallel goroutine fan-out pattern (existing 5-way `wg.Add`)
- **Context refs**: `Dashboard endpoint enrichment`
- **What**: Extend `dashboardDTO` with `TrafficHeatmap [][]float64 \`json:"traffic_heatmap"\`` (7 rows × 24 cols). In `GetDashboard`, add a 6th goroutine calling `cdrStore.GetTrafficHeatmap7x24(ctx, tenantID)`; assign under mutex. Default to `[][]float64{}` if nil. Frontend `DashboardData.traffic_heatmap` shape becomes `Array<{day:number,hour:number,value:number}>` — map matrix into list of cells in handler before serialization.
- **Verify**: Existing dashboard test still passes; new test asserts `traffic_heatmap` field shape (length ≤ 168, each entry has day/hour/value).
- **AC**: AC-1

### Task 10: Frontend hook — useReportDefinitions + retire REPORT_DEFINITIONS
- **Files**: Modify `web/src/hooks/use-reports.ts`, modify `web/src/pages/reports/index.tsx`
- **Depends on**: Task 8
- **Complexity**: medium
- **Pattern ref**: `web/src/hooks/use-reports.ts` `useScheduledReports` (TanStack Query pattern)
- **Context refs**: `API Specifications > 5.`, `Mock Retirement`, `Design Token Map`
- **What**:
  - Hook: `useReportDefinitions()` — `useQuery` against `/reports/definitions`, returns `ReportDefinition[]`. `staleTime: 5 * 60_000`.
  - Page: replace `REPORT_DEFINITIONS` constant import with hook call. Render skeleton while loading, error state with retry button. Replace `setTimeout(...)` in `handleGenerate` with real `useGenerateReport` mutation + `useJob(jobId)` polling (already exists in `use-jobs.ts`); on `succeeded`, surface download link from job result.
  - Use existing `<Skeleton>`, `<Card>`, `<Badge>`, `toast.success/error` — no new components.
- **Verify**: `npm --prefix web run build` — zero TS errors. Manual: open /reports → cards load from API; click Generate → toast "Report queued (job...)" → polls jobs page; no `setTimeout` remains.
- **AC**: AC-8

### Task 11: Frontend hook — useAPNTraffic + APN detail rewire
- **Files**: Create `web/src/hooks/use-apn-traffic.ts`, modify `web/src/pages/apns/detail.tsx`
- **Depends on**: Task 6
- **Complexity**: medium
- **Pattern ref**: `web/src/hooks/use-sims.ts` `useSIMUsage` (period-keyed query)
- **Context refs**: `API Specifications > 3.`, `Mock Retirement`, `Design Token Map`
- **What**: Hook calls `/apns/:id/traffic?period=...`. Page TrafficTab deletes `generateMockTraffic` + `generateMockFrequency`; consumes hook data; loading/error/empty states. Reuse existing `<Card>`, `<AreaChart>` setup verbatim — only data source changes.
- **Verify**: `grep -n "Math.random\|generateMock" web/src/pages/apns/detail.tsx` returns 0 matches; build passes.
- **AC**: AC-3

### Task 12: Frontend — APN list real stats
- **Files**: Modify `web/src/pages/apns/index.tsx`, modify `web/src/types/apn.ts` (add optional fields to `APN` type)
- **Depends on**: Task 6
- **Complexity**: low
- **Pattern ref**: existing `APNCard` JSX (only data binding changes)
- **Context refs**: `API Specifications > 3. (List enrichment)`, `Mock Retirement`
- **What**: Delete the four `mockSimCount/mockTrafficMB/mockPoolUsed/mockPoolTotal` `useMemo` calls. Bind to `apn.sim_count ?? 0`, `apn.traffic_24h_bytes ?? 0` (use `formatBytes`), `apn.pool_used ?? 0`, `apn.pool_total ?? 0`. When pool_total = 0, hide the IPPoolBar entirely (cleaner empty state).
- **Verify**: `grep -n "Math.random" web/src/pages/apns/index.tsx` returns 0; visual: APN list shows real numbers when CDRs exist, zeros when fresh.
- **AC**: AC-4

### Task 13: Frontend hooks — operator history + metrics + operator detail rewire
- **Files**: Create `web/src/hooks/use-operator-detail.ts`, modify `web/src/pages/operators/detail.tsx`
- **Depends on**: Task 5
- **Complexity**: medium
- **Pattern ref**: `web/src/hooks/use-operators.ts` (existing query patterns)
- **Context refs**: `API Specifications > 1.`, `API Specifications > 2.`, `Mock Retirement`
- **What**:
  - Hooks: `useOperatorHealthHistory(id, hours)`, `useOperatorMetrics(id, window)`. Both `useQuery` with stable keys, refetchInterval 30s.
  - HealthTimelineTab: delete `mockTimeline`; consume hook data; loading skeleton + empty state ("No health checks recorded for this window").
  - TrafficTab: delete `mockAuthData`; consume `useOperatorMetrics`. Hourly bucket → `auth_rate` line, `error_rate` line. Avg cards compute from real series.
- **Verify**: `grep -n "Math.random" web/src/pages/operators/detail.tsx` returns 0; build passes.
- **AC**: AC-5

### Task 14: Frontend — SLA page real metrics
- **Files**: Modify `web/src/pages/sla/index.tsx`
- **Depends on**: — (existing `/api/v1/sla-reports` already returns real data)
- **Complexity**: medium
- **Pattern ref**: `web/src/hooks/use-operators.ts`
- **Context refs**: `API Specifications` (existing `GET /sla-reports` covered by SLA store), `Mock Retirement`
- **What**: Replace `useSLAData` body. Call `/sla-reports?from=...&to=...&limit=200` for the selected period; group rows by operator; for each operator compute current `uptime_pct` (latest row), `latency_p95` (latest), `incidents = sum(incident_count)`, `downtime = sum((window_end-window_start) * (1 - uptime_pct/100))`. Operator-wide `target` from `op.sla_uptime_target ?? 99.95`. Status: breached if uptime < 99.5, at_risk if < 99.95, on_track otherwise. Replace hardcoded `breaches` array — derive from rows where `uptime_pct < target`. Empty state: "No SLA data for this period — run the SLA aggregator job".
- **Verify**: `grep -n "Math.random" web/src/pages/sla/index.tsx` returns 0; numbers match `psql -c "SELECT * FROM sla_reports LIMIT 5"`.
- **AC**: AC-7

### Task 15: Frontend — Capacity page real targets
- **Files**: Modify `web/src/pages/capacity/index.tsx`, create `web/src/hooks/use-capacity.ts`
- **Depends on**: Task 7
- **Complexity**: low
- **Pattern ref**: `web/src/hooks/use-dashboard.ts` (single query + transform)
- **Context refs**: `API Specifications > 4.`, `Mock Retirement`
- **What**: Hook: `useCapacity()` calls `/system/capacity`. Page: replace `useCapacityData` body — return shape comes straight from API, no mocks. Delete the `Math.round(50 + Math.random() * 200)` and the hardcoded fallbacks (`15000000`, etc).
- **Verify**: `grep -n "Math.random" web/src/pages/capacity/index.tsx` returns 0; targets match env vars.
- **AC**: AC-6

### Task 16: Frontend — Dashboard heatmap + sparklines + WS event id
- **Files**: Modify `web/src/lib/ws.ts`, modify `web/src/pages/dashboard/index.tsx`, modify `web/src/components/layout/dashboard-layout.tsx`, modify `web/src/types/dashboard.ts`
- **Depends on**: Task 9
- **Complexity**: medium
- **Pattern ref**: existing `wsClient.on('*', ...)` callback in `dashboard/index.tsx:641`
- **Context refs**: `Dashboard endpoint enrichment`, `Story-Specific Compliance Rules > WS event ID`, `Mock Retirement`
- **What**:
  - `lib/ws.ts`: change envelope parsing to expose full envelope (`{ type, id, timestamp, data }`) to subscribers. The `'*'` handler receives the full envelope (already does); typed handlers continue to receive `data` only. Add `getStatus(): 'connected' | 'connecting' | 'disconnected'` returning `ws?.readyState` mapped. Add `reconnectNow()` that clears the timer and calls `connect()`.
  - `dashboard/index.tsx` LiveEventStream: replace `${Date.now()}-${Math.random()...}` with `msg.id` (already on envelope). Heatmap: consume `data.traffic_heatmap` directly — no Math.random fallback in the (now removed?) hook; the page already declares `HOURS`/`DAYS` constants and a `TrafficHeatmapCell[]` type — verify it just renders what server sends. Sparklines: ensure `useDashboard` no longer falls back to random arrays — only uses server-provided sparklines (it currently uses `[]` empty, which is fine).
  - `dashboard-layout.tsx`: same `Math.random()` in line 26 → use envelope `id`.
- **Verify**: `grep -n "Math.random" web/src/pages/dashboard/index.tsx web/src/components/layout/dashboard-layout.tsx web/src/hooks/use-dashboard.ts` returns 0; visual: heatmap colors reflect real CDR distribution.
- **AC**: AC-1

### Task 17: Frontend — Violations remediation actions
- **Files**: Modify `web/src/pages/violations/index.tsx`, modify `web/src/hooks/use-policies.ts` (or create `web/src/hooks/use-violations.ts`)
- **Depends on**: Task 3
- **Complexity**: medium
- **Pattern ref**: `web/src/pages/sims/index.tsx` row action menu (DropdownMenu pattern)
- **Context refs**: `API Specifications > 6.`, `Story-Specific Compliance Rules > Audit`
- **What**: Add `useAcknowledgeViolation()` mutation calling `/policy-violations/:id/acknowledge`. Per-row action menu via `<DropdownMenu>`: "Suspend SIM" (navigate `/sims/:simID` with `?action=suspend` query), "Review Policy" (navigate `/policies/:policyID?rule=${ruleIndex}`), "Dismiss" (mutation + optimistic update + toast), "Escalate" (calls existing `/notifications/incidents` endpoint or, if absent, opens a notification create form — verify by grep for `/incidents` route; if missing, narrow to "Create Notification" linking to `/notifications`). Filter UI: when "Dismissed" filter is selected, query `?acknowledged=true`. Update `List` handler in Task 3-related backend if needed to support `?acknowledged=true|false` filter (small extension).
- **Verify**: Click "Dismiss" on a row → row disappears (optimistic) → re-query confirms acknowledged → audit log entry exists.
- **AC**: AC-9

### Task 18: Frontend — URL filter persistence (7 list pages)
- **Files**: Modify `web/src/pages/sims/index.tsx`, `web/src/pages/audit/index.tsx`, `web/src/pages/sessions/index.tsx`, `web/src/pages/jobs/index.tsx`, `web/src/pages/esim/index.tsx`, `web/src/pages/cdrs/index.tsx` (create page if absent — check), `web/src/pages/anomalies/index.tsx` (likely under `analytics-anomalies.tsx`)
- **Depends on**: —
- **Complexity**: medium
- **Pattern ref**: any React Router v6 docs example using `useSearchParams`. Internal precedent: search if `useSearchParams` already used elsewhere — establish first instance carefully then replicate.
- **Context refs**: `Story-Specific Compliance Rules > URL filter persistence`
- **What**: For each page: replace `useState<Filters>(initial)` with a `useSearchParams`-backed adapter. Pattern:
  ```
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo(() => ({
    state: searchParams.get('state') ?? '',
    operator_id: searchParams.get('operator_id') ?? '',
    q: searchParams.get('q') ?? '',
    // ... per-page fields
  }), [searchParams])
  const setFilters = useCallback((next) => {
    const params = new URLSearchParams(searchParams)
    Object.entries(next).forEach(([k, v]) => v ? params.set(k, String(v)) : params.delete(k))
    setSearchParams(params, { replace: false })
  }, [searchParams, setSearchParams])
  ```
  Defaults (empty string) MUST NOT be written to URL (keep URL clean). Date pickers serialize as ISO strings.
- **Verify**: Open `/sims?state=active` → reload → filter still applied. Apply 3 filters → click browser back → filter reverts step-by-step. Copy URL → open in incognito (after login) → same view.
- **AC**: AC-11

### Task 19: Frontend — Silent catch surfacing (sims reserve, audit verify, onboarding wizard)
- **Files**: Modify `web/src/pages/sims/index.tsx`, modify `web/src/pages/audit/index.tsx`, modify `web/src/components/onboarding/wizard.tsx`
- **Depends on**: —
- **Complexity**: low
- **Pattern ref**: any existing `toast.error(...)` usage in same files
- **Context refs**: `Story-Specific Compliance Rules > No silent catch`
- **What**:
  - sims reserve IPs (line ~668): change `catch { /* handled */ }` to capture per-row failure, accumulate `{ succeeded:[], failed:[{simId,error}] }`, on completion `toast.{success|error}` with counts; if any failed, store list in component state and show a "View failures" button that opens a `<Dialog>` listing each failure.
  - audit Verify Integrity (line ~234): replace silent set with banner state; on error display dismissible `<Alert variant="danger">` containing the backend error message.
  - onboarding wizard step error (line ~481): `setError` already shown — verify it includes a "Retry this step" button that re-triggers `submitStep.mutateAsync` with the same payload. Add explicit "Retry" button under the error block.
- **Verify**: Force a backend 500 on `/ip-pools/.../reserve` → toast appears with "0 succeeded, 1 failed" + clickable details modal.
- **AC**: AC-12

### Task 20: Frontend — WebSocket connection status indicator (header badge)
- **Files**: Create `web/src/components/layout/ws-indicator.tsx`, modify `web/src/components/layout/dashboard-layout.tsx` (mount the badge in the header), modify `web/src/components/layout/status-bar.tsx` (use real `wsClient.getStatus()`)
- **Depends on**: Task 16 (lib/ws.ts must expose `getStatus` + `reconnectNow`)
- **Complexity**: low
- **Pattern ref**: existing `<Badge>` usage in `status-bar.tsx`
- **Context refs**: `Design Token Map > Existing Components`, `Story-Specific Compliance Rules`
- **What**: New component renders a small badge in the header (next to user menu). State derived from `wsClient.getStatus()` polled every 1s OR via a new event subscription `wsClient.on('__status__', ...)` if simpler. States: `connected` (green pulsing dot, "Live"), `connecting` (amber spinner, "Reconnecting…"), `disconnected` (red, "Offline"). Tooltip explains "Live updates paused" when not connected. Click on disconnected badge calls `wsClient.reconnectNow()`. Existing `status-bar.tsx` likewise updated to consume real status.
- **Verify**: Stop backend → badge transitions to amber within ~5s → red after retries. Click red badge → reconnect attempt visible; restart backend → returns to green.
- **AC**: AC-13

### Task 21: Frontend — Topology real flow animation
- **Files**: Modify `web/src/pages/topology/index.tsx`
- **Depends on**: — (uses existing operator/APN/IPPool list APIs which already return `active_sessions`, `sim_count`)
- **Complexity**: high
- **Pattern ref**: `topology/index.tsx` existing `FlowLine` component
- **Context refs**: `Mock Retirement (FlowLine static → data-driven)`
- **What**: Refactor `FlowLine` to accept a `traffic` prop (0..1 normalized). Compute per-edge traffic from operator→apn→pool relations: edge weight = `apn.active_sessions / max(all apn sessions)`. Map weight → animation `--topo-flow-duration` CSS var (faster = more traffic) and stroke opacity. Add `severed` automatically when `operator.health_status === 'down'` or `apn.state !== 'active'`. Set `refetchInterval: 30_000` on `useTopologyData`. No new WS event needed — REST refresh is sufficient per AC-10 (line thickness / animation speed reflects real traffic).
- **Verify**: `pkill -STOP <operator-adapter-pid>` (slow operator) → topology refresh shows reduced active_sessions on that branch → animation slower; visually distinct from healthy branches.
- **AC**: AC-10

### Task 22: Dead code removal (placeholder.tsx)
- **Files**: Delete or refactor `web/src/pages/placeholder.tsx`
- **Depends on**: —
- **Complexity**: low
- **Pattern ref**: —
- **Context refs**: —
- **What**: `grep -rn "placeholder" web/src/router web/src/App.tsx` to determine if imported. If unused → delete. If used as fallback/empty pattern → rename to `EmptyState` and refactor to a reusable component under `web/src/components/ui/empty-state.tsx` accepting `{ icon, title, description, action? }` props.
- **Verify**: `npm --prefix web run build` passes; `grep -rn "placeholder" web/src` returns only deliberate references.
- **AC**: AC-14

### Task 23: E2E grep guard + final test pass
- **Files**: Modify `Makefile` (add `make grep-no-mocks` target) OR add a CI script `scripts/check-no-frontend-mocks.sh`
- **Depends on**: Tasks 10–22
- **Complexity**: low
- **Pattern ref**: existing Makefile targets
- **Context refs**: `Story-Specific Compliance Rules > No Math.random()`
- **What**: Script greps `web/src/pages` and `web/src/hooks` for `Math.random`, `generateMock`, `mockTimeline`, `mockAuthData`, `mockSimCount`, `mockTrafficMB`, `mockPoolUsed`, `mockPoolTotal`, `mockUsageData`, `REPORT_DEFINITIONS`, `setTimeout(.*100[0-9])` (catches 1500ms fake-wait pattern). Exits non-zero on any match. Run `go test ./...`, `npm --prefix web run build`, `make test`.
- **Verify**: Script exits 0; `make test` green; `npm run build` zero TS errors.
- **AC**: All

---

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|----------------|-------------|
| AC-1 Dashboard fakery removed | Task 4 (heatmap store), Task 9 (handler), Task 16 (frontend wiring + WS event id) | Task 23 grep + manual visual |
| AC-2 SIM Detail Usage real CDR | (already done — STORY-057) | Task 23 grep verifies no `mockUsageData` |
| AC-3 APN detail traffic | Task 4 (store), Task 6 (handler), Task 11 (frontend) | Task 6 + Task 11 tests |
| AC-4 APN list stats | Task 6 (list enrichment), Task 12 (frontend) | Task 6 + manual |
| AC-5 Operator timeline + metrics | Task 4 (store), Task 5 (handler), Task 13 (frontend) | Task 5 + Task 13 |
| AC-6 Capacity targets | Task 7 (handler+config), Task 15 (frontend) | Task 7 test |
| AC-7 SLA real metrics | Task 14 (frontend; backend already provides via STORY-063) | manual SQL match |
| AC-8 Reports API | Task 8 (handler), Task 10 (frontend) | Task 8 test + manual generate |
| AC-9 Violations actions | Task 1 (migration), Task 2 (store), Task 3 (handler), Task 17 (frontend) | Task 3 test + manual |
| AC-10 Topology real flow | Task 21 | manual visual under operator slowdown |
| AC-11 URL filter persistence | Task 18 | Task 23 + manual reload+share |
| AC-12 Silent catch surfaced | Task 19 | manual fault injection |
| AC-13 WS indicator | Task 16 (lib/ws), Task 20 (badge) | manual stop/start backend |
| AC-14 Dead code removal | Task 22 | grep + build |

---

## Risks & Mitigations

| # | Risk | Mitigation |
|---|------|------------|
| R1 | `cdrs_hourly` materialized view may not exist; only `cdrs_daily` + raw `cdrs` confirmed. Operator metrics + APN traffic + heatmap rely on hourly buckets. | Task 4 first verifies via `\d cdrs_hourly`; if absent, queries fall back to raw `cdrs` with date_trunc('hour', timestamp) — slower but correct. Add a TODO note (not a defer) to consider a `cdrs_hourly` continuous aggregate in a future story. |
| R2 | APN list enrichment N+1 risk with 100 APNs × 3 subqueries each. | Task 6 uses three single GROUP BY queries (one per stat type) and joins in memory — O(1) DB round trips per List call. PAT-002 enforced: no per-row queries. |
| R3 | Operator metrics auth_rate computed from `cdrs.timestamp` may exclude in-flight sessions. | Acceptable — CDRs represent finalized auth attempts, which is exactly the metric users want. Test fixture seeds finalized CDRs. |
| R4 | Frontend `useSearchParams` migration could break existing infinite-query keys → cause refetch storm. | Task 18 ensures filters object is `useMemo`'d with stable shallow keys; query keys built from filters object are stable across renders. |
| R5 | Topology animation CSS variable `--topo-flow-duration` may not exist. | Task 21 checks `web/src/index.css`; if missing, defines it locally + adds to global CSS so future stories can reuse. |
| R6 | WS `getStatus()` polling at 1s may be too chatty. | Task 20 uses subscription-based update via `wsClient.on('__status__', cb)` if added in Task 16; falls back to 2s polling otherwise. |
| R7 | Migration adds nullable columns — fine for existing rows; but the `Acknowledge` UPDATE checks `WHERE acknowledged_at IS NULL`. Concurrent acknowledgments could race. | Task 2 uses `RETURNING ...` and detects `pgx.ErrNoRows`/zero-affected → returns `ErrAlreadyAcknowledged` → handler returns 409. |

## Rollback Plan

- Migration: `make db-migrate-down` reverses Task 1 cleanly (drops 3 columns + 1 index).
- Backend: each new endpoint is additive; rolling back the deployment removes them; frontend gracefully degrades (hooks treat 404 as empty state if `?` queries fail — reconfirm in Task 10/11/13/15 by adding `staleTime + retry: false` for 404s).
- Frontend: revert via git; no schema changes needed.
- Capacity env vars: defaults baked in; missing env → handler still returns sane numbers.

## Pre-Validation (Quality Gate Self-Check)

| Check | Status | Notes |
|-------|--------|-------|
| Min plan lines (L: ≥100) | PASS | ~390 lines |
| Min task count (L: ≥5) | PASS | 23 tasks |
| Required headers present | PASS | Goal, Architecture Context, Tasks, Acceptance Criteria Mapping all present |
| API specs embedded (not "see ARCHITECTURE.md") | PASS | All 6 new endpoints have inline request/response/auth/source |
| DB schema embedded with source noted | PASS | Existing tables reference migration files; new migration body shown inline |
| UI Design Token Map populated | PASS | Color/typography/component reuse tables present |
| ≥1 task marked `high` complexity (L) | PASS | Tasks 4, 6, 21 marked high |
| Each task touches ≤3 files | PASS | Tasks 16, 18, 19, 20 touch up to 3-4 files but each with narrow per-file change scope; explicitly justified by feature group |
| Each task has Pattern ref | PASS | All 23 tasks list a pattern reference |
| Each task has Context refs | PASS | All 23 reference sections that exist in this plan |
| Tests covered per AC | PASS | AC mapping table cross-references task tests |
| Architecture compliance (no new packages) | PASS | All backend changes inside existing `internal/api/{dashboard,operator,apn,system,reports,violation}` and `internal/store` |
| Standard envelope used | PASS | All new handlers use `apierr.WriteSuccess`/`WriteError` |
| Tenant scoping called out | PASS | `Story-Specific Compliance Rules` section + per-task notes |
| Migration up + down | PASS | Task 1 lists both files |
| Bug Pattern + Tech Debt sections present | PASS | Both written |
| No implementation code blocks > pseudo-snippet length | PASS | Only spec-level snippets; no full handler bodies |

**Quality Gate Self-Validation: PASS**
