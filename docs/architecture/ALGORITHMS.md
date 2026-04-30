# Key Algorithms — Argus

> This document specifies the exact algorithms used for critical operations.
> Implementations reference these specs for correctness verification.

---

## 1. IP Address Allocation

**Package**: `internal/store/ip_store.go`
**Table**: TBL-09 `ip_addresses`

### Invocation Points (STORY-092)

`AllocateIP` is invoked from five call sites today — three AAA hot paths added by STORY-092 alongside the two pre-existing admin/import paths:

| Caller | Path | Trigger | Release path |
|--------|------|---------|--------------|
| RADIUS Access-Accept | `internal/aaa/radius/server.go` (`allocateDynamicIPIfNeeded`) | `sim.IPAddressID == nil && sim.APNID != nil` — after policy Allow check | RADIUS Accounting-Stop (`releaseDynamicIPIfNeededForSession`, dynamic only; static preserved) |
| Diameter Gx CCA-I | `internal/aaa/diameter/gx.go` (`handleInitial`) | same precondition — after SIM confirmed active | Diameter Gx CCR-T (`releaseDynamicIPIfNeeded`; static preserved) |
| 5G SBA Nsmf CreateSMContext | `internal/aaa/sba/nsmf.go` (`HandleCreate`) | POST to `/nsmf-pdusession/v1/sm-contexts` with active SIM + APN | DELETE `/nsmf-pdusession/v1/sm-contexts/{ref}` (`HandleRelease`; static preserved) |
| Admin / API | `internal/api/sim/handler.go`, `internal/api/ippool/handler.go` | Explicit operator action | Admin `ReleaseIP` + scheduled reclaim |
| Bulk import | `internal/job/import.go` | CSV import job | Admin release flow |

The `used_addresses` counter on `ip_pools` is maintained app-side (D2-A locked 2026-04-18) by `AllocateIP` / `ReleaseIP`. `RecountUsedAddresses` (added in STORY-092) is available as a deterministic reconciliation helper — used only from admin tools, never on the hot path.

### Allocation Algorithm

```
FUNCTION allocate_ip(pool_id, sim_id) → (ip_address, error)

1. BEGIN TRANSACTION (SERIALIZABLE isolation)
2. SELECT pool FROM ip_pools WHERE id = pool_id FOR UPDATE
3. IF pool.state = 'exhausted' OR pool.state = 'disabled':
     RETURN error POOL_EXHAUSTED
4. SELECT first available IPv4:
     SELECT id, address_v4 FROM ip_addresses
     WHERE pool_id = pool_id
       AND state = 'available'
       AND address_v4 IS NOT NULL
     ORDER BY address_v4 ASC
     LIMIT 1
     FOR UPDATE SKIP LOCKED
5. IF no IPv4 found:
     SELECT first available IPv6 (same query with address_v6)
6. IF no IP found:
     UPDATE ip_pools SET state = 'exhausted' WHERE id = pool_id
     RETURN error POOL_EXHAUSTED
7. UPDATE ip_addresses SET
     state = 'allocated',
     sim_id = sim_id,
     allocated_at = NOW()
   WHERE id = selected_ip.id
8. UPDATE ip_pools SET used_addresses = used_addresses + 1 WHERE id = pool_id
9. CHECK utilization thresholds:
     utilization = (pool.used_addresses + 1) / pool.total_addresses * 100
     IF utilization >= pool.alert_threshold_critical:
       PUBLISH event 'pool.threshold_critical'
     ELSE IF utilization >= pool.alert_threshold_warning:
       PUBLISH event 'pool.threshold_warning'
10. COMMIT TRANSACTION
11. RETURN allocated ip_address
```

### Release Algorithm

