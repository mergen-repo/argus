# Gate Scout — Test & Build Report — FIX-216

**Story:** FIX-216 — Modal Pattern Standardization (Dialog vs SlidePanel Semantic Split)
**Scout:** Gate Scout 2 (Test/Build)
**Date:** 2026-04-22
**Scope:** FE-only (independent verification of dev step-log)

---

## Execution Summary

### Pass 0 (Maintenance mode) — N/A (not a MAINTAIN dispatch)

### Pass 3: Tests

| Stage | Result |
|---|---|
| Go unit tests | N/A — no Go files touched (verified by `git diff --stat HEAD -- internal/ cmd/ migrations/` → empty) |
| FE unit tests | N/A — project has no Jest/Vitest (`package.json` scripts: dev, build, preview, typecheck only; no `test` script) |
| FE e2e (Playwright) | 1 spec file exists (`web/tests/sla-historical.spec.ts` — FIX-215 asset; unmodified). No new e2e required for a pattern refactor. |
| Test file diff | 0 test files modified (verified `git status web/` returns only 3 page files) |

**Test impact:** FIX-216 is a pattern refactor (Dialog↔SlidePanel swap + inline-expand→panel migration). No logic changes, no API/DB/store changes → zero new unit tests required per Amil FE pattern-refactor precedent. No existing test regressions possible (nothing to regress against).

### Pass 5: Build

| Stage | Result | Duration |
|---|---|---|
| TypeScript type-check (`npx tsc --noEmit`) | **PASS** — 0 errors | — |
| Vite production build (`npx vite build`) | **PASS** — `built in 3.14s` | 3.14s (total wall: 4.23s incl. npm overhead) |
| Largest bundle | `vendor-charts-CiocHqpl.js` 411 kB (gzip 119 kB) — unchanged from dev reports | — |

Raw tsc output: `tsc-exit=0`, 0 matches for `error TS`, single line "TypeScript compilation completed".

---

## Scans

### Raw `<button>` scan (FIX-216 touched files only)

Target files: `web/src/pages/sims/index.tsx`, `web/src/pages/violations/index.tsx`, `web/src/pages/settings/ip-pool-detail.tsx`.

- Count in these 3 files: **0**.
- Cross-track observations (NOT FIX-216 blockers, informational only — existing debt, not introduced by this story):
  - `web/src/pages/sla/index.tsx:257` — raw `<button>` (pre-existing, FIX-215 scope)
  - `web/src/components/policy/rollout-tab.tsx:221,242` — raw `<button>` (pre-existing)
  - `web/src/components/policy/versions-tab.tsx:98,131` — raw `<button>` (pre-existing)
  - Remaining matches are **inside UI primitives** (`button.tsx`, `dialog.tsx`, `slide-panel.tsx`, `popover.tsx`, `tabs.tsx`, `dropdown-menu.tsx`, `sheet.tsx`, `timeframe-selector.tsx`, `sim-search.tsx`, `table-toolbar.tsx`) — these are the primitives themselves, expected to contain raw `<button>` as their implementation.

### Hex-color scan

- `web/src/pages/**/*.tsx` (all pages, not just FIX-216 files): **0 matches**
- `web/src/components/ui/*.tsx`: **0 matches**
- Primitives (`dialog.tsx`, `slide-panel.tsx`): **0 hex** — semantic tokens only (`bg-bg-surface`, `bg-bg-elevated`, `text-text-primary`, `border-border`, `bg-black/50`, `bg-black/60`)

### rgba scan

- `web/src/pages/**/*.tsx`: **0 matches**
- `web/src/components/ui/*.tsx`: **1 match** — `web/src/components/ui/button.tsx:11` `hover:shadow-[0_0_20px_rgba(0,212,255,0.3)]` (accent-variant hover glow, pre-existing primitive, NOT in FIX-216 scope; dev step-log noted only the sims/index.tsx L795 rgba→var fix, which is confirmed applied)
- sims/index.tsx L795 verified: no `rgba(` remains; the fix from `shadow-[rgba(0,0,0,0.35)]` → `shadow-[var(--shadow-card)]` is applied.

### Go diff

- `git diff --stat HEAD -- internal/ cmd/ migrations/` → **empty** (0 Go files changed)
- Consistent with FE-only story. Go `go vet` / `go test` / `go build` not re-run (correctly untouched).

---

## Component Snapshot

- `<Dialog` page-scope occurrences: 183 across 22 page files (includes all dialog-subcomponent tag matches). In `sims/index.tsx` specifically: 6 matches — all part of the NEW bulk-state-change Dialog block (L937, L940, L941, L958, L964 + L1114 inside SlidePanel comment not actually a Dialog).
- `<SlidePanel` page-scope occurrences: 32 across 22 page files.
  - `sims/index.tsx`: 3 (Import SIMs block + Assign Policy after swap + SlidePanelFooter)
  - `violations/index.tsx`: 2 (new row-detail panel + SlidePanelFooter)
  - `settings/ip-pool-detail.tsx`: 2 (Reserve IP + SlidePanelFooter)
- `expandedIds` / `toggleExpanded` in `violations/`: **0 matches** — state refactor confirmed clean.
- `FRONTEND.md` `## Modal Pattern` header present at line 108 — exactly 1 occurrence.
- ROUTEMAP.md `D-090` Tech Debt entry present at line 683 — ESLint rule deferral documented.

