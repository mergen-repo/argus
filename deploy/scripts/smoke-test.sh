#!/bin/bash
# smoke-test.sh — Post-deploy verification
# Usage: smoke-test.sh <host> [<short-sha>]
set -euo pipefail

HOST="${1:-localhost:8084}"
EXPECTED_SHA="${2:-}"

echo "[1/4] /health/ready ..."
body=$(curl -sf "http://${HOST}/health/ready") || { echo "FAIL: /health/ready"; exit 1; }
state=$(echo "$body" | jq -r '.data.state' 2>/dev/null || echo "")
[ "$state" = "healthy" ] || [ "$state" = "degraded" ] || { echo "FAIL: state=$state"; exit 1; }

echo "[2/4] /api/v1/status ..."
body=$(curl -sf "http://${HOST}/api/v1/status") || { echo "FAIL: /api/v1/status"; exit 1; }
if [ -n "$EXPECTED_SHA" ]; then
    version=$(echo "$body" | jq -r '.data.git_sha' 2>/dev/null || echo "")
    [ "$version" = "$EXPECTED_SHA" ] || { echo "FAIL: git_sha mismatch got=$version want=$EXPECTED_SHA"; exit 1; }
fi

echo "[3/4] /metrics ..."
curl -sf "http://${HOST}/metrics" > /dev/null || { echo "FAIL: /metrics"; exit 1; }

echo "[4/4] /api/health (legacy) ..."
curl -sf "http://${HOST}/api/health" > /dev/null || { echo "FAIL: /api/health"; exit 1; }

echo "SMOKE TEST PASS"