```
FUNCTION release_ip(pool_id, sim_id) → error

1. BEGIN TRANSACTION
2. SELECT ip FROM ip_addresses
   WHERE pool_id = pool_id AND sim_id = sim_id AND state = 'allocated'
   FOR UPDATE
3. IF ip.allocation_type = 'static':
     -- Static IPs are NOT released, only moved to reclaiming after SIM termination
     UPDATE ip_addresses SET
       state = 'reclaiming',
       reclaim_at = NOW() + INTERVAL (pool.reclaim_grace_period_days || ' days')
     WHERE id = ip.id
4. ELSE (dynamic):
     UPDATE ip_addresses SET
       state = 'available',
       sim_id = NULL,
       allocated_at = NULL
     WHERE id = ip.id
     UPDATE ip_pools SET used_addresses = used_addresses - 1 WHERE id = pool_id
     IF pool.state = 'exhausted':
       UPDATE ip_pools SET state = 'active' WHERE id = pool_id
5. COMMIT TRANSACTION
```

### Reclaim Sweep (Scheduled Job)

Runs every hour via the job runner:

```
FUNCTION reclaim_expired_ips()

UPDATE ip_addresses SET
  state = 'available',
  sim_id = NULL,
  allocated_at = NULL,
  reclaim_at = NULL
WHERE state = 'reclaiming'
  AND reclaim_at <= NOW()
RETURNING pool_id

-- For each affected pool:
UPDATE ip_pools SET
  used_addresses = (SELECT COUNT(*) FROM ip_addresses WHERE pool_id = X AND state = 'allocated'),
  state = 'active'
WHERE id = X AND state = 'exhausted'
```

---

## 2. Audit Hash Chain

**Package**: `internal/audit/hash.go`
**Table**: TBL-19 `audit_logs`

### Hash Computation

```
FUNCTION compute_hash(entry, prev_hash) → string

1. Construct data string by concatenating with '|' separator:
     data = tenant_id|user_id|action|entity_type|entity_id|created_at(RFC3339Nano)|prev_hash

   Rules:
   - tenant_id: UUID string (lowercase, hyphenated)
   - user_id: UUID string or "system" if null
   - action: string as-is
   - entity_type: string as-is
   - entity_id: string as-is
   - created_at: time.RFC3339Nano format (e.g., "2026-03-18T14:02:00.123456789Z")
   - prev_hash: 64-character hex string

2. Compute SHA-256:
     hash = SHA256([]byte(data))

3. Return hex-encoded string (lowercase, 64 characters):
     return hex.EncodeToString(hash[:])
```

### Genesis Entry

The first audit log entry per tenant uses a special prev_hash:

```
prev_hash = "0000000000000000000000000000000000000000000000000000000000000000"
```
(64 zero characters)

### Chain Verification

```
FUNCTION verify_chain(tenant_id, from_id, to_id) → (valid bool, broken_at *int64, error)

1. SELECT entries FROM audit_logs
   WHERE tenant_id = tenant_id
     AND id BETWEEN from_id AND to_id
   ORDER BY id ASC

2. FOR each entry (starting from second):
     expected_hash = compute_hash(entry, prev_entry.hash)
     IF entry.hash != expected_hash:
       RETURN (false, entry.id, nil)
     IF entry.prev_hash != prev_entry.hash:
       RETURN (false, entry.id, nil)

3. RETURN (true, nil, nil)
```

### Concurrency

Audit log writes are serialized per tenant using a NATS-based queue to ensure sequential hash chain integrity. Each tenant has its own audit write channel.

```
NATS subject: argus.audit.write.{tenant_id}
Consumer: exclusive (one writer per tenant)
```

---

## 3. Rate Limiting (Sliding Window)

**Package**: `internal/gateway/ratelimit.go`
**Store**: Redis

### Sliding Window Counter Algorithm

