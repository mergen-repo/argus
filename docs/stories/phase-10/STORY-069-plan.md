# Implementation Plan: STORY-069 — Onboarding, Reporting & Notification Completeness

## Goal
Deliver a production-grade onboarding wizard (end-to-end), scheduled + on-demand report generation in PDF/CSV/Excel, webhook delivery tracking with HMAC signatures, per-tenant locale-aware notification preferences, GDPR data portability export, KVKK auto-purge + IP grace release cron schedulers, and SMS gateway outbound endpoints — closing every Phase 10 audit gap listed in AC-1..AC-12 with zero deferral.

## Architecture Context

### Components Involved

Backend (extend existing packages, do NOT add new top-level services):
- **SVC-03 Core API** — new sub-packages
  - `internal/api/onboarding/` (NEW) — 4 endpoints, session state machine
  - `internal/api/reports/` (NEW) — 5 endpoints (generate, scheduled CRUD)
  - `internal/api/webhooks/` (NEW) — 4 endpoints (configs + deliveries)
  - `internal/api/sms/` (NEW) — 2 endpoints (send, history)
  - `internal/api/compliance/` (EXTEND) — add data portability endpoint
  - `internal/api/notification/` (EXTEND) — preferences matrix + templates
- **SVC-07 Analytics** — report generation engines
  - `internal/report/` (NEW) — `pdf.go`, `csv.go`, `excel.go` formatters + `types.go` report definitions
- **SVC-08 Notification** — locale-aware dispatch + HMAC + delivery tracking
  - `internal/notification/template_store.go` (NEW) — locale lookup
  - `internal/notification/webhook.go` (EXTEND) — HMAC header + delivery log
  - `internal/notification/delivery.go` (EXTEND) — retry scheduler, DLQ
- **SVC-09 Job Runner** — cron jobs + scheduled reports
  - `internal/job/scheduled_report.go` (NEW) — processor for `scheduled_report_run`
  - `internal/job/kvkk_purge.go` (NEW) — processor for `kvkk_purge_daily`
  - `internal/job/ip_grace_release.go` (NEW) — processor for `ip_grace_release`
  - `internal/job/data_portability.go` (NEW) — processor for `data_portability_export`
  - `internal/job/sms_gateway.go` (NEW) — async send processor for `sms_outbound`
  - Existing `internal/job/scheduler.go` — AddEntry() for the new 3 crons in wire-up
- **SVC-10 Audit** — already present, reused for `sms.sent`, `kvkk.purge.run`, `webhook.delivered`, `data_portability.requested`
- **Storage layer** — `internal/store/` new stores:
  - `onboarding_session_store.go`, `scheduled_report_store.go`, `webhook_config_store.go`,
    `webhook_delivery_store.go`, `notification_preference_store.go`, `notification_template_store.go`,
    `sms_outbound_store.go`
- **S3** — reuse `internal/storage/s3_uploader.go` (from STORY-063) for report artefacts + portability archive
- **PDF** — reuse `github.com/go-pdf/fpdf` (already in go.mod). Excel: add `github.com/xuri/excelize/v2`.

Frontend:
- `web/src/components/onboarding/wizard.tsx` — rewrite to hit real `/api/v1/onboarding/*` endpoints + resume via session id
- `web/src/pages/reports/index.tsx` — replace hardcoded arrays + setTimeout with real `/api/v1/reports/*` queries
- `web/src/pages/reports/scheduled.tsx` (NEW) — CRUD scheduled reports
- `web/src/pages/webhooks/index.tsx` (NEW) — webhook config + delivery log
- `web/src/pages/notifications/preferences.tsx` (NEW) — per-tenant preference matrix
- `web/src/pages/sms/index.tsx` (NEW) — SMS send form + history
- `web/src/pages/compliance/data-portability.tsx` (NEW) — request portability export

### Data Flow

#### AC-1 Onboarding Wizard
```
User → POST /api/v1/onboarding/start (tenant context)
  → onboarding_session_store.Create → returns {session_id, current_step: 1}
  → frontend stores session_id in localStorage for resume

User → POST /api/v1/onboarding/:id/step/:n with step payload
  → onboarding handler validates step n, executes side-effects atomically within tx:
     step 1 → UPDATE tenants SET name, contact_email, contact_phone, locale, settings
     step 2 → INSERT admin user (auth.CreateUser w/ role=tenant_admin)
     step 3 → INSERT operator_grants rows
     step 4 → INSERT apns row
     step 5 → enqueue bulk_sim_import job from CSV S3 object key
  → ON SUCCESS: UPDATE onboarding_sessions SET step_n_data=payload, current_step=n+1
  → ON FAIL: tx rollback, HTTP 422 with field errors; session state unchanged
User → POST /api/v1/onboarding/:id/complete
  → mark complete, assign default policy, fire welcome notification, send NATS onboarding.completed
```

#### AC-2/AC-3/AC-4 Reports
```
On-demand:
  POST /api/v1/reports/generate { report_type, format, filters }
    → if small CSV (<1MB) AND report_type in SMALL_SET: sync — build + return bytes
    → else: create job `scheduled_report_run` inline → returns { job_id, status: "queued" }
    → job processor: builds report via internal/report/{pdf,csv,excel}.go
    → uploads to S3, UPDATE jobs.result = {s3_key, signed_url_expires}
    → frontend polls GET /api/v1/jobs/:id, when done shows download button

Scheduled:
  POST /api/v1/reports/scheduled → INSERT scheduled_reports row, compute next_run_at from cron
  cron scheduler (every minute) scans scheduled_reports WHERE next_run_at <= NOW() AND state='active'
    → for each row, enqueue scheduled_report_run job with payload={scheduled_report_id}
    → UPDATE last_run_at=NOW(), next_run_at=NextAfter(schedule_cron, NOW())
  job processor runs report, uploads to S3, sends email to recipients[] with signed URL
```

#### AC-5/AC-6 Webhooks
```
Event occurs → notification.Service.Dispatch()
  → reads notification_preferences for (tenant_id, event_type) → get enabled channels
  → if webhook: for each webhook_config matching event_type:
    → build payload {event, tenant_id, timestamp, data}
    → sign: X-Argus-Signature: sha256=HMAC_SHA256(secret, raw_body)
    → X-Argus-Timestamp, X-Argus-Event headers
    → POST with 5s timeout
    → INSERT webhook_deliveries row with attempt_count=1, response_status, response_body (1KB truncated)
    → if !(200..299): schedule retry. next_retry_at = NOW + backoff(attempt)
         backoff: 30s, 2m, 10m, 30m, 60m (max 5 attempts, 1h window)
  → retry scheduler (cron every 1m) picks webhook_deliveries WHERE next_retry_at<=NOW AND final_state='retrying'
  → on attempt 5 failure: final_state='dead_letter', audit log, UI surfacing
```

#### AC-9 GDPR Data Portability
```
POST /api/v1/compliance/data-portability/:user_id
  → auth: caller must be self OR tenant_admin+
  → create job data_portability_export with payload {user_id, requester_user_id}
  → returns { job_id, status: "queued" }
job processor:
  → gather: users row (sanitized), tenant name, sims where owner_user_id=userID,
    sessions WHERE tenant_id=X AND user_id=userID AND created_at>NOW-90d,
    cdrs joined via sim where sim.owner_user_id=userID AND created_at within retention,
    audit_logs WHERE user_id=userID
  → build archive.zip containing data.json + summary.pdf
  → upload to S3: tenants/{tenant_id}/portability/{job_id}.zip
  → create signed URL 7d TTL
  → enqueue notification {user_id, template=data_portability_ready, data={url}}
  → audit log action=data_portability.exported
```

#### AC-10 KVKK auto-purge
```
cron_kvkk_purge @daily → enqueues job kvkk_purge_daily (tenant_id=nil, sweeps all)
processor:
  SELECT * FROM tenants → for each:
    retention := tenant_retention_config OR tenants.purge_retention_days
    pseudonymize cdrs, sessions, user_sessions, audit_logs.details WHERE created_at < NOW - retention
       (UPDATE SET msisdn=sha256(msisdn), imsi=sha256(imsi) or delete per classification)
    if --dry-run (from payload.dry_run=true): count-only, no writes
    audit log kvkk.purge.run per tenant with counts
```

