# FIX-216 Gate Scout UI Report — Modal Pattern Standardization

**Scout:** Gate Scout 3 (UI/Frontend)
**Date:** 2026-04-22
**Story:** FIX-216 — Dialog vs SlidePanel semantic split (Option C)
**Scope:** FRONTEND.md rule doc + 3 code files (SIMs, Violations, IP Pool) + primitives audit + ROUTEMAP Tech Debt entry

---

## Summary

| Severity | Count | In-scope for FIX-216 |
|----------|-------|----------------------|
| CRITICAL | 0 | 0 |
| MAJOR    | 0 | 0 |
| MINOR    | 2 | 2 |
| OBSERVATION (cross-page, non-blocking) | 3 | 0 |

**Verdict:** **PASS** — all six ACs are materially satisfied; only MINOR documentation-cleanup items remain. No regressions to primitives, no hex/Tailwind-gray leaks in touched files, accessibility contract upheld.

---

## 1. Modal Pattern rule quality — `docs/FRONTEND.md` L108-176

**Finding M-1 (PASS with MINOR):** The canonical rule text is complete, internally consistent, and matches Option C.

Coverage checklist:
- [x] Dialog "when to use" covers confirm / destructive / ≤2 fields (L114-118)
- [x] SlidePanel "when to use" covers rich form / detail / list-with-search / row-expand (L127-131)
- [x] Structure (canonical) for both primitives (L120-123, L133-138)
- [x] Quick decision tree present (L140-144)
- [x] Visual contract AC-5 (L146-150): `variant="default"` + `variant="outline"` (+ `variant="destructive"`) for Dialog; SlidePanel header via `title`/`description` props only
- [x] Dark mode AC-6 clause (L152-154)
- [x] Accessibility asymmetry explained (L156-159): Dialog has no focus-trap by design (≤2 focusable elements); SlidePanel has focus-trap + aria-modal + restore-focus
- [x] Current usage map table (L161-172) covers all 7 screens touched or referenced by FIX-216
- [x] AC-4 ESLint deferral note (L174-176) cross-references ROUTEMAP Tech Debt

**MINOR finding UI-MIN-1:** L176 references "Tech Debt D-XXX" as placeholder; the actual entry in `docs/ROUTEMAP.md` L683 is `D-090`. FRONTEND.md should say `D-090` to close the cross-reference loop. Not blocking — trivial edit.

**MINOR finding UI-MIN-2:** The usage-map row for "Alerts row preview" (L172) still reads `_(future)_` with "Not yet implemented — use SlidePanel when added". This is correct per plan Scope Decision 3, but the row would read more cleanly with a consistent `—` or `TBD` in the Screen column instead of italicizing only half the cell. Cosmetic only.

---

## 2. Swap semantic correctness

### 2.1 SIMs — Bulk state-change (`web/src/pages/sims/index.tsx` L936-968)

- **Pattern:** Dialog (centered, compact)
- **Field count:** 1 (reason `<Input>` only) — well within Dialog's ≤2-field envelope (L114-117 of FRONTEND.md)
- **Title:** dynamic template with proper pluralization (L940) — matches existing SlidePanel title conventions used across the codebase
- **Description:** present via `<DialogDescription>` (L941) — explains action consequence
- **Footer contract (AC-5):** PASS
  - Cancel: `<Button variant="outline">` (L954)
  - Primary: `<Button variant={bulkDialog?.action === 'terminated' ? 'destructive' : 'default'}>` (L958) — correctly routes Terminate to destructive styling, Suspend/Resume to default
- **Loading state:** preserved — `bulkMutation.isPending` disables button and renders `Loader2` spinner (L963-964)
- **Verdict:** **CORRECT per Option C.** Clean swap.

### 2.2 SIMs — Assign Policy (`web/src/pages/sims/index.tsx` L1113-1192)

- **Pattern:** SlidePanel (right-sheet, rich form)
- **Field count:** 1 `Select` + preview block + (implicit segment-or-selected-ids branch) — this is arguably **borderline** for Option C's "3+ fields" threshold; however, Option C explicitly also covers "list pickers with search" (L130 FRONTEND.md) and "row-expand / rich form" flows. The policy picker has a preview panel that makes the flow read-heavy, which matches SlidePanel semantics. **Verdict: CORRECT** — acceptable under "rich form" clause even if raw field count is 1.
- **Header contract:** `title` + `description` props used (L1116-1117). No hand-rolled header. `width="md"` appropriate.
- **Title:** dynamic template preserves `selectAllSegment` branch (L1116) — entire-segment count shown when applicable
- **Footer contract (AC-5):** PASS
  - Cancel: `<Button variant="outline">` (L1158)
  - Primary: `<Button>` (default variant implied) with disabled state + spinner (L1161-1190)
  - `<SlidePanelFooter>` used (L1157) — not a hand-rolled `<div>`
