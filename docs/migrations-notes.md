# Migrations — Operational Notes

Warnings and runbook notes for migrations that require manual operator action before or after execution.

## AC-9 — Drop Roaming Agreements (FIX-238)

**Migration:** `migrations/20260505000001_drop_roaming_agreements.down.sql` (and its `.up.sql` counterpart)

**Warning:** Running the `drop_roaming_agreements` up migration against a production database **permanently destroys all data** in the `roaming_agreements` table (TBL-43). This table holds tenant-scoped SLA terms, cost terms, and agreement state history with partner operators.

**Required pre-migration action (production only):**

1. Export a CSV backup before applying the migration:
   ```sql
   COPY (SELECT * FROM roaming_agreements ORDER BY created_at)
   TO '/var/backups/roaming_agreements_export_YYYYMMDD.csv'
   WITH (FORMAT csv, HEADER true);
   ```
2. Confirm the export row count matches `SELECT COUNT(*) FROM roaming_agreements`.
3. Store the CSV in a durable location (S3 / offsite backup) before proceeding.
4. Apply the migration: `make db-migrate`.

**Rollback:** The down migration re-creates the table schema but does NOT restore data. A full rollback requires restoring from the CSV export via `COPY ... FROM`.

**Context:** FIX-238 removes the Roaming Agreement feature (STORY-071) — UI, API handlers, store layer, and DB table. The SoR (Steering of Roaming) engine is unaffected.
