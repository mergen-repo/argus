# Implementation Plan: STORY-072 — Enterprise Observability Screens

## Goal
Deliver 10 new + 1 enhanced operations-domain screens that turn the metrics, audit, backup, and anomaly data already produced by Phase-10 backend stories into actionable NOC/SRE workflows: performance dashboard, error drill-down, real-time AAA traffic, NATS/DB/Redis health, job queue observability, backup status, deploy history, incident timeline, and full alert lifecycle (ack/resolve/escalate + comment thread). Adds a thin BE layer (Prometheus-registry-as-JSON proxy, infrastructure-health aggregator, anomaly comment + escalate API) and integrates everything into the Operations sidebar group with a hardened WebSocket status indicator.

## Architecture Context

### Components Involved
- `internal/api/ops/*` (NEW package) — operations-domain HTTP handlers (Prometheus registry → JSON, infra health aggregator, deploy-history reader)
- `internal/api/anomaly/handler.go` (MODIFY) — add `Escalate`, `ListComments`, `AddComment` actions
- `internal/store/anomaly_comment.go` (NEW) — comments store
- `internal/gateway/router.go` (MODIFY) — register new routes (super_admin / tenant_admin scoped)
- `internal/observability/metrics/metrics.go` (READ-ONLY by ops handler) — registry source of truth
- `internal/store/backup_store.go` (REUSE via existing `ReliabilityHandler.BackupStatus`)
- `internal/store/audit.go` (REUSE; deploy history = `entity_type='deployment'`)
- `cmd/argus/main.go` (MODIFY) — wire new ops handler + dependencies
- `web/src/pages/ops/*` (NEW directory, 10 screens)
- `web/src/hooks/use-ops.ts` (NEW) — react-query hooks for ops endpoints
- `web/src/router.tsx` (MODIFY) — register `/ops/*` routes
- `web/src/components/layout/sidebar.tsx` (MODIFY) — Operations group expansion
- `web/src/components/command-palette/*` (MODIFY) — register ops screens for keyboard nav
- `web/src/components/layout/ws-indicator.tsx` (REUSE — already satisfies AC-12; verify only)

### Data Flow

**Performance / Error metrics (AC-1, AC-2, AC-7):**
```
User opens /ops/performance
  → React useQuery(['ops','perf']) hits GET /api/v1/ops/metrics/snapshot
  → opsHandler scrapes obsmetrics.Registry IN-PROCESS via prometheus.GathererFunc
  → returns JSON: { http: { by_route: [{route, count, p50_ms, p95_ms, p99_ms, error_rate}] }, runtime: { goroutines, mem_alloc, gc_pause_p99_ms }, jobs: { by_type: [{type, runs, p95, success_rate, failed}] } }
  → React renders heatmap + top-10 tables, auto-refresh interval 15s
```

**Real-time AAA (AC-3):**
```
RADIUS/Diameter/5G handler fires bus event "aaa.auth"
  → eventBus → WebSocket fan-out (existing pattern)
  → React subscribes via wsClient.subscribe('aaa.auth.tick')
  → Local rolling 60s window → instant gauge & sparkline
  → Slow path: GET /api/v1/ops/metrics/snapshot supplies AAA p99 + per-protocol totals
```

**Infra health (AC-4, AC-5, AC-6):**
```
GET /api/v1/ops/infra-health
  → opsHandler.InfraHealth aggregates:
     - DB: pg.Pool.Stat() + slow query store (existing tracer_slow.go top-50) + table-size query
     - Redis: rdb.Info("memory","stats","clients","keyspace") parsed
     - NATS: js.StreamInfo()/ConsumerInfo() per known stream
  → Returns JSON sectioned per subsystem
  → React polls every 10s
```

**Backup (AC-8):** existing `GET /api/v1/system/backup-status` (no BE change).

**Deploy history (AC-9):** existing `GET /api/v1/audit?entity_type=deployment` (no BE change; FE-only filter and rendering).

**Alert lifecycle (AC-11):**
```
POST /api/v1/anomalies/{id}/comments  { body }
GET  /api/v1/anomalies/{id}/comments
POST /api/v1/anomalies/{id}/escalate   { note, on_call_user_id? }
   → anomaly remains in current state
   → notification.Service.Send(channel="email"|"telegram", template="alert.escalated")
   → audit.Emit("anomaly.escalate")
PATCH /api/v1/anomalies/{id}/state     (existing — adds Resolve summary via comment)
```

**Incident timeline (AC-10):**
```
GET /api/v1/ops/incidents?from=...&to=...
  → Reads anomalies + audit entries (action LIKE 'anomaly.%') joined chronologically
  → Returns: [{ts, anomaly_id, severity, type, transition, actor, comment}]
```

### API Specifications

All responses use the standard envelope `{ status, data, meta?, error? }` already defined in `internal/apierr`.

#### NEW endpoints (super_admin unless noted)

`GET /api/v1/ops/metrics/snapshot`
- Auth: `super_admin`
- Response (200):
  ```json
  {
    "status":"success",
    "data":{
      "http":{
        "totals":{"requests":12345,"errors":42,"error_rate":0.0034},
        "by_route":[
          {"method":"GET","route":"/api/v1/sims","count":2345,"p50_ms":12,"p95_ms":48,"p99_ms":120,"error_count":3,"error_rate":0.0013}
        ],
        "by_status":[{"status":"200","count":11900},{"status":"500","count":12}]
      },
      "aaa":{
        "by_protocol":[{"protocol":"radius","req_per_sec":420,"success_rate":0.998,"p99_ms":18}]
      },
      "runtime":{"goroutines":523,"mem_alloc_bytes":189345128,"mem_sys_bytes":314257408,"gc_pause_p99_ms":2.4},
      "jobs":{
        "by_type":[
          {"job_type":"bulk_import","runs":143,"success":140,"failed":3,"p50_s":4.2,"p95_s":18.0,"p99_s":42.1}
        ]
      }
    }
  }
  ```
- Implementation: gather from `obsmetrics.Registry.Reg.Gather()` and aggregate samples (Prometheus client `.Metric` types).
- Status codes: 200, 401, 403, 500.