#### AC-11 IP grace release
```
cron_ip_grace_release @hourly → enqueue ip_grace_release job
processor:
  SELECT ip.* FROM ip_addresses ip JOIN sims s ON s.id=ip.sim_id
  WHERE ip.released_at IS NULL AND ip.grace_expires_at < NOW() AND s.state='terminated'
  → UPDATE ip_addresses SET state='available', sim_id=NULL, grace_expires_at=NULL,
     released_at=NOW(), allocated_at=NULL
  → bus.Publish "ip.released" {ip_id, tenant_id, address}
  → metrics.argus_ip_grace_released_total.Inc()
```

#### AC-12 SMS gateway outbound
```
POST /api/v1/sms/send { sim_id, text, priority? }
  → rate-limit (redis token bucket keyed sms:tenant:{tenant_id} with RATE_LIMIT_SMS_PER_MINUTE)
  → INSERT sms_outbound row status='queued', provider_message_id=NULL
  → enqueue sms_outbound job with {sms_id}
  → return {message_id: sms.id, queued_at}
job processor:
  → look up sims.msisdn by sim_id
  → call notification.SMSGatewaySender.SendSMS(ctx, msisdn, text)
  → on success: UPDATE sms_outbound SET status='sent', provider_message_id=X, sent_at=NOW()
  → on failure: UPDATE status='failed', error_code=msg
  → audit log action=sms.sent
existing POST /api/v1/notifications/sms/status webhook (API-185):
  → UPDATE sms_outbound SET status='delivered', delivered_at=NOW()
    WHERE provider_message_id=payload.MessageSid (matches BOTH notifications AND sms_outbound)
GET /api/v1/sms/history?sim_id=X&status=Y&from=&to=&cursor= → cursor-paginated list
```

### API Specifications

All responses use standard envelope `{status, data, meta?, error?}`. All endpoints require JWT middleware unless noted.

**AC-1 Onboarding** (RBAC: tenant_admin+ for start/complete, authenticated for step)
- `POST /api/v1/onboarding/start`
  - Body: `{}` (session created from JWT context)
  - 201 `{status:"success", data:{session_id, current_step:1, steps_total:5}}`
- `POST /api/v1/onboarding/:id/step/:n` (n=1..5)
  - Body varies per step:
    - step 1: `{company_name, contact_email, contact_phone?, locale ("tr"|"en")}`
    - step 2: `{admin_email, admin_name, admin_password, totp_enabled?}`
    - step 3: `{operator_grants: [{operator_id, enabled, rat_types[]}]}`
    - step 4: `{apn_name, realm, ip_pool_cidr, auth_type}`
    - step 5: `{csv_s3_key}` (CSV previously uploaded via existing bulk import upload endpoint)
  - 200 `{status, data:{session_id, current_step:n+1, step_result}}`
  - 422 validation error (non-transactional — session state not advanced)
- `GET /api/v1/onboarding/:id` → `{session_id, current_step, data_by_step, complete}`
- `POST /api/v1/onboarding/:id/complete` → finalize, assign default policy, send welcome notification, 200

**AC-2/AC-3 Reports** (RBAC: analyst+)
- `POST /api/v1/reports/generate` — Body `{report_type, format, filters?}` → 202 `{job_id, status:"queued"}` OR 200 `{file_url}` (sync small)
- `GET /api/v1/reports/scheduled` — cursor paginated list
- `POST /api/v1/reports/scheduled` — Body `{report_type, schedule_cron, format, recipients[]}` → 201 row
- `PATCH /api/v1/reports/scheduled/:id` — update schedule/recipients/state (active|paused)
- `DELETE /api/v1/reports/scheduled/:id` → 204

Allowed `report_type` enum: `compliance_kvkk`, `compliance_gdpr`, `compliance_btk`, `sla_monthly`, `usage_summary`, `cost_analysis`, `audit_log_export`, `sim_inventory`.
Allowed `format` enum: `pdf`, `csv`, `xlsx`.

**AC-5/AC-6 Webhooks** (RBAC: tenant_admin+)
- `GET /api/v1/webhooks` — list webhook configs
- `POST /api/v1/webhooks` — Body `{url, secret, event_types[], enabled}` → 201
- `PATCH /api/v1/webhooks/:id` — update
- `DELETE /api/v1/webhooks/:id`
- `GET /api/v1/webhooks/:id/deliveries?status=&cursor=` — cursor paginated deliveries
- `POST /api/v1/webhooks/:id/deliveries/:delivery_id/retry` → manual retry (returns new delivery row)

**AC-7 Preferences** (RBAC: tenant_admin+)
- `GET /api/v1/notification-preferences` — returns matrix (rows: event_types × cols: channels)
- `PUT /api/v1/notification-preferences` — Body `[{event_type, channels[], severity_threshold, enabled}]` → bulk upsert
  (Note: extends existing `/api/v1/notification-configs` — choose: extend existing OR add this new route. **Decision:** add new `/notification-preferences` endpoint pair that writes to `notification_preferences` table; keep `/notification-configs` as alias for backward compat, eventually deprecate.)

**AC-8 Templates** (RBAC: superadmin for mutation, read for all authed)
- `GET /api/v1/notification-templates?event_type=&locale=` — list
- `PUT /api/v1/notification-templates/:event_type/:locale` — upsert `{subject, body_text, body_html}`

**AC-9 Data Portability** (RBAC: tenant_admin+ OR self)
- `POST /api/v1/compliance/data-portability/:user_id` → 202 `{job_id}`
- (Download occurs via signed URL emailed to user)

**AC-12 SMS** (RBAC: sim_manager+)
- `POST /api/v1/sms/send` — Body `{sim_id, text (max 480 chars / 3 concat SMS), priority? ("normal"|"high")}` → 202 `{message_id, queued_at}`
- `GET /api/v1/sms/history?sim_id=&from=&to=&status=&cursor=` → cursor paginated rows

Error responses (per ERROR_CODES.md): `VALIDATION_ERROR` (422), `NOT_FOUND` (404), `UNAUTHORIZED` (401), `FORBIDDEN` (403), `RATE_LIMITED` (429), `INTERNAL` (500).

### Database Schema

All new tables belong to migration `20260413000001_story_069_schema.up.sql` / `.down.sql` (single migration for atomic roll-out). **Source: NEW — no existing migrations for these tables (verified via grep 2026-04-13).**

