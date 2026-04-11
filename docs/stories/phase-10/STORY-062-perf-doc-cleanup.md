# STORY-062: Performance Follow-ups & Documentation Drift Cleanup

## User Story
As an ops engineer and architect, I want the remaining perf follow-ups applied and every documentation drift closed in a single sweep, so that production handles growth and every doc matches the code.

## Description
Final cleanup pass. Track F (perf follow-ups that perf-optimizer flagged but didn't apply) and Track G (all documentation drift noted across 55 story reviews). Low-risk, mostly small changes, but must land before Documentation Phase starts so spec work can trust the docs.

## Architecture Reference
- Services: SVC-01 (API Gateway — dashboard endpoint), SVC-07 (Analytics — CDR export), SVC-10 (Audit)
- Packages: internal/api/dashboard, internal/store/msisdn, internal/store/cdr, internal/store/session, internal/store/audit, all docs/*
- Source: docs/reports/perf-optimizer-report.md, all docs/stories/phase-*/STORY-*-review.md drift notes

## Screen Reference
- SCR-001 (Dashboard — caching invisible to user but latency improves)
- SCR-080 (Audit Log — bounded pagination)
- Documentation pages not directly tied to screens.

## Acceptance Criteria
- [ ] AC-1: Dashboard endpoint cached in Redis with 30s TTL. Cache key `dashboard:{tenant_id}`. Invalidation: NATS subscribers for `sim.*`, `session.*`, `operator.health_changed`, `cdr.recorded` delete cache key. Cold load performance benchmark documented.
- [ ] AC-2: MSISDN bulk import uses batch INSERT (`INSERT ... VALUES (...), (...), ... ON CONFLICT DO NOTHING`) in chunks of 500. Per-row loop removed. Partial failure reported via error collection (idempotent retries supported).
- [ ] AC-3: CDR export uses server-side cursor. Handler streams rows via `rows.Next()` → `csv.Write` in chunks, does not buffer full result set. Works for 1M+ CDR exports without OOM.
- [ ] AC-4: Active sessions count served from Redis counter (`sessions:active:count:{tenant_id}`). Counter maintained via NATS `session.started`/`session.ended` subscribers. Full-table count only used as fallback reconciler (hourly).
- [ ] AC-5: Audit `GetByDateRange` replaced/augmented with cursor pagination. Unbounded range rejected with 400; `from`/`to` required and capped at 90 days. CSV export uses same streaming approach as CDR export.
- [ ] AC-6: **Doc drift batch — `ERROR_CODES.md`:**
  - Fix file reference (was `internal/gateway/errors.go`, should be `internal/apierr/apierr.go`)
  - Add missing codes: `MSISDN_NOT_FOUND`, `MSISDN_NOT_AVAILABLE`, `RESOURCE_LIMIT_EXCEEDED`, `TENANT_SUSPENDED`, 5 eSIM codes (`PROFILE_ALREADY_ENABLED`, `NOT_ESIM`, `INVALID_PROFILE_STATE`, `SAME_PROFILE`, `DIFFERENT_SIM`)
- [ ] AC-7: **Doc drift batch — `CONFIG.md`:**
  - NATS subjects section add `alert.triggered`, `job.completed`, `job.progress`, `audit.created` (4 missing)
  - Background Jobs section add `JOB_MAX_CONCURRENT_PER_TENANT`, `JOB_TIMEOUT_MINUTES`, `JOB_TIMEOUT_CHECK_INTERVAL`, `JOB_LOCK_TTL`, `JOB_LOCK_RENEW_INTERVAL`, `CRON_PURGE_SWEEP`, `CRON_IP_RECLAIM`, `CRON_SLA_REPORT`, `CRON_ENABLED` (9 vars)
  - Add `ENCRYPTION_KEY`, `ota:ratelimit:`, `operator:health:`, `sessions:active:count:` Redis namespaces
  - Fix `DEPLOYMENT_MODE` mismatch (`single | cluster` is authoritative, `.env.example` updated)
  - Rate limit vars aligned: `RATE_LIMIT_ALGORITHM`, `RATE_LIMIT_AUTH_PER_MINUTE`, `RATE_LIMIT_ENABLED`
- [ ] AC-8: **Doc drift batch — `ARCHITECTURE.md`, `DSL_GRAMMAR.md`, `ALGORITHMS.md`:**
  - Caching Strategy table rows: SoR cache, auth rate counters, auth latency window, dashboard cache, active sessions counter
  - Project structure tree: `internal/aaa/rattype/`, `internal/ota/`, `internal/api/ota/`, `internal/api/cdr/`, `internal/analytics/cdr/`
  - API count refreshed to actual total
  - Docker services table includes `:8443` (5G SBA)
  - DSL_GRAMMAR.md package path → `internal/policy/dsl/` (was `pkg/dsl/`)
  - ALGORITHMS.md Section 5 path → `internal/analytics/cdr/`
- [ ] AC-9: **Doc drift batch — `GLOSSARY.md`:** Add ~20 missing terms accumulated across reviews (eSIM Profile State Machine, Profile Switch, SM-DP+ Adapter, SMS-PP, BIP, KIC, KID, GSM 03.48, TAR, APDU, SoR Decision, SoR Priority, Operator Lock, IMSI Prefix Routing, Cost-Based Selection, Metrics Collector, MetricsRecorder Interface, Metrics Pusher, System Health Status, Connectivity Diagnostics, Diagnostic Step, Usage Analytics, Period Resolution, Real-Time Aggregation, Cost Analytics, Optimization Suggestion, Cost Per MB, Rating Engine, Cost Aggregation, CDR Consumer, CDR Export, Bulk Operation, Undo Record, Partial Success, WS Server, WS Hub, WS Close Code, Pseudonymization Salt).
- [ ] AC-10: **Doc drift batch — `db/_index.md`, `api/_index.md`:**
  - db: Add TBL-25, TBL-26 (ota_commands), TBL-27 (anomalies) listings
  - api: Add API-061b, API-061c, API-062b supplementary endpoints; refresh API counts
  - USERTEST.md endpoint paths corrected (OTA paths, STORY-030 `/errors` vs `/error-report`)
- [ ] AC-11: **Doc drift batch — `ROUTEMAP.md`:** Phase 4 header `[PENDING]` → `[DONE]` (stale). Phase 10 section added (this story). All per-story counters reconciled.

## Dependencies
- Blocked by: STORY-056..061 (code must stabilize before docs describe final state)
- Blocks: Documentation Phase

## Test Scenarios
- [ ] Benchmark: Dashboard endpoint p95 latency with 30s cache hit vs cold load, documented in perf report.
- [ ] Integration: MSISDN bulk import of 10K rows — single batch INSERT trace in DB log.
- [ ] Integration: CDR export of 1M rows — heap stable, no OOM.
- [ ] Integration: Active sessions counter reconciled hourly against table count (drift < 1%).
- [ ] Integration: Audit `GET /audit?from=2020-01-01` → 400 `INVALID_DATE_RANGE`.
- [ ] Lint: Docs CI (markdownlint + custom link checker) passes 0 broken references.
- [ ] Verification: `grep -r "internal/gateway/errors.go" docs/` returns zero results.
- [ ] Verification: Every ERROR_CODES.md code exists in `internal/apierr/apierr.go` and vice versa.
- [ ] Verification: Every CONFIG.md env var exists in `internal/config` struct tags.

## Effort Estimate
- Size: M
- Complexity: Low-Medium (mostly doc edits + 5 perf fixes)
