# Implementation Plan: STORY-010 — APN CRUD & IP Pool Management

## Goal
Implement full APN lifecycle (create, read, update, archive) and IP pool management (create pool, auto-generate addresses, allocate, reserve static IPs, utilization monitoring, reclaim logic) with dual-stack IPv4+IPv6 support.

## Architecture Context

### Components Involved
- **SVC-03 (Core API)**: HTTP API handlers for APN and IP Pool CRUD — `internal/api/apn/handler.go`, `internal/api/ippool/handler.go`
- **Store Layer**: Database access — `internal/store/apn.go`, `internal/store/ippool.go`
- **Gateway**: Route registration — `internal/gateway/router.go`
- **Main**: Dependency wiring — `cmd/argus/main.go`
- **API Error**: Standard envelope responses — `internal/apierr/apierr.go`
- **Audit**: Audit log integration — `internal/audit/service.go`

### Data Flow

```
APN Create:
  POST /api/v1/apns → JWTAuth → RequireRole(tenant_admin) → apn.Handler.Create
    → validate request fields
    → verify operator exists (operatorStore.GetByID)
    → verify operator grant exists for tenant (operatorStore.ListGrants filtered)
    → apnStore.Create(tenantID, params)
    → audit entry "apn.create"
    → 201 { status: "success", data: { apn } }

IP Pool Create:
  POST /api/v1/ip-pools → JWTAuth → RequireRole(tenant_admin) → ippool.Handler.Create
    → validate request fields
    → verify APN exists and belongs to tenant
    → parse CIDR → compute total usable addresses
    → ippoolStore.Create(tenantID, params)
    → bulk insert ip_addresses rows for each IP in CIDR
    → 201 { status: "success", data: { pool } }

IP Reserve:
  POST /api/v1/ip-pools/:id/addresses/reserve → JWTAuth → RequireRole(sim_manager)
    → validate sim_id and optional address_v4
    → ippoolStore.ReserveStaticIP(poolID, simID, addressV4)
    → UPDATE ip_addresses SET state='reserved', allocation_type='static', sim_id=...
    → 201 { status: "success", data: { ip_address } }
```

### API Specifications

#### APN Endpoints

**API-030: GET /api/v1/apns** — List APNs (cursor-paginated)
- Query params: `cursor`, `limit` (default 50, max 100), `state` (optional filter), `operator_id` (optional filter)
- Auth: JWT (sim_manager+)
- Scoped by tenant_id from JWT context
- Success: 200 `{ status: "success", data: [...], meta: { cursor, limit, has_more } }`

**API-031: POST /api/v1/apns** — Create APN
- Request body: `{ name: string, operator_id: UUID, apn_type: string, supported_rat_types: string[], display_name?: string, default_policy_id?: UUID, settings?: object }`
- Auth: JWT (tenant_admin+)
- Validation: name required, operator_id required (must exist + granted to tenant), apn_type in [private_managed, operator_managed, customer_managed]
- Unique constraint: (tenant_id, operator_id, name)
- Success: 201 `{ status: "success", data: { id, tenant_id, operator_id, name, display_name, apn_type, ... } }`
- Errors: 400 INVALID_FORMAT, 409 ALREADY_EXISTS, 422 VALIDATION_ERROR

**API-032: GET /api/v1/apns/:id** — Get APN detail
- Auth: JWT (sim_manager+)
- Scoped by tenant_id
- Success: 200 `{ status: "success", data: { ...apn, pool_count, sim_count } }`
- Errors: 404 NOT_FOUND

**API-033: PATCH /api/v1/apns/:id** — Update APN
- Request body: `{ display_name?: string, supported_rat_types?: string[], default_policy_id?: UUID, settings?: object }`
- Auth: JWT (tenant_admin+)
- Success: 200 `{ status: "success", data: { ...apn } }`
- Errors: 404 NOT_FOUND, 422 VALIDATION_ERROR

**API-034: DELETE /api/v1/apns/:id** — Archive APN (soft-delete)
- Auth: JWT (tenant_admin)
- Business rule: If APN has active SIMs → 422 APN_HAS_ACTIVE_SIMS
- If no active SIMs → set state = 'archived', return 204
- Errors: 404 NOT_FOUND, 422 APN_HAS_ACTIVE_SIMS

#### IP Pool Endpoints

