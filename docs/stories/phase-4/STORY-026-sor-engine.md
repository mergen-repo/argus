# STORY-026: Steering of Roaming (SoR) Engine

## User Story
As a platform operator, I want automatic steering of roaming based on IMSI prefix, RAT preference, and cost optimization, so that SIMs connect to the best available operator.

## Description
Implement Steering of Roaming (SoR) engine within SVC-06 (Operator Router). Uses IMSI-prefix routing tables, RAT-type preference (prefer 4G over 3G when available), and cost-based operator selection (cheapest carrier for region/RAT combo). SoR is invoked during authentication when multiple operators are available for a SIM, and during failover when primary operator is down.

## Architecture Reference
- Services: SVC-06 (Operator Router — internal/operator/sor)
- Database Tables: TBL-05 (operators — region, supported_rat_types), TBL-06 (operator_grants), TBL-10 (sims)
- Source: docs/architecture/services/_index.md (SVC-06)

## Screen Reference
- SCR-041: Operator Detail — SoR priority, routing rules
- SCR-040: Operator List — operator ranking per region

## Acceptance Criteria
- [ ] IMSI-prefix routing: map IMSI prefix ranges to preferred operator(s)
- [ ] SoR routing table stored in Redis for O(1) lookup, sourced from operator config
- [ ] RAT-type preference: configurable preference order (e.g., 5G > 4G > 3G > 2G)
- [ ] RAT-type awareness: only route to operators that support the requested RAT
- [ ] Cost-based selection: when multiple operators match, prefer lowest cost_per_mb
- [ ] SoR priority: explicit priority field on operator_grants (lower = preferred)
- [ ] SoR invoked on: (1) initial auth (choose best operator), (2) failover (choose next-best)
- [ ] SoR result cached per SIM in Redis (TTL configurable, default 1h)
- [ ] SoR override: per-SIM operator lock (manual assignment) bypasses SoR
- [ ] SoR decision logged in session record (TBL-17.sor_decision field)
- [ ] Bulk re-evaluation: trigger SoR recalculation for segment when operator costs change

## Dependencies
- Blocked by: STORY-009 (operator CRUD), STORY-018 (operator adapter), STORY-021 (failover triggers SoR)
- Blocks: STORY-027 (RAT awareness uses SoR data)

> **Note (post-STORY-021):** STORY-021 completed the failover system that triggers SoR. Key integration points: (1) `OperatorHealthEvent` on NATS `argus.events.operator.health` includes `current_status` and `circuit_breaker_state` — SoR engine should subscribe to this to invalidate cached routing decisions when operator goes down, (2) `HealthChecker.GetCircuitBreaker(opID)` returns per-operator circuit breaker state — SoR should check circuit state before routing, (3) `FailoverEngine.ExecuteAuth` (from STORY-018) already implements fallback_to_next — SoR should provide the "next-best operator" list that failover consumes. No effort change expected.

## Test Scenarios
- [ ] IMSI prefix 234-10 routes to Operator A (priority 1)
- [ ] IMSI prefix with no match → default operator for tenant
- [ ] Two operators available, Operator A is cheaper → SoR selects A
- [ ] Operator A down (circuit open) → SoR selects Operator B (next-best)
- [ ] SIM has manual operator lock → SoR bypassed, locked operator used
- [ ] RAT preference: 4G preferred, operator only supports 3G → select next operator with 4G
- [ ] SoR cache hit → no re-evaluation within TTL
- [ ] Operator cost change → bulk SoR recalculation for affected segment
- [ ] SoR decision recorded in session TBL-17

## Effort Estimate
- Size: L
- Complexity: High
