# Middleware Chain Specification — Argus

> Chi router (v5) middleware chain for all HTTP API requests.
> Order is critical — middleware executes top-to-bottom on request, bottom-to-top on response.

## Chain Order

```
HTTP Request
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│ 1. Recovery (panic handler)                              │
│    Catches panics in any downstream handler.             │
│    Logs stack trace via zerolog.                          │
│    Returns 500 with standard error envelope.             │
│    NEVER exposes panic details to client.                │
├─────────────────────────────────────────────────────────┤
│ 2. RequestID (correlation_id)                            │
│    Generates UUID v4 for every request.                  │
│    Sets X-Request-ID response header.                    │
│    Injects correlation_id into context:                  │
│      ctx = context.WithValue(ctx, CorrelationIDKey, id)  │
│    All downstream logs include this ID.                  │
│    All audit entries reference this ID.                  │
├─────────────────────────────────────────────────────────┤
│ 3. Logging (zerolog)                                     │
│    Logs request start: method, path, remote_addr.        │
│    Logs request end: status, duration_ms, bytes_written. │
│    Structured JSON: {"level":"info","correlation_id":    │
│      "abc-123","method":"GET","path":"/api/v1/sims",    │
│      "status":200,"duration_ms":12,"bytes":4521}         │
│    Log level per-route configurable (e.g., /health=debug)│
├─────────────────────────────────────────────────────────┤
│ 4. CORS                                                  │
│    Allowed origins: per-tenant domain list + localhost    │
│      in dev mode.                                        │
│    Allowed methods: GET, POST, PUT, PATCH, DELETE,       │
│      OPTIONS.                                            │
│    Allowed headers: Authorization, Content-Type,         │
│      X-API-Key, X-Request-ID.                            │
│    Exposed headers: X-Request-ID, X-RateLimit-Remaining, │
│      X-RateLimit-Reset, Retry-After.                     │
│    Max age: 86400 (24h).                                 │
│    Credentials: true (for httpOnly refresh token cookie).│
│    OPTIONS requests return 204 immediately (short-       │
│      circuit, no further middleware).                     │
├─────────────────────────────────────────────────────────┤
│ 5. RateLimiter (Redis-backed)                            │
│    Identifies caller: tenant_id + api_key_id (M2M) or   │
│      tenant_id + user_id (portal) or IP (unauthenticated)│
│    Algorithm: sliding window counter in Redis.           │
│    Key format: ratelimit:{identifier}:{endpoint}:{window}│
│    Limits checked (first match):                         │
│      1. API key-specific limit (from TBL-04)             │
│      2. Tenant-specific limit (from TBL-01)              │
│      3. Global default (env: RATE_LIMIT_DEFAULT_PER_MIN) │
│    On limit exceeded:                                    │
│      HTTP 429 + Retry-After header (seconds until reset) │
│      Error code: RATE_LIMITED                            │
│      Body: standard error envelope                       │
│    Response headers always set:                          │
│      X-RateLimit-Limit: 1000                             │
│      X-RateLimit-Remaining: 742                          │
│      X-RateLimit-Reset: 1710770520 (Unix timestamp)      │
│    Note: /api/health is exempt from rate limiting.       │
├─────────────────────────────────────────────────────────┤
│ 6. Auth (JWT or API Key)                                 │
│    Two authentication strategies (checked in order):     │
│                                                          │
│    A. JWT (portal users):                                │
│       Header: Authorization: Bearer <jwt>                │
│       Validates: signature (HS256, JWT_SECRET), expiry,  │
│         issuer ("argus").                                 │
│       Extracts claims: tenant_id, user_id, role.         │
│       On invalid/expired: 401 TOKEN_EXPIRED or           │
│         INVALID_CREDENTIALS.                             │
│                                                          │
│    B. API Key (M2M):                                     │
│       Header: X-API-Key: argus_<prefix>_<secret>         │
│       Looks up key_prefix in TBL-04 (api_keys).          │
│       Validates: SHA-256(secret) == key_hash.            │
│       Checks: not revoked, not expired, tenant active.   │
│       Extracts: tenant_id, api_key_id, scopes.           │
│       On invalid: 401 INVALID_CREDENTIALS.               │
│                                                          │
│    Public routes (no auth required):                     │
│      POST /api/v1/auth/login                             │
│      POST /api/v1/auth/refresh                           │
│      GET  /api/health                                    │
│                                                          │
│    Injects into context:                                 │
│      ctx.Set("auth_type", "jwt" | "api_key")             │
│      ctx.Set("tenant_id", uuid)                          │
│      ctx.Set("user_id", uuid)      // jwt only           │
│      ctx.Set("role", string)        // jwt only           │
│      ctx.Set("api_key_id", uuid)    // api_key only       │
│      ctx.Set("scopes", []string)    // api_key only       │
├─────────────────────────────────────────────────────────┤
│ 7. TenantContext                                         │
│    Reads tenant_id from auth context (set by step 6).    │
│    Loads tenant config from Redis cache (TBL-01):        │
│      - state (active, suspended, disabled)               │
│      - resource_limits (max_sims, max_apns, max_users)   │
│      - settings (rate limits, purge retention, etc.)     │
│    If tenant suspended/disabled: 403 TENANT_SUSPENDED.   │
│    Injects full tenant config into context:              │
│      ctx.Set("tenant", TenantConfig{...})                │
│    ALL downstream database queries MUST include          │
│      WHERE tenant_id = ? — enforced at store layer.      │
├─────────────────────────────────────────────────────────┤
│ 8. RBAC (Role-Based Access Control)                      │
│    Route → required minimum role mapping defined in      │
│      route registration (per-endpoint).                  │
│    Role hierarchy (highest to lowest):                   │
│      super_admin > tenant_admin > operator_manager >     │
│      sim_manager > policy_editor > analyst > api_user    │
│    Check: user.role >= route.required_role.              │
│    API key: checks scope list against endpoint scope.    │
│      e.g., scope "sims:read" allows GET /api/v1/sims    │
│      but not POST /api/v1/sims.                          │
│    On insufficient: 403 FORBIDDEN or INSUFFICIENT_ROLE   │
│      or SCOPE_DENIED.                                    │
│    super_admin bypasses tenant_id scoping for cross-     │
│      tenant operations (e.g., GET /api/v1/tenants).      │
├─────────────────────────────────────────────────────────┤
│ 9. Handler                                               │
│    The actual endpoint handler function.                  │
│    Has access to all context values set above.           │
│    Returns standard response envelope.                   │
└─────────────────────────────────────────────────────────┘
    │
    ▼
HTTP Response
```

