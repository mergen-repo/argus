#!/bin/bash
# deploy-snapshot.sh — Pre-deploy state snapshot for Argus
# Captures current image SHA, latest backup file, git SHA, and timestamp
# into deploy/snapshots/<ISO8601>.json before a blue-green deploy.
#
# Usage:
#   deploy-snapshot.sh <env>
#
# Output:
#   Prints the snapshot file path to stdout so callers can capture it.
#
# Example:
#   SNAPSHOT=$(deploy/scripts/deploy-snapshot.sh staging)

set -euo pipefail

# ─── Colours ─────────────────────────────────────────────────────────────────
RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

log_info()  { echo -e "${CYAN}[INFO]${NC}  $*" >&2; }
log_ok()    { echo -e "${GREEN}[OK]${NC}    $*" >&2; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*" >&2; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
die()       { log_error "$*"; exit 1; }

# ─── Arguments ────────────────────────────────────────────────────────────────
ENV="${1:-}"
[[ -z "$ENV" ]] && die "Usage: $(basename "$0") <env>  (e.g. staging, prod)"

# ─── Resolve repo root ────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SNAPSHOTS_DIR="${REPO_ROOT}/deploy/snapshots"
BACKUPS_DIR="${REPO_ROOT}/backups"

# ─── Gather state ─────────────────────────────────────────────────────────────
log_info "Gathering pre-deploy state for env=${ENV} ..."

# 1. Previous image SHA from running container (argus-app)
PREV_IMAGE_SHA=""
if docker inspect --format='{{.Image}}' argus-app > /dev/null 2>&1; then
  PREV_IMAGE_SHA="$(docker inspect --format='{{.Image}}' argus-app)"
  log_ok "prev_image_sha: ${PREV_IMAGE_SHA}"
else
  log_warn "Container argus-app not running — prev_image_sha will be empty"
fi

# 2. Latest backup file in backups/ (may be empty)
PREV_BACKUP_FILE=""
if [[ -d "$BACKUPS_DIR" ]]; then
  LATEST=$(ls -t "${BACKUPS_DIR}"/*.sql 2>/dev/null | head -1 || true)
  if [[ -n "$LATEST" ]]; then
    PREV_BACKUP_FILE="backups/$(basename "$LATEST")"
    log_ok "prev_backup_file: ${PREV_BACKUP_FILE}"
  else
    log_warn "No .sql files found in backups/ — prev_backup_file will be empty"
  fi
else
  log_warn "backups/ directory does not exist — prev_backup_file will be empty"
fi

# 3. Current git SHA
GIT_SHA=""
if command -v git > /dev/null 2>&1 && git -C "${REPO_ROOT}" rev-parse HEAD > /dev/null 2>&1; then
  GIT_SHA="$(git -C "${REPO_ROOT}" rev-parse HEAD)"
  log_ok "git_sha: ${GIT_SHA}"
else
  log_warn "git not available or not a git repo — git_sha will be empty"
fi

# 4. Timestamp
STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ISO_FILENAME="$(date -u +%Y%m%dT%H%M%SZ)"

# ─── Write snapshot ───────────────────────────────────────────────────────────
mkdir -p "${SNAPSHOTS_DIR}"
SNAPSHOT_FILE="${SNAPSHOTS_DIR}/${ISO_FILENAME}.json"

cat > "${SNAPSHOT_FILE}" <<EOF
{
  "env": "${ENV}",
  "prev_image_sha": "${PREV_IMAGE_SHA}",
  "prev_backup_file": "${PREV_BACKUP_FILE}",
  "git_sha": "${GIT_SHA}",
  "started_at": "${STARTED_AT}"
}
EOF

log_ok "Snapshot written: ${SNAPSHOT_FILE}"

# ─── Output ───────────────────────────────────────────────────────────────────
echo "${SNAPSHOT_FILE}"
