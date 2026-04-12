#!/bin/bash
# bluegreen-flip.sh — Blue-green atomic flip with health gating
# Argus deployment — safely promotes the idle slot to live traffic.
#
# Usage:
#   bluegreen-flip.sh <env> [--image=<sha>] [--dry-run]
#
# Arguments:
#   <env>           Target environment: staging | prod
#   --image=<sha>   Optional Docker image SHA to deploy on the new slot
#   --dry-run       Print all commands without executing any of them
#
# Requirements:
#   - docker and docker compose available
#   - Run from the argus repository root
#   - infra/nginx/upstream.conf must exist and contain either
#     argus-app-blue or argus-app-green
#   - Nginx container named argus-nginx must be running

set -euo pipefail

# ─── Defaults ────────────────────────────────────────────────────────────────
ENV=""
IMAGE_SHA=""
DRY_RUN=false
DRAIN_SECONDS="${DRAIN_SECONDS:-15}"
HEALTH_TIMEOUT=120
HEALTH_INTERVAL=2
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
UPSTREAM_CONF="${REPO_ROOT}/infra/nginx/upstream.conf"
COMPOSE_BASE="${REPO_ROOT}/deploy/docker-compose.yml"

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

# ─── Dry-run wrapper ─────────────────────────────────────────────────────────
run() {
    if [[ "${DRY_RUN}" == "true" ]]; then
        echo -e "${YELLOW}[DRY-RUN]${NC} $*"
    else
        eval "$@"
    fi
}

# ─── Argument Parsing ─────────────────────────────────────────────────────────
usage() {
    cat <<EOF
Usage: $(basename "$0") <env> [OPTIONS]

Arguments:
  <env>             Target environment: staging | prod

Options:
  --image=<sha>     Docker image SHA/tag to deploy on the new slot
  --dry-run         Print commands without executing
  -h, --help        Show this help
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        staging|prod)
            ENV="$1"
            shift
            ;;
        --image=*)
            IMAGE_SHA="${1#--image=}"
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            die "Unknown argument: $1. Run with --help for usage."
            ;;
    esac
done

[[ -z "${ENV}" ]] && { usage; die "Missing required argument: <env>"; }

# ─── Pre-flight checks ───────────────────────────────────────────────────────
log_info "Pre-flight checks ..."
command -v docker  >/dev/null 2>&1 || die "docker not found in PATH"
docker compose version >/dev/null 2>&1 || die "docker compose plugin not found"
[[ -f "${UPSTREAM_CONF}" ]] || die "upstream.conf not found: ${UPSTREAM_CONF}"
[[ -f "${COMPOSE_BASE}" ]]  || die "base compose file not found: ${COMPOSE_BASE}"

log_ok "Pre-flight OK (env=${ENV}, dry-run=${DRY_RUN})"

# ─── Step 1: Detect current (old) colour ─────────────────────────────────────
log_info "Detecting current active slot ..."
if grep -q "argus-app-blue" "${UPSTREAM_CONF}"; then
    OLD_COLOR="blue"
    NEW_COLOR="green"
    NEW_PORT=8086
elif grep -q "argus-app-green" "${UPSTREAM_CONF}"; then
    OLD_COLOR="green"
    NEW_COLOR="blue"
    NEW_PORT=8085
else
    die "Cannot determine active slot from ${UPSTREAM_CONF} — expected argus-app-blue or argus-app-green"
fi

log_info "Current slot: ${OLD_COLOR} → promoting: ${NEW_COLOR} (host port ${NEW_PORT})"

COMPOSE_NEW="${REPO_ROOT}/deploy/docker-compose.${NEW_COLOR}.yml"
COMPOSE_OLD="${REPO_ROOT}/deploy/docker-compose.${OLD_COLOR}.yml"
PROJECT_NEW="argus-${NEW_COLOR}"
PROJECT_OLD="argus-${OLD_COLOR}"

# ─── Step 2: Start new slot ──────────────────────────────────────────────────
log_info "Starting new slot (${NEW_COLOR}) ..."

if [[ -n "${IMAGE_SHA}" ]]; then
    run "ARGUS_IMAGE=${IMAGE_SHA} docker compose -p ${PROJECT_NEW} -f ${COMPOSE_BASE} -f ${COMPOSE_NEW} up -d argus"
else
    run "docker compose -p ${PROJECT_NEW} -f ${COMPOSE_BASE} -f ${COMPOSE_NEW} up -d argus"
fi

# ─── Step 3: Health gate — wait for new slot to be ready ─────────────────────
log_info "Waiting for ${NEW_COLOR} slot to become healthy (timeout ${HEALTH_TIMEOUT}s) ..."

if [[ "${DRY_RUN}" == "true" ]]; then
    log_warn "[DRY-RUN] Skipping health poll — would curl http://localhost:${NEW_PORT}/health/ready"
else
    elapsed=0
    while true; do
        if curl -sf "http://localhost:${NEW_PORT}/health/ready" >/dev/null 2>&1; then
            log_ok "${NEW_COLOR} slot is healthy"
            break
        fi
        if [[ ${elapsed} -ge ${HEALTH_TIMEOUT} ]]; then
            log_error "${NEW_COLOR} slot did not become healthy within ${HEALTH_TIMEOUT}s — rolling back"
            docker compose -p "${PROJECT_NEW}" -f "${COMPOSE_BASE}" -f "${COMPOSE_NEW}" stop argus || true
            die "Health gate failed — old slot (${OLD_COLOR}) remains live"
        fi
        log_info "  ... waiting (${elapsed}s/${HEALTH_TIMEOUT}s)"
        sleep "${HEALTH_INTERVAL}"
        elapsed=$(( elapsed + HEALTH_INTERVAL ))
    done
