#!/usr/bin/env bash
set -euo pipefail

NATS_HEALTHZ_URL="http://localhost:8222/healthz"

if response=$(curl -sf --max-time 5 "$NATS_HEALTHZ_URL" 2>/dev/null); then
    echo "NATS healthy: $response"
    exit 0
else
    echo "NATS unhealthy: no response from $NATS_HEALTHZ_URL"
    exit 1
fi
