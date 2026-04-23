# Implementation Plan: FIX-222 — Operator/APN Detail Polish

## Goal
Deliver KPI rows, consolidated tabs, technical-term tooltips, and a new eSIM Profiles tab on the Operator and APN detail screens so ops engineers can scan entity state without context overload.

## Scope Decisions (Discovery-Driven Revisions)

The original dispatch recommended deferring the eSIM Profiles tab, deferring SIMs server-side filter, and keeping Agreements pending FIX-238. Discovery overturned two of those:

| Dispatch Recommendation | Actual State Found | Revised Scope |
|-------------------------|---------------------|---------------|
| Defer eSIM Profiles tab (blocked by FIX-235) | `internal/store/esim.go` has `operator_id` column + `ListEnriched`; `internal/api/esim/handler.go::List` accepts `?operator_id=`; route wired at `internal/gateway/router.go:481` | **IN SCOPE** — FIX-235 is an SGP.22→SGP.02 backend refactor (distinct). F-184 only needs reverse-link listing which already works. Log as DEV-298. |
| Defer SIMs server-side filter (AC-3/AC-7) to FIX-233 | `OperatorSimsTab` (line 1035) already calls `useSIMList({ operator_id })`; APN detail (line 149) uses `apn_id` filter | **SATISFIED BY EXISTING** — no work needed. Flag in AC matrix. Log as DEV-299. |
| Keep Agreements tab pending FIX-238 | Roaming module still active (`useOperatorRoamingAgreements`, `/roaming-agreements/*` route live) | **KEEP** — tab stays until FIX-238. Final tab count 11 → **9** (spec said 8, but Agreements kept). Log as DEV-300. |
| AC-5 "Top Operator using this APN" | No backend endpoint; APN traffic hook only returns timeseries | **SCOPED DOWN** — compute top operator client-side from `useSIMList({apn_id})` group-by-operator on the first page. If impossible within one page, display `—` with tooltip "Not available". Defer a real `/apns/{id}/top-operators` endpoint to a follow-up. Log as DEV-301. |
| Tooltip copy source "GLOSSARY.md build-time generated" (Risk 3) | No build pipeline for glossary | **Inline constants file** `web/src/lib/glossary-tooltips.ts` with `// see docs/GLOSSARY.md` cross-ref comment. |
| Protocols tab "last probe result" (AC-3) | `TestConnection` endpoints exist, no persisted "last probe" table | **SCOPED DOWN** — render result ONLY from live Test Connection button in current session; if no probe run yet in session, keep current "No probe run yet" copy. Auto-probe placeholder stays disabled. Breaker state already available via `operator.circuit_state`. Log as DEV-302. |

Final AC coverage matrix:

| AC | In-Scope? | Notes |
|----|-----------|-------|
| AC-1 Operator KPI row (4 metrics) | YES | SIMs from `useSIMList({operator_id}).total`; Active Sessions from `useOperatorSessions`; Auth/s from `useOperatorMetrics` (avg `auth_rate_per_sec` over last 1h bucket); Uptime % from `useOperatorHealthHistory` (% `status='up'` over last 24h). |
| AC-2 Tab consolidation 11→9 | YES | Keep Agreements. Merge circuit→health, notifications→alerts. Add eSIM. |
| AC-3 Protocols polish | PARTIAL | Live Test result + breaker state: YES. Persisted last-probe + auto-probe: DEFERRED (DEV-302). |
| AC-4 Health tab post-merge | YES | Timeline already via `useOperatorHealthHistory`; breaker history from `circuit_state` transitions in same series; SLA line from `operator.sla_latency_threshold_ms`. |
| AC-5 APN KPI row | PARTIAL | "Top Operator" computed client-side from first page of SIMs (DEV-301). |
| AC-6 APN tab order | YES | Overview(new) → Config → IP Pools → SIMs → Traffic → Policies → Audit → Alerts. |
| AC-7 APN SIMs server-side filter | SATISFIED-BY-EXISTING | DEV-299. |
| AC-8 InfoTooltip + term copy | YES | New wrapper component around existing `Tooltip`. |
| AC-9 Hover+tap+ESC+500ms | YES | Enhancements in InfoTooltip wrapper. |
| AC-10 Tab order consistency | YES | Both pages: read-heavy → ops → data-dumps. |
| AC-11 URL tab persistence + redirects | YES | `useSearchParams` + redirect map for `circuit→health`, `notifications→alerts`. |
| AC-12 Action buttons consistent top-right | YES | Verify parity between pages; add Archive/Delete to APN header if missing. |
| AC-13 SIMs count parity with List page | YES | Already satisfied since `useSIMList({operator_id}).total` is source of truth (FIX-208). Add label "All SIMs in this tenant: N". |

