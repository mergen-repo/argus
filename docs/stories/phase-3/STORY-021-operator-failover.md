# STORY-021: Operator Failover & Circuit Breaker

## User Story
As a platform operator, I want automatic operator failover with circuit breaker, health checks, and configurable failover policies, so that SIM connectivity is maintained even when an operator goes down.

## Description
Implement circuit breaker pattern for operator connections with three states (closed/open/half-open). Configurable failover policies per operator: reject (immediate fail), fallback_to_next (route to SoR next-best operator), queue_with_timeout (hold briefly then fallback/reject). Health check heartbeat runs at configurable intervals. SLA violation events trigger alerts via notification service.

> **Note (post-STORY-009):** STORY-009 already implemented: circuit breaker (`internal/operator/circuit_breaker.go`), background health check loop (`internal/operator/health.go`) with per-operator goroutines, circuit breaker integration (RecordSuccess/RecordFailure), health status persistence to TBL-23 + Redis cache, and health status mapping (open->down, half_open->degraded, closed+success->healthy). This story should focus on **AAA-level failover routing** (reject/fallback_to_next/queue_with_timeout applied during actual RADIUS/Diameter request handling), NATS event publishing on state transitions, SVC-08 notification alerts, WebSocket push, and SLA violation detection. Effort may be reduced from L to M.
>
> **Note (post-STORY-018):** STORY-018 additionally implemented: `FailoverEngine` with `ExecuteAuth`/`ExecuteAcct` (reject/fallback_to_next/queue_with_timeout routing), `ForwardAuthWithFailover`, `ForwardAuthWithPolicy` on `OperatorRouter`, `FailoverConfig` struct, and 3 new router methods (`Authenticate`, `AccountingUpdate`, `FetchAuthVectors`) with circuit breaker integration. Core failover routing is now fully functional. This story's remaining scope is: NATS event publishing on state transitions, SVC-08 notification alerts, WebSocket push, and SLA violation detection. Effort reduced from L to M.
>
> **Note (post-STORY-019):** STORY-019 (Diameter server) implements DWR watchdog timeout-based peer failure detection, publishing `argus.events.operator.health` NATS events with `status: "down"` and `reason: "watchdog_timeout"` when a Diameter peer becomes unresponsive. This story's NATS event publishing and notification system should consume these existing Diameter peer health events alongside the circuit breaker state transitions from STORY-009/018. The Diameter peer health event format: `{operator_host, status, reason, timestamp}`.

## Architecture Reference
- Services: SVC-06 (Operator Router — internal/operator)
- Database Tables: TBL-05 (operators — circuit_breaker_threshold, failover_policy, health_check_interval_sec), TBL-23 (operator_health_logs)
- Data Flows: FLW-05 (Operator Failover)
- Source: docs/architecture/flows/_index.md (FLW-05)

## Screen Reference
- SCR-041: Operator Detail — health status, circuit breaker state, failover config, health log timeline
- SCR-040: Operator List — health indicator per operator

## Acceptance Criteria
- [x] Circuit breaker: failure count tracks consecutive failures per operator — done in STORY-009
- [x] Circuit breaker opens when failures >= circuit_breaker_threshold (from TBL-05) — done in STORY-009
- [x] Circuit breaker states: closed (normal) → open (all requests fail-fast) → half-open (test traffic) — done in STORY-009
- [ ] Failover policy 'reject': return Access-Reject immediately when circuit is open
- [ ] Failover policy 'fallback_to_next': route to SoR next-best operator for the SIM
- [ ] Failover policy 'queue_with_timeout': hold request for N ms, then fallback or reject
- [x] Health check heartbeat: send test request every health_check_interval_sec — done in STORY-009
- [x] Health check success on open circuit → transition to half-open, try real traffic — done in STORY-009
- [x] Half-open success → close circuit, mark operator healthy — done in STORY-009
- [x] Half-open failure → re-open circuit, reset health check timer — done in STORY-009
- [x] Health status changes persisted to TBL-23 (operator_health_logs) — done in STORY-009
- [ ] NATS: publish "operator.health_changed" on every state transition
- [ ] SVC-08 (Notification): send alert on operator down (email + Telegram + in-app)
- [ ] SVC-02 (WebSocket): push operator.health_changed to portal clients
- [ ] SLA tracking: log response_time_ms per request, detect SLA violations (p95 > threshold)
- [ ] SLA violation triggers alert.new event

## Dependencies
- Blocked by: STORY-018 (operator adapter), STORY-009 (operator CRUD)
- Blocks: STORY-026 (SoR engine uses failover as trigger)

## Test Scenarios
- [ ] 5 consecutive failures → circuit opens, operator marked 'down'
- [ ] Circuit open → failover policy 'reject' returns Access-Reject
- [ ] Circuit open → failover policy 'fallback_to_next' routes to alternate operator
- [ ] Circuit open → failover policy 'queue_with_timeout' waits then falls back
- [ ] Health check succeeds on open circuit → half-open → real traffic succeeds → circuit closes
- [ ] Health check succeeds but real traffic fails → circuit re-opens
- [ ] Operator health_changed event published to NATS and pushed via WebSocket
- [ ] Alert sent via notification service when operator goes down
- [ ] Operator recovery → alert cleared, health_status = 'healthy'
- [ ] SLA violation detected → alert.new event published

## Effort Estimate
- Size: L
- Complexity: High
