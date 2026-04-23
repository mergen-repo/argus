# Gate Report — FIX-221 (Dashboard Polish — Heatmap Tooltip, IP Pool KPI Clarity)

**Date:** 2026-04-23  Mode: AUTOPILOT  Priority: P2  Effort: S

## Summary
- Acceptance Criteria: **3/3 PASS** (AC-1 heatmap tooltip, AC-2 backend `raw_bytes`, AC-3 IP Pool KPI title+subtitle)
- Backend build/vet/tests: **PASS** (`go build ./...`, `go vet ./...`, `go test ./...` → 3531/3531)
- Frontend type-check/build: **PASS** (`tsc --noEmit`, `vite build` 2.47s)
- Token enforcement (dashboard scope): **PASS** (0 new hex, 0 raw `<button>`, 0 new arbitrary-px not in plan)
- Cross-scout de-dup: 2 overlapping a11y flags → merged under D-107
- **Overall Verdict: PASS-WITH-DEFERRALS**

## Team Composition
- Analysis Scout: 7 findings (5 PASS checks, 2 LOW → DEFERRED)
- Test/Build Scout: 3 MED coverage gaps (all under D-091 umbrella)
- UI Scout: 7 findings (5 PASS, 2 LOW → DEFERRED)
- De-duplicated: 17 raw → 3 new deferrals (D-114/D-115/D-116)

## Findings Table

| ID | Src | Severity | Area | Title | Disposition |
|----|-----|---------:|------|-------|-------------|
| F-A1 | Analysis | LOW | Backend code-quality | `GetTrafficHeatmap7x24WithRaw` duplicates SQL + normalization of legacy sibling | DEFERRED → **D-114** |
| F-A2 | Analysis | LOW | SQL correctness | `TopPoolUsage` ORDER BY lacks tiebreaker — non-deterministic on utilization ties | DEFERRED → **D-115** |
| F-A3 | Analysis | PASS | DTO | Additive fields + `omitempty` correct | — |
| F-A4 | Analysis | PASS | Concurrency | goroutine mutex pattern preserved for new `TopIPPool` field | — |
| F-A5 | Analysis | PASS | Cache | 30s TTL stale-payload window acceptable (plan Risk #1) | — |
| F-A6 | Analysis | PASS | TZ/DOW | ISODOW `Europe/Istanbul` formula identical between old/new methods | — |
| F-A7 | Analysis | PASS | Normalization | max-scaling semantics preserved | — |
| F-B1 | Test/Build | MED | Test coverage | No unit test for `GetTrafficHeatmap7x24WithRaw` | DEFERRED (under D-091) |
| F-B2 | Test/Build | MED | Test coverage | No unit test for `TopPoolUsage` (empty-tenant nil,nil) | DEFERRED (under D-091) |
| F-B3 | Test/Build | MED | Test coverage | No handler test asserting new DTO fields | DEFERRED (under D-091) |
| F-U1 | UI | LOW | a11y | Heatmap tooltip mouse-only; no `role="tooltip"` / `aria-describedby` | DEFERRED (under **D-107**, cross-cutting) |
| F-U2 | UI | LOW | UX polish | Tooltip anchored `absolute top-0 right-0` — can occlude top-right cells on small viewports | DEFERRED → **D-116** |
| F-U3 | UI | LOW | Reuse | Local `DAYS` constant — pre-existing duplication not introduced by FIX-221 | N/A (out of scope) |
| F-U4 | UI | PASS | Tokens | All tooltip/subtitle classes use existing tokens | — |
| F-U5 | UI | PASS | Dark mode | CSS-variable driven tokens theme-responsive | — |
| F-U6 | UI | PASS | Responsive | Subtitle `truncate` + title 2-line wrap accepted per plan | — |
| F-U7 | UI | PASS | Animation | stagger delays unchanged | — |

**BLOCKER: 0  HIGH: 0  MED: 3 (all deferred policy-level)  LOW: 4 (2 new D-###, 1 under D-107, 1 out of scope)**

## Fixes Applied Inline
None required. Implementation matched plan verbatim; the 2 LOW/Analysis items (code duplication + tiebreaker) are non-blocking polish with documented rationale; 1 LOW/UI is cross-cutting a11y already tracked in D-107; 1 LOW/UI is cross-cutting tooltip positioning behavior tracked as new D-116.

## Escalated
None.

## Deferred (added to `docs/ROUTEMAP.md → ## Deferred Items`)

| ID | Source | Description | Target Story |
|----|--------|-------------|--------------|
| **D-114** | FIX-221 Gate F-A1 | `GetTrafficHeatmap7x24WithRaw` (`internal/store/cdr.go:966-1016`) duplicates the SQL + normalization loop of legacy `GetTrafficHeatmap7x24` (`:1018-1069`). Retained intentionally to keep `cdr_test.go:329` test green (plan Task 1 + Risk #2). Follow-up: delete legacy method and migrate the test to assert matrix shape derived from the new flat-slice method, OR refactor legacy into a thin wrapper that calls new + builds matrix. | FIX-24x (cleanup wave) |
| **D-115** | FIX-221 Gate F-A2 | `TopPoolUsage` query (`internal/store/ippool.go:124-132`) `ORDER BY pct DESC NULLS LAST LIMIT 1` — no secondary tiebreaker. On ties (two pools at same utilization %) the surfaced "Top pool" name can flip between 30s cache refreshes. Fix: add `, name ASC` (or `, created_at ASC`) as secondary sort. Cosmetic only. | FIX-24x (UI polish) |
| **D-116** | FIX-221 Gate F-U2 | Heatmap tooltip at `web/src/pages/dashboard/index.tsx:504` uses fixed `absolute top-0 right-0` anchor — can visually occlude top-right cells (Sun/Sat evening hours) on narrow viewports. `pointer-events-none` prevents hit-break but UX is suboptimal. Fix: smart positioning (follow cursor with edge-flip, or offset anchor based on `dayIdx >= 5 && hour >= 18`). | FIX-24x (UI polish) |

## Verification (post-review)
| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go vet ./internal/store/... ./internal/api/dashboard/...` | PASS |
| `go test ./...` | PASS (3531 tests / 109 packages) |
| `cd web && npx tsc --noEmit` | PASS (0 errors) |
| `cd web && npm run build` | PASS (vite 2.47s) |
| Token enforcement (hex, raw-button) | PASS (0 / 0) |
| Acceptance Criteria AC-1/AC-2/AC-3 | PASS / PASS / PASS |

## Passed Items
- Backend additive DTO + `omitempty` semantics correct.
- TZ/DOW formula preserved verbatim (ISODOW `Europe/Istanbul` − 1).
- Max-normalization identical between old/new methods.
- `TopPoolUsage` empty-tenant returns `(nil, nil)` — plan-compliant.
- Goroutine concurrency safe under existing mutex pattern.
- Heatmap tooltip renders AC-1 exact format `"<bytes> @ <Day> HH:00"`.
- IP Pool KPI title always carries "(avg across all pools)" clarifier (per advisor correction in plan).
- IP Pool KPI subtitle conditional + `.toFixed(0)` pct format + `truncate`.
- Other 7 KPI cards unaffected by optional `subtitle` prop.
- Dark-mode parity via CSS-variable tokens.
- Zero new hex / raw-button / arbitrary-px introduced.
- Full Go test suite 3531/3531 green; tsc + vite clean.
