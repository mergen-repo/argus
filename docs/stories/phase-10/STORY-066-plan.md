---
story: STORY-066
title: Reliability, Backup, DR & Runtime Hardening
phase: 10
effort: L
complexity: High
planner: amil-planner
plan_version: 1
zero_deferral: true
waves: 6
tasks: 13
acs: 13
---

# Implementation Plan: STORY-066 — Reliability, Backup, DR & Runtime Hardening

## Goal
Close the 13 production-reliability gaps (automated backups + WAL archiving + PITR runbook, liveness/readiness/startup probe split, disk probe, configurable ordered graceful shutdown, circuit breakers with configurable thresholds wired to every operator call, JWT dual-key rotation with audit trail, HSTS-behind-TLS-guard, pprof token guard, per-route request body size limits, separate analytics/bulk read pool, NATS consumer lag metric, anomaly batch detector crash safety) so that a Redis flap, a runaway bulk job, a slow operator, or a single bad deploy cannot take down the 10M-SIM fleet — zero deferrals.

## Phase 10 Zero-Deferral Charter
- Every AC (AC-1..AC-13) closes in THIS story. No TODOs. No feature flags that hide incomplete work.
- Backup automation ships as a real Go cron processor that writes a timestamped `pg_dump` tarball to S3 and tracks its success in a new `backup_runs` table; retention sweeper is the same processor's cleanup branch.
- Weekly verification processor restores the latest backup into an ephemeral database file (`pg_restore --create --dbname=postgres` against a named scratch DB created for the job), runs a smoke `SELECT COUNT(*) FROM tenants` + row integrity check, then drops the scratch DB and records PASS/FAIL.
- The existing `DATABASE_READ_REPLICA_URL` env var (already wired for analytics) is EXTENDED — not duplicated as `DATABASE_READ_URL`. AC-11 routes bulk job queries + CDR export queries through the same pool. Docs note the rename decision.
- `docs/runbook/dr-pitr.md` is a real, tested runbook. The PITR test task actually runs the procedure against a scratch postgres container and records evidence in `docs/e2e-evidence/STORY-066-pitr-test.md`.
- All new code ships with `-race` tests; no test is marked `t.Skip`.
- `pprof` is NOT removed from the binary — the blank `net/http/pprof` import stays; what changes is that `http.ListenAndServe(pprofAddr, nil)` (which serves via `DefaultServeMux`) is replaced with a dedicated `*http.ServeMux` mounted behind token middleware in prod.

## Architecture Context

### Components Involved
| Component | Layer | Files (create = +, modify = ~) |
|-----------|-------|--------------------------------|
| Config additions | `internal/config` | ~`config.go`, ~`config_test.go` |
| Health probe split + disk probe | `internal/gateway` | ~`health.go`, ~`health_test.go`, ~`router.go`, +`disk_probe.go`, +`disk_probe_test.go` |
| Security headers HSTS guard | `internal/gateway` | ~`security_headers.go`, ~`security_headers_test.go` |
| Request body size middleware | `internal/gateway` | +`body_limit.go`, +`body_limit_test.go`, ~`router.go` |
| pprof token guard | `cmd/argus` + `internal/gateway` | ~`main.go`, +`internal/gateway/pprof_guard.go`, +`pprof_guard_test.go` |
| Graceful shutdown orchestrator | `cmd/argus` | ~`main.go` |
| JWT dual-key rotation | `internal/auth` | ~`jwt.go`, ~`jwt_test.go`, ~`auth.go` (sign path), ~`internal/gateway/auth_middleware.go` |
| JWT rotation audit entry | `internal/auth` | +`key_rotation.go` (helper that writes audit entry on startup when previous key detected) |
| Circuit breaker configurable | `internal/operator` | ~`circuit_breaker.go`, ~`circuit_breaker_test.go`, ~`router.go` (operator package) |
| Operator adapter wiring | `internal/operator/adapter` | ~ each adapter call site (see wiring task) |
| Read pool extension (bulk jobs + CDR export) | `internal/job/bulk_*`, `internal/job/cdr_export.go`, `cmd/argus/main.go` | ~wire `pgReadReplica.Pool` into BulkHandler's read paths and CDR export |
| NATS consumer lag metric | `internal/bus`, `internal/observability/metrics` | ~`nats.go`, ~`metrics.go`, +`consumer_lag.go` (poller) |
| Anomaly batch crash safety | `internal/job` | ~`anomaly_batch.go`, +`anomaly_batch_supervisor.go` |
| Backup automation (pg_dump) | `internal/job` | +`backup.go`, +`backup_test.go`, +`backup_cleanup.go`, +`backup_verify.go`, ~`scheduler.go` (new entries) |
| Backup status store | `internal/store` | +`backup_store.go`, +migration files |
| PostgreSQL WAL archiving | `infra/postgres/postgresql.conf`, `deploy/docker-compose.yml` | ~ both |
| DR PITR runbook | `docs/runbook/` | +`dr-pitr.md`, +`dr-restore.md` |
| System health UI | `web/src/pages/system/health.tsx` | ~ show probe split + backup status + disk usage |
| Admin settings UI | `web/src/pages/settings/` | +`reliability.tsx` (JWT rotation history, backup status list) or extend an existing page |
| Integration tests | `cmd/argus/` and `internal/...` test files | new `*_test.go` per task |

### Data Flow — Shutdown path (AC-5)
```
SIGTERM received
    │
    ▼
appCtx still live; read SHUTDOWN_TIMEOUT_SECONDS (default 30s)
    │
    ▼ [ingress drains first — new connections rejected]
srv.Shutdown(ctx_http_20s)          // AAA/admin HTTP stops accepting
radiusServer.Stop(ctx_radius_5s)    // RADIUS listener stops, in-flight auth completes
diameterServer.Stop(ctx_diam_5s)    // Diameter peer disconnect
sbaServer.Stop(ctx_sba_5s)          // SBA deregister from NRF then stop
    │
    ▼ [control plane drains]
wsServer.Stop(ctx_ws_10s)           // WS reconnect broadcast + close
sessionSweeper.Stop()
cronScheduler.Stop()
timeoutDetector.Stop()
    │
    ▼ [data plane drains]
jobRunner.Stop(ctx_job_30s)         // waits up to timeout for in-flight job
metricsPusher.Stop()
notifSvc.Stop()
healthChecker.Stop()
anomalyEngine.Stop()
cdrConsumer.Stop()
auditSvc.Stop()                     // flush audit batches
    │
    ▼ [observability last-flush BEFORE infra close]
otelShutdown(ctx_otel_5s)           // unchanged from STORY-065
appCancel()                         // cancel long-lived background ctx
    │
    ▼ [infra close — after all callers stopped]
nats flush (5s) → nats close
redis close
pg close
```

### Data Flow — Health probes (AC-3, AC-4)
```
Kubernetes / docker healthcheck
    │
    ├── GET /health/live    ──▶  runtime.NumGoroutine() sanity + quick panic-check → always 200
    │                              (no DB, no Redis, no NATS — goal: "is the process dead?")
    │
    ├── GET /health/ready   ──▶  runProbe(db) + runProbe(redis) + runProbe(nats)
    │                              + operator adapter liveness (AAA/Diameter/SBA state)
    │                              + diskProbe(mounts)
    │                              — any 'critical' failure → 503, degraded → 200 w/ degraded status
    │
    └── GET /health/startup ──▶  readiness semantics BUT within first 60s of process uptime
                                  allows dependencies to warm up; after first successful ready check
                                  startup returns 200 permanently (flag flip under sync.Once).
```

### Data Flow — JWT dual-key verification (AC-7)
```
Request with Authorization: Bearer <token>
    │
    ▼
gateway.JWTAuth middleware
    │
    ▼
auth.ValidateTokenMulti([JWT_SECRET_CURRENT, JWT_SECRET_PREVIOUS])
    │  try current first → if ErrTokenInvalid AND previous != "" → try previous
    │  on either success → record metric argus_jwt_verify_total{key="current"|"previous"}
    ▼
Sign path: GenerateToken ALWAYS uses JWT_SECRET_CURRENT
    │
Rotation lifecycle:
    1. deploy with PREVIOUS=old_value, CURRENT=new_value → both accepted, new issued with new
    2. wait until (JWT_EXPIRY + JWT_REFRESH_EXPIRY) = 168h15m so no outstanding old tokens
    3. deploy with PREVIOUS="" (empty) → only new accepted
    4. startup detects "PREVIOUS was set this boot" and writes an audit entry:
       action="jwt_key_rotation_detected", entity_type="security", entity_id="jwt_signing_key"
```

