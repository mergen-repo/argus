# Database Disk Full

## When to use

- Alert fires: `argus_disk_usage_percent{mount="/var/lib/postgresql/data"} > 85`
- PostgreSQL begins rejecting writes with `ENOSPC` errors
- `argus` application logs show `pq: could not extend file` errors
- CDR ingestion stops or stalls with write-failure audit events

## Prerequisites

- `docker`, `docker compose` installed on operator machine
- `aws-cli` configured with write access to `argus-backup` S3 bucket
- `psql` access to the running database (or exec into the container)
- Operator has DBA-level PostgreSQL access (`SUPERUSER` or `pg_monitor` + DDL rights)
- TimescaleDB extension available (installed by default in Argus stack)

## Estimated Duration

| Step | Expected time |
|------|---------------|
| Step 1 — Assess disk state | 2–5 min |
| Step 2 — Stop non-critical writes | 1–2 min |
| Step 3 — Identify largest partitions | 5 min |
| Step 4 — Archive old CDR partitions to S3 | 10–30 min per partition |
| Step 5 — Drop archived partitions | 2–5 min |
| Step 6 — VACUUM + reclaim space | 5–20 min |
| Step 7 — Resume and verify | 5 min |
| **Total** | **~30–70 min** |

---

## Procedure

### 1. Assess current disk state

```bash
# Check disk usage on the host
df -h /var/lib/docker/volumes/

# Check disk usage inside the postgres container
docker compose -f deploy/docker-compose.yml exec postgres \
  df -h /var/lib/postgresql/data
# Expected: shows current use% — above 85% is the trigger threshold

# Confirm the metric value in Prometheus
curl -s 'http://localhost:9090/api/v1/query?query=argus_disk_usage_percent%7Bmount%3D%22%2Fvar%2Flib%2Fpostgresql%2Fdata%22%7D' | jq .
# Expected: value field shows current percentage

# Identify which database objects consume most space
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -c "
    SELECT
      relname AS table_name,
      pg_size_pretty(pg_total_relation_size(oid)) AS total_size,
      pg_size_pretty(pg_relation_size(oid)) AS table_size,
      pg_size_pretty(pg_total_relation_size(oid) - pg_relation_size(oid)) AS index_size
    FROM pg_class
    WHERE relkind IN ('r','p')
    ORDER BY pg_total_relation_size(oid) DESC
    LIMIT 20;
  "
# Expected: cdr_records and its partitions typically dominate
```

### 2. Stop non-critical write paths temporarily

Stop the background CDR ingestion job to reduce write pressure while you reclaim space. The application itself stays up for read traffic.

```bash
# Pause CDR ingestion job via argusctl (prevents new writes during cleanup)
argusctl job pause cdr-ingestion
# Expected: Job 'cdr-ingestion' paused successfully

# Verify the job is paused
argusctl job status cdr-ingestion
# Expected: status=paused
```

### 3. Identify old CDR partitions available to drop

Argus uses TimescaleDB hypertable partitioning on `cdr_records` by `started_at`. Partitions older than the retention policy are safe to archive and drop.

```bash
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -c "
    SELECT
      chunk_schema,
      chunk_name,
      range_start,
      range_end,
      pg_size_pretty(total_bytes) AS size
    FROM timescaledb_information.chunks
    WHERE hypertable_name = 'cdr_records'
      AND range_end < now() - INTERVAL '90 days'
    ORDER BY range_start ASC;
  "
# Expected: list of chunks with their date ranges and sizes
# These are candidates for archival before dropping
```

### 4. Archive old CDR partitions to S3

For each chunk identified above, export its data to S3 before dropping. Replace `CHUNK_NAME` with actual chunk names.

```bash
# Export chunk data as CSV and upload to S3
CHUNK_NAME="_timescaledb_internal._hyper_1_1_chunk"
EXPORT_DATE=$(date +%Y%m%d%H%M%S)

docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -c "\COPY (SELECT * FROM ${CHUNK_NAME}) TO STDOUT WITH CSV HEADER" \
  | aws s3 cp - s3://argus-backup/cdr-archive/${CHUNK_NAME}_${EXPORT_DATE}.csv
# Expected: data streams to S3, exits 0

# Verify the archive arrived
aws s3 ls s3://argus-backup/cdr-archive/ | grep "${CHUNK_NAME}"
# Expected: file listed with non-zero size
```

Repeat for each chunk. For large environments with many chunks, use the batch script:

