# Fix Plan: FIX-250 — Vite-native env access in `info-tooltip.tsx`

## Goal
Replace the Node-only `process.env.NODE_ENV !== 'production'` guard at `web/src/components/ui/info-tooltip.tsx:47` with the Vite-native `import.meta.env.DEV` so `make build` completes clean while dev-only warning behavior is preserved.

## Architecture Reference
Frontend SPA (Vite 6 + React 19, see `web/package.json`). Build-time env is exposed by Vite as `import.meta.env.PROD` / `import.meta.env.DEV` — `process.env` does not exist in a Vite browser bundle. `tsc --noEmit` masks the failure because `@types/node` leaks transitively into the type graph; Vite Rollup catches it at build time, which is why `make build` (container) is the keystone verification, not `pnpm tsc`.

## Root Cause
FIX-222 introduced a dev-only `console.warn` for missing glossary terms inside `InfoTooltip` and used `process.env.NODE_ENV` (Node convention) instead of the Vite-native `import.meta.env.DEV`. Single line, single file, single occurrence in `web/src/`.

## Affected Files
| File | Change | Reason |
|------|--------|--------|
| `web/src/components/ui/info-tooltip.tsx` | Modify line 47 only | Swap `process.env.NODE_ENV !== 'production'` for `import.meta.env.DEV` |

## Wave Plan

### Wave 1 — Fix + Verify (single task, trivial)

#### Task 1: Swap the env guard
- **Files:** Modify `web/src/components/ui/info-tooltip.tsx` (line 47 only — 1-line edit)
- **Depends on:** —
- **Complexity:** trivial (low)
- **Pattern ref:** None — direct 1-line swap; replacement form is documented inline below
- **Context refs:** Goal, Root Cause, Decisions > DEC-A, Test Plan
- **What:**
  - Locate line 47:
    `if (process.env.NODE_ENV !== 'production') {`
  - Replace with:
    `if (import.meta.env.DEV) {`
  - Do NOT modify any other line. Do NOT touch imports, surrounding `useEffect`, or the `console.warn` body. Do NOT remove `@types/node` from `package.json` / `tsconfig*.json` (out of scope; may be used elsewhere).
  - Behavior is identical: `import.meta.env.DEV === true` in dev, `false` in prod build — same truth table as the original guard.
- **Verify:**
  1. `grep -n "process\.env" web/src/components/ui/info-tooltip.tsx` → zero matches.
  2. `grep -rn "process\.env\.NODE_ENV" web/src/` → zero matches in entire `web/src/`.
  3. `cd web && pnpm tsc --noEmit` → zero errors.
  4. `cd web && pnpm build` → zero errors, build succeeds.
  5. `make build` (from project root) → container image builds clean (keystone — AC-4).

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 — line 47 no longer references `process.env` | Task 1 (edit) | Verify step 1 (grep file) |
| AC-2 — replacement uses `import.meta.env.PROD/DEV` | Task 1 (edit) | Verify step 1 + visual diff |
| AC-3 — `pnpm build` zero TS errors | Task 1 (edit) | Verify step 4 |
| AC-4 — `make build` clean (KEYSTONE) | Task 1 (edit) | Verify step 5 |
| AC-5 — dev-only warn preserved (silent in prod) | Task 1 (edit — semantics-equivalent swap) | Manual dev smoke + prod tree-shake check (Test Plan items 4–5) |

## Test Plan (consolidated — Reviewer/Validator runs these)
1. **TS gate:** `cd web && pnpm tsc --noEmit` → zero errors.
2. **Vite gate:** `cd web && pnpm build` → zero errors. Inspect output for absence of `process.env.NODE_ENV` in dist bundles.
3. **Container gate (AC-4 keystone):** `make build` → image builds clean. This is the only verification that exercises the full Dockerfile-driven TS+Vite path UI Scout flagged.
4. **Dev smoke (AC-5):** Run `pnpm dev`, render an `<InfoTooltip term="__nonexistent__">` instance, confirm `[InfoTooltip] Unknown term: ...` warning fires in browser console.
5. **Prod smoke (AC-5):** In the prod build output (`web/dist/assets/*.js`), confirm the `console.warn` site is tree-shaken (Vite eliminates `if (false) { ... }` blocks under `import.meta.env.DEV` because it's a static build-time constant).

## Decisions (proposed for Reviewer)

### DEC-A — Replacement form: `import.meta.env.DEV` over `!import.meta.env.PROD`
- **Choice:** Use `if (import.meta.env.DEV) { ... }`.
- **Rationale:**
  - Semantically identical to the original `NODE_ENV !== 'production'` (both true in dev, false in prod).
  - Positive form reads cleaner than the negation `!import.meta.env.PROD` — fewer mental flips for future readers.
  - Both are Vite-built-in static booleans; either tree-shakes equally well, so the choice is purely readability.
  - `DEV` is the Vite-recommended canonical for "dev-only side-effects" patterns (per Vite docs § Env Variables and Modes).
- **Alternative rejected:** `!import.meta.env.PROD` — equivalent, but reads slightly worse.

## Risks & Regression
- **Regression risk:** None. Build-time boolean swap with identical dev/prod truth table. No runtime behavior change.
- **Existing functionality preserved:** `InfoTooltip` unknown-term warning still fires in dev; still silent in prod (and now properly tree-shaken).
- **Out-of-scope guard:** Do NOT remove `@types/node` from `package.json` / `tsconfig*.json` — it may be used by other tooling (Vite config, scripts). Removing it is a separate cleanup story if ever needed.

## Architecture Guard
- [x] No new patterns introduced — uses existing Vite-native env idiom.
- [x] No existing interfaces changed.
- [x] No DB schema modifications.
- [x] No new build steps, test infrastructure, or framework upgrades.
- [x] Fix follows existing code patterns (Vite + React 19 idioms in `web/src/`).
- [x] Single-line edit; surrounding code (imports, `useEffect`, JSX) untouched.

## Plan Reference
- Story spec: `docs/stories/fix-ui-review/FIX-250-vite-native-env.md`
- Discovery: `docs/stories/fix-ui-review/FIX-233-gate.md` § F-U3
