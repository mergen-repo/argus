# Architecture — Argus

> APN & Subscriber Intelligence Platform
> Scale: Large (246 APIs, 51 tables, 10 services)
> Architecture: Go modular monolith, multi-protocol

## Standard API Response Format

ALL API responses follow this envelope. No exceptions.

```json
// Success
{
  "status": "success",
  "data": { ... },
  "meta": { "cursor": "abc123", "limit": 50, "total": 10234567, "has_more": true }
}

// Error
{
  "status": "error",
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "ICCID is required",
    "details": [{ "field": "iccid", "message": "ICCID is required" }]
  }
}
```

Standard HTTP status codes: 200 OK, 201 Created, 204 No Content, 400 Bad Request, 401 Unauthorized, 403 Forbidden, 404 Not Found, 409 Conflict, 422 Unprocessable Entity, 429 Too Many Requests, 500 Internal Server Error.

## System Architecture

```
┌───────────────────────────────────────────────────────────────┐
│ CTN-01: Nginx (host:8084→:80)                                 │
│ /        → React SPA static files                             │
│ /api/*   → Go API (:8080)                                     │
│ /ws/*    → Go WebSocket (:8081)                                │
│ /health  → Go health check                                    │
└────────────────────────┬──────────────────────────────────────┘
                         │
┌────────────────────────▼──────────────────────────────────────┐
│ CTN-02: Argus (single Go binary)                              │
│                                                                │
│ ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌──────────┐ ┌────────┐│
│ │ SVC-01  │ │ SVC-02  │ │ SVC-04  │ │ SVC-04   │ │ SVC-04 ││
│ │ API GW  │ │ WS Srv  │ │ RADIUS  │ │ Diameter │ │ 5G SBA ││
│ │ :8080   │ │ :8081   │ │ :1812/13│ │ :3868    │ │ :8443  ││
│ └────┬────┘ └────┬────┘ └────┬────┘ └────┬─────┘ └───┬────┘│
│      │           │           │            │            │      │
│ ┌────▼───────────▼───────────▼────────────▼────────────▼────┐│
│ │                 INTERNAL PACKAGES                          ││
│ │ SVC-03: Core API    SVC-05: Policy Engine                  ││
│ │ SVC-06: Op Router   SVC-07: Analytics                      ││
│ │ SVC-08: Notifier    SVC-09: Job Runner                     ││
│ │ SVC-10: Audit                                              ││
│ └────────────────────────────────────────────────────────────┘│
└────────────┬──────────────┬──────────────┬────────────────────┘
             │              │              │
┌────────────▼───┐ ┌───────▼──────┐ ┌─────▼──────┐
│ CTN-03: PG+TS  │ │ CTN-04:Redis │ │ CTN-05:NATS│
│ :5432          │ │ :6379        │ │ :4222      │
└────────────────┘ └──────────────┘ └────────────┘
```

## Technology Stack

| Layer | Technology | Version | Rationale |
|-------|-----------|---------|-----------|
| Language | Go | 1.22+ | Performance, goroutines for protocol handlers, single binary deployment |
| Frontend | React + Vite | React 19, Vite 6 | SPA admin portal, largest component ecosystem |
| UI Framework | Tailwind CSS + shadcn/ui | Latest | Utility-first, customizable for premium dark-mode UI |
| State Mgmt | Zustand + TanStack Query | Latest | Zustand for UI state, TanStack for server cache |
| Forms | React Hook Form + Zod | Latest | Type-safe form validation |
| Charts | Recharts | Latest | React-native charts for analytics dashboards |
| Tables | TanStack Table | Latest | Virtual scrolling, sorting, filtering for 10M+ SIM views |
| Database | PostgreSQL + TimescaleDB | PG 16, TS 2.x | OLTP + time-series in single engine |
| Cache | Redis | 7 | Session cache, policy cache, rate limiting |
| Message Bus | NATS JetStream | Latest | Events, job queue, cache invalidation |
| RADIUS lib | layeh/radius | Latest | Go RADIUS protocol implementation |
| Migration | golang-migrate | Latest | Versioned, reversible SQL migrations |
| Auth | JWT (golang-jwt/jwt) | v5 | Portal auth with refresh tokens |
| 2FA | pquerna/otp | Latest | TOTP implementation |
| HTTP Router | chi | v5 | Lightweight, middleware-friendly Go router |
| WebSocket | gorilla/websocket | Latest | Real-time event streaming |
| Logging | zerolog | Latest | Structured JSON logging |
| Tracing | go.opentelemetry.io/otel | v1.43.0 | Distributed tracing — OTLP gRPC export, W3C TraceContext propagation |
| Metrics | prometheus/client_golang | v1.23.2 | Prometheus registry + `/metrics` scrape endpoint |
| DB Tracing | otelpgx | v0.10.0 | pgx v5 native OTel tracer (spans per query) |
| Config | envconfig | Latest | Environment variable configuration |
| Testing | Go testing + testify | Latest | Unit + integration tests |
| Container | Docker + Compose | Latest | Deployment |
| Reverse Proxy | Nginx | Alpine | Static serving, reverse proxy, routing (TLS deferred to production story) |

## Docker Architecture

| Container | Image | Port | Purpose | Health Check |
|-----------|-------|------|---------|-------------|
| CTN-01 | nginx:alpine | 8084→80 | Reverse proxy, static SPA (HTTP; TLS deferred) | GET / |
| CTN-02 | argus:latest (custom) | 8080, 8081, 1812, 1813, 3868, 8443 | Go monolith | GET :8080/health/ready |
| CTN-03 | timescale/timescaledb:latest-pg16 | 5432 | PostgreSQL + TimescaleDB | pg_isready |
| CTN-04 | redis:7-alpine | 6379 | Cache, rate limiting | redis-cli ping |
| CTN-05 | nats:latest | 4222, 8222 | Event bus, job queue | /healthz on :8222 |
| argus-operator-sim | operator-sim:latest (custom) | 9595 (API), 9596 (health+metrics) | Passive operator backend simulator. Probed by argus-app's HealthChecker per-protocol http fan-out. | GET :9596/-/health |

### Networks
- `argus-net`: All containers on single bridge network