```
FUNCTION check_rate_limit(identifier, endpoint, limit, window_seconds) → (allowed bool, remaining int, reset_at int64)

1. current_time = NOW() as Unix timestamp (seconds)
2. window_start = current_time - (current_time % window_seconds)
3. current_key = "ratelimit:{identifier}:{endpoint}:{window_start}"
4. previous_key = "ratelimit:{identifier}:{endpoint}:{window_start - window_seconds}"

5. Redis MULTI/EXEC pipeline:
     a. GET previous_key → prev_count (default 0)
     b. INCR current_key → curr_count
     c. EXPIRE current_key window_seconds * 2  (TTL = 2x window to cover overlap)

6. Calculate weighted count (sliding window approximation):
     elapsed_in_window = current_time - window_start
     weight = (window_seconds - elapsed_in_window) / window_seconds
     weighted_count = (prev_count * weight) + curr_count

7. IF weighted_count > limit:
     remaining = 0
     reset_at = window_start + window_seconds
     DECR current_key  (undo the INCR since request is rejected)
     RETURN (false, 0, reset_at)

8. remaining = limit - ceil(weighted_count)
   reset_at = window_start + window_seconds
   RETURN (true, remaining, reset_at)
```

### Rate Limit Resolution Order

When determining which limit to apply:

```
1. API key-specific limit (TBL-04 api_keys.rate_limit_per_minute)
   └─ If set and request uses API key → use this
2. Tenant-specific limit (TBL-01 tenants.settings.rate_limit_per_minute)
   └─ If set → use this
3. Endpoint-specific global limit (RATE_LIMIT_AUTH_PER_MINUTE for /auth/login)
   └─ If endpoint has special limit → use this
4. Global default (RATE_LIMIT_DEFAULT_PER_MINUTE)
   └─ Fallback
```

---

## 4. Anomaly Detection (Rule-Based v1)

**Package**: `internal/analytics/anomaly/`
**Store**: Redis (for real-time counters), PostgreSQL (for historical data)

### SIM Cloning Detection

```
FUNCTION detect_sim_cloning(imsi, nas_ip, timestamp)

1. Redis key: "anomaly:sim_clone:{imsi}"
   Value: sorted set of (nas_ip, timestamp) pairs

2. ZADD key timestamp nas_ip
3. ZRANGEBYSCORE key (timestamp - 300) timestamp → recent_entries
4. Extract unique NAS IPs from recent_entries
5. IF count(unique_nas_ips) >= 2:
     TRIGGER alert:
       type = "sim_cloning"
       severity = "critical"
       details = { imsi, nas_ips: unique_nas_ips, window_seconds: 300 }
6. EXPIRE key 600  (cleanup after 10 minutes)
```

**Rule**: Same IMSI authenticating from 2 or more different NAS IP addresses within a 5-minute window.

### Data Spike Detection

```
FUNCTION detect_data_spike(sim_id, current_hour_bytes)

1. Query average hourly usage for this SIM over last 7 days:
     SELECT AVG(bytes_total) as avg_hourly
     FROM cdr_hourly_agg
     WHERE sim_id = sim_id
       AND bucket >= NOW() - INTERVAL '7 days'

2. IF current_hour_bytes > avg_hourly * 3:
     spike_ratio = current_hour_bytes / avg_hourly
     TRIGGER alert:
       type = "data_spike"
       severity = IF spike_ratio > 10 THEN "critical" ELSE "warning"
       details = { sim_id, current_bytes: current_hour_bytes,
                   avg_bytes: avg_hourly, ratio: spike_ratio }
```

**Rule**: Current hour data usage exceeds 3x the 7-day hourly average.

### Auth Flood Detection

```
FUNCTION detect_auth_flood(nas_ip, auth_result, timestamp)

1. IF auth_result = "reject":
     Redis key: "anomaly:auth_flood:{nas_ip}"
     INCR key
     EXPIRE key 60  (1-minute window)

2. GET key → failed_count
3. IF failed_count > 100:
     TRIGGER alert:
       type = "auth_flood"
       severity = "critical"
       details = { nas_ip, failed_count, window_seconds: 60 }
     -- Optionally: auto-block NAS IP for 5 minutes
     SET "anomaly:blocked_nas:{nas_ip}" "auth_flood" EX 300
```

**Rule**: More than 100 failed authentication attempts from the same NAS IP within 1 minute.

