# Implementation Plan: STORY-013 - Bulk SIM Import (CSV)

## Goal

Wire up the existing bulk SIM import infrastructure (upload handler, job runner, import processor) into the application's router and main.go, add missing error report download endpoint route, and ensure the full pipeline (upload CSV → create job → NATS queue → job runner → process rows → progress events → completion notification) is functional end-to-end.

## Architecture Context

### Components Involved

- **SVC-01 (API Gateway)**: `internal/gateway/router.go` — Chi router, registers HTTP routes. Must add bulk import + job routes.
- **SVC-03 (Core API)**: `internal/api/sim/bulk_handler.go` — Upload handler (already implemented). `internal/api/job/handler.go` — Job CRUD + error report CSV download (already implemented).
- **SVC-09 (Job Runner)**: `internal/job/runner.go` — NATS queue consumer, dispatches to processors. `internal/job/import.go` — BulkImportProcessor (already implemented).
- **NATS Event Bus**: `internal/bus/nats.go` — Publishes job.progress and job.completed events.
- **Store Layer**: `internal/store/job.go` — Job CRUD, progress, lock, complete, error report.

### Data Flow

```
SIM Manager → POST /api/v1/sims/bulk/import (API-063)
    → SVC-01: auth (JWT) + RBAC (sim_manager+) + file upload (CSV, max 50MB)
    → BulkHandler.Import: validate CSV headers, count rows
    → JobStore.Create: insert TBL-20 (jobs), state = 'queued'
    → EventBus.Publish "argus.jobs.queue" with JobMessage
    → Response 202: { status: "success", data: { job_id, status: "queued" } }

SVC-09 (Job Runner) — running in background goroutine:
    → QueueSubscribe "argus.jobs.queue" in "job-runners" queue group
    → JobStore.Lock: set state = 'running', locked_by = worker_id
    → BulkImportProcessor.Process:
        → Parse CSV rows
        → For each row:
            → Validate: ICCID format, IMSI format, operator_code exists, apn_name exists
            → IF valid:
                → SIMStore.Create (state = 'ordered')
                → SIMStore.TransitionState → 'active'
                → IPPoolStore.AllocateIP from APN's pool
                → SIMStore.SetIPAndPolicy (IP + default policy)
            → IF invalid:
                → Append to error_report
            → Every 100 rows: JobStore.UpdateProgress + EventBus.Publish "argus.jobs.progress"
        → JobStore.Complete with error_report + result
        → EventBus.Publish "argus.jobs.completed"
```

### API Specifications

#### POST /api/v1/sims/bulk/import (API-063)
- Auth: JWT (sim_manager+)
- Content-Type: multipart/form-data
- Form field: `file` (CSV, max 50MB)
- CSV columns: `iccid`, `imsi`, `msisdn`, `operator_code`, `apn_name`
- Success response (202 Accepted):
  ```json
  { "status": "success", "data": { "job_id": "uuid", "status": "queued" } }
  ```
- Error responses:
  - 400: `INVALID_FORMAT` — file too large, not multipart, or not CSV
  - 422: `VALIDATION_ERROR` — missing CSV columns or empty file

#### GET /api/v1/jobs (API-120)
- Auth: JWT (sim_manager+)
- Query: `cursor`, `limit`, `type`, `state`
- Success: standard list envelope with job DTOs

#### GET /api/v1/jobs/:id (API-121)
- Auth: JWT (sim_manager+)
- Success: standard success envelope with job DTO

#### POST /api/v1/jobs/:id/cancel (API-122)
- Auth: JWT (tenant_admin)
- Success: `{ "status": "success", "data": { "status": "cancelled" } }`

#### POST /api/v1/jobs/:id/retry (API-123)
- Auth: JWT (sim_manager+)
- Success (202): `{ "status": "success", "data": { "job_id": "uuid", "status": "retry_pending" } }`

#### GET /api/v1/jobs/:id/errors
- Auth: JWT (sim_manager+)
- Query: `format=csv` for CSV download
- Success: JSON array of `{ row, iccid, error }` or CSV file

### Database Schema

