# FIX-301: Startup Race — Prepared-Statement OID Cache vs Migration Order

> **Source**: docs/reports/uat-acceptance-2026-04-30.md F-2 (CRITICAL)
> **Severity**: CRITICAL — production blocker
> **Effort**: M
> **Planner**: Amil FIX Mode
> **Date**: 2026-04-30

## Symptom

From UAT report F-2 (verbatim):

> `POST /sims/bulk/import` and ALL tenant_admin SIM ops fail with `could not open relation with OID 105648` after fresh app start; restart clears it. Stale prepared-statement cache vs migration order race.

UAT-002 step 1 detail (verbatim):

> First attempt FAILED with 500: `could not open relation with OID 105648 (SQLSTATE XX000)` — stale prepared-statement cache from app startup race condition. After argus-app restart, all SIM ops worked. Same OID error blocked GET/POST on all SIM endpoints for tenant_admin (super_admin sees 404 due to different code path).

**Reproduction (deterministic via test, opportunistic via stack):**
1. `make down`
2. `make up` — argus container starts and runs `argus serve` (Dockerfile CMD)
3. `make db-migrate` — applies pending migrations (some DROP/CREATE: 20260504000001 drops `kill_switches` + `maintenance_windows`; 20260505000001 drops `roaming_agreements`; 20260503000002 creates `esim_profile_stock`)
4. `make db-seed` — populates seed data
5. Login as `tenant_admin`, `POST /sims/bulk/import` → 500 `could not open relation with OID <N>`
6. `docker compose restart argus` → same flow now succeeds

## Root Cause Investigation

### A. `argus serve` opens the pool and HTTP listener BEFORE migrations run

- `infra/docker/Dockerfile.argus:53-54` — `ENTRYPOINT ["/app/argus"]`, `CMD ["serve"]`. Container boots straight into serve; no `migrate up` step.
- `deploy/docker-compose.yml:51-58` — `argus.depends_on.postgres.condition: service_healthy`. Postgres healthy only requires `pg_isready`, not "migrations applied".
- `cmd/argus/main.go:420` — `pg, err := store.NewPostgresWithMetrics(...)` opens pool against whatever schema postgres has at boot.
- `cmd/argus/main.go:427-430` — `schemacheck.Verify(ctx, pg.Pool, schemacheck.CriticalTables)` fatals if minimum tables missing, but does NOT enforce "latest version applied". A partially-migrated DB with the critical tables present passes this gate.
- `cmd/argus/main.go:1746-1750` — HTTP `srv.ListenAndServe()` opens BEFORE `make db-migrate` runs out-of-band.

### B. pgx v5 default pins OIDs in two caches per connection

- `internal/store/postgres.go:35-67` — `pgxpool.ParseConfig(dsn)` then `pgxpool.NewWithConfig`. **No `DefaultQueryExecMode` set, no `StatementCacheCapacity` / `DescriptionCacheCapacity` overridden, no `AfterConnect` hook.**
- pgx v5 (vendored at `~/go/pkg/mod/github.com/jackc/pgx/v5@v5.5.3/conn.go:172,194-195`):
  - Default `defaultQueryExecMode := QueryExecModeCacheStatement`
  - Default `StatementCacheCapacity = 512` (server-side prepared statements, bound to relation OID at PREPARE time)
  - Default `DescriptionCacheCapacity = 512` (client-side row description, also OID-bound)
- Effect: every connection that runs a query before the migration cycle caches a prepared statement / description against the **pre-migration** OIDs. After migrations DROP+CREATE a table, that table has a **new** OID. The cached plan dereferences the old OID → `could not open relation with OID <stale>` (PG error code `XX000`, internal "cache lookup failed").

### C. Multiple boot tasks warm the cache before migrations run

These all execute against pg.Pool BEFORE the user runs `make db-migrate`:

