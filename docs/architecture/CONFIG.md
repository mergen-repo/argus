# Configuration Reference — Argus

> All configuration is via environment variables, loaded at startup using `envconfig`.
> Implementation: `internal/config/config.go`
> Example file: `.env.example` (committed to repo, never contains real secrets)

## Configuration Loading

```go
import "github.com/kelseyhightower/envconfig"

type Config struct { ... }

func Load() (*Config, error) {
    var cfg Config
    err := envconfig.Process("", &cfg)
    return &cfg, err
}
```

All variables are read once at startup. Changing a variable requires restart (except tenant-level settings which are stored in the database and cached in Redis).

---

## Application

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `APP_ENV` | string | `development` | No | Environment: `development`, `staging`, `production`. Controls log level defaults, error verbosity, CORS permissiveness. |
| `APP_PORT` | int | `8080` | No | HTTP API server listen port. |
| `WS_PORT` | int | `8081` | No | WebSocket server listen port. |
| `LOG_LEVEL` | string | `info` | No | Zerolog level: `trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic`. In development, defaults to `debug`. |
| `DEPLOYMENT_MODE` | string | `single` | No | `single` (one instance) or `cluster` (multiple instances, enables distributed locking and NATS-based coordination). |
| `WS_MAX_CONNS_PER_TENANT` | int | `100` | No | Maximum concurrent WebSocket connections per tenant. Enforced after JWT auth with close code 4002. |
| `WS_MAX_CONNS_PER_USER` | int | `5` | No | Maximum concurrent WebSocket connections per user. When exceeded, the oldest connection is evicted with close code 4029. |
| `WS_PONG_TIMEOUT` | duration | `90s` | No | How long the server waits for a pong response after sending a ping before closing the connection. |

---

## Database (PostgreSQL + TimescaleDB)

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `DATABASE_URL` | string | — | **Yes** | PostgreSQL connection string. Format: `postgres://user:password@host:port/dbname?sslmode=disable`. |
| `DATABASE_MAX_CONNS` | int | `50` | No | Maximum open connections in the pool. Size based on: expected concurrent requests + background jobs + AAA handlers. |
| `DATABASE_MAX_IDLE_CONNS` | int | `10` | No | Maximum idle connections kept in pool. |
| `DATABASE_CONN_MAX_LIFETIME` | duration | `30m` | No | Maximum time a connection can be reused. Prevents stale connections after PG failover. |
| `DATABASE_READ_REPLICA_URL` | string | — | No | Read replica connection string. If set, all read-only queries (list, get, analytics) use this connection. Write queries always go to primary. |
| `ARGUS_AUTO_MIGRATE` | bool | `true` | No | **FIX-301**: Run pending migrations in-process at `argus serve` boot, before opening the application pool. Eliminates the boot race where pgx caches pre-migration relation OIDs. golang-migrate uses a Postgres advisory lock so multi-replica boots are safe. Set `false` in production blue-green deploys that prefer "migrate then deploy" via a separate `argus migrate up` step. |

### Connection String Examples

```bash
# Development (local Docker)
DATABASE_URL=postgres://argus:argus_dev@localhost:5432/argus_dev?sslmode=disable

# Production (with SSL)
DATABASE_URL=postgres://argus:SECURE_PASSWORD@db.example.com:5432/argus?sslmode=require

# With read replica
DATABASE_READ_REPLICA_URL=postgres://argus:SECURE_PASSWORD@db-replica.example.com:5432/argus?sslmode=require
```

### Row-Level Security (RLS)

Row-Level Security is enabled on all multi-tenant tables as defense-in-depth (migration `20260412000006_rls_policies`). The application role `argus_app` MUST hold `BYPASSRLS` before this migration runs or the platform will see empty result sets. Role grant is configured out-of-band in `deploy/` bootstrap, not in the migration itself. See `docs/architecture/db/rls.md` for deploy checklist, rationale (DEV-167), and how to set up a non-BYPASSRLS reporting role.

---

## Redis

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `REDIS_URL` | string | — | **Yes** | Redis connection URL. Format: `redis://[:password@]host:port[/db_number]`. |
| `REDIS_MAX_CONNS` | int | `100` | No | Maximum connections in the Redis pool. Higher value for AAA-heavy workloads (session cache, policy cache, rate limiting all hit Redis). |
| `REDIS_READ_TIMEOUT` | duration | `3s` | No | Read timeout for Redis operations. |
| `REDIS_WRITE_TIMEOUT` | duration | `3s` | No | Write timeout for Redis operations. |

### Redis Key Namespaces

| Prefix | TTL | Purpose |
|--------|-----|---------|
| `session:` | Session duration | Active session state |
| `sim:imsi:` | 5min | SIM lookup by IMSI |
| `policy:compiled:` | 10min | Compiled policy rules |
| `tenant:config:` | 5min | Tenant configuration |
| `ratelimit:` | Window size | Rate limit counters |
| `operator:prefix:` | 1hr | IMSI prefix routing table |
| `operator:health:` | 2 * health_check_interval_sec | Operator health status cache |
| `operator:latency:` | 2hr (auto-pruned to 1hr window) | SLA latency samples per operator (sorted set, score=timestamp, member=latencyMs) |
| `lock:` | 30s | Distributed locks (job runner) |
| `ota:ratelimit:` | 1hr | OTA per-SIM rate limit counters (INCR + EXPIRE) |
| `notif:rl:` | Per-minute sliding window TTL | Notification delivery rate limit counters per tenant (ZADD/ZREMRANGEBYSCORE/ZCARD) |
| `sessions:active:count:` | No TTL (SET by reconciler) | Active session counter per tenant (INCR on session.started, DECR on session.ended; reconciled hourly) |
| `dashboard:` | 30s | Cached dashboard aggregate response per tenant (DEL on sim.*, session.*, operator.health_changed, cdr.recorded NATS events) |