```sql
-- Source: migrations/20260320000002_core_schema.up.sql (ACTUAL)
-- TBL-20: jobs
CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    type VARCHAR(50) NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'queued',
    priority INTEGER NOT NULL DEFAULT 5,
    payload JSONB NOT NULL DEFAULT '{}',
    total_items INTEGER NOT NULL DEFAULT 0,
    processed_items INTEGER NOT NULL DEFAULT 0,
    failed_items INTEGER NOT NULL DEFAULT 0,
    progress_pct DECIMAL(5,2) NOT NULL DEFAULT 0,
    error_report JSONB,
    result JSONB,
    max_retries INTEGER NOT NULL DEFAULT 3,
    retry_count INTEGER NOT NULL DEFAULT 0,
    retry_backoff_sec INTEGER NOT NULL DEFAULT 30,
    scheduled_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id),
    locked_by VARCHAR(100),
    locked_at TIMESTAMPTZ
);
-- Indexes already exist in migration

-- TBL-10: sims (already exists, used by import processor)
-- TBL-11: sim_state_history (already exists, used by import processor)
-- TBL-08: ip_pools (already exists, used for IP allocation)
-- TBL-09: ip_addresses (already exists, used for IP allocation)
```

### NATS Subjects (from internal/bus/nats.go)
- `argus.jobs.queue` — Job dispatch queue (WorkQueue policy)
- `argus.jobs.progress` — Progress updates
- `argus.jobs.completed` — Job completion events

## Prerequisites

- [x] STORY-011 completed: SIM CRUD in `internal/store/sim.go` — Create, TransitionState, SetIPAndPolicy
- [x] STORY-010 completed: APN CRUD in `internal/store/apn.go` — GetByName method
- [x] STORY-009 completed: Operator store — GetByCode method
- [x] STORY-006 completed: NATS event bus in `internal/bus/nats.go` — Publish, QueueSubscribe
- [x] STORY-012 completed: SIM segments

## Tasks

### Task 1: Wire bulk import route + job routes in router

- **Files:** Modify `internal/gateway/router.go`
- **Depends on:** — (none)
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — follow the existing route group pattern (JWTAuth + RequireRole)
- **Context refs:** Architecture Context > Components Involved, API Specifications
- **What:**
  - Add `BulkHandler *simapi.BulkHandler` and `JobHandler *jobapi.Handler` fields to `RouterDeps` struct (import `jobapi "github.com/btopcu/argus/internal/api/job"`)
  - Register bulk import route: `POST /api/v1/sims/bulk/import` → `deps.BulkHandler.Import` under JWT + RequireRole("sim_manager")
  - Register job routes under JWT + RequireRole("sim_manager"):
    - `GET /api/v1/jobs` → `deps.JobHandler.List`
    - `GET /api/v1/jobs/{id}` → `deps.JobHandler.Get`
    - `POST /api/v1/jobs/{id}/retry` → `deps.JobHandler.Retry`
    - `GET /api/v1/jobs/{id}/errors` → `deps.JobHandler.ErrorReport`
  - Register job cancel route under JWT + RequireRole("tenant_admin"):
    - `POST /api/v1/jobs/{id}/cancel` → `deps.JobHandler.Cancel`
  - All routes guarded with nil check on `deps.BulkHandler` and `deps.JobHandler`
- **Verify:** `go build ./internal/gateway/...`

### Task 2: Wire bulk import handler, job handler, job runner in main.go

