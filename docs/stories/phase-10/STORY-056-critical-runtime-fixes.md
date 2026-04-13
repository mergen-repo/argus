# STORY-056: Critical Runtime Fixes

## User Story
As a platform operator, I want every page to load without crashing and every listed endpoint to respond correctly, so that Argus is usable end-to-end in production.

## Description
Close all runtime bugs surfaced by E2E browser testing (docs/reports/e2e-test-report.md), Acceptance testing (docs/reports/acceptance-report.md), and Phase 8/9 gates. These are real, reproducible failures that crash pages or return wrong status codes. Mostly field-mismatch and missing-wiring bugs — no new features.

## Architecture Reference
- Services: SVC-01 (API Gateway), SVC-02 (WebSocket), SVC-03 (Core API), SVC-08 (Notification)
- Packages: internal/api/session, internal/api/audit, internal/api/notification, internal/api/apn, web/src/pages/settings/ip-pools, web/src/pages/system/tenants, web/src/layouts
- Source: docs/reports/e2e-test-report.md, docs/reports/acceptance-report.md, docs/reports/phase-8-gate.md, docs/reports/phase-9-gate.md
- Proxy: deploy/nginx/argus.conf (WebSocket location block)

## Screen Reference
- SCR-112 (IP Pools), SCR-121 (Tenants), SCR-050 (Sessions), SCR-090 (Audit Log), SCR-030 (APN List), SCR-010 (Dashboard — notification badge), all SCR-* (ErrorBoundary behavior)

## Acceptance Criteria
- [ ] AC-1: IP Pools page (`/settings/ip-pools`) loads without crash. Frontend reads `total_addresses`, `used_addresses`, `utilization_pct` from API response (or API returns `available_addresses = total - used`). Response contract verified in both directions.
- [ ] AC-2: Tenants page (`/system/tenants`) loads without crash. API response contract for `slug`, `sim_count`, `user_count`, `retention_days`, `max_api_keys` matches frontend type definitions. Nullable fields handled explicitly.
- [ ] AC-3: `GET /api/v1/sessions` returns 200 with valid session list. Handler 500 root-caused and fixed. SIM detail Sessions tab also loads cleanly.
- [ ] AC-4: `GET /api/v1/audit` route registered and returns 200 with audit log list. 404 eliminated.
- [ ] AC-5: APN List page "Create APN" button opens dialog reliably. Dialog state wired correctly — click → open, submit → close + refresh.
- [ ] AC-6: WebSocket connects through Nginx on port 8084 with path `/ws/v1/events`. Nginx `location /ws` block with `proxy_http_version 1.1` + `Upgrade`/`Connection` headers + long read timeout. Browser console clean of `ws://` connection refused errors.
- [ ] AC-7: `GET /api/v1/notifications/unread-count` endpoint exists and returns `{ count: number }`. Notification badge in header displays correct count. Cache TTL 30s.
- [ ] AC-8: ErrorBoundary resets automatically on route change. Switching routes after a crash reloads the child tree (reset key = `location.pathname`). Manual "Try Again" still works.
- [ ] AC-9: `web/public/favicon.ico` present — 404 eliminated from browser Network tab.
- [ ] AC-10: HTTP on port 8084 confirmed as the canonical entry point. No HTTPS, no redirect to 443. `CLAUDE.md`, `docs/ARCHITECTURE.md`, and `docs/architecture/services/_index.md` updated from `https://localhost:8084` to `http://localhost:8084`. Nginx config has no `listen 443 ssl` block; existing cert files under `deploy/nginx/ssl/` kept on disk for future TLS story but not referenced. Supersedes STORY-054 AC-1/AC-2 HTTPS claim for dev/staging scope (production TLS deferred to a future dedicated story with proper cert rotation).
- [ ] AC-11: NATS container healthcheck: add docker-compose `healthcheck` that probes `:8222/healthz` via a curl/wget image layer OR accept distroless limitation with documented `depends_on: condition: service_started` + external monitor at `infra/monitoring/nats-check.sh`. Chosen approach documented in `docs/reports/infra-tuning.md` Addendum.
- [ ] AC-12: Root-level `.dockerignore` added. Excludes: `node_modules`, `web/dist`, `vendor`, `pgdata`, `natsdata`, `redisdata`, `.git`, `coverage`, `tmp`, `*.log`, `docs/output`, `.playwright-mcp`, `.claude`, `.env*`, `backups`, `.idea`, `.vscode`, `bin`, `*.pem`, `*.key` (except `deploy/nginx/ssl`). Verified: `docker build` context size < 50 MB (was 243 MB).
- [ ] AC-13: `deploy/Dockerfile` relocated to `infra/docker/Dockerfile.argus` per DevOps standard layout. `Makefile` targets (`make build`, `make up`) updated to reference the new path. `deploy/docker-compose.yml` `build.dockerfile` pointer updated. Old `deploy/Dockerfile` removed. Build verified clean.

## Dependencies
- Blocked by: Phase 9 [DONE]
- Blocks: STORY-058 (ErrorBoundary integration), STORY-057 (some fixes overlap session/audit APIs)

## Test Scenarios
- [ ] E2E: Navigate to `/settings/ip-pools` — page renders, utilization bars visible, no console errors.
- [ ] E2E: Navigate to `/system/tenants` — tenant list renders, create/edit dialogs functional.
- [ ] Integration: `GET /api/v1/sessions?limit=10` returns 200 with session array.
- [ ] Integration: `GET /api/v1/audit?limit=10` returns 200 with audit entries.
- [ ] E2E: Click "Create APN" on APN List — dialog opens immediately.
- [ ] E2E: Open browser DevTools WS panel — WS connection established through port 8084, no reconnect loop.
- [ ] Integration: `GET /api/v1/notifications/unread-count` returns `{count: N}` for authenticated user.
- [ ] E2E: Force a component error on one route, navigate to a different route — new route renders, no stuck error screen.
- [ ] E2E: Browser Network panel — `favicon.ico` returns 200.
- [ ] E2E: `curl -I http://localhost:8084` returns 200, no 301/302 to https, no TLS handshake attempted.
- [ ] Ops: `docker compose ps` shows NATS with healthy status OR documented `service_started` fallback + `/healthz` probe from host.
- [ ] Ops: `docker build` context size < 50 MB (measure pre/post `.dockerignore`).
- [ ] Ops: `make build` + `make up` succeed after Dockerfile relocation; `docker compose config` validates new path.

## Effort Estimate
- Size: M-L (expanded scope +4 ops ACs)
- Complexity: Medium (route/field fixes + HTTP cleanup + Docker reorg + WS proxy config)