- **Loading state:** preserved — `bulkPolicyAssignMutation.isPending` drives disabled + spinner
- **Verdict:** **CORRECT per Option C.**

### 2.3 Violations — Row expand (`web/src/pages/violations/index.tsx` L509-585)

- **Pattern:** SlidePanel (right-sheet, read-heavy detail)
- **Semantic fit:** PASS — detail inspection with 9-field metadata grid + dynamic `details` object — matches FRONTEND.md L129 ("Detail inspection panes, read-heavy, long content").
- **Header:** `title` dynamic (L512, violation name + policy) + `description` locale-formatted timestamp (L513) + `width="lg"` — CORRECT.
- **Body:** metadata grid (L518-566), detail key-value grid (L567-579). Content preserved from pre-refactor inline expand (per step-log STEP_2 W2 T4).
- **Footer:** `<SlidePanelFooter>` with Close `variant="outline"` (L582-584). No primary action — appropriate; actions are in the row dropdown.
- **Empty state:** `<EmptyState>` preserved (L418-423). PASS.
- **Loading/error states:** Skeleton grid (L280-287) + danger card with Retry (L290-296) preserved. PASS.
- **Verdict:** **CORRECT per Option C.**

### 2.4 IP Pool Reserve (`web/src/pages/settings/ip-pool-detail.tsx` L388-454)

- **Pattern:** SlidePanel (already compliant per plan; Gate scout confirms)
- **Header:** `title="Reserve IP Addresses"` + `description` dynamic (L392-393) + `width="lg"`. Inline header (no hand-rolled).
- **Body:** SIM search + queue list + currently-reserved list (multi-section rich form — 3+ logical fields). Correct SlidePanel fit.
- **Footer:** `<SlidePanelFooter>` (L447) with Cancel `variant="outline"` + primary Reserve with spinner. Step-log records this as a **GAP FIXED** (hand-rolled footer `<div>` → `SlidePanelFooter`).
- **Verdict:** **CORRECT per Option C; AC-5 conformance achieved.**

---

## 3. Dialog AC-5 visual rules in code (`sims/index.tsx` bulk-state-change)

| Rule | Code location | Status |
|------|--------------|--------|
| `<Dialog>` wraps `<DialogContent>` | L937-938 | PASS |
| `onClose` callback wired | L938 | PASS |
| `<DialogHeader>` + `<DialogTitle>` + `<DialogDescription>` | L939-942 | PASS |
| Body ≤2 fields | 1 Input (reason) | PASS |
| `<DialogFooter>` | L953 | PASS |
| Cancel: `variant="outline"` | L954 | PASS |
| Primary: `default` or `destructive` variant ternary | L958 (`bulkDialog?.action === 'terminated' ? 'destructive' : 'default'`) | PASS |
| Disabled + spinner on pending | L960-964 | PASS |
| No raw `<button>` inside DialogContent | grep count = 0 | PASS |

---

## 4. SlidePanel pattern conformance (all three SlidePanels in target files)

| Property | `sims.Assign Policy` | `violations.detail` | `ip-pool.reserve` |
|----------|---------------------|---------------------|-------------------|
| `title` prop | L1116 | L512 | L392 |
| `description` prop | L1117 | L513 | L393 |
| `width` prop | `md` | `lg` | `lg` |
| No hand-rolled header | PASS | PASS | PASS |
| `<SlidePanelFooter>` used | L1157 | L582 | L447 |
| No raw `<button>` in body | PASS (0) | PASS (0) | PASS (0) |
| Cancel `variant="outline"` | L1158 | L583 | L448 |
| Primary variant appropriate | default | (Close only) | default | 

All three SlidePanels fully conform to the FRONTEND.md canonical structure.

---

## 5. Accessibility audit

### 5.1 Dialog (primitive-level, `web/src/components/ui/dialog.tsx`)
- ESC closes: L13-14 PASS
- Body scroll lock on open: L18 PASS
- Backdrop click closes: L32 PASS
- **Gap (documented, intentional):** No `aria-modal` attribute on Dialog container. No focus-trap. Per FRONTEND.md L157-158 this is intentional — Dialog scope is ≤2 focusable elements; native Tab cycling is sufficient. **Non-blocker for FIX-216** (asymmetry explicitly documented).
- **Observation UI-OBS-1:** If a future Dialog ever grows to 3+ focusables, the lack of `aria-modal` + focus-trap becomes a real a11y gap. FRONTEND.md doc already nudges toward "convert to SlidePanel" in that case (L158). ESLint rule D-090 is the systemic mitigation.