## Architecture Context

### Components Involved
- **Page**: `web/src/pages/operators/detail.tsx` (1500+ lines)
- **Page**: `web/src/pages/apns/detail.tsx` (1100+ lines)
- **New atom**: `web/src/components/ui/info-tooltip.tsx` — wrapper around existing `Tooltip`
- **New data**: `web/src/lib/glossary-tooltips.ts` — term → description map
- **New tab component**: `web/src/components/operators/EsimProfilesTab.tsx` (co-located with `ProtocolsPanel.tsx`)
- **Reuse**: `web/src/components/ui/tooltip.tsx`, `web/src/hooks/use-esim.ts` (exists), `web/src/hooks/use-operator-detail.ts`

### Data Flow (KPI row)
Page mount → `useOperator`, `useOperatorMetrics`, `useOperatorHealthHistory`, `useSIMList({operator_id, limit:1})`, `useOperatorSessions(id, 1)` → compute derived values → render 4 `KPICard`s.

### API Endpoints Used (all already exist)
| Method | Path | Used for |
|--------|------|----------|
| GET | `/api/v1/operators/{id}` | Operator entity + breaker state |
| GET | `/api/v1/operators/{id}/metrics?window=1h` | Auth rate buckets |
| GET | `/api/v1/operators/{id}/health-history?hours=24` | Uptime % computation |
| GET | `/api/v1/operators/{id}/sessions?limit=1` | Active sessions count (meta.total) |
| GET | `/api/v1/sims?operator_id={id}&limit=1` | SIM total (meta.total) |
| GET | `/api/v1/esim-profiles?operator_id={id}` | eSIM Profiles tab listing |
| GET | `/api/v1/apns/{id}` | APN entity |
| GET | `/api/v1/apns/{id}/traffic?period=24h` | APN KPI (bytes_in+out sum) |
| GET | `/api/v1/sims?apn_id={id}&limit=1` | APN SIM total + top operator derive |
| POST | `/api/v1/operators/{id}/test/{protocol}` | Protocols tab live test |

No backend changes. No DB changes. No migrations.

### Token Map (from docs/FRONTEND.md)

#### Color Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Primary text | `text-text-primary` | `text-gray-900`, `text-[#0f172a]` |
| Secondary text | `text-text-secondary` | `text-gray-500` |
| Tertiary text | `text-text-tertiary` | `text-gray-400` |
| Card bg | `bg-surface` | `bg-white` |
| Elevated bg | `bg-bg-elevated` | `bg-gray-50` |
| Border | `border-border` | `border-gray-200` |
| Accent (link/CTA) | `text-accent`, `bg-accent` | `text-blue-500` |
| Success | `text-success`, `bg-success-dim` | `text-green-500` |
| Danger | `text-danger`, `bg-danger-dim` | `text-red-500` |

#### Typography
| Usage | Class | NEVER Use |
|-------|-------|-----------|
| Page title | `text-[16px] font-semibold` | `text-xl`, `text-2xl` |
| KPI label | `text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium` | Arbitrary small caps |
| KPI value | `text-[22px] font-semibold tabular-nums` | `text-2xl` |
| Mono ID | `font-mono text-xs text-text-secondary` | raw `<code>` |

#### Spacing & Shape
| Usage | Class |
|-------|-------|
| Card radius | `rounded-[var(--radius-md)]` (follow existing `Card`) |
| KPI card padding | `p-4` |
| Tab bar gap | `gap-1.5` (existing pattern) |

