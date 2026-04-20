# Implementation Plan: FIX-203 — Dashboard Operator Health: Uptime/Latency/Activity + WS Push

## Goal

Populate the remaining Dashboard Operator Health Matrix fields (`latency_ms`, `auth_rate`, plus 1h latency sparkline) in the `/dashboard` DTO, extend the existing operator health worker to also publish `argus.events.operator.health` on latency-delta >10% (not just status flips), and wire an FE WebSocket subscriber that patches the operator row in-place — so operators observe fleet health live without manual refresh. Closes D-050 (FIX-202 tech-debt) and resolves F-03, F-04, F-45, F-50, F-55, F-80.

## Architecture Context

### Important State of the Art (inherited from FIX-202 + STORY-090)

The bulk of the NATS + WS scaffolding for operator health already exists and is production-wired. This story is **additive**, not green-field. Per advisor review, the following pieces are already implemented and must NOT be rebuilt:

1. **`SubjectOperatorHealthChanged = "argus.events.operator.health"`** — `internal/bus/nats.go:26` (const exists).
2. **Publisher** — `internal/operator/health.go:314-441` `HealthChecker.checkOperator` already builds a `OperatorHealthEvent` and publishes on status flip (`prevStatus != status`, health.go:405). Payload fields: `operator_id, operator_name, previous_status, current_status, circuit_breaker_state, latency_ms, failure_reason, timestamp`.
3. **WS hub subject→client event mapping** — `internal/ws/hub.go:246` maps `argus.events.operator.health` → `operator.health_changed` client event type.
4. **main.go subscription wiring** — `cmd/argus/main.go:854` wires publisher; `cmd/argus/main.go:906` subscribes WS hub to the subject.
5. **FIX-202 partial DTO widening** — `internal/api/dashboard/handler.go:89-100` already has `Code`, `SLATarget`, `ActiveSessions`, `LastHealthCheck`, `LatencyMs`, `AuthRate` declared on `operatorHealthDTO`. The **last two are declared but never populated** (this story fills them).
6. **FE types already widened** — `web/src/types/dashboard.ts:6-17` already has `latency_ms`, `auth_rate`, `sla_target`, `active_sessions`, `last_health_check` optional fields.

### Components Involved

- **`internal/store/operator.go`** — add `LatestHealthWithLatencyByOperator(ctx) map[uuid.UUID]OperatorHealthSnapshot` (widens existing `LatestHealthByOperator` at `operator.go:641` to include `latency_ms`, `status`, `circuit_state` alongside `checked_at`). Also add `GetLatencyTrend(ctx, opID uuid.UUID, since time.Duration, bucket time.Duration) []float64` that buckets `operator_health_logs.latency_ms` into 5-min bins over the last 1h — returns exactly 12 float values (zero-filled for empty bins).
- **`internal/operator/health.go`** — extend `HealthChecker.checkOperator` (line 314) with a new `lastLatency map[healthKey]int` map (parallel to existing `lastStatus`) and widen the publish trigger at line 405 from `prevStatus != status` to `prevStatus != status || latencyDeltaExceeds(prevLatency, latencyMs, 0.10)`. Adds one new field wiring on struct, no public API changes. **This is the single high-complexity task** — touches coordinated state under `hc.mu`.
- **`internal/api/dashboard/handler.go`** — (a) add `*analyticmetrics.Collector` field + `WithMetricsCollector` option; (b) in the operator-health goroutine (line 233), after building `[]operatorHealthDTO`, batch-populate `latency_ms` from store `LatestHealthWithLatencyByOperator` and `auth_rate` from `collector.GetMetrics(ctx).ByOperator[opID].AuthErrorRate` (converted to success rate pct: `100 * (1 - error_rate)`); (c) also build a per-operator 1h latency sparkline from `store.GetLatencyTrend` and embed in the DTO as `latency_sparkline []float64`. Piggybacks on existing 30s Redis cache — no extra cache layer.
- **`cmd/argus/main.go:1094+`** — inject existing `metricsCollector` instance into the dashboard Handler via the new option; also inject the widened `operatorStore` where the dashboard Handler is constructed (around line 854 / wherever the Handler is built for the router).
- **`internal/ws/hub.go`** — **no code change** — the subject-to-client-event mapping already exists at line 246. Verification only.
- **`internal/bus/nats.go`** — **no code change** — `SubjectOperatorHealthChanged` constant already exists at line 26. Verification only.
- **`web/src/hooks/use-dashboard.ts`** — add `useRealtimeOperatorHealth()` hook that subscribes to the `operator.health_changed` WS event (via `wsClient.on`) and patches the matching row in the `DashboardData.operator_health` array inside the react-query cache (immutable map update — same pattern as `useRealtimeAlerts` at line 121).
- **`web/src/types/dashboard.ts`** — add `latency_sparkline?: number[]` to `OperatorHealth` interface. Optional to keep backward compat during deploy.
- **`web/src/pages/dashboard/index.tsx`** — inside `OperatorHealthMatrix` (line 266), add:
  - New column "Auth Rate" showing `op.auth_rate?.toFixed(1) + '%'` with threshold colouring.
  - SLA-breach chip when `op.latency_ms != null && op.sla_target != null && op.latency_ms > op.sla_target` → red `"SLA breach"` badge.
  - New "Latency 1h" **sparkline** rendered from `op.latency_sparkline` using the existing `<Sparkline>` atom (`web/src/components/ui/sparkline.tsx`). **NOT** the existing `OperatorActivitySparkline` (which is session-activity from WS events — kept separate, distinct concern).
