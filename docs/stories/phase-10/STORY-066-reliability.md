# STORY-066: Reliability, Backup, DR & Runtime Hardening

## User Story
As an ops engineer responsible for 99.95% uptime on a 10M SIM platform, I want automated backups with verified restore, point-in-time recovery, separate health probes, a hardened shutdown path, circuit breakers for every external dependency, guarded pprof, JWT key rotation, and a separate read pool for bulk queries, so that a single bad deploy, a Redis flap, a slow operator, or a runaway bulk job cannot take production down.

## Description
Production ops audit surfaced 15+ reliability gaps: no automated backups, no WAL archiving or PITR, no liveness/readiness split (transient Redis flap causes pod restart), 10s hardcoded shutdown timeout (may kill in-flight CoA), no circuit breaker middleware wired (operator slowness cascades), HSTS sent over plain HTTP (lockout risk), JWT signing key cannot rotate, pprof publicly accessible if flag slips, no request body size limit (slowloris), 50-conn DB pool shared between AAA hot path and bulk jobs (starvation). Close all of them.

## Architecture Reference
- Services: SVC-01 (Gateway), SVC-04 (AAA), SVC-06 (Operator), SVC-09 (Job Runner), SVC-10 (Audit)
- Packages: cmd/argus/main.go, internal/gateway, internal/auth/jwt, internal/operator/circuit_breaker, internal/config, infra/postgres, Makefile, deploy/docker-compose.yml
- Source: Phase 10 production ops audit (6-agent scan 2026-04-11), docs/reports/infra-tuning.md follow-ups

## Screen Reference
- SCR-120 (System Health — probe split visible)
- SCR-019 (Admin Settings — JWT rotation history, backup status)

## Acceptance Criteria
- [ ] AC-1: **Automated backups.**
  - `pg_dump` cron job (daily) via infrastructure cron OR Go-side scheduler
  - WAL archiving to S3 via `archive_command = 'aws s3 cp %p s3://argus-wal/%f'` (or MinIO equivalent) in `postgresql.conf`
  - Backup retention policy: daily for 14 days, weekly for 8 weeks, monthly for 12 months — expired backups deleted by cleanup cron
  - Backup verification cron (weekly): restore to ephemeral database, run smoke SELECT, report PASS/FAIL to ops channel
  - Backup status exposed in System Health UI + Prometheus metric `argus_backup_last_success_seconds`
- [ ] AC-2: **Point-in-time recovery (PITR) runbook.** Documented step-by-step recovery procedure in `docs/runbook/dr-pitr.md`. Tested by actually recovering to a point 1 hour in the past, verified restored DB contains expected rows.
- [ ] AC-3: **Liveness / readiness / startup probe split.**
  - `GET /health/live` — minimal in-process check (goroutine count, no DB/Redis), always 200 unless app deadlocked
  - `GET /health/ready` — full dependency check (DB, Redis, NATS, operator adapters), 503 if any critical dep unavailable
  - `GET /health/startup` — allows up to 60s for dependencies to come up on first boot; transitions to readiness after first successful check
  - docker-compose `healthcheck` switched to `/health/ready`; start_period raised to 60s
