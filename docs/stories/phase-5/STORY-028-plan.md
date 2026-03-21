# Implementation Plan: STORY-028 - eSIM Profile Management

## Goal
Deliver eSIM profile CRUD with lifecycle management (enable/disable/switch), SM-DP+ adapter interface, and 5 API endpoints for remote eSIM-capable device control.

## Architecture Context

### Components Involved
- **SVC-03 (Core API)**: HTTP handlers in `internal/api/esim/` — new package
- **Store layer**: `internal/store/esim.go` — new file, PostgreSQL data access for TBL-12 `esim_profiles`
- **SM-DP+ Adapter**: `internal/esim/smdp.go` — new adapter interface + mock implementation
- **Gateway Router**: `internal/gateway/router.go` — register eSIM routes
- **Main**: `cmd/argus/main.go` — wire new handler
- **Audit**: `internal/audit/` — existing audit service (Auditor interface)
- **Error Codes**: `internal/apierr/apierr.go` — add eSIM-specific error codes

### Data Flow
```
HTTP Request → Gateway (JWT auth, role check: sim_manager+)
  → eSIM Handler (validate input, parse IDs)
  → eSIM Store (PostgreSQL query with tenant_id scoping via SIM join)
  → [For enable/disable/switch] SM-DP+ Adapter call (mock in v1)
  → [For switch] Transaction: disable old profile + enable new + update SIM record
  → Audit log entry
  → Standard envelope response
```

### API Specifications

**API-070: GET /api/v1/esim-profiles**
- Query params: `?cursor&limit&sim_id&operator_id&state`
- Success: `{ status: "success", data: [{id, sim_id, eid, iccid_on_profile, operator_id, profile_state, sm_dp_plus_id, created_at, updated_at}], meta: {cursor, limit, has_more} }`
- Status: 200
- Auth: JWT (sim_manager+)
- Tenant scoping: JOIN sims ON esim_profiles.sim_id = sims.id WHERE sims.tenant_id = $tenant_id

**API-071: GET /api/v1/esim-profiles/:id**
- Success: `{ status: "success", data: {id, sim_id, eid, iccid_on_profile, operator_id, profile_state, sm_dp_plus_id, last_provisioned_at, last_error, created_at, updated_at} }`
- Error: 404 NOT_FOUND
- Auth: JWT (sim_manager+)

**API-072: POST /api/v1/esim-profiles/:id/enable**
- No request body
- Validates: profile exists, SIM is eSIM type, profile_state in (disabled), no other profile enabled for same SIM
- Success: `{ status: "success", data: {id, profile_state: "enabled"} }`
- Errors: 404 NOT_FOUND, 422 PROFILE_ALREADY_ENABLED / NOT_ESIM / INVALID_PROFILE_STATE
- Side effects: SM-DP+ EnableProfile(), audit log, sim_state_history entry
- Auth: JWT (sim_manager+)

**API-073: POST /api/v1/esim-profiles/:id/disable**
- No request body
- Validates: profile exists, profile_state == "enabled"
- Success: `{ status: "success", data: {id, profile_state: "disabled"} }`
- Errors: 404 NOT_FOUND, 422 INVALID_PROFILE_STATE
- Side effects: SM-DP+ DisableProfile(), audit log, sim_state_history entry
- Auth: JWT (sim_manager+)

**API-074: POST /api/v1/esim-profiles/:id/switch**
- Request body: `{ "target_profile_id": "uuid" }`
- Validates: both profiles exist, both belong to same SIM, SIM is eSIM, source is enabled, target is disabled
- Atomic TX: disable current + enable target + update sims.operator_id + update sims.apn_id (nullable) + update sims.esim_profile_id
- Success: `{ status: "success", data: {sim_id, old_profile: {id, profile_state}, new_profile: {id, profile_state}, new_operator_id} }`
- Errors: 404 NOT_FOUND, 422 INVALID_PROFILE_STATE / SAME_PROFILE / DIFFERENT_SIM
- Side effects: SM-DP+ DisableProfile() + EnableProfile(), CoA/DM if active session, audit log, sim_state_history
- Auth: JWT (sim_manager+)

### Database Schema

```sql
-- Source: migrations/20260320000002_core_schema.up.sql (ACTUAL)
CREATE TABLE IF NOT EXISTS esim_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL UNIQUE,
    eid VARCHAR(32) NOT NULL,
    sm_dp_plus_id VARCHAR(255),
    operator_id UUID NOT NULL REFERENCES operators(id),
    profile_state VARCHAR(20) NOT NULL DEFAULT 'disabled',
    iccid_on_profile VARCHAR(22),
    last_provisioned_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_esim_profiles_sim ON esim_profiles (sim_id);
CREATE INDEX IF NOT EXISTS idx_esim_profiles_eid ON esim_profiles (eid);
CREATE INDEX IF NOT EXISTS idx_esim_profiles_operator ON esim_profiles (operator_id);
```

