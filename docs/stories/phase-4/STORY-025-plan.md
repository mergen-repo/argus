# Implementation Plan: STORY-025 - Staged Policy Rollout

## Goal
Implement staged (canary) policy rollout: start at 1% of affected SIMs, advance through 10% then 100% with explicit user action, support rollback at any stage, track progress via TBL-16, send CoA for active sessions, and push progress via NATS/WebSocket.

## Architecture Context

### Components Involved
- **SVC-05 (Policy Engine)** — `internal/policy/rollout/` — new package for rollout service logic
- **SVC-03 (Core API)** — `internal/api/policy/handler.go` — extend existing handler with rollout endpoints
- **SVC-04 (AAA)** — `internal/aaa/session/coa.go` — existing CoASender for sending Change of Authorization
- **SVC-02 (WebSocket)** — `internal/ws/` — receives NATS events and pushes to portal clients
- **SVC-09 (Job Runner)** — `internal/job/` — background processor for large rollout stages (>100K SIMs)
- **Store layer** — `internal/store/policy.go` — extend with rollout CRUD operations
- **Event bus** — `internal/bus/nats.go` — publish `policy.rollout_progress` events
- **Gateway** — `internal/gateway/router.go` — register new routes

### Data Flow (FLW-03: Policy Staged Rollout)
```
Policy Editor → POST /api/v1/policy-versions/:id/rollout (API-096)
    → SVC-01 (Gateway): auth + RBAC (policy_editor+)
    → SVC-03 (Core API): validate version state = 'draft' (activatable)
    → SVC-05 (Policy Engine):
        → Set version state to 'rolling_out'
        → Create rollout record → TBL-16 (policy_rollouts)
        → Calculate affected SIMs (reuse dry-run affected_sim_count or CountByFilters)
        → Stage 1 (1%): select random 1% of affected SIMs
            → For each SIM: INSERT/UPDATE TBL-15 (policy_assignments)
            → UPDATE sims SET policy_version_id = new version
            → NATS: publish "argus.coa.send" per active session → SVC-04 sends CoA
            → Update TBL-16: migrated_sims count
        → NATS: publish "policy.rollout_progress"
            → SVC-02 (WS): push progress to portal
    → Response: rollout status with progress

Policy Editor → POST /api/v1/policy-rollouts/:id/advance (API-097)
    → SVC-05: advance to next stage (10% or 100%), repeat above for next batch

Policy Editor → POST /api/v1/policy-rollouts/:id/rollback (API-098)
    → SVC-05: revert ALL migrated SIMs to previous version
        → Mass update TBL-15 + sims table back to previous_version_id
        → Mass CoA via NATS → SVC-04
        → Update TBL-16: state = 'rolled_back'
        → Update policy_versions: state = 'rolled_back', rolled_back_at = NOW()
```

### API Specifications

#### API-095: POST /api/v1/policy-versions/:id/activate
Already implemented in `internal/api/policy/handler.go` (ActivateVersion method). This story does NOT modify it — it remains the "immediate 100%" activation path. The rollout path (API-096) is a separate endpoint.

#### API-096: POST /api/v1/policy-versions/:id/rollout
- **Request body:** `{ "stages": [1, 10, 100] }` (optional — defaults to `[1, 10, 100]`)
- **Success response (201):**
```json
{
  "status": "success",
  "data": {
    "rollout_id": "uuid",
    "version_id": "uuid",
    "policy_id": "uuid",
    "stages": [{"pct": 1, "status": "in_progress"}, {"pct": 10, "status": "pending"}, {"pct": 100, "status": "pending"}],
    "current_stage": 0,
    "total_sims": 234560,
    "migrated_sims": 0,
    "state": "in_progress",
    "started_at": "2026-03-21T..."
  }
}
```
- **Error responses:**
  - 404: Version not found
  - 422 `VERSION_NOT_DRAFT`: Version must be in draft state
  - 422 `ROLLOUT_IN_PROGRESS`: Another rollout is already in progress for this policy

#### API-097: POST /api/v1/policy-rollouts/:id/advance
- **Request body:** none
- **Success response (200):**
```json
{
  "status": "success",
  "data": {
    "rollout_id": "uuid",
    "current_stage_pct": 10,
    "migrated_count": 23456,
    "total_count": 234560,
    "state": "in_progress"
  }
}
```
- **Error responses:**
  - 404: Rollout not found
  - 422 `ROLLOUT_COMPLETED`: Rollout already completed
  - 422 `ROLLOUT_ROLLED_BACK`: Rollout was rolled back
  - 422 `STAGE_IN_PROGRESS`: Current stage is still processing (for async large batches)

