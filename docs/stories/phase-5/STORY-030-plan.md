# Implementation Plan: STORY-030 - Bulk Operations (State Change, Policy Assign, Operator Switch)

## Goal
Implement three bulk operation endpoints (state change, policy assign, operator switch) that accept a segment_id, create background jobs via SVC-09, and process SIMs with partial success handling, error reports, progress tracking, undo capability, and distributed locking.

## Architecture Context

### Components Involved
- **SVC-03 (Core API)**: `internal/api/sim/bulk_handler.go` — HTTP handlers for bulk operation endpoints
- **SVC-09 (Job Runner)**: `internal/job/` — Background processors for each bulk operation type
- **Store Layer**: `internal/store/segment.go` (segment query), `internal/store/sim.go` (state transitions), `internal/store/esim.go` (eSIM Switch), `internal/store/job.go` (job CRUD)
- **Event Bus**: `internal/bus/` — NATS publishing for progress/completion events
- **Distributed Lock**: `internal/job/lock.go` — Redis SETNX per-SIM locking
- **Router**: `internal/gateway/router.go` — Route registration

### Data Flow
```
User → POST /api/v1/sims/bulk/{operation}
  → BulkHandler validates request, counts segment SIMs
  → Creates job record (state=queued) in TBL-20
  → Publishes JobMessage to NATS job queue
  → Returns 202 {job_id, status:"queued", estimated_count}

Job Runner picks up message:
  → Locks job record (state=running)
  → Processor fetches SIM IDs via segment filter
  → Iterates SIMs in batches (default 100)
    → Acquires distributed lock per SIM
    → Processes SIM (state change / policy assign / operator switch)
    → Records previous_state for undo
    → Logs errors for failed SIMs
    → Updates progress every batch_size items
    → Publishes progress via NATS → WebSocket
  → Completes job with error_report and result
```

### API Specifications

#### API-064: POST /api/v1/sims/bulk/state-change
- Auth: JWT(sim_manager+)
- Request body: `{ "segment_id": "uuid", "target_state": "suspended|active|terminated", "reason": "optional string" }`
- Success response (202): `{ "status": "success", "data": { "job_id": "uuid", "status": "queued", "estimated_count": 150 } }`
- Error response (400): `{ "status": "error", "error": { "code": "VALIDATION_ERROR", "message": "..." } }`

#### API-065: POST /api/v1/sims/bulk/policy-assign
- Auth: JWT(policy_editor+)
- Request body: `{ "segment_id": "uuid", "policy_version_id": "uuid" }`
- Success response (202): `{ "status": "success", "data": { "job_id": "uuid", "status": "queued", "estimated_count": 150 } }`
- Error response (400): `{ "status": "error", "error": { "code": "VALIDATION_ERROR", "message": "..." } }`

#### API-066: POST /api/v1/sims/bulk/operator-switch
- Auth: JWT(tenant_admin)
- Request body: `{ "segment_id": "uuid", "target_operator_id": "uuid", "target_apn_id": "uuid" }`
- Success response (202): `{ "status": "success", "data": { "job_id": "uuid", "status": "queued", "estimated_count": 150 } }`
- Error response (400): `{ "status": "error", "error": { "code": "VALIDATION_ERROR", "message": "..." } }`

### Database Schema

#### TBL-10: sims (Source: migrations/20260320000002_core_schema.up.sql — ACTUAL)
Key columns used:
```sql
id UUID PRIMARY KEY,
tenant_id UUID NOT NULL,
operator_id UUID NOT NULL,
apn_id UUID,
iccid VARCHAR(22) NOT NULL,
imsi VARCHAR(15) NOT NULL,
policy_version_id UUID,
esim_profile_id UUID,
sim_type VARCHAR(20) DEFAULT 'physical', -- 'physical' or 'esim'
state VARCHAR(20) DEFAULT 'ordered',     -- ordered,active,suspended,stolen_lost,terminated,purged
```

#### TBL-11: sim_state_history (Source: migrations/20260320000002_core_schema.up.sql — ACTUAL)
```sql
id BIGSERIAL PRIMARY KEY,
sim_id UUID NOT NULL REFERENCES sims(id),
from_state VARCHAR(20),
to_state VARCHAR(20) NOT NULL,
reason TEXT,
triggered_by VARCHAR(50) NOT NULL,
user_id UUID,
job_id UUID,
created_at TIMESTAMPTZ DEFAULT NOW()
```

