# Gate Report: FIX-227

**Story:** FIX-227 — APN Connected SIMs SlidePanel (CDR + Usage graph + quick stats)
**Date:** 2026-04-25
**Team:** Analysis + Test/Build + UI scouts (3 parallel) + Gate Team Lead
**Result:** **PASS**

---

## Summary

- Requirements Tracing: Components 2/2 (new `QuickViewPanelBody` + modified `apns/detail.tsx`), Hooks 4/4 wired (`useSIMUsage`, `useCDRStats`, `useSIMSessions`, `useSIMStateAction`), Endpoints 0/0 backend (reuse only)
- Gap Analysis: 4/4 acceptance criteria passed (AC-1 identity, AC-2 usage+CDR+top-destinations future, AC-3 three quick actions, AC-4 lazy fetch)
- Compliance: COMPLIANT (after Gate fixes)
- Tests: 3526 Go tests passed (backend unchanged); FE type-check via `npm run build` PASS
- Build: **PASS** (vite 2.64s after fixes)
- Screen Mockup Compliance: matches plan L96-126
- UI Quality: 4 findings found, 4 fixed (F-A1 error state, F-1/F-A2/F-U1 purple token, F-A3 Suspend over-disabled, F-A4 aria-label)
- Token Enforcement: 1 violation found (text-purple-400), 1 fixed → 0 remaining
- Overall: **PASS**

## Team Composition

- Analysis Scout: 5 findings (F-A1..F-A5)
- Test/Build Scout: 0 findings (all checks clean)
- UI Scout: 1 finding (F-U1)
- De-duplicated: 6 → 4 merged findings (F-A2 + F-U1 → F-1; F-A5 INFO-only, no fix needed)

## Merged Finding Inventory

| # | Severity | Source(s) | Title | Category |
|---|----------|-----------|-------|----------|
| F-1 | MEDIUM | F-A2+F-U1 | `text-purple-400` bypasses `var(--color-purple)` token | FIXED |
| F-2 | MEDIUM | F-A1 | Missing error-state handling (Usage + CDR cards + toast) | FIXED |
| F-3 | LOW | F-A3 | Suspend button over-disabled on unrelated `isLoading` | FIXED |
| F-4 | LOW | F-A4 | Missing `aria-label` on SIM TableRow | FIXED |
| F-5 | INFO | F-A5 | DEV-319 avg-duration caveat — tracked by D-129 | NO-ACTION |

## Fixes Applied

