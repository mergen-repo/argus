# Implementation Plan: STORY-031 - Background Job System

## Goal
Extend the existing job runner (internal/job) with distributed Redis locking, scheduled cron jobs, job timeout detection, concurrency limiting, cancel/retry API improvements, and WebSocket progress broadcasting.

## Architecture Context

### Components Involved
- **SVC-09 Job Runner** (`internal/job/`): NATS-backed job processor with processor registration pattern. Already has basic runner, import, dry-run, disconnect, rollout processors.
- **Store layer** (`internal/store/job.go`): JobStore with Create, GetByID, List, Lock, UpdateProgress, Complete, Fail, Cancel, SetRetryPending, CheckCancelled.
- **Event Bus** (`internal/bus/nats.go`): NATS JetStream with subjects `argus.jobs.queue`, `argus.jobs.progress`, `argus.jobs.completed`. Streams: EVENTS (LimitsPolicy) and JOBS (WorkQueuePolicy).
- **Cache/Redis** (`internal/cache/redis.go`): Redis client wrapper. Used for rate limiting, caching. Will be extended for distributed SIM locks.
- **WebSocket Hub** (`internal/ws/hub.go`): Broadcasts events to tenant-scoped WS connections. Already maps `argus.jobs.progress` -> `job.progress` and `argus.jobs.completed` -> `job.completed`.
- **API Handler** (`internal/api/job/handler.go`): List, Get, Cancel, Retry, ErrorReport endpoints already exist.
- **Gateway Router** (`internal/gateway/router.go`): Job routes already registered.
- **Config** (`internal/config/config.go`): Environment variable configuration via envconfig.

### Data Flow
```
1. Job created (via API/internal) -> stored in DB (state=queued) -> published to NATS argus.jobs.queue
2. Runner consumes from NATS queue -> locks job in DB -> calls processor.Process()
3. Processor updates progress -> publishes argus.jobs.progress -> WS hub relays to browser
4. Processor completes/fails -> publishes argus.jobs.completed -> WS hub relays
5. Scheduled jobs: cron scheduler ticks -> creates job in DB -> publishes to NATS
6. Distributed lock: processor acquires Redis SETNX per-SIM -> processes -> releases lock
7. Timeout detector: periodic sweep -> finds stale running jobs -> marks failed
```

### API Specifications

Already implemented in `internal/api/job/handler.go`:
- **API-120** `GET /api/v1/jobs` — List jobs with `?cursor&limit&type&state` filters. Auth: JWT(sim_manager+). Returns 200.
- **API-121** `GET /api/v1/jobs/:id` — Get job detail. Auth: JWT(sim_manager+). Returns 200, 404.
- **API-122** `POST /api/v1/jobs/:id/cancel` — Cancel job. Auth: JWT(tenant_admin). Returns 200, 404, 422.
- **API-123** `POST /api/v1/jobs/:id/retry` — Retry failed job. Auth: JWT(sim_manager+). Returns 201/202, 404, 422.

The API handlers exist but need enhancements:
- Cancel should return `{id, state: "cancelled"}` per spec
- Retry should create a new job with failed items, return `{new_job_id, retry_count, state: "queued"}` with 201 status
- Both need duration field in Get response

### Database Schema

Source: `migrations/20260320000002_core_schema.up.sql` (ACTUAL)
```sql
CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    type VARCHAR(50) NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'queued',
    priority INTEGER NOT NULL DEFAULT 5,
    payload JSONB NOT NULL,
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

CREATE INDEX IF NOT EXISTS idx_jobs_tenant_state ON jobs (tenant_id, state);
CREATE INDEX IF NOT EXISTS idx_jobs_state_priority ON jobs (state, priority) WHERE state = 'queued';
CREATE INDEX IF NOT EXISTS idx_jobs_scheduled ON jobs (scheduled_at) WHERE state = 'queued' AND scheduled_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_locked ON jobs (locked_by) WHERE locked_by IS NOT NULL;
```

No schema changes needed -- existing table already has all required columns including `locked_by`, `locked_at`, `scheduled_at`.

### Existing Job Types
- `bulk_sim_import` (import.go)
- `bulk_session_disconnect` (bulk_disconnect.go)
- `policy_dry_run` (dryrun.go)
- `policy_rollout_stage` (rollout.go)