- **Files:** Modify `cmd/argus/main.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `cmd/argus/main.go` — follow existing handler instantiation pattern (NewXxxHandler, passed to RouterDeps)
- **Context refs:** Architecture Context > Components Involved, Architecture Context > Data Flow
- **What:**
  - Import packages: `jobapi "github.com/btopcu/argus/internal/api/job"`, `simapi "github.com/btopcu/argus/internal/api/sim"`, `"github.com/btopcu/argus/internal/job"`
  - Instantiate JobStore: already exists as `store.NewJobStore(pg.Pool)` — add `jobStore := store.NewJobStore(pg.Pool)`
  - Instantiate BulkHandler: `bulkHandler := simapi.NewBulkHandler(jobStore, eventBus, log.Logger)`
  - Instantiate JobHandler: `jobHandler := jobapi.NewHandler(jobStore, eventBus, log.Logger)`
  - Instantiate BulkImportProcessor: `importProcessor := job.NewBulkImportProcessor(jobStore, simStore, operatorStore, apnStore, ippoolStore, eventBus, log.Logger)`
  - Instantiate JobRunner: `jobRunner := job.NewRunner(jobStore, eventBus, log.Logger)`
  - Register processor: `jobRunner.Register(importProcessor)`
  - Start runner: `if err := jobRunner.Start(); err != nil { log.Fatal()... }`
  - Add to RouterDeps: `BulkHandler: bulkHandler, JobHandler: jobHandler`
  - Add graceful shutdown: `jobRunner.Stop()` before NATS close
  - IMPORTANT: The `simapi` package is already imported for `simapi.NewHandler`. The `BulkHandler` constructor is in `internal/api/sim/bulk_handler.go` — same package, no new import needed for it. But do add `jobapi` import alias.
- **Verify:** `go build ./cmd/argus/...`

### Task 3: Add MSISDN pool handler and API key auth middleware to bulk import route (API key support)

- **Files:** Modify `internal/api/sim/bulk_handler.go`
- **Depends on:** — (none)
- **Complexity:** low
- **Pattern ref:** Read `internal/api/sim/bulk_handler.go` — follow existing handler pattern
- **Context refs:** Architecture Context > Data Flow, API Specifications > POST /api/v1/sims/bulk/import
- **What:**
  - The bulk handler currently extracts `tenant_id` from context via `apierr.TenantIDKey` for creating the job (done by JobStore.Create which calls TenantIDFromContext). Verify this works correctly with the JWT middleware.
  - Add `TenantID` field to `bulkImportResponse` struct: `TenantID string \`json:"tenant_id"\`` — include tenant_id in 202 response for client convenience.
  - Ensure the `userIDFromRequest` function correctly extracts user_id from context (already done, using `apierr.UserIDKey`).
  - No major code changes needed — this is a verification + minor enhancement task.
- **Verify:** `go build ./internal/api/sim/...`

### Task 4: Add cancellation check to import processor for long-running jobs

