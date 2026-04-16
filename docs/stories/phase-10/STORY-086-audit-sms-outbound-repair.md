# STORY-086: [AUDIT-GAP] Restore missing `sms_outbound` table and add boot-time integrity check

## Source
- Gap Type: RUNTIME_BUG + MISSING (schema drift)
- Doc Reference: TBL-42 in `docs/architecture/db/_index.md`; API-171 in `docs/architecture/api/_index.md`; STORY-069 AC-12
- Audit Report: `docs/reports/compliance-audit-report.md` (2026-04-17)
- Discovered: Manual compliance audit 2026-04-17 via live runtime probe

## Description

`GET /api/v1/sms/history` returns **500 INTERNAL_ERROR** on the live environment
(`admin@argus.io` logged in, Docker stack healthy). Root cause: the
`sms_outbound` table is not present in the live PostgreSQL database, despite
the migration file `migrations/20260413000001_story_069_schema.up.sql` (lines
141–158) creating it and `schema_migrations.version=20260417000003, dirty=f`
indicating all migrations have been applied cleanly.

Sibling tables created in the same migration file — `onboarding_sessions`,
`scheduled_reports`, `webhook_configs`, `webhook_deliveries`,
`notification_preferences`, `notification_templates` — **do exist** in the
live DB. Only `sms_outbound` and its RLS policy are missing.

Likely mechanism: golang-migrate records the migration version **before** the
final statement completes, or the `CREATE TABLE IF NOT EXISTS sms_outbound`
block was skipped under some schema state where a partial object with the
same name already existed and then got dropped. Without a boot-time schema
check, this silent drift escaped detection from phase gate through two
compliance audits.

This breaks PRODUCT F-055 (SMS Gateway, marked COVERED in PRODUCT.md under
STORY-069 AC-12). The `/sms` page (SCR-132) and the send form silently
500 on every history fetch. Notifications channel "sms" delivery stats in
the admin delivery dashboard (API-253) also depend on this table via the
delivery-status store.

## Acceptance Criteria

- [ ] **AC-1**: Investigate the root cause. Inspect the live DB's audit log for
  any `DROP TABLE sms_outbound` or manual intervention, check golang-migrate
  internals for transaction boundaries, and confirm whether the missing table
  is a fresh-volume reproducer or only the current container's state. Document
  findings in `docs/brainstorming/decisions.md` as DEV-NNN.
- [ ] **AC-2**: Add a repair migration (`YYYYMMDDHHMMSS_sms_outbound_recover.up.sql`)
  that re-creates the `sms_outbound` table with the exact same schema +
  indexes + RLS policy as the original STORY-069 migration. The up migration
  must use `CREATE TABLE IF NOT EXISTS` and `CREATE INDEX IF NOT EXISTS` so
  it is safe on fresh volumes where STORY-069 did apply correctly. Mirror RLS
  via `ALTER TABLE ... ENABLE ROW LEVEL SECURITY`, `FORCE ROW LEVEL SECURITY`,
  and the `sms_outbound_tenant_isolation` policy (idempotent via DROP POLICY
  IF EXISTS + CREATE POLICY).
- [ ] **AC-3**: Add a boot-time schema-integrity check in `cmd/argus/main.go`
  that runs `SELECT to_regclass('public.sms_outbound')` (or a more general
  manifest of critical tables) immediately after migrations finish. If any
  expected table is missing, log a FATAL with the table name and exit
  non-zero. Scope for this story: verify the 7 STORY-069 tables + the 5
  STORY-077 tables (TBL-47..51) + TBL-42 (sms_outbound) — a minimal "recent
  tables" manifest so future migration drift cannot hide.
- [ ] **AC-4**: Regression test: integration test in `internal/store/sms_outbound_test.go`
  that opens a testcontainers PG, runs all migrations via
  `golang-migrate`, then asserts `sms_outbound` exists and accepts a
  tenant-scoped insert. The test must fail fast if the table is absent.
- [ ] **AC-5**: Doc sync. Remove the "NOTE (audit 2026-04-17)" caveat from
  `docs/architecture/db/_index.md` TBL-42 once the repair migration has run
  against the demo environment. Confirm `GET /api/v1/sms/history` returns
  200 (or 200 + empty array for a fresh tenant) end-to-end via
  `make smoke-test` before marking the AC complete.

## Technical Notes

- Architecture refs: API-170 (POST /api/v1/sms/send), API-171 (GET /api/v1/sms/history), TBL-42 (sms_outbound), STORY-069 AC-12
- Related stories: STORY-069 (origin), STORY-063 (SMS webhook delivery), STORY-073 (admin delivery status)
- Files to create/modify:
  - `migrations/YYYYMMDDHHMMSS_sms_outbound_recover.up.sql` + `.down.sql`
  - `cmd/argus/main.go` (boot schema manifest check)
  - `internal/store/sms_outbound_test.go` (new, testcontainers-backed)
  - `docs/architecture/db/_index.md` (remove audit caveat after fix)
  - `docs/brainstorming/decisions.md` (DEV-NNN root-cause note)
- Leverages:
  - Existing `internal/store/sms_outbound.go` (the code path is already wired; only the schema is missing)
  - Existing `golang-migrate` integration (no new libraries)
  - Existing `to_regclass` PG idiom for schema introspection

## Priority

HIGH — PRODUCT F-055 is currently broken in production-equivalent environment.
Silent 500s on documented endpoint. Also blocks a clean Documentation Phase
entry alongside STORY-079.

## Effort

S (estimated 0.5-1 day). Repair migration is 20 lines. Boot check is ~30 lines
of Go. Regression test reuses the existing testcontainers helper. Root-cause
investigation is time-boxed to 2 hours; if the cause is unclear, document what
was checked and close the investigation AC on that basis.