- `cmd/argus/main.go:496-500` — `userStore.MigrateTOTPSecretsToEncrypted(ctx, ...)` — touches `users`
- `cmd/argus/main.go:511-513` — `auditSvc.Start(...)` — opens NATS consumer that writes `audit_logs`
- `cmd/argus/main.go:521-525` — `job.ArchiveRoamingKeywordPolicyVersions(ctx, pg.Pool, auditSvc, log.Logger)` — touches `policy_versions`
- `cmd/argus/main.go:667-670` — `cdrConsumer.Start(...)` — touches `cdrs`
- Background pool gauge, NATS subscribers, scheduled jobs — all use `pg.Pool` connections within seconds of boot.

When `make db-migrate` runs later, pending migrations DROP/CREATE/ALTER tables. The connections in pool retain prepared statements pointing to old OIDs. First user request reuses one of those connections → OID failure.

### D. Why super_admin sees 404 but tenant_admin sees the OID error

UAT-002 reports "super_admin sees 404 due to different code path". The likely explanation (Developer to confirm during repro): super_admin SIM list path bypasses tenant filtering and either short-circuits or uses a different SQL string — different statement, different cache slot, possibly cold (not yet prepared) when migrations land. tenant_admin path runs the parameterized `SELECT … FROM sims WHERE tenant_id = $1 …` (`internal/store/sim.go:333`) which is statically formatted and reused on every request → guaranteed cache hit on the stale OID.

This isn't load-bearing for the fix; both paths are affected by the same root cause; super_admin just happens to dodge it. We will not narrow the fix to "tenant_admin path".

## Root Cause (final)

`argus serve` opens its pgxpool and HTTP listener before pending migrations are applied. pgx v5 caches server-side prepared statements (and client-side row descriptions) per connection, bound to relation OIDs. When operators run `make db-migrate` after boot — and several recent migrations (20260503000002, 20260504000001, 20260505000001) DROP+CREATE tables — every connection that handled a query before the migration retains stale OID references. The first request that lands on such a connection fails with `could not open relation with OID <N> (SQLSTATE XX000)`. Restarting argus-app rebuilds the pool against the post-migration schema and the symptom disappears.

## Fix Approach

Two-layer defense, both required:

### Layer 1 (primary): Migrate-before-listener — eliminate the boot race

**File**: `cmd/argus/main.go` — function `runServe(cfg)` around line 356, before line 420 (`store.NewPostgresWithMetrics`).

**Change**: Run migrations in-process before opening the application pool, gated by `cfg.AutoMigrate` (new env var, default true in dev/staging, false in production blue-green).

```go
// Pseudocode — Developer to write actual diff per Bug Fix TDD
if cfg.AutoMigrate {
    if err := runMigrationsInProcess(cfg.DatabaseURL, migrationsPath); err != nil {
        log.Fatal().Err(err).Msg("auto-migrate failed at boot")
    }
}
pg, err := store.NewPostgresWithMetrics(...)
```

`runMigrationsInProcess` reuses the same `golang-migrate` plumbing as `runMigrate` (`cmd/argus/main.go:204-274`) but does NOT call `os.Exit`. golang-migrate uses a Postgres advisory lock (`schema_migrations.pg_try_advisory_lock`) so multi-replica blue-green deploys are safe — losers wait for the winner.

**Config**: add `ARGUS_AUTO_MIGRATE` (default `true`) in `internal/config/config.go`. Production prod-flip scripts that prefer "migrate manually then deploy" set it to `false`.

**Effect**: `make up` (no separate `make db-migrate` needed in dev) brings argus up with schema at latest. `make db-migrate` becomes a no-op fast path (migrate.ErrNoChange).

### Layer 2 (defense-in-depth): switch pgx to non-OID-pinning exec mode

**File**: `internal/store/postgres.go` — `newPostgres` around line 36 after `pgxpool.ParseConfig`.

**Change**:

