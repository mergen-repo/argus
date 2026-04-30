# Implementation Plan: FIX-213 — Live Event Stream UX (Filter Chips, Usage Display, Alert Body, Clickable Entity)

- **Story**: `docs/stories/fix-ui-review/FIX-213-live-event-stream-ux.md`
- **Tier / Effort**: P1 · M · Wave 3
- **Findings addressed**: F-09, F-12, F-19, F-20 (see `docs/reviews/ui-review-2026-04-19.md`)
- **Depends on**: FIX-212 DONE (envelope + catalog + `web/src/types/events.ts`), FIX-211 DONE (5-level severity), FIX-210 DONE (dedup), FIX-209 DONE (alerts table, `meta.alert_id`)
- **Mode**: FIX-NNN pre-release — architecture NOT frozen; refactor existing `event-stream/` tree freely.

---

## Decisions (read first — resolve story-vs-codebase drift before tasks)

| # | Topic | Decision | Rationale |
|---|---|---|---|
| **D1** | File paths: story lists `web/src/components/live-event-stream/{stream,event-card,filter-bar}.tsx` + `web/src/hooks/use-live-events.ts` — **none exist** | **Extend existing `web/src/components/event-stream/`**. Split the monolithic `event-stream-drawer.tsx` (137 LOC, contains drawer + row + `SourceChips`) into: `event-stream-drawer.tsx` (shell + virtual list), `event-row.tsx` (single row card), `event-filter-bar.tsx` (NEW, sticky chips), `event-source-chips.tsx` (extracted from current file). No new `use-live-events.ts` hook — filter state lives in `stores/events.ts` zustand (keeps addEvent + filter selectors colocated). Document path deviation in commit body. | Story file paths were written against a hypothetical structure; actual tree uses `event-stream/`. AUTOPILOT: follow the codebase, not the story's imagined paths. No hook needed because there's no shared subscription — dashboard-layout already owns the single WS listener. |
| **D2** | Drawer vs page — story says "Event Stream page", dispatch says "Event Stream page", **codebase is a right-side Sheet drawer** | **Keep drawer (Sheet)**. All F-09/F-12/F-19/F-20 findings reference the Dashboard drawer, not a standalone page. No `/events` route exists in `router.tsx`. A standalone page is out-of-scope for FIX-213 (would be a new story). | Findings are drawer-scoped. Adding a new route would expand the blast radius into sidebar nav + SCREENS.md and is not what the findings ask for. |
| **D3** | Normalizer is the real bug surface for F-09/F-12 | **Patch `useGlobalEventListener` in `dashboard-layout.tsx:19-63`** to read the FIX-212 envelope shape first (`env.title`, `env.message`, `env.entity.*`, `env.meta.*`, `env.dedup_key`), fall back to legacy flat shape (`d.message`, `d.imsi`, `d.operator_id`) behind a dev-only `console.debug('legacy event shape', subject)` warn. **LiveEvent type gains**: `title`, `entity: {type,id,display_name}`, `meta: Record<string, unknown>`, `dedup_key`, `source`, `event_version`. Current flat fields (`imsi`, `operator_id`, etc.) stay as mirrored legacy fields for one release (FIX-212 D3 grace window). | F-09 root cause: `pickString(d.message) || msg.type.replace(/\./g, ' ')` reads legacy flat shape — FIX-212 envelope puts the human-readable text in `title` and `message`. Without normalizer patch, every other UI task in this plan is built on a lie. |
| **D4** | AC-3 "session events display bytes_in/out + duration" — **`session_duration` is NOT in catalog** | **Render `bytes_in + bytes_out` from `meta.bytes_in`/`meta.bytes_out`** (both are `int` per catalog). **Do NOT render duration from meta** — catalog only ships `session_id`, `termination_cause`, `bytes_in`, `bytes_out`. Instead, for `session.ended` rows, if the drawer happens to hold a matching `session.started` event in the 100-500 buffer (keyed by `meta.session_id`), compute client-side delta and render `"·  42s"`. If no match, render bytes only. This is best-effort, never a promise. | Story AC-3 wording says "duration" but upstream catalog doesn't expose it. Rather than forcing a backend change (out of FIX-213 scope), compute opportunistically from in-buffer pairs. Log drift as a follow-up in commit (future story = add `duration_seconds` to `session.ended` meta at publisher side). |
| **D5** | AC-4 "Details link to alert row" — which UUID? | **Use `meta.alert_id`** (populated by FIX-209 alerts-table persistence + FIX-212 envelope for operator_down / policy_violation / anomaly_* / system_storage). **If `meta.alert_id` present → link to `/alerts/:alert_id`**. Otherwise fall back to `entity.{type}/{id}` navigation (operator → `/operators/:id`, sim → `/sims/:id`, etc.). Never render a broken link. | Envelope's `entity` points at the impacted domain object (operator/sim), not the alert row itself. FIX-209's alerts table row UUID lives in `meta.alert_id`. Verified at `internal/operator/events.go:22` — already ships `AlertID` field. |
| **D6** | AC-6 pause/resume + queued events | **Add `paused: boolean` + `queuedEvents: LiveEvent[]` to `stores/events.ts`**. When `paused=true`, `addEvent` pushes into `queuedEvents` (capped 500). On resume, flush queue into `events` in reverse-chronological order, reset `queuedEvents=[]`. Resume button shows badge `"N new events queued"` when `queuedEvents.length > 0`. Histogram buckets keep updating even when paused (live heartbeat data for topbar sparkline does not freeze). | AC-6 explicit: "stop auto-append while reviewing (button icon change + 'N new events queued' badge on resume)". Separate buffer keeps scroll position stable during review. Histogram must stay live or topbar sparkline goes dead. |
| **D7** | AC-8 buffer size — zustand hardcoded `.slice(0, 100)` | **Raise to `.slice(0, 500)`**. Memory impact: 500 rows × ~600 bytes/row ≈ 300KB RAM — trivial. | AC-8 explicit: "Max 500 events in buffer". |
| **D8** | AC-9 virtual scrolling | **Install `@tanstack/react-virtual@^3`** (prerequisite task). Virtual list activates only when `filteredEvents.length > 100` (threshold avoids virtualization overhead for typical <100 buffer). | AC-9 explicit. Library is tiny (3KB gzip), industry-standard for TanStack-family projects (already use `@tanstack/react-query`). |
| **D9** | Relative time formatter "3m ago" (AC-2) | **No date-fns dependency**. Add `formatRelativeTime(iso: string): string` to existing `web/src/lib/format.ts`. Returns `"Xs ago"` / `"Xm ago"` / `"Xh ago"` / `"Xd ago"` (Turkish UI uses same format since these are universally understood abbreviations — consistent with existing `formatBytes` lib style). Refresh triggered by a single `setInterval` (15s) managed by `event-stream-drawer` while open; no re-render when closed. | `date-fns` not in `web/package.json`; full library is ~20KB. Bespoke helper is 12 LOC. Interval scoped to drawer-open keeps CPU silent when drawer is closed. |
| **D10** | Filter chip source of truth (PAT-015 gate) | **`useEventCatalog()` TanStack Query hook** fetches `GET /api/v1/events/catalog` on drawer first-open (staleTime 5min). Chips derived from catalog response: type chip set = `catalog.map(c => c.type)`, entity chip set = `[...new Set(catalog.map(c => c.entity_type))]`, source chip set = `[...new Set(catalog.map(c => c.source))]`. **Severity chip set is the static 5-level taxonomy from FIX-211** (hardcoded, closed taxonomy, does NOT change based on catalog). | PAT-015 requires FIX-213 to actually consume `web/src/types/events.ts` that FIX-212 shipped. Catalog-backed chips mean new subjects auto-appear in the UI. Severity exception: taxonomy is architecturally fixed at 5 levels per FIX-211; deriving from catalog would be noise. |
| **D11** | Filter persistence | **LocalStorage key `argus.events.filters.v1`** holds `{types: string[], severities: Severity[], entityTypes: string[], sources: string[], dateRange: 'session'\|'1h'\|'24h'}`. Rehydrate on drawer mount. Clear button resets filters AND clears localStorage. **Note: `sessionStartTs` is NOT persisted** — it's tab-scoped and resets on page reload, by design. Only the `filters` object goes to localStorage. | UX: user expects filters to persist across drawer open/close. Versioned key (`.v1`) allows future migration. Session timestamp must reset per-tab or stale values survive reloads. |
| **D12** | "Date range" filter semantics | **"current session"** = since drawer was first opened this tab (`sessionStartTs = Date.now()` captured once on first store init, NOT persisted to localStorage, reset on page reload). **"last 1h"** / **"last 24h"** = `Date.now() - (3600\|86400)*1000`. Histograms keep the 15-minute rolling window regardless of filter. Filter is applied client-side only — no API call for historical events (drawer is a live stream, not a log viewer). | AC-1 explicit. Log viewer = FIX-238 Events Page story (downstream, out of scope). |
| **D13** | Clickable entity — F-19 | **Entity displayName is rendered as a button, not an `<a>`**, using existing `onClick={() => navigate(...)}`. Routes table: `sim → /sims/:id`, `operator → /operators/:id`, `apn → /apns/:id`, `policy → /policies/:id`, `session → /sessions/:id`, `agreement → /roaming-agreements/:id`, `consumer → /ops/infra` (no detail page, go to infra overview), `system → /system/health`, `job → /jobs`, `anomaly → /analytics/anomalies`. Unknown `entity.type` → no click handler, render as plain span. Navigation closes drawer (`setDrawerOpen(false)`). | All routes verified in `web/src/router.tsx`. `consumer` and `system` have no detail page — graceful fallback to their overview pages avoids 404s. |
| **D14** | Turkish UI text scope | **Labels are Turkish**: filter chip headers (`Tür`, `Önem`, `Varlık`, `Kaynak`, `Zaman`), buttons (`Duraklat`, `Devam Et`, `Temizle`), badge (`N yeni olay`). **Catalog-derived values stay as-is** (English `session.started`, etc.) — those are event type identifiers, not user-facing prose. **Event titles/messages from envelope stay as-is** — backend decides language (currently English). | Codebase pattern: UI chrome Turkish, identifier-y technical content English. `frontend-design` aesthetic. |
| **D15** | Story AC-4 "Alert events expanded" rendering | **Do NOT build an expand/collapse row UI**. AC-4 is satisfied by: severity badge (already present), source chip (new, from `env.source`), title line (envelope `title`), message line (envelope `message`) rendered as a secondary muted text line when present and different from title, "Details" link (D5). No accordion — event rows stay flat (per existing drawer pattern, consistent with SIM list / alerts list density). | Inline expansion in a 400px-wide drawer would waste vertical space; a flat row with severity + title + message + details-link is equally expressive and preserves scan density. |

