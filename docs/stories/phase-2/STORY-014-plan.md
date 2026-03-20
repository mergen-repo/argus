# Implementation Plan: STORY-014 — MSISDN Number Pool Management

## Goal
Wire the existing MSISDN pool store and handler into the router and main.go so that the three MSISDN pool endpoints (list, import, assign) are accessible via HTTP, and ensure all code compiles and tests pass.

## Architecture Context

### Components Involved
- **MSISDNStore** (`internal/store/msisdn.go`): Data access layer for msisdn_pool table — already implemented with List, GetByID, GetByMSISDN, BulkImport, Assign, Release methods
- **MSISDN Handler** (`internal/api/msisdn/handler.go`): HTTP handler with List, Import (JSON + CSV), Assign methods — already implemented
- **Router** (`internal/gateway/router.go`): Chi router with RouterDeps struct — needs MSISDNHandler field and route registration
- **Main** (`cmd/argus/main.go`): Application entry point — needs MSISDNStore + Handler creation and wiring into RouterDeps
- **Database** (`migrations/20260320000002_core_schema.up.sql`): TBL-24 msisdn_pool table — already exists in migration

### Data Flow
```
Client → POST /api/v1/msisdn-pool/import → JWTAuth → RequireRole(tenant_admin) → Handler.Import → MSISDNStore.BulkImport → msisdn_pool table
Client → GET /api/v1/msisdn-pool → JWTAuth → RequireRole(sim_manager) → Handler.List → MSISDNStore.List → msisdn_pool table
Client → POST /api/v1/msisdn-pool/:id/assign → JWTAuth → RequireRole(sim_manager) → Handler.Assign → MSISDNStore.Assign → msisdn_pool + sims tables
```

### API Specifications

**API-160: GET /api/v1/msisdn-pool** — List MSISDN pool entries
- Query params: `cursor`, `limit`, `state` (available|assigned|reserved), `operator_id`
- Auth: JWT (sim_manager+)
- Success: `{ "status": "success", "data": [...], "meta": { "cursor": "...", "limit": 20, "has_more": true } }`

**API-161: POST /api/v1/msisdn-pool/import** — Bulk import MSISDNs
- Content-Type: multipart/form-data (CSV with `msisdn` column + `operator_id` field) or application/json
- Auth: JWT (tenant_admin+)
- Success: `{ "status": "success", "data": { "total": N, "imported": N, "skipped": N, "errors": [...] } }`

**API-162: POST /api/v1/msisdn-pool/:id/assign** — Assign MSISDN to SIM
- Request body: `{ "sim_id": "uuid" }`
- Auth: JWT (sim_manager+)
- Success: `{ "status": "success", "data": { "id": "...", "msisdn": "...", "state": "assigned", "sim_id": "..." } }`
- Errors: 404 MSISDN_NOT_FOUND, 409 MSISDN_NOT_AVAILABLE

### Database Schema
```sql
-- Source: migrations/20260320000002_core_schema.up.sql (ACTUAL)
CREATE TABLE IF NOT EXISTS msisdn_pool (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID NOT NULL REFERENCES operators(id),
    msisdn VARCHAR(20) NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'available',
    sim_id UUID,
    reserved_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_msisdn_pool_msisdn ON msisdn_pool (msisdn);
CREATE INDEX IF NOT EXISTS idx_msisdn_pool_tenant_op_state ON msisdn_pool (tenant_id, operator_id, state);
CREATE INDEX IF NOT EXISTS idx_msisdn_pool_sim ON msisdn_pool (sim_id) WHERE sim_id IS NOT NULL;
```

## Prerequisites
- [x] STORY-011 completed (SIM CRUD with store and handler)
- [x] TBL-24 msisdn_pool table exists in migration
- [x] MSISDNStore implemented in `internal/store/msisdn.go`
- [x] MSISDN Handler implemented in `internal/api/msisdn/handler.go`
- [x] Error codes CodeMSISDNNotFound and CodeMSISDNNotAvailable defined in `internal/apierr/apierr.go`

