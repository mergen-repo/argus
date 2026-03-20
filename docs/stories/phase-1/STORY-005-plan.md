# Implementation Plan: STORY-005 - Tenant Management & User CRUD

## Goal
Implement 8 API endpoints (API-006 to API-008 for Users, API-010 to API-014 for Tenants) providing full CRUD for tenants (super_admin only) and users (tenant_admin within own tenant), including resource limit enforcement, state management, cursor-based pagination, and audit logging.

## Architecture Context

### Components Involved
- **SVC-01 (Gateway)**: `internal/gateway/router.go` — route registration with JWTAuth + RequireRole middleware
- **SVC-03 (Core API)**: `internal/api/tenant/handler.go` (NEW), `internal/api/user/handler.go` (NEW) — HTTP handlers
- **Store layer**: `internal/store/tenant.go` (NEW), `internal/store/user.go` (MODIFY) — PostgreSQL data access
- **SVC-10 (Audit)**: `internal/audit/audit.go` — audit log entries for state changes
- **Error layer**: `internal/apierr/apierr.go` — error codes and response helpers

### Data Flow
```
Client → Nginx(:443) → Go API(:8080)
  → JWTAuth middleware (extract tenant_id, user_id, role from JWT)
  → RequireRole middleware (check minimum role level)
  → Handler (validate request, call store, audit, respond)
  → Store (PostgreSQL queries scoped by tenant_id)
  → Response via apierr.WriteSuccess / apierr.WriteList / apierr.WriteError
```

### API Specifications

#### API-010: GET /api/v1/tenants (List Tenants)
- Auth: JWT (super_admin only)
- Query params: `?cursor=<uuid>&limit=<int>&state=<string>`
- Success 200: `{ status: "success", data: [{id, name, domain, contact_email, state, max_sims, max_apns, max_users, created_at, updated_at}], meta: {cursor, limit, has_more} }`
- Errors: 401 Unauthorized, 403 INSUFFICIENT_ROLE

#### API-011: POST /api/v1/tenants (Create Tenant)
- Auth: JWT (super_admin only)
- Request body: `{name: string, domain: string, contact_email: string, contact_phone?: string, max_sims?: int, max_apns?: int, max_users?: int}`
- Success 201: `{ status: "success", data: {id, name, domain, contact_email, contact_phone, state: "active", max_sims, max_apns, max_users, created_at, updated_at} }`
- Errors: 400 INVALID_FORMAT, 409 ALREADY_EXISTS (duplicate domain), 422 VALIDATION_ERROR

#### API-012: GET /api/v1/tenants/:id (Get Tenant Detail)
- Auth: JWT (super_admin OR own tenant — tenant_admin+ can view own tenant)
- Success 200: `{ status: "success", data: {id, name, domain, contact_email, contact_phone, state, max_sims, max_apns, max_users, settings, sim_count, user_count, apn_count, created_at, updated_at} }`
- Errors: 403 FORBIDDEN, 404 NOT_FOUND

