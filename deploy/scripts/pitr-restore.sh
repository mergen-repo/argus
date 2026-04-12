#!/bin/bash
# PITR Restore Script — Argus
# Automates the Point-in-Time Recovery procedure described in docs/runbook/dr-pitr.md
#
# Usage:
#   pitr-restore.sh --target-time "YYYY-MM-DD HH:MM:SS UTC" \
#                   [--bucket argus-backup] \
#                   [--wal-bucket argus-wal] \
#                   [--wal-prefix argus/] \
#                   [--scratch-dir /tmp/pitr]
#
# Requirements:
#   - aws-cli configured with read access to backup + WAL buckets
#   - docker and docker compose available
#   - Run from the argus repository root (or pass --compose-file)

set -euo pipefail

# ─── Defaults ────────────────────────────────────────────────────────────────
TARGET_TIME=""
BUCKET="${ARGUS_BACKUP_BUCKET:-argus-backup}"
WAL_BUCKET="${ARGUS_WAL_BUCKET:-argus-wal}"
WAL_PREFIX="${ARGUS_WAL_PREFIX:-argus/}"
SCRATCH_DIR="/tmp/pitr-$(date +%s)"
COMPOSE_FILE="deploy/docker-compose.yml"
PGDATA_VOLUME="argus_pgdata"
POSTGRES_USER="${POSTGRES_USER:-argus}"
POSTGRES_DB="${POSTGRES_DB:-argus}"
DRY_RUN=false

# ─── Colours ─────────────────────────────────────────────────────────────────
RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

log_info()  { echo -e "${CYAN}[INFO]${NC}  $*"; }
log_ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
die()       { log_error "$*"; exit 1; }

# ─── Argument Parsing ─────────────────────────────────────────────────────────
usage() {
  cat <<EOF
Usage: $(basename "$0") --target-time "YYYY-MM-DD HH:MM:SS UTC" [OPTIONS]

Required:
  --target-time TIME    Recovery target time in UTC (e.g. "2026-04-12 14:30:00 UTC")

Options:
  --bucket BUCKET       S3 bucket containing full backups   (default: argus-backup)
  --wal-bucket BUCKET   S3 bucket containing WAL archive    (default: argus-wal)
  --wal-prefix PREFIX   Key prefix inside WAL bucket        (default: argus/)
  --scratch-dir DIR     Local temp directory for downloads  (default: /tmp/pitr-<epoch>)
  --compose-file FILE   Path to docker-compose.yml          (default: deploy/docker-compose.yml)
  --dry-run             Print steps without executing       (default: false)
  -h, --help            Show this help

Environment:
  POSTGRES_USER         Database user  (default: argus)
  POSTGRES_DB           Database name  (default: argus)
  ARGUS_BACKUP_BUCKET   Overrides --bucket
  ARGUS_WAL_BUCKET      Overrides --wal-bucket
  ARGUS_WAL_PREFIX      Overrides --wal-prefix

See docs/runbook/dr-pitr.md for the full manual procedure and rollback steps.
EOF
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target-time)   TARGET_TIME="$2";   shift 2 ;;
    --bucket)        BUCKET="$2";        shift 2 ;;
    --wal-bucket)    WAL_BUCKET="$2";    shift 2 ;;
    --wal-prefix)    WAL_PREFIX="$2";    shift 2 ;;
    --scratch-dir)   SCRATCH_DIR="$2";   shift 2 ;;
    --compose-file)  COMPOSE_FILE="$2";  shift 2 ;;
    --dry-run)       DRY_RUN=true;       shift   ;;
    -h|--help)       usage ;;
    *) die "Unknown argument: $1. Run with --help for usage." ;;
  esac
done

# ─── Validation ───────────────────────────────────────────────────────────────
[[ -z "$TARGET_TIME" ]] && die "--target-time is required. Example: \"2026-04-12 14:30:00 UTC\""

# Validate timestamp format loosely: YYYY-MM-DD HH:MM:SS
if ! echo "$TARGET_TIME" | grep -qE '^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}'; then
  die "--target-time must be in format 'YYYY-MM-DD HH:MM:SS UTC', got: '$TARGET_TIME'"
fi

command -v docker   > /dev/null 2>&1 || die "docker is not installed or not in PATH"
command -v aws      > /dev/null 2>&1 || die "aws-cli is not installed or not in PATH"

[[ -f "$COMPOSE_FILE" ]] || die "Compose file not found: $COMPOSE_FILE"

# ─── Dry-run wrapper ──────────────────────────────────────────────────────────
run() {
  if [[ "$DRY_RUN" == "true" ]]; then
    echo -e "${YELLOW}[DRY-RUN]${NC} $*"
  else
    eval "$@"
  fi
}