| # | Category | File:Line | Change | Verified |
|---|----------|-----------|--------|----------|
| 1 | Design token | `web/src/components/sims/quick-view-panel.tsx:165-170` | `text-purple-400` → inline `style={{color: 'var(--color-purple)'}}` (matches Data Out sparkline stroke + dashboard/index.tsx:37 pattern) | grep `text-purple-400` → 0 |
| 2 | Error state | `web/src/components/sims/quick-view-panel.tsx:1,4,30,41,42,46-52,128-132,190-194` | Added `isError` destructure on all 3 data hooks; `useEffect` fires a single `toast.error` (id-stable via `sim-quickview-error-${sim.id}` to dedupe across retries/re-opens); inline `AlertCircle` + "Failed to load …" row inside Usage and CDR cards on error. Identity card always renders (plan's "falls back to identity card only" contract). | grep `isError` → 3; grep `toast.error` → 1; build PASS |
| 3 | UX polish | `web/src/components/sims/quick-view-panel.tsx:237, 91 (removed)` | Dropped `|| isLoading` from Suspend `disabled` expr; removed now-unused `isLoading` local. Matches plan L237 template + `sims/detail.tsx:748-771` precedent. | No TS unused-var; build PASS |
| 4 | A11y | `web/src/pages/apns/detail.tsx:473` | Added `aria-label={\`Open SIM ${sim.iccid} quick view\`}` on the `<TableRow role="button">`. | grep `aria-label` → 1 match near SIM row |

## Plan Deviation Note (important for Review)

- **Plan L131 citation was wrong API.** Plan said: "Error state: Toast via `useUIStore().addToast({kind:'error', ...})`". That method does not exist on `useUIStore` (zustand store in `web/src/stores/ui.ts` only exposes `addRecentItem`, `toggleFavorite`, `addRecentSearch`). The codebase's canonical toast API is `toast` from `sonner` (see `web/src/hooks/use-undo.ts:2`, `web/src/hooks/use-export.ts:40`, `web/src/components/operators/ProtocolsPanel.tsx:2`). Gate fix follows the real API (`toast.error` with stable id), not the plan's cited one. **Review should note the plan referenced a non-existent method.**

## Escalated Issues

None.

## Deferred Items

No new deferrals. D-129 (DEV-319 avg-duration accuracy) was already written to ROUTEMAP by Dev Step (W3T3) — verified present.

## Token & Component Enforcement

| Check | Before | After | Status |
|-------|--------|-------|--------|
| Hardcoded hex colors | 0 | 0 | CLEAN |
| Arbitrary pixel values (`text-[10px]` / `text-[9px]`) | 5 | 5 | ACCEPTED (matches cross-codebase tiny-label convention, e.g. `apns/detail.tsx:498`; not a violation) |
| Raw HTML `<button>` / `<div>` as button | 0 | 0 | CLEAN |
| Competing UI library imports | 0 | 0 | CLEAN |
| Default Tailwind colors (`text-purple-400`) | 1 | 0 | FIXED |
| Inline SVG | 0 | 0 | CLEAN |
| `bg-white` / `bg-gray-*` / `text-gray-*` | 0 | 0 | CLEAN |
| `window.confirm` / `alert(` | 0 | 0 | CLEAN |

## Bug-Pattern Dedup

- Scanned `docs/brainstorming/bug-patterns.md` — no existing PAT covers **default-Tailwind color bypassing CSS-var token** (F-1). Closest: PAT-006 (shared struct field silently omitted) and PAT-012 (cross-surface count drift) — both different classes.
- **Candidate for Commit-step PAT-NNN:** "Default Tailwind color utility (`text-purple-400`) used on a component whose sibling element (sparkline stroke) uses the canonical CSS var (`var(--color-purple)`) — visual mismatch, design-token drift, clean compile. Prevention: Gate greps `text-(red|blue|green|yellow|purple|orange|pink|indigo|violet|amber|lime|emerald|teal|cyan|sky|rose|fuchsia)-\d{3}` in every new UI file; expected 0. The default Tailwind palette hex values DO NOT match the design system's CSS-var values (e.g. `text-purple-400` = `#c084fc`, but `var(--color-purple)` = `#A855F7`)." Recommended ID: PAT-018 (next after PAT-017 in bug-patterns.md). Not written at Gate per protocol.

## Verification

- Tests after fixes: backend `go test` not re-run (no Go changes touched this Gate iteration — backend fully unchanged); FE build pass implies tsc clean
- Build after fixes: **PASS** (`cd web && npm run build` → vite built in 2.64s, 0 TS errors)
- Token enforcement post-fix: ALL CLEAR (0 violations)
- Fix iterations: 1 (no internal retries needed)

### Post-fix grep results

```
text-purple-400 in quick-view-panel.tsx: 0
isError in quick-view-panel.tsx: 3
aria-label near SIM row in apns/detail.tsx: 1 (L473)
hex in quick-view-panel.tsx: 0
raw <button> in quick-view-panel.tsx: 0
bg-white/gray in quick-view-panel.tsx: 0
toast import from 'sonner': 1
```

## Passed Items

- Go vet/build: PASS (109 packages, backend unchanged)
- Go tests: 3526 PASS / 0 FAIL
- FE build (vite): PASS (2.64s, 0 TS errors)
- FE type check (`tsc --noEmit` embedded in build): PASS
- No new deps (`git diff web/package.json web/package-lock.json` empty)
- File structure: NEW component at expected path `web/src/components/sims/quick-view-panel.tsx` (252 LOC pre-fix → ~265 LOC post-fix with error-state additions)
- All 4 required hooks present and gated by `enabled: !!sim.id` (via React Query)
- Lazy-fetch contract preserved (React Query cancel on unmount; no manual AbortController per DEV-322)
- a11y: `role="button"` + `tabIndex={0}` + Enter/Space keyboard + `aria-label` (post-fix) — PAT-015 contract satisfied
- Design tokens: every color is semantic Tailwind class or `var(--color-*)`; every icon from `lucide-react`; every button is `<Button>`; every card is `<Card>` — UI compliance fully clean post-fix
- DEV-319..322 present in `docs/brainstorming/decisions.md`
- D-129 present in `docs/ROUTEMAP.md` Tech Debt table
- SCR-030 annotated in `docs/SCREENS.md`
- Pre-existing carry-over `docs/stories/fix-ui-review/FIX-226-step-log.txt` modification noted — NOT introduced by this story (flagged by Scout B)

## Phase-Gate Summary

- Gap coverage: 4/4 ACs fully satisfied
- Architectural invariants: FE-only scope honored; no backend drift; no new hook module; SlidePanel primitive reused (FIX-216 contract intact)
- Risk resolutions: all 7 plan risks (R1-R7) still hold post-fix
- Cross-cutting: no regression on `sims/detail.tsx`, no cross-page impact beyond the APN detail SIMs tab

**Verdict: PASS**
