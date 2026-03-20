# Decisions Log — Argus

> Track all architectural, product, and technical decisions made during planning and development.

---

| # | Date | Decision | Rationale | Impact | Status |
|---|------|----------|-----------|--------|--------|
| D-001 | 2026-03-18 | Project codename: Argus | Multi-operator APN/RADIUS management platform | — | ACCEPTED |
| D-002 | 2026-03-18 | Custom Go AAA core (not FreeRADIUS) | Diameter requirement, SQL bottleneck, policy engine need, cloud-native | Architecture | ACCEPTED |
| D-003 | 2026-03-18 | Primary target: Enterprise (B2B) + SaaS model | Enterprise IoT/M2M fleet mgmt as primary; managed service as secondary | Multi-tenant architecture required | ACCEPTED |
| D-004 | 2026-03-18 | Sector-agnostic platform, Energy/Utilities beachhead | High SIM density, long lifecycle, compliance needs — ideal anchor | No vertical-specific features in core | ACCEPTED |
| D-005 | 2026-03-18 | Operator-agnostic adapter pattern | No existing operator relationships — must develop independently | Mock/simulator layer for dev, pluggable adapters per operator | ACCEPTED |
| D-006 | 2026-03-18 | Fullstack monorepo, docker-compose delivery | Simple on-prem deployment for enterprise, K8s for SaaS | Single repo: web portal + AAA engine | ACCEPTED |
| D-007 | 2026-03-18 | Full 5-layer MVP — no phased feature delivery | Must compete globally (Enea, Alepo) and surpass TR alternatives | All layers ship together: AAA + SIM/APN + Multi-Op + Policy + Analytics | ACCEPTED |
| D-008 | 2026-03-18 | Solo dev + Claude Code (Amil) | All development AI-assisted | Small stories, auto-testing, AUTOPILOT mode, convention-heavy stack | ACCEPTED |
| D-009 | 2026-03-18 | Go backend + React/Vite frontend | Go: performance-critical AAA; React/Vite: SPA admin portal, no SSR needed, largest ecosystem | Single binary serves static + API | ACCEPTED |
| D-010 | 2026-03-18 | Full compliance suite in v1 | Enterprise procurement requires certs; global ambition needs GDPR | BTK + KVKK + GDPR + ISO 27001 audit + RadSec + RBAC | ACCEPTED |
| D-011 | 2026-03-18 | Data layer: PG + TimescaleDB + Redis + NATS | Single PG engine for OLTP+analytics, NATS lightweight vs Kafka | Reduced ops complexity for solo dev | ACCEPTED |
| D-012 | 2026-03-18 | eSIM: Inventory + SM-DP+ API integration (A+B) | Own SM-DP+ unrealistic (cert cost/complexity), BTK requires local operator | Operator provisions, Argus manages | ACCEPTED |
| D-013 | 2026-03-18 | eSIM as first-class citizen, not secondary feature | Core differentiator: unified SIM view, cross-op switch, bulk provision, policy bind | Potential operator white-label/resell channel | ACCEPTED |
| D-014 | 2026-03-18 | Dual deployment: on-prem + cloud, same artifact | Enterprise needs on-prem, SaaS for scale | No cloud-specific deps, S3-compatible, env-var config | ACCEPTED |
| D-015 | 2026-03-18 | Multi-tenant, no white-label in v1 | Reduce frontend complexity, can add later | Argus branding only, tenant data isolation | ACCEPTED |
| D-016 | 2026-03-18 | Scope expansion: 5G SBA + slicing + SoR + OTA + SMS + CDR + events | Competitive parity gap — Enea/Alepo have 5G/slicing, emnify has SoR/OTA/SMS | No competitor covers all layers — that's our edge | ACCEPTED |
| D-017 | 2026-03-18 | Out of scope (low risk): VoWiFi, TACACS+, geo-fence, device mgmt | Not IoT/M2M SIM platform concerns, different product categories | Can revisit post-v1 if market demands | ACCEPTED |

## Gap Analysis Decisions

