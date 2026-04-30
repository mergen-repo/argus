# FIX-216 — Gate Scout Analysis (Code / Compliance / Security / Performance)

Scope: Modal Pattern Standardization — Dialog vs SlidePanel (Option C). FE-only doc + component-pattern story.

Scout role: READ-ONLY analysis. No fixes.

Files under audit
- `docs/FRONTEND.md` (new `## Modal Pattern` section, 71 lines)
- `web/src/pages/sims/index.tsx` (2 swaps + shadow-token fix)
- `web/src/pages/violations/index.tsx` (inline row-expand → SlidePanel + state refactor + a11y)
- `web/src/pages/settings/ip-pool-detail.tsx` (hand-rolled footer → `SlidePanelFooter`)
- `docs/ROUTEMAP.md` (D-090 Tech Debt entry)
- Primitives (audit-only): `web/src/components/ui/dialog.tsx`, `web/src/components/ui/slide-panel.tsx`

---

## Inventories

### Field Inventory

N/A — this story is a FE pattern migration. No DB schema, API response, or model field is introduced. Existing types (`PolicyViolation`, `OperatorCode`) are consumed unchanged.

### Endpoint Inventory

N/A — zero backend touch.

### Workflow Inventory

| AC | Step | Action | Expected Result | Verified |
|----|------|--------|-----------------|----------|
| AC-2.1 | 1 | User selects SIMs, clicks Suspend/Resume | Centered Dialog opens | sims/index.tsx L937-968 Dialog wired to `bulkDialog` state |
| AC-2.1 | 2 | User clicks Terminate | Dialog w/ destructive primary button | L958 `variant={bulkDialog?.action === 'terminated' ? 'destructive' : 'default'}` |
| AC-2.2 | 1 | User clicks Assign Policy | Right-side SlidePanel opens (width="md") | L1113-1116 SlidePanel wired to `policyDialogOpen` |
| AC-2.2 | 2 | User picks policy → confirm | `SlidePanelFooter` w/ outline cancel + primary | L1157-1191 |
| AC-2.3 | N/A | IP Pool Reserve IP | Already SlidePanel; footer now canonical `SlidePanelFooter` | ip-pool-detail.tsx L26 imports `SlidePanelFooter` |
| AC-3 | 1 | User clicks violation row | SlidePanel opens with detail | violations/index.tsx L437 `onClick={() => handleRowClick(v)}` → L510 SlidePanel |
| AC-3 | 2 | User presses Enter/Space on row | Same SlidePanel opens | L438-443 onKeyDown with `e.preventDefault()` |
| AC-3 | 3 | User presses ESC | Panel closes, focus returns to row | Built into SlidePanel primitive (FIX-215 hardening) |

### UI Component Inventory (Option C compliance map)

| Surface | File | Pattern | Conforms? |
|---------|------|---------|-----------|
| SIMs Bulk state-change | `web/src/pages/sims/index.tsx:937-968` | Dialog (confirm w/ ≤1 field) | **PASS** — DialogHeader+Title+Description+Footer; outline cancel + destructive-variant primary for Terminate |
| SIMs Assign Policy | `web/src/pages/sims/index.tsx:1113-1192` | SlidePanel (picker + preview) | **PASS** — width="md", title+description props, `SlidePanelFooter` |
| SIMs Import | `web/src/pages/sims/index.tsx:971-1110` | SlidePanel (rich form) | **PASS** — untouched, width="lg", title+description |
| Violations Row Detail | `web/src/pages/violations/index.tsx:510-…` | SlidePanel (row detail, read-heavy) | **PASS** — width="lg", title+description |
| IP Pool Reserve IP | `web/src/pages/settings/ip-pool-detail.tsx:388-454` | SlidePanel (search + multi-row) | **PASS** — width="lg", title+description, now uses `SlidePanelFooter` (GAP-FIXED) |

### AC Summary

| # | Criterion | Status | Gaps |
|---|-----------|--------|------|
| AC-1 | FRONTEND.md `## Modal Pattern` section | PASS | Section exists, 71 lines; Dialog/SlidePanel rule + decision tree + usage map + a11y notes |
| AC-2.1 | SIMs state-change → Dialog | PASS | Swap done; destructive variant for Terminate |
| AC-2.2 | SIMs Assign Policy → SlidePanel | PASS | Swap done; SlidePanelFooter used |
| AC-2.3 | IP Pool Reserve → SlidePanel | PASS | Already compliant; footer GAP fixed |
| AC-2.4 | APNs Connected SIMs KEEP | PASS | No-op (apns/detail.tsx still uses SlidePanel) |
| AC-2.5 | Alerts preview future → SlidePanel | PASS | Documented in FRONTEND.md "Current usage map" / future row |
| AC-3 | Violations row → SlidePanel (F-171) | PASS | State refactor done; a11y added |
| AC-4 | ESLint rule flag Dialog >3 fields | DEFER | D-090 Tech Debt entry in ROUTEMAP |
| AC-5 | Visual consistency: Dialog variants + SlidePanel header | PASS | Dialog: outline+default/destructive; SlidePanel: title+description props used everywhere (no `SlidePanelHeader` component — doc codifies this as the standard) |
| AC-6 | Dark mode parity | PASS | Primitives use semantic tokens; hex-scan=0; tailwind-gray-scan=0 on modified files |

