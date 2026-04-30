# FIX-222 — Scout UI Findings

Gate: FIX-222 Operator/APN Detail Polish
Role: UI (visual structure, tab counts, token enforcement, a11y, deep-link behavior)

## Scope Covered
- Operator Detail tabs + KPI row + InfoTooltip placements
- APN Detail tabs + KPI row + InfoTooltip placements
- EsimProfilesTab visual states
- Alias redirect behavior, tab URL persistence
- Token/enforcement sweep (hex, arbitrary px, raw button, default tailwind colors)

<SCOUT-UI-FINDINGS>

F-U1 | PASS | Operator Detail tab count = 10
- Verified list: overview, protocols, health, traffic, sessions, sims, esim, alerts, audit, agreements.
- Matches plan (9+1 transitional Agreements pending FIX-238). `const OPERATOR_TABS` at detail.tsx:1160-1163.

F-U2 | PASS | APN Detail tab count = 8
- Verified list: overview, config, ip-pools, sims, traffic, policies, audit, alerts.
- Matches AC-6. `const APN_TABS` at apns/detail.tsx:925-927.

F-U3 | PASS | InfoTooltip call-site count = 11
- EsimProfilesTab: EID, ICCID (2)
- operators/detail.tsx SIMs table: ICCID, IMSI, MSISDN, APN (4)
- operators/detail.tsx header: MCC, MNC (2)
- apns/detail.tsx SIMs table: ICCID, IMSI, MSISDN (3)
- Total = 11 — exact match to prompt AC-8.

F-U4 | PASS | InfoTooltip ⓘ icon + 500ms delay + ESC + aria
- `Info` icon from lucide (h-3 w-3).
- `setTimeout(setOpen, 500)` on mouseenter/focus; clearTimeout on leave/blur.
- Document keydown listener bound when open, calls setOpen(false) on Escape.
- `aria-label="What is ${term}?"`, `aria-expanded={open}`, `role="tooltip"` on content panel.
- Tap toggles via onClick (mobile-friendly).
- Dev warn when unknown term is passed (console.warn in NODE_ENV !== 'production').

F-U5 | PASS | KPI grid responsive
- Both detail pages use `grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4`. Verified at operators/detail.tsx:1340 and apns/detail.tsx:1075.

F-U6 | PASS | KPI trend/delta rendering
- KPICard renders ArrowUpRight/ArrowDownRight/Minus based on delta sign.
- deltaColor: success / danger / text-tertiary via token classes (no hex).
- FIX-222 does NOT pass `delta` values to any KPICard — all 8 cards omit delta → `deltaText=null`, trend arrow not rendered. Expected behavior (KPIs are point-in-time, not deltas).

F-U7 | PASS | Token enforcement (design system)
- Hex colors in FIX-222 files: 0.
- Raw `<button>` elements in FIX-222 files: 0.
- CSS variables used throughout (var(--color-accent), var(--color-success), var(--color-warning), var(--radius-sm), var(--radius-md)).
- `text-[10px]` / `max-w-[160px]` arbitrary tokens present but consistent with existing Argus typography convention (kpi-card.tsx was extracted verbatim from dashboard, dashboard uses same values). Not a FRONTEND.md violation.

F-U8 | PASS | Dark mode parity
- All new components use token classes (text-text-primary/secondary/tertiary, bg-bg-surface/elevated/hover, border-border). These are defined for both themes in the design system. InfoTooltip panel uses `bg-bg-elevated border-border` — dark-mode safe.

F-U9 | PASS | Deep-link `?tab=health` works
- useTabUrlSync validates against OPERATOR_TABS; `health` is in list → activeTab=health. Tested logic.

F-U10 | PASS | `?tab=notifications` redirects to `?tab=alerts`
- aliases={notifications: 'alerts'} on both pages. Effect fires when rawTab='notifications': needsAliasRedirect=true → setSearchParams(tab=alerts, {replace:true}). URL replaced (no history entry). Second render: rawTab='alerts', needsAliasRedirect=false, needsInvalidFallback=false → effect no-op. No loop.

F-U11 | PASS | `?tab=circuit` redirects to `?tab=health` on Operator page
- aliases={circuit:'health', notifications:'alerts'} on operator page. Same mechanism as F-U10.

F-U12 | PASS | EsimProfilesTab empty state
- When `profiles.length===0` and not loading: renders EmptyState with Cpu icon, title "No eSIM profiles on this operator", description copy. Spans 5-column table row. Verified at EsimProfilesTab.tsx:119-129.

F-U13 | PASS | EsimProfilesTab error state (after F-A1 fix)
- When `isError=true`: renders AlertCircle + "Failed to load eSIM profiles." + Retry button. Matches pattern from HealthTimelineTab.

F-U14 | PASS | Action buttons top-right consistent (AC-12)
- Operator detail header (detail.tsx:1327-1336): Edit, Delete buttons flex gap-2 flex-shrink-0 on right.
- APN detail header (apns/detail.tsx:1062-1071): Edit, Delete buttons same pattern.
- Parity confirmed.

F-U15 | PASS | Tab order = read-heavy first (AC-10)
- Operator: overview→protocols→health→traffic→sessions→sims→esim→alerts→audit→agreements (config/overview first; data-dump audit toward end).
- APN: overview→config→ip-pools→sims→traffic→policies→audit→alerts (overview first).

</SCOUT-UI-FINDINGS>