**API-080: GET /api/v1/ip-pools** — List IP pools
- Query params: `cursor`, `limit`, `apn_id` (optional filter)
- Auth: JWT (operator_manager+)
- Scoped by tenant_id
- Success: 200 `{ status: "success", data: [...], meta: { cursor, limit, has_more } }`

**API-081: POST /api/v1/ip-pools** — Create IP pool
- Request body: `{ apn_id: UUID, name: string, cidr_v4?: string, cidr_v6?: string, alert_threshold_warning?: int, alert_threshold_critical?: int, reclaim_grace_period_days?: int }`
- Auth: JWT (tenant_admin+)
- Validation: apn_id required (must exist + belong to tenant), name required, at least one of cidr_v4/cidr_v6 required
- On create: parse CIDR, compute total_addresses, bulk-insert individual ip_addresses rows
- For IPv4 /24: 254 usable addresses (skip network + broadcast)
- Success: 201 `{ status: "success", data: { id, name, apn_id, total_addresses, used_addresses, ... } }`
- Errors: 400 INVALID_FORMAT, 422 VALIDATION_ERROR

**API-082: GET /api/v1/ip-pools/:id** — Get pool detail + utilization
- Auth: JWT (operator_manager+)
- Success: 200 `{ status: "success", data: { ...pool, utilization_pct } }`

**API-083: PATCH /api/v1/ip-pools/:id** — Update pool settings
- Request body: `{ name?: string, alert_threshold_warning?: int, alert_threshold_critical?: int, reclaim_grace_period_days?: int, state?: string }`
- Auth: JWT (tenant_admin+)
- Success: 200 `{ status: "success", data: { ...pool } }`

**API-084: GET /api/v1/ip-pools/:id/addresses** — List addresses in pool
- Query params: `cursor`, `limit`, `state` (optional filter: available, allocated, reserved, reclaiming)
- Auth: JWT (operator_manager+)
- Success: 200 `{ status: "success", data: [...], meta: { cursor, limit, has_more } }`

**API-085: POST /api/v1/ip-pools/:id/addresses/reserve** — Reserve static IP
- Request body: `{ sim_id: UUID, address_v4?: string }`
- Auth: JWT (sim_manager+)
- If address_v4 provided → reserve that specific IP (must be available)
- If not provided → allocate next available as static
- Success: 201 `{ status: "success", data: { id, address_v4, sim_id, allocation_type: "static", state: "reserved" } }`
- Errors: 409 IP_ALREADY_ALLOCATED, 422 POOL_EXHAUSTED

### Database Schema

Source: `migrations/20260320000002_core_schema.up.sql` (ACTUAL — tables already exist)

```sql
-- TBL-07: apns
CREATE TABLE IF NOT EXISTS apns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID NOT NULL REFERENCES operators(id),
    name VARCHAR(100) NOT NULL,
    display_name VARCHAR(255),
    apn_type VARCHAR(20) NOT NULL,
    supported_rat_types VARCHAR[] NOT NULL DEFAULT '{}',
    default_policy_id UUID REFERENCES policies(id),
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id),
    updated_by UUID REFERENCES users(id)
);
-- Indexes:
-- idx_apns_tenant_name UNIQUE on (tenant_id, operator_id, name)
-- idx_apns_tenant_state on (tenant_id, state)
-- idx_apns_operator on (operator_id)

-- TBL-08: ip_pools
CREATE TABLE IF NOT EXISTS ip_pools (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    apn_id UUID NOT NULL REFERENCES apns(id),
    name VARCHAR(100) NOT NULL,
    cidr_v4 CIDR,
    cidr_v6 CIDR,
    total_addresses INTEGER NOT NULL DEFAULT 0,
    used_addresses INTEGER NOT NULL DEFAULT 0,
    alert_threshold_warning INTEGER NOT NULL DEFAULT 80,
    alert_threshold_critical INTEGER NOT NULL DEFAULT 90,
    reclaim_grace_period_days INTEGER NOT NULL DEFAULT 7,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Indexes:
-- idx_ip_pools_tenant_apn on (tenant_id, apn_id)
-- idx_ip_pools_apn on (apn_id)

-- TBL-09: ip_addresses
CREATE TABLE IF NOT EXISTS ip_addresses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pool_id UUID NOT NULL REFERENCES ip_pools(id),
    address_v4 INET,
    address_v6 INET,
    allocation_type VARCHAR(10) NOT NULL DEFAULT 'dynamic',
    sim_id UUID,
    state VARCHAR(20) NOT NULL DEFAULT 'available',
    allocated_at TIMESTAMPTZ,
    reclaim_at TIMESTAMPTZ
);
-- Indexes:
-- idx_ip_addresses_pool_state on (pool_id, state)
-- idx_ip_addresses_sim on (sim_id) WHERE sim_id IS NOT NULL
-- idx_ip_addresses_v4 UNIQUE on (pool_id, address_v4) WHERE address_v4 IS NOT NULL
-- idx_ip_addresses_v6 UNIQUE on (pool_id, address_v6) WHERE address_v6 IS NOT NULL
-- idx_ip_addresses_reclaim on (reclaim_at) WHERE state = 'reclaiming'
```

