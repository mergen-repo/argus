# Implementation Plan: STORY-011 - SIM CRUD & State Machine

## Goal
Implement SIM CRUD operations with a full state machine (ORDERED->ACTIVE<->SUSPENDED->TERMINATED->PURGED + STOLEN/LOST), state transition validation, history logging, IP allocation on activation, and auto-purge scheduling.

## Architecture Context

### Components Involved
- **SIM Store** (`internal/store/sim.go`): Data access layer for sims table (TBL-10) and sim_state_history (TBL-11). Follows `APNStore` pattern in `internal/store/apn.go`.
- **SIM Handler** (`internal/api/sim/handler.go`): HTTP API handlers for SIM CRUD + state transitions. Follows `Handler` pattern in `internal/api/apn/handler.go`.
- **IP Pool Store** (`internal/store/ippool.go`): Existing store — `AllocateIP` and `ReleaseIP` used during activation/termination.
- **Tenant Store** (`internal/store/tenant.go`): Existing store — used to fetch `purge_retention_days` for purge_at calculation.
- **Audit Service** (`internal/audit/audit.go`): Existing `Auditor` interface — creates audit log entries for every state change.
- **Gateway Router** (`internal/gateway/router.go`): Route registration for new SIM endpoints.
- **Main** (`cmd/argus/main.go`): Wire SIM store and handler into the application.

### Data Flow
1. Client sends POST /api/v1/sims with SIM data
2. Gateway middleware: rate limit -> correlation ID -> JWT auth -> role check
3. SIM handler validates request, checks uniqueness
4. SIM store inserts into `sims` table with state=ordered
5. State history entry created in `sim_state_history`
6. Audit entry created via `audit.Auditor`
7. Response returned with standard envelope

State transition flow (e.g., activate):
1. Client sends POST /api/v1/sims/:id/activate
2. Handler fetches SIM, validates current state allows transition
3. Store allocates IP from pool (via IPPoolStore.AllocateIP)
4. Store updates SIM state, activated_at, ip_address_id
5. State history entry created
6. Audit entry created
7. Response returned

### API Specifications

#### API-042: POST /api/v1/sims — Create SIM
- Auth: JWT (sim_manager+)
- Request body:
```json
{
  "iccid": "string (required, max 22)",
  "imsi": "string (required, max 15)",
  "msisdn": "string (optional, max 20)",
  "operator_id": "uuid (required)",
  "apn_id": "uuid (required)",
  "sim_type": "string (required: physical|esim)",
  "rat_type": "string (optional: nb_iot|lte_m|lte|nr_5g)",
  "metadata": "object (optional)"
}
```
- Success 201: `{ status: "success", data: { id, iccid, imsi, msisdn, operator_id, apn_id, sim_type, state: "ordered", ... } }`
- Error 400: INVALID_FORMAT
- Error 409: ICCID_EXISTS or IMSI_EXISTS
- Error 422: VALIDATION_ERROR

#### API-040: GET /api/v1/sims — List SIMs
- Auth: JWT (sim_manager+)
- Query params: `cursor, limit, iccid, imsi, msisdn, operator_id, apn_id, state, rat_type, q` (q = free-text search across iccid/imsi/msisdn)
- Success 200: `{ status: "success", data: [...], meta: { cursor, has_more, limit } }`
- Cursor-based pagination (NOT offset)

#### API-041: GET /api/v1/sims/:id — Get SIM Detail
- Auth: JWT (sim_manager+)
- Success 200: `{ status: "success", data: { id, iccid, imsi, msisdn, ..., ip_address, policy_version_id, esim_profile_id } }`
- Error 404: NOT_FOUND

#### API-044: POST /api/v1/sims/:id/activate — Activate SIM
- Auth: JWT (sim_manager+)
- No request body
- Success 200: `{ status: "success", data: { id, state: "active", ip_address_id, activated_at } }`
- Error 404: NOT_FOUND
- Error 422: INVALID_STATE_TRANSITION (if not in ORDERED state)