---

## Option C Rule Audit (per-swap)

| Swap | Rule | Conformance |
|------|------|-------------|
| sims bulk state-change: SlidePanel → Dialog | "Quick confirmation (≤2 fields)" — bulk confirm with zero inline fields | CORRECT — use-case matches Dialog column in canonical rule |
| sims Assign Policy: Dialog → SlidePanel | "List picker / 3+ fields" — policy Select + preview block | CORRECT — fits SlidePanel column; width="md" matches "form + preview" on the width ladder |
| violations inline expand → SlidePanel | "Row-expand details, read-heavy" | CORRECT — width="lg" matches default for detail pane |
| ip-pool-detail reserve: SlidePanelFooter fix | Footer hand-roll removed; uses exported component | CORRECT — matches canonical "use exported `<SlidePanelFooter>`" |

**Verdict:** All four swaps are semantically correct per the new Option C rule and the newly codified FRONTEND.md section.

---

## Pattern Consistency Matrix

| Rule | Location | Compliant? | Evidence |
|------|----------|------------|----------|
| SlidePanel uses `title`+`description` props only (no hand-rolled header) | All SlidePanel sites | PASS | `grep SlidePanelHeader` returns zero matches across `web/src` |
| Dialog footer uses `Button` variants: `outline` cancel + `default`/`destructive` primary | sims/index.tsx:953-966 | PASS | L954 outline; L958 conditional destructive |
| SlidePanelFooter used instead of hand-rolled divs | sims, ip-pool-detail, sla/operator-breach | PASS | ip-pool-detail GAP-FIXED (hand-rolled div → `SlidePanelFooter`) |
| No raw `<button>` in swap blocks | sims/index.tsx, violations/index.tsx | PASS | Row `<div role="button">` in violations is the accepted pattern for table row click-target (same as sla/index.tsx MonthCard) — NOT a `<button>` atom violation |
| Button component for clickable actions | Cancel/Primary/Close across all swaps | PASS | All footer actions use `<Button>` |
| No hex, no tailwind grays, no `bg-white`/`text-black` in touched JSX | All 3 pages + 2 primitives | PASS | T6 scan: hex=0, rgba=0 (after FIXED), grays=0 |

---

## Cross-Page Audit Summary

**Dialog importer list (15 files):** unsaved-changes-prompt, saved-views-menu, related-violations-tab, rollout-tab, settings/sessions, settings/users, settings/api-keys, settings/security, operators/index, operators/detail, auth/two-factor, apns/detail, sims/index, plus two more. All are compact confirm or small-form flows → consistent with Option C.

**SlidePanel importer list (19+ files):** cdrs session-timeline-drawer, column-customizer, settings/ip-pools, settings/users, settings/ip-pool-detail, settings/api-keys, operators/index, operators/detail, apns/index, apns/detail, admin/maintenance, sessions/index, sla/month-detail, sla/operator-breach, webhooks/index, sims/index, violations/index, and more. All are rich forms / detail panes / pickers → consistent with Option C.

**FIX-215 files (SLA/CDR):** already compliant
- `sla/month-detail.tsx:149` SlidePanel (read-heavy month breakdown) — PASS
- `sla/operator-breach.tsx:63,163` SlidePanel + SlidePanelFooter (breach timeline) — PASS
- `operators/detail.tsx:43-50` imports both; uses Dialog for delete-operator confirm (compact) and SlidePanel for richer flows — PASS
- `cdrs/session-timeline-drawer.tsx:3` SlidePanel + SlidePanelFooter (session timeline detail) — PASS

**No Option C violations detected outside FIX-216 scope.**

---

## State Refactor Soundness (violations/index.tsx)

| Check | Result |
|-------|--------|
| `expandedIds` state removed | PASS — grep `expandedIds\|toggleExpanded` → zero matches |
| `selectedViolation: PolicyViolation \| null` state introduced | PASS — L204 |
| `handleRowClick` callback added | PASS — L265 `useCallback((v: PolicyViolation) => setSelectedViolation(v), [])` |
| Row `onClick` wired to new handler | PASS — L437 |
| Keyboard handler covers Enter + Space, preventDefault on Space | PASS — L438-443 (`e.preventDefault()` before `handleRowClick(v)` — correctly prevents default page scroll on Space) |
| Row a11y attributes | PASS — `role="button"` L433, `tabIndex={0}` L434, `aria-label` L435 |
| Panel open/close symmetry | PASS — `open={!!selectedViolation}` + `onOpenChange={(o) => !o && setSelectedViolation(null)}` |
| Unused imports removed | PASS — step-log confirms ChevronUp, ChevronDown, AlertTriangle removed |
| Panel content preserves original expand content | PASS — SIM link, Policy link, Operator chip, Severity, Type, Action, Time all present |