**CRITICAL pgx type handling:**
- `CIDR` columns: use `::text` cast in SELECT, `::cidr` cast in INSERT/UPDATE
- `INET` columns: use `::text` cast in SELECT, `::inet` cast in INSERT/UPDATE
- `TIMESTAMPTZ` columns: use `time.Time` / `*time.Time` in Go structs (NOT string)

### IP Address Generation Algorithm

When creating an IP pool with CIDR:

**IPv4 (e.g., 10.0.0.0/24):**
1. Parse CIDR to get network address and prefix length
2. Calculate total IPs: 2^(32 - prefix) - 2 (skip network and broadcast for /24 and smaller)
3. For /31: 2 usable addresses (point-to-point, no network/broadcast skip)
4. For /32: 1 usable address
5. Generate each IP in range, insert as individual `ip_addresses` row with state='available'

**IPv6 (e.g., 2001:db8::/120):**
1. Parse CIDR
2. Calculate total IPs: 2^(128 - prefix)
3. Cap at 65536 addresses per pool (to prevent massive inserts)
4. Generate each IP, insert as individual `ip_addresses` row

### IP Allocation Algorithm (from ALGORITHMS.md)

```
FUNCTION allocate_ip(pool_id, sim_id) → (ip_address, error)
1. BEGIN TRANSACTION (SERIALIZABLE isolation)
2. SELECT pool FROM ip_pools WHERE id = pool_id FOR UPDATE
3. IF pool.state = 'exhausted' OR pool.state = 'disabled': RETURN error POOL_EXHAUSTED
4. SELECT first available IP: ORDER BY address_v4 ASC LIMIT 1 FOR UPDATE SKIP LOCKED
5. IF no IP found: UPDATE ip_pools SET state = 'exhausted', RETURN error POOL_EXHAUSTED
6. UPDATE ip_addresses SET state = 'allocated', sim_id = sim_id, allocated_at = NOW()
7. UPDATE ip_pools SET used_addresses = used_addresses + 1
8. CHECK utilization thresholds (warning/critical alerts)
9. COMMIT
```

## Prerequisites
- [x] STORY-009 completed (operators table, operator_grants table, OperatorStore with GetByID/ListGrants)
- [x] Tables apns, ip_pools, ip_addresses already exist in core_schema migration
- [x] Standard API envelope (apierr package) available
- [x] Audit service available
- [x] Gateway router pattern established

## Tasks

