# Disaster Recovery: Point-in-Time Recovery (PITR)

## When to use

- Data corruption at a known point in time (e.g., bad migration, silent bit-rot)
- Accidental `DELETE` / `UPDATE` that needs to be rolled back past the latest full backup
- Logical disaster where a backup-only restore would lose too much data (hours of CDR, sessions, audit)

If you only need to restore to the latest full backup and WAL replay is not required, use `dr-restore.md` instead — it is faster.

---

## Prerequisites

- Recent full backup available in S3: `aws s3 ls s3://argus-backup/daily/` must return at least one `.dump` file
- WAL archive continuous since the latest full backup — verify: `aws s3 ls s3://argus-wal/argus/ | tail -10` shows recent segments
- `psql`, `pg_restore`, `aws-cli`, `docker`, `docker compose` installed on operator machine
- `ARGUS_WAL_BUCKET` env set: `export ARGUS_WAL_BUCKET=argus-wal`
- Operator has AWS credentials with read access to `argus-backup` and `argus-wal` buckets
- Target recovery timestamp identified (UTC): e.g., `2026-04-12 14:30:00 UTC`

---

## Estimated Duration

| Step | Duration |
|------|----------|
| Steps 1–2 (announce + snapshot) | 5–10 min |
| Step 3 (download full backup, ~5 GB) | 10–20 min depending on network |
| Step 4 (restore base, ~5 GB dump) | 20–40 min |
| Steps 5–6 (write recovery config + start) | 2–5 min |
| WAL replay (variable) | 5–60 min per hour of WAL to replay |
| Steps 7–8 (verify + bring up argus) | 5 min |
| **Total** | **~1–2 hours typical** |

---

## Procedure

### 1. Announce + stop argus

Notify the team via incident channel before starting. No writes should reach the database during recovery.

```bash
# Stop the application layer only — postgres continues running until step 2
docker compose -f deploy/docker-compose.yml stop argus nginx

# Confirm argus is down
docker compose -f deploy/docker-compose.yml ps
# Expected: argus-app and argus-nginx show status "exited"
```

### 2. Snapshot current pgdata (safety net)

Before touching pgdata, snapshot the current volume so you can roll back this recovery attempt if something goes wrong.

```bash
docker compose -f deploy/docker-compose.yml stop postgres

docker run --rm \
  -v argus_pgdata:/src \
  -v /tmp:/backup \
  alpine tar czf /backup/pgdata-$(date +%Y%m%d%H%M%S).tar.gz -C /src .
# Expected: creates /tmp/pgdata-YYYYMMDDHHMMSS.tar.gz (~2–5 GB compressed)
# Expected duration: 3–8 minutes

ls -lh /tmp/pgdata-*.tar.gz
# Expected: file size > 100 MB (sanity check that archive is not empty)
```

Keep this file until recovery is fully verified.

### 3. Download latest full backup

Identify the backup nearest to (but before) the target recovery time:

```bash
aws s3 ls s3://argus-backup/daily/ --human-readable
# Expected output lists .dump files by date, e.g.:
#   2026-04-12 03:00:01  4.8 GiB  20260412_030000.dump

# Download the most recent backup before your target time
aws s3 cp s3://argus-backup/daily/20260412_030000.dump /tmp/base.dump
# Expected: progress shown, exits 0
# Expected duration: 10–20 min for a 5 GB dump over typical VPN/cloud link
```

Verify checksum if the backup bucket stores checksums:

```bash
aws s3 cp s3://argus-backup/daily/20260412_030000.dump.sha256 /tmp/base.dump.sha256
sha256sum -c /tmp/base.dump.sha256
# Expected: /tmp/base.dump: OK
```

### 4. Restore base to fresh pgdata

```bash
# Remove the corrupted/dirty data volume and create a clean one
docker volume rm argus_pgdata
docker volume create argus_pgdata

# Start postgres with empty data dir so initdb runs
docker compose -f deploy/docker-compose.yml up -d postgres
# Wait ~30 seconds for initdb to complete
sleep 30

# Verify postgres is accepting connections
docker compose -f deploy/docker-compose.yml exec postgres pg_isready -U argus
# Expected: /var/run/postgresql:5432 - accepting connections

# Run pg_restore while postgres IS RUNNING (pg_restore is a client tool — it
# requires a live server). Connect to the 'postgres' maintenance database when
# using --create, because --create issues DROP DATABASE argus + CREATE DATABASE
# argus, which cannot be executed while connected to 'argus' itself.
docker compose -f deploy/docker-compose.yml exec \
  -e PGPASSWORD="${POSTGRES_PASSWORD:-argus_secret}" \
  postgres \
  pg_restore \
    --host=127.0.0.1 \
    --port=5432 \
    --username=argus \
    --dbname=postgres \
    --format=custom \
    --create \
    --clean \
    --no-acl \
    --no-owner \
    /tmp/base.dump
# Expected: some "already exists" notices (normal with --clean), exits 0
# Expected duration: 20–40 min for a 5 GB dump
```

