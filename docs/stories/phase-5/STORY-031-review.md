# Review: STORY-031 — Background Job System

**Reviewer:** Amil Reviewer Agent
**Date:** 2026-03-21
**Phase:** 5 (eSIM & Advanced Ops)
**Story Status:** DONE (gate PASS, 696 tests, 40 job-specific, 0 failures)

---

## 1. Next Story Impact

STORY-031 is a critical infrastructure story. Three upcoming stories directly depend on it:

| Story | Dependency | Impact |
|---|---|---|
| STORY-029 (OTA SIM Management) | `ota_command` job type + bulk OTA via job runner | **Ready.** `JobTypeOTACommand` constant defined in types.go, stub processor registered. STORY-029 must implement `internal/job/ota` processor and register it via `runner.Register()`. The distributed lock (`argus:lock:sim:{id}`) ensures no concurrent OTA commands on the same SIM. |
| STORY-030 (Bulk Operations) | `bulk_state_change`, `bulk_policy_assign`, `bulk_esim_switch` job types + per-SIM distributed lock + retry API | **Ready.** All 3 job type constants defined, stub processors registered. STORY-030 must implement real processors. `CreateRetryJob` store method enables retry of failed items. Distributed lock prevents concurrent bulk operations on overlapping SIMs. Per-tenant concurrency control (default 5) limits resource consumption. |
| STORY-028 (eSIM Profiles) | No direct dependency on job runner | **No impact.** eSIM profile enable/disable/switch are synchronous operations. However, STORY-030's bulk operator switch (which depends on STORY-028) will use the job runner. |
| STORY-039 (Compliance Purge) | `purge_sweep` cron job | **Ready.** `JobTypePurgeSweep` constant defined, stub processor registered, `@daily` cron schedule configured. STORY-039 must implement the actual purge logic in the processor. |

**Post-notes for STORY-029:**
- Use `runner.Register(job.JobTypeOTACommand, otaProcessor)` pattern
- Leverage `DistributedLock.Acquire("argus:lock:sim:{simID}")` for per-SIM OTA safety
- Bulk OTA should use the existing job progress publishing pattern (NATS `argus.jobs.progress`)

**Post-notes for STORY-030:**
- All 3 bulk job types have stubs ready -- replace with real processors
- `CreateRetryJob` in store/job.go creates a new job with `retry_count + 1` -- use for "retry failed items" feature
- `CancelJob` on runner supports context-based cancel -- use for abort during long-running bulk ops
- Per-tenant concurrency (default 5) may need tuning for tenants running multiple bulk ops simultaneously

---

## 2. Architecture Evolution

### 2a. ARCHITECTURE.md -- No Structural Changes Needed

The project structure tree already shows `internal/job/` as SVC-09 (line 155). No new sub-packages were created -- all new files (lock.go, types.go, stubs.go, scheduler.go, timeout.go) are flat in the `internal/job/` package. Consistent with existing convention.

### 2b. ARCHITECTURE.md -- Caching Strategy

No new Redis caching keys need to be documented in the caching table. The job system uses Redis for:
- Distributed lock keys: `argus:lock:job:{id}`, `argus:lock:sim:{id}` (operational, not cache)
- Cron dedup keys: `argus:cron:{name}:{tick}` with TTL (operational, not cache)
- Per-tenant concurrency tracking: in-memory `sync.Map` (no Redis)

These are operational Redis usage patterns, not data caching. No changes to the caching strategy table needed.

### 2c. CONFIG.md -- GAP FOUND

