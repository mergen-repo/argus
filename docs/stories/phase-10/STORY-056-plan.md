# STORY-056: Critical Runtime Fixes — Implementation Plan

## Story
As a platform operator, I want every page to load without crashing and every listed endpoint to respond correctly, so that Argus is usable end-to-end in production.

## Architecture Context

### Services Involved
- **SVC-01 (API Gateway)**: `internal/gateway/router.go` — Chi router, all route registration, JWT+RBAC middleware chain
- **SVC-02 (WebSocket)**: `internal/ws/` — gorilla/websocket server on :8081, proxied through Nginx
- **SVC-03 (Core API)**: `internal/api/*` — CRUD handlers for all domain entities
- **SVC-08 (Notification)**: `internal/api/notification/handler.go` — unread count, mark read, configs

### API Endpoints (affected by this story)

| Endpoint | Method | Handler | Status |
|----------|--------|---------|--------|
| `/api/v1/sessions` | GET | `sessionapi.Handler.List` | Exists, returns 500 — wiring/dependency issue |
| `/api/v1/audit` | GET | `auditapi.Handler.List` | Registered at router.go:183, verify E2E |
| `/api/v1/notifications/unread-count` | GET | `notifapi.Handler.UnreadCount` | Registered at router.go:469, verify E2E |
| `/api/v1/ip-pools` | GET | `ippoolapi.Handler.List` | Works, but response fields mismatch frontend types |
| `/api/v1/tenants` | GET | `tenantapi.Handler.List` | Works, but response fields may mismatch frontend types |

### Standard API Response Envelope
```json
{
  "status": "success",
  "data": { ... },
  "meta": { "cursor": "abc123", "limit": 50, "has_more": true }
}
```

### IP Pool Backend Response DTO (from `internal/api/ippool/handler.go:45-61`)
```go
type poolResponse struct {
    ID                      string  `json:"id"`
    TenantID                string  `json:"tenant_id"`
    APNID                   string  `json:"apn_id"`
    Name                    string  `json:"name"`
    CIDRv4                  *string `json:"cidr_v4"`
    CIDRv6                  *string `json:"cidr_v6"`
    TotalAddresses          int     `json:"total_addresses"`
    UsedAddresses           int     `json:"used_addresses"`
    AvailableAddresses      int     `json:"available_addresses"`
    UtilizationPct          float64 `json:"utilization_pct"`
    AlertThresholdWarning   int     `json:"alert_threshold_warning"`
    AlertThresholdCritical  int     `json:"alert_threshold_critical"`
    ReclaimGracePeriodDays  int     `json:"reclaim_grace_period_days"`
    State                   string  `json:"state"`
    CreatedAt               string  `json:"created_at"`
}
```

### Current Frontend IpPool Type (from `web/src/types/settings.ts:36-46`)
```ts
export interface IpPool {
  id: string
  tenant_id: string
  apn_id?: string
  name: string
  cidr: string              // BUG: backend sends cidr_v4/cidr_v6
  total_addresses: number
  used_addresses: number
  available_addresses: number
  created_at: string
  // MISSING: utilization_pct, state, alert_threshold_warning,
  //          alert_threshold_critical, reclaim_grace_period_days
}
```

### Current Frontend Tenant Type (from `web/src/types/settings.ts:111-124`)
```ts
export interface Tenant {
  id: string
  name: string
  slug: string
  plan?: string
  sim_count: number
  user_count: number
  retention_days: number
  max_sims: number
  max_users: number
  max_api_keys: number
  created_at: string
  updated_at: string
}
```

### Nginx Config (active: `infra/nginx/nginx.conf`)
- Listens on port 80 only (mapped to host 8084 via docker-compose)
- No `listen 443 ssl` block
- WebSocket proxy block at `location /ws/` already configured with `proxy_http_version 1.1`, `Upgrade`/`Connection` headers, `86400s` read timeout
- docker-compose.yml mounts from `../infra/nginx/nginx.conf`

### Docker Architecture
- `deploy/docker-compose.yml` — orchestrates all services
- `deploy/Dockerfile` — multi-stage Go+React build (to be relocated to `infra/docker/Dockerfile.argus`)
- No `.dockerignore` at root (context size bloated)
- NATS uses distroless image — no shell for healthcheck

