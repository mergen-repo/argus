# API Latency SLO Breach

## When to use

- Alert fires: `histogram_quantile(0.99, rate(argus_http_request_duration_seconds_bucket[5m])) > 0.5`
- Users report slow API responses or timeouts
- Grafana panel "p99 Latency" on the Argus Overview dashboard shows sustained spike
- SLO burn rate alert triggers (> 5× the error budget rate)

## Prerequisites

- Access to Grafana: `<grafana>/d/argus-overview?panel=12` (p99 latency panel)
- Access to Prometheus: `http://localhost:9090`
- `docker`, `docker compose` on operator machine
- pprof access: `http://localhost:8080/debug/pprof/` (enabled only briefly during investigation)
- `go tool pprof` installed (part of standard Go toolchain)

## Estimated Duration

| Step | Expected time |
|------|---------------|
| Step 1 — Confirm and scope the spike | 3–5 min |
| Step 2 — Identify hot endpoints | 3–5 min |
| Step 3 — Check downstream dependencies | 5–10 min |
| Step 4a — DB slow query investigation | 5–15 min |
| Step 4b — Operator adapter investigation | 5–10 min |
| Step 5 — Enable pprof and capture profile | 5 min |
| Step 6 — Remediate and verify | 5–15 min |
| **Total** | **~30–60 min** |

---

## Procedure

### 1. Confirm and scope the spike

```bash
# Query current p99 latency across all routes
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.99%2C+rate(argus_http_request_duration_seconds_bucket%5B5m%5D))' | \
  jq '.data.result[] | {route: .metric.route, method: .metric.method, p99: .value[1]}'
# Expected: shows p99 seconds per route — look for outliers above 0.5

# Check if the spike is tenant-specific
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.99%2C+rate(argus_http_request_duration_seconds_bucket%5B5m%5D))%20by%20(tenant_id%2C+route)' | \
  jq '.data.result[] | select(.value[1] | tonumber > 0.5) | {tenant: .metric.tenant_id, route: .metric.route, p99: .value[1]}'
# Expected: narrows down to specific tenant(s) or affects all tenants

# Check overall request rate (spike may be load-related)
curl -s 'http://localhost:9090/api/v1/query?query=rate(argus_http_requests_total%5B5m%5D)' | \
  jq '[.data.result[] | {route: .metric.route, rps: .value[1]}] | sort_by(.rps | tonumber) | reverse | .[0:10]'
# Expected: top-10 routes by request rate
```

Open Grafana: `<grafana>/d/argus-overview?panel=12`
- Set time range to last 30 minutes
- Enable "split by route" to identify which endpoints are slow

### 2. Identify hot endpoints

```bash
# Find routes with the highest p99 above threshold
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.99%2C+rate(argus_http_request_duration_seconds_bucket%5B5m%5D))%20by%20(route%2C+method)' | \
  jq '[.data.result[] | {route: .metric.route, method: .metric.method, p99_ms: (.value[1] | tonumber * 1000 | round)}] | sort_by(.p99_ms) | reverse | .[0:10]'
# Expected: sorted list of slow routes with p99 in milliseconds

# Find routes with elevated error rates (may indicate timeouts counted as errors)
curl -s 'http://localhost:9090/api/v1/query?query=rate(argus_http_requests_total%7Bstatus%3D~"5.."%7D%5B5m%5D)' | \
  jq '.data.result[] | {route: .metric.route, status: .metric.status, rate: .value[1]}'
# Expected: 5xx rates per route — high 504s indicate upstream timeout

# Check argus logs for slow handler warnings
docker compose -f deploy/docker-compose.yml logs --tail=500 argus | grep -E 'slow|timeout|deadline|context canceled' | tail -30
# Expected: log lines identifying slow operations with duration
```

### 3. Check downstream dependencies

