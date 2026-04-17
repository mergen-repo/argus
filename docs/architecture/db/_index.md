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
| TBL-05 | operators | Operator | System-level (no tenant_id) | No |
| TBL-06 | operator_grants | Operator | → TBL-01, → TBL-05 | No |
| TBL-07 | apns | SIM/APN | → TBL-01, → TBL-05 | No |
| TBL-08 | ip_pools | IPAM | → TBL-01, → TBL-07 | No |
| TBL-09 | ip_addresses | IPAM | → TBL-08, → TBL-10 | No |
| TBL-10 | sims | SIM/APN | → TBL-01, → TBL-05, → TBL-07, → TBL-15 | By operator_id |
| TBL-11 | sim_state_history | SIM/APN | → TBL-10 | By created_at |
| TBL-12 | esim_profiles | eSIM | → TBL-10 | No |
| TBL-13 | policies | Policy | → TBL-01 | No |
| TBL-14 | policy_versions | Policy | → TBL-13 | No |
| TBL-15 | policy_assignments | Policy | → TBL-10, → TBL-14 | No |
| TBL-16 | policy_rollouts | Policy | → TBL-14 | No |
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
| TBL-27 | sla_reports | Analytics/SLA | → TBL-01 (tenant_id), → TBL-05 (operator_id nullable) | No |
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

## Migration Convention
- Directory: `migrations/`
- Format: `YYYYMMDDHHMMSS_description.up.sql` / `YYYYMMDDHHMMSS_description.down.sql`
- Tool: golang-migrate
- All migrations reversible (up + down)