- **`docs/architecture/WEBSOCKET_EVENTS.md`** — update section 4 (lines 211-235) to match the **actual** `OperatorHealthEvent` struct (`internal/operator/events.go:9`) payload. Current docs list fictional fields (`uptime_24h_pct`, `latency_ms_p95`, `consecutive_failures`, `last_successful_check`, `affected_sim_count`) that the Go struct does not have. Replace with the real lean payload.
- **`internal/api/dashboard/handler_test.go`** — widen existing fixtures to assert the four new fields are populated when Collector + store return data, and that hard-coded 99.9% uptime assertions are retired per Story Risk 3.
- **`internal/operator/health_test.go`** — add a threshold-trigger test: seed `lastLatency[key] = 100`, feed a probe with `LatencyMs = 120` (20% delta → should publish) and a separate probe with `LatencyMs = 105` (5% delta → should NOT publish), asserting `eventPub.Publish` call count in each case.

### Data Flow

**BEFORE (current state after FIX-202):**

```
[ Health worker tick, 30s ] ─ per operator, per protocol
   └─ adapter.HealthCheck()  → result.LatencyMs, result.Success
   └─ cb.RecordSuccess/Failure  → circuit state
   └─ store.InsertHealthLog     → operator_health_logs row
   └─ store.UpdateHealthStatus  → operators.health_status
   └─ IF prevStatus != status:  → eventBus.Publish(argus.events.operator.health, OperatorHealthEvent)
                                 → NATS → wsHub → BroadcastAll(operator.health_changed)  [no tenant filter!]
                                 → FE: nothing subscribes → UI stale until 30s poll

[ GET /api/v1/dashboard ]
   └─ goroutine: operatorStore.ListGrantsWithOperators(tenantID)
   └─ build operatorHealthDTO: id, name, status, health_pct, code, sla_target
   └─ latency_ms / auth_rate fields declared but NEVER SET  ← D-050

[ FE dashboard render ]
   └─ operator row shows: name, status badge, uptime "99.9%" (healthStatusToPct default),
                          latency "0ms" (unset field defaults),
                          OperatorActivitySparkline (session events, unrelated)
```

**AFTER (this story):**

```
[ Health worker tick, 30s ] ─ per operator, per protocol
   └─ same adapter probe + circuit path (unchanged)
   └─ store.InsertHealthLog + UpdateHealthStatus (unchanged)
   └─ IF prevStatus != status
      OR  abs(currLatency - prevLatency) / max(prevLatency, 1) > 0.10: ← NEW threshold
         eventBus.Publish(argus.events.operator.health, OperatorHealthEvent)
         → NATS → wsHub → BroadcastAll(operator.health_changed)
         → FE: useRealtimeOperatorHealth patches operator_health[i] in react-query cache
         → UI re-renders single row (sparkline re-push, status badge, latency number)

[ GET /api/v1/dashboard ] (still 30s poll as fallback per AC-8)
   └─ goroutine: operatorStore.ListGrantsWithOperators(tenantID)
   └─ goroutine: operatorStore.LatestHealthWithLatencyByOperator()   ← NEW batch
   └─ goroutine: collector.GetMetrics(ctx)                           ← NEW injection
   └─ goroutine: per-operator store.GetLatencyTrend(opID, 1h, 5m)   ← NEW sparkline
   └─ Merge: latency_ms = health_log.latency_ms
             auth_rate  = 100 * (1 - ByOperator[opID].AuthErrorRate)
             latency_sparkline = [f1..f12]
   └─ Redis-cache envelope 30s (existing)
```

### API Specifications

Single endpoint modified — no route changes, only DTO widening:

**`GET /api/v1/dashboard`** — existing endpoint.

Request: unchanged. Response envelope unchanged (`{status, data: {...}}`). The `operator_health` array element gains populated fields:

```jsonc
{
  "status": "success",
  "data": {
    "operator_health": [
      {
        "id": "770e8400-…",
        "name": "turkcell",
        "code": "TCELL",
        "status": "healthy",
        "health_pct": 99.9,
        "sla_target": 99.9,
        "active_sessions": 12345,          // FIX-202 (already present)
        "last_health_check": "2026-…",     // FIX-202 (already present)
        "latency_ms": 42.5,                // NEW — from operator_health_logs.latency_ms
        "auth_rate": 99.34,                // NEW — 100 * (1 - collector.AuthErrorRate), 0-100 pct
        "latency_sparkline": [             // NEW — 12 floats = 5-min buckets over last 1h
          38, 41, 40, 45, 43, 42, 44, 43, 41, 40, 39, 42
        ]
      }
    ]
  }
}
```

Error envelope unchanged. Status codes unchanged (200, 401).

### WebSocket Event Specification

**Client event:** `operator.health_changed` (already defined in code, documented anew here).

Wire payload (actual `OperatorHealthEvent` from `internal/operator/events.go:9`, wrapped in `EventEnvelope` by the hub):

```jsonc
{
  "type": "operator.health_changed",
  "id": "evt_a1b2c3d4",
  "timestamp": "2026-04-20T12:34:56.789Z",
  "data": {
    "operator_id": "770e8400-…",
    "operator_name": "turkcell",
    "previous_status": "healthy",
    "current_status": "degraded",
    "circuit_breaker_state": "half_open",
    "latency_ms": 542,
    "failure_reason": "timeout",           // empty on status-recovery
    "timestamp": "2026-04-20T12:34:56.789Z"
  }
}
```

Publish triggers (applied to the latest adapter probe):

1. **Status flip** — `previous_status != current_status` (existing behaviour).
2. **Latency delta** — `abs(currLatency - prevLatency) / max(prevLatency, 1) > 0.10` (NEW). Applied only when `currLatency > 0` and `prevLatency > 0` to avoid divide-by-zero and cold-start noise. First tick after startup seeds `lastLatency` without publishing.