#### API-045: POST /api/v1/sims/:id/suspend — Suspend SIM
- Auth: JWT (sim_manager+)
- Request body: `{ "reason": "string (optional)" }`
- Success 200: `{ status: "success", data: { id, state: "suspended" } }`
- Error 422: INVALID_STATE_TRANSITION (if not in ACTIVE state)

#### API-046: POST /api/v1/sims/:id/resume — Resume SIM
- Auth: JWT (sim_manager+)
- No request body
- Success 200: `{ status: "success", data: { id, state: "active" } }`
- Error 422: INVALID_STATE_TRANSITION (if not in SUSPENDED state)

#### API-047: POST /api/v1/sims/:id/terminate — Terminate SIM
- Auth: JWT (tenant_admin)
- Request body: `{ "reason": "string (optional)" }`
- Success 200: `{ status: "success", data: { id, state: "terminated", purge_at } }`
- Error 422: INVALID_STATE_TRANSITION (if not in ACTIVE or SUSPENDED state)

#### API-048: POST /api/v1/sims/:id/report-lost — Report Lost/Stolen
- Auth: JWT (sim_manager+)
- Request body: `{ "reason": "string (optional)" }`
- Success 200: `{ status: "success", data: { id, state: "stolen_lost" } }`
- Error 422: INVALID_STATE_TRANSITION (if not in ACTIVE state)

#### API-050: GET /api/v1/sims/:id/history — Get SIM State History
- Auth: JWT (sim_manager+)
- Query params: `cursor, limit`
- Success 200: `{ status: "success", data: [{ from_state, to_state, reason, triggered_by, user_id, created_at }], meta: { cursor, has_more, limit } }`

### Database Schema

Source: migrations/20260320000002_core_schema.up.sql (ACTUAL)

#### TBL-10: sims (partitioned by operator_id)
```sql
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
    activated_at TIMESTAMPTZ,
    suspended_at TIMESTAMPTZ,
    terminated_at TIMESTAMPTZ,
    purge_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, operator_id)
) PARTITION BY LIST (operator_id);
```

Key indexes:
- `idx_sims_iccid` UNIQUE on (iccid, operator_id)
- `idx_sims_imsi` UNIQUE on (imsi, operator_id)
- `idx_sims_msisdn` on (msisdn) WHERE msisdn IS NOT NULL
- `idx_sims_tenant_state` on (tenant_id, state)
- `idx_sims_tenant_operator` on (tenant_id, operator_id)
- `idx_sims_tenant_apn` on (tenant_id, apn_id)
- `idx_sims_purge` on (purge_at) WHERE state = 'terminated'

**IMPORTANT**: sims table is partitioned by operator_id with composite PK (id, operator_id). Queries must include operator_id or use global unique indexes on iccid/imsi.

#### TBL-11: sim_state_history (partitioned by created_at)
```sql
CREATE TABLE IF NOT EXISTS sim_state_history (
    id BIGSERIAL,
    sim_id UUID NOT NULL,
    from_state VARCHAR(20),
    to_state VARCHAR(20) NOT NULL,
    reason VARCHAR(255),
    triggered_by VARCHAR(20) NOT NULL,
    user_id UUID,
    job_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
```

Index: `idx_sim_state_history_sim` on (sim_id, created_at DESC)

### State Machine Definition

Valid states: `ordered`, `active`, `suspended`, `stolen_lost`, `terminated`, `purged`

Valid transitions:
| From | To | Trigger | Side Effects |
|------|-----|---------|-------------|
| ordered | active | activate | Allocate IP, assign default policy, set activated_at |
| active | suspended | suspend | Set suspended_at, retain IP |
| suspended | active | resume | Clear suspended_at |
| active | stolen_lost | report-lost | Immediate suspension |
| active | terminated | terminate | Schedule IP reclaim, set terminated_at, set purge_at |
| suspended | terminated | terminate | Schedule IP reclaim, set terminated_at, set purge_at |
| terminated | purged | system purge | Release IP, anonymize data |

Invalid transitions return HTTP 422 with code INVALID_STATE_TRANSITION.