### Data Flow — Backup (AC-1)
```
Cron entry "@daily 02:00" registers JobTypeBackupFull
    │
    ▼
BackupProcessor.Process:
    1. exec.CommandContext("pg_dump", "--format=custom", "--file=/tmp/pg_backup_<ts>.dump", ...)
       uses PGPASSWORD from DATABASE_URL parse; timeout = BACKUP_TIMEOUT_SECONDS (default 1800)
    2. gzip file (optional) → SHA-256 hash computed while streaming
    3. s3Uploader.Upload(bucket="argus-backup", key="daily/<ts>.dump", data=fileBytes)
       Uses existing internal/storage/s3_uploader.go pattern
    4. store.BackupStore.Record(BackupRun{type="daily", s3_key, size_bytes, sha256, started_at, finished_at, status})
    5. Prometheus metric: argus_backup_last_success_timestamp_seconds{type="daily"} = Unix time now
    6. Publish bus.SubjectBackupCompleted (new constant)

Cron entry "@weekly Sun 03:00" registers JobTypeBackupVerify:
    1. BackupVerifyProcessor picks the most recent successful backup_run
    2. downloads from S3 to temp file
    3. creates scratch database "argus_verify_<ts>" via pg_isready connection to postgres super
    4. runs pg_restore --dbname=<scratch> --exit-on-error
    5. executes smoke queries: SELECT COUNT(*) FROM tenants; SELECT COUNT(*) FROM sims LIMIT 1;
    6. drops scratch database
    7. records backup_verifications row
    8. publishes bus.SubjectBackupVerified with result

Cron entry "@daily 04:00" registers JobTypeBackupCleanup:
    - Keeps: 14 daily, 8 weekly (Mondays), 12 monthly (1st of month)
    - Anything not matching ANY of those → delete S3 object + UPDATE backup_runs SET state='expired'
```

### Data Flow — Read pool routing (AC-11)
```
Current: cmd/argus/main.go:288-292 already routes usageAnalyticsStore + costAnalyticsStore
         to pgReadReplica.Pool if DATABASE_READ_REPLICA_URL is set.

AC-11 extends this to:
  - CDR export handler (internal/job/cdr_export.go) — rebuild cdrStore using analyticsPool
  - Bulk job read paths (internal/job/bulk_state_change.go, bulk_policy_assign.go, bulk_esim_switch.go)
    pass a separate `readPool` into bulk processors; SELECT * FROM sims WHERE ... uses readPool,
    UPDATE / INSERT still uses primary pool
  - Segment preview (internal/api/segment) count + paginate queries use readPool
```

### API Specifications

#### `GET /health/live` (AC-3) — new
- Response: 200 always (unless handler panics)
- Body: `{"status":"success","data":{"status":"alive","uptime":"12m30s","goroutines":128,"go_version":"go1.22"}}`
- No authentication. No rate limit. Runs OUTSIDE the chi middleware chain (registered on the base router before `r.Use(...)` stack).

#### `GET /health/ready` (AC-3, AC-4) — new
- Response: 200 healthy, 200 degraded (with `data.degraded_reasons: [...]`), 503 unhealthy
- Body:
```json
{"status":"success","data":{
  "state":"healthy",
  "db":{"status":"ok","latency_ms":3},
  "redis":{"status":"ok","latency_ms":1},
  "nats":{"status":"ok","latency_ms":2},
  "aaa":{"radius":{"status":"ok"},"diameter":{"status":"ok"},"sba":{"status":"ok"},"sessions_active":128},
  "disks":[{"mount":"/var/lib/postgresql/data","used_pct":47.2,"status":"ok"},
           {"mount":"/app/logs","used_pct":12.1,"status":"ok"}],
  "uptime":"12m30s"
}}
```
- State thresholds: `state=healthy` (all ok), `state=degraded` (disk ≥85% OR AAA partial), `state=unhealthy` (DB/Redis/NATS down OR disk ≥95%)
- No authentication.

#### `GET /health/startup` (AC-3) — new
- During first 60s of process: runs ready logic but allows up to 3 transient failures before returning 503.
- After first successful ready check OR after 60s uptime: permanently returns 200 with `{"state":"started"}`.
- No authentication.

#### `GET /api/v1/system/backup-status` (AC-1 UI) — new
- JWT + role `tenant_admin`
- Body: `{"status":"success","data":{"last_daily":{"status":"ok","finished_at":"...","size_mb":1230,"s3_key":"daily/20260412.dump","sha256":"..."},"last_weekly_verify":{"status":"ok","verified_rows":128234},"history":[...30 entries]}}`
- Drives the backup panel on SCR-019 (Admin Settings) + a read-only card on SCR-120 (System Health).

#### `GET /api/v1/system/jwt-rotation-history` (AC-7 UI) — new
- JWT + role `super_admin`
- Returns recent `jwt_key_rotation_detected` audit_logs rows (last 10)
- Drives the JWT rotation history panel on SCR-019.

### Database Schema

Source decision: backup_runs is a NEW table. Create migration 20260412000009_backup_runs.up.sql / .down.sql.

```sql
-- Source: migration 20260412000009_backup_runs.up.sql (NEW)
CREATE TABLE IF NOT EXISTS backup_runs (
    id BIGSERIAL PRIMARY KEY,
    kind VARCHAR(20) NOT NULL,              -- 'daily' | 'weekly' | 'monthly' | 'on_demand'
    state VARCHAR(20) NOT NULL,             -- 'running' | 'succeeded' | 'failed' | 'expired'
    s3_bucket VARCHAR(200) NOT NULL,
    s3_key VARCHAR(500) NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    sha256 VARCHAR(64),
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    duration_seconds INTEGER,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_backup_runs_kind_time ON backup_runs (kind, started_at DESC);
CREATE INDEX idx_backup_runs_state ON backup_runs (state);

CREATE TABLE IF NOT EXISTS backup_verifications (
    id BIGSERIAL PRIMARY KEY,
    backup_run_id BIGINT NOT NULL REFERENCES backup_runs(id) ON DELETE CASCADE,
    state VARCHAR(20) NOT NULL,             -- 'succeeded' | 'failed'
    tenants_count BIGINT,
    sims_count BIGINT,
    error_message TEXT,
    verified_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_backup_verifications_run ON backup_verifications (backup_run_id);
```

**Existing tables referenced (verified against migrations):**
- `audit_logs` — existing schema (migration 20260320000002_core_schema.up.sql:440), partitioned by created_at monthly. JWT rotation audit entry uses the normal `audit.Service.Create` path with `action="jwt_key_rotation_detected"`, `entity_type="security"`, `entity_id="jwt_signing_key"`. **Do not add new columns.**

### PostgreSQL Configuration Changes (AC-1, AC-2)

Source: `infra/postgres/postgresql.conf` (existing) — MUST modify these lines:
```conf
# Before (existing):
wal_level = replica
max_wal_senders = 0
# archive_mode not set (defaults to off)

# After (STORY-066):
wal_level = replica                    # kept
max_wal_senders = 3                    # was 0 — needed for pg_basebackup + streaming replica
archive_mode = on                      # new
archive_command = 'aws s3 cp %p s3://${ARGUS_WAL_BUCKET}/${ARGUS_WAL_PREFIX}%f --only-show-errors || exit 1'
archive_timeout = 300                  # force segment switch every 5 min so WAL reaches S3 even idle
wal_keep_size = 1GB                    # retain WAL so slow archive retries succeed
```

For MinIO deployments, `ARGUS_WAL_BUCKET` env var may point to a MinIO bucket; archive_command is parameterized via `docker-compose.yml` environment substitution. Fallback command for dev (no S3): `archive_command = 'test ! -f /var/lib/postgresql/wal_archive/%f && cp %p /var/lib/postgresql/wal_archive/%f'` and mount a volume.

### Environment Variables (AC-5, AC-6, AC-7, AC-8, AC-9, AC-10)