## Error Propagation

Every middleware that can reject a request returns the **standard error envelope** and short-circuits (no further middleware or handler executes):

```json
{
  "status": "error",
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable description",
    "details": []
  }
}
```

Middleware error responses by layer:

| Middleware | HTTP Status | Error Code | When |
|-----------|-------------|------------|------|
| Recovery | 500 | INTERNAL_ERROR | Panic caught |
| RateLimiter | 429 | RATE_LIMITED | Limit exceeded |
| Auth | 401 | INVALID_CREDENTIALS | No/invalid token or key |
| Auth | 401 | TOKEN_EXPIRED | JWT expired |
| Auth | 401 | INVALID_REFRESH_TOKEN | Bad refresh token |
| TenantContext | 403 | TENANT_SUSPENDED | Tenant not active |
| RBAC | 403 | FORBIDDEN | Role check failed |
| RBAC | 403 | INSUFFICIENT_ROLE | Role too low |
| RBAC | 403 | SCOPE_DENIED | API key missing scope |

## Implementation Pattern

```go
func NewRouter(deps Dependencies) *chi.Mux {
    r := chi.NewRouter()

    // Global middleware — applied to ALL routes
    r.Use(middleware.Recoverer(deps.Logger))
    r.Use(middleware.RequestID())
    r.Use(middleware.Logger(deps.Logger))
    r.Use(middleware.CORS(deps.Config.CORS))
    r.Use(middleware.RateLimiter(deps.Redis, deps.Config.RateLimit))

    // Public routes (no auth)
    r.Group(func(r chi.Router) {
        r.Post("/api/v1/auth/login", authHandler.Login)
        r.Post("/api/v1/auth/refresh", authHandler.Refresh)
        r.Get("/api/health", healthHandler.Check)
    })

    // Authenticated routes
    r.Group(func(r chi.Router) {
        r.Use(middleware.Auth(deps.JWTSecret, deps.APIKeyStore))
        r.Use(middleware.TenantContext(deps.TenantCache))
        r.Use(middleware.RBAC())

        // Routes registered with required role annotation
        r.With(rbac.Require("sim_manager")).Get("/api/v1/sims", simHandler.List)
        r.With(rbac.Require("tenant_admin")).Post("/api/v1/sims", simHandler.Create)
        // ... etc
    })

    return r
}
```

## Context Keys

All context values use typed keys to avoid collisions:

| Key | Type | Set By | Used By |
|-----|------|--------|---------|
| `correlation_id` | `string` (UUID) | RequestID | Logger, AuditLog, all handlers |
| `auth_type` | `string` | Auth | RBAC, handlers |
| `tenant_id` | `uuid.UUID` | Auth | TenantContext, RBAC, all handlers, store layer |
| `user_id` | `uuid.UUID` | Auth (JWT) | RBAC, AuditLog, handlers |
| `role` | `string` | Auth (JWT) | RBAC |
| `api_key_id` | `uuid.UUID` | Auth (API Key) | RBAC, AuditLog, RateLimiter |
| `scopes` | `[]string` | Auth (API Key) | RBAC |
| `tenant` | `TenantConfig` | TenantContext | Handlers (resource limit checks) |

## Request Lifecycle Example

```
POST /api/v1/sims (create SIM)

1. Recovery:     installed
2. RequestID:    generated "f47ac10b-58cc-4372-a567-0e02b2c3d479"
3. Logging:      log {"method":"POST","path":"/api/v1/sims","correlation_id":"f47ac10b..."}
4. CORS:         origin "https://argus.example.com" allowed
5. RateLimiter:  key "ratelimit:tenant-abc:user-123:POST/api/v1/sims:1710770400"
                 INCR → 42 (limit 1000) → pass
6. Auth:         Bearer token → valid JWT → tenant_id=abc, user_id=123, role=sim_manager
7. TenantContext: tenant "abc" → state=active, max_sims=1000000 → loaded
8. RBAC:         route requires sim_manager, user is sim_manager → pass
9. Handler:      create SIM → check resource limit (current: 500K < max: 1M) → insert → 201
10. Logging:     log {"status":201,"duration_ms":23,"bytes":512,"correlation_id":"f47ac10b..."}
```

## Notes

- **Health check** (`/api/health`) bypasses auth, tenant, and RBAC middleware entirely. It is only subject to Recovery, RequestID, Logging, and CORS.
- **WebSocket upgrade** (`/ws/v1/events`) uses the same Auth middleware but authenticates via JWT passed as query parameter `?token=<jwt>` or as the first WebSocket message. After upgrade, no further HTTP middleware applies.
- **AAA protocol endpoints** (RADIUS :1812/:1813, Diameter :3868, SBA :8443) do NOT use this HTTP middleware chain. They have their own protocol-specific listeners with shared-secret or certificate-based authentication.