```sql
-- Source: ARCHITECTURE.md (new for STORY-069)

-- AC-1: onboarding_sessions
CREATE TABLE IF NOT EXISTS onboarding_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    started_by UUID NOT NULL REFERENCES users(id),
    current_step INTEGER NOT NULL DEFAULT 1,
    step_1_data JSONB,
    step_2_data JSONB,
    step_3_data JSONB,
    step_4_data JSONB,
    step_5_data JSONB,
    state VARCHAR(20) NOT NULL DEFAULT 'in_progress', -- in_progress|completed|abandoned
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_onb_sessions_tenant_state ON onboarding_sessions (tenant_id, state);

-- AC-2: scheduled_reports
CREATE TABLE IF NOT EXISTS scheduled_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    report_type VARCHAR(50) NOT NULL,
    schedule_cron VARCHAR(100) NOT NULL,
    format VARCHAR(10) NOT NULL CHECK (format IN ('pdf','csv','xlsx')),
    recipients TEXT[] NOT NULL DEFAULT '{}',
    filters JSONB NOT NULL DEFAULT '{}',
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    last_job_id UUID,
    state VARCHAR(20) NOT NULL DEFAULT 'active', -- active|paused
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_sched_reports_tenant_state ON scheduled_reports (tenant_id, state);
CREATE INDEX idx_sched_reports_next_run ON scheduled_reports (next_run_at) WHERE state='active';

-- AC-5: webhook_configs
CREATE TABLE IF NOT EXISTS webhook_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    url TEXT NOT NULL,
    secret_encrypted BYTEA NOT NULL, -- AES-GCM via internal/crypto
    event_types VARCHAR[] NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_success_at TIMESTAMPTZ,
    last_failure_at TIMESTAMPTZ,
    failure_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_webhook_configs_tenant ON webhook_configs (tenant_id) WHERE enabled=true;

-- AC-5: webhook_deliveries
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    config_id UUID NOT NULL REFERENCES webhook_configs(id) ON DELETE CASCADE,
    event_type VARCHAR(50) NOT NULL,
    payload_hash VARCHAR(64) NOT NULL,
    payload_preview TEXT NOT NULL, -- first 1KB
    signature VARCHAR(128) NOT NULL,
    response_status INTEGER,
    response_body TEXT, -- truncated to 1KB
    attempt_count INTEGER NOT NULL DEFAULT 1,
    next_retry_at TIMESTAMPTZ,
    final_state VARCHAR(20) NOT NULL DEFAULT 'retrying', -- retrying|succeeded|dead_letter
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_webhook_deliveries_config_time ON webhook_deliveries (config_id, created_at DESC);
CREATE INDEX idx_webhook_deliveries_retry ON webhook_deliveries (next_retry_at) WHERE final_state='retrying';
CREATE INDEX idx_webhook_deliveries_tenant_state ON webhook_deliveries (tenant_id, final_state);

-- AC-7: notification_preferences (separate from legacy notification_configs)
CREATE TABLE IF NOT EXISTS notification_preferences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    event_type VARCHAR(50) NOT NULL,
    channels VARCHAR[] NOT NULL DEFAULT '{}', -- subset of email,in_app,webhook,sms,telegram
    severity_threshold VARCHAR(10) NOT NULL DEFAULT 'info', -- info|warning|error|critical
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, event_type)
);
CREATE INDEX idx_notif_prefs_tenant ON notification_preferences (tenant_id);

-- AC-8: notification_templates
CREATE TABLE IF NOT EXISTS notification_templates (
    event_type VARCHAR(50) NOT NULL,
    locale VARCHAR(5) NOT NULL, -- 'tr','en'
    subject VARCHAR(255) NOT NULL,
    body_text TEXT NOT NULL,
    body_html TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (event_type, locale)
);

-- AC-8: users.locale column
ALTER TABLE users ADD COLUMN IF NOT EXISTS locale VARCHAR(5) NOT NULL DEFAULT 'en' CHECK (locale IN ('tr','en'));

-- AC-11: ip_addresses grace columns (add fields missing vs design)
ALTER TABLE ip_addresses ADD COLUMN IF NOT EXISTS grace_expires_at TIMESTAMPTZ;
ALTER TABLE ip_addresses ADD COLUMN IF NOT EXISTS released_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_ip_addresses_grace_release
    ON ip_addresses (grace_expires_at)
    WHERE released_at IS NULL AND grace_expires_at IS NOT NULL;

-- AC-12: sms_outbound
CREATE TABLE IF NOT EXISTS sms_outbound (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    sim_id UUID NOT NULL REFERENCES sims(id),
    msisdn VARCHAR(20) NOT NULL,
    text_hash VARCHAR(64) NOT NULL, -- sha256 of text body (GDPR — do not store raw)
    text_preview VARCHAR(80), -- first 80 chars for debugging (safe)
    status VARCHAR(20) NOT NULL DEFAULT 'queued', -- queued|sent|delivered|failed
    provider_message_id VARCHAR(255),
    error_code VARCHAR(50),
    queued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ
);
CREATE INDEX idx_sms_outbound_tenant_sim_time ON sms_outbound (tenant_id, sim_id, queued_at DESC);
CREATE INDEX idx_sms_outbound_provider_id ON sms_outbound (provider_message_id) WHERE provider_message_id IS NOT NULL;
CREATE INDEX idx_sms_outbound_status ON sms_outbound (status);
```

Down migration drops all new tables + removes added columns (`users.locale`, `ip_addresses.grace_expires_at`, `ip_addresses.released_at`) + removes indexes.

**RLS note:** All new tenant-scoped tables MUST be added to `20260413000002_rls_story_069.up.sql` mirroring pattern from `20260412000006_rls_policies.up.sql` (ENABLE + FORCE row security, tenant isolation policy via `current_setting('argus.tenant_id')`). `notification_templates` is global-no-tenant and does NOT need RLS.

### Screen Mockups

#### SCR-030..034 Onboarding Wizard (5 steps)
```
┌─────────────────────────────────────────────────────────────┐
│ Argus Onboarding · Step 3 of 5                    [ X Exit] │
├─────────────────────────────────────────────────────────────┤
│  [●━━━●━━━●━━━○━━━○]                                        │
│   Company Admin Operators APN  SIMs                         │
├─────────────────────────────────────────────────────────────┤
│  Operator Grants                                            │
│  Select which MNO profiles your tenant may use.             │
│                                                             │
│  ☑ Turkcell NB-IoT Profile        RATs: [NB-IoT] [LTE-M]    │
│  ☑ Vodafone TR Profile            RATs: [4G]  [5G]          │
│  ☐ TT Mobil Profile                                         │
│                                                             │
│             [ ← Back ]              [ Skip ] [ Next → ]     │
└─────────────────────────────────────────────────────────────┘
```
Resume behaviour: on mount, read `localStorage.onboarding_session_id`, `GET /onboarding/:id`, hydrate to `current_step`.

#### SCR-125 Compliance Reports List
```
┌─────────────────────────────────────────────────────────────┐
│ Reports > Compliance               [+ Generate] [Scheduled] │
├─────────────────────────────────────────────────────────────┤
│ Type          Format Last Run       Status    Actions       │
│ KVKK          PDF    2h ago         ✓ Ready   [↓ Download]  │
│ GDPR          XLSX   yesterday      ⏳ Queued  [Cancel]      │
│ BTK           CSV    3d ago         ✓ Ready   [↓ Download]  │
│ SLA Monthly   PDF    Apr 01         ✓ Ready   [↓ Download]  │
└─────────────────────────────────────────────────────────────┘
```

#### SCR-127 Scheduled Reports
```
┌─────────────────────────────────────────────────────────────┐
│ Scheduled Reports                             [+ New Schedule]│
├─────────────────────────────────────────────────────────────┤
│ Type          Cron       Recipients    Next Run   State     │
│ Usage Summary 0 2 * * *  3 addrs       02:00      ▶ Active  │
│ SLA Monthly   0 3 1 * *  5 addrs       May 01     ⏸ Paused  │
│ KVKK          0 4 * * 0  2 addrs       Sun 04:00  ▶ Active  │
└─────────────────────────────────────────────────────────────┘
```

#### SCR-111 Notification Preferences Matrix
```
┌──────────────────────┬──────┬──────┬─────────┬──────┬──────┐
│ Event Type           │Email │InApp │ Webhook │ SMS  │Thresh│
├──────────────────────┼──────┼──────┼─────────┼──────┼──────┤
│ sim_state_change     │  ☑   │  ☑   │   ☐     │  ☐   │ info │
│ operator_degraded    │  ☑   │  ☑   │   ☑     │  ☐   │ warn │
│ policy_violation     │  ☑   │  ☑   │   ☐     │  ☐   │ warn │
│ ip_pool_warning      │  ☑   │  ☑   │   ☑     │  ☐   │ warn │
│ anomaly_detected     │  ☑   │  ☑   │   ☑     │  ☑   │ error│
└──────────────────────┴──────┴──────┴─────────┴──────┴──────┘
```

#### Webhook Delivery Log
```
┌─────────────────────────────────────────────────────────────┐
│ Webhook: https://example.com/hook   [✎ Edit] [↻ Test]       │
├─────────────────────────────────────────────────────────────┤
│ Recent deliveries:                                          │
│ 14:02  sim_state_change   200  succeeded  [View Payload]    │
│ 13:45  anomaly_detected   500  retrying (3/5) [↻ Retry now] │
│ 12:01  ip_pool_warning    —    dead_letter [↻ Retry now]    │
└─────────────────────────────────────────────────────────────┘
```