#### Components to REUSE (DO NOT recreate)
| Component | Path | Use For |
|-----------|------|---------|
| `<Card>`, `<CardContent>`, `<CardHeader>` | `web/src/components/ui/card.tsx` | KPI containers |
| `<Tooltip>` | `web/src/components/ui/tooltip.tsx` | InfoTooltip wraps this |
| `<Badge>` | `web/src/components/ui/badge.tsx` | State chips |
| `<Tabs>`, `<TabsList>`, `<TabsTrigger>`, `<TabsContent>` | `web/src/components/ui/tabs.tsx` | Tab bar |
| `<Skeleton>` | `web/src/components/ui/skeleton.tsx` | KPI/list loading |
| `<EmptyState>` | `web/src/components/shared/empty-state.tsx` | eSIM empty |
| `<EntityLink>` | `web/src/components/shared/entity-link.tsx` | eSIM rows → sim links (FIX-219) |
| `<Table>` family | `web/src/components/ui/table.tsx` | eSIM Profiles rows |
| `<DataFreshness>` | `web/src/components/shared/data-freshness.tsx` | KPI staleness indicators |

**NEVER** re-create `KPICard` — the dashboard's `KPICard` is page-local (`web/src/pages/dashboard/index.tsx:136`). Do NOT import cross-page; instead, build a lightweight inline KPI tile consistent with dashboard visuals (label + value + optional subtitle/delta) or extract `KPICard` to `web/src/components/shared/kpi-card.tsx` as prep work. **Decision: extract to shared** (Task 1 below) — two consumers = time to share.

### InfoTooltip Contract

```
<InfoTooltip term="MCC">MCC</InfoTooltip>
```
- Renders children + `ⓘ` icon (lucide `Info` h-3 w-3 text-text-tertiary) with 2px gap
- On hover (desktop) or tap (mobile): show `Tooltip` with copy from `glossary-tooltips.ts[term]`
- 500ms delay before show (AC-9)
- ESC key closes (AC-9)
- Falls back to nothing if term unknown (dev warning in console)

### URL Tab Persistence + Redirects (AC-11)

Hook: `web/src/hooks/use-tab-url-sync.ts` (NEW — small util)

```
useTabUrlSync({
  defaultTab: 'overview',
  aliases: { circuit: 'health', notifications: 'alerts' },
})
// returns [activeTab, setActiveTab]
```

Reads `?tab=` on mount; if value is in `aliases`, replace URL without pushing history; writes `?tab=` on change (replace, not push). Shared by both detail pages.

### Glossary Tooltip Copy (AC-8)

`web/src/lib/glossary-tooltips.ts`:
```
// Copy source: docs/GLOSSARY.md (single source of truth).
// When adding a term here, also add/update in GLOSSARY.md.
export const GLOSSARY_TOOLTIPS: Record<string, string> = {
  MCC: 'Mobile Country Code (3 digits identifying country, e.g. 286 = Turkey)',
  MNC: 'Mobile Network Code (2-3 digits identifying operator within country)',
  EID: 'Embedded UICC Identifier (32-digit eUICC chip serial)',
  MSISDN: 'Mobile Station ISDN Number (phone number)',
  APN: 'Access Point Name (network entry identifier)',
  IMSI: 'International Mobile Subscriber Identity (15-digit subscriber ID)',
  ICCID: 'Integrated Circuit Card Identifier (SIM card serial)',
  CoA: 'Change of Authorization (mid-session policy update, RFC 5176)',
  SLA: 'Service Level Agreement (uptime contract)',
}
```

## Prerequisites
- [x] FIX-202 (DTO standardization) — DONE
- [x] FIX-208 (aggregates facade) — DONE
- [x] FIX-219 (EntityLink) — DONE (used in eSIM rows)
- FIX-235 (eSIM provisioning pipeline) — NOT required for this story (see Scope Decisions)
- FIX-238 (roaming removal) — NOT required; Agreements tab stays

## Waves & Tasks (3 waves, 8 tasks)

### Wave 1 — Shared primitives (parallel)
- Task 1: Extract `KPICard` to shared
- Task 2: InfoTooltip + glossary copy
- Task 3: `useTabUrlSync` hook

### Wave 2 — Feature components (parallel, depend on Wave 1)
- Task 4: EsimProfilesTab component
- Task 5: Operator Detail refactor (KPI row + tabs + tooltips)
- Task 6: APN Detail refactor (KPI row + tabs + tooltips)

### Wave 3 — Integration & tests (sequential)
- Task 7: Verification sweep (manual walk-through + grep) and story-level doc updates
- Task 8: Unit tests (InfoTooltip, useTabUrlSync, KPI computations)

---

