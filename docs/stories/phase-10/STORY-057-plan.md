# STORY-057 Implementation Plan: Data Accuracy & Missing Endpoints

> Story: STORY-057 — Data Accuracy & Missing Endpoints
> Effort: L | Complexity: Medium-High
> Services: SVC-03 (Core API), SVC-07 (Analytics)
> Screens: SCR-001 (Dashboard), SCR-021b (SIM Sessions), SCR-021c (SIM Usage), SCR-032 (APN Detail), SCR-001 (Login)

---

## Architecture Context

### API Envelope (Standard)
```json
{
  "status": "success",
  "data": { ... },
  "meta": { "cursor": "abc123", "limit": 50, "has_more": true }
}
```
ListMeta struct: `internal/apierr/apierr.go:77` — has `Total int64`, `Cursor string`, `HasMore bool`, `Limit int`.

### Current Dashboard Handler
- File: `internal/api/dashboard/handler.go`
- 4 concurrent goroutines: SIM count by state, sessions+top APNs, operator health, recent anomalies
- Missing: monthly cost goroutine, sparklines data, deltas
- Top APNs already resolves names via `apnStore.GetByID()` (lines 148-166) — but relies on `GetActiveStats().ByAPN` keys being valid UUIDs
- Operator Health uses `ListGrantsWithOperators` which joins operator_grants + operators

### Sessions Table (TBL-17)
```sql
CREATE TABLE sessions (
    id UUID, sim_id UUID, tenant_id UUID, operator_id UUID, apn_id UUID,
    nas_ip INET, framed_ip INET, calling_station_id VARCHAR(50),
    called_station_id VARCHAR(100), rat_type VARCHAR(10),
    session_state VARCHAR(20) DEFAULT 'active',  -- 'active' | 'closed'
    auth_method VARCHAR(20), policy_version_id UUID,
    acct_session_id VARCHAR(100), started_at TIMESTAMPTZ, ended_at TIMESTAMPTZ,
    terminate_cause VARCHAR(50), bytes_in BIGINT, bytes_out BIGINT,
    packets_in BIGINT, packets_out BIGINT, last_interim_at TIMESTAMPTZ
);
```

### CDRs Table (TBL-18) — TimescaleDB hypertable
```sql
CREATE TABLE cdrs (
    id BIGSERIAL, session_id UUID, sim_id UUID, tenant_id UUID,
    operator_id UUID, apn_id UUID, rat_type VARCHAR(10),
    record_type VARCHAR(20), bytes_in BIGINT, bytes_out BIGINT,
    duration_sec INTEGER, usage_cost DECIMAL(12,4), carrier_cost DECIMAL(12,4),
    rate_per_mb DECIMAL(8,4), rat_multiplier DECIMAL(4,2), timestamp TIMESTAMPTZ
);
```

### CDR Continuous Aggregates
- `cdrs_hourly`: time_bucket('1 hour'), by tenant/operator/apn/rat_type → record_count, bytes_in, bytes_out, usage_cost, carrier_cost
- `cdrs_daily`: time_bucket('1 day'), by tenant/operator → active_sims, total_bytes, total_cost, total_carrier_cost
- `cdrs_monthly`: time_bucket('1 month'), by tenant/operator/apn/rat_type → record_count, unique_sims, total_bytes, total_usage_cost, total_carrier_cost
- All have `timescaledb.materialized_only = false` (real-time aggregation enabled)

### SIMs Table (TBL-10) — Key columns for PATCH
```sql
CREATE TABLE sims (
    id UUID, tenant_id UUID, operator_id UUID, apn_id UUID,
    iccid VARCHAR(22), imsi VARCHAR(15), msisdn VARCHAR(20),
    ip_address_id UUID, policy_version_id UUID, esim_profile_id UUID,
    sim_type VARCHAR(10), state VARCHAR(20), rat_type VARCHAR(10),
    metadata JSONB DEFAULT '{}',
    ...
);
```
**Decision (STORY-057):** `label` and `notes` are NOT separate columns. They will be stored in the existing `metadata` JSONB field as `metadata.label` and `metadata.notes`. `custom_attributes` also in metadata. `segment_id` is NOT a SIM field (segments are saved filters, not FK) — excluded from PATCH scope.

### Auth Config (Current)
```go
JWTSecret        string        `envconfig:"JWT_SECRET"`
JWTExpiry        time.Duration `envconfig:"JWT_EXPIRY" default:"15m"`
JWTRefreshExpiry time.Duration `envconfig:"JWT_REFRESH_EXPIRY" default:"168h"` // 7d
```