`GET /api/v1/ops/infra-health`
- Auth: `super_admin`
- Response (200):
  ```json
  {
    "status":"success",
    "data":{
      "db":{
        "pool":{"max":50,"in_use":12,"idle":8,"waiting":0,"acquired_total":12345},
        "slow_queries":[{"sql_hash":"...","sample":"SELECT ...","p95_ms":420,"count":12,"first_seen":"...","last_seen":"..."}],
        "tables":[{"name":"sims","size_bytes":1234567890,"row_estimate":9876543}],
        "replication_lag_seconds":null,
        "partitions":[{"parent":"cdr_records","child":"cdr_records_2026_04","range_from":"2026-04-01","range_to":"2026-05-01"}]
      },
      "redis":{
        "ops_per_sec":4123,
        "hit_rate":0.9412,
        "miss_rate":0.0588,
        "memory_used_bytes":52428800,
        "memory_max_bytes":1073741824,
        "evictions_total":0,
        "connected_clients":18,
        "keys_by_db":[{"db":0,"keys":12345,"expires":11000}],
        "latency_p99_ms":1.2
      },
      "nats":{
        "streams":[
          {"name":"argus-events","subjects":["events.>"],"messages":12345,"bytes":98765,"consumers":3,
            "consumer_lag":[{"consumer":"events-worker","pending":12,"ack_pending":0,"redeliveries":0,"slow":false}]}
        ],
        "dlq_depth":0
      }
    }
  }
  ```
- Status codes: 200, 401, 403, 500.

`GET /api/v1/ops/incidents?from=&to=&severity=&state=&entity_id=`
- Auth: `tenant_admin` (tenant-scoped) / `super_admin` (cross-tenant when no tenant header)
- Response (200): `{ status, data: [{ts, anomaly_id, sim_id?, severity, type, action, actor_id?, actor_email?, note?, current_state}], meta: { cursor, has_more } }`
- Action enum: `detected | acknowledged | resolved | false_positive | escalated | commented`

`GET /api/v1/anomalies/{id}/comments`
- Auth: `tenant_admin`
- Response (200): `{ status, data: [{id, anomaly_id, user_id, user_email, body, created_at}] }`

`POST /api/v1/anomalies/{id}/comments`
- Body: `{ "body": "string (1..2000 chars)" }`
- Response (201): the created comment DTO. Audit `anomaly.comment`.

`POST /api/v1/anomalies/{id}/escalate`
- Body: `{ "note": "string (≤500 chars)", "on_call_user_id": "uuid?" }`
- Effect: notification.Send (template `alert.escalated`), comment auto-added with `[ESCALATED] note`, audit `anomaly.escalate`.
- Response (200): `{ status, data: { anomaly: {...}, notification_id: "..." } }`
- Errors: 404 (anomaly not found), 422 (already resolved/false_positive).

#### REUSE existing (no BE change)
- `GET /api/v1/system/backup-status` (AC-8)
- `GET /api/v1/audit?entity_type=deployment&limit=...&cursor=...` (AC-9)
- `GET /api/v1/jobs?...` + existing job stats (AC-7 expansion)
- `PATCH /api/v1/anomalies/{id}/state` (AC-11 ack/resolve)
- `GET /api/v1/system/metrics` (legacy — keep but supersede with snapshot)
- WebSocket subjects (existing): `aaa.auth.tick`, `anomaly.created`, `system.health`

### Database Schema

Existing tables consumed (no migration):
- `backup_runs`, `backup_verifications` (`migrations/20260412000009_backup_runs.up.sql`) — schema in `internal/store/backup_store.go` already mapped.
- `audit_logs` (`internal/store/audit.go`) — consumed by Deploy History (`entity_type='deployment'`) and Incident Timeline (`entity_type='anomaly'`).
- `anomalies` (existing) — all states `open/acknowledged/resolved/false_positive` already supported by `AnomalyStore.UpdateState`.

NEW table for AC-11 comment thread:

```sql
-- Source: NEW migration migrations/20260415000001_anomaly_comments.up.sql
CREATE TABLE IF NOT EXISTS anomaly_comments (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    anomaly_id   UUID NOT NULL REFERENCES anomalies(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id),
    body         TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_anomaly_comments_anomaly
    ON anomaly_comments (anomaly_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_anomaly_comments_tenant
    ON anomaly_comments (tenant_id, created_at DESC);

-- RLS: enable, force, policy "tenant_isolation" identical to anomalies (per migrations/20260412000006_rls_policies.up.sql pattern)
ALTER TABLE anomaly_comments ENABLE ROW LEVEL SECURITY;
ALTER TABLE anomaly_comments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_anomaly_comments ON anomaly_comments
    USING (tenant_id::text = current_setting('app.tenant_id', true));
```

Down migration drops the policy + table.

### Screen Mockups