Note: `sim_id` has UNIQUE constraint but NOT a FK to sims (sims is partitioned). Tenant scoping achieved by JOINing with sims table.

Profile state machine: `disabled` → `enabled` ↔ `disabled` → `deleted`

### SIM Table Reference (TBL-10)
- `esim_profile_id UUID` — FK to esim_profiles.id (nullable, set when profile is active)
- `sim_type VARCHAR(10)` — must be 'esim' for eSIM operations
- `operator_id UUID` — updated on switch to match new profile's operator

## Prerequisites
- [x] STORY-011 completed (SIM CRUD — provides SIMStore, SIM model, sim_state_history)
- [x] STORY-010 completed (APN/IP pool — provides APNStore, IPPoolStore)
- [x] STORY-009 completed (Operator CRUD — provides OperatorStore, operator adapter registry)
- [x] Migration file exists with esim_profiles table (20260320000002_core_schema.up.sql)

## Tasks

### Task 1: eSIM Store — data access layer
- **Files:** Create `internal/store/esim.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/store/sim.go` — follow same store structure (struct, NewXxxStore, error vars, scan function, CRUD methods)
- **Context refs:** Database Schema, API Specifications
- **What:**
  Create `ESimProfileStore` with these methods:
  - `ESimProfile` struct matching TBL-12 columns: ID (uuid.UUID), SimID (uuid.UUID), EID (string), SMDPPlusID (*string), OperatorID (uuid.UUID), ProfileState (string), ICCIDOnProfile (*string), LastProvisionedAt (*time.Time), LastError (*string), CreatedAt (time.Time), UpdatedAt (time.Time)
  - Error vars: `ErrESimProfileNotFound`, `ErrProfileAlreadyEnabled`, `ErrInvalidProfileState`
  - `NewESimProfileStore(db *pgxpool.Pool) *ESimProfileStore`
  - `GetByID(ctx, tenantID uuid.UUID, id uuid.UUID) (*ESimProfile, error)` — JOIN sims for tenant scoping: `SELECT ep.* FROM esim_profiles ep JOIN sims s ON ep.sim_id = s.id WHERE ep.id = $1 AND s.tenant_id = $2`
  - `List(ctx, tenantID uuid.UUID, params ListESimProfilesParams) ([]ESimProfile, string, error)` — cursor-based pagination with filters (sim_id, operator_id, state), JOIN sims for tenant scoping
  - `ListESimProfilesParams` struct: Cursor, Limit, SimID (*uuid.UUID), OperatorID (*uuid.UUID), State (string)
  - `Enable(ctx, tenantID, profileID uuid.UUID, userID *uuid.UUID) (*ESimProfile, error)` — in TX: check no other enabled profile for same SIM, validate state transition (disabled→enabled), update profile_state, insert sim_state_history, update sims.esim_profile_id
  - `Disable(ctx, tenantID, profileID uuid.UUID, userID *uuid.UUID) (*ESimProfile, error)` — in TX: validate state (enabled→disabled), update profile_state, insert sim_state_history, clear sims.esim_profile_id
  - `Switch(ctx, tenantID, sourceProfileID, targetProfileID uuid.UUID, userID *uuid.UUID) (*SwitchResult, error)` — in TX: validate both profiles belong to same SIM, disable source, enable target, update sims (operator_id, esim_profile_id, apn_id=NULL), insert sim_state_history entries
  - `SwitchResult` struct: SimID, OldProfile, NewProfile *ESimProfile, NewOperatorID uuid.UUID
  - `GetEnabledProfileForSIM(ctx, simID uuid.UUID) (*ESimProfile, error)` — find currently enabled profile for a SIM
  - Helper: `esimProfileColumns` string, `scanESimProfile(row pgx.Row) (*ESimProfile, error)`
  - Use `FOR UPDATE` row locks in Enable/Disable/Switch transactions
- **Verify:** `go build ./internal/store/...`