---

## NATS JetStream

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `NATS_URL` | string | — | **Yes** | NATS server URL. Format: `nats://host:port`. |
| `NATS_CLUSTER_ID` | string | `argus-cluster` | No | NATS Streaming cluster ID (for JetStream). |
| `NATS_MAX_RECONNECT` | int | `60` | No | Maximum reconnection attempts before giving up. |
| `NATS_RECONNECT_WAIT` | duration | `2s` | No | Wait time between reconnection attempts. |

### NATS Subjects

| Subject | Purpose | Consumer |
|---------|---------|----------|
| `argus.events.session.*` | Session start/stop/update events | WebSocket broadcaster, CDR processor |
| `argus.events.sim.*` | SIM state change events | Cache invalidation, WebSocket |
| `argus.events.policy.*` | Policy change events | Cache invalidation, WebSocket |
| `argus.events.operator.*` | Operator health events | WebSocket, alert engine |
| `argus.events.notification.*` | Notification dispatch | Notification engine |
| `argus.jobs.queue` | Job queue (pull-based) | Job runner |
| `argus.jobs.completed` | Job completion notification | Dashboard, WebSocket broadcaster |
| `argus.jobs.progress` | Job progress updates (percent + message) | WebSocket broadcaster |
| `argus.events.alert.triggered` | Alert triggered (anomaly, threshold) | Alert engine, notification dispatcher |
| `argus.events.audit.create` | Audit log entry created | Audit consumer, WebSocket broadcaster |
| `argus.cache.invalidate` | Cache invalidation broadcast | All instances |

---

## Authentication & Security

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `JWT_SECRET` | string | — | **Yes** | HMAC secret for signing JWT tokens. Minimum 32 characters. **Keep secret.** |
| `JWT_EXPIRY` | duration | `15m` | No | Access token expiration. Short-lived for security. |
| `JWT_REFRESH_EXPIRY` | duration | `168h` | No | Refresh token expiration (168h = 7 days). Stored in httpOnly cookie and database (TBL-03). |
| `AUTH_JWT_REMEMBER_ME_TTL` | duration | `168h` | No | Access token TTL when `remember_me=true` on login (168h = 7 days). Enables persistent sessions across browser restarts. |
| `JWT_ISSUER` | string | `argus` | No | JWT `iss` claim value. |
| `BCRYPT_COST` | int | `12` | No | bcrypt cost factor for password hashing. Range 10-14. Higher = slower but more secure. 12 is ~250ms on modern hardware. |
| `LOGIN_MAX_ATTEMPTS` | int | `5` | No | Consecutive failed login attempts before account lockout. Must be >= 1. |
| `LOGIN_LOCKOUT_DURATION` | duration | `15m` | No | Account lockout duration after max failed attempts. Must be > 0. |
| `ENCRYPTION_KEY` | string | — | No | 32-byte hex-encoded key for AES-256-GCM encryption of sensitive fields (adapter_config, sm_dp_plus_config, totp_secret). Empty = no encryption (dev mode passthrough). **Keep secret.** |

---

## Password Policy (STORY-068)

Controls server-side password complexity and history enforcement. Applied on registration, password change, and admin-triggered resets. Tenant-level overrides are not supported — these are global platform minimums.

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `PASSWORD_MIN_LENGTH` | int | `12` | No | Minimum password length in characters. Must be >= 8. |
| `PASSWORD_REQUIRE_UPPER` | bool | `true` | No | Require at least one uppercase letter (A–Z). |
| `PASSWORD_REQUIRE_LOWER` | bool | `true` | No | Require at least one lowercase letter (a–z). |
| `PASSWORD_REQUIRE_DIGIT` | bool | `true` | No | Require at least one numeric digit (0–9). |
| `PASSWORD_REQUIRE_SYMBOL` | bool | `true` | No | Require at least one special character (e.g. `!@#$%^&*`). |
| `PASSWORD_MAX_REPEATING` | int | `3` | No | Maximum number of consecutive identical characters allowed (e.g. `aaa` violates limit of 3). Must be >= 2. |
| `PASSWORD_HISTORY_COUNT` | int | `5` | No | Number of previous password hashes stored per user. New password must not match any stored hash. 0 = history disabled. Must be >= 0. |
| `PASSWORD_MAX_AGE_DAYS` | int | `0` | No | Days before a password expires and user is forced to change it. 0 = expiry disabled. |

---

## Password Reset (FIX-228)

Self-service password reset flow: user submits email → receives tokenized link → confirms with new password. All config below is global (no per-tenant override).

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `PASSWORD_RESET_RATE_LIMIT_PER_HOUR` | int | `5` | No | Max password reset requests per email per hour. Enforced DB-side via `password_reset_tokens.email_rate_key` rolling window (no Redis). Valid range: 1–1000. |
| `PASSWORD_RESET_TOKEN_TTL_MINUTES` | int | `60` | No | Lifetime of a reset token in minutes. Token is single-use (row deleted on confirm). Valid range: 5–1440. |
| `PUBLIC_BASE_URL` | string | `http://localhost:8084` | No | Base URL used in password reset email links (e.g. `https://argus.example.com`). Reset link format: `{PUBLIC_BASE_URL}/auth/reset?token=<b64token>`. |

---

## Account Lockout

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `LOGIN_MAX_ATTEMPTS` | int | `5` | No | Consecutive failed login attempts before the account is temporarily locked. Must be >= 1. |
| `LOGIN_LOCKOUT_DURATION` | duration | `15m` | No | How long the account lockout persists before the user can attempt login again. Must be > 0. |

---