#### API-098: POST /api/v1/policy-rollouts/:id/rollback
- **Request body:** `{ "reason": "optional reason" }` (optional)
- **Success response (200):**
```json
{
  "status": "success",
  "data": {
    "rollout_id": "uuid",
    "state": "rolled_back",
    "reverted_count": 23456,
    "rolled_back_at": "2026-03-21T..."
  }
}
```
- **Error responses:**
  - 404: Rollout not found
  - 422 `ROLLOUT_COMPLETED`: Cannot rollback a completed rollout
  - 422 `ROLLOUT_ROLLED_BACK`: Already rolled back

#### API-099: GET /api/v1/policy-rollouts/:id
- **Success response (200):**
```json
{
  "status": "success",
  "data": {
    "rollout_id": "uuid",
    "policy_id": "uuid",
    "version_id": "uuid",
    "previous_version_id": "uuid",
    "stages": [
      {"pct": 1, "status": "completed", "sim_count": 2346},
      {"pct": 10, "status": "in_progress", "sim_count": 23456, "migrated": 15000},
      {"pct": 100, "status": "pending"}
    ],
    "current_stage": 1,
    "total_sims": 234560,
    "migrated_sims": 17346,
    "errors": [],
    "state": "in_progress",
    "started_at": "2026-03-21T...",
    "created_at": "2026-03-21T..."
  }
}
```
- **Error responses:**
  - 404: Rollout not found

### Database Schema

Source: `migrations/20260320000002_core_schema.up.sql` (ACTUAL — tables already exist)

#### TBL-15: policy_assignments
```sql
CREATE TABLE IF NOT EXISTS policy_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL,
    policy_version_id UUID NOT NULL REFERENCES policy_versions(id),
    rollout_id UUID,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    coa_sent_at TIMESTAMPTZ,
    coa_status VARCHAR(20) DEFAULT 'pending'
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_policy_assignments_sim ON policy_assignments (sim_id);
CREATE INDEX IF NOT EXISTS idx_policy_assignments_version ON policy_assignments (policy_version_id);
CREATE INDEX IF NOT EXISTS idx_policy_assignments_rollout ON policy_assignments (rollout_id);
CREATE INDEX IF NOT EXISTS idx_policy_assignments_coa ON policy_assignments (coa_status) WHERE coa_status != 'acked';
```

#### TBL-16: policy_rollouts
```sql
CREATE TABLE IF NOT EXISTS policy_rollouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_version_id UUID NOT NULL REFERENCES policy_versions(id),
    previous_version_id UUID REFERENCES policy_versions(id),
    strategy VARCHAR(20) NOT NULL DEFAULT 'canary',
    stages JSONB NOT NULL,
    current_stage INTEGER NOT NULL DEFAULT 0,
    total_sims INTEGER NOT NULL,
    migrated_sims INTEGER NOT NULL DEFAULT 0,
    state VARCHAR(20) NOT NULL DEFAULT 'pending',
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    rolled_back_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_policy_rollouts_version ON policy_rollouts (policy_version_id);
CREATE INDEX IF NOT EXISTS idx_policy_rollouts_state ON policy_rollouts (state);
```

#### TBL-14: policy_versions (existing columns used)
- `state` — needs `rolling_out` and `rolled_back` values (already in design: `draft/active/rolling_out/rolled_back/superseded`)
- `rolled_back_at` — already exists in schema
- `affected_sim_count` — populated by dry-run, used to calculate stage sizes

### Existing Code Patterns

#### Store pattern (from `internal/store/policy.go`):
- `PolicyStore` struct with `db *pgxpool.Pool`
- `scanXxx(row pgx.Row)` helper functions
- Transaction pattern with `db.Begin(ctx)` + `defer tx.Rollback(ctx)` + `tx.Commit(ctx)`
- Column constants: `var xxxColumns = "col1, col2, ..."`
- Error sentinels: `var ErrXxxNotFound = errors.New("store: ...")`