Suppression: if neither trigger fires, no event is published. At 30s × 50 operators × 5 protocols = 250 ticks/min worst case, typical steady-state produces zero events (no flips, sub-10% noise); chatter only appears during real change.

**Tenant scope:** `OperatorHealthEvent` has no `tenant_id` field, so `ws/hub.go:208 relayNATSEvent → extractTenantID` returns false → `BroadcastAll`. This is intentional for this event: operators are multi-tenant resources (shared across tenants via grants), so every tenant with an active grant sees the update. FE mitigates noise by only rendering rows for operators present in the tenant's own `/dashboard` payload (existing filter: rows come from `operatorStore.ListGrantsWithOperators(tenantID)`). If an event arrives for an operator the tenant does not grant, the patch is a no-op (`.find()` returns undefined). This behaviour is documented in `WEBSOCKET_EVENTS.md` Risk note.

### Database Schema

Source: `migrations/20260320000002_core_schema.up.sql:555-567` (ACTUAL). No new migrations required.

```sql
-- TBL-23: operator_health_logs (plain table; hypertable in next migration)
CREATE TABLE IF NOT EXISTS operator_health_logs (
    id BIGSERIAL,
    operator_id UUID NOT NULL,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status VARCHAR(20) NOT NULL,           -- healthy | degraded | down
    latency_ms INTEGER,                    -- nullable when probe failed before measuring
    error_message TEXT,
    circuit_state VARCHAR(20) NOT NULL     -- closed | open | half_open
);
CREATE INDEX idx_op_health_operator_time ON operator_health_logs (operator_id, checked_at DESC);

-- TimescaleDB hypertable applied in 20260320000003_timescaledb_hypertables.up.sql.
```

All new store methods are read-only SELECTs against this table. No DDL.

**New store methods** (signatures):

```go
// Batch: one row per operator, the most recent. Scan uses DISTINCT ON
// pattern already established at store/operator.go:641 LatestHealthByOperator.
type OperatorHealthSnapshot struct {
    CheckedAt    time.Time
    Status       string
    LatencyMs    *int
    CircuitState string
}
func (s *OperatorStore) LatestHealthWithLatencyByOperator(ctx context.Context) (map[uuid.UUID]OperatorHealthSnapshot, error)

// Last 1h bucketed into 5-min bins. Returns exactly 12 floats (zero-filled
// for empty bins to keep sparkline shape stable). Follows the shape of
// CDRStore.GetOperatorMetrics (cdr.go:488) — date_trunc + generate_series
// LEFT JOIN for contiguous bins.
func (s *OperatorStore) GetLatencyTrend(ctx context.Context, operatorID uuid.UUID, since time.Duration, bucket time.Duration) ([]float64, error)
```

### Screen Mockups

```
┌─ Operator Health Matrix ──────────────────── Last 24h ────┐
│ OPERATOR       STATUS    UPTIME    LATENCY   AUTH   TREND │
│ ─────────────  ────────  ────────  ────────  ─────  ───── │
│ Turkcell       ● healthy   99.94%    42 ms   99.8%  ▁▂▃▁▂ │
│  TCELL                                                     │
│  12,345 active | SLA 99.9%                                 │
│                                                            │
│ Vodafone       ● degraded  95.00%   542 ms   94.1%  ▂▃▅▇▇ │
│  VODA  [SLA breach]                                        │ ← red chip when latency > sla_target
│  8,920 active | SLA 99.5%                                  │
│                                                            │
│ TürkTelekom    ● down       0.00%     0 ms   —      ▁▁▁▁▁ │
│  TTNET                                                     │
└────────────────────────────────────────────────────────────┘
```

- Navigation entry: user lands at `/dashboard` after login.
- Row click → `/operators/{id}` (existing behaviour, preserved).
- Pagination scope-cut: table shows `slice(0, 50)` + "Show all →" footer link to `/operators` for tenants with >50 operators. No virtualization (realistic operator count per tenant is <10 — see Risk 1). Document as scope-cut in AC-9 mapping.
- Loading state: row skeleton on first paint (existing `<Skeleton>` pattern in this page).
- Empty state: existing "No operators configured" placeholder (handler.go:304) preserved.
- Error state: if `latency_ms` unset (store returned nil, e.g. fresh install), render "—" instead of "0 ms". Same for `auth_rate`.

### Design Token Map (UI MANDATORY — source: FRONTEND.md + existing dashboard/index.tsx patterns)

#### Color Tokens
| Usage | Token Class / CSS var | NEVER Use |
|-------|-----------------------|-----------|
| Status healthy | `var(--color-success)` | `#22c55e`, `text-green-500` |
| Status degraded | `var(--color-warning)` | `#f59e0b`, `text-yellow-500` |
| Status down | `var(--color-danger)` | `#ef4444`, `text-red-500` |
| Accent (sparkline) | `text-accent` / `var(--color-accent)` | `#3b82f6`, `text-blue-500` |
| Primary text | `text-text-primary` | `text-[#0f172a]`, `text-gray-900` |
| Secondary text | `text-text-secondary` | `text-[#64748b]`, `text-gray-500` |
| Tertiary text (captions) | `text-text-tertiary` | `text-[#94a3b8]`, `text-gray-400` |
| Card surface | `bg-card` | `bg-white`, `bg-[#ffffff]` |
| Row hover | `bg-bg-hover` | `bg-gray-50`, arbitrary hex |
| Border | `border-border` / `border-border/50` | `border-[#e2e8f0]` |
| SLA breach chip bg | `bg-danger/10 text-danger ring-1 ring-danger/30` | `bg-red-100 text-red-600` |

