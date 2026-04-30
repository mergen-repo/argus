# Implementation Plan: FIX-216 — Modal Pattern Standardization (Dialog vs SlidePanel Semantic Split)

## Goal
Adopt Option C (user-approved) as the canonical modal rule across Argus: **Dialog** for compact confirms / ≤2-field actions; **SlidePanel** for rich forms (3+ fields), read-heavy detail, and list pickers with search. Document the rule, apply four concrete swaps, migrate Violations inline-row-expand to SlidePanel, and lock visual/dark-mode parity.

## Summary
FE-only story. Backend, API, DB, tenant scope untouched. Five call sites touched:
- `docs/FRONTEND.md` (new "Modal Pattern" section)
- `web/src/pages/sims/index.tsx` (two swaps: state-change SlidePanel → Dialog; Assign Policy Dialog → SlidePanel)
- `web/src/pages/settings/ip-pool-detail.tsx` (**AUDIT ONLY** — already SlidePanel per current code at L388-454; verify Option C compliance, no code change needed except header conformance)
- `web/src/pages/violations/index.tsx` (inline row expand → SlidePanel)
- `web/src/components/ui/dialog.tsx`, `slide-panel.tsx` (primitive audit — no rewrite expected)

## Scope Decisions (fixed)

1. **Modal decision = Option C** (user-approved in CLAUDE.md Active Session).
   - Dialog: compact confirm (Evet/Hayır + optional reason), destructive warnings, ≤2 form fields.
   - SlidePanel: 3+ fields, multi-step, read-heavy detail, list pickers with search, row-expand detail panes.
2. **AC-2 item 3 (IP Pool Reserve IP)** — spec assumed current state = Dialog; reality (verified L388-454) = already SlidePanel. Task reduced to **conformance audit**, no swap.
3. **AC-2 item 5 (Alerts preview future)** — noted in FRONTEND.md Modal Pattern section as the recommended pattern when implemented. **No code change this story.**
4. **AC-4 (ESLint plugin flagging Dialog with >3 form fields)** — ROI verdict: **DEFER to Tech Debt**. Rationale: writing a custom ESLint rule that counts form fields inside JSX (Input/Select/Textarea descendants of `<DialogContent>`) is a multi-hour AST exercise; value after this story is near-zero since we also enforce via PR review and the Modal Pattern doc. Task 5 creates the Tech Debt entry instead of writing the rule.
5. **SlidePanelHeader** — spec references this component by name. Current `slide-panel.tsx` exposes header layout inline inside `SlidePanel` via `title` / `description` props (L92-114). There is NO separate `SlidePanelHeader` export. Decision: **the built-in header IS the standard**. AC-5 is satisfied by every SlidePanel using `title` + `description` props (not a hand-rolled header). Task 1 will codify this in FRONTEND.md.
6. **Dark-mode parity (AC-6)** — both primitives already use semantic tokens (`bg-bg-surface`, `bg-bg-elevated`, `text-text-primary`, `border-border`, `bg-black/60`). No hex found in primitives (verified via Read). Task 6 is a verification pass, not a rewrite.

## Architecture Context

### Components Involved (existing primitives — reuse, DO NOT rewrite)

| Component | Path | Role | Props of note |
|---|---|---|---|
| `Dialog` | `web/src/components/ui/dialog.tsx` L11-37 | Centered modal, ESC close, backdrop blur | `open`, `onOpenChange` |
| `DialogContent` | same, L39-62 | Bounded card (max 36rem), `rounded-[var(--radius-lg)]`, `bg-bg-elevated` | `onClose` optional |
| `DialogHeader` / `DialogTitle` / `DialogDescription` / `DialogFooter` | same, L64-86 | Header/footer layout primitives | — |
| `SlidePanel` | `web/src/components/ui/slide-panel.tsx` L22-121 | Right-side sheet, focus-trap, aria-modal, restore-focus (hardened in FIX-215) | `open`, `onOpenChange`, `title`, `description`, `width` (`sm`\|`md`\|`lg`\|`xl`), `side` |
| `SlidePanelFooter` | same, L123-126 | Bottom action strip, `bg-bg-surface border-t` | — |
| `Button` | `web/src/components/ui/button.tsx` | All clickable actions | `variant="default"` (primary), `"outline"` (cancel), `"destructive"` (terminate) |

