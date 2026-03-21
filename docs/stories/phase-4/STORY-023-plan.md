# Implementation Plan: STORY-023 - Policy CRUD & Versioning

## Goal

Implement full CRUD operations for policies (TBL-13) and policy versions (TBL-14) with immutable versioning, state machine (draft → active → archived), DSL compilation/validation, and version comparison.

## Architecture Context

### Components Involved

- **PolicyStore** (`internal/store/policy.go`): Data access layer for policies and policy_versions tables. Follows the same pattern as `internal/store/operator.go` — struct with `*pgxpool.Pool`, typed errors, scan helpers, cursor-based pagination.
- **PolicyHandler** (`internal/api/policy/handler.go`): HTTP handler for API-090 to API-095. Follows the same pattern as `internal/api/operator/handler.go` — request/response types, validation, audit logging.
- **Gateway Router** (`internal/gateway/router.go`): Route registration. Add `PolicyHandler` to `RouterDeps` and register policy routes with `RequireRole("policy_editor")`.
- **Main** (`cmd/argus/main.go`): Wire up PolicyStore and PolicyHandler, pass to router deps.
- **DSL Package** (`internal/policy/dsl/`): Already exists from STORY-022. Provides `dsl.CompileSource(source)` → `(*CompiledPolicy, []DSLError, error)` and `dsl.Validate(source)` → `[]DSLError`.

### Data Flow

```
Client → POST /api/v1/policies
  → JWTAuth middleware (extract tenant_id, user_id)
  → RequireRole("policy_editor") middleware
  → PolicyHandler.Create
    → Validate request (name, scope, dsl_source)
    → dsl.CompileSource(dsl_source) → compiled_rules JSON
    → PolicyStore.Create(policy + initial version v1 draft)
    → AuditSvc.CreateEntry("policy.create", ...)
    → Return 201 {status: "success", data: {id, name, versions: [{id, version: 1, state: "draft"}]}}
```

### API Specifications

#### API-090: GET /api/v1/policies
- Query params: `?cursor=<uuid>&limit=<int>&status=<active|disabled|archived>&q=<search>`
- Success (200): `{ status: "success", data: [{id, name, description, scope, active_version, sim_count, state, updated_at}], meta: {cursor, limit, has_more} }`
- Auth: JWT (policy_editor+)

#### API-091: POST /api/v1/policies
- Request body: `{ name: string, description?: string, scope: string("global"|"operator"|"apn"|"sim"), scope_ref_id?: uuid, dsl_source: string }`
- Success (201): `{ status: "success", data: {id, name, description, scope, versions: [{id, version: 1, state: "draft", dsl_source, compiled_rules}]} }`
- Error (400): Invalid JSON. (422): Validation errors or DSL compilation errors.
- Auth: JWT (policy_editor+)

#### API-092: GET /api/v1/policies/:id
- Success (200): `{ status: "success", data: {id, name, description, scope, scope_ref_id, state, current_version_id, versions: [{id, version, state, dsl_content, activated_at, created_at}], created_at, updated_at} }`
- Error (404): Policy not found.
- Auth: JWT (policy_editor+)

#### API-093: POST /api/v1/policies/:id/versions
- Request body: `{ dsl_source: string, clone_from_version_id?: uuid }`
- Success (201): `{ status: "success", data: {id, version, state: "draft", dsl_source, compiled_rules} }`
- Error (400): Invalid JSON. (404): Policy not found. (422): DSL compilation error.
- Auth: JWT (policy_editor+)

#### API-095: POST /api/v1/policy-versions/:id/activate
- Request body: (empty or `{}`)
- Success (200): `{ status: "success", data: {id, version, state: "active", activated_at} }`
- Error (404): Version not found. (422): DSL has errors / version not in draft state.
- Side effects: Previous active version → state "superseded". Policy.current_version_id updated.
- Auth: JWT (policy_editor+)

#### Additional endpoints on policy:
- **PATCH /api/v1/policies/:id** — Update policy metadata (name, description, state)
- **DELETE /api/v1/policies/:id** — Soft-delete policy (set state=archived). Rejected if SIMs assigned.
- **GET /api/v1/policy-versions/:id1/diff/:id2** — Compare two versions, return DSL diff.
- **PATCH /api/v1/policy-versions/:id** — Update draft version's DSL source (recompile).

### Database Schema

Source: `migrations/20260320000002_core_schema.up.sql` (ACTUAL — tables already exist)

```sql
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
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

CREATE UNIQUE INDEX idx_policies_tenant_name ON policies (tenant_id, name);
CREATE INDEX idx_policies_tenant_scope ON policies (tenant_id, scope);
CREATE INDEX idx_policies_state ON policies (state);

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

CREATE UNIQUE INDEX idx_policy_versions_policy_ver ON policy_versions (policy_id, version);
CREATE INDEX idx_policy_versions_policy_state ON policy_versions (policy_id, state);

ALTER TABLE policies ADD CONSTRAINT fk_policies_current_version
    FOREIGN KEY (current_version_id) REFERENCES policy_versions(id);
```

