# STORY-057: Data Accuracy & Missing Endpoints

## User Story
As an operator, I want dashboard widgets to show real data, SIM detail tabs to load from real endpoints, and every spec'd API to exist, so that no feature is a mock or a 404.

## Description
Close data-display bugs (Dashboard UUIDs, empty Operator Health, $0 Monthly Cost, fake sparkline) and implement the four backend endpoints specified but never built: API-035, API-043, API-051, API-052. Also wire `remember_me` backend consume for 7d JWT TTL.

## Architecture Reference
- Services: SVC-03 (Core API), SVC-07 (Analytics)
- Packages: internal/api/dashboard, internal/api/sim, internal/api/apn, internal/analytics/cdr, internal/analytics/usage, internal/auth, internal/store/cdr, internal/store/usage_analytics
- Source: docs/architecture/api/_index.md (API-035/043/051/052), docs/reports/seed-report.md, docs/reports/acceptance-report.md, docs/stories/phase-8/STORY-043-review.md, docs/stories/phase-8/STORY-044-review.md

## Screen Reference
- SCR-001 (Main Dashboard), SCR-041..045 (SIM Detail — 5 tabs), SCR-060 (APN List), SCR-011 (Login — remember_me)

## Acceptance Criteria
- [ ] AC-1: Dashboard "Top 5 APNs by Traffic" widget displays APN `name` (not UUID). Backend `GET /api/v1/dashboard` joins `apns` for the name in the top-N query.
- [ ] AC-2: Dashboard "Operator Health" section populates when operators exist. Query joins `operators` without requiring `operator_grants` OR grants query fixed. Empty state only when zero operators.
- [ ] AC-3: Dashboard "Monthly Cost" returns non-zero when CDRs exist. `cdrs_monthly` continuous aggregate verified refreshing; handler aggregates cost over current month; empty fallback returns 0 only when no CDRs.
- [ ] AC-4: Dashboard KPI sparklines show real 7-day trend. `Math.random()` path removed from frontend. Backend `/dashboard` returns per-metric 7-point series. Deltas computed from real values.
- [ ] AC-5: `meta.total` strategy for cursor-paginated list endpoints: return approximate count (from `pg_class.reltuples` or cached) or add `meta.has_more` and drop total. Documented in API standards doc. All list endpoints updated consistently.
- [ ] AC-6: **API-051** `GET /api/v1/sims/:id/sessions` implemented. Returns session list scoped by tenant + sim_id, cursor-paginated. Frontend SIM detail Sessions tab reads from this endpoint (no more placeholder).
- [ ] AC-7: **API-052** `GET /api/v1/sims/:id/usage` implemented. Returns per-period usage series (hourly last 24h, daily last 30d), plus top-N sessions. Frontend `useSIMUsage` hook wired; UsageTab removes `Math.random()` mock data and renders real CDR chart.
- [ ] AC-8: **API-035** `GET /api/v1/apns/:id/sims` implemented. Returns SIM list scoped by tenant + apn_id, cursor-paginated with filters (state, segment_id, q). APN Detail "Connected SIMs" tab consumes it.
- [ ] AC-9: **API-043** `PATCH /api/v1/sims/:id` implemented. Partial update for SIM editable fields (label, notes, segment_id, custom_attributes). Field-level validation + audit log entry + state machine guard (cannot patch locked states). Frontend SIM edit action uses it.
- [ ] AC-10: `remember_me` consumed by backend login handler. When true, JWT access TTL extended to 7 days (configurable via `AUTH_JWT_REMEMBER_ME_TTL`). Refresh token TTL also extended. Decision documented in decisions.md.

## Dependencies
- Blocked by: STORY-056 (sessions route must exist first, but AC-3/AC-6 implement the correct handler)
- Blocks: STORY-058 (frontend SIM detail tabs depend on these endpoints)

## Test Scenarios
- [ ] E2E: Dashboard loads with seed data — Top APNs shows names like `iot-m2m.argus`, not UUIDs.
- [ ] E2E: Dashboard Operator Health shows 3 operators (from seed) with health indicators.
- [ ] E2E: Dashboard Monthly Cost > 0 when CDRs exist in current month.
- [ ] E2E: Dashboard sparklines show distinct trends per KPI (not random noise).
- [ ] Integration: `GET /api/v1/sims/abc/sessions?limit=20` returns session list scoped to that SIM + tenant.
- [ ] Integration: `GET /api/v1/sims/abc/usage?period=24h` returns hourly usage series + top sessions.
- [ ] Integration: `GET /api/v1/apns/xyz/sims?state=active` returns filtered SIM list.
- [ ] Integration: `PATCH /api/v1/sims/abc` with `{label: "new"}` returns updated SIM + audit entry created.
- [ ] Integration: Login with `remember_me=true` returns JWT with 7-day exp; without flag uses default 30min.
- [ ] Integration: Cursor pagination `meta.has_more` accurate across all list endpoints.

## Effort Estimate
- Size: L
- Complexity: Medium-High (4 new endpoints + analytics wiring)