## Prerequisites
- [x] STORY-010 completed (APN CRUD, IP pool management, IPPoolStore with AllocateIP/ReleaseIP)
- [x] Core schema migration exists with sims and sim_state_history tables
- [x] Audit service, apierr package, gateway router all in place

## Tasks

### Task 1: SIM Store — Core CRUD
- **Files:** Create `internal/store/sim.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** `internal/store/apn.go`
- **Context refs:** Database Schema, API Specifications > API-042, API Specifications > API-040, API Specifications > API-041
- **What:**
  Create `SIMStore` struct with `pgxpool.Pool` dependency. Define `SIM` struct matching all columns from TBL-10 sims table. Define `SimStateHistory` struct matching TBL-11. Define `CreateSIMParams` struct with fields: ICCID, IMSI, MSISDN (*string), OperatorID, APNID, SimType, RATType (*string), Metadata (json.RawMessage). Implement methods:
  - `Create(ctx, tenantID, params) (*SIM, error)` — INSERT with state=ordered, return created SIM. Handle duplicate key errors for ICCID (ErrICCIDExists) and IMSI (ErrIMSIExists).
  - `GetByID(ctx, tenantID, id) (*SIM, error)` — SELECT by id with tenant_id scope. Note: sims table has composite PK (id, operator_id) but id column has a unique index via the default partition, so querying by id alone works.
  - `List(ctx, tenantID, params) ([]SIM, string, error)` — Cursor-based pagination with filters: iccid (exact), imsi (exact), msisdn (exact), operator_id, apn_id, state, rat_type, q (ILIKE search on iccid/imsi/msisdn). Define `ListSIMsParams` struct for filter params. ORDER BY created_at DESC, id DESC.
  - `ListStateHistory(ctx, simID, cursor, limit) ([]SimStateHistory, string, error)` — Cursor-based pagination on sim_state_history by sim_id, ORDER BY created_at DESC.

  Define error vars: `ErrSIMNotFound`, `ErrICCIDExists`, `ErrIMSIExists`. Use `isDuplicateKeyError` from `errors.go` — distinguish ICCID vs IMSI duplicate by checking the constraint name in the error message (idx_sims_iccid vs idx_sims_imsi).
- **Verify:** `go build ./internal/store/...`

### Task 2: SIM Store — State Machine Operations
- **Files:** Modify `internal/store/sim.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** `internal/store/ippool.go` (transaction pattern from AllocateIP/ReleaseIP)
- **Context refs:** State Machine Definition, Database Schema, API Specifications > API-044, API Specifications > API-045, API Specifications > API-046, API Specifications > API-047, API Specifications > API-048
- **What:**
  Add state transition methods to `SIMStore`. Each method: validates current state, updates SIM, creates sim_state_history entry, all in a transaction.

  Define `validTransitions` map: `map[string][]string` with allowed from->to states per the State Machine Definition.

  Define helper `validateTransition(currentState, targetState string) error` — returns `ErrInvalidStateTransition` if not allowed.

  Methods:
  - `Activate(ctx, tenantID, simID, userID, ipPoolStore) (*SIM, *IPAddress, error)` — Validate ordered->active. In TX: find SIM's APN, get first active IP pool for that APN, call ipPoolStore.AllocateIP (pass the tx or use separate call), update SIM state=active, ip_address_id, activated_at=NOW(). Insert sim_state_history (from=ordered, to=active, triggered_by=user). Return updated SIM + allocated IP.
  - `Suspend(ctx, tenantID, simID, userID, reason) (*SIM, error)` — Validate active->suspended. Update state, suspended_at. Insert history. IP retained (no release).
  - `Resume(ctx, tenantID, simID, userID) (*SIM, error)` — Validate suspended->active. Update state, clear suspended_at. Insert history.
  - `Terminate(ctx, tenantID, simID, userID, reason, purgeRetentionDays) (*SIM, error)` — Validate active/suspended->terminated. Update state, terminated_at=NOW(), purge_at=NOW()+purgeRetentionDays. Insert history. Schedule IP reclaim (set ip_addresses.reclaim_at via direct query).
  - `ReportLost(ctx, tenantID, simID, userID, reason) (*SIM, error)` — Validate active->stolen_lost. Update state. Insert history.

  Each transition method uses a database transaction (s.db.Begin). The SIM fetch inside TX must use FOR UPDATE to prevent concurrent transitions.

  For `Activate`: IP allocation must happen inside the same logical operation. Since `IPPoolStore.AllocateIP` uses its own TX, extract the allocation SQL logic and run it within the SIM activation TX. Alternatively, the handler can call AllocateIP first, then update the SIM — if SIM update fails, release the IP. Use the simpler approach: handler orchestrates (allocate IP, then update SIM in store method that accepts ip_address_id).

  Revised Activate: `Activate(ctx, tenantID, simID, ipAddressID, userID) (*SIM, error)` — takes already-allocated IP ID. Handler does the orchestration.

  Define `ErrInvalidStateTransition` error var.
