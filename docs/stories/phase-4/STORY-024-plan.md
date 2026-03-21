# Implementation Plan: STORY-024 - Policy Dry-Run Simulation

## Goal
Implement a POST endpoint that evaluates a policy version against the current SIM fleet in read-only mode, returning affected SIM counts broken down by operator, APN, and RAT type, behavioral changes, and sample before/after SIMs — with async job support for large fleets and Redis caching.

## Architecture Context

### Components Involved
- **SVC-05 Policy Engine** (`internal/policy/dsl/`): DSL parser, compiler, evaluator — provides `CompileSource()`, `EvaluateCompiled()`, `SessionContext`, `PolicyResult`, `CompiledPolicy`
- **SVC-03 Core API** (`internal/api/policy/handler.go`): Policy HTTP handlers — extend with `DryRun` method
- **SVC-01 API Gateway** (`internal/gateway/router.go`): Route registration — add dry-run route
- **SVC-09 Job Runner** (`internal/job/runner.go`): Async job processing — register dry-run processor for large fleets
- **Store Layer** (`internal/store/policy.go`, `internal/store/sim.go`): Data access — add SIM fleet aggregation queries
- **Cache Layer** (`internal/cache/redis.go`): Redis cache for dry-run results (5min TTL)
- **Entry Point** (`cmd/argus/main.go`): Wire dry-run service dependencies

### Data Flow
```
POST /api/v1/policy-versions/:id/dry-run
  → JWT auth + RequireRole("policy_editor")
  → Handler.DryRun()
    → Fetch PolicyVersion by ID (store.PolicyStore.GetVersionByID)
    → Verify policy belongs to tenant (join to policies table)
    → Check Redis cache for existing result
    → If cached → return cached result (200)
    → Compile DSL from version's dsl_content
    → If DSL invalid → 422 with compilation errors
    → Count matching SIMs via store query
    → If >100K SIMs → create async job, return 202 with job_id
    → If <=100K SIMs → evaluate synchronously:
      → Query SIMs matching policy MATCH block (operator, APN, RAT filters)
      → For each SIM: build SessionContext, evaluate policy
      → Aggregate results: by_operator, by_apn, by_rat_type
      → Detect behavioral_changes (compare with existing policy result)
      → Select sample_sims (first 10 with before/after)
      → Cache result in Redis (5min TTL)
      → Store result in policy_versions.dry_run_result
      → Return 200 with result
```

### API Specification

**POST /api/v1/policy-versions/:id/dry-run**

Request body (optional):
```json
{
  "segment_id": "uuid (optional — filter to specific SIM segment)"
}
```

Success response (synchronous, <=100K SIMs):
```json
{
  "status": "success",
  "data": {
    "version_id": "uuid",
    "total_affected": 2300000,
    "by_operator": {
      "Turkcell": 1200000,
      "Vodafone": 800000,
      "Turk Telekom": 300000
    },
    "by_apn": {
      "iot.meter": 1500000,
      "m2m.fleet": 800000
    },
    "by_rat": {
      "4G": 1800000,
      "NB-IoT": 400000,
      "5G": 100000
    },
    "behavioral_changes": [
      {
        "type": "qos_downgrade",
        "description": "bandwidth_down reduced from 10Mbps to 5Mbps",
        "affected_count": 500000,
        "field": "bandwidth_down",
        "old_value": 10000000,
        "new_value": 5000000
      }
    ],
    "sample_sims": [
      {
        "sim_id": "uuid",
        "iccid": "8990...",
        "operator": "Turkcell",
        "apn": "iot.meter",
        "rat_type": "4G",
        "before": {
          "allow": true,
          "qos_attributes": {"bandwidth_down": 10000000},
          "matched_rules": 2
        },
        "after": {
          "allow": true,
          "qos_attributes": {"bandwidth_down": 5000000},
          "matched_rules": 3
        }
      }
    ],
    "evaluated_at": "2026-03-21T10:00:00Z"
  }
}
```

Accepted response (async, >100K SIMs):
```json
{
  "status": "success",
  "data": {
    "job_id": "uuid",
    "message": "Dry-run queued for async processing. Use GET /api/v1/jobs/:id to check status."
  }
}
```