### 5. Write recovery.signal + postgresql.auto.conf

This tells PostgreSQL to enter recovery mode and replay WAL up to the target time.

Replace `YYYY-MM-DD HH:MM:SS UTC` with your actual target timestamp.

```bash
docker run --rm \
  -v argus_pgdata:/var/lib/postgresql/data \
  alpine sh -c '
    touch /var/lib/postgresql/data/recovery.signal
    cat >> /var/lib/postgresql/data/postgresql.auto.conf <<EOF
restore_command = '"'"'aws s3 cp s3://'"'"'${ARGUS_WAL_BUCKET:-argus-wal}'"'"'/argus/%f %p'"'"'
recovery_target_time = '"'"'YYYY-MM-DD HH:MM:SS UTC'"'"'
recovery_target_action = promote
EOF'

# Verify the files were written
docker run --rm \
  -v argus_pgdata:/var/lib/postgresql/data \
  alpine cat /var/lib/postgresql/data/postgresql.auto.conf
# Expected: last 3 lines contain restore_command, recovery_target_time, recovery_target_action
```

### 6. Start postgres (it replays WAL to target)

```bash
docker compose -f deploy/docker-compose.yml up -d postgres

# Tail the postgres log — you will see WAL replay progress
docker compose -f deploy/docker-compose.yml logs -f postgres
# Expected lines during replay:
#   LOG:  starting point-in-time recovery to YYYY-MM-DD HH:MM:SS+00
#   LOG:  restored log file "000000010000000X000000YY" from archive
#   LOG:  recovery stopping before commit of transaction ..., time YYYY-MM-DD HH:MM:SS
#   LOG:  pausing at the end of recovery
#   LOG:  database system is ready to accept read only connections
# Then (after promote):
#   LOG:  database system is ready to accept connections

# Wait for recovery to complete (WAL replay can take 5–60 min per hour of log)
# Press Ctrl+C when you see "ready to accept connections"
```

### 7. Confirm state with pg_controldata

```bash
docker compose -f deploy/docker-compose.yml exec postgres pg_controldata | grep -E "state|checkpoint"
# Expected:
#   Database cluster state:               in production
#   Latest checkpoint location:           X/XXXXXXXX
#   Prior checkpoint location:            X/XXXXXXXX
# "in production" confirms recovery completed and postgres promoted to primary.

# Spot-check data near the recovery target
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -c "SELECT now(), count(*) FROM audit_logs WHERE created_at > now() - interval '1 hour';"
# Verify the row count makes sense for your workload
```

### 8. Smoke test + restart argus

```bash
# Health check postgres directly
docker compose -f deploy/docker-compose.yml exec postgres pg_isready -U argus
# Expected: accepting connections

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

Clean up the safety snapshot only after full verification:

```bash
rm /tmp/base.dump /tmp/pgdata-*.tar.gz
```

---

## Rollback if restore fails

If at any point the recovery is not progressing or data looks wrong:

```bash
# Stop postgres immediately
docker compose -f deploy/docker-compose.yml stop postgres

# Restore the safety snapshot from step 2
docker volume rm argus_pgdata
docker volume create argus_pgdata
docker run --rm \
  -v argus_pgdata:/target \
  -v /tmp:/backup \
  alpine tar xzf /backup/pgdata-YYYYMMDDHHMMSS.tar.gz -C /target

# Restart the stack
docker compose -f deploy/docker-compose.yml up -d
```

Then contact the on-call DBA with the postgres logs from the failed attempt.

---

## Related Runbooks

- [deploy.md](deploy.md) — Standard deploy procedure; a pre-deploy snapshot is always taken before production deploys and is the recommended starting point for the full backup used in Step 3
- [rollback.md](rollback.md) — If the PITR is being performed to support a deployment rollback (e.g., a bad migration), coordinate with rollback.md to ensure the application version is reverted to match the recovered database schema

## Verification via API

After argus is fully restored and the stack is running, perform a final end-to-end API check:

```bash
curl -sf http://localhost:8084/api/v1/status | jq
# Expected: {"status":"ok", "db":"up", "cache":"up", "nats":"up", "version":"..."}
```

This confirms not just that postgres is up, but that argus can connect to all dependencies and is serving traffic correctly.
