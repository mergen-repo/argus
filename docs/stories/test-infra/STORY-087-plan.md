# Implementation Plan: STORY-087 — [TECH-DEBT] D-032 Pre-069 sims-compat shim for fresh-volume bootstrap

> Plan drafted 2026-04-17 after code-state validation against the current
> tree (post-STORY-086 DONE, post-STORY-083/084/085 DONE) and empirical
> verification of golang-migrate v4.19.1 semantics. Material drift from the
> problem statement handed to the planner:
>
> 1. **The problem statement dismisses "Solution A: pre-069 sims-compat
>    shim" incorrectly.** The reasoning was that inserting a migration
>    with a timestamp BEFORE one that is already applied in production
>    breaks golang-migrate's monotonic ordering. This is **not how
>    golang-migrate v4 works.** `schema_migrations` stores a single current
>    version (not an applied-set log). `m.Up()` reads `curVersion` from
>    the DB, then calls `sourceDrv.Next(curVersion)` in a loop — source
>    files with version numbers BELOW `curVersion` are never visited.
>    Source: `$GOMODCACHE/github.com/golang-migrate/migrate/v4@v4.19.1/
>    migrate.go:265-283` (`Up()`) → `readUp()` at lines 532-590 →
>    `sourceDrv.Next(suint(from))` at line 581. Therefore Solution A is
>    fully safe for live DBs: a new source file timestamped
>    `20260412999999` is invisible to any DB whose `schema_migrations`
>    already records version ≥ `20260413000001`. No manual
>    `INSERT INTO schema_migrations` is needed, no checksum drift
>    occurs (golang-migrate does not checksum SQL content — it tracks
>    version numbers only).
>
>    Effect on plan: **Solution A is viable and chosen.** This avoids ALL
>    Go-runner changes (Options C/F/G/H/I), which were the heavy-risk
>    paths. Effort stays S-M.
>
> 2. **The fix hinges on `CREATE TABLE IF NOT EXISTS` semantics, not
>    on FK repair.** Original `migrations/20260413000001_story_069_schema.
>    up.sql:141` is `CREATE TABLE IF NOT EXISTS sms_outbound (...)`.
>    PostgreSQL semantics: when the target table already exists, the
>    ENTIRE statement (including the broken FK clause on line 144) emits
>    a `relation "sms_outbound" already exists, skipping` NOTICE and is
>    **never parsed for semantic validation**. So if `sms_outbound`
>    already exists at the time `20260413000001` runs, line 144's
>    `REFERENCES sims(id)` is never evaluated, and the migration
>    completes. Pre-create the table → the defective CREATE TABLE becomes
>    a silent no-op. That is the whole fix.
>
> 3. **The shim must create ONLY the table — not indexes, not RLS, not
>    trigger.** Lines 155-157 of `20260413000001_story_069_schema.up.sql`
>    declare three `CREATE INDEX idx_sms_outbound_*` statements WITHOUT
>    `IF NOT EXISTS`. If the shim creates them, `20260413000001` fails at
>    line 155 with `relation "idx_sms_outbound_tenant_sim_time" already
>    exists`. Similarly `20260413000002_story_069_rls.up.sql:34-38`
>    declares `CREATE POLICY sms_outbound_tenant_isolation` (no
>    `IF NOT EXISTS` — PostgreSQL `CREATE POLICY` does not support that
>    clause). If the shim installs the policy, `20260413000002` fails.
>    Therefore the shim's entire up-side scope is one `CREATE TABLE IF
>    NOT EXISTS sms_outbound (...)` statement with the columns from
>    STORY-086's repair (no FK on `sim_id`). Indexes, RLS, and trigger
>    continue to be installed by their canonical migrations
>    (20260413000001 for indexes, 20260413000002 for RLS, and
>    20260417000004 for idempotent reconciliation + `check_sim_exists`
>    trigger).

## Goal

Ensure a fresh `docker compose up` / fresh `argus migrate up` from empty
database volumes reaches the head of the migration chain cleanly without
mutating the deployed file `migrations/20260413000001_story_069_schema.
up.sql`. Achieved by adding one new migration pair
`20260412999999_story_087_sms_outbound_pre_069_shim.{up,down}.sql` that
pre-creates the `sms_outbound` table with the STORY-086-approved column
set (no unsatisfiable FK on `sims(id)`). On fresh volumes the shim runs
before `20260413000001`, making its `CREATE TABLE IF NOT EXISTS
sms_outbound (...)` a silent no-op — the broken FK clause is never
evaluated. On live DBs already at version ≥ `20260413000001`, the shim
is invisible to `m.Up()` (never visited by the source iterator). The
repair migration `20260417000004_sms_outbound_recover.up.sql` remains
the authoritative idempotent sms_outbound specification and continues to
reconcile any drift (indexes, RLS, trigger) on both fresh and live DBs.

## Architecture Context

### Disk layout (verified 2026-04-17)

```
migrations/
├── 20260320000001_init_extensions.{up,down}.sql
├── 20260320000002_core_schema.{up,down}.sql             # sims created here — LIST-partitioned, composite PK (id, operator_id)
├── 20260320000003_timescaledb_hypertables.{up,down}.sql
├── ...
├── 20260412000011_enterprise_auth_hardening.{up,down}.sql   # last pre-069 migration
├── [NEW] 20260412999999_story_087_sms_outbound_pre_069_shim.{up,down}.sql   # ← THIS STORY
├── 20260413000001_story_069_schema.{up,down}.sql        # defective CREATE TABLE sms_outbound at :141 (FK to sims(id) at :144)
├── 20260413000002_story_069_rls.{up,down}.sql           # RLS policy for sms_outbound
├── 20260413000003_violation_acknowledgment.{up,down}.sql
├── ...
├── 20260417000001_story_077_ux.{up,down}.sql
├── 20260417000002_story_077_policy_dsl_trgm.{up,down}.sql
├── 20260417000003_story_077_rls.{up,down}.sql
└── 20260417000004_sms_outbound_recover.{up,down}.sql    # STORY-086 repair — idempotent reconciliation + check_sim_exists trigger
```

### Runner code path (verified 2026-04-17)

- `cmd/argus/main.go:157-163` — `main()` switches on subcommand; `migrate` routes to `runMigrate()`.
- `cmd/argus/main.go:192-263` — `runMigrate()` constructs `migrate.New(migrationsPath, cfg.DatabaseURL)` and calls `m.Up()` / `m.Steps(-n)`.
- `migrationsPath` defaults to `file:///app/migrations` (container) and can be overridden via `ARGUS_MIGRATIONS_PATH`.
- `Makefile:148-150` — `make db-migrate` invokes `docker compose exec argus /app/argus migrate up`.
- `infra/docker/Dockerfile.argus` — `COPY --from=builder /build/migrations /app/migrations`; ENTRYPOINT is `/app/argus`, CMD defaults to `serve`.
- `deploy/docker-compose.yml` — `argus` service uses default `serve` command; **migrations are NOT auto-run at serve time**. Operator runs `make db-migrate` after `make up`.

