# Implementation Plan: STORY-026 - Steering of Roaming (SoR) Engine

## Goal
Implement a Steering of Roaming (SoR) engine within SVC-06 (Operator Router) that selects the best operator for each SIM based on IMSI prefix routing, RAT-type preference, cost optimization, circuit breaker state, and manual overrides.

## Architecture Context

### Components Involved
- **SVC-06 Operator Router** (`internal/operator/`): Existing operator routing, circuit breaker, failover engine. SoR engine lives at `internal/operator/sor/` as a new sub-package.
- **Store Layer** (`internal/store/operator.go`): Existing `OperatorStore` with `Operator` struct containing `MCC`, `MNC`, `SupportedRATTypes`, `HealthStatus`, `FailoverPolicy`. `OperatorGrant` struct with `TenantID`, `OperatorID`, `Enabled`.
- **Store Layer** (`internal/store/sim.go`): `SIM` struct with `IMSI`, `OperatorID`, `TenantID`, `RATType`, `Metadata`.
- **Store Layer** (`internal/store/session_radius.go`): `RadiusSession` struct for session records in TBL-17.
- **Cache Layer** (`internal/cache/redis.go`): `Redis` wrapper exposing `Client *redis.Client`.
- **Bus Layer** (`internal/bus/nats.go`): `EventBus` with `Publish`, `Subscribe`, `QueueSubscribe`. Subject `SubjectOperatorHealthChanged = "argus.events.operator.health"`.
- **Operator Health** (`internal/operator/health.go`): `HealthChecker` with `GetCircuitBreaker(opID)` returning `*CircuitBreaker`.
- **Circuit Breaker** (`internal/operator/circuit_breaker.go`): `CircuitBreaker` with `State()`, `ShouldAllow()`.
- **Failover** (`internal/operator/failover.go`): `FailoverEngine` with `ExecuteAuth` consuming fallback operator lists.

### Data Flow
```
AAA Auth Request (RADIUS/Diameter/5G)
    → SoR Engine invoked with IMSI + tenant_id + requested RAT
    → Check SIM metadata for manual operator lock → if locked, return locked operator
    → Check Redis cache for cached SoR result → if hit, return cached result
    → Load operator grants for tenant (enabled only)
    → Filter by IMSI prefix match (MCC+MNC prefix routing)
    → Filter by RAT type support (operator must support requested RAT)
    → Filter by circuit breaker state (exclude operators with open circuit)
    → Sort by: (1) SoR priority on grant, (2) RAT preference order, (3) cost_per_mb ascending
    → Cache result in Redis (TTL configurable, default 1h)
    → Log SoR decision in session record
    → Return ranked operator list (primary + fallbacks)
```

### Database Schema

