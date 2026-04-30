# FIX-218 Plan — Views Button Removal + Operators Checkbox Cleanup

- **Story:** `docs/stories/fix-ui-review/FIX-218-views-button-removal.md`
- **Findings addressed:** F-59, F-60, F-66, F-71, F-90 (+ related F-120 scoped out)
- **Priority / Effort / Wave:** P2 · S · Wave 5
- **Phase track:** UI Review Remediation (dev phase, not maintenance)
- **Scope:** Frontend cleanup only. Backend `user_views` endpoints and `user_views` table remain intact (AC-3: future reintroduction).

---

## 1. AC Analysis & Scope Correction

Story AC lists six target pages for the "Views" button. Code audit shows only four currently render `SavedViewsMenu`:

| Page | Current State | Action |
|---|---|---|
| `/operators` | `SavedViewsMenu page="operators"` at line 401 | Remove |
| `/apns` | `SavedViewsMenu page="apns"` at line 383 | Remove |
| `/policies` | `SavedViewsMenu page="policies"` at line 207 | Remove |
| `/sims` | `SavedViewsMenu page="sims"` at line 376 | Remove |
| `/sessions` | No Views button (verified) | No-op — document in AC-1 sign-off |
| `/settings/ip-pools` | No Views button (verified) | No-op — document in AC-1 sign-off |

**Decision D-218-1:** AC-1 wording lists 6 pages, but only 4 have the widget. Sessions and IP Pools get a "verified no-op" sign-off line in the step log. Spec not amended (the intent — "remove everywhere it exists" — is satisfied).

### AC-2: Operators checkbox column removal

`OperatorListPage` uses `selectedIds: Set<string>` for a **3-way Compare** affordance (max 3 operators):
- Per-card `<Checkbox>` (line 441–445) — toggles `selectedIds`.
- Conditional "Compare (N)" button (line 391–400) — appears when `selectedIds.size >= 2`, routes to `/operators/compare?ids=…`.

Story AC-2: "checkbox column removed — **no bulk operator action planned**." Compare is selection-driven multi-row nav (not a bulk *mutation*), but the widget it powers has no alternative entry point today.

**Decision D-218-2:** Remove checkbox AND Compare button together. `/operators/compare` route stays (accessible via direct URL — future story can reintroduce a "Compare operators" affordance via RowActions "Add to Compare" or a dedicated selection UI if demand surfaces). This matches AC-2 literal wording and the user directive ("simpler UX, no bulk-select bar").

`selectedIds` state, `toggleSelect` fn, and `Checkbox` import are removed. Navigate-dependent routes unaffected.

### AC-3: Future reintroduction

FE-only removal. Do NOT touch:
- `web/src/hooks/use-saved-views.ts` (hook retained — dead code until reintroduction, small footprint).
- `web/src/components/shared/saved-views-menu.tsx` (component retained).
- `web/src/components/shared/index.ts` export line (retained).
- Backend: `internal/api/user/views_handler.go`, `internal/store/user_view.go`, router registrations at `internal/gateway/router.go:299-303`, `user_views` table.