### Volumes
- `pgdata`: PostgreSQL data persistence
- `natsdata`: NATS JetStream persistence
- `postgres_wal_archive`: WAL segment archive staging (mounted into postgres container; S3 shipping via `archive_command` when `ARGUS_WAL_BUCKET`/`ARGUS_WAL_PREFIX` are set)

### Environment Variables
See `.env.example` for complete list.

## Project Structure

```
argus/
├── cmd/
│   ├── argus/
│   │   └── main.go              # Entry point — starts all listeners
│   ├── argusctl/                # Ops CLI (STORY-067): tenant/apikey/user/sim/health/backup commands
│   │   ├── main.go
│   │   └── cmd/                 # cobra subcommands (root, tenant, apikey, user, compliance, sim, health, backup)
│   ├── simulator/               # AAA traffic simulator binary (STORY-082/083/084/085) — dev/test tool only
│   │   └── main.go              # Entry; SIMULATOR_ENABLED env guard; builds operator clients + engine
│   └── operator-sim/            # Operator SoR Simulator — passive HTTP server emulating Turkcell/Vodafone/TT backend systems for the argus HTTP adapter to probe
│       └── main.go
├── internal/
│   ├── gateway/                  # SVC-01: HTTP API gateway, middleware
│   ├── ws/                       # SVC-02: WebSocket server
│   ├── api/                      # SVC-03: Core CRUD handlers
│   │   ├── tenant/
│   │   ├── user/
│   │   ├── sim/
│   │   ├── apn/
│   │   ├── operator/
│   │   ├── esim/
│   │   ├── ippool/
│   │   ├── apikey/
│   │   ├── onboarding/           # STORY-069: Onboarding wizard session management
│   │   ├── reports/              # STORY-069: On-demand & scheduled report generation
│   │   ├── webhooks/             # STORY-069: Webhook config & delivery tracking
│   │   ├── sms/                  # STORY-069: SMS Gateway outbound + history
│   │   ├── announcement/         # STORY-077: System announcement CRUD + active + dismiss
│   │   ├── undo/                 # STORY-077: POST /undo/:action_id inverse-operation handler
│   │   ├── system/               # STORY-078: GET /system/config — redacted config + build metadata (super_admin)
│   │   ├── cdr/                  # CDR list + export endpoints
│   │   ├── ota/                  # OTA command dispatch endpoints (STORY-029)
│   │   └── ...
│   ├── aaa/                      # SVC-04: AAA engine
│   │   ├── radius/               # RADIUS server — Access-Accept dynamic AllocateIP + Accounting-Stop ReleaseIP (STORY-092)
│   │   ├── diameter/             # Diameter server — Gx CCA-I Framed-IP-Address AVP + CCR-T ReleaseIP (STORY-092)
│   │   ├── sba/                  # 5G SBA proxy — AUSF/UDM (STORY-020) + Nsmf mock Create/Release (STORY-092)
│   │   ├── eap/                  # EAP-SIM/AKA handlers
│   │   ├── rattype/              # RAT type canonical enum & mapping
│   │   └── session/              # Session management
│   ├── policy/                   # SVC-05: Policy engine
│   │   ├── dsl/                  # DSL parser + compiler + evaluator + sql_predicate.go (ToSQLPredicate — FIX-230)
│   │   ├── evaluator/            # Rule evaluation
│   │   └── rollout/              # Staged rollout
│   ├── operator/                 # SVC-06: Operator routing
│   │   ├── adapter/              # Pluggable adapters — per-(operator, protocol) registry; RADIUS/Diameter/SBA/HTTP/Mock (STORY-090)
│   │   ├── adapterschema/        # Nested adapter_config JSON schema: detect/validate/upconvert flat→nested (STORY-090)
│   │   ├── sor/                  # Steering of Roaming
│   │   ├── circuit/              # Circuit breaker
│   │   └── mock/                 # Mock simulator
│   ├── analytics/                # SVC-07: Analytics engine
│   │   ├── cdr/                  # CDR processing
│   │   ├── anomaly/              # Anomaly detection
│   │   ├── cost/                 # Cost optimization
│   │   ├── metrics/              # Redis-backed realtime metrics (WS dashboard, STORY-033)
│   │   └── aggregates/           # Cross-tab aggregation facade — Redis-cached (60s TTL), NATS-invalidated; canonical SIM/session counts (FIX-208)
│   ├── observability/            # Cross-cutting OTel + Prometheus infrastructure (STORY-065)
│   │   ├── otel.go               # OTel tracer provider init (OTLP gRPC, resource attrs, shutdown)
│   │   └── metrics/              # Prometheus registry, metric descriptors, AAA composite recorder
│   ├── alertstate/               # Alert state + dedup package (FIX-210, D-076): StateOpen/Acknowledged/Resolved/Suppressed constants, Transitions map, CanTransition/IsUpdateAllowed/IsActive predicates, DedupKey SHA-256 helper (excludes severity per D3)
│   ├── notification/             # SVC-08: Notification service — handleAlertPersist subscriber (FIX-209/FIX-210): UpsertWithDedup replaces Create; cooldown gate; 3 Prometheus outcome branches (inserted/dedup/cooldown)
│   ├── api/
│   │   └── alert/                # Alert API handler (FIX-209/FIX-210): GET /alerts, GET /alerts/{id}, PATCH /alerts/{id} state transition; cooldownMinutes wired from cfg; suppressed NOT patchable (API contract)
│   ├── job/                      # SVC-09: Job runner
│   ├── audit/                    # SVC-10: Audit service
│   ├── model/                    # Domain models
│   ├── store/                    # Database access (PG)
│   │   ├── alert.go              # AlertStore (FIX-209/FIX-210): UpsertWithDedup/SuppressAlert/UnsuppressAlert/FindActiveByDedupKey; UpdateState stamps cooldown_until on resolve; 4 new columns scanned (occurrence_count, first/last_seen_at, cooldown_until)
│   │   └── schemacheck/          # Boot-time schema integrity check (STORY-086): CriticalTables manifest + Verify — FATAL on missing table
│   ├── cache/                    # Redis cache layer
│   ├── bus/                      # NATS event bus — canonical `bus.Envelope` schema (FIX-212): event_version, id, type, tenant_id, severity, source, entity (EntityRef), dedup_key, meta; `Validate()` enforced on every publish; see docs/architecture/WEBSOCKET_EVENTS.md §Event Envelope
│   ├── events/                   # Event catalog + resolver (FIX-212): `events.TypeForSubject` subject→type map; `events.Resolver` interface + `NewRedisResolver` (TTL-600s entity display_name lookup); `GET /api/v1/events/catalog` handler (API-316)
│   ├── undo/                     # Undo registry — Redis-backed 15s TTL inverse-operation store (STORY-077)
│   ├── ota/                      # OTA command orchestration — SM-DP+ dispatch, polling, state machine
│   ├── geoip/                    # GeoIP lookup — MaxMind wrapper with graceful nil on missing DB (STORY-077)
│   ├── export/                   # CSV streaming helper — cursor-paged, Flusher-aware (STORY-077)
│   ├── middleware/
│   │   └── impersonation.go      # ImpersonationReadOnly middleware — blocks non-GET when impersonated (STORY-077)
│   ├── auth/                     # JWT, 2FA, API key
│   ├── tenant/                   # Tenant context middleware
│   ├── config/                   # Configuration
│   ├── operatorsim/              # Operator SoR Simulator runtime (config, HTTP server, handlers)
│   └── simulator/                # AAA traffic simulator packages (STORY-082/083/084/085) — dev/test tool only
│       ├── config/               # YAML config schema (RADIUS + Diameter + SBA + Reactive defaults, per-operator opt-in)
│       ├── discovery/            # Read-only PG fetch of SIMs / operators / APNs
│       ├── scenario/             # Weighted-random scenario picker
│       ├── radius/               # RADIUS Auth + Acct client
│       ├── engine/               # Session lifecycle orchestration (RADIUS + Diameter bracket + SBA fork + reactive hooks)
│       ├── metrics/              # Prometheus vectors (simulator_radius_* + simulator_diameter_* + simulator_sba_* + simulator_reactive_*)
│       ├── diameter/             # Diameter Gx/Gy client (STORY-083): peer state machine, CCR builders, high-level client
│       ├── sba/                  # 5G SBA client (STORY-084): AUSF 5G-AKA, UDM registration, per-operator opt-in
│       └── reactive/             # Reactive SIM emulator (STORY-085): state machine, CoA/DM UDP listener, reject backoff, retry-storm cap
├── web/                          # React SPA
│   ├── src/
│   │   ├── components/
│   │   │   ├── atoms/            # Button, Input, Badge, Icon, etc.
│   │   │   ├── molecules/        # FormField, SearchBar, StatusBadge, etc.
│   │   │   ├── organisms/        # Header, Sidebar, SimTable, PolicyEditor, etc.
│   │   │   ├── templates/        # DashboardLayout, AuthLayout
│   │   │   └── pages/            # LoginPage, DashboardPage, SimListPage, etc.
│   │   ├── hooks/                # Custom React hooks
│   │   ├── stores/               # Zustand stores
│   │   ├── api/                  # TanStack Query + API client
│   │   ├── lib/                  # Utilities
│   │   └── styles/               # Tailwind config, global styles
│   ├── index.html
│   ├── vite.config.ts
│   └── package.json
├── migrations/
│   ├── 20260318000001_initial_schema.up.sql
│   ├── 20260318000001_initial_schema.down.sql
│   └── seed/
│       ├── 001_admin_user.sql
│       └── 002_system_data.sql
├── deploy/
│   ├── docker-compose.yml
│   ├── docker-compose.blue.yml   # Blue stack (STORY-067): ports 8080/8081/1812/1813/3868/8443
│   ├── docker-compose.green.yml  # Green stack (STORY-067): ports 9080/9081/1822/1823/3878/9443
│   ├── docker-compose.prod.yml
│   ├── docker-compose.obs.yml    # Optional observability overlay: Prometheus + Grafana + OTel Collector (STORY-065)
│   ├── scripts/                  # Deployment automation (STORY-067)
│   │   ├── bluegreen-flip.sh     # Flip Nginx upstream; hard-fails on audit error
│   │   ├── rollback.sh           # Restore previous color from snapshot; hard-fails on audit error
│   │   ├── smoke-test.sh         # Post-deploy health assertions
│   │   ├── deploy-snapshot.sh    # Capture pre-deploy state as JSON snapshot
│   │   └── deploy-tag.sh         # Create git tag for deploy event
│   └── nginx/
│       └── nginx.conf
├── infra/
│   ├── docker/
│   │   └── Dockerfile.argus      # Multi-stage Go+React build
│   ├── monitoring/
│   │   └── nats-check.sh         # NATS health probe
│   ├── grafana/
│   │   ├── dashboards/           # 6 pre-built Grafana dashboard JSONs (STORY-065)
│   │   └── provisioning/         # Datasource + dashboard provisioning configs
│   ├── prometheus/
│   │   ├── prometheus.yml        # Prometheus scrape config (STORY-065)
│   │   └── alerts.yml            # 9 Prometheus alert rules (STORY-065)
│   └── otel/
│       └── otel-collector-config.yaml  # OTel Collector pipeline config (STORY-065)
├── .dockerignore
├── docs/                         # All planning & architecture docs
├── .env.example
├── .gitignore
├── CLAUDE.md
├── Makefile
├── README.md
└── go.mod
```

