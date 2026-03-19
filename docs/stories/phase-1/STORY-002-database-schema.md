# STORY-002: Core Database Schema & Migrations

## User Story
As a developer, I want the foundational database tables created via reversible migrations, so that all domain entities have storage.

## Description
Create all 24 tables defined in the DB architecture, with proper indexes, constraints, foreign keys, partitioning (SIMs by operator, audit by date, sessions/CDRs as TimescaleDB hypertables), and seed data.

## Architecture Reference
- Database: All tables TBL-01 through TBL-24
- Source files:
  - docs/architecture/db/_index.md
  - docs/architecture/db/platform.md (TBL-01 to TBL-04)
  - docs/architecture/db/operator.md (TBL-05, TBL-06, TBL-23)
  - docs/architecture/db/sim-apn.md (TBL-07 to TBL-12, TBL-24)
  - docs/architecture/db/policy.md (TBL-13 to TBL-16)
  - docs/architecture/db/aaa-analytics.md (TBL-17, TBL-18)
  - docs/architecture/db/platform-services.md (TBL-19 to TBL-22)

## Screen Reference
- None (database only)

## Acceptance Criteria
- [ ] All 24 tables created with exact column definitions from architecture docs
- [ ] All indexes created with specified names
- [ ] SIM table (TBL-10) partitioned by operator_id (list partition)
- [ ] sim_state_history (TBL-11) partitioned by created_at (range, monthly)
- [ ] audit_logs (TBL-19) partitioned by created_at (range, monthly)
- [ ] sessions (TBL-17) as TimescaleDB hypertable on started_at
- [ ] cdrs (TBL-18) as TimescaleDB hypertable on timestamp
- [ ] operator_health_logs (TBL-23) as TimescaleDB hypertable on checked_at
- [ ] CDR continuous aggregates created (hourly + daily)
- [ ] All foreign key constraints in place
- [ ] All migrations have both up.sql and down.sql
- [ ] SEED-01: Super admin user (admin@argus.io / admin)
- [ ] SEED-02: Mock operator, demo tenant, enum values, default rate limits
- [ ] `make db-migrate` and `make db-seed` execute cleanly
- [ ] Seeds are idempotent (safe to re-run)

## Database Changes
- Migration: `20260318000002_core_schema.up.sql` (all tables)
- Migration: `20260318000003_timescaledb_hypertables.up.sql`
- Migration: `20260318000004_continuous_aggregates.up.sql`
- Seed: `001_admin_user.sql`
- Seed: `002_system_data.sql`

## Dependencies
- Blocked by: STORY-001 (Docker + PG must be running)
- Blocks: STORY-003, STORY-004, and all domain stories

## Test Scenarios
- [ ] Fresh `make db-migrate` creates all tables without errors
- [ ] `make db-seed` creates admin user and system data
- [ ] `make db-migrate-down` reverses all migrations cleanly
- [ ] Re-running migrations is idempotent
- [ ] Re-running seeds is idempotent (ON CONFLICT DO NOTHING)
- [ ] Partition creation for SIM table works for mock operator

## Effort Estimate
- Size: L
- Complexity: High
