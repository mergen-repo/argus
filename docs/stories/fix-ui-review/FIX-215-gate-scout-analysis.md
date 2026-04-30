# FIX-215 — Gate Scout Analysis (Code / Compliance / Security / Performance)

Scout: gate-scout-analysis (READ-ONLY)
Story: FIX-215 — SLA Historical Reports + PDF Export + Drill-down
Plan: docs/stories/fix-ui-review/FIX-215-plan.md
Step-log: docs/stories/fix-ui-review/FIX-215-step-log.txt
Date: 2026-04-22

---

<SCOUT-ANALYSIS-FINDINGS>

## Inventories

### Field Inventory

| Field | Source | Model (Go) | API (JSON) | UI (TS) |
|-------|--------|------------|------------|---------|
| year | AC-1, AC-2, AC-3 | `store.MonthSummary.Year` (NO json tag) | `"Year"` (capitalized — BUG) | `SLAMonthSummary.year` (lowercase) — MISMATCH |
| month | AC-1, AC-2, AC-3 | `store.MonthSummary.Month` (NO json tag) | `"Month"` | `.month` — MISMATCH |
| overall | AC-1 | `store.MonthSummary.Overall` (NO json tag) | `"Overall"` | `.overall` — MISMATCH |
| operators[] | AC-2 | `store.MonthSummary.Operators` (NO json tag) | `"Operators"` | `.operators` — MISMATCH |
| operator_id | AC-2, AC-3 | `OperatorMonthAgg.OperatorID json:"operator_id"` | `"operator_id"` | `.operator_id` OK |
| operator_name | AC-2 | `OperatorName json:"operator_name"` | OK | OK |
| operator_code | AC-2, AC-5 | `OperatorCode json:"operator_code"` | OK | OK |
| uptime_pct | AC-1, AC-2 | `UptimePct json:"uptime_pct"` | OK | OK |
| incident_count | AC-1, AC-2 | `IncidentCount json:"incident_count"` | OK | OK |
| breach_minutes | AC-1, AC-2, AC-6 | `BreachMinutes json:"breach_minutes"` | OK on wire, but VALUE DRIFT (F-A2) | OK |
| latency_p95_ms | AC-2 | `LatencyP95Ms json:"latency_p95_ms"` | OK | OK |
| mttr_sec | AC-2 | `MTTRSec json:"mttr_sec"` | OK | OK |
| sessions_total | AC-2 | `SessionsTotal json:"sessions_total"` | OK | OK |
| sla_uptime_target | AC-5 | `operators.sla_uptime_target` + `OperatorMonthAgg.SLAUptimeTarget json:"sla_uptime_target"` | OK | OK |
| sla_latency_threshold_ms | AC-5, AC-6 | `operators.sla_latency_threshold_ms` + migration 20260424000001 | OK | OK |
| report_id | AC-1 (plan) | `ReportID *uuid.UUID json:"report_id,omitempty"` | OK | OK |
| started_at / ended_at | AC-3 | `Breach.StartedAt/EndedAt json:"started_at/ended_at"` | OK | OK |
| duration_sec | AC-3 | `DurationSec json:"duration_sec"` | OK | OK |
| cause | AC-3, AC-6 | `Cause json:"cause"` (values: down, latency, mixed) | OK | OK |
| samples_count | AC-3 | `SamplesCount json:"samples_count"` | OK | OK |
| affected_sessions_est | AC-3 (plan API spec) | Not on `Breach` struct | Only at response-level `meta.affected_sessions_est` (whole-month sessions_total) | FE type reads `meta.affected_sessions_est`; per-breach field missing (F-A3) |

### Endpoint Inventory

| Method | Path | Source | Impl Status |
|--------|------|--------|-------------|
| GET | /api/v1/sla/history | AC-1 | Present — `handler.History` under tenant+analyst group (router.go:770). Validates year/months/operator_id. |
| GET | /api/v1/sla/months/{year}/{month} | AC-2 | Present — `handler.MonthDetail` (router.go:771). |
| GET | /api/v1/sla/operators/{operatorId}/months/{year}/{month}/breaches | AC-3 | Present — `handler.OperatorMonthBreaches` (router.go:772). Retention-aware fallback at handler.go:277 reads `sla_reports.details.breaches` when window older than 91d. |
| GET | /api/v1/sla/pdf | AC-4 | Present — `handler.DownloadPDF` (router.go:773). Streams `application/pdf`, attachment filename uses operator code when present; month-wide otherwise. |
| PATCH | /api/v1/operators/{id} (EXTEND) | AC-5 | Present — handler.go:808 role gate `operator_manager+`, :823/:828 range validation, :946 routes to `UpdateSLATargets`. |