| # | Date | Type | Decision | Status |
|---|------|------|----------|--------|
| G-001 | 2026-03-18 | Enterprise Default | Empty States — all list/table screens | APPROVED |
| G-002 | 2026-03-18 | Enterprise Default | Loading & Skeleton — all data fetching | APPROVED |
| G-003 | 2026-03-18 | Enterprise Default | Audit Trail — created/updated by/at on critical entities | APPROVED |
| G-004 | 2026-03-18 | Enterprise Default | Onboarding Wizard — first-use setup | APPROVED |
| G-005 | 2026-03-18 | Enterprise Default | Credential Security — .env, encrypted DB, masked API | APPROVED |
| G-006 | 2026-03-18 | Enterprise Default | Confirm Dialogs — destructive actions | APPROVED |
| G-007 | 2026-03-18 | Enterprise Default | Keyboard Shortcuts — table nav, form submit, modal close | APPROVED |
| G-008 | 2026-03-18 | Enterprise Default | Server Pagination — 50/page default | APPROVED |
| G-009 | 2026-03-18 | Enterprise Default | Filter Debounce — 300ms | APPROVED |
| G-010 | 2026-03-18 | Enterprise Default | Virtual Scrolling — 500+ records | APPROVED |
| G-011 | 2026-03-18 | Enterprise Default | Data Export — CSV on all tables | APPROVED |
| G-012 | 2026-03-18 | Enterprise Default | Health Check — /api/health (DB, Redis, NATS, RADIUS) | APPROVED |
| G-013 | 2026-03-18 | Enterprise Default | DB Migrations — versioned, reversible | APPROVED |
| G-014 | 2026-03-18 | Enterprise Default | Code Splitting — React.lazy + Suspense | APPROVED |
| G-015 | 2026-03-18 | Functional | SIM state machine: ORDERED→ACTIVE (bulk import auto-activate), ACTIVE↔SUSPENDED, ACTIVE→TERMINATED, SUSPENDED→TERMINATED, +STOLEN/LOST as separate states, TERMINATED→PURGED (configurable retention period for KVKK/GDPR). No TEST state. | APPROVED |
| G-016 | 2026-03-18 | Functional | APN deletion rules: hard block if active SIMs attached, must migrate/deactivate first. Soft-delete to ARCHIVED state (no new SIM assignment, existing SIMs continue). | APPROVED |
| G-017 | 2026-03-18 | Functional | Operator failover: configurable policy per-operator (reject/fallback-to-next/queue-with-timeout), operator health check heartbeat, SLA violation events → alert + analytics | APPROVED |
| G-018 | 2026-03-18 | Functional | Policy versioning + rollback + dry-run simulation ("affects N SIMs") + staged rollout (canary: 1%→10%→100%) | APPROVED |
| G-019 | 2026-03-18 | Functional | RBAC roles: Super Admin, Tenant Admin, Operator Manager, SIM Manager, Policy Editor, Analyst (read-only), API User (M2M service account) | APPROVED |
| G-020 | 2026-03-18 | Functional | Notification system: channels = in-app + email + webhook + Telegram. Scopes = per-SIM, per-APN, per-operator, system-wide (percentage-based thresholds). User-configurable preferences per channel per event. Notification center (bell, read/unread). | APPROVED |
| G-021 | 2026-03-18 | Functional | Bulk operations: async job queue + progress bar, partial success (apply successful, report failed), retry failed, download error report (CSV), undo/rollback within configurable window | APPROVED |
| G-022 | 2026-03-18 | Functional | Tenant onboarding: Super Admin creates tenant → auto-create Tenant Admin → invite email → onboarding wizard (connect operators → define APNs → first SIM import → assign policy). Resource limits per tenant (max SIM, APN, users). | APPROVED |
| G-023 | 2026-03-18 | Functional | Session management: max concurrent sessions per SIM (configurable, default 1), CoA/DM to kill old session on duplicate, configurable idle/hard timeouts, real-time active session dashboard (per-operator, per-APN), force disconnect (single + bulk) | APPROVED |
| G-024 | 2026-03-18 | Functional | IPAM: pool utilization alerts (80/90/100%), conflict detection + auto-reject, static IP reservation per-SIM, configurable IP reclaim grace period post-terminate, IPv4 + IPv6 dual-stack | APPROVED |
| G-025 | 2026-03-18 | Functional | Deep audit log: who/when/what/before-after diff, policy change impact trace, SIM state transition history, login/logout + failed attempts, API key usage, tamper-proof (append-only, hash chain), search/filter, date-range export for compliance | APPROVED |
| G-026 | 2026-03-18 | Functional | API key management: create per-tenant/per-service, rotation (expire+renew), rate limiting per key, scope restriction (endpoint-level), usage stats on dashboard, instant revoke | APPROVED |
| G-027 | 2026-03-18 | Functional | SIM search & views: combo search (IMSI/MSISDN/ICCID/IP/APN/operator/state), SIM detail page (state history, sessions, usage chart, policy, APN, operator, eSIM profile), SIM comparison (side-by-side debug). GROUP-FIRST UX: primary navigation is by group (policy, APN, operator, state, tenant) not individual SIM. Saved filters/segments ("all active Turkcell SIMs on iot.fleet APN"), bulk actions on segments, group-level dashboards & stats. Individual SIM view is drill-down, not starting point. | APPROVED |
| G-028 | 2026-03-18 | Contradiction | Operator adapters = system-level (Super Admin manages, shared connection). Tenants get access grants to operators. Each tenant defines own APNs but shares operator connection. Tenant isolation: SIM/APN/policy/session data fully isolated, operator connection shared. | RESOLVED |
| G-029 | 2026-03-18 | Contradiction | On-prem vs SaaS same codebase: always multi-tenant code (tenant_id everywhere). On-prem: Super Admin role exists, initial setup creates one tenant. No hiding, no config flags for role differences. Same UX everywhere, on-prem just happens to have one tenant. | RESOLVED |
| G-030 | 2026-03-18 | Contradiction | Concurrent policy versions allowed during staged rollout. Each SIM tracks assigned policy version. Rollout progresses SIM-by-SIM with CoA trigger. Dashboard shows rollout progress. Rollback = mass revert + CoA. | RESOLVED |
| G-031 | 2026-03-18 | Contradiction | KVKK/GDPR purge vs tamper-proof audit: on purge, personal data in audit logs is pseudonymized (IMSI→hash, MSISDN→hash). Hash chain integrity preserved. Mapping table (hash→real) deleted with purge. Compliance + audit integrity both satisfied. | RESOLVED |
| G-032 | 2026-03-18 | Technical | Protocol resilience: RADIUS UDP retry (configurable timeout + max retries), Diameter TCP auto-reconnect + pending queue, dual-stack (Diameter primary, RADIUS fallback), request timeout → dead letter queue + alert, circuit breaker per-operator (N consecutive fails → disable) | APPROVED |
| G-033 | 2026-03-18 | Technical | Built-in observability (no external Grafana/Prometheus dependency): structured JSON logging with correlation ID, distributed tracing (request flow visualization in portal), built-in metrics dashboard (auth/s, latency, error rate, session count), configurable log levels per-component, built-in system health & performance dashboards in Argus portal | APPROVED |
| G-034 | 2026-03-18 | Technical | Background job system: NATS-based persistent queue, job dashboard in portal (running/queued/completed/failed), configurable retry policy per-job-type (max retries, backoff), distributed lock (no concurrent jobs on same SIM), scheduled jobs (cron-like: purge, SLA report, IP reclaim, session timeout sweep) | APPROVED |
| G-035 | 2026-03-18 | Technical | 10M+ DB scale: table partitioning (SIM by operator/state, audit by date), read replicas for analytics, connection pooling (PgBouncer), index strategy (IMSI/MSISDN/ICCID unique, composite tenant_id+state, tenant_id+operator_id+apn_id), archival (90+ day accounting → TimescaleDB compression → S3-compatible cold storage) | APPROVED |
| G-036 | 2026-03-18 | Technical | Security: JWT + refresh token + 2FA (TOTP) for portal, API key + OAuth2 client credentials for API, configurable rate limiting (per-tenant, per-API-key, per-endpoint, Redis-based), input validation/sanitization, configurable CORS per-tenant, TLS everywhere (HTTPS, RadSec, Diameter/TLS) | APPROVED |
| G-037 | 2026-03-18 | UX | Dashboard hierarchy: Tenant Dashboard (health, SIM summary, alert feed, active sessions, top APNs, quick actions). Sidebar: Dashboard, SIM Mgmt (segments→drill-down), APN, Operators, Policies, eSIM Profiles, Sessions (live), Analytics & Reports, Jobs, Notifications, Audit Log, Settings (Users/Roles, API Keys, Operators Config, IP Pools, Notification Prefs, System Config), System Health (Super Admin only) | APPROVED |
| G-038 | 2026-03-18 | UX | Dark mode default + optional light mode toggle. Data-dense tables (compact rows, multi-column, horizontal scroll). Real-time indicators (live dots, color-coded status badges). Desktop-first, responsive secondary. PREMIUM VISUAL QUALITY: frontend-design skill mandatory — top-tier aesthetic, not generic admin panel. Neon accents, sleek animations, terminal-inspired data views, visual discovery patterns. Must feel like a premium product. | APPROVED |
| G-039 | 2026-03-18 | UX | Error recovery: contextual error messages (what happened + impact + related stats), suggested actions (button next to error), undo capability (state change, policy assign, bulk op), command palette Ctrl+K (quick nav: search SIM, go to APN, open policy) | APPROVED |
| G-040 | 2026-03-18 | Performance | AAA latency budget: p50 <5ms, p95 <20ms, p99 <50ms. Redis-first session lookup, in-memory policy cache (NATS invalidation), pre-warmed operator connections, CI benchmark suite (target 10K+ auth/s single node) | APPROVED |
| G-041 | 2026-03-18 | Performance | Portal large dataset: cursor-based pagination (not offset), async count query (non-blocking), server-side lazy search, WebSocket push for live data (sessions, alerts, jobs), chart lazy load on viewport entry | APPROVED |
| G-042 | 2026-03-18 | Competitor | RAT-type awareness (NB-IoT, LTE-M, 4G, 5G): policy engine rules per RAT-type, APN config per RAT-type support, operator adapter RAT capability mapping, analytics RAT-type breakdown, SIM detail active RAT display, SoR engine RAT-type preference, session management RAT-type tracking, CDR per RAT-type cost differentiation | APPROVED |
| G-043 | 2026-03-18 | Competitor | Connectivity diagnostics: SIM auto-diagnosis ("why can't this SIM connect?" — last auth, reject reason, operator status, APN config, policy check), connectivity test from portal (trigger auth test, check session), troubleshooting wizard (step-by-step guided resolution) | APPROVED |