**No dangling references, no regressions.** State refactor is sound.

---

## Dark-Mode Parity

Primitives audited:
- `dialog.tsx`: uses `bg-bg-elevated`, `border-border`, `text-text-primary`, `bg-black/60` backdrop. No hex, no tailwind grays.
- `slide-panel.tsx`: uses `bg-bg-surface`, `bg-bg-elevated`, `text-text-primary/secondary/tertiary`, `border-border`. No hex, no tailwind grays.

New code in modified pages:
- `sims/index.tsx` — all new Dialog/SlidePanel body JSX uses semantic tokens (`text-text-primary`, `text-text-secondary`, `bg-bg-primary`, `border-border`)
- `violations/index.tsx` — all new SlidePanel body JSX uses `text-text-primary/secondary/tertiary`, `text-accent` (link), `text-danger`, `text-warning`
- `ip-pool-detail.tsx` — SlidePanelFooter uses primitive tokens

**Verdict:** Dark-mode parity holds. Zero theme violations introduced.

---

## Bug-Pattern Compliance (docs/brainstorming/bug-patterns.md)

| Pattern | Relevance | Status |
|---------|-----------|--------|
| PAT-006 (JSON tag drift) | N/A — no API/model types | N/A |
| PAT-012 (cross-surface count drift) | N/A — no aggregations | N/A |
| PAT-hex-in-jsx | Directly in scope | PASS — T6 scan confirms zero hex in new JSX; pre-existing rgba at sims/index.tsx L795 FIXED to `var(--shadow-card)` |
| PAT-raw-button-quality-scan-block | Directly in scope | PASS — no raw `<button>` introduced; `<div role="button">` on violations row is table-row click-target pattern (precedent: sla/index.tsx MonthCard) |
| PAT-missing-aria-modal | Directly in scope | PASS — SlidePanel primitive has `aria-modal`, focus-trap, ESC, restore-focus built in (FIX-215). Dialog scope is ≤2 focusables → focus-trap intentionally absent (documented in FRONTEND.md). |

---

## Security Scan

FE-only story; no API/auth/input-validation surface.

| Check | Result |
|-------|--------|
| SQL Injection | N/A (no backend) |
| XSS (`dangerouslySetInnerHTML`, `innerHTML=`) | Zero matches in modified files |
| Hardcoded Secrets | Zero |
| Insecure Randomness | Zero |
| CORS | N/A |
| Auth / Access Control | Unchanged (existing auth middleware on `/violations`, `/sims`, `/ip-pools` routes via existing chi router) |

**No security findings.**

---

## Performance Analysis

### 4.1 Query Analysis
N/A — zero DB touch.

### 4.2 Caching
N/A — no new data surfaces.

### 4.3 Frontend Performance

| Check | Result |
|-------|--------|
| Bundle size impact | Neutral — imports rearranged, no new dependencies |
| `React.memo` / `useMemo` / `useCallback` | `handleRowClick` uses `useCallback` (L265) — correct |
| Re-render risk | SlidePanel mounts only when `selectedViolation !== null`; body content gated by `{selectedViolation && (…)}` — correct conditional render; no unnecessary re-renders on table list |
| `useState<Set<string>>` → single-object state | PASS — memory footprint smaller (one ref vs Set) |
| Build times | T2 2.48s, T3 2.48s, T4 2.57s, all PASS per step-log |

**No performance findings.**

---

## Findings

### F-A1 | MEDIUM | gap
- Title: Violations SlidePanel lacks `SlidePanelFooter` with explicit Close button (AC-5 footer discipline partially met)
- Location: `web/src/pages/violations/index.tsx:510-…`
- Description: The other SlidePanel sites in the codebase (sla/operator-breach.tsx, sims Assign Policy, cdrs session-timeline-drawer, ip-pool-detail Reserve, webhooks/index) use `<SlidePanelFooter>` with at least a `Button variant="outline"` Close action. The violations SlidePanel relies only on the built-in header `X` close button + ESC + backdrop-click. While functionally complete, this is an inconsistency with the visual contract codified in FRONTEND.md Modal Pattern section ("Footer: use exported `<SlidePanelFooter>` with `Button variant="outline"` (Cancel) + primary action `Button`"). The plan (Task 4) did anticipate: *"Footer: `<SlidePanelFooter>` with `Button variant="outline"` Close + primary action (if any existed in inline expand, e.g., 'Go to SIM' / 'Suppress rule') — preserve."* Step-log does not confirm footer was added.
- Fixable: YES
- Suggested fix: Add `<SlidePanelFooter><Button variant="outline" onClick={() => setSelectedViolation(null)}>Close</Button></SlidePanelFooter>` at the end of the panel body. Optionally add "Go to SIM" primary action navigating to `/sims/{selectedViolation.sim_id}`.