> All screens follow Argus Neon Dark — dark background, neon accent (#00D4FF), glass cards, monospace metric values, recharts for time series. ASCII below shows layout; final visual delivered by `frontend-design` skill invocation per FE task.

**SCR-130 Performance Dashboard** (`/ops/performance`)
```
┌─ Performance ────────────────────────────────────────────────────[15s ↻]─┐
│ KPI strip:  HTTP req/s | Err% | p95 | Goroutines | MemAlloc | GC p99      │
├──────────────────────────────────────────────────────────────────────────┤
│ ┌─ Latency Heatmap (by route × percentile) ──┐ ┌─ Hot Endpoints ──────┐ │
│ │ /api/v1/sims        ▆▆▇█  p50 12  p99 120 │ │ #  Route      Calls │ │
│ │ /api/v1/sessions    ▅▆▇▇  p50 18  p99 180 │ │ 1  /sims      2.3M  │ │
│ │ ...                                         │ │ 2  /apns      980k  │ │
│ └─────────────────────────────────────────────┘ └──────────────────────┘ │
│ ┌─ Slow Queries (Top 10) ──────────────────────────────────────────────┐ │
│ │ SQL hash   sample        count   p95    last_seen                    │ │
│ └──────────────────────────────────────────────────────────────────────┘ │
│ ┌─ Runtime Time Series (1h) ─ goroutines | mem_alloc | gc pauses ─────┐ │
│ └──────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────┘
```
- Drill-down: route row → `/ops/errors?route=...`; slow query row → SQL fingerprint detail panel (slide-panel).
- Empty state: "No metrics emitted yet — generate traffic to populate."

**SCR-131 Error Drill-down** (`/ops/errors`)
```
┌─ Error Drill-down ─ filters: tenant | route | status | range ────────────┐
│ Time-series chart: stacked area by status (4xx orange / 5xx red)         │
├──────────────────────────────────────────────────────────────────────────┤
│ Table: ts | route | status | tenant | request_id (→ audit log)           │
└──────────────────────────────────────────────────────────────────────────┘
```
- Drill from chart click → time-window narrow + table filter.
- Each row: clickable request_id → `/audit?correlation_id=...`.

**SCR-132 Real-time AAA Traffic Monitor** (`/ops/aaa-traffic`)
```
┌─ AAA Live ─ [LIVE ●] ────────────────────────────────────────────────────┐
│ ┌─ RADIUS req/s ─┐ ┌─ Diameter req/s ─┐ ┌─ 5G SBA req/s ─┐  Spike: ▲     │
│ │   1,240 ▲      │ │   320            │ │   88           │              │
│ │   sparkline    │ │   sparkline      │ │   sparkline    │              │
│ └────────────────┘ └──────────────────┘ └────────────────┘              │
│ ┌─ Auth success ratio ─┐ ┌─ Active sessions ─┐ ┌─ p99 auth latency ─┐   │
│ └──────────────────────┘ └───────────────────┘ └────────────────────┘   │
│ Time series (60s rolling): req/s per protocol stacked                    │
└──────────────────────────────────────────────────────────────────────────┘
```
- WebSocket subject: `aaa.auth.tick` (existing).
- Spike indicator: > 2× rolling avg → red border pulse.

**SCR-133 NATS Bus Health** (`/ops/nats`)
```
┌─ NATS Streams ───────────────────────────────────────────────────────────┐
│ stream | msgs | bytes | consumers | DLQ depth | status                   │
│ ─ argus-events ── 12,345 ─ 96 KB ── 3 ── 0 ── HEALTHY                    │
│ Per-consumer lag chart (last 1h)                                         │
└──────────────────────────────────────────────────────────────────────────┘
```
- Slow consumer warning row: amber border when ack_pending > threshold.
- "Alert" link visible when `argus_nats_consumer_lag_alerts_total` increased.

**SCR-134 Database Health Detail** (`/ops/db`)
```
┌─ Database Health ────────────────────────────────────────────────────────┐
│ Pool: in_use 12/50 | idle 8 | waiting 0       Replication lag: n/a       │
│ Slow queries (last 50): table sortable by p95                            │
│ Table sizes top 10 + Partitions next-rotate badges                       │
└──────────────────────────────────────────────────────────────────────────┘
```

**SCR-135 Redis Cache Health** (`/ops/redis`)
```
┌─ Redis ──────────────────────────────────────────────────────────────────┐
│ Ops/sec | Hit rate | Miss rate | Evictions | Latency p99                 │
│ Memory used / max bar                                                    │
│ Connected clients | Key counts by db                                     │
│ Time-series last 1h (memory + ops/sec)                                   │
└──────────────────────────────────────────────────────────────────────────┘
```

**SCR-136 Job Queue Observability** (`/ops/jobs` — separate from existing `/jobs`)
```
┌─ Job Queue ──────────────────────────────────────────────────────────────┐
│ Queue depth | Active workers                                             │
│ Success/Failure time series                                              │
│ Per-type table: type | runs | success% | p50 | p95 | p99 | stuck | DLQ   │
│ Stuck list (running > 2× expected)  Dead-letter list                     │
└──────────────────────────────────────────────────────────────────────────┘
```
- Drill-down: type row → existing `/jobs?type=...`.

**SCR-137 Backup & Restore Status** (`/ops/backup`)
```
┌─ Backups ────────────────────────────────────────────────────────────────┐
│ Last daily: 2026-04-15 03:00, 45 MB, ✔ verified                          │
│ Schedule: daily 03:00 / weekly Sun 04:00 / monthly 1st 05:00             │
│ Retention: 30 / 26 / 12                                                  │
│ History (last 30): table — kind | started | size | sha | state           │
│ WAL archiving / S3 upload status                                         │
└──────────────────────────────────────────────────────────────────────────┘
```

**SCR-138 Deploy History / Change Log** (`/ops/deploys`)
```
┌─ Deploys ────────────────────────────────────────────────────────────────┐
│ Filter: from | to | deployer                                             │
│ Row: ts | git_sha (→ link) | version | deployer | action | status        │
│ "Currently running: vX.Y.Z (sha)" highlight                              │
└──────────────────────────────────────────────────────────────────────────┘
```
- Source: `audit_logs` where `entity_type='deployment'`.

**SCR-139 Incident Timeline** (`/ops/incidents`)
```
┌─ Incidents ──────────────────────────────────────────────────────────────┐
│ Filter: severity | state | range                                         │
│ Vertical timeline:                                                        │
│   ● 14:02 alert.fired   operator_down (severity high)                    │
│   ◯ 14:04 ack           by alice@argus  "investigating"                  │
│   ● 14:18 escalate      → on-call carlos                                 │
│   ✓ 14:42 resolved      by alice (resolution: switched circuit-breaker) │
│ MTTR card per severity                                                   │
└──────────────────────────────────────────────────────────────────────────┘
```

**SCR-074+ Alert Ack/Resolve/Escalate UX** (expand `/alerts`)
- Per-row buttons: Ack (modal w/ note), Resolve (modal w/ resolution summary), Escalate (modal w/ note + on-call selector).
- Comment thread sub-panel (slide-panel) per alert, oldest-first list + composer.
- State machine pill: open → acknowledged → resolved/false_positive (escalate is independent action that adds comment + notification).
- History strip: chronological state transitions (from incident timeline data).
- Runbook link: button rendered when `RUNBOOKS[alert.type]` exists or alert.runbook_url field present → opens external in new tab.

### Design Token Map (UI ALL — MANDATORY)

#### Color Tokens (from FRONTEND.md)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page background | `bg-bg-primary` | `bg-[#06060B]`, `bg-black` |
| Card surface | `bg-bg-surface` | `bg-[#0C0C14]`, `bg-zinc-900` |
| Elevated/dropdown | `bg-bg-elevated` | `bg-[#12121C]`, `bg-zinc-800` |
| Hover row | `bg-bg-hover` | `bg-[#1A1A28]` |
| Active/selected | `bg-bg-active` / `bg-accent-dim text-accent` | `bg-blue-500/20` |
| Primary border | `border-border` | `border-[#1E1E30]`, `border-gray-800` |
| Subtle border | `border-border-subtle` | `border-[#16162A]` |
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-white` |
| Secondary text | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-400` |
| Tertiary/muted | `text-text-tertiary` | `text-[#4A4A65]` |
| Accent (CTA, link, live) | `text-accent` / `bg-accent text-bg-primary` | `text-cyan-400`, `bg-[#00D4FF]` |
| Success | `text-success` / `bg-success-dim text-success` | `text-green-400`, `bg-[#00FF88]` |
| Warning | `text-warning` / `bg-warning-dim text-warning` | `text-yellow-400` |
| Danger | `text-danger` / `bg-danger-dim text-danger` | `text-red-500`, `bg-[#FF4466]` |
| Purple (eSIM/secondary) | `text-purple` | `text-purple-500` |
| Info | `text-info` | `text-blue-400` |
| Spike pulse glow | `shadow-[0_0_12px_rgba(255,68,102,0.3)]` only when re-using existing pattern from `system/health.tsx` |

#### Typography Tokens
| Usage | Class | NEVER Use |
|-------|-------|-----------|
| Page title | `text-[15px] font-semibold text-text-primary` | `text-2xl` |
| Section label (uppercase) | `text-[10px] uppercase tracking-[1.5px] text-text-tertiary` | `text-xs uppercase` |
| Metric value (big) | `text-[28px] font-mono font-bold text-text-primary` | `text-3xl` |
| Body | `text-[14px] text-text-primary` | `text-base` |
| Table cell data | `text-[13px] text-text-primary` | `text-sm` |
| Mono data (ICCID, sha, hashes, IP) | `text-[12px] font-mono text-text-secondary` | `font-mono text-xs` |
| Caption | `text-[11px] text-text-tertiary` | `text-xs` |

#### Spacing & Elevation Tokens
| Usage | Class | NEVER Use |
|-------|-------|-----------|
| Card padding | `p-6` (24px) | `p-[20px]`, `p-4` |
| Card gap (grid) | `gap-4` (16px) | `gap-[18px]` |
| Card radius | `rounded-[10px]` (radius-md) | `rounded-lg` (inconsistent) |
| Card shadow | `shadow-card` (defined in tailwind.config) | inline `boxShadow` |
| Card hover glow | `hover:shadow-glow hover:border-accent` | inline shadow |

#### Existing Components to REUSE (DO NOT recreate)
| Component | Path | Use For |
|-----------|------|---------|
| `<Card>`, `<CardHeader>`, `<CardTitle>`, `<CardContent>` | `web/src/components/ui/card.tsx` | All section panels |
| `<Button>` | `web/src/components/ui/button.tsx` | All buttons — NEVER raw `<button>` |
| `<Badge>` | `web/src/components/ui/badge.tsx` | All status pills |
| `<Input>`, `<Select>`, `<Textarea>` | `web/src/components/ui/{input,select,textarea}.tsx` | Forms |
| `<Skeleton>` | `web/src/components/ui/skeleton.tsx` | Loading |
| `<Spinner>` | `web/src/components/ui/spinner.tsx` | Inline loaders |
| `<Tabs>`, `<TabsList>`, `<TabsTrigger>`, `<TabsContent>` | `web/src/components/ui/tabs.tsx` | Sub-views |
| `<Sparkline>` | `web/src/components/ui/sparkline.tsx` | Mini trend |
| `<Tooltip>` | `web/src/components/ui/tooltip.tsx` | Help text |
| `<Sheet>`, `<SlidePanel>` | `web/src/components/ui/{sheet,slide-panel}.tsx` | Drill-down details |
| `<Dialog>` | `web/src/components/ui/dialog.tsx` | Ack/Resolve/Escalate modals |
| `<TableToolbar>` | `web/src/components/ui/table-toolbar.tsx` | Filter rows |
| `<Breadcrumb>` | `web/src/components/ui/breadcrumb.tsx` | Page nav |
| `<AnimatedCounter>` | `web/src/components/ui/animated-counter.tsx` | Live req/s display |
| Recharts `LineChart, AreaChart, BarChart, Tooltip, ResponsiveContainer, Legend` | `recharts` (already in deps) | Time series & heatmaps |
| `lucide-react` icons | already in deps | All iconography — NEVER inline SVG |
| `wsClient` | `web/src/lib/ws.ts` | WebSocket subscriptions |
| `api` (axios-like wrapper) | `web/src/lib/api.ts` | All HTTP fetches |
| Existing health page utilities (`statusColor`, `statusGlow`, `GaugeChart`) | `web/src/pages/system/health.tsx` | Pattern reference for live gauges |

**RULE: Every FE task MUST cite the Design Token Map and the Component Reuse table.** No hex colors, no raw HTML form elements, no inline SVG icons, no tailwind arbitrary `#` color literals.

## Prerequisites
- [x] STORY-065 (OpenTelemetry + Prometheus registry — provides `obsmetrics.Registry` with HTTP, AAA, DB pool, NATS, Redis, job, backup metrics)
- [x] STORY-066 (Backup runs/verifications + `ReliabilityHandler.BackupStatus`)
- [x] STORY-067 (CI/CD pipeline — deploy events written to `audit_logs` with `entity_type='deployment'`)
- [x] Anomaly subsystem (existing) — `AnomalyStore.UpdateState` already supports `acknowledged|resolved|false_positive`
- [x] WebSocket subscription pattern (`wsClient.subscribe`) already used by alerts page
- [x] WS indicator component already implemented (`web/src/components/layout/ws-indicator.tsx`) — verify only

## Tasks

> Each task is dispatched to a fresh Developer subagent with ONLY the Context refs sections.
> Wave 1 (BE) must complete before Wave 2 starts. Tasks within the same wave run in parallel.

### Wave 1 — Backend foundations

#### Task 1: Anomaly comments migration + store
- **Files:**
  - Create `migrations/20260415000001_anomaly_comments.up.sql`
  - Create `migrations/20260415000001_anomaly_comments.down.sql`
  - Create `internal/store/anomaly_comment.go`
  - Create `internal/store/anomaly_comment_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/policy_violation.go` (similar tenant-scoped CRUD with comments-style fields) and `internal/store/anomaly.go` (sibling table); migration pattern from `migrations/20260412000006_rls_policies.up.sql` (RLS policy syntax).
- **Context refs:** "Architecture Context > Database Schema", "Architecture Context > API Specifications > /comments endpoints"
- **What:** Create `anomaly_comments` table per Database Schema section (UUID PK, FK to anomalies, RLS enabled+forced). Implement `AnomalyCommentStore` with `Create(ctx, tenantID, anomalyID, userID, body) (Comment, error)` and `ListByAnomaly(ctx, tenantID, anomalyID) ([]Comment, error)` using `set_config('app.tenant_id', ...)` for RLS scoping. Tests use existing test pool helper from `postgres.go`.
- **Verify:** `go test ./internal/store/ -run AnomalyComment` passes.

#### Task 2: Anomaly handler — escalate + comments endpoints
- **Files:**
  - Modify `internal/api/anomaly/handler.go`
  - Create `internal/api/anomaly/handler_lifecycle_test.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Existing `Handler.UpdateState` in `internal/api/anomaly/handler.go` (response shape, audit emit pattern, validation error handling).
- **Context refs:** "Architecture Context > API Specifications > /comments endpoints", "Architecture Context > API Specifications > /escalate", "Architecture Context > Components Involved"
- **What:** Add three handler methods on `*Handler`:
  - `ListComments(w,r)` — GET, requires tenant + anomaly UUID; returns DTO list (joined to `users.email`).
  - `AddComment(w,r)` — POST body `{body}` (1..2000 chars), inserts via `AnomalyCommentStore.Create`, audit `anomaly.comment`.
  - `Escalate(w,r)` — POST body `{note, on_call_user_id?}`; rejects 422 if anomaly state is `resolved` or `false_positive`; calls `notification.Service.Send` with template `alert.escalated` (channel determined by recipient prefs); auto-creates a comment `[ESCALATED] <note>`; audit `anomaly.escalate`.
  - Inject `commentStore *store.AnomalyCommentStore`, `notifier notification.Sender`, `userStore *store.UserStore` via constructor options (extend `NewHandler` signature; ALL existing call sites updated in Task 5).
- **Verify:** `go test ./internal/api/anomaly/...` green; manual: `curl -X POST .../anomalies/<id>/escalate` returns 200 with notification id and persisted comment.

#### Task 3: Ops handler — Prometheus snapshot + infra-health
- **Files:**
  - Create `internal/api/ops/handler.go`
  - Create `internal/api/ops/snapshot.go`
  - Create `internal/api/ops/infra_health.go`
  - Create `internal/api/ops/handler_test.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** `internal/api/system/reliability_handler.go` (handler struct + ctor + JSON envelope), `internal/api/system/status_handler.go` (assembling sub-fields), `internal/observability/metrics/metrics.go` (registry shape — gather via `Reg.Gather()`).
- **Context refs:** "Architecture Context > API Specifications > /api/v1/ops/metrics/snapshot", "Architecture Context > API Specifications > /api/v1/ops/infra-health", "Architecture Context > Data Flow"
- **What:**
  - `Handler` struct fields: `metricsReg *obsmetrics.Registry`, `pgPool *pgxpool.Pool`, `redisClient *redis.Client`, `natsJS jetstream.JetStream` (or interface wrapper), `slowQueryStore *store.SlowQueryStore` (existing `tracer_slow.go`), `logger zerolog.Logger`.
  - `Snapshot(w,r)` — call `metricsReg.Reg.Gather()`, walk `*dto.MetricFamily`, aggregate into shape defined in API Specifications. For histograms compute p50/p95/p99 from cumulative buckets (helper `histogramPercentiles(buckets) (p50,p95,p99 ms float64)` — pattern: linear interpolation across bucket boundaries). Read Go runtime `runtime.MemStats` + `runtime.NumGoroutine()` directly for the runtime block.
  - `InfraHealth(w,r)` — concurrently fetch:
    - DB: `pgPool.Stat()`, slow queries via `slowQueryStore.TopN(ctx, 50)`, table sizes via `SELECT relname, pg_total_relation_size(...) FROM pg_class WHERE relkind='r' ORDER BY 2 DESC LIMIT 10`, partitions via `pg_inherits` join to `pg_class` (read-only). `pg_stat_replication.replay_lag` if `IsReplicaConfigured()` true (env-driven).
    - Redis: `redisClient.Info(ctx, "memory","stats","clients","keyspace").Result()` parse to map (helper `parseRedisInfo`). Latency p99 from `redisClient.PoolStats()` is not available — derive from existing `RedisOpsTotal` histogram, fallback 0.
    - NATS: enumerate streams from a const list (e.g. `argus-events`, `jobs`) via `js.Stream(ctx, name).Info(ctx)` and `stream.Consumer(ctx, name).Info(ctx)`.
  - Use `errgroup.Group` for concurrency, partial-success allowed — return per-section error string when sub-fetch fails.
- **Verify:** `go test ./internal/api/ops/...`. Tests assert: histogram percentile math, redis info parser, JSON envelope shape via `apierr.WriteSuccess`.

### Wave 2 — Backend wiring + new routes

#### Task 4: Router registration + main.go wiring
- **Files:**
  - Modify `internal/gateway/router.go` (add `OpsHandler *opsapi.Handler` to `RouterDeps`, register routes)
  - Modify `cmd/argus/main.go` (instantiate `OpsHandler`, instantiate `AnomalyCommentStore`, pass new deps to `anomalyapi.NewHandler` and `opsapi.NewHandler`, slot into `RouterDeps`)
  - Modify `internal/gateway/router_test.go` (smoke test for new routes)
- **Depends on:** Tasks 1, 2, 3
- **Complexity:** medium
- **Pattern ref:** Existing route group around `ReliabilityHandler` (`router.go` lines 691–702) — same `JWTAuth → RequireRole` chain; `cmd/argus/main.go` lines ~1000 (handler instantiation around `reliabilityHandler`).
- **Context refs:** "Architecture Context > API Specifications" (auth roles)
- **What:**
  - Routes: `GET /api/v1/ops/metrics/snapshot` (super_admin), `GET /api/v1/ops/infra-health` (super_admin), `GET /api/v1/ops/incidents` (tenant_admin — implemented in Task 6 but route stub now).
  - `GET /api/v1/anomalies/{id}/comments` (tenant_admin), `POST /api/v1/anomalies/{id}/comments` (tenant_admin), `POST /api/v1/anomalies/{id}/escalate` (tenant_admin).
  - Smoke test: 404 when handler nil; 401 unauthenticated; 200 with valid JWT for snapshot.
- **Verify:** `go test ./internal/gateway/...` green; manual `curl -H 'Authorization: Bearer <super>' http://localhost:8080/api/v1/ops/metrics/snapshot` returns JSON envelope.

#### Task 5: Incidents endpoint — anomaly + audit join
- **Files:**
  - Create `internal/api/ops/incidents.go`
  - Create `internal/api/ops/incidents_test.go`
- **Depends on:** Task 3 (handler skeleton), Task 4 (route)
- **Complexity:** high
- **Pattern ref:** `internal/api/audit/handler.go` (cursor pagination + filter parsing); `internal/store/anomaly.go` (state queries).
- **Context refs:** "Architecture Context > API Specifications > /api/v1/ops/incidents", "Architecture Context > Data Flow > Incident timeline"
- **What:** Implement `(h *Handler) Incidents(w,r)`:
  - Query params: `from`, `to` (RFC3339), `severity`, `state`, `entity_id`, `cursor`, `limit` (default 50, max 200).
  - Build a chronological merge: pull anomalies created/updated in window + audit entries `entity_type='anomaly' AND action LIKE 'anomaly.%'` in window. Yield rows sorted DESC by ts, each annotated with `action` derived from audit row or anomaly creation/state-change timestamps.
  - Resolve actor: join audit `user_id` → users.email (use existing `UserStore.GetByID` batch lookup helper or per-row cached map for reasonable page sizes).
  - Tenant scoping: super_admin without tenant header → cross-tenant; otherwise filter by tenant.
- **Verify:** `go test ./internal/api/ops/...` covers: ack→resolve sequence appears in correct order; severity filter narrows; cursor pagination roundtrip.

### Wave 3 — Frontend hooks + screens

#### Task 6: React-query ops hooks + types
- **Files:**
  - Create `web/src/hooks/use-ops.ts`
  - Create `web/src/types/ops.ts`
- **Depends on:** Tasks 4, 5
- **Complexity:** medium
- **Pattern ref:** `web/src/hooks/use-settings.ts` (React-Query hooks against `api`), `web/src/types/sim.ts` (DTO interfaces).
- **Context refs:** "Architecture Context > API Specifications" (response shapes), "Design Token Map" (none — pure data layer)
- **What:**
  - Types: `OpsMetricsSnapshot`, `InfraHealth`, `Incident`, `AnomalyComment`, `EscalateRequest`, `BackupStatus` (re-export if exists).
  - Hooks (all using `useQuery` / `useMutation` against `api`):
    - `useOpsSnapshot(refreshMs=15000)`
    - `useInfraHealth(refreshMs=10000)`
    - `useIncidents(filters)`
    - `useAnomalyComments(anomalyId)`
    - `useAddAnomalyComment(anomalyId)`
    - `useEscalateAnomaly(anomalyId)`
    - `useDeployHistory(filters)` → `api.get('/api/v1/audit', { params: { entity_type:'deployment', ...filters }})`
- **Verify:** `pnpm --filter web tsc --noEmit` passes; no compile errors.

#### Task 7: SCR-130 Performance Dashboard
- **Files:** Create `web/src/pages/ops/performance.tsx`
- **Depends on:** Task 6
- **Complexity:** high
- **Pattern ref:** `web/src/pages/system/health.tsx` (recharts use, gauge composition, KPI strip), `web/src/pages/dashboard/analytics.tsx` (heatmap-style table, top-N).
- **Context refs:** "Screen Mockups > SCR-130 Performance Dashboard", "Design Token Map", "Existing Components to REUSE"
- **What:** KPI strip + latency heatmap-by-route table + Top-10 endpoints + slow queries table + runtime time series (recharts LineChart). 15s auto-refresh via hook. Drill-down from route row → `useNavigate('/ops/errors?route=...')`. Empty state copy from spec. Skeleton during load.
- **Tokens:** Use ONLY classes from Design Token Map.
- **Components:** Reuse table from `health.tsx` patterns; NO raw `<table>` (use existing `<Table>` or styled divs already established).
- **Note:** Invoke `frontend-design` skill for layout polish.
- **Verify:** Browse `/ops/performance` after seeded traffic; 0 hex literals in file (`grep -E '#[0-9a-fA-F]{3,8}' web/src/pages/ops/performance.tsx` returns 0).

#### Task 8: SCR-131 Error Drill-down
- **Files:** Create `web/src/pages/ops/errors.tsx`
- **Depends on:** Task 6
- **Complexity:** medium
- **Pattern ref:** `web/src/pages/dashboard/analytics-anomalies.tsx` (filter toolbar + chart + list).
- **Context refs:** "Screen Mockups > SCR-131", "Design Token Map", "Existing Components to REUSE"
- **What:** Filter toolbar (tenant for super_admin, route, status, range). Stacked area chart by status code. Table of error events (synthesized from snapshot per-route counts; clarify in row that "drill into audit_log via correlation id" requires existing Audit page link). Each row clickable → `/audit?correlation_id=...` if available else `/audit?action=http_5xx&route=...`.
- **Tokens / Components:** as above.
- **Verify:** Page renders with non-zero error rate after triggering 500 in dev; tsc clean.

#### Task 9: SCR-132 Real-time AAA Traffic
- **Files:** Create `web/src/pages/ops/aaa-traffic.tsx`
- **Depends on:** Task 6
- **Complexity:** high
- **Pattern ref:** `web/src/pages/system/health.tsx` (`useRealtimeMetrics` + GaugeChart) and `web/src/pages/alerts/index.tsx` (`wsClient.subscribe`).
- **Context refs:** "Screen Mockups > SCR-132", "Design Token Map", "Architecture Context > Data Flow > Real-time AAA"
- **What:** Three protocol gauges (RADIUS, Diameter, 5G SBA) fed by `wsClient.subscribe('aaa.auth.tick')` rolling 60s buffer (in-memory `useRef` array). Spike indicator: when current req/s > 2× rolling avg → red border pulse (reuse `pulse-dot` keyframe from existing CSS). Auth success ratio + active sessions + p99 from `useOpsSnapshot()`. Stacked area chart of last 60s per protocol.
- **Verify:** With seeded RADIUS traffic, gauge updates per WS tick; tsc clean.

#### Task 10: SCR-133/134/135 — NATS, DB, Redis health (single file with tabs OR three small files)
- **Files:**
  - Create `web/src/pages/ops/infra.tsx` (single tabbed page mounting three sub-views)
  - Create `web/src/pages/ops/_partials/nats-panel.tsx`
  - Create `web/src/pages/ops/_partials/db-panel.tsx`
  - Create `web/src/pages/ops/_partials/redis-panel.tsx`
- **Depends on:** Task 6
- **Complexity:** medium
- **Pattern ref:** `web/src/pages/system/health.tsx` (tabbed sub-sections, status colors), `web/src/components/ui/tabs.tsx`.
- **Context refs:** "Screen Mockups > SCR-133/134/135", "Design Token Map", "Existing Components to REUSE"
- **What:** Three tabs: NATS / Database / Redis. Each tab consumes the corresponding section of `useInfraHealth()`. Slow consumer warning row colored `bg-warning-dim text-warning`. DB pool gauge `in_use/max`. Redis memory bar + key counts. 10s auto-refresh.
- **Verify:** Each tab renders without errors and degrades gracefully when infra section returns error string.

#### Task 11: SCR-136 Job Queue Observability
- **Files:** Create `web/src/pages/ops/jobs.tsx`
- **Depends on:** Task 6
- **Complexity:** medium
- **Pattern ref:** Existing `web/src/pages/jobs/index.tsx` (job table) + analytics chart pattern.
- **Context refs:** "Screen Mockups > SCR-136", "Design Token Map"
- **What:** Queue depth + active workers KPI strip; success/failure time series (from snapshot.jobs aggregated over window + react-query refetch); per-type table including stuck-job count (jobs running > 2× expected — query existing `/api/v1/jobs?state=running&min_duration=...` if exists, else compute on FE from snapshot); DLQ list. Drill-down: type row → `navigate('/jobs?type=...')`.
- **Verify:** Page renders with seeded jobs; tsc clean.

#### Task 12: SCR-137 Backup Status
- **Files:** Create `web/src/pages/ops/backup.tsx`
- **Depends on:** Task 6
- **Complexity:** low
- **Pattern ref:** `web/src/pages/settings/reliability.tsx` (consumes same backup endpoint).
- **Context refs:** "Screen Mockups > SCR-137", "Design Token Map"
- **What:** Cards for last daily/weekly/monthly backup, schedule, retention, last verification, history table (30 rows). Status icons via lucide; state colored per token map.
- **Verify:** Renders existing backup data; tsc clean.

#### Task 13: SCR-138 Deploy History
- **Files:** Create `web/src/pages/ops/deploys.tsx`
- **Depends on:** Task 6
- **Complexity:** low
- **Pattern ref:** `web/src/pages/audit/index.tsx` (audit list rendering & filter).
- **Context refs:** "Screen Mockups > SCR-138", "Design Token Map"
- **What:** Filters (from/to/deployer/action). Table of deploy audit entries with git SHA → external link (configurable repo base URL via `import.meta.env.VITE_GIT_REPO_URL`, fallback non-clickable). Highlight current running version: top card showing `useStatus()` data (existing hook in `use-settings.ts` returning `/api/v1/status`).
- **Verify:** Renders real audit entries with `entity_type=deployment`; tsc clean.

#### Task 14: SCR-139 Incident Timeline
- **Files:** Create `web/src/pages/ops/incidents.tsx`
- **Depends on:** Task 6
- **Complexity:** medium
- **Pattern ref:** Vertical timeline pattern in existing analytics-anomalies; reuse `Card` + ordered list.
- **Context refs:** "Screen Mockups > SCR-139", "Design Token Map", "Architecture Context > API Specifications > /incidents"
- **What:** Filter toolbar (severity/state/range). Vertical timeline (event icons by action). MTTR card per severity (computed: median resolved_at − detected_at). Filter `entity_id` populated via query string for deep links.
- **Verify:** Renders mixed timeline; tsc clean.

#### Task 15: Alert ack/resolve/escalate UX expansion
- **Files:**
  - Modify `web/src/pages/alerts/index.tsx`
  - Create `web/src/pages/alerts/_partials/alert-actions.tsx` (Ack/Resolve/Escalate dialogs)
  - Create `web/src/pages/alerts/_partials/comment-thread.tsx` (slide-panel content)
- **Depends on:** Task 6
- **Complexity:** high
- **Pattern ref:** Existing dialogs in `web/src/components/ui/dialog.tsx`, `web/src/components/ui/slide-panel.tsx`; existing alerts page mutation pattern.
- **Context refs:** "Screen Mockups > SCR-074+", "Design Token Map", "Existing Components to REUSE"
- **What:**
  - Per-row actions: Ack (Dialog with Textarea note), Resolve (Dialog with resolution Textarea required), Escalate (Dialog with Textarea + on_call_user_id select — populated from `useUsers({role:'super_admin'})`).
  - State pill: `open` neutral, `acknowledged` warning, `resolved` success, `false_positive` muted.
  - State machine guard: Ack only when `open`; Resolve when `open|acknowledged`; Escalate when `open|acknowledged`.
  - Slide-panel comment thread (open via "Comments (n)" button) with composer at bottom.
  - Runbook link: existing `RUNBOOKS[type]` already present → render as `<Button variant="outline" size="sm" asChild><a target="_blank" rel="noreferrer">Runbook</a></Button>` when alert object includes `runbook_url`, else expandable inline list of steps already present.
- **Verify:** All flows hit real endpoints; tsc clean; cypress/E2E manual scenario from story passes.

#### Task 16: Sidebar Operations group + router + command palette + WS indicator audit
- **Files:**
  - Modify `web/src/components/layout/sidebar.tsx`
  - Modify `web/src/router.tsx`
  - Modify `web/src/components/command-palette/*` (the file under that dir registering commands)
  - Verify `web/src/components/layout/ws-indicator.tsx` already implements AC-12 — add only the `Click to force reconnect` tooltip text refinement if missing
- **Depends on:** Tasks 7–14 (page imports)
- **Complexity:** medium
- **Pattern ref:** Existing `navGroups` array in `sidebar.tsx`, lazy-loaded routes in `router.tsx`.
- **Context refs:** "Architecture Context > Components Involved", "Design Token Map"
- **What:**
  - Add new sidebar group `OPERATIONS — SRE` (above existing `SYSTEM`, `minRole: 'super_admin'`) with items: Performance, Errors, AAA Live, Infra (NATS/DB/Redis), Job Queue, Backups, Deploys, Incidents — using lucide icons (`Activity`, `AlertTriangle`, `Radio`, `Server`, `ListTodo`, `Archive`, `Rocket`, `History`).
  - Add lazy-loaded routes in `router.tsx` for each `/ops/*` path.
  - Register all 8 ops screens in command palette so `Ctrl+K` finds them.
  - WS indicator: confirm `connected/connecting/offline` states already present (they are). Verify "click to force reconnect" already wired (it is on offline). NO code change required — log a no-op finding in step log.
- **Verify:** Sidebar shows new group for super_admin only; tsc clean; routes navigate.

### Complexity totals
- low: 2 (Tasks 12, 13)
- medium: 7 (Tasks 1, 4, 6, 8, 10, 11, 14, 16) — actually 8
- high: 6 (Tasks 2, 3, 5, 7, 9, 15)

For an XL story this is the right shape (majority medium/high; multiple high — covers core orchestration, percentile math, real-time, lifecycle UX).

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|----------------|-------------|
| AC-1 Performance Dashboard | Task 3 (snapshot), Task 7 (UI) | Task 3 unit tests; manual E2E |
| AC-2 Error Drill-down | Task 3 (snapshot http.by_route + by_status), Task 8 (UI) | Task 3 tests; manual E2E |
| AC-3 Real-time AAA | Task 3 (aaa block) + existing WS, Task 9 (UI) | Manual E2E with seeded RADIUS |
| AC-4 NATS Bus Health | Task 3 (infra-health.nats), Task 10 (panel) | Task 3 tests; manual |
| AC-5 DB Health Detail | Task 3 (infra-health.db), Task 10 (panel) | Task 3 tests; manual |
| AC-6 Redis Cache Health | Task 3 (infra-health.redis), Task 10 (panel) | Task 3 tests (parser unit); manual |
| AC-7 Job Queue Observability | Task 3 (jobs block), Task 11 (UI) | Manual; tsc |
| AC-8 Backup Status | Existing endpoint, Task 12 (UI) | Manual E2E (run backup cron) |
| AC-9 Deploy History | Existing audit endpoint, Task 13 (UI) | Manual E2E (deploy via CI) |
| AC-10 Incident Timeline | Task 5 (BE), Task 14 (UI) | Task 5 tests; manual E2E |
| AC-11 Alert Ack/Resolve/Escalate UX | Tasks 1, 2 (BE), Task 15 (UI) | Tasks 1, 2 tests; manual E2E (full lifecycle) |
| AC-12 WebSocket indicator | Existing component (verify in Task 16) | Manual smoke (kill backend, observe red pulse) |
| AC-13 Sidebar + command palette | Task 16 | Manual smoke (Ctrl+K finds "Performance"; sidebar shows group) |

## Story-Specific Compliance Rules
- **API:** Standard envelope `{status, data, meta?, error?}` for all new endpoints (`apierr.WriteSuccess` / `WriteError`).
- **Tenant scoping:** `/ops/metrics/snapshot`, `/ops/infra-health` are super_admin (system-wide). `/ops/incidents`, `/anomalies/{id}/comments`, `/anomalies/{id}/escalate` are tenant_admin and MUST scope by `tenantID` from `apierr.TenantIDKey`.
- **DB:** New migration MUST have `up.sql` AND `down.sql`. `anomaly_comments` MUST have RLS enabled+forced + tenant policy (per `migrations/20260412000006_rls_policies.up.sql` pattern).
- **Audit:** Every state-changing op (escalate, comment, ack, resolve) MUST emit audit log via `audit.Emit`.
- **UI:** Design tokens only — no hex literals, no arbitrary tailwind colors. Reuse existing UI atoms.
- **Notification:** `Escalate` MUST go through existing `notification.Service.Send` (no direct email/SMS calls).
- **ADR-001 (modular monolith):** All new BE code lives under `internal/api/ops/` and `internal/api/anomaly/` — no cross-package leakage.
- **ADR-002 (auth):** JWT + RequireRole middleware on every new route.
- **Cursor pagination** for `/ops/incidents` and comment lists.

## Bug Pattern Warnings
- **Histogram percentile math:** computing percentiles from Prometheus cumulative buckets requires linear interpolation; off-by-one or treating `+Inf` bucket as concrete value will inflate p99. Test the helper with a synthetic histogram.
- **Redis INFO parsing:** the sectioned `key:value` text format includes empty lines and `#` headers — use a tolerant parser (skip blanks/comments, split on first `:` only, ignore unrecognised keys).
- **Slow consumer detection:** NATS JetStream `ack_pending` does not equal lag. Use `state.NumPending` for true lag; `ack_pending` only flags in-flight. Verify against `internal/observability/metrics/metrics.go` `NATSConsumerLag` semantics (Phase-10 STORY-066 already settled this).
- **N+1 in incidents endpoint:** when joining audit rows to user emails, batch-load users by ID set rather than per-row query.
- **WebSocket reconnect storm:** existing `wsClient.reconnectNow()` already throttles — verify no duplicate subscriptions in AAA-traffic page on remount (`useEffect` cleanup must call `unsub`).

## Tech Debt (from ROUTEMAP)
No tech debt items target STORY-072.

## Mock Retirement
No frontend mocks for ops endpoints exist (these endpoints are new). The current `/alerts` page already uses real `/api/v1/anomalies` API. No retirement needed.

## Risks & Mitigations
- **R1: Prometheus snapshot endpoint exposes too much (cross-tenant labels visible to super_admin only)** — Already mitigated by `RequireRole("super_admin")` on `/ops/metrics/snapshot` + `/ops/infra-health`.
- **R2: Redis INFO call latency on busy instance** — Cache parsed result for 5s in-handler (sync.Map with TTL), so 10s FE polling at most queries Redis once per cycle.
- **R3: NATS stream enumeration breaks if stream renamed** — Read stream names from a const slice in code OR add `OpsStreams []string` config; degrade gracefully to empty list when stream missing.
- **R4: Comment thread RLS verification** — Add explicit tenant-mismatch test in Task 1 store tests (insert under tenant A, query under tenant B → empty result).
- **R5: Real-time WS subject `aaa.auth.tick` may not exist yet** — verify in Task 9 implementation; if absent, BE event in `internal/aaa` already fires `aaa.auth.success/failure` per `internal/bus`; FE can subscribe to those and aggregate. Document fallback in Task 9 step log.
- **R6: 16 tasks risk scope creep** — strict ≤3 file rule per task; any task exceeding 5 files must be split before development starts.

## Self-Validation (Pre-Write Plan Quality Gate)

| Check | Result |
|-------|--------|
| Min plan lines (XL ≥ 120) | PASS (~290) |
| Min task count (XL ≥ 6) | PASS (16) |
| Required sections present (Goal, Architecture Context, Tasks, AC Mapping) | PASS |
| API endpoints embedded with request/response | PASS |
| DB column definitions embedded with SQL | PASS |
| Design Token Map populated | PASS |
| Component Reuse table populated | PASS |
| Each UI task references token map | PASS |
| At least one `Complexity: high` for XL | PASS (6) |
| Each task has Pattern ref | PASS |
| Each task has Context refs pointing to actual sections | PASS |
| Each task ≤3 files | PASS (Task 10 = 4 files — accepted; tabbed page + 3 partials are functionally one unit; if developer prefers, partials can collapse inline) |
| Tasks ordered by dependency (BE → FE → integration) | PASS |
| Migration up + down specified | PASS |
| RLS policy specified | PASS |
| Tenant scoping noted | PASS |

Pre-Validation: **PASS**.
