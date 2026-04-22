# Gate Report: FIX-215 — SLA Historical Reports + PDF Export + Drill-down

Date: 2026-04-22
Gate Lead: gate-team (opus)
Scouts: Analysis (F-A, 13), Test/Build (F-B, 1), UI (F-U, 20)

---

## Summary

- Requirements Tracing: Fields 20/20, Endpoints 5/5, Workflows 7/7 (post-fix), Components 12/14 (2 deferred wireframe features)
- Gap Analysis: 7/7 acceptance criteria pass after fixes
- Compliance: COMPLIANT (API envelope, token system, tenant scoping, audit action plan-aligned)
- Tests: 3549/3549 full suite passed (109 packages); 1 new body-shape canary added for PAT-006 regression
- Performance: 1 residual duplicate-query (F-A9) kept intentionally for 404 semantics — see Passed/Deferred
- Build: Go PASS (0 errors, 0 vet); TypeScript PASS; Vite build 2.74s
- Screen Mockup Compliance: 14/18 elements implemented (sparkline / operator matrix / reset-defaults / last-changed deferred — not AC-required)
- UI Quality: CRITICAL fixed (F-U1), 7/7 MAJOR fixed, 6/12 MINOR fixed, 6 MINOR deferred with rationale
- Token Enforcement: 2 undefined tokens → 0 (F-U1); 3 hardcoded rgba shadows → 0 (F-U4 via new `--shadow-card-*` tokens)
- Overall: **PASS**

---

## Team Composition

- Analysis Scout: 13 findings (F-A1..F-A13)
- Test/Build Scout: 1 finding (F-B1)
- UI Scout: 20 findings (F-U1..F-U20)
- De-duplicated merges: F-A3+F-U5 (affected_sessions_est), F-A4+F-U18 (sparkline), F-U4+scout enforcement grep, F-A11 trivial rename
- Final unified finding count: 31 raw → 28 distinct after dedup

---

## Merge Table

| Source | ID | Category | Severity | Merged With | Verdict |
|---|---|---|---|---|---|
| Analysis | F-A1 | JSON shape drift (PAT-006) | CRITICAL | — | FIX_NOW |
| Analysis | F-A2 | breach_minutes formula drift (PAT-012) | HIGH | — | FIX_NOW |
| Analysis | F-A3 | affected_sessions_est per-breach + totals | HIGH | F-U5 | FIX_NOW |
| Analysis | F-A4 | Sparkline per-operator trend | MEDIUM | F-U18 | DEFER (wireframe-only, not in AC) |
| Analysis | F-A5 | Operator × Month matrix | MEDIUM | — | DEFER (wireframe-only, not in AC) |
| Analysis | F-A6 | Not-generated month placeholders | MEDIUM | — | DEFER (data-flow convention, not AC) |
| Analysis | F-A7 | Playwright stub (AC-1..4 E2E) | MEDIUM | F-B1 | DEFER (cross-track infra — FIX-24x) |
| Analysis | F-A8 | Tenant scoping defense-in-depth | MEDIUM | — | FIX_NOW |
| Analysis | F-A9 | Duplicate ListByTenant pre-flight in PDF | MEDIUM | — | DEFER (needed for 404 semantics — see rationale) |
| Analysis | F-A10 | Unweighted aggregate mean; max latency | MEDIUM | — | FIX_NOW (session-weighted avg) |
| Analysis | F-A11 | Audit action name drift | LOW | — | FIX_NOW |
| Analysis | F-A12 | Store default months 12 vs handler 6 | LOW | — | FIX_NOW |
| Analysis | F-A13 | Seed volume note | LOW | — | ACCEPT (design choice documented) |
| Test/Build | F-B1 | Playwright infra absent | LOW | F-A7 | DEFER (cross-track FIX-24x) |
| UI | F-U1 | Undefined tokens `bg-bg-card` / `--radius-card` | CRITICAL | — | FIX_NOW |
| UI | F-U2 | Nested SlidePanel z-wrapper fragile | MAJOR | — | FIX_NOW |
| UI | F-U3 | Segmented-control `role="group"` missing | MAJOR | — | FIX_NOW |
| UI | F-U4 | Hardcoded rgba shadows | MAJOR | — | FIX_NOW |
| UI | F-U5 | Missing per-row `affected_sessions_est` + totals header | MAJOR | F-A3 | FIX_NOW |
| UI | F-U6 | SlidePanel lacks dialog aria + focus trap | MAJOR | — | FIX_NOW |
| UI | F-U7 | PDF `<a download>` won't attach Bearer token | MAJOR | — | FIX_NOW |
| UI | F-U8 | MonthDetail conflates 404 / transient error | MAJOR | — | FIX_NOW |
| UI | F-U9 | Static year options `['2024','2025','2026']` | MINOR | — | FIX_NOW |
| UI | F-U10 | `uptimeStatus` ignores `target` (BR-3 violation) | MINOR | — | FIX_NOW |
| UI | F-U11 | `uptimeColorClass` threshold duplication | MINOR | — | FIX_NOW (extracted to `lib/sla.ts`) |
| UI | F-U12 | English-only copy (i18n) | MINOR | — | DEFER (separate effort, explicit) |
| UI | F-U13 | URL deep-linking for drawers | MINOR | — | DEFER (screens.md document drawer-only UX) |
| UI | F-U14 | Row-level click target | MINOR | — | DEFER (button-only is accessible; mockup arrow cosmetic) |
| UI | F-U15 | Reset defaults + last-changed line | MINOR | — | DEFER (not AC-5 required; wireframe-only) |
| UI | F-U16 | `aria-describedby` on SLA input labels | MINOR | — | DEFER (FIX-248 a11y polish) |
| UI | F-U17 | `grid-cols-5` no responsive fallback | MINOR | — | FIX_NOW |
| UI | F-U18 | Sparkline column | MINOR | F-A4 | DEFER |
| UI | F-U19 | `isDirty` NaN semantics | MINOR | — | DEFER (invalid-input edge case, non-blocking) |
| UI | F-U20 | `SLAOverallAgg` type over-specified | MINOR | — | DEFER (cosmetic type refinement) |