---

## Problem Context

### Current state (verified files)

- `web/src/components/event-stream/event-stream-drawer.tsx` — monolithic 137 LOC: Sheet shell + row rendering + `SourceChips`. No filter bar, no pause, no virtualization, no entity resolution, no catalog consumption.
- `web/src/stores/events.ts` — zustand store: `events[]` capped `slice(0, 100)`, histogram, operatorHistogram, drawerOpen. No paused/queue state, no filter state.
- `web/src/components/layout/dashboard-layout.tsx:19-63` — `useGlobalEventListener` normalizer. Reads **legacy flat shape**: `d.message`, `d.imsi`, `d.operator_id`, etc. Does NOT read envelope `title` / `entity.display_name` / `meta.*`. This is the root cause of F-09 ("alert.new shows generic 'alert new'") and F-12 (no bytes chip).
- `web/src/types/events.ts` — FIX-212 envelope types already declared (BusEnvelope, EntityRef, EventSeverity, EventCatalogEntry, EventCatalogResponse, envelopeRowLabel). Zero runtime consumers today.
- `internal/api/events/catalog.go` — 14+ catalog entries with meta_schema; served at `GET /api/v1/events/catalog`.

### Drift vs findings

| Finding | Current behavior | Target behavior |
|---|---|---|
| F-09 | Alert row shows `"alert new"` (generic) + `"operator 20000000"` (truncated UUID) | Alert row shows envelope `title` (e.g. `"SLA violation for operator Turkcell"`) + envelope `message` + clickable operator display_name. Severity badge + source chip. |
| F-12 | `session.updated` row shows IMSI/IP/MSISDN chips, NO bytes | `session.updated` row shows `↓2.1MB ↑48KB` chip derived from `meta.bytes_in/out` via `formatBytes()`. |
| F-19 | Entity ID truncated to UUID prefix, not clickable | Entity renders as display_name (from `env.entity.display_name`), clickable → navigates to detail route per D13. |
| F-20 | No filter chips at all | Sticky filter bar above event list: type (multi-select), severity (5-level), entity type, source, date range. Filters derived from catalog endpoint (D10). |

