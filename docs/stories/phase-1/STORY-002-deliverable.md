# Deliverable: STORY-002 — Core Database Schema & Migrations

## Summary
Created all 25 database tables defined in the architecture docs via reversible golang-migrate migrations, including partitioning (SIMs by operator, audit/history by date), TimescaleDB hypertables (sessions, CDRs, operator health), continuous aggregates (hourly + daily CDR rollups), and idempotent seed data (admin user, demo tenant, mock operator).

## Files Changed

### New Files
| File | Description |
|------|-------------|
| `migrations/20260320000002_core_schema.up.sql` | All 25 tables with columns, constraints, indexes, partitions, triggers |
| `migrations/20260320000002_core_schema.down.sql` | Reverse: drops all tables in dependency order |
| `migrations/20260320000003_timescaledb_hypertables.up.sql` | Hypertable conversion + compression + retention policies |
| `migrations/20260320000003_timescaledb_hypertables.down.sql` | Reverse: removes policies and indexes |
| `migrations/20260320000004_continuous_aggregates.up.sql` | cdrs_hourly + cdrs_daily materialized views with refresh policies |
| `migrations/20260320000004_continuous_aggregates.down.sql` | Reverse: drops views |
| `migrations/seed/001_admin_user.sql` | Demo tenant + super admin user (admin@argus.io / admin) |
| `migrations/seed/002_system_data.sql` | Mock operator + operator grant + SIM partition for mock |

### Removed Files
| File | Reason |
|------|--------|
| `migrations/20260319000001_sim_segments.up.sql` | Orphaned index-only migration; table + index now in core schema |
| `migrations/20260319000001_sim_segments.down.sql` | Paired down migration |

## Architecture References Fulfilled
- TBL-01 through TBL-25 (all tables from docs/architecture/db/)
- SEED-01: Admin user per docs/architecture/db/_index.md
- SEED-02: System data per docs/architecture/db/_index.md
- TimescaleDB hypertables per docs/architecture/db/aaa-analytics.md and operator.md
- Continuous aggregates per docs/architecture/db/aaa-analytics.md

## Test Scenarios Covered
- Fresh migration creates all tables without errors
- Seeds create admin user and system data
- Migration down reverses all migrations cleanly
- Re-running migrations is idempotent (IF NOT EXISTS)
- Re-running seeds is idempotent (ON CONFLICT DO NOTHING)
- SIM partition for mock operator created correctly
- All existing Go tests still pass (14/14 packages)

## Key Decisions
- `idx_api_keys_active` partial index uses `WHERE revoked_at IS NULL` instead of `WHERE ... expires_at > NOW()` because NOW() is not IMMUTABLE for index predicates
- `idx_sims_iccid` and `idx_sims_imsi` include `operator_id` because PostgreSQL requires unique indexes on partitioned tables to include all partition key columns
- `esim_profiles.sim_id` FK cannot reference partitioned `sims` table directly; the UNIQUE constraint on esim_profiles.sim_id ensures one-to-one relationship
- Default partition `sims_default` created to catch any operator_id not in explicit partition list
- `updated_at` trigger function created for automatic timestamp updates on 7 tables
