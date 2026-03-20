# STORY-013 Deliverable: Bulk SIM Import (CSV)

## Summary

Bulk SIM import from CSV with background job processing, partial success handling, NATS progress events, job cancellation, and error report CSV download. 6 new API routes for import upload and job management.

## Acceptance Criteria Status

| # | Criteria | Status |
|---|---------|--------|
| 1 | POST /api/v1/sims/bulk/import accepts CSV (max 50MB) | DONE |
| 2 | CSV columns: ICCID, IMSI, MSISDN, operator_code, apn_name | DONE |
| 3 | Creates background job, returns 202 with job_id | DONE |
| 4 | Job runner: validate → create SIM → auto-activate → allocate IP → assign policy | DONE |
| 5 | Partial success: valid rows applied, invalid in error_report JSONB | DONE |
| 6 | Progress via NATS (job.progress) | DONE |
| 7 | Error report downloadable as CSV | DONE |
| 8 | Duplicate ICCID/IMSI fail gracefully (error report, don't stop job) | DONE |
| 9 | Notification sent on job completion | DONE |
| 10 | Retry failed items via API | DONE |

## Files Changed

| File | Change |
|------|--------|
| `internal/api/sim/bulk_handler.go` | MODIFIED — Added tenant_id to 202 response |
| `internal/job/import.go` | MODIFIED — Added cancellation check every 100 rows |
| `internal/store/job.go` | MODIFIED — Added CheckCancelled method, extended Cancel for running jobs |
| `internal/gateway/router.go` | MODIFIED — 6 new routes (bulk import + job CRUD + cancel) |
| `cmd/argus/main.go` | MODIFIED — Wired JobStore, BulkHandler, JobHandler, BulkImportProcessor, JobRunner |
| `internal/gateway/router_test.go` | MODIFIED — Route registration tests |
| `internal/job/import_test.go` | MODIFIED — Import processor tests (serialization, column reorder, ICCID boundary) |
| `internal/api/job/handler_test.go` | MODIFIED — Job DTO tests, error report format |

## Architecture References Fulfilled

- SVC-03: Upload handler (POST multipart)
- SVC-09: Job Runner (background processing)
- API-063: Bulk SIM import
- TBL-20 (jobs): Job lifecycle management
- FLW-04: Bulk SIM Import data flow

## Gate Results

- Gate Status: PASS
- Fixes Applied: 3 (HasMore in Job ListMeta, Cancel support for running jobs, 422 for non-cancellable states)
- Escalated: 0

## Test Coverage

- 25 story-specific tests across 3 packages
- Full suite: 29/29 packages pass
