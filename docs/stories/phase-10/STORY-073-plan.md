# Implementation Plan: STORY-073 — Multi-Tenant Admin & Compliance Screens

> Phase 10 — zero-deferral. Effort: L. Complexity: Medium-High (many screens + some new BE endpoints).
> Status: Plan. Dispatched: 2026-04-13.
> Dependencies DONE: STORY-068 (enterprise auth), STORY-069 (onboarding/reports/webhooks/DSAR/KVKK).

## Goal
Deliver 12 super_admin / tenant_admin admin & compliance screens (SCR-140..SCR-151) that close Phase 10's enterprise screen audit gap, backed by the minimum BE endpoints required (two new tables: `kill_switches`, `maintenance_windows`; ~8 lightweight admin read endpoints).

## Architecture Context

### Components Involved
- `web/src/pages/admin/**` (NEW) — 12 new pages, super_admin-gated, under `/admin/...` routes.
- `web/src/pages/admin/quotas.tsx` (NEW) — SCR-141 quota breakdown (derived from `GET /api/v1/tenants` + `tenants/:id/stats`).
- `web/src/pages/system/tenants.tsx` (MODIFY) — link quota progress into new admin screen drill-down.
- `web/src/components/layout/sidebar.tsx` (MODIFY) — AC-13 "Admin" section with 12 links, super_admin-only.
- `web/src/router.tsx` (MODIFY) — register the 12 new lazy routes.
- `web/src/hooks/use-admin.ts` (NEW) — React-Query hooks for every admin endpoint.
- `web/src/types/admin.ts` (NEW) — TS types for all new admin resources.
- `internal/api/admin/` (NEW Go package) — handlers for:
  - `tenant_resources.go` — cross-tenant aggregation (SCR-140 + SCR-141 + SCR-142).
  - `security_events.go` — thin wrapper on audit with security action whitelist (SCR-143).
  - `sessions_global.go` — cross-tenant active portal sessions list + force-logout (SCR-144).
  - `api_usage.go` — per-API-key request-rate snapshot from Redis rate-limit counters (SCR-145).
  - `dsar_queue.go` — DSAR queue view on `jobs` table filtered by job_type (SCR-146).
  - `purge_history.go` — dedicated view on `sim_state_history` for purge runs (SCR-148).
  - `kill_switch.go` — CRUD + toggle for `kill_switches` table (SCR-149).
  - `maintenance_window.go` — CRUD for `maintenance_windows` table (SCR-150).
  - `delivery_status.go` — aggregated delivery stats from `webhook_deliveries` + `notifications` (SCR-151).
- `internal/store/killswitch.go` (NEW) + `internal/store/maintenance_window.go` (NEW)
- `internal/killswitch/` (NEW, small package) — runtime cache for kill-switch flags, enforcement helpers used by AAA + gateway + job + notification layers.
- `internal/gateway/router.go` (MODIFY) — mount new admin routes; super_admin group.
- `internal/aaa` + `internal/job` + `internal/api/sim` + `internal/notification` (MODIFY — minimal) — consult `killswitch` service at mutation entry points (return 503 `SERVICE_DEGRADED` when a matching flag is on).
- `migrations/20260416000001_admin_compliance.{up,down}.sql` (NEW)
- `cmd/argus/main.go` (MODIFY) — wire new stores, handlers, killswitch service.
- `docs/SCREENS.md`, `docs/architecture/api/_index.md`, `docs/USERTEST.md`, `docs/brainstorming/decisions.md`, `docs/ROUTEMAP.md`, `docs/CONFIG.md` (MODIFY — documentation).

### Data Flow

**Multi-Tenant Resources (SCR-140):**
1. super_admin → `GET /api/v1/admin/tenants/resources?sort=sim_count&limit=100`
2. Handler calls `TenantStore.List` + fans out `TenantStore.GetTenantStats` (existing) per tenant, aggregates SIM count / API req/s (from Redis `ratelimit:tenant:*`) / active sessions / 24h CDR bytes (`analytics.metrics.collector.CDRVolume`) / storage used (`pg_total_relation_size` per tenant schema OR sims×avg-row approximation).
3. Response: array of `{ tenant_id, name, sim_count, api_rps, active_sessions, cdr_bytes_24h, storage_bytes, spark_series: {…} }`.

**Cost per Tenant (SCR-142):** existing `/api/v1/analytics/cost?tenant_id=…` per-tenant endpoint (STORY-039 era). We reuse + add a small aggregator `GET /api/v1/admin/cost/by-tenant?period=month` that iterates active tenants. Budget alerts configured via existing `notification_preferences` (new event_type `budget_threshold`).

**Security Event Feed (SCR-143):** fully FE-only. Filter `GET /api/v1/audit-logs` with `action IN ('auth.login_failed', 'auth.lockout', 'auth.2fa_failed', 'apikey.abuse', 'ip.blocked', 'user.privilege_escalated', 'user.password_reset')` (pass CSV via new optional `actions` query parameter — add to existing audit handler). Realtime via existing WebSocket `audit.created` event.

**Active User Sessions (SCR-144):** new `GET /api/v1/admin/sessions/active?tenant_id=…&limit=50` (super_admin global; tenant_admin scope-locked). Reads from existing `user_sessions` table. Force-logout reuses `POST /api/v1/users/:id/revoke-sessions?session_id=…` (session_id query param added to existing handler) and drops WS. GeoIP — optional best-effort via MaxMind if configured else `null`; do NOT introduce a new dependency — if `geoip.DB` is nil, emit `location: null`.

**API Usage (SCR-145):** new `GET /api/v1/admin/api-keys/usage?window=1h|24h|7d`. Handler reads Redis keys `ratelimit:apikey:{hash}:min` / `…:hour` summed across window buckets (the existing `checkRateLimit` already writes these). For anomaly spike detection reuse EMA algorithm from `internal/analytics/anomaly/detector.go`. Block/throttle per key = `PATCH /api/v1/api-keys/:id` already supports rate_limit; add `state=suspended` flag to `api_keys` already supported (`state`).

**DSAR Queue (SCR-146):** new `GET /api/v1/admin/dsar/queue` — SELECT from `jobs` WHERE type IN ('data_portability_export','kvkk_purge_daily','sim_erasure') ORDER BY created_at DESC. Derives status (`received | processing | completed | delivered`) from `jobs.state` (queued → received, running → processing, succeeded → completed, notification sent → delivered via `jobs.payload.notified_at`). SLA timer = NOW() − created_at; red if > 30d. Generate-response button = enqueue existing `data_portability_export` job via existing `POST /api/v1/compliance/data-portability/:user_id`.

**Compliance Posture (SCR-147):** reuse existing `GET /api/v1/compliance/dashboard`. No new BE. FE computes KVKK/GDPR/BTK scores as: KVKK = `% of sims with retention_days <= 365 + % of users with consent captured + % of auditable events with hash chain valid`; GDPR = `pending_purge_count=0 ? 100 : 70` + `dsar_sla_met_pct`; BTK = `btk_report_last_run_at within 30d ? 100 : 50`. Checklist rendered from static config JSON.