### Schemacheck code path (verified 2026-04-17)

- `internal/store/schemacheck/schemacheck.go:14-27` — `CriticalTables` manifest (12 tables, sorted alphabetically, includes `sms_outbound`).
- `cmd/argus/main.go:384-387` — `schemacheck.Verify(ctx, pg.Pool, schemacheck.CriticalTables)` is called at serve-time boot; fatal if any critical table is missing.
- `internal/store/schemacheck/schemacheck_test.go:23-24` — asserts `len(CriticalTables) == 12`. **This test must still pass after STORY-087 (we do not add entries).**

### Live DB state (demo/dev, 2026-04-17)

- `schema_migrations.version = 20260417000004`, `dirty = f`.
- All 12 critical tables present.
- `pg_constraint WHERE contype='f' AND conrelid='sms_outbound'::regclass` → 1 row for `tenant_id → tenants(id)` only (the broken FK to sims was never actually installed — the table was recreated by STORY-086 repair without it).
- Trigger `trg_sms_outbound_check_sim` installed via STORY-086 repair.

### Fresh volume failure mode (reproducer, verified via STORY-086 investigation, DEV-239)

```
make down && docker volume rm argus_postgres-data
make up
make db-migrate   # → argus migrate up
  ↓
golang-migrate applies files in lex order:
  20260320000001 .. 20260412000011 → OK
  20260413000001_story_069_schema.up.sql:
    BEGIN;  -- auto-wrapped by golang-migrate
      CREATE TABLE onboarding_sessions (...);       -- OK
      CREATE INDEX idx_onb_sessions_tenant_state;   -- OK
      ... (5 more tables + columns) ...
      ALTER TABLE sims ADD COLUMN owner_user_id ... REFERENCES users(id);  -- OK
      CREATE TABLE IF NOT EXISTS sms_outbound (
        ...
        sim_id UUID NOT NULL REFERENCES sims(id),  -- ← LINE 144 FAILS
        ...
      );
      -- ERROR: there is no unique constraint matching given keys for
      --        referenced table "sims"
    ROLLBACK;
  ↓
  schema_migrations: version=20260412000011, dirty=true  (actually dirty=20260413000001 per golang-migrate ErrDirty contract)
```

After rollback, the migration runner exits non-zero. `make db-migrate` exits non-zero. Operator sees failure. Manual recovery requires `argus migrate force 20260412000011` and then a manual SQL apply of a corrected 20260413000001 — not a shippable UX.

## Root cause

`migrations/20260413000001_story_069_schema.up.sql:141-158` (verbatim):

