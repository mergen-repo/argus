# NATS Consumer Lag Alert

## When to use

- Alert fires: `argus_nats_consumer_lag{stream="...", consumer="..."} > 10000`
- `argus_nats_consumer_lag_alerts_total` counter is incrementing rapidly
- Background jobs (CDR processing, audit fan-out, notification delivery) are visibly delayed
- NATS JetStream monitor at `:8222` shows consumer `num_pending` growing indefinitely

## Prerequisites

- `nats` CLI installed: `nats --version` (install via `brew install nats-io/nats-tools/nats` or `go install github.com/nats-io/natscli/nats@latest`)
- NATS server accessible at `localhost:4222`
- `docker`, `docker compose` on operator machine
- Access to Grafana dashboard: `<grafana>/d/argus-overview`

## Estimated Duration

| Step | Expected time |
|------|---------------|
| Step 1 — Identify lagging consumer | 2–5 min |
| Step 2 — Diagnose root cause | 5–10 min |
| Step 3a — Scale consumer (if CPU-bound) | 2–5 min |
| Step 3b — Purge and reset (if safe) | 5–10 min |
| Step 4 — Monitor recovery | 10–20 min |
| **Total** | **~25–50 min** |

---

## Procedure

### 1. Identify the lagging consumer

```bash
# Query Prometheus for current lag per consumer
curl -s 'http://localhost:9090/api/v1/query?query=argus_nats_consumer_lag%20%3E%201000' | \
  jq '.data.result[] | {stream: .metric.stream, consumer: .metric.consumer, lag: .value[1]}'
# Expected output: JSON objects identifying the stream+consumer pair and lag count

# Cross-check via NATS CLI
nats --server nats://localhost:4222 consumer report --all
# Expected: table showing stream name, consumer name, num_pending, num_redelivered, last_acked

# Check the NATS HTTP monitor endpoint
curl -s http://localhost:8222/jsz?consumers=true | jq '.account_details[].stream_detail[] | {name: .stream_name, consumers: [.consumer_detail[] | {name: .name, num_pending: .num_pending}]}'
# Expected: per-consumer pending counts
```

### 2. Diagnose root cause

```bash
# Check argus application logs for consumer-related errors
docker compose -f deploy/docker-compose.yml logs --tail=200 argus | grep -E 'nats|consumer|jetstream' | grep -iE 'error|fail|timeout|lag'
# Expected: either no errors (consumer is slow but healthy) or specific error messages

# Check if the argus container itself is CPU/memory starved
docker stats --no-stream argus-app
# Expected: CPU% and MEM% — if CPU > 90%, the process is struggling

# Check argus health
curl -sf http://localhost:8084/health/ready | jq
# Expected: {"status":"ok", ...} — if not, argus restart may be needed

# Check NATS server health
curl -sf http://localhost:8222/healthz | jq
# Expected: {"status":"ok"}

# Check if specific messages are being nack'd or causing redelivery loops
nats --server nats://localhost:4222 consumer info <STREAM> <CONSUMER>
# Expected: look at num_redelivered — if >> num_ack_pending, messages are poison-pilling

# Example streams in Argus:
#   ARGUS.CDR         cdr-processor
#   ARGUS.AUDIT       audit-fanout
#   ARGUS.NOTIFY      notification-dispatcher
#   ARGUS.OPERATOR    operator-sync
```

### 3a. Scale consumer (if CPU-bound or throughput-limited)

If the consumer is healthy but simply cannot keep up with message rate, restart argus with increased worker concurrency or temporarily scale the background job workers.

```bash
# Restart argus to pick up any configuration changes
docker compose -f deploy/docker-compose.yml restart argus
# Expected: container restarts, health check passes within 30 seconds

# After restart, watch lag trend
watch -n 5 'curl -s "http://localhost:9090/api/v1/query?query=argus_nats_consumer_lag" | jq ".data.result[] | {consumer: .metric.consumer, lag: .value[1]}"'
# Expected: lag value decreasing over time

# If lag is not decreasing after 5 minutes, check job concurrency in config
argusctl config show | grep -i worker
# Adjust concurrency via environment variable if needed (requires restart)
```

### 3b. Purge consumer if messages are stale or poison-pilling

Use this path only if: (a) the lagging messages are known-bad (test data, already-processed events from a previous incident), AND (b) reprocessing them would cause duplicate side effects. **This is destructive — get approval before purging production consumers.**

```bash
# First, inspect a sample of pending messages to confirm they are safe to discard
nats --server nats://localhost:4222 consumer next <STREAM> <CONSUMER> --count 5
# Expected: review message content and headers

# If messages are confirmed safe to discard, purge the stream
# WARNING: purge removes ALL pending messages — only use for known-stale backlog
nats --server nats://localhost:4222 stream purge <STREAM> --force
# Expected: "Purged stream <STREAM>" confirmation

# Reset the consumer after purge so it starts from the current position
nats --server nats://localhost:4222 consumer delete <STREAM> <CONSUMER>
# Consumer will be recreated by argus on next startup
docker compose -f deploy/docker-compose.yml restart argus
```

### 4. Monitor recovery

```bash
# Watch the lag metric converge to 0
watch -n 10 'curl -s "http://localhost:9090/api/v1/query?query=argus_nats_consumer_lag" | jq ".data.result[] | {consumer: .metric.consumer, lag: .value[1]}"'
# Expected: all values trending toward 0 over 10–20 minutes

# Check alert counter stopped incrementing
curl -s 'http://localhost:9090/api/v1/query?query=rate(argus_nats_consumer_lag_alerts_total[5m])' | jq '.data.result[] | {consumer: .metric.consumer, rate: .value[1]}'
# Expected: rate = 0 when lag is resolved

# Confirm messages are being processed
curl -s 'http://localhost:9090/api/v1/query?query=rate(argus_nats_consumed_total[5m])' | jq '.data.result[] | {subject: .metric.subject, rate: .value[1]}'
# Expected: positive consumption rate on affected subjects
```

---

## Verification

- `argus_nats_consumer_lag` < 1000 for all consumers
- `rate(argus_nats_consumer_lag_alerts_total[5m])` == 0
- `curl http://localhost:8084/health/ready` returns 200
- NATS monitor at `http://localhost:8222/jsz?consumers=true` shows `num_pending` < 100 for all consumers
- Background job processing resumes (CDR timestamps catching up, notification queue draining)

---

## Post-incident

- Audit log entry: `argusctl audit log --action=nats_lag_remediation --resource=nats --note="consumer=<name>, peak_lag=<N>, action=<scale|purge>"`
- If purge was used, create a data loss incident report noting which messages were discarded and the time window
- Review consumer throughput capacity and NATS server resource limits
- Consider adding a dead-letter stream for poison messages to prevent future lag from redelivery loops
- **Comms template (incident channel):**
  > `[RESOLVED] NATS consumer lag alert resolved. Consumer <name> on stream <stream> reached lag of <N>. Action taken: <scaled/purged>. Recovery time: ~<duration>. Messages lost: <0 | N>. No user-visible impact on reads.`
- **Stakeholder email:**
  > Subject: [Argus] NATS message processing delay resolved
  > Body: Background message processing experienced a lag of <N> messages at <time>. Action taken: <describe>. Real-time reads and API responses were unaffected. Background operations (CDR processing, notifications) were delayed by approximately <duration>. All messages have been processed as of <time>.