## Frontend Component Architecture

### Routing

| Route | Page | Auth | Layout |
|-------|------|------|--------|
| /login | LoginPage | No | AuthLayout |
| /setup | OnboardingWizardPage | JWT (first login) | AuthLayout |
| / | DashboardPage | JWT | DashboardLayout |
| /dashboard | DashboardPage (alias) | JWT | DashboardLayout |
| /sims | SimListPage (segments) | JWT (sim_manager+) | DashboardLayout |
| /sims/:id | SimDetailPage | JWT (sim_manager+) | DashboardLayout |
| /apns | ApnListPage | JWT (sim_manager+) | DashboardLayout |
| /apns/:id | ApnDetailPage | JWT (sim_manager+) | DashboardLayout |
| /operators | OperatorListPage | JWT (operator_manager+) | DashboardLayout |
| /operators/:id | OperatorDetailPage | JWT (operator_manager+) | DashboardLayout |
| /policies | PolicyListPage | JWT (policy_editor+) | DashboardLayout |
| /policies/:id | PolicyEditorPage | JWT (policy_editor+) | DashboardLayout |
| /esim | EsimListPage | JWT (sim_manager+) | DashboardLayout |
| /sessions | SessionListPage | JWT (sim_manager+) | DashboardLayout |
| /cdrs | CDRExplorerPage | JWT (analyst+) | DashboardLayout |
| /analytics | AnalyticsDashboardPage | JWT (analyst+) | DashboardLayout |
| /analytics/cost | CostAnalyticsPage | JWT (analyst+) | DashboardLayout |
| /jobs | JobListPage | JWT (sim_manager+) | DashboardLayout |
| /audit | AuditLogPage | JWT (tenant_admin+) | DashboardLayout |
| /settings | SettingsPage (unified tabbed) | JWT (any) | DashboardLayout — 5 tabs: Security, Sessions, Reliability (super_admin only), Notifications, Preferences; hash routing via `useHashTab`; tabs lazy-loaded; RBAC filter via `hasMinRole` (`src/lib/rbac.ts`); FIX-240 |
| /settings/security | → redirect `/settings#security` | — | 301-replace; FIX-240 |
| /settings/sessions | → redirect `/settings#sessions` | — | 301-replace; FIX-240 |
| /settings/reliability | → redirect `/settings#reliability` | — | 301-replace; FIX-240 |
| /settings/notifications | → redirect `/settings#notifications` | — | 301-replace; FIX-240 |
| /settings/users | UserManagementPage | JWT (tenant_admin+) | DashboardLayout |
| /settings/api-keys | ApiKeyPage | JWT (tenant_admin+) | DashboardLayout |
| /settings/ip-pools | IpPoolPage | JWT (operator_manager+) | DashboardLayout |
| /settings/system | SystemConfigPage | JWT (super_admin) | DashboardLayout |
| /system/health | SystemHealthPage | JWT (super_admin) | DashboardLayout |
| /system/tenants | TenantManagementPage | JWT (super_admin) | DashboardLayout |
| /admin/tenant-usage | TenantUsagePage | JWT (super_admin) | DashboardLayout — unified quota+resource dashboard; card/table toggle; 30s auto-refresh; 80%/95% threshold pulse rings; breach drill-down; FIX-246 |
| /admin/quotas | → redirect `/admin/tenant-usage` | — | 301-replace; FIX-246 |
| /admin/resources | → redirect `/admin/tenant-usage` | — | 301-replace; FIX-246 |