#### TBL-05: operators (ACTUAL — from migrations/20260320000002_core_schema.up.sql)
```sql
CREATE TABLE IF NOT EXISTS operators (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE,
    code VARCHAR(20) NOT NULL UNIQUE,
    mcc VARCHAR(3) NOT NULL,
    mnc VARCHAR(3) NOT NULL,
    adapter_type VARCHAR(30) NOT NULL,
    adapter_config JSONB NOT NULL DEFAULT '{}',
    sm_dp_plus_url VARCHAR(500),
    sm_dp_plus_config JSONB DEFAULT '{}',
    supported_rat_types VARCHAR[] NOT NULL DEFAULT '{}',
    health_status VARCHAR(20) NOT NULL DEFAULT 'unknown',
    health_check_interval_sec INTEGER NOT NULL DEFAULT 30,
    failover_policy VARCHAR(20) NOT NULL DEFAULT 'reject',
    failover_timeout_ms INTEGER NOT NULL DEFAULT 5000,
    circuit_breaker_threshold INTEGER NOT NULL DEFAULT 5,
    circuit_breaker_recovery_sec INTEGER NOT NULL DEFAULT 60,
    sla_uptime_target DECIMAL(5,2) DEFAULT 99.90,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

#### TBL-06: operator_grants (ACTUAL — from migrations/20260320000002_core_schema.up.sql)
```sql
CREATE TABLE IF NOT EXISTS operator_grants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID NOT NULL REFERENCES operators(id),
    enabled BOOLEAN NOT NULL DEFAULT true,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    granted_by UUID REFERENCES users(id)
);
```
**Note:** The story requires `sor_priority` and `cost_per_mb` fields on operator_grants. These do NOT exist yet — migration needed.

#### TBL-10: sims (ACTUAL — from migrations/20260320000002_core_schema.up.sql)
```sql
CREATE TABLE IF NOT EXISTS sims (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,
    apn_id UUID,
    iccid VARCHAR(22) NOT NULL,
    imsi VARCHAR(15) NOT NULL,
    msisdn VARCHAR(20),
    -- ... other columns ...
    rat_type VARCHAR(10),
    metadata JSONB NOT NULL DEFAULT '{}',
    -- ... timestamps ...
    PRIMARY KEY (id, operator_id)
) PARTITION BY LIST (operator_id);
```
**Note:** `metadata` JSONB can store `operator_lock` field for manual SoR override. No schema change needed.

#### TBL-17: sessions (ACTUAL — from migrations/20260320000002_core_schema.up.sql)
```sql
CREATE TABLE IF NOT EXISTS sessions (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,
    -- ... other columns ...
    session_state VARCHAR(20) NOT NULL DEFAULT 'active',
    -- ... more columns ...
);
```
**Note:** The story requires `sor_decision` JSONB field on sessions. Does NOT exist yet — migration needed.

### Migration Required (New)
```sql
-- Add SoR fields to operator_grants
ALTER TABLE operator_grants ADD COLUMN IF NOT EXISTS sor_priority INTEGER NOT NULL DEFAULT 100;
ALTER TABLE operator_grants ADD COLUMN IF NOT EXISTS cost_per_mb DECIMAL(8,4) DEFAULT 0.0;
ALTER TABLE operator_grants ADD COLUMN IF NOT EXISTS region VARCHAR(50);

-- Add SoR decision field to sessions
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS sor_decision JSONB;