**Rationale:** Story says future reintroduction "with full implementation"; keeping the scaffolding costs ~5KB and zero runtime load (lazy-imported only from pages we're editing — after removal, nothing imports it). We tree-shake the component out of the SPA bundle automatically. Backend endpoints stay because `schemacheck` references the table and migrating it out is a separate concern.

**Decision D-218-3:** Leave `SavedViewsMenu` component + `useSavedViews` hook + backend in place. Remove only the page-level imports and render call-sites. Flag "consider deleting component + backend in a future cleanup story" in Risks.

### F-120 (Policies checkbox) — OUT OF SCOPE

F-120 suggests Policies bulk Activate/Deactivate/Clone *may* want the checkbox. Story FIX-218 AC-2 scopes checkbox removal to Operators ONLY. Policies checkboxes + SIMs checkboxes STAY. SIMs already uses `useBulkStateChange`/`useBulkPolicyAssign` (confirmed in imports) — checkbox is load-bearing. Policies uses `selectedIds` for a similar Compare button; we leave it untouched for this story.

---

## 2. Files to Touch (exhaustive)

| # | File | Lines | Changes |
|---|---|---|---|
| 1 | `web/src/pages/operators/index.tsx` | 3, 20, 333, 358–365 (toggleSelect), 391–400 (Compare btn), 401 (SavedViewsMenu), 441–446 (Checkbox) | Remove `Checkbox` import, `SavedViewsMenu` import, `selectedIds` state, `toggleSelect` fn, Compare button block, SavedViewsMenu render, Checkbox render |
| 2 | `web/src/pages/apns/index.tsx` | 37 (import), 383 (render) | Remove `SavedViewsMenu` import + render call |
| 3 | `web/src/pages/policies/index.tsx` | 57 (import), 207 (render) | Remove `SavedViewsMenu` import + render call. Keep `selectedIds` + Compare button (out of scope). |
| 4 | `web/src/pages/sims/index.tsx` | 75 (import), 376 (render) | Remove `SavedViewsMenu` import + render call. Keep Compare button + `selectedIds` (bulk state). |

**Files NOT touched:**
- `web/src/pages/sessions/index.tsx` — no Views button present.
- `web/src/pages/settings/ip-pools.tsx` — no Views button present.
- `web/src/components/shared/saved-views-menu.tsx` — retain for future.
- `web/src/components/shared/index.ts` — retain `SavedViewsMenu` export.
- `web/src/hooks/use-saved-views.ts` — retain.
- Backend — retain.

---

## 3. Task Decomposition (single wave)

Effort = S; 4 page files, each modification is an import-drop + a JSX-drop. Group into ONE wave with 2 tasks for implementation ergonomics.

### Wave 1 — Implementation

**Task 1 — Operators page: checkbox + Compare + Views removal**
- File: `web/src/pages/operators/index.tsx`
- Changes:
  - Line 3: remove `import { Checkbox } from '@/components/ui/checkbox'`.
  - Line 20: remove `import { SavedViewsMenu } from '@/components/shared/saved-views-menu'`.
  - Line 333: remove `const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())`.
  - Line ~358–365: remove `toggleSelect` callback.
  - Lines 391–400: remove `{selectedIds.size >= 2 && ( … Compare button … )}` block.
  - Line 401: remove `<SavedViewsMenu page="operators" />`.
  - Lines 441–446 (inside card map): remove the `<Checkbox … />` element; collapse the surrounding `<div className="absolute top-2 right-2 …">` so `RowActionsMenu` remains the sole child (or simplify the container if only RowActionsMenu remains).
- Self-check: `tsc --noEmit` on the web project must remain clean. No references to `selectedIds`, `Checkbox`, `SavedViewsMenu`, `toggleSelect` after edit.

**Task 2 — APNs + Policies + SIMs: Views button removal**
- Files:
  - `web/src/pages/apns/index.tsx`: drop line 37 import, drop line 383 render.
  - `web/src/pages/policies/index.tsx`: drop line 57 import, drop line 207 render. Do NOT touch `selectedIds`/Compare button.
  - `web/src/pages/sims/index.tsx`: drop line 75 import, drop line 376 render. Do NOT touch `selectedIds`, Compare button, or bulk-action scaffolding.
- Self-check: `tsc --noEmit` clean. `grep 'SavedViewsMenu' web/src/pages/**` returns zero results after all four files edited.

### (No Wave 2) — Scope is pure deletion; no new tokens, no new components, no backend changes.

---

## 4. Test Strategy

- **Unit / integration tests:** Deferred to D-091 (no test runner mandate during UI Review track).
- **Manual smoke test** (required before Gate):
  1. `make web-build` must succeed (type-check + build).
  2. `/operators` — Views button absent, no checkboxes on cards, no Compare button visible, RowActionsMenu still works.
  3. `/apns` — Views button absent; filters + Create APN + Export still work.
  4. `/policies` — Views button absent; checkboxes on rows STILL PRESENT; Compare button still appears when ≥2 selected.
  5. `/sims` — Views button absent; row checkboxes STILL PRESENT; Compare + Import SIMs still work; bulk state-change bar still surfaces.
  6. `/sessions` — no visible change (baseline confirmed).
  7. `/settings/ip-pools` — no visible change (baseline confirmed).
  8. Browser Network tab: no `GET /api/v1/users/me/views?page=…` calls fire from any of the 4 edited pages.
- **Regression risk probes:**
  - `/operators/compare` direct URL still loads (route intact).
  - `/policies/compare?ids=…` still works from Policies page (Compare button retained there).
  - `/sims/compare?sim_id_a=…` still works.

---

## 5. Design Token Map

Pure deletions — no new tokens introduced, no tokens removed from the palette. Tokens in `docs/FRONTEND.md` unchanged.

Edge case: the Operators card's top-right hover affordance (`absolute top-2 right-2 flex items-center gap-1 opacity-0 group-hover:opacity-100`) currently holds `<Checkbox>` + `<RowActionsMenu>`. After checkbox removal, the same container holds only `RowActionsMenu`. No layout regression expected — container keeps `flex items-center gap-1` which degrades gracefully to a single child. **Verify visually** during smoke test.

---

## 6. Decisions & Risks

### Decisions

- **D-218-1:** AC-1 page list over-counts (Sessions, IP Pools) — document no-op instead of amending spec.
- **D-218-2:** Remove Operators Compare button alongside checkbox (its only entry point). `/operators/compare` route retained for direct access.
- **D-218-3:** Retain `SavedViewsMenu` component + `useSavedViews` hook + backend endpoints + `user_views` table for AC-3 future reintroduction. Tree-shaking eliminates the component from bundle.
- **D-218-4:** Policies and SIMs checkboxes retained (out of story scope; F-120 explicitly deferred).

### Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| User muscle-memory loss of Views / Compare on Operators | Low | Story explicitly user-requested; no docs reference these widgets. |
| Dead-code accumulation (`SavedViewsMenu` unused) | Low | Flagged in Risks. Tree-shake removes from bundle. Future cleanup story can drop component + backend. |
| `/operators/compare` becomes unreachable via UI | Low-Med | Route preserved; direct URL works. RowActions "Add to Compare" can be added in a future story if demand emerges. |
| Layout regression on Operators card hover (single-child flex container) | Low | Smoke-test verification step 2; container collapse visually neutral. |
| TypeScript unused-import lint error on `Checkbox` | Low | Task 1 explicitly drops the import line. |

---

## 7. Quality Gate Self-Check

- [x] All ACs mapped to tasks (AC-1 → Tasks 1+2; AC-2 → Task 1; AC-3 → no-op by decision).
- [x] Files list is exhaustive — verified via grep of `SavedViewsMenu` usages (4 pages) and `selectedIds` in operators page.
- [x] Scope correction documented (Sessions + IP Pools no-op) in AC Analysis.
- [x] Out-of-scope items explicitly flagged (Policies checkbox, SIMs checkbox, backend endpoints, component deletion).
- [x] Regression risks identified and mitigated.
- [x] Test strategy defined (manual smoke test, type-check, build).
- [x] Design tokens: no new tokens required.
- [x] Task size: 1–2 files per task; single wave; effort = S matches story estimate.

**Plan status:** READY for implementation.
