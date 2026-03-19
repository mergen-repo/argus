# STORY-001: Project Scaffold & Docker Infrastructure

## User Story
As a developer, I want the Go project structure, Docker Compose, and build pipeline ready, so that all subsequent stories have a working foundation.

## Description
Set up the Go module, directory structure per ARCHITECTURE.md, Docker Compose with all 5 containers (Nginx, Argus, PostgreSQL+TimescaleDB, Redis, NATS), Makefile targets, and basic health check endpoint.

## Architecture Reference
- Services: SVC-01 (API Gateway)
- API Endpoints: API-180 (GET /api/health)
- Docker Containers: CTN-01 (Nginx), CTN-02 (Argus), CTN-03 (PG+TS), CTN-04 (Redis), CTN-05 (NATS)
- Source: docs/ARCHITECTURE.md (Project Structure, Docker Architecture)

## Screen Reference
- None (infrastructure only)

## Acceptance Criteria
- [ ] `go mod init` with Go 1.22+
- [ ] Directory structure matches ARCHITECTURE.md (cmd/, internal/, web/, migrations/, deploy/)
- [ ] `docker compose up` starts all 5 containers successfully
- [ ] PostgreSQL with TimescaleDB extension is accessible on :5432
- [ ] Redis is accessible on :6379
- [ ] NATS with JetStream is accessible on :4222
- [ ] `GET /api/health` returns `{ status: "success", data: { db: "ok", redis: "ok", nats: "ok" } }`
- [ ] Nginx reverse proxy routes /api/* to Go app, / to static placeholder
- [ ] `make up`, `make down`, `make status`, `make logs` work
- [ ] `.env.example` loaded via envconfig

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-180 | GET | /api/health | — | `{ db: string, redis: string, nats: string, uptime: string }` | None | 200, 503 |

## Database Changes
- TimescaleDB extension enabled: `CREATE EXTENSION IF NOT EXISTS timescaledb`
- Migration framework (golang-migrate) configured
- Migration: `20260318000001_init_extensions.up.sql`

## Dependencies
- Blocked by: None (first story)
- Blocks: All other stories

## Test Scenarios
- [ ] All containers start and pass health checks within 30s
- [ ] Health endpoint returns 200 when all services are up
- [ ] Health endpoint returns 503 with detail when PG is down
- [ ] Health endpoint returns 503 with detail when Redis is down
- [ ] Makefile targets execute without errors

## Effort Estimate
- Size: M
- Complexity: Medium
