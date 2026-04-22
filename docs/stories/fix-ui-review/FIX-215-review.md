# Post-Story Review: FIX-215 — SLA Historical Reports + PDF Export + Drill-down

> Date: 2026-04-22
> Reviewer: Reviewer Agent (sonnet)
> Gate: PASS (21 fixes landed, all tests PASS)

## Summary

| Status | Count |
|--------|-------|
| PASS | 9 |
| UPDATED | 6 |
| OPEN | 0 |
| ESCALATED | 0 |
| NEEDS_ATTENTION | 0 |

**Total findings: 16**

---

## Findings Table

| ID | Check# | Severity | File | Line | Description | Status |
|----|--------|----------|------|------|-------------|--------|
| R-01 | 1 (Docs — TBL-27 retention) | LOW | `docs/architecture/db/_index.md` | 37 | TBL-27 `sla_reports` retention note ("24 months minimum, no cleanup cron. Per FIX-215 AC-7") was already present in the index. PASS. | PASS |
| R-02 | 1 (Docs — TBL-05 new column) | LOW | `docs/architecture/db/_index.md` | 15 | TBL-05 `operators` did not document new `sla_latency_threshold_ms` column added by `20260424000001` migration. Fixed: description updated to include column + constraint. | UPDATED |
| R-03 | 1 (Docs — API index new endpoints) | MEDIUM | `docs/architecture/api/_index.md` | 238 | "SLA Reports" section had only 2 legacy endpoints (API-183, API-184). Four new FIX-215 endpoints missing: `GET /sla/history`, `GET /sla/months/:year/:month`, `GET /sla/operators/:operatorId/months/:year/:month/breaches`, `GET /sla/pdf`. Fixed: added API-320..API-323 with auth, description, and story reference. | UPDATED |
| R-04 | 1 (Docs — SCREENS.md SLA page) | MEDIUM | `docs/SCREENS.md` | 85 | SCR-185 description still referenced "STORY-072/063: operator SLA reports" without FIX-215 changes. Month-detail drawer (SCR-185a) and Operator-Breach drawer (SCR-185b) entirely absent. Fixed: SCR-185 description updated; SCR-185a and SCR-185b rows added. | UPDATED |
| R-05 | 1 (Docs — FRONTEND.md shadow tokens) | MEDIUM | `docs/FRONTEND.md` | 102 | New CSS tokens `--shadow-card-success`, `--shadow-card-warning`, `--shadow-card-danger` referenced in `web/src/lib/sla.ts:uptimeStatusColor` were not registered in FRONTEND.md Component Tokens table. Fixed: three token rows added with dark/light values and FIX-215 attribution. | UPDATED |
| R-06 | 2 (API contract) | LOW | `internal/api/sla/handler.go` | — | Envelope shape: `{status, data, meta}` used throughout. `meta.breach_source`, `meta.months_requested`, `meta.months_returned` present. Year/month/months validation present. Tenant-scoping via `operator_grants` JOIN confirmed in store. PASS. | PASS |
| R-07 | 3 (Test coverage — ACs) | LOW | `internal/api/sla/handler_test.go`, `internal/store/sla_report_test.go`, `internal/store/operator_breach_test.go` | — | AC-1 → `TestSLAHandler_History_*`; AC-2 → `TestSLAHandler_MonthDetail_*`; AC-3 → `TestSLAHandler_OperatorMonthBreaches_*` + `TestOperatorBreach_*`; AC-4 → `TestSLAHandler_DownloadPDF_*`; AC-5 → `TestOperatorHandler_PatchSLATargets_*`; AC-6 → implicit via `aggregateOverall` session-weighted tests; AC-7 → documented (no cron code); `TestHandler_History_BodyShape` PAT-006 canary. All ACs covered. PASS. | PASS |
| R-08 | 4 (Breaking changes) | LOW | `internal/gateway/router.go` | — | Legacy `GET /sla-reports` and `GET /sla-reports/{id}` endpoints (API-183, API-184) are unchanged. New endpoints under `/sla/...` prefix add net-new routes. BR-7 satisfied. PASS. | PASS |
| R-09 | 5 (Migration reversibility) | LOW | `migrations/20260424000001_sla_latency_threshold.{up,down}.sql`, `migrations/20260424000002_sla_reports_month_unique.{up,down}.sql` | — | Gate report confirms roundtrip `up → down → up` tested clean. Both migrations have proper `.down.sql` reversals. PASS. | PASS |
| R-10 | 6 (Security — tenant isolation) | LOW | `internal/store/sla_report.go` | 194, 302 | Gate fix #3 added `EXISTS (SELECT 1 FROM operator_grants og WHERE og.operator_id = r.operator_id AND og.tenant_id = r.tenant_id AND og.enabled = true)` to both `HistoryByMonth` and `MonthDetail` queries. BR-6 (PDF tenant-scope) enforced via `GetGrantByTenantOperator` check. PASS. | PASS |
| R-11 | 7 (Performance) | LOW | `internal/store/sla_report.go` | — | `HistoryByMonth` bounded to ≤24 months (no unbounded scan). `BreachesForOperatorMonth` scoped to single operator+month window. `20260424000002` unique index supports efficient `ON CONFLICT` upsert. No new N+1 paths detected. `aggregateOverall` session-weighted formula runs in-process over bounded slice. PASS. | PASS |
| R-12 | 8 (Error handling) | LOW | `internal/api/sla/handler.go` | — | `sla_month_not_available` error code returned as 404 envelope when no data. `meta.breach_source` distinguishes live vs persisted. `apierr` package used throughout. PASS. | PASS |
| R-13 | 10 (Story file alignment) | LOW | `docs/stories/fix-ui-review/FIX-215-sla-historical-reports.md` | — | Gate confirmed F-A4/F-A5/F-A6 (sparkline, matrix, missing-month placeholder) were wireframe-only features not in AC table. Spec ACs are still accurate for what was delivered. No drift that would require UPDATED status on the story spec itself. PASS. | PASS |
| R-14 | 11 (Deferred items — ROUTEMAP coverage) | MEDIUM | `docs/ROUTEMAP.md` | — | Gate report listed 13 deferred items (D-1..D-13). Gate noted "ROUTEMAP additions follow via amil-autopilot." None had been written. Fixed: D-083..D-089 added covering all 12 actionable deferred items (D-13 is LOW/no-action): D-083..D-088 cover D-1..D-5+D-7..D-12; D-089 covers D-6 (F-U12 i18n English-only labels, deferred to separate i18n wave). | UPDATED |
| R-15 | 12 (Decisions doc) | MEDIUM | `docs/brainstorming/decisions.md` | — | Two key decisions from plan Scope Decisions section had no decisions.md entry: (a) AC-4 PDF parameter-driven shape, (b) breach source live vs persisted fallback, (c) session-weighted `aggregateOverall`. Fixed: added DEV-281, DEV-282, DEV-283. | UPDATED |
| R-16 | 14 (Bug patterns — PAT-006/PAT-012 recurrence) | MEDIUM | `docs/brainstorming/bug-patterns.md` | — | Scout identified PAT-006 and PAT-012 recurrences in FIX-215 gate (F-A1, F-A2). Gate report recommended updating bug-patterns.md post-gate. No entries existed for FIX-215. Fixed: added PAT-006 RECURRENCE [FIX-215] and PAT-012 RECURRENCE [FIX-215] entries with root cause, prevention, and test coverage. | UPDATED |