**Data Purge History (SCR-148):** new `GET /api/v1/admin/purge-history?tenant=…&limit=50&cursor=…` — SELECT from `sim_state_history` WHERE `to_state='purged'` plus from `compliance_retention_runs` (exists from STORY-059). "Run Purge Now" = `POST /api/v1/jobs` with `type=kvkk_purge_daily`, `payload={dry_run: false}` (existing).

**Kill Switch (SCR-149):** new table `kill_switches`. `GET /api/v1/admin/kill-switches` lists all flags (seed 5 canonical: `radius_auth`, `session_create`, `bulk_operations`, `read_only_mode`, `external_notifications`). `PATCH /api/v1/admin/kill-switches/:key` toggles `{enabled: bool, reason: string}`. On change:
  - Insert audit entry `killswitch.toggled`.
  - `killswitch.Service.Reload()` refreshes in-memory cache (15s TTL fallback pull from DB).
  - Mutation entry points (RADIUS Access-Request, `POST /api/v1/sessions`, `POST /api/v1/sims/bulk/*`, `POST /api/v1/notifications/*`, all non-GET under `read_only_mode`) check `killswitch.IsEnabled(key)` and return `503 SERVICE_DEGRADED` `{code, key, reason}`.

**Maintenance Windows (SCR-150):** new table `maintenance_windows`. `GET /api/v1/admin/maintenance-windows?active=true`; `POST` to schedule; `DELETE` to cancel. Active window banner consumed by portal in STORY-077 (out of scope). Recurring schedule = `cron_expression` column.

**Notification Delivery Status (SCR-151):** new `GET /api/v1/admin/delivery/status`. Aggregates:
  - Webhook: `webhook_deliveries` GROUP BY final_state + latency percentiles (`response_status`, `created_at..updated_at`).
  - In-app: `notifications` GROUP BY read/unread, 1h/24h counts.
  - Email/SMS: `sms_outbound` GROUP BY status + stub row for email (if notification table has a channel column).
  - Retry: count `webhook_deliveries` where final_state='retrying'.
  - Per-channel: `{success_rate, failure_rate, retry_depth, last_delivery_at, latency_p50/p95/p99, health: green|yellow|red}` (health = success_rate >= 0.98 green, >= 0.85 yellow, else red).

### API Specifications

All use standard envelope `{status, data, meta?, error?}`. All mutations emit audit entries. All new admin endpoints require JWT + `super_admin` (except AC-13 subset: tenant_admin sees quota / security events / DSAR queue scoped to own tenant).

**New endpoints (12):**

- `GET /api/v1/admin/tenants/resources?sort={sim_count|api_rps|active_sessions}&limit=100` — super_admin
  - 200 `{status, data: Array<{tenant_id, name, slug, sim_count, api_rps, active_sessions, cdr_bytes_24h, storage_bytes, spark_series: {sim_count_7d: number[12], api_rps_7d: number[12]}}>}`
- `GET /api/v1/admin/cost/by-tenant?period={day|week|month|quarter}` — super_admin
  - 200 `{status, data: Array<{tenant_id, name, cost_total, currency, by_operator: map, by_apn: map, by_rat: map, top_sims: Array<{iccid, cost}>, trend_6m: number[6]}>}`
- `GET /api/v1/audit-logs?actions=a,b,c` — EXTENDED existing; add CSV `actions` filter. super_admin+tenant_admin.
- `GET /api/v1/admin/sessions/active?tenant_id=…&cursor=…&limit=50` — super_admin (global) / tenant_admin (scope-locked)
  - 200 `{status, data: Array<{session_id, user_id, user_email, tenant_id, tenant_name, ip_address, user_agent, ua_parsed: {device, os, browser}, location: {country, city}|null, created_at, last_activity_at, idle_seconds}>, meta:{cursor, has_more}}`
- `POST /api/v1/users/:id/revoke-sessions?session_id=…` — EXTENDED; specific-session revoke added.
- `GET /api/v1/admin/api-keys/usage?window={1h|24h|7d}&cursor=…&limit=50` — super_admin
  - 200 `{status, data: Array<{api_key_id, name, prefix, requests, rate_limit, consumption_pct, error_rate, top_endpoints: Array<{path, count}>, anomaly: boolean, ema: number}>, meta:{cursor, has_more}}`
- `GET /api/v1/admin/dsar/queue?status={received|processing|completed|delivered}&cursor=…&limit=50` — super_admin / tenant_admin (scope)
  - 200 `{status, data: Array<{job_id, type, user_id, user_email, tenant_id, status, assignee_user_id, sla_days_remaining, created_at, completed_at}>, meta:{cursor, has_more}}`
- `GET /api/v1/admin/purge-history?tenant_id=…&cursor=…&limit=50` — super_admin / tenant_admin (scope)
  - 200 `{status, data: Array<{id, tenant_id, date, sims_pseudonymized, sims_deleted, policy_applied, initiator, dry_run}>, meta:{cursor, has_more}}`
- `GET /api/v1/admin/kill-switches` — super_admin
  - 200 `{status, data: Array<{key, label, description, enabled, reason, toggled_by, toggled_at}>}`
- `PATCH /api/v1/admin/kill-switches/:key` — super_admin
  - Req: `{enabled: bool, reason: string}` (reason required when enabled=true)
  - 200 `{status, data: KillSwitch}`
  - 422 `VALIDATION_ERROR` if reason empty on enable
- `GET /api/v1/admin/maintenance-windows?active=true` — super_admin+tenant_admin
  - 200 `{status, data: Array<{id, title, description, starts_at, ends_at, affected_services: string[], cron_expression|null, state, notify_plan, created_by, created_at}>}`
- `POST /api/v1/admin/maintenance-windows` — super_admin
  - Req: `{title, description, starts_at, ends_at, affected_services: string[], cron_expression?: string, notify_plan: {advance_minutes: number, channels: string[]}}`
  - 201 `{status, data: MaintenanceWindow}`
- `DELETE /api/v1/admin/maintenance-windows/:id` — super_admin
- `GET /api/v1/admin/delivery/status?window={1h|24h|7d}` — super_admin
  - 200 `{status, data: {webhook:{success_rate,failure_rate,retry_depth,latency_p50,latency_p95,latency_p99,last_delivery_at,health}, email:{…}, sms:{…}, in_app:{…}, telegram:{…}}}`

**Error codes (new):** `SERVICE_DEGRADED` (503 when kill-switch on), `MAINTENANCE_WINDOW_ACTIVE` (503 when active window matches request).

### Database Schema

> Source: ARCHITECTURE.md design — these are NEW tables.

