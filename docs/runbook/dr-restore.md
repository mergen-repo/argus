# Disaster Recovery: Full Backup Restore

## When to use

- Full database loss (disk failure, volume corruption, cloud provider incident)
- Environment rebuild from scratch (new host, new Docker deployment)
- Scenario where data loss back to the last full backup is acceptable

If you need to recover to a specific point in time and preserve more recent data, use `dr-pitr.md` instead.

---

## Prerequisites

- Recent full backup available in S3: `aws s3 ls s3://argus-backup/daily/` must return at least one `.dump` file
- `psql`, `pg_restore`, `aws-cli`, `docker`, `docker compose` installed on operator machine
- Operator has AWS credentials with read access to `s3://argus-backup`
- PostgreSQL admin password available (`POSTGRES_PASSWORD` env or 1Password)

---

## Estimated Duration

| Step | Duration |
|------|----------|
| Steps 1–2 (stop + download) | 10–25 min |
| Step 3 (drop + recreate DB) | < 1 min |
| Step 4 (pg_restore) | 20–40 min |
| Step 5 (smoke test + restart) | 5 min |
| **Total** | **~35–70 minutes** |

---

## Procedure

### 1. Stop argus + postgres

Stop the application layer first so no new writes arrive, then stop postgres.

```bash
docker compose -f deploy/docker-compose.yml stop argus nginx
docker compose -f deploy/docker-compose.yml stop postgres

docker compose -f deploy/docker-compose.yml ps
# Expected: argus-app, argus-nginx, argus-postgres all show status "exited"
```

### 2. Download latest full backup

```bash
# List available backups — pick the most recent
aws s3 ls s3://argus-backup/daily/ --human-readable
# Expected output (example):
#   2026-04-12 03:00:01  4.8 GiB  20260412_030000.dump

# Download
aws s3 cp s3://argus-backup/daily/20260412_030000.dump /tmp/base.dump
# Expected: progress shown, exits 0
# Expected duration: 10–20 min for a 5 GB dump

# Optional: verify checksum
aws s3 cp s3://argus-backup/daily/20260412_030000.dump.sha256 /tmp/base.dump.sha256 && sha256sum -c /tmp/base.dump.sha256
# Expected: /tmp/base.dump: OK
```

### 3. Drop + recreate argus DB via psql admin

Bring postgres up with only the admin `postgres` superuser (bypass the argus DB entirely):

```bash
# Start postgres
docker compose -f deploy/docker-compose.yml up -d postgres

# Wait for postgres to be ready
sleep 15
docker compose -f deploy/docker-compose.yml exec postgres pg_isready -U argus
# Expected: accepting connections

# Drop and recreate the argus database
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U "${POSTGRES_USER:-argus}" -d postgres -c "
    SELECT pg_terminate_backend(pid)
    FROM pg_stat_activity
    WHERE datname = '${POSTGRES_DB:-argus}' AND pid <> pg_backend_pid();
    DROP DATABASE IF EXISTS \"${POSTGRES_DB:-argus}\";
    CREATE DATABASE \"${POSTGRES_DB:-argus}\" OWNER \"${POSTGRES_USER:-argus}\";
  "
# Expected:
#   pg_terminate_backend
#   ─────────────────────
#   (0 rows)
#   DROP DATABASE
#   CREATE DATABASE
```

### 4. pg_restore

Restore schema and data from the custom-format dump:

```bash
# Copy dump into a location the postgres container can access
docker cp /tmp/base.dump argus-postgres:/tmp/base.dump

# Run pg_restore inside the running postgres container
# (avoids host-resolution issues across Docker environments)
docker compose -f deploy/docker-compose.yml exec postgres \
  bash -c 'PGPASSWORD="${POSTGRES_PASSWORD:-argus_secret}" pg_restore \
    --format=custom \
    --host=localhost \
    --username="${POSTGRES_USER:-argus}" \
    --dbname="${POSTGRES_DB:-argus}" \
    --no-acl \
    --no-owner \
    --verbose \
    /tmp/base.dump 2>&1 | tee /tmp/pg_restore.log | tail -20'
# Expected: "pg_restore: finished main parallel loop", exits 0
# Expected duration: 20–40 min for a 5 GB dump
# Warnings about pre-existing sequences/types are normal with --clean; errors are not

docker compose -f deploy/docker-compose.yml exec postgres \
  grep -i "error" /tmp/pg_restore.log || echo "No errors found"
# Review any actual errors — constraint violations usually indicate a stale backup
```

### 5. Smoke test + restart argus

```bash
# Quick row-count sanity check
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -c "SELECT count(*) FROM sims;"
# Expected: count > 0 (unless the backup was from an empty system)

# Bring argus and nginx back up
docker compose -f deploy/docker-compose.yml up -d argus nginx

# Wait for argus to become healthy (~30–60 sec)
docker compose -f deploy/docker-compose.yml ps
# Expected: argus-app shows "(healthy)"

# End-to-end health check
curl -sf http://localhost:8084/health/ready | jq
# Expected: {"status":"ok", ...}

# Confirm login works
curl -sf -X POST http://localhost:8084/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq '.status'
# Expected: "success"
```

Clean up temporary files:

```bash
rm /tmp/base.dump /tmp/pg_restore.log
```

---

## Post-Restore Checklist

- [ ] Verify SIM count matches pre-incident export (if available)
- [ ] Verify most recent audit log entry timestamp is as expected
- [ ] Verify active sessions/CDR tables are populated for last known billing period
- [ ] Send incident update: restore complete, data loss window = `<backup_timestamp>` to `<incident_timestamp>`
- [ ] File post-mortem if data loss was > 1 hour
- [ ] Review WAL archiving config (`dr-pitr.md`) to reduce future data loss window