### 5.2 SlidePanel (primitive-level, `web/src/components/ui/slide-panel.tsx`)
- `role="dialog"` + `aria-modal="true"`: L78-79 PASS
- `aria-labelledby` / `aria-describedby`: L80-81 PASS (wired from title/description props)
- ESC closes: L37-40 PASS
- Focus trap (Tab/Shift+Tab cycling): L41-55 PASS — delivered by FIX-215 hardening, NOT regressed
- Initial focus on close button: L32-34 PASS
- Restore focus on close: L29-30 + L63 PASS
- Close button has `aria-label="Close panel"`: L110 PASS

### 5.3 Violations row a11y (`violations/index.tsx` L432-443)
- `role="button"`: L433 PASS
- `tabIndex={0}`: L434 PASS
- `aria-label` descriptive (includes violation type + SIM iccid): L435 PASS
- `onKeyDown` handler: L438-443 PASS
  - Enter triggers open (L439-441) PASS
  - Space triggers open (L439-441) PASS
  - `e.preventDefault()` on Space prevents page scroll: L440 PASS
- `onClick` preserved: L437 PASS
- Interactive children `stopPropagation`: inner `<Link>` (L449) and `<OperatorChip>` wrapper (L453) and DropdownMenuTrigger (L467) all stop propagation — PASS (prevents accidental row-open on nested clicks)

**Verdict:** Full a11y contract upheld. No regressions.

---

## 6. Dark-mode parity + token usage

### 6.1 Primitives
- `dialog.tsx` hex/gray scan: **0 matches**
- `slide-panel.tsx` hex/gray scan: **0 matches**
- Both primitives consume semantic tokens only: `bg-bg-elevated`, `bg-bg-surface`, `text-text-primary`, `text-text-tertiary`, `border-border`, `bg-black/60` (opacity utility, not literal color)

### 6.2 Target files (post-refactor)
- `sims/index.tsx` hex/rgba/gray scan: **0 matches** (L795 bulk-action-bar shadow now uses `shadow-[var(--shadow-card)]` token — correct per step-log STEP_2 W3 T6)
- `violations/index.tsx` hex/rgba/gray scan: **0 matches**. Note: `TYPE_COLORS` / `SEVERITY_CHART_FILLS` use `var(--color-*)` CSS vars (L104-116) — semantic, not literal hex.
- `ip-pool-detail.tsx` hex/rgba/gray scan: **0 matches**

### 6.3 Shadow token (L795 SIMs bulk-action-bar)
- Pre-refactor (regression risk): `shadow-[rgba(0,0,0,0.35)]`
- Post-refactor (current code L795): `shadow-[var(--shadow-card)]` — PASS

**Verdict:** Full token compliance. Zero hex, zero rgba literals, zero Tailwind grays in any target file.

---

## 7. Loading / empty / error state preservation

| Flow | Loading | Empty | Error |
|------|---------|-------|-------|
| SIMs bulk state-change (Dialog) | `Loader2` + disabled btn (L963-964) | n/a | toast on mutation failure (existing) |
| SIMs Assign Policy (SlidePanel) | `Loader2` + disabled btn (L1188) | n/a | toast on mutation failure (existing) |
| Violations list | Skeleton grid (L280-287) | `<EmptyState>` (L418-423) | danger card + Retry (L290-296) |
| Violations detail (SlidePanel) | n/a (synchronous from state) | guard `{selectedViolation && ...}` (L516) | n/a |
| IP Pool Reserve (SlidePanel) | `Loader2` + disabled btn (L449-450) | Clear-all + empty queue guard | toast (existing) |

All states preserved. No regressions.

---

## 8. ROUTEMAP Tech Debt entry (AC-4 deferral)

`docs/ROUTEMAP.md` L683:
```
| D-090 | FIX-216 AC-4 | ESLint rule to flag `Dialog` usage with >3 form fields
   (nudge toward SlidePanel per Option C). ROI vs PR review + FRONTEND.md
   Modal Pattern doc judged LOW; documented rule + human review sufficient.
   Revisit if Modal-Pattern violations recur in ≥3 stories. | future lint-infra
   wave | OPEN |
```

- [x] ID assigned (D-090, next available after D-089)
- [x] Source cross-references FIX-216 AC-4
- [x] Rationale present
- [x] Revisit trigger explicit ("≥3 stories")
- [x] Target wave set
- [x] Status OPEN

**Verdict:** PASS. See UI-MIN-1 above re: FRONTEND.md L176 pointing at `D-XXX` instead of `D-090`.