```sql
-- migrations/20260416000001_admin_compliance.up.sql

-- Kill switches: runtime feature flags used to degrade or disable subsystems.
CREATE TABLE IF NOT EXISTS kill_switches (
    key          VARCHAR(64) PRIMARY KEY,
    label        VARCHAR(128) NOT NULL,
    description  TEXT NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT false,
    reason       TEXT,
    toggled_by   UUID REFERENCES users(id),
    toggled_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the 5 canonical kill switches (idempotent)
INSERT INTO kill_switches (key, label, description) VALUES
  ('radius_auth',           'Disable RADIUS Auth',            'Reject all RADIUS Access-Request with Access-Reject'),
  ('session_create',        'Disable New Session Creation',   'Reject new session creation (AAA + portal)'),
  ('bulk_operations',       'Disable Bulk Operations',        'Reject bulk SIM state-change / policy-assign / eSIM-switch'),
  ('read_only_mode',        'Read-Only Mode',                 'Reject all non-GET mutations except auth/logout'),
  ('external_notifications','Disable External Notifications', 'Suppress email/SMS/webhook/telegram dispatch')
ON CONFLICT (key) DO NOTHING;

-- Maintenance windows: scheduled service degradation periods.
CREATE TABLE IF NOT EXISTS maintenance_windows (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID REFERENCES tenants(id),  -- NULL = global
    title              VARCHAR(255) NOT NULL,
    description        TEXT NOT NULL,
    starts_at          TIMESTAMPTZ NOT NULL,
    ends_at            TIMESTAMPTZ NOT NULL,
    affected_services  VARCHAR[] NOT NULL DEFAULT '{}',
    cron_expression    VARCHAR(100),
    notify_plan        JSONB NOT NULL DEFAULT '{}',
    state              VARCHAR(20) NOT NULL DEFAULT 'scheduled' CHECK (state IN ('scheduled','active','completed','cancelled')),
    created_by         UUID REFERENCES users(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (ends_at > starts_at)
);
CREATE INDEX idx_maintenance_windows_active
    ON maintenance_windows (starts_at, ends_at)
    WHERE state IN ('scheduled','active');
CREATE INDEX idx_maintenance_windows_tenant
    ON maintenance_windows (tenant_id)
    WHERE tenant_id IS NOT NULL;

-- RLS: maintenance_windows scoped by tenant_id (NULL = global, visible to super_admin only).
ALTER TABLE maintenance_windows ENABLE ROW LEVEL SECURITY;
ALTER TABLE maintenance_windows FORCE ROW LEVEL SECURITY;
CREATE POLICY maintenance_windows_tenant_isolation ON maintenance_windows
    USING (
        tenant_id IS NULL
        OR tenant_id = current_setting('app.current_tenant', true)::uuid
    );

-- kill_switches is GLOBAL — no RLS (super_admin only via router).
```

```sql
-- migrations/20260416000001_admin_compliance.down.sql
DROP TABLE IF EXISTS maintenance_windows;
DROP TABLE IF EXISTS kill_switches;
```

### Screen Mockups

All screens follow `docs/screens/_patterns.md` Dark Neon pattern.

#### SCR-140 — Multi-Tenant Resource Dashboard (`/admin/tenants`)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ Multi-Tenant Resources                          [Sort ▼] [Export]        │
├──────────────────────────────────────────────────────────────────────────┤
│ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐     │
│ │ ACME Corp    │ │ Orbit IoT    │ │ TelKart      │ │ FleetOps     │     │
│ │ ● active     │ │ ● active     │ │ ● degraded   │ │ ● active     │     │
│ │              │ │              │ │              │ │              │     │
│ │ SIMs  12.4k  │ │ SIMs   3.1k  │ │ SIMs  41.2k  │ │ SIMs  8.8k   │     │
│ │ ▁▂▃▄▅▆▇█     │ │ ▁▁▂▃▂▃▂▃     │ │ ▆▇█▇▆▇▆▇     │ │ ▃▄▅▄▅▆▅▆     │     │
│ │ API   12 r/s │ │ API    3 r/s │ │ API  210 r/s │ │ API  45 r/s  │     │
│ │ Sess    142  │ │ Sess    22   │ │ Sess  1,450  │ │ Sess    280  │     │
│ │ CDR   1.2 TB │ │ CDR  120 GB  │ │ CDR  15.2 TB │ │ CDR  3.1 TB  │     │
│ │ Stor  440 GB │ │ Stor   42 GB │ │ Stor  1.2 TB │ │ Stor  280 GB │     │
│ └──────────────┘ └──────────────┘ └──────────────┘ └──────────────┘     │
│ (click a card → /admin/quotas?tenant=acme)                               │
└──────────────────────────────────────────────────────────────────────────┘
```

#### SCR-141 — Tenant Quota Breakdown (`/admin/quotas`)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ Tenant Quotas                              [Search ⌘K]  [Edit Limits]   │
├──────────────────────────────────────────────────────────────────────────┤
│ ACME Corp                                                    [Edit ▶]   │
│  SIMs        12,412 / 50,000   ████░░░░░░  24.8%      [green]           │
│  APNs        18 / 20           ████████░░  90.0%      [yellow]  ⚠       │
│  Users       42 / 50           ████████░░  84.0%      [yellow]          │
│  API Keys    19 / 20           █████████░  95.0%      [red]    ⚠ banner │
│                                                                          │
│ ⚠ Approaching Limit: API Keys at 95%. Contact billing to raise cap.     │
└──────────────────────────────────────────────────────────────────────────┘
```

#### SCR-142 — Cost per Tenant (`/admin/cost`)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ Cost per Tenant                              [Month ▼]  [Alerts ⚙]      │
├──────────────────────────────────────────────────────────────────────────┤
│ ┌ Tenant ┬ Operator ┬ APN ┬ RAT ┬ Total ┬ Trend (6m) ┐                  │
│ │ ACME   │ TCell    │ iot │ LTE │ ₺4.2k │ ▂▃▄▅▆▇█   │                  │
│ │ Orbit  │ VF       │ nbs │ NB  │ ₺1.1k │ ▁▁▂▂▃▃▂   │                  │
│ └────────┴──────────┴─────┴─────┴───────┴────────────┘                  │
│                                                                          │
│ Top 10 Cost Contributors (SIMs)                                          │
│ 1. 8990...123   ACME   ₺420                                              │
│ …                                                                        │
└──────────────────────────────────────────────────────────────────────────┘
```

#### SCR-143 — Security Event Feed (`/admin/security-events`)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ Security Events (LIVE)   [Tenant ▼] [User ▼] [Event ▼] [Time ▼] [Clear]│
├──────────────────────────────────────────────────────────────────────────┤
│ ⏱ 14:32:12  🔴 HIGH   auth.login_failed   user@acme.com  192.0.2.1 [→]  │
│ ⏱ 14:31:45  🟡 MED    auth.2fa_failed     bora@orbit     10.0.0.5  [→]  │
│ ⏱ 14:30:00  🔴 HIGH   apikey.abuse        key_*a1b2      203.0.0.9 [→]  │
└──────────────────────────────────────────────────────────────────────────┘
```