### Workflow Inventory

| AC | Step | User Action | Expected | Chain Status |
|----|------|-------------|----------|--------------|
| AC-1 | 1 | Open /sla | Month cards render with uptime/incidents/breach-min | BROKEN — wire keys `Year/Month/Overall/Operators`, FE reads `.year/.month/.overall/.operators` → undefined. Cards render empty / crash on `.toFixed(3)`. (F-A1) |
| AC-1 | 2 | Change year / rolling | Refetch | OK on the wire; render still blocked by F-A1 |
| AC-2 | 1 | Click month card | MonthDetailPanel opens; operator rows | BROKEN (same F-A1 on MonthDetail response shape) |
| AC-3 | 1 | Click "View" on operator row | OperatorBreachPanel lists breaches | Works (shape OK); `affected_sessions_est` partial (F-A3) |
| AC-3 | 2 | >91d window | Reads from persisted `details.breaches` | OK — seed populates `details.breaches`; `parseBreachesFromDetails` tested |
| AC-4 | 1 | Click PDF download | Browser downloads | OK — anchor hits `/api/v1/sla/pdf?year=&month=[&operator_id=]`; 404 if no rows; tenant-scoped |
| AC-5 | 1 | Operator Detail → SLA Targets → Save | PATCH, toast, persisted | OK — validation, role gate, audit via transactional `UpdateSLATargets` |
| AC-6 | — | Breach = ≥5min continuous down OR latency>threshold | CTE | OK — store/operator.go:906-963 LAG+gap>120s+duration≥300; 5 test scenarios |
| AC-7 | — | 24-month retention | No cleanup cron, documented | OK — docs/architecture/db/_index.md TBL-27 line 37 retention note added |

### UI Component Inventory

| Component | Location | Plan Ref | Impl Status |
|-----------|----------|----------|-------------|
| Breadcrumb | /sla header | FRONTEND.md | Present (index.tsx:264) |
| Year selector | /sla header | Design token map | Present `<Select>` (index.tsx:270) |
| Rolling window toggle (6/12/24 mo) | /sla header | Plan wireframe | Present (index.tsx:277-293) — uses raw `<button>` styled; acceptable as toggle chips |
| KPI row (4 cards) | /sla body | Plan wireframe | Present (index.tsx:307-341); `AnimatedCounter` preserved |
| Monthly cards grid | /sla body | Plan wireframe | Present (index.tsx:382-390) — **blocked by F-A1** at data bind |
| PDF anchor (month-wide) | Month card hover | AC-4 | Present (index.tsx:111-121) |
| EmptyState | /sla body | shadcn pattern | Present (index.tsx:373) |
| Loading skeletons | KPI + cards | — | Present |
| MonthDetailPanel (SlidePanel) | drawer | Plan | Present (month-detail.tsx) — blocked by F-A1 at data bind |
| OperatorBreachPanel (SlidePanel, nested) | drawer | Plan | Present (operator-breach.tsx) |
| Breach timeline items (cause badge, duration, samples) | OperatorBreach | Plan | Present |
| SLATargetsSection | /operators/:id Protocols tab | AC-5 | Present (detail.tsx:1427-1520); inline validation, unsaved indicator, dirty gating |
| Sparkline (6-month per-operator trend) | /sla matrix | Plan design map | **Missing** (F-A4) |
| Operator × Month matrix | /sla body | Plan wireframe | **Not implemented** — only month cards rendered (F-A5) |

### AC Summary

| # | Criterion | Status | Gaps |
|---|-----------|--------|------|
| AC-1 | 6-month monthly summary cards, selectable year | BLOCKED (runtime) | F-A1 JSON shape mismatch → empty cards |
| AC-2 | Per-month drill-down | BLOCKED (runtime) | Same F-A1 on MonthDetail |
| AC-3 | Per-operator month breaches | PARTIAL | `affected_sessions_est` AC language not fully met (F-A3) |
| AC-4 | PDF export | PASS | Works; small perf duplication (F-A9) |
| AC-5 | Editable SLA target per operator | PASS | Audit action name drift (F-A11) |
| AC-6 | Breach ≥5min down or latency | PASS | CTE correct; 5 scenarios unit-tested |
| AC-7 | 24-month retention | PASS | Documented |

