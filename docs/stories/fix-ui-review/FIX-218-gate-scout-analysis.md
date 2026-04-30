<SCOUT-ANALYSIS-FINDINGS>

## Inventories

### Field Inventory
N/A — pure deletion story, no data fields added/modified.

### Endpoint Inventory
| Method | Path | Source | Impl Status |
|--------|------|--------|-------------|
| GET/POST/PATCH/DELETE | /users/me/views[/:id][/default] | AC-3 (retained) | Backend untouched (verified: `web/src/hooks/use-saved-views.ts` intact, `web/src/lib/api.ts:90` silent-path retained) |

### Workflow Inventory
| AC | Step | Chain Status |
|----|------|--------------|
| AC-1 | Open /operators — no Views button rendered | OK — `SavedViewsMenu page="operators"` removed at old line 401 |
| AC-1 | Open /apns — no Views button rendered | OK — `SavedViewsMenu page="apns"` removed at old line 383 |
| AC-1 | Open /policies — no Views button rendered | OK — `SavedViewsMenu page="policies"` removed at old line 207 |
| AC-1 | Open /sims — no Views button rendered | OK — `SavedViewsMenu page="sims"` removed at old line 376 |
| AC-1 | /sessions + /settings/ip-pools — no Views button pre-existing | OK — verified no-op (no grep hit) |
| AC-2 | Operators: checkbox column gone | OK — `<Checkbox>` + `selectedIds` + `toggleSelect` + Compare button block removed |
| AC-3 | `SavedViewsMenu` component + `useSavedViews` hook + backend retained | OK — all 3 layers intact for future reintroduction |

### UI Component Inventory
| Component | Location | Arch Ref | Impl Status |
|-----------|----------|----------|-------------|
| OperatorListPage | `web/src/pages/operators/index.tsx` | CMP-operators-list | Views/checkbox/Compare removed; RowActionsMenu (View Details / Assign / Remove) retained |
| ApnListPage | `web/src/pages/apns/index.tsx` | CMP-apns-list | Views removed; all other affordances retained |
| PolicyListPage | `web/src/pages/policies/index.tsx` | CMP-policies-list | Views removed; `selectedIds` + Compare (≥2) + `Checkbox` column RETAINED (out of scope per D-218-4) |
| SimListPage | `web/src/pages/sims/index.tsx` | CMP-sims-list | Views removed; bulk scaffolding (`useBulkStateChange`, `useBulkPolicyAssign`, `selectedIds`, `toggleSelect`, bulk action bar, Compare button, segment selector) RETAINED |
| OperatorComparePage | `web/src/pages/operators/compare.tsx` | CMP-operators-compare | Untouched; route `/operators/compare` registered at `web/src/router.tsx:145` inside `<ProtectedRoute />` tree (auth preserved) |
| SavedViewsMenu | `web/src/components/shared/saved-views-menu.tsx` | shared | Retained per D-218-3; tree-shakeable (zero importers after this story) |

### AC Summary
| # | Criterion | Status | Gaps |
|---|-----------|--------|------|
| AC-1 | Views button removed from Operators/APNs/IP-Pools/SIMs/Policies/Sessions | PASS | None — 4 widget-bearing pages cleaned; Sessions + IP-Pools verified pre-absent |
| AC-2 | Operators checkbox column removed | PASS | None — checkbox + state + handler + Compare button all removed |
| AC-3 | Component + hook + backend retained for future reintroduction | PASS | None — `SavedViewsMenu`, `useSavedViews`, `/users/me/views` routes + `user_views` table untouched |

## Findings

### F-A1 | LOW | performance
- Title: `SavedViewsMenu` is now an orphan module
- Location: `web/src/components/shared/saved-views-menu.tsx`, exported from `web/src/components/shared/index.ts:16`
- Description: After this story, zero app code imports `SavedViewsMenu`. Tree-shaking should drop it from the bundle. The barrel re-export in `index.ts` may still anchor it if any consumer imports the barrel with dynamic access — spot check shows barrel consumers import named symbols, so ESM tree-shaking should work. Intentional per D-218-3 (kept for AC-3 future reintroduction).
- Fixable: YES (deferred by plan)
- Suggested fix: None for this story. Future cleanup story may drop the component + hook + `user_views` table if reintroduction is ruled out.

### F-A2 | LOW | compliance
- Title: `useSavedViews` hook now has zero consumers
- Location: `web/src/hooks/use-saved-views.ts`, silent-path allow-list `web/src/lib/api.ts:90`
- Description: The hook is only referenced by the now-orphan `SavedViewsMenu`. Retained per D-218-3. No runtime impact (hook is only invoked when the component mounts; component no longer mounts anywhere).
- Fixable: YES (deferred)
- Suggested fix: None for this story.

## Residual Symbol Scan (clean)