#### SCR-144 — Active User Sessions (`/admin/sessions`)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ Active Sessions                                    [Revoke All by User] │
├──────────────────────────────────────────────────────────────────────────┤
│ User          │ IP           │ Device      │ Location │ Login    │ Idle │
│ alice@acme    │ 1.2.3.4      │ Chrome/Mac  │ TR/IST   │ 12:00    │ 2m   │
│ bob@orbit     │ 10.1.2.3     │ Firefox/Win │ —        │ 09:15    │ 3h   │
│                                              [Force Logout]  [Kill All] │
└──────────────────────────────────────────────────────────────────────────┘
```

#### SCR-145 — API Usage / Rate Limit (`/admin/api-usage`)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ API Usage                [1h] [24h] [7d]                    [Export ⬇]  │
├──────────────────────────────────────────────────────────────────────────┤
│ Key       │ req/s│ Limit │ Consumed │ Err % │ Top Endpoint          │ ⚙ │
│ pk_*acme  │ 12.4 │ 100   │ ██░░ 12% │ 0.5%  │ GET /sims             │ ⋮ │
│ pk_*orb   │ 210  │ 200   │ ██████ 🔴│ 2.1%  │ POST /sessions 🚨spike│ ⋮ │
│                                                                          │
│ Actions: [Suspend Key] [Change Rate Limit] [View History]                │
└──────────────────────────────────────────────────────────────────────────┘
```

#### SCR-146 — DSAR Queue (`/admin/dsar`)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ DSAR Queue (GDPR/KVKK)                      [Status ▼]  [New Request]   │
├──────────────────────────────────────────────────────────────────────────┤
│ User           │ Type   │ Status    │ SLA │ Assignee │ Created    │ ⋮   │
│ alice@acme     │ Export │ received  │  2d │ —        │ 2026-04-11 │ ⋮   │
│ bob@orbit      │ Erase  │ process.. │ 25d │ boratop  │ 2026-03-19 │ ⋮   │
│                                               [Generate Response]       │
└──────────────────────────────────────────────────────────────────────────┘
```

#### SCR-147 — Compliance Posture (`/admin/compliance`)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ Compliance Posture                                       [Export PDF ⬇] │
├──────────────────────────────────────────────────────────────────────────┤
│ ┌───────┐ ┌───────┐ ┌───────┐                                            │
│ │ KVKK  │ │ GDPR  │ │ BTK   │                                            │
│ │ 94 %  │ │ 88 %  │ │ 72 %  │                                            │
│ └───────┘ └───────┘ └───────┘                                            │
│                                                                          │
│ Checklist                                                                │
│  ✅ Data retention policy set (365d)                                    │
│  ✅ Audit hash chain verified                                           │
│  ⚠  DSAR SLA at risk (1 request > 25d)                                  │
│  ❌ BTK report not generated in last 30d                                │
└──────────────────────────────────────────────────────────────────────────┘
```

#### SCR-148 — Data Purge History (`/admin/purge-history`)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ Data Purge History                               [Dry-Run]  [Run Now]   │
├──────────────────────────────────────────────────────────────────────────┤
│ Date         │ Tenant │ Pseudon. │ Deleted │ Policy     │ Initiator     │
│ 2026-04-12   │ ACME   │    1,204 │      42 │ 365d       │ cron          │
│ 2026-04-11   │ Orbit  │      310 │      18 │ DSAR       │ manual/bora   │
└──────────────────────────────────────────────────────────────────────────┘
```

#### SCR-149 — Kill Switch / Degrade Mode (`/admin/kill-switches`)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ Kill Switches                                        [Runbook Link ↗]   │
├──────────────────────────────────────────────────────────────────────────┤
│  RADIUS Auth                                          [ OFF ]           │
│  New Session Creation                                 [ OFF ]           │
│  Bulk Operations                                      [ ON  ] ● danger  │
│      Reason: "Incident INC-482 — bulk policy bug"                       │
│      Toggled by: boratop at 12:05                                       │
│  Read-Only Mode                                       [ OFF ]           │
│  External Notifications                               [ OFF ]           │
│                                                                          │
│  Toggling ON requires: confirmation dialog + reason field + type        │
│  "CONFIRM" to arm.                                                       │
└──────────────────────────────────────────────────────────────────────────┘
```

#### SCR-150 — Maintenance Windows (`/admin/maintenance`)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ Maintenance Windows                                      [+ Schedule]   │
├──────────────────────────────────────────────────────────────────────────┤
│ Active now:                                                              │
│  ▶ DB migration — 14:00–16:00 UTC — tenants: ALL — services: portal,ws │
│                                                                          │
│ Scheduled:                                                               │
│  ◷ TCell adapter upgrade — 2026-04-15 02:00–04:00 UTC — services: aaa  │
│                                                                          │
│ History (30d): 12 completed, 1 cancelled                                │
│                                                                          │
│ Recurring: [Nightly backup at 02:00] [Weekly rollup Sun 03:00]          │
└──────────────────────────────────────────────────────────────────────────┘
```

#### SCR-151 — Notification Delivery Status (`/admin/delivery`)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ Delivery Status                         [1h] [24h] [7d]      [Refresh]  │
├──────────────────────────────────────────────────────────────────────────┤
│ Channel │ Success│ Failure│ Retry│ Last Delivery   │ p95 (ms) │ Health  │
│ email   │ 99.2%  │ 0.8%   │   0  │ 12:04           │ 412      │ 🟢      │
│ SMS     │ 97.1%  │ 2.9%   │   3  │ 11:58           │ 1,240    │ 🟡      │
│ webhook │ 88.4%  │ 11.6%  │  42  │ 12:05           │ 2,100    │ 🔴      │
│ telegram│ 100%   │ 0%     │   0  │ 12:02           │ 98       │ 🟢      │
│ in-app  │ 100%   │ 0%     │   0  │ 12:05           │  12      │ 🟢      │
│                                                                          │
│ Failed Deliveries (42)  [Retry All]                                     │
│  • webhook id=abc event=sim.suspended status=503 [Retry]                │
└──────────────────────────────────────────────────────────────────────────┘
```

### Design Token Map

> Per `docs/FRONTEND.md`. Tailwind tokens configured in `web/tailwind.config.*`.
> NEVER use raw hex or arbitrary values.

#### Color Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page background | `bg-bg-primary` | `bg-[#06060B]`, `bg-black` |
| Card surface | `bg-bg-surface` | `bg-[#0C0C14]`, `bg-gray-900` |
| Elevated (dropdown/modal) | `bg-bg-elevated` | `bg-[#12121C]` |
| Hover | `bg-bg-hover` | `bg-gray-800` |
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-white` |
| Secondary text | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-400` |
| Tertiary / placeholder | `text-text-tertiary` | `text-[#4A4A65]` |
| Border | `border-border` / `border-border-subtle` | `border-gray-700` |
| Accent (CTA/link) | `text-accent`, `bg-accent`, `border-accent` | `text-[#00D4FF]`, `text-cyan-400` |
| Accent dim | `bg-accent-dim` | any hardcoded rgba |
| Success | `text-success`, `bg-success-dim` | `text-green-400` |
| Warning | `text-warning`, `bg-warning-dim` | `text-yellow-500` |
| Danger | `text-danger`, `bg-danger-dim` | `text-red-500` |
| Info | `text-info` | `text-blue-400` |
| Purple secondary | `text-purple` | `text-purple-500` |