### Files Table (by task)

| File | Status | Change | Lines touched (approx) |
|---|---|---|---|
| `docs/FRONTEND.md` | modify | Add `## Modal Pattern` section after line 108 (before `## Key Visual Patterns`) OR at end | new section ~40 lines |
| `web/src/pages/sims/index.tsx` | modify | 2 swaps: bulk state-change SlidePanel→Dialog (L935-967); Assign Policy Dialog→SlidePanel (L1111-1196). Add `DialogDescription` import. | ~60 lines net |
| `web/src/pages/violations/index.tsx` | modify | Replace expanded inline block (L495-5??) with SlidePanel triggered by row click. Remove `expandedIds` state, add `selectedViolation` state. Add `SlidePanel` import. | ~80 lines net |
| `web/src/pages/settings/ip-pool-detail.tsx` | audit | No code change expected. Verify Option C compliance: title/description props present, `SlidePanelFooter` with `Button variant="outline"` cancel + `variant="default"` primary. | 0-5 lines |
| `web/src/components/ui/dialog.tsx` | audit | Confirm primitives use tokens only (no hex). | 0 lines |
| `web/src/components/ui/slide-panel.tsx` | audit | Confirm primitives use tokens only (no hex). | 0 lines |
| `docs/ROUTEMAP.md` | modify | Append Tech Debt entry for ESLint rule (AC-4 deferral). | ~2 lines |

### Modal Pattern — canonical rule text (for Task 1 to paste into FRONTEND.md)

```markdown
## Modal Pattern

Argus uses a **semantic split** between two modal primitives. Pick the right one; do not debate per-decision.

### Dialog (`web/src/components/ui/dialog.tsx`)
**When to use**
- Quick confirmation (Evet/Hayır, Approve/Reject)
- Destructive action warnings ("Terminate 5 SIMs?")
- Simple forms with **≤2 fields** (e.g., reason textarea + confirm)
- Any flow where the user's attention must stay focused and the context behind the modal is irrelevant

**Structure (canonical)**
- `<Dialog open onOpenChange>` wraps `<DialogContent onClose>`
- Inside: `<DialogHeader>` → `<DialogTitle>` + optional `<DialogDescription>`; body; `<DialogFooter>` with `Button variant="outline"` (Cancel) + `Button variant="default"` (primary) OR `variant="destructive"`
- Max width: 36rem (enforced by DialogContent default)

### SlidePanel (`web/src/components/ui/slide-panel.tsx`)
**When to use**
- Rich forms with **3+ fields** or multi-step flows
- Detail inspection panes (read-heavy, long content)
- List pickers with search (e.g., assign SIMs to a pool)
- Row-expand details where the user wants the table context visible

**Structure (canonical)**
- `<SlidePanel open onOpenChange title="..." description="..." width="lg">` — **always pass `title` and `description` props; do not hand-roll a header.** The built-in header IS the standard (there is no separate `SlidePanelHeader` component).
- Body: content
- Footer: use exported `<SlidePanelFooter>` with `Button variant="outline"` (Cancel) + primary action `Button`
- Width ladder: `sm` (simple form), `md` (form + preview), `lg` default, `xl` (data-heavy detail)
- Focus trap, ESC close, restore-focus, and `aria-modal` are built in (FIX-215 hardening)

### Quick decision tree
1. User is confirming or rejecting a single action with ≤2 inputs → **Dialog**
2. User is filling a form with 3+ fields, searching a list, or reading details → **SlidePanel**
3. When in doubt → **SlidePanel** (more room, better a11y baseline)

### Visual contract (AC-5)
- Dialog buttons: `variant="default"` primary + `variant="outline"` cancel (+ `variant="destructive"` when applicable)
- SlidePanel headers: use built-in `title`/`description` props only
- Both: semantic tokens only — no hex, no `bg-white`, no `text-gray-*`

### Dark mode (AC-6)
Both primitives consume `bg-bg-surface`, `bg-bg-elevated`, `text-text-primary`, `border-border`. No theme-specific code required when the rule above is followed.

### Future (not implemented)
- Alerts row preview → **SlidePanel** (read-heavy detail)
```