### Task 1: APN Store — Data Access Layer
- **Files:** Create `internal/store/apn.go` (replace stubs)
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/store/operator.go` — follow same store structure (struct, params, scan, CRUD methods)
- **Context refs:** Database Schema, API Specifications > APN Endpoints
- **What:**
  Replace the stub `APNStore` in `internal/store/stubs.go` with a full implementation in `internal/store/apn.go`. Remove the APN-related stubs from stubs.go.

  Implement:
  - `APN` struct with all columns from TBL-07: id (UUID), tenant_id (UUID), operator_id (UUID), name (string), display_name (*string), apn_type (string), supported_rat_types ([]string), default_policy_id (*UUID), state (string), settings (json.RawMessage), created_at (time.Time), updated_at (time.Time), created_by (*UUID), updated_by (*UUID)
  - `CreateAPNParams` struct: Name, OperatorID, APNType, SupportedRATTypes, DisplayName, DefaultPolicyID, Settings
  - `UpdateAPNParams` struct: DisplayName, SupportedRATTypes, DefaultPolicyID, Settings (all optional/pointer)
  - Error sentinels: `ErrAPNNotFound`, `ErrAPNNameExists`, `ErrAPNHasActiveSIMs`
  - Methods: `Create(ctx, tenantID, params)`, `GetByID(ctx, tenantID, id)`, `List(ctx, tenantID, cursor, limit, stateFilter, operatorIDFilter)`, `Update(ctx, tenantID, id, params)`, `Archive(ctx, tenantID, id)` (checks active SIM count first via: `SELECT COUNT(*) FROM sims WHERE apn_id = $1 AND state NOT IN ('terminated', 'purged')`)
  - `GetByName(ctx, tenantID, operatorID, name)` — needed by SIM import
  - `CountByTenant(ctx, tenantID)` — for tenant limit checks
  - All queries scoped by tenant_id
  - Use `::text` cast for any INET/CIDR reads if needed in JOIN scenarios
  - Cursor-based pagination using created_at DESC, id DESC ordering (same as operator store)
- **Verify:** `go build ./internal/store/...`

### Task 2: IP Pool & IP Address Store — Data Access Layer
- **Files:** Create `internal/store/ippool.go` (replace stubs)
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/store/operator.go` — follow same store structure
- **Context refs:** Database Schema, IP Address Generation Algorithm, IP Allocation Algorithm, API Specifications > IP Pool Endpoints
- **What:**
  Replace the stub `IPPoolStore` in `internal/store/stubs.go` with a full implementation in `internal/store/ippool.go`. Remove IP pool/address stubs from stubs.go.

  Implement types:
  - `IPPool` struct with all columns from TBL-08: id, tenant_id, apn_id, name, cidr_v4 (*string), cidr_v6 (*string), total_addresses, used_addresses, alert_threshold_warning, alert_threshold_critical, reclaim_grace_period_days, state, created_at. Use `*string` for CIDR columns (read with `::text` cast).
  - `IPAddress` struct with all columns from TBL-09: id, pool_id, address_v4 (*string), address_v6 (*string), allocation_type, sim_id (*UUID), state, allocated_at (*time.Time), reclaim_at (*time.Time). Use `*string` for INET columns (read with `::text` cast).
  - `CreateIPPoolParams`: APNID, Name, CIDRv4, CIDRv6, AlertThresholdWarning, AlertThresholdCritical, ReclaimGracePeriodDays
  - `UpdateIPPoolParams`: Name, AlertThresholdWarning, AlertThresholdCritical, ReclaimGracePeriodDays, State (all optional/pointer)

  Implement IPPoolStore methods:
  - `Create(ctx, tenantID, params)` — insert pool, then bulk-insert ip_addresses. Use `::cidr` cast for CIDR inserts, `::inet` cast for address inserts. Return the created pool.
  - `GetByID(ctx, tenantID, id)` — with `cidr_v4::text, cidr_v6::text` casts
  - `List(ctx, tenantID, cursor, limit, apnIDFilter)` — cursor-paginated
  - `Update(ctx, tenantID, id, params)`
  - `ListAddresses(ctx, poolID, cursor, limit, stateFilter)` — list ip_addresses for a pool with `address_v4::text, address_v6::text` casts
  - `ReserveStaticIP(ctx, poolID, simID, addressV4)` — if addressV4 provided, find that specific available IP; else find next available. Set state='reserved', allocation_type='static', sim_id=simID. Increment used_addresses.
  - `AllocateIP(ctx, poolID, simID)` — per allocation algorithm: SELECT ... FOR UPDATE SKIP LOCKED, update state='allocated', increment used_addresses, check thresholds
  - `ReleaseIP(ctx, poolID, simID)` — per release algorithm
  - Helper: `generateIPv4Addresses(cidr string) ([]string, error)` — parse CIDR, return list of usable IP strings
  - Helper: `generateIPv6Addresses(cidr string) ([]string, int, error)` — parse CIDR, return list (capped at 65536), total count
  - Error sentinels: `ErrIPPoolNotFound`, `ErrPoolExhausted`, `ErrIPAlreadyAllocated`, `ErrIPNotFound`
- **Verify:** `go build ./internal/store/...`