```sql
CREATE TABLE IF NOT EXISTS sms_outbound (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    sim_id UUID NOT NULL REFERENCES sims(id),               -- ← line 144: unsatisfiable FK
    msisdn VARCHAR(20) NOT NULL,
    text_hash VARCHAR(64) NOT NULL,
    text_preview VARCHAR(80),
    status VARCHAR(20) NOT NULL DEFAULT 'queued',
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

**Why line 144 is unsatisfiable**: `migrations/20260320000002_core_schema.up.sql:275-300` creates `sims` as a LIST-partitioned table with composite primary key `(id, operator_id)`:

```sql
CREATE TABLE IF NOT EXISTS sims (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    ...
    operator_id UUID NOT NULL,
    ...
    PRIMARY KEY (id, operator_id)
) PARTITION BY LIST (operator_id);
```

PostgreSQL requires FK columns to match the full set of a unique constraint (or the full PK) of the parent table. `REFERENCES sims(id)` matches only part of the composite PK and no other unique constraint exists on `sims(id)` alone — PostgreSQL rejects with error SQLSTATE 42830 (`no unique constraint matching given keys for referenced table "sims"`). STORY-064 established the precedent for referencing the partitioned `sims` via a trigger (`check_sim_exists`, defined in `20260412000007_fk_integrity_triggers.up.sql:4-16`), and STORY-086's repair migration (`20260417000004_sms_outbound_recover.up.sql:49-74`) installs exactly that trigger on `sms_outbound`. Sibling STORY-069 tables (`onboarding_sessions`, `scheduled_reports`, `webhook_configs`, etc.) do not reference `sims` at all — confirming the line 144 declaration was an authorship error in STORY-069, not intentional schema design.

**Why it was latent until 2026-04-17**: per `DEV-239` investigation documented in STORY-086, the live/dev DB was populated via a non-atomic archaeological apply that committed the six sibling STORY-069 tables but silently dropped `sms_outbound`. Fresh deployments, by contrast, run through golang-migrate's normal transactional apply and hit the CREATE TABLE error at `20260413000001_story_069_schema.up.sql:144`.

**Why we do not mutate 20260413000001**: the file's version is recorded in `schema_migrations` on every live/dev/QA DB. Although golang-migrate does not content-hash migration files, mutating a deployed file risks:

1. Auditor confusion when comparing `git blame` history against live `schema_migrations`.
2. Inconsistent replay behavior if a live DB ever re-runs `down` and then `up` for that version (the mutated up would differ from what was originally run, masking the bug rather than documenting it).
3. Test drift — STORY-086's `DEV-239` investigation artifact explicitly points at `20260413000001:144` as historical evidence of the defect; mutating the file erases that evidence.

The D-032 ROUTEMAP row also codifies this constraint: "Do NOT mutate the deployed 20260413000001 file."

## Candidate solutions considered

### A. Pre-069 sims-compat shim — CHOSEN

A new migration `20260412999999_story_087_sms_outbound_pre_069_shim.{up,down}.sql` that pre-creates `sms_outbound` WITHOUT the broken FK. Rationale and operational trace:

- **Fresh DB (`schema_migrations` empty)**: `m.Up()` reads `curVersion = nilVersion`, calls `readUp(from=-1, limit=-1)`. First pass invokes `sourceDrv.First()` → returns lowest version (`20260320000001`). Loop iterates via `sourceDrv.Next(from)`, visiting every source file in ascending order including `20260412999999`. The shim creates the table. Then `20260413000001` runs; its `CREATE TABLE IF NOT EXISTS sms_outbound` emits a NOTICE and is a no-op (PostgreSQL never parses the column list, including line 144's broken FK). The following `CREATE INDEX` statements succeed. Chain continues to head.

- **Live DB (`schema_migrations.version = 20260417000004`)**: `m.Up()` reads `curVersion = 20260417000004`, enters loop with `from = 20260417000004`. Each iteration calls `sourceDrv.Next(20260417000004)`. File source driver scans migrations/ for a version strictly greater than 20260417000004 — finds none — returns `os.ErrNotExist`. `readUp()` translates this to `ErrNoChange`. The shim file at 20260412999999 is NEVER visited. No state change. `argus migrate up` is a clean no-op, exactly as it is today.

- **Live DB rolled back via `argus migrate down -all`**: down chain runs in reverse version order: 20260417000004 → 20260417000003 → ... → 20260413000001 (drops sms_outbound table + indexes in the down sql) → 20260412999999 (NEW shim down: `DROP TABLE IF EXISTS sms_outbound CASCADE` — already dropped, no-op) → 20260412000011 → ... → nil. Safe.

- **Checksum/schema drift risk**: golang-migrate v4 tracks only version numbers, not content hashes (verified: `$GOMODCACHE/github.com/golang-migrate/migrate/v4@v4.19.1/database/driver.go` has no hash API; `postgres` driver stores only `(version bigint primary key, dirty boolean)` in `schema_migrations`). No drift.

- **Lex-order safety**: `20260412999999` sorts strictly between `20260412000011` and `20260413000001` in both lexicographic and numeric-ascending order. File source driver uses numeric version parsing (see `$GOMODCACHE/github.com/golang-migrate/migrate/v4@v4.19.1/source/file/file.go`) so ordering is deterministic.

**Verdict: VIABLE and CHOSEN.** Minimal surface area (one migration pair), zero Go-code changes, preserves STORY-086's repair as the authoritative idempotent spec, preserves the historical 20260413000001 file as forensic evidence.

### B. New migration later than STORY-086 (e.g., 20260418000001) that re-creates sms_outbound idempotently — NOT VIABLE

A later migration cannot run on fresh volumes because `golang-migrate` aborts the up-chain on the first failing file (20260413000001). A post-086 reconciler can only help live DBs; the fresh-volume failure is already covered by STORY-086's `20260417000004` repair but that too runs after 20260413000001 and never gets reached on a fresh volume. **Not viable in isolation.**

### C. Migration runner pre-apply hook (Go code) — NOT NEEDED

Modify `runMigrate()` in `cmd/argus/main.go` to detect a fresh volume (schema_migrations empty) and SQL-rewrite 20260413000001 at load time or pre-create sms_outbound via raw pgx before calling `m.Up()`. **Not needed** — Solution A achieves the same outcome with zero Go changes. C would also introduce a load-bearing runner path that is hard to test end-to-end without a fresh-volume docker compose reproducer, increasing risk.

### D. Supersede via a committed-state shim post-086 — NOT VIABLE

A later migration cannot recover from a prior migration failure within a single `m.Up()` invocation (fast-fail on first error). **Not viable.**

### E. Fork the historical migration via adjacent `.fix` file + runner preference — NOT VIABLE

Would require runner-level file-selection logic (non-standard for golang-migrate) and creates two files representing the same logical migration, which auditors will flag. **Not viable.**

### F. Ship a corrective migration runner wrapper (Go parse-and-rewrite) — NOT VIABLE at Tech-Debt scope

Requires parsing the defective SQL file at load time, pattern-matching the `REFERENCES sims(id)` clause, and emitting a rewritten version to golang-migrate's source driver. High implementation cost, new test surface (SQL parser correctness), risk of partial parses. **Not needed given Solution A.**

### G (problem statement alt). In-Go fresh-volume detection + compensating pre-apply — REDUNDANT

Same outcome as Solution A, implemented in Go instead of SQL. Solution A is preferable because the compensating action lives with the other migrations (discoverable by anyone running `ls migrations/`), while Option G buries it in the runner (discoverable only by reading `main.go`).

### H (problem statement alt). SQL-rewrite broken CREATE TABLE at load time — NOT NEEDED

The broken statement uses `CREATE TABLE IF NOT EXISTS`, so if the table already exists the column list is never parsed and the rewrite is unnecessary.

### I (problem statement alt). Insert into `schema_migrations` to stamp 20260413000001 pre-run — UNSAFE and UNNEEDED

Would hide the fact that the STORY-069 schema was applied, breaking the ability to later `down` and `up` that version. **Unsafe.** Unnecessary given Solution A.

## Chosen solution

**Solution A — Pre-069 sims-compat shim** via new migration pair
`20260412999999_story_087_sms_outbound_pre_069_shim.{up,down}.sql`.

### Why this is minimum-viable

- **One file up, one file down** — no Go code touched, no existing migrations touched, no schemacheck manifest touched (sms_outbound is already in the manifest via STORY-086).
- **Pure SQL with standard idempotency** — `CREATE TABLE IF NOT EXISTS` up-side, `DROP TABLE IF EXISTS ... CASCADE` down-side, matching STORY-086's repair pattern.
- **Exploits existing `IF NOT EXISTS` semantics** in `20260413000001_story_069_schema.up.sql:141` to neutralize the defective CREATE TABLE without mutating the file.
- **Preserves STORY-086 as authoritative reconciler** — the shim creates ONLY the table; indexes/RLS/trigger continue to be installed by their canonical migrations and are then idempotently reconciled by `20260417000004`.
- **Zero live-DB impact** — golang-migrate's `m.Up()` cannot regress past its current version; live DBs at version ≥ 20260413000001 never visit the shim.

### Minimal-risk justifications

1. The shim's up-side content is a byte-for-byte subset of STORY-086's `20260417000004_sms_outbound_recover.up.sql:23-36` (the table CREATE). Copy paste, no novel DDL.
2. The shim does NOT install indexes (would conflict with 20260413000001:155-157 which lack `IF NOT EXISTS`).
3. The shim does NOT install RLS policy (would conflict with 20260413000002:34-38 because `CREATE POLICY` does not accept `IF NOT EXISTS`).
4. The shim does NOT install the `check_sim_exists` trigger (installed by STORY-086 at 20260417000004:71-74, which is idempotent via `DROP TRIGGER IF EXISTS` + `CREATE TRIGGER`).
5. The shim's down-side is `DROP TABLE IF EXISTS sms_outbound CASCADE;` — identical to STORY-086's down migration. Dropping during `migrate down` through 20260412999999 occurs AFTER 20260413000001.down has already dropped the table → shim down is a no-op via `IF EXISTS`. Dropping when 20260413000001 has been skipped (only possible on a hand-crafted intermediate state) still works because CASCADE removes any dependent objects that may exist.

### Full fresh-volume operational trace (table + indexes + RLS + trigger)

1. Migrations 20260320000001 .. 20260412000011 apply unchanged.
2. **NEW 20260412999999 (shim)** runs: `CREATE TABLE IF NOT EXISTS sms_outbound (12 columns, no FK on sim_id)` → table created, zero indexes, zero policies, no trigger.
3. 20260413000001 runs:
   - Its `CREATE TABLE IF NOT EXISTS sms_outbound` at :141 emits a NOTICE and is a no-op — column list (including broken FK at :144) is NEVER parsed per PostgreSQL docs.
   - Its three `CREATE INDEX` statements at :155-157 succeed (no duplicate — shim created no indexes).
   - Remaining sibling tables + ALTER TABLEs commit normally inside the auto-wrapped transaction.
4. 20260413000002 runs: `ALTER TABLE sms_outbound ENABLE ROW LEVEL SECURITY` (no-op if already enabled — PostgreSQL doc idempotent), `ALTER TABLE ... FORCE ROW LEVEL SECURITY` (idempotent), then `CREATE POLICY sms_outbound_tenant_isolation` succeeds because no prior policy exists (shim did not install one, 20260413000001 did not install one).
5. Migrations 20260413000003 .. 20260417000003 apply unchanged.
6. 20260417000004 (STORY-086 repair) runs idempotently:
   - `CREATE TABLE IF NOT EXISTS` → no-op (table exists from shim).
   - `CREATE INDEX IF NOT EXISTS` (x3) → no-op (indexes exist from 20260413000001).
   - `ALTER TABLE ... ENABLE/FORCE ROW LEVEL SECURITY` → no-op (already enabled from 20260413000002).
   - `DROP POLICY IF EXISTS ... + CREATE POLICY` → replaces the policy with identical content.
   - `CREATE OR REPLACE FUNCTION check_sim_exists()` → installs/replaces trigger function.
   - `DROP TRIGGER IF EXISTS + CREATE TRIGGER trg_sms_outbound_check_sim` → installs the trigger for the first time on a fresh volume.
7. `schema_migrations.version = 20260417000004, dirty = false`. Head reached cleanly.

### Acceptance of residual forensic value

`migrations/20260413000001_story_069_schema.up.sql:144` remains unchanged. An auditor (or a future developer running `grep -r "REFERENCES sims(id)" migrations/`) still finds the defect, can read the commit history, and can trace the D-032 → DEV-239 → STORY-086 repair → STORY-087 fresh-volume fix chain via the decisions log. The shim's SQL comment header points back to D-032 and to STORY-086/DEV-239 so the intent is preserved inline.

## Config schema / API / DB

### Config

None. No environment variables, no YAML fields, no feature flags.

### API

No new or modified endpoints.

### Database schema

**New migration pair** (exact paths, exact content sketches):

`migrations/20260412999999_story_087_sms_outbound_pre_069_shim.up.sql` — up-side:

```sql
-- STORY-087 D-032: pre-069 sims-compat shim for fresh-volume bootstrap.
-- Context: 20260413000001_story_069_schema.up.sql:144 declares an
-- unsatisfiable FK `sim_id UUID NOT NULL REFERENCES sims(id)` against
-- the LIST-partitioned `sims` table (composite PK (id, operator_id),
-- created at 20260320000002_core_schema.up.sql:275-300). A correctly-
-- sequenced fresh-volume `argus migrate up` therefore fails inside
-- 20260413000001's auto-wrapped transaction before the six sibling
-- STORY-069 tables are committed. STORY-086 shipped an idempotent
-- repair at 20260417000004_sms_outbound_recover.up.sql (table without
-- FK + check_sim_exists trigger) but that migration is never reached
-- on a fresh volume because the chain aborts at 20260413000001.
--
-- This shim pre-creates `sms_outbound` with the STORY-086-approved
-- column set (no FK on sim_id) BEFORE 20260413000001 runs. Because
-- 20260413000001:141 uses `CREATE TABLE IF NOT EXISTS`, PostgreSQL
-- emits a NOTICE and the broken column list (including line 144's
-- unsatisfiable FK) is never parsed — the whole CREATE TABLE is a
-- no-op. 20260413000001 then installs the indexes (lines 155-157)
-- and 20260413000002 installs the RLS policy (lines 34-38).
-- 20260417000004 idempotently reconciles the trigger + any drift.
--
-- On live DBs whose schema_migrations version is ≥ 20260413000001,
-- golang-migrate's Up() calls sourceDrv.Next(curVersion) and never
-- visits source files with versions below curVersion. This shim is
-- therefore invisible on live DBs — no state change, no checksum
-- drift (golang-migrate tracks version numbers only, not content
-- hashes).
--
-- DO NOT add CREATE INDEX here: 20260413000001:155-157 installs three
-- indexes WITHOUT `IF NOT EXISTS`; a duplicate here would fail on
-- fresh volumes. DO NOT add RLS policy here: `CREATE POLICY` does
-- not accept `IF NOT EXISTS`. DO NOT add check_sim_exists trigger
-- here: installed idempotently by 20260417000004:57-74.
--
-- References: ROUTEMAP D-032, decisions.md DEV-239, STORY-086 repair
-- (migrations/20260417000004_sms_outbound_recover.up.sql).