---

## Findings

### F-A1 | CRITICAL | gap
- Title: `store.MonthSummary` missing JSON tags — wire shape is `{"Year","Month","Overall","Operators"}`, FE reads lowercase; /sla renders empty at runtime
- Location: `internal/store/sla_report.go:179-184`
- Description: `MonthSummary` has no `json:"..."` tags on `Year`, `Month`, `Overall`, `Operators`. Go's `encoding/json` default-marshals these as capitalized keys. FE types (`web/src/types/sla.ts:15-20`) and pages (`web/src/pages/sla/index.tsx:82-83, 238-258`; `month-detail.tsx:142-148`) read `.year`, `.month`, `.overall.uptime_pct`, `.operators`. Empirically verified with a minimal Go program — output `{"Year":2026,"Month":4,"Overall":{...},"Operators":null}`. At runtime KPI reduce accumulates NaN, card renders blank, `.toFixed(3)` throws on `undefined`. AC-1 and AC-2 fully broken. Neither handler tests nor FE tsc caught it — handler tests only assert error-code responses, never parse a 200 body; FE types are optimistic. This is a PAT-006 (shared struct field silently omitted — shape drift the compiler can't see) recurrence.
- Fixable: YES
- Suggested fix: Add json tags on `MonthSummary`:
  ```go
  type MonthSummary struct {
      Year      int                `json:"year"`
      Month     int                `json:"month"`
      Overall   OperatorMonthAgg   `json:"overall"`
      Operators []OperatorMonthAgg `json:"operators"`
  }
  ```
  Add a handler test that unmarshals the 200 body into a canary `struct { Year int \`json:"year"\`; ... }` and asserts non-zero. Consider a Go-level integration test that invokes the full handler chain against a real DB (the store tests already use `DATABASE_URL`) so shape regressions surface in CI.

### F-A2 | HIGH | compliance (PAT-012 recurrence — cross-surface count drift)
- Title: `breach_minutes` in month list/detail uses `mttr_sec * incident_count / 60` formula — ignores stored `details.breach_minutes`
- Location: `internal/store/sla_report.go:225` (HistoryByMonth), `:302` (MonthDetail)
- Description: Plan (DB Schema > `sla_reports.details` convention, BR-1) specifies `breach_minutes` is persisted in `details.breach_minutes` by the rollup path (seed confirms at `migrations/seed/003_comprehensive_seed.sql:1512-1518`). But both history and month-detail SQL compute `breach_minutes` as `COALESCE((r.mttr_sec * r.incident_count / 60), 0)`. The number shown on month cards will NOT match cumulative downtime computed from `BreachesForOperatorMonth` breach runs. This is exactly the "two strategies for the same logical metric" class PAT-012 codifies (FIX-208). For the seed (random mttr, random incidents, separate random breach_min), the drift is guaranteed.
- Fixable: YES
- Suggested fix: Read `breach_minutes` from the JSON column:
  `COALESCE((r.details->>'breach_minutes')::int, (r.mttr_sec * r.incident_count / 60), 0) AS breach_minutes`
  (keep the formula as last-resort fallback only). Add a regression test: insert a row with `details={"breach_minutes":17}`, `mttr_sec=100`, `incident_count=3`; assert history endpoint returns 17, not 5.

### F-A3 | HIGH | gap
- Title: `affected_sessions_est` not emitted per-breach nor as `totals` — plan spec unmet
- Location: `internal/api/sla/handler.go:288-298, 320-329`; `internal/store/operator.go:898-904` (Breach struct)
- Description: Plan API spec (lines 147-165) defines `data.breaches[].affected_sessions_est` AND a top-level `totals: { breaches_count, downtime_seconds, affected_sessions_est }`. Implementation emits `data: []Breach{started_at, ended_at, duration_sec, cause, samples_count}` (no per-breach field) and a single response-level `meta.affected_sessions_est = rep.SessionsTotal` (which is the WHOLE-MONTH sessions_total, not the per-breach approximation). AC-3 text ("affected session count") is not fully met. Plan formula: `sessions_total × (duration_sec / month_seconds)` per breach.
- Fixable: YES
- Suggested fix: Extend `store.Breach` with `AffectedSessionsEst int64 \`json:"affected_sessions_est"\``; compute in handler or via SQL subquery against the month's sessions_total. Add a `totals` object to the response `data` per plan. Update `SLABreachesResponse` type and `operator-breach.tsx` to render per-breach session counts (plan wireframe shows "87 sessions", "142 sess.", etc.).