#### TBL-20: jobs (Source: migrations/20260320000002_core_schema.up.sql — ACTUAL)
```sql
id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
tenant_id UUID NOT NULL,
type VARCHAR(50) NOT NULL,
state VARCHAR(20) DEFAULT 'queued',
priority INTEGER DEFAULT 5,
payload JSONB DEFAULT '{}',
total_items INTEGER DEFAULT 0,
processed_items INTEGER DEFAULT 0,
failed_items INTEGER DEFAULT 0,
progress_pct NUMERIC(5,2) DEFAULT 0,
error_report JSONB,
result JSONB,
max_retries INTEGER DEFAULT 3,
retry_count INTEGER DEFAULT 0,
retry_backoff_sec INTEGER DEFAULT 60,
scheduled_at TIMESTAMPTZ,
started_at TIMESTAMPTZ,
completed_at TIMESTAMPTZ,
created_at TIMESTAMPTZ DEFAULT NOW(),
created_by UUID,
locked_by VARCHAR(100),
locked_at TIMESTAMPTZ
```

#### TBL-15: policy_assignments — stored as `sims.policy_version_id` FK
The policy assignment is a column on the sims table, not a separate table. Use `SIMStore.SetIPAndPolicy()` to update.

#### Valid State Transitions (from store/sim.go)
```go
var validTransitions = map[string][]string{
    "ordered":     {"active"},
    "active":      {"suspended", "stolen_lost", "terminated"},
    "suspended":   {"active", "terminated"},
    "stolen_lost": {},
    "terminated":  {"purged"},
    "purged":      {},
}
```

### Existing Store Methods Used
- `SegmentStore.CountMatchingSIMs(ctx, segmentID)` — counts SIMs matching segment filter
- `SegmentStore.buildSegmentFilterQuery(ctx, segmentID)` — internal, returns SegmentFilter + tenantID
- `buildFilterConditions(filter, tenantID)` — builds WHERE clause from SegmentFilter
- `SIMStore.TransitionState(ctx, simID, targetState, userID, triggeredBy, reason, purgeRetentionDays)` — validates + transitions state with history
- `SIMStore.SetIPAndPolicy(ctx, simID, ipAddressID, policyVersionID)` — updates policy_version_id
- `ESimProfileStore.GetEnabledProfileForSIM(ctx, simID)` — finds enabled profile (returns nil, nil if none)
- `ESimProfileStore.Switch(ctx, tenantID, sourceProfileID, targetProfileID, userID)` — atomic disable+enable+update SIM operator
- `JobStore.Create(ctx, CreateJobParams)` / `CreateWithTenantID(ctx, tenantID, CreateJobParams)` — create job
- `JobStore.Complete(ctx, jobID, errorReport, result)` — mark completed
- `JobStore.UpdateProgress(ctx, jobID, processed, failed, total)` — update progress
- `JobStore.CheckCancelled(ctx, jobID)` — check if cancelled
- `DistributedLock.Acquire(ctx, key, holderID, ttl)` — Redis SETNX
- `DistributedLock.Release(ctx, key, holderID)` — Lua script release
- `DistributedLock.SIMKey(simID)` — returns "sim:{simID}"

### Existing Job Type Constants (from job/types.go)
```go
JobTypeBulkStateChange  = "bulk_state_change"
JobTypeBulkPolicyAssign = "bulk_policy_assign"
JobTypeBulkEsimSwitch   = "bulk_esim_switch"
```

### Segment Filter Structure (from store/segment.go)
```go
type SegmentFilter struct {
    OperatorID *uuid.UUID `json:"operator_id,omitempty"`
    State      string     `json:"state,omitempty"`
    APNID      *uuid.UUID `json:"apn_id,omitempty"`
    RATType    string     `json:"rat_type,omitempty"`
}
```

### Undo Payload Structure
Each processor stores per-SIM previous state in the job result:
```json
{
  "type": "bulk_state_change",
  "undo_records": [
    {"sim_id": "uuid", "previous_state": "active", "previous_policy_version_id": "uuid", ...}
  ],
  "processed_count": 100,
  "failed_count": 5
}
```
Undo creates a NEW job with type=same, payload containing the undo_records to revert.