## Product Definition Decisions

| # | Date | Decision | Status |
|---|------|----------|--------|
| P-001 | 2026-03-18 | SCOPE.md created — 5-layer platform, 7 personas, success metrics defined | APPROVED |
| P-002 | 2026-03-18 | PRODUCT.md created — 73 features (F-001 to F-073), 6 workflows, 7 business rules, data model with 17 entities | APPROVED |
| P-003 | 2026-03-18 | GLOSSARY.md created — 60+ terms across 6 domains | APPROVED |

## Feature Discovery Decisions

| # | Date | Decision | Status |
|---|------|----------|--------|
| FD-001 | 2026-03-18 | Future Phase: AI & Predictive Intelligence — 4 features (AI Anomaly Engine, Predictive Quota, Auto-SoR, Network Quality Scoring) | APPROVED |
| FD-002 | 2026-03-18 | Future Phase: Connectivity Marketplace & eSIM Exchange | REJECTED |
| FD-003 | 2026-03-18 | Future Phase: Developer Platform & API Ecosystem | REJECTED |
| FD-004 | 2026-03-18 | Future Phase: Mobile Companion App | REJECTED |
| FD-005 | 2026-03-18 | Future Phase: Digital Twin & Network Simulation — 3 features (Network Digital Twin, What-If Scenarios, Load Testing) | APPROVED |
| FD-006 | 2026-03-18 | Future Phase: SGP.32 & Next-Gen eSIM | REJECTED |