### Task 1: Extract KPICard to shared primitive
- **Files:** Create `web/src/components/shared/kpi-card.tsx`; Modify `web/src/pages/dashboard/index.tsx` (import instead of inline)
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Copy from `web/src/pages/dashboard/index.tsx:121-260` (KPICard) preserving props surface (title, value, formatter, sparklineData, color, delta, deltaFormat, live, suffix, subtitle, onClick, delay).
- **Context refs:** Token Map > Components to REUSE; Token Map > Color Tokens; Token Map > Typography
- **What:** Export `KPICard` (memo), `KPICardProps` from new file. Dashboard imports it. No visual regression allowed. Props unchanged.
- **Verify:** `grep -n "KPICard" web/src/pages/dashboard/index.tsx` shows import only, not re-declaration. Dashboard renders identically (visual check).

### Task 2: InfoTooltip component + glossary copy file
- **Files:** Create `web/src/components/ui/info-tooltip.tsx`; Create `web/src/lib/glossary-tooltips.ts`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `web/src/components/ui/tooltip.tsx` — InfoTooltip wraps (does NOT replace) Tooltip. Read `web/src/components/shared/data-freshness.tsx` for tooltip-usage precedent (line 36).
- **Context refs:** Token Map > Color Tokens; InfoTooltip Contract; Glossary Tooltip Copy
- **What:**
  1. `glossary-tooltips.ts` — export const map for 9 terms (see Glossary section above).
  2. `info-tooltip.tsx` — accepts `term: string` + `children`. Renders children inline followed by a lucide `Info` icon (`h-3 w-3 text-text-tertiary inline-block`) wrapped in a button with `aria-label`. Use 500ms onMouseEnter timer (clear on leave); on focus/click (mobile), toggle. ESC key listener when open. Under the hood reuses the existing `<Tooltip content={...}>` primitive — passes `content={GLOSSARY_TOOLTIPS[term]}`; if missing, logs `console.warn` in dev and renders children without tooltip.
- **Tokens:** icon uses `text-text-tertiary`; underlying Tooltip already token-correct. Zero hardcoded hex.
- **Verify:** `grep -E '#[0-9a-fA-F]{3,6}' web/src/components/ui/info-tooltip.tsx web/src/lib/glossary-tooltips.ts` returns zero matches. TS compiles.

### Task 3: useTabUrlSync hook
- **Files:** Create `web/src/hooks/use-tab-url-sync.ts`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `web/src/hooks/use-timeframe-url-sync.ts` (referenced in DEV-291) — mirror its shape for URL param (`tf`) handling.
- **Context refs:** URL Tab Persistence + Redirects
- **What:** Hook signature `useTabUrlSync({defaultTab, aliases?, paramName?='tab'}) => [tab, setTab]`. Uses `useSearchParams`. On mount: if `?tab=X` and `X in aliases`, call `setSearchParams({tab: aliases[X]}, {replace: true})`; if `X` invalid (not in known tabs — consumer passes `validTabs?: string[]`), fall back to defaultTab with replace. `setTab(t)` writes `?tab=t` via `setSearchParams(..., {replace: true})` — no history push. ESLint-clean, SSR-safe (though not needed here).
- **Verify:** `tsc --noEmit` passes. Basic render test in Task 8.

