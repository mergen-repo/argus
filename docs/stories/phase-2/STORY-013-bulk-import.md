# STORY-013: Bulk SIM Import (CSV)

## User Story
As a SIM manager, I want to import thousands of SIMs from a CSV file with auto-activation, so that I can onboard large fleets quickly.

## Architecture Reference
- Services: SVC-03 (upload handler), SVC-09 (Job Runner)
- API Endpoints: API-063
- Database Tables: TBL-10 (sims), TBL-11 (history), TBL-20 (jobs)
- Data Flow: FLW-04 (Bulk SIM Import) — docs/architecture/flows/_index.md

## Screen Reference
- SCR-003: Onboarding Wizard Step 4
- SCR-080: Job List

## Acceptance Criteria
- [ ] POST /api/v1/sims/bulk/import accepts CSV file (max 50MB)
- [ ] CSV columns: ICCID, IMSI, MSISDN, operator_code, apn_name
- [ ] Creates background job (TBL-20), returns 202 with job_id
- [ ] Job runner processes rows: validate → create SIM (ordered) → auto-activate → allocate IP → assign default policy
- [ ] Partial success: valid rows applied, invalid rows in error_report JSONB
- [ ] Progress published via NATS (job.progress) → WebSocket to portal
- [ ] Error report downloadable as CSV (row number, ICCID, error reason)
- [ ] Duplicate ICCID/IMSI rows fail gracefully (added to error report, don't stop job)
- [ ] Notification sent on job completion
- [ ] Retry failed items via API-123

## Dependencies
- Blocked by: STORY-011 (SIM CRUD), STORY-006 (NATS/jobs)
- Blocks: None

## Effort Estimate
- Size: L
- Complexity: High
