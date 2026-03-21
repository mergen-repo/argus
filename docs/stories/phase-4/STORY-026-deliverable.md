# Deliverable: STORY-026 — Steering of Roaming (SoR) Engine

## Summary

Implemented Steering of Roaming engine within SVC-06 (Operator Router). SoR selects the best operator for a SIM based on IMSI-prefix routing, RAT-type preference, cost optimization, and circuit breaker state. Results cached in Redis with configurable TTL (default 1h).

## What Was Built

### SoR Engine (internal/operator/sor/engine.go)
- `Evaluate(ctx, request) → SoRDecision`: Main evaluation function
- IMSI-prefix matching (MCC+MNC string prefix)
- RAT-type filtering: only operators supporting requested RAT
- RAT-type preference ranking: configurable order (5G > 4G > 3G > 2G > NB-IoT > LTE-M)
- Cost-based selection: sort by priority ASC, RAT rank, cost_per_mb ASC
- Circuit breaker check: skip operators with open circuit
- Manual operator lock bypass via SIM metadata.operator_lock
- Interfaces: GrantProvider, CircuitBreakerChecker for clean DI

### SoR Types (internal/operator/sor/types.go)
- SoRDecision: selected operator, reason, candidates evaluated, cache hit
- SoRRequest: tenant_id, sim_id, imsi, requested_rat, operator_lock
- SoRConfig: TTL, RAT preference order
- CandidateOperator: operator with grant priority, supported RATs, cost

### SoR Cache (internal/operator/sor/cache.go)
- Redis GET/SET/DELETE for per-SIM SoR decisions
- SCAN with COUNT 100 for bulk cache invalidation
- Configurable TTL (default 1h)

### NATS Subscriber (internal/operator/sor/subscriber.go)
- Subscribes to operator.health events
- Invalidates cached SoR decisions when operator goes down
- Automatic re-evaluation on next auth request (lazy)

### DB Migration
- `sor_fields.up.sql`: Adds priority, cost_per_mb, supported_rat_types to operator_grants; sor_decision JSONB to sessions
- `sor_fields.down.sql`: Reverse migration

### Store Extensions
- OperatorGrant SoR fields (priority, cost_per_mb, supported_rat_types)
- GrantWithOperator type for JOIN query
- ListGrantsWithOperators, UpdateGrant
- SoRDecision field on RadiusSession

### Tests
- 16 test functions (21 with subtests) covering all 9 story test scenarios
- Full suite passing, zero regressions

## Architecture References Fulfilled
- SVC-06: Operator Router / SoR engine
- TBL-05/06: operator grants with SoR priority and cost
- TBL-17: session sor_decision field

## Files Changed
```
internal/operator/sor/types.go       (new)
internal/operator/sor/engine.go      (new)
internal/operator/sor/cache.go       (new)
internal/operator/sor/subscriber.go  (new)
internal/operator/sor/engine_test.go (new)
internal/store/operator.go           (modified)
internal/store/session_radius.go     (modified)
migrations/20260321000001_sor_fields.up.sql   (new)
migrations/20260321000001_sor_fields.down.sql (new)
```

## Dependencies Unblocked
- STORY-027 (RAT-Type Awareness) — can use SoR data for RAT-aware routing
