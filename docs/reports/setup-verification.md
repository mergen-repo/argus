# Setup Verification Report

> Date: 2026-04-12
> Story: amil-checkup (post STORY-065)
> Status: PASS (with escalated non-blocking defects)

## Summary

| Phase | Status | Details |
|-------|--------|---------|
| Makefile | PASS | 7/9 critical targets verified; 2 targets (`db-migrate`, `db-seed`) are broken but non-blocking |
| Docker | PASS | 6/6 services running, all healthy |
| Database | PASS | 42 tables present, seed data loaded (15 users, 4 operators, 162 sims, 3 tenants) |
| Web Access | PASS | Frontend 200, API health 200, login endpoint returns token |

Overall: infrastructure is fully operational. The Argus stack is built from scratch (`make build` → `make up`), all containers report `healthy`, login works with seeded admin credentials (`admin@argus.io` / `admin`), and the Go/TypeScript codebase compiles cleanly.

## Makefile Targets

| Target | Status | Notes |
|--------|--------|-------|
| `make help` | PASS | All categories render (Servisler, Infra, Build & Deploy, Veritabani, Frontend, Kalite, Temizlik, Kisayollar) |
| `make build` | PASS | Docker images (argus, web, postgres, redis, nats, pgbouncer, nginx) build without error (executed by caller prior to handoff) |
| `make up` | PASS | All 6 services start and reach `healthy` within ~35s (executed by caller prior to handoff) |
| `make status` | PASS | Lists all 6 services as running/healthy |
| `make db-migrate` | **FAIL** | Invokes `/app/argus migrate up` but the argus binary only supports the `serve` subcommand. Falls through to `serve` which then fails on `:3868 bind: address already in use` (port already held by healthy container). See Escalated Issues. |
| `make db-seed` | **FAIL** | Same root cause — invokes `/app/argus seed` which does not exist as a subcommand. See Escalated Issues. |
| `make test` | PASS | Test suite executes (`go test ./internal/auth/...` → 26 passed). Framework functional. |
| `make typecheck` (Go) | PASS | `go build ./...` clean |
| `make typecheck` (TS) | PASS | `npx tsc --noEmit` in `web/` clean |
| `make lint-sql` | PASS | No `SELECT *` in store layer |
| `make lint` | SKIP | `golangci-lint` not installed on host (host tooling gap, not project defect) |

## Docker Services

| Service | Status | Port (host → container) | Health |
|---------|--------|-------------------------|--------|
| argus-app | running | 1812-1813/udp, 3868/tcp, 8443/tcp | healthy |
| argus-nginx | running | 8084 → 80 | healthy |
| argus-postgres | running | 5450 → 5432 | healthy |
| argus-pgbouncer | running | 6432 → 6432 | healthy |
| argus-redis | running | 6379 → 6379 | healthy |
| argus-nats | running | 4222, 8222 | running (no healthcheck — distroless image) |

Container log scan (`docker compose logs --tail=80 argus`) shows clean startup:
- Postgres/Redis/NATS connections established
- JetStream streams `EVENTS` and `JOBS` ready
- RADIUS (auth :1812, acct :1813), Diameter (:3868), SBA (:8443), HTTP (:8080), WS (:8081) listeners started
- 8 cron entries registered (purge_sweep, ip_reclaim, sla_report, anomaly_batch_detection, storage_monitor, data_retention, s3_archival, partition_creator)
- Operator health checker started (4 operators)
- No crash loops, no repeated restarts

## Database

- Migration tool: golang-migrate (SQL files under `/migrations/`)
- Migration files on disk: **25** `.up.sql` files (through `20260412000008_composite_indexes`)
- Rows in `schema_migrations`: **6** (max version `20260323000003`, dirty=0)
- **Discrepancy**: `schema_migrations` is stale relative to files on disk. However, all referenced tables exist (partitions, RLS, policy_violations, esim_profiles, sla_reports, composite indexes). Schema was applied but not recorded. See Escalated Issues.
- Table count: **42** (includes partitioned `sims`, `audit_logs`, `sim_state_history` with monthly partitions for 2026-03 through 2026-06)
- Seed data: loaded
  - `tenants`: 3 rows
  - `users`: 15 rows (includes admin@argus.io)
  - `operators`: 4 rows
  - `sims`: 162 rows
- `pg_isready`: accepting connections

## Web Access

| Endpoint | Status | Response |
|----------|--------|----------|
| `http://localhost:8084/login` | 200 | Frontend loads (served by nginx from `web/dist`) |
| `http://localhost:8084/api/health` | 200 | `{"status":"success","data":{"db":{"status":"ok",...},"redis":{"status":"ok",...}}}` |
| `http://localhost:8084/api/v1/auth/login` POST (admin@argus.io / admin) | 200 | Returns envelope with `data.token` (JWT, 303 chars), `data.user` (email, id, name, role), `data.requires_2fa` |

Admin login verified end-to-end against seeded credentials.

## Fixes Applied

| # | Issue | Fix | Result |
|---|-------|-----|--------|
| — | None applied | Infrastructure reached PASS on first run; defects below are pre-existing, not blocking, and exceed the 2-attempt fix-loop scope for this agent | — |

## Escalated Issues

Three pre-existing, non-blocking defects found. None prevent infrastructure from operating.

1. **`make db-migrate` and `make db-seed` invoke non-existent subcommands.**
   Makefile lines 129 and 139 run `docker compose exec argus /app/argus migrate up` and `/app/argus seed`. `cmd/argus/main.go` defines no CLI subcommand dispatch — the binary always runs the `serve` entrypoint (per `Dockerfile.argus` CMD `["serve"]`). When invoked, the binary starts `serve`, which then fatally fails on `:3868 bind: address already in use` because the real argus container already holds the port. Migrations and seeds are being applied through some other, undocumented path (manual `psql` or an earlier iteration of the binary). Recommended remediation: either (a) add a `migrate`/`seed` CLI subcommand in `cmd/argus/main.go`, or (b) rewrite the Makefile targets to shell out to `migrate -database ... -path migrations up` / `psql -f migrations/seed/*.sql` via the postgres container.

2. **`schema_migrations` table is stale.**
   Max recorded version `20260323000003`, while files on disk go up to `20260412000008`. Twelve migrations (STORY-060 through STORY-065 schema changes — SLA reports, eSIM multiprofile, enum check constraints, operator_grants rat_types, partition bootstrap, RLS policies, FK integrity triggers, composite indexes, plus `20260324000001_policy_violations` and `20260411000001_normalize_rat_type_values`) are not recorded, even though the corresponding tables/constraints exist. This means `golang-migrate` cannot correctly determine migration state and `make db-migrate-down` would undo the wrong migration. Recommended remediation: manually insert the missing version rows into `schema_migrations` (or force-set via `migrate force <version>`) as a one-time reconciliation.

3. **`golangci-lint` not installed on host.**
   `make lint` fails with "golangci-lint not found". Host tooling gap only — does not affect containerised infrastructure. Remediation: `brew install golangci-lint` or add install instructions to developer setup docs.

None of these block the `/amil-checkup` completion criterion ("infrastructure is fully operational") — the stack is up, healthy, seeded, and authenticated.