### Task 4: EsimProfilesTab component
- **Files:** Create `web/src/components/operators/EsimProfilesTab.tsx`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/operators/detail.tsx:1033-1154` (`OperatorSimsTab`) — follow the same Card + Table + Skeleton + EmptyState + Load-more structure. Read `web/src/hooks/use-esim.ts` to confirm list hook shape (should already expose operator_id filter; if not, add one — see Verify).
- **Context refs:** API Endpoints > `/api/v1/esim-profiles?operator_id=`; Token Map > Components to REUSE; Data Flow
- **What:** Takes `operatorId: string`. Calls `useESimProfiles({ operator_id: operatorId })` (verify or extend existing hook). Table columns: **EID** (with InfoTooltip), **ICCID** (InfoTooltip), **Profile State** (Badge: released/installed/enabled/disabled), **SIM** (EntityLink to `/sims/{sim_id}`), **Created At**. Summary strip above table: "Installed: N · Enabled: M · Disabled: K" computed from page. Empty state: "No eSIM profiles on this operator." Loading: 5 skeleton rows. Use `EntityLink` from FIX-219.
- **Tokens:** ALL from Token Map. Zero hex.
- **Verify:** `grep -E '#[0-9a-fA-F]{3,6}' web/src/components/operators/EsimProfilesTab.tsx` → zero matches. Opens tab on operator detail and shows list (smoke in Task 7).

### Task 5: Operator Detail — KPI row + tab consolidation + tooltips
- **Files:** Modify `web/src/pages/operators/detail.tsx`
- **Depends on:** Task 1, Task 2, Task 3, Task 4
- **Complexity:** high
- **Pattern ref:** Follow the page's existing sections — e.g. KPI row goes ABOVE the `<Tabs>` block starting at line 1295. Tabs list order: overview, protocols, health, traffic, sessions, sims, esim, alerts, audit, agreements (trailing, transitional). Remove `circuit` and `notifications` TabsTrigger + TabsContent blocks.
- **Context refs:** Architecture Context; Data Flow (KPI row); API Endpoints; Token Map (ALL sections); URL Tab Persistence; AC coverage matrix
- **What:**
  1. Replace `useState('overview')` with `useTabUrlSync({defaultTab:'overview', aliases:{circuit:'health', notifications:'alerts'}, validTabs:[...]})`.
  2. Insert KPI row (grid 1/2/4 responsive, 4 cards) below the header actions block (around line 1293):
     - **SIMs** — value from `useSIMList({operator_id, limit:1}).data.pages[0].meta.total` (or equivalent); subtitle "All SIMs in this tenant" (AC-13).
     - **Active Sessions** — from `useOperatorSessions(id, 1)` — count returned by the endpoint (check hook; if endpoint doesn't return total, fall back to visible-row count with a note subtitle "current page"). Live indicator if WebSocket active.
     - **Auth/s (1h)** — average of `metrics.buckets[*].auth_rate_per_sec` over last 12 buckets (5min × 12 = 1h). 0 if no buckets. Sparkline = `buckets[*].auth_rate_per_sec`.
     - **Uptime % (24h)** — `healthHistory.filter(e => e.status==='up').length / healthHistory.length * 100`. Format `##.#%`. Subtitle "Last 24h".
  3. Circuit Breaker content (previously tab `circuit`) moves into Health tab as a section below the timeline chart (reuse existing JSX). Notifications content moves into Alerts tab (existing Alerts section gets a tabbed sub-switcher `Alerts | System notifications` OR simply concatenate the two lists — pick concat to keep simple; Developer notes in code comment).
  4. Add eSIM tab: `<TabsTrigger value="esim">` + `<TabsContent value="esim"><EsimProfilesTab operatorId={operator.id} /></TabsContent>`.
  5. Wrap technical term labels with `<InfoTooltip term="MCC">`, `term="MNC"`, `term="EID"` (in profile strip), `term="MSISDN"`, `term="ICCID"`, `term="IMSI"`, `term="APN"` wherever they appear as labels (overview config rows, SIMs tab column headers, etc.). Do NOT wrap data values — only labels.
  6. Verify action buttons (Edit, Delete) stay top-right (AC-12).
- **Tokens:** Zero hex/arbitrary px. Grid: `grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4`.
- **Note:** Invoke `frontend-design` skill for final polish review.
- **Verify:**
  1. `grep -E '#[0-9a-fA-F]{3,6}' web/src/pages/operators/detail.tsx` no new matches beyond pre-existing.
  2. Navigate `/operators/<id>?tab=circuit` → URL replaces to `?tab=health`.
  3. Navigate `/operators/<id>?tab=notifications` → URL replaces to `?tab=alerts`.
  4. Tab count = 10 (agreements transitional); spec count after FIX-238 = 9.