```bash
# Check database query latency
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.99%2C+rate(argus_db_query_duration_seconds_bucket%5B5m%5D))%20by%20(operation%2C+table)' | \
  jq '[.data.result[] | {op: .metric.operation, table: .metric.table, p99_ms: (.value[1] | tonumber * 1000 | round)}] | sort_by(.p99_ms) | reverse | .[0:10]'
# Expected: slow tables/operations — if DB is the bottleneck, p99 > 100ms is concerning

# Check database connection pool
curl -s 'http://localhost:9090/api/v1/query?query=argus_db_pool_connections' | \
  jq '.data.result[] | {state: .metric.state, count: .value[1]}'
# Expected: idle > 0; if idle = 0 and waiting > 0, pool is exhausted

# Check operator adapter health
curl -s 'http://localhost:9090/api/v1/query?query=argus_operator_health' | \
  jq '.data.result[] | {operator: .metric.operator_id, health: .value[1]}'
# Expected: all operators showing 2 (healthy); 1 = degraded, 0 = down

# Check circuit breaker states
curl -s 'http://localhost:9090/api/v1/query?query=argus_circuit_breaker_state%7Bstate%3D"open"%7D' | \
  jq '.data.result[] | {operator: .metric.operator_id, state: .metric.state}'
# Expected: empty result — any open circuit breaker means operator requests are failing fast

# Check Redis latency (cache misses increase DB load)
curl -s 'http://localhost:9090/api/v1/query?query=rate(argus_redis_cache_misses_total%5B5m%5D)%20%2F%20(rate(argus_redis_cache_hits_total%5B5m%5D)+%2B+rate(argus_redis_cache_misses_total%5B5m%5D))' | \
  jq '.data.result[] | {cache: .metric.cache, miss_rate: .value[1]}'
# Expected: miss rate < 0.3 (30%) — higher indicates cache invalidation or Redis issue
```

### 4a. DB slow query investigation

If `argus_db_query_duration_seconds` p99 > 100ms:

```bash
# Check currently running long queries
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -c "
    SELECT
      pid,
      now() - pg_stat_activity.query_start AS duration,
      query,
      state
    FROM pg_stat_activity
    WHERE (now() - pg_stat_activity.query_start) > INTERVAL '1 second'
      AND state != 'idle'
    ORDER BY duration DESC;
  "
# Expected: shows long-running queries; note the query text for index analysis

# Check for lock contention
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -c "
    SELECT
      blocked_locks.pid AS blocked_pid,
      blocked_activity.query AS blocked_query,
      blocking_locks.pid AS blocking_pid,
      blocking_activity.query AS blocking_query
    FROM pg_catalog.pg_locks blocked_locks
    JOIN pg_catalog.pg_stat_activity blocked_activity ON blocked_activity.pid = blocked_locks.pid
    JOIN pg_catalog.pg_locks blocking_locks
      ON blocking_locks.locktype = blocked_locks.locktype
      AND blocking_locks.relation IS NOT DISTINCT FROM blocked_locks.relation
      AND blocking_locks.pid != blocked_locks.pid
    JOIN pg_catalog.pg_stat_activity blocking_activity ON blocking_activity.pid = blocking_locks.pid
    WHERE NOT blocked_locks.granted;
  "
# Expected: empty = no lock contention; rows = kill blocking_pid if safe

# Kill a blocking query if safe (verify the query is not critical first)
# docker compose -f deploy/docker-compose.yml exec postgres psql -U argus -d argus -c "SELECT pg_terminate_backend(<blocking_pid>);"
```

### 4b. Operator adapter investigation

If circuit breaker is open or `argus_operator_health` shows 0/1:

```bash
# Check operator adapter logs
docker compose -f deploy/docker-compose.yml logs --tail=200 argus | grep -E 'operator|adapter|RADIUS|Diameter' | grep -iE 'error|timeout|fail' | tail -20
# Expected: timeout or connection refused messages indicate upstream issue

# Check AAA auth latency
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.99%2C+rate(argus_aaa_auth_latency_seconds_bucket%5B5m%5D))%20by%20(protocol%2C+operator_id)' | \
  jq '.data.result[] | {protocol: .metric.protocol, operator: .metric.operator_id, p99_ms: (.value[1] | tonumber * 1000 | round)}'
# Expected: p99 > 500ms for specific operator indicates upstream slowness

# If upstream operator is slow, consider switching to failover — see operator-down.md
```