## AAA Protocol Servers

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `RADIUS_AUTH_PORT` | int | `1812` | No | RADIUS Authentication server UDP port. |
| `RADIUS_ACCT_PORT` | int | `1813` | No | RADIUS Accounting server UDP port. |
| `RADIUS_SECRET` | string | — | **Yes** (if RADIUS enabled) | Default RADIUS shared secret. Per-operator secrets override this in operator settings (TBL-05). |
| `RADIUS_WORKER_POOL_SIZE` | int | `256` | No | Number of goroutines handling RADIUS requests concurrently. |
| `RADIUS_COA_PORT` | int | `3799` | No | Default CoA/DM target port on NAS (can be overridden per operator). |
| `DIAMETER_PORT` | int | `3868` | No | Diameter server TCP port. |
| `DIAMETER_ORIGIN_HOST` | string | — | **Yes** (if Diameter enabled) | Diameter Origin-Host AVP. FQDN of this Argus instance. |
| `DIAMETER_ORIGIN_REALM` | string | — | **Yes** (if Diameter enabled) | Diameter Origin-Realm AVP. Domain realm. |
| `DIAMETER_TLS_ENABLED` | bool | `false` | No | Enable TLS wrapping of Diameter TCP listener on port 3868 |
| `DIAMETER_TLS_CERT_PATH` | string | — | If TLS | PEM server cert path |
| `DIAMETER_TLS_KEY_PATH` | string | — | If TLS | PEM server key path |
| `DIAMETER_TLS_CA_PATH` | string | — | No | PEM CA bundle for peer mTLS; when set, Argus requires and verifies client certificates |
| `DIAMETER_VENDOR_ID` | int | `99999` | No | Argus vendor ID (for custom AVPs). 3GPP AVPs use 10415. |
| `DIAMETER_WATCHDOG_INTERVAL` | duration | `30s` | No | DWR (Device-Watchdog-Request) send interval. |
| `SBA_PORT` | int | `8443` | No | 5G SBA HTTPS/HTTP2 server port. |
| `SBA_ENABLED` | bool | `false` | No | Enable 5G SBA proxy server. |
| `SBA_ENABLE_MTLS` | bool | `false` | No | Enable mutual TLS (mTLS) for 5G SBA server. When true, requires client certificates for NF-to-NF communication. |
| `SBA_NRF_URL` | string | — | No | NRF (Network Repository Function) registration endpoint URL. When set, Argus registers on startup and sends heartbeats. Example: `https://nrf.5gc.example.com`. |
| `SBA_NF_INSTANCE_ID` | string | `argus-sba-01` | No | NF Instance ID sent in NRF registration requests. Must be unique per Argus instance in a 5G core cluster. |
| `SBA_NRF_HEARTBEAT_SEC` | int | `30` | No | NRF heartbeat interval in seconds. Argus sends a PUT to the NRF profile URL at this interval to maintain registration. |

---

## Rate Limiting

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `RATE_LIMIT_DEFAULT_PER_MINUTE` | int | `1000` | No | Default requests per minute per tenant/user. Applied when no specific limit is configured. |
| `RATE_LIMIT_DEFAULT_PER_HOUR` | int | `30000` | No | Default requests per hour per tenant/user. |
| `RATE_LIMIT_ALGORITHM` | string | `sliding_window` | No | Algorithm: `sliding_window` (default, most accurate) or `fixed_window` (simpler, slightly less accurate). |
| `RATE_LIMIT_AUTH_PER_MINUTE` | int | `10` | No | Login attempts per minute per IP (brute force protection). |
| `RATE_LIMIT_ENABLED` | bool | `true` | No | Master switch to disable rate limiting (useful in development). |
| `NOTIFICATION_RATE_LIMIT_PER_MINUTE` | int | `60` | No | Maximum notification deliveries per minute per tenant. Enforced via Redis sliding window (`notif:rl:` namespace, ZADD/ZREMRANGEBYSCORE/ZCARD). Applied by `DeliveryTracker` in `internal/notification/`. |
| `RATE_LIMIT_SMS_PER_MINUTE` | int | `60` | No | Maximum outbound SMS messages per minute per tenant via `POST /api/v1/sms/send`. Enforced via Redis sliding window (key `sms:rate:{tenant_id}`). Exceeding the limit returns 429 with error code `RATE_LIMITED`. |

---

## Background Jobs (SVC-09)

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `JOB_MAX_CONCURRENT_PER_TENANT` | int | `5` | No | Maximum simultaneously running background jobs per tenant. Prevents a single tenant from monopolizing job runner capacity. |
| `JOB_TIMEOUT_MINUTES` | int | `30` | No | Minutes before a stale running job (no progress) is automatically marked as failed by the timeout detector. |
| `JOB_TIMEOUT_CHECK_INTERVAL` | duration | `5m` | No | How often the timeout detector sweeps for stale running jobs. |
| `ORPHAN_SESSION_CHECK_INTERVAL` | duration | `30m` | No | How often the orphan session detector scans for active sessions with NULL apn_id (data integrity check). Accepts Go duration format (e.g. `15m`, `1h`). Non-positive and unparseable values fall back to the 30m default. |
| `JOB_LOCK_TTL` | duration | `60s` | No | Redis distributed lock TTL for job-level and SIM-level locks (SETNX). Auto-renewed during execution. |
| `JOB_LOCK_RENEW_INTERVAL` | duration | `30s` | No | How often the lock renewal goroutine extends the lock TTL. Must be less than `JOB_LOCK_TTL`. |
| `CRON_ENABLED` | bool | `true` | No | Enable/disable the cron scheduler. Set to `false` in test environments or when running multiple instances without Redis dedup. |
| `CRON_PURGE_SWEEP` | string | `@daily` | No | Cron schedule for the purge sweep job (KVKK/GDPR auto-purge of terminated SIMs). Supports `@daily`, `@hourly`, `@weekly`, `@monthly`, or 5-field cron expressions. |
| `CRON_IP_RECLAIM` | string | `@hourly` | No | Cron schedule for the IP reclaim job (returns terminated SIM IPs to pool after grace period). |
| `CRON_SLA_REPORT` | string | `@daily` | No | Cron schedule for the SLA report generation job. |
| `CRON_FLEET_DIGEST` | string | `*/15 * * * *` | No | FIX-237 — Cron schedule for the fleet digest worker (Tier 2 aggregator). Runs every 15 min by default; tune to operator preference. |

