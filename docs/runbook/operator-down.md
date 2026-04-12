# Upstream Operator Adapter Failure

## When to use

- Alert fires: `argus_circuit_breaker_state{operator_id="<X>", state="open"} == 1`
- `argus_operator_health{operator_id="<X>"}` == 0 (down) or 1 (degraded)
- RADIUS/Diameter/5G auth requests failing for specific operator with connection errors
- Operator team contacts you reporting an outage on their side
- `argus_aaa_auth_latency_seconds` p99 spikes for a specific `operator_id`

## Prerequisites

- Access to Prometheus: `http://localhost:9090`
- `docker`, `docker compose` on operator machine
- `argusctl` CLI installed and authenticated with admin rights
- Operator status page URL (per operator — see internal runbook contacts doc)
- Access to argus application logs
- Failover operator adapter configuration available (if applicable)

## Estimated Duration

| Step | Expected time |
|------|---------------|
| Step 1 — Confirm operator is down | 3–5 min |
| Step 2 — Check operator status page | 2–3 min |
| Step 3 — Enable failover adapter | 5–10 min |
| Step 4 — Notify operator team | 2–5 min |
| Step 5 — Monitor failover | 10–15 min |
| Step 6 — Restore primary operator | 5–10 min |
| **Total** | **~30–50 min** |

---

## Procedure

### 1. Confirm the operator is down

```bash
# Check circuit breaker state for all operators
curl -s 'http://localhost:9090/api/v1/query?query=argus_circuit_breaker_state' | \
  jq '.data.result[] | {operator: .metric.operator_id, state: .metric.state, value: .value[1]}'
# Expected: state="open" with value=1 identifies the tripped operator

# Get the specific operator_id from the alert
OPERATOR_ID="<operator_id_from_alert>"

# Confirm circuit breaker is open for that operator
curl -s "http://localhost:9090/api/v1/query?query=argus_circuit_breaker_state%7Boperator_id%3D%22${OPERATOR_ID}%22%2Cstate%3D%22open%22%7D" | \
  jq '.data.result[] | {operator: .metric.operator_id, open_since: .value[0], value: .value[1]}'
# Expected: value=1 confirms open circuit

# Check operator health gauge
curl -s "http://localhost:9090/api/v1/query?query=argus_operator_health%7Boperator_id%3D%22${OPERATOR_ID}%22%7D" | \
  jq '.data.result[] | {operator: .metric.operator_id, health: .value[1]}'
# health: 0=down, 1=degraded, 2=healthy

# Check AAA failure rate for this operator
curl -s "http://localhost:9090/api/v1/query?query=rate(argus_aaa_auth_requests_total%7Bresult%3D%22failure%22%2Coperator_id%3D%22${OPERATOR_ID}%22%7D%5B5m%5D)" | \
  jq '.data.result[] | {protocol: .metric.protocol, fail_rps: .value[1]}'
# Expected: elevated failure rate confirms auth path to this operator is broken

# Check active sessions affected
curl -s "http://localhost:9090/api/v1/query?query=argus_active_sessions%7Boperator_id%3D%22${OPERATOR_ID}%22%7D" | \
  jq '.data.result[] | {tenant: .metric.tenant_id, sessions: .value[1]}'
# Expected: shows how many sessions are at risk
```

### 2. Check the operator status page

Before taking any action, confirm whether this is an operator-side outage or an Argus configuration/network issue.

```bash
# Test direct connectivity to the operator's RADIUS endpoint
docker compose -f deploy/docker-compose.yml exec argus \
  sh -c 'nc -zv <operator_radius_host> 1812; echo "exit: $?"'
# Expected: "succeeded!" + exit 0 = reachable; "Connection refused" or timeout = operator down

# Test Diameter endpoint (port 3868)
docker compose -f deploy/docker-compose.yml exec argus \
  sh -c 'nc -zv <operator_diameter_host> 3868; echo "exit: $?"'

# Check argus logs for the specific error class
docker compose -f deploy/docker-compose.yml logs --tail=200 argus | \
  grep "${OPERATOR_ID}" | grep -iE 'error|fail|connect|timeout' | tail -20
# Expected: "connection refused" = operator endpoint down; "timeout" = network issue

# Visit the operator's public status page (see ops/contacts.md for URLs)
# Example: https://status.operator-x.com
# If they show active incident → it's their problem → proceed with failover
# If no incident reported → may be a network or config issue → check internal routing
```

### 3. Enable failover adapter

If a failover operator adapter is configured in Argus, activate it to route traffic away from the failed operator.

```bash
# List available operator adapters and their failover configuration
argusctl operator list
# Expected: table with operator_id, name, status, failover_operator_id

# Check if a failover operator is configured for the affected operator
argusctl operator show ${OPERATOR_ID}
# Expected: shows failover_operator_id field (if configured)

FAILOVER_ID="<failover_operator_id>"

# Enable failover: route traffic to the failover adapter
argusctl operator failover enable --from=${OPERATOR_ID} --to=${FAILOVER_ID}
# Expected: Failover enabled: traffic for operator <id> routed to <failover_id>

# Verify the failover is active
argusctl operator failover status --operator=${OPERATOR_ID}
# Expected: status=active, failover_to=<failover_id>

# Monitor that auth requests are now using the failover operator
curl -s "http://localhost:9090/api/v1/query?query=rate(argus_aaa_auth_requests_total%7Bresult%3D%22success%22%2Coperator_id%3D%22${FAILOVER_ID}%22%7D%5B2m%5D)" | \
  jq '.data.result[] | {protocol: .metric.protocol, success_rps: .value[1]}'
# Expected: increasing success rate on the failover operator
```

