# FIX-250 — Vite-native env access in `info-tooltip.tsx`

**Tier:** P2 | **Effort:** XS | **Wave:** UI Review Remediation — cleanup
**Dependencies:** none
**Surfaced by:** FIX-233 Gate UI Scout (`make build` failure on FIX-222 leftover)

## Problem Statement

`web/src/components/ui/info-tooltip.tsx:47` references `process.env.NODE_ENV !== 'production'` — a Node-only global that does not exist in a Vite-bundled browser context. Vite exposes build-time env via `import.meta.env`, not `process.env`.

Effect: `pnpm tsc --noEmit` is happy (it sees `@types/node` in transitive devDeps), but `pnpm build` (Vite production rollup) fails with `Cannot find name 'process'` and the container image cannot be built clean. UI Scout caught this on `make build`.

This was introduced when FIX-222 added the dev-only validation log inside `InfoTooltip` and forgot to switch to Vite-native env.

## Acceptance Criteria

- [ ] **AC-1:** `web/src/components/ui/info-tooltip.tsx:47` no longer references `process.env`.
- [ ] **AC-2:** Replacement uses `import.meta.env.PROD` (or `import.meta.env.DEV`) — Vite-native build-time constant.
- [ ] **AC-3:** `pnpm build` completes with zero TS errors.
- [ ] **AC-4:** `make build` (full container build) completes clean.
- [ ] **AC-5:** Behavior preserved — dev-only warning still fires in dev, silent in prod.

## Files to Touch

- `web/src/components/ui/info-tooltip.tsx` — line 47 only

## Recommended Fix

```diff
- if (process.env.NODE_ENV !== 'production') {
+ if (import.meta.env.DEV) {
    console.warn('[InfoTooltip] ...')
  }
```

(or equivalently `if (!import.meta.env.PROD)`).

## Risks & Regression

- None. The change swaps one build-time boolean for another with identical truth-table in dev-vs-prod modes. Behavior identical at runtime.

## Test Plan

- [ ] `cd web && pnpm tsc --noEmit` → zero errors.
- [ ] `cd web && pnpm build` → zero errors.
- [ ] `make build` → image builds clean.
- [ ] Dev smoke: load page with InfoTooltip, confirm warn fires (if applicable trigger present).
- [ ] Prod smoke: build prod bundle, grep `console.warn` site eliminated by tree-shake.

## Plan Reference

Surfaced in: `docs/stories/fix-ui-review/FIX-233-gate.md` § F-U3