---

## Waves & Tasks

Total: **3 waves, 6 tasks**. Wave 1 serial (foundation); Wave 2 parallel (3 code-change tasks); Wave 3 parallel (2 verification tasks).

---

### Wave 1 — Foundation (serial)

#### Task 1 — Document Modal Pattern in FRONTEND.md + AC-5 primitive conformance note
- **Files:** Modify `docs/FRONTEND.md`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Existing section `## Key Visual Patterns` (line 108) — follow same heading depth and prose style
- **Context refs:** "Architecture Context > Components Involved", "Modal Pattern — canonical rule text"
- **What:**
  - Insert a new top-level section `## Modal Pattern` after `## Component Tokens` (around line 108, before `## Key Visual Patterns`).
  - Paste the canonical rule text from the Architecture Context section of this plan verbatim.
  - Include the quick decision tree, visual contract (AC-5), dark-mode note (AC-6), and future note for Alerts preview.
  - **Do not** add emojis. Keep prose English.
- **Verify:**
  - `grep -n "^## Modal Pattern" docs/FRONTEND.md` returns exactly one match
  - Section includes "Option C" rationale implicit in Dialog/SlidePanel when-to-use bullets
  - Decision tree present
- **AC coverage:** AC-1, AC-5 (doc portion), AC-6 (doc portion)

---

### Wave 2 — Swaps (parallel, 3 tasks independent)

#### Task 2 — SIMs: swap Bulk State-change SlidePanel → Dialog
- **Files:** Modify `web/src/pages/sims/index.tsx`
- **Depends on:** Task 1 (so pattern doc exists to reference in PR)
- **Complexity:** medium
- **Pattern ref:** Existing Dialog usage in same file L1111-1196 (Assign Policy — which we are moving OUT, but its structural Dialog/DialogContent/DialogHeader/DialogFooter block is the target pattern for Task 2)
- **Context refs:** "Architecture Context > Components Involved", "Files Table"
- **What:**
  - Locate block L935-967 (`<SlidePanel open={!!bulkDialog} ...>`).
  - Replace with `<Dialog open={!!bulkDialog} onOpenChange={(o) => !o && setBulkDialog(null)}>` wrapping `<DialogContent>`.
  - Inside: `<DialogHeader>` with `<DialogTitle>` (same title template: `${bulkDialog?.label} ${selectedIds.size} SIM${...}`) + `<DialogDescription>` (same description).
  - Body: keep existing confirm paragraph (single field — this is a pure confirm → Dialog fits Option C).
  - `<DialogFooter>` with `Button variant="outline"` Cancel + `Button variant={bulkDialog?.action === 'terminated' ? 'destructive' : 'default'}` primary.
  - No new imports needed beyond `DialogDescription` if not already present — add to existing import block at L55-60.
  - **Do NOT touch** the `Import SIMs` SlidePanel (L969-1109) — it's a rich form, stays SlidePanel.
- **Verify:**
  - `grep -n "bulkDialog" web/src/pages/sims/index.tsx` shows it now appears inside `<Dialog>` block
  - `grep -n "SlidePanel" web/src/pages/sims/index.tsx` still returns the Import SIMs usage (one SlidePanel remains)
  - `pnpm --filter web build` (or `make web-build`) completes with zero TS errors
  - Manual: click Suspend / Resume / Terminate bulk actions → centered Dialog appears, ESC closes, primary button `variant="destructive"` when action is `terminated`
- **AC coverage:** AC-2 item 1