```bash
# List all archivable chunks and archive in sequence
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -t -c "
    SELECT chunk_schema || '.' || chunk_name
    FROM timescaledb_information.chunks
    WHERE hypertable_name = 'cdr_records'
      AND range_end < now() - INTERVAL '90 days';
  " | while read chunk; do
    chunk=$(echo $chunk | xargs)
    [ -z "$chunk" ] && continue
    echo "Archiving $chunk ..."
    docker compose -f deploy/docker-compose.yml exec -T postgres \
      psql -U argus -d argus -c "\COPY (SELECT * FROM ${chunk}) TO STDOUT WITH CSV HEADER" \
      | aws s3 cp - "s3://argus-backup/cdr-archive/${chunk}_$(date +%Y%m%d).csv"
    echo "Done: $chunk"
  done
```

### 5. Drop archived partitions

After confirming each chunk's S3 archive, drop the chunk to reclaim disk space.

```bash
# Drop chunks older than 90 days using TimescaleDB API
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -c "
    SELECT drop_chunks('cdr_records', TIMESTAMPTZ 'now() - INTERVAL 90 days');
  "
# Expected: returns number of chunks dropped, e.g., (3 rows)

# Verify the chunks are gone
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -c "
    SELECT count(*) FROM timescaledb_information.chunks
    WHERE hypertable_name = 'cdr_records'
      AND range_end < now() - INTERVAL '90 days';
  "
# Expected: count = 0
```

### 6. VACUUM to reclaim space

PostgreSQL does not immediately return disk space to the OS after DROP. Run VACUUM to reclaim it.

```bash
# VACUUM FULL on cdr_records to return pages to OS (will briefly lock the table)
# WARNING: VACUUM FULL acquires AccessExclusiveLock — run during low-traffic window
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -c "VACUUM FULL VERBOSE cdr_records;"
# Expected: outputs vacuum progress, exits after completion

# Check disk usage again
docker compose -f deploy/docker-compose.yml exec postgres \
  df -h /var/lib/postgresql/data
# Expected: use% should have dropped significantly

# If VACUUM FULL lock is unacceptable, use regular VACUUM + pg_repack instead:
# docker compose -f deploy/docker-compose.yml exec postgres \
#   psql -U argus -d argus -c "VACUUM ANALYZE cdr_records;"
```

### 7. Resume CDR ingestion and verify

```bash
# Resume the CDR ingestion job
argusctl job resume cdr-ingestion
# Expected: Job 'cdr-ingestion' resumed successfully

# Confirm disk usage is below threshold
docker compose -f deploy/docker-compose.yml exec postgres \
  df -h /var/lib/postgresql/data
# Expected: use% < 70%

# Health check
curl -sf http://localhost:8084/health/ready | jq
# Expected: {"status":"ok", ...}

# Verify CDR records are being written again
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -c "
    SELECT count(*), max(started_at)
    FROM cdr_records
    WHERE started_at > now() - INTERVAL '5 minutes';
  "
# Expected: count > 0 and max(started_at) is recent
```

---

## Verification

- `argus_disk_usage_percent{mount="/var/lib/postgresql/data"}` < 80
- `curl http://localhost:8084/health/ready` returns 200
- CDR ingestion job status shows `running`
- No `ENOSPC` or `could not extend file` errors in argus logs in the past 10 minutes

---

## Post-incident

- Audit log entry: `argusctl audit log --action=db_disk_cleanup --resource=cdr_records --note="dropped partitions <dates>, archived to S3"`
- Review TimescaleDB retention policy and set `drop_chunks` via scheduled job if not already automated
- Raise a ticket to add disk capacity if cleanup only bought < 30 days of runway
- **Comms template (incident channel):**
  > `[RESOLVED] DB disk full alert resolved. Archived and dropped CDR partitions for <date range>. Disk usage: <before>% → <after>%. CDR ingestion resumed. Root cause: retention policy not enforced. Action item: automate chunk pruning.`
- **Stakeholder email:**
  > Subject: [Argus] Database disk space incident resolved
  > Body: Disk usage on the PostgreSQL volume exceeded 85% at <time>. CDR partitions from <date range> were archived to S3 and dropped. CDR ingestion was paused for <duration> minutes. Service impact: write operations paused briefly, reads unaffected. Data archived is accessible in S3 at `argus-backup/cdr-archive/`. Preventive action: scheduled retention job configured.