### Error Report Structure
```json
[
  {"sim_id": "uuid", "iccid": "89901234...", "error_code": "INVALID_STATE_TRANSITION", "error_message": "cannot transition from ordered to suspended"}
]
```

## Prerequisites
- [x] STORY-011 completed (SIM CRUD — SIMStore methods)
- [x] STORY-012 completed (Segments — SegmentStore with CountMatchingSIMs)
- [x] STORY-028 completed (eSIM profiles — ESimProfileStore.Switch())
- [x] STORY-031 completed (Job runner — Runner, DistributedLock, StubProcessor)

## Tasks

### Task 1: Add ListMatchingSIMIDs method to SegmentStore
- **Files:** Modify `internal/store/segment.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/segment.go` — follow `CountMatchingSIMs` pattern (uses `buildSegmentFilterQuery` + `buildFilterConditions`)
- **Context refs:** Database Schema, Existing Store Methods Used, Segment Filter Structure
- **What:** Add a `ListMatchingSIMIDs(ctx, segmentID uuid.UUID) ([]uuid.UUID, error)` method that returns all SIM IDs matching the segment's filter. Follows the same pattern as `CountMatchingSIMs`: call `buildSegmentFilterQuery(ctx, id)` to get filter + tenantID, then `buildFilterConditions(filter, tenantID)` to build WHERE clause, then query `SELECT id FROM sims WHERE ...` with `ORDER BY id`. Also add `ListMatchingSIMIDsWithDetails(ctx, segmentID uuid.UUID) ([]SIMBulkInfo, error)` that returns `[]SIMBulkInfo` struct containing `{ID uuid.UUID, ICCID string, State string, SimType string, PolicyVersionID *uuid.UUID, OperatorID uuid.UUID, ESimProfileID *uuid.UUID}` — needed by processors for validation and undo. Use the same filter building approach.
- **Verify:** `go build ./internal/store/...`

### Task 2: Bulk API handlers for state-change, policy-assign, operator-switch
- **Files:** Modify `internal/api/sim/bulk_handler.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/api/sim/bulk_handler.go` — follow `Import` handler pattern (validate, count, create job, publish to NATS, return 202)
- **Context refs:** API Specifications, Architecture Context > Data Flow, Existing Store Methods Used, Existing Job Type Constants
- **What:** Add three new handler methods to `BulkHandler`:
  1. `StateChange(w, r)` — parses `{segment_id, target_state, reason?}`, validates target_state is one of valid states, calls `SegmentStore.CountMatchingSIMs` for estimated_count, creates job with type `bulk_state_change` and payload `{segment_id, target_state, reason}`, publishes to NATS, returns 202.
  2. `PolicyAssign(w, r)` — parses `{segment_id, policy_version_id}`, validates both UUIDs, counts segment, creates job with type `bulk_policy_assign` and payload `{segment_id, policy_version_id}`, returns 202.
  3. `OperatorSwitch(w, r)` — parses `{segment_id, target_operator_id, target_apn_id}`, validates all UUIDs, counts segment, creates job with type `bulk_esim_switch` and payload `{segment_id, target_operator_id, target_apn_id}`, returns 202.

  BulkHandler needs a new field `segments *store.SegmentStore` — update the constructor `NewBulkHandler` to accept it. Each handler creates a standard `bulkJobResponse{JobID, Status:"queued", EstimatedCount}` DTO.
- **Verify:** `go build ./internal/api/sim/...`