#### STORY-069 Cron Entries (hard-coded schedules — no env vars)

| Cron name | Schedule | Job type | Purpose |
|-----------|----------|----------|---------|
| `kvkk_purge_daily` | `@daily` | `kvkk_purge_daily` | Pseudonymises PII in `cdrs`/`sessions`/`audit_logs` past tenant retention (KVKK/GDPR). Honors `payload.dry_run=true` for first prod run. |
| `ip_grace_release` | `@hourly` | `ip_grace_release` | Returns IPs whose grace window has elapsed back to the pool, publishes `ip.released` event. |
| `webhook_retry_sweep` | `*/1 * * * *` | `webhook_retry` | Re-sends due `webhook_deliveries` rows; backoff 30s/2m/10m/30m/60m; after 5 attempts marks `dead_letter` and emits `webhook.dead_letter` notification. |
| `scheduled_report_sweeper` | `*/1 * * * *` | `scheduled_report_sweeper` | Scans `scheduled_reports.next_run_at <= now()` and enqueues a `scheduled_report_run` per due row. |
| `alerts_retention` | `15 3 * * *` | `alerts_retention` | FIX-209 — purges rows from the unified `alerts` table older than `ALERTS_RETENTION_DAYS` (default 180). |
| `stuck_rollout_reaper` | `*/5 * * * *` | `stuck_rollout_reaper` | FIX-231 — finds rollouts where `migrated_sims >= total_sims AND age > ARGUS_STUCK_ROLLOUT_GRACE_MINUTES` and calls `CompleteRollout` for each. Emits `policy.rollout_progress` event per reaped rollout. |

### FIX-237 Digest Worker Thresholds

The fleet digest worker (run every 15 min by default — see `CRON_FLEET_DIGEST`) emits Tier 2 fleet events when these thresholds are crossed. All values are operator-tunable; defaults target a 10M+ M2M fleet.

| Env var | Type | Default | Required | Description |
|---------|------|---------|----------|-------------|
| `ARGUS_DIGEST_MASS_OFFLINE_PCT` | float | `5.0` | No | % of active SIMs offline in 15-min window that triggers `fleet.mass_offline`. Severity scales: low <5%, medium 5-15%, high 15-30%, critical >30%. |
| `ARGUS_DIGEST_MASS_OFFLINE_FLOOR` | int | `10` | No | Absolute minimum offline-SIM count. Below this, no event regardless of percentage (prevents noise on small fleets). |
| `ARGUS_DIGEST_TRAFFIC_SPIKE_RATIO` | float | `3.0` | No | Bytes/15-min divided by rolling baseline that triggers `fleet.traffic_spike`. |
| `ARGUS_DIGEST_QUOTA_BREACH_COUNT` | int | `50` | No | Number of SIMs crossing quota in window that triggers `fleet.quota_breach_count`. |
| `ARGUS_DIGEST_QUOTA_BREACH_FLOOR` | int | `10` | No | Absolute floor for quota breaches (always 10 minimum). |
| `ARGUS_DIGEST_VIOLATION_SURGE_RATIO` | float | `2.0` | No | policy_violation events/15-min divided by baseline that triggers `fleet.violation_surge`. |
| `ARGUS_DIGEST_VIOLATION_SURGE_FLOOR` | int | `10` | No | Absolute floor for violation surge (always 10 minimum). |

See `docs/architecture/EVENTS.md` for severity-scaling table and the M2M event taxonomy these thresholds support.

### Redis Key Patterns (Job System)

| Pattern | TTL | Purpose |
|---------|-----|---------|
| `argus:lock:job:{id}` | `JOB_LOCK_TTL` | Distributed lock per job (prevents concurrent processing) |
| `argus:lock:sim:{id}` | `JOB_LOCK_TTL` | Distributed lock per SIM (prevents concurrent bulk ops on same SIM) |
| `argus:cron:{name}:{tick}` | ~schedule interval | Cron dedup key (SETNX ensures single-instance execution per tick) |

---

## Capacity Targets (SVC-01) — STORY-070

These variables set the expected platform-wide capacity targets shown in the System Capacity dashboard (`GET /api/v1/system/capacity`). They do not enforce hard limits — they are display targets only.

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `ARGUS_CAPACITY_SIM` | int | `15000000` | No | Total SIM capacity target (across all tenants). Used as denominator in capacity utilisation gauge. |
| `ARGUS_CAPACITY_SESSION` | int | `2000000` | No | Maximum concurrent active session target. |
| `ARGUS_CAPACITY_AUTH` | int | `5000` | No | Target maximum authentications per second. |
| `ARGUS_CAPACITY_GROWTH_SIMS_MONTHLY` | int | `72000` | No | Expected net new SIM activations per month (growth forecast for trend line). |

---

## Roaming Agreements — REMOVED (FIX-238, 2026-04-30)

`ROAMING_RENEWAL_ALERT_DAYS` and `ROAMING_RENEWAL_CRON` were removed along with
the roaming agreements feature (STORY-071). The variables are no longer read by
the binary. Any deployment env files referencing them should drop the entries.

---

## Notifications

### Email (SMTP)

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `SMTP_HOST` | string | — | No | SMTP server hostname. Required if email notifications enabled. |
| `SMTP_PORT` | int | `587` | No | SMTP server port. 587 for STARTTLS, 465 for implicit TLS. |
| `SMTP_USER` | string | — | No | SMTP authentication username. |
| `SMTP_PASSWORD` | string | — | No | SMTP authentication password. **Keep secret.** |
| `SMTP_FROM` | string | `noreply@argus.io` | No | From address for outgoing emails. |
| `SMTP_TLS` | bool | `true` | No | Enable TLS for SMTP. |