**9 new environment variables** added to `internal/config/config.go` but NOT documented in `docs/architecture/CONFIG.md`:

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `JOB_MAX_CONCURRENT_PER_TENANT` | int | `5` | Maximum concurrent running jobs per tenant |
| `JOB_TIMEOUT_MINUTES` | int | `30` | Minutes before a stale running job is auto-failed |
| `JOB_TIMEOUT_CHECK_INTERVAL` | duration | `5m` | How often the timeout detector sweeps for stale jobs |
| `JOB_LOCK_TTL` | duration | `60s` | Redis distributed lock TTL for job/SIM locks |
| `JOB_LOCK_RENEW_INTERVAL` | duration | `30s` | How often the lock renewal goroutine extends the TTL |
| `CRON_PURGE_SWEEP` | string | `@daily` | Cron schedule for purge sweep job |
| `CRON_IP_RECLAIM` | string | `@hourly` | Cron schedule for IP reclaim job |
| `CRON_SLA_REPORT` | string | `@daily` | Cron schedule for SLA report job |
| `CRON_ENABLED` | bool | `true` | Enable/disable cron scheduler |

**Action Required:** Add these to the "Background Jobs" section in CONFIG.md.

### 2d. SVC-09 Service Description

The services index already lists: "Bulk ops execution, scheduled tasks (cron), OTA commands, IP reclaim, purge, distributed lock". This matches the STORY-031 deliverables. No update needed.

### 2e. WEBSOCKET_EVENTS.md

`job.progress` and `job.completed` events already documented (events 6 and 7). STORY-031's WebSocket wiring (`hub.SubscribeToNATS(SubjectJobProgress, SubjectJobCompleted)`) aligns with the documented schemas. No update needed.

---

## 3. GLOSSARY.md Updates

### Terms to Add

| Term | Definition | Context |
|------|-----------|---------|
| Distributed Lock (Job) | Redis SETNX-based lock with configurable TTL and Lua-script atomic release/renew. Key patterns: `argus:lock:job:{id}` (job-level), `argus:lock:sim:{id}` (SIM-level). Prevents concurrent processing of the same job or SIM across multiple worker instances. Lock TTL auto-extended via renewal goroutine during execution. | SVC-09, STORY-031, F-068 |
| Cron Scheduler | In-process scheduler supporting `@hourly`, `@daily`, `@weekly`, `@monthly` shorthands and 5-field cron expressions (min hour dom mon dow). Uses Redis SETNX deduplication to ensure only one instance fires per tick in multi-instance deployments. Default jobs: `purge_sweep` (@daily), `ip_reclaim` (@hourly), `sla_report` (@daily). | SVC-09, STORY-031, F-068 |
| Job Timeout Detector | Background sweeper that periodically scans for running jobs exceeding the configured timeout threshold (default 30min). Stale jobs are marked `failed` with reason "timeout" and a NATS completion event is published. | SVC-09, STORY-031 |
| Per-Tenant Concurrency Control | Mechanism limiting the maximum number of simultaneously running background jobs per tenant (configurable, default 5). Enforced in-memory via `sync.Map` slot tracking. Prevents a single tenant from monopolizing job runner capacity. | SVC-09, STORY-031, F-068 |

### Existing Terms -- Updates Needed

| Term | Current | Update |
|------|---------|--------|
| Job Runner | "NATS JetStream-backed background job processor... Supports cancellation, retry, and graceful shutdown." | Add: "Distributed Redis locking for job-level and SIM-level mutual exclusion. Per-tenant concurrency control (default 5). Lock TTL auto-renewed during execution. 11 job types with registered processors." Update context to include STORY-031. |

---

## 4. FUTURE.md Relevance

No FUTURE.md features are directly impacted by STORY-031. The job system is internal infrastructure.

However, **FTR-001 (AI Anomaly Engine)** could potentially use the cron scheduler for periodic ML model training or inference sweeps. The scheduler's Redis dedup pattern would ensure single-instance execution. No FUTURE.md changes needed -- this is an implementation detail.

**No FUTURE.md changes needed.**

---

## 5. Decisions (decisions.md)

### New Decisions to Record

