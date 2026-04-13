# Gate Report: STORY-070 — Frontend Real-Data Wiring

## Summary
- Requirements Tracing: Fields — new response fields present (sim_count, traffic_24h_bytes, pool_used, pool_total, traffic_heatmap, acknowledged_at/by/note, operator metrics bucket, APN traffic bucket, capacity); Endpoints 6/6 new + 1 enriched dashboard; Workflows 14/14 ACs addressed; Components — new `ws-indicator.tsx`, hooks `use-apn-traffic`, `use-operator-detail`, `use-capacity`, `useReportDefinitions` all present.
- Gap Analysis: 14/14 acceptance criteria verified.
- Compliance: COMPLIANT (envelope, tenant scoping, audit, middleware ordering, shadcn/ui, semantic tokens all clean).
- Tests: 2576 passed / 0 failed (full `go test ./...` 75 packages); TS build clean; Vite build ok (3.79s).
- Test Coverage: AC-9 (acknowledge) — happy + conflict + not-found tested; AC-3/AC-5 (CDR aggregations) — bucket assertions; AC-6 (capacity) — handler test; AC-8 (report defs) — 8 definition entries asserted; AC-1 (heatmap) — shape asserted.
- Performance: APN list enrichment uses 3 single `GROUP BY` queries (no N+1); policy_violations partial index on `(tenant_id, created_at DESC) WHERE acknowledged_at IS NULL` added; cdrs aggregations reuse `cdrs_hourly`/`cdrs_daily` materialized views.
- Build: PASS (Go + TypeScript + Vite).
- Screen Mockup Compliance: covered via rewire only (no new screens).
- UI Quality: visual browser inspection skipped — dev server not running; token lint already clean (hex=0, math_random=0 per step-log STEP_2.5; re-verified via Grep).
- Token Enforcement: 0 violations.
- Turkish Text: N/A (no user-facing Turkish strings in modified regions beyond existing).
- Overall: **PASS**.

## Fixes Applied
| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Compliance / Error Handling | `internal/store/policy_violation.go` | Introduced sentinel `ErrViolationNotFound`; replaced fragile `fmt.Errorf("violation not found")` magic-string returns with typed sentinel. | `go test ./internal/store` 407 pass |
| 2 | Compliance / Error Handling | `internal/api/violation/handler.go` | Replaced brittle `err.Error() == "violation not found"` string comparison with `errors.Is(err, store.ErrViolationNotFound)`. | `go test ./...` 2576 pass |
| 3 | AC-11 Gap | `web/src/pages/esim/index.tsx` | esim page `filters` state moved from `useState` to `useSearchParams`-backed adapter (state + operator_id), mirroring sims/sessions/jobs/audit/violations/apns pattern. | `npx tsc --noEmit` clean, `npm run build` ok |

## Escalated Issues
None.

## Deferred Items
None (zero-deferral mandate).

## Performance Summary
### Queries Analyzed
| # | File:Line | Query/Pattern | Issue | Severity | Status |
|---|-----------|--------------|-------|----------|--------|
| P1 | `internal/api/apn/handler.go:181-198` | APN List enrichment — 3 aggregated queries (`simStore.CountByAPN`, `cdrStore.SumBytesByAPN24h`, `ipPoolStore.SumByAPN`) | None — single GROUP BY per stat, O(3) round trips regardless of APN count | — | OK |
| P2 | `internal/store/cdr.go` (new methods) | `GetOperatorMetrics`, `GetAPNTraffic`, `GetTrafficHeatmap7x24` | All use `cdrs_hourly`/`cdrs_daily` materialized views + tenant_id filter + bucketed GROUP BY; parameterized | — | OK |
| P3 | `migrations/20260413000003_violation_acknowledgment.up.sql` | Partial index on unack rows | Appropriate for `WHERE acknowledged_at IS NULL` list pattern | — | OK |
| P4 | `internal/api/system/capacity_handler.go` | `sessionStore.CountActive` lacks explicit tenant filter | RLS on `sessions` table auto-enforces tenant scope (migration `20260412000006_rls_policies.up.sql:64`) | — | OK (RLS verified) |

### Caching Verdicts
| # | Data | Location | TTL | Decision | Status |
|---|------|----------|-----|----------|--------|
| C1 | Report definitions | Client (TanStack Query) | 5 min staleTime | CACHE — static content | DONE |
| C2 | Dashboard heatmap | Per-request aggregation | none | SKIP — existing dashboard 5s cache envelopes it | OK |
| C3 | APN traffic series | Client (TanStack Query) | default | SKIP — period-keyed, user interactive | OK |

## Token & Component Enforcement (UI)
| Check | Matches Before | Matches After | Status |
|-------|---------------|---------------|--------|
| Hardcoded hex colors in story files | 0 | 0 | CLEAN |
| Arbitrary pixel values | 0 | 0 | CLEAN |
| Raw HTML elements outside `components/ui/` | 0 | 0 | CLEAN |
| Competing UI library imports | 0 | 0 | CLEAN |
| Default Tailwind `bg-white/bg-gray-*/text-gray-*` | 0 | 0 | CLEAN |
| `Math.random()` anywhere in `web/src` | 0 | 0 | CLEAN |
| `mockUsageData`/`mockTimeline`/`mockAuthData`/`mockSimCount`/`mockTrafficMB`/`mockPoolUsed`/`mockPoolTotal`/`generateMockTraffic`/`generateMockFrequency`/`REPORT_DEFINITIONS`/`SCHEDULED_REPORTS` | 0 | 0 | CLEAN |