### Task 3: Register bulk routes in gateway router
- **Files:** Modify `internal/gateway/router.go`, Modify `cmd/argus/main.go`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` lines 267-273 — follow BulkHandler route registration pattern
- **Context refs:** API Specifications, Architecture Context > Components Involved
- **What:**
  1. In `router.go`: Within the existing `if deps.BulkHandler != nil` block, add three new routes with appropriate role guards:
     - `POST /api/v1/sims/bulk/state-change` with `RequireRole("sim_manager")` — same group as Import
     - Add a NEW group for `POST /api/v1/sims/bulk/policy-assign` with `RequireRole("policy_editor")`
     - Add a NEW group for `POST /api/v1/sims/bulk/operator-switch` with `RequireRole("tenant_admin")`
  2. In `main.go`: Update `NewBulkHandler` call to pass `segmentStore` as the new dependency.
- **Verify:** `go build ./cmd/argus/...`

### Task 4: Bulk state change processor
- **Files:** Create `internal/job/bulk_state_change.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/job/ota.go` — follow OTAProcessor pattern (struct, constructor, Type(), Process(), updateProgressPeriodic())
- **Context refs:** Architecture Context > Data Flow, Database Schema, Existing Store Methods Used, Error Report Structure, Undo Payload Structure, Valid State Transitions
- **What:** Create `BulkStateChangeProcessor` implementing `Processor` interface:
  - **Struct fields:** `jobs *store.JobStore`, `sims *store.SIMStore`, `segments *store.SegmentStore`, `distLock *DistributedLock`, `eventBus *bus.EventBus`, `logger zerolog.Logger`
  - **Payload struct:** `BulkStateChangePayload{SegmentID uuid.UUID, TargetState string, Reason *string, UndoRecords []UndoRecord}` where `UndoRecord{SimID uuid.UUID, PreviousState string}`
  - **Process logic:**
    1. Unmarshal payload. If `UndoRecords` is present, process undo mode (revert each SIM to its PreviousState).
    2. Otherwise: call `segments.ListMatchingSIMIDsWithDetails(ctx, segmentID)` to get SIM list
    3. Iterate SIMs: for each SIM:
       a. Check cancellation every 100 items
       b. Acquire distributed lock via `distLock.Acquire(ctx, distLock.SIMKey(simID), job.ID.String(), 30s)`; if lock fails, add to errors and continue
       c. Call `sims.TransitionState(ctx, simID, targetState, nil, "bulk_job", reason, 0)`
       d. If `ErrInvalidStateTransition` → add to error report with sim_id, iccid, error_code, error_message; continue
       e. If success → record `UndoRecord{SimID, PreviousState: sim.State}` in undo list
       f. Release distributed lock
       g. Update progress periodically (every batch of 100)
    4. Complete job with error_report and result JSON containing undo_records + counts
  - **Error report entries:** `BulkOpError{SimID string, ICCID string, ErrorCode string, ErrorMessage string}`
  - Use `bus.SubjectJobProgress` and `bus.SubjectJobCompleted` for event publishing
- **Verify:** `go build ./internal/job/...`

### Task 5: Bulk policy assign processor
- **Files:** Create `internal/job/bulk_policy_assign.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/job/ota.go` — follow OTAProcessor pattern; also read Task 4's file `internal/job/bulk_state_change.go` for shared types
- **Context refs:** Architecture Context > Data Flow, Database Schema, Existing Store Methods Used, Error Report Structure, Undo Payload Structure
- **What:** Create `BulkPolicyAssignProcessor` implementing `Processor` interface:
  - **Struct fields:** `jobs *store.JobStore`, `sims *store.SIMStore`, `segments *store.SegmentStore`, `distLock *DistributedLock`, `eventBus *bus.EventBus`, `logger zerolog.Logger`
  - **Payload struct:** `BulkPolicyAssignPayload{SegmentID uuid.UUID, PolicyVersionID uuid.UUID, UndoRecords []PolicyUndoRecord}` where `PolicyUndoRecord{SimID uuid.UUID, PreviousPolicyVersionID *uuid.UUID}`
  - **Process logic:**
    1. If `UndoRecords` present → revert each SIM's policy_version_id to previous value via `sims.SetIPAndPolicy(ctx, simID, nil, prevPolicyVersionID)`
    2. Otherwise: call `segments.ListMatchingSIMIDsWithDetails` to get SIM list
    3. Iterate SIMs:
       a. Check cancellation every 100 items
       b. Acquire per-SIM distributed lock
       c. Record `PolicyUndoRecord{SimID, PreviousPolicyVersionID: sim.PolicyVersionID}`
       d. Call `sims.SetIPAndPolicy(ctx, simID, nil, &payload.PolicyVersionID)` to assign policy
       e. On error → add to error report
       f. Release lock, update progress
    4. Complete job with error_report and result JSON containing undo_records + counts
- **Verify:** `go build ./internal/job/...`

### Task 6: Bulk eSIM operator switch processor
- **Files:** Create `internal/job/bulk_esim_switch.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/job/ota.go` — follow OTAProcessor pattern; read `internal/store/esim.go` Switch() method for eSIM logic
- **Context refs:** Architecture Context > Data Flow, Database Schema, Existing Store Methods Used, Error Report Structure, Undo Payload Structure, Post-STORY-028 Notes
- **What:** Create `BulkEsimSwitchProcessor` implementing `Processor` interface:
  - **Struct fields:** `jobs *store.JobStore`, `sims *store.SIMStore`, `segments *store.SegmentStore`, `esimStore *store.ESimProfileStore`, `distLock *DistributedLock`, `eventBus *bus.EventBus`, `logger zerolog.Logger`
  - **Payload struct:** `BulkEsimSwitchPayload{SegmentID uuid.UUID, TargetOperatorID uuid.UUID, TargetAPNID uuid.UUID, UndoRecords []EsimUndoRecord}` where `EsimUndoRecord{SimID uuid.UUID, OldProfileID uuid.UUID, NewProfileID uuid.UUID, PreviousOperatorID uuid.UUID}`
  - **Process logic:**
    1. If `UndoRecords` present → for each record, call `esimStore.Switch(ctx, tenantID, newProfileID, oldProfileID, nil)` to revert
    2. Otherwise: call `segments.ListMatchingSIMIDsWithDetails` to get SIM list
    3. Iterate SIMs:
       a. Check cancellation every 100 items
       b. If `sim.SimType != "esim"` → skip with info log (physical SIMs don't have profiles to switch)
       c. Acquire per-SIM distributed lock
       d. Call `esimStore.GetEnabledProfileForSIM(ctx, simID)` — if nil → add to errors ("no enabled profile"), release lock, continue
       e. Find target profile: query `esimStore.List(ctx, tenantID, ListESimProfilesParams{SimID: &simID, OperatorID: &targetOperatorID, State: "disabled"})` — if no matching profile → add to errors ("no profile for target operator"), release lock, continue
       f. Call `esimStore.Switch(ctx, tenantID, enabledProfile.ID, targetProfile.ID, nil)` — atomic switch
       g. If `ErrInvalidProfileState` → add to errors, continue
       h. Record undo record: `EsimUndoRecord{SimID, OldProfileID: enabledProfile.ID, NewProfileID: targetProfile.ID, PreviousOperatorID: sim.OperatorID}`
       i. Release lock, update progress
    4. Complete job
- **Verify:** `go build ./internal/job/...`

### Task 7: Register processors in main.go, replace stubs
- **Files:** Modify `cmd/argus/main.go`
- **Depends on:** Task 4, Task 5, Task 6
- **Complexity:** medium
- **Pattern ref:** Read `cmd/argus/main.go` lines 170-195 — follow existing processor registration pattern
- **Context refs:** Architecture Context > Components Involved
- **What:** In main.go:
  1. Replace the three stub processors with real ones:
     - `bulkStateChangeStub` → `bulkStateChangeProc := job.NewBulkStateChangeProcessor(jobStore, simStore, segmentStore, distLock, eventBus, log.Logger)`
     - `bulkPolicyAssignStub` → `bulkPolicyAssignProc := job.NewBulkPolicyAssignProcessor(jobStore, simStore, segmentStore, distLock, eventBus, log.Logger)`
     - `bulkEsimSwitchStub` → `bulkEsimSwitchProc := job.NewBulkEsimSwitchProcessor(jobStore, simStore, segmentStore, esimStore, distLock, eventBus, log.Logger)`
  2. Update `jobRunner.Register(...)` calls to use real processors instead of stubs
  3. Remove the three stub variable declarations
- **Verify:** `go build ./cmd/argus/...`

### Task 8: CSV error report support for bulk operations
- **Files:** Modify `internal/api/job/handler.go`
- **Depends on:** Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/job/handler.go` `writeErrorReportCSV` method — follow existing CSV export pattern
- **Context refs:** Error Report Structure
- **What:** The existing `writeErrorReportCSV` only handles `ImportRowError` structs. Update it to handle both import row errors and bulk operation errors:
  1. Create a `BulkOpError` type in the job handler package (or import from `internal/job/`) matching `{sim_id, iccid, error_code, error_message}`
  2. Attempt to unmarshal as `[]BulkOpError` first. If that fails, fall back to `[]ImportRowError`.
  3. For bulk op errors, write CSV with columns: `sim_id,iccid,error_code,error_message`
  4. For import row errors, keep existing behavior: `row,iccid,error`