### Anomaly Alert Deduplication

```
FUNCTION should_alert(tenant_id, alert_type, entity_id) → bool

1. dedup_key = "anomaly:dedup:{tenant_id}:{alert_type}:{entity_id}"
2. result = SET dedup_key "1" NX EX 3600  (1-hour dedup window)
3. RETURN result == OK  (only alert if key was newly set)
```

---

## 5. Cost Calculation

**Package**: `internal/analytics/cdr/`
**Triggered by**: Accounting-Stop event (CDR creation)

### Per-Session Cost

```
FUNCTION calculate_session_cost(session) → Cost

1. total_bytes = session.bytes_in + session.bytes_out
2. total_mb = total_bytes / (1024 * 1024)

3. Lookup rate:
     policy = get_policy_for_sim(session.sim_id)
     rate_per_mb = policy.charging.rate_per_mb

4. Apply RAT type multiplier:
     multiplier = policy.charging.rat_type_multiplier[session.rat_type]
     IF multiplier is null: multiplier = 1.0

5. usage_cost = total_mb × rate_per_mb × multiplier

6. Carrier cost (tracked separately):
     carrier_rate = get_carrier_rate(session.operator_id, session.rat_type)
     carrier_cost = total_mb × carrier_rate.rate_per_mb

7. margin = usage_cost - carrier_cost

8. RETURN Cost{
     usage_cost:  round(usage_cost, 6),
     carrier_cost: round(carrier_cost, 6),
     margin:      round(margin, 6),
     currency:    tenant.settings.currency,
     rate_per_mb: rate_per_mb,
     multiplier:  multiplier,
     total_mb:    round(total_mb, 4),
   }
```

### Monthly Cost Aggregation

```sql
-- TimescaleDB continuous aggregate (materialized view)
CREATE MATERIALIZED VIEW cost_monthly_agg
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 month', created_at) AS bucket,
    tenant_id,
    operator_id,
    apn_id,
    rat_type,
    SUM(bytes_in + bytes_out) AS total_bytes,
    SUM(usage_cost) AS total_usage_cost,
    SUM(carrier_cost) AS total_carrier_cost,
    SUM(usage_cost - carrier_cost) AS total_margin,
    COUNT(*) AS session_count
FROM cdrs
GROUP BY bucket, tenant_id, operator_id, apn_id, rat_type;
```

---

## 5a. CDR Export Streaming

**Package**: `internal/api/cdr/` (handler), `internal/export/` (streaming primitives)
**Endpoint**: `GET /api/v1/cdrs/export` → `ExportCSV`

### Cursor-Pagination Streaming

CDR export never buffers the full result set. Instead it iterates in 500-row cursor pages and writes rows directly to the HTTP response stream:

```
FUNCTION ExportCSV(w, r)

1. Extract tenant_id from request context (forbidden if missing)
2. Parse optional filters: operator_id, sim_id
3. Set response headers:
     Content-Type: text/csv; charset=utf-8
     Content-Disposition: attachment; filename="cdrs_{filters}_{date}.csv"
     Cache-Control: no-cache

4. params = ListCDRParams{ Limit: 500 }
   cursor = ""

5. LOOP:
     params.Cursor = cursor
     cdrs, next, err = cdrStore.ListByTenant(ctx, tenantID, params)
     IF err: log error, RETURN

     FOR each cdr in cdrs:
       yield([]string{ id, session_id, sim_id, operator_id, apn_id,
                       rat_type, record_type, bytes_in, bytes_out,
                       duration_sec, usage_cost, timestamp })

     IF next == "": BREAK
     cursor = next

6. After every 500 rows written:
     csv.Writer.Flush()          -- flush csv buffer to http.ResponseWriter
     http.Flusher.Flush()        -- flush http buffer to network (chunked transfer)

7. Final csv.Writer.Flush() after loop ends
```

### Key Properties