### F-A4 | MEDIUM | gap
- Title: Sparkline (6-month per-operator trend) missing from /sla
- Location: `web/src/pages/sla/index.tsx`
- Description: Plan Design Token Map explicitly lists `Sparkline` (from `components/ui/sparkline.tsx`); wireframe shows "6M Trend ▁▃▂▅▂▁" per operator. No trend surface exists — only tenant-overall month cards. UX signal for "which operator is trending down" is absent.
- Fixable: YES
- Suggested fix: Add Sparkline to an operator row section (depends on F-A5) or explicitly descope with ROUTEMAP note.

### F-A5 | MEDIUM | gap
- Title: Operator × Month matrix not rendered — /sla only shows tenant-aggregate month cards
- Location: `web/src/pages/sla/index.tsx`
- Description: Plan wireframe (lines 343-349) specifies `Operator │ Target │ Apr │ Mar │ … │ 6M Trend [PDF]` — a pivot with rows per operator and columns per month. Current page renders only the month cards; operator-level data is accessible ONLY via the drawer. At-a-glance multi-operator comparison (typical SLA-report UI) is absent.
- Fixable: YES
- Suggested fix: Add a second section below month cards: table with one row per operator (derive from `summaries.flatMap(m.operators)` deduped by `operator_id`), N columns for the visible months, cell = `{uptime_pct}%` color-coded by status vs target. Sparkline column uses the same derived data.

### F-A6 | MEDIUM | gap
- Title: Missing-month summary placeholder ("not generated") not implemented
- Location: `internal/store/sla_report.go:186-289` (HistoryByMonth); `web/src/pages/sla/index.tsx`
- Description: Plan data-flow line 93 says "If an expected month is missing for an operator → `null` summary flagged 'not generated'". Current `HistoryByMonth` returns only months with ≥1 row; missing months silently disappear — the FE cannot distinguish "no data" from "not generated" nor show a contiguous strip.
- Fixable: YES
- Suggested fix: Synthesize expected N-month window in Go (loop `now.AddDate(0, -i, 0)` for `i = 0..months-1`), fill missing months with an empty `MonthSummary{Year, Month, Overall: {}, Operators: []}` plus a new `Status string \`json:"status"\`` field ("generated" | "not_generated"). FE renders a greyed "not generated" card.