---

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/architecture/db/_index.md` | TBL-05 updated with `sla_latency_threshold_ms` column description | UPDATED |
| `docs/architecture/api/_index.md` | API-320..API-323 added (4 new SLA endpoints) | UPDATED |
| `docs/SCREENS.md` | SCR-185 updated; SCR-185a + SCR-185b added (month-detail + operator-breach drawers) | UPDATED |
| `docs/FRONTEND.md` | `--shadow-card-success/warning/danger` tokens registered | UPDATED |
| `docs/ROUTEMAP.md` | D-083..D-089 tech debt entries added; D-052 marked RESOLVED | UPDATED |
| `docs/brainstorming/decisions.md` | DEV-281, DEV-282, DEV-283 added | UPDATED |
| `docs/brainstorming/bug-patterns.md` | PAT-006 and PAT-012 recurrence entries for FIX-215 added | UPDATED |
| `docs/USERTEST.md` | NOT updated — per reviewer protocol, USERTEST update is Step 5 Commit's responsibility | NO_CHANGE (deferred to Step 5) |

---

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-248 (Reports Polish) | Receives D-083..D-088 deferred items (sparkline, matrix, missing-month placeholder, PDF ErrNoData sentinel, URL deep-link, minor UX polish). DEV-281 PDF decision clarifies the param-driven approach FIX-248 must build on. | NO_CHANGE (story file already targets reports polish) |
| Any story touching `operators` table | `sla_latency_threshold_ms` column now exists. Any future PATCH to `operators` DTO must include/account for this field (PAT-006 struct-literal discipline). | NO_CHANGE |
| FIX-24x (Test Infra) | D-085 Playwright E2E suite for SLA pages assigned. | NO_CHANGE |

---

## Cross-Doc Consistency

- Contradictions found: 0
- `TBL-27` in `_index.md` correctly shows `sla_reports` (not TBL-17 which is `sessions`). The task prompt's "TBL-17 (sla_reports)" was an error in the prompt — actual sla_reports is TBL-27, already documented correctly.
- BR-4 (24-month retention) documented in TBL-27 row. PASS.
- BR-7 (backward compat) — legacy endpoints unchanged. PASS.

---

## Decision Tracing

- Decisions checked: 3 scope decisions from plan (AC-4 PDF shape, breach source, aggregateOverall)
- Orphaned (in plan, not in decisions.md before this review): 3 → FIXED (DEV-281, DEV-282, DEV-283 added)

---

## USERTEST Completeness

- Entry exists for FIX-215: NO
- Type: UI story — test scenarios required
- Action: DEFERRED to Step 5 Commit agent per amil-autopilot protocol (not this reviewer's responsibility)

---

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting FIX-215 before this story: D-052 (per-operator latency threshold — FIX-203 Gate F-A3)
- D-052 targeted "POST-GA UX polish" (no specific story), not FIX-215 specifically. FIX-215 delivered the column (`sla_latency_threshold_ms`) and edit UI. D-052 can be marked RESOLVED.
- D-052 status update: RESOLVED by FIX-215 (column + PATCH handler + Operator Detail UI).

---

## Mock Status

- No `src/mocks/` directory. N/A.

---

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | USERTEST.md FIX-215 section missing | NON-BLOCKING | RESOLVED in Step 5 Commit | USERTEST.md FIX-215 section added (5 scenarios): rolling window history, MonthDetail drawer, OperatorBreach drawer, PDF download, SLA target PATCH with audit entry. |

---

## Project Health

- FIX stories completed this wave: FIX-201 → FIX-215 (15 stories)
- Current phase: UI Review Remediation [IN PROGRESS]
- Next story: FIX-216
- Blockers: None