#### SMS History
```
┌─────────────────────────────────────────────────────────────┐
│ SMS Gateway                       [+ Send SMS]              │
├─────────────────────────────────────────────────────────────┤
│ SIM / MSISDN         Text Preview       Status   Queued     │
│ 8990… / +90532…     "Test kısa mesa…"  delivered 14:02      │
│ 8990… / +90533…     "Cihaz güncell…"   failed   13:45 err03│
└─────────────────────────────────────────────────────────────┘
```

### Design Token Map

#### Color Tokens (from FRONTEND.md + index.css)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page bg | `bg-bg-primary` | `bg-black`, `bg-[#06060B]` |
| Card/panel bg | `bg-bg-surface` | `bg-white`, `bg-[#0c0c14]` |
| Elevated modal bg | `bg-bg-elevated` | `bg-gray-900`, hex |
| Hover bg | `bg-bg-hover` | inline hex |
| Default border | `border-border` | `border-[#1e1e30]`, `border-gray-700` |
| Subtle border | `border-border-subtle` | hex |
| Primary text | `text-text-primary` | `text-white`, `text-[#e4e4ed]` |
| Secondary text | `text-text-secondary` | `text-gray-400`, hex |
| Tertiary text | `text-text-tertiary` | hex |
| Accent (primary action) | `text-accent`, `bg-accent` | `text-blue-400`, `text-[#00d4ff]` |
| Success state | `text-success`, `bg-success-dim` | `text-green-*` |
| Warning state | `text-warning`, `bg-warning-dim` | `text-yellow-*` |
| Danger / failed / dead-letter | `text-danger`, `bg-danger-dim` | `text-red-*` |

#### Typography
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page title (15-16px 600) | `text-base font-semibold` | `text-2xl` |
| Table data | `text-[13px]` OR `text-sm` | `text-base` |
| ICCID/IMSI/IP mono | `font-mono text-[12px]` | `text-xs` without font-mono |
| Section label uppercase | `text-[11px] uppercase tracking-wider text-text-secondary` | hex |
| Metric mono bold | `font-mono text-2xl font-bold` | |

#### Spacing / Radii / Elevation
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Card radius | `rounded-md` (10px) | `rounded-lg` |
| Button radius | `rounded-sm` (6px) | `rounded` |
| Modal radius | `rounded-lg` (14px) | |
| Card shadow | `shadow-[0_2px_8px_rgba(0,0,0,0.3),0_0_1px_rgba(255,255,255,0.05)]` OR a class `shadow-card` if defined | arbitrary |
| Content padding | `p-6` (24px) | `p-4`, `p-5` |
| Field gap | `gap-3` (12px) | `gap-2` |

#### Existing Components to REUSE (DO NOT recreate)
| Component | Path | Use For |
|-----------|------|---------|
| `<Button>` | `web/src/components/ui/button.tsx` | ALL buttons — NEVER raw `<button>` |
| `<Input>` | `web/src/components/ui/input.tsx` | ALL text inputs — NEVER raw `<input>` |
| `<Textarea>` | `web/src/components/ui/textarea.tsx` | SMS text field |
| `<Select>` | `web/src/components/ui/select.tsx` | Format / report-type pickers |
| `<Card>` | `web/src/components/ui/card.tsx` | All panels/sections |
| `<Badge>` | `web/src/components/ui/badge.tsx` | Status chips (queued/sent/failed) |
| `<Dialog>` | `web/src/components/ui/dialog.tsx` | Generate-report modal, new-schedule modal |
| `<SlidePanel>` | `web/src/components/ui/slide-panel.tsx` | Webhook delivery payload inspector |
| `<Table>` / `<TableHeader>` / `<TableBody>` | `web/src/components/ui/table.tsx` | Every list table |
| `<Tabs>` | `web/src/components/ui/tabs.tsx` | Reports page tab nav |
| `<Spinner>` | `web/src/components/ui/spinner.tsx` | Loading indicators |
| `<Skeleton>` | `web/src/components/ui/skeleton.tsx` | List skeletons |
| `<TableToolbar>` | `web/src/components/ui/table-toolbar.tsx` | Filters row |
| Icons | `lucide-react` | NEVER raw SVG |

**Frontend rule:** every new TSX file under `web/src/pages/{reports,webhooks,notifications,sms,compliance}` MUST pass `grep -E '#[0-9a-fA-F]{3,8}' <file>` with ZERO matches (all colors via token classes).

## Prerequisites
- [x] STORY-059 — PDF export base (`internal/compliance/service.go#buildBTKReportPDF` uses `fpdf` — pattern to follow)
- [x] STORY-063 — S3 uploader (`internal/storage/s3_uploader.go`), SMS Twilio adapter (`internal/notification/sms_twilio.go`), in-app adapter
- [x] STORY-065 — job metrics (`argus_job_*` Prometheus counters, reused)
- [x] STORY-068 — enterprise auth hardening (JWT + RBAC middleware in place)
- [x] `github.com/go-pdf/fpdf` already in `go.mod`
- [ ] Add `github.com/xuri/excelize/v2` to `go.mod`

## Tasks

> Each task is dispatched to a FRESH Developer subagent with isolated context.
> Complexity mapping: this is an **L** story with wide surface area → most tasks medium, multi-service orchestration tasks (webhook HMAC+delivery+retry, scheduled-report processor, data portability) marked **high**.

---

### Wave 1 — DB migrations (must land first, all other work depends on schema)

#### Task 1: Create STORY-069 schema migration
- **Files:** Create `migrations/20260413000001_story_069_schema.up.sql`, `migrations/20260413000001_story_069_schema.down.sql`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `migrations/20260412000011_enterprise_auth_hardening.up.sql` + `.down.sql` — follow same structure (IF NOT EXISTS, indexes, comments, down drops everything).
- **Context refs:** `Database Schema`, `Architecture Context > Components Involved`
- **What:** Create all 7 new tables (onboarding_sessions, scheduled_reports, webhook_configs, webhook_deliveries, notification_preferences, notification_templates, sms_outbound) + ALTER users ADD locale + ALTER ip_addresses ADD grace_expires_at/released_at + all listed indexes. Down migration reverses everything.
- **Verify:** `make db-migrate` succeeds, `make db-migrate-down 1 && make db-migrate` is idempotent, `\d onboarding_sessions` shows columns.