If no failover operator is configured, skip failover and proceed with operator team coordination.

### 4. Notify the operator team

```bash
# Open a priority ticket / contact operator NOC
# Required information to include:
echo "Operator ID: ${OPERATOR_ID}"
echo "Error type: $(docker compose -f deploy/docker-compose.yml logs --tail=50 argus | grep ${OPERATOR_ID} | grep -iE 'error|fail' | head -1)"
echo "First failure: $(docker compose -f deploy/docker-compose.yml logs argus | grep ${OPERATOR_ID} | grep -iE 'error|fail' | head -1 | awk '{print $1, $2}')"
echo "Auth failures per second: <from step 1>"
echo "Sessions affected: <from step 1>"

# Create an audit log entry for the incident
argusctl audit log \
  --action=operator_failover \
  --resource=operator \
  --resource-id=${OPERATOR_ID} \
  --note="Circuit breaker open. Failover activated to ${FAILOVER_ID}. Operator team notified."
# Expected: Audit log entry created
```

### 5. Monitor failover

```bash
# Watch key metrics while failover is active
watch -n 15 'echo "=== Circuit Breaker ===" && \
  curl -s "http://localhost:9090/api/v1/query?query=argus_circuit_breaker_state%7Bstate%3D%22open%22%7D" | jq ".data.result[] | {op: .metric.operator_id}" && \
  echo "=== Auth Success Rate ===" && \
  curl -s "http://localhost:9090/api/v1/query?query=rate(argus_aaa_auth_requests_total%7Bresult%3D%22success%22%7D%5B2m%5D)" | jq "[.data.result[] | {op: .metric.operator_id, rps: .value[1]}]"'
# Expected: failed operator shows open circuit; failover operator shows increasing success rate

# Check active sessions are recovering
curl -s 'http://localhost:9090/api/v1/query?query=argus_active_sessions' | \
  jq '[.data.result[] | {tenant: .metric.tenant_id, operator: .metric.operator_id, sessions: .value[1]}]'
# Expected: sessions migrating to failover operator

# If no failover is configured: accept that sessions for this operator are interrupted
# and focus on root cause resolution with the operator team
```

### 6. Restore primary operator when recovered

Once the operator team confirms the issue is resolved:

```bash
# Test connectivity again before restoring
docker compose -f deploy/docker-compose.yml exec argus \
  sh -c 'nc -zv <operator_radius_host> 1812 && echo REACHABLE || echo UNREACHABLE'
# Expected: REACHABLE

# Disable failover to restore traffic to primary operator
argusctl operator failover disable --operator=${OPERATOR_ID}
# Expected: Failover disabled: traffic restored to operator <id>

# Restart argus to reset the circuit breaker
docker compose -f deploy/docker-compose.yml restart argus
# Expected: container restarts successfully; circuit breakers reset to closed

# Monitor circuit breaker transitions to closed
curl -s "http://localhost:9090/api/v1/query?query=argus_circuit_breaker_state%7Boperator_id%3D%22${OPERATOR_ID}%22%2Cstate%3D%22closed%22%7D" | \
  jq '.data.result[] | {operator: .metric.operator_id, closed: .value[1]}'
# Expected: value=1 (closed is 1 = active, open is 0 = inactive)

# Confirm auth is flowing again
curl -s "http://localhost:9090/api/v1/query?query=rate(argus_aaa_auth_requests_total%7Bresult%3D%22success%22%2Coperator_id%3D%22${OPERATOR_ID}%22%7D%5B2m%5D)" | \
  jq '.data.result[] | {protocol: .metric.protocol, rps: .value[1]}'
# Expected: positive success rate

# Health check
curl -sf http://localhost:8084/health/ready | jq
# Expected: {"status":"ok", ...}
```

---

## Verification

- `argus_circuit_breaker_state{operator_id="<X>", state="open"}` == 0 (circuit closed)
- `argus_operator_health{operator_id="<X>"}` == 2 (healthy)
- `rate(argus_aaa_auth_requests_total{result="success", operator_id="<X>"}[5m])` > 0
- `curl http://localhost:8084/health/ready` returns 200
- `argus_active_sessions` returning toward baseline

---

## Post-incident

- Audit log entry: `argusctl audit log --action=operator_restored --resource=operator --resource-id=<id> --note="Outage duration: <N>min. Failover was <active|not active>. Sessions impacted: <N>."`
- Create RCA document with: outage timeline, operator response time, sessions impacted, SLA breach assessment
- Review whether failover configuration exists for all production operators; create ticket if any operator lacks a failover adapter
- Consider reducing circuit breaker trip threshold if operator has history of instability
- **Comms template (incident channel):**
  > `[RESOLVED] Operator <id> outage resolved. Outage duration: <N> min. Failover <was|was not> activated. Sessions restored. Root cause: <operator-side issue | network>. Operator confirmed resolution at <time>.`
- **Stakeholder email:**
  > Subject: [Argus] Operator <name> connectivity outage resolved
  > Body: Operator <name> (ID: <id>) became unreachable at <time>. <Failover to <backup> was activated | No failover was available>. The operator resolved their incident at <time>. Full connectivity restored at <time>. SIM sessions affected: approximately <N>. Action items: <list>.
