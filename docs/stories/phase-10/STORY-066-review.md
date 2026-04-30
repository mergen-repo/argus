# Post-Story Review: STORY-066 — Reliability, Backup, DR & Runtime Hardening

**Date:** 2026-04-12
**Reviewer:** Phase 10 Post-Story Reviewer
**Gate verdict:** PASS (2 dispatches — F-1 AC-4 disk probe wiring, F-2 PITR evidence doc sync)
**Tests at review:** 2135 passed / 0 failed

---

## Review Checklist (14 checks)

| # | Check | Result |
|---|-------|--------|
| 1 | Next story impact | REPORT ONLY (see below) |
| 2 | Architecture doc sync (ARCHITECTURE.md) | FIXED (3 gaps) |
| 3 | API index sync (api/_index.md) | FIXED (5 new endpoints) |
| 4 | DB schema index sync (db/_index.md) | FIXED (2 new tables) |
| 5 | Glossary sync (GLOSSARY.md) | FIXED (12 new terms) |
| 6 | Decisions log (decisions.md) | FIXED (DEV-178 to DEV-185) |
| 7 | USERTEST.md entry | FIXED (13 scenarios added) |
| 8 | ROUTEMAP status update | FIXED (DONE, 10/22) |
| 9 | PITR evidence doc accuracy | FIXED (3 errors corrected) |
| 10 | Story file accuracy | REPORT ONLY (see below) |
| 11 | PITR runbook technical validity | ESCALATED → FIXED (2 critical bugs) |
| 12 | Config doc sync (CONFIG.md/FUTURE.md) | PASS — no new gaps found |
| 13 | Screen references accuracy | REPORT ONLY — SCR-019 stale ref (see below) |
| 14 | Web mock retirement | PASS — no health/backup mocks found in web/src |

---

## Check #1 — Next Story Impact (REPORT ONLY)

STORY-067 (CI/CD Pipeline, Deployment & Ops Tooling) depends on STORY-066. The following STORY-066 additions are relevant to STORY-067 planning:

- Three health probe endpoints (`/health/live`, `/health/ready`, `/health/startup`) should be used in CI health checks and Docker healthcheck directives — STORY-067 should replace any legacy `/api/health` references in CI scripts.
- `DISK_PROBE_MOUNTS` env var needs to be configured with paths actually mounted into argus-app in the CI/staging compose files (currently defaults to container paths from the postgres container, which shows as `status=missing`).
- `BACKUP_S3_BUCKET`, `AWS_REGION`, `BACKUP_S3_PREFIX` need to be set in staging environment for BackupProcessor to run successfully.
- `ARGUS_WAL_BUCKET` + `ARGUS_WAL_PREFIX` need to be set in staging for live WAL shipping.
- `SHUTDOWN_TIMEOUT_SECONDS` should be tuned for CI deployment containers (shorter than the 30s default may be appropriate for fast CI cycles).
- **UPDATED count trigger:** 1 (health probe URL change: `/api/health` → `/health/ready` in health check directives).

---

## Check #10 — Story File Accuracy (REPORT ONLY)

**Issue:** STORY-066 references `SCR-019` (Admin Settings screen) which does not exist in `docs/SCREENS.md`. Settings screens start at SCR-110. This is a stale/incorrect screen reference in the story spec.

**Action:** NOT fixed (story files are read-only per reviewer protocol). Noted for awareness. No functional impact — STORY-066 implementation is complete and gate-verified. SCR-019 stale reference is a documentation cosmetic issue.

---

## Findings Summary

### F-CRITICAL-01: PITR Runbook Step 4 — Two Technical Bugs (FIXED)

**File:** `docs/runbook/dr-pitr.md`, Step 4

**Bug #1 — pg_restore called against stopped server:**
The runbook stopped PostgreSQL immediately before calling `pg_restore`. `pg_restore` is a client tool (`libpq` based) — it requires a live, running PostgreSQL server. With the server stopped, every `pg_restore` invocation would fail with `connection refused`.

**Bug #2 — `--create` flag used with `--dbname=argus`:**
`--create` issues `DROP DATABASE argus; CREATE DATABASE argus;` before restoring. You cannot drop or create a database you are currently connected to. The `--dbname` must be the `postgres` maintenance database when `--create` is used.

**Fix applied:**
- Removed the `docker compose stop postgres` step before pg_restore.
- Changed `pg_restore` to run while PostgreSQL IS running, via `docker compose exec postgres pg_restore`.
- Changed `--dbname=argus` → `--dbname=postgres` to allow `--create` to issue DROP/CREATE correctly.
- Added explanatory comments to the runbook for future operators.

**Severity:** CRITICAL — broken DR runbook would fail during an actual disaster recovery event.

---

### F-HIGH-02: Evidence Doc — `standby.signal` Instead of `recovery.signal` (FIXED)

**File:** `docs/e2e-evidence/STORY-066-pitr-test.md`, Step 4

`standby.signal` instructs PostgreSQL to enter streaming replication standby mode (waiting for a primary to connect). `recovery.signal` instructs PostgreSQL to replay WAL from archive until `recovery_target_time`. Using `standby.signal` for PITR would cause PostgreSQL to wait for a replication connection that never arrives, stalling the recovery.

**Fix applied:** `standby.signal` → `recovery.signal` with explanatory comment.

---

### F-MEDIUM-03: Evidence Doc — `audit_log` vs `audit_logs` (FIXED)