### Router Registration Pattern
Routes registered in `internal/gateway/router.go` via `RouterDeps` struct with nil-guard blocks.

### Frontend Patterns
- Hooks: `web/src/hooks/use-dashboard.ts`, `web/src/hooks/use-sims.ts`
- Dashboard: `web/src/pages/dashboard/index.tsx`
- SIM Detail: `web/src/pages/sims/detail.tsx` (has `SessionsTab`, `UsageTab` with Math.random())
- API client: `web/src/lib/api.ts` (authApi.login sends `remember_me`)

---

## Bug Patterns from decisions.md

| Pattern | Source | Relevance |
|---------|--------|-----------|
| Missing HasMore in ListMeta | DEV-031, DEV-033, DEV-036 | All new list endpoints MUST set `HasMore: nextCursor != ""` |
| Tenant scoping in session queries | DEV-048 | All new session queries MUST scope by tenant_id |
| SELECT FOR UPDATE on state transitions | DEV-061 | SIM PATCH with state guard should use row-level lock |
| EXISTS instead of COUNT for checks | DEV-062 | Use EXISTS for "has active SIMs" in state guard |
| Parameterized queries only | DEV-030 | Never use fmt.Sprintf for SQL values |
| updated_at explicit in SIM store | DEV-027 | SIM UPDATE must include `updated_at = NOW()` |
| Interface-in-consumer pattern | DEV-070 | New dependencies wired in main.go via interfaces |

---

## Tasks

### Task 1: Dashboard — Monthly Cost & Sparklines Backend

**What:** Add a 5th goroutine to `GetDashboard` for monthly cost from `cdrs_monthly` aggregate. Add sparklines field with 7-point daily series from `cdrs_daily`. Add deltas computed from real values (today vs yesterday).

**Files:**
- `internal/api/dashboard/handler.go` (modify)
- `internal/store/cdr.go` (add `GetMonthlyCostForTenant`, `GetDailySparklines` methods)

**Depends on:** None
**Complexity:** Medium
**Context refs:** AC-3, AC-4, dashboard handler lines 96-251, cdrs_monthly aggregate, cdrs_daily aggregate

**Spec — dashboardDTO additions:**
```
MonthlyCost    float64                   → from SUM(total_usage_cost) WHERE bucket >= date_trunc('month', NOW())
Sparklines     map[string][]float64      → keys: total_sims, active_sessions, auth_per_sec, monthly_cost
Deltas         map[string]float64        → keys: total_sims_delta, active_sessions_delta, monthly_cost_delta
```

**Store methods to add in `internal/store/cdr.go`:**
- `GetMonthlyCostForTenant(ctx, tenantID) (float64, error)` — `SELECT COALESCE(SUM(total_usage_cost),0) FROM cdrs_monthly WHERE tenant_id=$1 AND bucket >= date_trunc('month', NOW())`
- `GetDailyKPISparklines(ctx, tenantID, days int) (map[string][]float64, error)` — Queries cdrs_daily for last N days, groups by bucket, returns per-metric arrays. Also queries SIM count snapshots from recent sim_state_history.

**Handler changes:**
- Add 5th goroutine for cost + sparklines
- Replace `MonthlyCost: 0` with real value
- Add `Sparklines` and `Deltas` to DTO and response

**Verify:** `go test ./internal/api/dashboard/... ./internal/store/...` passes. Dashboard response includes non-zero monthly_cost when CDRs exist, sparklines array with 7 real data points.

---

### Task 2: Dashboard — Operator Health Fix

**What:** Ensure Operator Health populates when operators exist even if operator_grants are missing or have edge cases. Verify `ListGrantsWithOperators` returns data correctly.

**Files:**
- `internal/api/dashboard/handler.go` (modify)
- `internal/store/operator.go` (verify/fix `ListGrantsWithOperators`)

**Depends on:** None
**Complexity:** Low
**Context refs:** AC-2, dashboard handler lines 177-197, operator_grants table schema

**Spec:**
- Verify `ListGrantsWithOperators` uses `LEFT JOIN operators ON operator_grants.operator_id = operators.id` (not INNER JOIN that drops results when operator is missing)
- If operator_grants table is empty for a tenant, result should be empty array (not error). If operators exist but no grants, consider listing operators directly for super_admin
- Ensure `resp.OperatorHealth` defaults to `[]` (already done line 234)