### ErrorBoundary Current State
- `web/src/components/error-boundary.tsx` — class component with Try Again button
- `web/src/components/layout/dashboard-layout.tsx:60` — already uses `<ErrorBoundary key={location.pathname}>` (route-change reset already wired)

### Screen References
| ID | Screen | Route | Relevance |
|----|--------|-------|-----------|
| SCR-112 | IP Pools | `/settings/ip-pools` | AC-1: type mismatch crash |
| SCR-121 | Tenant Management | `/system/tenants` | AC-2: field mismatch |
| SCR-050 | Live Sessions | `/sessions` | AC-3: handler 500 |
| SCR-090 | Audit Log | `/audit` | AC-4: route verification |
| SCR-030 | APN List | `/apns` | AC-5: dialog state |
| SCR-010 | Dashboard | `/` | AC-7: notification badge |
| All SCR-* | All Pages | All routes | AC-8: ErrorBoundary |

### Design Token Map (for UI tasks)
- Card: `bg-bg-surface`, `border-border`, `rounded-[var(--radius-md)]`
- Text primary: `text-text-primary`, secondary: `text-text-secondary`, tertiary: `text-text-tertiary`
- Mono data: `font-mono text-xs`
- Accent: `text-accent`, `bg-accent-dim`
- Success/Warning/Danger: `text-success/warning/danger`, `bg-success-dim/warning-dim/danger-dim`
- Badge: 10px uppercase tracking, use `<Badge>` component
- Status bar: 2px accent line at card bottom

## Bug Patterns (derived from this story's bugs)

| Pattern | Description | Occurrences |
|---------|-------------|-------------|
| BP-1: Frontend/Backend DTO mismatch | Frontend TypeScript type has different field names than Go JSON struct tags | AC-1 (`cidr` vs `cidr_v4`/`cidr_v6`), AC-2 (tenant nullable fields) |
| BP-2: Missing static assets | Browser 404s on `favicon.ico` because file was never created | AC-9 |
| BP-3: Stale doc references after infra changes | Docs still reference `https://` after port was switched to HTTP-only | AC-10 |
| BP-4: Handler wiring gap | Handler code exists but its dependencies (session manager, store) may be nil at runtime causing 500 | AC-3 |
| BP-5: Missing DevOps artifacts | `.dockerignore` absent, Dockerfile in non-standard path | AC-12, AC-13 |

---

## Tasks

### Wave 1: Backend Fixes (no frontend dependencies)

---

#### Task 1: Fix Sessions API 500 — Root-cause and repair handler wiring

**What:** `GET /api/v1/sessions` returns 500. The handler code in `internal/api/session/handler.go` looks syntactically correct. The 500 likely originates from `sessionMgr.ListActive()` — either the session manager is nil, its Redis dependency is misconfigured, or the store layer errors. Root-cause by tracing the dependency chain from `cmd/argus/main.go` through the session handler construction.

**Files:**
- `cmd/argus/main.go` (read, possibly edit — check session handler wiring)
- `internal/api/session/handler.go` (verify, possibly fix)
- `internal/aaa/session/manager.go` (read — check ListActive implementation)

**Depends on:** None
**Complexity:** M
**Context refs:** Architecture Context > API Endpoints, Architecture Context > Services Involved (SVC-01, SVC-03)

**Verify:**
- `curl -H "Authorization: Bearer $TOKEN" http://localhost:8084/api/v1/sessions?limit=10` returns 200
- Response follows standard envelope with `data` array and `meta` object
- SIM detail Sessions tab loads without error

---

#### Task 2: Verify Audit Route Registration — Confirm E2E 200

**What:** `GET /api/v1/audit` is registered at `router.go:183` pointing to `AuditHandler.List`. The 404 may have been from a previous state or a frontend URL mismatch. Verify the route works end-to-end. If the frontend hits a different path (e.g., `/api/v1/audit-logs`), update the frontend API client to use `/api/v1/audit` as canonical path.

**Files:**
- `internal/gateway/router.go` (read — confirm line 183)
- `web/src/hooks/use-audit.ts` or similar (find and verify API path matches `/api/v1/audit`)
- `web/src/pages/audit/index.tsx` (verify page loads)

**Depends on:** None
**Complexity:** S
**Context refs:** Architecture Context > API Endpoints

**Verify:**
- `curl -H "Authorization: Bearer $TOKEN" http://localhost:8084/api/v1/audit?limit=10` returns 200
- Audit Log page at `/audit` renders entries