### Task 3: APN API Handler
- **Files:** Create `internal/api/apn/handler.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/operator/handler.go` — follow same handler structure (request/response types, validation, audit, error handling)
- **Context refs:** API Specifications > APN Endpoints, Architecture Context > Data Flow
- **What:**
  Create the APN HTTP handler with the following:

  Handler struct: apnStore (*store.APNStore), operatorStore (*store.OperatorStore), auditSvc (audit.Auditor), logger (zerolog.Logger)

  Request/response types following operator handler pattern:
  - `createAPNRequest`: name, operator_id, apn_type, supported_rat_types, display_name, default_policy_id, settings
  - `updateAPNRequest`: display_name, supported_rat_types, default_policy_id, settings
  - `apnResponse`: all fields as strings for UUIDs, time.RFC3339Nano for timestamps

  Handler methods:
  - `List(w, r)` — GET /api/v1/apns: tenant-scoped, cursor pagination, optional state/operator_id filters
  - `Create(w, r)` — POST /api/v1/apns: validate fields, verify operator_id exists and is granted to tenant (check operator_grants), create APN, audit log
  - `Get(w, r)` — GET /api/v1/apns/:id: tenant-scoped get by ID
  - `Update(w, r)` — PATCH /api/v1/apns/:id: partial update, audit log
  - `Archive(w, r)` — DELETE /api/v1/apns/:id: soft-delete to archived if no active SIMs, audit log

  Validation rules:
  - name: required, max 100 chars
  - operator_id: required, valid UUID
  - apn_type: required, must be one of [private_managed, operator_managed, customer_managed]
  - supported_rat_types: each must be valid [nb_iot, lte_m, lte, nr_5g]

  Use apierr.WriteSuccess, apierr.WriteList, apierr.WriteError for all responses.
  Use chi.URLParam(r, "id") for path parameters.
  Get tenant_id from context: `r.Context().Value(apierr.TenantIDKey).(uuid.UUID)`
- **Verify:** `go build ./internal/api/apn/...`