- **Verify:** `go build ./internal/api/job/...`

### Task 9: Tests for bulk handlers and processors
- **Files:** Create `internal/job/bulk_state_change_test.go`, Modify `internal/api/sim/bulk_handler_test.go`
- **Depends on:** Task 2, Task 4, Task 5, Task 6
- **Complexity:** high
- **Pattern ref:** Read `internal/api/sim/bulk_handler_test.go` — follow existing bulk test pattern; read `internal/job/runner_test.go` for processor test patterns
- **Context refs:** API Specifications, Error Report Structure, Undo Payload Structure, Valid State Transitions
- **What:**
  **API Handler tests** (in `bulk_handler_test.go`):
  - `TestBulkStateChangeMissingSegmentID` — request without segment_id → 400
  - `TestBulkStateChangeInvalidState` — target_state = "invalid" → 400
  - `TestBulkPolicyAssignMissingPolicyVersionID` — request without policy_version_id → 400
  - `TestBulkOperatorSwitchMissingFields` — request without target_operator_id → 400

  **Processor unit tests** (in `bulk_state_change_test.go`):
  - Test payload unmarshalling for each processor type
  - Test UndoRecord serialization/deserialization
  - Test BulkOpError structure matches expected JSON format
  - Test that valid transitions are respected (using validTransitions map)