### Task 6: APN Detail — KPI row + tab reorder + tooltips
- **Files:** Modify `web/src/pages/apns/detail.tsx`
- **Depends on:** Task 1, Task 2, Task 3
- **Complexity:** medium
- **Pattern ref:** Follow same pattern as Task 5 at lines 1037-1090. Current APN tabs: config, ip-pools, sims, audit, notifications, alerts, traffic, policies. New order (AC-6): **overview (NEW), config, ip-pools, sims, traffic, policies, audit, alerts**. Merge notifications into alerts (same pattern as Task 5).
- **Context refs:** Architecture Context; Token Map; URL Tab Persistence; API Endpoints (APN block); AC-5, AC-6, AC-11
- **What:**
  1. Replace activeTab state with `useTabUrlSync({defaultTab:'overview', aliases:{notifications:'alerts'}, validTabs:[...]})`.
  2. Add **overview** tab as first position. Overview content: config summary + IP pool summary + recent alerts (lightweight) — or simply the existing config tab contents if time-boxed; developer picks. Primary goal is to establish read-heavy-first order.
  3. Add KPI row (grid 4) above Tabs:
     - **SIMs** — from `useSIMList({apn_id, limit:1}).meta.total`.
     - **Active Sessions** — from `/apns/{id}/sessions` if exists; else show "—" with tooltip "Endpoint pending" (check hook; if missing, skip this KPI and show 3 KPIs, not 4, with a TODO comment).
     - **Traffic Last 24h** — sum of `useAPNTraffic(apnId,'24h').series[*].(bytes_in+bytes_out)` via `formatBytes`.
     - **Top Operator** — from first page of `useSIMList({apn_id})`, group SIMs by `operator_name` and pick max. If tie or empty, show `—`.
  4. Wrap APN, MCC, MNC (if shown in config), ICCID/IMSI/MSISDN (in SIMs column headers — though done in Task 5 if shared) with InfoTooltip.
  5. Verify action buttons (Edit, Delete, Archive) top-right on the page header block (line 1001-ish).
- **Tokens:** Zero hex/arbitrary.
- **Verify:**
  1. Navigate `/apns/<id>?tab=notifications` → URL replaces to `?tab=alerts`.
  2. Tab count = 8 per AC-6.
  3. KPI row renders non-empty values on seeded data.

### Task 7: Verification sweep + doc updates
- **Files:** Modify `docs/GLOSSARY.md` (confirm all 9 terms present, add EID/MCC/MNC/APN/CoA/SLA entries if missing); Modify `docs/ROUTEMAP.md` Tech Debt table (add D-entries for deferred items); Modify `docs/brainstorming/decisions.md` (DEV-298..DEV-302 already recorded in Task 0 — confirm present); No new stories.
- **Depends on:** Task 5, Task 6
- **Complexity:** low
- **Pattern ref:** Read `docs/brainstorming/decisions.md:2570-2600` (recent DEV- entries) for format.
- **Context refs:** Scope Decisions section of this plan
- **What:**
  1. Walk both detail pages in dev server; verify URL redirects, KPI values, tooltip popups with 500ms delay, ESC close.
  2. `grep -r 'MCC\|MNC\|MSISDN' web/src/pages/operators/detail.tsx web/src/pages/apns/detail.tsx` — every acronym that is a label must be wrapped in `InfoTooltip` (data values exempt).
  3. Confirm GLOSSARY.md has entries for: MCC, MNC, EID, MSISDN, APN, IMSI, ICCID, CoA, SLA. Add any missing with same copy as `glossary-tooltips.ts`.
  4. Add ROUTEMAP Tech Debt rows: D-108 (FIX-235 eSIM provisioning pipeline — plan decoupled from FIX-222), D-109 (FIX-233 SIM filter deferred — verify if already tracked), D-110 (APN top-operator backend endpoint — defer to FIX-236 scale work).
- **Verify:** `grep -n "InfoTooltip" web/src/pages/operators/detail.tsx web/src/pages/apns/detail.tsx | wc -l` ≥ 15 (9 terms × 2 pages minus duplicates).

### Task 8: Unit tests
- **Files:** Create `web/src/components/ui/__tests__/info-tooltip.test.tsx`; Create `web/src/hooks/__tests__/use-tab-url-sync.test.tsx`
- **Depends on:** Task 2, Task 3
- **Complexity:** low
- **Pattern ref:** Read any existing `*.test.tsx` in `web/src/components` — match Vitest + RTL convention.
- **Context refs:** InfoTooltip Contract; URL Tab Persistence + Redirects
- **What:**
  1. `info-tooltip.test.tsx`: renders term + Info icon; opens tooltip after 500ms hover; ESC closes; unknown term warns in dev console.
  2. `use-tab-url-sync.test.tsx`: aliased query params redirect on mount; invalid tab falls back to default; setTab writes URL.
- **Verify:** `cd web && npm test` — both files pass.