### F-A7 | MEDIUM | compliance (test plan)
- Title: Playwright suite is a `test.skip` stub — AC-1..AC-4 have no browser E2E coverage
- Location: `web/tests/sla-historical.spec.ts`
- Description: Plan Task 10 deliverable "Playwright suite green". File exists, every `test(...)` is `test.skip`, Playwright not installed. Step-log: `browser-e2e=SKIPPED_NEEDS_PLAYWRIGHT_SETUP`. Unit tests cover only validation error codes, never 200 bodies — which is why F-A1 escaped. Without a real render assertion chain (or a Go-level body-shape test), the Gate cannot validate the drill-down flow.
- Fixable: YES (but may exceed this story's scope)
- Suggested fix: Either (a) install `@playwright/test`, add `test:e2e` script, un-skip the spec, include in `make test`; or (b) — higher ROI — add Go handler integration tests that run against the real seeded DB (pattern already in `operator_breach_test.go`) and assert full JSON body shape. Option (b) would have caught F-A1 + F-A2 and costs one file.

### F-A8 | MEDIUM | compliance (defense-in-depth)
- Title: History/MonthDetail JOIN operators without `operator_grants` tenant scoping
- Location: `internal/store/sla_report.go:218-237, 295-315`
- Description: Filter is `r.tenant_id = $1 AND r.operator_id IS NOT NULL`; `JOIN operators o ON o.id = r.operator_id` has no `o.tenant_id`/`operator_grants` scoping. Operators are global (per `operator_grants` model). Tenant isolation is enforced only by `r.tenant_id`; if a misseeded sla_reports row pointed at an operator the tenant does not grant, the JOIN would still return it. No active bug, but not defense in depth. Plan BR-5/BR-6 expect tenant isolation.
- Fixable: YES
- Suggested fix: Add `AND EXISTS (SELECT 1 FROM operator_grants og WHERE og.operator_id = r.operator_id AND og.tenant_id = r.tenant_id AND og.enabled = true)` to both queries.

### F-A9 | MEDIUM | performance
- Title: `DownloadPDF` performs duplicate `ListByTenant` pre-flight before provider reads the same rows
- Location: `internal/api/sla/handler.go:383-392`
- Description: `DownloadPDF` calls `h.store.ListByTenant(ctx, tenantID, windowStart, windowEnd, operatorID, "", 1)` only to 404-early on empty; then `engine.Build` triggers `StoreProvider.SLAMonthly` which calls `ListByTenant(..., limit=200)` for the same (tenant, month, operator). Two round-trips where one would suffice. At current scale acceptable; flagged for future cleanup.
- Fixable: YES
- Suggested fix: (a) move existence check inside `StoreProvider.SLAMonthly` returning a sentinel `ErrNoData` mapped to 404 in handler, or (b) rely on `len(artifact.Rows) == 0` post-Build (partially done at handler.go:414) and drop the pre-flight.

### F-A10 | MEDIUM | correctness (aggregation method)
- Title: `aggregateOverall` is unweighted arithmetic mean; `max` used for LatencyP95Ms and MTTRSec (overstates)
- Location: `internal/store/sla_report.go:401-423`
- Description: Overall uptime is `sum(uptime_pct)/N_operators` — does not weight by `sessions_total`. LatencyP95Ms takes `max` across operators (overstates). MTTRSec takes `max` (ditto). Plan didn't specify the formula explicitly (BR-2 is per-operator only), so observation rather than spec violation — but the "Total Breaches", "Breach Minutes" KPIs on /sla headline will reflect these biases. Worth a BR clarification.
- Fixable: YES
- Suggested fix: (a) session-weighted average: `SUM(uptime_pct*sessions_total)/NULLIF(SUM(sessions_total),0)`; (b) use session-weighted p95 latency (or a documented "worst observed"); comment the chosen method. Non-blocking.

### F-A11 | LOW | compliance
- Title: Audit action string is `operator.sla_targets.update`; plan specified `operator.updated`
- Location: `internal/store/operator.go:1028`
- Description: Plan API spec line 189 and Scope Decisions line 52 call for `operator.updated`; implementation emits `operator.sla_targets.update`. Both write the audit row with before/after — BR-5 functionally satisfied. But audit consumers filtering on `operator.updated` will miss these entries.
- Fixable: YES
- Suggested fix: Rename to `operator.updated` (aligns plan) OR keep the specific action and add it to the action-name registry / docs.

### F-A12 | LOW | consistency
- Title: Rolling-window default inconsistency (handler 6, store 12)
- Location: `internal/api/sla/handler.go:182` vs `internal/store/sla_report.go:188`
- Description: Handler injects `months=6` when both year and months are absent; store's own fallback is `months=12`. Today the handler always normalizes first so the store branch is unreachable — but the store contract is ambiguous and a future caller could hit either value.
- Fixable: YES
- Suggested fix: Align store default to 6 (or document handler as the single source of truth).

### F-A13 | LOW | performance
- Title: Seed volume (~172k `operator_health_logs`) — CI cold-start cost
- Location: `migrations/seed/003_comprehensive_seed.sql:802-900`
- Description: R1 risk from plan. `NOT EXISTS` guard keeps re-runs cheap (verified by step-log). Fresh volumes add ~20-30 MB. Acceptable; flagged for operator awareness.
- Fixable: NO (design choice; plan accepted this)

---

## Non-Fixable (Escalate)

None. All 13 findings are tractable in-story.

---

## Security Scan

### A. Dependency CVE Audit
Skipped — no new dependencies introduced by FIX-215 (git diff: no additions to `go.mod` or `web/package.json`); `govulncheck` / `npm audit` not executed as part of this scout run.

### B. OWASP Pattern Grep (on FIX-215 new/modified files)

- **SQL Injection:** None. All queries use parameterized placeholders `$1/$2/...`. `fmt.Sprintf` appears in `HistoryByMonth` only for column/time-filter templating (never user input).
- **XSS:** No `dangerouslySetInnerHTML` / `innerHTML =` in FIX-215 FE files.
- **Path Traversal:** None. PDF filename built from `operatorID.String()` (UUID) or `op.Code` (validated DB column, no slashes by convention).
- **Hardcoded Secrets:** None.
- **Insecure Randomness:** `migrations/seed/003_comprehensive_seed.sql` uses SQL `random()` — seed-only, not security-relevant.
- **CORS Wildcard:** N/A.

### C. Auth & Access Control

- All 4 new endpoints under `r.Use(JWTAuth(...))` + `r.Use(RequireRole("analyst"))` (router.go:766-773). Tenant context asserted via `apierr.TenantIDKey` in every handler. OK.
- `PATCH /operators/:id` SLA fields gated to `operator_manager+` (handler.go:808-814) — exceeds plan BR-5.
- `DownloadPDF` tenant+operator_grant scoping at handler.go:371-376; 404 on cross-tenant (avoids enumeration).
- `OperatorMonthBreaches` live path verifies `GetGrantByTenantOperator` at handler.go:302. Persisted fallback path relies on `GetByTenantOperatorMonth` tenant filter — cross-tenant blocked at DB; grant check would be defense-in-depth. LOW.

### D. Input Validation

- `year ∈ [2020, now]`, `month ∈ [1,12]`, `months ∈ [1,24]`, `operator_id` UUID — all validated in History, MonthDetail, OperatorMonthBreaches, DownloadPDF.
- PATCH SLA fields: uptime ∈ [50.0, 100.0], latency ∈ [50, 60000]. OK. DB `CHECK (sla_latency_threshold_ms BETWEEN 50 AND 60000)` backs it up (migration 20260424000001).

### E. Mock Retirement
N/A — no mocks introduced.

---

## Performance Summary

### Queries Analyzed

| # | File:Line | Pattern | Issue | Severity |
|---|-----------|---------|-------|----------|
| Q1 | sla_report.go:218-237 (HistoryByMonth) | Single query + JOIN + Go aggregation | ≤ 240 rows at 24m × 10 ops; uses `idx_sla_reports_tenant_time`. Acceptable. | — |
| Q2 | sla_report.go:295-315 (MonthDetail) | Filter (tenant_id, operator_id IS NOT NULL, window_start range) | Index-covered; ~10 rows typical. | — |
| Q3 | operator.go:906-963 (BreachesForOperatorMonth CTE) | LAG + gap detection + GROUP BY on hypertable | ≤ 44,640 rows/month (1/min × 31d). `idx_op_health_operator_time` covers filter. <200ms at dev volume per plan R2. | — |
| Q4 | sla_report.go:369-399 (UpsertMonthlyRollup) | ON CONFLICT via unique index | `sla_reports_month_key` (migration 20260424000002) exactly matches the ON CONFLICT tuple (with `COALESCE(operator_id, sentinel_uuid)`). Idempotent. | — |
| Q5 | handler.go:383 + store_provider.go:153 | Duplicate ListByTenant (pre-flight + provider) | See F-A9. | MEDIUM |
| Q6 | operator.go:971-1001 (UpdateSLATargets) | BEGIN/UPDATE/COMMIT, audit post-commit | Correct atomicity; commit-before-audit per step-log. OK. | — |

No N+1 detected. Migration 20260424000001 adds a CHECK constraint (not an index); migration 20260424000002 adds the needed unique index for idempotent upsert. `operator_health_logs` hypertable has `idx_op_health_operator_time` from core schema. No missing indexes identified.

### Caching Verdicts

| # | Data | Location | TTL | Decision |
|---|------|----------|-----|----------|
| C1 | `sla/history` response | FE TanStack `staleTime: 30s` | 30s | CACHE — monthly rollups don't change intra-day |
| C2 | `sla/months/:y/:m` response | FE TanStack | 30s | CACHE |
| C3 | `sla/breaches` response | FE TanStack | 30s | CACHE |
| C4 | PDF bytes | None | — | SKIP — attachment; not a perf hotspot |
| C5 | `operators.sla_*_target` | No server cache | — | SKIP — read on breach compute only, rare |
| C6 | Server-side redis cache for HistoryByMonth | None | — | SKIP — ≤240 rows index-covered; Redis adds invalidation burden with no win |

No uncached cacheable hot path. No unjustified cache.

### Frontend Performance

- Bundle: 2.53s prod build per step-log. No heavyweight deps added.
- Lazy loading: /sla routes eagerly bundled (project convention).
- Memoization: `useMemo` for KPI aggregation (index.tsx:246-258). OK.
- Virtualization: N/A (≤24 cards).
- Re-render: conditional-render drawers; no redundant churn.

### API Performance
- Bounded payloads (≤24 months × ~10 operators).
- Pagination intentionally omitted (plan).
- Compression via nginx default; no per-endpoint headers set.

---

## Bug-Pattern Compliance (docs/brainstorming/bug-patterns.md)

| Pattern | Title | Relevance | Status |
|---------|-------|-----------|--------|
| PAT-006 | Shared payload struct field silently omitted | DIRECTLY VIOLATED | F-A1 (JSON shape drift — compiler-invisible). PAT-006 explicitly says "do NOT rely on the compiler to catch missing assignments in struct literals"; same class here (missing struct tags = missing field names at wire). |
| PAT-009 | Nullable FK COALESCE in analytics | NOT VIOLATED | HistoryByMonth filters `r.operator_id IS NOT NULL` and uses `COALESCE(o.sla_uptime_target, 99.9)`. OK. |
| PAT-012 | Cross-surface count drift | DIRECTLY VIOLATED | F-A2 (`breach_minutes` on month card vs breach drawer totals). Recurrence. |
| PAT-013 | CHECK constraint drop ILIKE form | NOT TRIGGERED | Migration 20260424000001 ADDs a CHECK; uses `pg_constraint` conname lookup — safe form. |
| PAT-014 | Seed paired timestamps via CROSS JOIN LATERAL | NOT TRIGGERED | sla_reports seed uses single `wstart = make_date(y,m,1); wend = wstart + 1 month` — no random pair. |
| PAT-015 | Declared-but-unmounted React component | NOT TRIGGERED | `<SLATargetsSection/>` mount confirmed in operator detail; `<SLAMonthDetailPanel>` mounted in /sla; `<SLAOperatorBreachPanel>` nested. |
| PAT-016 | Cross-store PK confusion | NOT TRIGGERED | No cross-store linkage in this story. |
| PAT-017 | REST handler constructor missing new config parameter | NOT TRIGGERED | `slaapi.NewHandler` receives `slaReportStore, operatorStore, slaReportEngine` from `cmd/argus/main.go:1225`. |

Two recurrences (PAT-006 via F-A1, PAT-012 via F-A2) — both are high-cost (visible to end user) and both slipped past handler unit tests for the same reason (tests assert validation error codes only, never happy-path response body shape).

---

## Summary

**Findings total: 13** — 1 CRITICAL, 2 HIGH, 7 MEDIUM, 3 LOW. All fixable in-story.

**Critical blocker (F-A1):** `store.MonthSummary` missing JSON tags — /sla runtime empty. Must fix before any UAT. Small code delta; high visibility.

**High priorities:**
- F-A2 — `breach_minutes` formula drift (PAT-012 recurrence).
- F-A3 — `affected_sessions_est` per-breach + totals object missing (AC-3 spec gap).

**Scope gaps (MEDIUM):** F-A4 Sparkline, F-A5 operator×month matrix, F-A6 missing-month "not generated" placeholders — these are plan wireframe features not landed in FE. AC-1/AC-2 can be read as satisfied (cards alone), but plan's explicit matrix + sparkline is absent — Gate Lead to decide scope-fix vs scope-trim.

**Recommendation for Gate Lead:**
- Dispatch F-A1, F-A2, F-A3 immediately as fixes (backend-light, high impact, low risk).
- Decide F-A4/F-A5/F-A6 as scope extension or explicit descope (document in ROUTEMAP if descoped).
- F-A7 (browser E2E) — recommend adding Go handler integration tests (faster + would have caught F-A1/F-A2) and keeping Playwright stub for future.
- F-A8, F-A9, F-A10, F-A11, F-A12 — roll into the same fix pass if cheap; otherwise track as follow-up Tech Debt under FIX-215 regression column.
- After fix: update `docs/brainstorming/bug-patterns.md` with PAT-006 and PAT-012 recurrence entries referencing FIX-215 gate so the prevention checklist evolves.

</SCOUT-ANALYSIS-FINDINGS>