# ─── Pre-flight checks ────────────────────────────────────────────────────────
log_info "Pre-flight checks..."

# Check S3 backup bucket access
aws s3 ls "s3://${BUCKET}/daily/" > /dev/null 2>&1 \
  || die "Cannot list s3://${BUCKET}/daily/ — check AWS credentials and bucket name"

# Check WAL bucket access
aws s3 ls "s3://${WAL_BUCKET}/${WAL_PREFIX}" > /dev/null 2>&1 \
  || die "Cannot list s3://${WAL_BUCKET}/${WAL_PREFIX} — check AWS credentials and wal-bucket/wal-prefix"

log_ok "S3 access verified"

# Find most recent backup before target time
# Backup filenames are YYYYMMDD_HHMMSS.dump — filter by date component only
TARGET_DATE=$(echo "$TARGET_TIME" | cut -c1-10 | tr -d '-')
LATEST_BACKUP=$(aws s3 ls "s3://${BUCKET}/daily/" \
  | grep '\.dump$' \
  | awk '{print $4}' \
  | sort \
  | awk -F'[_.]' -v td="$TARGET_DATE" '$1 <= td {last=$0} END {print last}')

[[ -z "$LATEST_BACKUP" ]] && die "No backup found in s3://${BUCKET}/daily/ at or before ${TARGET_DATE}. Cannot proceed."
log_ok "Using backup: ${LATEST_BACKUP}"

# ─── Step 1: Announce ─────────────────────────────────────────────────────────
echo ""
log_warn "=========================================================="
log_warn " PITR RESTORE — TARGET: ${TARGET_TIME}"
log_warn " Backup:  s3://${BUCKET}/daily/${LATEST_BACKUP}"
log_warn " WAL:     s3://${WAL_BUCKET}/${WAL_PREFIX}"
log_warn " Volume:  ${PGDATA_VOLUME}"
log_warn "=========================================================="
echo ""
log_warn "This will DESTROY the current database and replace it."
log_warn "All data after ${LATEST_BACKUP} will be replayed from WAL up to ${TARGET_TIME}."
echo ""
read -r -p "Type 'yes-i-understand' to proceed: " CONFIRM
[[ "$CONFIRM" == "yes-i-understand" ]] || die "Aborted by user."

# ─── Setup scratch directory ──────────────────────────────────────────────────
mkdir -p "$SCRATCH_DIR"
log_info "Scratch directory: ${SCRATCH_DIR}"

# ─── Step 2: Stop argus + snapshot pgdata ────────────────────────────────────
log_info "[Step 2] Stopping argus and nginx..."
run "docker compose -f '${COMPOSE_FILE}' stop argus nginx 2>&1 || true"

log_info "[Step 2] Stopping postgres..."
run "docker compose -f '${COMPOSE_FILE}' stop postgres"

SNAPSHOT_FILE="${SCRATCH_DIR}/pgdata-snapshot-$(date +%Y%m%d%H%M%S).tar.gz"
log_info "[Step 2] Snapshotting ${PGDATA_VOLUME} → ${SNAPSHOT_FILE} ..."
run "docker run --rm \
  -v '${PGDATA_VOLUME}:/src' \
  -v '${SCRATCH_DIR}:/backup' \
  alpine tar czf /backup/$(basename "${SNAPSHOT_FILE}") -C /src ."
log_ok "Snapshot complete: ${SNAPSHOT_FILE}"

# ─── Step 3: Download full backup ─────────────────────────────────────────────
DUMP_FILE="${SCRATCH_DIR}/base.dump"
log_info "[Step 3] Downloading s3://${BUCKET}/daily/${LATEST_BACKUP} → ${DUMP_FILE} ..."
run "aws s3 cp 's3://${BUCKET}/daily/${LATEST_BACKUP}' '${DUMP_FILE}'"
log_ok "Download complete: ${DUMP_FILE}"

# ─── Step 4: Restore base to fresh pgdata ─────────────────────────────────────
log_info "[Step 4] Recreating pgdata volume..."
run "docker volume rm '${PGDATA_VOLUME}' 2>/dev/null || true"
run "docker volume create '${PGDATA_VOLUME}'"

log_info "[Step 4] Starting postgres to run initdb..."
run "docker compose -f '${COMPOSE_FILE}' up -d postgres"
log_info "[Step 4] Waiting 30s for initdb to complete..."
sleep 30

log_info "[Step 4] Stopping postgres for pg_restore..."
run "docker compose -f '${COMPOSE_FILE}' stop postgres"