-- Index for SoR queries
CREATE INDEX IF NOT EXISTS idx_operator_grants_tenant_sor ON operator_grants (tenant_id, sor_priority) WHERE enabled = true;
```

### Redis Cache Keys
- `sor:result:{tenant_id}:{imsi}` — Cached SoR decision (TTL: configurable, default 1h)
- `sor:prefix:{tenant_id}` — IMSI prefix → operator mapping table (TTL: 1h, invalidated on operator change)

### NATS Integration
- **Subscribe to:** `argus.events.operator.health` — invalidate SoR cache when operator health changes
- **Subscribe to:** `argus.cache.invalidate` — handle bulk SoR cache invalidation

### SoR Decision Structure
```json
{
  "primary_operator_id": "uuid",
  "fallback_operator_ids": ["uuid", "uuid"],
  "reason": "imsi_prefix_match|cost_optimized|rat_preference|manual_lock",
  "imsi_prefix": "234-10",
  "rat_type": "4G",
  "cost_per_mb": 0.05,
  "evaluated_at": "2026-03-21T10:00:00Z",
  "cached": false
}
```

### RAT Type Preference Order (configurable)
Default: `5G > 4G > 3G > 2G > NB-IoT > LTE-M`

### Business Rules
- SoR is invoked during: (1) initial auth — choose best operator, (2) failover — choose next-best
- Per-SIM operator lock (manual assignment) stored in `sims.metadata.operator_lock` bypasses SoR entirely
- When multiple operators match IMSI prefix + RAT + circuit state: sort by sor_priority ASC, then cost_per_mb ASC
- SoR result cached per SIM in Redis with configurable TTL (default 1h)
- NATS `operator.health` events invalidate cached SoR results for affected operator
- Bulk re-evaluation: when operator costs change, invalidate all SoR cache entries for that tenant

## Prerequisites
- [x] STORY-009 completed (operator CRUD — OperatorStore, Operator model)
- [x] STORY-018 completed (operator adapter framework, OperatorRouter, FailoverEngine)
- [x] STORY-021 completed (HealthChecker, CircuitBreaker, OperatorHealthEvent on NATS)

## Tasks

### Task 1: Database migration — Add SoR columns to operator_grants and sessions
- **Files:** Create `migrations/20260321000001_sor_fields.up.sql`, Create `migrations/20260321000001_sor_fields.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260320000006_session_protocol_type.up.sql` — follow same ALTER TABLE pattern
- **Context refs:** [Database Schema, Migration Required (New)]
- **What:** Create migration that adds `sor_priority` (INTEGER NOT NULL DEFAULT 100), `cost_per_mb` (DECIMAL(8,4) DEFAULT 0.0), and `region` (VARCHAR(50)) columns to `operator_grants` table. Also adds `sor_decision` (JSONB) column to `sessions` table. Create index `idx_operator_grants_tenant_sor` on `(tenant_id, sor_priority) WHERE enabled = true`. Down migration drops the added columns and index.
- **Verify:** `cat migrations/20260321000001_sor_fields.up.sql` shows correct ALTER TABLE statements

### Task 2: Update store layer — Extend OperatorGrant struct and add SoR query methods
- **Files:** Modify `internal/store/operator.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/operator.go` — follow existing OperatorGrant struct and query patterns
- **Context refs:** [Database Schema, Migration Required (New), Data Flow]
- **What:**
  1. Add `SoRPriority int`, `CostPerMB *float64`, `Region *string` fields to the `OperatorGrant` struct
  2. Update `CreateGrant` to accept and insert `sor_priority`, `cost_per_mb`, `region`
  3. Update `ListGrants` and `GetGrantByID` scan to include new columns
  4. Add new method `ListGrantsWithOperators(ctx, tenantID) ([]GrantWithOperator, error)` that JOINs operator_grants with operators to return both grant fields (sor_priority, cost_per_mb, region) and operator fields (mcc, mnc, supported_rat_types, health_status) in a single query — this is the main query SoR engine uses
  5. Add `UpdateGrant(ctx, grantID, UpdateGrantParams)` method for updating sor_priority, cost_per_mb, region
- **Verify:** `go build ./internal/store/...`

### Task 3: SoR engine core — types, IMSI prefix matching, operator selection algorithm
- **Files:** Create `internal/operator/sor/engine.go`, Create `internal/operator/sor/types.go`
- **Depends on:** Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/operator/failover.go` — follow same engine pattern (struct with dependencies, methods)
- **Context refs:** [Architecture Context, Data Flow, SoR Decision Structure, RAT Type Preference Order, Business Rules]
- **What:**
  **types.go:**
  - `SoRDecision` struct: PrimaryOperatorID, FallbackOperatorIDs, Reason, IMSIPrefix, RATType, CostPerMB, EvaluatedAt, Cached
  - `SoRRequest` struct: IMSI, TenantID, RequestedRAT, SimID, SimMetadata (json.RawMessage)
  - `SoRConfig` struct: CacheTTL (time.Duration, default 1h), RATPreferenceOrder ([]string), DefaultRATOrder
  - `CandidateOperator` struct: OperatorID, MCC, MNC, SupportedRATs, SoRPriority, CostPerMB, HealthStatus, CircuitState

  **engine.go:**
  - `Engine` struct with dependencies: store (*store.OperatorStore), redisClient (*redis.Client), healthChecker (interface for GetCircuitBreaker), logger, config
  - `NewEngine(...)` constructor
  - `Evaluate(ctx, req SoRRequest) (*SoRDecision, error)` — main entry point:
    1. Check sim metadata for `operator_lock` → if present, return locked operator as primary with reason "manual_lock"
    2. Check Redis cache `sor:result:{tenant_id}:{imsi}` → if hit, return cached decision
    3. Call store to get grants with operators for tenant
    4. Filter candidates: enabled grants, operator state=active, circuit breaker allows
    5. Match IMSI prefix (MCC+MNC from IMSI first 5-6 digits against operator MCC+MNC)
    6. Filter by RAT type (operator must support requested RAT)
    7. Sort: sor_priority ASC, then RAT preference order, then cost_per_mb ASC
    8. Build SoRDecision with primary (first) and fallbacks (rest)
    9. Cache result in Redis
    10. Return decision
  - `matchIMSIPrefix(imsi string, mcc, mnc string) bool` — checks if IMSI starts with MCC+MNC
  - `filterByRAT(candidates, requestedRAT) []CandidateOperator`
  - `sortCandidates(candidates, ratPreferenceOrder) []CandidateOperator`
  - `InvalidateCache(ctx, tenantID, imsi)` — delete specific cached result
  - `InvalidateTenantCache(ctx, tenantID)` — delete all cached results for tenant (uses SCAN+DEL pattern)
  - `BulkRecalculate(ctx, tenantID uuid.UUID) error` — invalidate all cached SoR for tenant, triggering re-evaluation on next auth