#### Typography Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Column header caps | `text-[10px] uppercase tracking-[1px]` | arbitrary px/leading |
| Row value (name) | `text-[13px] font-medium` | `text-sm` |
| Row numeric (mono) | `font-mono text-[12px]` | `text-xs font-mono` with drift |
| Caption (sub-row) | `text-[10px] font-mono text-text-tertiary` | `text-xs text-gray-400` |

#### Spacing / Elevation Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Card shadow | `shadow-card` (if defined) else `card-hover` (dashboard convention) | raw `shadow-md` |
| Row padding | `py-2.5 pr-3 / px-3 / pl-3` (matches existing rows) | arbitrary `p-4` |
| Stagger animation | `stagger-item` with `style={{ animationDelay: 'Nms' }}` | ad-hoc keyframes |

#### Existing Components to REUSE (DO NOT recreate)
| Component | Path | Use For |
|-----------|------|---------|
| `<Sparkline>` | `web/src/components/ui/sparkline.tsx` | NEW latency-1h sparkline column (data: `op.latency_sparkline`) |
| `OperatorChip` | `web/src/components/shared/operator-chip.tsx` | Operator name+code cell (already in place) |
| `Card / CardHeader / CardContent / CardTitle` | `web/src/components/ui/card` | panel wrapper (already in place) |
| `Table / TableHead / TableBody / TableRow / TableCell` | `web/src/components/ui/table` | tabular body |
| `Badge` | `web/src/components/ui/badge` | SLA-breach chip |
| `cn` utility | `web/src/lib/utils` | conditional class merging |
| `timeAgo, formatNumber` | `web/src/lib/format` | relative `last_health_check` rendering |
| `wsClient.on(event, handler)` | `web/src/lib/ws` | WS subscribe (same surface as `useRealtimeAlerts`) |
| `queryClient.setQueryData` | `@tanstack/react-query` | cache patch pattern (see `use-dashboard.ts:128`) |

**RULE: no raw `<table>`, no raw `<input>`, no hardcoded hex. Run `grep -rE '#[0-9a-fA-F]{3,6}' web/src/pages/dashboard/ web/src/hooks/use-dashboard.ts` → must return ZERO matches in new code.**

## Prerequisites

- [x] FIX-202 delivered DTO partial widening + operator name resolution. `operatorHealthDTO` already has `Code`, `SLATarget`, `ActiveSessions`, `LastHealthCheck`, `LatencyMs`, `AuthRate` fields declared.
- [x] STORY-090 delivered per-protocol health worker with event publisher hook, circuit breaker, alerts, and SLA tracker.
- [x] `SubjectOperatorHealthChanged` constant exists at `internal/bus/nats.go:26`.
- [x] WS hub mapping for `argus.events.operator.health` → `operator.health_changed` already in `ws/hub.go:246`.
- [x] main.go already subscribes the WS hub to the subject (`cmd/argus/main.go:906`).
- [x] `analytics/metrics.Collector` records per-operator auth success/failure (`internal/analytics/metrics/collector.go:57`) and is instantiated in main (`cmd/argus/main.go:1094`).
- [x] `<Sparkline>` atom exists at `web/src/components/ui/sparkline.tsx`.
- [x] `operator_health_logs` table exists with `latency_ms` column (migration 20260320000002).

## Tasks