- [ ] AC-4: **Disk space probe** in readiness. Checks `/var/lib/postgresql/data`, `/data`, `/app/logs` usage; returns degraded above 85%, unhealthy above 95%. Metric `argus_disk_usage_percent{mount}` emitted.
- [ ] AC-5: **Graceful shutdown configurable timeout.** `SHUTDOWN_TIMEOUT_SECONDS` env var, default 30s (up from 10s). Per-subsystem drain: HTTP 20s, WS 10s, RADIUS 5s, Diameter 5s, SBA 5s, JobRunner 30s (wait for in-flight jobs up to this), NATS flush 5s, DB close 5s. Ordered so AAA ingress drains before DB closes. Documented in code with comments referencing this AC.
- [ ] AC-6: **Circuit breaker middleware** wired for every operator adapter call. Breaker state exposed in metrics (STORY-065 AC-11). Thresholds configurable (`CIRCUIT_BREAKER_THRESHOLD`, `CIRCUIT_BREAKER_RECOVERY_SEC`). When open, calls fail fast with `OPERATOR_UNAVAILABLE` instead of timing out.
- [ ] AC-7: **JWT signing key rotation.**
  - Config supports `JWT_SECRET_CURRENT` and `JWT_SECRET_PREVIOUS` (dual-key)
  - Tokens signed with CURRENT, verified against both (grace period)
  - Rotation procedure: set PREVIOUS=current, CURRENT=new, deploy, wait until all tokens expire, drop PREVIOUS
  - Rotation history logged to audit table
- [ ] AC-8: **HSTS guarded.** `security_headers.go` only emits HSTS when `TLS_ENABLED=true` OR `X-Forwarded-Proto=https` detected. Default dev config (STORY-056 HTTP-only on 8084) never emits HSTS. Prevents the "1-year lockout after cert loss" footgun.
- [ ] AC-9: **pprof access controlled.**
  - In dev: unguarded (existing behavior)
  - In staging/prod: `PPROF_ENABLED=false` default; when `true`, requires `PPROF_TOKEN` query parameter matching env var
  - Alternatively: pprof behind BasicAuth middleware when prod
- [ ] AC-10: **Request body size limit middleware.** `http.MaxBytesReader` with 10MB default, configurable per route (1MB for auth, 50MB for bulk import, 10MB default). Prevents slowloris and memory exhaustion.
- [ ] AC-11: **Separate DB read pool.** `DATABASE_READ_URL` config var (optional, defaults to primary). When set, analytics queries (`internal/analytics/*`), bulk job queries (`internal/job/bulk/*`), and export handlers use the read pool. AAA hot path + mutations continue on primary pool. Starvation eliminated. Documented in ARCHITECTURE.md caching table.
- [ ] AC-12: **Audit / CDR consumer lag monitoring.** NATS subscribers expose `argus_nats_consumer_lag{consumer}` metric. Alert fires at >10k pending for 5m. Reconciler runs hourly to verify NATS subjects are not backing up.
- [ ] AC-13: **Anomaly batchDetector crash safety.** Wrap in restart loop with exponential backoff; log crash; prevent silent stop.

## Dependencies
- Blocked by: STORY-063 (health probes real), STORY-065 (metrics for alerting)
- Blocks: Phase 10 Gate, STORY-067 (runbook references these procedures)

## Test Scenarios
- [ ] Integration: Trigger pg_dump cron → backup file appears in S3 bucket.
- [ ] Integration: Kill Redis mid-request → `/health/ready` returns 503, `/health/live` returns 200.
- [ ] Integration: SIGTERM during bulk import → job runner waits up to SHUTDOWN_TIMEOUT for job to finish, then exits cleanly. In-flight DB transactions committed.
- [ ] Integration: Operator adapter hang → circuit breaker opens after threshold, subsequent calls fail fast with OPERATOR_UNAVAILABLE, metric shows state=open.
- [ ] Integration: Sign token with old key, rotate to new key with old as PREVIOUS → old token still validates during grace period, new tokens use new key.
- [ ] Integration: Run app with `TLS_ENABLED=false` → response headers contain no `Strict-Transport-Security`.
- [ ] Integration: Access `/debug/pprof/` in prod config without token → 401. With correct token → pprof UI served.
- [ ] Load: Bulk import 100k SIMs while AAA hot path active → AAA latency p99 does not degrade (separate read pool working).
- [ ] DR: Simulate database loss at T+30min → restore from latest backup + WAL replay to T+29min → verify row-level recovery.

## Effort Estimate
- Size: L
- Complexity: High (backup automation is infra-heavy, PITR test is time-consuming)