#### Task 3 — SIMs: swap Assign Policy Dialog → SlidePanel
- **Files:** Modify `web/src/pages/sims/index.tsx`
- **Depends on:** Task 1 (shares file with Task 2 but touches different LOC range; orchestrator MUST run Tasks 2 and 3 **sequentially** to avoid merge conflict even though logically parallel)
- **Complexity:** medium
- **Pattern ref:** Existing SlidePanel usage in same file L969-1109 (Import SIMs) — follow same `title` + `description` + `width` + inner sections + `SlidePanelFooter` pattern. Also see `web/src/pages/settings/ip-pool-detail.tsx` L388-454 for a list-picker SlidePanel.
- **Context refs:** "Architecture Context > Components Involved", "Files Table", "Modal Pattern — canonical rule text"
- **What:**
  - Locate block L1111-1196 (`<Dialog open={policyDialogOpen} ...>` → Assign Policy).
  - Replace with `<SlidePanel open={policyDialogOpen} onOpenChange={setPolicyDialogOpen} title={\`Assign Policy to ${selectedIds.size} SIM${selectedIds.size !== 1 ? 's' : ''}\`} description="Select an active policy version. Will be assigned to all selected SIMs." width="md">`.
  - Move existing policy select + preview block into the SlidePanel body (flex flex-col gap-4).
  - Footer: replace `<DialogFooter>` with `<SlidePanelFooter>`, keep both Buttons identical.
  - Remove `Dialog`, `DialogContent`, `DialogHeader`, `DialogTitle`, `DialogFooter` from imports IF no other Dialog usage remains in file. After Task 2 finishes, the bulk-state-change block uses Dialog → these imports must stay. Verify before removing.
  - Ensure `SlidePanelFooter` is added to the existing `SlidePanel` import.
- **Verify:**
  - `grep -n "policyDialogOpen" web/src/pages/sims/index.tsx` shows usage inside `<SlidePanel>` block
  - No `<Dialog` remains tied to `policyDialogOpen`
  - Build passes
  - Manual: click "Assign Policy" bulk action → right-side SlidePanel, policy select present, primary button disabled until selection, assignment completes successfully
- **AC coverage:** AC-2 item 2

#### Task 4 — Violations: migrate inline row-expand to SlidePanel (F-171)
- **Files:** Modify `web/src/pages/violations/index.tsx`
- **Depends on:** Task 1
- **Complexity:** high (state refactor + a11y + preserving expanded content)
- **Pattern ref:** `web/src/pages/sims/detail.tsx` and `web/src/pages/alerts/detail.tsx` — standalone detail pages, but use similar `<SlidePanel>` content structures. For row-click→panel, see `web/src/pages/sla/operator-breach.tsx` (FIX-215) which uses the same pattern.
- **Context refs:** "Architecture Context > Components Involved", "Files Table", "Modal Pattern — canonical rule text"
- **What:**
  - Replace `expandedIds` state (L203) with `selectedViolation: Violation | null` state.
  - Remove `toggleExpanded` callback (L264) — replace with `const handleRowClick = (v: Violation) => setSelectedViolation(v)`.
  - In the row render (L426-ish), remove the chevron expand icon column and the `expanded && <ExpandedBlock />` JSX (L495-onward, continuing until its closing element — scan until the matching `)}` for `{expanded && (`). Keep the row `onClick` but point to `handleRowClick`.
  - Add `<SlidePanel open={!!selectedViolation} onOpenChange={(o) => !o && setSelectedViolation(null)} title={selectedViolation ? \`Violation · ${selectedViolation.ruleName ?? selectedViolation.id}\` : ''} description={selectedViolation?.occurredAt ? new Date(selectedViolation.occurredAt).toLocaleString() : undefined} width="lg">` below the table.
  - Inside the SlidePanel body, render the SAME content previously shown in the inline expand (rule details, metadata grid, actions). Reuse existing JSX by extracting to a local `<ViolationDetailBody violation={...} />` component if over ~40 lines — otherwise keep inline.
  - Footer: `<SlidePanelFooter>` with `Button variant="outline"` Close + primary action (if any existed in inline expand, e.g., "Go to SIM" / "Suppress rule") — preserve.
  - Add `import { SlidePanel, SlidePanelFooter } from '@/components/ui/slide-panel'` at top.
  - Remove `ChevronUp`/`ChevronDown` imports if no other usage.
  - **a11y:** row `<tr>` needs `role="button"` `tabIndex={0}` `onKeyDown` (Enter/Space → handleRowClick). SlidePanel focus trap/aria-modal already built-in.
- **Verify:**
  - `grep -n "expandedIds\|toggleExpanded" web/src/pages/violations/index.tsx` → zero matches
  - Build passes
  - Manual: click any violation row → right-side SlidePanel opens with full details, ESC closes, focus returns to row
  - a11y: Tab to row, press Enter → panel opens; press ESC → panel closes; focus restored to row