| Property | Value |
|----------|-------|
| Chunk size | 500 rows per `ListByTenant` call |
| Cursor field | opaque string (ID-based cursor, `""` = first page) |
| Memory bound | O(500) rows in memory at any point |
| Network flush | Every 500 rows via `http.Flusher` (chunked HTTP) |
| CSV flush | Every 500 rows + final flush after loop |
| Backpressure | `yield` returns `false` to abort mid-stream (client disconnect) |

### Flow Diagram

```
Client                Handler (ExportCSV)           DB (ListByTenant)
  │                         │                              │
  │── GET /cdrs/export ────►│                              │
  │                         │── SELECT LIMIT 500 cursor="" ►│
  │                         │◄─ cdrs[0..499], next="abc" ──│
  │                         │  write 500 rows → csv.Flush + http.Flush
  │◄── chunk 1 ─────────────│                              │
  │                         │── SELECT LIMIT 500 cursor="abc" ►│
  │                         │◄─ cdrs[0..387], next="" ─────│
  │                         │  write 387 rows → csv.Flush
  │◄── chunk 2 (EOF) ───────│                              │
```

---

## 6. Policy Evaluation Order

**Package**: `internal/policy/evaluator/`

### Scope Resolution

```
FUNCTION resolve_policy(sim) → compiled_rules

1. Check SIM-specific override:
     SELECT pv.compiled_rules FROM policy_assignments pa
     JOIN policy_versions pv ON pa.policy_version_id = pv.id
     WHERE pa.sim_id = sim.id
     → IF found: RETURN compiled_rules

2. Check SIM's explicitly assigned policy version:
     IF sim.policy_version_id IS NOT NULL:
       SELECT compiled_rules FROM policy_versions WHERE id = sim.policy_version_id
       → RETURN compiled_rules

3. Check APN-level policy:
     SELECT p.current_version_id FROM policies p
     WHERE p.tenant_id = sim.tenant_id
       AND p.scope = 'apn'
       AND p.scope_ref_id = sim.apn_id
       AND p.state = 'active'
     ORDER BY p.created_at DESC LIMIT 1
     → IF found: RETURN compiled_rules for that version

4. Check operator-level policy:
     SELECT p.current_version_id FROM policies p
     WHERE p.tenant_id = sim.tenant_id
       AND p.scope = 'operator'
       AND p.scope_ref_id = sim.operator_id
       AND p.state = 'active'
     ORDER BY p.created_at DESC LIMIT 1
     → IF found: RETURN compiled_rules for that version

5. Check tenant default (global scope):
     SELECT p.current_version_id FROM policies p
     WHERE p.tenant_id = sim.tenant_id
       AND p.scope = 'global'
       AND p.state = 'active'
     ORDER BY p.created_at DESC LIMIT 1
     → IF found: RETURN compiled_rules for that version

6. No policy found: RETURN default_rules (system-wide hardcoded fallback)
```

**Rule**: Most specific wins. SIM-specific > APN-level > operator-level > tenant default.

### Rule Evaluation Within a Policy

```
FUNCTION evaluate_rules(compiled_rules, session_context) → EvaluationResult

1. Start with default assignments:
     result = compiled_rules.rules.defaults  (bandwidth_down, bandwidth_up, etc.)

2. Evaluate WHEN blocks in order (top to bottom):
     FOR each when_block in compiled_rules.rules.when_blocks:
       IF evaluate_condition(when_block.condition, session_context):
         -- Apply assignments (override defaults)
         result.merge(when_block.assignments)
         -- Collect actions
         result.actions.append(when_block.actions)

3. Within same scope level: last matching WHEN block wins for conflicting assignments.
   Actions from ALL matching WHEN blocks are collected (not overridden).

4. RETURN result
```

---

## 7. Staged Rollout SIM Selection

**Package**: `internal/policy/rollout/`
**Table**: TBL-16 `policy_rollouts`, TBL-15 `policy_assignments`

### Stage Execution

