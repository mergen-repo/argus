# STORY-030: Bulk Operations (State Change, Policy Assign, Operator Switch)

## User Story
As a SIM manager, I want to perform bulk state changes, policy assignments, and operator switches on SIM segments, so that I can manage large fleets efficiently with partial success handling and undo capability.

## Description
Bulk operations on SIM segments: bulk state change (e.g., suspend all SIMs in segment), bulk policy assign (apply policy version to segment), and bulk eSIM operator switch. All bulk operations run as background jobs via SVC-09 with progress tracking, partial success (continue on individual SIM failure), retry of failed items, downloadable error reports, and undo capability (revert to previous state).

## Architecture Reference
- Services: SVC-03 (Core API), SVC-09 (Job Runner)
- API Endpoints: API-064 to API-066
- Database Tables: TBL-10 (sims), TBL-11 (sim_state_history), TBL-15 (policy_assignments), TBL-12 (esim_profiles), TBL-20 (jobs)
- Data Flows: FLW-06 (eSIM Cross-Operator Switch)
- Source: docs/architecture/api/_index.md (SIM Segments & Bulk section)

## Screen Reference
- SCR-020: SIM List — bulk actions bar (state change, policy assign)
- SCR-080: Job List — bulk job progress, error report download

## Acceptance Criteria
- [ ] POST /api/v1/sims/bulk/state-change accepts segment_id + target_state, creates job
- [ ] POST /api/v1/sims/bulk/policy-assign accepts segment_id + policy_version_id, creates job
- [ ] POST /api/v1/sims/bulk/operator-switch accepts segment_id + target_operator_id, creates job
- [ ] All bulk endpoints return 202 with job_id
- [ ] Job runner processes SIMs sequentially with configurable batch_size (default 100)
- [ ] Partial success: valid SIMs processed, invalid SIMs logged in error_report
- [ ] Error report: JSONB array with {sim_id, iccid, error_code, error_message}
- [ ] Error report downloadable as CSV via job detail endpoint
- [ ] Retry: POST /api/v1/jobs/:id/retry re-processes only failed items
- [ ] Undo: job stores previous_state per SIM, undo job reverts all changes
- [ ] Progress: job.progress_pct updated every batch, published via NATS → WebSocket
- [ ] Bulk state change validates each transition (skip invalid, log error)
- [ ] Bulk policy assign: update TBL-15, send CoA for active sessions
- [ ] Bulk operator switch: disable old profile, enable new profile, update SIM record (FLW-06)
- [ ] Distributed lock: no two bulk jobs can process the same SIM concurrently

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-064 | POST | /api/v1/sims/bulk/state-change | `{segment_id,target_state,reason?}` | `{job_id,status:"queued",estimated_count}` | JWT(sim_manager+) | 202, 400 |
| API-065 | POST | /api/v1/sims/bulk/policy-assign | `{segment_id,policy_version_id}` | `{job_id,status:"queued",estimated_count}` | JWT(policy_editor+) | 202, 400 |
| API-066 | POST | /api/v1/sims/bulk/operator-switch | `{segment_id,target_operator_id,target_apn_id}` | `{job_id,status:"queued",estimated_count}` | JWT(tenant_admin) | 202, 400 |

## Dependencies
- Blocked by: STORY-011 (SIM CRUD), STORY-012 (segments), STORY-028 (eSIM profiles), STORY-031 (job runner)
- Blocks: None

## Test Scenarios
- [ ] Bulk state change: suspend 100 active SIMs → 100 suspended, history entries created
- [ ] Bulk state change: 5 SIMs already suspended → 5 errors in report, 95 succeed
- [ ] Bulk policy assign: assign policy to segment → TBL-15 updated, CoA sent
- [ ] Bulk operator switch: switch eSIM segment → profiles switched, SIM records updated
- [ ] Retry: re-process 5 failed items from previous job → 5 retried
- [ ] Undo: revert bulk suspend → all SIMs back to active state
- [ ] Progress: WebSocket receives job.progress events during processing
- [ ] Concurrent bulk jobs on overlapping segments → second job waits (distributed lock)
- [ ] Error report CSV download → valid CSV with error details

## Effort Estimate
- Size: XL
- Complexity: High
