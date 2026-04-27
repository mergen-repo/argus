# Database Schema Index — Argus

> Database: PostgreSQL 16 + TimescaleDB extension
> Migration tool: golang-migrate
> Naming: snake_case, plural table names

## Tables

| ID | Table | Domain | Key Relationships | Partitioned |
|----|-------|--------|-------------------|-------------|
| TBL-01 | tenants | Platform | Root entity | No |
| TBL-02 | users | Platform | → TBL-01 (tenant_id) | No |
| TBL-03 | user_sessions | Platform | → TBL-02 (user_id) | No |
| TBL-04 | api_keys | Platform | → TBL-01 (tenant_id) | No |
| TBL-05 | operators | Operator | System-level (no tenant_id). FIX-215 added `sla_latency_threshold_ms INTEGER NOT NULL DEFAULT 500 CHECK (BETWEEN 50 AND 60000)` — per-operator latency breach threshold (ms). | No |
| TBL-06 | operator_grants | Operator | → TBL-01, → TBL-05 | No |
| TBL-07 | apns | SIM/APN | → TBL-01, → TBL-05 | No |
| TBL-08 | ip_pools | IPAM | → TBL-01, → TBL-07 | No |
| TBL-09 | ip_addresses | IPAM | → TBL-08, → TBL-10 | No |
| TBL-10 | sims | SIM/APN | → TBL-01, → TBL-05, → TBL-07, → TBL-15. Phase 11 (STORY-094): added device-binding columns — `bound_imei VARCHAR(15) NULL`, `binding_mode VARCHAR(20) NULL CHECK (binding_mode IN ('strict','allowlist','first-use','tac-lock','grace-period','soft'))`, `binding_status VARCHAR(20) NULL CHECK (binding_status IN ('verified','pending','mismatch','unbound','disabled'))`, `binding_verified_at TIMESTAMPTZ NULL`, `last_imei_seen_at TIMESTAMPTZ NULL`, `binding_grace_expires_at TIMESTAMPTZ NULL`. Partial index `idx_sims_binding_mode` ON (binding_mode) WHERE binding_mode IS NOT NULL. Migration default: `binding_mode = NULL` for all existing rows (zero-risk opt-in per ADR-004). See ADR-004. | By operator_id |
| TBL-11 | sim_state_history | SIM/APN | → TBL-10 | By created_at |
| TBL-12 | esim_profiles | eSIM | → TBL-10 | No |
| TBL-13 | policies | Policy | → TBL-01 | No |
| TBL-14 | policy_versions | Policy | → TBL-13. FIX-231: `policy_active_version` partial unique (policy_id) WHERE state='active' — at most one active version per policy. | No |
| TBL-15 | policy_assignments | Policy | → TBL-10, → TBL-14. FIX-231: **canonical source of truth** for active policy per SIM; trigger `trg_sims_policy_version_sync` propagates changes to `sims.policy_version_id` (sole writer). FIX-233: `stage_pct INT NULL` column added (migration `20260429000001`); NULL for legacy rows; composite index `idx_policy_assignments_rollout_stage` on `(rollout_id, stage_pct)` supports SIM list cohort filter. FIX-234: `coa_status` VARCHAR(20) CHECK constraint `chk_coa_status` enforces 6-state canonical set (`pending`,`queued`,`acked`,`failed`,`no_session`,`skipped`); partial index `idx_policy_assignments_coa_failed_age` on `(coa_status, coa_sent_at) WHERE coa_status='failed'` for alerter sweep. Migration `20260430000001_coa_status_enum_extension`. | No |
| TBL-16 | policy_rollouts | Policy | → TBL-13 (policy_id added FIX-231), → TBL-14. FIX-231: `policy_active_rollout` partial unique (policy_id) WHERE state IN ('pending','in_progress') — at most one active rollout per policy; INSERT violation maps to 422 `ROLLOUT_IN_PROGRESS`. FIX-232: `aborted_at TIMESTAMPTZ` column (migration 20260428000001) — set when admin aborts an in-progress rollout via `POST /policy-rollouts/{id}/abort`; does NOT revert assignments. Partial index `idx_policy_rollouts_aborted_at WHERE aborted_at IS NOT NULL`. | No |
| TBL-17 | sessions | AAA | → TBL-10, → TBL-05 | By started_at (TimescaleDB) |
| TBL-18 | cdrs | Analytics | → TBL-17 | By timestamp (TimescaleDB) |
| TBL-19 | audit_logs | Audit | → TBL-01 | By created_at |
| TBL-20 | jobs | Jobs | → TBL-01 | No |
| TBL-21 | notifications | Notification | → TBL-01, → TBL-02 | No |
| TBL-22 | notification_configs | Notification | → TBL-01 | No |
| TBL-23 | operator_health_logs | Operator | → TBL-05 | By checked_at (TimescaleDB) |
| TBL-24 | msisdn_pool | SIM/APN | → TBL-01, → TBL-05 | No |
| TBL-25 | sim_segments | SIM/APN | → TBL-01, → TBL-02 | No |
| TBL-26 | ota_commands | OTA | → TBL-01, → TBL-10, → TBL-20, → TBL-02 | No |
| TBL-27 | sla_reports | Analytics/SLA | → TBL-01 (tenant_id), → TBL-05 (operator_id nullable). Retention: 24 months minimum (no cleanup cron). Per FIX-215 compliance requirement (AC-7). | No |
| TBL-28 | anomalies | Analytics/Anomalies | → TBL-01 (tenant_id), → TBL-10 (sim_id nullable) | No |
| TBL-29 | policy_violations | Policy Engine | → TBL-01, → TBL-10, → TBL-13, → TBL-14 | No |
| TBL-30 | s3_archival_log | Platform Services | → TBL-01 (tenant_id) | No |
| TBL-31 | tenant_retention_config | Platform Services | → TBL-01 (tenant_id, UNIQUE) | No |
| TBL-32 | backup_runs | Backup | System-level (no tenant_id) | No |
| TBL-33 | backup_verifications | Backup | → TBL-32 (backup_run_id) | No |
| TBL-34 | password_history | Auth/Security | → TBL-02 (user_id via JOIN to users.tenant_id for RLS) | No |
| TBL-35 | user_backup_codes | Auth/Security | → TBL-02 (user_id via JOIN to users.tenant_id for RLS) | No |
| TBL-36 | onboarding_sessions | Onboarding | → TBL-01 (tenant_id); RLS enabled | No |
| TBL-37 | scheduled_reports | Reporting | → TBL-01 (tenant_id); RLS enabled | No |
| TBL-38 | webhook_configs | Notifications | → TBL-01 (tenant_id); secret AES-GCM encrypted; RLS enabled | No |
| TBL-39 | webhook_deliveries | Notifications | → TBL-38 (webhook_config_id); retry state machine; RLS enabled | No |
| TBL-40 | notification_preferences | Notifications | → TBL-01 (tenant_id); event_type × channel matrix; RLS enabled | No |
| TBL-41 | notification_templates | Notifications | Global (no tenant_id); locale-keyed TR/EN; 28 seed rows | No |
| TBL-42 | sms_outbound | SMS Gateway | → TBL-01 (tenant_id); body stored as SHA-256 hash + 80-char preview (GDPR); RLS enabled. Original STORY-069 migration (20260413000001) included an unsatisfiable FK to partitioned `sims(id)` which blocked table creation; repair migration 20260417000004 restores the table without the FK (sim_id is enforced in application code), matching STORY-064's precedent for partitioned-sims references. | No |
| TBL-43 | roaming_agreements | Operator | → TBL-01 (tenant_id), → TBL-05/TBL-06 (operator_id); partial unique index on (tenant_id, operator_id) WHERE state='active'; RLS enabled | No |
| TBL-44 | anomaly_comments | Analytics/Anomalies | → TBL-28 (anomaly_id), → TBL-02 (author_id); body varchar(2000); index on (anomaly_id, created_at DESC); RLS via app.current_tenant | No |
| TBL-45 | kill_switches | Admin/Operations | System-level (no tenant_id); key + enabled + reason + toggled_by + toggled_at; 5 canonical keys seeded | No |
| TBL-46 | maintenance_windows | Admin/Operations | → TBL-01 (tenant_id nullable — may be system-wide); affected_services JSONB; notify_plan JSONB; RLS enabled | No |
| TBL-47 | announcements | Admin/Ops | → TBL-01 (tenant_id nullable for target='all'); severity + starts_at + ends_at + dismissible; RLS where scoped | No |
| TBL-48 | announcement_dismissals | Admin/Ops | → TBL-47 (announcement_id), → TBL-02 (user_id); unique(user_id, announcement_id) | No |
| TBL-49 | chart_annotations | Analytics | → TBL-01 (tenant_id), → TBL-02 (author_id); chart_key + timestamp + label + severity + body; RLS enabled | No |
| TBL-50 | user_views | Platform/UX | → TBL-02 (user_id); page + name + filters_json; partial unique index (user_id,page) WHERE is_default=true | No |
| TBL-51 | user_column_preferences | Platform/UX | → TBL-02 (user_id); page + preferences_json (density, columns, language) | No |
| TBL-52 | session_quarantine | AAA/Data Integrity | Plain (non-hypertable) quarantine store; original_table CHECK ('sessions','cdrs'); quarantined_at, reason, raw_data JSONB; system-level (no tenant_id). Migration A (20260421000001). FIX-207 AC-6. | No |
| TBL-53 | alerts | Analytics/Alerts | Unified alert history: operator, infra, policy, SIM-level. 3 CHECK constraints (severity 5-val, state 4-val, source 5-val), 7 indices + 2 FIX-210 indices, RLS tenant-scoped. Retention 180d via `alerts_retention` cron. FIX-209. Extended by FIX-210: +4 columns (`occurrence_count INT DEFAULT 1`, `first_seen_at TIMESTAMPTZ`, `last_seen_at TIMESTAMPTZ`, `cooldown_until TIMESTAMPTZ NULL`), partial unique index `idx_alerts_dedup_unique` on `(tenant_id, dedup_key) WHERE state IN ('open','acknowledged','suppressed')`, partial lookup index `idx_alerts_cooldown_lookup` on resolved rows with non-null `cooldown_until`. | No |
| TBL-54 | password_reset_tokens | Auth | → TBL-02 (user_id CASCADE); platform-global (no tenant_id) | No |
| TBL-55 | alert_suppressions | Analytics/Alerts | → TBL-01 (tenant_id CASCADE), → TBL-02 (created_by nullable); scope_type CHECK ('this','type','operator','dedup_key'); partial unique index on (tenant_id, rule_name) WHERE rule_name IS NOT NULL; RLS tenant-scoped. rule_name NULL = ad-hoc mute (AC-1); NOT NULL = saved rule (AC-5). FIX-229 (DEV-333). | No |
| TBL-56 | imei_whitelist | Device Identity / IMEI | → TBL-01 (tenant_id CASCADE), → TBL-02 (created_by nullable). Columns: `id UUID PK`, `tenant_id UUID NOT NULL`, `kind VARCHAR(15) NOT NULL CHECK (kind IN ('full_imei','tac_range'))`, `imei_or_tac VARCHAR(15) NOT NULL`, `device_model VARCHAR(255) NULL`, `description TEXT NULL`, `created_by UUID NULL`, `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`. UNIQUE (tenant_id, imei_or_tac). Index (tenant_id, kind). RLS tenant-scoped. Phase 11 STORY-095. ADR-004. | No |
| TBL-57 | imei_greylist | Device Identity / IMEI | Same shape as TBL-56 + additional `quarantine_reason TEXT NOT NULL`. Greylist = "allow but log/alert"; pre-check produces `device.imei_in_pool('greylist')=true` for matching auths. Phase 11 STORY-095. | No |
| TBL-58 | imei_blacklist | Device Identity / IMEI | Same shape as TBL-56 + additional `block_reason TEXT NOT NULL`, `imported_from VARCHAR(20) NOT NULL CHECK (imported_from IN ('manual','gsma_ceir','operator_eir'))`. Hard-deny — pre-check returns Access-Reject when `device.imei_in_pool('blacklist')=true`. Phase 11 STORY-095. | No |
| TBL-59 | imei_history | Device Identity / IMEI | → TBL-01 (tenant_id CASCADE), → TBL-10 (sim_id CASCADE). Append-only IMEI observation log per SIM. Columns: `id UUID PK`, `tenant_id UUID`, `sim_id UUID`, `observed_imei VARCHAR(15)`, `observed_software_version VARCHAR(2) NULL`, `observed_at TIMESTAMPTZ NOT NULL`, `capture_protocol VARCHAR(20) NOT NULL CHECK (capture_protocol IN ('radius','diameter_s6a','5g_sba'))`, `nas_ip_address INET NULL`, `was_mismatch BOOLEAN NOT NULL DEFAULT FALSE`, `alarm_raised BOOLEAN NOT NULL DEFAULT FALSE`. Index (sim_id, observed_at DESC). RLS tenant-scoped. Phase 11 STORY-094 — drives API-330 imei-history endpoint and forensic re-pair flow. | No |
| TBL-60 | sim_imei_allowlist | Device Identity / IMEI | → TBL-10 (sim_id CASCADE). SIM-allowlist mode join table — list of additional IMEIs accepted for a SIM under `binding_mode='allowlist'`. Columns: `sim_id UUID NOT NULL`, `imei VARCHAR(15) NOT NULL`, `added_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `added_by UUID NULL FK users`. PK (sim_id, imei). RLS via `sims` lookup (sim_id resolves tenant). Phase 11 STORY-094. | No |
| TBL-61 | syslog_destinations | Logging / SIEM | → TBL-01 (tenant_id CASCADE), → TBL-02 (created_by nullable). Configurable RFC 3164 / 5424 forward destinations. Columns: `id UUID PK`, `tenant_id UUID NOT NULL`, `name VARCHAR(255) NOT NULL`, `host VARCHAR(255) NOT NULL`, `port INT NOT NULL`, `transport VARCHAR(10) NOT NULL CHECK (transport IN ('udp','tcp','tls'))`, `format VARCHAR(10) NOT NULL CHECK (format IN ('rfc3164','rfc5424'))`, `filter_categories TEXT[] NOT NULL` (subset of `auth,audit,alert,policy,imei,system`), `enabled BOOLEAN NOT NULL DEFAULT TRUE`, `last_delivery_at TIMESTAMPTZ NULL`, `last_error TEXT NULL`, `created_by UUID NULL`, `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`. UNIQUE (tenant_id, name). Index (tenant_id, enabled). RLS tenant-scoped. Phase 11 STORY-098. | No |

## Domain Detail Files

| Domain | File | Tables |
|--------|------|--------|
| Platform (Tenant, User, API Key) | [platform.md](platform.md) | TBL-01, TBL-02, TBL-03, TBL-04, TBL-34, TBL-35 |
| Operator | [operator.md](operator.md) | TBL-05, TBL-06, TBL-23, TBL-43 |
| SIM & APN | [sim-apn.md](sim-apn.md) | TBL-07, TBL-08, TBL-09, TBL-10, TBL-11, TBL-12, TBL-24, TBL-25 |
| Policy | [policy.md](policy.md) | TBL-13, TBL-14, TBL-15, TBL-16 |
| AAA & Analytics | [aaa-analytics.md](aaa-analytics.md) | TBL-17, TBL-18, TBL-27, TBL-28 |
| Audit, Jobs, Notifications, OTA | [platform-services.md](platform-services.md) | TBL-19, TBL-20, TBL-21, TBL-22, TBL-26, TBL-29, TBL-30, TBL-31 |
| Backup | [platform-services.md](platform-services.md) | TBL-32, TBL-33 |
| UX / Personalization (STORY-077) | (no dedicated file yet) | TBL-47, TBL-48, TBL-49, TBL-50, TBL-51 |
| Device Identity / IMEI (Phase 11) | (no dedicated file yet) | TBL-56, TBL-57, TBL-58, TBL-59, TBL-60 |
| Logging / SIEM (Phase 11) | (no dedicated file yet) | TBL-61 |

## Entity Relationship Diagram

```
┌──────────┐    ┌──────────────┐    ┌──────────────┐
│ TBL-01   │◀──┐│ TBL-02       │    │ TBL-04       │
│ tenants  │   ││ users        │    │ api_keys     │
│          │───┘│              │    │              │
│          │◀───│ tenant_id    │    │ tenant_id ──▶│
└────┬─────┘    └──────────────┘    └──────────────┘
     │
     │ tenant_id on all tenant-scoped tables
     │
     ├──────────────────────┬────────────────────┐
     │                      │                    │
     ▼                      ▼                    ▼