#### Task 2: Create STORY-069 RLS migration
- **Files:** Create `migrations/20260413000002_story_069_rls.up.sql` + `.down.sql`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260412000006_rls_policies.up.sql` (tenant_retention_config block lines 135-140) — follow same ENABLE ROW LEVEL SECURITY + FORCE + CREATE POLICY.
- **Context refs:** `Database Schema`
- **What:** Enable + FORCE RLS on onboarding_sessions, scheduled_reports, webhook_configs, webhook_deliveries, notification_preferences, sms_outbound. Policy: `tenant_id = current_setting('argus.tenant_id')::uuid`. NOT on notification_templates (global). NOT on users (already handled).
- **Verify:** Unit test in `internal/store/rls_test.go` (if exists) with 2 tenants can't cross-read.

#### Task 3: Seed notification templates (tr+en) for all event types
- **Files:** Create `migrations/seed/004_notification_templates.sql`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `migrations/seed/003_comprehensive_seed.sql` — use DO $$ blocks + INSERT ON CONFLICT DO UPDATE.
- **Context refs:** `Architecture Context > Data Flow > AC-2/AC-3/AC-4 Reports`, `API Specifications > AC-8 Templates`
- **What:** Seed rows for 14 event types × 2 locales (28 rows). Event types: `welcome`, `sim_state_change`, `operator_degraded`, `policy_violation`, `ip_pool_warning`, `anomaly_detected`, `data_portability_ready`, `kvkk_purge_completed`, `sms_delivery_failed`, `onboarding_completed`, `report_ready`, `webhook_dead_letter`, `ip_released`, `session_login`. Turkish body must use proper diacritics (ç, ğ, ı, ö, ş, ü) — NOT ASCII-only (see PAT-003 warning).
- **Verify:** `SELECT count(*) FROM notification_templates;` returns 28.

---

### Wave 2 — Backend: Stores (data access layer, all enabled by Wave 1)

#### Task 4: Onboarding session store
- **Files:** Create `internal/store/onboarding_session_store.go` + `internal/store/onboarding_session_store_test.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/job_store.go` for tenant-scoped CRUD pattern (GetByID, Create, Update, tenant context via ctx).
- **Context refs:** `Database Schema > onboarding_sessions`, `Architecture Context > Data Flow > AC-1 Onboarding Wizard`
- **What:** `Create(ctx, tenantID, startedBy)`, `GetByID(ctx, id)`, `UpdateStep(ctx, id, stepN int, stepData []byte, newCurrentStep int)`, `MarkCompleted(ctx, id)`. All queries scoped to tenant_id. Tests cover create/advance/complete/tenant isolation.
- **Verify:** `go test ./internal/store/ -run OnboardingSession` passes.

#### Task 5: Scheduled report store
- **Files:** Create `internal/store/scheduled_report_store.go` + test
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** `internal/store/job_store.go`.
- **Context refs:** `Database Schema > scheduled_reports`
- **What:** CRUD + `ListDue(ctx, now)` returning rows where `state='active' AND next_run_at<=NOW()` for scheduler sweep. `UpdateLastRun(ctx, id, lastRunAt, nextRunAt, lastJobID)`.
- **Verify:** Test passes.

#### Task 6: Webhook config + delivery stores
- **Files:** Create `internal/store/webhook_store.go` (both tables, one file) + test
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** `internal/store/job_store.go` and `internal/store/audit_store.go` (for secret encryption see `internal/crypto/`).
- **Context refs:** `Database Schema > webhook_configs`, `Database Schema > webhook_deliveries`
- **What:** `WebhookConfigStore.{Create,Get,List,Update,Delete}` (encrypt secret via `internal/crypto` on write, decrypt on read — NEVER return decrypted secret in API responses, only to dispatcher). `WebhookDeliveryStore.{Insert,UpdateAttempt,ListByConfig,ListDueForRetry(ctx, now),MarkFinal(id, state), GetByID}`.
- **Verify:** Test covers encryption round-trip + retry listing.

#### Task 7: Notification preference + template stores
- **Files:** Create `internal/store/notification_preference_store.go`, `internal/store/notification_template_store.go` + tests
- **Depends on:** Task 1, Task 3
- **Complexity:** low
- **Pattern ref:** `internal/store/notification_store.go` (existing notification_configs store).
- **Context refs:** `Database Schema > notification_preferences`, `Database Schema > notification_templates`
- **What:** PreferenceStore: `GetMatrix(ctx, tenantID)`, `Upsert(ctx, tenantID, []Preference)`. TemplateStore: `Get(ctx, eventType, locale)` with fallback to `en` if row missing, `Upsert(ctx, eventType, locale, subject, bodyText, bodyHTML)`.
- **Verify:** Test including fallback path.

#### Task 8: SMS outbound store
- **Files:** Create `internal/store/sms_outbound_store.go` + test
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** `internal/store/job_store.go`.
- **Context refs:** `Database Schema > sms_outbound`
- **What:** `Insert`, `UpdateStatus(ctx, id, status, providerMsgID, errCode, sentAt)`, `MarkDelivered(ctx, providerMsgID, deliveredAt)` (matches by provider_message_id so it can be called from both outbound + inbound status webhook), `List(ctx, tenantID, filters, cursor)`.
- **Verify:** Test.

---

### Wave 3 — Backend: Report engine (AC-4) — parallel with Wave 2 except it needs no stores yet

#### Task 9: Report formats core — types + CSV + PDF
- **Files:** Create `internal/report/types.go`, `internal/report/csv.go`, `internal/report/pdf.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/compliance/service.go` lines ~200-400 (`ExportBTKReportCSV`, `buildBTKReportPDF`) — follow same fpdf usage pattern (tenant branding header, sections, footer).
- **Context refs:** `Architecture Context > Data Flow > AC-2/AC-3/AC-4 Reports`, `API Specifications > AC-2/AC-3 Reports`
- **What:** Define `ReportRequest{Type, Format, TenantID, Filters, Locale}`, `ReportEngine interface { Build(ctx, ReportRequest) ([]byte, mimeType string, error) }`. CSV engine for all 8 report types (query SVC-07 analytics + compliance service + audit store). PDF engine via fpdf — add pageHeader, sections, footer with page number. 8 report types: `compliance_kvkk`, `compliance_gdpr`, `compliance_btk`, `sla_monthly`, `usage_summary`, `cost_analysis`, `audit_log_export`, `sim_inventory`. Compliance reports reuse existing compliance service; SLA reuses existing sla report; usage/cost from analytics; audit from audit store; sim_inventory from sim store.
- **Verify:** `go test ./internal/report/ -run PDF -run CSV` — golden-file fixtures for each type.

#### Task 10: Excel (.xlsx) format
- **Files:** Create `internal/report/excel.go` + test, Modify `go.mod` to add `github.com/xuri/excelize/v2 v2.8.x`
- **Depends on:** Task 9
- **Complexity:** medium
- **Pattern ref:** None (first Excel code in repo). Follow excelize v2 README: `f := excelize.NewFile(); f.SetSheetName(...); f.SetCellValue(sheet, cell, v); f.Write(io.Writer)`.
- **Context refs:** `Architecture Context > Data Flow > AC-2/AC-3/AC-4 Reports`
- **What:** Excel builder with multi-sheet layout for complex reports (e.g. `sla_monthly` has Summary + Per-SIM + Breach Log sheets; `audit_log_export` single sheet with filter metadata sheet). Reuses Task 9 data-fetch helpers.
- **Verify:** Test generates valid xlsx, openable with excelize in read-back assertion.

---

### Wave 4 — Backend: API handlers + services (depends on Waves 2 & 3)

#### Task 11: Onboarding API handler (AC-1)
- **Files:** Create `internal/api/onboarding/handler.go`, `internal/api/onboarding/handler_test.go`
- **Depends on:** Task 4
- **Complexity:** high
- **Pattern ref:** `internal/api/compliance/handler.go` (envelope, error mapping). Tx pattern: `internal/api/bulk/import.go` (multi-step atomic commit with rollback).
- **Context refs:** `Architecture Context > Data Flow > AC-1 Onboarding Wizard`, `API Specifications > AC-1 Onboarding`
- **What:** Implement 4 endpoints. Each `step/:n` handler validates the step's payload schema, opens a DB tx, applies side-effects, on success updates session `current_step = n+1`, commits. On failure, rollback + 422 with per-field errors. Step 5 enqueues bulk_sim_import job; step `/complete` assigns default policy (via `internal/policy` service), sends welcome notification (`event_type=onboarding_completed`), audits. Register routes in `internal/gateway/router.go`.
- **Verify:** Handler tests cover happy path + resume + failure rollback on step 3 (operator grant invalid operator_id → no grants inserted + session step unchanged). `go test ./internal/api/onboarding/` passes.

#### Task 12: Reports API handler + sync/async generation (AC-2, AC-3)
- **Files:** Create `internal/api/reports/handler.go` + test
- **Depends on:** Task 5, Task 9, Task 10
- **Complexity:** high
- **Pattern ref:** `internal/api/sla/handler.go` for list/get. `internal/api/bulk/import.go` for async job enqueue pattern.
- **Context refs:** `API Specifications > AC-2/AC-3 Reports`, `Architecture Context > Data Flow > AC-2/AC-3/AC-4 Reports`
- **What:** 5 endpoints. `POST /generate`: size heuristic — if (format=csv AND report_type IN [usage_summary,sim_inventory] AND filter scope <5k rows estimate) run sync; else enqueue `scheduled_report_run` job and return 202 + job_id. Cron expression validation on scheduled CRUD (shared `job/scheduler.go#matchCronExpr` for format only, next-run computation via new helper `nextRunAfter(schedule, now)`).
- **Verify:** Tests: create scheduled, list, patch state, delete, sync generate CSV, async PDF enqueues job with type=scheduled_report_run.