fi

# ─── Step 4: Smoke test against new slot ────────────────────────────────────
log_info "Running smoke test against ${NEW_COLOR} slot (localhost:${NEW_PORT}) ..."

SMOKE_SCRIPT="${SCRIPT_DIR}/smoke-test.sh"
if [[ -f "${SMOKE_SCRIPT}" ]]; then
    if [[ "${DRY_RUN}" == "true" ]]; then
        log_warn "[DRY-RUN] Skipping smoke test — would run: ${SMOKE_SCRIPT} localhost:${NEW_PORT} ${IMAGE_SHA:-}"
    else
        if ! bash "${SMOKE_SCRIPT}" "localhost:${NEW_PORT}" "${IMAGE_SHA:-}"; then
            log_error "Smoke test FAILED against ${NEW_COLOR} slot — rolling back"
            docker compose -p "${PROJECT_NEW}" -f "${COMPOSE_BASE}" -f "${COMPOSE_NEW}" stop argus || true
            die "Smoke test failed — old slot (${OLD_COLOR}) remains live"
        fi
        log_ok "Smoke test passed"
    fi
else
    log_warn "smoke-test.sh not found at ${SMOKE_SCRIPT} — skipping smoke test"
fi

# ─── Step 5: Atomic upstream rewrite ─────────────────────────────────────────
log_info "Rewriting upstream.conf to point at ${NEW_COLOR} ..."

BAK_FILE="${UPSTREAM_CONF}.bak"

if [[ "${DRY_RUN}" == "true" ]]; then
    log_warn "[DRY-RUN] Would write upstream.conf pointing to argus-app-${NEW_COLOR}"
else
    cp "${UPSTREAM_CONF}" "${BAK_FILE}"

    UPSTREAM_NEW_FILE="${UPSTREAM_CONF}.new"
    cat >"${UPSTREAM_NEW_FILE}" <<UPCONF
upstream api {
    server argus-app-${NEW_COLOR}:8080;
    keepalive 64;
    keepalive_requests 1000;
    keepalive_timeout 60s;
}

upstream websocket {
    server argus-app-${NEW_COLOR}:8081;
    keepalive 32;
}
UPCONF

    mv -f "${UPSTREAM_NEW_FILE}" "${UPSTREAM_CONF}"
    log_ok "upstream.conf rewritten (backup at ${BAK_FILE})"
fi

# ─── Step 6: Reload Nginx ─────────────────────────────────────────────────────
log_info "Reloading Nginx ..."

if [[ "${DRY_RUN}" == "true" ]]; then
    log_warn "[DRY-RUN] Would exec: docker compose exec -T nginx nginx -s reload"
else
    if ! docker compose -f "${COMPOSE_BASE}" exec -T nginx nginx -s reload; then
        log_error "Nginx reload failed — reverting upstream.conf"
        cp "${BAK_FILE}" "${UPSTREAM_CONF}"
        die "Nginx reload failed — upstream.conf restored to ${OLD_COLOR}"
    fi
    log_ok "Nginx reloaded — traffic now flows to ${NEW_COLOR}"
fi

# ─── Step 7: Drain ───────────────────────────────────────────────────────────
log_info "Draining old slot (${OLD_COLOR}) for ${DRAIN_SECONDS}s ..."
run "sleep ${DRAIN_SECONDS}"

# ─── Step 8: Stop old slot ───────────────────────────────────────────────────
log_info "Stopping old slot (${OLD_COLOR}) ..."
run "docker compose -p ${PROJECT_OLD} -f ${COMPOSE_BASE} -f ${COMPOSE_OLD} stop argus"
log_ok "Old slot (${OLD_COLOR}) stopped"

# ─── Step 9: Emit audit event ────────────────────────────────────────────────
log_info "Emitting audit event ..."

ARGUS_API_URL="${ARGUS_API_URL:-http://localhost:8084}"
ENTITY_ID="bluegreen-flip-$(date -u +%Y%m%dT%H%M%SZ)"
AFTER_DATA="{\"env\":\"${ENV}\",\"old_color\":\"${OLD_COLOR}\",\"new_color\":\"${NEW_COLOR}\",\"image\":\"${IMAGE_SHA:-unknown}\",\"actor\":\"${USER:-ci}\"}"
AUDIT_PAYLOAD="{\"action\":\"bluegreen_flip\",\"entity_type\":\"deployment\",\"entity_id\":\"${ENTITY_ID}\",\"after_data\":${AFTER_DATA}}"

if [[ "${DRY_RUN}" == "true" ]]; then
    log_warn "[DRY-RUN] Would POST audit event: ${AUDIT_PAYLOAD}"
else
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
fi

# ─── Done ─────────────────────────────────────────────────────────────────────
echo ""
log_ok "Blue-green flip complete: ${OLD_COLOR} → ${NEW_COLOR} (env: ${ENV})"
echo ""
