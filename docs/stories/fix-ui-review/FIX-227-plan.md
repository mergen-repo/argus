# Implementation Plan: FIX-227 — APN Connected SIMs SlidePanel (CDR + Usage Graph + Quick Stats)

## Goal
Enrich the existing "Connected SIMs" SlidePanel on the APN Detail page (SCR-030) with a 7-day usage sparkline, CDR summary (total sessions / total bytes / avg duration), policy-applied / last-session header context, and three quick actions (View Full Details, Suspend, View CDRs) — all fetched lazily only when the panel opens.

## Scope Summary

FE-only story. **The SlidePanel already exists** (`web/src/pages/apns/detail.tsx` L523-572, added during FIX-216 / APN polish). Current state: shows 6 identifier fields in a grid + a single "Open Full Detail" button. FIX-227 enriches it — does NOT build from zero.

Concrete touch-set (2 files, 1 NEW + 1 modified — hook reuse only, no new hook module):
- **NEW** `web/src/components/sims/quick-view-panel.tsx` — the SIM quick-view content component (header card, usage sparkline, CDR summary, quick actions). Wrapped by parent's existing `<SlidePanel>`.
- **MODIFY** `web/src/pages/apns/detail.tsx` — replace the inline quick-view body (L530-571) with `<QuickViewPanelBody sim={selectedSim} onClose={...} />`. The outer `<SlidePanel>` wrapper (L523-529) stays — its title/description/width props are already correct.

**No backend work.** Every endpoint already exists:
- `GET /api/v1/sims/{id}/usage?period=7d` → `SIMUsageData` with `series[]` (bucketed bytes_in/bytes_out) + `total_bytes_in` + `total_bytes_out` + `total_cost`. Hook: `useSIMUsage(simId, '7d')` (`web/src/hooks/use-sims.ts:105-119`).
- `GET /api/v1/cdrs/stats?sim_id={id}&from={iso}&to={iso}` → `CDRStats { total_count, total_bytes_in, total_bytes_out, total_cost, unique_sims, unique_sessions }`. Hook: `useCDRStats({sim_id, from, to})` (`web/src/hooks/use-cdrs.ts:98-108`).
- `POST /api/v1/sims/{id}/suspend` → state change. Hook: `useSIMStateAction()` (`web/src/hooks/use-sims.ts:202-227`) — already used on SIM detail page with an undo snackbar flow.

**No new DTO shape required.** Frontend derives `avg_duration_sec = 0` when `total_count === 0` and computes from an additional field we expose from CDRStats: note CDRStats has no `duration_sum` today. See Decision D-FIX227-1 below — we surface avg duration via the already-present `SIMUsageData.top_sessions` (sum their `duration_sec`) OR via computing from bytes+rate heuristic. **Verified resolution: use `useSIMSessions(simId, 'all')` already exported (`use-sims.ts:85-103`) → sum `duration_sec` across last 7d, divide by session count.** One additional hook call, already implemented, no backend change.

## Architecture Context

### Data Flow

```
User clicks a SIM row in APN Detail → SIMs tab
  → setSelectedSim(sim)                            [already wired L469]
  → <SlidePanel open={!!selectedSim}>              [already wired L523]
    → <QuickViewPanelBody sim={selectedSim} />     [NEW — this story]
      ├── useSIMUsage(sim.id, '7d')                [lazy: enabled=!!sim.id]
      ├── useCDRStats({sim_id: sim.id, from: 7d ago, to: now})
      ├── useSIMSessions(sim.id)                   [for avg-duration calc]
      └── useSIMStateAction()                      [Suspend button mutation]
```

React Query lazy-fetch: hooks are gated by `enabled: !!simId` — zero network traffic until the panel opens. On close (`onOpenChange(false)` fires, parent sets `selectedSim=null`), hooks un-mount and inflight promises are cancelled by React Query's abort signal.

### Components Involved

| Component | Layer | File | Role |
|---|---|---|---|
| `SlidePanel` | Primitive | `web/src/components/ui/slide-panel.tsx` | Outer panel — reused as-is |
| `QuickViewPanelBody` | Organism (NEW) | `web/src/components/sims/quick-view-panel.tsx` | Body content: header, sparkline, stats, actions |
| `Sparkline` | Atom | `web/src/components/ui/sparkline.tsx` | 7d in/out bytes line |
| `Card`, `CardContent`, `CardHeader`, `CardTitle` | Molecule | `web/src/components/ui/card.tsx` | Inner grouping |
| `Badge` | Atom | `web/src/components/ui/badge.tsx` | State badge (reuses `stateVariant` from `@/lib/sim-utils`) |
| `Button` | Atom | `web/src/components/ui/button.tsx` | Three quick action buttons |
| `Spinner` | Atom | `web/src/components/ui/spinner.tsx` | Loading state |
| `useSIMUsage` | Hook | `web/src/hooks/use-sims.ts:105` | 7d usage series |
| `useSIMSessions` | Hook | `web/src/hooks/use-sims.ts:85` | Sessions for avg-duration |
| `useCDRStats` | Hook | `web/src/hooks/use-cdrs.ts:98` | Total sessions + total bytes (authoritative) |
| `useSIMStateAction` | Hook | `web/src/hooks/use-sims.ts:202` | `action: 'suspend'` mutation |