> **Dev SMTP fixture (FIX-228 DEV-328):** `deploy/docker-compose.yml` ships a `mailhog` service (`mailhog/mailhog:v1.0.1`) on port 1025 (SMTP) and 8025 (web UI at `http://localhost:8025`). `.env.example` SMTP defaults point to mailhog (`SMTP_HOST=localhost`, `SMTP_PORT=1025`, `SMTP_TLS=false`). SHA256 digest pin deferred to D-130 (infra pinning wave).

### Telegram

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `TELEGRAM_BOT_TOKEN` | string | — | No | Telegram Bot API token. Required if Telegram notifications enabled. **Keep secret.** |
| `TELEGRAM_DEFAULT_CHAT_ID` | string | — | No | Default Telegram chat/group ID for system-wide alerts. Per-tenant chat IDs stored in notification_configs (TBL-22). |

---

## Storage (S3-Compatible)

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `S3_ENDPOINT` | string | — | No | S3-compatible endpoint URL. For AWS S3 leave empty. For MinIO: `http://minio:9000`. |
| `S3_ACCESS_KEY` | string | — | No | S3 access key ID. **Keep secret.** |
| `S3_SECRET_KEY` | string | — | No | S3 secret access key. **Keep secret.** |
| `S3_BUCKET` | string | `argus-storage` | No | S3 bucket name for exports, archives, and bulk import files. |
| `S3_REGION` | string | `eu-west-1` | No | S3 region. |
| `S3_PATH_STYLE` | bool | `false` | No | Use path-style S3 URLs (required for MinIO). |

### S3 Object Key Structure

```
{bucket}/
├── exports/
│   ├── cdrs/{tenant_id}/{date}/export_{job_id}.csv
│   └── audit/{tenant_id}/{date}/export_{job_id}.csv
├── imports/
│   └── sims/{tenant_id}/{job_id}/upload.csv
├── archives/
│   ├── audit/{tenant_id}/{year}/{month}/audit_logs_{partition}.parquet
│   └── cdrs/{tenant_id}/{year}/{month}/cdrs_{partition}.parquet
└── backups/
    └── db/{date}/argus_backup.sql.gz
```

## Report Storage (FIX-248)

The report subsystem uses a `Storage` interface (`internal/storage/storage.go`)
with two interchangeable backends. Pre-FIX-248 the codebase only supported S3
and silently no-op'd in environments without EC2 IMDS; this is fixed.

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `REPORT_STORAGE` | string | `local` | No | `local` (LocalFS, default) or `s3`. When `s3`, the S3_* variables above are reused. |
| `REPORT_STORAGE_PATH` | path | `/var/lib/argus/reports` | No | LocalFS root. Must be writeable by the argus process. The Docker compose mounts `./data/reports` to this path. |
| `REPORT_SIGNING_KEY` | hex string | (auto-generated) | **Yes for multi-instance** | 32-byte hex (≥16 raw bytes) used for HMAC-SHA256 signing of download URLs. **Multi-instance deployments MUST set this** — auto-generated keys differ per instance, breaking URLs. Logs a warning when missing. |
| `REPORT_RETENTION_DAYS` | int | `90` | No | LocalFS file retention. Cleanup cron deletes files where `mtime + retention < now`. (Cleanup processor itself deferred to D-167.) |
| `REPORT_PUBLIC_BASE_URL` | url | `http://localhost:8084` | No | Base URL prepended to LocalFS signed download links. Set to your customer-facing host (e.g. `https://argus.example.com`) in production. |

### LocalFS layout

```
{REPORT_STORAGE_PATH}/tenants/{tenant_id}/reports/{job_id}/{filename}
```

The hierarchical path matches the S3 object key shape used by
`scheduledReportProcessor`, so neither backend cares about path semantics.

### Signed URL contract

```
{REPORT_PUBLIC_BASE_URL}/api/v1/reports/download/{key_b64}?expires={unix}&sig={hex}

  key_b64  = base64.RawURLEncoding(key)
  sig      = hex(HMAC-SHA256(key + "|" + expires_unix, REPORT_SIGNING_KEY))
  TTL      = 7 days (matches the pre-FIX-248 S3 contract)
```

Verification: the download handler decodes `key_b64`, parses `expires`, and
re-derives the HMAC with the same signing key. Constant-time compare; bad
signatures return 401 with `{error: "Invalid download token"}`. Path
traversal (`..`, absolute paths) is rejected during decode.

---

## eSIM SM-DP+

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `ESIM_SMDP_PROVIDER` | string | `mock` | No | SM-DP+ adapter: `mock` (development, always succeeds) or `generic` (production HTTP adapter targeting SGP.22 ES9+ JSON endpoints). |
| `ESIM_SMDP_BASE_URL` | string | — | If generic | Base URL of the SM-DP+ server. Example: `https://smdp.example.com`. |
| `ESIM_SMDP_API_KEY` | string | — | If generic | API key for SM-DP+ authentication. **Keep secret.** |
| `ESIM_SMDP_CLIENT_CERT_PATH` | string | — | No | Path to mTLS client certificate (PEM) for SM-DP+ mutual TLS. |
| `ESIM_SMDP_CLIENT_KEY_PATH` | string | — | No | Path to mTLS client private key (PEM) for SM-DP+ mutual TLS. |

---

## SMS Gateway

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `SMS_PROVIDER` | string | `` | No | SMS provider: `twilio` or empty/unset (SMS disabled). |
| `SMS_ACCOUNT_ID` | string | — | If Twilio | Twilio Account SID. **Keep secret.** |
| `SMS_AUTH_TOKEN` | string | — | If Twilio | Twilio Auth Token. **Keep secret.** |
| `SMS_FROM_NUMBER` | string | — | If Twilio | Sender phone number in E.164 format (e.g. `+15005550006`). |
| `SMS_STATUS_CALLBACK_URL` | string | — | No | Webhook URL for Twilio delivery status callbacks. |