Error responses:
- 404: `{"status":"error","error":{"code":"NOT_FOUND","message":"Policy version not found"}}`
- 422: `{"status":"error","error":{"code":"INVALID_DSL","message":"DSL compilation failed: ..."}}`

Auth: JWT with role `policy_editor` or higher.

### Database Schema

**Source: migrations/20260320000002_core_schema.up.sql (ACTUAL)**

```sql
-- TBL-14: policy_versions
CREATE TABLE IF NOT EXISTS policy_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id UUID NOT NULL REFERENCES policies(id),
    version INTEGER NOT NULL,
    dsl_content TEXT NOT NULL,
    compiled_rules JSONB NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'draft',
    affected_sim_count INTEGER,
    dry_run_result JSONB,
    activated_at TIMESTAMPTZ,
    rolled_back_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

-- TBL-10: sims (partitioned by operator_id)
CREATE TABLE IF NOT EXISTS sims (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,
    apn_id UUID,
    iccid VARCHAR(22) NOT NULL,
    imsi VARCHAR(15) NOT NULL,
    msisdn VARCHAR(20),
    ip_address_id UUID,
    policy_version_id UUID,
    esim_profile_id UUID,
    sim_type VARCHAR(10) NOT NULL DEFAULT 'physical',
    state VARCHAR(20) NOT NULL DEFAULT 'ordered',
    rat_type VARCHAR(10),
    max_concurrent_sessions INTEGER NOT NULL DEFAULT 1,
    session_idle_timeout_sec INTEGER NOT NULL DEFAULT 3600,
    session_hard_timeout_sec INTEGER NOT NULL DEFAULT 86400,
    metadata JSONB NOT NULL DEFAULT '{}',
    ...
    PRIMARY KEY (id, operator_id)
) PARTITION BY LIST (operator_id);

-- TBL-15: policy_assignments
CREATE TABLE IF NOT EXISTS policy_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL,
    policy_version_id UUID NOT NULL REFERENCES policy_versions(id),
    rollout_id UUID,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    coa_sent_at TIMESTAMPTZ,
    coa_status VARCHAR(20) DEFAULT 'pending'
);

-- TBL-13: policies
CREATE TABLE IF NOT EXISTS policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name VARCHAR(100) NOT NULL,
    description TEXT,
    scope VARCHAR(20) NOT NULL,
    scope_ref_id UUID,
    current_version_id UUID,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    ...
);
```

**Key queries for dry-run:**
- Count SIMs matching MATCH block filters: `SELECT COUNT(*) FROM sims WHERE tenant_id = $1 AND state = 'active' [AND operator_id IN (...)] [AND apn_id IN (...)] [AND rat_type IN (...)]`
- SIM fleet aggregation by operator: `SELECT o.name, COUNT(*) FROM sims s JOIN operators o ON s.operator_id = o.id WHERE s.tenant_id = $1 AND s.state = 'active' GROUP BY o.name`
- SIM fleet aggregation by APN: `SELECT a.name, COUNT(*) FROM sims s JOIN apns a ON s.apn_id = a.id WHERE s.tenant_id = $1 AND s.state = 'active' GROUP BY a.name`
- SIM fleet aggregation by RAT type: `SELECT rat_type, COUNT(*) FROM sims WHERE tenant_id = $1 AND s.state = 'active' GROUP BY rat_type`
- Sample SIMs: `SELECT ... FROM sims WHERE tenant_id = $1 AND state = 'active' [filters] LIMIT 10`

### Operators Table (for name lookup)

```sql
-- From migration (TBL-06)
CREATE TABLE IF NOT EXISTS operators (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    ...
);
```

### APNs Table (for name lookup)

```sql
-- From migration (TBL-07)
CREATE TABLE IF NOT EXISTS apns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    name VARCHAR(100) NOT NULL,
    ...
);
```

## Prerequisites
- [x] STORY-022 completed (DSL evaluator: `internal/policy/dsl/evaluator.go`, `CompileSource`, `EvaluateCompiled`, `SessionContext`, `PolicyResult`)
- [x] STORY-023 completed (Policy versioning: `internal/store/policy.go` with `PolicyStore`, `PolicyVersion`, `GetVersionByID`)
- [x] STORY-011 completed (SIM data: `internal/store/sim.go` with `SIMStore`, `SIM` struct)
- [x] STORY-013 completed (Job runner: `internal/job/runner.go`, `internal/store/job.go`)