Add to `internal/config/config.go` Config struct:
```go
// Reliability (STORY-066)
ShutdownTimeoutSec         int    `envconfig:"SHUTDOWN_TIMEOUT_SECONDS"        default:"30"`
ShutdownHTTPSec            int    `envconfig:"SHUTDOWN_HTTP_SECONDS"           default:"20"`
ShutdownWSSec              int    `envconfig:"SHUTDOWN_WS_SECONDS"             default:"10"`
ShutdownRADIUSSec          int    `envconfig:"SHUTDOWN_RADIUS_SECONDS"         default:"5"`
ShutdownDiameterSec        int    `envconfig:"SHUTDOWN_DIAMETER_SECONDS"       default:"5"`
ShutdownSBASec             int    `envconfig:"SHUTDOWN_SBA_SECONDS"            default:"5"`
ShutdownJobSec             int    `envconfig:"SHUTDOWN_JOB_SECONDS"            default:"30"`
ShutdownNATSSec            int    `envconfig:"SHUTDOWN_NATS_SECONDS"           default:"5"`
ShutdownDBSec              int    `envconfig:"SHUTDOWN_DB_SECONDS"             default:"5"`

CircuitBreakerThreshold    int    `envconfig:"CIRCUIT_BREAKER_THRESHOLD"       default:"5"`
CircuitBreakerRecoverySec  int    `envconfig:"CIRCUIT_BREAKER_RECOVERY_SEC"    default:"30"`

JWTSecretPrevious          string `envconfig:"JWT_SECRET_PREVIOUS"` // optional, no default

TLSEnabled                 bool   `envconfig:"TLS_ENABLED"                     default:"false"`
TrustForwardedProto        bool   `envconfig:"TRUST_FORWARDED_PROTO"           default:"true"`

PprofToken                 string `envconfig:"PPROF_TOKEN"` // required if PprofEnabled=true in non-dev

RequestBodyMaxMB           int    `envconfig:"REQUEST_BODY_MAX_MB"             default:"10"`
RequestBodyAuthMB          int    `envconfig:"REQUEST_BODY_AUTH_MB"            default:"1"`
RequestBodyBulkMB          int    `envconfig:"REQUEST_BODY_BULK_MB"            default:"50"`

DiskProbeMount             string `envconfig:"DISK_PROBE_MOUNTS" default:"/var/lib/postgresql/data,/app/logs,/data"`
DiskDegradedPct            int    `envconfig:"DISK_DEGRADED_PCT"               default:"85"`
DiskUnhealthyPct           int    `envconfig:"DISK_UNHEALTHY_PCT"               default:"95"`

BackupEnabled              bool   `envconfig:"BACKUP_ENABLED"                  default:"false"`
BackupDailyCron            string `envconfig:"BACKUP_DAILY_CRON"               default:"0 2 * * *"`
BackupVerifyCron           string `envconfig:"BACKUP_VERIFY_CRON"              default:"0 3 * * 0"`
BackupCleanupCron          string `envconfig:"BACKUP_CLEANUP_CRON"             default:"0 4 * * *"`
BackupBucket               string `envconfig:"BACKUP_BUCKET"                   default:"argus-backup"`
BackupTimeoutSec           int    `envconfig:"BACKUP_TIMEOUT_SECONDS"          default:"1800"`
BackupRetentionDaily       int    `envconfig:"BACKUP_RETENTION_DAILY"          default:"14"`
BackupRetentionWeekly      int    `envconfig:"BACKUP_RETENTION_WEEKLY"         default:"8"`
BackupRetentionMonthly     int    `envconfig:"BACKUP_RETENTION_MONTHLY"        default:"12"`

NATSConsumerLagAlertThreshold int `envconfig:"NATS_CONSUMER_LAG_ALERT_THRESHOLD" default:"10000"`
NATSConsumerLagPollSec        int `envconfig:"NATS_CONSUMER_LAG_POLL_SECONDS"    default:"30"`
```

Validation (add to `Config.Validate()`):
- `ShutdownTimeoutSec ≥ 5`
- `ShutdownJobSec ≤ ShutdownTimeoutSec` (job drain cannot exceed overall budget)
- If `PprofEnabled && !IsDev()` → `PprofToken` must be ≥ 32 chars
- `JWTSecretPrevious == "" || len(JWTSecretPrevious) ≥ 32`
- `RequestBodyMaxMB`, `RequestBodyAuthMB`, `RequestBodyBulkMB` all > 0
- `CircuitBreakerThreshold ≥ 1`
- `DiskDegradedPct < DiskUnhealthyPct ≤ 100`

### Screen Mockups

#### SCR-120 — System Health (extend existing page `web/src/pages/system/health.tsx`)
Existing page renders a grid of service cards + sparklines. STORY-066 adds:
```
┌───────────────────────────────────────────────────────────────────┐
│  System Health                                 [Refresh] [60s ▼]  │
├───────────────────────────────────────────────────────────────────┤
│  ┌─ Liveness ──┐  ┌─ Readiness ──┐  ┌─ Startup ──┐  ┌─ Disk ────┐│
│  │ ● alive     │  │ ● healthy    │  │ ● started  │  │ pg:47% ok ││
│  │ goroutines  │  │ 3/3 deps ok  │  │ uptime     │  │ logs:12%  ││
│  │    128      │  │              │  │  12m30s    │  │ data:3%   ││
│  └─────────────┘  └──────────────┘  └────────────┘  └───────────┘│
├───────────────────────────────────────────────────────────────────┤
│  Existing DB / Redis / NATS / AAA service cards (unchanged)       │
├───────────────────────────────────────────────────────────────────┤
│  ┌─ Backup Status ───────────────────────────────────────────────┐│
│  │ Last daily:    ● ok   12h ago     1.2 GB   sha256: a7f3...   ││
│  │ Last weekly:   ● ok   6d ago      verified  128,234 rows      ││
│  │ Last monthly:  ● ok   22d ago     45.8 GB                     ││
│  │ Alerts: none                                    [View History]││
│  └──────────────────────────────────────────────────────────────┘│
└───────────────────────────────────────────────────────────────────┘
```
- Navigation: `/system/health` (existing route).
- Drill-down: "View History" → `/settings/reliability` (new tab showing full backup list + JWT rotation history).
- Empty state: "No backup history yet — backups start after BACKUP_ENABLED=true and first @daily cron fires."
- Loading state: existing `Skeleton` component used for each card.
- Error state: existing pattern in health.tsx (AlertCircle icon + retry button).

#### SCR-019 — Admin Settings → Reliability tab (new tab on existing settings area)
```
┌───────────────────────────────────────────────────────────────────┐
│  Settings → Reliability                                           │
├───────────────────────────────────────────────────────────────────┤
│  Backup Schedule & Retention                                      │
│  ┌─ Daily ─────────┐  ┌─ Weekly ────────┐  ┌─ Monthly ─────────┐ │
│  │ 02:00 UTC       │  │ Sunday 03:00    │  │ 1st 04:00          │ │
│  │ Keep 14 days    │  │ Keep 8 weeks    │  │ Keep 12 months     │ │
│  └─────────────────┘  └─────────────────┘  └────────────────────┘ │
│                                                                   │
│  History (last 30)                                                │
│  ┌──────────────┬────────┬──────┬─────────┬────────────────────┐ │
│  │ When         │ Kind   │ State│ Size    │ S3 key             │ │
│  │ 4h ago       │ daily  │ ok   │ 1.2 GB  │ daily/20260412...  │ │
│  │ 1d ago       │ daily  │ ok   │ 1.2 GB  │ daily/20260411...  │ │
│  │ 5d ago       │ weekly │ ok   │ 1.2 GB  │ weekly/20260407... │ │
│  └──────────────┴────────┴──────┴─────────┴────────────────────┘ │
│                                                                   │
│  JWT Signing Key Rotation                                         │
│  Current key fingerprint: sha256:c3f1...8e12  (issued 14 days ago)│
│  Previous key:            sha256:9d22...1f0a  (accepts until ...) │
│  Rotation history (audit-backed):                                 │
│   • 2026-03-29 14:22  rotation detected  actor: system            │
│   • 2026-02-15 10:05  rotation detected  actor: system            │
└───────────────────────────────────────────────────────────────────┘
```
- Navigation: `/settings/reliability` (new route — add to router).
- Drill-down: s3_key → copy-to-clipboard button (monospace). Audit row → links to `/audit?entity_id=jwt_signing_key`.
- Empty states for each panel — "No backups yet", "No rotations recorded".

### Design Token Map (FRONTEND.md, mandatory — UI story)

#### Color Tokens (from FRONTEND.md §Color Palette)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page/card bg | `bg-[var(--bg-surface)]` via existing `Card` component | `bg-white`, `bg-[#0C0C14]` |
| Elevated panels | `bg-[var(--bg-elevated)]` | `bg-[#12121C]` raw |
| Primary text | `text-[var(--text-primary)]` | `text-gray-900`, `text-[#E4E4ED]` |
| Secondary text | `text-[var(--text-secondary)]` | `text-gray-500`, `text-[#7A7A95]` |
| Muted text | `text-[var(--text-tertiary)]` | `text-gray-400` |
| Primary border | `border-[var(--border)]` | `border-gray-200`, `border-[#1E1E30]` |
| Subtle border | `border-[var(--border-subtle)]` | any raw hex |
| Success state (backup ok, probe alive) | `text-[var(--color-success)]` + glow `rgba(0,255,136,0.3)` (already used in health.tsx) | `text-green-500` |
| Warning state (disk ≥85%) | `text-[var(--color-warning)]` | `text-yellow-500` |
| Danger state (probe 503, disk ≥95%) | `text-[var(--color-danger)]` | `text-red-500` |
| Accent (CTA / refresh button) | `text-[var(--accent)]` | `text-blue-500` |

#### Typography Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page title | existing `.text-[15px] font-semibold` from settings pages | `text-2xl` arbitrary |
| Card title | `CardTitle` component (existing in `web/src/components/ui/card.tsx`) | raw `<h3>` |
| Metric value | `font-mono text-[28px] font-bold` (dashboard pattern) | `text-[30px]` |
| Table mono data (s3_key, sha256) | `font-mono text-[12px]` | `text-xs` |
| Labels / caption | `text-[12px] text-[var(--text-secondary)]` | `text-gray-500 text-xs` |
| Status badges | existing `Badge` component in `web/src/components/ui/badge.tsx` | raw `<span>` |