### Task 4: IP Pool API Handler
- **Files:** Create `internal/api/ippool/handler.go`
- **Depends on:** Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/api/operator/handler.go` — follow same handler structure
- **Context refs:** API Specifications > IP Pool Endpoints, IP Address Generation Algorithm, IP Allocation Algorithm, Architecture Context > Data Flow
- **What:**
  Create the IP Pool HTTP handler with:

  Handler struct: ippoolStore (*store.IPPoolStore), apnStore (*store.APNStore), auditSvc (audit.Auditor), logger (zerolog.Logger)

  Request/response types:
  - `createPoolRequest`: apn_id, name, cidr_v4, cidr_v6, alert_threshold_warning, alert_threshold_critical, reclaim_grace_period_days
  - `updatePoolRequest`: name, alert_threshold_warning, alert_threshold_critical, reclaim_grace_period_days, state
  - `reserveIPRequest`: sim_id, address_v4 (optional)
  - `poolResponse`: all fields, utilization_pct as float64 computed from used_addresses/total_addresses*100
  - `addressResponse`: id, pool_id, address_v4, address_v6, allocation_type, sim_id, state, allocated_at

  Handler methods:
  - `List(w, r)` — GET /api/v1/ip-pools: tenant-scoped, cursor pagination, optional apn_id filter
  - `Create(w, r)` — POST /api/v1/ip-pools: validate, verify APN exists + belongs to tenant, create pool with address generation, audit log
  - `Get(w, r)` — GET /api/v1/ip-pools/:id: with utilization percentage
  - `Update(w, r)` — PATCH /api/v1/ip-pools/:id: partial update, audit log
  - `ListAddresses(w, r)` — GET /api/v1/ip-pools/:id/addresses: cursor pagination, state filter
  - `ReserveIP(w, r)` — POST /api/v1/ip-pools/:id/addresses/reserve: reserve static IP for SIM

  Validation rules:
  - apn_id: required, valid UUID
  - name: required, max 100 chars
  - At least one of cidr_v4 or cidr_v6 required
  - cidr_v4: must be valid IPv4 CIDR (e.g., 10.0.0.0/24)
  - cidr_v6: must be valid IPv6 CIDR
  - alert thresholds: 0-100 range
  - sim_id for reserve: required, valid UUID
- **Verify:** `go build ./internal/api/ippool/...`

### Task 5: Gateway Route Registration & Main Wiring
- **Files:** Modify `internal/gateway/router.go`, Modify `cmd/argus/main.go`
- **Depends on:** Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — follow existing route group pattern, Read `cmd/argus/main.go` — follow existing handler wiring pattern
- **Context refs:** API Specifications > APN Endpoints, API Specifications > IP Pool Endpoints
- **What:**
  1. In `internal/gateway/router.go`:
     - Add imports for `apnapi "github.com/btopcu/argus/internal/api/apn"` and `ippoolapi "github.com/btopcu/argus/internal/api/ippool"`
     - Add `APNHandler *apnapi.Handler` and `IPPoolHandler *ippoolapi.Handler` to RouterDeps struct
     - Register APN routes (following operator pattern):
       - Group with JWTAuth + RequireRole("sim_manager"): GET /api/v1/apns, GET /api/v1/apns/{id}
       - Group with JWTAuth + RequireRole("tenant_admin"): POST /api/v1/apns, PATCH /api/v1/apns/{id}, DELETE /api/v1/apns/{id}
     - Register IP Pool routes:
       - Group with JWTAuth + RequireRole("operator_manager"): GET /api/v1/ip-pools, GET /api/v1/ip-pools/{id}, GET /api/v1/ip-pools/{id}/addresses
       - Group with JWTAuth + RequireRole("tenant_admin"): POST /api/v1/ip-pools, PATCH /api/v1/ip-pools/{id}
       - Group with JWTAuth + RequireRole("sim_manager"): POST /api/v1/ip-pools/{id}/addresses/reserve

  2. In `cmd/argus/main.go`:
     - Import `apnapi` and `ippoolapi` packages
     - Create `apnStore := store.NewAPNStore(pg.Pool)`
     - Create `ippoolStore := store.NewIPPoolStore(pg.Pool)`
     - Create `apnHandler := apnapi.NewHandler(apnStore, operatorStore, auditSvc, log.Logger)`
     - Create `ippoolHandler := ippoolapi.NewHandler(ippoolStore, apnStore, auditSvc, log.Logger)`
     - Add `APNHandler: apnHandler` and `IPPoolHandler: ippoolHandler` to RouterDeps
- **Verify:** `go build ./cmd/argus/...`

### Task 6: Clean Up Stubs
- **Files:** Modify `internal/store/stubs.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** low
- **Pattern ref:** Read `internal/store/stubs.go` — remove replaced stubs
- **Context refs:** Database Schema
- **What:**
  Remove from `internal/store/stubs.go`:
  - The `APN` struct (lines 60-64)
  - The `APNStore` struct and `NewAPNStore` func (lines 66-72)
  - The `GetByName` method on APNStore (lines 74-76)
  - The `IPPool`, `IPAddress`, `IPAllocation` structs (lines 78-90)
  - The `IPPoolStore` struct, `NewIPPoolStore` func (lines 92-98)
  - The `List` method on IPPoolStore (lines 100-102)
  - The `AllocateIP` method on IPPoolStore (lines 104-106)

  Keep:
  - The SIM-related stubs (`SIMStore`, `SIM`, `CreateSIMParams`, etc.) — those will be replaced by STORY-011
  - The error sentinels for SIM (`ErrICCIDExists`, `ErrIMSIExists`)

  Update any references in other files that import the old stub types to use the new real types from apn.go/ippool.go. The real `APN` struct in `apn.go` will have more fields, so callers accessing `APN.ID`, `APN.Name`, `APN.DefaultPolicyID` will still work.
- **Verify:** `go build ./...`

### Task 7: APN Store Tests
- **Files:** Create `internal/store/apn_test.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/operator/handler_test.go` — follow same test structure (table-driven tests, httptest)
- **Context refs:** Database Schema, API Specifications > APN Endpoints
- **What:**
  Write unit tests for the APN store covering struct creation and field mapping:
  - Test `APN` struct field initialization
  - Test `CreateAPNParams` defaults
  - Test `UpdateAPNParams` nil handling
  - Test error sentinel values are distinct

  Note: Integration tests (against real DB) are out of scope — these are struct/logic unit tests matching the operator_test pattern which tests response structures and validation.
- **Verify:** `go test ./internal/store/ -run TestAPN -v`

