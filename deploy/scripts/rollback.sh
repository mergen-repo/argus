#!/bin/bash
# rollback.sh — Blue-green rollback for Argus
# Reverts to a previous image using the pre-deploy snapshot and optionally
# restores the database from the snapshot's backup file.
#
# Usage:
#   rollback.sh <git-tag-or-sha> [--with-db-restore] [--yes]
#
#   Makefile path: rollback.sh $(VERSION) $(WITH_DB_RESTORE)
#     where WITH_DB_RESTORE is "true" or "false"
#
# Examples:
#   rollback.sh v1.2.3
#   rollback.sh v1.2.3 --with-db-restore
#   rollback.sh v1.2.3 true --yes
#   VERSION=v1.2.3 WITH_DB_RESTORE=true make rollback

set -euo pipefail

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
VERSION=""
WITH_DB_RESTORE=false
YES=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --with-db-restore) WITH_DB_RESTORE=true; shift ;;
    --yes)             YES=true;             shift ;;
    true)              WITH_DB_RESTORE=true; shift ;;
    false)             WITH_DB_RESTORE=false; shift ;;
    -*)                die "Unknown flag: $1. Run with --help for usage." ;;
    *)
      if [[ -z "$VERSION" ]]; then
        VERSION="$1"
      else
        die "Unexpected argument: $1"
      fi
      shift
      ;;
  esac
done

[[ -z "$VERSION" ]] && die "Usage: $(basename "$0") <git-tag-or-sha> [--with-db-restore] [--yes]"

# ─── Resolve paths ────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SNAPSHOTS_DIR="${REPO_ROOT}/deploy/snapshots"
REGISTRY="${ARGUS_REGISTRY:-ghcr.io/argus}"
TS="$(date -u +%Y%m%dT%H%M%SZ)"

# ─── Banner ───────────────────────────────────────────────────────────────────
echo ""
log_warn "=========================================================="
log_warn " ARGUS ROLLBACK"
log_warn " Version:         ${VERSION}"
log_warn " With DB restore: ${WITH_DB_RESTORE}"
log_warn "=========================================================="
echo ""

# ─── Confirmation ─────────────────────────────────────────────────────────────
if [[ "$YES" != "true" ]]; then
  log_warn "This will replace the running image and may cause brief downtime."
  [[ "$WITH_DB_RESTORE" == "true" ]] && log_warn "DB RESTORE is ENABLED — database will be overwritten from backup."
  echo ""
  read -r -p "Type 'yes-rollback' to proceed: " CONFIRM
  [[ "$CONFIRM" == "yes-rollback" ]] || die "Aborted by user."
fi

# ─── Find snapshot ────────────────────────────────────────────────────────────
log_info "[Step 1] Locating snapshot for version=${VERSION} ..."

SNAPSHOT_FILE=""