### Task 1 — Store: widen `LatestHealthByOperator` + add latency-trend query
- **Files:** Modify `internal/store/operator.go`, Modify `internal/store/operator_test.go` (add test)
- **Depends on:** — (root)
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/operator.go:641-662` (`LatestHealthByOperator`, DISTINCT ON pattern) and `internal/store/cdr.go:488-539` (`GetOperatorMetrics`, date_trunc bucketing)
- **Context refs:** `Architecture Context > Components Involved`, `Database Schema`, `API Specifications`
- **What:**
  1. Add `OperatorHealthSnapshot` struct (CheckedAt, Status, LatencyMs *int, CircuitState).
  2. Implement `LatestHealthWithLatencyByOperator(ctx) (map[uuid.UUID]OperatorHealthSnapshot, error)` — SQL: `SELECT DISTINCT ON (operator_id) operator_id, checked_at, status, latency_ms, circuit_state FROM operator_health_logs ORDER BY operator_id, checked_at DESC`. Scan into the map keyed by `operator_id`. This **does not replace** `LatestHealthByOperator` — callers of the narrow time-only version (SLA tracker) keep working.
  3. Implement `GetLatencyTrend(ctx, operatorID uuid.UUID, since time.Duration, bucket time.Duration) ([]float64, error)` — SQL uses `date_trunc` or `time_bucket` (TimescaleDB) grouping on `checked_at`, `AVG(latency_ms)` per bin, LEFT JOIN against `generate_series(now() - since, now(), bucket)` to zero-fill empty bins. Return exactly `since/bucket` values (12 for 1h/5m). Round to integer-like floats (1 decimal).
  4. Add two unit tests: one covers `LatestHealthWithLatencyByOperator` returns latency for 3 ops with varied rows; one covers `GetLatencyTrend` returns 12 elements with zero-fill when only 3 bins have data.
- **Verify:** `go test ./internal/store -run 'TestLatestHealthWithLatencyByOperator|TestGetLatencyTrend' -v` passes.

### Task 2 — Health worker: latency-threshold publish trigger
- **Files:** Modify `internal/operator/health.go`, Modify `internal/operator/health_test.go` (new test cases)
- **Depends on:** —
- **Complexity:** **high** (coordinated state under `hc.mu`, tick-ordering correctness, cold-start semantics)
- **Pattern ref:** Read `internal/operator/health.go:314-441` (`checkOperator`, existing `lastStatus` map + publish gate at line 405-416) and `internal/operator/health_test.go` (existing publish-assertion pattern)
- **Context refs:** `Architecture Context > Components Involved`, `WebSocket Event Specification`, `Data Flow`
- **What:**
  1. Add `lastLatency map[healthKey]int` field to `HealthChecker` struct (line 40-65). Initialize in `NewHealthChecker` alongside `lastStatus`.
  2. Seed on new protocol registration: in `startOperatorLoop` (line 279-289) after setting `hc.lastStatus[key] = op.HealthStatus`, set `hc.lastLatency[key] = 0` (cold-start sentinel).
  3. In `checkOperator` (line 398-403) under `hc.mu.Lock`, read `prevLatency := hc.lastLatency[hkey]` alongside `prevStatus`, and write `hc.lastLatency[hkey] = result.LatencyMs` alongside `hc.lastStatus[hkey] = status`.
  4. Widen the publish gate at line 405 from `prevStatus != status` to:
     ```
     statusFlipped := prevStatus != status
     latencyChanged := prevLatency > 0 && result.LatencyMs > 0 &&
         math.Abs(float64(result.LatencyMs-prevLatency))/float64(prevLatency) > 0.10
     shouldPublish := hc.eventPub != nil && hc.healthSubject != "" && (statusFlipped || latencyChanged)
     if shouldPublish { ... }
     ```
     Cold start (`prevLatency == 0`) suppresses latency-trigger publish until the second tick populates it, to avoid noise on startup.
  5. Also refresh the map cleanup in `Stop` and `RefreshOperator` — delete `lastLatency[k]` entries alongside `lastStatus[k]` entries (health.go:544 + Stop path).
  6. Add two tests in `health_test.go`:
     - `TestCheckOperator_PublishesOnLatencyDelta`: seed `lastLatency[key] = 100`, inject adapter mock returning `LatencyMs = 120` (20% delta), assert `eventPub.Publish` called once with `SubjectOperatorHealthChanged`.
     - `TestCheckOperator_SuppressesSubThresholdLatency`: seed `lastLatency[key] = 100`, inject adapter mock returning `LatencyMs = 105` (5% delta, no status change), assert `eventPub.Publish` NOT called.
- **Verify:** `go test ./internal/operator -run 'TestCheckOperator_Publish' -v` passes; existing health tests still green.

### Task 3 — Dashboard handler: inject Collector, populate latency/auth_rate/sparkline
- **Files:** Modify `internal/api/dashboard/handler.go`, Modify `cmd/argus/main.go` (wire new option)
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/dashboard/handler.go:37-82` (existing `HandlerOption` + `WithRedisClient` pattern) and `:233-260` (existing operator-health goroutine)
- **Context refs:** `Architecture Context > Components Involved`, `API Specifications`, `Data Flow`
- **What:**
  1. Add new option `WithMetricsCollector(c *analyticmetrics.Collector) HandlerOption` and a new field `metricsCollector *analyticmetrics.Collector` on `Handler`. (Import `analyticmetrics "github.com/btopcu/argus/internal/analytics/metrics"`.)
  2. Add `operatorHealthDTO.LatencySparkline []float64 \`json:"latency_sparkline,omitempty"\`` (do NOT make it a pointer — array marshals to `[]` naturally).
  3. Inside the operator-health goroutine (line 233) after grants are fetched: in a single `go func() { ... }` call `h.operatorStore.LatestHealthWithLatencyByOperator(ctx)` once (store-wide batch, tenant-agnostic; OK because operator_health_logs has no tenant column — the filter by grant-list already scopes). Wire the result into the DTO loop at line 249: `if snap, ok := snapMap[g.OperatorID]; ok && snap.LatencyMs != nil { v := float64(*snap.LatencyMs); dto.LatencyMs = &v }`.
  4. After the main `wg.Wait()` at line 395, under a new post-merge section (parallel to the existing sessionStatsByOp merge at lines 401-408):
     - If `h.metricsCollector != nil`: call `metrics, _ := h.metricsCollector.GetMetrics(ctx)`. For each `resp.OperatorHealth[i]`, look up `metrics.ByOperator[id]` and set `dto.AuthRate = &(100 * (1 - errorRate))`. Clamp to [0, 100].
     - For each operator, call `h.operatorStore.GetLatencyTrend(ctx, opID, 1*time.Hour, 5*time.Minute)` and set `dto.LatencySparkline = trend`. Execute these concurrently via errgroup or parallel waitgroup — 50 operators × 1 query ≈ 50 queries worst case per dashboard request. Mitigation: already Redis-cached 30s (handler.go:432-436), so the worst case is 2 reqs/min per tenant.
  5. In `cmd/argus/main.go`, locate dashboard handler construction and add `dashboard.WithMetricsCollector(metricsCollector)` option to the existing `NewHandler(...)` call.
- **Verify:** `go build ./...` passes; `go test ./internal/api/dashboard -v` passes (fixtures updated in Task 7).