### Task 8: APN & IP Pool Handler Tests
- **Files:** Create `internal/api/apn/handler_test.go`, Create `internal/api/ippool/handler_test.go`
- **Depends on:** Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/operator/handler_test.go` — follow same test structure (validation tests, response struct tests, invalid ID tests)
- **Context refs:** API Specifications > APN Endpoints, API Specifications > IP Pool Endpoints
- **What:**
  Write handler-level unit tests:

  For `internal/api/apn/handler_test.go`:
  - TestCreateAPNValidation: table-driven tests for missing name, missing operator_id, invalid apn_type, invalid json → correct HTTP status codes (400/422)
  - TestUpdateAPNValidation: invalid ID format → 400, invalid json → 400
  - TestArchiveAPNInvalidID: invalid UUID → 400
  - TestToAPNResponse: verify field mapping from store.APN to response struct
  - TestToAPNResponseNilRATTypes: ensure nil becomes empty array
  - TestValidAPNTypes: verify valid apn_type set
  - TestValidRATTypes: verify valid rat_type set

  For `internal/api/ippool/handler_test.go`:
  - TestCreatePoolValidation: missing apn_id, missing name, no CIDR, invalid CIDR format, invalid json → correct status codes
  - TestUpdatePoolValidation: invalid ID → 400, invalid json → 400
  - TestReserveIPValidation: missing sim_id → 422, invalid pool ID → 400
  - TestToPoolResponse: verify field mapping
  - TestToAddressResponse: verify field mapping with nil address fields
  - TestIPv4Generation: test that CIDR parsing generates correct number of addresses (e.g., /24 → 254, /30 → 2, /32 → 1)
- **Verify:** `go test ./internal/api/apn/ -v && go test ./internal/api/ippool/ -v`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| POST /api/v1/apns creates APN linked to operator + tenant | Task 1, Task 3 | Task 8 (TestCreateAPNValidation) |
| APN name unique per (tenant_id, operator_id) | Task 1 (DB unique index) | Task 8 |
| DELETE soft-deletes to ARCHIVED if no active SIMs, else 422 | Task 1 (Archive method), Task 3 | Task 8 (TestArchiveAPNInvalidID) |
| POST /api/v1/ip-pools creates pool with CIDR, auto-generates IPs | Task 2, Task 4 | Task 8 (TestCreatePoolValidation, TestIPv4Generation) |
| IP allocation: next available, conflict detection | Task 2 (AllocateIP) | Task 8 |
| Static IP reservation per SIM | Task 2 (ReserveStaticIP), Task 4 | Task 8 (TestReserveIPValidation) |
| Pool utilization alerts at thresholds | Task 2 (AllocateIP threshold checks) | Task 8 |
| Pool utilization % updated on allocate/release | Task 2 (used_addresses increment/decrement) | Task 8 |
| IPv4 + IPv6 dual-stack support | Task 2 (generateIPv4/IPv6 helpers) | Task 8 (TestIPv4Generation) |
| IP reclaim: grace period | Task 2 (ReleaseIP with reclaim state) | Task 8 |

## Story-Specific Compliance Rules

- **API**: Standard envelope `{ status, data, meta?, error? }` via apierr package for all responses
- **DB**: All queries scoped by tenant_id (enforced in store layer). Tables already exist in core_schema migration — no new migration needed.
- **DB pgx types**: CIDR → `::text`/`::cidr` cast, INET → `::text`/`::inet` cast, TIMESTAMPTZ → `time.Time`/`*time.Time`
- **Naming**: Go camelCase, DB snake_case, routes kebab-case
- **Cursor pagination**: cursor-based (not offset) for all list endpoints, using created_at DESC + id DESC ordering
- **Audit**: Every state-changing operation creates an audit log entry via `audit.Auditor`
- **Error codes**: Use existing apierr constants. Add `CodeAPNHasActiveSIMs = "APN_HAS_ACTIVE_SIMS"`, `CodePoolExhausted = "POOL_EXHAUSTED"`, `CodeIPAlreadyAllocated = "IP_ALREADY_ALLOCATED"` to apierr
- **ADR-001**: Modular monolith — all code in internal/ packages, no external service calls
- **ADR-002**: PostgreSQL for OLTP — use pgxpool, transactions for critical sections

## Risks & Mitigations

- **Large CIDR bulk insert**: Creating /16 pool = 65K+ IPs → use batch INSERT with COPY protocol or chunked inserts (1000 per batch). Mitigate by capping IPv6 at 65536 addresses.
- **Race condition on IP allocation**: Use `FOR UPDATE SKIP LOCKED` and serializable transaction isolation per ALGORITHMS.md
- **SIM table partitioned**: Archive check query on sims table works across partitions with the default partition already created