## Verification
- Go build: PASS
- Go tests after fixes: 2576/0
- TypeScript `tsc --noEmit`: PASS
- Vite `npm run build`: PASS
- Fix iterations: 1

## AC-by-AC Status
| AC | Criterion | Status | Evidence |
|----|-----------|--------|----------|
| AC-1 | Dashboard fakery removed | PASS | `use-dashboard.ts` no random arrays; dashboard heatmap uses `data.traffic_heatmap` from new dashboard goroutine; event id comes from envelope `msg.id`. Verified by zero `Math.random` matches. |
| AC-2 | SIM Detail Usage real CDR | PASS | `mockUsageData` absent; pre-satisfied by STORY-057 per plan. |
| AC-3 | APN detail traffic | PASS | `apns/detail.tsx` `generateMockTraffic`/`generateMockFrequency` removed; `useAPNTraffic` hook wired to `/apns/:id/traffic`; backend handler `GetTraffic` + store `GetAPNTraffic` present; test `TestAPNTraffic*` passes. |
| AC-4 | APN list stats | PASS | `mockSimCount`/`mockTrafficMB`/`mockPoolUsed`/`mockPoolTotal` removed; `apnResponse` extended with `sim_count/traffic_24h_bytes/pool_used/pool_total`; list handler enriches via 3 aggregated queries. |
| AC-5 | Operator timeline + metrics | PASS | `mockTimeline`/`mockAuthData` removed; `GetHealthHistory`, `GetMetrics` handlers + `WithCDRStore` option wired in router; hooks `useOperatorHealthHistory`/`useOperatorMetrics` consume them. |
| AC-6 | Capacity targets | PASS | `Math.random()` allocation removed; `/system/capacity` handler live; capacity envs (`ARGUS_CAPACITY_SIM/SESSION/AUTH/GROWTH_SIMS_MONTHLY`) added to `config.go`; `useCapacity` hook wired. |
| AC-7 | SLA real metrics | PASS | `sla/index.tsx` rewired to `/sla-reports` with per-operator grouping; zero `Math.random`; breaches derived from rows where `uptime_pct < target`. |
| AC-8 | Reports API | PASS | `REPORT_DEFINITIONS`/`SCHEDULED_REPORTS` constants removed; `useReportDefinitions`→`/reports/definitions` (8 definitions); `handleGenerate` uses real POST + job_id toast; no `setTimeout` fakery. |
| AC-9 | Violations actions | PASS | Migration `20260413000003_violation_acknowledgment.up/down.sql` adds 3 columns + partial index; `PolicyViolationStore.Acknowledge` with sentinel errors; handler `POST /policy-violations/:id/acknowledge` with audit; DropdownMenu in `violations/index.tsx` with Suspend/Review/Dismiss/Escalate + `?acknowledged` filter. |
| AC-10 | Topology real flow | PASS | `topology/index.tsx` apnSevered=(isDown\|\|state!='active'); per-APN traffic derived from sim_count; refetchInterval 30s; FlowLine consumes real `traffic` prop. |
| AC-11 | URL filter persistence | PASS | `useSearchParams` in sims/apns/sessions/jobs/audit/violations/esim (7 pages); operators skipped — no filter UI (documented in step-log); cdrs/anomalies pages don't exist as filterable list views in this codebase. |
| AC-12 | Silent catch surfaced | PASS | sims reserve IPs → bulk toast with success/fail counts; audit Verify Integrity → dismissible `<Alert variant="danger">` banner; onboarding wizard → explicit Retry button. |
| AC-13 | WS indicator | PASS | `ws-indicator.tsx` (Badge + Tooltip + semantic tokens); mounted in `topbar.tsx:89`; `wsClient.getStatus/onStatus/reconnectNow` all exposed. |
| AC-14 | Dead code removal | PASS | `web/src/pages/placeholder.tsx` deleted. |

## Compliance Checklist
- [x] API envelope: `apierr.WriteSuccess` / `apierr.WriteError` / `apierr.WriteList` used on all new handlers
- [x] Tenant scoping: all new handlers gate on `apierr.TenantIDKey`; `CountActive` platform-wide but enforced by RLS
- [x] Cursor-based pagination: list endpoints return `ListMeta{Cursor, HasMore, Limit}`
- [x] Audit entries: `violation.acknowledge` via `audit.Emit`; existing audit coverage preserved
- [x] Middleware: new routes placed inside existing `RequireRole` groups per router.go convention
- [x] Migration: additive, reversible (tested via step-log; up + down files present)
- [x] shadcn/ui: Dialog/DropdownMenu/Badge/Tooltip/Alert/Card/Button/Input/Skeleton/Spinner reused; no competing library imports
- [x] Design tokens: zero hex, zero arbitrary pixel, zero `bg-gray-*`/`text-gray-*` in story files
- [x] No TODO/hardcoded/workaround (STORY-070 marker absent per step-log grep)
- [x] WS connection status + envelope `id` exposed
- [x] URL filter persistence via `useSearchParams`
- [x] No `Math.random` in web/src

## Passed Items
- All 23 plan tasks executed (step-log verified).
- Zero-deferral mandate held.
- Regression: none — existing 2498 tests still green after Wave 1; grew to 2576 with new tests.
- Backend build PASS; TS compile PASS; Vite build PASS.