### Data Contracts (embedded — do not refer out)

**`SIMUsageData`** (`web/src/types/sim.ts:153-161`):
```ts
{
  sim_id: string
  period: string
  total_bytes_in: number
  total_bytes_out: number
  total_cost: number
  series: Array<{ bucket: string; bytes_in: number; bytes_out: number }>  // SIMUsageSeriesBucket
  top_sessions: Array<{ ... duration_sec ... }>                           // SIMUsageTopSession
}
```

**`CDRStats`** (`web/src/hooks/use-cdrs.ts:22-29`):
```ts
{
  total_count: number           // == total CDR rows (use as total_sessions proxy; see D-FIX227-1)
  total_bytes_in: number
  total_bytes_out: number
  total_cost: number
  unique_sims: number
  unique_sessions: number       // CANONICAL total sessions count for this SIM in the window
}
```

**`SIMSession`** (for avg-duration — summed client-side):
```ts
// From useSIMSessions(simId) — paginated; the first page has 50 rows — plenty for 7d of sessions for one SIM.
{ duration_sec: number, ... }   // sum → divide by unique_sessions for avg
```

**Filter timestamps:**
```ts
const to   = new Date().toISOString()
const from = new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString()
```

### Screen Mockup — enriched SlidePanel body

```
┌────────────────────────────────────────────────────────┐
│ SIM 55443322                                        [×]│   ← existing SlidePanel header (title/description props)
│ 894411250000055443322                                  │
├────────────────────────────────────────────────────────┤
│                                                        │
│ ┌─ Identity ─────────────────────────────────────────┐ │
│ │ ICCID       8944... 33  (mono)                     │ │   ← header card: 6 identity fields kept,
│ │ IMSI        2860... 21  (mono)                     │ │     now compact single-column list.
│ │ MSISDN      905... 55                              │ │     Badge + policy added.
│ │ State       [ACTIVE •]                             │ │
│ │ Policy      meter-low-v1 (applied)                 │ │   ← policy_name from SIM model
│ │ Last Sess.  3 hours ago                            │ │   ← from useSIMSessions first row
│ └────────────────────────────────────────────────────┘ │
│                                                        │
│ ┌─ Usage (last 7 days) ──────────────────────────────┐ │
│ │     ▁▂▃▅▆▇█▇▅▃▂▁  ← Sparkline bytes_in (accent)   │ │
│ │     ▁▁▂▂▃▃▄▄▃▂▁▁  ← Sparkline bytes_out (purple)  │ │
│ │                                                    │ │
│ │ Total In   2.4 GB   Total Out   450 MB             │ │
│ └────────────────────────────────────────────────────┘ │
│                                                        │
│ ┌─ CDR Summary (7d) ─────────────────────────────────┐ │
│ │ Sessions     127                                   │ │
│ │ Total Bytes  2.9 GB                                │ │
│ │ Avg Duration 4m 32s                                │ │
│ │ Top Destinations  (coming soon)              [dim] │ │   ← future — static "Coming soon" line, no API
│ └────────────────────────────────────────────────────┘ │
│                                                        │
│ [ View Full Details ] [ Suspend ] [ View CDRs ]        │   ← three quick-action Buttons
│                                                        │
└────────────────────────────────────────────────────────┘
```

Loading state: Each card shows a small `<Spinner>` centered when its hook `isLoading`. Usage card shows "No usage data in last 7 days" if `series.length === 0`. CDR card shows "No CDRs in last 7 days" if `total_count === 0`.

Error state: Toast via `useUIStore().addToast({kind:'error', message:'...'})` on fetch error. Rendered body falls back to the identity card only.

### Design Token Map (UI story — MANDATORY)

