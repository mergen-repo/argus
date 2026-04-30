# Gate Report: STORY-056 — Critical Runtime Fixes

## Summary
- Requirements Tracing: Fields 22/22, Endpoints 5/5, Workflows 13/13
- Gap Analysis: 13/13 acceptance criteria passed
- Compliance: COMPLIANT
- Tests: 1745/1745 Go tests passed, full suite green
- Test Coverage: N/A (bug-fix story, no new test scenarios required per plan)
- Performance: 0 issues found
- Build: PASS (Go build, TypeScript tsc --noEmit)
- Screen Mockup Compliance: N/A (bug-fix story, no new screens)
- UI Quality: Pre-existing raw HTML elements noted, deferred to STORY-077
- Token Enforcement: 0 new violations introduced by this story
- Overall: **PASS**

## AC Verification

| # | Criterion | Status | Evidence |
|---|-----------|--------|----------|
| AC-1 | IP Pools page loads without crash | PASS | `IpPool` interface in `web/src/types/settings.ts` aligned with backend DTO. Fields `cidr_v4`, `cidr_v6`, `utilization_pct`, `state`, `alert_threshold_warning`, `alert_threshold_critical`, `reclaim_grace_period_days` present. `ip-pools.tsx:48` uses `pool.cidr_v4 \|\| pool.cidr_v6 \|\| ''`. `ip-pool-detail.tsx:207` same. `tsc --noEmit` passes. |
| AC-2 | Tenants page loads without crash | PASS | `Tenant` interface in `web/src/types/settings.ts` includes `slug`, `sim_count`, `user_count`, `state`, `domain`, `contact_email`, `contact_phone`, `max_sims`, `max_apns`, `max_users`, `settings`, `apn_count`. Nullable fields handled with `??` defaults in `tenants.tsx:97-101,208-209`. `tsc --noEmit` passes. |
| AC-3 | Sessions API returns 200 | PASS | `internal/aaa/session/session.go` `ListActive()` uses DB store when available (line 256), falls back to Redis scan (line 305). `listActiveFromRedis` correctly scans with `session:*` pattern, filters by state and filter criteria. No nil pointer path. `go build` and `go test` pass. |
| AC-4 | Audit route returns 200 | PASS | Router at `router.go:182` registers `GET /api/v1/audit` mapped to `AuditHandler.List`. Frontend `use-audit.ts:30` calls `/audit`. Verify chain uses `/audit-logs/verify` (router.go:180) -- both routes work. |
| AC-5 | APN Create dialog opens reliably | PASS | `apns/index.tsx:274` has `useState(false)` for `createOpen`, button at line 316 calls `setCreateOpen(true)`, `CreateAPNDialog` at line 442 receives `open={createOpen}` and `onClose={() => setCreateOpen(false)}`. Submit handler resets form and calls `onClose()`. State wiring is correct. |
| AC-6 | WebSocket connects through Nginx | PASS | `infra/nginx/nginx.conf:125-133` has `location /ws/` with `proxy_pass http://websocket/ws/`, `proxy_http_version 1.1`, Upgrade/Connection headers, `proxy_read_timeout 86400s`. `web/src/lib/ws.ts:14` uses `ws://` or `wss://` based on page protocol + `/ws/v1/events`. Path matches. |
| AC-7 | Notifications unread count works | PASS | `web/src/hooks/use-notifications.ts:35-55` calls `/notifications/unread-count` with `staleTime: 30_000` (30s cache) and `refetchInterval: 60_000`. Router.go:469 registers the endpoint. Topbar badge uses `useUnreadCount`. |
| AC-8 | ErrorBoundary resets on route change | PASS | `dashboard-layout.tsx:60` uses `<ErrorBoundary key={location.pathname}>`. React will unmount/remount on pathname change, clearing error state. `handleRetry` at `error-boundary.tsx:29` resets state for manual retry. |
| AC-9 | Favicon present, no 404 | PASS | `web/public/favicon.svg` (278B, valid SVG with Argus "A" in #00D4FF on #0a0e17 background). `web/public/favicon.ico` (6B stub, suppresses legacy browser 404). `web/index.html:9` has `<link rel="icon" type="image/svg+xml" href="/favicon.svg" />`. |
| AC-10 | HTTP references corrected | PASS | `grep "https://localhost" CLAUDE.md docs/ARCHITECTURE.md docs/architecture/services/_index.md Makefile` returns 0 matches. Remaining `https://localhost` references are only in historical review files (phase-5/6), story description files, and `docs/USERTEST.md:300` (5G SBA :8443 -- legitimately HTTPS). Nginx config has no `listen 443 ssl` block. |
| AC-11 | NATS healthcheck documented | PASS | `infra/monitoring/nats-check.sh` exists, is executable, probes `http://localhost:8222/healthz` via curl. `deploy/docker-compose.yml:50` uses `condition: service_started` for NATS. Approach documented in `docs/reports/infra-tuning.md` Addendum (lines 340-385). |
| AC-12 | .dockerignore added | PASS | `.dockerignore` at project root with 20 exclusion patterns matching AC-12 spec exactly: `node_modules`, `web/dist`, `vendor`, `pgdata`, `natsdata`, `redisdata`, `.git`, `coverage`, `tmp`, `*.log`, `docs/output`, `.playwright-mcp`, `.claude`, `.env*`, `backups`, `.idea`, `.vscode`, `bin`, `*.pem`, `*.key`, `!deploy/nginx/ssl`. |
| AC-13 | Dockerfile relocated | PASS | `infra/docker/Dockerfile.argus` exists (multi-stage Go+React build, 44 lines). `deploy/Dockerfile` does not exist (removed). `deploy/docker-compose.yml:35` references `dockerfile: infra/docker/Dockerfile.argus`. `go build ./...` succeeds. |

## Fixes Applied

No fixes were needed during gate. All 13 acceptance criteria passed on first check.

| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| — | — | — | No fixes required | — |

## Escalated Issues

None.

## Deferred Items (tracked in ROUTEMAP Tech Debt)

| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-1 | Raw `<input>` element in `ip-pool-detail.tsx:233` (search filter) should use shadcn/ui `Input` component | STORY-077 | YES |
| D-2 | Raw `<button>` elements in `ip-pool-detail.tsx` (lines 241, 344, 370, 402, 414) and `apns/index.tsx` (lines 128, 332, 374) -- pre-existing, should use shadcn/ui `Button` | STORY-077 | YES |

## Performance Summary

### Queries Analyzed

| # | File:Line | Query/Pattern | Issue | Severity | Status |
|---|-----------|--------------|-------|----------|--------|
| 1 | session.go:305 | Redis SCAN for listActiveFromRedis | Fallback path only (when sessionStore is nil). Production uses DB store path. | LOW | ACCEPTABLE |

### Caching Verdicts

| # | Data | Location | TTL | Decision | Status |
|---|------|----------|-----|----------|--------|
| 1 | Unread notification count | TanStack Query (browser) | 30s staleTime + 60s refetchInterval | CACHE | Already implemented |
| 2 | Audit list | TanStack Query (browser) | 15s staleTime | CACHE | Already implemented |

## Token & Component Enforcement (UI stories)

| Check | Matches Before | Matches After | Status |
|-------|---------------|---------------|--------|
| Hardcoded hex colors | 0 | 0 | CLEAN |
| Arbitrary pixel values | 0 | 0 | CLEAN |
| Raw HTML elements (shadcn/ui) | 7 (pre-existing) | 7 (pre-existing, deferred D-1/D-2) | DEFERRED → STORY-077 |
| Competing UI library imports | 0 | 0 | CLEAN |
| Default Tailwind colors | 0 | 0 | CLEAN |
| Inline SVG | 0 | 0 | CLEAN |
| Missing elevation | 0 | 0 | CLEAN |

## Security Scan

| Check | Result |
|-------|--------|
| SQL Injection (raw string concat) | CLEAN — no raw SQL in story files |
| XSS (dangerouslySetInnerHTML) | CLEAN — no matches |
| Hardcoded secrets | CLEAN — no matches |
| CORS wildcard | CLEAN — no matches |
| Auth middleware | PASS — all affected routes use JWTAuth + RequireRole |

## Verification
- Go build: PASS (`go build ./...` succeeds)
- Go tests: PASS (1745 tests in 62 packages)
- TypeScript: PASS (`npx tsc --noEmit` clean)
- Token enforcement: 0 new violations (pre-existing raw HTML deferred)
- Fix iterations: 0 (no fixes needed)