#### Task 13: Webhook API handler + HMAC dispatcher (AC-5, AC-6)
- **Files:** Create `internal/api/webhooks/handler.go` + test, Modify `internal/notification/webhook.go` to add HMAC + delivery log hook
- **Depends on:** Task 6
- **Complexity:** high
- **Pattern ref:** `internal/api/compliance/handler.go` + `internal/notification/webhook.go` (existing simple dispatcher).
- **Context refs:** `API Specifications > AC-5/AC-6 Webhooks`, `Architecture Context > Data Flow > AC-5/AC-6 Webhooks`, `Database Schema > webhook_configs`, `Database Schema > webhook_deliveries`
- **What:** 6 endpoints. Modify `webhook.go#SendWebhook` to compute `sig := hmac.New(sha256, secret); sig.Write(body); hex := sig.Sum`, set header `X-Argus-Signature: sha256=<hex>`, `X-Argus-Timestamp`, `X-Argus-Event`. After POST, call `webhookDeliveryStore.Insert(...)` with response + attempt 1. Expose `DispatchToConfigs(ctx, tenantID, event, payload)` used by notification.Service. Add doc file `docs/architecture/WEBHOOK_HMAC.md` with verification example (Node, Python, Go snippets). Inbound: confirm existing SMS status webhook verifies `X-Twilio-Signature`.
- **Verify:** Test: valid signature, invalid signature rejected, delivery inserted + retry row listed. `go test ./internal/api/webhooks/ ./internal/notification/` passes.

#### Task 14: Webhook retry scheduler + DLQ processor
- **Files:** Create `internal/job/webhook_retry.go` + test, Modify `cmd/argus/main.go` (or wherever runner registers processors) to add `JobTypeWebhookRetry` + cron entry
- **Depends on:** Task 13
- **Complexity:** medium
- **Pattern ref:** `internal/job/runner.go` processor pattern; `internal/job/scheduler.go#AddEntry` usage.
- **Context refs:** `Architecture Context > Data Flow > AC-5/AC-6 Webhooks`
- **What:** Cron entry `webhook_retry_sweep` `*/1 * * * *`. Processor: select deliveries due for retry, re-send (reusing dispatcher), on success mark succeeded, on failure increment attempt, compute next_retry_at via backoff [30s, 2m, 10m, 30m, 60m], after 5th failure mark dead_letter + emit `webhook.dead_letter` notification (read from prefs/templates). Add metric `argus_webhook_retries_total{result="..."}`.
- **Verify:** Test: insert retry row with next_retry_at=past, run processor, verify re-send; simulate 5 failures → dead_letter state.

#### Task 15: Notification preference + template API handlers (AC-7, AC-8) + dispatch integration
- **Files:** Modify `internal/api/notification/handler.go` (add 4 new handlers), Modify `internal/notification/service.go` (read preferences + templates during dispatch), Create `internal/notification/template_store_test.go` integration
- **Depends on:** Task 7
- **Complexity:** high
- **Pattern ref:** Existing notification handler + `internal/notification/service.go#Dispatch`.
- **Context refs:** `API Specifications > AC-7 Preferences`, `API Specifications > AC-8 Templates`, `Architecture Context > Data Flow > AC-5/AC-6 Webhooks` (for dispatch path)
- **What:** Add 4 endpoints for prefs matrix GET/PUT + templates GET/PUT. In `service.go#Dispatch`: before sending per channel, check `notification_preferences` for tenant+event; also check severity threshold. For each recipient (from context), look up template by `(event_type, user.locale)` with fallback to `en`; render `{{ .field }}` Go templates against event payload; send rendered subject+body. Keep legacy `/notification-configs` endpoint as alias reading from new table (graceful migration).
- **Verify:** Test: user locale=tr, send event with tr template seeded → email sender receives Turkish subject; preference disables `anomaly_detected` for webhook → dispatcher skips webhook.

#### Task 16: Data portability handler + job processor (AC-9)
- **Files:** Create `internal/api/compliance/data_portability.go` (new method on existing Handler), Create `internal/job/data_portability.go` + test
- **Depends on:** Task 5 (job queue), existing compliance Service, existing storage/s3_uploader
- **Complexity:** high
- **Pattern ref:** `internal/compliance/service.go#DataSubjectAccess` (already fetches sim-scoped data; extend for user-scoped).
- **Context refs:** `API Specifications > AC-9 Data Portability`, `Architecture Context > Data Flow > AC-9 GDPR Data Portability`
- **What:** `POST /api/v1/compliance/data-portability/:user_id` creates job. Processor: gather profile, tenant name, owned sims, last-90d sessions (Note: sims table may not have owner_user_id column — **check before impl; if missing, add in Task 1 migration or scope to tenant**), cdrs via sim, audit actions by user. Build archive.zip (stdlib `archive/zip`) containing `data.json` (canonical JSON with BOM-less UTF-8) + `summary.pdf` via fpdf. Upload to S3 key `tenants/{tenant_id}/portability/{user_id}/{job_id}.zip`. Send notification with signed URL (7d TTL). Audit `data_portability.exported`.
- **Verify:** Test with user having 5 sims, 20 cdrs, 3 audit rows → archive contains all sections. **NOTE: before this task starts, grep `sims` schema for `owner_user_id` — if absent, add column via Task 1 migration or change aggregation to fall back to all tenant sims (document choice in decisions.md).**

#### Task 17: KVKK auto-purge cron job (AC-10)
- **Files:** Create `internal/job/kvkk_purge.go` + test, register cron + processor in `cmd/argus/main.go` wiring
- **Depends on:** Task 5
- **Complexity:** medium
- **Pattern ref:** `internal/job/purge_sweep.go` (existing partial purge logic) + `internal/job/data_retention.go`.
- **Context refs:** `Architecture Context > Data Flow > AC-10 KVKK auto-purge`
- **What:** Cron entry `kvkk_purge_daily @daily`. Processor iterates all tenants, reads `tenant_retention_config` (fallback `tenants.purge_retention_days`) for each table (cdr/session/audit). Pseudonymize PII (msisdn→sha256, imsi→sha256) in rows older than retention for cdrs, sessions, user_sessions; redact `audit_logs.details` jsonb user-identifying keys. Support `payload.dry_run=true` to only count and return `{would_purge: {...}}` in job.result. Audit per-tenant `kvkk.purge.run` with counts. Metric `argus_kvkk_purge_rows_total{table=...}`.
- **Verify:** Test with mock rows older than retention → after run, rows' msisdn is hashed; dry-run leaves rows intact.

#### Task 18: IP grace-period release cron job (AC-11)
- **Files:** Create `internal/job/ip_grace_release.go` + test, wire cron + processor
- **Depends on:** Task 1 (for `grace_expires_at`, `released_at` columns)
- **Complexity:** medium
- **Pattern ref:** `internal/job/ip_reclaim.go` (existing pattern — similar sweep against ip_addresses).
- **Context refs:** `Architecture Context > Data Flow > AC-11 IP grace release`
- **What:** Cron `ip_grace_release_hourly @hourly`. Processor SELECT with JOIN to sims.state='terminated', UPDATE state=available + released_at=NOW() + sim_id=NULL, publish NATS `ip.released` per row. Add metric `argus_ip_grace_released_total`. Audit `ip.released` (bulk summary per run).
- **Verify:** Test with 3 expired rows + 1 not-expired → after run, 3 released + NATS published.

#### Task 19: SMS gateway API + processor + rate limit (AC-12)
- **Files:** Create `internal/api/sms/handler.go` + test, Create `internal/job/sms_gateway.go` + test, Modify `internal/api/notification/sms_webhook.go` (if needed — extend match to sms_outbound)
- **Depends on:** Task 8, existing `internal/notification/sms_twilio.go`
- **Complexity:** high
- **Pattern ref:** `internal/api/notification/handler.go` (pagination), `internal/notification/sms_twilio.go` (SMS send).
- **Context refs:** `API Specifications > AC-12 SMS`, `Architecture Context > Data Flow > AC-12 SMS gateway outbound`
- **What:** 2 endpoints. POST `/sms/send` enforces redis token-bucket rate limit (key `sms:rate:{tenant_id}`, size=`RATE_LIMIT_SMS_PER_MINUTE` env default 60, window 60s — reuse `internal/notification/redis_ratelimiter.go`). Insert sms_outbound row, enqueue `sms_outbound_send` job. Job processor: resolve msisdn, call `SMSGatewaySender.SendSMS`, update row status. GET `/sms/history` cursor paginated. Extend sms_webhook delivery-status handler to lookup BOTH `notifications` and `sms_outbound` by provider_message_id. Audit `sms.sent`. New env var `RATE_LIMIT_SMS_PER_MINUTE` (document in `docs/architecture/CONFIG.md`).
- **Verify:** Test: send succeeds + row queued; 61st in a minute returns 429; processor sends + status update on inbound webhook works for outbound row. `go test ./internal/api/sms/ ./internal/job/ -run SMS` passes.