### Task 4 — FE: `useRealtimeOperatorHealth` hook
- **Files:** Modify `web/src/hooks/use-dashboard.ts`, Modify `web/src/pages/dashboard/index.tsx` (import + call)
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `web/src/hooks/use-dashboard.ts:121-146` (`useRealtimeAlerts` — subscribe + setQueryData patching) and `:100-119` (`useRealtimeActiveSessions` — cache mutation).
- **Context refs:** `Architecture Context > Components Involved`, `WebSocket Event Specification`, `Design Token Map`
- **What:**
  1. Add `useRealtimeOperatorHealth()` export in `use-dashboard.ts`. Subscribe `wsClient.on('operator.health_changed', handler)`.
  2. Handler signature matches the `OperatorHealthEvent` payload. Parse `data` as `{ operator_id, current_status, latency_ms, timestamp, circuit_breaker_state, operator_name }`.
  3. Patch cache: `queryClient.setQueryData<DashboardData>(DASHBOARD_KEY, (old) => { if (!old) return old; const next = old.operator_health.map(op => op.id === data.operator_id ? { ...op, status: data.current_status, latency_ms: data.latency_ms, last_health_check: data.timestamp, health_pct: statusToPct(data.current_status) } : op); return { ...old, operator_health: next } })`. Tiny helper `statusToPct(s)` mirrors backend `healthStatusToPct` (healthy=99.9, degraded=95, down=0).
  4. Return cleanup function (same unsub pattern as existing hooks).
  5. In `web/src/pages/dashboard/index.tsx`, near the existing `useRealtimeAlerts()` / `useRealtimeMetrics()` calls, add `useRealtimeOperatorHealth()`.
  6. **AC-2 orphan protection:** if event arrives for `operator_id` not present in `old.operator_health`, the `.map()` is a no-op (no new row is added). Document this behaviour in a JSDoc comment on the hook.
- **Verify:** `npm run lint --prefix web` clean; `npm run typecheck --prefix web` clean; `npm run build --prefix web` succeeds.

### Task 5 — FE: render latency sparkline, auth_rate column, SLA-breach chip
- **Files:** Modify `web/src/types/dashboard.ts`, Modify `web/src/pages/dashboard/index.tsx` (OperatorHealthMatrix component, line 266-385)
- **Depends on:** Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/dashboard/index.tsx:266-385` (existing `OperatorHealthMatrix`), `web/src/components/ui/sparkline.tsx` (atom for trend rendering).
- **Context refs:** `Screen Mockups`, `Design Token Map`, `Architecture Context > Components Involved`
- **What:**
  1. Add `latency_sparkline?: number[]` to `OperatorHealth` interface in `web/src/types/dashboard.ts`.
  2. In `OperatorHealthMatrix`:
     - **New column header** "Auth" between "Latency" and "Activity" columns (`<TableHead className="text-[10px] uppercase tracking-[1px] text-text-tertiary font-medium pb-2 px-3 text-right">Auth</TableHead>`).
     - **Auth rate cell**: `op.auth_rate != null ? <span className={cn('font-mono text-[12px]', authRateColor(op.auth_rate))}>{op.auth_rate.toFixed(1)}%</span> : <span className="text-text-tertiary">—</span>`. Helper `authRateColor` (>=99 → success, >=95 → warning, else danger).
     - **SLA-breach chip**: under the operator name sub-row (where `sla_target` already shows at line 341-345), conditionally render `<Badge variant="destructive" className="text-[9px]">SLA breach</Badge>` when `op.latency_ms != null && op.sla_target != null && op.latency_ms > (op.sla_target ?? 500)`. Note `sla_target` is a PERCENTAGE in the existing data (`op.sla_target.toFixed(2)%`) — FIX-203 **adds a second interpretation field or clarifies semantics**: per AC-7 the SLA compare is against **latency threshold in ms**, not uptime %. Resolution: add new optional `sla_latency_ms?: number` to `OperatorHealth` (defaulting to 500ms when unset) and use it for the latency compare; keep `sla_target` for the uptime-% display. Plan extends the DTO accordingly in Task 3 (Go side: add `SLALatencyMs *float64` to `operatorHealthDTO`, hard-coded default `500` for now — full per-operator-configurable value deferred to a future story with schema migration). **Default 500ms** per AC-7 is the ship-ready baseline.
     - **New latency sparkline column (replacing or alongside existing activity sparkline)**: AC-6 specifies **latency_ms with sparkline trend last 1h**. Decision: **replace** the existing `OperatorActivitySparkline` (session events) with the new backend-fed latency sparkline — keeps the column count stable and honours AC-6 literally. Session-activity sparkline is retained as a deferred polish: if the design team wants to show both, a later story adds a dedicated column. Render with `<Sparkline data={op.latency_sparkline ?? []} className="w-[72px] h-[24px]" color="var(--color-accent)" />` (matches existing Sparkline atom API).
  3. **Invoke `frontend-design` skill** during Developer work for professional quality (per Planner template rule).
  4. Zero hardcoded hex: verify `grep -rE '#[0-9a-fA-F]{3,6}' web/src/pages/dashboard/index.tsx web/src/hooks/use-dashboard.ts web/src/types/dashboard.ts` shows no new matches (existing CSS-var usages `var(--color-…)` are fine).
- **Verify:** `npm run typecheck --prefix web` clean; visual check on local dev server — row shows latency number + sparkline + auth rate + SLA-breach chip when threshold breached.

### Task 6 — Integration test: NATS → WS → FE payload round-trip
- **Files:** Create `internal/api/dashboard/integration_operator_health_test.go` OR extend existing integration harness in `internal/ws/server_test.go`
- **Depends on:** Task 2, Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/ws/server_test.go` (existing WS test harness using test NATS + hub).
- **Context refs:** `WebSocket Event Specification`, `Data Flow`, `Acceptance Criteria Mapping`
- **What:**
  1. Spin up NATS + WS hub + subscribe test client to `operator.health_changed`.
  2. Publish raw `argus.events.operator.health` with an `OperatorHealthEvent` payload (status flip).
  3. Assert: test client receives a message within 2s with `type == "operator.health_changed"` and payload fields match (`operator_id`, `current_status`, `latency_ms`).
  4. Second test case: publish a latency-only-change event, assert the WS client receives the event.