---

## 9. Cross-page observations (non-blocking, NOT part of FIX-216 scope)

These are **observations only** — not FIX-216 blockers per plan Scope Decision 3. Surface as candidates for future Modal Pattern drift (the D-090 "revisit if ≥3 stories" trigger).

### UI-OBS-1: Modal Pattern drift candidates (cross-page scan)
Files using `<Dialog>` AND with high `Input|Textarea|Select` identifier density (may indicate >3-field Dialog usage, violating Option C):

| File | Identifier count | Action recommended |
|------|------------------|-------------------|
| `web/src/pages/settings/api-keys.tsx` | 18 | FUTURE AUDIT — candidate for Dialog→SlidePanel swap if fields actually embedded in Dialog |
| `web/src/pages/admin/announcements.tsx` | 7 | FUTURE AUDIT |
| `web/src/pages/settings/users.tsx` | 7 | FUTURE AUDIT |

Note: grep identifier count over-counts (imports, types, etc.). A real audit should verify children of `<DialogContent>`. Deferred to D-090 ESLint rule scope.

### UI-OBS-2: Plan explicitly scoped only 3 code files
Other `<Dialog>` usages in `web/src/pages/**` (22 files total) are **out of scope** for FIX-216. Option C rule now governs future PRs. No action required for this story.

### UI-OBS-3: `ip-pool-detail.tsx` Reserve SlidePanel semantic fit
Strictly speaking the flow is a **list picker with search + preview** — exactly what FRONTEND.md L130 recommends SlidePanel for. No concern; included here only because audit path required explicit confirmation.

---

## 10. Enforcement matrix (ACs vs Gate Scout evidence)

| AC | Evidence | Verdict |
|---|----------|--------|
| **AC-1** Modal Pattern section in FRONTEND.md | L108-176, full structure + decision tree + visual contract + a11y asymmetry + usage map | **PASS** (MINOR: UI-MIN-1 D-XXX placeholder) |
| **AC-2 item 1** SIMs state-change → Dialog | `sims/index.tsx` L936-968 | **PASS** |
| **AC-2 item 2** SIMs Assign Policy → SlidePanel | `sims/index.tsx` L1113-1192 | **PASS** |
| **AC-2 item 3** IP Pool Reserve → SlidePanel (audit) | `ip-pool-detail.tsx` L388-454 | **PASS** (gap fixed: hand-rolled footer → `SlidePanelFooter`) |
| **AC-2 item 4** APNs Connected SIMs KEEP SlidePanel | Not touched (confirmed in FRONTEND.md usage map L168) | **PASS** (no-op) |
| **AC-2 item 5** Alerts preview future → SlidePanel | Doc-only, FRONTEND.md L172 | **PASS** (future) |
| **AC-3** Violations row expand → SlidePanel (F-171) | `violations/index.tsx` L432-443 (row) + L509-585 (panel) | **PASS** |
| **AC-4** ESLint rule | Deferred; D-090 entry in ROUTEMAP L683 | **PASS** (DEFER per plan) |
| **AC-5** Visual consistency | §3 (Dialog) + §4 (SlidePanel) matrices all PASS | **PASS** |
| **AC-6** Dark-mode parity | §6 zero-hex/zero-rgba/zero-gray scan across all touched files + primitives | **PASS** |

---

## 11. MINOR findings requiring closure (optional polish)

**UI-MIN-1** — `docs/FRONTEND.md:176`
- Current: `Deferred (ROUTEMAP Tech Debt D-XXX): static lint rule flagging 'Dialog' usage with >3 form fields.`
- Fix: replace `D-XXX` with `D-090`
- Severity: MINOR (cross-reference hygiene)
- Blocker: NO

**UI-MIN-2** — `docs/FRONTEND.md:172`
- Current: `| Alerts row preview | _(future)_ | SlidePanel | Not yet implemented — use SlidePanel when added |`
- Observation: inconsistent column typography (italics only on Screen column). Not a correctness issue.
- Severity: MINOR (cosmetic)
- Blocker: NO

---

## 12. Final verdict

**PASS — READY TO PROCEED** to Gate Phase Gate.

Zero CRITICAL, zero MAJOR findings. Two MINOR documentation items (both in `docs/FRONTEND.md`) are trivial to close but are not required for gate pass — they're hygiene. All six ACs are materially satisfied with direct code evidence. Primitives unchanged (no regression). Accessibility contract upheld. Dark-mode tokens fully compliant.

**Recommended optional closure:** Apply UI-MIN-1 (D-XXX → D-090) in a single-line edit before closing FIX-216.