## Tasks

### Task 1: Dry-Run Service — Core Simulation Logic
- **Files:** Create `internal/policy/dryrun/service.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/policy/dsl/evaluator.go` — follow same package structure with exported types and methods
- **Context refs:** Architecture Context > Data Flow, API Specification, Database Schema
- **What:**
  Create the `dryrun` package under `internal/policy/dryrun/` with:

  1. **Types:**
     - `DryRunRequest` — version ID, tenant ID, optional segment ID
     - `DryRunResult` — total_affected int, by_operator map[string]int, by_apn map[string]int, by_rat map[string]int, behavioral_changes []BehavioralChange, sample_sims []SampleSIM, evaluated_at time.Time
     - `BehavioralChange` — type string (qos_downgrade, qos_upgrade, charging_change, action_added, action_removed), description string, affected_count int, field string, old_value/new_value interface{}
     - `SampleSIM` — sim_id, iccid, operator, apn, rat_type string, before/after *dsl.PolicyResult

  2. **Service struct** with dependencies:
     - `policyStore *store.PolicyStore`
     - `simStore *store.SIMStore`
     - `db *pgxpool.Pool` (for direct aggregation queries)
     - `cache *redis.Client` (for result caching)
     - `logger zerolog.Logger`

  3. **Methods:**
     - `Execute(ctx, req DryRunRequest) (*DryRunResult, error)` — main entry point
       - Fetch policy version by ID, verify tenant via join
       - Check Redis cache key `dryrun:{version_id}:{segment_id_or_all}` (5min TTL)
       - Compile DSL from version's `dsl_content` using `dsl.CompileSource()`
       - Extract MATCH block conditions (operator, APN, RAT type filters) from compiled policy
       - Run aggregation queries against sims table with extracted filters + tenant_id scope + state='active'
       - Evaluate policy against sample SIMs (LIMIT 10), building before/after comparison
       - For "before": if SIM has existing policy_version_id, compile & evaluate that version's DSL
       - Detect behavioral changes by comparing before/after QoS attributes and charging params
       - Cache result in Redis, store in policy_versions.dry_run_result and affected_sim_count
       - Return DryRunResult
     - `CountMatchingSIMs(ctx, tenantID, filters) (int, error)` — count SIMs matching MATCH conditions
     - `buildFiltersFromMatch(compiled *dsl.CompiledPolicy) DryRunFilters` — extract operator/APN/RAT filters from MATCH block
     - `aggregateByOperator/APN/RAT(ctx, tenantID, filters) (map[string]int, error)` — grouped COUNT queries
     - `fetchSampleSIMs(ctx, tenantID, filters, limit int) ([]store.SIM, error)` — get sample SIMs
     - `detectBehavioralChanges(before, after *dsl.PolicyResult) []BehavioralChange` — diff QoS/charging

  4. **DryRunFilters struct** — operatorIDs []uuid.UUID, apnNames []string, ratTypes []string, segmentID *uuid.UUID

  5. Use `pgxpool.Pool` directly for aggregation queries (JOIN operators/apns tables for name resolution) — this is read-only, no transaction needed.

- **Verify:** `go build ./internal/policy/dryrun/...`

