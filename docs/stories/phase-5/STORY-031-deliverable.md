# Deliverable: STORY-031 — Background Job System

## Summary

Extended the existing job runner with distributed locking, cron scheduler, timeout detection, per-tenant concurrency control, and enhanced cancel/retry APIs. NATS JetStream-backed queue with 11 job types, Redis-based distributed locks with Lua atomic operations, and cron-like scheduler with Redis deduplication for multi-instance safety.

## What Was Built

### Distributed Lock (internal/job/lock.go)
- Redis SETNX with configurable TTL
- Lua scripts for atomic release and renewal
- SIM-level key format: `argus:lock:sim:{id}`
- Lease renewal goroutine during job execution

### Cron Scheduler (internal/job/scheduler.go)
- Shorthand: `@hourly`, `@daily`, `@weekly`, `@monthly`
- 5-field cron expressions (min hour dom mon dow)
- Redis SETNX deduplication for multi-instance safety
- Default schedules: purge_sweep (daily), ip_reclaim (hourly), sla_report (daily)

### Timeout Detector (internal/job/timeout.go)
- Periodic sweep for stale running jobs (default 30min no progress)
- Marks failed + publishes NATS event
- Configurable check interval and timeout threshold

### Enhanced Runner (internal/job/runner.go)
- Per-tenant concurrency control (default 5, configurable)
- Context-based cancel with `CancelJob()` method
- Lock renewal goroutine via `TouchLock`
- Graceful shutdown: finish current batch, stop accepting new

### Job Types (internal/job/types.go)
- 11 constants: bulk_sim_import, bulk_session_disconnect, bulk_state_change, bulk_policy_assign, bulk_esim_switch, ota_command, purge_sweep, ip_reclaim, sla_report, policy_dry_run, policy_rollout_stage
- Stub processors for not-yet-implemented types

### Store Extensions (internal/store/job.go)
- FindTimedOutJobs, CreateRetryJob, CountActiveByTenant, TouchLock

### API Enhancements (internal/api/job/handler.go)
- Cancel: returns `{id, state:"cancelled"}`
- Retry: creates NEW job, returns 201 `{new_job_id, retry_count, state:"queued"}`
- Get: includes duration + locked_by fields

### Config (internal/config/config.go)
- 9 new fields: max concurrent, timeout, lock TTL, cron schedules, cron enabled

### Tests
- 40 job-specific tests across 5 test files
- Full suite: 696 tests passing, zero regressions

## Gate Fixes
- Replaced 3 hardcoded job type strings in dryrun.go and rollout.go with constants from types.go

## Files Changed
```
internal/job/lock.go              (new)
internal/job/types.go             (new)
internal/job/stubs.go             (new)
internal/job/scheduler.go         (new)
internal/job/timeout.go           (new)
internal/job/lock_test.go         (new)
internal/job/scheduler_test.go    (new)
internal/job/timeout_test.go      (new)
internal/job/types_test.go        (new)
internal/job/runner.go            (modified)
internal/job/import.go            (modified)
internal/job/bulk_disconnect.go   (modified)
internal/store/job.go             (modified)
internal/api/job/handler.go       (modified)
internal/config/config.go         (modified)
cmd/argus/main.go                 (modified)
internal/job/runner_test.go       (modified)
```

## Dependencies Unblocked
- STORY-029 (OTA SIM Management) — can use job runner for OTA commands
- STORY-030 (Bulk Operations) — can use job runner for bulk state/policy changes
- STORY-039 (Compliance Purge) — purge_sweep job type ready