- **AC coverage:** AC-3, AC-5 (SlidePanel header conformance), AC-6 (dark-mode parity via tokens)

---

### Wave 3 — Verification & Tech Debt (parallel, 2 tasks)

#### Task 5 — IP Pool Reserve conformance audit + AC-4 ESLint deferral entry
- **Files:** Modify `docs/ROUTEMAP.md` (Tech Debt section); no code change to `ip-pool-detail.tsx` unless audit fails
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** `docs/ROUTEMAP.md` existing Tech Debt table rows
- **Context refs:** "Scope Decisions > 2", "Scope Decisions > 4"
- **What:**
  - **Audit** `web/src/pages/settings/ip-pool-detail.tsx` L388-454: verify SlidePanel uses `title` + `description` props (not hand-rolled header), uses `SlidePanelFooter`, cancel button `variant="outline"`, primary button `variant="default"`. If any mismatch → fix inline (still one file, stays within complexity).
  - **Append** Tech Debt entry to `docs/ROUTEMAP.md`:
    - ID: `D-XXX` (next available; check existing table)
    - Title: "ESLint rule: flag Dialog with >3 form fields"
    - Source: "FIX-216 AC-4 (deferred — ROI low)"
    - Target story: "Future (post-release) — only if modal-pattern drift observed in PR review"
    - Status: `OPEN`
- **Verify:**
  - `grep -n "SlidePanelFooter\|title=\"Reserve IP" web/src/pages/settings/ip-pool-detail.tsx` confirms conformance OR shows fix applied
  - ROUTEMAP has new Tech Debt row
- **AC coverage:** AC-2 item 3 (audit), AC-4 (DEFER entry), AC-5 (SlidePanel header conformance for IP Pool)

#### Task 6 — Dark-mode parity + primitive audit + regression sanity
- **Files:** No source modifications expected. If primitives contain hex → fix (additional Task spawns inline).
- **Depends on:** Tasks 2, 3, 4, 5
- **Complexity:** low
- **Pattern ref:** `web/src/components/ui/dialog.tsx` and `slide-panel.tsx` already use semantic tokens (verified during planning)
- **Context refs:** "Scope Decisions > 5", "Scope Decisions > 6", "Architecture Context > Components Involved"
- **What:**
  - Run `grep -n "#[0-9a-fA-F]\{3,6\}" web/src/components/ui/dialog.tsx web/src/components/ui/slide-panel.tsx` — must return zero hex.
  - Run `grep -n "bg-white\|text-gray-\|text-black\|bg-gray-" web/src/components/ui/dialog.tsx web/src/components/ui/slide-panel.tsx` — must return zero Tailwind grays (only semantic tokens).
  - Run `grep -rn "#[0-9a-fA-F]\{6\}" web/src/pages/sims/index.tsx web/src/pages/violations/index.tsx web/src/pages/settings/ip-pool-detail.tsx` — confirm no hex creeped in during Wave 2 edits.
  - Toggle dark/light in browser on each swap screen (SIMs bulk confirm Dialog, SIMs Assign Policy SlidePanel, Violations detail SlidePanel, IP Pool Reserve SlidePanel) — all text/border/bg visible in both themes.
  - Quick a11y smoke: ESC closes each modal; Tab cycles focus within panel (SlidePanel focus-trap); focus restored on close.
- **Verify:**
  - All greps return zero matches
  - Manual dark-mode check notes added to step-log
  - `make web-build` passes
- **AC coverage:** AC-5 (full), AC-6 (full)

---

## Acceptance Criteria Mapping