### Job Type Constants Needed
New job types to register (processor stubs for future stories):
- `bulk_state_change`
- `bulk_policy_assign`
- `ota_command`
- `purge_sweep`
- `ip_reclaim`
- `sla_report`

## Prerequisites
- [x] STORY-001 completed (scaffold, NATS)
- [x] STORY-002 completed (DB, TBL-20 jobs table)
- [x] STORY-013 completed (basic job runner, import processor)

## Tasks

### Task 1: Distributed Lock Manager (Redis SETNX)
- **Files:** Create `internal/job/lock.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/cache/redis.go` for Redis client usage pattern, `internal/job/runner.go` for job package conventions
- **Context refs:** Architecture Context > Components Involved, Data Flow (step 6)
- **What:**
  Create a `DistributedLock` struct in `internal/job/lock.go` that provides per-SIM distributed locking via Redis:
  - `type DistributedLock struct` with `redis.Client`, `logger`, `ttl` (default 60s)
  - `func NewDistributedLock(client *redis.Client, logger zerolog.Logger) *DistributedLock`
  - `func (dl *DistributedLock) Acquire(ctx context.Context, key string, holderID string, ttl time.Duration) (bool, error)` — Redis SETNX with TTL on key `argus:lock:{key}` with value `holderID`
  - `func (dl *DistributedLock) Release(ctx context.Context, key string, holderID string) error` — Lua script to atomically check holder and DEL (only release if we own the lock)
  - `func (dl *DistributedLock) Renew(ctx context.Context, key string, holderID string, ttl time.Duration) (bool, error)` — Lua script to atomically check holder and PEXPIRE (lease renewal)
  - `func (dl *DistributedLock) IsHeld(ctx context.Context, key string) (bool, error)` — EXISTS check
  - Key format: `argus:lock:sim:{simID}` for SIM-level locks
  - Default TTL: 60 seconds, renewed every 30 seconds by the runner
  - Lua scripts for atomic check-and-release and check-and-renew to prevent releasing another worker's lock
- **Verify:** `go build ./internal/job/...`

### Task 2: Enhanced Runner with Concurrency Control, Timeout Detection, and Lock Renewal
- **Files:** Modify `internal/job/runner.go`, Create `internal/job/timeout.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/job/runner.go` (existing runner pattern), `internal/aaa/session/timeout_sweeper.go` for periodic ticker pattern
- **Context refs:** Architecture Context > Components Involved, Architecture Context > Data Flow, Database Schema
- **What:**
  Enhance the Runner to support:

  **Runner changes (`runner.go`):**
  - Add `distLock *DistributedLock` field to Runner struct
  - Add `maxConcurrent int` field (default 5), `activeMu sync.Mutex`, `activeCount map[uuid.UUID]int` (per-tenant concurrency tracking)
  - Add `cancelMu sync.RWMutex`, `cancelFuncs map[uuid.UUID]context.CancelFunc` for graceful cancel support
  - Update `NewRunner` signature to accept `*DistributedLock` and `maxConcurrent int`
  - In `handleMessage`: check per-tenant active count before starting goroutine; if at max, NAK/requeue the message
  - In `processJob`: create cancellable context, store cancel func in map; start lease renewal goroutine that renews DB lock every 30s; on completion remove cancel func and decrement active count
  - Add `CancelJob(jobID uuid.UUID)` method that calls stored cancel func
  - Publish `job.progress` and `job.completed` events with tenant_id for WS routing

  **Timeout detector (`timeout.go`):**
  - `type TimeoutDetector struct` with `jobs *store.JobStore`, `eventBus`, `logger`, `interval time.Duration` (default 5 min), `timeout time.Duration` (default 30 min)
  - `func NewTimeoutDetector(jobs *store.JobStore, eventBus *bus.EventBus, logger zerolog.Logger) *TimeoutDetector`
  - `func (td *TimeoutDetector) Start()` — starts ticker goroutine
  - `func (td *TimeoutDetector) Stop()` — stops ticker
  - `func (td *TimeoutDetector) sweep(ctx context.Context)` — queries `SELECT id FROM jobs WHERE state = 'running' AND locked_at < NOW() - INTERVAL '30 minutes'`, fails each stale job
  - Add `FindTimedOutJobs(ctx context.Context, timeout time.Duration) ([]Job, error)` to JobStore
- **Verify:** `go build ./internal/job/...`