---

## TLS Certificates

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `TLS_CERT_PATH` | string | — | No | Path to TLS certificate file (PEM format). For SBA HTTPS and general TLS. |
| `TLS_KEY_PATH` | string | — | No | Path to TLS private key file (PEM format). |
| `RADSEC_CERT_PATH` | string | — | No | Path to RadSec server certificate (PEM). Required if any operator uses RadSec transport. |
| `RADSEC_KEY_PATH` | string | — | No | Path to RadSec server private key (PEM). |
| `RADSEC_CA_PATH` | string | — | No | Path to CA bundle for verifying RadSec client certificates. |

---

## Tenant Defaults

These values are used when creating new tenants. They can be overridden per-tenant in the database (TBL-01 `tenants.resource_limits`).

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `DEFAULT_MAX_SIMS` | int | `1000000` | No | Maximum SIMs per tenant. |
| `DEFAULT_MAX_APNS` | int | `100` | No | Maximum APNs per tenant. |
| `DEFAULT_MAX_USERS` | int | `50` | No | Maximum portal users per tenant. |
| `DEFAULT_MAX_API_KEYS` | int | `20` | No | Maximum API keys per tenant. |
| `DEFAULT_PURGE_RETENTION_DAYS` | int | `90` | No | Days to retain terminated SIM data before auto-purge. KVKK/GDPR compliance. |
| `DEFAULT_AUDIT_RETENTION_DAYS` | int | `365` | No | Days to retain audit logs before archiving to S3. |
| `DEFAULT_CDR_RETENTION_DAYS` | int | `180` | No | Days to retain CDR records in TimescaleDB before compression/archiving. |
| `ALERTS_RETENTION_DAYS` | int | `180` | No | FIX-209 — days to retain rows in the unified `alerts` table. Minimum enforced: `30`. Older rows are purged daily at 03:15 UTC by the `alerts_retention` job. |
| `ALERT_COOLDOWN_MINUTES` | int | `5` | No | FIX-210 — minutes an alert stays in cooldown after resolve. Repeat events with the same `dedup_key` are dropped (metric: `argus_alerts_cooldown_dropped_total`) during the window. Range: `0..1440` (0 disables cooldown; values above 1440 are clamped). |
| `ARGUS_STUCK_ROLLOUT_GRACE_MINUTES` | int | `10` | No | FIX-231 — minutes a rollout that has `migrated_sims >= total_sims` is allowed to remain `in_progress` before the stuck-rollout reaper auto-completes it. Config-time clamped to `[5, 120]`. PAT-017 wiring: env → config struct → constructor → `StuckRolloutReaperProcessor` field → SQL `make_interval(mins => $grace)`. |

---

## Development Overrides

These variables are only meaningful in development mode (`APP_ENV=development`):

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `DEV_CORS_ALLOW_ALL` | bool | `true` | Allow all CORS origins (for localhost dev server). Hardening deferred to STORY-074 AC-3. |

---

## Observability

> `/metrics` exposes Prometheus text format (NOT the `/api/v1/system/metrics` JSON envelope — that endpoint remains for admin-UI real-time push).
> Grafana dashboards: `infra/grafana/dashboards/`
> Alert rules: `infra/prometheus/alerts.yml`
> Docker Compose overlay: `docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.obs.yml up`

### OpenTelemetry (Tracing)

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | string | `""` | No | OTLP gRPC endpoint for trace export. Empty = noop tracer (tracing disabled). Example: `http://jaeger:4317`. |
| `OTEL_SAMPLER_RATIO` | float | `1.0` | No | Span sampling ratio (0.0–1.0) via ParentBased/TraceIDRatioBased sampler. Set to `0.1` in high-throughput production. |
| `OTEL_SERVICE_NAME` | string | `"argus"` | No | `service.name` resource attribute attached to all spans. |
| `OTEL_SERVICE_VERSION` | string | `"dev"` | No | `service.version` resource attribute. Override to `$(git describe --tags)` in production CI. |
| `OTEL_DEPLOYMENT_ENVIRONMENT` | string | `"development"` | No | `deployment.environment` resource attribute. Use `staging` / `production` in upper environments. |
| `OTEL_BSP_EXPORT_TIMEOUT_SEC` | int | `5` | No | BatchSpanProcessor export timeout in seconds. Increase if the OTLP collector is remote/slow. |

### Prometheus (Metrics)

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `METRICS_ENABLED` | bool | `true` | No | Global Prometheus registry enable switch. Set to `false` to disable the `/metrics` endpoint entirely. |
| `METRICS_NAMESPACE` | string | `"argus"` | No | Metric name prefix (e.g. `argus_http_requests_total`). Reserved for future multi-instance disambiguation. |
| `METRICS_TENANT_LABEL_ENABLED` | bool | `true` | No | Cardinality kill switch for the `tenant_id` label on HTTP and AAA metrics. Set to `false` in emergency to reduce cardinality without redeploy. |

---

## Kill Switches (FIX-245)

Kill switches are read from environment variables at runtime with a 30-second in-process TTL cache. No DB table, no admin UI. Semantic: variable **unset** (or any value other than `on`, `true`, `1`) = feature **permitted/mutating**. Setting to `on` **blocks** the named operation system-wide.

