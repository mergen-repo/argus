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
| `internal/policy/dsl/` | Policy DSL parser and evaluator |

## Sub-Components & Phase 11 Additions

> Phase 11 (Enterprise Readiness Pack — IMEI Ecosystem + Native Syslog Forwarder) introduces no new top-level SVC IDs. All capability lives as sub-packages of existing services. See [ADR-004](../../adrs/ADR-004-imei-binding-architecture.md).

| Affected SVC | Sub-Component | Package | Responsibility |
|--------------|---------------|---------|----------------|
| SVC-04 (AAA Engine) | IMEI Capture Pipeline | `internal/aaa/imei/` (new) — wired into `internal/protocol/radius`, `internal/protocol/diameter`, `internal/aaa/sba` | Parse 3GPP-IMEISV VSA (RADIUS), Terminal-Information AVP 350 (Diameter S6a), and PEI (5G SBA); normalize to `SessionContext.Device {IMEI, TAC, SoftwareVersion, IMEISV, PEIRaw, CaptureProtocol}`; null-safe + counter-instrumented. STORY-093. |
| SVC-04 (AAA Engine) | Binding Pre-Check | `internal/aaa/binding/` (new) | Runs BEFORE policy DSL when `sim.binding_mode IS NOT NULL`; consults `sims.bound_imei`, `sim_imei_allowlist`, `imei_blacklist`, `imei_greylist`, `imei_whitelist`; emits `device.binding_status` + appends to `imei_history`. Hard-rejects in `strict`/`allowlist`/`first-use`/`tac-lock`/`grace-period` modes; `soft` mode records mismatch but proceeds. STORY-096. |
| SVC-05 (Policy Engine) | Device Predicate Evaluator | `internal/policy/dsl/` (extension) | Evaluates `device.*`, `sim.binding_*`, `device.imei_in_pool(...)`, and `tac()` predicates against SessionContext; runtime-only — explicitly excluded from MATCH→SQL whitelist. STORY-094. |
| SVC-03 (Core API) | IMEI Pool Service | `internal/api/imei_pool/` (new) | CRUD over TBL-56/57/58, bulk CSV import (delegates to SVC-09 jobs), IMEI Lookup (cross-references pools + bound SIMs). API-331..335. STORY-095. |
| SVC-03 (Core API) | Device Binding Handlers | `internal/api/sim/binding.go` (new) | Per-SIM device-binding GET/PATCH, re-pair, IMEI history. API-327..330. STORY-094, STORY-097. |
| SVC-09 (Job Runner) | IMEI Bulk Import / Bulk SIM-Binding | `internal/job/imei_pool_import.go`, `internal/job/sim_binding_bulk.go` (new) | Async CSV processors; reuses STORY-013 job orchestration; per-row error reporting. STORY-094, STORY-095. |
| SVC-08 (Notification Service) | Device Event Subscribers | `internal/notification/` (extension) | New EventTypes: `imei.captured`, `imei.changed`, `imei.mismatch_detected`, `imei.grace_period_expired`, `imei.pool.exhausted_warning`, `imei.blacklist_hit`, `device.binding_failed`, `device.binding_locked`, `device.binding_re_paired`, `device.binding_grace_change`, `device.binding_grace_expiring`. STORY-093, STORY-096, STORY-097. |
| SVC-08 (Notification Service) | Native Syslog Forwarder | `internal/notification/syslog/` (new) | RFC 3164 + RFC 5424 emitter; UDP/TCP/TLS transports; subscribes to canonical `bus.Envelope` stream; configurable per-tenant destinations + filter rules (event categories, min severity); resilient delivery (per-destination retry + last-error state). API-337/338. STORY-098. |
| SVC-10 (Audit Service) | Device-Binding Audit Constants | `internal/audit/` (extension) | New audit actions: SIM binding state — `sim.imei_captured`, `sim.binding_mode_changed`, `sim.binding_verified`, `sim.binding_mismatch`, `sim.binding_first_use_locked`, `sim.binding_soft_mismatch`, `sim.binding_blacklist_hit`, `sim.imei_repaired`, `sim.imei_unbound`. IMEI pool — `imei_pool.entry_added`, `imei_pool.entry_removed`, `imei_pool.bulk_imported`. Log forwarding — `log_forwarding.destination_added`, `log_forwarding.destination_updated`, `log_forwarding.destination_disabled`, `log_forwarding.destination_removed`. STORY-093..098. |

### Out of Scope (v1)

EIR (Equipment Identity Register) integration via Diameter S13 (4G/EPC) or 5G N17 is OUT OF SCOPE for Phase 11 v1 per ADR-004. No EIR client packages, no S13 stub handlers, no N17 SBA mock, no AVP scaffolding. All enforcement is local; integration with operator EIRs is a future-track item.