#### Typography
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page title | `text-[16px] font-semibold text-text-primary` | `text-2xl` |
| Section label (uppercase) | `text-[10px] uppercase tracking-[1px] text-text-tertiary font-medium` | `text-xs uppercase` |
| Table body | `text-[13px] text-text-primary` | `text-sm` |
| Mono data (ICCID, IP) | `font-mono text-[12px]` | `font-mono text-xs` |
| Metric value (card) | `font-mono text-[28px] font-bold text-text-primary` | `text-3xl` |
| Form label | `text-xs text-text-secondary` | `text-sm` |

#### Spacing & Elevation
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Card shadow | `shadow-[var(--shadow-card)]` | `shadow-md` |
| Card radius | `rounded-xl` (10px — matches `--radius-md`) | `rounded-md` |
| Section padding | `p-4` / `p-6` (aligned with 16px/24px) | `p-[20px]` |
| Metric grid gap | `gap-4` | `gap-5` |
| Page content padding | `p-6` | `p-[24px]` |

#### Existing Components to REUSE
| Component | Path | Use For |
|-----------|------|---------|
| `Card` | `web/src/components/ui/card.tsx` | ALL panels |
| `Button` | `web/src/components/ui/button.tsx` | ALL buttons |
| `Badge` | `web/src/components/ui/badge.tsx` | severity chips, status |
| `Input` | `web/src/components/ui/input.tsx` | ALL form inputs |
| `Select` | `web/src/components/ui/select.tsx` | ALL dropdowns |
| `Table*` | `web/src/components/ui/table.tsx` | ALL tables |
| `Skeleton` | `web/src/components/ui/skeleton.tsx` | loading state |
| `Sparkline` | `web/src/components/ui/sparkline.tsx` | tenant cards |
| `SlidePanel` | `web/src/components/ui/slide-panel.tsx` | create/edit drawers |
| `Sheet` | `web/src/components/ui/sheet.tsx` | detail side panels |
| `Dialog` | `web/src/components/ui/dialog.tsx` | confirmations (kill switch) |
| `Dropdown` | `web/src/components/ui/dropdown-menu.tsx` | row action ⋮ |
| `Tooltip` | `web/src/components/ui/tooltip.tsx` | hover hints |
| `Tabs` | `web/src/components/ui/tabs.tsx` | compliance checklist tabs |
| `Spinner` | `web/src/components/ui/spinner.tsx` | pending |

**RULE:** No raw `<input>`, `<button>`, `<table>`, no hex colors, no hardcoded px beyond tokens.

## Prerequisites
- [x] STORY-068 DONE — tenants.max_api_keys column, user_sessions FK, revoke-sessions, audit hash chain.
- [x] STORY-069 DONE — webhook_deliveries, scheduled_reports, sms_outbound, notifications table, data_portability job.

## Story-Specific Compliance Rules
- **API envelope:** All new admin endpoints MUST use `apierr.WriteSuccess` / `WriteList`.
- **RBAC:** 12 of 13 endpoints require `super_admin`; SCR-141 quotas + SCR-143 security events + SCR-146 DSAR queue also allow `tenant_admin` scoped to own tenant. AC-13 sidebar must conditionally render entries per role.
- **Audit:** Every mutation (kill-switch toggle, maintenance window create/delete, force-logout, rate-limit change, purge run) emits `auditor.CreateEntry`.
- **Tenant isolation:** `maintenance_windows` has RLS on `tenant_id` (NULL = global); `kill_switches` is global (super_admin-only, no RLS).
- **UI tokens:** zero hex, zero hardcoded px; dark-first.
- **Cursor pagination:** sessions/active, dsar/queue, purge-history, api-keys/usage — per ADR-001 + architecture conventions.
- **Confirmation dialogs:** Kill switch enable requires `type-to-confirm "CONFIRM"` + reason text; force-logout requires Dialog.
- **ADR-002 (Postgres):** RLS preserved via `app.current_tenant` setting (see DEV-212).
- **Kill-switch enforcement:** when toggling `radius_auth` ON, RADIUS Access-Request → Access-Reject with audit `killswitch.denied`; `session_create` ON, session create handler returns 503 `SERVICE_DEGRADED`; `bulk_operations` ON, bulk endpoints return 503; `read_only_mode` ON, all non-GET outside `/auth/*` return 503; `external_notifications` ON, notification dispatcher no-ops and emits metric `notifications_suppressed_total`.