### Version State Machine

```
draft → active (activation: archives/supersedes previous active)
draft → archived (manual archive of unused draft)
active → superseded (when a new version is activated)
active → archived (manual deactivation)
```

Valid states: `draft`, `active`, `superseded`, `archived`

### Policy State Values

- `active` — Policy is usable, can have versions
- `disabled` — Policy exists but cannot be assigned
- `archived` — Soft-deleted, read-only

## Prerequisites

- [x] STORY-022 completed (provides `internal/policy/dsl/` package with Parse, CompileSource, Validate)
- [x] STORY-002 completed (DB schema including TBL-13, TBL-14 already in migrations)
- [x] Core infrastructure (store patterns, apierr, audit, gateway) established

## Tasks

### Task 1: Policy Store — Core CRUD operations
- **Files:** Create `internal/store/policy.go`
- **Depends on:** — (none)
- **Complexity:** high
- **Pattern ref:** Read `internal/store/operator.go` — follow same store structure (struct, errors, scan helpers, Create/GetByID/List/Update/Delete methods)
- **Context refs:** ["Database Schema", "API Specifications > API-090", "API Specifications > API-091", "API Specifications > API-092", "Version State Machine"]
- **What:** Create `PolicyStore` with `*pgxpool.Pool`. Define structs: `Policy` (matches TBL-13 columns exactly: id, tenant_id, name, description, scope, scope_ref_id, current_version_id, state, created_at, updated_at, created_by), `PolicyVersion` (matches TBL-14 columns: id, policy_id, version, dsl_content, compiled_rules as json.RawMessage, state, affected_sim_count, dry_run_result as json.RawMessage, activated_at, rolled_back_at, created_at, created_by), `CreatePolicyParams`, `UpdatePolicyParams`, `CreateVersionParams`. Implement methods:
  - `Create(ctx, tenantID, params) → (*Policy, error)` — INSERT into policies
  - `GetByID(ctx, tenantID, id) → (*Policy, error)` — SELECT with tenant_id scope
  - `List(ctx, tenantID, cursor, limit, stateFilter, search) → ([]Policy, nextCursor, error)` — cursor-based pagination, optional state filter, optional name search (ILIKE)
  - `Update(ctx, tenantID, id, params) → (*Policy, error)` — partial update (name, description, state)
  - `Delete(ctx, tenantID, id) → error` — set state='archived' (soft-delete)
  - `CreateVersion(ctx, params) → (*PolicyVersion, error)` — INSERT version with auto-increment version number (SELECT MAX(version)+1)
  - `GetVersionByID(ctx, id) → (*PolicyVersion, error)`
  - `GetVersionsByPolicyID(ctx, policyID) → ([]PolicyVersion, error)` — ordered by version DESC
  - `UpdateVersion(ctx, id, dslContent, compiledRules) → (*PolicyVersion, error)` — update draft version
  - `ActivateVersion(ctx, versionID) → (*PolicyVersion, error)` — in a transaction: set version state='active', set activated_at=NOW(), supersede any previous active version (set state='superseded'), update policy.current_version_id
  - `CountAssignedSIMs(ctx, policyID) → (int, error)` — COUNT from policy_assignments where policy_version_id IN (SELECT id FROM policy_versions WHERE policy_id=X)
  - Define sentinel errors: `ErrPolicyNotFound`, `ErrPolicyNameExists`, `ErrPolicyVersionNotFound`, `ErrPolicyInUse`, `ErrVersionNotDraft`
- **Verify:** `go build ./internal/store/...`

### Task 2: Policy Store — Tests
- **Files:** Create `internal/store/policy_test.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/operator_test.go` — follow same test structure
- **Context refs:** ["Database Schema", "Version State Machine"]
- **What:** Write unit tests for PolicyStore using the same mock/test pattern as operator_test.go. Test scenarios:
  - Create policy → returns policy with correct fields
  - Create policy with duplicate name → returns ErrPolicyNameExists
  - GetByID with valid ID → returns policy
  - GetByID with invalid ID → returns ErrPolicyNotFound
  - List with pagination → cursor works correctly
  - List with state filter → filters correctly
  - CreateVersion → auto-increments version number
  - ActivateVersion → supersedes previous active, updates current_version_id
  - Delete policy with assigned SIMs → returns ErrPolicyInUse
  - Update draft version DSL → works; update non-draft → returns ErrVersionNotDraft
- **Verify:** `go test ./internal/store/ -run TestPolicy -v`