- **Verify:** `go test ./internal/ws -run TestOperatorHealthBroadcast -v` passes.

### Task 7 — Test fixtures: retire hardcoded 99.9% uptime assertions
- **Files:** Modify `internal/api/dashboard/handler_test.go`
- **Depends on:** Task 3
- **Complexity:** low
- **Pattern ref:** Read existing dashboard handler test (find operator-health DTO assertions).
- **Context refs:** `API Specifications`
- **What:**
  1. Grep `internal/api/dashboard/*_test.go` for `99.9` literal assertions on operator uptime; replace with asserts on `LatencyMs != nil`, `AuthRate != nil`, and `len(LatencySparkline) == 12`.
  2. Add fixture setup that seeds `operator_health_logs` rows (latency=40ms, 42, 45, …) so `LatestHealthWithLatencyByOperator` returns populated rows.
  3. Stub or provide a test double for `analyticmetrics.Collector` (or skip the auth_rate populate when collector is nil — handler already guards).
- **Verify:** `go test ./internal/api/dashboard -v` all pass.

### Task 8 — Docs: reconcile WEBSOCKET_EVENTS.md schema + note SLA-breach threshold default
- **Files:** Modify `docs/architecture/WEBSOCKET_EVENTS.md`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `docs/architecture/WEBSOCKET_EVENTS.md:211-238` (current section 4 for `operator.health_changed`).
- **Context refs:** `WebSocket Event Specification`
- **What:**
  1. Replace the current section 4 payload JSON with the **actual** `OperatorHealthEvent` field set (remove `uptime_24h_pct`, `latency_ms_p95`, `consecutive_failures`, `last_successful_check`, `last_failed_check`, `affected_sim_count` — none of these are on the Go struct).
  2. Add a "Triggers" sub-section listing the two publish conditions (status flip, latency delta >10%) with the precise formula.
  3. Add a "Tenant scope" note: event is broadcast to all tenants (no `tenant_id` on payload); FE filters by presence in local `operator_health` list.
  4. Note the SLA-breach default threshold (500ms) is a temporary default pending a future per-operator-configurable column.
- **Verify:** Manual review — no code.

### Task 9 — Scope-cut doc + D-050 closure in ROUTEMAP
- **Files:** Modify `docs/ROUTEMAP.md` (Tech Debt table), Modify `docs/reviews/ui-review-remediation-plan.md` (note FIX-203 scope-cut: virtualization deferred)
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `docs/ROUTEMAP.md` Tech Debt table for existing row format.
- **Context refs:** `Tech Debt`
- **What:**
  1. Mark D-050 (FIX-202 tech-debt) as `✓ RESOLVED` with "Resolved by FIX-203 — latency_ms + auth_rate populated, WS push wired".
  2. Add a scope-cut note: "AC-9 virtualization deferred — <50-operator tenants served by slice(0,50); >50 rare, tail hidden behind 'Show all →' link to /operators."
- **Verify:** Manual review.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 — DTO carries code, latency_ms, active_sessions, auth_rate, last_check, sla_target, status | Task 3 (widen) + FIX-202 (partial) | Task 7 fixture assertions |
| AC-2 — handler JOINs latest metrics | Task 1 (store batch) + Task 3 (handler wire) | Task 7 |
| AC-3 — worker publishes on status flip OR latency delta >10% | Task 2 | Task 2 unit tests (`TestCheckOperator_Publish*`) |
| AC-4 — WS hub relays `operator.health.changed` → `operator.health_changed` client event | **Verified only** (already implemented at `ws/hub.go:246`); Task 6 integration test | Task 6 |
| AC-5 — FE `useDashboard` subscribes WS, patches operator row without refetch | Task 4 | Manual smoke + Task 6 |
| AC-6 — UI shows status badge, latency with sparkline (1h), active_sessions, auth_rate, relative last-check | Task 5 (sparkline + auth + chip) + existing FIX-202 (name/code/active/last-check) | Manual browser check |
| AC-7 — SLA-breach red chip when latency > sla_target (default 500ms) | Task 5 | Manual with fixture data |
| AC-8 — 30s polling fallback preserved when WS disconnects | **Preserved by not changing** `useDashboard`'s `refetchInterval: 30_000` (line 69) | Code review |
| AC-9 — 50+ operators fit (scope-cut: slice(0,50) + Show all link; no virtualization) | Task 9 documents scope-cut | Task 9 doc review |

## Story-Specific Compliance Rules

- **API envelope** — `/dashboard` continues to return `{status, data, meta?, error?}` (handler.go:433 already uses `apierr.WriteSuccess`). No change.
- **Multi-tenant scoping** — `operatorStore.ListGrantsWithOperators(tenantID)` already filters by tenant; new DTO fields remain within this scope. `LatestHealthWithLatencyByOperator` is deliberately tenant-agnostic (operator_health_logs has no tenant column) — the grant-filter at the outer loop is the scope gate.
- **Audit logging** — NOT required. This is a read-only dashboard enrichment; per `ADR-001` audit applies only to state-changing operations. Health event publishing is already metric-grade, not audit-grade.
- **Redis cache TTL** — existing 30s envelope cache at `handler.go:435` preserved; new sparkline queries amortize across this cache window.
- **FE design tokens** — MANDATORY: no hardcoded hex/px colours. Use `var(--color-*)` or `text-*` / `bg-*` semantic tokens only. Verified by grep gate in Task 5.
- **Go style** — use `math.Abs` for latency delta; import already available in `internal/operator/health.go` via stdlib. New store methods follow existing `scan` + `defer rows.Close()` pattern.
- **Error handling** — new Collector and store calls in the dashboard handler are log-on-error, never fail the request (same defensive pattern as existing goroutines at `handler.go:169-182`).