#### Handler pattern (from `internal/api/policy/handler.go`):
- `Handler` struct with store + services + eventBus + auditSvc + logger
- `chi.URLParam(r, "id")` for path params
- `apierr.WriteSuccess/WriteError` for responses
- `apierr.TenantIDKey` from context
- `userIDFromContext(r)` helper
- `createAuditEntry(r, action, entityID, before, after)` helper

#### Job Processor pattern (from `internal/job/import.go`):
- Implement `Processor` interface: `Type() string` + `Process(ctx context.Context, job *store.Job) error`
- Constructor: `NewXxxProcessor(deps...)`
- Register in `cmd/argus/main.go`: `jobRunner.Register(processor)`
- Publish to job queue via `eventBus.Publish(ctx, bus.SubjectJobQueue, job.JobMessage{...})`

#### EventBus pattern (from `internal/bus/nats.go`):
- Subject constants: `SubjectXxx = "argus.events.xxx.yyy"`
- `eventBus.Publish(ctx, subject, payload)` — payload is any serializable struct

#### Session Manager (from `internal/aaa/session/session.go`):
- `Manager.GetSessionsForSIM(ctx, simID)` — returns `[]*Session` for active sessions
- `Session.NASIP`, `Session.AcctSessionID`, `Session.IMSI` — fields needed for CoA

#### CoA Sender (from `internal/aaa/session/coa.go`):
- `CoASender.SendCoA(ctx, CoARequest{NASIP, AcctSessionID, IMSI, Attributes})` — sends RADIUS CoA packet
- Returns `*CoAResult` with `Status` ("ack", "nak", "timeout", "error")

#### Dry-run reuse (from `internal/policy/dryrun/service.go`):
- `buildFiltersFromMatch(compiled)` extracts MATCH conditions → `DryRunFilters`
- `SIMFleetFilters` struct used by `simStore.CountByFilters()`, `FetchSample()` etc.
- `asyncThreshold = 100000` — above this, use background job

### NATS Event Subject
```go
SubjectPolicyRolloutProgress = "argus.events.policy.rollout_progress"
```

### WebSocket Event (from WEBSOCKET_EVENTS.md — event #9)
The `policy.rollout_progress` event is already defined in the WebSocket spec. The WS hub subscribes to NATS subjects and pushes to connected clients. We need to add the subject to the hub's subscription list in `cmd/argus/main.go`.

## Prerequisites
- [x] STORY-023 completed — PolicyStore, PolicyVersion model, ActivateVersion handler
- [x] STORY-024 completed — SIMFleetFilters, CountByFilters, buildFiltersFromMatch, async job pattern
- [x] STORY-017 completed — Session Manager, CoASender, DMSender

## Tasks