## Architecture Decisions

| # | Date | Decision | Status |
|---|------|----------|--------|
| A-001 | 2026-03-18 | Go modular monolith (not microservices). 10 logical services as Go packages in single binary. Multiple protocol listeners (RADIUS/Diameter/HTTP/WS) as goroutines. Split to microservices later if needed. | ACCEPTED |
| A-002 | 2026-03-18 | Architecture complete: 10 services (SVC-01..10), 104 REST APIs + 10 WS events, 24 DB tables, 5 Docker containers, 3 ADRs, 7 data flows | ACCEPTED |
| A-003 | 2026-03-18 | Split architecture files (Large scale): api/, db/, services/, flows/ directories with _index.md summaries | ACCEPTED |
| A-004 | 2026-03-18 | Project root files generated: README.md, CLAUDE.md, Makefile, .env.example, .gitignore, Dockerfile, docker-compose.yml, nginx.conf | ACCEPTED |
| A-005 | 2026-03-18 | Screen Design: 26 screens + pattern library + data volume analysis | ACCEPTED |
| A-006 | 2026-03-18 | Theme: "Argus Neon Dark" — Linear×Bloomberg×Vercel aesthetic. Dark-first (#06060B), cyan neon accent (#00D4FF), Inter+JetBrains Mono, glass-morphism, pulsing status dots, ambient mesh gradients. 3 HTML mockups approved. FRONTEND.md generated. | ACCEPTED |

## Development Decisions

| # | Date | Decision | Status |
|---|------|----------|--------|
| DEV-001 | 2026-03-20 | STORY-001: Go version in Dockerfile updated from 1.22 to 1.25 to match go.mod go 1.25.6 | ACCEPTED |
| DEV-002 | 2026-03-20 | STORY-001: Created stub packages (apierr, audit, store/stubs, session, adapter/types, circuit_breaker) to allow later story code to compile. These are minimal stubs that will be fully implemented in their respective stories. | ACCEPTED |
| DEV-003 | 2026-03-20 | STORY-001: init_extensions migration creates TimescaleDB + uuid-ossp extensions | ACCEPTED |
| DEV-004 | 2026-03-20 | STORY-002: idx_api_keys_active partial index uses `WHERE revoked_at IS NULL` only (removed `expires_at > NOW()` since NOW() is not IMMUTABLE for index predicates) | ACCEPTED |
| DEV-005 | 2026-03-20 | STORY-002: idx_sims_iccid and idx_sims_imsi include operator_id because PostgreSQL requires unique indexes on partitioned tables to include all partition key columns | ACCEPTED |
| DEV-006 | 2026-03-20 | STORY-002: sims_default partition created as catch-all for operator_ids not in explicit partitions | ACCEPTED |
| DEV-007 | 2026-03-20 | STORY-002: Old sim_segments migration (20260319000001) removed; table + index included in core_schema migration | ACCEPTED |
| DEV-008 | 2026-03-20 | STORY-003: Partial token pattern for 2FA — login returns a short-lived (5min) JWT with `partial: true` claim. JWTAuth middleware rejects partial tokens; only JWTAuthAllowPartial permits them (used solely for /2fa/verify). This prevents 2FA-pending users from accessing protected endpoints. | ACCEPTED |
| DEV-009 | 2026-03-20 | STORY-004: Linear RBAC hierarchy cannot fully represent the non-linear ARCHITECTURE.md RBAC matrix (e.g., operator_manager cannot manage SIMs but sim_manager can, despite lower level; policy_editor can manage policies but sim_manager and operator_manager cannot). Current linear `RequireRole(minRole)` works for Phase 1 (only api_user/tenant_admin/super_admin boundaries used). Must refine to permission-based RBAC (`RequirePermission(permission)` with role-to-permission map) before STORY-011 (SIM CRUD, Phase 2). | ACCEPTED |
| DEV-010 | 2026-03-20 | STORY-006: Zerolog dev/prod output split — development uses `ConsoleWriter` (human-readable colored output), production uses JSON to stdout. Decision is implicit via `cfg.IsDev()`, no separate env var for log format. | ACCEPTED |
| DEV-011 | 2026-03-20 | STORY-006: EventBus.Publish uses JetStream (durable, at-least-once) for all event bus messages, not plain NATS publish. Slightly higher latency vs plain NATS, but durability is required for audit events (STORY-007) and job processing (STORY-031). Aligns with ADR-002. | ACCEPTED |
| DEV-012 | 2026-03-20 | STORY-006: Three NATS subjects defined in code but not in CONFIG.md — `argus.events.alert.triggered`, `argus.jobs.completed`, `argus.jobs.progress`. These are forward declarations for STORY-033/STORY-036/STORY-031. All fall under configured stream wildcards (`argus.events.>`, `argus.jobs.>`). CONFIG.md to be updated when those stories are implemented. | ACCEPTED |
| DEV-013 | 2026-03-20 | STORY-007: Old `audit.Service` stub (empty CreateEntry) replaced by `audit.FullService` which implements hash chain, NATS consumer, and per-tenant mutex serialization. `FullService` satisfies the `Auditor` interface for backward compatibility with tenant/user/session handlers. | ACCEPTED |
| DEV-014 | 2026-03-20 | STORY-007: `user_name` field omitted from API-140 response — response returns `user_id` only. Adding `user_name` would require a JOIN to users table on every audit list query. Frontend can resolve user names client-side. Aligns with plan spec which lists `user_id` in handler task. | ACCEPTED |
| DEV-015 | 2026-03-20 | STORY-007: CSV export (API-142) returns 200 with streamed CSV attachment instead of 202 with download_url. Per plan: "For v1, generate CSV inline and stream it as attachment (simplify: return 200 with CSV directly)". Background job approach deferred to future optimization. | ACCEPTED |
| DEV-016 | 2026-03-20 | STORY-007: `eventBusSubscriber` adapter in main.go bridges `bus.EventBus.QueueSubscribe` (returns `*nats.Subscription`) to `audit.MessageSubscriber` interface (returns `audit.Subscription`). Same pattern as `userStoreAdapter` and `sessionStoreAdapter`. | ACCEPTED |
| DEV-017 | 2026-03-20 | STORY-008: `RequireScope` in rbac.go updated to use `hasScopeAccess` with wildcard support (`*`, `resource:*`). Previously had exact-match-only comparison. Duplicate `ScopeCheck` in apikey_auth.go removed. | ACCEPTED |
| DEV-018 | 2026-03-20 | STORY-008: `CombinedAuth` middleware defined but not yet applied to existing routes. Current routes (tenant, user, audit, api-key management) remain JWT-only. `CombinedAuth` will be used when SIM/CDR/analytics routes are added in Phase 2+, enabling API key authentication for M2M endpoints. | ACCEPTED |
| DEV-019 | 2026-03-20 | STORY-008: API key lookup by prefix (GetByPrefix) queries DB without caching. Acceptable for Phase 1 scale. Redis caching with NATS invalidation can be added as a performance optimization when API key auth traffic grows significantly. | ACCEPTED |
| DEV-020 | 2026-03-20 | STORY-008: `isRedisNil` helper in ratelimit.go uses `errors.Is(err, redis.Nil)` as primary check with string-contains `"redis: nil"` as fallback, since Redis pipeline errors may wrap `redis.Nil` in ways `errors.Is` cannot unwrap. Gate initially recommended removing string fallback but it was retained for robustness. | ACCEPTED |
| DEV-021 | 2026-03-20 | STORY-009: Redis cache TTL for operator health was hardcoded to 60s. Gate fixed to use `2 * health_check_interval_sec` per operator, matching the plan spec. Minimum TTL floor of 30s added for safety. | ACCEPTED |
| DEV-022 | 2026-03-20 | STORY-009: `validAdapterTypes` in handler includes `sba` but adapter registry has no SBA factory. Acceptable for now — SBA adapter is a future story. Health check will fail gracefully (logged error, no crash). Will be resolved when SBA adapter is implemented. | ACCEPTED |
| DEV-023 | 2026-03-20 | STORY-009: `updated_at` column on operators table is handled by DB trigger `trg_operators_updated_at` (same pattern as tenants, users). No need for explicit `SET updated_at = NOW()` in Go code. | ACCEPTED |
| DEV-024 | 2026-03-20 | STORY-010: Error codes `CodeAPNHasActiveSIMs`, `CodePoolExhausted`, `CodeIPAlreadyAllocated` added to apierr package as constants, replacing hardcoded string literals in handlers. Consistent with existing error code pattern. | ACCEPTED |
| DEV-025 | 2026-03-20 | STORY-010: APN Archive method does not accept `userID` for `updated_by` — audit log captures the actor. The DB trigger `trg_apns_updated_at` handles `updated_at`. Acceptable for v1; if needed, `updated_by` can be added to Archive signature later. | ACCEPTED |
| DEV-026 | 2026-03-20 | STORY-010: APN Archive SIM count query (`SELECT COUNT(*) FROM sims WHERE apn_id = $1`) is not scoped by tenant_id. Acceptable because `apn_id` is a globally unique UUID and the handler already verifies APN belongs to the tenant via `GetByID` before calling Archive. | ACCEPTED |

## Performance Decisions

| # | Date | Decision | Status |
|---|------|----------|--------|
| PERF-001 | 2026-03-20 | STORY-009: Operator list not cached in Redis — admin-only endpoint, low frequency, acceptable latency. | ACCEPTED |
| PERF-002 | 2026-03-20 | STORY-009: operator:health:{id} Redis key with TTL = 2 * health_check_interval_sec. Provides fast health status lookup without DB hit. Fallback to DB on cache miss. | ACCEPTED |
| PERF-003 | 2026-03-20 | STORY-010: APN and IP Pool list endpoints not cached in Redis — admin-only, low frequency. Same rationale as PERF-001. | ACCEPTED |
| PERF-004 | 2026-03-20 | STORY-010: IP allocation uses DB-level FOR UPDATE SKIP LOCKED instead of Redis cache — correctness over speed for financial-grade IP inventory. Cache would introduce stale reads and double-allocation risk. | ACCEPTED |

---