```
FUNCTION execute_rollout_stage(rollout, stage_index) → error

1. stage = rollout.stages[stage_index]
2. target_count = ceil(rollout.total_sims * stage.pct / 100) - rollout.migrated_sims

3. Select SIMs for this stage:
     SELECT sim_id FROM sims
     WHERE tenant_id = rollout.tenant_id
       AND state = 'active'
       AND policy_version_id = rollout.previous_version_id
       AND sim_id NOT IN (
         SELECT sim_id FROM policy_assignments
         WHERE rollout_id = rollout.id
       )
     ORDER BY random()
     LIMIT target_count
     FOR UPDATE SKIP LOCKED

4. FOR each selected SIM in batches of 1000:
     a. INSERT INTO policy_assignments (sim_id, policy_version_id, rollout_id, assigned_at)
        VALUES (sim.id, rollout.policy_version_id, rollout.id, NOW())
        ON CONFLICT (sim_id) DO UPDATE SET
          policy_version_id = EXCLUDED.policy_version_id,
          rollout_id = EXCLUDED.rollout_id,
          assigned_at = NOW(),
          coa_status = 'pending'

     b. UPDATE sims SET policy_version_id = rollout.policy_version_id
        WHERE id = sim.id

     c. IF SIM has active session:
          Queue CoA message:
            NATS publish "argus.coa.send" { sim_id, session_id, new_policy_version_id }

     d. UPDATE rollout SET migrated_sims = migrated_sims + 1

     e. PUBLISH event "policy.rollout_progress"

5. UPDATE rollout SET current_stage = stage_index
6. IF stage.pct = 100 AND all SIMs migrated:
     UPDATE rollout SET state = 'completed', completed_at = NOW()
     UPDATE policy_versions SET state = 'active' WHERE id = rollout.policy_version_id
     UPDATE policy_versions SET state = 'superseded' WHERE id = rollout.previous_version_id
```

### Rollback

```
FUNCTION rollback_rollout(rollout) → error

1. SELECT all SIMs assigned by this rollout:
     SELECT sim_id FROM policy_assignments WHERE rollout_id = rollout.id

2. FOR each SIM in batches of 1000:
     a. UPDATE sims SET policy_version_id = rollout.previous_version_id
        WHERE id = sim.id

     b. UPDATE policy_assignments SET
          policy_version_id = rollout.previous_version_id,
          coa_status = 'pending'
        WHERE sim_id = sim.id

     c. Queue CoA for active sessions

3. UPDATE rollout SET state = 'rolled_back', rolled_back_at = NOW()
4. UPDATE policy_versions SET state = 'rolled_back' WHERE id = rollout.policy_version_id
5. PUBLISH event "policy.rollout_progress" with state = "rolled_back"
```

---

## 8. Session Timeout

**Package**: `internal/aaa/session/timeout.go`
**Schedule**: Every 60 seconds via internal ticker (not a NATS job)

### Timeout Check

```
FUNCTION check_session_timeouts()

1. Idle Timeout Check:
     SELECT session_id, sim_id, acct_session_id, nas_ip
     FROM sessions
     WHERE state = 'active'
       AND last_interim_at < NOW() - (idle_timeout_sec * INTERVAL '1 second')
     LIMIT 1000

   FOR each timed-out session:
     a. Send Disconnect-Message (DM) to NAS:
          RADIUS DM packet → NAS IP : CoA port
          Attributes: Acct-Session-Id, User-Name (IMSI)
     b. UPDATE session SET state = 'terminated', terminate_cause = 'idle_timeout'
     c. PUBLISH "session.ended" event

2. Hard Timeout Check:
     SELECT session_id, sim_id, acct_session_id, nas_ip
     FROM sessions
     WHERE state = 'active'
       AND started_at < NOW() - (hard_timeout_sec * INTERVAL '1 second')
     LIMIT 1000

   FOR each timed-out session:
     a. Send DM to NAS
     b. UPDATE session SET state = 'terminated', terminate_cause = 'session_timeout'
     c. PUBLISH "session.ended" event
```

### Timeout Values