| # | Date | Decision | Status |
|---|------|----------|--------|
| DEV-079 | 2026-03-21 | STORY-031: Distributed lock uses Redis SETNX with Lua scripts for atomic release (check-and-DEL) and renewal (check-and-PEXPIRE). Lua scripts ensure the lock holder identity is verified before modification, preventing accidental release of another worker's lock. Same pattern as Redlock (single-node variant). | ACCEPTED |
| DEV-080 | 2026-03-21 | STORY-031: Cron scheduler deduplication uses Redis SETNX with TTL per cron tick. Key format: `argus:cron:{name}:{tick}`. Only the instance that wins SETNX executes the job. TTL ensures key is cleaned up even if the winning instance crashes. No Lua needed -- SETNX is already atomic. | ACCEPTED |
| DEV-081 | 2026-03-21 | STORY-031: Per-tenant concurrency control uses in-memory `sync.Map` (not Redis). Acceptable for single-instance deployment. Multi-instance deployment would require Redis-based slot tracking. Given `DEPLOYMENT_MODE=single` default, this is correct for v1. Can be migrated to Redis counters if multi-instance job processing is needed. | ACCEPTED |
| DEV-082 | 2026-03-21 | STORY-031: Job type constants centralized in `types.go` with `AllJobTypes` slice. Gate fixed 3 hardcoded string literals in dryrun.go and rollout.go to use constants. All future job types MUST be added to `types.go` -- no hardcoded strings. | ACCEPTED |
| DEV-083 | 2026-03-21 | STORY-031: 7 stub processors registered for job types not yet implemented (bulk_state_change, bulk_policy_assign, bulk_esim_switch, ota_command, purge_sweep, ip_reclaim, sla_report). Stubs return `errors.New("not yet implemented")` which marks the job as failed. This is intentional -- prevents silent success on unimplemented operations. STORY-029/030/039 will replace stubs with real processors. | ACCEPTED |
| DEV-084 | 2026-03-21 | STORY-031: Cancel API returns `{id, state:"cancelled"}` (not full job DTO). Retry API creates a NEW job (not re-runs the old one) and returns 201 with `{new_job_id, retry_count, state:"queued"}`. These semantics align with the spec and ensure immutable job history. | ACCEPTED |

---

## 6. Cross-Document Consistency

| Document | Check | Status | Detail |
|----------|-------|--------|--------|
| SCOPE.md | "Bulk operations (async queue, partial success, retry failed, error report CSV, undo/rollback)" | OK | Job runner infrastructure ready. Bulk operations themselves are STORY-030 scope. |
| SCOPE.md | "Configurable KVKK/GDPR purge retention period with auto-purge" | OK | `purge_sweep` cron job scheduled @daily with stub processor. STORY-039 implements the actual purge logic. |
| PRODUCT.md | F-018 "Bulk operations -- async job queue, progress bar, partial success, retry failed, error report CSV" | OK | Job runner supports all listed capabilities. Retry via `CreateRetryJob`, progress via NATS, error report CSV via handler. |
| PRODUCT.md | F-068 "Background job system -- NATS queue, job dashboard, distributed lock, scheduled jobs (cron)" | OK | All F-068 components delivered: NATS queue (runner.go), distributed lock (lock.go), scheduled jobs (scheduler.go). Job dashboard is SCR-080 (frontend, Phase 8). |
| PRODUCT.md | BR-1 TERMINATED->PURGED "System (scheduled job)" | OK | `purge_sweep` cron job exists as scheduled trigger. |
| PRODUCT.md | BR-3 "IP held for configurable grace period, then reclaimed" | OK | `ip_reclaim` cron job scheduled @hourly. |
| ARCHITECTURE.md | SVC-09 description | OK | Already includes "distributed lock, scheduled tasks (cron)" |
| ARCHITECTURE.md | Project structure `internal/job/` | OK | Listed at line 155 |
| ARCHITECTURE.md | "NATS JetStream (events, job queue)" in Tech Stack | OK | Job runner uses JetStream QueueSubscribe |
| CONFIG.md | Job/Cron env vars | **GAP** | 9 new env vars not documented (see section 2c) |
| ROUTEMAP.md | STORY-031 row | OK | Marked `[x] DONE`, date 2026-03-21 |
| ROUTEMAP.md | Phase 5 header | OK | Shows `[PENDING]` which is correct -- 3 remaining stories |
| ROUTEMAP.md | Progress counter | OK | "27/55 (49%)" -- should be 28/55 (51%) after STORY-031 |
| ROUTEMAP.md | Change log | OK | STORY-031 entry present |
| ROUTEMAP.md | Overall progress header | OK | Says "51%" at top, matches 28/55 |
| WEBSOCKET_EVENTS.md | job.progress, job.completed | OK | Events 6 and 7 documented with correct schemas |
| GLOSSARY.md | Job Runner term | NEEDS UPDATE | Current definition doesn't mention distributed locking, cron, timeout detection, or per-tenant concurrency |
| GLOSSARY.md | New terms (Distributed Lock, Cron Scheduler, etc.) | **GAP** | 4 new terms needed (see section 3) |
| decisions.md | STORY-031 entries | **GAP** | No DEV-079..084 entries yet (see section 5) |
| STORY-029 | Dependency on STORY-031 | OK | Lists "STORY-031 (job runner for bulk OTA)" as blocker |
| STORY-030 | Dependency on STORY-031 | OK | Lists "STORY-031 (job runner)" as blocker |