**Verify:** Integration test: seed 3 operators + grants → dashboard returns 3 operator health entries.

---

### Task 3: Frontend — Remove Math.random() from Dashboard & SIM Usage

**What:** Remove all `Math.random()` fallback data from dashboard hooks and SIM detail UsageTab. Wire real sparklines from backend. Wire UsageTab to real `/sims/:id/usage` endpoint.

**Files:**
- `web/src/hooks/use-dashboard.ts` (modify — remove lines 49-68 Math.random fallbacks)
- `web/src/pages/sims/detail.tsx` (modify — replace `mockUsageData` with real hook data)
- `web/src/pages/dashboard/index.tsx` (modify — use real sparklines from API, remove fallback line 942)

**Depends on:** Task 1 (backend sparklines), Task 7 (API-052 usage endpoint)
**Complexity:** Medium
**Context refs:** AC-4, AC-7, use-dashboard.ts lines 48-68, detail.tsx lines 319-330

**Spec:**
- `use-dashboard.ts`: Remove the sparklines/heatmap `Math.random()` fallback. If backend returns empty sparklines, show empty/flat chart (not random noise). Keep staleTime/refetchInterval.
- `detail.tsx` `UsageTab`: Replace `mockUsageData` with data from `useSIMUsage(simId)` hook. Pass `period` param based on timeframe selector. Render real bytes_in/bytes_out from response. Empty state when no CDRs.
- `dashboard/index.tsx` line 942: Remove `fallbackSparkline` with Math.random(). Use empty array or flat line as fallback.

**Verify:** `npm run build` (no TS errors). Dashboard sparklines show distinct trends per KPI (not random). SIM Usage tab shows real CDR chart data.

---

### Task 4: API-051 — GET /api/v1/sims/:id/sessions

**What:** Implement endpoint returning session history (active + ended) for a specific SIM, cursor-paginated, tenant-scoped.

**Files:**
- `internal/store/session_radius.go` (add `ListBySIM` method)
- `internal/api/sim/handler.go` (add `GetSessions` handler method)
- `internal/gateway/router.go` (register route)

**Depends on:** None
**Complexity:** Medium
**Context refs:** AC-6, API-051 spec, sessions table schema, RadiusSessionStore patterns

**API Spec:**
```
GET /api/v1/sims/:id/sessions
Auth: JWT (sim_manager+)
Query: ?cursor=X&limit=50&state=active|closed
Response: {
  "status": "success",
  "data": [
    {
      "id": "uuid",
      "sim_id": "uuid",
      "operator_id": "uuid",
      "apn_id": "uuid",
      "nas_ip": "1.2.3.4",
      "framed_ip": "10.0.0.1",
      "rat_type": "lte",
      "session_state": "active|closed",
      "acct_session_id": "...",
      "started_at": "RFC3339",
      "ended_at": "RFC3339|null",
      "bytes_in": 12345,
      "bytes_out": 67890,
      "duration_sec": 3600,
      "protocol_type": "radius"
    }
  ],
  "meta": { "cursor": "...", "has_more": true, "limit": 50 }
}
```

**Store method:** `ListBySIM(ctx, tenantID, simID uuid.UUID, params ListBySIMSessionParams) ([]RadiusSession, string, error)` — queries `sessions WHERE sim_id=$1 AND tenant_id=$2`, ordered by `started_at DESC`, supports `state` filter (optional, default all), cursor-based pagination.

**Handler:** Parse `:id` param, validate UUID + tenant context. Call store method. Map to response DTO. Return with `WriteList`.

**Router:** Add `r.Get("/api/v1/sims/{id}/sessions", deps.SIMHandler.GetSessions)` in the sim_manager group.

**SIM Handler needs RadiusSessionStore:** Add `sessionStore *store.RadiusSessionStore` to `sim.Handler` struct and wire in main.go.

**Verify:** `go test ./internal/api/sim/... ./internal/store/...` passes. Integration: `GET /api/v1/sims/abc/sessions?limit=20` returns session list scoped by SIM + tenant.

---

### Task 5: API-052 — GET /api/v1/sims/:id/usage

**What:** Implement endpoint returning per-period usage series + top sessions for a specific SIM, using CDR aggregates.

**Files:**
- `internal/store/cdr.go` (add `GetSIMUsage` method)
- `internal/api/sim/handler.go` (add `GetUsage` handler method)
- `internal/gateway/router.go` (register route)

