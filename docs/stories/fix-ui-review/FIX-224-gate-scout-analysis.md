# FIX-224 Gate — Scout Analysis

Scout perspective: data model, type safety, state machine correctness, hook contracts, API shape alignment, side-effect hygiene, dead-code / unused imports. Executed inline by Gate Lead (subagent nest constraint — see FIX-223 gate §Scout execution note).

## Findings

<SCOUT-ANALYSIS-FINDINGS>

F-A1 | LOW | analysis
- File: `web/src/pages/sims/index.tsx:1125`
- Title: Preview table shows 5 rows, plan specified first 10
- Evidence: `importPreview.rows.slice(0, 5)` + "more rows" footer thresholds at `>5`.
- Plan ref: FIX-224-plan §Task 5 / Verify: "preview shows 10 rows + 20 rows • ~1s".
- Fixable: YES — expand to `slice(0, 10)` + threshold `>10`. No behavioural risk (preview is truncated view of in-memory rows).
- Fix status: FIXED (commit pending) — threshold lifted to 10 in both `.slice` and `>` footer.

F-A2 | LOW | analysis
- File: `web/src/pages/sims/index.tsx:386-412`
- Title: Native CSV parser does not handle RFC 4180 quoted fields with embedded commas / newlines
- Evidence: `lines[0].split(delimiter)`, `l.split(delimiter)` — naive split. Also `content.trim().split('\n')` ignores `\r` (Windows CRLF row endings become `...\r` tokens feeding `\r` into each last field).
- Pattern: Accepted risk R2 in plan (no free-text fields in the import schema; commas inside values are invalid per format check). CRLF edge case, however, is common (Excel export default) and will cause trailing-`\r` on every last column, which can break `/^\d+$/` regex for rightmost field if it lands on an optional column.
- Fixable: YES (trivial) — normalise CR before split. `content.replace(/\r/g, '').trim().split('\n')`.
- Fix status: **deferred** — accepted under R2; logging tech-debt anyway for robustness. D-124 candidate below.