### State Management
- **Zustand**: Auth state, UI preferences (dark/light mode), sidebar state, command palette
- **TanStack Query**: All server data (SIMs, APNs, policies, sessions, analytics)
- **React Hook Form + Zod**: All forms (SIM create, APN create, policy DSL editor, etc.)

## Security Architecture

### Authentication Flow
```
Login: POST /api/v1/auth/login → validate credentials → check account lockout
    → check 2FA (TOTP or backup code)
    → if password_change_required=true: return partial JWT + PASSWORD_CHANGE_REQUIRED
    → issue JWT (15min) + refresh token (7d, stored in TBL-03)
    → set refresh token as httpOnly cookie

Force-Change Flow: partial JWT → POST /api/v1/auth/password/change
    → validate current password + policy + history → clear flag → issue full JWT

API Request: Authorization: Bearer <jwt>
    → gateway middleware validates JWT
    → extracts tenant_id + user_id + role
    → injects into request context

Token Refresh: POST /api/v1/auth/refresh (httpOnly cookie)
    → in-handler rate limit: 60/min per session (SHA-256 cookie → Redis sliding window)
    → validate refresh token against TBL-03
    → issue new JWT + rotate refresh token
    → return {token, expires_in, refresh_expires_in} in response body
    → refresh_token rotated in-place (httpOnly cookie; never in body)

FE Refresh Interceptor (web/src/lib/api.ts + web/src/stores/auth.ts):
    → 401 response → single-flight (isRefreshing + failedQueue): exactly 1 refresh fires
    → on success: setToken() derives tokenExpiresAt from JWT exp claim (server expires_in is fallback only)
    → on failure: logout() + redirect /login?reason=session_expired&return_to=<path>
    → pre-emptive scheduler: fires 5 min before JWT exp — silent refresh, no spinner
    → cross-tab sync: BroadcastChannel('argus-auth-broadcast') — token_refreshed message propagates to all open tabs

API Key: X-API-Key: argus_<prefix>_<secret>
    → gateway looks up key_prefix in TBL-04
    → validates SHA-256(secret) == key_hash
    → checks scopes, rate limits, expiry, IP whitelist (allowed_ips CIDR match)
```

### Enterprise Auth Hardening (STORY-068)

- **Password Policy**: Configurable complexity (length, upper/lower/digit/symbol, max-repeating) enforced at user create, password change, admin reset, invite-complete. Error codes: `PASSWORD_TOO_SHORT`, `PASSWORD_MISSING_CLASS`, `PASSWORD_REPEATING_CHARS`.
- **Password History**: TBL-34 (`password_history`) stores last N bcrypt hashes (default 5). Reuse rejected with `PASSWORD_REUSED`. Trimmed to N entries post-insert.
- **Force Password Change (AC-3)**: `users.password_change_required` BOOLEAN. Set true on: admin-triggered reset, invite activation, password expiry. Login returns `partial: true` + `reason: password_change_required` → frontend navigates to change-password screen; full JWT issued only after successful change.
- **2FA Backup Codes (AC-4)**: TBL-35 (`user_backup_codes`) — 10 bcrypt-hashed single-use codes per user (crypto/rand). Login accepts TOTP OR backup code. Used codes marked; regenerate invalidates all prior. Warning in meta when <3 remaining.
- **API Key IP Whitelist (AC-5)**: `api_keys.allowed_ips TEXT[]` (CIDR notation). GIN-indexed. Empty array = any IP allowed (backwards compat). Middleware rejects non-whitelisted IPs with `API_KEY_IP_NOT_ALLOWED`.
- **Session Revoke (AC-6)**: `POST /api/v1/users/:id/revoke-sessions` — tenant_admin or self. Invalidates all refresh tokens; optional `?include_api_keys=true`. WS connections dropped.
- **Force-Logout All (AC-7)**: `POST /api/v1/system/revoke-all-sessions?tenant=X` — super_admin (or tenant_admin scoped). Sends notifications if email configured.
- **Tenant Resource Limits (AC-8)**: Middleware reads tenants.max_sims/apns/users/max_api_keys (cached 5min Redis). Rejects create operations with `TENANT_LIMIT_EXCEEDED` + resource/current/max payload.
- **Account Lockout (AC-10)**: After N failed logins (`LOGIN_MAX_ATTEMPTS=5`), locked for `LOGIN_LOCKOUT_DURATION=15m`. Error code `ACCOUNT_LOCKED` with retry-after. Tenant admin can manually unlock via `POST /api/v1/users/:id/unlock`. Auto-unlock on expiry.

