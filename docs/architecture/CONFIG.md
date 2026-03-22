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

---

## Database (PostgreSQL + TimescaleDB)

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `DATABASE_URL` | string | — | **Yes** | PostgreSQL connection string. Format: `postgres://user:password@host:port/dbname?sslmode=disable`. |
| `DATABASE_MAX_CONNS` | int | `50` | No | Maximum open connections in the pool. Size based on: expected concurrent requests + background jobs + AAA handlers. |
| `DATABASE_MAX_IDLE_CONNS` | int | `10` | No | Maximum idle connections kept in pool. |
| `DATABASE_CONN_MAX_LIFETIME` | duration | `30m` | No | Maximum time a connection can be reused. Prevents stale connections after PG failover. |
| `DATABASE_READ_REPLICA_URL` | string | — | No | Read replica connection string. If set, all read-only queries (list, get, analytics) use this connection. Write queries always go to primary. |

### Connection String Examples

```bash
# Development (local Docker)
DATABASE_URL=postgres://argus:argus_dev@localhost:5432/argus_dev?sslmode=disable

# Production (with SSL)
DATABASE_URL=postgres://argus:SECURE_PASSWORD@db.example.com:5432/argus?sslmode=require

# With read replica
DATABASE_READ_REPLICA_URL=postgres://argus:SECURE_PASSWORD@db-replica.example.com:5432/argus?sslmode=require
```

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
| `argus.cache.invalidate` | Cache invalidation broadcast | All instances |

---

## Authentication & Security

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `JWT_SECRET` | string | — | **Yes** | HMAC secret for signing JWT tokens. Minimum 32 characters. **Keep secret.** |
| `JWT_EXPIRY` | duration | `15m` | No | Access token expiration. Short-lived for security. |
| `JWT_REFRESH_EXPIRY` | duration | `168h` | No | Refresh token expiration (168h = 7 days). Stored in httpOnly cookie and database (TBL-03). |
| `JWT_ISSUER` | string | `argus` | No | JWT `iss` claim value. |
| `BCRYPT_COST` | int | `12` | No | bcrypt cost factor for password hashing. Range 10-14. Higher = slower but more secure. 12 is ~250ms on modern hardware. |
| `LOGIN_MAX_ATTEMPTS` | int | `5` | No | Consecutive failed login attempts before account lockout. |
| `LOGIN_LOCKOUT_DURATION` | duration | `15m` | No | Account lockout duration after max failed attempts. |
| `ENCRYPTION_KEY` | string | — | No | 32-byte hex-encoded key for AES-256-GCM encryption of sensitive fields (adapter_config, sm_dp_plus_config). Empty = no encryption (dev mode passthrough). **Keep secret.** |

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
| `DIAMETER_VENDOR_ID` | int | `99999` | No | Argus vendor ID (for custom AVPs). 3GPP AVPs use 10415. |
| `DIAMETER_WATCHDOG_INTERVAL` | duration | `30s` | No | DWR (Device-Watchdog-Request) send interval. |
| `SBA_PORT` | int | `8443` | No | 5G SBA HTTPS/HTTP2 server port. |
| `SBA_ENABLED` | bool | `false` | No | Enable 5G SBA proxy server. |
| `SBA_ENABLE_MTLS` | bool | `false` | No | Enable mutual TLS (mTLS) for 5G SBA server. When true, requires client certificates for NF-to-NF communication. |

---

## Rate Limiting

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `RATE_LIMIT_DEFAULT_PER_MINUTE` | int | `1000` | No | Default requests per minute per tenant/user. Applied when no specific limit is configured. |
| `RATE_LIMIT_DEFAULT_PER_HOUR` | int | `30000` | No | Default requests per hour per tenant/user. |
| `RATE_LIMIT_ALGORITHM` | string | `sliding_window` | No | Algorithm: `sliding_window` (default, most accurate) or `fixed_window` (simpler, slightly less accurate). |
| `RATE_LIMIT_AUTH_PER_MINUTE` | int | `10` | No | Login attempts per minute per IP (brute force protection). |
| `RATE_LIMIT_ENABLED` | bool | `true` | No | Master switch to disable rate limiting (useful in development). |

---

## Background Jobs (SVC-09)

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `JOB_MAX_CONCURRENT_PER_TENANT` | int | `5` | No | Maximum simultaneously running background jobs per tenant. Prevents a single tenant from monopolizing job runner capacity. |
| `JOB_TIMEOUT_MINUTES` | int | `30` | No | Minutes before a stale running job (no progress) is automatically marked as failed by the timeout detector. |
| `JOB_TIMEOUT_CHECK_INTERVAL` | duration | `5m` | No | How often the timeout detector sweeps for stale running jobs. |
| `JOB_LOCK_TTL` | duration | `60s` | No | Redis distributed lock TTL for job-level and SIM-level locks (SETNX). Auto-renewed during execution. |
| `JOB_LOCK_RENEW_INTERVAL` | duration | `30s` | No | How often the lock renewal goroutine extends the lock TTL. Must be less than `JOB_LOCK_TTL`. |
| `CRON_ENABLED` | bool | `true` | No | Enable/disable the cron scheduler. Set to `false` in test environments or when running multiple instances without Redis dedup. |
| `CRON_PURGE_SWEEP` | string | `@daily` | No | Cron schedule for the purge sweep job (KVKK/GDPR auto-purge of terminated SIMs). Supports `@daily`, `@hourly`, `@weekly`, `@monthly`, or 5-field cron expressions. |
| `CRON_IP_RECLAIM` | string | `@hourly` | No | Cron schedule for the IP reclaim job (returns terminated SIM IPs to pool after grace period). |
| `CRON_SLA_REPORT` | string | `@daily` | No | Cron schedule for the SLA report generation job. |

### Redis Key Patterns (Job System)

| Pattern | TTL | Purpose |
|---------|-----|---------|
| `argus:lock:job:{id}` | `JOB_LOCK_TTL` | Distributed lock per job (prevents concurrent processing) |
| `argus:lock:sim:{id}` | `JOB_LOCK_TTL` | Distributed lock per SIM (prevents concurrent bulk ops on same SIM) |
| `argus:cron:{name}:{tick}` | ~schedule interval | Cron dedup key (SETNX ensures single-instance execution per tick) |

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

---

## Development Overrides

These variables are only meaningful in development mode (`APP_ENV=development`):

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `DEV_SEED_DATA` | bool | `true` | Auto-run seed migrations on startup. |
| `DEV_MOCK_OPERATOR` | bool | `true` | Register mock operator adapter. |
| `DEV_CORS_ALLOW_ALL` | bool | `true` | Allow all CORS origins (for localhost dev server). |
| `DEV_DISABLE_2FA` | bool | `true` | Skip 2FA verification in development. |
| `DEV_LOG_SQL` | bool | `false` | Log all SQL queries to stdout. |

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
DEV_SEED_DATA=true
DEV_MOCK_OPERATOR=true
DEV_CORS_ALLOW_ALL=true
DEV_DISABLE_2FA=true
DEV_LOG_SQL=false
```