### Task 1: Rollout store layer — CRUD for TBL-16 and TBL-15 operations
- **Files:** Modify `internal/store/policy.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/store/policy.go` — follow same store method patterns (scan helpers, column constants, error sentinels, transactions)
- **Context refs:** "Database Schema", "Existing Code Patterns > Store pattern"
- **What:**
  Add to `internal/store/policy.go`:

  1. **New types:**
     - `PolicyRollout` struct matching TBL-16 columns: `ID, PolicyVersionID, PreviousVersionID, Strategy, Stages (json.RawMessage), CurrentStage, TotalSIMs, MigratedSIMs, State, StartedAt, CompletedAt, RolledBackAt, CreatedAt, CreatedBy`
     - `PolicyAssignment` struct matching TBL-15 columns: `ID, SimID, PolicyVersionID, RolloutID, AssignedAt, CoASentAt, CoAStatus`
     - `CreateRolloutParams` struct: `PolicyVersionID, PreviousVersionID, Strategy, Stages (json.RawMessage), TotalSIMs, CreatedBy`
     - `RolloutStage` struct: `Pct int, Status string, SimCount *int, Migrated *int` (for JSON serialization)

  2. **New error sentinels:**
     - `ErrRolloutNotFound`
     - `ErrRolloutInProgress` — checked when starting new rollout
     - `ErrRolloutCompleted`
     - `ErrRolloutRolledBack`
     - `ErrStageInProgress`

  3. **New methods on PolicyStore:**
     - `CreateRollout(ctx, tenantID, params) (*PolicyRollout, error)` — INSERT into TBL-16 + set version state to `rolling_out`
     - `GetRolloutByID(ctx, rolloutID) (*PolicyRollout, error)` — SELECT with tenant join
     - `GetRolloutByIDWithTenant(ctx, rolloutID, tenantID) (*PolicyRollout, error)` — tenant-scoped
     - `GetActiveRolloutForPolicy(ctx, policyID) (*PolicyRollout, error)` — find in_progress rollout for a policy (to prevent concurrent rollouts)
     - `UpdateRolloutProgress(ctx, rolloutID, migratedSIMs, currentStage int, stages json.RawMessage) error` — update progress
     - `CompleteRollout(ctx, rolloutID) error` — set state=completed, completed_at=NOW(), version state→active, previous version→superseded
     - `RollbackRollout(ctx, rolloutID) error` — set state=rolled_back, rolled_back_at=NOW(), version state→rolled_back
     - `SelectSIMsForStage(ctx, tenantID, rolloutID, previousVersionID uuid.UUID, targetCount int) ([]uuid.UUID, error)` — SELECT sim IDs not yet migrated, ORDER BY random(), LIMIT targetCount, FOR UPDATE SKIP LOCKED
     - `AssignSIMsToVersion(ctx, simIDs []uuid.UUID, versionID, rolloutID uuid.UUID) (int, error)` — batch INSERT/UPDATE policy_assignments + UPDATE sims.policy_version_id, returns count affected
     - `RevertRolloutAssignments(ctx, rolloutID, previousVersionID uuid.UUID) (int, error)` — revert all SIMs assigned by this rollout back to previous version
     - `UpdateAssignmentCoAStatus(ctx, simID uuid.UUID, status string) error` — update coa_status on policy_assignments
     - `GetAssignmentsByRollout(ctx, rolloutID uuid.UUID) ([]PolicyAssignment, error)` — list all assignments for a rollout

  4. **Modify ActivateVersion:**
     - The existing `ActivateVersion` currently only accepts `state = 'draft'`. Add `'rolling_out'` as also valid for transition to `'active'` (needed when rollout completes at 100% and the version is finalized).

- **Verify:** `go build ./internal/store/...`

### Task 2: Rollout service — core business logic
- **Files:** Create `internal/policy/rollout/service.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/policy/dryrun/service.go` — follow same service structure (constructor, dependencies, logger)
- **Context refs:** "Architecture Context > Data Flow", "API Specifications", "Database Schema", "Existing Code Patterns > Session Manager", "Existing Code Patterns > CoA Sender", "Existing Code Patterns > Dry-run reuse"
- **What:**
  Create `internal/policy/rollout/service.go` with a `Service` struct:

  1. **Dependencies (constructor args):**
     - `policyStore *store.PolicyStore`
     - `simStore *store.SIMStore`
     - `dryRunSvc *dryrun.Service` (for `buildFiltersFromMatch` / `CountMatchingSIMs`)
     - `sessionMgr` (interface for GetSessionsForSIM — to avoid import cycle, define a `SessionProvider` interface in this package)
     - `coaSender` (interface for SendCoA — similarly, define `CoADispatcher` interface)
     - `eventBus *bus.EventBus`
     - `jobStore *store.JobStore`
     - `db *pgxpool.Pool`
     - `logger zerolog.Logger`

  2. **Interfaces (define in this package to avoid import cycles):**
     ```
     SessionProvider interface {
         GetSessionsForSIM(ctx, simID string) ([]*session.Session, error)
     }
     CoADispatcher interface {
         SendCoA(ctx, req CoARequest) (*CoAResult, error)
     }
     ```
     Where `CoARequest` and `CoAResult` can be re-imported from `internal/aaa/session` or defined as local types.

  3. **Key methods:**
     - `StartRollout(ctx, tenantID, versionID uuid.UUID, stages []int, createdBy *uuid.UUID) (*store.PolicyRollout, error)`:
       - Validate version exists and is in `draft` state (via `GetVersionWithTenant`)
       - Check no active rollout for this policy (`GetActiveRolloutForPolicy`)
       - Get affected SIM count from version's `affected_sim_count` field, or if nil, use `dryRunSvc.CountMatchingSIMs`
       - Set version state to `rolling_out`
       - Create rollout record with stages JSON
       - Execute first stage (1%) inline or via job if count > asyncThreshold
       - Return rollout record

     - `ExecuteStage(ctx, rollout *store.PolicyRollout, stageIndex int) error`:
       - Calculate targetCount: `ceil(totalSIMs * stagePct / 100) - migratedSIMs`
       - Call `policyStore.SelectSIMsForStage` to get SIM IDs
       - Process in batches of 1000:
         - `policyStore.AssignSIMsToVersion` for the batch
         - For each SIM, check for active sessions via `sessionProvider.GetSessionsForSIM`
         - Send CoA for each active session (log failures but continue)
         - Update `policyStore.UpdateAssignmentCoAStatus` per SIM
         - Update rollout progress after each batch
         - Publish `policy.rollout_progress` NATS event after each batch
       - If stage is 100% and all SIMs migrated → call `policyStore.CompleteRollout`
       - Update stages JSON with status

     - `AdvanceRollout(ctx, tenantID, rolloutID uuid.UUID) (*store.PolicyRollout, error)`:
       - Get rollout, validate state = `in_progress`
       - Determine next stage index
       - If async needed (remaining SIMs > asyncThreshold), create background job
       - Otherwise execute stage inline
       - Return updated rollout

     - `RollbackRollout(ctx, tenantID, rolloutID uuid.UUID, reason string) (*store.PolicyRollout, int, error)`:
       - Get rollout, validate state = `in_progress`
       - Revert all assignments: `policyStore.RevertRolloutAssignments`
       - Send CoA for all reverted SIMs with active sessions (batch of 1000)
       - Update rollout state to `rolled_back`
       - Update version state to `rolled_back`, `rolled_back_at = NOW()`
       - Publish rollout progress event with state=rolled_back
       - Return reverted count

     - `GetProgress(ctx, tenantID, rolloutID uuid.UUID) (*store.PolicyRollout, error)`:
       - Get rollout by ID with tenant scope
       - Return rollout record

  4. **Batch processing:**
     - Process SIMs in batches of 1000 (configurable constant `batchSize = 1000`)
     - On CoA failure: log error, set `coa_status = 'failed'`, continue
     - Update rollout progress after each batch

  5. **NATS events:**
     - After each batch and on stage completion: publish to `SubjectPolicyRolloutProgress`
     - Event payload matches WebSocket event #9 schema (rollout_id, policy_id, stages, current_stage, total_sims, migrated_sims, etc.)