┌──────────┐    ┌──────────────┐    ┌──────────────┐
│ TBL-07   │    │ TBL-13       │    │ TBL-22       │
│ apns     │    │ policies     │    │ notif_configs │
│          │    │              │    │              │
│ op_id ──▶│    │              │    └──────────────┘
└────┬─────┘    └──────┬───────┘
     │                 │
     │                 ▼
     │          ┌──────────────┐
     │          │ TBL-14       │
     │          │ policy_vers  │
     │          └──────┬───────┘
     │                 │
     ▼                 ▼
┌──────────────────────────────┐    ┌──────────────┐
│ TBL-10: sims                 │───▶│ TBL-12       │
│ tenant_id, operator_id,      │    │ esim_profiles│
│ apn_id, policy_version_id,   │    └──────────────┘
│ ip_address_id, state         │
└──────┬───────┬───────────────┘
       │       │
       │       ▼
       │ ┌──────────────┐
       │ │ TBL-11       │
       │ │ sim_state_   │
       │ │ history      │
       │ └──────────────┘
       │
       ▼
┌──────────────┐    ┌──────────────┐
│ TBL-17       │───▶│ TBL-18       │
│ sessions     │    │ cdrs         │
│ (TimescaleDB)│    │ (TimescaleDB)│
└──────────────┘    └──────────────┘