| Source | Idle Timeout | Hard Timeout |
|--------|-------------|--------------|
| SIM-level (TBL-10) | `session_idle_timeout_sec` (default 3600) | `session_hard_timeout_sec` (default 86400) |
| Policy override | `idle_timeout` property in RULES block | `session_timeout` property in RULES block |
| Effective | MIN(sim_level, policy_level) | MIN(sim_level, policy_level) |

### last_interim_at Tracking

The `last_interim_at` field on the session is updated every time a RADIUS Accounting-Interim or Diameter CCR-U is received. If no interim update arrives within `idle_timeout_sec`, the session is considered idle.

---

## 9. Operator Health Score

**Package**: `internal/operator/health.go`
**Table**: TBL-23 `operator_health_logs`

### Health Check Execution

```
FUNCTION run_health_check(operator) → HealthResult

1. Send protocol-appropriate probe:
     RADIUS: Access-Request with test IMSI (operator.settings.test_imsi)
     Diameter: DWR (Device-Watchdog-Request)
     5G SBA: GET /health or /nausf-auth/v1/status

2. Measure response time (latency_ms)
3. Determine success/failure:
     - Response received within timeout → success
     - Timeout or error → failure

4. Record result:
     INSERT INTO operator_health_logs
       (operator_id, check_type, success, latency_ms, error_message, created_at)
     VALUES (operator.id, 'heartbeat', success, latency_ms, error_msg, NOW())
```

### Uptime Calculation (24h)

```
FUNCTION calculate_uptime_24h(operator_id) → float64

total_checks = SELECT COUNT(*) FROM operator_health_logs
               WHERE operator_id = operator_id
                 AND created_at >= NOW() - INTERVAL '24 hours'

failed_checks = SELECT COUNT(*) FROM operator_health_logs
                WHERE operator_id = operator_id
                  AND created_at >= NOW() - INTERVAL '24 hours'
                  AND success = false

IF total_checks = 0: RETURN 100.0

uptime_pct = (total_checks - failed_checks) / total_checks * 100
RETURN round(uptime_pct, 2)
```

### Latency Percentiles

```
FUNCTION calculate_latency_percentiles(operator_id) → (p50, p95, p99)

1. Redis sorted set: "operator:latency:{operator_id}"
   Score = timestamp, Member = latency_ms

2. On each health check:
     ZADD key timestamp latency_ms
     ZREMRANGEBYSCORE key 0 (NOW() - 3600)  // keep last 1 hour

3. To calculate percentiles:
     all_latencies = ZRANGEBYSCORE key (NOW() - 3600) NOW()
     SORT all_latencies

     p50 = all_latencies[len * 0.50]
     p95 = all_latencies[len * 0.95]
     p99 = all_latencies[len * 0.99]

4. RETURN (p50, p95, p99)
```

### Health Status Determination

```
FUNCTION determine_health_status(operator_id) → string

uptime = calculate_uptime_24h(operator_id)
_, p95, _ = calculate_latency_percentiles(operator_id)
consecutive_failures = get_consecutive_failures(operator_id)

IF consecutive_failures >= operator.settings.circuit_breaker_threshold:
     RETURN "down"
ELSE IF uptime < 99.0 OR p95 > operator.settings.latency_threshold_ms:
     RETURN "degraded"
ELSE:
     RETURN "healthy"
```

### Circuit Breaker States

```
CLOSED (normal) ──[N consecutive failures]──► OPEN (rejecting)
                                                   │
                                          [recovery_window elapsed]
                                                   │
                                                   ▼
                                             HALF_OPEN (testing)
                                             │              │
                                    [test succeeds]    [test fails]
                                             │              │
                                             ▼              ▼
                                          CLOSED          OPEN
```

| Parameter | Default | Config |
|-----------|---------|--------|
| Failure threshold | 5 | `operator.settings.circuit_breaker_threshold` |
| Recovery window | 60s | `operator.settings.circuit_breaker_recovery_sec` |
| Half-open test count | 1 | Fixed |
