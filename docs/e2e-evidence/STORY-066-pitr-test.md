# STORY-066 — PITR (Point-In-Time Recovery) DR Test Evidence

**Status:** DRY RUN — procedure validated via `bash -n` syntax check + container-free
walk-through. Live PITR smoke test scheduled for next staging release.
Track: Tech Debt — STORY-PITR-LIVE or next staging maintenance window.

**Date:** 2026-04-12
**Author:** STORY-066 automated test harness (Developer agent)
**Story:** STORY-066 — Reliability & DR hardening

---

## Procedure Overview

The PITR restore procedure targets PostgreSQL 16 + TimescaleDB running in the
Argus Docker Compose stack. The procedure restores the database to an arbitrary
point in time from a continuous WAL archive.

### Prerequisites

- PostgreSQL 16 with `wal_level = replica` and `archive_mode = on`
- WAL archive destination: S3 bucket (`argus-wal-archive/`) or local NFS mount
- A recent base backup taken via `BackupProcessor` (stored in `argus-backups/`)
- `pg_restore` and `psql` available on the recovery host
- Scratch container: `postgres:16` Docker image (isolated from production)

---

## Step-by-Step Dry Run

### Step 1 — Identify Recovery Target

```bash
# Determine target time (e.g. 5 minutes before an accidental table drop)
TARGET_TIME="2026-04-12 03:00:00 UTC"
echo "Recovery target: $TARGET_TIME"
```

Expected output: echo confirms the timestamp.

### Step 2 — Spin Up Scratch Container

```bash
docker run -d \
  --name pitr-scratch \
  -e POSTGRES_PASSWORD=recover \
  -v /tmp/pitr-restore:/var/lib/postgresql/data \
  postgres:16
```

Expected: Container starts, PG initialises `/var/lib/postgresql/data`.

### Step 3 — Restore Base Backup

```bash
# Fetch latest daily backup from S3 (or local MinIO)
aws s3 cp s3://argus-backups/daily/$(latest_key).dump /tmp/pg_base.dump

# Restore into scratch container
docker exec pitr-scratch pg_restore \
  --host=127.0.0.1 --port=5432 \
  --username=postgres --dbname=postgres \
  --format=custom /tmp/pg_base.dump
```

Expected: `pg_restore` exits 0; `argus` database present in scratch container.

### Step 4 — Configure WAL Recovery

```bash
# Create recovery.conf (PG ≤12) or recovery_target_time in postgresql.conf (PG 16)
docker exec pitr-scratch bash -c "cat >> /var/lib/postgresql/data/postgresql.conf <<'EOF'
restore_command = 'aws s3 cp s3://argus-wal-archive/%f %p'
recovery_target_time = '2026-04-12 03:00:00 UTC'
recovery_target_action = 'promote'
EOF"

# Signal PITR recovery mode (NOT standby.signal, which triggers streaming replication)
docker exec pitr-scratch touch /var/lib/postgresql/data/recovery.signal
docker restart pitr-scratch
```

Expected: PostgreSQL replays WAL segments up to `TARGET_TIME` then promotes.
Log line: `LOG:  recovery stopping before commit of transaction ...`

### Step 5 — Validate Row Counts

```bash
docker exec pitr-scratch psql -U postgres -d argus -c "
  SELECT
    (SELECT COUNT(*) FROM tenants)       AS tenants,
    (SELECT COUNT(*) FROM sims)          AS sims,
    (SELECT COUNT(*) FROM audit_logs)    AS audit_rows
  ;
"
```

Expected vs Actual (dry run — values from pre-incident snapshot):

| Table       | Expected (pre-incident) | Actual (restored) | Match |
|-------------|------------------------|-------------------|-------|
| tenants     | 5                      | DRY RUN           | N/A   |
| sims        | 10,000+                | DRY RUN           | N/A   |
| audit_logs  | varies                 | DRY RUN           | N/A   |

In a live run, "Actual" is populated from the scratch container query output.
A deviation > 1% in non-audit tables triggers an incident.

### Step 6 — Application Smoke Test

```bash
# Point Argus to scratch DB and run health check
DATABASE_URL="postgres://postgres:recover@127.0.0.1:5432/argus" \
  ./argus &
APP_PID=$!
sleep 3
curl -sf http://localhost:8080/health/ready | jq .data.state
kill $APP_PID
```

Expected: `"healthy"` (or `"degraded"` if Redis/NATS not reachable — acceptable
for a DB-only PITR smoke test).

### Step 7 — Cleanup

```bash
docker stop pitr-scratch
docker rm pitr-scratch
rm -rf /tmp/pitr-restore /tmp/pg_base.dump
```

---

## Bash Syntax Validation (Dry Run Evidence)

The entire procedure above was validated with `bash -n` (syntax-only, no
execution) against the script file `deploy/scripts/pitr-restore.sh`:

```
bash -n deploy/scripts/pitr-restore.sh
# Exit code: 0 — no syntax errors
```

---

## Open Items / Tech Debt

1. **Live smoke test** — schedule for next staging release cycle (estimated:
   next sprint). At that point populate the "Actual" column in Step 5 and
   attach logs as `STORY-066-pitr-live-YYYYMMDD.log`.

2. **WAL archive setup** — `archive_mode = on` is enabled via
   `infra/postgres/postgresql.conf:106` and the `postgres_wal_archive` volume
   is mounted in `deploy/docker-compose.yml`. Live S3/MinIO WAL shipping
   requires `ARGUS_WAL_BUCKET` / `ARGUS_WAL_PREFIX` env vars to be populated
   at deploy time (currently plumbed but unset by default). Populate these
   env vars for the staging deploy where the live PITR smoke test will run.

3. **Automation** — integrate `deploy/scripts/pitr-restore.sh` into the staging
   CI pipeline as a monthly scheduled job (GitHub Actions cron or Argus's own
   cron scheduler).

---

## Acceptance Criteria Coverage

| AC | Description | Status |
|----|-------------|--------|
| AC-9 | PITR restore procedure documented and validated | Dry run complete |
| AC-9 | Evidence file exists and non-empty | This file |
| AC-9 | Live run scheduled | Next staging window |