- **Verify:** `go build ./internal/store/...`

### Task 3: SIM Handler — Create, Get, List, History
- **Files:** Create `internal/api/sim/handler.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** `internal/api/apn/handler.go`
- **Context refs:** API Specifications > API-042, API Specifications > API-040, API Specifications > API-041, API Specifications > API-050, State Machine Definition
- **What:**
  Create `Handler` struct with dependencies: `*store.SIMStore`, `*store.APNStore`, `*store.OperatorStore`, `*store.IPPoolStore`, `*store.TenantStore`, `audit.Auditor`, `zerolog.Logger`.

  Define response structs: `simResponse` (all SIM fields as strings for UUIDs/timestamps), `simHistoryResponse`.
  Define `toSIMResponse(*store.SIM) simResponse` helper.
  Define `toHistoryResponse(*store.SimStateHistory) simHistoryResponse` helper.

  Implement handlers:
  - `Create(w, r)` — Parse+validate request body (iccid required max 22, imsi required max 15, operator_id required uuid, apn_id required uuid, sim_type required enum physical/esim). Verify operator exists and is granted. Verify APN exists and belongs to operator. Call store.Create. Create audit entry (action=sim.create, entity_type=sim). Return 201.
  - `Get(w, r)` — Parse id from URL param. Call store.GetByID. Return 200.
  - `List(w, r)` — Parse query params (cursor, limit, iccid, imsi, msisdn, operator_id, apn_id, state, rat_type, q). Call store.List. Return 200 with ListMeta.
  - `GetHistory(w, r)` — Parse sim id from URL, cursor/limit from query. Call store.ListStateHistory. Return 200 with ListMeta.

  Reuse `userIDFromContext` pattern from apn handler (extract to local func or duplicate).
  Reuse `createAuditEntry` helper pattern from apn handler.

  Add error code constants to `internal/apierr/apierr.go`: `CodeICCIDExists`, `CodeIMSIExists`, `CodeInvalidStateTransition`.
- **Verify:** `go build ./internal/api/sim/...`

### Task 4: SIM Handler — State Transition Endpoints
- **Files:** Modify `internal/api/sim/handler.go`
- **Depends on:** Task 2, Task 3
- **Complexity:** high
- **Pattern ref:** `internal/api/apn/handler.go` (Archive method pattern for state-changing operations)
- **Context refs:** API Specifications > API-044, API Specifications > API-045, API Specifications > API-046, API Specifications > API-047, API Specifications > API-048, State Machine Definition
- **What:**
  Add state transition handler methods to the SIM Handler:

  - `Activate(w, r)` — Parse sim id. Fetch SIM. Fetch SIM's APN to find IP pools. Find first active IP pool for this APN. Call ipPoolStore.AllocateIP(poolID, simID). Call simStore.Activate(tenantID, simID, ipAddressID, userID). If Activate fails, release the allocated IP. Create audit entry (action=sim.activate). Return 200 with updated SIM data including ip_address_id.

  - `Suspend(w, r)` — Parse sim id. Parse optional reason from body. Call simStore.Suspend. Create audit entry (action=sim.suspend). Return 200.

  - `Resume(w, r)` — Parse sim id. Call simStore.Resume. Create audit entry (action=sim.resume). Return 200.

  - `Terminate(w, r)` — Parse sim id. Parse optional reason from body. Fetch tenant to get purge_retention_days. Call simStore.Terminate with purgeRetentionDays. Create audit entry (action=sim.terminate). Return 200 with purge_at in response.

  - `ReportLost(w, r)` — Parse sim id. Parse optional reason from body. Call simStore.ReportLost. Create audit entry (action=sim.report_lost). Return 200.

  Error handling: Map store errors to HTTP responses:
  - ErrSIMNotFound -> 404 NOT_FOUND
  - ErrInvalidStateTransition -> 422 INVALID_STATE_TRANSITION
  - ErrPoolExhausted -> 422 POOL_EXHAUSTED (no IP available during activation)
- **Verify:** `go build ./internal/api/sim/...`

### Task 5: Router + Main Wiring
- **Files:** Modify `internal/gateway/router.go`, Modify `cmd/argus/main.go`, Modify `internal/apierr/apierr.go`
- **Depends on:** Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** `internal/gateway/router.go` (existing APN/IPPool route registration pattern)
- **Context refs:** Architecture Context > Components Involved, API Specifications
- **What:**
  1. Add to `internal/apierr/apierr.go`: `CodeICCIDExists = "ICCID_EXISTS"`, `CodeIMSIExists = "IMSI_EXISTS"`, `CodeInvalidStateTransition = "INVALID_STATE_TRANSITION"`.

  2. Add `SIMHandler` field to `RouterDeps` struct in router.go: `SIMHandler *simapi.Handler`.

  3. Add import alias: `simapi "github.com/btopcu/argus/internal/api/sim"`.

  4. Register SIM routes in `NewRouterWithDeps`:
  ```
  if deps.SIMHandler != nil {
      // sim_manager+ can list, get, search, view history
      r.Group(func(r chi.Router) {
          r.Use(JWTAuth(deps.JWTSecret))
          r.Use(RequireRole("sim_manager"))
          r.Get("/api/v1/sims", deps.SIMHandler.List)
          r.Post("/api/v1/sims", deps.SIMHandler.Create)
          r.Get("/api/v1/sims/{id}", deps.SIMHandler.Get)
          r.Get("/api/v1/sims/{id}/history", deps.SIMHandler.GetHistory)
          r.Post("/api/v1/sims/{id}/activate", deps.SIMHandler.Activate)
          r.Post("/api/v1/sims/{id}/suspend", deps.SIMHandler.Suspend)
          r.Post("/api/v1/sims/{id}/resume", deps.SIMHandler.Resume)
          r.Post("/api/v1/sims/{id}/report-lost", deps.SIMHandler.ReportLost)
      })
      // tenant_admin required for terminate
      r.Group(func(r chi.Router) {
          r.Use(JWTAuth(deps.JWTSecret))
          r.Use(RequireRole("tenant_admin"))
          r.Post("/api/v1/sims/{id}/terminate", deps.SIMHandler.Terminate)
      })
  }
  ```

  5. In `cmd/argus/main.go`:
  - Create `simStore := store.NewSIMStore(pg.Pool)`
  - Create `simHandler := simapi.NewHandler(simStore, apnStore, operatorStore, ippoolStore, tenantStore, auditSvc, log.Logger)`
  - Add `SIMHandler: simHandler` to RouterDeps
- **Verify:** `go build ./cmd/argus/...`

### Task 6: SIM Store Tests
- **Files:** Create `internal/store/sim_test.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** medium
- **Pattern ref:** `internal/store/apn_test.go`
- **Context refs:** Database Schema, State Machine Definition
- **What:**
  Create unit tests for SIM store structs and state machine logic:
  - `TestSIMStructFields` — Verify SIM struct field assignments (all fields including optional pointers)
  - `TestSIMStructNilFields` — Verify nil behavior for optional fields
  - `TestSimStateHistoryStruct` — Verify SimStateHistory struct fields
  - `TestCreateSIMParamsDefaults` — Verify CreateSIMParams with defaults
  - `TestValidTransitions` — Test all valid state transitions from the state machine map
  - `TestInvalidTransitions` — Test invalid transitions (ordered->suspended, ordered->terminated, etc.) return ErrInvalidStateTransition
  - `TestListSIMsParamsStruct` — Verify ListSIMsParams struct fields