- **Verify:** `go build ./internal/operator/sor/...`

### Task 4: SoR cache layer — Redis caching for SoR results
- **Files:** Create `internal/operator/sor/cache.go`
- **Depends on:** Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/operator/health.go` lines 187-201 — follow same Redis SET/GET with JSON marshal pattern
- **Context refs:** [Redis Cache Keys, SoR Decision Structure, Business Rules]
- **What:**
  - `SoRCache` struct wrapping `*redis.Client` with configurable TTL
  - `NewSoRCache(client *redis.Client, defaultTTL time.Duration) *SoRCache`
  - `Get(ctx, tenantID, imsi) (*SoRDecision, error)` — fetch from Redis key `sor:result:{tenant_id}:{imsi}`, JSON unmarshal. Returns nil,nil on cache miss.
  - `Set(ctx, tenantID, imsi, decision *SoRDecision, ttl time.Duration) error` — JSON marshal, SET with TTL
  - `Delete(ctx, tenantID, imsi) error` — DEL specific key
  - `DeleteByOperator(ctx, tenantID, operatorID) error` — SCAN for `sor:result:{tenant_id}:*`, check decision JSON for operatorID, DEL matching keys. Use SCAN with COUNT 100 to avoid blocking Redis.
  - `DeleteAllForTenant(ctx, tenantID) error` — SCAN+DEL pattern for `sor:result:{tenant_id}:*`
  - `cacheKey(tenantID, imsi) string` — helper to build Redis key
- **Verify:** `go build ./internal/operator/sor/...`

### Task 5: NATS event subscriber — Cache invalidation on operator health changes
- **Files:** Create `internal/operator/sor/subscriber.go`
- **Depends on:** Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/bus/nats.go` lines 152-162 — follow EventBus.Subscribe/QueueSubscribe pattern
- **Context refs:** [NATS Integration, Architecture Context > Components Involved]
- **What:**
  - `SoRSubscriber` struct with dependencies: engine (*Engine), cache (*SoRCache), logger
  - `NewSoRSubscriber(engine, cache, logger) *SoRSubscriber`
  - `SubscribeHealthEvents(eventBus *bus.EventBus) error` — subscribe to `argus.events.operator.health` with queue group "sor_invalidation". On receiving `OperatorHealthEvent`:
    1. Parse event payload (JSON unmarshal to `operator.OperatorHealthEvent`)
    2. If `CurrentStatus` is "down" (circuit open), invalidate all cached SoR decisions that reference this operator via `cache.DeleteByOperator`
    3. If status changed from "down" to "healthy"/"degraded", also invalidate to allow re-evaluation with recovered operator
    4. Log invalidation action
  - `SubscribeCacheInvalidation(eventBus *bus.EventBus) error` — subscribe to `argus.cache.invalidate` for SoR-specific invalidation requests
- **Verify:** `go build ./internal/operator/sor/...`

