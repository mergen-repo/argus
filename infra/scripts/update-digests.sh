#!/bin/bash
# update-digests.sh — Argus
# Resolves current pinned image tags to latest digests and rewrites files in place.
#
# Usage:
#   update-digests.sh              # Update all files in place
#   update-digests.sh --check      # Exit 1 if any digest is stale (for CI monthly runs)
#
# Requirements:
#   - docker CLI available and authenticated for any private registries
#   - Run from the argus repository root (or any directory; files are resolved relative to script)
#
# How it works:
#   - Iterates over a target list of (file, image, tag) tuples
#   - For each entry: calls `docker buildx imagetools inspect` to resolve the current multi-arch
#     manifest digest for that tag
#   - Compares against the digest already written in the file
#   - In normal mode: rewrites the file if the digest differs
#   - In --check mode: reports drift and exits 1 without modifying any file

set -euo pipefail

# ─── Paths ───────────────────────────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

DOCKERFILE="${REPO_ROOT}/infra/docker/Dockerfile.argus"
COMPOSEFILE="${REPO_ROOT}/deploy/docker-compose.yml"

# ─── Mode ────────────────────────────────────────────────────────────────────

CHECK_MODE=false
if [[ "${1:-}" == "--check" ]]; then
  CHECK_MODE=true
fi

# ─── Target list ─────────────────────────────────────────────────────────────
# Format: "file|image|tag"
# Each entry describes one pinned FROM or image: line to maintain.

TARGETS=(
  "${DOCKERFILE}|golang|1.25-alpine"
  "${DOCKERFILE}|node|20-alpine"
  "${DOCKERFILE}|alpine|3.19"
  "${COMPOSEFILE}|timescale/timescaledb|latest-pg16"
  "${COMPOSEFILE}|redis|7-alpine"
  "${COMPOSEFILE}|nats|2.10-alpine"
  "${COMPOSEFILE}|nginx|alpine"
  "${COMPOSEFILE}|edoburu/pgbouncer|latest"
)

# ─── Helpers ─────────────────────────────────────────────────────────────────

log()  { echo "[update-digests] $*"; }
warn() { echo "[update-digests] WARN: $*" >&2; }
err()  { echo "[update-digests] ERROR: $*" >&2; }

check_docker() {
  if ! command -v docker &>/dev/null; then
    warn "docker not found in PATH — cannot resolve digests"
    if $CHECK_MODE; then
      err "docker is required in --check mode"
      exit 1
    fi
    log "Skipping all updates (docker unavailable)"
    exit 0
  fi

  if ! docker info &>/dev/null; then
    warn "Docker daemon is not running or not accessible"
    if $CHECK_MODE; then
      err "Docker daemon must be running in --check mode"
      exit 1
    fi
    log "Skipping all updates (docker daemon unavailable)"
    exit 0
  fi
}

resolve_digest() {
  local image="$1"
  local tag="$2"
  docker buildx imagetools inspect "${image}:${tag}" --format '{{json .Manifest}}' 2>/dev/null \
    | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('digest',''))" 2>/dev/null \
    || true
}

extract_current_digest() {
  local file="$1"
  local image="$2"
  local tag="$3"
  grep -E "${image}:${tag}@sha256:[a-f0-9]+" "${file}" 2>/dev/null \
    | head -1 \
    | sed -E 's|.*'"${image}:${tag}"'@(sha256:[a-f0-9]+).*|\1|' \
    || true
}

rewrite_digest() {
  local file="$1"
  local image="$2"
  local tag="$3"
  local old_digest="$4"
  local new_digest="$5"

  if [[ -z "${old_digest}" ]]; then
    warn "${image}:${tag} — no pinned entry found in ${file}, skipping rewrite"
    return
  fi

  sed -i.bak \
    "s|${image}:${tag}@${old_digest}|${image}:${tag}@${new_digest}|g" \
    "${file}"
  rm -f "${file}.bak"
}

# ─── Main ────────────────────────────────────────────────────────────────────

check_docker

DRIFT_COUNT=0

for target in "${TARGETS[@]}"; do
  IFS='|' read -r file image tag <<< "${target}"

  log "Checking ${image}:${tag} in ${file##*/} ..."

  current_digest="$(extract_current_digest "${file}" "${image}" "${tag}")"
  latest_digest="$(resolve_digest "${image}" "${tag}")"

  if [[ -z "${latest_digest}" ]]; then
    warn "Could not resolve digest for ${image}:${tag} — skipping"
    continue
  fi

  if [[ "${current_digest}" == "${latest_digest}" ]]; then
    log "  OK  — digest is current: ${latest_digest:0:19}..."
    continue
  fi

  if [[ -z "${current_digest}" ]]; then
    warn "  MISSING — ${image}:${tag} not yet pinned in ${file}; latest is ${latest_digest}"
    DRIFT_COUNT=$((DRIFT_COUNT + 1))
    continue
  fi

  log "  DRIFT — old: ${current_digest:0:19}..."
  log "          new: ${latest_digest:0:19}..."

  if $CHECK_MODE; then
    DRIFT_COUNT=$((DRIFT_COUNT + 1))
  else
    rewrite_digest "${file}" "${image}" "${tag}" "${current_digest}" "${latest_digest}"
    log "  UPDATED in ${file##*/}"
  fi
done

if $CHECK_MODE; then
  if [[ "${DRIFT_COUNT}" -gt 0 ]]; then
    err "${DRIFT_COUNT} image(s) have stale digests — run update-digests.sh to refresh"
    exit 1
  fi
  log "All ${#TARGETS[@]} images are up to date."
  exit 0
fi

log "Done. ${#TARGETS[@]} images processed."