#### API-013: PATCH /api/v1/tenants/:id (Update Tenant)
- Auth: JWT (super_admin can update everything; tenant_admin can update own tenant's name, contact_email, contact_phone, settings only — NOT limits or state)
- Request body: `{name?: string, contact_email?: string, contact_phone?: string, max_sims?: int, max_apns?: int, max_users?: int, state?: string, settings?: object}`
- State transitions: active → suspended → terminated (super_admin only)
- Success 200: `{ status: "success", data: {id, name, domain, ...updated fields...} }`
- Errors: 400, 403, 404, 422

#### API-014: GET /api/v1/tenants/:id/stats (Tenant Stats)
- Auth: JWT (tenant_admin+ — non-super_admin only see own tenant)
- Success 200: `{ status: "success", data: {sim_count, user_count, apn_count, active_sessions: 0, storage_bytes: 0} }`
- Errors: 403, 404

#### API-006: GET /api/v1/users (List Users)
- Auth: JWT (tenant_admin+) — auto-scoped by tenant_id from JWT
- Query params: `?cursor=<uuid>&limit=<int>&role=<string>&state=<string>`
- Success 200: `{ status: "success", data: [{id, email, name, role, state, last_login_at, created_at}], meta: {cursor, limit, has_more} }`
- Errors: 401, 403

#### API-007: POST /api/v1/users (Create User)
- Auth: JWT (tenant_admin+)
- Request body: `{email: string, name: string, role: string}`
- Validates: email format, role is valid enum, max_users limit not exceeded
- Creates user with state="invited", password_hash="" (placeholder for invite flow)
- Success 201: `{ status: "success", data: {id, email, name, role, state: "invited", created_at} }`
- Errors: 400, 409 ALREADY_EXISTS (duplicate email in tenant), 422 RESOURCE_LIMIT_EXCEEDED / VALIDATION_ERROR

#### API-008: PATCH /api/v1/users/:id (Update User)
- Auth: JWT (tenant_admin+ OR self — self can update own name only, not role/state)
- Request body: `{name?: string, role?: string, state?: string}`
- State: invited/active/disabled
- Success 200: `{ status: "success", data: {id, email, name, role, state, last_login_at, created_at} }`
- Errors: 400, 403, 404, 422

### Database Schema
```sql
-- Source: migrations/20260320000002_core_schema.up.sql (ACTUAL)
-- TBL-01: tenants
CREATE TABLE IF NOT EXISTS tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    domain VARCHAR(255) UNIQUE,
    contact_email VARCHAR(255) NOT NULL,
    contact_phone VARCHAR(50),
    max_sims INTEGER NOT NULL DEFAULT 100000,
    max_apns INTEGER NOT NULL DEFAULT 100,
    max_users INTEGER NOT NULL DEFAULT 50,
    purge_retention_days INTEGER NOT NULL DEFAULT 90,
    settings JSONB NOT NULL DEFAULT '{}',
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID
);
-- Indexes: idx_tenants_domain, idx_tenants_state

-- TBL-02: users
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    email VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(100) NOT NULL,
    role VARCHAR(30) NOT NULL,
    totp_secret VARCHAR(255),
    totp_enabled BOOLEAN NOT NULL DEFAULT false,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    last_login_at TIMESTAMPTZ,
    failed_login_count INTEGER NOT NULL DEFAULT 0,
    locked_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Indexes: idx_users_tenant_email (UNIQUE), idx_users_tenant_role, idx_users_state
```

### Error Codes Used
- `RESOURCE_LIMIT_EXCEEDED` (422): tenant max_users reached
- `ALREADY_EXISTS` (409): duplicate domain or email
- `NOT_FOUND` (404): tenant or user not found
- `FORBIDDEN` (403): cross-tenant access attempt
- `INSUFFICIENT_ROLE` (403): role too low
- `VALIDATION_ERROR` (422): input validation failures
- `INVALID_FORMAT` (400): malformed JSON

### Existing Patterns
- **Handler pattern**: `internal/api/auth/handler.go` — struct with dependencies, request/response DTOs, json.NewDecoder for parsing, apierr helpers for responses
- **Store pattern**: `internal/store/job.go` — pgxpool.Pool, TenantIDFromContext, cursor pagination with limit+1 fetch, isDuplicateKeyError
- **Router pattern**: `internal/gateway/router.go` — chi.Router groups with JWTAuth + RequireRole middleware
- **Audit pattern**: `internal/api/session/handler.go` — audit.CreateEntryParams with tenant_id, user_id, action, entity_type, entity_id, after_data
- **Context keys**: `apierr.TenantIDKey`, `apierr.UserIDKey`, `apierr.RoleKey`
- **Role hierarchy**: `gateway.HasRole(userRole, minRole)` — linear: api_user(1) < analyst(2) < ... < tenant_admin(6) < super_admin(7)

### Valid Roles (for user creation validation)
`api_user`, `analyst`, `policy_editor`, `sim_manager`, `operator_manager`, `tenant_admin`
Note: `super_admin` can only be assigned by super_admin; tenant_admin cannot create super_admin users.

### Tenant State Machine
```
active → suspended (super_admin only)
suspended → active (super_admin reactivation)
suspended → terminated (super_admin only, irreversible)
```

### User State Machine
```
invited → active (on first login / password set — future story)
active → disabled (tenant_admin or super_admin)
disabled → active (tenant_admin or super_admin)
```

## Prerequisites
- [x] STORY-002 completed (TBL-01 tenants, TBL-02 users tables exist in migration)
- [x] STORY-003 completed (JWT auth middleware, apierr package, context keys)
- [x] STORY-004 completed (RequireRole, HasRole, RoleLevel in gateway/rbac.go)

## Tasks

### Task 1: Tenant Store — CRUD + Pagination
- **Files:** Create `internal/store/tenant.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/job.go` — follow same store structure (pgxpool, TenantIDFromContext, cursor pagination with limit+1, isDuplicateKeyError)
- **Context refs:** Database Schema, API Specifications (API-010, API-011, API-012, API-013, API-014), Existing Patterns > Store pattern, Tenant State Machine
- **What:**
  - Create `Tenant` struct matching all columns from TBL-01 tenants table
  - Create `TenantStore` with `db *pgxpool.Pool`
  - `Create(ctx, CreateTenantParams) (*Tenant, error)` — INSERT with defaults, return created tenant. Handle duplicate domain with `isDuplicateKeyError` → return `ErrDomainExists`
  - `GetByID(ctx, id uuid.UUID) (*Tenant, error)` — SELECT by id, no tenant scoping (super_admin access). Return `ErrTenantNotFound` if not found
  - `List(ctx, cursor string, limit int, stateFilter string) ([]Tenant, string, error)` — cursor-based pagination ordered by created_at DESC, id DESC. Use limit+1 pattern for nextCursor
  - `Update(ctx, id uuid.UUID, UpdateTenantParams) (*Tenant, error)` — dynamic UPDATE building only for non-nil fields. Return updated tenant
  - `GetStats(ctx, tenantID uuid.UUID) (*TenantStats, error)` — query user count, SIM count, APN count using COUNT queries on respective tables filtered by tenant_id
  - `CountUsersByTenant(ctx, tenantID uuid.UUID) (int, error)` — used for resource limit check
  - Define error vars: `ErrTenantNotFound`, `ErrDomainExists`
- **Verify:** `go build ./internal/store/...`

### Task 2: User Store — CRUD Extension + Pagination
- **Files:** Modify `internal/store/user.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/job.go` — follow same List pattern (cursor + limit+1 + filter)
- **Context refs:** Database Schema, API Specifications (API-006, API-007, API-008), Existing Patterns > Store pattern, User State Machine
- **What:**
  - Add `CreateUser(ctx, CreateUserParams) (*User, error)` — INSERT with tenant_id from context, state="invited", password_hash="" placeholder. Handle duplicate email via isDuplicateKeyError → `ErrEmailExists`
  - Add `ListByTenant(ctx, cursor string, limit int, roleFilter string, stateFilter string) ([]User, string, error)` — cursor pagination scoped by tenant_id from context. Order by created_at DESC, id DESC
  - Add `UpdateUser(ctx, id uuid.UUID, UpdateUserParams) (*User, error)` — dynamic UPDATE for name, role, state. Scope by tenant_id from context. Return `ErrUserNotFound` if not found
  - Add `CountByTenant(ctx, tenantID uuid.UUID) (int, error)` — SELECT COUNT(*) WHERE tenant_id = $1 AND state != 'terminated'
  - Define new error var: `ErrEmailExists`
  - `CreateUserParams`: Email, Name, Role (no password — invite flow)
  - `UpdateUserParams`: Name *string, Role *string, State *string (all optional)
- **Verify:** `go build ./internal/store/...`

### Task 3: Tenant Handler — API-010 to API-014
- **Files:** Create `internal/api/tenant/handler.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/auth/handler.go` — follow handler struct + DTO + NewHandler pattern. Read `internal/api/session/handler.go` — follow List pagination + audit integration
- **Context refs:** API Specifications (API-010, API-011, API-012, API-013, API-014), Existing Patterns, Tenant State Machine, Error Codes Used
- **What:**
  - `TenantHandler` struct with `tenantStore`, `auditSvc`, `logger`
  - `List(w, r)` — API-010: parse cursor/limit/state from query, call store.List, return WriteList with ListMeta
  - `Create(w, r)` — API-011: decode JSON body, validate required fields (name, domain, contact_email), call store.Create, audit log "tenant.create", return WriteSuccess 201
  - `Get(w, r)` — API-012: parse :id from chi.URLParam, check authorization (super_admin can see any; others can only see own tenant via context TenantID match), call store.GetByID + store.GetStats, return combined response
  - `Update(w, r)` — API-013: parse :id, authorization check (super_admin can update all fields; tenant_admin can only update name/contact_email/contact_phone/settings of own tenant, NOT limits or state), validate state transitions, call store.Update, audit log "tenant.update" with before/after, return updated tenant
  - `Stats(w, r)` — API-014: parse :id, non-super_admin must match own tenant_id, call store.GetStats, return stats
  - DTOs: tenantResponse (JSON output), createTenantRequest, updateTenantRequest
  - State transition validation: active→suspended, suspended→active, suspended→terminated
  - Use `chi.URLParam(r, "id")` for path params, `uuid.Parse()` for validation
  - super_admin check: `role == "super_admin"` from context
- **Verify:** `go build ./internal/api/tenant/...`

### Task 4: User Handler — API-006 to API-008
- **Files:** Create `internal/api/user/handler.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/auth/handler.go` — handler pattern. Read `internal/api/session/handler.go` — List + audit
- **Context refs:** API Specifications (API-006, API-007, API-008), Existing Patterns, User State Machine, Error Codes Used, Valid Roles
- **What:**
  - `UserHandler` struct with `userStore`, `tenantStore` (for resource limit check), `auditSvc`, `logger`
  - `List(w, r)` — API-006: parse cursor/limit/role/state from query, call userStore.ListByTenant (auto-scoped), return WriteList
  - `Create(w, r)` — API-007: decode JSON, validate (email format, role is valid, not super_admin unless caller is super_admin), check max_users limit via tenantStore.GetByID + userStore.CountByTenant, call userStore.CreateUser, audit "user.create", return 201
  - `Update(w, r)` — API-008: parse :id, authorization (tenant_admin+ can update any user in tenant; self can update own name only), validate state transitions and role changes, call userStore.UpdateUser, audit "user.update" with before/after, return updated user
  - Role validation: ensure role is one of the valid enum values
  - Resource limit: compare current user count against tenant.max_users before creation
  - Self-update logic: if user_id from context == target user_id, allow name change only (not role/state). Return FORBIDDEN if attempting role/state change on self without tenant_admin+
  - Email validation: basic format check (contains @, non-empty local and domain parts)
- **Verify:** `go build ./internal/api/user/...`

### Task 5: Router Registration + Error Code Constants
- **Files:** Modify `internal/gateway/router.go`, Modify `internal/apierr/apierr.go`
- **Depends on:** Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — follow existing route group pattern
- **Context refs:** API Specifications (all 8 endpoints), Existing Patterns > Router pattern, Error Codes Used
- **What:**
  - Add missing error code constants to `apierr.go`: `CodeResourceLimitExceeded = "RESOURCE_LIMIT_EXCEEDED"`, `CodeTenantSuspended = "TENANT_SUSPENDED"`
  - In `router.go`, update `NewRouter` signature to accept `tenantHandler *tenant.TenantHandler, userHandler *user.UserHandler` (or use an options struct)
  - Add tenant routes group: JWTAuth + RequireRole("super_admin") for API-010, API-011; separate handling for API-012, API-013, API-014 which need JWTAuth but more nuanced auth (super_admin or own tenant)
  - Tenant routes (super_admin only group):
    - `GET /api/v1/tenants` → tenantHandler.List
    - `POST /api/v1/tenants` → tenantHandler.Create
  - Tenant routes (authenticated, authorization in handler):
    - `GET /api/v1/tenants/{id}` → tenantHandler.Get
    - `PATCH /api/v1/tenants/{id}` → tenantHandler.Update
    - `GET /api/v1/tenants/{id}/stats` → tenantHandler.Stats
  - User routes (tenant_admin+ group): JWTAuth + RequireRole("tenant_admin")
    - `GET /api/v1/users` → userHandler.List
    - `POST /api/v1/users` → userHandler.Create
  - User update route (authenticated, auth logic in handler): JWTAuth + RequireRole("api_user")
    - `PATCH /api/v1/users/{id}` → userHandler.Update
- **Verify:** `go build ./internal/gateway/...`

### Task 6: Tests — Store + Handler
- **Files:** Create `internal/store/tenant_test.go`, Create `internal/api/tenant/handler_test.go`, Create `internal/api/user/handler_test.go`
- **Depends on:** Task 5
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/session/handler_test.go` — follow test patterns if exists; otherwise `internal/api/msisdn/handler_test.go`
- **Context refs:** API Specifications, Acceptance Criteria Mapping, Test Scenarios
- **What:**
  - Test tenant creation with valid data → 201
  - Test tenant creation with duplicate domain → 409 ALREADY_EXISTS
  - Test user creation in tenant → 201, state = "invited"
  - Test user creation when max_users reached → 422 RESOURCE_LIMIT_EXCEEDED
  - Test tenant admin cannot see other tenants (API-010 requires super_admin)
  - Test update user role → 200
  - Test disable user → state changes to "disabled"
  - Test tenant state transitions (active → suspended → terminated)
  - Test cursor pagination returns correct cursors
  - Test self-update: user can change own name but not role
  - Tests will use httptest.NewServer + chi router for integration-style tests
  - Mock store interfaces where needed
- **Verify:** `go test ./internal/store/... ./internal/api/tenant/... ./internal/api/user/...`

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| POST /api/v1/tenants creates tenant with name, domain, resource limits | Task 1, Task 3 | Task 6 |
| GET /api/v1/tenants lists all tenants (super_admin) | Task 1, Task 3 | Task 6 |
| GET /api/v1/tenants/:id returns tenant detail + stats | Task 1, Task 3 | Task 6 |
| PATCH /api/v1/tenants/:id updates tenant | Task 1, Task 3 | Task 6 |
| POST /api/v1/users creates user in tenant, sends invite email placeholder | Task 2, Task 4 | Task 6 |
| GET /api/v1/users lists users in own tenant | Task 2, Task 4 | Task 6 |
| PATCH /api/v1/users/:id updates role, state | Task 2, Task 4 | Task 6 |
| Resource limits enforced: max_users | Task 1, Task 2, Task 4 | Task 6 |
| User creation triggers audit log entry | Task 4 | Task 6 |
| Tenant state transitions: active → suspended → terminated | Task 1, Task 3 | Task 6 |

## Story-Specific Compliance Rules
- API: Standard envelope format required (`{ status, data, meta?, error? }`) via apierr helpers
- DB: No new migrations — uses existing TBL-01, TBL-02 from STORY-002
- DB: All user queries scoped by tenant_id (enforced in store layer via TenantIDFromContext)
- Business: Tenant state transitions super_admin only (BR-6)
- Business: Resource limits per tenant (F-062, G-022)
- Business: Every state-changing operation creates audit log entry (BR-7)
- ADR: JWT auth per ADR-002 (already implemented)
- Cursor-based pagination (not offset) for all list endpoints (G-041)

## Risks & Mitigations
- **Risk:** Router signature change may break compilation if main.go wires dependencies. **Mitigation:** Accept nil handlers gracefully; wire dependencies only when available. Use conditional routing if handlers are nil.
- **Risk:** TenantStore.GetStats joins multiple tables, could be slow. **Mitigation:** Use separate COUNT queries, not JOINs; these are admin-only endpoints.