#### Color Tokens (from FRONTEND.md)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-white`, `text-gray-100` |
| Secondary text | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-400` |
| Tertiary / muted text | `text-text-tertiary` | `text-[#4A4A65]`, `text-gray-500` |
| Accent (data in) | `var(--color-accent)` / `text-accent` / `stroke="var(--color-accent)"` | `#00D4FF`, `text-blue-500` |
| Purple (data out) | `var(--color-purple)` / `stroke="var(--color-purple)"` | `#A855F7`, `text-purple-500` |
| Success (active SIM badge) | already inside `<Badge variant={stateVariant(...)}>` | hardcoded |
| Warning (suspended badge) | same | hardcoded |
| Danger (destructive confirm, e.g. Suspend button) | `<Button variant="destructive">` | `bg-[#FF4466]`, `bg-red-500` |
| Card border | `border-border` | `border-[#1E1E30]`, `border-gray-700` |
| Card subtle border | `border-border-subtle` | `border-[#16162A]`, `border-gray-800` |
| Card background | `bg-bg-surface` | `bg-[#0C0C14]`, `bg-slate-900` |
| Elevated (inner card) | `bg-bg-elevated` | `bg-[#12121C]`, `bg-black` |
| Hover | `bg-bg-hover` | — |

#### Typography Tokens
| Usage | Class | NEVER Use |
|-------|-------|-----------|
| Label rows (identity grid) | `text-[10px] uppercase tracking-wider text-text-tertiary` | `text-xs` alone; arbitrary `text-[9px]` |
| Mono data values | `font-mono text-xs` or `font-mono text-[11px]` | `font-mono text-[12.5px]` |
| Section headers inside panel | `text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium` | `text-sm font-bold` (too loud) |
| Big metric number | `font-mono text-xl font-bold text-text-primary` | arbitrary sizes |
| Button label | inherited from `<Button>` variants | — |

#### Spacing & Elevation Tokens
| Usage | Class | NEVER Use |
|-------|-------|-----------|
| Panel section gap | `space-y-4` | `gap-[17px]` |
| Card padding | via `<Card>` / `<CardContent className="pt-4">` | `p-[15px]` |
| Inner grid gap | `gap-3` | `gap-[13px]` |
| Card radius | `rounded-[var(--radius-md)]` via `<Card>`; inner info cells: `rounded-[var(--radius-sm)]` | `rounded-lg` (inconsistent) |

#### Existing Components to REUSE (do not recreate)
| Component | Path | Use For |
|---|---|---|
| `<SlidePanel>` | `web/src/components/ui/slide-panel.tsx` | Panel wrapper — already in place, KEEP |
| `<Card>`, `<CardContent>`, `<CardHeader>`, `<CardTitle>` | `web/src/components/ui/card.tsx` | All grouping |
| `<Badge>` | `web/src/components/ui/badge.tsx` | State badge (with `variant={stateVariant(sim.state)}`) |
| `<Button>` | `web/src/components/ui/button.tsx` | All three quick actions |
| `<Sparkline>` | `web/src/components/ui/sparkline.tsx` | 7d usage lines (`data: number[]`, `color: string`, `width={240}` `height={32}`) |
| `<Spinner>` | `web/src/components/ui/spinner.tsx` | Loading placeholders |
| `stateVariant` helper | `web/src/lib/sim-utils.ts` | SIM state → Badge variant |
| `formatBytes` | `web/src/lib/format.ts` | All byte counts |
| `RAT_DISPLAY` | `web/src/lib/constants.ts` | RAT type labels |

**RULE: Zero raw `<button>`, `<div class="bg-white">`, or hex colors. All icons come from `lucide-react`.**

## Prerequisites
- [x] FIX-216 DONE (SlidePanel primitive + canonical Modal Pattern in FRONTEND.md)
- [x] FIX-214 DONE (CDR Explorer page at `/cdrs`; accepts `?sim_id=` query param)
- [x] `useSIMUsage`, `useCDRStats`, `useSIMSessions`, `useSIMStateAction` all already implemented

## Waves & Tasks

Total: **3 waves, 3 tasks**. Small story (Effort=S per ROUTEMAP) → 2-3 tasks expected. DB/API pre-flight is an audit (zero code change), so it's folded into Task 1 as a gate-checklist rather than a separate task.

---

### Wave 1 — Foundation + API audit (serial)

#### Task 1 — Build `QuickViewPanelBody` component (NEW FILE)
- **Files:** Create `web/src/components/sims/quick-view-panel.tsx` (≤220 LOC target)
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** 
  - `web/src/pages/sims/detail.tsx` L326-425 (`UsageTab`) — for `useSIMUsage` + recharts usage pattern. BUT our panel uses the lightweight `<Sparkline>` atom, not a full recharts AreaChart — far simpler.
  - `web/src/components/cdrs/session-timeline-drawer.tsx` — SlidePanel body structure (header row + scrollable sections + footer CTAs) already established in codebase.
  - `web/src/pages/apns/detail.tsx` L530-571 — current inline body for the 6-field identity grid to port in.
- **Context refs:** "Architecture Context > Components Involved", "Data Contracts", "Screen Mockup", "Design Token Map"
- **What:**
  - Export `function QuickViewPanelBody({ sim }: { sim: SIM }): JSX.Element` — receives the already-selected SIM; parent manages open/close.
  - **Hooks (order, all gated by `enabled: !!sim.id` via each hook's existing `!!simId` guard):**
    1. `const { data: usage, isLoading: usageLoading } = useSIMUsage(sim.id, '7d')`
    2. `const { data: cdrStats, isLoading: statsLoading } = useCDRStats({ sim_id: sim.id, from, to })` with `from`/`to` memoized via `useMemo` — the 7d window.
    3. `const { data: sessionsPages, isLoading: sessionsLoading } = useSIMSessions(sim.id)` — first page of 50 gives enough for 7d avg-duration approximation.
    4. `const stateMut = useSIMStateAction()` — for Suspend action.
  - **Derived values** (via `useMemo`):
    - `bytesInSeries: number[] = usage?.series?.map(b => b.bytes_in) ?? []`
    - `bytesOutSeries: number[] = usage?.series?.map(b => b.bytes_out) ?? []`
    - `avgDurationSec: number = ` — sum `duration_sec` across `sessionsPages.pages[0].data[].filter(s => timestamp within 7d window)` / `cdrStats?.unique_sessions || 1`. If zero sessions → show `—`.
    - `lastSessionRelative: string` — if `sessionsPages.pages[0].data[0]?.started_at` present → compute relative (e.g. "3 hours ago") via `formatDistanceToNow` (check if already imported elsewhere; if not, use a 10-line inline helper since `date-fns` may not be in deps — alternative: `new Date(ts).toLocaleString()`). Verify dep presence before committing: `grep -rE "date-fns|formatDistanceToNow" web/src` to decide. If absent → use `toLocaleString()`.
  - **Render structure** (match the Screen Mockup):
    1. **Identity Card** — `<Card>` containing 6 `InfoRow`s (ICCID, IMSI, MSISDN, State, Policy, Last Session). Use existing `<InfoRow>` from `web/src/components/ui/info-row.tsx` (already imported elsewhere in apns/detail.tsx — verify path). State row: `<InfoRow label="State" value={<Badge variant={stateVariant(sim.state)}>{sim.state.toUpperCase()}</Badge>} />`. Policy row: `<InfoRow label="Policy" value={sim.policy_name ?? 'None'} mono={!!sim.policy_name} />`. Last session: computed string above.
    2. **Usage Card** — `<Card><CardHeader><CardTitle>Usage (last 7 days)</CardTitle></CardHeader><CardContent>`. If `usageLoading` → centered `<Spinner/>`. If `bytesInSeries.length < 2` → "No usage data in last 7 days" in `text-xs text-text-tertiary`. Else:
       - Two `<Sparkline>` rows stacked: one for `bytes_in` (`color="var(--color-accent)"`), one for `bytes_out` (`color="var(--color-purple)"`), each with `width={240}` `height={32}`.
       - Below: 2-col grid of Total In / Total Out using `formatBytes(usage.total_bytes_in)` / `formatBytes(usage.total_bytes_out)`.
    3. **CDR Summary Card** — `<Card><CardHeader><CardTitle>CDR Summary (7d)</CardTitle></CardHeader><CardContent>`. If `statsLoading` → Spinner. Else: 3 stats rows — Sessions (`cdrStats?.unique_sessions ?? 0`), Total Bytes (`formatBytes((cdrStats?.total_bytes_in ?? 0) + (cdrStats?.total_bytes_out ?? 0))`), Avg Duration (`formatDuration(avgDurationSec)` — inline helper or reuse). Plus a dim row: `<div className="text-[10px] uppercase tracking-wider text-text-tertiary mt-3">Top Destinations <span className="text-[9px] italic normal-case">(coming soon)</span></div>` — static, no API.
    4. **Quick Actions Footer** — NOT using `<SlidePanelFooter>` because the panel content scrolls; footer is at end of body:
       - `<Button variant="default" size="sm" onClick={() => navigate('/sims/' + sim.id)}><ExternalLink .../> View Full Details</Button>`
       - `<Button variant="destructive" size="sm" disabled={sim.state !== 'active' || stateMut.isPending} onClick={handleSuspend}>Suspend</Button>` — only enabled for active SIMs. **handleSuspend:** optimistic confirm inline via `if (!window.confirm(...))` is BANNED per PAT-raw-confirm — use the existing `<ConfirmDialog>` pattern if it exists, OR simply fire the mutation and rely on the standard `useSIMStateAction` toast + undo (it already provides an undo action id). **Decision (DEV-FIX227-4 below):** call `stateMut.mutate({simId: sim.id, action: 'suspend'})` directly, show the undo toast via existing UI store pattern (see `sims/detail.tsx` state-change flow for copy-paste semantics). On success: invalidate queries (hook handles it), close panel via callback.
       - `<Button variant="outline" size="sm" onClick={() => navigate('/cdrs?sim_id=' + sim.id)}>View CDRs</Button>`
  - **Props contract:**
    ```ts
    interface QuickViewPanelBodyProps {
      sim: SIM
      onClose: () => void   // called after Suspend succeeds; parent clears selectedSim
    }
    ```
  - **AbortController / stale-query guard:** React Query's built-in `enabled: !!sim.id` + query key (includes `sim.id`) means closing the panel (parent sets `selectedSim=null`) un-mounts this component, which cancels in-flight queries. No manual AbortController needed — DO NOT add one.
  - **No raw HTML:** every button is `<Button>`, every icon is `lucide-react`, every card is `<Card>`, every badge is `<Badge>`.
  - **No hex colors:** only `var(--color-*)` in `style=` / `stroke=` attrs and semantic Tailwind classes.
- **Verify:**
  - `grep -nE "#[0-9a-fA-F]{3,6}" web/src/components/sims/quick-view-panel.tsx` → **zero matches**
  - `grep -nE "<button[^>]*>" web/src/components/sims/quick-view-panel.tsx` → zero raw `<button>`
  - `grep -nE "bg-white|bg-gray-|text-gray-" web/src/components/sims/quick-view-panel.tsx` → zero matches
  - `grep -nE "window\.confirm|alert\(" web/src/components/sims/quick-view-panel.tsx` → zero matches (per PAT-raw-confirm)
  - `make web-build` passes with zero TS errors
- **AC coverage:** AC-1 (identity header), AC-2 (usage sparkline + CDR summary + top-destinations-future), AC-3 (three quick actions), AC-4 (lazy via `enabled: !!sim.id`)

---

### Wave 2 — Wire-up (serial, depends on Task 1)

#### Task 2 — Wire `QuickViewPanelBody` into APN Detail SIMs tab
- **Files:** Modify `web/src/pages/apns/detail.tsx` (delta ~45 LOC removed, ~3 LOC added)
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** `web/src/pages/apns/detail.tsx` L523-572 — the existing inline SlidePanel body; we replace its children with the new component, keep the SlidePanel wrapper and its `title`/`description`/`width` props intact.
- **Context refs:** "Architecture Context > Data Flow", "Design Token Map"
- **What:**
  - Add import: `import { QuickViewPanelBody } from '@/components/sims/quick-view-panel'`
  - Replace lines L530-571 (the entire `{selectedSim && (...)}` block inside `<SlidePanel>`) with:
    ```tsx
    {selectedSim && (
      <QuickViewPanelBody sim={selectedSim} onClose={() => setSelectedSim(null)} />
    )}
    ```
  - Keep the `<SlidePanel open={!!selectedSim} onOpenChange={...} title=... description=... width="lg">` wrapper as-is (L523-529).
  - The row-click handler (L469 `onClick={() => setSelectedSim(sim)}`) stays as-is.
  - **a11y carry-over:** the row already uses `className="cursor-pointer"` + native `<TableRow onClick>`. For keyboard access (AC-1 implied), add `tabIndex={0}` and `onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') setSelectedSim(sim) }}` on the `<TableRow>` — mirrors PAT-015 FIX-216 keyboard contract for clickable rows. Also add `role="button"` for SR semantics.
  - Remove now-unused imports **only if** they are not referenced elsewhere in the file after the edit (likely: `InfoRow` imported at L70 stays — it's used in ConfigTab; `Badge` stays — many uses; `stateVariant` stays — used in SIMs table row L489; `RAT_DISPLAY` stays — L497). Verify grep after edit.
- **Verify:**
  - `grep -n "QuickViewPanelBody" web/src/pages/apns/detail.tsx` → exactly 2 matches (import + usage)
  - `grep -n "SlidePanel" web/src/pages/apns/detail.tsx` → still wraps the selectedSim panel (1 match)
  - Keyboard: Tab to SIM row → Enter → panel opens; ESC closes; focus restored (primitive handles)
  - Click SIM row → panel opens with three cards + three action buttons
  - Click "View Full Details" → navigates to `/sims/<id>`
  - Click "View CDRs" → navigates to `/cdrs?sim_id=<id>` (FIX-214 route)
  - Click "Suspend" when SIM is active → state change fires, toast appears with undo, panel closes (or stays — decided by implementer, prefer stays-open to show updated state once `sim` prop refreshes, but the simpler path is close-on-success — see Risk R2 below)
  - `make web-build` passes
- **AC coverage:** AC-1, AC-2, AC-3, AC-4 (complete coverage when combined with Task 1)

---

### Wave 3 — Docs & Tech Debt (parallel, low risk)

#### Task 3 — Record DEV decisions + update SCREENS.md + log D-item for top-destinations
- **Files:** Modify `docs/brainstorming/decisions.md` (append 4 DEV-NNN rows), modify `docs/SCREENS.md` (SCR-030 annotation), modify `docs/ROUTEMAP.md` (1 new Tech Debt D-129)
- **Depends on:** Tasks 1, 2
- **Complexity:** low
- **Pattern ref:** Existing DEV-NNN rows in decisions.md (e.g. DEV-318 format), existing D-128 row in ROUTEMAP.
- **Context refs:** "Scope Summary", "Data Contracts"
- **What:**
  - Append 4 DEV-NNN rows to `docs/brainstorming/decisions.md` (IDs **DEV-319 through DEV-322** — next free per grep of `decisions.md` max=DEV-318 on 2026-04-23):
    - **DEV-319** — 2026-04-25 — FIX-227 — "avg_duration_sec computed client-side from `useSIMSessions(simId)` first page + `CDRStats.unique_sessions` denominator, NOT a new backend endpoint. Rationale: `CDRStats` DTO today carries no `duration_sum_sec` field and extending it requires a backend change (GROUP BY plus SUM in `CDRStatsInWindow` in `internal/store/cdr.go`). Accuracy trade-off: the first sessions page (50 rows) covers 7d of activity for any real SIM; for anomalously active SIMs the computed avg is biased toward the most-recent 50 sessions. Acceptable for a summary panel. Tracked as D-129 for backend enrichment when someone wants exact 7d-average." — ACCEPTED
    - **DEV-320** — 2026-04-25 — FIX-227 — "`Top Destinations` (AC-2 line item, spec says 'future') rendered as dim 'coming soon' placeholder inside CDR Summary card — NOT a fetch. Rationale: no destination-aggregation endpoint exists; CDRs carry `rat_type` + `record_type` but no destination IP/host. A dedicated endpoint (`GET /api/v1/sims/{id}/top-destinations?period=7d`) plus store method is a separate story. Placeholder preserves layout contract without fake data." — ACCEPTED
    - **DEV-321** — 2026-04-25 — FIX-227 — "Suspend action uses existing `useSIMStateAction` mutation + toast/undo pattern from SIM detail page, NOT a dedicated inline confirm Dialog. Rationale: FIX-216 Modal Pattern rule (Option C) says Dialog is for compact confirms — but duplicating a Dialog-inside-SlidePanel nests two modals, which the SlidePanel focus trap + Dialog focus trap will fight. Instead we rely on the existing undo-snackbar (30s window) for reversibility. Suspend button `disabled` unless `sim.state === 'active'` to reduce accidents. Matches `useSIMStateAction` docstring intent. Panel closes on successful mutation." — ACCEPTED
    - **DEV-322** — 2026-04-25 — FIX-227 — "Lazy fetch implemented via hook `enabled` guards (already present in `useSIMUsage`, `useCDRStats`, `useSIMSessions`), no manual AbortController. React Query cancels in-flight promises on unmount; closing the panel (parent `setSelectedSim(null)`) unmounts `QuickViewPanelBody`. AC-4 satisfied by the existing React Query semantics — no explicit abort plumbing needed." — ACCEPTED
  - Update `docs/SCREENS.md` SCR-030 note: annotate "FIX-227: Connected SIMs tab row click opens SlidePanel with identity card + 7d usage sparkline + CDR summary (7d) + 3 quick actions (View Full Details / Suspend / View CDRs). Lazy-fetched on open."
  - Append to `docs/ROUTEMAP.md` Tech Debt table:
    - **D-129** | FIX-227 DEV-319 | `avg_duration_sec` computed client-side in `QuickViewPanelBody` from first page of `useSIMSessions` (50 rows) + `CDRStats.unique_sessions` denominator. True 7d average requires `store.CDRStatsInWindow` to return a `duration_sum_sec` field (SUM(duration_sec) in SQL GROUP BY) so FE can compute `sum / unique_sessions` server-side. Acceptable for summary panel; visible inaccuracy only on SIMs with >50 sessions in 7 days. | FIX-24x (CDR stats enrichment) | OPEN |
  - Also append a matching ROUTEMAP change-log row (date 2026-04-25, entry REVIEW or DOC).
- **Verify:**
  - `grep -cE "^\| DEV-31[9]|^\| DEV-32[0-2]" docs/brainstorming/decisions.md` → 4
  - `grep -n "FIX-227" docs/SCREENS.md` → ≥1 match
  - `grep -n "D-129" docs/ROUTEMAP.md` → ≥1 match
  - No other file changes.
- **AC coverage:** documentation only (supports AC-2, AC-4, closes DEV trail)

---

## Acceptance Criteria Mapping

| AC | Task(s) | Verification |
|---|---|---|
| AC-1 (row click → SlidePanel with ICCID/IMSI/MSISDN header + state badge + policy + last session) | Task 1 (Identity Card) + Task 2 (row-click wiring + keyboard handler) | Manual click + tab/Enter key + grep `QuickViewPanelBody` |
| AC-2 (usage sparkline last 7d, CDR summary total sessions / bytes / avg duration, top destinations future) | Task 1 (Usage Card + CDR Summary Card + Top Destinations placeholder) | Manual inspection; `grep "coming soon"` in quick-view-panel.tsx |
| AC-3 (three quick actions: View Full Details → `/sims/{id}`, Suspend, View CDRs → `/cdrs?sim_id={id}`) | Task 1 (three `<Button>`s) | Manual click each → correct nav / mutation |
| AC-4 (lazy fetch on open only) | Task 1 (hook `enabled` gates; no explicit fetch before open) | DevTools Network tab: no /sims/{id}/usage, /cdrs/stats, /sims/{id}/sessions calls until panel opens |

---

## Story-Specific Compliance Rules

- **UI:** Design tokens ONLY. Zero hex, zero `bg-white`/`text-gray-*`. Reuse `<SlidePanel>`, `<Card>`, `<Button>`, `<Badge>`, `<Sparkline>`, `<Spinner>` primitives. Icons from `lucide-react`. Never raw `<button>` / `<div>`-as-button.
- **a11y:** Row click target MUST have `role="button"`, `tabIndex={0}`, Enter/Space keyboard handler (inherited FIX-216 / PAT-015 contract). SlidePanel focus-trap + ESC + restore-focus are primitive-level (FIX-215 hardening).
- **API / DB / Tenant:** UNCHANGED. All endpoints pre-exist. Tenant scoping is enforced by existing hooks (React Query passes JWT via `api` interceptor).
- **Lazy fetch:** EACH of the 3 data hooks must honor the `enabled: !!sim.id` gate. Closing the panel (parent `setSelectedSim(null)`) must unmount the body — DO NOT cache across opens with different SIM IDs (React Query keyed by sim.id handles this naturally).
- **Option C (FIX-216):** Suspend action does NOT nest a Dialog inside the SlidePanel — relies on the existing toast/undo flow (DEV-321).

## Bug Pattern Warnings

- **PAT-006 / PAT-011 / PAT-017 (wiring propagation):** When adding the new component, verify every hook is invoked inside the body (not conditionally behind a prop that's sometimes missing). Grep after implementation: `grep -n "useSIMUsage\|useCDRStats\|useSIMSessions\|useSIMStateAction" web/src/components/sims/quick-view-panel.tsx` → must list all 4.
- **PAT-012 (cross-surface count drift):** "Total Sessions" in the CDR Summary card MUST come from `cdrStats.unique_sessions` (canonical) — NOT from counting sessionsPages rows (different source). The avg-duration numerator is the only use of sessionsPages.
- **PAT-015 (declared-but-unmounted component):** After Task 2, run `rg -n '<QuickViewPanelBody' web/src` → must return ≥1 match (the APN detail wiring). `rg -n 'function QuickViewPanelBody' web/src` → 1 declaration. Counts must match.
- **PAT-raw-button-quality-scan-block:** Zero raw `<button>` tags — every clickable uses `<Button>`.
- **PAT-hex-in-jsx:** Zero hex in new file; Sparkline colors passed as `var(--color-accent)` / `var(--color-purple)` (strings — these go into SVG `stroke=` attribute which accepts CSS vars).

## Tech Debt (from ROUTEMAP)

No OPEN items in ROUTEMAP currently target FIX-227 directly. FIX-216 Tech Debt candidates (D-041/D-044 mentioned in dispatch) are NOT listed in ROUTEMAP at current scope (max is D-128) and are out of scope — this story does NOT touch `BulkJobResponse` or bulk policy-assign `reason`. Dispatched concern noted and dismissed (no relevant debt).

**NEW Tech Debt created by this story:** D-129 (see Task 3) — `avg_duration_sec` backend enrichment.

## Mock Retirement
No mocks — all data endpoints are real and pre-existing.

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| **R1: 7d sparkline aggregate performance** (spec mentioned reusing `internal/analytics/aggregates/` from FIX-208). | **Resolution:** FE calls `useSIMUsage(simId, '7d')` which hits `GET /api/v1/sims/{id}/usage?period=7d` — this endpoint **already routes through `aggregates.SIMUsageInWindow`** (per FIX-208 aggregates facade, verified in `internal/analytics/aggregates/doc.go` per PAT-012 rule). Zero raw SQL introduced by this story. |
| **R2: Suspend quick action — panel UX after success** — do we close or stay? | **Resolution (DEV-321 confirmed):** close on success. Staying open would show a stale `sim.state='active'` Badge until the parent `useAPNSims` query revalidates, which is confusing. Close + toast-with-undo is the consistent pattern. Implement in `handleSuspend`: `await stateMut.mutateAsync(...)` → `onClose()`. |
| **R3: Lazy-fetch race (panel close before fetch resolves)** | **Resolution (DEV-322):** React Query's `queryClient.cancelQueries` fires automatically on component unmount. No manual AbortController. Verified by: closing panel mid-load must NOT cause a React "state update on unmounted component" warning — the hooks' built-in staleness guard handles this. |
| **R4: FIX-216 Tech Debt D-041/D-044 scope creep** | **Resolution:** this story does NOT materially touch `BulkJobResponse` or `BulkPolicyAssignPayload.reason`. No upgrade needed. No in-scope debt. Dispatched concern explicitly dismissed. |
| **R5: "Top destinations (future)"** | **Resolution (DEV-320):** rendered as inline dim "coming soon" placeholder. Not built, not fetched. No hook wired. Tracked informally via the story AC-2 text — no new Tech Debt entry needed (FIX-24x follow-up story will propose the endpoint when prioritized). |
| **R6: `useSIMSessions` first page may not span 7d for a very active SIM** | Acknowledged via DEV-319 + D-129. Panel accuracy: "Avg Duration" may reflect most-recent 50 sessions not the full 7d window. Visible only for SIMs with >50 sessions in 7d. Acceptable for a summary panel. |
| **R7: `InfoRow` component support for ReactNode `value`** | Inspect `web/src/components/ui/info-row.tsx` during Task 1 before using `<InfoRow value={<Badge .../>} />`. If it only accepts strings, fall back to a hand-laid row using the same 10px-uppercase-label + mono-value pattern as the existing L532-548 identity grid. Plan does not mandate `InfoRow` use — component structure in mockup is the contract. |

## Pre-Validation (self-check)

- [x] **Minimum substance** (Effort=S → 30 lines, 2 tasks): plan is ~300 lines, 3 tasks → **PASS**
- [x] **Required sections:** Goal, Architecture Context, Tasks, Acceptance Criteria Mapping all present
- [x] **Embedded specs:** hook signatures + data contracts + screen mockup + token map all inline, no "see X.md" references
- [x] **Task complexity cross-check:** Small story → 0 high tasks; Task 1 marked `medium` (3 hooks + composable UI) — appropriate for S effort
- [x] **Context refs validation:** every task's Context refs point to sections that exist in this plan
- [x] **Architecture compliance:** FE-only; no layer violations; hooks in `web/src/hooks/`; component in `web/src/components/sims/`; page wiring in `web/src/pages/apns/detail.tsx` — standard atomic-design layering
- [x] **No DB changes**, no API changes — verified against codebase
- [x] **UI compliance:** screen mockup embedded; Design Token Map populated; Component Reuse table populated; frontend-design skill usage noted (Task 1 complexity medium with explicit tokens/reuse rules)
- [x] **Task decomposition:** 3 tasks, each ≤2 files (Task 3 is doc-only multi-file which is fine for docs; code tasks are 1 file each)
- [x] **Pattern refs** present on the only new-file task (Task 1) — 3 reference files listed
- [x] **Bug Pattern Warnings** listed with concrete greps
- [x] **Self-containment:** every referenced file path, hook signature, and type shape copied inline
- [x] **Zero unresolved questions** — R1-R7 all have resolutions

## Quality Gate

- [x] **Architecture compliance** — component in atoms/molecules/organisms hierarchy (placed in `components/sims/` which is a domain subfolder of organisms — matches `components/cdrs/session-timeline-drawer.tsx` precedent). Cross-layer imports: hooks ← components OK; components → primitives OK. No page → component → page cycles.
- [x] **No raw SQL introduced** — every data hook hits an existing endpoint; backend unchanged.
- [x] **No arbitrary Tailwind colors** — Design Token Map restricts to semantic classes only; verification grep specified in Task 1 Verify step.
- [x] **Reuses existing hooks** — `useSIMUsage`, `useCDRStats`, `useSIMSessions`, `useSIMStateAction` all pre-existing; no new hook module created.
- [x] **FIX-216 SlidePanel primitive reused** — outer `<SlidePanel>` wrapper at apns/detail.tsx:523 kept intact; no new panel primitive; canonical `title` + `description` + `width` header contract (FIX-216 Modal Pattern rule) preserved.
- [x] **DEV-NNN IDs reserved** — DEV-319, DEV-320, DEV-321, DEV-322 (next free after DEV-318 on 2026-04-23). Verified via `grep -cE "^\| DEV-" decisions.md` → 350 rows, max=DEV-318.
- [x] **Accessibility** — row click inherits FIX-215 focus-trap + ESC + restore-focus from SlidePanel primitive; Task 2 adds `role="button"` + `tabIndex={0}` + Enter/Space handler to the `<TableRow>` per PAT-015 contract.
- [x] **Scope containment** — 3 files total (1 NEW component, 1 MODIFY page, 3 DOC modifications). No drift into bulk payload code, no touch to `useAPNSims`, no backend code, no migrations.
- [x] **Tech Debt accounting** — D-129 (new) logged for CDRStats duration_sum_sec enrichment; no legacy debt relevant to this story.
- [x] **No unresolved questions** — all 7 risks resolved with explicit resolutions.

**Quality Gate Result: PASS**
