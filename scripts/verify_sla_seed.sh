#!/usr/bin/env bash
# verify_sla_seed.sh — Smoke-verifies that SLA seed data is present in the DB.
# Usage: ./scripts/verify_sla_seed.sh
# Exit 0 = PASS, non-zero = FAIL.

set -euo pipefail

PSQL="docker exec argus-postgres psql -U argus -d argus -t -c"
PASS=0
FAIL=0

check() {
  local label="$1"
  local query="$2"
  local expected_min="$3"
  local result
  result=$($PSQL "$query" 2>&1 | tr -d ' \n')
  if ! [[ "$result" =~ ^[0-9]+$ ]]; then
    echo "FAIL [$label]: query error — $result"
    FAIL=$((FAIL + 1))
    return
  fi
  if [ "$result" -ge "$expected_min" ]; then
    echo "PASS [$label]: $result >= $expected_min"
    PASS=$((PASS + 1))
  else
    echo "FAIL [$label]: $result < $expected_min (expected >= $expected_min)"
    FAIL=$((FAIL + 1))
  fi
}

echo "=== SLA Seed Verification ==="

# 1. sla_reports total >= 100 (9 operators x 12 months = 108)
check "sla_reports total" \
  "SELECT COUNT(*) FROM sla_reports;" \
  100

# 2. Distinct months covered >= 12
check "sla_reports distinct months" \
  "SELECT COUNT(DISTINCT date_trunc('month', window_start)) FROM sla_reports;" \
  12

# 3. operator_health_logs in last 60 days >= 100000
check "operator_health_logs 60d" \
  "SELECT COUNT(*) FROM operator_health_logs WHERE checked_at > NOW() - INTERVAL '60 days';" \
  100000

# 4. At least one continuous down run >= 5 minutes in last 30 days
# Use island detection (gap-and-island) without nested window functions
echo -n "CHECK [5min continuous down run]: "
DOWN_RUNS=$($PSQL "
WITH numbered AS (
  SELECT
    operator_id,
    checked_at,
    status,
    ROW_NUMBER() OVER (PARTITION BY operator_id ORDER BY checked_at) AS rn
  FROM operator_health_logs
  WHERE checked_at > NOW() - INTERVAL '30 days'
),
grouped AS (
  SELECT
    operator_id,
    checked_at,
    status,
    rn - ROW_NUMBER() OVER (PARTITION BY operator_id, status ORDER BY checked_at) AS grp
  FROM numbered
),
down_runs AS (
  SELECT
    operator_id,
    grp,
    MIN(checked_at) AS run_start,
    MAX(checked_at) AS run_end
  FROM grouped
  WHERE status = 'down'
  GROUP BY operator_id, grp
)
SELECT COUNT(*) FROM down_runs WHERE (run_end - run_start) >= INTERVAL '5 minutes';
" 2>&1 | tr -d ' \n')

if ! [[ "$DOWN_RUNS" =~ ^[0-9]+$ ]]; then
  echo "FAIL: query error — $DOWN_RUNS"
  FAIL=$((FAIL + 1))
elif [ "$DOWN_RUNS" -ge 1 ]; then
  echo "PASS: $DOWN_RUNS run(s) of >= 5 min downtime found"
  PASS=$((PASS + 1))
else
  echo "FAIL: 0 continuous down runs >= 5 minutes found in last 30 days"
  FAIL=$((FAIL + 1))
fi

echo ""
echo "=== Results: $PASS PASS, $FAIL FAIL ==="

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
