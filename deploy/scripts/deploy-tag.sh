#!/bin/bash
# deploy-tag.sh — Creates and pushes a git deploy tag for Argus
# Tags the current HEAD as deploy-<env>-<ISO8601> and pushes to origin.
#
# Usage:
#   deploy-tag.sh <env>
#
# Examples:
#   deploy-tag.sh staging
#   deploy-tag.sh prod

set -euo pipefail

# ─── Colours ─────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

log_info()  { echo -e "${CYAN}[INFO]${NC}  $*"; }
log_ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
die()       { log_error "$*"; exit 1; }

# ─── Arguments ────────────────────────────────────────────────────────────────
ENV="${1:-}"
[[ -z "$ENV" ]] && die "Usage: $(basename "$0") <env>  (e.g. staging, prod)"

# ─── Resolve repo root ────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

command -v git > /dev/null 2>&1 || die "git is not installed or not in PATH"
git -C "${REPO_ROOT}" rev-parse HEAD > /dev/null 2>&1 \
  || die "Not a git repository: ${REPO_ROOT}"

# ─── Tag ──────────────────────────────────────────────────────────────────────
TAG="deploy-${ENV}-$(date -u +%Y%m%dT%H%M%SZ)"

log_info "Creating git tag: ${TAG}"
git -C "${REPO_ROOT}" tag "${TAG}"

log_info "Pushing tag to origin..."
git -C "${REPO_ROOT}" push origin "${TAG}"

log_ok "Tagged and pushed: ${TAG}"
echo "${TAG}"