### Task 2: Store Layer — SIM Fleet Aggregation Queries
- **Files:** Modify `internal/store/sim.go`, Modify `internal/store/policy.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/sim.go` — follow same query builder pattern with dynamic WHERE conditions and arg index tracking
- **Context refs:** Database Schema, Architecture Context > Data Flow
- **What:**
  Add read-only aggregation methods to support dry-run:

  1. **SIMStore additions:**
     - `CountByFilters(ctx, tenantID uuid.UUID, filters SIMFleetFilters) (int, error)` — count active SIMs matching operator/APN/RAT filters. Uses dynamic WHERE clause builder (same pattern as `List`). Only counts `state = 'active'` SIMs.
     - `AggregateByOperator(ctx, tenantID uuid.UUID, filters SIMFleetFilters) ([]OperatorCount, error)` — `SELECT o.name, COUNT(*) FROM sims s JOIN operators o ON s.operator_id = o.id WHERE s.tenant_id = $1 AND s.state = 'active' [filters] GROUP BY o.id, o.name`
     - `AggregateByAPN(ctx, tenantID uuid.UUID, filters SIMFleetFilters) ([]APNCount, error)` — same pattern with apns table join
     - `AggregateByRATType(ctx, tenantID uuid.UUID, filters SIMFleetFilters) ([]RATCount, error)` — `GROUP BY rat_type`
     - `FetchSample(ctx, tenantID uuid.UUID, filters SIMFleetFilters, limit int) ([]SIM, error)` — get limited sample SIMs

  2. **SIMFleetFilters struct:** OperatorIDs []uuid.UUID, APNIDs []uuid.UUID, RATTypes []string, SegmentID *uuid.UUID

  3. **OperatorCount, APNCount, RATCount structs:** Name string, Count int

  4. **PolicyStore additions:**
     - `UpdateDryRunResult(ctx, versionID uuid.UUID, result json.RawMessage, affectedCount int) error` — update `policy_versions.dry_run_result` and `affected_sim_count` columns
     - `GetVersionWithTenant(ctx, versionID, tenantID uuid.UUID) (*PolicyVersion, error)` — get version with tenant_id check via JOIN to policies table

- **Verify:** `go build ./internal/store/...`

### Task 3: API Handler — DryRun Endpoint
- **Files:** Modify `internal/api/policy/handler.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/policy/handler.go` — follow same handler pattern: parse URL param, validate, call service, return envelope response
- **Context refs:** API Specification, Architecture Context > Data Flow
- **What:**
  Add `DryRun` method to the existing policy `Handler`:

  1. Modify `Handler` struct to accept dry-run service dependency (add `dryRunSvc *dryrun.Service` field)
  2. Modify `NewHandler` to accept the dry-run service
  3. Add `DryRun(w http.ResponseWriter, r *http.Request)` method:
     - Extract `tenantID` from context (same pattern as other handlers)
     - Parse version ID from URL param `chi.URLParam(r, "id")`
     - Parse optional request body `{segment_id?}`
     - Call `dryRunSvc.CountMatchingSIMs()` to check fleet size
     - If >100K: create async job via jobStore, publish to NATS job queue, return 202
     - If <=100K: call `dryRunSvc.Execute()`, return 200 with result
     - Handle errors: 404 for version not found, 422 for invalid DSL
  4. Add `dryRunResponse` type matching the API spec
  5. Add `dryRunAsyncResponse` type for 202 response

  Also modify `NewHandler` signature — add `dryRunSvc` parameter. The handler now holds both `policyStore` and `dryRunSvc`.

  For async job creation: use `jobStore.Create()` with type `"policy_dry_run"`, payload containing version_id and segment_id.

- **Verify:** `go build ./internal/api/policy/...`

### Task 4: Job Processor — Async Dry-Run for Large Fleets
- **Files:** Create `internal/job/dryrun.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/import.go` — follow same Processor interface pattern with `Type() string` and `Process(ctx, job) error`
- **Context refs:** Architecture Context > Data Flow, API Specification
- **What:**
  Create dry-run job processor implementing `job.Processor`:

  1. `DryRunProcessor` struct with dependencies: dryRunSvc, jobStore, eventBus, logger
  2. `Type() string` → returns `"policy_dry_run"`
  3. `Process(ctx, job *store.Job) error`:
     - Unmarshal job.Payload to get version_id and segment_id
     - Call `dryRunSvc.Execute()` with extracted params and job.TenantID
     - Marshal result as JSON
     - Call `jobStore.Complete()` with result
     - Publish job completion event via eventBus
  4. Constructor `NewDryRunProcessor(...)` following same pattern as `NewBulkImportProcessor`

- **Verify:** `go build ./internal/job/...`

### Task 5: Router + Main Wiring
- **Files:** Modify `internal/gateway/router.go`, Modify `cmd/argus/main.go`
- **Depends on:** Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — follow same route group pattern with JWT auth + RequireRole
- **Context refs:** Architecture Context > Components Involved
- **What:**
  1. **Router** (`internal/gateway/router.go`):
     - Add route in the PolicyHandler group: `r.Post("/api/v1/policy-versions/{id}/dry-run", deps.PolicyHandler.DryRun)`
     - Place it in the same `r.Group` block as other policy_editor routes

  2. **Main** (`cmd/argus/main.go`):
     - Import `dryrun` package
     - Create `dryrun.NewService(policyStore, simStore, pg.Pool, rdb.Client, log.Logger)` after existing store setup
     - Update `policyapi.NewHandler(...)` call to pass dryRunSvc
     - Create `job.NewDryRunProcessor(dryRunSvc, jobStore, eventBus, log.Logger)`
     - Register dry-run processor: `jobRunner.Register(dryRunProcessor)`