- **Verify:** `go test ./internal/job/... ./internal/api/sim/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| POST /api/v1/sims/bulk/state-change accepts segment_id + target_state | Task 2 | Task 9 |
| POST /api/v1/sims/bulk/policy-assign accepts segment_id + policy_version_id | Task 2 | Task 9 |
| POST /api/v1/sims/bulk/operator-switch accepts segment_id + target_operator_id | Task 2 | Task 9 |
| All bulk endpoints return 202 with job_id | Task 2, Task 3 | Task 9 |
| Job runner processes SIMs with configurable batch_size | Task 4, Task 5, Task 6 | Task 9 |
| Partial success: valid SIMs processed, invalid logged | Task 4, Task 5, Task 6 | Task 9 |
| Error report: JSONB array with sim_id, iccid, error_code, error_message | Task 4, Task 5, Task 6 | Task 9 |
| Error report downloadable as CSV | Task 8 | Task 9 |
| Retry: POST /api/v1/jobs/:id/retry re-processes failed items | Existing (job handler) | Task 9 |
| Undo: job stores previous_state, undo reverts changes | Task 4, Task 5, Task 6 | Task 9 |
| Progress: job.progress_pct updated every batch | Task 4, Task 5, Task 6 | Task 9 |
| Bulk state change validates each transition | Task 4 | Task 9 |
| Bulk policy assign: update policy_version_id | Task 5 | Task 9 |
| Bulk operator switch: Switch() profiles | Task 6 | Task 9 |
| Distributed lock: no two jobs process same SIM | Task 4, Task 5, Task 6 | Task 9 |

## Story-Specific Compliance Rules

- API: Standard envelope `{status, data}` for all responses, 202 for async job creation
- DB: All queries scoped by tenant_id via context or explicit parameter
- Business: State transitions validated per `validTransitions` map — invalid transitions logged in error report, not job failure
- Pattern: Follow OTAProcessor pattern for processor structure, BulkHandler.Import for API handler structure
- Lock: Per-SIM distributed lock key format: `argus:lock:sim:{simID}` with 30s TTL
- eSIM: Physical SIMs skip operator switch (log as skip, not error); `ErrInvalidProfileState` logged as skip

## Bug Pattern Warnings

No matching bug patterns.

## Risks & Mitigations

- **Large segments (100k+ SIMs)**: `ListMatchingSIMIDs` loads all IDs into memory. Mitigated by the segment filter narrowing results; future optimization can add batched cursor queries.
- **Distributed lock contention**: Two bulk jobs on overlapping segments will have SIMs waiting for locks. Mitigated by 30s TTL and retry-on-lock-fail pattern (log error and continue).
- **Undo completeness**: Undo for operator switch requires the old profile to still be in "disabled" state. If manually changed between job and undo, the undo will fail for that SIM (logged in error report).