### Task 2: SM-DP+ adapter interface and mock
- **Files:** Create `internal/esim/smdp.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/operator/adapter/types.go` and `internal/operator/adapter/mock.go` — follow interface + mock pattern
- **Context refs:** API Specifications (SM-DP+ operations)
- **What:**
  Define the SM-DP+ adapter interface and mock implementation in a single file:
  - `SMDPAdapter` interface with 4 methods:
    - `DownloadProfile(ctx context.Context, req DownloadProfileRequest) (*DownloadProfileResponse, error)`
    - `EnableProfile(ctx context.Context, req EnableProfileRequest) error`
    - `DisableProfile(ctx context.Context, req DisableProfileRequest) error`
    - `DeleteProfile(ctx context.Context, req DeleteProfileRequest) error`
  - Request/response types for each method (EID, ICCID, SMDPPlusID, OperatorID fields)
  - `MockSMDPAdapter` struct implementing the interface — all methods succeed with a configurable latency (default 50ms sleep), log calls via zerolog
  - `NewMockSMDPAdapter(logger zerolog.Logger) *MockSMDPAdapter`
  - Error vars: `ErrSMDPConnectionFailed`, `ErrSMDPProfileNotFound`, `ErrSMDPOperationFailed`
- **Verify:** `go build ./internal/esim/...`