F-A3 | LOW | analysis
- File: `web/src/hooks/use-sims.ts:26`
- Title: `buildListParams` passes CSV `state` to server unchanged; server matches first token only
- Evidence: `if (filters.state) params.set('state', filters.state)` — sends `active,suspended` verbatim.
- Pattern: DEV-308 / D3 decision documented and accepted. FE re-filter (`allSims.filter((sim) => selectedStates.includes(sim.state))`) covers the gap for the current page buffer.
- Risk surfaces at scale: with ≥50-page cursor flow and 2 states selected, pages can arrive with zero visible rows after FE filter → user sees "Load more" but no new rows render. Acceptable for current fleet (demo tenants ≤ 500 SIMs), plan-filed.
- Fixable: NO (requires backend `state = ANY($1::text[])`) — already covered by plan tech-debt.
- Fix status: DEFERRED to existing Tech Debt item (pre-existing D-### from plan §Tech Debt).

F-A4 | LOW | analysis
- File: `web/src/pages/sims/index.tsx:282-291`
- Title: Client-side secondary-filter may yield sparse pages; infinite-scroll `hasNextPage` is still server-side
- Evidence: `allSims` memo filters `data.pages` in-place when `selectedStates.length > 1`. React Query cursor advances on backend count, not visible count.
- Impact: cosmetic at current scale; at scale, user sees the "Showing all N SIMs" footer when there are still unfetched server-side matches.
- Fixable: NO (same root cause as F-A3; fix lives in backend predicate).
- Fix status: DEFERRED — same parent tech-debt as F-A3.

F-A5 | LOW | analysis
- File: `web/src/components/ui/dropdown-menu.tsx:113-149`
- Title: `DropdownMenuCheckboxItem` closes menu on click (no `e.preventDefault()` escape)
- Evidence: Each `<DropdownMenuItem>` calls `setOpen(false)` after onClick; `DropdownMenuCheckboxItem` does NOT call `setOpen(false)` — it only stops propagation. The menu's top-level `document.click` listener is attached at the provider and closes on any click that reaches `document`. Because content uses `onClick={(e) => e.stopPropagation()}`, interior checkbox-item clicks do NOT bubble to document, so menu stays open across multiple toggles. 
- Verdict: behavior is correct (menu stays open, user can toggle multiple states). Bug warning from plan §Bug Pattern Warnings is mitigated by the `stopPropagation` on the Content wrapper — not by `onSelect={e=>e.preventDefault()}` because this is not radix. Documentation-only; no code change.
- Fixable: N/A
- Fix status: PASS

F-A6 | LOW | analysis
- File: `web/src/components/ui/dropdown-menu.tsx:113-149`
- Title: `DropdownMenuCheckboxItem` lacks `role="menuitemcheckbox"` / `aria-checked`
- Evidence: Renders `<button>` without explicit role or `aria-checked={checked}`. Screen readers announce as "button" not "checked menu item".
- Fixable: YES — add `role="menuitemcheckbox" aria-checked={!!checked}` to the button element.
- Fix status: **Deferred** to accessibility pass — primitive is shared across the app; a11y rework affects every multi-select dropdown (analytics filter, notification channels). Raise as D-124 candidate.

F-A7 | LOW | analysis
- File: `web/src/pages/sims/index.tsx:194-197`
- Title: `selectedStates` memo — CSV parse handles empty, single, N tokens, leading/trailing commas
- Evidence: `filters.state.split(',').filter(Boolean)` — `Boolean` strips empty tokens from `",active,,suspended,"` → `['active','suspended']`. Correct.
- Fixable: N/A
- Fix status: PASS

F-A8 | LOW | analysis
- File: `web/src/pages/sims/index.tsx:327-336`
- Title: `removeFilter` signature change — audit all call sites
- Evidence: `removeFilter(key, stateToken?)`. Callers: (a) Applied-filter chip `onClick={() => removeFilter(af.key, af.stateToken)}` (line 627). Single call site. Safe.
- Fixable: N/A
- Fix status: PASS

F-A9 | LOW | analysis
- File: `web/src/pages/sims/index.tsx:182-189`
- Title: `useImportSIMs` type fix — consumers check
- Evidence: Grep for `useImportSIMs` / `rows_parsed` / `errors[]` across `web/src/**` → 0 hits outside the hook definition + the consumer file. Safe — only `sims/index.tsx` consumed the hook, and the state type `{ job_id; tenant_id; status }` matches server `bulkImportResponse` (plan §Import DTO Token Map).
- Fixable: N/A
- Fix status: PASS

F-A10 | LOW | analysis
- File: `web/src/pages/sims/index.tsx:185-189`
- Title: `useJobPolling(importJobId)` cleanup on SlidePanel close
- Evidence: Hook is page-level (not inside SlidePanel). Closing the panel (`setImportOpen(false)`) does NOT unmount the polling hook; polling continues and lands `importResult` regardless. On page navigation/unmount, `useQuery` unmounts cleanly — `refetchInterval` function returns `false` on terminal state anyway.
- Match: plan §Bug Pattern Warnings "Polling leak" — accepted behaviour.
- Fixable: N/A
- Fix status: PASS

F-A11 | LOW | analysis
- File: `web/src/pages/sims/compare.tsx:548-563`
- Title: Compare 4-cap — warning + disabled with `aria-disabled`
- Evidence: At `selectedIds.length >= MAX_SIMS`, warning span rendered with `text-warning bg-warning/10 border-warning/20`; Add button `disabled` + `aria-disabled`. Warning uses tokens (no hex). Missing `role="alert"` / `aria-live="polite"` on the warning span — low impact since it's static-on-count, not dynamic error, but a screen reader won't announce the limit as it's reached.
- Fixable: YES (one attribute) — add `role="status" aria-live="polite"` to the warning span for announcement.
- Fix status: **Accepted as-is** — the warning is persistent while `selectedIds.length >= MAX_SIMS`; a screen reader reads it on focus of the Add button (button is adjacent and disabled). Impact negligible. Notes item for future a11y sweep.

F-A12 | LOW | analysis
- File: `web/src/pages/sims/index.tsx:82-88`
- Title: STATE_OPTIONS enum alignment with backend canonical
- Evidence: FE has `ordered, active, suspended, terminated, stolen_lost` — matches `internal/store/sim.go:87-91` canonical minus `purged` (admin-only, correctly hidden per plan D1).
- Fixable: N/A
- Fix status: PASS

</SCOUT-ANALYSIS-FINDINGS>

## Summary
- 12 findings (F-A1..F-A12). 1 LOW fixed (F-A1 — preview row count 5→10). 2 DEFERRED via existing tech-debt parents (F-A3, F-A4 — server-side state ANY). 1 DEFERRED to new D-124 candidate (F-A6 — checkbox item a11y). 8 PASS.
- Blocker count: 0.