### 5. Enable pprof briefly and capture CPU profile

Use pprof only if you cannot identify the bottleneck from metrics and logs. Enable it for the minimum duration needed (the endpoint is protected but should not be left open indefinitely).

```bash
# pprof is available on the internal port (not exposed via nginx)
# Port 8080 must be accessible from the operator machine or via docker exec

# Capture a 30-second CPU profile
curl -o /tmp/argus-cpu.prof 'http://localhost:8080/debug/pprof/profile?seconds=30'
# Expected: downloads a pprof binary profile after 30 seconds

# Analyze the profile
go tool pprof -top /tmp/argus-cpu.prof
# Expected: shows top CPU-consuming functions — identifies hot code paths

# Capture a heap profile
curl -o /tmp/argus-heap.prof 'http://localhost:8080/debug/pprof/heap'
go tool pprof -top /tmp/argus-heap.prof
# Expected: shows top memory allocations

# Open interactive web UI (requires browser access to operator machine)
go tool pprof -http=:6060 /tmp/argus-cpu.prof &
# Then open http://localhost:6060 in browser
```

### 6. Remediate and verify

Based on root cause:

**If DB pool exhausted:** restart argus to reset connections; consider increasing pool size via `DATABASE_MAX_OPEN_CONNS` env var.

```bash
docker compose -f deploy/docker-compose.yml restart argus
```

**If specific query is slow:** add missing index or update query plan statistics:

```bash
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U argus -d argus -c "ANALYZE <table_name>;"
```

**If operator is causing timeouts:** follow `operator-down.md` to enable failover.

**If it's a traffic spike:** enable rate limiting or temporary caching at nginx layer.

After remediation, verify recovery:

```bash
# Confirm p99 is below SLO threshold
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.99%2C+rate(argus_http_request_duration_seconds_bucket%5B5m%5D))' | \
  jq '[.data.result[] | {route: .metric.route, p99_ms: (.value[1] | tonumber * 1000 | round)}] | sort_by(.p99_ms) | reverse | .[0:5]'
# Expected: all routes below 500ms

# Health check
curl -sf http://localhost:8084/health/ready | jq
# Expected: {"status":"ok", ...}
```

---

## Verification

- `histogram_quantile(0.99, rate(argus_http_request_duration_seconds_bucket[5m]))` < 0.5 for all routes
- `curl http://localhost:8084/health/ready` returns 200
- `argus_db_pool_connections{state="idle"}` > 0 (pool not exhausted)
- No ongoing slow queries in `pg_stat_activity`
- Grafana panel `<grafana>/d/argus-overview?panel=12` shows downward trend

---

## Post-incident

- Audit log entry: `argusctl audit log --action=latency_slo_breach_resolved --resource=api --note="peak_p99=<ms>ms, root_cause=<db|operator|code>, action=<taken>"`
- Delete pprof profile files: `rm /tmp/argus-*.prof`
- Document the slow query or hot endpoint for the next sprint backlog
- **Comms template (incident channel):**
  > `[RESOLVED] API p99 latency SLO breach resolved. Peak: <ms>ms. Root cause: <describe>. Resolved by: <action>. Duration: <minutes> min. User impact: slow responses on <endpoints>.`
- **Stakeholder email:**
  > Subject: [Argus] API latency SLO breach resolved
  > Body: API response latency (p99) exceeded 500ms at <time>, peaking at <ms>ms. Root cause: <describe>. Resolution: <action taken>. Service restored at <time>. Affected routes: <list>. Tenant impact: <none|specific tenants>.