### Task 3: eSIM API handler
- **Files:** Create `internal/api/esim/handler.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/api/sim/handler.go` — follow same handler structure (Handler struct, NewHandler, request/response types, handler methods, audit logging, error handling, userIDFromCtx)
- **Context refs:** API Specifications, Architecture Context > Data Flow
- **What:**
  Create the eSIM handler with 5 endpoint methods:
  - `Handler` struct with dependencies: esimStore (*store.ESimProfileStore), simStore (*store.SIMStore), smdpAdapter (esim.SMDPAdapter), auditSvc (audit.Auditor), logger (zerolog.Logger)
  - `NewHandler(...)` constructor
  - Response types:
    - `profileResponse` — JSON struct for list/get responses
    - `switchResponse` — JSON struct for switch result
  - `toProfileResponse(p *store.ESimProfile) profileResponse` mapper
  - **List** handler: parse query params (cursor, limit, sim_id, operator_id, state), call store.List, return standard list response
  - **Get** handler: parse `{id}` from URL, call store.GetByID, return standard success response
  - **Enable** handler: parse `{id}`, get profile, verify SIM is eSIM type via simStore.GetByID, call smdpAdapter.EnableProfile (log errors but don't fail — SM-DP+ is placeholder), call store.Enable, create audit entry (action: "esim_profile.enable"), return updated profile
  - **Disable** handler: parse `{id}`, get profile, call smdpAdapter.DisableProfile, call store.Disable, create audit entry (action: "esim_profile.disable"), return updated profile
  - **Switch** handler: parse `{id}` (source profile), decode body for target_profile_id, validate both exist and belong to same SIM, call smdpAdapter.DisableProfile + EnableProfile, call store.Switch, create audit entry (action: "esim_profile.switch"), return switch result
  - Use apierr error codes: NOT_FOUND, PROFILE_ALREADY_ENABLED, NOT_ESIM, INVALID_PROFILE_STATE, SAME_PROFILE, DIFFERENT_SIM
  - `createAuditEntry` private method — same pattern as sim handler
  - `userIDFromCtx` helper — same pattern as sim handler
- **Verify:** `go build ./internal/api/esim/...`

### Task 4: Error codes for eSIM
- **Files:** Modify `internal/apierr/apierr.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/apierr/apierr.go` — follow existing code const pattern
- **Context refs:** API Specifications
- **What:**
  Add new error code constants:
  - `CodeProfileAlreadyEnabled = "PROFILE_ALREADY_ENABLED"`
  - `CodeNotESIM = "NOT_ESIM"`
  - `CodeInvalidProfileState = "INVALID_PROFILE_STATE"`
  - `CodeSameProfile = "SAME_PROFILE"`
  - `CodeDifferentSIM = "DIFFERENT_SIM"`
- **Verify:** `go build ./internal/apierr/...`

### Task 5: Gateway router + main.go wiring
- **Files:** Modify `internal/gateway/router.go`, Modify `cmd/argus/main.go`
- **Depends on:** Task 1, Task 2, Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — follow existing handler registration pattern (RouterDeps field, nil guard, route group with JWT+RequireRole)
- **Context refs:** Architecture Context > Components Involved, API Specifications
- **What:**
  **router.go:**
  - Add import for `esimapi "github.com/btopcu/argus/internal/api/esim"`
  - Add `ESimHandler *esimapi.Handler` field to `RouterDeps` struct
  - Add route group block (nil guard pattern like SIMHandler):
    ```
    if deps.ESimHandler != nil {
      r.Group(func(r chi.Router) {
        r.Use(JWTAuth(deps.JWTSecret))
        r.Use(RequireRole("sim_manager"))
        r.Get("/api/v1/esim-profiles", deps.ESimHandler.List)
        r.Get("/api/v1/esim-profiles/{id}", deps.ESimHandler.Get)
        r.Post("/api/v1/esim-profiles/{id}/enable", deps.ESimHandler.Enable)
        r.Post("/api/v1/esim-profiles/{id}/disable", deps.ESimHandler.Disable)
        r.Post("/api/v1/esim-profiles/{id}/switch", deps.ESimHandler.Switch)
      })
    }
    ```

  **main.go:**
  - Add imports: `esimapi "github.com/btopcu/argus/internal/api/esim"` and `"github.com/btopcu/argus/internal/esim"`
  - After simStore creation, create: `esimStore := store.NewESimProfileStore(pg.Pool)`
  - Create mock SM-DP+ adapter: `smdpAdapter := esim.NewMockSMDPAdapter(log.Logger)`
  - Create handler: `esimHandler := esimapi.NewHandler(esimStore, simStore, smdpAdapter, auditSvc, log.Logger)`
  - Add to RouterDeps: `ESimHandler: esimHandler`
- **Verify:** `go build ./cmd/argus/...`

### Task 6: eSIM store tests
- **Files:** Create `internal/store/esim_test.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/sim_test.go` — follow same test structure (TestMain, setup helpers, table-driven tests)
- **Context refs:** Database Schema, API Specifications
- **What:**
  Write unit tests for the eSIM store (using pgxpool mock or test helpers from existing tests):
  - Test List with filters (sim_id, operator_id, state)
  - Test GetByID — found and not found cases
  - Test Enable — success case (disabled→enabled), already enabled case (422), invalid state case
  - Test Disable — success (enabled→disabled), invalid state case
  - Test Switch — success (disable old + enable new), same profile error, different SIM error
  - Test tenant scoping — profile only accessible if SIM belongs to tenant
  - Follow existing test patterns: use `testify/assert` and `testify/require`
- **Verify:** `go test ./internal/store/... -run TestESimProfile -count=1`

### Task 7: eSIM handler tests
- **Files:** Create `internal/api/esim/handler_test.go`
- **Depends on:** Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/sim/handler_test.go` — follow same handler test structure (httptest, chi context, mock stores)
- **Context refs:** API Specifications, Architecture Context > Data Flow
- **What:**
  Write handler tests:
  - Test List endpoint — returns standard list response
  - Test Get endpoint — found returns 200, not found returns 404
  - Test Enable — success 200, already enabled 422, not eSIM 422, not found 404
  - Test Disable — success 200, invalid state 422, not found 404
  - Test Switch — success 200, same profile 422, different SIM 422, not found 404
  - Test audit log creation on state changes
  - Use httptest.NewRecorder, chi.NewRouter for test routing
  - Use mock store implementations
- **Verify:** `go test ./internal/api/esim/... -count=1`

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| GET /api/v1/esim-profiles lists with filters | Task 1, Task 3 | Task 6, Task 7 |
| GET /api/v1/esim-profiles/:id returns detail | Task 1, Task 3 | Task 6, Task 7 |
| POST /:id/enable enables profile | Task 1, Task 3 | Task 6, Task 7 |
| POST /:id/disable disables profile | Task 1, Task 3 | Task 6, Task 7 |
| POST /:id/switch switches profiles | Task 1, Task 3 | Task 6, Task 7 |
| Only one profile per SIM enabled | Task 1 (Enable TX check) | Task 6 |
| Switch is atomic (single TX) | Task 1 (Switch method) | Task 6 |
| Switch triggers operator/APN update on SIM | Task 1 (Switch method) | Task 6 |
| SM-DP+ adapter interface defined | Task 2 | Build check |
| SM-DP+ mock adapter | Task 2 | Build check |
| Profile operations create audit log | Task 3 | Task 7 |
| Profile operations create sim_state_history | Task 1 | Task 6 |

## Story-Specific Compliance Rules
- **API:** Standard envelope `{ status, data, meta? }` for all responses
- **DB:** Tenant scoping via JOIN sims (esim_profiles has no tenant_id column — scoping through sim_id→sims.tenant_id)
- **DB:** esim_profiles table already exists in migration — no new migration needed
- **Audit:** All state-changing operations (enable, disable, switch) create audit log entries
- **Business:** Only one profile per SIM can be enabled at a time (enforced in store layer TX)
- **Business:** Switch is atomic — disable old + enable new in single transaction
- **ADR-001:** All code in internal/ packages within single binary

## Risks & Mitigations
- **SM-DP+ is placeholder**: Mock adapter simulates operations. Interface is ready for real operator-specific implementations. No external API calls in v1.
- **CoA/DM on switch**: Story mentions triggering CoA/DM on active sessions during switch. This requires session management integration (STORY-017). The handler will call CoA/DM if session handler is available, but won't fail if it's not wired — graceful degradation.
- **Partitioned sims table**: esim_profiles.sim_id cannot have FK to sims (partitioned). Integrity maintained by application-level validation (GetByID on SIM before profile operations).