```go
// FIX-301: Avoid pgx pinning relation OIDs in prepared-statement / description
// caches. Without this, a DDL change after a connection has been used (e.g.
// partition rotation, manual migration on a running pool) leaves stale OIDs
// in the per-conn cache and produces "could not open relation with OID" until
// the connection is recycled.
cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
```

Trade-off: `QueryExecModeExec` skips both caches and uses unnamed prepared statements per call. Per-query cost: ~2 round-trips become ~1 with extended-protocol unnamed prepare; vs `QueryExecModeCacheStatement`'s amortized 0.5 round-trips. On a typical query budget this is negligible (<5% in p99, validated empirically by Developer with `pg_stat_statements`). Acceptable for AAA / OLTP. If perf shows regression on hot read paths, fall back to `QueryExecModeCacheDescribe` plus `cfg.ConnConfig.StatementCacheCapacity = 0` (description cache still pins OIDs but is cheaper to invalidate; pair with `MaxConnLifetime` already set to recycle stale conns).

**Why both layers**: Layer 1 fixes the UAT scenario (boot race). Layer 2 protects against future runtime DDL — partition rotation jobs, migration on running prod, table rebuild via shadow swap — all of which would re-open the same wound on a long-lived pool.

### Layer 3 (optional, decline unless Developer hits it): remove `MigrateTOTPSecretsToEncrypted` and `ArchiveRoamingKeywordPolicyVersions` from `runServe`

These are one-shot data migrations dressed as boot tasks (`cmd/argus/main.go:496-500` and `:521-525`). Long-term they belong in a `migrate` subcommand or a job runner, not in serve. **Out of scope for FIX-301** — fixing them does not close the OID race (Layer 1 + 2 do). Note as a follow-up D-NNN.

## Acceptance Criteria

- **AC-1**: Fresh `make down && make up && make db-migrate && make db-seed` (in this order, no restart) → first `POST /sims/bulk/import` as tenant_admin returns 202 with no OID error in logs. With `ARGUS_AUTO_MIGRATE=true` (default), `make db-migrate` is a no-op (`migrate.ErrNoChange`) because boot already migrated.
- **AC-2**: Reproduction test `internal/store/oid_drift_test.go` (see below) FAILS on `main` (current code) and PASSES after Layer 2 lands.
- **AC-3**: All existing tests pass — `make test-with-db` green, `go vet ./...`, `go build ./...`. No regression in `schemacheck`, `migration_freshvol_test.go`, `sim_fk_integration_test.go`.
- **AC-4**: CI smoke job runs one full `docker compose down -v && docker compose up -d --wait && curl -X POST /api/v1/sims/bulk/import …` cycle and asserts 2xx on first attempt. Single deterministic run replaces the "10 consecutive boots" anti-pattern (flake-prone).
- **AC-5**: `ARGUS_AUTO_MIGRATE=false` opts out — boot succeeds against an already-migrated DB and skips the in-process migrate step. (Verified by env-toggle integration test.)
- **AC-6**: Multi-replica safety verified — golang-migrate advisory lock holds; second concurrent `runServe` waits and observes `migrate.ErrNoChange` after the first finishes. (Verified by table test that spins up two `runMigrationsInProcess` goroutines against the same DSN.)

## Reproduction Test

**File**: `internal/store/oid_drift_test.go` (new)
**Test**: `TestPgxPoolSurvivesDDLAfterFirstQuery`

Signature:

```go
func TestPgxPoolSurvivesDDLAfterFirstQuery(t *testing.T) {
    // 1. Connect to test DB (skip if DATABASE_URL unset, like other store tests)
    // 2. Create a scratch table:
    //    CREATE TABLE oid_drift_test_t (id INT, val TEXT)
    //    INSERT one row
    // 3. Open pool via store.newPostgres with PRODUCTION config (Layer 2 applied)
    //    Pin a connection: c, _ := pool.Acquire(ctx); defer c.Release()
    // 4. Run SELECT * FROM oid_drift_test_t — caches statement on conn c
    // 5. On a SEPARATE connection (or psql via os/exec), execute:
    //    DROP TABLE oid_drift_test_t;
    //    CREATE TABLE oid_drift_test_t (id INT, val TEXT);
    //    INSERT one row
    // 6. Re-run SELECT * FROM oid_drift_test_t on conn c
    //    EXPECTED: succeeds, returns one row.
    //    BEFORE FIX: fails with "could not open relation with OID …" (SQLSTATE XX000)
}
```