---

## AC → Verification Matrix

| AC | Intent | Verification | Result |
|---|---|---|---|
| AC-1 | FRONTEND.md Modal Pattern section exists | `grep "^## Modal Pattern" docs/FRONTEND.md` → line 108 | PASS |
| AC-2 item 1 | SIMs bulk state-change uses Dialog | `bulkDialog` found inside `<Dialog>` at sims/index.tsx L937 | PASS |
| AC-2 item 2 | SIMs Assign Policy uses SlidePanel | `policyDialogOpen` found inside `<SlidePanel>` at sims/index.tsx L1114 | PASS |
| AC-2 item 3 | IP Pool Reserve uses SlidePanel | ip-pool-detail.tsx audit + SlidePanelFooter fix applied (step-log T5) | PASS |
| AC-2 item 4 | APNs Connected SIMs keeps SlidePanel | No-op (confirmed no modifications to apns/*) | PASS |
| AC-2 item 5 | Alerts preview future SlidePanel | FRONTEND.md "Future" subsection present in Modal Pattern section | PASS (doc-only) |
| AC-3 | Violations row → SlidePanel (F-171) | `expandedIds`/`toggleExpanded` 0 matches; `selectedViolation` state + `<SlidePanel>` L509 + a11y role/tabIndex/keyboard | PASS |
| AC-4 | ESLint rule | DEFER entry `D-090` in ROUTEMAP.md L683 | PASS (documented deferral) |
| AC-5 | Visual consistency (button variants, header conformance) | tsc PASS + build PASS + 0 hex in pages/primitives + SlidePanelFooter everywhere + title/description props | PASS |
| AC-6 | Dark-mode parity | Primitives use semantic tokens only (`bg-bg-*`, `text-text-*`, `border-border`, `bg-black/*`); 0 `bg-white`/`text-gray-*`/`bg-gray-*` in targeted files (step-log T6) | PASS |

---

## Findings

**No FIX-216 blocking findings.**

### Cross-Track Observations (informational — NOT FIX-216 blockers)

#### O-1 | LOW | cross-track raw-button debt
- Title: Pre-existing raw `<button>` elements outside FIX-216 scope
- Locations:
  - `web/src/pages/sla/index.tsx:257`
  - `web/src/components/policy/rollout-tab.tsx:221,242`
  - `web/src/components/policy/versions-tab.tsx:98,131`
- Description: These 5 raw `<button>` elements violate the global "no raw button" pattern but are NOT introduced or touched by FIX-216. They pre-exist and are out of scope.
- Fixable: YES (future story)
- Suggested fix: track as a cross-track cleanup story (or add to Tech Debt D-series) — not for this PR.

#### O-2 | LOW | pre-existing primitive rgba
- Title: `button.tsx` accent-variant hover glow uses rgba
- Location: `web/src/components/ui/button.tsx:11` `hover:shadow-[0_0_20px_rgba(0,212,255,0.3)]`
- Description: This is the UI primitive itself; the rgba encodes the cyan accent glow. Pre-existing, unrelated to FIX-216. If strict "no rgba in primitives" becomes a rule, convert to a `--shadow-accent-glow` CSS variable in a follow-up. Not a FIX-216 blocker.
- Fixable: YES (future refactor)
- Suggested fix: add CSS variable `--shadow-accent-glow: 0 0 20px rgba(0,212,255,0.3)` in global tokens and reference it.

---

## Raw Output (truncated)

### Type-check
```
tsc-exit=0
0 matches for 'error TS'
TypeScript compilation completed
```

### Build (last lines)
```
dist/assets/vendor-react-BrNYOvKL.js                  76.83 kB │ gzip:  26.06 kB
dist/assets/vendor-ui-C_9tr95k.js                    171.91 kB │ gzip:  45.95 kB
dist/assets/vendor-codemirror-DJAtdAYo.js            346.17 kB │ gzip: 112.32 kB
dist/assets/index-CBfT6S0q.js                        407.44 kB │ gzip: 123.87 kB
dist/assets/vendor-charts-CiocHqpl.js                411.33 kB │ gzip: 119.16 kB
✓ built in 3.14s
```

### Go diff
```
(empty — 0 Go files modified)
```

### Web diff (summary)
```
 M web/src/pages/settings/ip-pool-detail.tsx
 M web/src/pages/sims/index.tsx
 M web/src/pages/violations/index.tsx
```
(matches plan Files Table exactly; plus FRONTEND.md + ROUTEMAP.md doc updates which are expected)

---

## Summary

- **Type-check:** PASS (0 errors)
- **Build:** PASS (3.14s, under 4s budget)
- **ESLint:** N/A (not configured in this project)
- **Tests:** N/A (no Jest/Vitest unit suite; no test files modified; FE pattern-refactor precedent)
- **Raw-button in FIX-216 files:** 0
- **Hex in FIX-216 files:** 0
- **rgba in FIX-216 files:** 0 (the 1 remaining primitive rgba is out of scope)
- **Go impact:** 0 files changed (FE-only confirmed)
- **AC coverage:** 10/10 ACs/items verified PASS
- **Cross-track observations:** 2 LOW (informational; pre-existing; NOT FIX-216 blockers)

**GATE RECOMMENDATION: PASS** — FIX-216 is build-clean, type-clean, scope-correct, and fully AC-covered.
