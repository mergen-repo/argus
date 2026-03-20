# Implementation Plan: STORY-012 - SIM Segments & Group-First UX

## Goal
Deliver SIM segment CRUD endpoints (API-060 to API-062) with route registration, state summary counts, and wire everything into the application bootstrap — enabling saved filter combinations for managing SIM fleets at scale.

## Architecture Context

### Components Involved
- **SVC-03 (Core API)**: Handles SIM segment CRUD handlers in `internal/api/segment/`
- **Store layer**: `internal/store/segment.go` — SegmentStore with pgx/pgxpool
- **Gateway router**: `internal/gateway/router.go` — chi router with middleware chain
- **Bootstrap**: `cmd/argus/main.go` — service initialization and dependency injection

### Data Flow
```
Client → POST /api/v1/sim-segments → JWTAuth → RequireRole(sim_manager) → segment.Handler.Create
  → store.SegmentStore.Create(ctx, params) → INSERT INTO sim_segments → return segment DTO

Client → GET /api/v1/sim-segments → JWTAuth → RequireRole(sim_manager) → segment.Handler.List
  → store.SegmentStore.List(ctx, cursor, limit) → SELECT FROM sim_segments → return list + cursor

Client → GET /api/v1/sim-segments/:id/count → JWTAuth → RequireRole(sim_manager) → segment.Handler.Count
  → store.SegmentStore.CountMatchingSIMs(ctx, id) → COUNT(*) FROM sims WHERE filters → return count

Client → DELETE /api/v1/sim-segments/:id → JWTAuth → RequireRole(sim_manager) → segment.Handler.Delete
  → store.SegmentStore.Delete(ctx, id) → DELETE FROM sim_segments → 204 No Content

Client → GET /api/v1/sim-segments/:id/summary → JWTAuth → RequireRole(sim_manager) → segment.Handler.StateSummary
  → store.SegmentStore.StateSummary(ctx, id) → COUNT(*) ... GROUP BY state FROM sims → return summary
```

### API Specifications

**API-060: List Segments**
- `GET /api/v1/sim-segments`
- Query params: `cursor` (string), `limit` (int, default 20, max 100)
- Auth: JWT (sim_manager+)
- Success 200: `{ "status": "success", "data": [{ "id", "tenant_id", "name", "filter_definition", "created_by", "created_at" }], "meta": { "cursor", "limit", "has_more" } }`

**API-061: Create Segment**
- `POST /api/v1/sim-segments`
- Request body: `{ "name": string (required), "filter_definition": object (required) }`
- Filter definition fields: `{ "operator_id": UUID, "state": string, "apn_id": UUID, "rat_type": string }`
- Auth: JWT (sim_manager+)
- Success 201: `{ "status": "success", "data": { "id", "tenant_id", "name", "filter_definition", "created_by", "created_at" } }`
- Error 409: `{ "status": "error", "error": { "code": "ALREADY_EXISTS", "message": "A segment with this name already exists" } }`
- Error 422: Validation errors for missing name or filter_definition

**API-062: Count SIMs in Segment**
- `GET /api/v1/sim-segments/:id/count`
- Auth: JWT (sim_manager+)
- Success 200: `{ "status": "success", "data": { "segment_id": UUID, "count": int64 } }`
- Error 404: Segment not found

**Delete Segment** (supplementary)
- `DELETE /api/v1/sim-segments/:id`
- Auth: JWT (sim_manager+)
- Success 204: No Content
- Error 404: Segment not found

**State Summary** (AC-5: summary cards per state)
- `GET /api/v1/sim-segments/:id/summary`
- Auth: JWT (sim_manager+)
- Success 200: `{ "status": "success", "data": { "segment_id": UUID, "total": int64, "by_state": { "active": int64, "suspended": int64, "ordered": int64, ... } } }`
- Error 404: Segment not found

### Database Schema

```sql
-- Source: migrations/20260320000002_core_schema.up.sql (ACTUAL — already exists)
CREATE TABLE IF NOT EXISTS sim_segments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name VARCHAR(100) NOT NULL,
    filter_definition JSONB NOT NULL DEFAULT '{}',
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sim_segments_tenant_name ON sim_segments (tenant_id, name);
```

No migration needed — TBL-25 already exists in the core schema migration.

### Existing Implementation Status

The following code already exists from a previous partial implementation:
- `internal/store/segment.go` — SegmentStore with Create, GetByID, List, Delete, CountMatchingSIMs
- `internal/store/segment_test.go` — Filter parse tests
- `internal/api/segment/handler.go` — Handler with List, Create, Delete, Count
- `internal/api/segment/handler_test.go` — DTO and validation tests

**What is MISSING (must be implemented):**
1. Route registration in `internal/gateway/router.go` — SegmentHandler not in RouterDeps, no routes
2. SegmentStore/Handler initialization in `cmd/argus/main.go`
3. StateSummary store method and handler endpoint (AC-5: "Summary cards show counts per state")
4. GetByID handler endpoint (useful for segment detail, though not in AC list)

## Prerequisites
- [x] STORY-011 completed (SIM CRUD — `internal/store/sim.go`, `internal/api/sim/handler.go`)
- [x] TBL-25 sim_segments table exists in core_schema migration
- [x] Store and handler code partially implemented

## Tasks