---

## Fix Log

### Backend (Go)

| # | File:Line | Change | Verification |
|---|---|---|---|
| 1 | `internal/store/sla_report.go:179-184` | Added `json:"year/month/overall/operators"` tags on `MonthSummary` | New `TestHandler_History_BodyShape` canary passes; PAT-006 regression blocked |
| 2 | `internal/store/sla_report.go:225, :302` | `breach_minutes` now `COALESCE(NULLIF((r.details->>'breach_minutes'),'')::int, formula, 0)` — reads seeded value first | Full store suite green |
| 3 | `internal/store/sla_report.go:194-198, :310-316` | Added `EXISTS (… operator_grants …)` tenant-scoping to both History and MonthDetail JOINs | Full store suite green |
| 4 | `internal/store/sla_report.go:188` | Store default months 12 → 6 (aligned to handler) | Full store suite green |
| 5 | `internal/store/sla_report.go:401-451` | `aggregateOverall` rewritten to session-weighted mean for uptime/latency/MTTR (plain mean fallback when sessions=0) | Full store suite green |
| 6 | `internal/store/operator.go:898-905` | Added `AffectedSessionsEst int64 \`json:"affected_sessions_est"\`` on `Breach` | Go build + vet + full suite green |
| 7 | `internal/store/operator.go:1028` | Audit action `operator.sla_targets.update` → `operator.updated` per plan | Full suite green |
| 8 | `internal/api/sla/handler.go:277-344` | `OperatorMonthBreaches` now enriches per-breach `affected_sessions_est` using `sessions_total × (duration_sec / month_seconds)` from `sla_reports.details.sessions_total`; response data is `{ breaches, totals }` per plan API spec | Existing validation tests still pass; new body shape captured in TypeScript types + FE consumption |
| 9 | `internal/api/sla/handler.go:442-478` | Added `enrichAffectedSessions` + `computeBreachTotals` helpers | go vet + gofmt clean |
| 10 | `internal/api/sla/handler_test.go:+` | Added `TestHandler_History_BodyShape` — asserts lowercase JSON keys, rejects capitalized `Year/Month/Overall/Operators` | Test PASS |

### Frontend (TypeScript / React)

