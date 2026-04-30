# RADIUS Session Loss / AAA Failures

## When to use

- `argus_active_sessions` gauge drops sharply (> 10% decrease in < 5 minutes)
- `histogram_quantile(0.99, rate(argus_aaa_auth_latency_seconds_bucket[5m]))` > 0.5 seconds
- `argus_aaa_auth_requests_total{result="failure"}` rate spikes
- RADIUS/Diameter/5G SBA authentication failures increase
- SIMs report no connectivity (operator reports from field)
- `argus_circuit_breaker_state{state="open"}` is 1 for one or more operators

## Prerequisites

- `docker`, `docker compose` on operator machine
- Access to Prometheus: `http://localhost:9090`
- Access to Grafana: `<grafana>/d/argus-overview`
- `argusctl` CLI installed and authenticated
- RADIUS log access (inside the argus container)
- Ability to roll back recent policy deployments

## Estimated Duration

| Step | Expected time |
|------|---------------|
| Step 1 — Quantify session loss | 3–5 min |
| Step 2 — Check operator adapter health | 3–5 min |
| Step 3 — Scan RADIUS/AAA logs | 5–10 min |
| Step 4 — Check circuit breaker state | 2–3 min |
| Step 5 — Roll back recent policy changes | 5–10 min |
| Step 6 — Force session re-auth | 5 min |
| Step 7 — Verify recovery | 5–10 min |
| **Total** | **~30–45 min** |

---

## Procedure

### 1. Quantify the session loss

```bash
# Check current active session count vs historical baseline
curl -s 'http://localhost:9090/api/v1/query?query=argus_active_sessions' | \
  jq '.data.result[] | {tenant: .metric.tenant_id, operator: .metric.operator_id, sessions: .value[1]}'
# Expected: compare to baseline; a sharp drop is the trigger

# Query change over 10 minutes to confirm sudden drop
curl -s 'http://localhost:9090/api/v1/query?query=argus_active_sessions+-+argus_active_sessions+offset+10m' | \
  jq '.data.result[] | {tenant: .metric.tenant_id, operator: .metric.operator_id, delta: .value[1]}'
# Expected: large negative values confirm session loss

# Check AAA auth failure rate
curl -s 'http://localhost:9090/api/v1/query?query=rate(argus_aaa_auth_requests_total%7Bresult%3D"failure"%7D%5B5m%5D)' | \
  jq '.data.result[] | {protocol: .metric.protocol, operator: .metric.operator_id, fail_rate: .value[1]}'
# Expected: elevated failure rate indicates the auth path is broken

# Check AAA auth latency p99
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.99%2C+rate(argus_aaa_auth_latency_seconds_bucket%5B5m%5D))%20by%20(protocol%2C+operator_id)' | \
  jq '.data.result[] | {protocol: .metric.protocol, operator: .metric.operator_id, p99_ms: (.value[1] | tonumber * 1000 | round)}'
# Expected: high p99 (> 500ms) indicates upstream timeout rather than immediate rejection
```

### 2. Check operator adapter health

```bash
# Check operator health gauges
curl -s 'http://localhost:9090/api/v1/query?query=argus_operator_health' | \
  jq '.data.result[] | {operator: .metric.operator_id, health: .value[1]}'
# health values: 2=healthy, 1=degraded, 0=down

# Identify which operators are down or degraded
curl -s 'http://localhost:9090/api/v1/query?query=argus_operator_health+%3C+2' | \
  jq '.data.result[] | {operator: .metric.operator_id, health: .value[1]}'
# Expected: empty if all healthy; rows indicate problematic operators

# Test reachability to operator endpoint (replace with actual operator host)
docker compose -f deploy/docker-compose.yml exec argus \
  sh -c 'nc -zv <operator_radius_host> 1812 && echo OK || echo FAIL'
# Expected: OK if RADIUS port reachable

# For Diameter (port 3868):
docker compose -f deploy/docker-compose.yml exec argus \
  sh -c 'nc -zv <operator_diameter_host> 3868 && echo OK || echo FAIL'

# If operator is down, follow operator-down.md for failover procedure
```

### 3. Scan RADIUS/AAA logs

```bash
# Check argus logs for AAA-related errors in the last 100 lines
docker compose -f deploy/docker-compose.yml logs --tail=300 argus | \
  grep -iE 'radius|diameter|aaa|auth|session' | \
  grep -iE 'error|fail|reject|timeout|deny' | \
  tail -30
# Expected: error messages with reason codes

# Look for specific RADIUS reject codes
docker compose -f deploy/docker-compose.yml logs --tail=1000 argus | \
  grep -E 'Access-Reject|Auth-Failure|Session-Terminate' | \
  tail -20
# Expected: reject reason (wrong PSK, unknown IMSI, policy violation, etc.)

# Check for policy engine errors (policy rejecting auth)
docker compose -f deploy/docker-compose.yml logs --tail=300 argus | \
  grep -iE 'policy|rule|eval' | \
  grep -iE 'error|reject|deny' | \
  tail -20
# Expected: policy DSL evaluation errors indicate a bad policy was recently deployed

# Check session termination events in the database
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -c "
    SELECT
      tenant_id,
      operator_id,
      terminate_cause,
      count(*) AS session_count
    FROM cdr_records
    WHERE started_at > now() - INTERVAL '30 minutes'
      AND terminated_at IS NOT NULL
    GROUP BY tenant_id, operator_id, terminate_cause
    ORDER BY session_count DESC;
  "
# Expected: shows termination causes; 'Lost-Carrier' or 'NAS-Error' in bulk = upstream issue
```