#### Task 20: Scheduled-report cron sweeper + job processor (AC-2)
- **Files:** Create `internal/job/scheduled_report.go` + test, wire cron entry
- **Depends on:** Task 5, Task 9, Task 10, Task 12
- **Complexity:** high
- **Pattern ref:** `internal/job/sla_report.go` (existing — builds PDF + stores result).
- **Context refs:** `Architecture Context > Data Flow > AC-2/AC-3/AC-4 Reports`
- **What:** Cron `scheduled_report_sweeper */1 * * * *` scans `ListDue`, enqueues `scheduled_report_run` per row. Processor: loads scheduled_reports row, calls report engine, uploads to S3 via `storage.S3Uploader`, generates signed URL, sends email notification to each recipient (use notification service with `event_type=report_ready` template). Updates `last_run_at`, `next_run_at`, `last_job_id`. Metric `argus_scheduled_report_runs_total{type=,result=}`.
- **Verify:** Test with fake time, scheduled row next_run_at=past → sweeper enqueues → processor uploads + updates row.

---

### Wave 5 — Frontend (depends on Wave 4 APIs being live)

#### Task 21: Onboarding wizard — wire real API + resume
- **Files:** Modify `web/src/components/onboarding/wizard.tsx`, Create `web/src/hooks/use-onboarding.ts`
- **Depends on:** Task 11
- **Complexity:** medium
- **Pattern ref:** `web/src/hooks/use-operators.ts` for React Query hook pattern.
- **Context refs:** `API Specifications > AC-1 Onboarding`, `Screen Mockups > SCR-030..034`, `Design Token Map`
- **What:** Replace current multi-call wizard logic with single session: on mount, read `localStorage.argus_onboarding_session`, call `GET /onboarding/:id`, hydrate step state. Each `Next` calls `POST /onboarding/:id/step/:n`. On 422 render per-field errors inline. Final step `POST /onboarding/:id/complete` → navigate to dashboard. Use ONLY Design Token Map classes, existing UI atoms. Skill: frontend-design.
- **Verify:** Manual: refresh browser mid-step → wizard resumes at the same step with prior data prefilled. `grep -E '#[0-9a-fA-F]{3,8}' web/src/components/onboarding/wizard.tsx` → ZERO matches.

#### Task 22: Reports page — replace hardcoded + setTimeout with real API
- **Files:** Modify `web/src/pages/reports/index.tsx`, Create `web/src/hooks/use-reports.ts`, Create `web/src/pages/reports/scheduled.tsx`
- **Depends on:** Task 12
- **Complexity:** medium
- **Pattern ref:** `web/src/pages/sessions.tsx` for table + query pattern.
- **Context refs:** `API Specifications > AC-2/AC-3 Reports`, `Screen Mockups > SCR-125`, `Screen Mockups > SCR-127`, `Design Token Map`
- **What:** `useReportsList`, `useGenerateReport`, `useScheduledReports`, `useCreateScheduledReport` etc. Main Reports page shows list of report_types with Generate button → opens `<Dialog>` (format select, filters). Submitting calls `/reports/generate`; shows `<Spinner>` while job pending (poll via `use-jobs.ts`), then shows `<Button>Download</Button>` with signed URL. Scheduled tab (`scheduled.tsx`) lists + CRUD scheduled reports. No hardcoded colors. Skill: frontend-design.
- **Verify:** Generate KVKK PDF → download works. `grep -E '#[0-9a-fA-F]{3,8}' web/src/pages/reports/` → ZERO matches.

#### Task 23: Webhooks page — configs + delivery log
- **Files:** Create `web/src/pages/webhooks/index.tsx`, Create `web/src/hooks/use-webhooks.ts`, add route in `web/src/App.tsx`
- **Depends on:** Task 13
- **Complexity:** medium
- **Pattern ref:** `web/src/pages/settings/api-keys.tsx` for config list/create/delete modals.
- **Context refs:** `API Specifications > AC-5/AC-6 Webhooks`, `Screen Mockups > Webhook Delivery Log`, `Design Token Map`
- **What:** Two-section page: top list of webhook configs (CRUD via dialog, secret shown once on create), bottom panel `<SlidePanel>` on row-click showing last 20 deliveries with status Badge, response code, Retry button. Use frontend-design skill for polish.
- **Verify:** Create webhook, trigger event (dev tool), delivery appears. No hardcoded hex.

#### Task 24: Notification preferences matrix + templates editor
- **Files:** Modify `web/src/pages/notifications/index.tsx` (add Preferences + Templates tabs), Create `web/src/hooks/use-notification-preferences.ts`
- **Depends on:** Task 15
- **Complexity:** medium
- **Pattern ref:** Existing notifications page tabs structure.
- **Context refs:** `API Specifications > AC-7 Preferences`, `API Specifications > AC-8 Templates`, `Screen Mockups > SCR-111`, `Design Token Map`
- **What:** Preferences tab: matrix `<Table>` rows=event_types × cols=channels with checkboxes + severity dropdown; Save button → PUT upsert. Templates tab: picker by event_type+locale, textareas for subject/body_text/body_html, live preview panel, Save → PUT. Turkish accents preserved (PAT-003: use `String.normalize` if needed, grep after render).
- **Verify:** Toggle preference → reflected after refresh. No hex colors.

#### Task 25: SMS + Data-portability pages
- **Files:** Create `web/src/pages/sms/index.tsx` (add route), Create `web/src/pages/compliance/data-portability.tsx` (add route), Create `web/src/hooks/use-sms.ts`, Create `web/src/hooks/use-data-portability.ts`
- **Depends on:** Task 19, Task 16
- **Complexity:** medium
- **Pattern ref:** `web/src/pages/sessions.tsx` for table + filters + cursor pagination.
- **Context refs:** `API Specifications > AC-12 SMS`, `API Specifications > AC-9 Data Portability`, `Screen Mockups > SMS History`, `Design Token Map`
- **What:** SMS page: send form (SIM search via existing `<SimSearch>`, textarea for text max 480, priority select), history table with status Badge + filters. Data-portability page: user picker, "Request Export" button → 202 w/ job id, list of past exports w/ download link when ready. Skill: frontend-design.
- **Verify:** Send SMS → row appears. Request portability → job queued → after processor runs, download URL emailed. Zero hex.

---

### Wave 6 — Cross-cutting + gate tasks

#### Task 26: Wire all cron entries + processors in main.go
- **Files:** Modify `cmd/argus/main.go`
- **Depends on:** Task 14, Task 17, Task 18, Task 19, Task 20
- **Complexity:** low
- **Pattern ref:** existing cron AddEntry calls in main.go (backup, sla_report etc.).
- **Context refs:** `Architecture Context > Components Involved`
- **What:** Register the 5 new cron entries: `kvkk_purge @daily`, `ip_grace_release @hourly`, `webhook_retry_sweep */1 * * * *`, `scheduled_report_sweeper */1 * * * *`. Register 5 new job processors on runner: `kvkk_purge_daily`, `ip_grace_release`, `webhook_retry`, `scheduled_report_run`, `sms_outbound_send`, `data_portability_export`. Add all new route mounts (onboarding/reports/webhooks/sms/data-portability/prefs/templates).
- **Verify:** `make run` starts clean; `curl /api/v1/onboarding/start` hits handler (401 w/o auth).