**Depends on:** None
**Complexity:** High
**Context refs:** AC-7, API-052 spec, cdrs_hourly/cdrs_daily aggregates, CDR table schema

**API Spec:**
```
GET /api/v1/sims/:id/usage
Auth: JWT (analyst+)
Query: ?period=24h|7d|30d (default 30d)
Response: {
  "status": "success",
  "data": {
    "sim_id": "uuid",
    "period": "30d",
    "total_bytes_in": 123456789,
    "total_bytes_out": 987654321,
    "total_cost": 12.50,
    "series": [
      { "bucket": "2026-04-01T00:00:00Z", "bytes_in": 1234, "bytes_out": 5678, "cost": 0.50 }
    ],
    "top_sessions": [
      { "session_id": "uuid", "started_at": "RFC3339", "bytes_total": 50000, "duration_sec": 7200 }
    ]
  }
}
```

**Store method:** `GetSIMUsage(ctx, tenantID, simID uuid.UUID, period string) (*SIMUsageResult, error)`
- period=24h → query `cdrs` raw table for last 24h, bucket by 1h
- period=7d → query `cdrs_daily` for last 7d
- period=30d → query `cdrs_daily` for last 30d
- Top sessions: `SELECT session_id, MIN(timestamp) as started_at, SUM(bytes_in+bytes_out) as bytes_total, MAX(duration_sec) as duration_sec FROM cdrs WHERE sim_id=$1 AND tenant_id=$2 AND timestamp >= $3 GROUP BY session_id ORDER BY bytes_total DESC LIMIT 5`

**Router:** Add `r.Get("/api/v1/sims/{id}/usage", deps.SIMHandler.GetUsage)` in an analyst+ role group.

**Verify:** `go test ./internal/api/sim/... ./internal/store/...` passes. Integration: `GET /api/v1/sims/abc/usage?period=24h` returns hourly usage series.

---

### Task 6: API-035 — GET /api/v1/apns/:id/sims

**What:** Implement endpoint returning SIM list for a specific APN, cursor-paginated with filters.

**Files:**
- `internal/store/sim.go` (add `ListByAPN` method or use existing `List` with apn_id filter)
- `internal/api/apn/handler.go` (add `ListSIMs` handler method, add simStore dependency)
- `internal/gateway/router.go` (register route)

**Depends on:** None
**Complexity:** Medium
**Context refs:** AC-8, API-035 spec, SIM store List method pattern, APN handler

**API Spec:**
```
GET /api/v1/apns/:id/sims
Auth: JWT (sim_manager+)
Query: ?cursor=X&limit=50&state=active|suspended&q=search_term
Response: {
  "status": "success",
  "data": [
    {
      "id": "uuid", "iccid": "...", "imsi": "...", "msisdn": "...",
      "state": "active", "rat_type": "lte", "operator_name": "Turkcell",
      "created_at": "RFC3339"
    }
  ],
  "meta": { "cursor": "...", "has_more": true, "limit": 50 }
}
```

**Store approach:** Reuse existing `SIMStore.List` with `apn_id` filter parameter (already supported in List query). If not supported, add `APNID *uuid.UUID` to `ListSIMParams`.

**Handler:** Parse `:id`, validate UUID + tenant context. Verify APN exists via `apnStore.GetByID`. Call SIM store list with apn_id filter. Map to response DTO. Return with `WriteList`.

**APN Handler needs SIMStore:** Add `simStore *store.SIMStore` to `apn.Handler` struct and wire in main.go.

**Router:** Add `r.Get("/api/v1/apns/{id}/sims", deps.APNHandler.ListSIMs)` in the sim_manager group.

**Verify:** `go test ./internal/api/apn/... ./internal/store/...` passes. Integration: `GET /api/v1/apns/xyz/sims?state=active` returns filtered SIM list.

---

### Task 7: API-043 — PATCH /api/v1/sims/:id

**What:** Implement partial update for SIM editable fields: label, notes, custom_attributes (stored in metadata JSONB). Field-level validation + audit log + state machine guard.

**Files:**
- `internal/store/sim.go` (add `PatchMetadata` method)
- `internal/api/sim/handler.go` (add `Patch` handler method)
- `internal/gateway/router.go` (register route)

**Depends on:** None
**Complexity:** High
**Context refs:** AC-9, API-043 spec, SIM store patterns, audit patterns, SIM state machine (BR-1)