# Try to find a snapshot whose git_sha starts with VERSION (or exact match)
if [[ -d "$SNAPSHOTS_DIR" ]]; then
  for f in "${SNAPSHOTS_DIR}"/*.json; do
    [[ -f "$f" ]] || continue
    sha=$(grep '"git_sha"' "$f" 2>/dev/null | sed 's/.*"git_sha"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' || true)
    if [[ "$sha" == "$VERSION"* ]] || [[ "$VERSION" == "$sha"* ]]; then
      SNAPSHOT_FILE="$f"
      break
    fi
  done

  # Fall back to most recent snapshot
  if [[ -z "$SNAPSHOT_FILE" ]]; then
    log_warn "No snapshot matched version=${VERSION} — using most recent snapshot"
    SNAPSHOT_FILE=$(ls -t "${SNAPSHOTS_DIR}"/*.json 2>/dev/null | head -1 || true)
  fi
fi

if [[ -z "$SNAPSHOT_FILE" ]] || [[ ! -f "$SNAPSHOT_FILE" ]]; then
  die "No snapshot found in ${SNAPSHOTS_DIR}. Cannot determine previous image SHA."
fi

log_ok "Using snapshot: ${SNAPSHOT_FILE}"

# ─── Parse snapshot ───────────────────────────────────────────────────────────
PREV_IMAGE_SHA=$(grep '"prev_image_sha"' "$SNAPSHOT_FILE" | sed 's/.*"prev_image_sha"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' || true)
PREV_BACKUP_FILE=$(grep '"prev_backup_file"' "$SNAPSHOT_FILE" | sed 's/.*"prev_backup_file"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' || true)

[[ -z "$PREV_IMAGE_SHA" ]] && die "prev_image_sha is empty in snapshot. Cannot rollback."

log_info "prev_image_sha:   ${PREV_IMAGE_SHA}"
log_info "prev_backup_file: ${PREV_BACKUP_FILE:-<none>}"

# ─── Step 2: Re-tag old image ─────────────────────────────────────────────────
log_info "[Step 2] Re-tagging previous image for reference..."
ROLLBACK_TAG="${REGISTRY}/argus:rollback-${TS}"
docker tag "${PREV_IMAGE_SHA}" "${ROLLBACK_TAG}" 2>/dev/null \
  || log_warn "Could not re-tag image ${PREV_IMAGE_SHA} — image may no longer be local"
log_ok "Tagged as: ${ROLLBACK_TAG}"

# ─── Step 3: Blue-green flip back to old image ────────────────────────────────
log_info "[Step 3] Invoking blue-green flip with previous image..."
"${SCRIPT_DIR}/bluegreen-flip.sh" rollback "--image=${PREV_IMAGE_SHA}"
log_ok "Blue-green flip complete"

# ─── Step 4: DB restore (optional) ───────────────────────────────────────────
if [[ "$WITH_DB_RESTORE" == "true" ]]; then
  log_info "[Step 4] DB restore requested — invoking pitr-restore.sh ..."
  if [[ -z "$PREV_BACKUP_FILE" ]]; then
    die "WITH_DB_RESTORE=true but snapshot has no prev_backup_file. Aborting DB restore."
  fi
  "${SCRIPT_DIR}/pitr-restore.sh" "--from=${PREV_BACKUP_FILE}"
  log_ok "DB restore complete"
else
  log_info "[Step 4] DB restore skipped (WITH_DB_RESTORE=false)"
fi

# ─── Step 5: Smoke test ───────────────────────────────────────────────────────
log_info "[Step 5] Running smoke test..."
"${SCRIPT_DIR}/smoke-test.sh" "localhost:8084"
log_ok "Smoke test passed"

# ─── Step 6: Audit event ─────────────────────────────────────────────────────
log_info "[Step 6] Appending rollback audit event..."
ARGUS_HOST="${ARGUS_HOST:-localhost:8084}"
ARGUS_API_URL="${ARGUS_API_URL:-http://${ARGUS_HOST}}"
ENTITY_ID="rollback-${VERSION}-${TS}"
AFTER_DATA="{\"version\":\"${VERSION}\",\"with_db_restore\":${WITH_DB_RESTORE},\"snapshot\":\"$(basename "${SNAPSHOT_FILE}")\",\"prev_image_sha\":\"${PREV_IMAGE_SHA}\",\"git_sha\":\"${PREV_GIT_SHA:-unknown}\",\"actor\":\"${USER:-ci}\",\"ts\":\"${TS}\"}"
AUDIT_PAYLOAD="{\"action\":\"rollback\",\"entity_type\":\"deployment\",\"entity_id\":\"${ENTITY_ID}\",\"after_data\":${AFTER_DATA}}"

[[ -z "${ARGUS_API_TOKEN:-}" ]] && die "ARGUS_API_TOKEN required for audit emission (super_admin JWT)"
AUDIT_RESP_FILE="/tmp/argus_audit_resp.$$"
set +e
HTTP_STATUS=$(curl -sS -o "${AUDIT_RESP_FILE}" -w "%{http_code}" --max-time 10 -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${ARGUS_API_TOKEN}" \
  -d "${AUDIT_PAYLOAD}" \
  "${ARGUS_API_URL}/api/v1/audit/system-events")
CURL_EXIT=$?
set -e
if [[ "${CURL_EXIT}" -ne 0 ]]; then
    rm -f "${AUDIT_RESP_FILE}"
    die "Audit event delivery failed (curl exit ${CURL_EXIT}, HTTP ${HTTP_STATUS:-000})"
fi
if [[ "${HTTP_STATUS}" == "201" || "${HTTP_STATUS}" == "200" ]]; then
    log_ok "Audit event emitted (HTTP ${HTTP_STATUS})"
    rm -f "${AUDIT_RESP_FILE}"
else
    RESP_BODY=$(cat "${AUDIT_RESP_FILE}" 2>/dev/null || echo "<no body>")
    rm -f "${AUDIT_RESP_FILE}"
    die "Audit event delivery failed (HTTP ${HTTP_STATUS}): ${RESP_BODY}"
fi

# ─── Done ─────────────────────────────────────────────────────────────────────
echo ""
log_ok "=========================================================="
log_ok " ROLLBACK COMPLETE"
log_ok " Rolled back to:   ${PREV_IMAGE_SHA}"
log_ok " DB restore:       ${WITH_DB_RESTORE}"
log_ok " Snapshot used:    $(basename "${SNAPSHOT_FILE}")"
log_ok "=========================================================="
echo ""