- **Verify:** `go build ./internal/policy/rollout/...`

### Task 3: Rollout job processor for async large stages
- **Files:** Create `internal/job/rollout.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/dryrun.go` — follow same processor pattern (Type(), Process(), constructor)
- **Context refs:** "Existing Code Patterns > Job Processor pattern", "Existing Code Patterns > EventBus pattern"
- **What:**
  Create `internal/job/rollout.go`:

  1. `RolloutStageProcessor` struct implementing `Processor` interface
  2. Constructor: `NewRolloutStageProcessor(rolloutSvc *rollout.Service, jobStore *store.JobStore, eventBus *bus.EventBus, logger zerolog.Logger)`
  3. `Type() string` returns `"policy_rollout_stage"`
  4. `Process(ctx, job *store.Job) error`:
     - Extract from job payload: `rollout_id`, `stage_index`, `tenant_id`
     - Call `rolloutSvc.ExecuteStage(ctx, rollout, stageIndex)`
     - Update job progress as stage executes
     - On completion: update job state to `completed`
     - On error: update job state to `failed`

  Similarly for rollback: `RolloutRollbackProcessor` (type = `"policy_rollout_rollback"`)
  - Extracts `rollout_id`, `tenant_id`, `reason` from payload
  - Calls `rolloutSvc.RollbackRollout`

- **Verify:** `go build ./internal/job/...`