log_info "[Step 4] Running pg_restore from ${DUMP_FILE} ..."
run "docker run --rm \
  -v '${PGDATA_VOLUME}:/var/lib/postgresql/data' \
  -v '${SCRATCH_DIR}:/tmp/restore' \
  -e PGPASSWORD='${POSTGRES_PASSWORD:-argus_secret}' \
  postgres:16 \
  pg_restore \
    --format=custom \
    --dbname='postgres' \
    --create \
    --clean \
    --no-acl \
    --no-owner \
    /tmp/restore/base.dump"
log_ok "pg_restore complete"

# ─── Step 5: Write recovery.signal + postgresql.auto.conf ──────────────────────
log_info "[Step 5] Writing recovery.signal and postgresql.auto.conf ..."
RESTORE_CMD="aws s3 cp s3://${WAL_BUCKET}/${WAL_PREFIX}%f %p"
run "docker run --rm \
  -v '${PGDATA_VOLUME}:/var/lib/postgresql/data' \
  alpine sh -c '
    touch /var/lib/postgresql/data/recovery.signal
    cat >> /var/lib/postgresql/data/postgresql.auto.conf <<EOF
restore_command = '"'"'${RESTORE_CMD}'"'"'
recovery_target_time = '"'"'${TARGET_TIME}'"'"'
recovery_target_action = promote
EOF'"
log_ok "Recovery configuration written"

# ─── Step 6: Start postgres (WAL replay) ──────────────────────────────────────
log_info "[Step 6] Starting postgres — WAL replay begins now..."
log_info "         Tail logs with: docker compose -f ${COMPOSE_FILE} logs -f postgres"
log_info "         WAL replay duration: ~5–60 min per hour of WAL"
run "docker compose -f '${COMPOSE_FILE}' up -d postgres"

# Poll until postgres is ready (max 30 min)
MAX_WAIT=1800
ELAPSED=0
POLL_INTERVAL=10
log_info "[Step 6] Waiting for postgres to finish WAL replay (max ${MAX_WAIT}s)..."

if [[ "$DRY_RUN" == "false" ]]; then
  until docker compose -f "${COMPOSE_FILE}" exec postgres pg_isready -U "${POSTGRES_USER}" > /dev/null 2>&1; do
    if [[ $ELAPSED -ge $MAX_WAIT ]]; then
      die "Postgres did not become ready within ${MAX_WAIT}s. Check logs: docker compose -f ${COMPOSE_FILE} logs postgres"
    fi
    sleep $POLL_INTERVAL
    ELAPSED=$((ELAPSED + POLL_INTERVAL))
    log_info "  Still waiting... (${ELAPSED}s elapsed)"
  done
fi
log_ok "Postgres is ready"

# ─── Step 7: Confirm state with pg_controldata ────────────────────────────────
log_info "[Step 7] Checking pg_controldata state..."
run "docker compose -f '${COMPOSE_FILE}' exec postgres pg_controldata | grep -E 'state|checkpoint'"

log_info "[Step 7] Quick row-count sanity check..."
run "docker compose -f '${COMPOSE_FILE}' exec postgres \
  psql -U '${POSTGRES_USER}' -d '${POSTGRES_DB}' \
  -c \"SELECT count(*) AS sims FROM sims; SELECT count(*) AS audit_logs FROM audit_logs;\""

# ─── Step 8: Smoke test + restart argus ──────────────────────────────────────
log_info "[Step 8] Bringing argus and nginx back up..."
run "docker compose -f '${COMPOSE_FILE}' up -d argus nginx"

log_info "[Step 8] Waiting for argus to become healthy (max 120s)..."
if [[ "$DRY_RUN" == "false" ]]; then
  ELAPSED=0
  until docker compose -f "${COMPOSE_FILE}" ps argus | grep -q "(healthy)"; do
    if [[ $ELAPSED -ge 120 ]]; then
      log_warn "Argus not healthy within 120s — check logs: docker compose -f ${COMPOSE_FILE} logs argus"
      break
    fi
    sleep 5
    ELAPSED=$((ELAPSED + 5))
  done
fi

log_info "[Step 8] Smoke-testing /health/ready ..."
run "curl -sf http://localhost:8084/health/ready | (command -v jq > /dev/null 2>&1 && jq || cat)"

# ─── Done ─────────────────────────────────────────────────────────────────────
echo ""
log_ok "=========================================================="
log_ok " PITR RESTORE COMPLETE"
log_ok " Target time:     ${TARGET_TIME}"
log_ok " Backup used:     ${LATEST_BACKUP}"
log_ok " Snapshot saved:  ${SNAPSHOT_FILE}"
log_ok "=========================================================="
echo ""
log_info "Safety snapshot is at: ${SNAPSHOT_FILE}"
log_info "Remove it once you have verified the restore is correct:"
log_info "  rm -rf ${SCRATCH_DIR}"
echo ""
log_info "See docs/runbook/dr-pitr.md for manual rollback procedure if needed."