### Task 6: SoR engine unit tests
- **Files:** Create `internal/operator/sor/engine_test.go`
- **Depends on:** Task 3, Task 4
- **Complexity:** high
- **Pattern ref:** Read `internal/operator/failover_test.go` — follow same test structure with mock adapters
- **Context refs:** [Architecture Context, Data Flow, SoR Decision Structure, RAT Type Preference Order, Business Rules, Test Scenarios from Story]
- **What:** Comprehensive tests covering ALL story test scenarios:
  1. `TestSoR_IMSIPrefixRouting` — IMSI prefix 234-10 routes to Operator A (priority 1)
  2. `TestSoR_IMSIPrefixNoMatch` — IMSI with no prefix match → falls back to default (lowest priority operator)
  3. `TestSoR_CostBasedSelection` — Two operators available, Operator A cheaper → SoR selects A
  4. `TestSoR_CircuitBreakerOpen` — Operator A down (circuit open) → SoR selects Operator B (next-best)
  5. `TestSoR_ManualOperatorLock` — SIM has manual operator lock in metadata → SoR bypassed
  6. `TestSoR_RATPreference` — 4G preferred, operator only supports 3G → select next operator with 4G
  7. `TestSoR_CacheHit` — SoR cache hit → no re-evaluation, cached decision returned
  8. `TestSoR_CacheInvalidation` — Operator health change → cache invalidated
  9. `TestSoR_SortByPriorityThenCost` — Multiple operators, verify sort order: priority ASC, then cost ASC
  10. `TestSoR_NoAvailableOperators` — All operators down or unsupported → returns error

  Use mock implementations: mock store (in-memory operators/grants), mock redis (miniredis or interface mock), mock circuit breaker checker.
- **Verify:** `go test ./internal/operator/sor/... -v`

### Task 7: Integration with session store — Record SoR decision in session
- **Files:** Modify `internal/store/session_radius.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/session_radius.go` — follow existing CreateRadiusSessionParams and scan pattern
- **Context refs:** [Database Schema, Migration Required (New), SoR Decision Structure]
- **What:**
  1. Add `SoRDecision json.RawMessage` field to `RadiusSession` struct
  2. Add `SoRDecision json.RawMessage` field to `CreateRadiusSessionParams` struct
  3. Update the CREATE query in session creation to include `sor_decision` column
  4. Update all scan calls for RadiusSession to include `sor_decision` field
  5. Ensure nil/null handling: `sor_decision` is optional (can be NULL in DB)
- **Verify:** `go build ./internal/store/...`

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| IMSI-prefix routing | Task 3 (matchIMSIPrefix) | Task 6, Test 1 |
| SoR routing table in Redis | Task 4 (SoRCache) | Task 6, Test 7 |
| RAT-type preference | Task 3 (filterByRAT, sortCandidates) | Task 6, Test 6 |
| RAT-type awareness | Task 3 (filterByRAT) | Task 6, Test 6 |
| Cost-based selection | Task 3 (sortCandidates) | Task 6, Test 3 |
| SoR priority on grants | Task 1 (migration), Task 2 (store) | Task 6, Test 1 |
| SoR on auth + failover | Task 3 (Evaluate) | Task 6, Tests 1-4 |
| SoR cache with TTL | Task 4 (SoRCache.Set) | Task 6, Test 7 |
| SoR override (operator lock) | Task 3 (Evaluate) | Task 6, Test 5 |
| SoR decision in session | Task 7 (session_radius.go) | Task 6, Test 9 |
| Bulk re-evaluation | Task 3 (BulkRecalculate), Task 5 (subscriber) | Task 6, Test 8 |

## Story-Specific Compliance Rules
- DB: Migration script with up+down for operator_grants SoR columns and sessions sor_decision
- DB: All grant queries scoped by tenant_id (enforced in store layer)
- Cache: Redis keys prefixed with `sor:` namespace, TTL always set (never infinite)
- Performance: IMSI prefix matching must be O(1) via MCC+MNC string prefix comparison
- Events: NATS subscription with queue group to allow horizontal scaling
- Business: Manual operator lock always takes precedence over SoR algorithm

## Risks & Mitigations
- **Risk:** SCAN command for cache invalidation could be slow with many keys → **Mitigation:** Use SCAN with COUNT 100, do in background goroutine, set upper limit on iterations
- **Risk:** SoR cache stale when operator costs change → **Mitigation:** BulkRecalculate invalidates all tenant cache entries; NATS health events auto-invalidate
- **Risk:** IMSI prefix collision (operator MCC+MNC partially overlaps) → **Mitigation:** Sort by specificity (longer prefix first), then by priority