**File:** `docs/e2e-evidence/STORY-066-pitr-test.md`, Step 5 (lines 93 and 104)

Table name in evidence doc was `audit_log` (singular). Correct table name is `audit_logs` (plural), confirmed by migration `20260412000005_partition_bootstrap.up.sql`.

**Fix applied:** Both occurrences corrected to `audit_logs`.

---

### F-MEDIUM-04: API Index Missing 5 New Endpoints (FIXED)

**File:** `docs/architecture/api/_index.md`

Five endpoints introduced by STORY-066 were not documented:

| API ID | Path | Description |
|--------|------|-------------|
| API-187 | GET /health/live | Liveness probe |
| API-188 | GET /health/ready | Readiness probe + disk probe |
| API-189 | GET /health/startup | Startup probe (60s grace) |
| API-190 | GET /api/v1/system/backup-status | Backup run history + verification |
| API-191 | GET /api/v1/system/jwt-rotation-history | JWT rotation audit log |

**Fix applied:** "System Health" section expanded from 3 to 8 endpoints. API-180 description updated to mark it as legacy (kept for backward compat).

---

### F-MEDIUM-05: DB Index Missing 2 New Tables (FIXED)

**Files:** `docs/architecture/db/_index.md`, `docs/architecture/db/platform-services.md`

Tables introduced by migration `20260412000009_backup_runs` were not indexed:

- `backup_runs` → assigned TBL-32
- `backup_verifications` → assigned TBL-33

**Fix applied:** Both tables added to `_index.md` table list and full column/index/relationship detail added to `platform-services.md`.

---

### F-LOW-06: ARCHITECTURE.md — Three Gaps (FIXED)

**File:** `docs/ARCHITECTURE.md`

1. **CTN-02 health check URL stale:** Listed as `GET :8080/api/health` — updated to `GET :8080/health/ready` (new readiness probe endpoint).
2. **`postgres_wal_archive` volume missing:** Volumes section listed only `pgdata` and `natsdata`. The WAL archive volume introduced by STORY-066 was absent.
3. **No Backup Infrastructure section:** The backup pipeline, health probe split, disk probe, and PITR are significant architectural additions with no narrative in ARCHITECTURE.md.

**Fix applied:** All three gaps corrected. New "Backup Infrastructure" section added. Reference ID Registry updated (API-NNN: 109 → 115, TBL-NN: 31 → 33).

---

### F-LOW-07: GLOSSARY.md Missing ~12 Terms (FIXED)

**File:** `docs/GLOSSARY.md`

New "Reliability & DR Terms" section added with 12 terms:
- Liveness Probe
- Readiness Probe
- Startup Probe
- Disk Space Probe
- Backup Run
- Backup Verification
- PITR (Point-In-Time Recovery)
- WAL Archiving
- Graceful Shutdown
- Circuit Breaker (Operator)
- JWT Dual-Key Rotation
- Request Body Size Limit

---

### F-LOW-08: decisions.md Missing STORY-066 Implementation Decisions (FIXED)

**File:** `docs/brainstorming/decisions.md`

Eight implementation decisions added (DEV-178 to DEV-185) covering: health probe split, disk probe defaults, backup automation choice (Go scheduler vs pg_cron), DATABASE_READ_REPLICA_URL extension, JWT dual-key rotation, pprof token guard, HSTS guard, and anomaly crash safety backoff.

---

### F-LOW-09: USERTEST.md Missing STORY-066 Section (FIXED)

**File:** `docs/USERTEST.md`

STORY-066 section added with 13 backend verification scenarios covering: health probes, backup-status API, JWT rotation history API, disk usage metric, shutdown drain sequence, pprof access control, and migration verification.

---

## Fixes Applied Summary

| # | File | Change |
|---|------|--------|
| 1 | docs/runbook/dr-pitr.md | Step 4: pg_restore runs against live server; --dbname=postgres for --create |
| 2 | docs/e2e-evidence/STORY-066-pitr-test.md | standby.signal → recovery.signal (Step 4) |
| 3 | docs/e2e-evidence/STORY-066-pitr-test.md | audit_log → audit_logs (Step 5, ×2) |
| 4 | docs/architecture/api/_index.md | System Health 3→8 endpoints; API-187..191 added |
| 5 | docs/architecture/db/_index.md | TBL-32 backup_runs, TBL-33 backup_verifications added |
| 6 | docs/architecture/db/platform-services.md | Full column/index detail for TBL-32 and TBL-33 |
| 7 | docs/ARCHITECTURE.md | CTN-02 health check URL; postgres_wal_archive volume; Backup Infrastructure section; Reference ID Registry updated |
| 8 | docs/GLOSSARY.md | 12 new Reliability & DR Terms added |
| 9 | docs/brainstorming/decisions.md | DEV-178..185 added (8 STORY-066 decisions) |
| 10 | docs/USERTEST.md | STORY-066 section added (13 scenarios) |
| 11 | docs/ROUTEMAP.md | STORY-066 marked DONE, counter 10/22, changelog entry added |

**Total files updated: 11**
**Findings fixed: 9**
**Findings escalated then fixed: 1 (PITR runbook — critical)**
**Findings report-only: 2 (#1 next story impact, #10 SCR-019 stale ref in story file)**

---

## Project Health

- Stories completed: Phase 10 10/22
- Test suite: 2135 passed / 0 failed
- Zero-deferral charter: upheld
- Tech Debt items introduced: 0
- Next story: STORY-067 (CI/CD Pipeline, Deployment & Ops Tooling)