---

#### Task 3: Verify Notifications Unread Count Endpoint

**What:** `GET /api/v1/notifications/unread-count` is registered at `router.go:469` with handler `UnreadCount()`. Frontend hook `useUnreadCount` in `web/src/hooks/use-notifications.ts` already calls this path. The topbar badge at `web/src/components/layout/topbar.tsx` displays `unreadCount` from the notification store. Verify the full chain works. Ensure the TanStack Query `staleTime: 30_000` satisfies the AC-7 "cache TTL 30s" requirement.

**Files:**
- `internal/api/notification/handler.go` (read — verify UnreadCount handler)
- `web/src/hooks/use-notifications.ts` (verify query path and staleTime)
- `web/src/components/layout/topbar.tsx` (verify badge rendering)

**Depends on:** None
**Complexity:** S
**Context refs:** Architecture Context > API Endpoints, Architecture Context > Services Involved (SVC-08)

**Verify:**
- `curl -H "Authorization: Bearer $TOKEN" http://localhost:8084/api/v1/notifications/unread-count` returns `{"status":"success","data":{"count":N}}`
- Header bell icon shows correct count
- Count updates within 30s of new notification

---

### Wave 2: Frontend Type Fixes

---

#### Task 4: Fix IP Pools Frontend Type Mismatch (AC-1)

**What:** The `IpPool` TypeScript interface has `cidr: string` but the backend sends `cidr_v4: string | null` and `cidr_v6: string | null`. The type also lacks `utilization_pct`, `state`, `alert_threshold_warning`, `alert_threshold_critical`, `reclaim_grace_period_days`. The `ip-pools.tsx` page reads `pool.cidr` on line 48 which is `undefined`, causing the utilization display to fail.

**Step-by-step:**
1. Update `IpPool` interface in `web/src/types/settings.ts` to match backend `poolResponse` exactly
2. Update `ip-pools.tsx` — replace `pool.cidr` with `pool.cidr_v4 || pool.cidr_v6 || ''`
3. Update `ip-pool-detail.tsx` — same field alignment
4. Verify compilation: `npx tsc --noEmit`

**Files:**
- `web/src/types/settings.ts` (edit IpPool interface)
- `web/src/pages/settings/ip-pools.tsx` (edit field references)
- `web/src/pages/settings/ip-pool-detail.tsx` (edit field references)

**Depends on:** None
**Complexity:** M
**Context refs:** Architecture Context > IP Pool Backend Response DTO, Architecture Context > Current Frontend IpPool Type, Design Token Map
**Pattern ref:** `web/src/types/settings.ts` (existing file, extend IpPool interface)

**Verify:**
- `npx tsc --noEmit` passes
- Navigate to `/settings/ip-pools` — page renders without crash
- Utilization bars visible with correct percentages
- Pool CIDR shown correctly (v4 or v6)

---

#### Task 5: Fix Tenants Page Field Mismatch (AC-2)

**What:** The `Tenant` interface in `web/src/types/settings.ts` must exactly match the backend tenant handler's response DTO. Read `internal/api/tenant/handler.go` first to identify the actual JSON fields returned. Compare with the frontend `Tenant` type. Fix any mismatches. Ensure nullable fields (like `plan`) are handled with `??` or default values in the page component. The `tenants.tsx` page accesses `tenant.slug`, `tenant.sim_count`, `tenant.user_count`, `tenant.retention_days`, `tenant.max_api_keys` — all must exist in the API response.

**Step-by-step:**
1. Read `internal/api/tenant/handler.go` — identify the exact response DTO struct and JSON tags
2. Diff backend DTO against `Tenant` type in `web/src/types/settings.ts`
3. Update the TypeScript interface to match
4. Update `tenants.tsx` to handle nullable fields safely (e.g., `tenant.plan ?? 'standard'` already done on line 101/209)
5. Verify compilation

**Files:**
- `internal/api/tenant/handler.go` (read only)
- `web/src/types/settings.ts` (edit Tenant interface if needed)
- `web/src/pages/system/tenants.tsx` (edit if needed for field safety)

**Depends on:** None
**Complexity:** M
**Context refs:** Architecture Context > Current Frontend Tenant Type, Design Token Map