### RBAC Matrix

| Action | super_admin | tenant_admin | operator_mgr | sim_mgr | policy_editor | analyst | api_user |
|--------|:-----------:|:------------:|:------------:|:-------:|:-------------:|:-------:|:--------:|
| Manage tenants | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Manage operators | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Manage users | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Manage APNs | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| Manage IP pools | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| Manage SIMs | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | scoped |
| Manage eSIM | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | scoped |
| Manage policies | ✅ | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ |
| View analytics | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | scoped |
| View audit logs | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Force disconnect | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | scoped |
| System config | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

### Database-Level Tenant Isolation (Defense-in-Depth)

Row-Level Security (RLS) is enabled with `FORCE ROW LEVEL SECURITY` on all 30 tenant-scoped tables (TBL-01 to TBL-31 + TBL-34 password_history + TBL-35 user_backup_codes, excluding system tables). Policies use `current_setting('app.current_tenant', true)::uuid` to validate tenant context. The app database role uses `BYPASSRLS` — RLS operates as a defense-in-depth layer, not as the primary isolation boundary. Per-request transaction-scoped RLS enforcement is future work (DEV-167). See [docs/architecture/db/rls.md](architecture/db/rls.md) for full policy definitions.

## Performance Architecture

### AAA Hot Path (p99 < 50ms target)

```
RADIUS Request → UDP listener (goroutine pool)
    → Parse packet (in-memory, ~0.1ms)
    → Redis: lookup session + policy cache (~0.5ms)
    → Policy evaluate (in-memory, ~0.1ms)
    → Operator route (in-memory IMSI prefix table, ~0.01ms)
    → Forward to operator adapter (~5-30ms network)
    → Redis: update session (~0.5ms)
    → NATS: publish accounting event (async, ~0.1ms)
    → Response (~0.1ms)
```

### Caching Strategy

| Data | Store | TTL | Invalidation |
|------|-------|-----|-------------|
| SIM auth data (IMSI→SIM) | Redis | 5min | NATS on SIM update |
| Policy compiled rules | Redis | 10min | NATS on policy version change |
| Operator IMSI prefix table | In-memory | 1hr | NATS on operator change |
| Session state | Redis | Session duration | CoA/DM events |
| Tenant config | Redis | 5min | NATS on tenant update |
| EAP session state | Redis | 30s | Auto-expire (TTL) |
| MSK stash (EAP session) | In-memory sync.Map | 10s (30s sweeper) | LoadAndDelete on consume (single-use) |
| Auth vector pre-fetch | Redis list | 5min | Auto-expire (TTL) |
| Rate limit counters | Redis | Sliding window | Auto-expire |
| SoR decision (per-SIM) | Redis | 1hr (configurable) | NATS on operator health change |
| Auth rate counters | Redis INCR | 5s | Auto-expire (TTL) |
| Auth latency window | Redis ZSET | 120s | Auto-expire + 60s sliding prune |
| Dashboard aggregates | TimescaleDB continuous agg | 1hr | Auto-refresh |
| Aggregates cache (per-tenant, per-method) | Redis | 60s | NATS on sim.updated, policy.changed, session.started, session.ended (queue: aggregates-invalidator) |
| Dashboard cache (per-tenant) | Redis | 30s | NATS on sim.*, session.*, operator.health_changed, cdr.recorded |
| Active sessions counter (per-tenant) | Redis INCR | No TTL | NATS session.started/ended + hourly reconciler SET |
| Diagnostic result (per-SIM) | Redis | 1min | Auto-expire (TTL) |

## Observability Architecture

Added in STORY-065 (Phase 10 production hardening). All instrumentation is cross-cutting with zero upward dependencies on business packages.

### Distributed Tracing (OpenTelemetry)

- **Provider init**: `internal/observability/otel.go` — OTLP gRPC exporter to `OTEL_EXPORTER_OTLP_ENDPOINT`. Resource attributes: `service.name`, `service.version`, `deployment.environment`. Graceful shutdown flushes spans.
- **HTTP**: Chi router wrapped with `otelhttp.NewHandler` (outermost layer). Span attributes include `http.method`, `http.route`, `http.status_code`, `correlation_id`, `tenant_id`, `user_id`.
- **DB**: pgx pool uses `compositeTracer` (otelpgx v0.10.0 + `SlowQueryTracer`). Every query produces a child span with `db.statement`, `db.operation`, `db.system=postgresql`. Queries >100ms get `db.slow_query=true` attribute.
- **NATS**: `Publish()` injects `traceparent` W3C header; consumer handlers extract and create child spans. Legacy Subscribe paths preserved.
- **Context propagation**: W3C TraceContext (`propagation.TraceContext{}`) set as global propagator. `correlation_id` flows from HTTP log middleware → OTel span attributes → NATS headers.

### Metrics (Prometheus)

