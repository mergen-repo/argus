# STORY-031: Background Job System

## User Story
As a platform operator, I want a robust background job system with a dashboard, distributed locking, and scheduled tasks, so that bulk operations, OTA commands, and maintenance tasks run reliably without blocking the API.

## Description
NATS JetStream-backed job queue with job types: bulk_import, bulk_state_change, bulk_policy_assign, bulk_esim_switch, ota_command, purge_sweep, ip_reclaim, sla_report. Job dashboard shows running/queued/completed/failed jobs with progress. Distributed lock ensures no concurrent jobs process the same SIM. Scheduled jobs via cron-like scheduler for recurring tasks (purge sweep, IP reclaim, SLA report generation).

## Architecture Reference
- Services: SVC-09 (Job Runner — internal/job)
- API Endpoints: API-120 to API-123
- Database Tables: TBL-20 (jobs)
- Source: docs/architecture/api/_index.md (Jobs section), docs/architecture/services/_index.md (SVC-09)

## Screen Reference
- SCR-080: Job List — job dashboard with status, progress, type, duration, actions

## Acceptance Criteria
- [ ] NATS JetStream consumer processes jobs from durable queue (at-least-once delivery)
- [ ] Job types: bulk_import, bulk_state_change, bulk_policy_assign, bulk_esim_switch, ota_command, purge_sweep, ip_reclaim, sla_report
- [ ] Job state machine: queued → running → completed / failed / cancelled
- [ ] TBL-20 tracks: type, state, tenant_id, created_by, started_at, completed_at, progress_pct, total_items, processed_items, failed_items, error_report (JSONB), locked_by
- [ ] GET /api/v1/jobs lists jobs with filters (type, state, tenant_id), cursor pagination
- [ ] GET /api/v1/jobs/:id returns job detail with progress, error report, duration
- [ ] POST /api/v1/jobs/:id/cancel cancels running job (graceful: finish current batch, stop)
- [ ] POST /api/v1/jobs/:id/retry re-enqueues failed items as new job
- [ ] Distributed lock: Redis-based lock per SIM (SETNX with TTL), prevents concurrent processing
- [ ] Lock TTL auto-extended while job is running (lease renewal)
- [ ] Scheduled jobs: cron expressions stored in config
  - purge_sweep: daily, TERMINATED SIMs past purge_at → state=PURGED, data anonymized
  - ip_reclaim: hourly, terminated SIM IPs released back to pool
  - sla_report: daily, generate SLA compliance report per operator
- [ ] Job progress published via NATS → WebSocket (job.progress, job.completed events)
- [ ] Notification sent to job creator on completion/failure
- [ ] Max concurrent jobs configurable (default: 5 per tenant)
- [ ] Job timeout: auto-fail if no progress update for 30 minutes

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-120 | GET | /api/v1/jobs | `?cursor&limit&type&state` | `[{id,type,state,progress_pct,total_items,created_by,started_at,duration}]` | JWT(sim_manager+) | 200 |
| API-121 | GET | /api/v1/jobs/:id | — | `{id,type,state,progress_pct,total_items,processed_items,failed_items,error_report,created_by,started_at,completed_at}` | JWT(sim_manager+) | 200, 404 |
| API-122 | POST | /api/v1/jobs/:id/cancel | — | `{id,state:"cancelled"}` | JWT(tenant_admin) | 200, 404, 422 |
| API-123 | POST | /api/v1/jobs/:id/retry | — | `{new_job_id,retry_count,state:"queued"}` | JWT(sim_manager+) | 201, 404, 422 |

## Dependencies
- Blocked by: STORY-001 (scaffold — NATS), STORY-002 (DB — TBL-20)
- Blocks: STORY-013 (bulk import), STORY-029 (OTA), STORY-030 (bulk operations), STORY-039 (compliance purge job)

## Test Scenarios
- [ ] Create job → state=queued, published to NATS queue
- [ ] Job consumed by runner → state=running, locked_by set
- [ ] Job completes → state=completed, progress_pct=100, completed_at set
- [ ] Cancel running job → finishes current batch, state=cancelled
- [ ] Retry failed job → new job created with only failed items
- [ ] Distributed lock: two jobs try to process same SIM → second waits
- [ ] Lock TTL expires without renewal → lock released, job marked failed
- [ ] Scheduled purge_sweep → terminated SIMs past purge_at purged
- [ ] Scheduled ip_reclaim → terminated SIM IPs released to pool
- [ ] Job timeout: no progress for 30min → auto-fail
- [ ] WebSocket receives job.progress and job.completed events

## Effort Estimate
- Size: XL
- Complexity: High