- **Verify:** `go test ./internal/store/...`

### Task 7: SIM Handler Tests
- **Files:** Create `internal/api/sim/handler_test.go`
- **Depends on:** Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** `internal/api/apn/handler_test.go`
- **Context refs:** API Specifications, State Machine Definition
- **What:**
  Create unit tests for SIM handler:
  - `TestToSIMResponse` — Verify SIM-to-response conversion including all fields, UUID formatting, timestamp formatting
  - `TestToSIMResponseNilFields` — Verify nil optional fields in response
  - `TestToHistoryResponse` — Verify history entry response conversion
  - `TestValidSIMTypes` — Verify valid sim_type values (physical, esim)
  - `TestValidRATTypes` — Verify valid rat_type values (nb_iot, lte_m, lte, nr_5g)
  - `TestCreateSIMValidation` — Test HTTP handler validation: missing iccid returns 422, missing imsi returns 422, invalid sim_type returns 422
  - `TestListSIMsQueryParsing` — Test query param parsing for List handler
- **Verify:** `go test ./internal/api/sim/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| POST /api/v1/sims creates SIM in ORDERED state | Task 1 (store), Task 3 (handler) | Task 6, Task 7 |
| POST /api/v1/sims/:id/activate -> ORDERED->ACTIVE, allocates IP | Task 2 (store), Task 4 (handler) | Task 6, Task 7 |
| POST /api/v1/sims/:id/suspend -> ACTIVE->SUSPENDED, retains IP | Task 2 (store), Task 4 (handler) | Task 6 |
| POST /api/v1/sims/:id/resume -> SUSPENDED->ACTIVE | Task 2 (store), Task 4 (handler) | Task 6 |
| POST /api/v1/sims/:id/terminate -> TERMINATED, schedules purge | Task 2 (store), Task 4 (handler) | Task 6 |
| POST /api/v1/sims/:id/report-lost -> STOLEN_LOST | Task 2 (store), Task 4 (handler) | Task 6 |
| Invalid transitions return 422 | Task 2 (store validation) | Task 6 |
| Every state transition creates sim_state_history entry | Task 2 (store) | Task 6 |
| GET /api/v1/sims supports combo search | Task 1 (store), Task 3 (handler) | Task 7 |
| Cursor-based pagination | Task 1 (store) | Task 7 |
| GET /api/v1/sims/:id returns full detail | Task 1 (store), Task 3 (handler) | Task 7 |
| ICCID and IMSI globally unique | Task 1 (store error handling) | Task 6 |
| purge_at = terminated_at + purge_retention_days | Task 2 (store) | Task 6 |
| Audit log entry for every state change | Task 3+4 (handler audit calls) | Task 7 |

## Story-Specific Compliance Rules

- API: Standard envelope `{ status, data, meta? }` via apierr.WriteSuccess/WriteList/WriteError
- DB: Existing migration covers sims + sim_state_history tables. No new migrations needed.
- Auth: sim_manager+ for most ops, tenant_admin for terminate
- Business: purge_at = terminated_at + tenant.purge_retention_days
- Convention: Cursor-based pagination, tenant_id scoping on all queries
- State machine: Strict transition validation, no skipping states

## Risks & Mitigations

- **Partitioned sims table**: The composite PK (id, operator_id) means some queries need care. Mitigation: Use the global unique indexes on iccid/imsi for lookups. For GetByID, query by id + tenant_id which works across partitions.
- **IP allocation + SIM activation atomicity**: Two separate stores involved. Mitigation: Handler orchestrates — allocate IP first, then activate SIM. If SIM activation fails, release IP in error path.
- **Concurrent state transitions**: Two requests could race on the same SIM. Mitigation: SELECT ... FOR UPDATE in transaction to serialize access.