- **Registry**: Custom `*prometheus.Registry` in `internal/observability/metrics/metrics.go`. Handler: `promhttp.HandlerFor`. Exposed at `GET /metrics` (no auth, Prometheus scrape format).
- **Core metric set** (17 vectors): HTTP counters/histograms, AAA auth counters/latency, active sessions, DB pool/query histograms, NATS pub/consume counters, Redis ops/cache hit counters, job run counters/duration, operator health gauge, circuit breaker state gauge. See AC-6 in STORY-065 for full label sets.
- **Tenant labeling**: `tenant_id` label on HTTP and AAA metrics. Kill-switch: `METRICS_TENANT_LABEL_ENABLED=false` drops the label to control cardinality (DEV-173).
- **AAA wiring**: `CompositeMetricsRecorder` wraps both the legacy Redis `Collector` (WS realtime dashboard, STORY-033) and new `PrometheusRecorder` — both receive every auth event (DEV-172).
- **Security note**: `tenant_id` is added to labels only after auth middleware extracts it from JWT; unauthenticated requests do not propagate tenant_id into metrics labels.

### Grafana Dashboards & Alert Rules

- **Dashboards** (`infra/grafana/dashboards/`, 6 files, schemaVersion 38):
  - `argus-overview.json` — request rate, error rate, p95/p99 latency, goroutines, memory
  - `argus-aaa.json` — auth/s per protocol, latency percentiles, operator health, circuit breaker
  - `argus-database.json` — pool utilization, query duration, slow queries
  - `argus-messaging.json` — NATS rates, Redis ops/hit rate
  - `argus-tenant.json` — per-tenant metrics (templated by `tenant_id`)
  - `argus-jobs.json` — job throughput, duration, failure rate
- **Alert rules** (`infra/prometheus/alerts.yml`, 9 rules): `ArgusHighErrorRate`, `ArgusAuthLatencyHigh`, `ArgusOperatorDown`, `ArgusCircuitBreakerOpen`, `ArgusDBPoolExhausted`, `ArgusNATSConsumerLag`, `ArgusJobFailureRate`, `ArgusRedisEvictionStorm`, `ArgusDiskSpaceLow`.
- **Deployment**: Optional overlay compose file `deploy/docker-compose.obs.yml` starts Prometheus, Grafana, and OTel Collector on `argus_argus-net` (DEV-176). The core Argus binary's `/metrics` endpoint works standalone without the overlay.

## Backup Infrastructure (STORY-066)

Automated PostgreSQL backup pipeline running inside the Argus binary via the SVC-09 job scheduler:

- **AlertsRetentionJob** (`internal/job/alerts_retention.go`) — daily cron at 03:15 UTC; calls `AlertStore.DeleteOlderThan(now - ALERTS_RETENTION_DAYS)` (default 180 days, min 30) to purge old `alerts` rows. Wired in `cmd/argus/main.go` after `notifSvc.SetAlertStore(alertStore)`. FIX-209.
- **DataIntegrityDetector** (`internal/job/data_integrity.go`) — daily cron (`17 3 * * *`) that scans recent sessions/CDRs for four invariant violations: negative-duration sessions (`ended_at < started_at`), negative-duration CDRs (`duration_sec < 0`), framed-IP outside the SIM's assigned pool, and malformed IMSI. Violations are quarantined (sessions/CDRs) or logged+metered (IMSI), and exposed via `argus_data_integrity_violations_total{kind}` Prometheus counter. FIX-207 AC-5.
- **BackupProcessor** (`internal/job/backup.go`) — schedules daily/weekly/monthly `pg_dump` runs, uploads compressed dumps to S3 (`AWS_REGION`, `BACKUP_S3_BUCKET`, `BACKUP_S3_PREFIX`), and records every run in TBL-32 (`backup_runs`). Configurable retention sweep: `BACKUP_DAILY_RETAIN`, `BACKUP_WEEKLY_RETAIN`, `BACKUP_MONTHLY_RETAIN`.
- **Weekly verification** — a follow-on job restores the latest daily dump to a scratch container and counts rows in `tenants` and `sims`, writing results to TBL-33 (`backup_verifications`). Deviation > 1% triggers an incident log.
- **WAL archiving** — `archive_mode = on` and `archive_command` are set in `infra/postgres/postgresql.conf`. The `postgres_wal_archive` Docker volume provides local staging. Live S3/MinIO WAL shipping activates when `ARGUS_WAL_BUCKET` + `ARGUS_WAL_PREFIX` env vars are set at deploy time.
- **PITR** — Point-in-time recovery uses `recovery.signal` + `recovery_target_time` + `recovery_target_action = promote` in `postgresql.auto.conf`. Full procedure documented in `docs/runbook/dr-pitr.md`.
- **Health probe split** (`internal/gateway/health.go`): three distinct endpoints replace the legacy `/api/health`:
  - `GET /health/live` — goroutine-only, always 200 while process runs (API-187)
  - `GET /health/ready` — full dependency check + disk space probe (API-188)
  - `GET /health/startup` — 60-second grace period, then delegates to ready (API-189)
- **Disk space probe** — `argus_disk_usage_percent{mount}` Prometheus gauge; configurable mounts via `DISK_PROBE_MOUNTS`, thresholds via `DISK_DEGRADED_PCT`/`DISK_UNHEALTHY_PCT`.

## CI/CD Pipeline & Ops Tooling (STORY-067)

### GitHub Actions CI Pipeline

Five-stage pipeline (`.github/workflows/ci.yml`): `lint` → `test` → `security-scan` → `build` → `deploy`. Fail-fast: downstream stages are gated by upstream success.

- **lint**: `golangci-lint` + `npm run lint` + `npm run type-check`
- **test**: `go test ./... -race -short` + `npm test`
- **security-scan**: `govulncheck` + `gosec` + `npm audit`
- **build**: Multi-stage Docker build; digest-pinned base image (see DEV-190); pushes to registry
- **deploy**: Parameterized `deploy-staging` / `deploy-prod` jobs; calls `bluegreen-flip.sh`; creates git deploy tag

All Docker `FROM` statements are pinned via `image@sha256:<digest>` (DEV-190). `infra/scripts/update-digests.sh` re-pins on demand.

### Blue-Green Deployment

Two Docker Compose stacks (`deploy/docker-compose.blue.yml` / `deploy/docker-compose.green.yml`) on distinct port ranges. Nginx upstream toggled via `infra/nginx/upstream.conf` include (see DEV-186).