| Variable | Default | Effect when active | TTL cache | Restart needed |
|----------|---------|-------------------|-----------|----------------|
| `KILLSWITCH_RADIUS_AUTH` | unset (permit) | Blocks all RADIUS Access-Request handling — NAS receives Access-Reject | 30s | Recommended |
| `KILLSWITCH_SESSION_CREATE` | unset (permit) | Blocks new session creation (RADIUS Accounting-Start, SBA UE-Registration) | 30s | Recommended |
| `KILLSWITCH_BULK_OPERATIONS` | unset (permit) | Blocks all bulk endpoints (`POST /api/v1/sims/bulk-*`, `/api/v1/esim-profiles/bulk-switch`, import jobs) | 30s | Recommended |
| `KILLSWITCH_EXTERNAL_NOTIFICATIONS` | unset (permit) | Suppresses all outbound notifications (SMTP, Telegram, webhook dispatch) | 30s | Recommended |
| `KILLSWITCH_READ_ONLY_MODE` | unset (mutations OK) | Blocks all state-changing API operations (POST/PUT/PATCH/DELETE); read-only GETs and WebSocket pass through | 30s | Recommended |

> **Implementation**: `internal/killswitch/service.go` — `Service` caches `os.Getenv` results for 30s to avoid syscall overhead on high-throughput paths. The cache TTL is measured via an injectable `clock` seam (test-friendly). `IsEnabled(name)` returns `true` when the env var resolves to `on`, `true`, or `1` (case-insensitive).
>
> **No restart required for immediate effect**: the 30-second cache means toggles take effect within one TTL window without restarting the process. A restart accelerates propagation to instant.
>
> **Operational reference**: see `docs/operational/EMERGENCY_PROCEDURES.md` for toggle runbook.

---

## CI/CD & Ops Tooling (STORY-067)

### Deploy Scripts (bluegreen-flip.sh / rollback.sh)

These variables are consumed by `deploy/scripts/bluegreen-flip.sh` and `deploy/scripts/rollback.sh`. They are **not** read by the Argus binary.

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `ARGUS_API_URL` | string | `http://localhost:8084` | No | Base URL used by deploy scripts to post audit events to `POST /api/v1/audit/system-events`. Must be HTTPS in production. |
| `ARGUS_API_TOKEN` | string | — | **Yes** | Bearer token (super_admin JWT) for deploy/rollback script audit emission. Scripts abort with `die` if unset. |

### argusctl CLI

`argusctl` uses the `ARGUSCTL_` Viper env prefix. Env vars map directly to CLI flags (e.g. `ARGUSCTL_TOKEN` = `--token`).

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `ARGUSCTL_API_URL` | string | `http://localhost:8084` | No | Argus API base URL for argusctl commands. |
| `ARGUSCTL_TOKEN` | string | — | No | Admin JWT or API key. Alternatively passed via `--token`. |
| `ARGUSCTL_CERT` | string | — | No | mTLS client certificate path. |
| `ARGUSCTL_KEY` | string | — | No | mTLS client private key path. |
| `ARGUSCTL_CA` | string | — | No | mTLS CA certificate path. |
| `ARGUSCTL_FORMAT` | string | `table` | No | Output format: `table` or `json`. |

Config file (optional): `~/.argusctl.yaml` — keys map to flag names without the `ARGUSCTL_` prefix (e.g. `token:`, `api_url:`).

---

## Simulator Environment (dev/demo only)

The RADIUS + Diameter + 5G SBA traffic simulator is an **opt-in** component started via `docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.simulator.yml up simulator`. It runs as a separate binary (`cmd/simulator`) with its own YAML config file (`deploy/simulator/config.example.yaml`) and a dedicated read-only PG role (`argus_sim`).

All `ARGUS_SIM_*` env vars override YAML fields at startup. When unset, YAML values are used as-is. Validate() rejects invalid values — there is no silent clamping.

| Variable | Maps to YAML | Default (YAML) | Notes |
|----------|--------------|----------------|-------|
| `SIMULATOR_ENABLED` | — | (unset = exit) | Must be `"true"` for the binary to run; exit 1 if absent or non-true |
| `ARGUS_SIM_CONFIG` | — | `/etc/simulator/config.yaml` | Path to YAML config file |
| `ARGUS_SIM_LOG_LEVEL` | `log.level` | `info` | `debug\|info\|warn\|error` |
| `ARGUS_SIM_DB_URL` | `argus.db_url` | (YAML) | Read-only PG URL (`argus_sim` role) |
| `ARGUS_SIM_RADIUS_HOST` | `argus.radius_host` | `argus-app` | Argus RADIUS server hostname |
| `ARGUS_SIM_RADIUS_SECRET` | `argus.radius_shared_secret` | (YAML) | Must match Argus `RADIUS_SECRET` |
| `ARGUS_SIM_COA_SECRET` | `reactive.coa_listener.shared_secret` | (inherits RADIUS secret) | CoA/DM listener shared secret |
| `ARGUS_SIM_SESSION_RATE_PER_SEC` | `rate.max_radius_requests_per_second` | `25` | Max RADIUS req/s; must be > 0 |
| `ARGUS_SIM_VIOLATION_RATE_PCT` | `scenarios[aggressive_m2m].weight` | `1.0` (=1%) | Float 0–100; rescales `aggressive_m2m` weight, reduces `normal_browsing` proportionally |
| `ARGUS_SIM_DIAMETER_ENABLED` | all `operators[*].diameter` blocks | `true` | `false` = nil out all operator Diameter configs globally |
| `ARGUS_SIM_SBA_ENABLED` | all `operators[*].sba` blocks | `true` | `false` = nil out all operator SBA configs globally |
| `ARGUS_SIM_INTERIM_INTERVAL_SEC` | `scenarios[*].interim_interval_seconds` | `0` (=use YAML) | When > 0, overrides ALL scenario interim intervals at startup |

**NOT ADDED** (follow-up if demand arises):
- `SIM_COUNT_TARGET` — simulator uses read-only `argus_sim` role; SIM creation requires a writer-component story (see DEV-317).
- `SBA_USE_RATE_FLOOR` — reserved for future knob to guarantee minimum SBA traffic volume.