- **Files:** Modify `internal/job/import.go`
- **Depends on:** — (none)
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/import.go` — follow existing loop pattern
- **Context refs:** Architecture Context > Data Flow, Database Schema
- **What:**
  - The current import processor does not check if a job has been cancelled during processing. For large CSVs (50MB = potentially 100K+ rows), the processor should periodically check job state.
  - Add a cancellation check inside the row loop (every `progressInterval` = 100 rows):
    - Query `JobStore.GetByIDInternal` to check if state changed to 'cancelled'
    - If cancelled, break the loop, set final progress, and return `store.ErrJobCancelled` error
  - Add a `CheckCancelled` method to JobStore in `internal/store/job.go`:
    - `func (s *JobStore) CheckCancelled(ctx context.Context, jobID uuid.UUID) (bool, error)` — SELECT state FROM jobs WHERE id = $1, return true if state = 'cancelled'
  - Update the processor to call `p.jobs.CheckCancelled(ctx, job.ID)` every `progressInterval` rows
- **Verify:** `go build ./internal/job/... && go build ./internal/store/...`

### Task 5: Add integration tests for bulk import handler and job wiring

- **Files:** Modify `internal/api/sim/bulk_handler_test.go`, Modify `internal/job/import_test.go`, Modify `internal/api/job/handler_test.go`
- **Depends on:** Task 1, Task 2, Task 3, Task 4
- **Complexity:** high
- **Pattern ref:** Read `internal/api/sim/bulk_handler_test.go` — follow existing test patterns (httptest, multipart form)
- **Context refs:** Acceptance Criteria Mapping, API Specifications, Architecture Context > Data Flow
- **What:**
  - In `bulk_handler_test.go`, add test for valid CSV upload that creates a job (requires mock JobStore + EventBus). Test that:
    - Valid CSV with correct headers returns 202 with job_id
    - Response body contains `{ "status": "success", "data": { "job_id": "...", "status": "queued" } }`
  - In `import_test.go`, add tests for:
    - `TestImportResultStruct`: verify ImportResult and ImportRowError JSON serialization
    - `TestValidateRow_MaxLengthICCID`: test boundary (19 and 22 char ICCIDs)
    - `TestMapColumns_ReorderedHeaders`: verify column mapping works with different column order
  - In `handler_test.go`, add tests for:
    - Error report CSV format download (test `writeErrorReportCSV` helper)
    - Job retry state validation (only failed/completed jobs can be retried)
  - All tests should use `testing` and `httptest` packages, no external test DB required.
- **Verify:** `go test ./internal/api/sim/... && go test ./internal/job/... && go test ./internal/api/job/...`

### Task 6: Add router test for bulk import + job route registration

- **Files:** Modify `internal/gateway/middleware_test.go` or create `internal/gateway/router_test.go`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `internal/gateway/middleware_test.go` — follow existing test patterns
- **Context refs:** API Specifications
- **What:**
  - Add a test that verifies bulk import and job routes are registered when BulkHandler and JobHandler are provided in RouterDeps
  - Test: create router with mock deps, verify POST /api/v1/sims/bulk/import returns non-404 (it should return 401 since no JWT)
  - Test: create router with nil BulkHandler/JobHandler, verify routes return 404
  - This is a smoke test for route registration, not full integration
- **Verify:** `go test ./internal/gateway/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| POST /api/v1/sims/bulk/import accepts CSV (max 50MB) | Already in `bulk_handler.go`, wired in Task 1+2 | Task 5 (bulk_handler_test), Task 6 |
| CSV columns: ICCID, IMSI, MSISDN, operator_code, apn_name | Already in `bulk_handler.go` + `import.go` | Task 5 (import_test) |
| Creates background job (TBL-20), returns 202 with job_id | Already in `bulk_handler.go`, wired in Task 1+2 | Task 5 |
| Job runner processes rows: validate → create → activate → allocate IP → assign policy | Already in `import.go`, wired in Task 2 | Task 5 |
| Partial success: valid rows applied, invalid in error_report | Already in `import.go` | Task 5 |
| Progress via NATS (job.progress) → WebSocket | Already in `import.go` (updateProgressPeriodic) | Manual (NATS) |
| Error report downloadable as CSV | Already in `handler.go` (ErrorReport), wired in Task 1 | Task 5 |
| Duplicate ICCID/IMSI fail gracefully | Already in `import.go` (ErrICCIDExists/ErrIMSIExists handling) | Task 5 |
| Notification on completion | `import.go` publishes job.completed event | Manual |
| Retry failed items via API-123 | Already in `handler.go` (Retry), wired in Task 1 | Task 5 |

## Story-Specific Compliance Rules

- API: Standard envelope `{ status, data, meta?, error? }` — all handlers already follow this pattern
- API: 202 Accepted for async job creation — bulk_handler.go returns 202
- DB: All queries scoped by tenant_id — JobStore.Create uses TenantIDFromContext
- DB: No new migrations needed — TBL-20 (jobs) already exists in core_schema migration
- Business: SIM state machine: ordered → active transition validated by `validateTransition`
- Business: IP allocated from APN's pool on activation
- Business: Partial success — failed rows don't stop the import job

## Risks & Mitigations

- **Risk**: Large CSV files (50MB) may cause memory issues since entire CSV is stored in job payload JSONB
  - **Mitigation**: Current implementation reads CSV to memory and stores in payload. For v1 this is acceptable (50MB limit). Future: store CSV as file reference, not inline in JSONB.
- **Risk**: Job runner not starting if NATS connection is slow
  - **Mitigation**: Job runner Start() subscribes to NATS queue — if NATS is connected (verified earlier in main.go), subscription will succeed.
- **Risk**: Concurrent bulk imports could exhaust IP pool
  - **Mitigation**: IPPoolStore.AllocateIP uses `FOR UPDATE SKIP LOCKED` for concurrent safety.