### Task 3: Cron Scheduler for Scheduled Jobs
- **Files:** Create `internal/job/scheduler.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/session/timeout_sweeper.go` for periodic task pattern, `internal/job/runner.go` for job package conventions
- **Context refs:** Architecture Context > Components Involved, Architecture Context > Data Flow (step 5), Database Schema
- **What:**
  Create a cron-like scheduler that creates jobs at configured intervals:
  - `type CronEntry struct` with `Name string`, `Schedule string` (cron expression), `JobType string`, `TenantID *uuid.UUID` (nil = all tenants), `Payload json.RawMessage`
  - `type Scheduler struct` with `jobs *store.JobStore`, `eventBus *bus.EventBus`, `entries []CronEntry`, `logger`, `stopCh`, `wg`
  - `func NewScheduler(jobs *store.JobStore, eventBus *bus.EventBus, logger zerolog.Logger) *Scheduler`
  - `func (s *Scheduler) AddEntry(entry CronEntry)` — registers a cron entry
  - `func (s *Scheduler) Start()` — starts a goroutine that checks every minute if any entry is due to fire. Use simple cron parsing (support `@daily`, `@hourly`, and basic `min hour dom month dow` syntax) or use `robfig/cron/v3` parser
  - `func (s *Scheduler) Stop()` — graceful stop
  - When entry fires: create job via `JobStore.CreateWithTenantID` (or for system-wide, use a system tenant context), publish to NATS queue
  - Built-in schedules (configurable via config):
    - `purge_sweep`: `@daily` — type `purge_sweep`
    - `ip_reclaim`: `@hourly` — type `ip_reclaim`
    - `sla_report`: `@daily` — type `sla_report`
  - Use Redis SETNX with short TTL to prevent duplicate scheduling in multi-instance deployments (key: `argus:cron:last:{jobType}:{timestamp}`)
- **Verify:** `go build ./internal/job/...`

### Task 4: Job Type Constants and Stub Processors
- **Files:** Create `internal/job/types.go`, Create `internal/job/stubs.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/job/import.go` for processor pattern (Type() string, Process method)
- **Context refs:** Architecture Context > Existing Job Types, Job Type Constants Needed
- **What:**
  - Create `internal/job/types.go` with all job type constants:
    ```
    const (
      JobTypeBulkImport        = "bulk_sim_import"       // already in import.go, move here
      JobTypeBulkDisconnect    = "bulk_session_disconnect" // already in bulk_disconnect.go, move here
      JobTypeBulkStateChange   = "bulk_state_change"
      JobTypeBulkPolicyAssign  = "bulk_policy_assign"
      JobTypeBulkEsimSwitch    = "bulk_esim_switch"
      JobTypeOTACommand        = "ota_command"
      JobTypePurgeSweep        = "purge_sweep"
      JobTypeIPReclaim         = "ip_reclaim"
      JobTypeSLAReport         = "sla_report"
      JobTypePolicyDryRun      = "policy_dry_run"
      JobTypeRolloutStage      = "policy_rollout_stage"
    )
    ```
  - Remove duplicate constants from `import.go` and `bulk_disconnect.go`, import from `types.go`
  - Create `internal/job/stubs.go` with placeholder processors for new types (purge_sweep, ip_reclaim, sla_report) that just log and complete — future stories will implement real logic:
    - `type StubProcessor struct { jobType string; jobs *store.JobStore; logger zerolog.Logger }`
    - `func NewStubProcessor(jobType string, jobs *store.JobStore, logger zerolog.Logger) *StubProcessor`
    - Process just completes the job with a result `{"status": "stub", "message": "not yet implemented"}`
- **Verify:** `go build ./internal/job/...`