**Verify:**
- `npx tsc --noEmit` passes
- Navigate to `/system/tenants` — page renders without crash
- Create/edit dialogs functional
- All table columns display data correctly

---

#### Task 6: Fix APN Create Dialog State (AC-5)

**What:** The "Create APN" button on the APN List page must reliably open the create dialog. `CreateAPNDialog` component exists at `web/src/pages/apns/index.tsx:42` using `SlidePanel`. Read the full parent component (lines 100+) to verify the `useState` for `showCreate` and the button's `onClick` handler. The bug may be a race condition, missing state variable, or the button being outside the component's render scope. Fix: ensure `open` prop flows from parent state, button click sets state to true, dialog close callback resets to false + refreshes list.

**Step-by-step:**
1. Read full `web/src/pages/apns/index.tsx` (especially the parent component and button wiring)
2. Identify the bug (likely state variable name mismatch or missing `onClick`)
3. Fix state wiring: button click -> open dialog -> submit -> close + invalidate query -> refresh list
4. Verify compilation

**Files:**
- `web/src/pages/apns/index.tsx` (read full, edit as needed)

**Depends on:** None
**Complexity:** S
**Context refs:** Architecture Context > Screen References (SCR-030)

**Verify:**
- Navigate to `/apns`
- Click "Create APN" button — dialog opens immediately
- Fill form and submit — dialog closes, list refreshes with new APN
- Re-open dialog — form is reset

---

### Wave 3: Infrastructure & DevOps

---

#### Task 7: Verify WebSocket Nginx Proxy (AC-6)

**What:** The WebSocket proxy block in `infra/nginx/nginx.conf:125-133` already has all required directives: `proxy_http_version 1.1`, `Upgrade`/`Connection` headers, `86400s` read timeout. The frontend WS client at `web/src/lib/ws.ts` constructs the URL using `window.location.host` + `/ws/v1/events`. Verify:
1. The `upstream websocket` block at line 82 points to `argus:8081` (correct)
2. The WS path matches: Nginx `location /ws/` proxies to `http://websocket/ws/` — frontend connects to `/ws/v1/events`
3. No `wss://` used when port is HTTP (the client already uses protocol detection on line 14)

If verification passes, this is a no-op. If there is a subtle path mismatch (e.g., trailing slash), fix it.

**Files:**
- `infra/nginx/nginx.conf` (verify, possibly edit)
- `web/src/lib/ws.ts` (verify only)

**Depends on:** None
**Complexity:** S
**Context refs:** Architecture Context > Nginx Config

**Verify:**
- Open browser DevTools WS panel — connection established to `ws://localhost:8084/ws/v1/events`
- No reconnect loop in console
- Clean of `connection refused` errors

---

#### Task 8: Add Favicon (AC-9)

**What:** `web/public/favicon.ico` does not exist. Browser requests it on every page load, resulting in a 404. Create a minimal favicon. Options:
1. Generate a simple 16x16/32x32 `.ico` with the Argus brand color (`#00D4FF`) and letter "A"
2. Or use a minimal SVG favicon referenced in `web/index.html`

Check `web/index.html` for existing `<link rel="icon">` tags. Add one if missing.

**Files:**
- `web/public/favicon.ico` (create — binary file, generate with tool)
- `web/index.html` (verify/add `<link rel="icon">`)

**Depends on:** None
**Complexity:** S
**Context refs:** Design Token Map (accent color `#00D4FF`)
**Pattern ref:** `web/public/` directory (existing static assets location)

**Verify:**
- `ls web/public/favicon.ico` — file exists
- Browser Network panel — `favicon.ico` returns 200
- No 404 errors in console

---

#### Task 9: Fix HTTPS References to HTTP (AC-10)

**What:** HTTP on port 8084 is the canonical dev/staging entry point. No TLS in dev. Update all doc references from `https://localhost:8084` (or `https://localhost`) to `http://localhost:8084`. Also update `CLAUDE.md` Admin Access section URL. Architecture diagram's CTN-01 label shows `:443/:80` — update to show `:80` only (mapped to host `:8084`).

**Files to grep and update:**
- `CLAUDE.md` (Admin Access URL)
- `docs/ARCHITECTURE.md` (CTN-01 label, Docker Architecture table)
- `docs/architecture/services/_index.md` (if references exist)
- `Makefile` (line 67: `echo "Baslatildi: https://localhost:8084"`)