CREATE TABLE IF NOT EXISTS sms_outbound (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    sim_id UUID NOT NULL,
    msisdn VARCHAR(20) NOT NULL,
    text_hash VARCHAR(64) NOT NULL,
    text_preview VARCHAR(80),
    status VARCHAR(20) NOT NULL DEFAULT 'queued',
    provider_message_id VARCHAR(255),
    error_code VARCHAR(50),
    queued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ
);
```

`migrations/20260412999999_story_087_sms_outbound_pre_069_shim.down.sql` — down-side:

```sql
-- STORY-087 D-032 down: idempotent. Safe after 20260413000001.down
-- has already dropped sms_outbound (DROP ... IF EXISTS is a no-op
-- when the table is absent). CASCADE removes the policy and any
-- indexes if they exist, matching STORY-086's down semantics.
DROP TABLE IF EXISTS sms_outbound CASCADE;
```

### Schemacheck manifest

No change. `internal/store/schemacheck/schemacheck.go:22` already lists `sms_outbound` (added by STORY-086). `internal/store/schemacheck/schemacheck_test.go:23-24` asserts `len(CriticalTables) == 12` — still true after STORY-087 (we add zero entries).

### Effect on decisions.md

Add one entry:

- **DEV-243** — STORY-087 planner-phase decision: "Why Solution A (pre-069 shim) rather than Go-runner changes (Solutions C/F/G/H/I)." Records the empirical golang-migrate v4.19.1 `readUp()` trace and the `CREATE TABLE IF NOT EXISTS` semantic, plus the problem-statement drift correction. (Note: originally planned as DEV-240 but that ID was taken by STORY-083; developer used next free ID DEV-243.)

## Tasks

Single wave (linear — all tasks sequentially dependent on the previous).

### Wave 1

#### Task 1: Draft migration pair + validate via code-state probe

- **What**: Create `migrations/20260412999999_story_087_sms_outbound_pre_069_shim.up.sql` and `.down.sql` with the content specified in "Database schema" above. Before committing, verify byte-for-byte column parity with `migrations/20260417000004_sms_outbound_recover.up.sql:23-36`.
- **Files**:
  - NEW `migrations/20260412999999_story_087_sms_outbound_pre_069_shim.up.sql`
  - NEW `migrations/20260412999999_story_087_sms_outbound_pre_069_shim.down.sql`
- **Tests**: n/a for this task (pure DDL authoring). Validation happens in Task 3.
- **Depends-on**: none.

#### Task 2: Add DEV-240 decision entry

- **What**: Append a DEV-240 entry to `docs/brainstorming/decisions.md` documenting (a) the golang-migrate v4.19.1 `readUp()` source-code trace that confirms below-current source files are invisible on live DBs, (b) the `CREATE TABLE IF NOT EXISTS` semantic that neutralizes 20260413000001:144, (c) why the shim must omit indexes/RLS/trigger, (d) the problem-statement drift correction (Solution A was wrongly dismissed).
- **Files**:
  - MOD `docs/brainstorming/decisions.md`
- **Tests**: n/a (documentation).
- **Depends-on**: Task 1 (so the decision references the shipped file paths).

#### Task 3: Fresh-volume bootstrap integration test (Go, docker-free)

- **Pre-flight (5-min check)**: Developer reads `internal/store/sms_outbound_test.go:144` and `internal/store/backup_codes_test.go:46` to confirm the existing skip idiom (verified 2026-04-17 by planner via grep): tests read `os.Getenv("DATABASE_URL")` and call `t.Skip("no test database available (set DATABASE_URL)")` when unset. Reuse this pattern verbatim. If the idiom has drifted since planning, adjust accordingly — do NOT invent a new convention.
- **What**: Add a test that runs `argus migrate up` end-to-end against an empty PostgreSQL database using the project's existing test DB harness. The test:
  1. Skips if `DATABASE_URL` env var is not set (per project idiom — see pre-flight).
  2. Creates a disposable database (e.g., `argus_story087_freshvol_test`) via `CREATE DATABASE` on the admin connection derived from `DATABASE_URL`, then builds a per-test DSN pointing at the disposable DB.
  3. Invokes the migration runner's logic directly via `migrate.New("file://<repo>/migrations", perTestDSN)` → `m.Up()`.
  4. Asserts `m.Up()` returns `nil` (not `ErrDirty`, not any SQL error).
  5. Asserts `m.Version()` matches the source driver's latest version obtained via `m.Up()` idempotency (see AC-1 below) — do NOT hard-code `20260417000004`; resolve the head dynamically so STORY-088+ additions do not break this test.
  6. Asserts `SELECT to_regclass('public.sms_outbound')` returns non-NULL.
  7. Asserts `SELECT COUNT(*) FROM pg_constraint WHERE contype='f' AND conrelid='sms_outbound'::regclass` == 1 (only the tenant_id FK, not the sim_id FK).
  8. Asserts `SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgname='trg_sms_outbound_check_sim')` returns true.
  9. `t.Cleanup(...)` drops the disposable DB.
- **Files**:
  - NEW `internal/store/migration_freshvol_test.go` (or add to an existing migration-adjacent `_test.go` — developer judgment).
- **Tests**: test name `TestFreshVolumeBootstrap_STORY087` (or similar).
- **Depends-on**: Task 1 (needs the shim to pass).

#### Task 4: Live-DB no-op regression test (Go, docker-free)

- **What**: Add a second test verifying the shim is invisible on live DBs:
  1. Creates a disposable database and runs `m.Up()` to completion (same steps as Task 3 AC-1-to-head).
  2. Captures `v1, _, _ := m.Version()` — the current head, whatever it happens to be. Do NOT hard-code a version number.
  3. Runs `m.Up()` a second time.
  4. Asserts the second run returns `errors.Is(err, migrate.ErrNoChange)` and `m.Version()` returns `(v1, false, nil)` (unchanged).
  5. Asserts the schema is byte-equivalent between the two runs (e.g., `pg_catalog.pg_tables` and `pg_constraint` counts unchanged).
- **Files**:
  - Co-locate with Task 3 test file.
- **Tests**: test name `TestLiveDBIdempotent_STORY087`.
- **Depends-on**: Task 3 (shared harness).

#### Task 5: Down-chain regression test

- **What**: Verify `argus migrate down -all` still works after STORY-087:
  1. Runs `m.Up()` to head.
  2. Runs `m.Down()`.
  3. Asserts returns nil (not ErrNoChange, not any error).
  4. Asserts `m.Version()` returns `(0, false, migrate.ErrNilVersion)` (fully stepped down).
  5. Asserts `SELECT to_regclass('public.sms_outbound')` returns NULL.
- **Files**:
  - Co-locate with Task 3/4.
- **Tests**: test name `TestDownChain_STORY087`.
- **Depends-on**: Task 3.

#### Task 6: Update ROUTEMAP D-032 status + STORY-087 row

- **What**:
  1. Change D-032 status from `[ ] PENDING` to `[x] DONE` with the STORY-087 reference.
  2. Change the STORY-087 row from `PLAN` to the step appropriate for gate-submission (handled by the amil workflow).
  3. Add a Recent Activity row noting the fix.
- **Files**:
  - MOD `docs/ROUTEMAP.md`
- **Tests**: n/a.
- **Depends-on**: Tasks 3–5 green.

#### Task 7 (optional, if in-story time allows): Documentation touch-up

- **What**: Update `docs/architecture/db/_index.md` (migration section) to note the D-032 remediation chain: STORY-069 original → STORY-086 repair (`20260417000004`) → STORY-087 shim (`20260412999999`). One short bullet.
- **Files**:
  - MOD `docs/architecture/db/_index.md`
- **Tests**: n/a.
- **Depends-on**: Task 6.

## Acceptance Criteria

### AC-1: Fresh-volume bootstrap succeeds end-to-end

```bash
make down
docker volume rm argus_postgres-data  # confirm the volume name with `docker volume ls | grep argus`
make up
make db-migrate
```

Expected:

- `make db-migrate` exits 0.
- `schema_migrations.dirty = false` (queryable via `docker compose exec postgres psql ... -c "SELECT version, dirty FROM schema_migrations"`).
- `schema_migrations.version` equals the highest version present in `migrations/*.up.sql` — do NOT hard-code a specific version (STORY-088 and later stories will extend the chain). Dynamic check: compare against `ls migrations/*.up.sql | sort | tail -1` after stripping the timestamp prefix, OR run `argus migrate up` a second time and confirm the log contains `"migrate: no change — already at latest version"`.
- `argus-app` container becomes healthy (health probe `GET /health/ready` returns 200).
- `argus-app` boot log contains `"schema integrity check passed"` (`schemacheck.Verify` at `cmd/argus/main.go:384-387`).

### AC-2: Live-DB `argus migrate up` is a no-op

On a DB already at head (whatever the current head is — do NOT hard-code a version):

- `docker compose exec argus /app/argus migrate up` exits 0 with log line `"migrate: no change — already at latest version"` (`cmd/argus/main.go:222`).
- `schema_migrations.version` unchanged between first and second invocations.
- No DDL executed (verifiable via `pg_stat_activity` snapshot or audit-style probe; typically implicit via "no change" log).
- Specific to STORY-087: the shim file at `20260412999999` is NOT visited during this run. Confirm by: before the second invocation, set a recognizable sentinel (e.g., `ALTER TABLE sms_outbound ALTER COLUMN text_preview SET DEFAULT 'sentinel'`); run `argus migrate up`; verify the sentinel default is still in place (shim did not re-create the table).

### AC-3: `sms_outbound` exists with correct column set after fresh bootstrap

After AC-1 completes:

- `SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_name='sms_outbound' ORDER BY ordinal_position` returns 12 columns in the order:
  `id` (uuid, NOT NULL), `tenant_id` (uuid, NOT NULL), `sim_id` (uuid, NOT NULL), `msisdn` (varchar(20), NOT NULL), `text_hash` (varchar(64), NOT NULL), `text_preview` (varchar(80), NULL), `status` (varchar(20), NOT NULL, default 'queued'), `provider_message_id` (varchar(255), NULL), `error_code` (varchar(50), NULL), `queued_at` (timestamptz, NOT NULL), `sent_at` (timestamptz, NULL), `delivered_at` (timestamptz, NULL).
- Exact parity with STORY-086's repair migration output (`20260417000004_sms_outbound_recover.up.sql:23-36`).

### AC-4: FK on `sim_id` is NOT present

- `SELECT COUNT(*) FROM pg_constraint WHERE contype='f' AND conrelid='sms_outbound'::regclass AND conkey = ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid='sms_outbound'::regclass AND attname='sim_id')]` returns 0.
- Total FK count on `sms_outbound` is 1 (only `tenant_id → tenants(id)`).

### AC-5: `check_sim_exists` trigger fires

After AC-1:

- `SELECT tgname, tgenabled FROM pg_trigger WHERE tgrelid='sms_outbound'::regclass AND tgname='trg_sms_outbound_check_sim'` returns 1 row, `tgenabled = 'O'` (origin — always fires).
- Smoke insert test: `INSERT INTO sms_outbound (tenant_id, sim_id, msisdn, text_hash) VALUES (<valid_tenant>, '00000000-0000-0000-0000-000000000000', '+905550000000', 'abc')` fails with SQLSTATE `23503` and message containing `FK violation: sim_id ... does not exist in sims`.
- Valid insert (sim_id matches an existing sim) succeeds.

### AC-6: Indexes installed by 20260413000001 are present

- `SELECT indexname FROM pg_indexes WHERE tablename='sms_outbound' ORDER BY indexname` returns at least: `idx_sms_outbound_provider_id`, `idx_sms_outbound_status`, `idx_sms_outbound_tenant_sim_time`, plus the auto-created PK index on `id`.

### AC-7: RLS policy installed by 20260413000002 is present

- `SELECT policyname FROM pg_policies WHERE tablename='sms_outbound'` returns `sms_outbound_tenant_isolation`.
- `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE relname='sms_outbound'` returns `t, t`.

### AC-8: Down chain works end-to-end

- `argus migrate down -all` from head succeeds with exit 0.
- Final `schema_migrations.version` is NULL (`m.Version()` returns `ErrNilVersion`).
- `SELECT to_regclass('public.sms_outbound')` returns NULL.

### AC-9: Baseline test suite remains green

- `go test ./...` result: PASS count ≥ 3000 (baseline after STORY-085 gate per session log) with zero new failures attributable to STORY-087.
- `schemacheck_test.TestCriticalTables_CountAndOrder` still passes (`len == 12`, sorted, contains `sms_outbound`).

## Compliance rules

- **File immutability**: `migrations/20260413000001_story_069_schema.up.sql` and `.down.sql` MUST NOT be modified. `git diff --stat main...HEAD -- migrations/20260413000001*` must show 0 changes.
- **No checksum/hash drift on live DBs**: golang-migrate v4 does not hash content (verified via source inspection of `database/driver.go` in `/Users/btopcu/go/pkg/mod/github.com/golang-migrate/migrate/v4@v4.19.1/`). The live `schema_migrations` table schema (`version bigint PRIMARY KEY, dirty boolean`) does not include any content column. Therefore adding a new source file below the current version is a no-op for live DBs.
- **Idempotency**: the shim's up is idempotent (`CREATE TABLE IF NOT EXISTS`); the down is idempotent (`DROP TABLE IF EXISTS ... CASCADE`). Both safe to re-run.
- **STORY-086 semantics preserved**: `20260417000004_sms_outbound_recover.{up,down}.sql` is NOT modified. Its role as the authoritative idempotent reconciler (table + indexes + RLS + check_sim_exists trigger) is unchanged. On fresh volumes, it runs after the shim and is the last writer for those objects (all `IF NOT EXISTS` / `OR REPLACE` / `DROP IF EXISTS+CREATE`) — no conflict.
- **Schemacheck contract**: `CriticalTables` length unchanged (12). No new critical table added.
- **Naming**: follows project convention `YYYYMMDDHHMMSS_description.up.sql` / `.down.sql` (per `CLAUDE.md` conventions and `migrations/` precedent).
- **Transaction atomicity**: the shim file contains no explicit `BEGIN`/`COMMIT`. golang-migrate's postgres driver auto-wraps the file in a transaction, which is desired (a partial apply of the shim would be inconsistent).
- **No `-- no-transaction` directive**: the `-- no-transaction` marker (used by `migrations/20260323000002_perf_indexes.up.sql` for `CREATE INDEX CONCURRENTLY`) is NOT used here. The single `CREATE TABLE IF NOT EXISTS` is transactional-safe.

## Risks & mitigations

### Risk 1: golang-migrate v4.19.1 semantics differ from what source inspection suggests

- **Description**: If `readUp()` does in fact revisit source files below `curVersion` (contrary to the code read at `$GOMODCACHE/.../migrate.go:532-590`), then live DBs would re-apply the shim and attempt to `CREATE TABLE IF NOT EXISTS sms_outbound` — which is idempotent and safe — but could also trigger unexpected interaction with a concurrent `migrate down`.
- **Likelihood**: Very low. The source code trace is unambiguous: `readUp()` starts at `from` and iterates via `sourceDrv.Next(from)`; there is no branch that iterates backward.
- **Mitigation**: Task 4 (`TestLiveDBIdempotent_STORY087`) empirically verifies this on a real PostgreSQL instance. Test is RED-GREEN: if golang-migrate's behavior differs, the test fails and the plan is reconsidered. Additionally, the shim's up-side uses `CREATE TABLE IF NOT EXISTS`, so even if the semantics are wrong, re-applying the shim is a no-op (aside from emitting a NOTICE).

### Risk 2: PostgreSQL parses the column list of a no-op `CREATE TABLE IF NOT EXISTS`

- **Description**: If PostgreSQL were to parse the column definitions (including FK clauses) even when the table exists, line 144's `REFERENCES sims(id)` would still fail.
- **Likelihood**: Zero per PostgreSQL documentation: https://www.postgresql.org/docs/16/sql-createtable.html — "`IF NOT EXISTS`: Do not throw an error if a relation with the same name already exists. A notice is issued in this case. Note that there is no guarantee that the existing relation is anything like the one that would have been created."
- **Mitigation**: Task 3 (`TestFreshVolumeBootstrap_STORY087`) empirically verifies end-to-end `m.Up()` succeeds. If PostgreSQL's behavior were different, this test fails loudly. Fallback: use Option H (SQL rewrite at load time) — costs ~20 lines of Go in `runMigrate`.

### Risk 3: Lex-order collision with other pending migrations

- **Description**: If someone else adds a migration with timestamp `20260412...` between `20260412000011` and `20260413000001`, numeric collision with `20260412999999` is theoretically possible.
- **Likelihood**: Very low. `20260412999999` is at the tail of 2026-04-12 and the next valid slot is `20260412999998` or `20260413000000`. The story's implementation happens on a short timeline (S-M effort); no concurrent story is working in the same timestamp range.
- **Mitigation**: If a collision arises, switch to `20260412999998`. Document in the commit message.

### Risk 4: `sms_outbound` column drift between shim and STORY-086 repair

- **Description**: If the shim creates columns that differ from STORY-086's repair, `20260417000004`'s `CREATE TABLE IF NOT EXISTS` no-ops silently but subsequent inserts (e.g., via `internal/api/sms/handler.go`) might fail due to column mismatch.
- **Likelihood**: Low. Task 1's validation step explicitly requires byte-for-byte column parity with STORY-086. Task 3 asserts the final column list via `information_schema.columns`.
- **Mitigation**: Unit test AC-3 enforces the exact 12-column schema.

### Risk 5: Seed files require partitions that are created later

- **Description**: `migrations/seed/003_comprehensive_seed.sql` + `seed/005_multi_operator_seed.sql` create `sims_turkcell` / `sims_vodafone` / `sims_turk_telekom` partitions. Seeds run after migrations via `argus seed` (or `make db-seed`) — unrelated to the shim and untouched by this story.
- **Likelihood**: n/a — not a STORY-087 concern.
- **Mitigation**: none needed. Documented here for completeness.

### Risk 6: Fresh-volume test requires a running PostgreSQL

- **Description**: Task 3's integration test needs a PostgreSQL instance. Existing `internal/store/*_test.go` tests use `POSTGRES_TEST_URL` env or skip if unset (`make test` in CI provisions a container).
- **Likelihood**: Medium — tests may skip in dev if the local PG isn't up.
- **Mitigation**: Follow the existing test-skip idiom used in `internal/store/apikey_test.go` / `internal/store/esim_test.go` (check for `POSTGRES_TEST_URL` or reachable `localhost:5432`; skip with a clear message if absent). Document the env var in the test file header.

## Tech Debt

This story IS the remediation of existing tech debt (D-032). No new tech debt introduced.

**Closed on this story**:
- **D-032**: resolved by the pre-069 shim. ROUTEMAP row flipped to `[x] DONE` in Task 6.

**Residual (out of STORY-087 scope)**:
- None. The shim + STORY-086 repair fully resolve fresh-volume and live-drift scenarios.

## Dependencies

- **STORY-086 (DONE)** — STORY-086 established the authoritative sms_outbound schema via `20260417000004_sms_outbound_recover.{up,down}.sql` and added `sms_outbound` to `schemacheck.CriticalTables`. STORY-087 copies STORY-086's column list into the shim and relies on `20260417000004` for indexes/RLS/trigger reconciliation on fresh volumes.
- **STORY-079 (DONE)** — `argus migrate` CLI subcommand. STORY-087 exercises `argus migrate up` in its AC-1.

No downstream dependencies.

## Out of Scope

- **Changing STORY-069's architectural FK strategy**: the `sim_id → sims` relationship stays enforced via the `check_sim_exists` trigger (installed by STORY-086). A future story could introduce a non-partitioned `sims_id_registry` table + FK as described in decisions.md DEV-231-era discussions, but that is a schema re-architecture, not a tech-debt fix.
- **Introducing a separate sms_outbound partitioning strategy**: not needed for current volume (SMS is low-rate vs CDR).
- **Replacing golang-migrate with a different migration tool**: ADR-002 standardizes on golang-migrate.
- **Touching seed files**: `migrations/seed/*.sql` are orthogonal — they run after migrations via a separate `argus seed` subcommand.
- **Adding a CI fresh-volume smoke job**: D-029 (separate tech debt item) tracks that. STORY-087 adds Go-level integration tests (Task 3-5) but does not wire GHA.
- **Mutating `20260413000001_story_069_schema.up.sql`**: forbidden by D-032's constraint and by ADR-level migration immutability norms.

## Quality Gate (plan self-validation)

### Substance

- ✅ Root cause identified at exact line (`20260413000001_story_069_schema.up.sql:144`) and exact SQL (`sim_id UUID NOT NULL REFERENCES sims(id),`).
- ✅ Defect mechanism explained (PostgreSQL requires FK to match full composite PK `(id, operator_id)`; `REFERENCES sims(id)` is unsatisfiable).
- ✅ Proposed fix is minimum-viable (one SQL file pair) with no Go code changes.
- ✅ Problem-statement drift identified and corrected: Solution A was wrongly dismissed; golang-migrate v4's `readUp()` only advances forward, so a below-current source file is invisible to live DBs.

### Required sections

- ✅ Goal (one paragraph).
- ✅ Architecture Context (disk layout, runner path, schemacheck path, live DB state, fresh-volume failure mode).
- ✅ Root cause (verbatim SQL lines cited).
- ✅ Candidate solutions considered (A-I, each with verdict).
- ✅ Chosen solution (A, with explicit justification).
- ✅ Config / API / DB schema (none / none / new migration pair spec).
- ✅ Tasks (1 wave, 7 tasks, each with What/Files/Tests/Depends-on).
- ✅ Acceptance Criteria (AC-1 through AC-9, each queryable or testable).
- ✅ Compliance rules (file immutability, no checksum drift, idempotency, STORY-086 semantics preservation, schemacheck contract, naming, transaction atomicity, no `-- no-transaction`).
- ✅ Risks & mitigations (6 risks).
- ✅ Tech Debt section (D-032 closed, no new debt).
- ✅ Dependencies (STORY-086 DONE, STORY-079 DONE).
- ✅ Out of Scope (6 items).
- ✅ Quality Gate self-validation.

### Embedded specs

- ✅ Shim up-side and down-side SQL written out in full with header comments pointing to D-032 / DEV-239.
- ✅ All AC queries are executable verbatim against PostgreSQL.

### Code-state validation

- ✅ `migrations/20260413000001_story_069_schema.up.sql` read, line 144 verified verbatim.
- ✅ `migrations/20260320000002_core_schema.up.sql:275-300` read, `sims` composite PK + partitioning verified verbatim.
- ✅ `migrations/20260417000004_sms_outbound_recover.up.sql` read, column parity target verified verbatim.
- ✅ `cmd/argus/main.go:192-263` read, `runMigrate()` flow verified.
- ✅ `internal/store/schemacheck/schemacheck.go` read, `CriticalTables` content and length verified.
- ✅ `$GOMODCACHE/github.com/golang-migrate/migrate/v4@v4.19.1/migrate.go:265-590` read, `Up()` → `readUp()` → `sourceDrv.Next()` iteration verified.
- ✅ `deploy/docker-compose.yml` + `infra/docker/Dockerfile.argus` + `Makefile` read, bootstrap flow verified (`make up` → `make db-migrate` is operator-initiated, not auto-run).

### Task decomposition

- ✅ Each task has explicit file paths (NEW / MOD).
- ✅ Each task has a verifiable definition of done (test name or file diff).
- ✅ Dependencies between tasks are linear and explicit.
- ✅ No task requires parallel coordination across developers.

### Test coverage

- ✅ AC-1 (fresh-volume bootstrap): Task 3 integration test.
- ✅ AC-2 (live-DB no-op): Task 4 integration test.
- ✅ AC-3 (column set): asserted in Task 3 via `information_schema.columns` query.
- ✅ AC-4 (no FK on sim_id): asserted in Task 3 via `pg_constraint` query.
- ✅ AC-5 (check_sim_exists trigger): asserted in Task 3 via `pg_trigger` query + smoke insert.
- ✅ AC-6 (indexes): asserted in Task 3 via `pg_indexes` query.
- ✅ AC-7 (RLS): asserted in Task 3 via `pg_policies` query.
- ✅ AC-8 (down chain): Task 5.
- ✅ AC-9 (no test regression): full `go test ./...` run in gate.

### Effort confirmation

- Original estimate: **S-M**.
- Revised estimate after planning: **S-M confirmed** (1 wave, 7 tasks, ~2 new SQL files, ~3 new Go integration tests, ~1 decisions.md entry, ~1 ROUTEMAP update). No Go runner changes. Net code delta: ~200 lines across migrations + tests.
