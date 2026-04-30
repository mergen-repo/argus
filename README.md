# Argus

APN & Subscriber Intelligence Platform — manage 10M+ IoT/M2M SIM cards across multiple mobile operators from a single portal.

## Tech Stack

- **Backend:** Go 1.22+ (modular monolith)
- **Frontend:** React 19 + Vite + Tailwind CSS + shadcn/ui
- **Database:** PostgreSQL 16 + TimescaleDB
- **Cache:** Redis 7
- **Message Bus:** NATS JetStream
- **Protocols:** RADIUS (RFC 2865/2866), Diameter (RFC 6733), 5G SBA (HTTP/2)
- **Deployment:** Docker Compose / Kubernetes + Helm

## Prerequisites

- Docker & Docker Compose v2
- Go 1.22+ (for local development)
- Node.js 20+ (for frontend development)
- Make

## Quick Start

```bash
git clone <repo-url>
cd argus
cp .env.example .env
make build
make up
make db-migrate
make db-seed
make help
```

Open http://localhost:8084 in your browser.

## Project Structure

```
cmd/argus/          → Entry point
internal/           → Go packages (gateway, aaa, api, policy, operator, analytics, etc.)
web/                → React SPA (Vite + Tailwind)
migrations/         → SQL migrations (golang-migrate)
deploy/             → Docker Compose, Nginx config, Dockerfile
docs/               → Architecture, product, planning docs
```

## Services (single binary)

| Service | Port | Protocol |
|---------|------|----------|
| API Gateway | :8080 | HTTP/REST |
| WebSocket | :8081 | WS |
| RADIUS Auth | :1812 | UDP |
| RADIUS Acct | :1813 | UDP |
| Diameter | :3868 | TCP |
| 5G SBA | :8443 | HTTP/2 |

## Development

```bash
make infra-up       # Start PG, Redis, NATS
make web-dev        # Start React dev server
go run ./cmd/argus  # Start Go backend
make test           # Run tests
```

## Deployment

```bash
make deploy-dev     # Build + start all containers
make deploy-prod    # Backup DB + build + start (with confirmation)
```

## Observability

Argus ships with full OpenTelemetry tracing (HTTP → DB → NATS) and Prometheus metrics out of the box.

- **Tracing:** spans exported via OTLP gRPC to any compatible backend (Jaeger, Tempo, etc.)
- **Metrics:** `/metrics` endpoint in Prometheus text format; six pre-built Grafana dashboards covering HTTP, DB, AAA, jobs, operator health, and system resources
- **Alerts:** nine Prometheus alert rules (latency, error-rate, circuit-breaker, operator down, queue depth) at `infra/prometheus/alerts.yml`

**Start the full observability stack (Prometheus + Grafana + Jaeger):**

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.obs.yml up
```

Grafana: http://localhost:3000 (admin/admin) — dashboards auto-provisioned from `infra/grafana/dashboards/`

See [`docs/architecture/CONFIG.md`](docs/architecture/CONFIG.md#observability) for all `OTEL_*` and `METRICS_*` environment variables.

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Product Definition](docs/PRODUCT.md)
- [Scope](docs/SCOPE.md)
- [API Index](docs/architecture/api/_index.md)
- [Database Schema](docs/architecture/db/_index.md)