> **Note:** `nas_ip` in the operator config block must be a valid IPv4 address (RFC 5737 TEST-NET-1 is recommended: `192.0.2.10/20/30`). DNS hostnames are silently skipped by `net.ParseIP` and cause NAS-IP-Address AVP to be omitted; the `simulator_nas_ip_missing_total` Prometheus counter tracks this condition.

---

## Complete .env.example

```bash
# === Application ===
APP_ENV=development
APP_PORT=8080
WS_PORT=8081
LOG_LEVEL=debug
DEPLOYMENT_MODE=single

# === Database ===
DATABASE_URL=postgres://argus:argus_dev@localhost:5432/argus_dev?sslmode=disable
DATABASE_MAX_CONNS=50
DATABASE_MAX_IDLE_CONNS=10
# DATABASE_READ_REPLICA_URL=

# === Redis ===
REDIS_URL=redis://localhost:6379/0
REDIS_MAX_CONNS=100

# === NATS ===
NATS_URL=nats://localhost:4222
NATS_CLUSTER_ID=argus-cluster

# === Auth ===
JWT_SECRET=change-me-to-a-long-random-string-at-least-32-chars
JWT_EXPIRY=15m
JWT_REFRESH_EXPIRY=168h
BCRYPT_COST=12
ENCRYPTION_KEY=  # 32-byte hex key for AES-256-GCM (empty = no encryption in dev)

# === AAA ===
RADIUS_AUTH_PORT=1812
RADIUS_ACCT_PORT=1813
RADIUS_SECRET=testing123
DIAMETER_PORT=3868
DIAMETER_ORIGIN_HOST=argus.local
DIAMETER_ORIGIN_REALM=local
SBA_PORT=8443
SBA_ENABLED=false
SBA_ENABLE_MTLS=false
# SBA_NRF_URL=https://nrf.5gc.example.com
# SBA_NF_INSTANCE_ID=argus-sba-01
# SBA_NRF_HEARTBEAT_SEC=30

# === Diameter TLS (optional) ===
# DIAMETER_TLS_ENABLED=true
# DIAMETER_TLS_CERT_PATH=/etc/argus/diameter-server.pem
# DIAMETER_TLS_KEY_PATH=/etc/argus/diameter-server-key.pem
# DIAMETER_TLS_CA_PATH=/etc/argus/diameter-ca.pem

# === Rate Limiting ===
RATE_LIMIT_DEFAULT_PER_MINUTE=1000
RATE_LIMIT_DEFAULT_PER_HOUR=30000
RATE_LIMIT_ALGORITHM=sliding_window

# === Background Jobs ===
JOB_MAX_CONCURRENT_PER_TENANT=5
JOB_TIMEOUT_MINUTES=30
JOB_TIMEOUT_CHECK_INTERVAL=5m
JOB_LOCK_TTL=60s
JOB_LOCK_RENEW_INTERVAL=30s
CRON_ENABLED=true
CRON_PURGE_SWEEP=@daily
CRON_IP_RECLAIM=@hourly
CRON_SLA_REPORT=@daily

# === eSIM SM-DP+ (optional) ===
# ESIM_SMDP_PROVIDER=generic
# ESIM_SMDP_BASE_URL=https://smdp.example.com
# ESIM_SMDP_API_KEY=
# ESIM_SMDP_CLIENT_CERT_PATH=/certs/smdp-client.pem
# ESIM_SMDP_CLIENT_KEY_PATH=/certs/smdp-client-key.pem

# === SMS Gateway (optional) ===
# SMS_PROVIDER=twilio
# SMS_ACCOUNT_ID=
# SMS_AUTH_TOKEN=
# SMS_FROM_NUMBER=+15005550006
# SMS_STATUS_CALLBACK_URL=

# === Notifications (optional) ===
# SMTP_HOST=smtp.gmail.com
# SMTP_PORT=587
# SMTP_USER=
# SMTP_PASSWORD=
# SMTP_FROM=noreply@argus.io
# TELEGRAM_BOT_TOKEN=
# TELEGRAM_DEFAULT_CHAT_ID=

# === Storage (optional) ===
# S3_ENDPOINT=http://localhost:9000
# S3_ACCESS_KEY=minioadmin
# S3_SECRET_KEY=minioadmin
# S3_BUCKET=argus-storage
# S3_REGION=us-east-1
# S3_PATH_STYLE=true

# === TLS (optional, required for production) ===
# TLS_CERT_PATH=/certs/argus.pem
# TLS_KEY_PATH=/certs/argus-key.pem
# RADSEC_CERT_PATH=/certs/radsec.pem
# RADSEC_KEY_PATH=/certs/radsec-key.pem

# === Tenant Defaults ===
DEFAULT_MAX_SIMS=1000000
DEFAULT_MAX_APNS=100
DEFAULT_MAX_USERS=50
DEFAULT_PURGE_RETENTION_DAYS=90

# === Development ===
DEV_CORS_ALLOW_ALL=true

# === Observability (OpenTelemetry) ===
# OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4317
OTEL_SAMPLER_RATIO=1.0
OTEL_SERVICE_NAME=argus
OTEL_SERVICE_VERSION=dev
OTEL_DEPLOYMENT_ENVIRONMENT=development
OTEL_BSP_EXPORT_TIMEOUT_SEC=5

# === Observability (Prometheus) ===
METRICS_ENABLED=true
METRICS_NAMESPACE=argus
METRICS_TENANT_LABEL_ENABLED=true

# === Kill Switches (FIX-245) — all unset by default (permit/mutate) ===
# KILLSWITCH_RADIUS_AUTH=on
# KILLSWITCH_SESSION_CREATE=on
# KILLSWITCH_BULK_OPERATIONS=on
# KILLSWITCH_EXTERNAL_NOTIFICATIONS=on
# KILLSWITCH_READ_ONLY_MODE=on
```