| # | File:Line | Change | Verification |
|---|---|---|---|
| 11 | `web/src/index.css:71-74, :124-126` | Added `--shadow-card-success/-warning/-danger` tokens (both dark + light scopes) | Used by MonthCard hover glow |
| 12 | `web/src/pages/operators/detail.tsx:1471` | `rounded-[var(--radius-card)] bg-bg-card` → `rounded-[var(--radius-md)] bg-bg-surface` (F-U1 undefined tokens) | Web build PASS |
| 13 | `web/src/lib/sla.ts` (NEW) | Extracted `classifyUptime`, `uptimeStatusColor`, `uptimeStatusLabel`, `yearOptions` — BR-3 single source of truth | typecheck PASS |
| 14 | `web/src/pages/sla/index.tsx` | Dropped `uptimeStatus` bug (F-U10), adopted `classifyUptime`; `YEAR_OPTIONS = yearOptions(5)` (F-U9); rolling-window has `role="group" aria-label` (F-U3); month card PDF now uses `useSLAPDFDownload` blob-fetch with loading state (F-U4, F-U7); removed inline rgba shadows (F-U4) | typecheck + build PASS |
| 15 | `web/src/hooks/use-sla.ts` | Added `useSLAPDFDownload` (Bearer-attached fetch → blob → saveAs, toast lifecycle); added `SLANotAvailableError` + retry guard on `useSLAMonthDetail` | typecheck PASS |
| 16 | `web/src/pages/sla/month-detail.tsx` | Removed duplicate `uptimeColorClass/uptimeBarColor` (F-U11); MonthDetailPanel renders dedicated `EmptyState` on 404 `sla_month_not_available` (F-U8); `grid-cols-5` → `grid-cols-2 sm:grid-cols-3 md:grid-cols-5` (F-U17); PDF link uses `useSLAPDFDownload` (F-U7) | typecheck + build PASS |
| 17 | `web/src/pages/sla/operator-breach.tsx` | Removed fragile `fixed z-[60] pointer-events-none` nested wrapper (F-U2); response shape updated to `{ breaches, totals }`; renders totals header (`breaches_count · downtime · ~sessions`) per mockup line 373; per-row `~N sessions` badge (F-U5 / F-A3); added footer `[Download operator-month PDF]` via `useSLAPDFDownload` | typecheck + build PASS |
| 18 | `web/src/types/sla.ts` | `SLABreach.affected_sessions_est` (non-null); new `SLABreachTotals` + `SLABreachesData`; `SLABreachesResponse.data: SLABreachesData` | typecheck PASS |
| 19 | `web/src/components/ui/slide-panel.tsx` | Added `role="dialog" aria-modal="true" aria-labelledby/describedby`, auto-focus close button, Tab focus-trap, restore-focus on unmount (F-U6) | typecheck + build PASS; used by CDR session-timeline + SLA drawers |

---

## Verification

### Commands + tail output

```
$ go build ./...
(exit 0)

$ go vet ./...
(exit 0, 0 issues)

$ go test ./... -count=1 -timeout 180s
3549 passed across 109 packages (exit 0)

$ go test ./internal/api/sla/... -count=1 -run TestHandler_History_BodyShape -v
PASS (1 new test)

$ gofmt -l internal/store/sla_report.go internal/store/operator.go \
       internal/api/sla/handler.go internal/api/sla/handler_test.go \
       internal/api/operator/handler.go
(empty — all clean)

$ cd web && npm run typecheck
tsc --noEmit   (exit 0)

$ cd web && npm run build
✓ built in 2.74s
(exit 0)
```

### Fix iterations

1 pass — no rollbacks required; all tests + builds green on the first re-verify.

### Token Enforcement Matrix (post-fix)

| Check | Before | After | Status |
|-------|--------|-------|--------|
| Hardcoded hex colors | 0 | 0 | CLEAN |
| Hardcoded rgba() shadows in arbitrary classes | 3 | 0 | FIXED via `--shadow-card-*` tokens |
| Undefined design tokens (`bg-bg-card`, `--radius-card`) | 2 | 0 | FIXED (→ `bg-bg-surface`, `--radius-md`) |
| Raw HTML `<button>` in segmented control | 1 | 1 | ACCEPTED (role=group + aria-label added; shadcn has no Segmented primitive) |
| `any` usage in new hooks/types | 0 | 0 | CLEAN |
| Default Tailwind color classes | 0 | 0 | CLEAN |
| Inline SVG outside atoms | 0 | 0 | CLEAN (all icons via lucide-react) |

---

## Passed Items (Scout-acknowledged)

- AC-4 PDF streaming endpoint (handler + store provider) functional; Bearer-auth issue fixed at UI
- AC-5 editable SLA targets, validation, role gate, transactional audit — all green
- AC-6 breach CTE (LAG + 120s gap + duration≥300) — 5 unit tests cover 3-min/5-min/latency-only/mixed/gap-split
- AC-7 24-month retention — documented in `docs/architecture/db/_index.md` TBL-17
- Migrations 20260424000001 + 20260424000002 — idempotent, reversible, tested via up/down/up
- `make db-seed` idempotency verified (144 rows unchanged across 2 re-runs)
- `scripts/verify_sla_seed.sh` — 4/4 PASS

---

## Escalated Issues

None. All fixable findings were addressed in-story; remaining items classified as DEFER with rationale.

---

## Deferred Items (ROUTEMAP → Tech Debt)