### Task 4: API handler methods for rollout endpoints
- **Files:** Modify `internal/api/policy/handler.go`
- **Depends on:** Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/api/policy/handler.go` — follow existing handler method patterns (validation, error handling, audit logging)
- **Context refs:** "API Specifications", "Existing Code Patterns > Handler pattern"
- **What:**
  Add to the existing `Handler` struct in `internal/api/policy/handler.go`:

  1. **Add dependency:** `rolloutSvc *rollout.Service` field in Handler struct
  2. **Update `NewHandler` constructor** to accept `rolloutSvc` parameter

  3. **New handler methods:**

     - `StartRollout(w, r)` — API-096:
       - Parse version ID from URL path
       - Decode optional request body `{stages: [1,10,100]}`
       - Default stages to `[1, 10, 100]` if not provided
       - Validate stages (each 1-100, ascending, last must be 100)
       - Call `rolloutSvc.StartRollout(ctx, tenantID, versionID, stages, userID)`
       - Create audit entry
       - Return 201 with rollout data

     - `AdvanceRollout(w, r)` — API-097:
       - Parse rollout ID from URL path
       - Call `rolloutSvc.AdvanceRollout(ctx, tenantID, rolloutID)`
       - Create audit entry
       - Return 200 with progress data

     - `RollbackRollout(w, r)` — API-098:
       - Parse rollout ID from URL path
       - Decode optional request body `{reason: "..."}`
       - Call `rolloutSvc.RollbackRollout(ctx, tenantID, rolloutID, reason)`
       - Create audit entry
       - Return 200 with rollback result

     - `GetRollout(w, r)` — API-099:
       - Parse rollout ID from URL path
       - Call `rolloutSvc.GetProgress(ctx, tenantID, rolloutID)`
       - Return 200 with rollout data

  4. **Response types:**
     - `rolloutResponse` struct for API responses
     - `toRolloutResponse(r *store.PolicyRollout)` converter function

  5. **Error mapping:**
     - `ErrRolloutNotFound` → 404
     - `ErrRolloutInProgress` → 422 `ROLLOUT_IN_PROGRESS`
     - `ErrRolloutCompleted` → 422 `ROLLOUT_COMPLETED`
     - `ErrRolloutRolledBack` → 422 `ROLLOUT_ROLLED_BACK`
     - `ErrVersionNotDraft` → 422 `VERSION_NOT_DRAFT`

- **Verify:** `go build ./internal/api/policy/...`

### Task 5: Route registration + NATS subject + main.go wiring
- **Files:** Modify `internal/gateway/router.go`, Modify `internal/bus/nats.go`, Modify `cmd/argus/main.go`
- **Depends on:** Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` (policy routes block), `cmd/argus/main.go` (job runner registration), `internal/bus/nats.go` (subject constants)
- **Context refs:** "Architecture Context > Components Involved", "Existing Code Patterns > EventBus pattern"
- **What:**

  1. **`internal/bus/nats.go`:**
     - Add subject constant: `SubjectPolicyRolloutProgress = "argus.events.policy.rollout_progress"`

  2. **`internal/gateway/router.go`:**
     - Add 4 new routes under the existing `PolicyHandler` group (same auth: `RequireRole("policy_editor")`):
       ```
       POST /api/v1/policy-versions/{id}/rollout → PolicyHandler.StartRollout
       POST /api/v1/policy-rollouts/{id}/advance → PolicyHandler.AdvanceRollout
       POST /api/v1/policy-rollouts/{id}/rollback → PolicyHandler.RollbackRollout
       GET  /api/v1/policy-rollouts/{id} → PolicyHandler.GetRollout
       ```

  3. **`cmd/argus/main.go`:**
     - Import `rollout` package: `"github.com/btopcu/argus/internal/policy/rollout"`
     - Create rollout service: `rolloutSvc := rollout.NewService(policyStore, simStore, dryRunSvc, sessionMgr, coaSender, eventBus, jobStore, pg.Pool, log.Logger)`
       (sessionMgr and coaSender are available in the RADIUS block — need to handle nil case when RADIUS is disabled)
     - Pass `rolloutSvc` to `policyapi.NewHandler`
     - Create rollout processors: `rolloutStageProc := job.NewRolloutStageProcessor(rolloutSvc, jobStore, eventBus, log.Logger)`
     - Register: `jobRunner.Register(rolloutStageProc)`
     - Add `bus.SubjectPolicyRolloutProgress` to WS hub NATS subscription list

- **Verify:** `go build ./cmd/argus/...`