**API Spec:**
```
PATCH /api/v1/sims/:id
Auth: JWT (sim_manager+)
Body: {
  "label": "Fleet Vehicle #42",
  "notes": "Replaced SIM card 2026-04-01",
  "custom_attributes": { "fleet_id": "F-042", "zone": "north" }
}
Response: {
  "status": "success",
  "data": { /* full SIM response */ }
}
```

**Design decisions:**
- `label`, `notes`, `custom_attributes` stored in `metadata` JSONB column (no migration needed)
- Store method uses `jsonb_set` or `||` operator to merge partial updates into existing metadata
- State guard: PATCH blocked when SIM state is `terminated` or `purged` (returns 422)
- Audit log entry created with before/after metadata diff

**Store method:** `PatchMetadata(ctx, tenantID, simID uuid.UUID, patch map[string]interface{}) (*SIM, error)` — uses `UPDATE sims SET metadata = metadata || $3, updated_at = NOW() WHERE id = $1 AND tenant_id = $2 AND state NOT IN ('terminated', 'purged') RETURNING *`

**Handler:** Parse `:id`, decode body, validate fields (label max 255 chars, notes max 2000 chars, custom_attributes max 50 keys). Get existing SIM for audit before state. Call store. Create audit entry. Return updated SIM.

**Router:** Add `r.Patch("/api/v1/sims/{id}", deps.SIMHandler.Patch)` in the sim_manager group.

**Verify:** `go test ./internal/api/sim/...` passes. Integration: `PATCH /api/v1/sims/abc` with `{label: "new"}` returns updated SIM + audit entry created. PATCH on terminated SIM returns 422.

---

### Task 8: Remember Me — Backend Consume

**What:** Add `remember_me` field to login request. When true, extend JWT access TTL and refresh token TTL. Add config env var.

**Files:**
- `internal/api/auth/handler.go` (modify `loginRequest`, pass to service)
- `internal/auth/auth.go` (modify `Login` signature, pass rememberMe to `createFullSession`)
- `internal/config/config.go` (add `JWTRememberMeExpiry` env var)
- `cmd/argus/main.go` (wire new config value)

**Depends on:** None
**Complexity:** Low
**Context refs:** AC-10, STORY-042-review.md Observation 1, auth handler, auth service, config

**Spec:**
- Add `RememberMe bool` to `loginRequest` struct in handler
- Add `rememberMe bool` parameter to `Service.Login`
- When rememberMe=true: JWT expiry = `cfg.JWTRememberMeExpiry` (default 7d via `AUTH_JWT_REMEMBER_ME_TTL`), refresh TTL also extended
- When rememberMe=false: JWT expiry = `cfg.JWTExpiry` (default 15m), refresh TTL = `cfg.JWTRefreshExpiry`
- Config: `JWTRememberMeExpiry time.Duration envconfig:"AUTH_JWT_REMEMBER_ME_TTL" default:"168h"`
- Pass through `createFullSession` which calls `GenerateToken` with appropriate duration

**Verify:** `go test ./internal/auth/... ./internal/api/auth/...` passes. Login with `remember_me=true` returns JWT with 7d exp; without uses default 15m.

---

### Task 9: meta.total Strategy — Document & Standardize

**What:** Document the cursor pagination convention. Ensure `meta.total` is consistently 0 across all list endpoints (approximate count only when explicitly needed). Verify all handlers set `HasMore`.

**Files:**
- `internal/apierr/apierr.go` (add `Total` field doc comment, consider `omitempty`)
- Audit all list handlers to confirm consistent `HasMore` usage

**Depends on:** None
**Complexity:** Low
**Context refs:** AC-5, apierr.ListMeta struct, DEV-031/033/036

**Spec:**
- `ListMeta.Total` field: Change to `json:"total,omitempty"` so 0 values are omitted from response
- Document convention: cursor-based pagination uses `has_more` + `cursor`. `total` only populated for specific count endpoints (segment count). List endpoints do NOT compute COUNT(*) — too expensive at 10M+ scale.
- Verify all ~15 list handlers pass `HasMore: nextCursor != ""` (most already do per DEV-031/033/036 fixes)
- No changes needed to frontend — it already uses `meta.has_more` from `ListResponse`

**Verify:** `go test ./internal/apierr/...` passes. `grep` confirms all WriteList calls include HasMore.

---

### Task 10: Frontend — Wire SIM Sessions Tab to New Endpoint