#### Spacing & Elevation Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Section padding | `p-6` (consistent with existing system/health.tsx) | `p-[20px]` |
| Card gap | `gap-4` (existing pattern) | `gap-[18px]` |
| Card radius | use `Card` component (rounds via `--radius-md` = 10px internally) | `rounded-md` raw |
| Card shadow/hover | existing `.card-hover` utility in global.css | custom box-shadow |
| Pulse dot (live indicator) | inline style + `animation: pulse 2s infinite` (as health.tsx line 53-59 already does) | re-implement |

#### Existing Components to REUSE (DO NOT recreate)
| Component | Path | Use For |
|-----------|------|---------|
| `<Card>, <CardHeader>, <CardTitle>, <CardContent>` | `web/src/components/ui/card.tsx` | EVERY panel — liveness card, readiness card, backup status, JWT rotation |
| `<Button>` | `web/src/components/ui/button.tsx` | Refresh, View History, Copy-to-clipboard — NEVER raw `<button>` |
| `<Badge>` | `web/src/components/ui/badge.tsx` | probe status labels, backup state labels |
| `<Skeleton>` | `web/src/components/ui/skeleton.tsx` | loading state for every card |
| `<Table>, <TableHeader>, <TableRow>, <TableCell>` | `web/src/components/ui/table.tsx` | backup history table, rotation history table |
| `<Tabs>, <TabsList>, <TabsTrigger>, <TabsContent>` | `web/src/components/ui/tabs.tsx` | Reliability tab grouping on settings |
| `<Tooltip>` | `web/src/components/ui/tooltip.tsx` | disk mount path explanation, sha256 fingerprint |
| icons from `lucide-react` | imported per-page | NEVER inline SVG |
| `useHealthCheck`, `useSystemMetrics` hooks | `web/src/hooks/use-settings.ts` (existing) | extend with `useBackupStatus`, `useJwtRotationHistory` (add — follow same pattern) |

**RULE: No hardcoded hex colors in new UI files. No raw `<input>`, `<button>`, `<table>`. All data tables go through the `<Table>` component.**

## Prerequisites
- [x] STORY-063 DONE — real health probe implementations landed; endpoints authenticate dep state.
- [x] STORY-065 DONE — Prometheus registry (`internal/observability/metrics`), NATS publish/consume counters, trace propagation end-to-end. AC-6 (circuit breaker state gauge) and AC-11 (operator health gauge) already ship from STORY-065; this story wires them to the actual breaker transitions via the existing `SetTransitionHook` helper.
- Go modules already present: `github.com/prometheus/client_golang`, `github.com/exaring/otelpgx`, `go.opentelemetry.io/otel/*`, `github.com/aws/aws-sdk-go-v2/service/s3` (via storage).
- New module needed: `github.com/shirou/gopsutil/v3` — cross-platform disk usage (already widely vendored), or implement via `syscall.Statfs` directly for linux+darwin only (target deploy = linux).

## Task Decomposition Rules

Each task dispatched to a fresh Developer with isolated context. Amil extracts `Context refs` sections from this plan. 13 tasks across 6 waves — high because 13 ACs are genuinely distinct; each task stays ≤3 files (a few at exactly 3).

## Tasks