- `deploy/scripts/bluegreen-flip.sh` — identifies active color, starts inactive color, runs smoke test, flips Nginx, saves JSON deploy snapshot, posts audit event to `POST /api/v1/audit/system-events`; on failure, reverts Nginx immediately
- `deploy/scripts/rollback.sh` — reads deploy snapshot by `VERSION=`, starts rollback color, smoke-tests, flips Nginx back, posts audit event; fails hard on non-2xx audit response
- `deploy/scripts/smoke-test.sh` — asserts `/health/ready` and `/api/v1/status` return 200

### argusctl CLI

Cobra-based binary (`cmd/argusctl/`). Auth: `--token` flag or `ARGUSCTL_TOKEN` env (Viper prefix `ARGUSCTL_`). Config: `~/.argusctl.yaml`. Subcommands: `tenant` (list/create/suspend/resume), `apikey` (list/create), `user` (purge), `compliance` (dsar/erasure), `sim` (state), `health`, `backup` (verify). Build: `make build-ctl` → `dist/argusctl`.

### Status Endpoint

`GET /api/v1/status` — public aggregate (no auth). `GET /api/v1/status/details` — auth-gated (super_admin). Both served by `internal/api/system/status_handler.go`. `argus_build_info{version,git_sha,build_time}` Prometheus gauge emitted at startup.

## IMEI Binding & Device Capture (Phase 11)

> Phase 11 (Enterprise Readiness Pack) adds device-identity capture and SIM-to-device binding enforcement on the AAA hot path. Authoritative decision: [ADR-004](adrs/ADR-004-imei-binding-architecture.md). Six stories: STORY-093 (capture), STORY-094 (binding model + DSL), STORY-095 (pool management), STORY-096 (enforcement), STORY-097 (re-pair workflow), STORY-098 (native syslog forwarder).

### Capture Pipeline (SVC-04 sub-component)

The AAA engine reads device identity on every supported protocol and feeds a normalized SessionContext into the policy engine before evaluation:

- **RADIUS** (`internal/protocol/radius`) — parses `3GPP-IMEISV` VSA (vendor 10415, attr 20) on Access-Request and Accounting-Start frames.
- **Diameter S6a** (`internal/protocol/diameter`) — unpacks grouped `Terminal-Information` AVP 350 (sub-AVPs `IMEI` 1402, `Software-Version` 1403, `IMEI-SV` 1404) on AIR/ULR exchanges.
- **5G SBA** (`internal/aaa/sba`) — strips and tags `PEI` (`imei-…` / `imeisv-…` / `mac-…` / `eui64-…`) from `Nudm_UEAuthentication` request bodies and `Namf_Communication` UE-context payloads.

All three parsers are null-safe (auth proceeds when IMEI absent) and instrumented with `argus_imei_capture_parse_errors_total{protocol}` Prometheus counters. The normalized fields populate `SessionContext.Device {IMEI, TAC, SoftwareVersion, IMEISV, PEIRaw, CaptureProtocol, BindingStatus}`.

See [architecture/PROTOCOLS.md](architecture/PROTOCOLS.md) §"IMEI Capture (Cross-Protocol)" for wire-format details.

### Binding Pre-Check (SVC-04 sub-component)

When `sims.binding_mode IS NOT NULL` the AAA engine runs a pre-check **before** policy DSL evaluation:

| Mode | Behaviour |
|------|-----------|
| `NULL` (default) | Skip — `device.binding_status = "disabled"`. Existing behaviour preserved for migrated rows. |
| `strict` (1:1) | Allow only when `device.imei = sims.bound_imei`. Else Access-Reject. |
| `allowlist` (1:N) | Allow when IMEI is in `sim_imei_allowlist` (TBL-60). Else Access-Reject. |
| `first-use` | Auto-bind first observed IMEI; subsequent mismatches Access-Reject. |
| `tac-lock` | Allow any IMEI with same TAC (first 8 digits) as `sims.bound_imei`. |
| `grace-period` | Like `first-use`, but allows IMEI changes inside the `binding_grace_expires_at` window. |
| `soft` | Never reject — emit `imei.mismatch_detected` event, append to `imei_history`, set `binding_status='mismatch'`, then proceed to policy DSL. |

Pool-level checks run regardless of `binding_mode`: blacklist hit always rejects; greylist hit always emits `imei.blacklist_hit` / `imei.captured` events with elevated severity.

### Policy Engine Extensions (SVC-05)

The Policy DSL gains the `device.*` / `sim.binding_*` predicate namespace consumable inside `WHEN` blocks. Predicates are runtime-only (excluded from `MATCH → SQL` whitelist). See [architecture/DSL_GRAMMAR.md](architecture/DSL_GRAMMAR.md) §"Device Binding Examples (Phase 11)" for grammar and example rules.

### IMEI Pool Service (SVC-03 sub-component)

A lightweight CRUD + bulk-import service over TBL-56/57/58/59/60. Lives in `internal/api/imei_pool/`. Endpoints API-331..335. Bulk imports reuse SVC-09 (Job Runner) infrastructure originally built for STORY-013 — same progress endpoints, same error-row CSV emission. The IMEI Lookup endpoint (API-335) backs the Settings → IMEI Lookup tool used for forensic cross-reference (which list/which SIMs).

### Notification Service Extensions (SVC-08)

New EventTypes published by SVC-04 capture pipeline + binding pre-check:

| EventType | Severity | Trigger |
|-----------|----------|---------|
| `imei.captured` | info | First IMEI observation for a SIM (no prior history) |
| `imei.changed` | info / warning | Subsequent change observed; severity escalates with stricter `binding_mode` |
| `imei.mismatch_detected` | high | Strict-class mode (`strict`/`allowlist`/`first-use`/`tac-lock`) with mismatch — Access-Reject sent |
| `imei.grace_period_expired` | warning | `grace-period` window closed without re-verification |
| `imei.pool.exhausted_warning` | info | Pool size approaches operational threshold (capacity guard) |
| `imei.blacklist_hit` | high | IMEI matched in `imei_blacklist` — auth blocked |
| `device.binding_failed` | high | Generic policy reject due to `device.*` predicate |
| `device.binding_locked` | info | `first-use` mode auto-locked SIM to its first observed IMEI |
| `device.binding_re_paired` | info | Admin manually re-paired SIM to a new IMEI (audited workflow) |
| `device.binding_grace_change` | warning | IMEI changed during `grace-period` window — accepted with countdown |
| `device.binding_grace_expiring` | warning | `grace-period` countdown nearing zero (24h pre-expiry warning) |