## Tasks

### Task 1: Wire MSISDN handler into router and main.go
- **Files:** Modify `internal/gateway/router.go`, Modify `cmd/argus/main.go`
- **Depends on:** — (none)
- **Complexity:** low
- **Pattern ref:** Read `internal/gateway/router.go` — follow the existing IPPoolHandler or SIMHandler wiring pattern
- **Context refs:** Architecture Context > Components Involved, Architecture Context > Data Flow, API Specifications
- **What:**
  1. In `internal/gateway/router.go`:
     - Add import for `msisdnapi "github.com/btopcu/argus/internal/api/msisdn"`
     - Add `MSISDNHandler *msisdnapi.Handler` field to `RouterDeps` struct
     - Register routes with proper RBAC:
       - `GET /api/v1/msisdn-pool` → `deps.MSISDNHandler.List` (RequireRole "sim_manager")
       - `POST /api/v1/msisdn-pool/import` → `deps.MSISDNHandler.Import` (RequireRole "tenant_admin")
       - `POST /api/v1/msisdn-pool/{id}/assign` → `deps.MSISDNHandler.Assign` (RequireRole "sim_manager")
     - Guard with `if deps.MSISDNHandler != nil { ... }` like other handlers
  2. In `cmd/argus/main.go`:
     - Add import for `msisdnapi "github.com/btopcu/argus/internal/api/msisdn"`
     - Create `msisdnStore := store.NewMSISDNStore(pg.Pool)`
     - Create `msisdnHandler := msisdnapi.NewHandler(msisdnStore, log.Logger)`
     - Add `MSISDNHandler: msisdnHandler` to the RouterDeps struct literal
- **Verify:** `cd /Users/btopcu/workspace/argus && go build ./...`

### Task 2: Add router test for MSISDN routes
- **Files:** Modify `internal/gateway/router_test.go`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `internal/gateway/router_test.go` — follow existing TestRouterJobRoutesRegistered pattern
- **Context refs:** API Specifications
- **What:**
  Add `TestRouterMSISDNPoolRoutesRegistered` test function that:
  1. Creates a router with `RouterDeps` where `MSISDNHandler` is nil
  2. Tests that GET `/api/v1/msisdn-pool`, POST `/api/v1/msisdn-pool/import`, and POST `/api/v1/msisdn-pool/{id}/assign` return 404 when handler is nil
  Follow the exact pattern of `TestRouterJobRoutesRegistered`.
- **Verify:** `cd /Users/btopcu/workspace/argus && go test ./internal/gateway/...`

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| POST /api/v1/msisdn-pool/import accepts CSV of MSISDNs per operator | Already in handler.go (Import method) | Task 1 (route wired), Task 2 (route test) |
| GET /api/v1/msisdn-pool lists numbers with state | Already in handler.go (List method) | Task 1 (route wired), Task 2 (route test) |
| POST /api/v1/msisdn-pool/:id/assign assigns MSISDN to SIM | Already in handler.go (Assign method) | Task 1 (route wired), Task 2 (route test) |
| MSISDN globally unique across all tenants/operators | UNIQUE INDEX idx_msisdn_pool_msisdn in migration | Already enforced at DB level |
| Released on SIM termination (after grace period) | store.Release method exists | Called by SIM termination flow |

## Story-Specific Compliance Rules
- API: Standard envelope format (already used in handler via apierr.WriteSuccess/WriteList/WriteError)
- DB: msisdn_pool table already exists in migration — no new migration needed
- RBAC: Import requires tenant_admin+, List and Assign require sim_manager+ (per API index)
- Business: MSISDN uniqueness enforced by UNIQUE INDEX on msisdn column (global across tenants/operators)

## Risks & Mitigations
- **Risk:** None significant — store and handler already implemented and tested. This is pure wiring.
- **Mitigation:** Build verification after each task ensures compilation.