### Task 5: Store Enhancements — Timed-Out Jobs Query and Retry with New Job
- **Files:** Modify `internal/store/job.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/job.go` (existing patterns)
- **Context refs:** Database Schema, Architecture Context > Components Involved
- **What:**
  Add new methods to `JobStore`:
  - `func (s *JobStore) FindTimedOutJobs(ctx context.Context, timeout time.Duration) ([]Job, error)` — `SELECT * FROM jobs WHERE state = 'running' AND locked_at < NOW() - $1::interval` (returns jobs that haven't been updated within timeout)
  - `func (s *JobStore) CreateRetryJob(ctx context.Context, original *Job, failedPayload json.RawMessage) (*Job, error)` — creates new job with same type/tenant but new ID, state=queued, payload=failedPayload, retry_count=original.retry_count+1, links to original via result JSONB field `{"retry_of": "original_id"}`
  - `func (s *JobStore) CountActiveByTenant(ctx context.Context, tenantID uuid.UUID) (int, error)` — `SELECT COUNT(*) FROM jobs WHERE tenant_id = $1 AND state = 'running'`
  - `func (s *JobStore) TouchLock(ctx context.Context, jobID uuid.UUID, lockedBy string) error` — `UPDATE jobs SET locked_at = NOW() WHERE id = $1 AND locked_by = $2 AND state = 'running'` (heartbeat for timeout detection)
- **Verify:** `go build ./internal/store/...`

### Task 6: API Handler Enhancements — Cancel Response and Retry with New Job
- **Files:** Modify `internal/api/job/handler.go`
- **Depends on:** Task 5
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/job/handler.go` (existing handler), read `internal/api/sim/handler.go` for similar patterns
- **Context refs:** API Specifications, Architecture Context > Components Involved
- **What:**
  Enhance existing handlers to match API contract:
  - **Cancel handler**: After successful cancel, return `{id: jobID, state: "cancelled"}` as per spec. Also, if Runner is available, call `runner.CancelJob(jobID)` to signal graceful stop to running processor. Add Runner reference to Handler struct (optional, nil if not wired).
  - **Retry handler**: Instead of setting retry_pending on same job, create a NEW job via `JobStore.CreateRetryJob()`. Return 201 with `{new_job_id, retry_count, state: "queued"}`. Publish new job to NATS queue.
  - **Get handler**: Add `duration` field to jobDTO — calculated as `completed_at - started_at` (or `now - started_at` if running). Add `locked_by` to detail response.
  - Update `jobDTO` to include `Duration *string` and `LockedBy *string` fields
- **Verify:** `go build ./internal/api/...`

### Task 7: Config — Job System Settings
- **Files:** Modify `internal/config/config.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/config/config.go` (existing config pattern)
- **Context refs:** Architecture Context > Components Involved
- **What:**
  Add job system configuration fields:
  - `JobMaxConcurrentPerTenant int` — `envconfig:"JOB_MAX_CONCURRENT_PER_TENANT" default:"5"`
  - `JobTimeoutMinutes int` — `envconfig:"JOB_TIMEOUT_MINUTES" default:"30"`
  - `JobTimeoutCheckInterval time.Duration` — `envconfig:"JOB_TIMEOUT_CHECK_INTERVAL" default:"5m"`
  - `JobLockTTL time.Duration` — `envconfig:"JOB_LOCK_TTL" default:"60s"`
  - `JobLockRenewInterval time.Duration` — `envconfig:"JOB_LOCK_RENEW_INTERVAL" default:"30s"`
  - `CronPurgeSweep string` — `envconfig:"CRON_PURGE_SWEEP" default:"@daily"`
  - `CronIPReclaim string` — `envconfig:"CRON_IP_RECLAIM" default:"@hourly"`
  - `CronSLAReport string` — `envconfig:"CRON_SLA_REPORT" default:"@daily"`
  - `CronEnabled bool` — `envconfig:"CRON_ENABLED" default:"true"`
- **Verify:** `go build ./internal/config/...`

### Task 8: Main.go Integration and WebSocket Subscription
- **Files:** Modify `cmd/argus/main.go`
- **Depends on:** Task 1, Task 2, Task 3, Task 4, Task 5, Task 6, Task 7
- **Complexity:** high
- **Pattern ref:** Read `cmd/argus/main.go` (existing wiring pattern — see how jobRunner, healthChecker, notifSvc are created and started)
- **Context refs:** Architecture Context > Components Involved, Architecture Context > Data Flow
- **What:**
  Wire all new components in main.go:
  - Create `DistributedLock` using `rdb.Client`
  - Update `NewRunner` call to pass `DistributedLock` and `cfg.JobMaxConcurrentPerTenant`
  - Create `TimeoutDetector` with `jobStore`, `eventBus`, configure timeout from `cfg.JobTimeoutMinutes`
  - Start timeout detector, add to shutdown sequence
  - Create `Scheduler`, add cron entries from config (if `cfg.CronEnabled`):
    - `purge_sweep` with schedule from `cfg.CronPurgeSweep`
    - `ip_reclaim` with schedule from `cfg.CronIPReclaim`
    - `sla_report` with schedule from `cfg.CronSLAReport`
  - Register stub processors for new job types
  - Start scheduler, add to shutdown sequence
  - Add `argus.jobs.progress` and `argus.jobs.completed` to WebSocket hub NATS subscriptions (already in hub.go mapping but need to ensure subscription in main.go)
  - Pass Runner reference to job API handler for cancel support
- **Verify:** `go build ./cmd/argus/...`

### Task 9: Tests — Lock, Scheduler, Timeout, Runner Enhancements
- **Files:** Create `internal/job/lock_test.go`, Create `internal/job/scheduler_test.go`, Create `internal/job/timeout_test.go`, Modify `internal/job/runner_test.go`
- **Depends on:** Task 1, Task 2, Task 3, Task 4, Task 5
- **Complexity:** high
- **Pattern ref:** Read `internal/job/runner_test.go`, `internal/job/import_test.go` for test patterns
- **Context refs:** Architecture Context, Database Schema
- **What:**
  - **lock_test.go**: Test acquire (SETNX), release (only owner can release), renew (extend TTL), concurrent acquire (second fails), acquire after release. Use miniredis or mock.
  - **scheduler_test.go**: Test cron expression parsing (`@daily`, `@hourly`, `0 * * * *`), entry scheduling fires at correct time, duplicate prevention, stop/start lifecycle.
  - **timeout_test.go**: Test sweep finds stale jobs, marks them failed, publishes event. Test non-stale jobs are untouched.
  - **runner_test.go**: Extend existing tests — test max concurrency enforcement, test cancel via context, test lease renewal.
  - **types_test.go**: Test all job type constants are unique.
- **Verify:** `go test ./internal/job/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| NATS JetStream consumer (at-least-once) | Already exists in runner.go | Task 2 enhances |
| Job types registered | Task 4 | Task 9 |
| Job state machine (queued→running→completed/failed/cancelled) | Already in store, enhanced by Task 2 | Task 9 |
| TBL-20 tracks all fields | Already exists in migration | — |
| GET /api/v1/jobs (API-120) | Already exists | Task 6 adds duration |
| GET /api/v1/jobs/:id (API-121) | Already exists | Task 6 adds duration/locked_by |
| POST /api/v1/jobs/:id/cancel (API-122) | Already exists, Task 6 enhances response | Task 9 |
| POST /api/v1/jobs/:id/retry (API-123) | Task 6 creates new job | Task 9 |
| Distributed lock (Redis SETNX with TTL) | Task 1 | Task 9 |
| Lock TTL auto-extended (lease renewal) | Task 2 | Task 9 |
| Scheduled jobs (cron) | Task 3 | Task 9 |
| purge_sweep daily | Task 3 + Task 4 (stub) | Task 9 |
| ip_reclaim hourly | Task 3 + Task 4 (stub) | Task 9 |
| sla_report daily | Task 3 + Task 4 (stub) | Task 9 |
| Job progress via WS | Task 8 (subscription wiring) | Manual |
| Max concurrent jobs per tenant | Task 2 | Task 9 |
| Job timeout (30 min auto-fail) | Task 2 | Task 9 |

## Story-Specific Compliance Rules

- **API**: Standard envelope `{ status, data, meta?, error? }` for all responses (apierr package)
- **DB**: All queries scoped by tenant_id (enforced in store layer). No schema changes needed.
- **Business**:
  - purge_sweep: TERMINATED SIMs past `purge_at` -> state=PURGED, data anonymized (BR-1)
  - ip_reclaim: terminated SIM IPs released back to pool (BR-3)
  - Max concurrent jobs: 5 per tenant (configurable)
  - Job timeout: 30 minutes no progress -> auto-fail
- **Redis locks**: Use Lua scripts for atomic operations to prevent race conditions
- **Graceful shutdown**: All new components (scheduler, timeout detector) must respect shutdown signals

## Risks & Mitigations

- **Risk**: Cron scheduler in multi-instance deployment fires duplicate jobs
  - **Mitigation**: Redis SETNX deduplication key with TTL per schedule tick
- **Risk**: Lock TTL too short causes premature release during long operations
  - **Mitigation**: Configurable TTL (default 60s) with automatic renewal every 30s
- **Risk**: Stub processors mask real failures when future stories implement them
  - **Mitigation**: Stubs log clearly and complete with `{"status": "stub"}` result