## Bug Pattern Warnings
- No matching bug patterns. (PAT-001/002/003 do not intersect this story's scope — no BR tests modified, no IP parsing work, no EAP-MAC.)

## Tech Debt (from ROUTEMAP)
- No tech debt items for this story. (D-001, D-002 target STORY-077; D-003 target STORY-062; D-004/D-005 resolved.)

## Mock Retirement
- No `web/src/mocks/` directory exists at repo root. No mock retirement for this story.

## Tasks

> Amil dispatches each task to a fresh Developer subagent. Each task lists `Context refs` — Amil extracts those sections from this plan only.

### Wave 1 — DB + core BE scaffolding (parallelizable T1 alone first, then T2+T3 parallel)

### Task 1: Migration — kill_switches + maintenance_windows tables
- **Files:** Create `migrations/20260416000001_admin_compliance.up.sql`, `migrations/20260416000001_admin_compliance.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260413000001_story_069_schema.up.sql` — follow same structure (header comment block, CREATE TABLE IF NOT EXISTS, indexes, RLS enable + policy using `app.current_tenant`).
- **Context refs:** "Database Schema"
- **What:** Create both tables exactly as in Database Schema section. Seed 5 kill_switches rows. Add index + RLS on maintenance_windows. Down migration drops both.
- **Verify:** `go run ./cmd/argus migrate up` + `migrate down` round-trip clean. Psql: `SELECT count(*) FROM kill_switches;` returns 5.

### Task 2: killswitch service + store
- **Files:** Create `internal/store/killswitch.go`, `internal/killswitch/service.go`, `internal/killswitch/service_test.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/apikey.go` (store CRUD pattern) and `internal/notification/dispatcher.go` (service with cache/TTL pattern).
- **Context refs:** "Architecture Context > Components Involved", "Database Schema", "Data Flow > Kill Switch"
- **What:** `KillSwitchStore.List(ctx) []KillSwitch`, `Toggle(ctx, key, enabled, reason, actorUserID) (*KillSwitch, error)`, `GetByKey(ctx, key)`. Service: `killswitch.Service{cache: map[string]bool, ttl: 15s}`, `IsEnabled(key string) bool`, `Reload(ctx)`, `GetAll()`. Unit tests: toggle writes audit (via injected auditor stub), cache refresh after TTL, IsEnabled handles unknown key → false.
- **Verify:** `go test ./internal/store/... ./internal/killswitch/... -run KillSwitch` green.

### Task 3: maintenance_window store + helper
- **Files:** Create `internal/store/maintenance_window.go`, `internal/store/maintenance_window_test.go`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `internal/store/roaming_agreement.go` (similar tenant-scoped CRUD + state column + indexes).
- **Context refs:** "Database Schema"
- **What:** CRUD (`Create`, `Get`, `List(tenantID, activeOnly)`, `Delete`). `IsActiveFor(ctx, tenantID|nil, service string) (*MaintenanceWindow, error)` returns nearest matching window where NOW() BETWEEN starts_at AND ends_at and state IN ('scheduled','active'). Unit tests: CRUD + active lookup.
- **Verify:** `go test ./internal/store/... -run Maintenance` green.

### Wave 2 — Admin BE endpoints (parallel T4/T5/T6)

### Task 4: Admin handlers batch A — kill_switches + maintenance + delivery_status
- **Files:** Create `internal/api/admin/kill_switch.go`, `maintenance_window.go`, `delivery_status.go`, `handler.go` (shared struct + NewHandler), `handler_test.go`
- **Depends on:** Task 2, Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/webhooks/handler.go` — same RBAC-gated CRUD + delivery aggregation style.
- **Context refs:** "Data Flow", "API Specifications" (kill-switches, maintenance-windows, delivery/status), "Story-Specific Compliance Rules"
- **What:** Handlers for `GET/PATCH /admin/kill-switches`, `GET/POST/DELETE /admin/maintenance-windows`, `GET /admin/delivery/status`. Kill-switch PATCH emits audit `killswitch.toggled`; maintenance POST emits `maintenance.scheduled`, DELETE emits `maintenance.cancelled`. Delivery status aggregates `webhook_deliveries` GROUP BY final_state + latency pct, reads `notifications` + `sms_outbound` counts. Unit tests: 403 non-super_admin; 422 kill-switch enable without reason; delivery_status math.
- **Verify:** `go test ./internal/api/admin/... -run KillSwitch|Maintenance|Delivery` green.

### Task 5: Admin handlers batch B — tenants/resources + cost/by-tenant + purge-history
- **Files:** Create `internal/api/admin/tenant_resources.go`, `cost_by_tenant.go`, `purge_history.go`, `handler_batch_b_test.go`
- **Depends on:** —  (handler.go exists after T4; if T5 runs before T4, T5 creates handler.go instead — Amil serializes to T4 first.)
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/system/capacity_handler.go` (aggregation read-only) and `internal/api/analytics/handler.go` (cost aggregation pattern).
- **Context refs:** "Data Flow > Multi-Tenant Resources / Cost per Tenant / Data Purge History", "API Specifications"
- **What:** Tenant resources: fan-out per tenant via existing `TenantStore.GetTenantStats` + Redis rate-limit sum + existing `analytics.metrics.CDRVolumeByTenant`. Cost: wrap existing per-tenant cost + trend query. Purge history: SELECT from `sim_state_history WHERE to_state='purged'` joined with `compliance_retention_runs` if present, cursor-paginated. Unit tests: empty tenant list, sort param, pagination.
- **Verify:** `go test ./internal/api/admin/... -run TenantResources|CostByTenant|PurgeHistory` green.

### Task 6: Admin handlers batch C — sessions/active + api-keys/usage + dsar/queue + audit actions filter
- **Files:** Create `internal/api/admin/sessions_global.go`, `api_usage.go`, `dsar_queue.go`, `handler_batch_c_test.go`; Modify `internal/api/audit/handler.go` (add `actions` CSV filter — extends `ListAuditParams`).
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/auth/handler.go` `ListSessions` + `internal/gateway/ratelimit.go` (Redis key pattern).
- **Context refs:** "Data Flow > Active User Sessions / API Usage / DSAR Queue / Security Event Feed", "API Specifications"
- **What:** Sessions/active reads user_sessions joined users; tenant scope enforced. api-keys/usage: iterate api_keys, sum Redis `ratelimit:apikey:{hash}:min` for window, compute EMA spike flag. dsar/queue: jobs WHERE type IN ('data_portability_export','kvkk_purge_daily','sim_erasure') with status mapping. audit `actions` filter: add `Actions []string` to `ListAuditParams`, SQL `AND action = ANY($n)`. Unit tests: tenant_admin scope-lock, window math, actions filter SQL.
- **Verify:** `go test ./internal/api/admin/... ./internal/api/audit/... -run SessionsGlobal|APIKeysUsage|DSARQueue|ActionsFilter` green.

### Wave 3 — Kill-switch enforcement hooks (medium; parallel with Wave 4 FE)

### Task 7: Kill-switch enforcement wiring
- **Files:** Modify `internal/aaa/radius/handler.go` (or nearest Access-Request entry); `internal/api/sim/bulk_*.go` (bulk endpoints — 3 files); `internal/notification/dispatcher.go`; `internal/gateway/router.go` (new `KillSwitchMiddleware` for `read_only_mode`).
- **Depends on:** Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/gateway/ratelimit.go` (middleware pattern) and `internal/aaa/radius/handler.go` (where Access-Request is dispatched).
- **Context refs:** "Data Flow > Kill Switch", "Story-Specific Compliance Rules" (bullet kill-switch enforcement)
- **What:** Each enforcement point calls `killswitch.IsEnabled("key")`; if true, return 503 `SERVICE_DEGRADED` (HTTP) or Access-Reject with audit (AAA) or silently suppress + metric (notifications). Add `KillSwitchMiddleware("read_only_mode", allowlistPrefixes={"/api/v1/auth/","/api/v1/admin/kill-switches"})` at router root (excludes GET). Metrics: `killswitch_denied_total{key=…}` Prometheus counter. Unit tests: bulk op returns 503 when flag ON; notifications suppressed when flag ON.
- **Verify:** `go test ./internal/aaa/... ./internal/api/sim/... ./internal/notification/... ./internal/gateway/... -run KillSwitch` green.

### Task 8: main.go wiring — stores + service + handlers + middleware
- **Files:** Modify `cmd/argus/main.go`, `internal/gateway/router.go` (register /api/v1/admin/* routes), `internal/gateway/types.go` (or wherever deps struct is).
- **Depends on:** Task 4, Task 5, Task 6, Task 7
- **Complexity:** medium
- **Pattern ref:** Read recent `cmd/argus/main.go` diff in STORY-069 commit `fac87b0` (same wiring style).
- **Context refs:** "Architecture Context > Components Involved", "API Specifications"
- **What:** Instantiate KillSwitchStore, MaintenanceWindowStore, killswitch.Service (start reload loop), AdminHandler. Mount 12 new routes under `super_admin` group (13th route — quotas — reuses existing tenant endpoints). Tenant-scoped subset (SCR-141 quota, SCR-143 audit, SCR-146 dsar) additionally under `tenant_admin` group for self-tenant. Attach `KillSwitchMiddleware` at router root.
- **Verify:** `go build ./...` clean; `curl -H "Authorization: Bearer $TOKEN" /api/v1/admin/kill-switches` returns 5 rows.

### Wave 4 — FE pages (parallelizable T9..T16; T9 blocks T17 for sidebar/router)

### Task 9: FE types + hooks (use-admin.ts, types/admin.ts)
- **Files:** Create `web/src/types/admin.ts`, `web/src/hooks/use-admin.ts`
- **Depends on:** Task 8 (endpoints live)
- **Complexity:** low
- **Pattern ref:** Read `web/src/hooks/use-ops.ts` and `web/src/types/ops.ts`.
- **Context refs:** "API Specifications"
- **What:** TS interfaces matching every response above. React-Query hooks: `useTenantResources`, `useCostByTenant`, `useSecurityEvents(filters)`, `useActiveSessions(filters)`, `useAPIKeyUsage(window)`, `useDSARQueue(status)`, `usePurgeHistory(tenant)`, `useKillSwitches`, `useToggleKillSwitch`, `useMaintenanceWindows`, `useCreateMaintenanceWindow`, `useDeleteMaintenanceWindow`, `useDeliveryStatus(window)`, `useForceLogoutSession`.
- **Verify:** `pnpm -C web tsc --noEmit` clean.

### Task 10: SCR-140 Multi-Tenant Resources page
- **Files:** Create `web/src/pages/admin/tenant-resources.tsx`
- **Depends on:** Task 9
- **Complexity:** low
- **Pattern ref:** Read `web/src/pages/dashboard/index.tsx` (grid of metric cards + sparkline).
- **Context refs:** "Screen Mockups > SCR-140", "Design Token Map", "Existing Components to REUSE"
- **What:** Grid of tenant cards, Sparkline for sim_count_7d + api_rps_7d. Sort dropdown. Click card → navigate to `/admin/quotas?tenant=<id>`. Empty/loading/error states per `_patterns.md`.
- **Tokens:** Use ONLY Design Token Map classes.
- **Components:** `Card`, `Sparkline`, `Select`, `Skeleton`.
- **Verify:** `grep -E '#[0-9a-fA-F]{3,6}|\\[\\d+px\\]' web/src/pages/admin/tenant-resources.tsx` → zero matches.

### Task 11: SCR-141 Quota Breakdown page (+ modify SCR-121 tenants link)
- **Files:** Create `web/src/pages/admin/quotas.tsx`; Modify `web/src/pages/system/tenants.tsx` (add quota column → link to quotas page).
- **Depends on:** Task 9
- **Complexity:** low
- **Pattern ref:** Read `web/src/pages/system/tenants.tsx` existing file.
- **Context refs:** "Screen Mockups > SCR-141", "Design Token Map"
- **What:** Progress bars per (sims/apns/users/api_keys). Color: <80 success-dim, <95 warning-dim, >=95 danger-dim + banner. Edit Limits button opens existing SlidePanel (reuse code from tenants.tsx).
- **Tokens:** Use ONLY Design Token Map classes.
- **Verify:** `pnpm -C web tsc --noEmit` clean; hex/px grep zero.

### Task 12: SCR-142 Cost per Tenant + SCR-147 Compliance Posture
- **Files:** Create `web/src/pages/admin/cost.tsx`, `web/src/pages/admin/compliance.tsx`
- **Depends on:** Task 9
- **Complexity:** low
- **Pattern ref:** Read `web/src/pages/dashboard/analytics-cost.tsx` (cost table + trend) and `web/src/pages/settings/reliability.tsx` (score cards).
- **Context refs:** "Screen Mockups > SCR-142, SCR-147", "Data Flow > Compliance Posture", "Design Token Map"
- **What:** Cost page: table + Sparkline trend + Top10 list + monthly alert config. Compliance page: 3 score cards (KVKK/GDPR/BTK) computed from `compliance/dashboard`; checklist with ✅/⚠/❌; export PDF button (reuse existing BTK PDF).
- **Verify:** hex/px grep zero; tsc clean.

### Task 13: SCR-143 Security Events + SCR-144 Active Sessions
- **Files:** Create `web/src/pages/admin/security-events.tsx`, `web/src/pages/admin/sessions-global.tsx`
- **Depends on:** Task 9
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/audit/index.tsx` (events list + filter bar) and `web/src/pages/settings/sessions.tsx` (session list + revoke).
- **Context refs:** "Screen Mockups > SCR-143, SCR-144", "Data Flow > Security Event Feed / Active User Sessions", "Design Token Map"
- **What:** Security events: filter bar (tenant/user/event/time) + severity Badge + WS subscribe to `audit.created`. Sessions: table w/ IP, device-parsed UA, geo (or — when null), idle seconds, [Force Logout] per row + [Revoke All] per user Dialog.
- **Verify:** hex/px grep zero; tsc clean.

### Task 14: SCR-145 API Usage + SCR-146 DSAR Queue
- **Files:** Create `web/src/pages/admin/api-usage.tsx`, `web/src/pages/admin/dsar.tsx`
- **Depends on:** Task 9
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/settings/api-keys.tsx` (rate-limit UI) and `web/src/pages/jobs/index.tsx` (job queue list).
- **Context refs:** "Screen Mockups > SCR-145, SCR-146", "Data Flow > API Usage / DSAR Queue", "Design Token Map"
- **What:** API Usage: window selector, consumption bar, spike badge, row action → suspend key (existing API) / change rate_limit. DSAR: status filter, SLA timer color (green<15d, yellow<25d, red>25d), Generate Response button (existing endpoint).
- **Verify:** hex/px grep zero; tsc clean.

### Task 15: SCR-148 Purge History + SCR-151 Delivery Status
- **Files:** Create `web/src/pages/admin/purge-history.tsx`, `web/src/pages/admin/delivery.tsx`
- **Depends on:** Task 9
- **Complexity:** low
- **Pattern ref:** Read `web/src/pages/jobs/index.tsx` (job history table).
- **Context refs:** "Screen Mockups > SCR-148, SCR-151", "Design Token Map"
- **What:** Purge history table + [Dry-Run] + [Run Now] Dialog. Delivery status: per-channel card grid + failed-deliveries sub-table + [Retry All] (existing `/webhooks/:id/deliveries/:delivery_id/retry` fan-out).
- **Verify:** hex/px grep zero; tsc clean.

### Task 16: SCR-149 Kill Switches + SCR-150 Maintenance Windows
- **Files:** Create `web/src/pages/admin/kill-switches.tsx`, `web/src/pages/admin/maintenance.tsx`
- **Depends on:** Task 9
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/system/tenants.tsx` (CRUD + SlidePanel + Dialog).
- **Context refs:** "Screen Mockups > SCR-149, SCR-150", "Data Flow > Kill Switch / Maintenance Windows", "Story-Specific Compliance Rules" (confirmation dialog rule)
- **What:** Kill Switches: toggle rows + Confirm Dialog requiring `reason` + "CONFIRM" type-to-confirm. Maintenance: calendar-ish list (active now / scheduled / history tabs) + New SlidePanel form (title, description, starts_at, ends_at, affected_services multi-select, cron_expression optional, notify_plan JSON-lite fields).
- **Verify:** hex/px grep zero; tsc clean.

### Task 17: Router + Sidebar wiring (AC-13)
- **Files:** Modify `web/src/router.tsx`, `web/src/components/layout/sidebar.tsx`
- **Depends on:** Task 10..Task 16
- **Complexity:** low
- **Pattern ref:** Read existing router.tsx + sidebar.tsx.
- **Context refs:** "Architecture Context > Components Involved", "Screen Mockups"
- **What:** Register 12 lazy routes under `/admin/*`. Add "Admin" sidebar section, super_admin-only render (`useAuthStore()`); tenant_admin sees subset: Quotas, Security Events (own), DSAR Queue (own).
- **Verify:** `pnpm -C web build` succeeds; navigate to each route in dev.

### Wave 5 — Tests + docs

### Task 18: E2E + integration tests
- **Files:** Create `internal/api/admin/e2e_test.go` (Go integration: tenant→seed→GET /admin/* round-trip); `web/src/__tests__/admin.smoke.test.tsx` (vitest smoke on 2 pages).
- **Depends on:** Task 17
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/compliance/data_portability_test.go` and `web/src/__tests__/` latest file.
- **Context refs:** "Acceptance Criteria Mapping", "Test Scenarios"
- **What:** E2E scenarios from story: kill-switch toggle blocks bulk op; DSAR enqueue; maintenance window create/list; force-logout invalidates session; 5 failed logins → security event feed shows 5 HIGH.
- **Verify:** full suite: `go test ./... -count=1` green; `pnpm -C web test`.

### Task 19: Documentation update
- **Files:** Modify `docs/SCREENS.md`, `docs/architecture/api/_index.md`, `docs/USERTEST.md`, `docs/ROUTEMAP.md`, `docs/brainstorming/decisions.md`, `docs/CONFIG.md`, `docs/ARCHITECTURE.md`.
- **Depends on:** Task 18
- **Complexity:** low
- **Pattern ref:** Read STORY-069 close-out diff in commit `fac87b0` (same docs touched).
- **Context refs:** (none — docs are authored from the plan itself)
- **What:** Add 12 SCR rows to SCREENS.md. Add 14 new API rows under new "Admin" section in api/_index.md. Add USERTEST scenarios (8). ROUTEMAP: mark STORY-073 DONE. decisions.md: capture DEV-214 (kill-switch 15s TTL cache choice), DEV-215 (maintenance_windows RLS policy). ARCHITECTURE.md: note TBL-25 kill_switches + TBL-26 maintenance_windows; SVC-10 extension.
- **Verify:** `grep -c 'SCR-15' docs/SCREENS.md` ≥ 12.

## Acceptance Criteria Mapping
| AC | Screen | Implemented In | Verified By |
|----|--------|---------------|-------------|
| AC-1 | SCR-140 | T5 (resources endpoint), T10 | T18 |
| AC-2 | SCR-141 | T11 | T18 |
| AC-3 | SCR-142 | T5 (cost endpoint), T12 | T18 |
| AC-4 | SCR-143 | T6 (audit actions filter), T13 | T18 |
| AC-5 | SCR-144 | T6 (sessions), T13 | T18 |
| AC-6 | SCR-145 | T6 (api-keys/usage), T14 | T18 |
| AC-7 | SCR-146 | T6 (dsar queue), T14 | T18 |
| AC-8 | SCR-147 | T12 (reuses `/compliance/dashboard`) | T18 |
| AC-9 | SCR-148 | T5 (purge-history), T15 | T18 |
| AC-10 | SCR-149 | T2, T4, T7 (enforcement), T16 | T18 |
| AC-11 | SCR-150 | T3, T4, T16 | T18 |
| AC-12 | SCR-151 | T4 (delivery status), T15 | T18 |
| AC-13 | sidebar | T17 | T18 |

## Test Scenarios (copy from story)
1. super_admin opens Multi-Tenant Resource → sees all tenants with SIM counts matching reality. (T18)
2. Tenant at 95% SIM quota → yellow progress bar + "Approaching Limit" banner. (T18)
3. 5 failed login attempts → Security Event Feed shows 5 entries with severity=HIGH. (T18)
4. Admin clicks "Force Logout" on active session → session invalidated, user redirected to login. (T18)
5. Create DSAR request → status "received" → trigger export → status "processing" → complete → "delivered". (T18)
6. Toggle "disable bulk operations" kill switch → bulk SIM suspend returns 503 SERVICE_DEGRADED. (T18)
7. Schedule maintenance window → banner appears for all users in affected tenant. (T18 — banner integration deferred to STORY-077 per scope; test asserts API state only.)

## Risks & Mitigations
- **R1: Kill-switch enforcement gaps.** Easy to miss entry points. → Mitigation: T7 lists every mutation point explicitly; T18 includes a regression matrix covering all 5 switches × 1 blocking scenario each.
- **R2: Cross-tenant data leak in admin endpoints.** → Mitigation: every admin handler asserts `callerRole == "super_admin"` OR explicitly scopes `tenantID = caller's tenant` (no trust of query param for tenant_admin).
- **R3: Redis rate-limit key scan on api-keys/usage may O(N) all keys.** → Mitigation: iterate known api_keys (fixed set, capped at tenants.max_api_keys × tenant count ≈ thousands), not `KEYS *`.
- **R4: Storage-used aggregation expensive.** → Mitigation: Use approximate `pg_total_relation_size('sims')` ÷ total sims × tenant sim count. Cache 5min in Redis.
- **R5: UA parsing library not present.** → Mitigation: T13 uses existing lightweight UA regex already present in `internal/api/auth/handler.go`'s ListSessions response (no new dep).
- **R6: Test flake on websocket-driven security event test.** → Mitigation: T18 uses in-process hub stub; no socket over network.

---

## Pre-Validation Self-Check (Planner)

- [x] Story effort L → 19 tasks (≥5), at least 1 high-complexity (T7), multiple medium.
- [x] Required sections present: Goal, Architecture Context, Tasks, Acceptance Criteria Mapping.
- [x] API specs embedded with methods, paths, request/response, status codes.
- [x] DB schema embedded with exact SQL and RLS, source noted ("new tables — ARCHITECTURE.md design").
- [x] Screen mockups embedded for all 12 SCRs.
- [x] Design Token Map populated — color/typo/spacing/elevation + Component Reuse table.
- [x] Every task has Pattern ref + Context refs pointing to existing plan sections.
- [x] Task granularity ≤3 files per task (T7 and T8 are 3–4; T19 is docs-only across doc files — acceptable).
- [x] Task ordering: DB → stores → handlers → wiring → FE → tests → docs.
- [x] Tenant-isolation + RBAC + audit + tokens rules explicit in Story-Specific Compliance Rules.
- [x] Bug Patterns: "No matching bug patterns."
- [x] Tech Debt: "No tech debt items for this story."
- [x] Mock retirement: "No mock retirement for this story."

Pre-Validation: **PASS**.