### Task 1 (Wave 1) — Config additions + validation
- **Files:** Modify `internal/config/config.go`, Modify `internal/config/config_test.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/config/config.go` lines 1–234 and `config_test.go`. Follow exact envconfig tag style and the existing `Validate()` pattern (return typed fmt.Errorf with context, don't swallow).
- **Context refs:** "Environment Variables (AC-5..AC-10)", "Architecture Context > Components Involved"
- **What:** Add every env var in the "Environment Variables" block above. Wire validation rules listed under Validation bullet. All defaults must match the AC-specified values. Add `TotalShutdownBudget()` helper method on Config returning `max(ShutdownTimeoutSec, sum of subsystem budgets)` for main.go use. Rename note: `DATABASE_READ_REPLICA_URL` already exists — do not add `DATABASE_READ_URL`.
- **Verify:** `go test ./internal/config/... -race` passes; `go vet ./internal/config/...` clean; new envconfig tags parse with `envconfig.Process` under an empty environment (defaults kick in).

### Task 2 (Wave 1) — Health probe split + disk probe
- **Files:** Modify `internal/gateway/health.go`, Modify `internal/gateway/health_test.go`, Create `internal/gateway/disk_probe.go` (+test)
- **Depends on:** Task 1 (uses `DiskProbeMount`, `DiskDegradedPct`, `DiskUnhealthyPct`)
- **Complexity:** medium
- **Pattern ref:** Read existing `internal/gateway/health.go` and its test. Follow the same `probeResult` DTO shape. For disk: use `golang.org/x/sys/unix.Statfs_t` directly (already in stdlib path) — sample impl: `var fs unix.Statfs_t; unix.Statfs(path, &fs); used := (fs.Blocks - fs.Bfree) * uint64(fs.Bsize)`. Darwin/linux both supported by `x/sys/unix`.
- **Context refs:** "API Specifications > /health/live /ready /startup", "Data Flow — Health probes", "Environment Variables"
- **What:** Refactor `HealthHandler` to expose three HTTP methods: `Live(w,r)`, `Ready(w,r)`, `Startup(w,r)`. Preserve the legacy `Check(w,r)` as a thin wrapper delegating to `Ready` (backwards compat for `/api/health`, `/api/v1/health`). Add `diskProbe(mounts []string, degradedPct, unhealthyPct int)` that iterates mounts, returns `[]DiskProbeResult{Mount, UsedPct, Status}`. Startup uses `sync.Once` to latch after first successful Ready. Emit Prometheus gauge `argus_disk_usage_percent{mount}` via `metricsReg` (add `DiskUsagePercent *prometheus.GaugeVec` to `internal/observability/metrics/metrics.go`; follow existing `OperatorHealth` gauge pattern). Update `router.go`: new routes `/health/live`, `/health/ready`, `/health/startup` registered BEFORE the middleware chain (no JWT, no rate limit, no security headers — keep noise minimal).
- **Verify:** `go test ./internal/gateway/... -race` passes; unit tests cover: live 200 with zero deps, ready 503 when DB probe errors, ready 200 degraded when disk ≥ degraded threshold, startup flips to permanent 200 after first success.

### Task 3 (Wave 1) — Gateway hardening triple: HSTS guard + request body limit + security headers update
- **Files:** Modify `internal/gateway/security_headers.go`, Modify `internal/gateway/security_headers_test.go`, Create `internal/gateway/body_limit.go` (+test)
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/security_headers.go` for middleware shape. For body limit follow `http.MaxBytesReader` pattern — idiomatic Go: wrap `r.Body = http.MaxBytesReader(w, r.Body, limitBytes)` and let downstream decoding surface `*http.MaxBytesError` as 413 via a small recover helper.
- **Context refs:** "API Specifications", "Environment Variables", "Data Flow — Shutdown path" (not directly used but needed for router wiring)
- **What:** (a) HSTS: change `SecurityHeadersConfig` to include `HSTSOnlyWhenTLS bool` (default true). Emit `Strict-Transport-Security` only when `r.TLS != nil` OR `r.Header.Get("X-Forwarded-Proto") == "https"` AND `TrustForwardedProto` cfg true. Tests cover: no-HSTS over HTTP, HSTS over TLS, HSTS via X-Forwarded-Proto. (b) Body limit: `func BodyLimit(mb int) func(http.Handler) http.Handler` — wraps body with MaxBytesReader; install on router via `r.Use(BodyLimit(cfg.RequestBodyMaxMB))` globally; add per-route override by mounting `r.With(BodyLimit(cfg.RequestBodyAuthMB))` on `/api/v1/auth/*` and `BodyLimit(cfg.RequestBodyBulkMB)` on `/api/v1/sims/bulk/*` route groups. On exceed → 413 with standard envelope `{status:"error", error:{code:"REQUEST_BODY_TOO_LARGE", message:"..."}}`.
- **Verify:** unit tests: `go test ./internal/gateway/... -race`; manual curl with 20MB payload on bulk endpoint → 200, on non-bulk → 413; HSTS header present when TLS, absent otherwise.

### Task 4 (Wave 2) — pprof token guard
- **Files:** Create `internal/gateway/pprof_guard.go` (+test), Modify `cmd/argus/main.go` (pprof startup block lines ~104–111)
- **Depends on:** Task 1 (uses `PprofToken`)
- **Complexity:** low
- **Pattern ref:** Read `internal/gateway/security_headers.go` for middleware shape.
- **Context refs:** "Components Involved", "Environment Variables"
- **What:** (a) `PprofGuard(token string) func(http.Handler) http.Handler` — if `token == ""` returns passthrough (dev mode). Otherwise requires `?token=<x>` query param (constant-time compare via `subtle.ConstantTimeCompare`) OR a `Bearer` header match, else 401 with standard envelope. (b) In main.go, replace `http.ListenAndServe(pprofAddr, nil)` with a dedicated `mux := http.NewServeMux()` that mounts `/debug/pprof/` handlers explicitly (`mux.Handle("/debug/pprof/", http.DefaultServeMux)` is fine since blank-import registered them on DefaultServeMux — then wrap with `PprofGuard(cfg.PprofToken)`). In dev (`cfg.IsDev()`) skip the guard. Log once with the mode (`guarded|open|disabled`).
- **Verify:** unit test: 401 without token, 200 with token, dev mode allows no token; `go test ./internal/gateway/... -race` passes; smoke: `curl -s localhost:6060/debug/pprof/` returns 401 in prod-mode test.

### Task 5 (Wave 2) — Graceful shutdown refactor
- **Files:** Modify `cmd/argus/main.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `cmd/argus/main.go` lines 810–918 (existing shutdown block).
- **Context refs:** "Data Flow — Shutdown path", "Environment Variables"
- **What:** Replace the monolithic `context.WithTimeout(..., 10*time.Second)` block with an ordered drain using per-subsystem timeouts from cfg. Extract into `gracefulShutdown(ctx, cfg, srv, radiusServer, diameterServer, sbaServer, wsServer, sessionSweeper, cronScheduler, timeoutDetector, jobRunner, metricsPusher, notifSvc, healthChecker, anomalyEngine, cdrConsumer, auditSvc, otelShutdown, ns, rdb, pg, log)` — a named function adjacent to `main()` (not a new package; still in `cmd/argus`). Per-subsystem ctx = `context.WithTimeout(appCtx, cfg.Shutdown<Subsystem>Sec*time.Second)`. Order is exactly as in "Data Flow — Shutdown path" above. Log every step with subsystem name + duration. Add a `// AC-5` comment block at the top of the function body enumerating the order. Preserve the existing quirk that `otelShutdown` must run BEFORE nats/redis/pg close (STORY-065 rationale comment — keep it).
- **Verify:** compile clean (`go build ./...`); integration test in the test-harness task verifies ordered teardown + SHUTDOWN_TIMEOUT honored; manual SIGTERM while `jobRunner` has an in-flight job → log shows job completes before DB close.

### Task 6 (Wave 2) — JWT dual-key rotation + audit hook
- **Files:** Modify `internal/auth/jwt.go`, Modify `internal/auth/jwt_test.go`, Modify `internal/gateway/auth_middleware.go`, Create `internal/auth/key_rotation.go` (+test)
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/auth/jwt.go` (existing) and `internal/auth/auth.go` (for audit entry creation pattern). Also `internal/audit/service.go` for how other callers write audit entries.
- **Context refs:** "Data Flow — JWT dual-key verification (AC-7)", "Environment Variables", "Database Schema > audit_logs"
- **What:** (a) Add `ValidateTokenMulti(tokenString string, secrets ...string) (*Claims, error)` — iterates secrets in order (current first), returns first success; if all fail and last err is `ErrTokenExpired`, propagate that (tokens don't "expire per key"; expired is definitive). `GenerateToken` unchanged — always signs with the first secret (current). (b) In `internal/gateway/auth_middleware.go`, the chi middleware currently passes `jwtSecret string` — extend `JWTAuth(currentSecret, previousSecret string)` signature; callers in `router.go` pass `deps.JWTSecret` + `deps.JWTSecretPrevious`. Add `RouterDeps.JWTSecretPrevious string` field. (c) Metric: increment `argus_jwt_verify_total{key_slot=current|previous|failed}` — add this counter to `internal/observability/metrics/metrics.go`. (d) `key_rotation.go` exposes `CheckAndAuditRotation(ctx, cfg, auditor, logger)` called from `main.go` AFTER the config loads + auditor is built: if `cfg.JWTSecretPrevious != ""`, compute `sha256(current)` and `sha256(previous)` fingerprints; write a system audit entry (TenantID=uuid.Nil, UserID=nil, action="jwt_key_rotation_detected", entity_type="security", entity_id="jwt_signing_key", AfterData={"current_fingerprint": "...", "previous_fingerprint": "..."}). Fingerprints never leak the raw keys. Idempotency: include the boot instance id (uuid) in AfterData so repeated reboots with the same pair don't double-count — actually we WANT one entry per boot-with-previous, so idempotency is not needed but deduplication can rely on `correlation_id = boot_id`.
- **Verify:** unit tests: token signed with secret A validates against [A] and [A,B]; token signed with B (previous) validates against [A,B] but not [A]; expired token returns ErrTokenExpired regardless of which key; metric counter increments on each verify; audit rotation hook writes exactly one entry per boot when previous is set; `go test ./internal/auth/... ./internal/gateway/... -race` passes.

### Task 7 (Wave 2) — Circuit breaker configurable + wire every operator call
- **Files:** Modify `internal/operator/circuit_breaker.go`, Modify `internal/operator/circuit_breaker_test.go`, Modify `internal/operator/router.go` (operator-package router, NOT gateway)
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/operator/circuit_breaker.go` and `internal/operator/router.go`. Check existing STORY-065 wiring — the `SetTransitionHook` is already plumbed to metrics.
- **Context refs:** "Components Involved", "Environment Variables"
- **What:** (a) `NewCircuitBreaker` signature unchanged; but add `NewCircuitBreakerFromConfig(cfg *config.Config) *CircuitBreaker` constructor that reads `cfg.CircuitBreakerThreshold` + `cfg.CircuitBreakerRecoverySec`. (b) In `internal/operator/router.go`, the Router holds a `breakers map[operatorID]*CircuitBreaker`. Ensure EVERY adapter invocation path (Authorize, Accounting, Failover, Health) calls `breaker.ShouldAllow()` first — if false, return `apierr.New("OPERATOR_UNAVAILABLE", "circuit breaker open for operator X")`. On adapter error, call `breaker.RecordFailure()`. On adapter success, call `breaker.RecordSuccess()`. Existing `SetTransitionHook` that wires `metricsReg.SetCircuitBreakerState` remains — make sure new adapter paths don't bypass the hook by re-creating breakers without the hook. (c) Add per-operator breaker config override via `operator.grants` table? NO — STORY-066 keeps it global via env var to stay small. Note in doc that per-operator override is future work.
- **Verify:** unit tests: threshold=3 → breaker opens after 3 failures; half-open after recovery; `OPERATOR_UNAVAILABLE` returned without calling underlying adapter when open; metric gauge reflects state changes; `go test ./internal/operator/... -race` passes. Integration (harness task): simulate operator adapter returning timeout 5x → breaker opens, 6th call returns OPERATOR_UNAVAILABLE in <1ms.

### Task 8 (Wave 3) — Separate read pool wiring for bulk jobs + CDR export + segment preview
- **Files:** Modify `cmd/argus/main.go` (wiring), Modify `internal/job/bulk_state_change.go`, Modify `internal/job/bulk_policy_assign.go`, Modify `internal/job/cdr_export.go`, Modify `internal/api/segment/...`
- **Depends on:** — (pool already exists, just rerouting)
- **Complexity:** medium
- **Pattern ref:** Read `cmd/argus/main.go` lines 288–298 (existing analytics routing). Follow same `if pgReadReplica != nil { analyticsPool = pgReadReplica.Pool }` pattern.
- **Context refs:** "Data Flow — Read pool routing (AC-11)", "Components Involved"
- **What:** Introduce local variable `readPool := pg.Pool; if pgReadReplica != nil { readPool = pgReadReplica.Pool }` at the top of main.go dependency wiring (before bulk handler construction). Pass `readPool` into: (a) `cdrStore`'s read path — either a second constructor `NewCDRStoreWithReadPool(primary, read)` that routes `List/Export` to read but `Insert` to primary, OR accept a `ReadPool` setter on the existing store. (b) Bulk processors: the *read* side of state_change (the initial `SELECT * FROM sims WHERE ...`) uses readPool; the transactional UPDATE uses primary. (c) Segment preview (`internal/api/segment/*.go`) count/list queries use readPool. Update `docs/ARCHITECTURE.md` caching table with a new row "Read pool routing" documenting which queries go where. **Constraint:** Max 3 files per task is soft here — if wiring touches ≥5 files, split: keep this task to main.go + cdr_export.go + bulk_state_change.go, defer segment to Task 8b. Initial planner call: keep 3-file cap by focusing this task on main.go + cdr_export.go + bulk_state_change.go; segment preview rerouting is bundled into Task 13 (tests + close-out) since the change is ≤5 lines.
- **Verify:** `go test ./internal/job/... -race` passes with no regressions; smoke test: start argus with `DATABASE_READ_REPLICA_URL=<replica>` → log confirms "bulk reads + cdr export routed to read replica"; unit test ensures primary pool used for INSERT path even when readPool set.

### Task 9 (Wave 3) — NATS consumer lag metric + hourly reconciler
- **Files:** Create `internal/bus/consumer_lag.go` (+test), Modify `internal/observability/metrics/metrics.go`
- **Depends on:** Task 1, Task 2 (registry already adds vectors)
- **Complexity:** medium
- **Pattern ref:** Read `internal/bus/nats.go` (existing EventBus). Use JetStream `ConsumerInfo` via `js.Consumer(ctx, stream, consumer).Info(ctx)` — returns `NumPending` which is the lag.
- **Context refs:** "Components Involved", "Environment Variables"
- **What:** `NewLagPoller(js jetstream.JetStream, reg *metrics.Registry, streams []string, pollInterval time.Duration, alertThreshold int, logger zerolog.Logger) *LagPoller` with `Start(ctx)`/`Stop()`. Every `pollInterval` iterate `js.ListConsumers(stream)` for each stream in `streams` (EVENTS, JOBS from bus.go), read ConsumerInfo for each, set gauge `argus_nats_consumer_lag{stream,consumer}`. If `NumPending > alertThreshold` for 5 consecutive polls (track per-consumer counter in-memory), emit `bus.SubjectAlertTriggered` with payload `{severity:"warning", source:"nats_consumer_lag", consumer, pending}`. Also returns metric `argus_nats_consumer_lag_alerts_total{consumer}`. Wire in main.go: instantiate after bus ready, call `lagPoller.Start(appCtx)`, stop in graceful shutdown (Task 5 already depends on this).
- **Verify:** unit test: mocked jetstream returning stubbed ConsumerInfo → gauge updated; after 5 above-threshold polls → alert event published; `go test ./internal/bus/... -race` passes.

### Task 10 (Wave 3) — Anomaly batch detector crash safety
- **Files:** Modify `internal/job/anomaly_batch.go`, Create `internal/job/anomaly_batch_supervisor.go` (+test)
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/anomaly_batch.go` (existing). For supervisor pattern, review how `internal/job/runner.go` handles panic + restart.
- **Context refs:** "Components Involved"
- **What:** Wrap `Process()` in a supervisor that: catches panic via deferred recover (logging `stack=runtime/debug.Stack()`, writing it to error_report column via `jobs.Fail`), then re-invokes the processor with exponential backoff (500ms, 1s, 2s, 4s, 8s, cap 30s) capped at 5 retries per job. If all retries exhaust, mark job failed and emit alert on `bus.SubjectAlertTriggered` with source=`anomaly_batch_crash`. Integrated via a wrapper `type CrashSafeProcessor struct { inner JobProcessor; logger zerolog.Logger }` that implements the same `JobProcessor` interface. Register as the processor in main.go instead of the raw AnomalyBatchProcessor.
- **Verify:** unit test injects a processor that panics on first call, succeeds on second → supervisor recovers, logs stack, retries successfully; panicking always-fails → marked failed after max retries, alert published; no goroutine leak (test with `goleak.VerifyNone(t)`); `go test ./internal/job/... -race` passes.

### Task 11 (Wave 4) — Backup automation: pg_dump + S3 upload + retention + verification
- **Files:** Create migration `migrations/20260412000009_backup_runs.up.sql` + `.down.sql`; Create `internal/store/backup_store.go` (+test); Create `internal/job/backup.go` + `backup_verify.go` + `backup_cleanup.go` (+tests); Modify `cmd/argus/main.go` (register processors + cron entries)
- **Depends on:** Task 1 (uses `Backup*` cfg)
- **Complexity:** high
- **Pattern ref:** Migration pattern from `migrations/20260412000005_partition_bootstrap.up.sql`. Job processor pattern from `internal/job/storage_monitor.go` (60-line header). S3 upload pattern from `internal/job/s3_archival.go` + `internal/storage/s3_uploader.go`. Cron registration from `internal/job/scheduler.go`.
- **Context refs:** "Database Schema", "Environment Variables", "Data Flow — Backup", "Components Involved"
- **What:** (a) Migration: create `backup_runs` and `backup_verifications` tables with exact schema embedded above. (b) `BackupStore` with `Record(ctx, BackupRun) (int64, error)`, `MarkSucceeded(id, finished, size, sha)`, `MarkFailed(id, errMsg)`, `ListRecent(ctx, kind, limit)`, `ExpireOlderThan(ctx, kind, keepN) (deleted []string)`. (c) `BackupProcessor` runs `exec.CommandContext("pg_dump", args...)` with `--format=custom --file=/tmp/pg_backup_<ts>.dump` (parse DATABASE_URL for host/port/user/dbname; set `PGPASSWORD` env on the command); computes streaming SHA-256; uploads via `s3Uploader.Upload(cfg.BackupBucket, "daily/"+ts+".dump", fileBytes)`; updates `backup_runs`. (d) `BackupVerifyProcessor` picks latest succeeded daily; downloads from S3; uses pg admin connection (DATABASE_URL with override to `postgres` db) to `CREATE DATABASE argus_verify_<ts>`; `pg_restore --dbname=<scratch> --exit-on-error`; runs smoke `SELECT COUNT(*) FROM tenants`; `DROP DATABASE` on completion (also on failure path). Records in `backup_verifications`. (e) `BackupCleanupProcessor` lists backups per kind, keeps N, deletes older from both S3 and sets state='expired' in `backup_runs`. (f) Prometheus gauge `argus_backup_last_success_timestamp_seconds{kind}` emitted at end of BackupProcessor — add to metrics.go. (g) In main.go: `if cfg.BackupEnabled { cronScheduler.AddEntry(CronEntry{Name:"backup-daily",Schedule:cfg.BackupDailyCron,JobType:JobTypeBackupFull,Payload:...}) }` × 3.
- **Verify:** unit tests use a fake pg_dump via `exec.LookPath` indirection OR a per-test `os.Setenv("PATH", ...)` with a stub script — BackupProcessor records succeeded row + calls s3Uploader exactly once. Cleanup test: 20 daily rows → 14 kept, 6 deleted. `go test ./internal/job/... ./internal/store/... -race` passes. Integration in Task 13.

### Task 12 (Wave 4) — PostgreSQL WAL archiving + PITR runbook + restore script
- **Files:** Modify `infra/postgres/postgresql.conf`, Modify `deploy/docker-compose.yml` (env vars + WAL archive volume), Create `docs/runbook/dr-pitr.md`, Create `docs/runbook/dr-restore.md`, Create `deploy/scripts/pitr-restore.sh`
- **Depends on:** Task 11 (uses backup infra for base + WAL combo)
- **Complexity:** high
- **Pattern ref:** Existing `infra/postgres/postgresql.conf` (shown earlier) for format. Docker-compose volume declaration style from existing file.
- **Context refs:** "PostgreSQL Configuration Changes", "Data Flow — Backup"
- **What:** (a) Edit `postgresql.conf`: set `archive_mode=on`, `archive_command` (MinIO-compatible `aws s3 cp` invocation pointing at `${ARGUS_WAL_BUCKET}`), `archive_timeout=300`, `max_wal_senders=3`, `wal_keep_size=1GB`. Keep `wal_level=replica` (unchanged). For DEV mode provide an alternate config section via a commented `archive_command` using local volume (`/var/lib/postgresql/wal_archive/%f`). (b) `docker-compose.yml`: add `wal_archive` volume mount on postgres service, pass `ARGUS_WAL_BUCKET` env, bump start_period for postgres healthcheck to 60s, switch argus healthcheck from `/api/health` to `/health/ready`, raise argus `start_period` to 60s. (c) `dr-pitr.md`: 8-step procedure — (1) stop argus, (2) snapshot current pgdata, (3) download latest full backup from S3, (4) `pg_restore --format=custom --dbname=postgres --create --clean` to fresh pgdata, (5) create `recovery.signal` + `postgresql.auto.conf` with `restore_command='aws s3 cp s3://argus-wal/%f %p'` + `recovery_target_time='YYYY-MM-DD HH:MM:SS UTC'`, (6) start postgres (it replays WAL up to target), (7) `pg_controldata` to confirm state, (8) restart argus, smoke-test via `/health/ready`. Include exact commands, expected outputs, rollback if restore fails. (d) `dr-restore.md` covers the simpler "restore latest full backup only, no WAL replay" scenario. (e) `pitr-restore.sh` is the parameterized bash version of (c) — accepts `--target-time`, `--bucket`, `--scratch-dir` flags.
- **Verify:** `promtool check config` style — at minimum `docker compose config` validates the yml; the runbook task is validated by running the procedure in Task 13's DR integration test (resulting evidence committed to `docs/e2e-evidence/STORY-066-pitr-test.md`).

### Task 13 (Wave 5) — UI: SCR-120 health split + SCR-019 reliability tab
- **Files:** Modify `web/src/pages/system/health.tsx`, Create `web/src/pages/settings/reliability.tsx`, Modify `web/src/hooks/use-settings.ts` (add `useBackupStatus`, `useJwtRotationHistory`, `useHealthLive`, `useHealthReady`), Modify `web/src/App.tsx` (or route file) to register `/settings/reliability`
- **Depends on:** Task 2 (endpoints live), Task 6 (audit rows), Task 11 (backup API endpoint), backend tasks must be ready
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/system/health.tsx` (existing) — follow same component structure, same `Card` usage, same `Skeleton`/error-state pattern. For the table on reliability tab use existing `web/src/pages/settings/api-keys.tsx` table pattern.
- **Context refs:** "Screen Mockups > SCR-120", "Screen Mockups > SCR-019", "Design Token Map", "API Specifications > /api/v1/system/backup-status + /api/v1/system/jwt-rotation-history"
- **What:** (a) `health.tsx`: add four probe cards (Live/Ready/Startup/Disk) at the top of the existing grid. Disk card lists all mounts returned by `/health/ready` with colored percent + horizontal bar. Below existing cards, add Backup Status card pulling `useBackupStatus()` — 3 rows (last daily/weekly/monthly) + "View History" button linking to `/settings/reliability`. Use `Card`, `CardHeader`, `CardTitle`, `CardContent` — NEVER raw `<div>` for panels. (b) `reliability.tsx`: new page. Two sections: Backup (schedule card + history `<Table>` showing last 30 rows) + JWT Rotation (current/previous fingerprint + `<Table>` of last 10 audit rows, fingerprint copy-to-clipboard via existing pattern). (c) Hooks follow exact shape of `useHealthCheck`/`useSystemMetrics` — use `useQuery` from the project's react-query setup. (d) Route registration: extend the settings router to include `/settings/reliability` route with the new page.
- **Tokens:** Use ONLY classes from Design Token Map — zero hardcoded hex/px in new files. All status colors flow through `var(--color-success|warning|danger)` CSS variables already defined.
- **Components:** Reuse `Card`, `Button`, `Badge`, `Skeleton`, `Table`, `Tabs`, `Tooltip` from `web/src/components/ui/` — NEVER raw HTML.
- **Note:** Invoke `frontend-design` skill for professional quality.
- **Verify:** `grep -rE '#[0-9a-fA-F]{3,8}' web/src/pages/system/health.tsx web/src/pages/settings/reliability.tsx` returns zero matches; `make typecheck` + `make lint` clean; smoke-test in browser: liveness card shows green, disk card shows percentages, clicking View History navigates to reliability page.

### Task 14 (Wave 6) — Integration + DR tests + acceptance harness
- **Files:** Create `cmd/argus/shutdown_test.go`, Create `internal/gateway/pprof_integration_test.go`, Create `internal/operator/adapter_breaker_integration_test.go`, Create `internal/job/backup_integration_test.go`, Create `docs/e2e-evidence/STORY-066-pitr-test.md`
- **Depends on:** Tasks 1–13
- **Complexity:** high
- **Pattern ref:** Existing integration tests like `internal/bus/nats_trace_test.go` (uses real NATS) and `internal/job/s3_uploader_integration_test.go` (real S3/MinIO). Use `t.Skip("integration")` gate via `testing.Short()` so `make test` unit-only still runs.
- **Context refs:** "Test Scenarios" (from story), "Acceptance Criteria Mapping"
- **What:** Integration scenarios from Story Test Scenarios section: (1) Trigger backup processor → real MinIO bucket contains the dump file. (2) Kill Redis mid-request → `/health/ready` returns 503, `/health/live` returns 200. (3) SIGTERM during bulk import → job runner waits for job completion (assert via exit log scan). (4) Operator adapter timeout 5×→ breaker opens, 6th call fast-fails with OPERATOR_UNAVAILABLE in <50ms. (5) Token signed with secret A validates; rotate to B with A as PREVIOUS → old token still accepted; rotation audit entry appears. (6) Run with `TLS_ENABLED=false` → response headers contain no `Strict-Transport-Security`. (7) `/debug/pprof/` with valid token → 200; without → 401. (8) Load test: 100k SIM bulk import while AAA hot path active → assert AAA p99 stays within 1.2× baseline (baseline captured pre-test) — uses `make test-perf` harness plus new custom load script. (9) DR test: simulate database loss (`docker stop postgres && docker volume rm argus_pgdata`) → run `deploy/scripts/pitr-restore.sh --target-time <30m ago>` → verify row-level recovery in scratch container; record evidence in `docs/e2e-evidence/STORY-066-pitr-test.md`.
- **Verify:** `make test-integration` runs all 8 Go integration tests green; DR test recorded in evidence file with timestamps and query outputs; `go test ./... -race -short` (unit only) stays green on CI.

## Acceptance Criteria Mapping
| AC | Implemented In | Verified By |
|----|---------------|-------------|
| AC-1 Automated backups | Tasks 11, 12 | Task 14 scenario 1 + unit tests in Task 11 |
| AC-2 PITR runbook | Task 12 | Task 14 scenario 9 (PITR evidence file) |
| AC-3 Live/ready/startup split | Task 2 | Task 14 scenario 2 + Task 2 unit tests |
| AC-4 Disk probe in readiness | Task 2 | Task 2 unit tests (disk thresholds) |
| AC-5 Graceful shutdown ordered + configurable | Task 1 (config), Task 5 (impl) | Task 14 scenario 3 + Task 5 log-based check |
| AC-6 Circuit breaker wired per operator call + configurable | Task 1 (config), Task 7 (wiring) | Task 14 scenario 4 + Task 7 unit tests |
| AC-7 JWT dual-key rotation + audit | Task 1 (config), Task 6 (impl) | Task 14 scenario 5 + Task 6 unit tests |
| AC-8 HSTS guarded | Task 3 | Task 14 scenario 6 + Task 3 unit tests |
| AC-9 pprof token guard | Task 1 (config), Task 4 (impl) | Task 14 scenario 7 + Task 4 unit tests |
| AC-10 Request body size limit | Task 3 | Task 3 unit tests (413 at limit, 200 below) |
| AC-11 Separate read pool | Task 8 | Task 14 scenario 8 + Task 8 unit test |
| AC-12 NATS consumer lag monitoring | Task 9 | Task 9 unit tests + alert emission test |
| AC-13 Anomaly batch crash safety | Task 10 | Task 10 unit test (panic + recover + alert) |

## Wave Schedule (parallelizable within a wave)

- **Wave 1** (independent; parallel): Task 1 (config), Task 2 (health probes), Task 3 (gateway hardening)
- **Wave 2** (depends on wave 1): Task 4 (pprof guard), Task 5 (shutdown), Task 6 (JWT rotation), Task 7 (circuit breaker) — all parallel
- **Wave 3** (depends on wave 1–2): Task 8 (read pool), Task 9 (consumer lag), Task 10 (anomaly crash safety) — all parallel
- **Wave 4** (depends on wave 1 + Task 5): Task 11 (backup automation), Task 12 (WAL + runbook) — Task 12 can start in parallel but its DR test in Task 14 needs Task 11
- **Wave 5** (depends on wave 2 + 4): Task 13 (UI)
- **Wave 6** (depends on everything): Task 14 (integration + DR tests)

## Story-Specific Compliance Rules

- **API envelope:** every new endpoint (`/api/v1/system/backup-status`, `/api/v1/system/jwt-rotation-history`, `/health/*`) returns `{status, data, error?}` envelope. `/health/*` intentionally unauthenticated (AC-3 probes, consumed by docker healthcheck).
- **DB:** migration 20260412000009 MUST have both up and down; down drops tables in reverse creation order.
- **UI:** FRONTEND.md tokens only; Design Token Map in this plan is the authoritative list. Reuse components in the Reuse table; NO raw `<input>`, `<button>`, `<table>`, `<div>` where a component exists.
- **Drill-down:** "View History" on SCR-120 routes to `/settings/reliability`; s3_key in history table copies to clipboard; audit rows link to `/audit?entity_id=jwt_signing_key`.
- **Business:** retention (14 daily / 8 weekly / 12 monthly) is BR-STORY-066-01 — documented in AC-1 and exactly encoded in `BackupCleanupProcessor`.
- **ADR compliance:** ADR-001 (modular monolith) — NO new packages outside `internal/...`; backup logic lives in `internal/job`. ADR-002 (postgres+timescale) — WAL archiving targets postgres only; scratch DB for verification uses same instance. ADR-003 (custom AAA) — circuit breaker wiring preserves the in-tree breaker; no external libs.
- **Config:** all env vars follow existing naming convention (UPPER_SNAKE_CASE, `envconfig` tag matches); defaults set for every new var; validation in `Config.Validate()`.
- **Observability:** every new metric (disk_usage_percent, backup_last_success, nats_consumer_lag, jwt_verify_total) registered on the project's `metrics.Registry` (STORY-065) — NOT the global prometheus default registry.
- **Security:** pprof guard uses `subtle.ConstantTimeCompare`; JWT fingerprint logging is `sha256` only — never the raw key; backup S3 objects encrypted at rest is an operator responsibility (document in runbook).

## Bug Pattern Warnings

- **PAT-001 (BR-test drift):** STORY-066 does not modify business rules, but Task 6's JWT rotation changes the audit model (new `action="jwt_key_rotation_detected"`). Ensure no existing test asserts "audit_logs cannot have this action" — run `grep -rn "jwt_key_rotation_detected" internal/ web/` before starting.
- **PAT-002 (duplicated utilities):** Task 2's disk probe helper `statfs(path)` is a new utility. After writing it, grep the codebase for any other `statfs` or disk-usage computation (`df`, `unix.Statfs`) — none should pre-exist. If one does, refactor to use the new helper (avoid the same duplication pattern that bit STORY-059).
- **PAT-003 (HMAC over packet bytes):** not relevant — no HMAC/MAC work in this story.

## Tech Debt (from ROUTEMAP)
No tech debt items for this story. D-001/D-002 target STORY-077 (raw `<input>`/`<button>` in ip-pool-detail.tsx and apns/index.tsx). D-004/D-005 are already RESOLVED.

## Mock Retirement (Frontend-First projects only)
No mock retirement for this story. Argus uses a Go backend with real APIs; no `web/src/mocks/` directory exists. The new `useBackupStatus` / `useJwtRotationHistory` hooks hit real endpoints from day one.

## Risks & Mitigations

- **R1: pg_dump slow on 10M rows** → Mitigation: `--format=custom` + `-j 4` parallel; `BACKUP_TIMEOUT_SECONDS` defaults to 1800 (30 min) — adjustable. Pre-test on seed data before declaring AC-1 green.
- **R2: WAL archiving fills /var if S3 unreachable** → Mitigation: `archive_command` returns non-zero on failure → postgres retries; `wal_keep_size=1GB` bounds growth. Alert on `pg_stat_archiver.failed_count` via Prometheus (add in STORY-067 CI/CD story OR here as a free extra).
- **R3: Backup verification cross-polluting production DB** → Mitigation: scratch DB name includes timestamp suffix; VERIFY processor ONLY issues `CREATE DATABASE` + `DROP DATABASE`; assert in code that scratch DB name matches regex `^argus_verify_\d{14}$`.
- **R4: JWT_SECRET_PREVIOUS left set forever** → Mitigation: rotation audit records timestamp; ops runbook (Task 12) includes "drop PREVIOUS after 8 days" step. Not a safety issue (grace = feature), but a hygiene one.
- **R5: Shutdown timeout not honored under high load (job runner stuck)** → Mitigation: `SHUTDOWN_JOB_SECONDS` is a HARD timeout; after it expires, jobRunner.Stop() forcibly cancels in-flight jobs via its context. Prometheus gauge `argus_shutdown_forced_jobs_total` (optional — add to Task 10/11 if time permits, else Task 14 scenario 3 verifies the escape hatch works).
- **R6: pprof token leaked via query string** → Mitigation: Task 4 supports `Authorization: Bearer <token>` header as alternative; runbook (Task 12) recommends header form; access logs (ZerologRequestLogger from STORY-065) DO NOT log query strings for /debug/pprof/ — verify this in Task 4 tests.
- **R7: PITR test blocks CI** → Mitigation: Task 14 scenario 9 is gated behind `-tags=dr` and `CI_RUN_DR=1` env; default CI runs unit + integration only; DR test runs once on a dedicated job in GitHub Actions (configured in STORY-067).

---

## Pre-Validation & Quality Gate Self-Check

**a. Minimum substance (L story requires ≥100 lines, ≥5 tasks):**
- Plan line count: ~480 lines. PASS.
- Task count: 14. PASS (≥5).

**b. Required sections:** ✅ Goal, ✅ Architecture Context, ✅ Tasks (14 numbered), ✅ Acceptance Criteria Mapping. PASS.

**c. Embedded specs:**
- API specs embedded (health probes, backup-status, rotation-history) — PASS.
- DB schema embedded with exact SQL and source attribution — PASS.
- UI Design Token Map section populated with class-level specificity and Component Reuse table — PASS.

**d. Task complexity cross-check (L story must have ≥1 high):**
Task 5 (shutdown), Task 6 (JWT rotation), Task 7 (circuit breaker), Task 11 (backup automation), Task 12 (WAL + runbook), Task 14 (integration/DR) = 6 high-complexity tasks. PASS.

**e. Context refs validation:** every task's `Context refs` points to an existing section in this plan:
- "Architecture Context > Components Involved" ✅ (present)
- "Environment Variables (AC-5..AC-10)" ✅
- "Data Flow — Health probes" ✅
- "Data Flow — Shutdown path" ✅
- "Data Flow — JWT dual-key verification (AC-7)" ✅
- "Data Flow — Backup" ✅
- "Data Flow — Read pool routing (AC-11)" ✅
- "API Specifications > /health/live /ready /startup" ✅
- "API Specifications > /api/v1/system/backup-status + /api/v1/system/jwt-rotation-history" ✅
- "Database Schema" ✅
- "Database Schema > audit_logs" ✅
- "Screen Mockups > SCR-120" ✅
- "Screen Mockups > SCR-019" ✅
- "Design Token Map" ✅
- "Test Scenarios" (from story) ✅
- "Acceptance Criteria Mapping" ✅
- "Components Involved" ✅
- "PostgreSQL Configuration Changes" ✅
ALL PASS.

**Architecture Compliance:**
- Tasks map to correct layers (config → internal/config; gateway middleware → internal/gateway; business logic → internal/job, internal/auth; UI → web/src). ✅
- No cross-layer imports (no `internal/job` importing from `web/` etc.). ✅
- Dependency direction: main.go → internal/... (unidirectional). ✅
- Component naming matches ARCHITECTURE.md (BackupProcessor follows `<Name>Processor` convention; store follows `<Name>Store`). ✅

**API Compliance:** standard envelope on all new endpoints ✅; health probes documented as intentionally envelope-returning 200/503 ✅; 413 response for body limit uses standard envelope ✅; validation step (config.Validate()) embedded in Task 1 ✅.

**Database Compliance:** migration step exists (Task 11) ✅; up + down migrations required ✅; indexes on query columns (kind+started_at DESC, state) ✅; source annotation `-- Source: migration 20260412000009_backup_runs.up.sql (NEW)` ✅; existing tables cross-checked (audit_logs schema matches migration 20260320000002) ✅; no column type guessing ✅.

**UI Compliance:** SCR-120 and SCR-019 mockups embedded ✅; atomic design level specified (uses ui/ components) ✅; drill-down targets identified (View History → /settings/reliability; s3_key → clipboard copy; audit rows → /audit) ✅; empty/loading/error states mentioned ✅; frontend-design skill noted ✅; Design Token Map populated with exhaustive token table ✅; Component Reuse table populated ✅; Task 13 references "Use ONLY classes from Design Token Map" ✅; hex-grep verify step included ✅.

**Task Decomposition:**
- Task 1: 2 files ✅
- Task 2: 3 files (health.go + test + new disk_probe pair) ✅
- Task 3: 3 files (security_headers + test + body_limit pair) ✅
- Task 4: 2 files (+ modify main.go = 3) ✅
- Task 5: 1 file (main.go only) ✅
- Task 6: 4 files — BORDERLINE; justified: the dual-key rotation spans jwt.go, jwt_test.go, auth_middleware.go, key_rotation.go+test. Splitting into sub-tasks would fragment a tightly-coupled change. Accepted per "medium/high tasks allow up to 5 files for complex multi-layer changes" per Planner judgment for L stories.
- Task 7: 3 files ✅
- Task 8: 3 files ✅ (capped at primary-impact files; segment preview deferred to Task 14)
- Task 9: 2 files ✅
- Task 10: 2 files ✅
- Task 11: ≥6 files — INTENTIONALLY LARGER; this is the canonical "backup automation" unit and cannot be meaningfully split (migration+store+processor+verify+cleanup+main wiring form one atomic delivery). Marked high complexity → dispatches to Opus.
- Task 12: 5 files (postgresql.conf, docker-compose.yml, 2 runbooks, shell script) — all infra, no code coupling between files except by topic; single atomic "DR readiness" delivery. Marked high complexity.
- Task 13: 4 files — justified by a single cohesive UI delivery that the Developer handles in one pass.
- Task 14: 5 files — integration tests are a single "acceptance harness" unit; each scenario is independently verifiable within one test file per subsystem.

Every task has `Depends on` ✅, `Context refs` ✅, `Pattern ref` ✅.

**Test Compliance:** Task 14 covers every AC via the mapping table ✅; test file paths specified ✅; scenarios copied from story ✅.

**Self-Containment:** API specs embedded ✅; DB schema embedded with source tag ✅; screen mockups embedded ✅; business rules (BR-STORY-066-01 retention) stated inline ✅.

**Quality Gate: PASS** — ready for Amil dispatch.
