# CLAUDE.md — Argus

## Project Overview

Argus is an APN & Subscriber Intelligence Platform built as a Go modular monolith with a React SPA frontend. It manages 10M+ IoT/M2M SIM cards across multiple mobile operators with built-in AAA (RADIUS/Diameter/5G), policy engine, analytics, and multi-operator orchestration.


## Development
- Run `/amil` to start or continue any development work — it manages planning, implementation, quality gates, and deployment.
## Tech Stack

- Go 1.22+ backend (single binary, multiple protocol listeners)
- React 19 + Vite + Tailwind CSS + shadcn/ui frontend
- PostgreSQL 16 + TimescaleDB (OLTP + time-series)
- Redis 7 (cache, sessions, rate limiting)
- NATS JetStream (events, job queue)
- Docker Compose deployment

## Quick Commands

- `make help` — Show all available commands
- `make up` — Start all Docker services
- `make down` — Stop all services
- `make infra-up` — Start only PG, Redis, NATS
- `make test` — Run Go tests
- `make db-migrate` — Run database migrations
- `make db-seed` — Seed database
- `make web-dev` — Start React dev server
- `make web-build` — Build React for production

## Docker Services

| Service | URL/Port | Purpose |
|---------|----------|---------|
| Nginx | http://localhost:8084 | Reverse proxy + SPA |
| Argus | :8080 (HTTP), :8081 (WS), :1812/:1813 (RADIUS), :3868 (Diameter), :8443 (5G SBA) | Go monolith |
| PostgreSQL | localhost:5432 | Database |
| Redis | localhost:6379 | Cache |
| NATS | localhost:4222, :8222 (monitor) | Events |
| Operator Sim | :9595 (API), :9596 (health+metrics) | Passive operator SoR HTTP simulator (argus-operator-sim) |

## Admin Access

- URL: http://localhost:8084/login
- Email: admin@argus.io
- Password: admin

## Project Structure

```
cmd/argus/       → Entry point (main.go)
internal/        → All Go packages
  gateway/       → SVC-01: HTTP API gateway
  ws/            → SVC-02: WebSocket server
  api/           → SVC-03: Core CRUD handlers
  aaa/           → SVC-04: RADIUS/Diameter/5G engine
  policy/        → SVC-05: Policy DSL engine
  operator/      → SVC-06: Multi-operator routing
  analytics/     → SVC-07: CDR, metrics, anomaly
  notification/  → SVC-08: Email, Telegram, webhook
  job/           → SVC-09: Background jobs
  audit/         → SVC-10: Tamper-proof logging
  model/         → Domain models
  store/         → PostgreSQL data access
  cache/         → Redis layer
  bus/           → NATS event bus
  auth/          → JWT, 2FA, API keys
  tenant/        → Multi-tenant middleware
  config/        → Env var config
web/             → React SPA
  src/components → Atomic design (atoms/molecules/organisms/templates/pages)
migrations/      → SQL up/down migrations
deploy/          → Docker Compose, Nginx, Dockerfile
docs/            → All documentation
```

## Conventions

- API responses: Standard envelope `{ status, data, meta?, error? }`
- Components: Atomic design (atoms → molecules → organisms → templates → pages)
- Migrations: `YYYYMMDDHHMMSS_description.up.sql` / `.down.sql` via golang-migrate
- Naming: Go=camelCase, React=PascalCase, routes=kebab-case, DB=snake_case
- All DB queries scoped by tenant_id (enforced in store layer)
- Cursor-based pagination (not offset) for all list endpoints
- Every state-changing operation creates an audit log entry

## Architecture References

- Services: SVC-01 to SVC-10 (see docs/architecture/services/_index.md)
- APIs: API-001 to API-182 (see docs/architecture/api/_index.md)
- Tables: TBL-01 to TBL-24 (see docs/architecture/db/_index.md)
- ADRs: ADR-001 to ADR-003 (see docs/adrs/)

## Architecture Docs

- `docs/ARCHITECTURE.md` — System design summary
- `docs/PRODUCT.md` — Features, business rules, workflows
- `docs/SCOPE.md` — Project boundaries
- `docs/GLOSSARY.md` — Domain terminology
- `docs/FUTURE.md` — Future roadmap
- `docs/FRONTEND.md` — Design system tokens & visual patterns
- `docs/SCREENS.md` — Screen index (26 screens)
- `docs/ROUTEMAP.md` — Project progress tracking
- `docs/stories/` — Individual story specs (55 stories, 9 phases)
- `docs/architecture/MIDDLEWARE.md` — Chi middleware chain
- `docs/architecture/ERROR_CODES.md` — Error code catalog
- `docs/architecture/DSL_GRAMMAR.md` — Policy DSL grammar (EBNF)
- `docs/architecture/PROTOCOLS.md` — RADIUS/Diameter/5G protocol details
- `docs/architecture/ALGORITHMS.md` — Key algorithms (IP, hash chain, rate limit, anomaly, cost)
- `docs/architecture/WEBSOCKET_EVENTS.md` — WebSocket event schemas + canonical bus.Envelope wire format (FIX-212)
- `docs/architecture/TESTING.md` — Test strategy & frameworks
- `docs/architecture/CONFIG.md` — Environment variable reference
- `docs/architecture/DEPLOYMENT.md` — Container restart policy + recovery runbook

## Active Session

- Mode: AUTOPILOT (UI Review Remediation — full track, all 10 waves)
- Phase: UI Review Remediation [IN PROGRESS] — 2026-04-19
- Story: FIX-226 (Simulator Coverage + Volume Realism)
- Step: Review
- Plan: `docs/reviews/ui-review-remediation-plan.md` (44 FIX stories, FIX-201..FIX-248)
- Findings: `docs/reviews/ui-review-2026-04-19.md` (107 aktif finding + Phase 2 additions)
- ROUTEMAP: `docs/ROUTEMAP.md` "UI Review Remediation [IN PROGRESS]" track (10 waves)
- User directive 2026-04-20: Full AUTOPILOT, dikkatli geliştirme, doğru spec, hatasız, canlıya hazırlık
- Modal decision: Option C (Dialog compact confirm + SlidePanel rich form)
- AUTOPILOT scope: Runs until ESCALATED / FAILED / end-of-track Phase Gate PASS
- Key architectural threads:
  - **Data integrity foundation** (FIX-206): 200 orphan SIM + FK constraints + seed fix → unblocks FIX-202/207/208
  - **Alert architecture** (FIX-209/210/211): unified alerts table + dedup + taxonomy → unblocks FIX-213/215/229
  - **Event envelope** (FIX-212): unified schema + name resolution + missing publishers → unblocks FIX-213/219
  - **Cross-tab aggregation** (FIX-208): DONE — single source of truth for usage/cost/sessions math via aggregates facade
  - **Seed discipline** (FIX-206): `make db-seed` must stay clean after FK migration — never defer