### Task 1: Add StateSummary to SegmentStore
- **Files:** Modify `internal/store/segment.go`
- **Depends on:** — (none)
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/segment.go` — follow existing CountMatchingSIMs pattern
- **Context refs:** Database Schema, API Specifications > State Summary
- **What:** Add `StateSummary(ctx, id uuid.UUID) (map[string]int64, int64, error)` method to SegmentStore. It should:
  1. Call GetByID to fetch the segment and its filter_definition
  2. Parse filter_definition into SegmentFilter
  3. Build WHERE clause using same pattern as CountMatchingSIMs (tenant_id + optional operator_id, state, apn_id, rat_type)
  4. Execute: `SELECT state, COUNT(*) FROM sims WHERE [filters] GROUP BY state`
  5. Return map[string]int64 (state->count) and total int64
  6. Use same dynamic query builder pattern as CountMatchingSIMs
- **Verify:** `go build ./internal/store/...`

### Task 2: Add StateSummary and GetByID handler endpoints
- **Files:** Modify `internal/api/segment/handler.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/segment/handler.go` — follow existing Count handler pattern
- **Context refs:** API Specifications > State Summary, Architecture Context > Data Flow
- **What:** Add two new handler methods:
  1. `GetByID(w, r)` — extract ID from chi URL param, call store.GetByID, return segmentDTO with WriteSuccess
  2. `StateSummary(w, r)` — extract ID from chi URL param, call store.StateSummary, return summaryDTO
  3. Define `summaryDTO` struct: `{ SegmentID uuid.UUID, Total int64, ByState map[string]int64 }`
  4. Handle errors: 400 bad ID format, 404 not found, 500 internal
  5. Follow same error handling pattern as Count handler
- **Verify:** `go build ./internal/api/segment/...`

### Task 3: Register segment routes in router and wire in main.go
- **Files:** Modify `internal/gateway/router.go`, Modify `cmd/argus/main.go`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — follow SIMHandler registration pattern
- **Context refs:** Architecture Context > Components Involved, API Specifications
- **What:**
  In `internal/gateway/router.go`:
  1. Add import for `segmentapi "github.com/btopcu/argus/internal/api/segment"`
  2. Add `SegmentHandler *segmentapi.Handler` to RouterDeps struct
  3. Add route group with JWTAuth + RequireRole("sim_manager"):
     - `GET /api/v1/sim-segments` → SegmentHandler.List
     - `POST /api/v1/sim-segments` → SegmentHandler.Create
     - `GET /api/v1/sim-segments/{id}` → SegmentHandler.GetByID
     - `DELETE /api/v1/sim-segments/{id}` → SegmentHandler.Delete
     - `GET /api/v1/sim-segments/{id}/count` → SegmentHandler.Count
     - `GET /api/v1/sim-segments/{id}/summary` → SegmentHandler.StateSummary
  4. Wrap in `if deps.SegmentHandler != nil { ... }` guard (same pattern as other handlers)

  In `cmd/argus/main.go`:
  1. Add import for `segmentapi "github.com/btopcu/argus/internal/api/segment"`
  2. After simStore/simHandler initialization, add:
     - `segmentStore := store.NewSegmentStore(pg.Pool)`
     - `segmentHandler := segmentapi.NewHandler(segmentStore, log.Logger)`
  3. Add `SegmentHandler: segmentHandler` to RouterDeps struct literal
- **Verify:** `go build ./cmd/argus/...`

### Task 4: Add tests for new functionality
- **Files:** Modify `internal/store/segment_test.go`, Modify `internal/api/segment/handler_test.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** low
- **Pattern ref:** Read `internal/api/segment/handler_test.go` — follow existing test patterns
- **Context refs:** API Specifications > State Summary, Database Schema
- **What:**
  In `internal/store/segment_test.go`:
  1. Add test for StateSummary result parsing — verify map[string]int64 marshals correctly

  In `internal/api/segment/handler_test.go`:
  1. Add test for summaryDTO JSON serialization — verify segment_id, total, by_state fields
  2. Add test for GetByID handler error cases (bad UUID format)
- **Verify:** `go test ./internal/store/... ./internal/api/segment/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| POST /api/v1/sim-segments creates segment with filter definition | Already in handler.go (Create) | Task 3 (route registration), Task 4 (tests) |
| GET /api/v1/sim-segments/:id/count returns count (<5s for 10M) | Already in handler.go (Count) + store.go (CountMatchingSIMs with index-optimized SQL) | Task 3 (route registration) |
| Segment filter executes as optimized SQL using indexes | Already in store.go (dynamic WHERE with indexed columns: operator_id, state, apn_id, rat_type) | Existing indexes on sims table |
| Bulk action toolbar appears when SIMs selected | Frontend — deferred to Phase 7 UI stories | N/A (backend only in this phase) |
| Summary cards show counts per state filtered by current segment | Task 1 (StateSummary store), Task 2 (StateSummary handler) | Task 4 (tests) |

## Story-Specific Compliance Rules

- API: Standard envelope `{ status, data, meta? }` for all responses (from apierr package)
- DB: All queries scoped by tenant_id (enforced in store layer via TenantIDFromContext)
- DB: Cursor-based pagination for list endpoints
- Auth: JWT + RBAC sim_manager+ role required
- Audit: State-changing operations (create, delete) should create audit entries (can be added later)
- Naming: Go camelCase, routes kebab-case, DB snake_case

## Risks & Mitigations

- **Risk**: StateSummary on 10M SIMs could be slow without proper indexes
  - **Mitigation**: sims table has `idx_sims_tenant_state`, `idx_sims_tenant_operator`, `idx_sims_tenant_apn` indexes. GROUP BY state with WHERE on indexed columns should be fast. Consider adding EXPLAIN ANALYZE verification in integration tests.
- **Risk**: Existing store/handler code might have bugs not caught by unit tests
  - **Mitigation**: Integration with router will surface any issues during build step