- **Verify:** `go build ./cmd/argus/...`

### Task 6: Tests — Dry-Run Service + Handler
- **Files:** Create `internal/policy/dryrun/service_test.go`, Modify `internal/api/policy/handler_test.go`
- **Depends on:** Task 1, Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/policy/dsl/evaluator_test.go` — follow same test structure with table-driven tests
- **Context refs:** API Specification, Acceptance Criteria Mapping
- **What:**
  1. **Service tests** (`internal/policy/dryrun/service_test.go`):
     - Test `buildFiltersFromMatch()` — extract operator/APN/RAT from compiled MATCH block
     - Test `detectBehavioralChanges()` — QoS downgrade, upgrade, charging change, no changes
     - Test `DryRunResult` JSON serialization

  2. **Handler tests** (`internal/api/policy/handler_test.go`):
     - Test DryRun handler with valid version ID → 200
     - Test DryRun with non-existent version → 404
     - Test DryRun with invalid DSL version → 422
     - Test DryRun result includes all required fields

  Tests focus on unit-testable logic (filter extraction, change detection, JSON shape). Integration tests requiring DB/Redis are deferred to gate phase.

- **Verify:** `go test ./internal/policy/dryrun/... ./internal/api/policy/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| POST /api/v1/policy-versions/:id/dry-run evaluates version against SIM fleet | Task 3 (handler), Task 1 (service) | Task 6 |
| Dry-run scoped to policy's MATCH block (operator, APN, RAT filters) | Task 1 (buildFiltersFromMatch) | Task 6 |
| Response includes total_affected_sims, by_operator, by_apn, by_rat_type | Task 1 (DryRunResult), Task 2 (aggregation queries) | Task 6 |
| Response includes behavioral_changes | Task 1 (detectBehavioralChanges) | Task 6 |
| Response includes sample_sims with before/after | Task 1 (fetchSampleSIMs + evaluation) | Task 6 |
| Dry-run read-only (no DB writes except caching) | Task 1 (no mutations), Task 2 (SELECT only) | Task 6 |
| Large fleets >100K → async 202 with job_id | Task 3 (handler threshold check), Task 4 (job processor) | Task 6 |
| Small fleets <100K → sync 200 | Task 3 (handler) | Task 6 |
| Cached for 5 minutes | Task 1 (Redis SET with 5min TTL) | Task 6 |
| Invalid DSL → 422 with compilation errors | Task 1, Task 3 | Task 6 |

## Story-Specific Compliance Rules

- **API:** Standard envelope `{status, data, meta?}` for all responses. 202 for async.
- **DB:** All queries scoped by `tenant_id`. Read-only queries only (no writes to sims/policy_assignments).
- **Auth:** JWT with `policy_editor` role or higher (matches RBAC matrix for "Manage policies").
- **Cache:** Redis key pattern `dryrun:{version_id}:{segment_id_or_all}`, TTL 5 minutes.
- **Business:** Policy evaluation follows BR-4 order: SIM-specific > APN-level > operator-level > tenant default.
- **Audit:** Dry-run is read-only, no audit entry needed (per BR-7: only state-changing operations).

## Risks & Mitigations

- **Performance on large SIM fleets:** Aggregation queries with JOINs on 10M+ rows could be slow. Mitigation: use COUNT with indexes (idx_sims_tenant_state, idx_sims_tenant_operator, idx_sims_tenant_apn), limit sample to 10 SIMs, async fallback for >100K.
- **DSL MATCH block filter extraction:** CompiledPolicy.Match.Conditions may not always map directly to SQL WHERE clauses (e.g., metadata.* fields). Mitigation: only extract operator/apn/rat_type conditions, fall back to full table scan for exotic MATCH conditions.
- **Cache invalidation:** SIM fleet changes should invalidate cached dry-run results. Mitigation: 5-minute TTL is short enough; explicit invalidation deferred to future work.
