# Data Flow Index — Argus

## FLW-01: RADIUS Authentication

```
IoT Device → Operator P-GW → RADIUS Access-Request (UDP :1812)
    → SVC-04 (AAA Engine): parse RADIUS packet
    → Redis: lookup SIM by IMSI (cache hit ~0.5ms)
        → cache miss → SVC-03 (store) → TBL-10 (sims) → cache set
    → SVC-06 (Operator Router): validate operator, check circuit breaker
    → SVC-05 (Policy Engine): evaluate policy from Redis cache
        → compiled_rules from TBL-14 (cached in Redis)
    → SVC-04: build Access-Accept/Reject response
        → set Framed-IP from TBL-09 (ip_addresses)
        → set QoS attributes from policy
    → Redis: create/update session state
    → NATS: publish "session.started" event
        → SVC-07 (Analytics): CDR start record → TBL-18
        → SVC-02 (WS): push to connected portal clients
    → RADIUS Access-Accept → Operator P-GW → Device connected
```

## FLW-02: RADIUS Accounting

```
Operator P-GW → RADIUS Accounting-Request (UDP :1813)
    → SVC-04: parse, extract Acct-Status-Type
    → IF start:
        → Redis: confirm session exists
        → TBL-17: create session record
    → IF interim-update:
        → Redis: update session counters (bytes_in/out)
        → NATS: publish "session.interim" event
            → SVC-07: CDR interim record → TBL-18
            → SVC-05: check quota against policy
                → IF quota exceeded → SVC-04: send CoA (throttle) or DM (disconnect)
    → IF stop:
        → Redis: remove session
        → TBL-17: update session (ended_at, terminate_cause)
        → NATS: publish "session.ended"
            → SVC-07: CDR stop record → TBL-18
            → SVC-02: push session.ended to portal
    → RADIUS Accounting-Response → Operator P-GW
```

## FLW-03: Policy Staged Rollout

```
Policy Editor → POST /api/v1/policy-versions/:id/rollout (API-096)
    → SVC-01 (Gateway): auth + RBAC (policy_editor+)
    → SVC-03 (Core API): validate version state = 'active' or 'draft'
    → SVC-05 (Policy Engine):
        → Create rollout record → TBL-16 (policy_rollouts)
        → Calculate affected SIMs (from TBL-10 via segment filter)
        → Stage 1 (1%): select random 1% of affected SIMs
            → For each SIM: update TBL-15 (policy_assignments) to new version
            → NATS: publish "policy.assignment_changed" per SIM
                → SVC-04: send CoA to each active session
            → Update TBL-16: migrated_sims count
        → NATS: publish "policy.rollout_progress"
            → SVC-02 (WS): push progress to portal
    → Response: rollout status with progress

Policy Editor → POST /api/v1/policy-rollouts/:id/advance (API-097)
    → SVC-05: advance to next stage (10%), repeat above for next batch

Policy Editor → POST /api/v1/policy-rollouts/:id/rollback (API-098)
    → SVC-05: revert ALL SIMs to previous version
        → Mass update TBL-15 back to previous_version_id
        → Mass CoA via NATS → SVC-04
        → Update TBL-16: state = 'rolled_back'
```

## FLW-04: Bulk SIM Import

```
SIM Manager → POST /api/v1/sims/bulk/import (API-063)
    → SVC-01: auth + RBAC + file upload (CSV, max 50MB)
    → SVC-03: validate CSV header columns
    → Create job → TBL-20 (jobs), state = 'queued'
    → NATS: publish "job.created" to job queue
    → Response 202: { jobId, status: "queued" }

SVC-09 (Job Runner):
    → Consume from NATS job queue
    → Lock job (set locked_by in TBL-20)
    → Parse CSV rows
    → For each row:
        → Validate: ICCID unique, IMSI unique, operator exists, APN exists
        → IF valid:
            → Insert TBL-10 (sims), state = 'ordered'
            → Auto-activate: state → 'active', allocate IP from TBL-09
            → Insert TBL-11 (sim_state_history)
            → Assign default policy from APN → TBL-15
            → Increment job.processed_items
        → IF invalid:
            → Add to error_report JSONB
            → Increment job.failed_items
        → Update TBL-20: progress_pct
        → Every 100 rows: NATS publish "job.progress"
            → SVC-02 (WS): push to portal
    → Complete: update TBL-20 state = 'completed'
    → NATS: publish "job.completed"
        → SVC-08 (Notification): send notification to job creator
```

## FLW-05: Operator Failover

```
SVC-04 (AAA Engine): forward auth to operator adapter
    → SVC-06 (Operator Router): select adapter for operator_id
    → Adapter sends RADIUS/Diameter to operator
    → Timeout / connection error
    → SVC-06: increment circuit breaker failure count
        → IF count >= threshold (from TBL-05.circuit_breaker_threshold):
            → Open circuit breaker
            → Update TBL-05: health_status = 'down'
            → Insert TBL-23 (operator_health_logs)
            → NATS: publish "operator.health_changed"
                → SVC-08: send alert (email + Telegram + in-app)
                → SVC-02: push to portal
            → Check failover_policy:
                → 'reject': return Access-Reject
                → 'fallback_to_next': route to next operator via SoR
                → 'queue_with_timeout': hold for N ms, then fallback or reject

SVC-06: periodic health check (every health_check_interval_sec)
    → Send test RADIUS request to operator
    → IF success AND circuit is open:
        → Half-open: try real traffic
        → IF real traffic succeeds: close circuit
        → Update TBL-05: health_status = 'healthy'
        → NATS: publish "operator.health_changed"
```

## FLW-06: eSIM Cross-Operator Switch

```
SIM Manager → POST /api/v1/sims/bulk/operator-switch (API-066)
    → SVC-01: auth + RBAC (tenant_admin)
    → SVC-03: validate segment, target operator, APN availability
    → Create job → TBL-20, type = 'bulk_esim_switch'
    → Response 202: { jobId }

SVC-09 (Job Runner):
    → For each SIM in segment:
        → SVC-06: call current operator SM-DP+ API → disable profile
        → Update TBL-12: profile_state = 'disabled'
        → SVC-06: call target operator SM-DP+ API → enable profile
        → Update TBL-12: operator_id = new, profile_state = 'enabled'
        → Update TBL-10: operator_id = new, apn_id = new APN
        → Reallocate IP from new APN's pool → TBL-09
        → Reassign policy version → TBL-15
        → IF active session: send CoA/DM → SVC-04
        → Insert TBL-11: state_history (operator_switch)
        → NATS: publish "sim.operator_switched"
    → On completion: cost comparison analytics → SVC-07
```

## FLW-07: Connectivity Diagnostics

```
SIM Manager → POST /api/v1/sims/:id/diagnose (API-049)
    → SVC-03: load SIM from TBL-10
    → Step 1: Check SIM state (must be 'active')
    → Step 2: Check last auth from TBL-17 (sessions)
        → Find last session, check if rejected
        → If rejected: extract reject reason from RADIUS response
    → Step 3: Check operator health from TBL-05
        → If operator down: "Operator connection is down"
    → Step 4: Check APN config
        → Verify APN exists, is active, mapped to operator
    → Step 5: Check policy
        → Verify policy version is active, not throttled to 0
    → Step 6: Check IP pool
        → Verify pool has available addresses
    → Step 7: Optionally trigger test auth
        → SVC-04: send test Access-Request through operator
        → Wait for response (timeout 5s)
    → Response: diagnostic report with findings + suggested actions
```