### Audit Service Extensions (SVC-10)

New audit actions:
- SIM binding state: `sim.imei_captured`, `sim.binding_mode_changed`, `sim.binding_verified`, `sim.binding_mismatch`, `sim.binding_first_use_locked`, `sim.binding_soft_mismatch`, `sim.binding_blacklist_hit`, `sim.imei_repaired`, `sim.imei_unbound`
- IMEI pool: `imei_pool.entry_added`, `imei_pool.entry_removed`, `imei_pool.bulk_imported`
- Log forwarding: `log_forwarding.destination_added`, `log_forwarding.destination_updated`, `log_forwarding.destination_disabled`, `log_forwarding.destination_removed`

### Log Forwarding (Syslog) — SVC-08 sub-component

Native RFC 3164 / RFC 5424 emitter (`internal/notification/syslog/`) subscribes to canonical `bus.Envelope` events and forwards them to per-tenant configured SIEM destinations. Transports: UDP (RFC 3164 default), TCP, TLS (mutual auth optional via `tls_client_cert_pem` + `tls_client_key_pem`). Each destination carries a filter rule: event categories whitelist (`audit | alert | session | policy | system | aaa | binding`) + optional `min_severity` floor. Per-destination state (last delivery success / failure timestamp + last error string) is persisted and surfaced via API-337. Endpoints API-337/338. STORY-098.

### Out of Scope (v1)

EIR integration via Diameter S13 (4G/EPC) or 5G N17 is **out of scope** for v1 per ADR-004. No EIR client, no S13 stub, no N17 SBA mock, no AVP scaffolding. All enforcement is local; operator-EIR integration is a future-track item. Migration default: `binding_mode = NULL` for all existing SIM rows — opt-in only, zero risk to in-flight tenants (DEV-410).

## Extension Points (for FUTURE.md)

| Extension | Design Provision |
|-----------|-----------------|
| AI Anomaly Engine | Analytics service exposes raw CDR stream via NATS topic. Plugin interface for anomaly detectors. |
| Auto-SoR | SoR engine has pluggable strategy interface: RuleBased (v1), AIBased (future). |
| Digital Twin | Operator adapter has "simulation mode" flag. Policy engine supports "shadow evaluation" (evaluate without enforce). |
| Network Quality Scoring | Operator health logs (TBL-23) + CDR data provide training data. Scoring model pluggable via interface. |

## Reference ID Registry Summary

| Prefix | Count | Range |
|--------|-------|-------|
| SVC-NN | 10 | SVC-01 to SVC-10 |
| API-NNN | 269 | API-001 to API-338 (gaps absorbed by FIX/STORY backfill) |
| TBL-NN | 60 | TBL-01 to TBL-60 |
| CTN-NN | 5 | CTN-01 to CTN-05 |
| ADR-NNN | 4 | ADR-001 to ADR-004 |

## Architecture Decision Records

| ID | Title | Status |
|----|-------|--------|
| [ADR-001](adrs/ADR-001-modular-monolith.md) | Go Modular Monolith Architecture | Accepted |
| [ADR-002](adrs/ADR-002-database-stack.md) | PostgreSQL + TimescaleDB + Redis + NATS Data Stack | Accepted |
| [ADR-003](adrs/ADR-003-custom-aaa-engine.md) | Custom Go AAA Engine (Not FreeRADIUS) | Accepted |
| [ADR-004](adrs/ADR-004-imei-binding-architecture.md) | IMEI Binding Architecture — AAA-Side Local Enforcement | Accepted |

## Data Volume & Capacity Planning

See [flows/data-volumes.md](architecture/flows/data-volumes.md) for full analysis.

| Resource | Sizing |
|----------|--------|
| PostgreSQL disk | 500 GB SSD (200 GB compressed/year) |
| Redis memory | 16 GB (5 GB peak for 10M SIMs) |
| NATS disk | 10 GB JetStream |
| S3 cold storage | 1 TB/year (CDR + audit archives) |
| CDR volume | 30M-150M records/day |
| Auth throughput | 10K-15K req/sec peak |
| Concurrent sessions | up to 5M |

## Split Architecture Files

| Directory | Content |
|-----------|---------|
| [architecture/services/](architecture/services/_index.md) | Service definitions (SVC-01 to SVC-10) |
| [architecture/api/](architecture/api/_index.md) | API surface (269 endpoints + story links) |
| [architecture/db/](architecture/db/_index.md) | Database schema (60 tables) |
| [architecture/flows/](architecture/flows/_index.md) | Data flows (FLW-01 to FLW-07) |
| [architecture/flows/data-volumes.md](architecture/flows/data-volumes.md) | Capacity planning & data volume analysis |

## Supplementary Architecture Docs

| Document | Content |
|----------|---------|
| [architecture/MIDDLEWARE.md](architecture/MIDDLEWARE.md) | Chi router middleware chain order & specification |
| [architecture/ERROR_CODES.md](architecture/ERROR_CODES.md) | Complete error code catalog (38 codes, 12 domains) |
| [architecture/DSL_GRAMMAR.md](architecture/DSL_GRAMMAR.md) | Policy DSL formal EBNF grammar |
| [architecture/PROTOCOLS.md](architecture/PROTOCOLS.md) | RADIUS/Diameter/RadSec/5G SBA protocol details & attribute mappings |
| [architecture/ALGORITHMS.md](architecture/ALGORITHMS.md) | Key algorithms (IP alloc, hash chain, rate limit, anomaly, cost calc, rollout) |
| [architecture/WEBSOCKET_EVENTS.md](architecture/WEBSOCKET_EVENTS.md) | WebSocket event payload schemas (10 event types) |
| [architecture/TESTING.md](architecture/TESTING.md) | Testing strategy (Go testify, testcontainers, Vitest, Playwright) |
| [architecture/CONFIG.md](architecture/CONFIG.md) | Complete env var reference (all services) |