#### Task 27: End-to-end integration test
- **Files:** Create `tests/e2e/story_069_test.go` (or `internal/e2e/` if that's the project convention)
- **Depends on:** Task 21, Task 22, Task 23, Task 24, Task 25, Task 26
- **Complexity:** high
- **Pattern ref:** Existing e2e tests in `tests/` (search for `_test.go` with fixtures spawning docker compose).
- **Context refs:** Test scenarios block from story, `API Specifications`
- **What:** Cover 7 scenarios from Test Scenarios block: onboarding E2E + resume, scheduled report runs + S3 + email, KVKK PDF content, webhook retry + DLQ, HMAC verify, locale tr/en template, GDPR portability S3 delivery, KVKK purge, IP grace release.
- **Verify:** `make test-e2e` passes.

#### Task 28: Docs updates
- **Files:** Create `docs/architecture/WEBHOOK_HMAC.md`, Modify `docs/architecture/CONFIG.md` (new envs: `RATE_LIMIT_SMS_PER_MINUTE`, `WEBHOOK_SIGNATURE_SECRET_DEFAULT`, `REPORT_STORAGE_BUCKET`), Modify `docs/architecture/api/_index.md` (register API-170..API-185 + new API-186..API-200 for new endpoints), Modify `docs/architecture/db/_index.md` (register TBL-25..TBL-31), Modify `docs/ROUTEMAP.md` (mark STORY-069 DONE row).
- **Depends on:** All implementation tasks
- **Complexity:** low
- **Pattern ref:** Existing API/TBL index entries.
- **Context refs:** `API Specifications`, `Database Schema`
- **What:** Keep architecture docs in sync so STORY-070+ (Frontend wiring story) knows contracts.
- **Verify:** `grep -r STORY-069 docs/architecture/` returns new entries.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 Onboarding wizard E2E | Task 1 (schema), Task 4 (store), Task 11 (API), Task 21 (UI) | Task 27 E2E scenario 1 |
| AC-2 Scheduled reports | Task 1, Task 5, Task 12, Task 20, Task 22 | Task 27 scenario 2 |
| AC-3 On-demand reports | Task 12, Task 22 | Task 27 scenario 2 |
| AC-4 PDF/CSV/Excel formats | Task 9, Task 10 | Tasks 9/10 unit tests; Task 27 scenario 3 |
| AC-5 Webhook delivery tracking | Task 1, Task 6, Task 13, Task 14, Task 23 | Task 14 test; Task 27 scenario 4 |
| AC-6 Webhook HMAC | Task 13, Task 28 (docs) | Task 13 test; Task 27 scenario 5 |
| AC-7 Per-tenant preferences | Task 1, Task 7, Task 15, Task 24 | Task 15 test |
| AC-8 Locale templates | Task 1, Task 3 (seed), Task 7, Task 15, Task 24 | Task 15 test; Task 27 scenario 6 |
| AC-9 GDPR data portability | Task 1, Task 16, Task 25 | Task 16 test; Task 27 scenario 7 |
| AC-10 KVKK auto-purge cron | Task 1, Task 17, Task 26 | Task 17 test; Task 27 scenario 8 |
| AC-11 IP grace release cron | Task 1, Task 18, Task 26 | Task 18 test; Task 27 scenario 9 |
| AC-12 SMS gateway outbound | Task 1, Task 8, Task 19, Task 25 | Task 19 test |

## Story-Specific Compliance Rules

- **API**: All new endpoints return standard envelope `{status, data, meta?, error?}`. Cursor-based pagination for list endpoints (`/sms/history`, `/webhooks/:id/deliveries`, `/reports/scheduled`, `/notification-templates`). All mutations create audit_log entry via `internal/audit` service.
- **DB**: Both up+down migrations required; RLS on all tenant-scoped tables (except global notification_templates); indexes on tenant_id + high-cardinality filter columns.
- **UI**: Design tokens ONLY (zero hex). Atomic reuse from `web/src/components/ui/*`. Turkish strings with proper diacritics (ç ğ ı ö ş ü). Skill frontend-design MUST be invoked for new pages.
- **Business**: Onboarding step N failure MUST NOT advance session (tx-scoped). Webhook secret NEVER returned in any API response after creation. SMS text stored as hash (GDPR minimisation) + 80-char preview only. Data portability archive lifecycle: 7-day signed URL, auto-purge S3 object after 30 days (reuse S3 lifecycle config — doc in CONFIG.md).
- **ADR**: ADR-003 (audit hash chain) — all new audit log entries continue chain. ADR-001 (multi-tenant isolation) — RLS policies mandatory on new tables. JWT + RBAC per STORY-068 hardening.

## Bug Pattern Warnings

- **PAT-001 [BR-test drift]**: If Task 15 changes notification dispatch behavior, grep for `*_br_test.go` and `service_br_test.go` in `internal/notification/` and `internal/compliance/` — update assertions to match new preference-aware behavior.
- **PAT-002 [Utility drift]**: Task 14 webhook retry backoff + Task 20 scheduled-report next-run-time share cron parsing logic with `internal/job/scheduler.go#matchCronExpr`. Extract any new helper into `internal/job/cron_helpers.go` — do NOT duplicate `matchCronExpr` or `fieldMatches`.
- **PAT-003 [Turkish ASCII-only]**: Task 3 (seed) + Task 15 + Task 24 — Turkish body MUST contain proper characters. After impl, run `grep -E '[ç|ğ|ı|ö|ş|ü|Ç|Ğ|İ|Ö|Ş|Ü]' migrations/seed/004_notification_templates.sql` — must return HUNDREDS of matches (one per Turkish row). An empty result = ASCII-only violation.

## Tech Debt

No ROUTEMAP tech-debt items currently target STORY-069 (D-001/D-002 target STORY-056, D-003 target STORY-058, D-004/D-005 target STORY-059 and are already resolved per commit 34944a3 docs).

## Mock Retirement

No `src/mocks/` directory exists in this repo (backend-first project). N/A.

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| **Scope — 12 ACs, 28 tasks, L story is large for one wave** | Strict wave ordering + parallelism within waves. Gate on wave completion before next wave dispatch. |
| **`sims.owner_user_id` may not exist (needed by AC-9)** | Task 16 begins with `\d sims` check. If absent: add column `owner_user_id UUID REFERENCES users(id)` in Task 1 migration or fall back to tenant-level aggregation. Record decision in `docs/brainstorming/decisions.md`. |
| **Excelize v2 is a new dependency** | Task 10 pins v2.8.x. `go mod tidy` + `go build ./...` in task's verify step. |
| **Webhook retry scheduler running every minute may pile up jobs** | Dedup via `argus:cron:last:webhook_retry_sweep:*` redis key (existing pattern in scheduler.go). Processor batches deliveries with `LIMIT 100 FOR UPDATE SKIP LOCKED`. |
| **KVKK purge pseudonymisation is destructive** | Dry-run flag required in job payload; default production behavior: first run MUST be dry-run (documented in runbook). Audit log each run with counts. |
| **RLS on new tables may break existing test fixtures** | Task 2 RLS migration runs AFTER seed (Task 3) and existing tests are expected to set `argus.tenant_id` via store helper — if any test bypasses store layer, it fails and must be fixed in same task. |
| **Template rendering with Go text/template could leak secrets if event payload carries credentials** | Template system receives only sanitized `event.PublicFields` struct, NOT raw payload. Task 15 documents this contract + has a negative test. |
| **Onboarding tx can lock tenants row for extended time if step handlers are slow** | Each step handler has 10-second context timeout (ctx.WithTimeout). File upload step 5 uses existing bulk-import job (async), not in tx. |
| **Signed URLs for S3 must expire** | 7-day expiry hard-coded in Task 20 + Task 16. Hygiene cron to auto-delete objects after 30d via S3 lifecycle policy — Task 28 documents bucket policy. |

## Step Log

STEP_2 DEV TASK 15: EXECUTED | files=2+1 (handler, service, test) | endpoints=4 | result=PASS
STEP_2 DEV TASK 13: EXECUTED | files=4 (handler+test, webhook.go, HMAC doc) | endpoints=6 | result=PASS