### Prior Review Gaps Still Open

From STORY-027 review:
| # | Action | Status |
|---|--------|--------|
| STORY-027 #2 | Add `rattype/` to ARCHITECTURE.md project structure | **STILL OPEN** |

---

## 7. ROUTEMAP.md Consistency

The ROUTEMAP.md is consistent:
- Overall progress header: 51% (correct for 28/55)
- Stories completed counter: 27/55 in the body text -- **minor gap**, should be 28/55
- STORY-031 marked DONE with correct date
- Phase 5 header correctly shows `[PENDING]` (3 remaining stories)
- Change log has STORY-031 entry

---

## 8. Action Items Summary

| # | Priority | Action | Target File |
|---|----------|--------|-------------|
| 1 | HIGH | Add 9 job/cron env vars to Background Jobs section | `docs/architecture/CONFIG.md` |
| 2 | MEDIUM | Add 4 new glossary terms (Distributed Lock, Cron Scheduler, Job Timeout Detector, Per-Tenant Concurrency Control) | `docs/GLOSSARY.md` |
| 3 | MEDIUM | Update "Job Runner" glossary entry with distributed locking, cron, timeout, concurrency details | `docs/GLOSSARY.md` |
| 4 | LOW | Add DEV-079..084 decisions | `docs/brainstorming/decisions.md` |
| 5 | LOW | Fix story counter in ROUTEMAP.md body from "27/55 (49%)" to "28/55 (51%)" | `docs/ROUTEMAP.md` |
| 6 | LOW | Add `rattype/` to ARCHITECTURE.md project structure (carryover from STORY-027 review) | `docs/ARCHITECTURE.md` |

---

## 9. Verdict

**STORY-031 is well-implemented and delivers a complete background job infrastructure.** The distributed lock with Lua scripts is production-grade. The cron scheduler with Redis dedup is clean for multi-instance safety. The timeout detector prevents zombie jobs. Per-tenant concurrency control is pragmatic (in-memory for v1, upgradeable to Redis for multi-instance).

**Key strengths:**
- 11 job type constants with stub processors ensure type safety and forward compatibility
- Enhanced cancel/retry APIs follow REST semantics (cancel returns minimal response, retry creates new resource with 201)
- Graceful shutdown sequence (scheduler -> timeout detector -> runner) prevents orphaned jobs
- Gate fix: hardcoded string replacement with constants improves consistency across dryrun.go and rollout.go

**1 documentation gap identified** (CONFIG.md missing 9 env vars) -- this is the highest priority action item since operators need to know about configurable job/cron parameters. Other gaps are glossary and decisions log updates.

**STORY-029 and STORY-030 are fully unblocked** by this story's deliverables.