**Depends on:** None
**Complexity:** S
**Context refs:** Architecture Context > Nginx Config

**Verify:**
- `grep -r "https://localhost" CLAUDE.md docs/ARCHITECTURE.md Makefile` returns no matches
- `curl -I http://localhost:8084` returns 200, no redirect to HTTPS

---

#### Task 10: Add .dockerignore (AC-12)

**What:** No `.dockerignore` exists at the project root. Docker build context includes everything (~243MB). Create a `.dockerignore` with the exclusions specified in AC-12.

**Exclusions:**
```
node_modules
web/dist
vendor
pgdata
natsdata
redisdata
.git
coverage
tmp
*.log
docs/output
.playwright-mcp
.claude
.env*
backups
.idea
.vscode
bin
*.pem
*.key
!deploy/nginx/ssl
```

**Files:**
- `.dockerignore` (create)

**Depends on:** None
**Complexity:** S
**Context refs:** Architecture Context > Docker Architecture
**Pattern ref:** `.gitignore` (existing ignore file, similar syntax)

**Verify:**
- `ls .dockerignore` — file exists
- `docker build` context size < 50 MB (measure with `docker build --no-cache -t argus-test -f deploy/Dockerfile . 2>&1 | head -5`)

---

#### Task 11: NATS Container Healthcheck (AC-11)

**What:** NATS uses distroless image (no shell, no wget/curl). The docker-compose.yml already has a comment at line 119 documenting this limitation. Choose the `service_started` + external monitor approach:
1. Confirm `docker-compose.yml` uses `condition: service_started` for NATS dependency (already the case at line 50)
2. Create `infra/monitoring/nats-check.sh` — a host-side script that probes `http://localhost:8222/healthz`
3. Document the approach in `docs/reports/infra-tuning.md` as an addendum

**Files:**
- `deploy/docker-compose.yml` (verify only — already uses `service_started`)
- `infra/monitoring/nats-check.sh` (create)
- `docs/reports/infra-tuning.md` (append addendum)

**Depends on:** None
**Complexity:** S
**Context refs:** Architecture Context > Docker Architecture
**Pattern ref:** `infra/redis/redis.conf` (existing infra config file)

**Verify:**
- `docker compose ps` shows NATS running (no healthcheck column, but container up)
- `./infra/monitoring/nats-check.sh` returns healthy status
- Documented in infra-tuning.md addendum

---

### Wave 4: Dockerfile Relocation (widest blast radius)

---

#### Task 12: Relocate Dockerfile and Update Build References (AC-13)

**What:** Move `deploy/Dockerfile` to `infra/docker/Dockerfile.argus` per DevOps standard layout. Update all references:
1. `deploy/docker-compose.yml` → change `dockerfile: deploy/Dockerfile` to `dockerfile: infra/docker/Dockerfile.argus`
2. `Makefile` → verify `make build` and `make up` still work (they use `docker compose -f deploy/docker-compose.yml build` which reads `build.dockerfile` from compose)
3. Remove old `deploy/Dockerfile`

**Step-by-step:**
1. Create directory `infra/docker/`
2. Copy `deploy/Dockerfile` to `infra/docker/Dockerfile.argus`
3. Edit `deploy/docker-compose.yml` line 35: change `dockerfile: deploy/Dockerfile` to `dockerfile: infra/docker/Dockerfile.argus`
4. Verify `docker compose -f deploy/docker-compose.yml config` validates
5. Remove `deploy/Dockerfile`
6. Verify `make build` succeeds

**Files:**
- `deploy/Dockerfile` (read, then delete)
- `infra/docker/Dockerfile.argus` (create — copy of Dockerfile)
- `deploy/docker-compose.yml` (edit line 35)

**Depends on:** Task 10 (.dockerignore should exist first)
**Complexity:** M
**Context refs:** Architecture Context > Docker Architecture
**Pattern ref:** `deploy/Dockerfile` (the file being relocated)

**Verify:**
- `ls infra/docker/Dockerfile.argus` — file exists
- `ls deploy/Dockerfile` — file gone
- `docker compose -f deploy/docker-compose.yml config` validates
- `make build` succeeds
- `make up` succeeds

---

### Wave 5: Verification & Error Boundary (AC-8)

---

#### Task 13: Verify ErrorBoundary Route-Reset Behavior (AC-8)