- `SavedViewsMenu` in pages: **0 hits** (only component self-definition + barrel export remain — expected).
- `selectedIds` / `toggleSelect` / `Checkbox` in `web/src/pages/operators/index.tsx`: **0 hits**. Compare button block also removed.
- `Checkbox` import scan across codebase: still used by `policies/index.tsx`, `roaming/*`, `webhooks/index.tsx`, `notifications/preferences-panel.tsx`, `column-customizer.tsx` — none of these are affected by this story. Component file `components/ui/checkbox.tsx` untouched.
- `/operators/compare` route: still registered at `router.tsx:145` under `<ProtectedRoute />`; direct URL remains auth-gated as designed in D-218-2.
- No broken imports: `type React` import dropped only from `operators/index.tsx` where it was only used by removed `toggleSelect(e: React.MouseEvent)`; `policies/index.tsx` retains `import type React` because its own `toggleSelect` (out-of-scope) still uses it.

## Preserved Scaffolding Verification (load-bearing)

- **Policies** (`policies/index.tsx`): `selectedIds` state (L105), `toggleSelect` (L107), `Checkbox` import (L3), Compare button (L196-205), checkbox column (L336-343) — all UNCHANGED.
- **SIMs** (`sims/index.tsx`): `useBulkStateChange` + `useBulkPolicyAssign` imports (L51), `selectedIds` (L127), `toggleSelect`/`toggleSelectAll` (L279/288), bulk action bar (L790-876), Compare button (L379-388), Import SIMs + policy dialog — all UNCHANGED.
- **Backend**: `internal/api/user/views_handler.go`, `internal/store/user_view.go`, router registration at `internal/gateway/router.go:299-303`, `user_views` table — confirmed untouched by step-log (scope explicitly FE-only).

## Compliance (Pass 2)

- **Design tokens**: No new hex/rgba introduced. Pre-existing `rgba(...)` at `operators/index.tsx:53-55` (healthGlow) is unchanged by this story — not a regression. No hardcoded spacing/typography added.
- **shadcn/ui discipline**: No raw HTML (`<input>`, `<button>`, `<dialog>`) introduced. Removed elements (`<Checkbox>`, custom `<Button>` Compare) were already shadcn-compliant. `<SavedViewsMenu>` was a composed shared component.
- **ARCHITECTURE.md**: Atomic design boundaries respected — edits only at page layer (pages/), no atoms/molecules/organisms touched.
- **ADRs**: No ADR impact (pure FE deletion).
- **bug-patterns.md**: Not present (skipped per scout protocol).
- **Makefile**: No new services/scripts — no update needed.

## Security (Pass 2.5)

- **OWASP grep** (SQL inj, XSS, path traversal, hardcoded secrets, insecure random, CORS wildcard) across the 4 edited files: **0 hits**. Diff is pure deletion, no new code paths.
- **Auth & access control**: `/operators/compare` route remains inside `<ProtectedRoute />` wrapper at `router.tsx:129` → `router.tsx:145`. Removing the UI entry point does NOT expose a previously-privileged route; route was already auth-gated and still is. D-218-2 explicitly retains the route for direct-URL access, which is correct: auth guard handles unauthorized users.
- **Input validation**: N/A — no new inputs.
- **Mock retirement**: N/A — no mocks affected.
- **Dependency CVE audit**: Skipped — pure deletion, no dependency changes. `package-lock.json` untouched.

## Performance (Pass 4)

- **Query analysis**: No DB queries added/modified. Removing `SavedViewsMenu` mount from 4 pages eliminates 4× `GET /users/me/views?page=...` calls on page load (small perf win, also observable in smoke test step 8).
- **Frontend perf**:
  - Bundle impact: SHRINK. `SavedViewsMenu` + `useSavedViews` become orphan modules; Vite/Rollup tree-shaking via ESM named exports should drop them from the build. Step-log shows `web-build=PASS(2.56s)` — confirmed building.
  - Lazy loading: `OperatorListPage` / `ApnListPage` / `PolicyListPage` / `SimListPage` already lazy-loaded in `router.tsx` — untouched.
  - Memoization: No impact — deletions do not alter render dependencies.
  - Re-renders: Operators page now drops `selectedIds` state, reducing re-renders on hover-based checkbox interactions (net positive).
- **API perf**: Fewer network calls on list page mounts (4 endpoints no longer called from the 4 pages).

### Queries Analyzed
| # | File:Line | Pattern | Issue | Severity |
|---|-----------|---------|-------|----------|
| 1 | n/a | n/a | No queries added/modified | — |

### Caching Verdicts
| # | Data | Location | TTL | Decision |
|---|------|----------|-----|----------|
| 1 | Saved views | — | — | SKIP (no caching change; feature dormant) |

## Non-Fixable (Escalate)

None. All findings are LOW-severity dead-code concerns explicitly covered by plan decisions D-218-3.

## Summary

Implementation exactly matches plan. All three ACs satisfied. Residual scans clean on all 4 target pages. Preserved scaffolding (Policies + SIMs + backend + `SavedViewsMenu` component + `useSavedViews` hook + `/operators/compare` route) verified intact. No compliance/security/performance regressions. Bundle expected to shrink (dead-code tree-shaken). 2 LOW findings are dead-code follow-ups, already acknowledged in plan risks — not blockers.

**Verdict: PASS**

</SCOUT-ANALYSIS-FINDINGS>