## Acceptance Criteria Mapping
| AC | Implemented In | Verified By |
|----|---------------|-------------|
| AC-1 Operator KPI row | Task 5 | Task 7 walkthrough |
| AC-2 Tab consolidation 11→9+1 transitional | Task 5 | Task 7 (count tabs) |
| AC-3 Protocols polish (partial) | Task 5 (breaker + live test result; persisted probe DEV-302) | Task 7 |
| AC-4 Health tab post-merge | Task 5 | Task 7 |
| AC-5 APN KPI row (Top Operator client-derived) | Task 6 | Task 7 |
| AC-6 APN tab order | Task 6 | Task 7 |
| AC-7 APN SIMs server filter | Already satisfied (DEV-299) | Discovery verified |
| AC-8 InfoTooltip + term copy | Task 2, Task 5, Task 6 | Task 7 grep, Task 8 |
| AC-9 Hover+tap+ESC+500ms | Task 2 | Task 8 |
| AC-10 Tab order consistency | Task 5, Task 6 | Task 7 |
| AC-11 URL tab persistence + redirects | Task 3, Task 5, Task 6 | Task 8 + Task 7 nav test |
| AC-12 Action buttons top-right | Task 5, Task 6 | Task 7 visual |
| AC-13 SIM count parity + tenant label | Task 5 | Task 7 |

## Story-Specific Compliance Rules
- **UI:** Design tokens only — no hex/arbitrary px. `frontend-design` skill review on Task 5.
- **UX:** InfoTooltip must not steal focus or block table clicks. Icon button has `aria-label` + keyboard focus ring.
- **Routing:** URL sync uses `replace`, not `push` — tab changes must NOT pollute browser history.
- **Performance:** KPI computations (uptime%, top operator) happen in `useMemo`; no unnecessary re-renders.
- **i18n:** All tooltip copy in English per project convention.
- **Backward compat:** Old `?tab=circuit` + `?tab=notifications` links still work (AC-11 redirect).

## Bug Pattern Warnings
(Scanning `docs/brainstorming/bug-patterns.md`.)
- Apply **PAT-001** (list endpoints with FK relations → eager loading) — eSIM list hook must use `ListEnriched` (already confirmed in `internal/store/esim.go` line 518) — no change needed.
- Apply **PAT-003** (Turkish character integrity) — N/A (English copy).
- New risk to add AFTER this story ships: tab URL redirect logic tends to infinite-loop if alias maps to another alias — write tests covering `circuit→health` and `notifications→alerts` do NOT re-trigger.

## Tech Debt (from ROUTEMAP)
No existing Tech Debt items target FIX-222. New items added by this story (via Task 7):
- D-108 — eSIM Profiles tab built on pre-existing infra; FIX-235 pipeline refactor still pending (unblocks deeper SGP.22 flows, not this tab).
- D-109 — Confirm AC-3 persisted "last probe" feature tracked or add new line (Protocols auto-probe).
- D-110 — APN "Top Operator" KPI computed client-side from first page; backend aggregation deferred.

## Mock Retirement
No mocks involved. All endpoints real.

## Risks & Mitigations
- **R1 Tab merge loses information:** Circuit breaker content lives as a labelled section inside Health tab (not collapsed). Notifications lives as a second list within Alerts tab with a clear section header.
- **R2 KPI compute heavy:** All four operator KPIs derive from existing cached hooks (staleTime 15-30s). No new endpoints.
- **R3 InfoTooltip regression for existing Tooltip users:** InfoTooltip is a NEW wrapper — existing `Tooltip` is untouched. Zero blast radius on 7 current consumers.
- **R4 APN Top Operator incorrect on large datasets:** Compute uses first page (50 SIMs) — flag in subtitle "Based on first 50 SIMs" if `hasNextPage`. True max deferred to backend endpoint (D-110).
- **R5 URL param collision:** Use `tab` — confirmed no collision in either page's existing `useSearchParams` usage (grep passed).

## Pre-Validation Checklist
- [x] Min lines (plan > 60 for M): MET
- [x] Min tasks for M (3): 8 tasks — MET
- [x] Required sections (Goal, Architecture Context, Tasks, AC Mapping): PRESENT
- [x] API specs embedded: YES
- [x] No DB changes (nothing to embed): N/A
- [x] Design Token Map populated: YES
- [x] Component Reuse table: YES
- [x] Task complexity mix: 1 high (Task 5), 1 medium (Task 4, Task 6), 5 low — appropriate for M story
- [x] Context refs point to real sections: VERIFIED
- [x] Pattern refs on every creating task: YES
- [x] Scope decisions documented with DEV-### IDs: YES
