# Row-Level Security (RLS) — Defense-in-Depth

> Status: enabled from migration `20260412000006_rls_policies.up.sql` (STORY-064 Task 7, DEV-167).
> Scope: 28 multi-tenant tables.
> Primary tenant enforcement: still the application's `WHERE tenant_id = $1` clauses. RLS is a backstop.

## Why RLS

Every Argus multi-tenant table already filters by `tenant_id` in the Go store layer. RLS adds a second line of defense that activates whenever a database connection does NOT hold the application role:

- Ad-hoc `psql` sessions by an operator or DBA
- Reporting / BI tools connecting with a read-only role
- Misconfigured services connecting with the wrong role
- Connections that accidentally drop the tenant-scope parameter

Without RLS, any such connection sees every tenant's data. With RLS, a non-app connection without `SET app.current_tenant` sees zero rows, and a connection with the wrong tenant UUID sees only that tenant.

## How it works

Each multi-tenant table has:

```sql
ALTER TABLE <table> ENABLE ROW LEVEL SECURITY;
ALTER TABLE <table> FORCE ROW LEVEL SECURITY;
CREATE POLICY <table>_tenant_isolation ON <table>
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);
```

- `ENABLE ROW LEVEL SECURITY` turns policies on for non-superuser, non-owner, non-BYPASSRLS roles.
- `FORCE ROW LEVEL SECURITY` also applies policies to the table owner (useful so the migration role does not accidentally bypass RLS during development/testing).
- `current_setting('app.current_tenant', true)` returns `NULL` when the session variable is not set; casting `NULL::uuid` yields `NULL` and `tenant_id = NULL` is never true → zero rows.

For tables without a direct `tenant_id` column (`policy_versions`, `policy_assignments`, `policy_rollouts`, `ip_addresses`, `esim_profiles`, `user_sessions`, `sim_state_history`), the policy uses a nested `IN (SELECT … WHERE tenant_id = …)` subquery against a parent table. See the migration for exact join paths.

## Why the application role holds BYPASSRLS

Enforcing RLS per-request would require the app to execute `SET LOCAL app.current_tenant = '<uuid>'` at the start of every request's transaction. The existing store layer in `internal/store/*.go` uses the pool directly (`pgxpool.Pool.QueryRow(ctx, …)`) without an explicit transaction scope. `SET LOCAL` only works inside a `BEGIN/COMMIT` block, and `SET SESSION` leaks across pooled connections — so wiring per-request RLS requires refactoring every store method to use tx-scoped sessions. That refactor is out of scope for STORY-064.

Decision (DEV-167): for now the app role `argus_app` is granted `BYPASSRLS`. The `WHERE tenant_id = $1` clauses in the store layer remain the primary enforcement. RLS is purely a backstop against non-app access paths.

Per-request RLS enforcement via tx-scoped sessions is filed as a future item.

## Deploy checklist

**CRITICAL: before applying migration `20260412000006_rls_policies.up.sql` in production, the application role must already hold `BYPASSRLS`.** Otherwise the Argus process will see empty result sets for every tenant-scoped query and the platform will effectively be down.

The role grant is configured out-of-band in `deploy/` Docker bootstrap (and in dev environments `postgres` superuser which holds BYPASSRLS implicitly).

```sql
-- Run ONCE as a superuser BEFORE running migration 20260412000006:
ALTER ROLE argus_app BYPASSRLS;
```

Verify:

```sql
SELECT rolname, rolbypassrls FROM pg_roles WHERE rolname = 'argus_app';
-- expected: rolbypassrls = t
```

If `argus_app` shows `f`, stop — do not run the migration yet.

## Setting up a reporting / read-only role

A typical BI or ad-hoc reporting role should NOT receive `BYPASSRLS`. Then every query must be preceded by `SET app.current_tenant` for the policies to allow reads:

```sql
-- as superuser, once:
CREATE ROLE argus_report LOGIN PASSWORD '...' NOBYPASSRLS;
GRANT USAGE ON SCHEMA public TO argus_report;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO argus_report;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT ON TABLES TO argus_report;

-- in the reporting client, before running queries:
SET app.current_tenant = '<target-tenant-uuid>';
SELECT count(*) FROM sims;  -- will only see the target tenant's SIMs
```

`SET app.current_tenant` persists for the session; if the client pools connections, use `SET LOCAL app.current_tenant` inside an explicit transaction and commit per-query.

## Operational notes

- `sessions` and `cdrs` are TimescaleDB hypertables. RLS applies to the parent hypertable on PG 13+ and TimescaleDB 2.x. Historical compressed chunks may behave differently for non-app roles; because the app holds `BYPASSRLS`, application access is unaffected. Verify with your TimescaleDB version before enabling broad reporting access.
- `sims` is LIST-partitioned by `operator_id`. RLS policies on the partitioned parent apply to all partitions automatically.
- `audit_logs` and `sim_state_history` are RANGE-partitioned by `created_at`. Same inheritance — policies apply to every existing and future partition.
- Dropping or replacing any of these tables requires re-creating its RLS policy. The down migration removes all policies in the reverse order they were created.

## Future work

- **Enforced per-request RLS** (deferred): refactor store layer to tx-scoped sessions, execute `SET LOCAL app.current_tenant = $1` at the start of each request transaction, revoke `BYPASSRLS` from `argus_app`, and treat RLS as the primary enforcement mechanism instead of a backstop. This is a multi-week change touching every store method and is filed as a FUTURE item.
- **Policy coverage for write ops**: the current policies have a `USING` clause only (applies to SELECT/UPDATE/DELETE existing-row checks). Adding a `WITH CHECK` clause would additionally reject INSERTs and UPDATEs that target a different tenant. Not required today because the app holds `BYPASSRLS`; revisit when per-request RLS lands.

## References

- Migration: `migrations/20260412000006_rls_policies.up.sql`, `.down.sql`
- Decision: DEV-167 in `docs/stories/phase-10/STORY-064-plan.md`
- PostgreSQL docs: https://www.postgresql.org/docs/16/ddl-rowsecurity.html
