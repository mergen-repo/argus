# STORY-001 Deliverable: Project Scaffold & Docker Infrastructure

## Summary
Set up the Go project foundation: module init, directory structure, Docker Compose with 5 containers (Nginx, Argus, PostgreSQL+TimescaleDB, Redis, NATS), config loading via envconfig, database/Redis/NATS connection packages, health check endpoint, Makefile targets, and initial migration.

## Files Created
- `cmd/argus/main.go` -- Application entry point with graceful shutdown
- `migrations/20260320000001_init_extensions.up.sql` -- TimescaleDB + uuid-ossp extensions
- `migrations/20260320000001_init_extensions.down.sql` -- Drop extensions
- `internal/apierr/apierr.go` -- Standard API response envelope helpers
- `internal/audit/audit.go` -- Audit service stub
- `internal/store/errors.go` -- Store error types and tenant context helper
- `internal/store/stubs.go` -- Store type stubs for later stories
- `internal/aaa/session/session.go` -- Session types and manager stub
- `internal/operator/adapter/types.go` -- Operator adapter interface and types
- `internal/operator/circuit_breaker.go` -- Circuit breaker implementation

## Files Modified
- `deploy/Dockerfile` -- Updated Go builder image from 1.22 to 1.25
- `go.mod` -- Dependencies resolved (chi, pgx, go-redis, nats, zerolog, envconfig)
- `go.sum` -- Generated dependency checksums

## Existing Files (from previous run, verified correct)
- `internal/config/config.go` -- Config struct with all env vars
- `internal/store/postgres.go` -- PostgreSQL pgxpool connection
- `internal/cache/redis.go` -- Redis go-redis/v9 connection
- `internal/bus/nats.go` -- NATS + JetStream connection
- `internal/gateway/router.go` -- Chi router with middleware
- `internal/gateway/health.go` -- Health check handler with DB/Redis/NATS checks
- `Makefile` -- All build/deploy/db/test targets
- `.env.example` -- Environment variable reference
- `deploy/docker-compose.yml` -- 5-container Docker Compose
- `deploy/nginx/nginx.conf` -- Reverse proxy + SPA config
- `deploy/nginx/ssl/` -- Self-signed SSL certs

## Architecture References
- SVC-01 (API Gateway): `internal/gateway/`
- API-180 (GET /api/health): `internal/gateway/health.go`
- CTN-01..05: `deploy/docker-compose.yml`

## API Endpoint
| Method | Path | Status | Response |
|--------|------|--------|----------|
| GET | /api/health | 200/503 | `{status, data: {db, redis, nats, uptime}}` |

## Test Scenarios
- All 5 Docker containers configured with health checks
- Health endpoint returns 200 when all services healthy
- Health endpoint returns 503 with detail when any service down
- Makefile targets: up, down, status, logs, infra-up, infra-down, test, build