**What:** `dashboard-layout.tsx:60` already uses `<ErrorBoundary key={location.pathname}>`. This means React will unmount and remount the ErrorBoundary whenever the pathname changes, clearing any error state. The "Try Again" button calls `handleRetry` which resets state. Both mechanisms are already implemented. Verify this works correctly:
1. Confirm `key={location.pathname}` is on the ErrorBoundary wrapping `<Outlet />`
2. Verify that route-level `lazySuspense()` wrapper in `router.tsx` also uses ErrorBoundary (it does, line 69)
3. Manual test: force a component error, navigate away, confirm new route renders

**Files:**
- `web/src/components/layout/dashboard-layout.tsx` (verify only)
- `web/src/components/error-boundary.tsx` (verify only)
- `web/src/router.tsx` (verify only)

**Depends on:** Tasks 4, 5, 6 (pages must load without crash first)
**Complexity:** S
**Context refs:** Architecture Context > ErrorBoundary Current State

**Verify:**
- Force error on one route (e.g., temporarily throw in a component)
- Navigate to different route — new route renders cleanly
- No stuck error screen
- "Try Again" button still works on error screen

---

## Acceptance Criteria Mapping

| AC | Task(s) | Type |
|----|---------|------|
| AC-1 (IP Pools page) | Task 4 | Fix — type mismatch |
| AC-2 (Tenants page) | Task 5 | Fix — field mismatch |
| AC-3 (Sessions 500) | Task 1 | Fix — handler wiring |
| AC-4 (Audit route) | Task 2 | Verify — route exists |
| AC-5 (APN dialog) | Task 6 | Fix — dialog state |
| AC-6 (WebSocket proxy) | Task 7 | Verify — config exists |
| AC-7 (Unread count) | Task 3 | Verify — endpoint + badge |
| AC-8 (ErrorBoundary) | Task 13 | Verify — already implemented |
| AC-9 (Favicon) | Task 8 | Create — missing asset |
| AC-10 (HTTP refs) | Task 9 | Fix — stale docs |
| AC-11 (NATS health) | Task 11 | Create — external probe |
| AC-12 (.dockerignore) | Task 10 | Create — missing file |
| AC-13 (Dockerfile reloc) | Task 12 | Move — DevOps layout |

## Test Scenarios Mapping

| Test | Task | Method |
|------|------|--------|
| E2E: `/settings/ip-pools` renders | Task 4 | Browser nav + DevTools |
| E2E: `/system/tenants` renders | Task 5 | Browser nav + DevTools |
| Integration: `GET /api/v1/sessions?limit=10` → 200 | Task 1 | curl |
| Integration: `GET /api/v1/audit?limit=10` → 200 | Task 2 | curl |
| E2E: "Create APN" dialog opens | Task 6 | Browser click test |
| E2E: WS connection established | Task 7 | Browser DevTools WS panel |
| Integration: `GET /api/v1/notifications/unread-count` → 200 | Task 3 | curl |
| E2E: ErrorBoundary resets on nav | Task 13 | Manual force-error test |
| E2E: `favicon.ico` → 200 | Task 8 | Browser Network panel |
| E2E: `curl -I http://localhost:8084` → 200, no redirect | Task 9 | curl |
| Ops: NATS healthy status | Task 11 | docker compose ps + script |
| Ops: Docker build context < 50 MB | Task 10 | docker build measurement |
| Ops: `make build` + `make up` succeed | Task 12 | CLI execution |

## Execution Order

```
Wave 1 (parallel):  Task 1, Task 2, Task 3
Wave 2 (parallel):  Task 4, Task 5, Task 6
Wave 3 (parallel):  Task 7, Task 8, Task 9, Task 10, Task 11
Wave 4 (sequential): Task 12 (depends on Task 10)
Wave 5 (sequential): Task 13 (depends on Tasks 4, 5, 6)
```

## Risk Assessment

| Risk | Mitigation |
|------|-----------|
| Session 500 may be deep in Redis/session manager layer | Trace full dependency chain from main.go; check for nil pointers |
| Dockerfile relocation may break CI/CD | Verify with `docker compose config` before removing old file |
| Frontend type changes may cascade | Run `npx tsc --noEmit` after every type change |
| NATS distroless limitation | Accept `service_started` approach; external probe for monitoring |
| APN dialog bug may be subtle race condition | Read full component, check event propagation and state updates |
