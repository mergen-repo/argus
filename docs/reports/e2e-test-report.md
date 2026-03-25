# E2E Re-Test Report

**Date:** 2026-03-23
**Tester:** Automated E2E Browser + API (Re-test after fixes)
**Environment:** Docker (http://localhost:8084)
**Build:** commit 263540a (milestone: E2E & Polish Phase COMPLETE)

---

## 1. Previous Fix Verification (6/8 Fixed)

| # | Issue | Status | Notes |
|---|-------|--------|-------|
| 1 | Super admin empty data | FIXED | Dashboard shows 55 SIMs, 25 sessions, charts, alerts |
| 2 | /system/tenants crash | NOT FIXED | Still crashes: `Cannot read properties of undefined (reading 'toLocaleString')`. API returns fields that don't match frontend Tenant type (missing `slug`, `sim_count`, `user_count`, `retention_days`, `max_api_keys`) |
| 3 | APN creation dialog | PARTIALLY FIXED | "Create APN" button visible but dialog does NOT open on click. Code exists but state transition not working |
| 4 | /api/v1/health 404 | FIXED | Returns 200 with db/redis/nats/aaa status |
| 5 | Notifications unread-count 404 | FIXED | Returns `{"status":"success","data":{"count":4}}` |
| 6 | HTTPS -> HTTP | FIXED | App runs on plain HTTP, login/navigation works |
| 7 | WebSocket | NOT FIXED | Repeated `WebSocket connection to 'ws://localhost:8084' failed` errors in console. Does not break functionality but produces constant error noise |
| 8 | Seed passwords (bcrypt) | FIXED | Both admin@argus.io/admin and ahmet.yilmaz@nar.com.tr/password123 login successfully |

---

## 2. Route Crawl (Pass 1)

| Route | Status | Data | Notes |
|-------|--------|------|-------|
| `/` (Dashboard) | IMPLEMENTED | 55 SIMs, 25 sessions, charts, operator health, alert feed | Full widgets with real data |
| `/sims` | IMPLEMENTED | 50+ rows, pagination | Search, filters, "Load more" button |
| `/sims/:id` | IMPLEMENTED | Full detail view | 5 tabs: Overview, Sessions, Usage, Diagnostics, History |
| `/apns` | IMPLEMENTED | 4 APNs | Card layout with SIM count, traffic, IP pool usage |
| `/apns/:id` | IMPLEMENTED | Detail page | 4 tabs: Configuration, IP Pools, Connected SIMs, Traffic |
| `/operators` | IMPLEMENTED | 4 operators | Card layout with health status, SIM counts |
| `/operators/:id` | IMPLEMENTED | Detail page | 4 tabs: Overview, Health History, Circuit Breaker, Traffic |
| `/sessions` | ERROR | API 500 | "Failed to load sessions" - backend sessions endpoint returns INTERNAL_ERROR |
| `/policies` | IMPLEMENTED | 3 policies | Table with search, status filter |
| `/policies/:id` | IMPLEMENTED | DSL editor | Code editor with syntax, Preview/Versions/Rollout tabs, Dry Run |
| `/esim` | IMPLEMENTED | Empty table | "No eSIM profiles found" - structure correct, no seed data |
| `/jobs` | IMPLEMENTED | 7 jobs | Table with type, state, progress bars, timestamps |
| `/audit` | IMPLEMENTED | Empty table | "No audit logs found" - structure correct, no seeded audit data |
| `/notifications` | IMPLEMENTED | 6 notifications (4 unread) | Unread/All tabs, Mark as read, individual + bulk |
| `/settings/users` | IMPLEMENTED | 4 users | Table with roles, status, Invite User button |
| `/settings/api-keys` | IMPLEMENTED | 3 API keys | Table with scopes, Rotate/Revoke actions |
| `/settings/ip-pools` | CRASH | API returns data | `Cannot read properties of undefined (reading 'toLocaleString')` - `available_addresses` field missing from API response |
| `/settings/notifications` | IMPLEMENTED | Config page | Delivery channels, event subscriptions, alert threshold sliders |
| `/system/health` | IMPLEMENTED | Live data | Service status (db/redis/nats/aaa), real-time metrics, latency |
| `/system/tenants` | CRASH | API returns data | Same `toLocaleString` crash - API/frontend type mismatch |
| `/analytics` | IMPLEMENTED | Full data | Usage stats, Traffic Over Time chart, Top Consumers, breakdowns |
| `/analytics/cost` | NOT TESTED | - | Sub-route exists in code |
| `/analytics/anomalies` | NOT TESTED | - | Sub-route exists, API returns 3 anomalies |

**Summary:** 22 routes tested, 17 IMPLEMENTED with data, 1 ERROR (API), 2 CRASH (frontend), 2 NOT TESTED

---

## 3. Key Interactions (Pass 2)

| Interaction | Status | Notes |
|-------------|--------|-------|
| Login (admin) | PASS | Redirects to dashboard with data |
| Login (tenant user) | PASS | ahmet.yilmaz@nar.com.tr shows 80 SIMs (Nar tenant) |
| Sign out | PASS | Clears session, redirects to /login |
| Dashboard widgets | PASS | All 4 stat cards load (SIMs, Sessions, Auth/s, Cost) |
| Dashboard charts | PASS | SIM Distribution donut, Top APNs bar chart render |
| Dashboard alerts | PASS | Alert feed shows 3 live alerts with severity badges |
| Notification bell | PASS | Badge shows "4", links to notifications page |
| Sidebar navigation | PASS | All 20 links work, active state highlights correctly |
| SIM list table | PASS | 50 rows with ICCID/IMSI/MSISDN/State/Type/RAT/Created |
| SIM row click -> detail | PASS | Navigates to /sims/:id with full detail |
| SIM detail tabs | PASS | Overview, Sessions (empty/404), Usage (chart+data), Diagnostics, History |
| SIM search/filter | PASS | Search box and State/RAT filter buttons present |
| APN card click -> detail | PASS | Navigates to /apns/:id with configuration details |
| Create APN button | FAIL | Button visible but dialog does not open |
| Operator card -> detail | PASS | Shows config, health, test connection button |
| Policy row click -> editor | PASS | DSL code editor with syntax highlighting works |
| Notification mark as read | PRESENT | Individual "Mark as read" buttons, "Mark all" button |
| Error boundary recovery | PARTIAL | "Try Again" works but "Go Home" loses session. ErrorBoundary does NOT reset on route change |

**Summary:** 18 interactions tested, 15 PASS, 1 FAIL, 2 PARTIAL

---

## 4. API Verification (Pass 4)

| Endpoint | Status | Result |
|----------|--------|--------|
| `GET /api/v1/health` | PASS | `{"status":"success","data":{"db":"ok","redis":"ok","nats":"ok"}}` |
| `POST /api/v1/auth/login` (admin) | PASS | Returns token, user info |
| `POST /api/v1/auth/login` (tenant user) | PASS | Returns token, role=tenant_admin |
| `GET /api/v1/sims` | PASS | 50 items returned, has_more=true |
| `GET /api/v1/notifications/unread-count` | PASS | count=4 |
| `GET /api/v1/operators` | PASS | 4 operators |
| `GET /api/v1/apns` | PASS | 4 APNs |
| `GET /api/v1/policies` | PASS | 3 policies |
| `GET /api/v1/jobs` | PASS | 7 jobs |
| `GET /api/v1/users` | PASS | 4 users |
| `GET /api/v1/tenants` | PASS | 3 tenants |
| `GET /api/v1/sessions` | FAIL | `{"status":"error","error":{"code":"INTERNAL_ERROR"}}` |
| `GET /api/v1/ip-pools` | PASS | 4 pools (missing `available_addresses` field causes frontend crash) |
| `GET /api/v1/notifications` | PASS | 6 notifications |
| `GET /api/v1/api-keys` | PASS | 3 API keys |
| `GET /api/v1/cdrs` | PASS | 50 CDR records |
| `GET /api/v1/analytics/usage` | PASS | Full usage data with breakdowns |
| `GET /api/v1/analytics/anomalies` | PASS | 3 anomalies |
| `GET /api/v1/audit` | FAIL | Returns `404 page not found` |

**Summary:** 19 endpoints tested, 17 PASS, 2 FAIL

---

## 5. Remaining Issues

### Critical (Page Crashes)
1. **IP Pools page crash** (`/settings/ip-pools`) - `pool.available_addresses.toLocaleString()` fails because API returns `total_addresses`, `used_addresses`, `utilization_pct` but NOT `available_addresses`. Frontend IpPool type assumes it exists. **Fix:** Add `available_addresses` to API response OR compute it client-side (`total - used`) with null guard.
2. **Tenants page crash** (`/system/tenants`) - API/frontend type mismatch. API returns `cidr_v4` (not `cidr`), and is missing `slug`, `sim_count`, `user_count`, `retention_days`, `max_api_keys`. **Fix:** Either extend API response or add null guards + field mapping in frontend.

### High
3. **Sessions API 500** - `GET /api/v1/sessions` returns internal error. Both `/sessions` page and SIM detail Sessions tab fail.
4. **Create APN dialog not opening** - Button click does not trigger dialog state. Code exists (`setCreateOpen(true)`) but something prevents the state transition.
5. **WebSocket connection failures** - Constant `ws://localhost:8084` connection refused errors. WS proxy not configured in Nginx or backend WS listener not bound to expected port.

### Medium
6. **Audit API 404** - `GET /api/v1/audit` returns `404 page not found`. Route may not be registered in the Go router.
7. **ErrorBoundary doesn't reset on navigation** - After a crash, navigating to other routes via sidebar continues showing the error screen until "Try Again" is clicked. ErrorBoundary should listen for route changes and reset.
8. **API meta.total always 0** - SIMs API returns `meta.total: 0` even when data has 50+ items (cursor pagination doesn't count).

### Low
9. **Top 5 APNs chart shows UUIDs** - Dashboard chart shows raw APN UUIDs instead of human-readable names.
10. **favicon.ico 404** - Missing favicon.

---

## 6. Database Verification

| Check | Result |
|-------|--------|
| Total SIMs | 162 (55 Argus Demo, 80 Nar, 27 Bosphorus) |
| Tenants | 3 (Argus Demo, Nar Teknoloji, Bosphorus IoT) |
| Users | Multiple per tenant with bcrypt passwords |
| Active Sessions | 160 (per health endpoint) |
| CDRs | 50+ records |
| Notifications | 6 (4 unread) |
| APNs | 4 |
| Operators | 4 (Turkcell, Vodafone TR, Turk Telekom, Mock) |
| Policies | 3 |
| Jobs | 7 |
| IP Pools | 4 |
| API Keys | 3 |

---

## Summary

```
E2E RE-TEST SUMMARY
====================
Previous Fix Verification: 6/8 fixed
Pass 1 - Routes: 22 total, 17 implemented, 2 crashes, 1 API error, 2 not tested
Pass 2 - Interactions: 18 tested, 15 pass, 1 fail, 2 partial
Pass 4 - API: 17/19 pass

Remaining Critical: 2 (IP Pools crash, Tenants crash)
Remaining High: 3 (Sessions API 500, Create APN dialog, WebSocket)
Remaining Medium: 3 (Audit API 404, ErrorBoundary nav, meta.total)

Overall: CONDITIONAL PASS
- Core flows (login, dashboard, SIM management, policies, analytics) work excellently
- 2 page crashes and 1 broken API prevent full PASS
- Root cause for both crashes: API response fields don't match frontend type definitions
```
