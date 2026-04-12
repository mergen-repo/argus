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

## Domain Detail Files

| Domain | File | Tables |
|--------|------|--------|
| Platform (Tenant, User, API Key) | [platform.md](platform.md) | TBL-01, TBL-02, TBL-03, TBL-04 |
| Operator | [operator.md](operator.md) | TBL-05, TBL-06, TBL-23 |
| SIM & APN | [sim-apn.md](sim-apn.md) | TBL-07, TBL-08, TBL-09, TBL-10, TBL-11, TBL-12, TBL-24, TBL-25 |
| Policy | [policy.md](policy.md) | TBL-13, TBL-14, TBL-15, TBL-16 |
| AAA & Analytics | [aaa-analytics.md](aaa-analytics.md) | TBL-17, TBL-18, TBL-27, TBL-28 |
| Audit, Jobs, Notifications, OTA | [platform-services.md](platform-services.md) | TBL-19, TBL-20, TBL-21, TBL-22, TBL-26, TBL-29, TBL-30, TBL-31 |

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