┌──────────┐    System-level (no tenant_id)
│ TBL-05   │◀──┐
│ operators │   │ ┌──────────────┐
│          │───┘ │ TBL-06       │
│          │◀────│ operator_    │
└──────────┘     │ grants       │
                 │ tenant_id ──▶ TBL-01
                 └──────────────┘

┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ TBL-19       │  │ TBL-20       │  │ TBL-21       │
│ audit_logs   │  │ jobs         │  │ notifications│
│ (partitioned)│  │ (NATS-backed)│  │              │
└──────────────┘  └──────────────┘  └──────────────┘
```

## Seed Files

### SEED-01: Super Admin Account
- File: `migrations/seed/001_admin_user.sql`
- Email: `admin@argus.io`
- Password: `admin` (bcrypt hashed)
- Role: super_admin
- Idempotent: INSERT ... ON CONFLICT DO NOTHING

### SEED-02: System Initial Data
- File: `migrations/seed/002_system_data.sql`
- Default operator: Mock Simulator (for development)
- Default tenant: "Argus Demo" (for development)
- SIM states enum values
- RAT type enum values
- Default notification event types
- Default rate limit presets
- Idempotent: all entries use ON CONFLICT DO NOTHING

### SEED-06: Dynamic IP Reservation (STORY-092 D1-A)
- File: `migrations/seed/006_reserve_sim_ips.sql`
- Extended 2026-04-18 by STORY-092 to materialise `ip_addresses` rows for seed 003's 13 APN pools + the previously-missing `m2m.water` pool (14 pools total).
- Materialises 700 `ip_addresses` rows across all pools (state=`available` on first run).
- Reservation CTE: issues a deterministic reservation for 129/129 active + APN-assigned SIMs from seed 003/005 — each gets one `ip_addresses` row flipped to `allocated` + `sim.ip_address_id` set. The original reservation for seed 005's 16 SIMs is preserved.
- Idempotent: every `INSERT` uses `WHERE NOT EXISTS`; every reservation update uses `WHERE state='available'`. Second run produces zero mutations.
- Fail-fast guard at end: `DO $$ RAISE EXCEPTION $$` block asserts the reservation count matches the expected 129 before committing — catches silent seed regressions on fresh volumes.
- Interaction with D-032 chain: guards all seed-005-specific pool INSERTs with `WHERE EXISTS` so seed-003-only databases (missing m2m.health / m2m.industrial) don't FK-fail.

## Migration Convention
- Directory: `migrations/`
- Format: `YYYYMMDDHHMMSS_description.up.sql` / `YYYYMMDDHHMMSS_description.down.sql`
- Tool: golang-migrate
- All migrations reversible (up + down)
- D-032 remediation chain: STORY-069 original (`20260413000001`) → STORY-086 repair (`20260417000004`) → STORY-087 pre-069 shim (`20260412999999`).
