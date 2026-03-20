# Services Index — Argus

> All services compile into a single Go binary (modular monolith).
> Each service is a Go package under `internal/`.

| ID | Service | Package | Ports | Responsibility |
|----|---------|---------|-------|---------------|
| SVC-01 | API Gateway | `internal/gateway` | :8080 (HTTP) | Request routing, auth middleware, rate limiting, RBAC, tenant scoping, CORS |
| SVC-02 | WebSocket Server | `internal/ws` | :8081 (WS) | Event push, live session updates, alert feed, job progress |
| SVC-03 | Core API | `internal/api` | — (internal) | SIM CRUD, APN CRUD, Tenant/User mgmt, eSIM orchestration, bulk ops, IP mgmt |
| SVC-04 | AAA Engine | `internal/aaa` | :1812/:1813 (RADIUS), :3868 (Diameter), :8443 (5G SBA) | Authentication, accounting, CoA/DM, session management, EAP-SIM/AKA |
| SVC-05 | Policy Engine | `internal/policy` | — (internal) | Rule evaluation, DSL parser, staged rollout, dry-run simulation, CoA trigger |
| SVC-06 | Operator Router | `internal/operator` | — (internal) | IMSI routing, SoR engine, circuit breaker, failover, health check, Diameter bridge, operator adapters |
| SVC-07 | Analytics Engine | `internal/analytics` | — (internal) | CDR processing, real-time metrics, anomaly detection, cost optimization, observability dashboards |
| SVC-08 | Notification Service | `internal/notification` | — (internal) | In-app, email (SMTP), Telegram, webhook delivery, SMS gateway, notification preferences |
| SVC-09 | Job Runner | `internal/job` | — (internal) | Bulk ops execution, scheduled tasks (cron), OTA commands, IP reclaim, purge, distributed lock |
| SVC-10 | Audit Service | `internal/audit` | — (internal) | Append-only logging, hash chain, pseudonymization, tamper detection, export |

## Shared Packages

| Package | Purpose |
|---------|---------|
| `internal/model` | Domain models, entity definitions |
| `internal/store` | Database access layer (PostgreSQL + TimescaleDB) |
| `internal/cache` | Redis cache layer |
| `internal/bus` | NATS event bus abstraction |
| `internal/auth` | JWT, 2FA, API key validation |
| `internal/tenant` | Tenant context, middleware, isolation |
| `internal/config` | Configuration loading (env vars) |
| `internal/protocol/radius` | RADIUS protocol implementation (RFC 2865/2866) |
| `internal/protocol/diameter` | Diameter protocol implementation (RFC 6733, Gx, Gy) |
| `internal/aaa/sba` | 5G SBA HTTP/2 proxy (AUSF/UDM, EAP-AKA' proxy, NRF placeholder) |
| `pkg/dsl` | Policy DSL parser and evaluator |