### Task 3: Policy API Handler — CRUD endpoints
- **Files:** Create `internal/api/policy/handler.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/api/operator/handler.go` — follow same handler structure (Handler struct, request/response types, toXxxResponse converters, validation, audit logging, error handling with apierr)
- **Context refs:** ["API Specifications", "Architecture Context > Components Involved", "Architecture Context > Data Flow", "Version State Machine"]
- **What:** Create `Handler` struct with `policyStore *store.PolicyStore`, `auditSvc audit.Auditor`, `logger zerolog.Logger`. Implement HTTP handlers:
  - `List(w, r)` — API-090: parse query params (cursor, limit, status, q), call store.List with tenant_id from context, return list response with ListMeta
  - `Create(w, r)` — API-091: decode request body, validate (name required, scope required and must be valid enum, dsl_source required), compile DSL with `dsl.CompileSource()`, marshal compiled to JSON, create policy + initial version v1 in store, audit log "policy.create", return 201
  - `Get(w, r)` — API-092: parse policy ID from URL, get policy with tenant_id scope, get all versions for policy, return combined response
  - `Update(w, r)` — PATCH: parse ID, decode body, validate, update policy metadata, audit log "policy.update", return 200
  - `Delete(w, r)` — DELETE: parse ID, check CountAssignedSIMs > 0 → 422 POLICY_IN_USE, otherwise soft-delete, audit log "policy.delete", return 204
  - `CreateVersion(w, r)` — API-093: parse policy ID, decode body (dsl_source, clone_from_version_id), if clone_from_version_id provided get that version's DSL, compile DSL, create version, audit log "policy_version.create", return 201
  - `ActivateVersion(w, r)` — API-095: parse version ID, get version → verify state is "draft", validate DSL compiles without errors, activate in store (transaction), audit log "policy_version.activate", return 200
  - `UpdateVersion(w, r)` — PATCH version: parse version ID, decode body (dsl_source), verify version is draft, compile DSL, update version, audit log "policy_version.update", return 200
  - `DiffVersions(w, r)` — GET: parse two version IDs, fetch both, compute line-by-line diff of dsl_content, return diff
  - Define request/response types, toXxxResponse converters. Use `apierr.WriteSuccess`, `apierr.WriteList`, `apierr.WriteError` consistently.
  - Use `chi.URLParam(r, "id")` for path parameters.
  - Extract tenant_id from context: `tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)`
  - Extract user_id for audit: same pattern as operator handler's `userIDFromContext`
- **Verify:** `go build ./internal/api/policy/...`

### Task 4: Policy API Handler — Tests
- **Files:** Create `internal/api/policy/handler_test.go`
- **Depends on:** Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/operator/handler_test.go` — follow same test structure
- **Context refs:** ["API Specifications", "Version State Machine"]
- **What:** Write HTTP handler tests using httptest. Test scenarios from the story:
  - POST /policies → 201 with policy + v1 draft version
  - POST /policies with invalid DSL → 422
  - POST /policies with missing name → 422 validation error
  - GET /policies → 200 with list
  - GET /policies/:id → 200 with policy + versions
  - GET /policies/:id with invalid ID → 404
  - POST /policies/:id/versions → 201 with new draft
  - POST /policy-versions/:id/activate → 200, previous active → superseded
  - POST /policy-versions/:id/activate with DSL error → 422
  - DELETE /policies/:id with assigned SIMs → 422 POLICY_IN_USE
  - DELETE /policies/:id without SIMs → 204
- **Verify:** `go test ./internal/api/policy/ -v`

### Task 5: Gateway Router + Main wiring
- **Files:** Modify `internal/gateway/router.go`, Modify `cmd/argus/main.go`
- **Depends on:** Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — follow existing route registration pattern (RouterDeps field, nil check, Group with JWTAuth+RequireRole)
- **Context refs:** ["API Specifications", "Architecture Context > Components Involved"]
- **What:**
  - In `internal/gateway/router.go`:
    - Add import for `policyapi "github.com/btopcu/argus/internal/api/policy"`
    - Add `PolicyHandler *policyapi.Handler` to `RouterDeps` struct
    - Add route group for policy endpoints:
      ```
      if deps.PolicyHandler != nil {
        r.Group(func(r chi.Router) {
          r.Use(JWTAuth(deps.JWTSecret))
          r.Use(RequireRole("policy_editor"))
          r.Get("/api/v1/policies", deps.PolicyHandler.List)
          r.Post("/api/v1/policies", deps.PolicyHandler.Create)
          r.Get("/api/v1/policies/{id}", deps.PolicyHandler.Get)
          r.Patch("/api/v1/policies/{id}", deps.PolicyHandler.Update)
          r.Delete("/api/v1/policies/{id}", deps.PolicyHandler.Delete)
          r.Post("/api/v1/policies/{id}/versions", deps.PolicyHandler.CreateVersion)
          r.Patch("/api/v1/policy-versions/{id}", deps.PolicyHandler.UpdateVersion)
          r.Post("/api/v1/policy-versions/{id}/activate", deps.PolicyHandler.ActivateVersion)
          r.Get("/api/v1/policy-versions/{id1}/diff/{id2}", deps.PolicyHandler.DiffVersions)
        })
      }
      ```
  - In `cmd/argus/main.go`:
    - Add import for `policyapi "github.com/btopcu/argus/internal/api/policy"`
    - After existing store initializations, add: `policyStore := store.NewPolicyStore(pg.Pool)`
    - Create handler: `policyHandler := policyapi.NewHandler(policyStore, auditSvc, log.Logger)`
    - Add `PolicyHandler: policyHandler` to `gateway.RouterDeps{...}`
- **Verify:** `go build ./cmd/argus/...`

### Task 6: Integration test — Full policy lifecycle
- **Files:** Create `internal/api/policy/integration_test.go`
- **Depends on:** Task 4, Task 5
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/operator/handler_test.go` — follow same integration approach
- **Context refs:** ["API Specifications", "Version State Machine", "Acceptance Criteria Mapping"]
- **What:** Write end-to-end test covering the full policy lifecycle:
  1. Create policy with DSL source → verify v1 draft created
  2. Create new version (v2) → verify v2 draft, v1 unchanged
  3. Activate v2 → verify v2 active, v1 still draft (was never activated)
  4. Create v3, activate v3 → verify v3 active, v2 superseded
  5. Attempt activate with invalid DSL → 422
  6. Get policy → all versions returned in order
  7. List policies with status filter → correct results
  8. Version diff → returns changes
  9. Delete policy (no SIMs) → 204
  Test the state machine invariant: only one active version per policy at any time.