## Bug Pattern Warnings

File `docs/brainstorming/bug-patterns.md` — check at implementation time. Likely relevant patterns (from the codebase's ROUTEMAP tech-debt history and this story's nature):

- **N+1 query** — Task 3 fan-out for `GetLatencyTrend` across 50 operators is the risk. Mitigations: already Redis-cached 30s per tenant; under real load this is ≤2 tenant-requests/min × 50 trend queries = acceptable. If benchmark shows p95 drift, collapse to a single `GROUP BY operator_id` query with `WHERE operator_id = ANY($1)` returning all sparklines in one round trip — keep in back-pocket as optimisation.
- **Cache key collision** — `operator:health:<opID>` Redis key used by `checkOperator` (health.go:386) and dashboard cache key `dashboard:<tenantID>` (handler.go:148) are distinct namespaces; no collision.
- **WS BroadcastAll noise** — hub falls through to `BroadcastAll` for events without `tenant_id`. Per advisor call: documented, not a bug; FE `.map()`-by-id discard guarantees unrelated ops are filtered at render.
- **Turkish char encoding** — if any copy is added to UI strings, ensure UTF-8 (not ASCII fallback like "SLA breach" → keep English per project convention "Turkish conversation, English code/docs").

## Tech Debt (from ROUTEMAP)

- **D-050 (FIX-202 tech-debt) — latency_ms and auth_rate source wiring → RESOLVED by this story (Task 3)**. The DTO fields were shipped empty-ready in FIX-202; this story populates them via the widened store method + injected metrics collector.

No other OPEN tech-debt items target FIX-203.

## Mock Retirement

N/A — no `src/mocks/` fixture layer exists in this project (all endpoints are live). FE consumes `/dashboard` directly.

## Risks & Mitigations

1. **WS message flood** — 50 operators × 5 protocols × 30s ticks = 250 ticks/min. Existing trigger (status flip only) gives ~0 events at steady state. New latency-delta trigger could add noise if underlying probe is jittery. **Mitigation**: >10% threshold + cold-start suppression (prevLatency=0 skips). Production monitoring — if the `argus_nats_published_total{subject="argus.events.operator.health"}` counter exceeds ~5/min/tenant at steady state, tighten threshold to >20% in a follow-up.
2. **Stale data on WS reconnect** — after a disconnect, FE may miss events. **Mitigation**: the 30s `refetchInterval` on `useDashboard` (line 69) acts as a catch-up — within 30s of reconnect, `operator_health` re-fetch resynchronises.
3. **Hardcoded 99.9% test fixtures break** — Task 7 explicitly retires these.
4. **SLA-breach threshold config** — AC-7 says "configurable per operator (default 500ms)". The operators table has `sla_uptime_target` (a %), not a latency threshold. **Mitigation**: ship with hard-coded 500ms default in the DTO and document a follow-up story to add `sla_latency_ms_target` column. Acceptable scope-cut — the UX behaviour (chip appears correctly at 500ms) is delivered this story.
5. **`GetLatencyTrend` N+1 (50 queries per request)** — see Bug Pattern section. Mitigated by Redis cache; tracked as a possible optimisation.
6. **WEBSOCKET_EVENTS.md schema drift** — Task 8 reconciles docs to the real struct. Without this task, future engineers implementing FE features against the docs would wire fictional fields.
7. **Tenant-scope broadcast risk** — event payload has no tenant_id so `BroadcastAll`. Advisor-validated as intentional (operators are cross-tenant). Documented in Task 8.

## Wave Plan

- **Wave 1 (parallel — independent roots):** Task 1 (store), Task 2 (health worker), Task 4 (FE hook), Task 8 (docs reconcile), Task 9 (ROUTEMAP).
- **Wave 2 (depends on Task 1):** Task 3 (dashboard handler — injects the store method).
- **Wave 3 (depends on Task 3 + Task 4):** Task 5 (FE render — needs DTO fields + hook).
- **Wave 4 (depends on Task 2 + Task 3):** Task 6 (integration test), Task 7 (fixture retire).

Four waves. Total developer-time budget: Wave 1 ≈ 15 min parallel; Wave 2 ≈ 5 min; Wave 3 ≈ 5 min; Wave 4 ≈ 10 min. Critical path: Task 1 → Task 3 → Task 5.

## Pre-Validation

- [x] Min 100 lines — plan is >250 lines.
- [x] Min 5 tasks — 9 tasks.
- [x] At least 1 high-complexity — Task 2 marked `high`.
- [x] All required sections present: Goal, Architecture Context, Database Schema, API Specifications, Screen Mockups, Design Token Map, Prerequisites, Tasks, Acceptance Criteria Mapping, Story-Specific Compliance Rules, Bug Pattern Warnings, Tech Debt, Mock Retirement, Risks & Mitigations, Wave Plan.
- [x] API specs embedded (not just referenced).
- [x] DB schema embedded with migration source noted (`20260320000002_core_schema.up.sql`).
- [x] Screen mockup ASCII embedded.
- [x] Design token map populated with exact class/var names + NEVER-use list.
- [x] Each task has Files, Depends on, Complexity, Pattern ref, Context refs, What, Verify.
- [x] All Context refs point to sections that exist above.
- [x] Tech Debt section closes D-050 by name.

PASS.