### Out of scope (do NOT touch)

- Standalone `/events` page/route (would be a new story).
- Backend event catalog additions (no new meta keys in FIX-213).
- Notification preferences UI (that's FIX-240).
- Session duration in `session.ended` meta (mentioned in D4 as follow-up).
- Histogram bucket logic (already works in `stores/events.ts`).
- Topbar sparkline (reads `histogram` — unaffected).
- Notification drawer (`/components/notification/notification-drawer.tsx`) — separate UX.

---

## Architecture Context

### Data flow

```
NATS subject (e.g. argus.events.alert.triggered)
    → internal/bus/envelope.go (FIX-212 bus.Envelope JSON)
    → internal/ws/hub.go relayNATSEvent
    → WebSocket → web/src/lib/ws.ts
    → wsClient.subscribe('*', handler)
    → dashboard-layout.tsx useGlobalEventListener
        [PATCH HERE: read envelope shape, not flat legacy shape]
    → stores/events.ts addEvent(liveEvent)  [or queuedEvents if paused]
    → EventStreamDrawer subscribes to store
        → useEventCatalog() TanStack query for filter chip options
        → EventFilterBar (sticky top)
        → VirtualList (if filteredEvents.length > 100)
            → EventRow
                → SeverityBadge + SourceChip + EntityButton(onClick=navigate) + title + message + time
                → UsageChips (bytes_in/out via formatBytes) for session.* types
                → DetailsLink ("/alerts/:alert_id" via meta.alert_id) for alert.* types
```

### Components (after refactor)

| File | Role | LOC est. |
|------|------|----------|
| `web/src/components/event-stream/event-stream-drawer.tsx` | Sheet shell, header, live indicator, virtual list container, pause/resume/clear buttons | ~120 |
| `web/src/components/event-stream/event-row.tsx` | Single row: icon + severity badge + source chip + title + message + entity button + timestamp + usage chips + details link | ~150 |
| `web/src/components/event-stream/event-filter-bar.tsx` | Sticky chip groups: type / severity / entity-type / source / date-range. Popover multi-select with counts | ~180 |
| `web/src/components/event-stream/event-source-chips.tsx` | Extracted from drawer — renders IMSI/IP/MSISDN chips. Bytes chip added here | ~90 |
| `web/src/components/event-stream/event-entity-button.tsx` | Clickable display_name → navigate(route map) | ~50 |
| `web/src/hooks/use-event-catalog.ts` | TanStack Query hook for `/api/v1/events/catalog`, 5min staleTime | ~30 |
| `web/src/stores/events.ts` | Extend with `paused`, `queuedEvents`, `filters`, filter selectors, localStorage sync | +100 |
| `web/src/types/events.ts` | **No changes** (FIX-212 types consumed as-is) | 0 |
| `web/src/components/layout/dashboard-layout.tsx` | Patch `useGlobalEventListener` to read envelope shape first | +30 |
| `web/src/lib/format.ts` | Add `formatRelativeTime(iso)` | +15 |

### API surface (consumed, not defined)

| Endpoint | Method | Source | Fields used |
|---|---|---|---|
| `/api/v1/events/catalog` | GET | FIX-212 | `events[].{type, source, default_severity, entity_type, description, meta_schema}` |

---

## Screen Mockup (ASCII — Live Event Stream drawer)

```
┌─────────────────────────────────────────────────────────────┐
│  Activity  Live Event Stream                    ●LIVE      │
│  ─────────────────────────────────────────────────────────  │
│  Son 420 olay, 14 aktif filtre                              │
│  ┌───────────────────────────────────────────────────────┐ │
│  │ Tür (4) ▾ │ Önem (2) ▾ │ Varlık ▾ │ Kaynak ▾ │ 1s ▾  │ │
│  └───────────────────────────────────────────────────────┘ │
│  [Duraklat ⏸]  [Temizle ✕]              3 yeni olay ▲     │
│  ─────────────────────────────────────────────────────────  │
│  🔴 14:23:07  SLA violation for operator Turkcell          │
│     │ ● HIGH  source=operator  alert.triggered              │
│     │ → Turkcell  (clickable → /operators/20000000-…)       │
│     │ Latency 4.2s exceeded 3s threshold over 5 min         │
│     │ Details →                                             │
│  ─────────────────────────────────────────────────────────  │
│  🟢 14:23:05  Session updated                               │
│     │ ● INFO  source=aaa  session.updated                   │
│     │ → SIM 8935201234567890123  (clickable → /sims/…)      │
│     │ IMSI 286011234567890  IP 10.64.2.15  ↓2.1MB ↑48KB    │
│  ─────────────────────────────────────────────────────────  │
│  🟡 14:22:58  Policy violation                              │
│     │ ● MEDIUM  source=policy  policy_violation             │
│     │ → SIM 8935201234567890124  (clickable)                │
│     │ Exceeded monthly quota (1GB / 800MB)                  │
│     │ Details →                                             │
│  ─────────────────────────────────────────────────────────  │
│  [… 15m ago]                                                │
└─────────────────────────────────────────────────────────────┘
```

### Filter popover (click `Tür (4) ▾`)

```
┌────────────────────────────────────┐
│ Tür ara...                         │
│ ──────────────────────────────────  │
│ ☑ session.started          (234)   │
│ ☑ session.updated          (1.2k)  │
│ ☑ session.ended            (196)   │
│ ☑ sim.state_changed        (42)    │
│ ☐ operator_down            (3)     │
│ ☐ policy_violation         (18)    │
│ ☐ anomaly_sim_cloning      (1)     │
│ ☐ …                                │
│ ──────────────────────────────────  │
│ [Tümünü Seç]  [Temizle]            │
└────────────────────────────────────┘
```

Severity chip group is inline (no popover — 5 items fit):

```
  [● CRITICAL]  [● HIGH]  [● MEDIUM]  [○ LOW]  [○ INFO]
     off           on        on          off       off
```

---

## Design Tokens Used

| Token | Usage |
|-------|-------|
| `--bg-surface` `#0C0C14` | Drawer background |
| `--bg-elevated` `#12121C` | Filter popover background |
| `--bg-hover` `#1A1A28` | Row hover |
| `--bg-active` `#1E1E2E` | Active filter chip |
| `--border` `#1E1E30` | Filter bar bottom border |
| `--border-subtle` `#16162A` | Row separator |
| `--text-primary` `#E4E4ED` | Event title |
| `--text-secondary` `#7A7A95` | Event message, chip labels |
| `--text-tertiary` `#4A4A65` | Timestamp, muted counts |
| `--accent` `#00D4FF` | Entity button text, focus ring |
| `--accent-dim` `rgba(0,212,255,0.15)` | Active filter chip background |
| `--success` `#00FF88` | `●LIVE` pulse dot, severity=info/low (paired with `severityIconClass`) |
| `--warning` `#FFB800` | severity=medium |
| `--danger` `#FF4466` | severity=critical/high |
| `--purple` `#A855F7` | severity icon for certain anomaly types (via `severityIconClass`) |
| `--radius-sm` | Row rounded corners |
| `--shadow-glow` | Focus state on chips |
| `pulse-dot` animation | Already defined in global CSS |
| `animate-slide-up-in` | Already defined — new row entrance |

No hardcoded hex. All severity color choices go through `severityIconClass(severity)` (existing at `web/src/lib/severity.ts`).

---

## Prerequisites

1. **Install `@tanstack/react-virtual@^3`** — `cd web && npm i @tanstack/react-virtual@^3`. Verify it appears in `package.json` dependencies. (One-time, per D8.)
2. **Install shadcn `popover` primitive** — `cd web && npx shadcn@latest add popover`. Verified via `ls web/src/components/ui/` — `popover.tsx` is NOT present today, only `command.tsx`, `dropdown-menu.tsx`, etc. Filter bar popovers (T6) require it.
3. **Verify FIX-212 wiring shipped**: `web/src/types/events.ts` exports `BusEnvelope`; `GET /api/v1/events/catalog` returns HTTP 200 with `{data: {events: [...]}}` (spot-check with `curl -H "Authorization: Bearer <dev token>" http://localhost:8084/api/v1/events/catalog | jq '.data.events | length'` → expect 14+).
4. **Verify ws relay shape**: open browser devtools on dashboard, look at WS frames — frame should be `{id, type, data: {event_version: 1, ...envelope fields}}`. If data still shows legacy flat shape only, FIX-212 publisher migration is incomplete and this story blocks on it.
5. **Capture baseline**: open dashboard, trigger a session + an alert via seed/simulator, screenshot current drawer for regression comparison.

---

## Tasks

### T1 — Install `@tanstack/react-virtual` + verify build
- **Files**: `web/package.json`, `web/package-lock.json`
- **What**: `npm i @tanstack/react-virtual@^3` in `web/`. Verify `npm run build` passes.
- **Verify**: `grep '@tanstack/react-virtual' web/package.json` shows `^3.x`. `npm run build` exits 0.
- **Complexity**: XS · **Pattern**: n/a · **Context**: D8

### T2 — Extend `LiveEvent` type + `stores/events.ts` with envelope fields + pause/queue/filter state
- **Files**: `web/src/stores/events.ts`
- **What**:
  - Extend `LiveEvent` interface with: `title?: string`, `source?: string`, `entity?: { type: string; id: string; display_name?: string }`, `meta?: Record<string, unknown>`, `dedup_key?: string`, `event_version?: number`. Keep existing flat fields (`imsi`, `operator_id`, etc.) for one-release shim window.
  - Add `paused: boolean`, `queuedEvents: LiveEvent[]` to store state. Raise buffer cap from `slice(0, 100)` to `slice(0, 500)` (D7).
  - Add `filters: { types: string[], severities: Severity[], entityTypes: string[], sources: string[], dateRange: 'session'|'1h'|'24h' }` + `sessionStartTs: number` to state.
  - Add actions: `setPaused(p: boolean)`, `resumeAndFlush()`, `clear()`, `setFilters(f: Partial<Filters>)`.
  - Add selector: `useFilteredEvents()` (returns `events.filter(...)` applying all filter predicates + date range). Filter predicate logic is pure — unit-testable.
  - Add localStorage sync: on filter change, write `argus.events.filters.v1`; on store init, read + rehydrate.
  - When `paused=true`, `addEvent` pushes into `queuedEvents` (cap 500) instead of `events`. Histogram buckets still update (D6).
- **Verify**:
  - `npm run typecheck` passes.
  - Manual test: toggle `setPaused(true)` → new events append to `queuedEvents`, not visible list; `resumeAndFlush()` merges them.
  - Unit: add `stores/events.test.ts` with 4 cases: filter-by-type, filter-by-severity, pause-queue, resume-flush.
- **Complexity**: M · **Pattern**: zustand extension · **Context**: D3, D6, D7, D11, D12

### T3 — Patch `useGlobalEventListener` normalizer in `dashboard-layout.tsx` to read envelope shape
- **Files**: `web/src/components/layout/dashboard-layout.tsx`
- **What**:
  - Read envelope fields first: `title = pickString(d.title) || pickString(d.message) || msg.type.replace(/\./g, ' ')`, `message = pickString(d.message)` (separate from title), `source = pickString(d.source)`, `entity = pickObject(d.entity)`, `meta = pickObject(d.meta) || {}`, `dedup_key = pickString(d.dedup_key)`, `event_version = pickNumber(d.event_version)`.
  - **Flat-field merge (envelope wins, legacy second)** — always check BOTH sources, never branch on `event_version`:
    ```ts
    const opId = pickString(meta.operator_id) || pickString(d.operator_id)
    const apnId = pickString(meta.apn_id) || pickString(d.apn_id)
    const simId = (entity?.type === 'sim' ? entity.id : undefined)
                 || pickString(meta.sim_id) || pickString(d.sim_id)
    const iccid = (entity?.type === 'sim' ? entity.display_name : undefined)
                 || pickString(meta.iccid) || pickString(d.iccid)
    const framedIp = pickString(meta.framed_ip) || pickString(d.framed_ip)
    const bytesIn = pickNumber(meta.bytes_in) ?? pickNumber(d.bytes_in)
    const bytesOut = pickNumber(meta.bytes_out) ?? pickNumber(d.bytes_out)
    // …same pattern for msisdn, nas_ip, policy_id, job_id, progress_pct
    ```
    This guarantees `stores/events.ts:81` (`if (event.operator_id)` → per-operator histogram) still fires for envelope-shaped session events (where `operator_id` lives in `meta.*`), AND legacy flat-shaped events keep working. Envelope fields win because they're the canonical future shape.
  - In dev mode (`import.meta.env.DEV`), `console.debug('[events] legacy shape', msg.type)` when `event_version` is undefined — easy visibility into publisher migration gaps.
- **Verify**:
  - Trigger a `session.started` event via simulator — browser console shows new envelope fields populated.
  - Trigger a legacy-shaped test event (manual NATS publish with flat shape) — still renders, console.debug fires in dev.
  - `npm run typecheck` passes.
- **Complexity**: S · **Pattern**: envelope-aware normalization · **Context**: D3, FIX-212 D3 shim

### T4 — Add `formatRelativeTime` to `lib/format.ts`
- **Files**: `web/src/lib/format.ts`
- **What**: Append `export function formatRelativeTime(iso: string): string`. Returns `"Xs ago"`, `"Xm ago"`, `"Xh ago"`, `"Xd ago"`. Under 10s → `"just now"`. Over 7d → `formatAbsoluteDate`. Handle invalid ISO → `""` gracefully.
- **Verify**:
  - Unit test: `formatRelativeTime(new Date(Date.now()-5000).toISOString()) === 'just now'`; `...-65000 === '1m ago'`; `...-3700*1000 === '1h ago'`.
  - `npm run typecheck` passes.
- **Complexity**: XS · **Pattern**: pure utility · **Context**: D9

### T5 — `use-event-catalog.ts` TanStack Query hook
- **Files**: `web/src/hooks/use-event-catalog.ts` (NEW)
- **What**:
  - Export `useEventCatalog()` hook using `useQuery` from `@tanstack/react-query`.
  - Query key: `['events', 'catalog']`. `staleTime: 5 * 60 * 1000`. `queryFn` → `fetch('/api/v1/events/catalog')` (via existing `apiClient` from `web/src/lib/api.ts`).
  - Returns `{ catalog: EventCatalogEntry[] | undefined, isLoading, error }` and derived `{ types, entityTypes, sources }` as memoized string arrays.
  - Uses `EventCatalogEntry`, `EventCatalogResponse` from `web/src/types/events.ts` (NO redeclare).
- **Verify**:
  - Manual: mount drawer, DevTools Network tab shows ONE `GET /api/v1/events/catalog` call, returns 200 with 14+ events. Second drawer-open within 5min uses cached data (no network call).
  - `npm run typecheck` passes.
- **Complexity**: S · **Pattern**: React Query hook, `types/events.ts` consumer (PAT-015 gate) · **Context**: D10

### T6 — `event-filter-bar.tsx` sticky filter chips
- **Files**: `web/src/components/event-stream/event-filter-bar.tsx` (NEW)
- **What**:
  - Sticky `<div className="sticky top-0 z-10 bg-bg-surface/95 backdrop-blur-md border-b border-border">`.
  - Five chip groups, left-to-right: Type (popover multi-select), Severity (inline 5-way toggle), Entity Type (popover), Source (popover), Date Range (popover single-select).
  - Type/EntityType/Source chip sets derived from `useEventCatalog()`. Severity from hardcoded FIX-211 taxonomy `['critical','high','medium','low','info']`.
  - Each popover uses shadcn `Popover` + `Command` (search + virtualized checkbox list). Item text = catalog.type; secondary text = count (`filters.types.length === 0 ? totalTypeCount : filteredCount`).
  - Chip label shows selection summary: `Tür` (all), `Tür (4)` (4 selected), `Tür · session.started` (single).
  - Turkish labels per D14: `Tür`, `Önem`, `Varlık`, `Kaynak`, `Zaman`.
  - `[Tümünü Seç]` / `[Temizle]` buttons inside each popover footer.
- **Verify**:
  - Open drawer, filter bar renders 5 chip groups. Click `Tür` → popover shows catalog type list with counts.
  - Select 2 types → chip label becomes `Tür (2)`. Event list filters down. Unselect → chip label becomes `Tür`, full list returns.
  - Severity chips toggle inline (click to add/remove from filter).
  - Filters persist across drawer close/reopen (localStorage).
  - `npm run typecheck` passes.
- **Complexity**: L · **Pattern**: shadcn `Popover` + `Command` + zustand selector · **Context**: D10, D11, D14

### T7 — `event-entity-button.tsx` clickable entity
- **Files**: `web/src/components/event-stream/event-entity-button.tsx` (NEW)
- **What**:
  - Props: `entity: { type: string; id: string; display_name?: string }`, `onNavigate: () => void`.
  - Render: `<button className="inline-flex items-center gap-1 text-accent hover:text-accent-bright hover:underline ...">{entity.display_name || entity.id}</button>`.
  - Click handler: apply D13 route map → `navigate(route)`, call `onNavigate()` (to close drawer).
  - Unknown entity type → render `<span>` (not clickable), muted color.
  - Arrow icon (ChevronRight from lucide) after text, 10px.
- **Verify**:
  - Click operator entity → drawer closes, routes to `/operators/:id`. Verify in 4 route types: sim/operator/apn/policy.
  - Unknown entity type (e.g. `entity.type="foobar"`) → non-clickable span, no nav attempt.
  - `npm run typecheck` passes.
- **Complexity**: S · **Pattern**: route map + `useNavigate` · **Context**: D13

### T8 — `event-source-chips.tsx` extract + add bytes chip
- **Files**: `web/src/components/event-stream/event-source-chips.tsx` (NEW, move logic from `event-stream-drawer.tsx`)
- **What**:
  - Extract existing `SourceChips` component from `event-stream-drawer.tsx`.
  - Add: when `event.type.startsWith('session.') && (meta.bytes_in || meta.bytes_out)`, append two chips `↓${formatBytes(bytes_in)}` `↑${formatBytes(bytes_out)}` with `text-accent` highlight.
  - Read `bytes_in` / `bytes_out` from `event.meta` (envelope shape) first, falling back to top-level `event.bytes_in` / `event.bytes_out` (legacy shim — may not exist, fine).
  - Keep existing IMSI/IP/MSISDN/operator_id/apn_id/policy_id/job_id/progress_pct chips unchanged.
- **Verify**:
  - `session.updated` event with `meta.bytes_in: 2100000, meta.bytes_out: 48000` renders `↓2.1 MB ↑48 KB` (formatBytes already produces human-readable output).
  - `session.started` event with no bytes still renders normally (no empty chips).
  - Non-session event (e.g. `alert.triggered`) does not show byte chips even if bytes present in meta.
- **Complexity**: S · **Pattern**: extract + extend · **Context**: D4, F-12

### T9 — `event-row.tsx` full-fidelity row
- **Files**: `web/src/components/event-stream/event-row.tsx` (NEW, extracted from `event-stream-drawer.tsx`)
- **What**:
  - Props: `event: LiveEvent`, `onClose: () => void`.
  - Layout (vertical stack, single row in drawer):
    - Line 1: timestamp (absolute `HH:MM:SS`) + `•` + **title** (from `event.title || event.message || event.type`), severity badge on right.
    - Line 2: severity dot · source chip (`source=operator`) · type chip (`alert.triggered`) · `·` + `formatRelativeTime(event.timestamp)`.
    - Line 3: entity button (`<EventEntityButton />`) if `event.entity` present.
    - Line 4: `event.message` in `text-text-secondary` if present AND different from `event.title`.
    - Line 5: `<EventSourceChips />` (IMSI/IP/MSISDN/bytes).
    - Line 6: Details link shown **iff `event.meta?.alert_id` is a non-empty string**. Link goes to `/alerts/${meta.alert_id}`. This condition is future-proof: any publisher that populates `alert_id` gets the link automatically — no type-allowlist to maintain when new alert subjects land. Non-alert events (session, sim.state_changed) simply won't have `meta.alert_id` and won't show the link.
  - Severity icon color from `severityIconClass(event.severity)`.
  - Event icon from `eventIcon(event.type)` (keep existing mapping).
  - Background: `bg-bg-hover` on hover. Entrance animation `animate-slide-up-in` for index 0.
  - Click on row (NOT on entity button / details link — stop propagation there) does NOT navigate (row-level click is deprecated; only entity button and details link are navigational). This is a UX correctness fix versus the old drawer, which navigated on whole-row click — too easy to misfire.
- **Verify**:
  - Alert row (title="SLA violation for operator Turkcell", message="Latency 4.2s exceeded 3s threshold") renders title + message + `→ Turkcell` clickable + `Details →` link to `/alerts/:alert_id`.
  - Session.updated row renders bytes chip.
  - Entity button click navigates; row click does nothing.
  - `npm run typecheck` passes.
- **Complexity**: L · **Pattern**: composed row with atomic sub-components · **Context**: D5, D13, D15, F-09

### T10 — `event-stream-drawer.tsx` rebuilt as shell + virtualized list + pause/resume
- **Files**: `web/src/components/event-stream/event-stream-drawer.tsx`
- **What**:
  - Sheet shell unchanged (header + LIVE dot).
  - Header second row: `Showing N of M events · K filters active` (secondary text) + `[Duraklat ⏸]` / `[Devam Et ▶]` button + `[Temizle ✕]` button + `N yeni olay` badge (only when queue non-empty).
  - Mount `<EventFilterBar />` sticky below header.
  - Body: if `filteredEvents.length > 100`, use `useVirtualizer` from `@tanstack/react-virtual` (overscan=5, estimateSize=84px). Else render map directly.
  - Use selector `useFilteredEvents()` from store.
  - Single `setInterval(15000)` (only when drawer open) forces re-render via `setTick` to refresh `formatRelativeTime`. Clear on drawer-close.
  - Empty state unchanged (`Waiting for events...`) shown when filteredEvents empty AND events non-empty (filter-empty) vs events-empty (different copy: `Filtre eşleşmesi yok` vs `Olay bekleniyor...`).
- **Verify**:
  - Drawer opens → filter bar + 0–500 rows render.
  - Generate 200 events via simulator → virtualization kicks in (devtools shows only ~15 rendered DOM rows).
  - Pause button → icon flips, new events appear in queue badge counter.
  - Resume → queue flushes into list (animation, reverse-chron order). Badge disappears.
  - Clear button → events + queuedEvents + filters all reset; localStorage cleared.
  - Close+reopen drawer → filters persist from localStorage.
  - Relative timestamps refresh every 15s.
- **Complexity**: L · **Pattern**: virtualized list + zustand selector · **Context**: D6, D8, D9, D12

### T11 — Smoke tests (unit + browser walk-through)
- **Files**: `web/src/stores/events.test.ts` (NEW), `web/src/lib/format.test.ts` (append)
- **What**:
  - Unit: filter predicate (type/severity/entity-type/source/dateRange combinations — 6 cases).
  - Unit: pause + queue flush (3 cases: pause-empty, pause-add-resume, pause-overflow-500).
  - Unit: `formatRelativeTime` (5 cases).
  - Manual browser walkthrough recorded in `docs/stories/fix-ui-review/FIX-213-step-log.txt`: open drawer, trigger 5 event types, filter each way, pause/resume, clear, entity click navigates, details link navigates for alert row.
- **Verify**: `cd web && npm test -- --run` passes. Walkthrough captures 4 screenshots (baseline, filtered, paused, alert row expanded view).
- **Complexity**: M · **Pattern**: Vitest unit + manual browser · **Context**: all ACs

### T12 — Documentation + step log
- **Files**: `docs/stories/fix-ui-review/FIX-213-step-log.txt`, `docs/ROUTEMAP.md` (mark FIX-213 DONE at end)
- **What**: Append implementation summary to step log (files touched, decisions applied, AC mapping, deferred items per D4). Mark FIX-213 DONE row in ROUTEMAP with commit hash.
- **Verify**: `docs/ROUTEMAP.md` diff shows FIX-213 status transition.
- **Complexity**: XS · **Pattern**: standard story wrap-up · **Context**: all

---

## Acceptance Criteria Mapping

| AC | Addressed by | Verification |
|---|---|---|
| AC-1 — Filter chips (type/sev/entity/date) | T2 (filter state), T5 (catalog), T6 (filter bar UI) | Open drawer → click each of 5 chip groups, confirm values, confirm list filters |
| AC-2 — Event card: severity + type + clickable entity + title + message + abs+rel time | T3 (normalizer), T7 (entity), T9 (row layout), T4 (rel time) | Row visual walkthrough (alert + session + sim events) |
| AC-3 — Session bytes_in/out | T3 (meta pass-through), T8 (bytes chip with formatBytes) | Trigger session.updated with bytes — chip visible |
| AC-4 — Alert expanded (severity + source + description + details link) | T9 (row layout — details link gated by `meta.alert_id` presence), D5 (meta.alert_id routing) | Trigger operator_down → row shows title + message + Details link to `/alerts/:alert_id`. Trigger a session.updated (no alert_id) → no Details link, as expected. |
| AC-5 — Sticky filter header | T6 (`sticky top-0`) | Scroll event list, filter bar stays visible |
| AC-6 — Pause/resume + queue badge | T2 (store state), T10 (buttons + badge) | Pause, generate events, see "N yeni olay" badge, resume flushes |
| AC-7 — Clear resets list + filters | T2 (`clear()` action), T10 (button wiring) | Click clear — events + filters + localStorage all reset |
| AC-8 — 500 event buffer cap | T2 (slice cap raised) | Generate 600 events, confirm `events.length === 500` |
| AC-9 — Virtualization > 100 events | T10 (`useVirtualizer`) | Generate 200 events, devtools shows ~15 DOM rows |

Plus (not in story AC but from findings):
- **F-09** — T3 + T9 (title/message rendered instead of generic).
- **F-12** — T3 + T8 (bytes chip).
- **F-19** — T7 (clickable entity button, display_name).
- **F-20** — T5 + T6 (catalog-driven filter chips).

---

## Story-Specific Compliance Rules

1. **PAT-015 gate**: This story MUST have at least one runtime consumer of `web/src/types/events.ts`. T5 (`useEventCatalog`) imports `EventCatalogEntry` + `EventCatalogResponse`. T6 imports `EventSeverity`. T2 imports `BusEnvelope` (for the extended LiveEvent meta typing). Gate verified if `grep -r "from '@/types/events'" web/src` returns ≥3 files after this story.
2. **No re-declaration**: Do NOT add a parallel types file under `event-stream/`. All envelope types live in `web/src/types/events.ts`.
3. **Catalog as filter source of truth**: Type/EntityType/Source chip values are DERIVED from `useEventCatalog()`. NO hardcoded string arrays for these three filters. Severity is the only hardcoded filter (per D10).
4. **Turkish UI chrome, English identifiers**: chip labels = Turkish, event type values = English identifiers (`session.started`).
5. **No `dangerouslySetInnerHTML`**, no raw JSON rendering — all envelope text fields go through text nodes.
6. **Route map is complete**: every known `entity.type` has a destination (D13 table). Unknown types render non-clickable (graceful degrade, no broken nav).
7. **Back-compat shim**: legacy flat-shape events STILL render (F-12/F-09 findings fix is additive, not breaking). Shim removal is tracked as a follow-up story — do not remove here.
8. **`wsClient.subscribe('*', ...)`**: listener registration is unchanged. DO NOT add per-subject listeners — single global normalizer stays in dashboard-layout.
9. **Design tokens only**: no hardcoded hex in new files. All severity coloring via `severityIconClass(severity)`.

---

## Bug Pattern Warnings

- **Catalog-backed filters, NOT hardcoded** — if a dev hardcodes `['session.started', 'session.ended', ...]`, new subjects (future stories) won't appear in filter UI. Enforced via T6 implementation + CR.
- **Raw JSON in event body — FORBIDDEN**. Every envelope text surface is `title` / `message` / `entity.display_name` / `meta.<specific_key>`. Never `JSON.stringify(meta)` or `{...event}` spread-rendered.
- **Broken entity nav — `/alerts/:entity_id` is WRONG**. Alert rows entity is operator/sim/policy, NOT the alert row itself. Use `meta.alert_id` for the alerts-row link (D5).
- **Row-click navigation was a bug** — multiple findings (F-155, F-196, F-206) flag "Row actions inert" issue. Same anti-pattern in event row: the current drawer navigates on ANY row click, which misfires when user wants to hover/read. Switch to explicit entity-button click (D13, T9).
- **Pause leak** — forgetting to flush `queuedEvents` on resume produces silent event loss. Cap queue at 500; on overflow, log `console.warn('[events] queue overflow, dropping oldest')`.
- **Virtualization height drift** — rows have variable height (alert rows are taller due to description line). Use `estimateSize` + `measureElement` callback from `@tanstack/react-virtual`, NOT a hardcoded fixed height. Failing to do so produces overlapping rows.
- **Filter localStorage corruption** — if JSON.parse fails on rehydrate, catch + reset to default filters. Never crash the drawer on bad localStorage.
- **`setInterval` leak** — the 15s relative-time tick MUST be cleared on drawer close AND on unmount (React StrictMode will double-mount in dev, exposing this bug early).
- **Date range "session" ambiguity** — "current session" is WS session, not backend session. Defined as `sessionStartTs = Date.now()` captured once on first store init, NOT `Date.now()` on filter change.
- **Severity taxonomy drift** — FIX-211 locked 5-level. Do NOT accept `severity="warning"` or other legacy strings — normalizer (T3) must coerce unknown severity to `info` with a dev console.debug.
- **`meta.alert_id` missing on some alert publishers** — policy_violation/anomaly paths SHOULD populate it per FIX-209 but may lag. The Details-link gate is precisely `typeof meta.alert_id === 'string' && meta.alert_id.length > 0` (see T9) — if the field is missing, the link simply doesn't render, so there is no 404 risk. Entity-button navigation (T7) is the universal fallback for all rows.
- **Alert-type allowlist maintenance trap** — an earlier design enumerated `event.type.startsWith('alert.')`/`operator_down`/`policy_violation`/`anomaly_*`. Rejected: every new alert subject would need this allowlist updated. The `meta.alert_id` presence check (T9 line 6) is future-proof and one condition.

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| FIX-212 publisher migration incomplete — some events still flat-shaped | Med | Rows render with muted content (no title) | Normalizer (T3) shim preserves legacy rendering; dev-only warn surfaces gaps |
| `meta.alert_id` missing on some alert types | Low | Details link degrades to entity nav | D5 fallback documented; T9 branches explicitly |
| Virtualized row height drift | Med | Overlapping / clipped rows | `measureElement` callback; manual test at 500 events |
| Filter popover overflow on small viewport | Low | Popover cut off | shadcn `Popover` has `align="start" side="bottom"`; test at 1024px width |
| localStorage quota exceeded | Very Low | Filters don't persist | Try/catch + silent fallback; filter state size < 1KB |
| `useEventCatalog` 401 on unauth user | N/A | Drawer is inside `ProtectedRoute` | Covered by existing auth layer |
| `@tanstack/react-virtual` version clash with existing `@tanstack/react-query` | Low | Type conflict | Separate packages, independent versioning; npm install dry-run verify |
| Histogram sparkline breaks when paused | Low | Topbar sparkline goes flat | D6: histogram updates regardless of pause state |
| Drawer animation conflict with virtualization scroll-restore | Low | Scroll position jumps on reopen | Drawer is unmount-on-close in shadcn Sheet — list re-initializes at top, expected |
| Router fallback: `consumer`/`system` entity types → nav misfires | Very Low | User lands on wrong page | D13: explicit routes for these (→ `/ops/infra`, `/system/health`) |

---

## Summary

- **Scope**: Refactor Live Event Stream drawer into envelope-aware, filter-chipped, virtualized, pause/resume-capable UX. Fix F-09 (alert body), F-12 (bytes), F-19 (clickable entity), F-20 (filter chips).
- **Root-cause surface**: `useGlobalEventListener` normalizer in `dashboard-layout.tsx` (T3). Without the patch, downstream UI fixes render empty. This is the keystone task.
- **PAT-015**: satisfied by ≥3 runtime consumers of `web/src/types/events.ts` (T2, T5, T6).
- **New files** (7): `event-row.tsx`, `event-filter-bar.tsx`, `event-source-chips.tsx`, `event-entity-button.tsx`, `use-event-catalog.ts`, `events.test.ts`, step log entries.
- **Modified files** (4): `event-stream-drawer.tsx` (rebuilt), `stores/events.ts` (extended), `dashboard-layout.tsx` (normalizer patch), `lib/format.ts` (formatRelativeTime).
- **Prerequisites**: `npm i @tanstack/react-virtual@^3` (T1).
- **Deferred** (tracked in commit body, not implemented here): session_duration in `session.ended` meta (D4); shim removal for flat-shape events (follow-up story after FIX-213+214 land).
- **12 tasks**, complexity: 2× XS · 4× S · 2× M · 4× L. No XL. Total estimate: ~2 dev-days.