- **Verify:** `go test ./internal/api/policy/ -run TestIntegration -v`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| POST /api/v1/policies creates policy with initial draft version | Task 1 (store), Task 3 (handler) | Task 4, Task 6 |
| GET /api/v1/policies lists policies with active version summary | Task 1 (store List), Task 3 (handler List) | Task 4, Task 6 |
| GET /api/v1/policies/:id returns policy with all versions | Task 1 (store GetByID + GetVersionsByPolicyID), Task 3 (handler Get) | Task 4, Task 6 |
| POST /api/v1/policies/:id/versions creates new draft version | Task 1 (store CreateVersion), Task 3 (handler CreateVersion) | Task 4, Task 6 |
| Draft version can be edited (DSL source updated, recompiled) | Task 1 (store UpdateVersion), Task 3 (handler UpdateVersion) | Task 4 |
| Version state machine: draft → active (activation archives previous) | Task 1 (store ActivateVersion), Task 3 (handler ActivateVersion) | Task 4, Task 6 |
| Only one active version per policy at any time | Task 1 (ActivateVersion transaction) | Task 6 |
| Archived versions are read-only but viewable | Task 1 (UpdateVersion checks state), Task 3 (Get returns all) | Task 4 |
| Version comparison: diff two versions | Task 3 (handler DiffVersions) | Task 6 |
| Policy deletion: soft-delete, only if no SIMs assigned | Task 1 (CountAssignedSIMs, Delete), Task 3 (handler Delete) | Task 4, Task 6 |
| Compiled rules stored alongside DSL source | Task 1 (CreateVersion stores compiled_rules), Task 3 (compiles on create) | Task 4 |
| DSL must compile without errors before activation | Task 3 (ActivateVersion validates DSL) | Task 4, Task 6 |
| Audit log entry for every policy/version create, update, activate, archive | Task 3 (all handlers call createAuditEntry) | Task 4 |

## Story-Specific Compliance Rules

- **API:** Standard envelope `{status, data, meta?, error?}` for all responses. Cursor-based pagination for list endpoints.
- **DB:** All queries scoped by `tenant_id`. Tables already exist in migration 20260320000002.
- **Auth:** JWT required, minimum role `policy_editor` for all policy endpoints.
- **Audit:** Every state-changing operation creates audit entry with before/after diff.
- **DSL:** Use `dsl.CompileSource()` from `internal/policy/dsl/` package for validation and compilation. Do not reimplement parsing.
- **Business:** Policy evaluation order: SIM-specific > APN-level > operator-level > tenant default (BR-4). Only one active version per policy at any time.

## Risks & Mitigations

- **Risk:** Version activation race condition (two concurrent activations). **Mitigation:** Use database transaction with SELECT FOR UPDATE on the policy row during activation.
- **Risk:** DSL compilation failure could leave policy in inconsistent state. **Mitigation:** Compile DSL before any database write; fail fast with 422.
- **Risk:** SIM count query for deletion check could be slow on large datasets. **Mitigation:** Use EXISTS query instead of COUNT for the deletion check (early termination).
