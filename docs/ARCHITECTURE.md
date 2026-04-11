# Architecture — Argus

> APN & Subscriber Intelligence Platform
> Scale: Large (108 APIs, 24 tables, 10 services)
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
| Config | envconfig | Latest | Environment variable configuration |
| Testing | Go testing + testify | Latest | Unit + integration tests |
| Container | Docker + Compose | Latest | Deployment |
| Reverse Proxy | Nginx | Alpine | Static serving, reverse proxy, routing (TLS deferred to production story) |

## Docker Architecture

| Container | Image | Port | Purpose | Health Check |
|-----------|-------|------|---------|-------------|
| CTN-01 | nginx:alpine | 8084→80 | Reverse proxy, static SPA (HTTP; TLS deferred) | GET / |
| CTN-02 | argus:latest (custom) | 8080, 8081, 1812, 1813, 3868, 8443 | Go monolith | GET :8080/api/health |
| CTN-03 | timescale/timescaledb:latest-pg16 | 5432 | PostgreSQL + TimescaleDB | pg_isready |
| CTN-04 | redis:7-alpine | 6379 | Cache, rate limiting | redis-cli ping |
| CTN-05 | nats:latest | 4222, 8222 | Event bus, job queue | /healthz on :8222 |

### Networks
- `argus-net`: All containers on single bridge network

### Volumes
- `pgdata`: PostgreSQL data persistence
- `natsdata`: NATS JetStream persistence

### Environment Variables
See `.env.example` for complete list.

## Project Structure

```
argus/
├── cmd/
│   └── argus/
│       └── main.go              # Entry point — starts all listeners
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
│   │   └── ...
│   ├── aaa/                      # SVC-04: AAA engine
│   │   ├── radius/               # RADIUS server
│   │   ├── diameter/             # Diameter server
│   │   ├── sba/                  # 5G SBA proxy
│   │   ├── eap/                  # EAP-SIM/AKA handlers
│   │   ├── rattype/              # RAT type canonical enum & mapping
│   │   └── session/              # Session management
│   ├── policy/                   # SVC-05: Policy engine
│   │   ├── dsl/                  # DSL parser
│   │   ├── evaluator/            # Rule evaluation
│   │   └── rollout/              # Staged rollout
│   ├── operator/                 # SVC-06: Operator routing
│   │   ├── adapter/              # Pluggable adapters
│   │   ├── sor/                  # Steering of Roaming
│   │   ├── circuit/              # Circuit breaker
│   │   └── mock/                 # Mock simulator
│   ├── analytics/                # SVC-07: Analytics engine
│   │   ├── cdr/                  # CDR processing
│   │   ├── anomaly/              # Anomaly detection
│   │   ├── cost/                 # Cost optimization
│   │   └── metrics/              # Built-in observability
│   ├── notification/             # SVC-08: Notification service
│   ├── job/                      # SVC-09: Job runner
│   ├── audit/                    # SVC-10: Audit service
│   ├── model/                    # Domain models
│   ├── store/                    # Database access (PG)
│   ├── cache/                    # Redis cache layer
│   ├── bus/                      # NATS event bus
│   ├── auth/                     # JWT, 2FA, API key
│   ├── tenant/                   # Tenant context middleware
│   └── config/                   # Configuration
├── pkg/
│   └── dsl/                      # Public Policy DSL package
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
│   ├── docker-compose.prod.yml
│   └── nginx/
│       └── nginx.conf
├── infra/
│   ├── docker/
│   │   └── Dockerfile.argus      # Multi-stage Go+React build
│   ├── monitoring/
│   │   └── nats-check.sh         # NATS health probe
│   └── ...                       # postgres, redis, nats config
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
| /analytics | AnalyticsDashboardPage | JWT (analyst+) | DashboardLayout |
| /analytics/cost | CostAnalyticsPage | JWT (analyst+) | DashboardLayout |
| /jobs | JobListPage | JWT (sim_manager+) | DashboardLayout |
| /audit | AuditLogPage | JWT (tenant_admin+) | DashboardLayout |
| /settings/users | UserManagementPage | JWT (tenant_admin+) | DashboardLayout |
| /settings/api-keys | ApiKeyPage | JWT (tenant_admin+) | DashboardLayout |
| /settings/ip-pools | IpPoolPage | JWT (operator_manager+) | DashboardLayout |
| /settings/notifications | NotificationConfigPage | JWT (any) | DashboardLayout |
| /settings/system | SystemConfigPage | JWT (super_admin) | DashboardLayout |
| /system/health | SystemHealthPage | JWT (super_admin) | DashboardLayout |
| /system/tenants | TenantManagementPage | JWT (super_admin) | DashboardLayout |

### State Management
- **Zustand**: Auth state, UI preferences (dark/light mode), sidebar state, command palette
- **TanStack Query**: All server data (SIMs, APNs, policies, sessions, analytics)
- **React Hook Form + Zod**: All forms (SIM create, APN create, policy DSL editor, etc.)

## Security Architecture

### Authentication Flow
```
Login: POST /api/v1/auth/login → validate credentials → check 2FA
    → issue JWT (15min) + refresh token (7d, stored in TBL-03)
    → set refresh token as httpOnly cookie

API Request: Authorization: Bearer <jwt>
    → gateway middleware validates JWT
    → extracts tenant_id + user_id + role
    → injects into request context

Token Refresh: POST /api/v1/auth/refresh (httpOnly cookie)
    → validate refresh token against TBL-03
    → issue new JWT + rotate refresh token

API Key: X-API-Key: argus_<prefix>_<secret>
    → gateway looks up key_prefix in TBL-04
    → validates SHA-256(secret) == key_hash
    → checks scopes, rate limits, expiry
```

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
| Auth vector pre-fetch | Redis list | 5min | Auto-expire (TTL) |
| Rate limit counters | Redis | Sliding window | Auto-expire |
| SoR decision (per-SIM) | Redis | 1hr (configurable) | NATS on operator health change |
| Auth rate counters | Redis INCR | 5s | Auto-expire (TTL) |
| Auth latency window | Redis ZSET | 120s | Auto-expire + 60s sliding prune |
| Dashboard aggregates | TimescaleDB continuous agg | 1hr | Auto-refresh |
| Diagnostic result (per-SIM) | Redis | 1min | Auto-expire (TTL) |

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
| API-NNN | 108 | API-001 to API-182 |
| TBL-NN | 26 | TBL-01 to TBL-26 |
| CTN-NN | 5 | CTN-01 to CTN-05 |
| ADR-NNN | 3 | ADR-001 to ADR-003 |

## Architecture Decision Records

| ID | Title | Status |
|----|-------|--------|
| [ADR-001](adrs/ADR-001-modular-monolith.md) | Go Modular Monolith Architecture | Accepted |
| [ADR-002](adrs/ADR-002-database-stack.md) | PostgreSQL + TimescaleDB + Redis + NATS Data Stack | Accepted |
| [ADR-003](adrs/ADR-003-custom-aaa-engine.md) | Custom Go AAA Engine (Not FreeRADIUS) | Accepted |

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
| [architecture/api/](architecture/api/_index.md) | API surface (108 endpoints + story links) |
| [architecture/db/](architecture/db/_index.md) | Database schema (26 tables) |
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
