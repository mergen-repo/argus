# STORY-021: Operator Failover & Circuit Breaker

## User Story
As a platform operator, I want automatic operator failover with circuit breaker, health checks, and configurable failover policies, so that SIM connectivity is maintained even when an operator goes down.

## Description
Implement circuit breaker pattern for operator connections with three states (closed/open/half-open). Configurable failover policies per operator: reject (immediate fail), fallback_to_next (route to SoR next-best operator), queue_with_timeout (hold briefly then fallback/reject). Health check heartbeat runs at configurable intervals. SLA violation events trigger alerts via notification service.

## Architecture Reference
- Services: SVC-06 (Operator Router — internal/operator)
- Database Tables: TBL-05 (operators — circuit_breaker_threshold, failover_policy, health_check_interval_sec), TBL-23 (operator_health_logs)
- Data Flows: FLW-05 (Operator Failover)
- Source: docs/architecture/flows/_index.md (FLW-05)

## Screen Reference
- SCR-041: Operator Detail — health status, circuit breaker state, failover config, health log timeline
- SCR-040: Operator List — health indicator per operator

## Acceptance Criteria
- [ ] Circuit breaker: failure count tracks consecutive failures per operator
- [ ] Circuit breaker opens when failures >= circuit_breaker_threshold (from TBL-05)
- [ ] Circuit breaker states: closed (normal) → open (all requests fail-fast) → half-open (test traffic)
- [ ] Failover policy 'reject': return Access-Reject immediately when circuit is open
- [ ] Failover policy 'fallback_to_next': route to SoR next-best operator for the SIM
- [ ] Failover policy 'queue_with_timeout': hold request for N ms, then fallback or reject
- [ ] Health check heartbeat: send test RADIUS request every health_check_interval_sec
- [ ] Health check success on open circuit → transition to half-open, try real traffic
- [ ] Half-open success → close circuit, mark operator healthy
- [ ] Half-open failure → re-open circuit, reset health check timer
- [ ] Health status changes persisted to TBL-23 (operator_health_logs)
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