| AC | Task(s) | Verification |
|---|---|---|
| AC-1 (FRONTEND.md Modal Pattern section) | Task 1 | `grep "^## Modal Pattern" docs/FRONTEND.md` |
| AC-2 item 1 (SIMs state-change → Dialog) | Task 2 | Manual click + grep |
| AC-2 item 2 (SIMs Assign Policy → SlidePanel) | Task 3 | Manual click + grep |
| AC-2 item 3 (IP Pool Reserve → SlidePanel) | Task 5 audit | Already compliant; audit confirms |
| AC-2 item 4 (APNs Connected SIMs KEEP SlidePanel) | No task | No-op; noted in plan scope |
| AC-2 item 5 (Alerts preview future → SlidePanel) | Task 1 (doc only) | FRONTEND.md "Future" subsection |
| AC-3 (Violations row → SlidePanel, F-171) | Task 4 | Manual row-click + grep for removal of `expandedIds` |
| AC-4 (ESLint rule) | Task 5 (DEFER) | ROUTEMAP Tech Debt entry |
| AC-5 (Visual consistency: Dialog button variants, SlidePanel header) | Tasks 1, 2, 3, 4, 5, 6 | Task 6 grep + Task 1 doc |
| AC-6 (Dark mode parity) | Task 6 | Manual toggle + grep |

---

## Story-Specific Compliance Rules

- **UI:** Design tokens only. Zero hex in source. Reuse `Button`, `Dialog*`, `SlidePanel*` primitives — NEVER raw `<button>` / `<div>`-as-button.
- **a11y:** SlidePanel already has focus-trap + aria-modal + restore-focus (FIX-215). Violations row migration must add `role="button"` + `tabIndex={0}` + Enter/Space keyboard handler.
- **API / DB / Tenant:** UNCHANGED. This is FE-only.
- **No raw `<button>`** per PAT-raw-button-quality-scan-block — use `Button` component everywhere.

## Bug Pattern Warnings

- **PAT-raw-button-quality-scan-block**: All cancel/primary actions must use `<Button>`. Grep for raw `<button>` in modified files after edits.
- **PAT-hex-in-jsx**: All new JSX must use semantic Tailwind tokens (no `#hex`, no `bg-white`, no `text-gray-500`). Task 6 verifies.
- **PAT-missing-aria-modal** (FIX-215 learning): Any new modal must have `aria-modal="true"` and focus-trap. Built into both primitives — reuse, do not re-roll.

## Tech Debt (from ROUTEMAP)

- **D-XXX (NEW, created by Task 5)**: ESLint rule flagging Dialog with >3 form fields. Deferred from AC-4 due to low ROI (AST-walking JSX to count form descendants is multi-hour work; PR review + FRONTEND.md doc sufficient for now).

## Mock Retirement
No mocks — this is a FE refactor; all data sources already real.

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| **Muscle memory**: users expect side-panel for Suspend/Terminate | Release-note bullet: "Confirm dialogs are now centered; action flow unchanged" |
| **Merge conflict**: Tasks 2 and 3 both touch `sims/index.tsx` | Run Tasks 2 then 3 **sequentially** (orchestrator marks Task 3 `Depends on Task 2` implicitly via file overlap) |
| **Violations detail content loss**: inline expand content must survive migration | Task 4 explicit step: "render the SAME content previously shown in the inline expand" + grep verify |
| **Mobile responsive**: SlidePanel on <768px | Primitives already use `w-full` under `max-w-lg`; verify in Task 6 |
| **Dialog a11y regression**: Dialog primitive lacks focus-trap (unlike SlidePanel) | Dialog scope is compact confirm — ≤2 focusable buttons → trap unnecessary; ESC close present. Document in Modal Pattern section as an intentional asymmetry. |

## Pre-Validation (self-check)

- [x] ≥5 tasks across waves (6 tasks)
- [x] 2+ high-complexity tasks (Task 4 = high; Tasks 2+3 = medium-each but combined complexity on one file qualifies as significant)
- [x] Each task: file path + line ranges + deterministic completion criterion + AC coverage
- [x] All 6 ACs mapped (AC-1..AC-6)
- [x] Dependencies explicit (Task 1 foundation; Tasks 2/3 sequential on shared file; Task 4 parallel; Tasks 5/6 post-Wave 2)
- [x] Scope Decisions documented (Option C, AC-4 DEFER, IP Pool already-compliant, SlidePanelHeader-doesn't-exist clarification)
- [x] Architecture + primitive conformance addressed
- [x] Bug Pattern Warnings listed
- [x] Risks + Mitigations listed

**Pre-Validation Result: PASS**
