# STORY-001 Implementation Plan

## Summary
Set up the Go module, directory structure per ARCHITECTURE.md, Docker Compose with all 5 containers, config loading, database/Redis/NATS connections, health endpoint, and Makefile targets.

## Tasks

### Task 1: Go Module Init & Directory Structure
- `go mod init github.com/btopcu/argus`
- Create missing directories: `cmd/argus/`, `internal/{gateway,config,cache,bus,model,auth,tenant,audit,analytics,notification,policy,ws}/`, `pkg/dsl/`
- Existing dirs to keep: `internal/{aaa,api,job,operator,store}/`

### Task 2: Configuration (internal/config/config.go)
- envconfig struct with all vars from CONFIG.md
- `Load()` function using `kelseyhightower/envconfig`
- Validation for required fields (DATABASE_URL, REDIS_URL, NATS_URL, JWT_SECRET)

### Task 3: Database Connection (internal/store/postgres.go)
- pgxpool connection using DATABASE_URL
- `NewPostgres()` constructor with pool config
- `HealthCheck()` method (ping)
- `Close()` method

### Task 4: Redis Connection (internal/cache/redis.go)
- go-redis v9 client using REDIS_URL
- `NewRedis()` constructor
- `HealthCheck()` method (ping)
- `Close()` method

### Task 5: NATS Connection (internal/bus/nats.go)
- NATS client + JetStream context
- `NewNATS()` constructor using NATS_URL
- `HealthCheck()` method (connection status)
- `Close()` method

### Task 6: HTTP Router (internal/gateway/router.go)
- chi v5 router
- Recovery, RequestID, RealIP, Logger middleware (chi built-ins for now)
- Mount /api/health endpoint

### Task 7: Health Handler (internal/gateway/health.go)
- GET /api/health
- Checks DB, Redis, NATS health
- Returns standard API envelope: `{status: "success", data: {db, redis, nats, uptime}}`
- Returns 503 with details when any service is down

### Task 8: Entry Point (cmd/argus/main.go)
- Load config via envconfig
- Connect DB (pgxpool), Redis, NATS
- Build chi router with health handler
- Start HTTP server on configurable port (default :8080)
- Graceful shutdown on SIGINT/SIGTERM

### Task 9: Docker & Deploy Verification
- Update Dockerfile Go version from 1.22 to 1.23
- Verify docker-compose.yml (already has 5 containers)
- SSL certs already exist in deploy/nginx/ssl/
- Update .env.example with Docker-internal hostnames

### Task 10: Initial Migration
- `migrations/20260320000001_init_extensions.up.sql` (TimescaleDB + uuid-ossp)
- `migrations/20260320000001_init_extensions.down.sql`

## Dependencies (go.mod)
- github.com/go-chi/chi/v5
- github.com/jackc/pgx/v5
- github.com/redis/go-redis/v9
- github.com/nats-io/nats.go
- github.com/rs/zerolog
- github.com/kelseyhightower/envconfig
- github.com/golang-jwt/jwt/v5
- github.com/pquerna/otp
- github.com/google/uuid

## Estimated Steps: 10
## Risk: Low (first story, no dependencies)