### 4. Check circuit breaker state

```bash
# Check all circuit breakers
curl -s 'http://localhost:9090/api/v1/query?query=argus_circuit_breaker_state%7Bstate%3D"open"%7D' | \
  jq '.data.result[] | {operator: .metric.operator_id, state: .metric.state}'
# Expected: empty = all closed (healthy); rows = circuit breaker open for that operator

# If circuit breaker is open, check when it opened
curl -s 'http://localhost:9090/api/v1/query_range?query=argus_circuit_breaker_state%7Bstate%3D"open"%7D&start='"$(date -u -v-1H +%s)"'&end='"$(date -u +%s)"'&step=60' | \
  jq '.data.result[] | {operator: .metric.operator_id, transitions: [.values[] | select(.[1] == "1") | .[0]]}'
# Expected: shows timestamps when the circuit opened

# The argus circuit breaker will auto-probe after its configured timeout
# To manually trigger a half-open probe attempt, restart argus
docker compose -f deploy/docker-compose.yml restart argus
# Expected: argus restarts and circuit breakers reset to closed, probing begins
```

### 5. Roll back recent policy changes

If logs show policy evaluation errors or reject decisions that look like policy misconfiguration:

```bash
# List recent policy deployments
argusctl policy list --limit=10 --sort=created_at:desc
# Expected: table with policy name, version, tenant, created_at

# Check which policy was active at the time of incident
argusctl policy history --since=30m
# Expected: shows policy changes in last 30 minutes

# Identify the policy version before the change
PREV_VERSION="<policy_id_from_history>"

# Roll back to the previous version
argusctl policy rollback --id="${PREV_VERSION}"
# Expected: Policy rolled back to version <N>

# Verify policy is active
argusctl policy show "${PREV_VERSION}"
# Expected: status=active

# Confirm auth is recovering
curl -s 'http://localhost:9090/api/v1/query?query=rate(argus_aaa_auth_requests_total%7Bresult%3D"success"%7D%5B2m%5D)' | \
  jq '.data.result[] | {protocol: .metric.protocol, operator: .metric.operator_id, rate: .value[1]}'
# Expected: success rate recovering toward baseline
```

### 6. Force session re-authentication

If sessions were lost due to a transient error (not policy misconfiguration), instruct the NAS/operator to trigger CoA (Change of Authorization) or re-auth to re-establish sessions. This is typically done via the operator's provisioning portal. For operator-controlled session recovery, open a ticket with the operator team.

If argus-controlled SIM policies can force re-auth:

```bash
# Re-trigger auth for affected SIMs (if supported by operator adapter)
argusctl session reauth --tenant=<tenant_id> --operator=<operator_id>
# Expected: queues reauth requests for affected sessions
```

### 7. Verify recovery

```bash
# Watch active session count recover
watch -n 15 'curl -s "http://localhost:9090/api/v1/query?query=argus_active_sessions" | jq "[.data.result[] | {tenant: .metric.tenant_id, operator: .metric.operator_id, sessions: .value[1]}]"'
# Expected: session counts returning toward baseline over 5–15 minutes

# Confirm auth success rate is restored
curl -s 'http://localhost:9090/api/v1/query?query=rate(argus_aaa_auth_requests_total%7Bresult%3D"success"%7D%5B5m%5D)+%2F+rate(argus_aaa_auth_requests_total%5B5m%5D)' | \
  jq '.data.result[] | {protocol: .metric.protocol, operator: .metric.operator_id, success_rate: .value[1]}'
# Expected: success rate > 0.95 (95%) for all operators

# Health check
curl -sf http://localhost:8084/health/ready | jq
# Expected: {"status":"ok", ...}

# Confirm circuit breakers are closed
curl -s 'http://localhost:9090/api/v1/query?query=argus_circuit_breaker_state%7Bstate%3D"open"%7D' | jq '.data.result'
# Expected: [] (empty array)
```

---

## Verification

- `argus_active_sessions` returning to pre-incident baseline
- `rate(argus_aaa_auth_requests_total{result="failure"}[5m])` < 0.01 per second
- `histogram_quantile(0.99, rate(argus_aaa_auth_latency_seconds_bucket[5m]))` < 0.1 seconds
- `argus_circuit_breaker_state{state="open"}` returns no results
- `curl http://localhost:8084/health/ready` returns 200

---

## Post-incident

- Audit log entry: `argusctl audit log --action=session_loss_remediation --resource=aaa --note="operator=<id>, peak_loss=<N>sessions, root_cause=<policy|operator|network>, action=<taken>"`
- If policy rollback was performed: open a bug report on the policy that caused the failure; add policy validation test
- Coordinate with operator team if upstream was the root cause (provide log evidence of their outage window)
- **Comms template (incident channel):**
  > `[RESOLVED] RADIUS session loss resolved. Session count dropped by <N> at <time>. Root cause: <policy misconfiguration | operator adapter failure | network issue>. Recovery action: <rollback | restart | operator failover>. Sessions restored by <time>. Customer impact: SIM connectivity interruption for ~<duration> minutes.`
- **Stakeholder email:**
  > Subject: [Argus] SIM session connectivity incident resolved
  > Body: An authentication failure caused SIM session loss at <time>. Approximately <N> sessions were affected. Root cause: <describe>. Resolution: <action>. Full service restored at <time>. We are implementing <preventive measure> to prevent recurrence.