### Task 6: Unit tests for rollout store and service
- **Files:** Create `internal/store/policy_rollout_test.go`, Create `internal/policy/rollout/service_test.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/policy/dryrun/service_test.go` — follow test patterns; Read `internal/job/import_test.go` — follow mock patterns
- **Context refs:** "API Specifications", "Database Schema", "Acceptance Criteria Mapping"
- **What:**
  Test scenarios from the story:

  1. **Store tests (`internal/store/policy_rollout_test.go`):**
     - `TestCreateRollout` — creates rollout, verifies TBL-16 record
     - `TestGetActiveRolloutForPolicy` — returns active rollout, nil when none
     - `TestSelectSIMsForStage` — selects correct count, excludes already migrated
     - `TestAssignSIMsToVersion` — updates policy_assignments and sims table
     - `TestRevertRolloutAssignments` — reverts all SIMs to previous version
     - `TestCompleteRollout` — sets state, timestamps, version states
     - `TestRollbackRollout` — sets state, timestamps, version states

  2. **Service tests (`internal/policy/rollout/service_test.go`):**
     - `TestStartRollout_Success` — 1% of SIMs migrated, rollout record created
     - `TestStartRollout_RolloutInProgress` — returns error when another rollout is active
     - `TestStartRollout_VersionNotDraft` — rejects non-draft version
     - `TestAdvanceRollout_To10Pct` — additional 9% SIMs migrated
     - `TestAdvanceRollout_To100Pct` — all remaining SIMs, rollout completed
     - `TestAdvanceRollout_AlreadyCompleted` — returns error
     - `TestRollbackRollout_At10Pct` — reverts 10%, CoA sent, state=rolled_back
     - `TestRollbackRollout_AlreadyRolledBack` — returns error
     - `TestExecuteStage_CoAFailure` — CoA failure logged but continues
     - `TestExecuteStage_AsyncForLargeCount` — creates job when > asyncThreshold

  Use mock interfaces for SessionProvider and CoADispatcher.

- **Verify:** `go test ./internal/store/... ./internal/policy/rollout/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| POST activate activates immediately (100%) | Already done (STORY-023) | Existing tests |
| POST rollout starts staged rollout at 1% | Task 2 (StartRollout), Task 4 (handler) | Task 6 (TestStartRollout_Success) |
| Rollout record created in TBL-16 with stages | Task 1 (CreateRollout), Task 2 | Task 6 (TestCreateRollout) |
| Stage 1: 1% SIMs selected, TBL-15 updated, CoA sent | Task 2 (ExecuteStage) | Task 6 (TestStartRollout_Success) |
| POST advance moves to next stage | Task 2 (AdvanceRollout), Task 4 | Task 6 (TestAdvanceRollout_To10Pct) |
| Advance requires explicit user action | Task 4 (separate endpoint) | By design |
| Concurrent versions during rollout | Task 1 (policy_assignments tracks version per SIM) | Task 6 |
| Policy eval uses SIM-specific version from TBL-15 | Already in policy evaluator (resolve_policy step 1) | Existing |
| POST rollback reverts all SIMs | Task 2 (RollbackRollout) | Task 6 (TestRollbackRollout_At10Pct) |
| Rollback triggers mass CoA | Task 2 (RollbackRollout sends CoA) | Task 6 |
| GET rollout returns progress | Task 2 (GetProgress), Task 4 | Task 6 |
| NATS: publish rollout_progress | Task 2 (event publishing), Task 5 (subject constant) | Task 6 |
| WebSocket: push progress | Task 5 (WS hub subscription) | Manual verification |
| CoA failure logged but continues | Task 2 (error handling in ExecuteStage) | Task 6 (TestExecuteStage_CoAFailure) |
| Only one active rollout per policy | Task 1 (GetActiveRolloutForPolicy), Task 2 | Task 6 (TestStartRollout_RolloutInProgress) |

## Story-Specific Compliance Rules
- **API:** Standard envelope `{ status, data, meta?, error? }` for all responses
- **DB:** All queries scoped by tenant_id (enforced by joining policy_versions → policies with tenant_id check)
- **Audit:** Every state-changing endpoint creates audit entry (rollout.start, rollout.advance, rollout.rollback)
- **Business:** Policy evaluation order: SIM-specific (TBL-15) > APN-level > operator-level > tenant default (from ALGORITHMS.md #6)
- **Business:** Staged rollout concurrent versions allowed (BR-4 from PRODUCT.md)
- **ADR-001:** All code in `internal/` packages, modular monolith structure

## Risks & Mitigations
- **Large rollout performance:** Batching (1000 SIMs per batch) + async job for stages > 100K SIMs mitigates DB pressure
- **CoA failures during rollout:** Log and continue pattern — partial success is acceptable, tracked via coa_status in TBL-15
- **Race condition on concurrent rollout check:** Use `GetActiveRolloutForPolicy` with transaction isolation to prevent dual starts
- **Import cycle with session package:** Define interfaces (SessionProvider, CoADispatcher) in rollout package to break dependency
- **RADIUS not configured:** Handle nil sessionMgr/coaSender gracefully — skip CoA when not available