### F-A2 | LOW | compliance
- Title: `handleRowClick` useCallback dependency array review
- Location: `web/src/pages/violations/index.tsx:265`
- Description: `useCallback((v: PolicyViolation) => setSelectedViolation(v), [])` — empty deps are correct here because `setSelectedViolation` is a stable setter reference from `useState`. Flagged only for completeness; no action required.
- Fixable: N/A (not a bug)
- Suggested fix: None.

### F-A3 | LOW | compliance
- Title: `aria-describedby` on violations row button pattern
- Location: `web/src/pages/violations/index.tsx:433-435`
- Description: The row uses `role="button"` + `tabIndex={0}` + `aria-label` — strong a11y baseline. For parity with FIX-248 candidates (F-U16 in FIX-215 gate), the row could optionally expose the severity/policy badges via `aria-describedby` to screen readers. Not a regression; precedent exists (sla/index.tsx MonthCard does not expose badges via aria-describedby either). Acceptable.
- Fixable: YES (future polish)
- Suggested fix: Defer to FIX-248 a11y/polish pass.

### F-A4 | LOW | pattern
- Title: Re-export `SlidePanelHeader` as alias (documentation signaling)
- Location: `web/src/components/ui/slide-panel.tsx`
- Description: FIX-216 spec (AC-5) literally says "all SlidePanel headers use `SlidePanelHeader` component". The plan correctly interpreted that no such component exists and codified "built-in header via `title`/`description` props IS the standard." To future-proof against an engineer adding a `SlidePanelHeader` out of reflex (because the spec mentioned it), consider either (a) exporting a named `SlidePanelHeader = () => null` deprecation shim that throws at dev-time, OR (b) adding an ESLint rule. The FRONTEND.md doc already covers the intent — this is optional hardening.
- Fixable: YES
- Suggested fix: DEFER — the explicit FRONTEND.md statement "there is no separate `SlidePanelHeader` component" is sufficient; revisit only if engineers create one in future PRs.

---

## Non-Fixable (Escalate)

None. All findings are minor polish or deferrals consistent with the AUTOPILOT scope.

---

## Plan-vs-Delivered Scope Check

| Plan Task | Delivered? | Notes |
|-----------|------------|-------|
| T1: FRONTEND.md Modal Pattern section | YES | 71 lines, includes canonical rule + decision tree + usage map + a11y note |
| T2: sims Bulk state-change SlidePanel → Dialog | YES | L937-968, destructive variant for Terminate |
| T3: sims Assign Policy Dialog → SlidePanel | YES | L1113-1192, width="md", SlidePanelFooter, preserves `selectAllSegment` title branch |
| T4: violations inline expand → SlidePanel | YES | State refactor + a11y. **Missing `SlidePanelFooter` Close button** (F-A1). |
| T5: IP Pool audit + AC-4 DEFER entry | YES | Hand-rolled footer → `SlidePanelFooter` (GAP fixed beyond original audit scope); D-090 entry added to ROUTEMAP |
| T6: dark-mode + primitive audit + rgba fix | YES | rgba literal at sims/index.tsx L795 fixed to `var(--shadow-card)`; primitives clean |

**Plan delivery: 6/6 tasks EXECUTED. Only F-A1 (violations SlidePanelFooter) is a partial deliverable gap within Task 4 scope.**

---

## Performance Summary

### Queries Analyzed
N/A — zero DB queries introduced.

### Caching Verdicts
N/A — no new data surfaces.

---

## Summary

**CRITICAL: 0 · HIGH: 0 · MEDIUM: 1 · LOW: 3**

- F-A1 (MEDIUM) — violations SlidePanel missing `SlidePanelFooter` + Close button
- F-A2 (LOW) — useCallback deps (informational, no action)
- F-A3 (LOW) — a11y polish (defer to FIX-248)
- F-A4 (LOW) — SlidePanelHeader export shim (optional hardening)

FIX-216 meets all 6 ACs (AC-4 legitimately deferred with D-090 Tech Debt entry). Option C rule is correctly applied to all four swaps, FRONTEND.md codifies the standard, no hex/gray/raw-button/hardcoded-shadow regressions. Only meaningful gap is the missing SlidePanelFooter on the violations SlidePanel — a small follow-up fix for visual-contract parity.

**Gate recommendation: PASS-WITH-MINOR-FIX** (fix F-A1 before gate PASS).