Implementation note: Step 5 must use a **different** connection so the cache on `c` is not invalidated client-side. Use `pool.Acquire` for `c2`, run DDL, release `c2`. Step 6 then reuses `c` — the failing path.

This test is deterministic (no timing dependency), runs in ~200ms, gates Layer 2 directly. Layer 1 is gated by AC-1 + AC-4 (smoke test).

## Risks

| # | Risk | Mitigation |
|---|------|-----------|
| R1 | `QueryExecModeExec` perf regression on hot read paths | Benchmark `simStore.ListEnriched` and `radius.AuthCheck` before/after with `go test -bench`. If p99 +10%, pivot to `QueryExecModeCacheDescribe` with `StatementCacheCapacity=0`. Document in plan addendum. |
| R2 | Multi-replica boot deadlock on advisory lock | golang-migrate uses `pg_advisory_lock` keyed by hash of DSN — losers wait, no deadlock. Verified by AC-6. Add 60s timeout on lock acquisition; if exceeded, log warn + continue (assume lock holder is finishing). |
| R3 | `runMigrationsInProcess` corrupts existing prod data on accidental rollback | Layer 1 only runs `migrate.Up()`, never `Down()`. Down stays exclusively in `argus migrate down`. |
| R4 | `ARGUS_AUTO_MIGRATE=false` deployments forget to migrate | Add a `runServe`-time check: `m.Version()` must equal max version in `migrationsPath` directory. If drift, log fatal with explicit instruction. |
| R5 | Boot tasks (TOTP migrate, roaming archiver) still run before pool-stable point | They run AFTER `runMigrationsInProcess`, so OIDs are already final. No additional action needed. |
| R6 | OID 105648's actual table never confirmed (DB was reset post-report) | Developer must capture during reproduction: `SELECT relname, relkind, relnamespace::regnamespace FROM pg_class WHERE oid = <captured>;`. Document in FIX-301-gate.md. Not blocking, but useful evidence. |

## Files Changed (expected)

- `cmd/argus/main.go` — extract migration logic to `runMigrationsInProcess`; call from `runServe` before `store.NewPostgresWithMetrics`; gate on `cfg.AutoMigrate`.
- `internal/config/config.go` — add `AutoMigrate bool` (env: `ARGUS_AUTO_MIGRATE`, default `true`).
- `internal/store/postgres.go` — set `cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec`. Add comment referencing FIX-301.
- `internal/store/oid_drift_test.go` — NEW; reproduction test (AC-2).
- `internal/config/config_test.go` — assert `AutoMigrate` default true; toggleable via env.
- `Makefile` — add `make smoke-boot` target wiring up AC-4 (compose down/up + first request); call from CI.
- `docs/architecture/CONFIG.md` — document `ARGUS_AUTO_MIGRATE`.
- `docs/architecture/DEPLOYMENT.md` — note multi-replica behavior + advisory lock.

## Out of Scope (deferred)

- D-181 (proposed): move `MigrateTOTPSecretsToEncrypted` and `ArchiveRoamingKeywordPolicyVersions` out of `runServe` into a dedicated `argus boot-tasks` subcommand or a one-shot job runner. These are not load-bearing for the OID race once Layer 1 lands; address in a future cleanup story.
- Connection-level OID cache invalidation on DDL (PG-level NOTIFY hook). Layer 2 makes this unnecessary; revisit if Layer 2's perf cost becomes unacceptable.