**What:** Update `useSIMSessions` hook to call `/sims/:id/sessions` instead of `/sessions?sim_id=X`. Update hook for API-052 usage endpoint.

**Files:**
- `web/src/hooks/use-sims.ts` (modify `useSIMSessions` URL, update `useSIMUsage` response type)
- `web/src/types/sim.ts` (add `SIMUsageData` type if needed)

**Depends on:** Task 4 (API-051), Task 5 (API-052)
**Complexity:** Low
**Context refs:** AC-6, AC-7, use-sims.ts lines 80-110

**Spec:**
- `useSIMSessions`: Change URL from `/sessions?sim_id=${simId}` to `/sims/${simId}/sessions`
- Remove `params.set('sim_id', simId)` — sim_id is now in the path
- Add optional `state` filter param
- `useSIMUsage`: Add `period` param, update return type to match API-052 response shape
- Add `SIMUsageData` type: `{ sim_id, period, total_bytes_in, total_bytes_out, total_cost, series: Array<{bucket, bytes_in, bytes_out, cost}>, top_sessions: Array<{session_id, started_at, bytes_total, duration_sec}> }`

**Verify:** `npm run build` (no TS errors). SIM detail Sessions tab loads from new endpoint. Usage tab renders real data.

---

## Acceptance Criteria Mapping

| AC | Task(s) | Verification |
|----|---------|-------------|
| AC-1 (Top APNs name not UUID) | Task 2 (verify existing resolution) | Dashboard shows "iot-m2m.argus" not UUIDs |
| AC-2 (Operator Health populates) | Task 2 | Dashboard shows 3 operators with health |
| AC-3 (Monthly Cost > 0) | Task 1 | Dashboard cost shows real CDR sum |
| AC-4 (Real sparklines) | Task 1, Task 3 | No Math.random(), real 7-point trends |
| AC-5 (meta.total strategy) | Task 9 | Total omitted when 0, has_more consistent |
| AC-6 (API-051 sessions) | Task 4, Task 10 | GET /sims/:id/sessions returns data |
| AC-7 (API-052 usage) | Task 5, Task 3, Task 10 | GET /sims/:id/usage returns real series |
| AC-8 (API-035 apn sims) | Task 6 | GET /apns/:id/sims returns SIM list |
| AC-9 (API-043 patch sim) | Task 7 | PATCH /sims/:id updates metadata |
| AC-10 (remember_me) | Task 8 | JWT 7d exp with remember_me=true |

## Test Scenario Mapping

| Test | Task | Type |
|------|------|------|
| Dashboard Top APNs shows names | Task 2 | E2E |
| Dashboard Operator Health shows 3 operators | Task 2 | E2E |
| Dashboard Monthly Cost > 0 | Task 1 | E2E |
| Dashboard sparklines distinct trends | Task 1, Task 3 | E2E |
| GET /sims/abc/sessions?limit=20 | Task 4 | Integration |
| GET /sims/abc/usage?period=24h | Task 5 | Integration |
| GET /apns/xyz/sims?state=active | Task 6 | Integration |
| PATCH /sims/abc {label: "new"} | Task 7 | Integration |
| Login remember_me=true → 7d JWT | Task 8 | Integration |
| Cursor pagination has_more accurate | Task 9 | Integration |

## Execution Waves

### Wave 1 (Independent backend — no dependencies)
- Task 1: Dashboard monthly cost + sparklines backend
- Task 2: Dashboard operator health fix
- Task 4: API-051 SIM sessions
- Task 5: API-052 SIM usage
- Task 6: API-035 APN SIMs
- Task 7: API-043 PATCH SIM
- Task 8: Remember me backend
- Task 9: meta.total strategy

### Wave 2 (Frontend — depends on Wave 1 backends)
- Task 3: Frontend remove Math.random()
- Task 10: Frontend wire new endpoints

## Risks

| Risk | Mitigation |
|------|-----------|
| cdrs_monthly may have no data if continuous aggregate hasn't refreshed | Use real-time aggregation (materialized_only=false) + fallback to raw cdrs query |
| SIM handler growing too large (already ~400 lines) | New methods are small (30-50 lines each). Consider splitting to sim/sessions_handler.go if needed. |
| RadiusSessionStore on SIM handler creates import coupling | Wire via functional option `WithSessionStore(*store.RadiusSessionStore)` like existing `WithPolicyStore` |
| remember_me 7d access token is unusually long | Follows story spec exactly. Document as intentional in decisions.md. |