| # | Finding | Severity | Target | Rationale |
|---|---------|----------|--------|-----------|
| D-1 | F-A4 + F-U18: Sparkline per-operator 6-month trend | MEDIUM/MINOR | FIX-248 (Reports polish) | Listed in plan Design Token Map and wireframe line 344-347 but not in AC Mapping table (AC-1/2/3). Month cards alone satisfy "6-month summary cards" per AC-1. |
| D-2 | F-A5: Operator × Month matrix table | MEDIUM | FIX-248 | Wireframe line 343-349 shows pivot; AC-2 is "per-month drill-down" — already satisfied by MonthDetail drawer. Matrix is additional comparison UX, not AC. |
| D-3 | F-A6: Not-generated month placeholder with `status` field | MEDIUM | FIX-248 | Plan data-flow note, not AC. Synthesizing missing months requires new response contract — prefer batching with matrix/sparkline work. |
| D-4 | F-A7 + F-B1: Playwright E2E suite | MEDIUM/LOW | FIX-24x (test infra) | Cross-track infra (not FIX-215 alone). 36 new Go handler tests + body-shape canary cover AC surfaces. |
| D-5 | F-A9: Duplicate `ListByTenant` pre-flight before PDF build | MEDIUM | FIX-248 | Investigated: `report.Artifact` exposes only `Bytes`/`MIME`/`Filename` — no `Rows` count. Removing preflight breaks 404 semantics for empty months (an empty-rows PDF would still have bytes). Safer refactor requires adding `ErrNoData` sentinel to `StoreProvider.SLAMonthly` — out-of-scope. |
| D-6 | F-U12: i18n copy (English-only labels) | MINOR | Separate i18n wave | Plan deliberately defers i18n; FIX-214 shipped English-first. |
| D-7 | F-U13: URL deep-link for drawer state | MINOR | FIX-248 | Mockup explicitly uses drawers; URL sync is a polish improvement, not an AC. |
| D-8 | F-U14: Full-row click target in operator table | MINOR | FIX-248 | Current "View" button is accessible; mockup "›" arrow is cosmetic. |
| D-9 | F-U15: Reset-defaults button + last-changed line | MINOR | FIX-248 | Wireframe only; AC-5 "editable SLA target per operator" is satisfied by Save mutation + audit. |
| D-10 | F-U16: `aria-describedby` on SLA input help text | MINOR | FIX-248 a11y polish | Present-but-incomplete; SR announces value + error via existing `aria-invalid`. |
| D-11 | F-U19: `isDirty` flashes on invalid numeric input | MINOR | FIX-248 | Edge-case UX; Save is correctly disabled on `hasErrors`, so no data corruption. |
| D-12 | F-U20: `SLAOverallAgg` type split | MINOR | FIX-248 | Cosmetic — `overall.operator_id` is synthesized to empty; no runtime impact. |
| D-13 | F-A13: Seed volume (~172k health log rows) | LOW | — | Plan-accepted risk R1; guard makes re-runs cheap. No action. |

All DEFERRED items have targets. `ROUTEMAP.md → Tech Debt` additions follow the gate report via amil-autopilot (not modified here per single-writer scope).

---

## Business Rule / Plan Compliance (post-fix)

| Rule | Status | Evidence |
|---|---|---|
| BR-1 (breach definition: ≥5 min continuous down or latency>threshold, gap ≤ 120s) | PASS | CTE at `operator.go:906-963`; 5 test scenarios in `operator_breach_test.go` |
| BR-2 (uptime formula) | PASS | Seed + rollup path persists in `sla_reports.details`; `breach_minutes` now reads from details (fix #2) |
| BR-3 (on_track/at_risk/breached thresholds) | PASS | `classifyUptime` in `lib/sla.ts` — single source of truth, used by both index + month-detail |
| BR-4 (24-month retention) | PASS | No cleanup cron; documented in `docs/architecture/db/_index.md` |
| BR-5 (target editable by operator_manager+) | PASS | Handler role gate at `operator/handler.go:808`; audit emits `operator.updated` per plan |
| BR-6 (PDF tenant-scope) | PASS | `GetGrantByTenantOperator` at `sla/handler.go:372`; 404 on cross-tenant |
| BR-7 (backward compat `/sla-reports` list) | PASS | Legacy List/Get unchanged |
| PAT-006 (JSON shape drift) | FIXED + regression canary added |
| PAT-012 (cross-surface count drift) | FIXED (`breach_minutes` now single source) |

---

## Gate Verdict: **PASS**

Ready for autopilot progression. No escalation required. Production-readiness not blocked by any remaining item.
