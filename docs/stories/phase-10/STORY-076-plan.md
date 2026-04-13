# Implementation Plan: STORY-076 — Universal Search, Navigation & Clipboard

## Goal

Make Argus navigation frictionless: a single global `GET /api/v1/search` endpoint, an entity-aware command palette (Cmd+K), full keyboard-shortcut system (`?`, `/`, `g`+letter, list `j/k/Enter/x`, detail `e/Backspace`), auto-populated Recent/Favorites sidebar sections (capacity aligned to AC limits), per-entity row action menus with clipboard actions, and row hover quick-peek cards — all built on STORY-075's EntityLink/detail-page foundation with zero deferral.

## Architecture Context

### Components Involved

Backend (single new package + 2 router lines):
- `internal/api/search/handler.go` (NEW) — one handler type with `Search(w, r)` method. Runs parallel tenant-scoped ILIKE queries across sims/apns/policies/users + operator-grants-scoped query for operators. Returns grouped result DTO.
- `internal/api/search/handler_test.go` (NEW) — unit test covering tenant isolation, type filter, empty query, limit enforcement.
- `internal/gateway/router.go` — wire `r.Get("/api/v1/search", deps.SearchHandler.Search)` inside the authenticated group (same block as `/api/v1/sims`). Rate-limited like existing read endpoints.
- `cmd/argus/main.go` — construct `search.Handler` with existing stores (simStore, apnStore, operatorStore, policyStore, userStore) + tenantStore for operator-grant scoping, and inject into gateway deps.

Frontend (hooks, store updates, palette rewrite, row actions, quick-peek):
- `web/src/hooks/use-search.ts` (NEW) — `useSearch(query, opts?)` → react-query wrapped call to `/api/v1/search`, debounced in palette consumer (not inside the hook).
- `web/src/stores/ui.ts` (MODIFY) — bump `recentItems` cap 10→20, `favorites` cap 5→20; add `recentSearches: string[]` (cap 10) + `addRecentSearch(q)`; persist both.
- `web/src/components/command-palette/command-palette.tsx` (MODIFY) — when input has ≥2 chars, fetch via `useSearch` and render entity groups (SIM/APN/Operator/Policy/User) with `[Type]` badge + icon + secondary meta (ICCID tail, operator name, state). When input empty, render Recent Searches (if any) followed by static Pages/Settings/System groups (existing behaviour preserved). "View all N results for Q" footer link navigates to the appropriate filtered list page (`/sims?q=Q`, `/apns?q=Q`, etc). Keep cmdk library; keyboard nav already provided by cmdk.
- `web/src/hooks/use-keyboard-nav.ts` (MODIFY) — extend existing global handler: add `/` (focus palette input via opening palette + cmdk auto-focus), list-page `j/k` (next/prev row highlight, uses data attribute `data-row-index`), list-page `Enter` (open detail by reading data-href), list-page `x` (toggle row select via data-sim-id dispatch event), detail-page `e` (dispatches custom `argus:edit` event — detail pages listen), detail-page `Backspace` (navigate(-1) when not in input). Form shortcuts `Cmd+Enter` / `Cmd+S` are handled by individual forms (see hook doc).
- `web/src/components/ui/keyboard-shortcuts.tsx` (MODIFY) — extend SHORTCUT_GROUPS with the new entries (`/`, list `j/k/Enter/x`, detail `e/Backspace`, form `Cmd+Enter`/`Cmd+S`). Existing `?` toggle behaviour preserved.
- `web/src/components/shared/favorite-toggle.tsx` (NEW atom) — star button (☆/★) that reads `favorites` from `useUIStore` and toggles for a given `{ type, id, label, path }`. Used in detail page headers.
- `web/src/components/shared/row-actions-menu.tsx` (NEW molecule) — `<RowActionsMenu actions={[{label, icon?, onClick, destructive?}]} />` built on existing `DropdownMenu` primitives. Ellipsis (⋮) trigger, appears on row hover/focus. Keyboard: trigger receives focus; Enter/Space opens menu; arrow keys navigate items (leverage DropdownMenu's existing behavior).
- `web/src/components/shared/row-quick-peek.tsx` (NEW molecule) — wrapper for a table row that shows a floating card after 500ms hover with slots `{title, fields[]}`. Dismiss on mouse-leave or Esc. Uses `onMouseEnter`/`onMouseLeave` timer.
- Per-entity action list factories (colocated with list pages, no separate files needed):
  - `web/src/pages/sims/list.tsx` — add `<RowActionsMenu>` + `<RowQuickPeek>` integration per row.
  - `web/src/pages/apns/index.tsx`
  - `web/src/pages/operators/index.tsx`
  - `web/src/pages/policies/index.tsx`
  - `web/src/pages/audit/index.tsx`
  - `web/src/pages/sessions/index.tsx`
  - `web/src/pages/jobs/index.tsx`
  - `web/src/pages/alerts/index.tsx` (or anomalies list — check which is the primary route)
- `web/src/pages/sessions/detail.tsx`, `web/src/pages/settings/user-detail.tsx`, `web/src/pages/alerts/detail.tsx`, `web/src/pages/violations/detail.tsx`, `web/src/pages/system/tenant-detail.tsx` — add (a) `addRecentItem` on mount (same pattern as sim/apn/operator detail), (b) `<FavoriteToggle>` in header.
- `web/src/components/shared/index.ts` — export new components.

### Data Flow

#### Global search
```
User types "89012" in Cmd+K palette
  → Palette debounces input 200ms (useState inside palette)
  → useSearch("89012") → GET /api/v1/search?q=89012&types=sim,apn,operator,policy,user&limit=5
  → Backend concurrently runs (errgroup):
       sims:      SELECT id,iccid,imsi,msisdn,state,operator_id
                  FROM sims WHERE tenant_id=$tenant
                    AND (iccid ILIKE '%89012%' OR imsi ILIKE '%89012%' OR msisdn ILIKE '%89012%')
                  ORDER BY created_at DESC LIMIT 5
       apns:      SELECT id,name,state FROM apns
                  WHERE tenant_id=$tenant AND name ILIKE '%89012%' ORDER BY created_at DESC LIMIT 5
       operators: SELECT o.id,o.name,o.code,o.health_status FROM operators o
                  JOIN operator_grants g ON g.operator_id=o.id
                  WHERE g.tenant_id=$tenant
                    AND (o.name ILIKE '%89012%' OR o.code ILIKE '%89012%' OR o.mcc ILIKE '%89012%')
                  ORDER BY o.created_at DESC LIMIT 5
       policies:  SELECT id,name,state FROM policies
                  WHERE tenant_id=$tenant AND name ILIKE '%89012%' LIMIT 5
       users:     SELECT id,email,name,role FROM users
                  WHERE tenant_id=$tenant AND (email ILIKE '%89012%' OR name ILIKE '%89012%') LIMIT 5
     Each query resolves an operator_name lookup for sims (in-handler map).
  → Envelope: { status:"success", data:{ sims:[…], apns:[…], operators:[…], policies:[…], users:[…] }, meta:{ query, took_ms } }
  → FE maps each group to <Command.Group> with <Command.Item> per result; Enter navigates via EntityLink resolver (reuse ENTITY_ROUTE_MAP logic or router path map).
```

#### Shortcut dispatch
```
User presses `e` on sim detail page
  → useKeyboardNav handler verifies target is not INPUT/TEXTAREA/contentEditable
  → dispatches new CustomEvent('argus:edit') on document
  → Sim detail page useEffect listens for 'argus:edit' → opens its edit modal/drawer (or navigates to edit route)
  → Same pattern for 'argus:delete'-if-extended and 'argus:back' (Backspace)
```

#### Recent/Favorites persistence
```
Detail page mount
  → useEffect → useUIStore.addRecentItem({type, id, label, path})
  → Zustand persist middleware writes to localStorage key 'argus-ui'
  → Sidebar reads favorites/recentItems from same store, slices to 5 for display.
```

### API Specifications

#### GET /api/v1/search

Query string:
- `q` (required, string, min length 1, max 128) — search fragment (case-insensitive substring).
- `types` (optional, comma-separated) — default `sim,apn,operator,policy,user`. Invalid types ignored.
- `limit` (optional, int) — per-type max results, default 5, cap 20.

Auth: Bearer JWT (standard middleware chain). Tenant derived from JWT claims.

Rate limit: reuses `middleware.RateLimit` bucket for authenticated read endpoints (same as `/api/v1/sims`). No new bucket required.

Success (200):
```json
{
  "status": "success",
  "data": {
    "sims": [
      { "id":"uuid","iccid":"8901...","imsi":"2341...","msisdn":"+90...","state":"active","operator_id":"uuid","operator_name":"Turkcell" }
    ],
    "apns": [ { "id":"uuid","name":"iot-m2m.argus","state":"active" } ],
    "operators": [ { "id":"uuid","name":"Turkcell","code":"TR-TCELL","mcc":"286","health_status":"healthy" } ],
    "policies": [ { "id":"uuid","name":"Default M2M","state":"published" } ],
    "users": [ { "id":"uuid","email":"ops@acme.com","name":"Ops User","role":"tenant_admin" } ]
  },
  "meta": { "query": "89012", "took_ms": 23 }
}
```

Error responses:
- 400 `{ status:"error", error:{ code:"VALIDATION_ERROR", message:"q is required" } }` when q missing / too long.
- 401 standard unauthenticated envelope from auth middleware.
- 429 standard rate-limit envelope.
- 500 on DB error — envelope `INTERNAL_ERROR` (no query details leaked).

Performance target: P50 < 50ms with existing indexes (iccid/imsi/msisdn indexes exist per `20260412000008_composite_indexes.up.sql`; operator name/code unique indexes; apn name index exists via unique(tenant_id,name)). Handler uses `errgroup.Group` with `context.WithTimeout(ctx, 500*time.Millisecond)` to bound latency.

### Database Schema

**No migrations.** Story uses existing columns only:
- `sims(iccid, imsi, msisdn, state, operator_id, tenant_id)` — source `migrations/*_sims.up.sql` verified earlier.
- `apns(name, state, tenant_id)` — source existing `apns` table.
- `operators(name, code, mcc, mnc, health_status, state)` — global table, tenant-scoped via `operator_grants(operator_id, tenant_id)`.
- `policies(name, state, tenant_id)`.
- `users(email, name, role, tenant_id, state)`.

Index audit (reuse existing):
- `sims`: composite and per-column indexes on `iccid`, `imsi`, `msisdn` already exist (from `20260412000008_composite_indexes.up.sql`).
- `apns`: unique(tenant_id, name) — ILIKE on name uses seq scan on small tables; acceptable (tenants have <500 APNs typically).
- `operators`: unique indexes on `code`, `(mcc,mnc)`, `name`. `operator_grants` has index on `tenant_id`.
- `policies`: `(tenant_id, name)` — exists.
- `users`: unique(tenant_id, email), index on name if present; otherwise seq scan acceptable (users table small).

ILIKE on indexed text columns will NOT use the btree index for `%pat%` wildcards — but the tenant filter limits scan scope, and target is 50ms across ~10M rows only for `sims`. Sims index plan: the existing `iccid_trgm` / `imsi_trgm` GIN indexes (check migration history) OR leading-anchored fallback. Developer should add a note if GIN trigram indexes are missing and add them in a follow-up migration — NOT in this story (per "no migrations" scope). For Phase 10 zero-deferral: if trigram missing, embed a 500ms timeout + operator-name enrichment via in-memory map; do not modify schema.

### Screen Mockups

#### Command Palette (Cmd+K) — entity search mode
```
┌────────────────────────────────────────────────────────────────┐
│ [search icon]  89012                                     Esc × │
├────────────────────────────────────────────────────────────────┤
│ SIMS                                                           │
│   [SIM]  8901234567890123456 — Active — Turkcell               │
│   [SIM]  8901234567890987654 — Suspended — Vodafone            │
│   → View all 47 SIM results                                    │
│ OPERATORS                                                      │
│   [OP]   Turkcell (TR-TCELL) — healthy                         │
│ POLICIES                                                       │
│   [POL]  M2M Default — published                               │
│ USERS                                                          │
│   [USR]  ops@acme.com — Ops User — tenant_admin                │
├────────────────────────────────────────────────────────────────┤
│ ↑↓ navigate   ↵ open   Esc close                               │
└────────────────────────────────────────────────────────────────┘
```

#### Command Palette — empty query (default + recent)
```
┌────────────────────────────────────────────────────────────────┐
│ [search icon]  Search pages, SIMs, commands…                   │
├────────────────────────────────────────────────────────────────┤
│ RECENT SEARCHES                                                │
│   clock  89012                                                 │
│   clock  iot-m2m                                               │
│ PAGES                                                          │
│   dashboard / SIM Cards / APNs / Operators / …                 │
│ SETTINGS                                                       │
│   Users & Roles / API Keys / IP Pools / …                      │
└────────────────────────────────────────────────────────────────┘
```

#### Row Actions Menu (on SIM list row hover)
```
SIM list row:
  8901… │ Turkcell │ active │ …                    [⋮]
                                                    │
                                                    ├─ View Detail
                                                    ├─ Copy ICCID
                                                    ├─ Copy IMSI
                                                    ├─ Suspend
                                                    ├─ Activate
                                                    ├─ Assign Policy
                                                    ├─ Run Diagnostics
                                                    └─ View Audit
```

#### Row Quick-Peek (hover 500ms)
```
     ┌──────────────────────────────────┐
     │ SIM 8901234567890123456          │
     │ IMSI 234110000001234             │
     │ State: active   Op: Turkcell    │
     │ Last session: 12 min ago         │
     │ [View detail →]                  │
     └──────────────────────────────────┘
```

#### Detail page header — Favorite toggle
```
┌─ SIM Detail ────────────────────────────────────────────────┐
│ 8901234567890123456   [☆ Favorite]   [Edit] [Actions ▾]     │
├────────────────────────────────────────────────────────────┤
│ Tabs: Overview | Sessions | Audit | Notifications | …       │
```

### Design Token Map (UI — MANDATORY)

#### Color Tokens (from FRONTEND.md)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page / primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-white`, `text-gray-100` |
| Secondary text, labels | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-400` |
| Muted / placeholder | `text-text-tertiary` | `text-[#4A4A65]`, `text-gray-500` |
| Accent link / highlighted row | `text-accent` / `bg-accent-dim` | `text-[#00D4FF]`, `bg-cyan-500/15` |
| Success state | `text-success` / `bg-success-dim` | `text-green-400` |
| Warning state | `text-warning` / `bg-warning-dim` | `text-amber-400` |
| Danger / destructive action | `text-danger` / `bg-danger-dim` | `text-red-500` |
| Surface card bg | `bg-bg-surface` | `bg-white`, `bg-slate-900` |
| Elevated panel bg | `bg-bg-elevated` | hardcoded hex |
| Hover row bg | `bg-bg-hover` | `hover:bg-gray-800` |
| Border | `border-border` | `border-[#1E1E30]`, `border-gray-800` |
| Subtle border | `border-border-subtle` | hardcoded hex |

#### Typography Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Palette group heading | `text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary` | any custom hex |
| Palette item label | `text-sm text-text-secondary` | `text-[14px]` |
| Palette meta (secondary) | `text-[11px] text-text-tertiary font-mono` | arbitrary |
| Row-peek title | `text-[13px] font-semibold text-text-primary` | — |
| Row-peek field | `text-[11px] text-text-secondary` | — |
| Data (ICCID/IMSI) | `font-mono text-xs` | non-mono |

#### Spacing / Elevation Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Dropdown shadow | `shadow-2xl` (existing palette style) | custom `box-shadow` |
| Radius sm | `rounded-[var(--radius-sm)]` | `rounded-md` |
| Radius md | `rounded-[var(--radius-md)]` | `rounded-lg` |
| Palette backdrop | `bg-black/60 backdrop-blur-sm` | — |

#### Existing Components to REUSE (DO NOT recreate)
| Component | Path | Use For |
|-----------|------|---------|
| `DropdownMenu`, `DropdownMenuTrigger`, `DropdownMenuContent`, `DropdownMenuItem`, `DropdownMenuSeparator` | `web/src/components/ui/dropdown-menu.tsx` | Row actions menu — NEVER raw `<div>` dropdowns |
| `Tooltip` | `web/src/components/ui/tooltip.tsx` | Shortcut tooltips on buttons |
| `Button` | `web/src/components/ui/button.tsx` | All buttons |
| `Input` | `web/src/components/ui/input.tsx` | Form fields (palette uses cmdk internal input which is acceptable) |
| `EntityLink` | `web/src/components/shared/entity-link.tsx` | ALL cross-entity links in palette results |
| `CopyableID` | `web/src/components/shared/copyable-id.tsx` | Already exists; reuse for clipboard icons if needed |
| `KeyboardShortcuts` | `web/src/components/ui/keyboard-shortcuts.tsx` | Help modal — extend, do NOT rewrite |
| `Command*` (from cmdk) | `web/src/components/command-palette/command-palette.tsx` | Keep cmdk primitives |
| `useUIStore` | `web/src/stores/ui.ts` | Recent/Favorites/recentSearches state |
| lucide icons (`Star`, `Clock`, `Search`, `MoreHorizontal`, per-entity icons matching sidebar) | `lucide-react` | All icons — NEVER inline SVG |

## Prerequisites
- [x] STORY-075 completed — EntityLink / CopyableID / shared components, session/user/alert/violation/tenant detail pages all exist and are routed. Verified in `web/src/components/shared/` and `web/src/pages/`.
- [x] cmdk library already installed and in use.
- [x] DropdownMenu, Tooltip primitives already exist.

## Task Decomposition Rules

Each task is dispatched to a FRESH Developer subagent with isolated context. Amil orchestrator extracts only the `Context refs` sections and passes them directly.

Granularity: each task 1–3 files, functionally grouped (DB-free story → logic-first ordering).

## Tasks

### Task 1: Backend Search Endpoint
- **Files:** Create `internal/api/search/handler.go`, `internal/api/search/handler_test.go`; Modify `internal/gateway/router.go` (1 route line), `cmd/argus/main.go` (handler construction + deps wiring).
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/api/apn/handler.go` for handler struct + `Get`/`List` shape, `internal/api/sim/handler.go` `List` method for tenant-scoped query pattern, `internal/apierr/` for error helpers, `internal/gateway/router.go` lines 391–420 for how sim routes are wired.
- **Context refs:** `Architecture Context > Components Involved` (backend section), `Data Flow > Global search`, `API Specifications > GET /api/v1/search`, `Database Schema`, `Story-Specific Compliance Rules`, `Bug Pattern Warnings`.
- **What:**
  - Create `Handler` struct with fields `simStore *store.SIMStore`, `apnStore *store.APNStore`, `operatorStore *store.OperatorStore`, `policyStore *store.PolicyStore`, `userStore *store.UserStore`, `db *pgxpool.Pool`, `logger *zap.Logger`.
  - `Search(w http.ResponseWriter, r *http.Request)` method:
    1. Read tenant ID via `middleware.TenantIDFromContext` (same helper used by existing handlers).
    2. Parse `q` (required, 1..128 chars), `types` (CSV, default all five), `limit` (default 5, clamp 1..20).
    3. Run queries concurrently with `golang.org/x/sync/errgroup` + `context.WithTimeout(r.Context(), 500*time.Millisecond)`.
    4. Per-type SQL: sims/apns/policies/users filter by `tenant_id = $1`; operators JOIN `operator_grants` on `tenant_id = $1` (AC-1 tenant scoping — **see gotcha in Compliance Rules**).
    5. Return grouped DTO with standard envelope via existing `apierr.WriteSuccess` / `apierr.WriteError` helpers (follow sim handler style).
    6. Enrich sims with `operator_name` in handler memory (small in-result loop: fetch distinct operator_ids then `operatorStore.GetByID` OR single IN query — whichever is simpler in the pattern of existing sim list enrichment).
  - Test: table-driven with mock stores — cover q validation, type filter, tenant isolation (sim from other tenant not returned), limit cap, empty query rejection, happy path.
  - Wire `r.Get("/api/v1/search", deps.SearchHandler.Search)` in router.go authenticated block next to sims routes.
  - main.go: add `searchHandler := search.NewHandler(simStore, apnStore, operatorStore, policyStore, userStore, db, logger)` and `deps.SearchHandler = searchHandler`.
- **Verify:**
  - `make test` on `internal/api/search/...` passes.
  - `go build ./...` succeeds.
  - `curl -s -H "Authorization: Bearer $TOKEN" "http://localhost:8080/api/v1/search?q=89" | jq .status` → `"success"`.
  - P50 latency <50ms in log output on seeded DB.

### Task 2: UI Store Capacity + Recent Searches
- **Files:** Modify `web/src/stores/ui.ts`.
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Existing `web/src/stores/ui.ts` — follow same Zustand + persist middleware structure.
- **Context refs:** `Architecture Context > Components Involved`, `Data Flow > Recent/Favorites persistence`.
- **What:**
  - Bump `recentItems` slice cap from 10 to **20** (AC-4).
  - Bump `favorites` slice cap from 5 to **20** (AC-5).
  - Add state `recentSearches: string[]` (max 10), action `addRecentSearch(q: string)` that dedupes + prepends + slices to 10.
  - Extend `partialize` to persist `recentSearches` too.
- **Verify:**
  - `bun run typecheck` passes.
  - Sidebar still renders Recent/Favorites sections (max 5 shown as UI slice).

### Task 3: use-search Hook
- **Files:** Create `web/src/hooks/use-search.ts`.
- **Depends on:** Task 1 (endpoint must exist for runtime check), Task 2 (optional — hook does not read store).
- **Complexity:** medium
- **Pattern ref:** Read `web/src/hooks/use-sims.ts` and `web/src/components/ui/sim-search.tsx` (lines 23–32 for the react-query pattern with enabled gate).
- **Context refs:** `API Specifications > GET /api/v1/search`, `Architecture Context > Data Flow > Global search`.
- **What:**
  - Export `useSearch(query: string, opts?: { types?: EntityType[]; limit?: number; enabled?: boolean })`.
  - Internally wraps `useQuery` with queryKey `['search', query, types, limit]`, `enabled: (query?.length ?? 0) >= 2 && opts?.enabled !== false`, `staleTime: 10_000`.
  - Returns typed response `{ sims, apns, operators, policies, users, meta } | null`.
  - Calls `api.get<SearchEnvelope>(\`/search?q=...&types=...&limit=...\`)` using existing `@/lib/api`.
- **Verify:** `bun run typecheck` passes; hook usable from palette.

### Task 4: Command Palette Entity Search Rewrite
- **Files:** Modify `web/src/components/command-palette/command-palette.tsx`.
- **Depends on:** Task 2, Task 3.
- **Complexity:** high
- **Pattern ref:** Current file `web/src/components/command-palette/command-palette.tsx` — preserve the cmdk scaffolding and keyboard plumbing; only swap out the body/results. Reference `web/src/components/ui/sim-search.tsx` for debounce + result rendering.
- **Context refs:** `Architecture Context > Components Involved` (palette line), `Screen Mockups > Command Palette entity search mode` + `empty query`, `Data Flow > Global search`, `Design Token Map`, `Story-Specific Compliance Rules`.
- **What:**
  - Add `query` local state, `debouncedQuery` via 200ms setTimeout (not inside palette re-renders).
  - Call `useSearch(debouncedQuery)`. When `query.length >= 2`, render result groups (SIMS / APNS / OPERATORS / POLICIES / USERS) using `Command.Group` + `Command.Item`. Each item's `onSelect` navigates using EntityLink route map (import via `ENTITY_ROUTE_MAP` or a shared helper — if the map is not exported, export it from `entity-link.tsx`).
  - Labels: SIM `[SIM] <iccid> — <state> — <operator_name>`; APN `[APN] <name>`; OP `[OP] <name> (<code>)`; POL `[POL] <name> — <state>`; USR `[USR] <email>`.
  - When `query.length < 2`, render (a) `RECENT SEARCHES` (from store) with click-to-reuse, then (b) existing PAGES/SETTINGS/SYSTEM/SRE static groups (preserve today's UX).
  - On `Enter` against a recent-search item: set `query` to that string, do not close.
  - On `Enter` against an entity: navigate + close + `addRecentItem` (reuse EntityLink-style label) + `addRecentSearch(query)`.
  - Empty state when API returns nothing: "No results for 'X'".
  - "View all N results" link per non-empty group → navigate to `/sims?q=X` etc. (Check each list page supports `?q=` — sim list already does; for others, wire basic `?q=` acceptance in Task 7 if missing.)
  - Tokens ONLY from Design Token Map. NO hardcoded hex.
- **Verify:**
  - `grep -n '#[0-9a-fA-F]\{3,\}' web/src/components/command-palette/command-palette.tsx` → zero hits.
  - Manual: Cmd+K, type "89", see SIM results; Enter navigates.
  - Empty palette still shows Pages/Settings groups.
- **Note:** Invoke `frontend-design` skill before writing final JSX.

### Task 5: Keyboard Shortcuts System Extensions
- **Files:** Modify `web/src/hooks/use-keyboard-nav.ts`; Modify `web/src/components/ui/keyboard-shortcuts.tsx`.
- **Depends on:** Task 4 (palette open action).
- **Complexity:** medium
- **Pattern ref:** Current `web/src/hooks/use-keyboard-nav.ts` (existing `g`+letter chord, `[`/`]` handlers).
- **Context refs:** `Architecture Context > Components Involved`, `Data Flow > Shortcut dispatch`, `Bug Pattern Warnings`.
- **What:**
  - In `use-keyboard-nav.ts`: add handlers for:
    - `/` (no modifier, not in input) → open command palette (use `useUIStore.setCommandPaletteOpen(true)`); when palette is open, cmdk auto-focuses its input.
    - On pages matching list pattern (detect via `location.pathname` being one of `/sims`, `/apns`, `/operators`, `/policies`, `/sessions`, `/jobs`, `/alerts`, `/audit`): `j` / `k` → dispatch `CustomEvent('argus:row-nav', { detail: { dir: 'next'|'prev' } })`; `Enter` → `argus:row-open`; `x` → `argus:row-select-toggle`. List pages listen (Task 6 integration).
    - On pages matching detail pattern (ends with UUID segment): `e` → `argus:edit`; `Backspace` (not in input) → `navigate(-1)`.
    - `Cmd+Enter` / `Ctrl+Enter` on forms → `argus:form-submit` (global event — forms opt-in by listening).
    - `Cmd+S` / `Ctrl+S` on forms → `argus:form-save` (preventDefault).
  - Guard ALL handlers with the existing INPUT/TEXTAREA/contentEditable check (copy the check).
  - In `keyboard-shortcuts.tsx`: extend SHORTCUT_GROUPS — add `/`, `j`, `k`, `Enter`, `x`, `e`, `Backspace`, `Cmd+Enter`, `Cmd+S`. Sort into NAVIGATION / TABLES / DETAIL / FORMS / ACTIONS groups.
- **Verify:**
  - Press `?` → modal shows new shortcuts.
  - Press `/` outside inputs → palette opens.
  - On `/sims`, press `j` 3 times → third row visually highlights (Task 6 wires receiver).
  - Inside a `<input>`, `/` does nothing.
  - `bun run typecheck` passes.

### Task 6: Row Actions Menu + List Page Wiring
- **Files:** Create `web/src/components/shared/row-actions-menu.tsx`; Modify `web/src/components/shared/index.ts`; Modify `web/src/pages/sims/list.tsx`, `web/src/pages/apns/index.tsx`, `web/src/pages/operators/index.tsx`, `web/src/pages/policies/index.tsx`, `web/src/pages/sessions/index.tsx`, `web/src/pages/jobs/index.tsx`, `web/src/pages/alerts/index.tsx`, `web/src/pages/audit/index.tsx` (wire actions per entity + j/k receiver for highlight/Enter/x).
- **Depends on:** Task 5 (events), Task 2 (favorites — star actions optional).
- **Complexity:** high
- **Pattern ref:** `web/src/components/ui/dropdown-menu.tsx` for primitives; existing list pages for table structure and row rendering.
- **Context refs:** `Architecture Context > Components Involved`, `Screen Mockups > Row Actions Menu`, `Design Token Map > Existing Components to REUSE`, `Bug Pattern Warnings`.
- **What:**
  - `RowActionsMenu` props: `actions: Array<{ label: string; icon?: LucideIcon; onClick: () => void | Promise<void>; destructive?: boolean; separatorAbove?: boolean }>`. Trigger = `<MoreHorizontal>` icon button. Use existing `DropdownMenu*` primitives. Keyboard: focus trigger on row focus; `Enter`/`Space` opens (DropdownMenu handles this).
  - Visible on row hover (group-hover classes on `<tr>`) OR when trigger has focus (so keyboard users see it).
  - Per-entity action sets (AC-6 full list):
    - **SIM:** View Detail → `/sims/:id`; Copy ICCID/IMSI → `navigator.clipboard.writeText` + toast; Suspend → POST `/sims/:id/suspend` via existing `useSims.suspend`; Activate → existing hook; Assign Policy → open existing assign-policy modal (check existing sim list); Run Diagnostics → POST `/sims/:id/diagnose`; View Audit → navigate `/audit?entity_id=:id&entity_type=sim`.
    - **APN:** View Detail; Copy ID; Archive (calls existing hook if present, else navigate to detail Edit); View Connected SIMs → `/sims?apn_id=:id`.
    - **Operator:** View Detail; Copy Code; Test Connection → POST `/operators/:id/test`; View Health History → `/operators/:id` tab=health.
    - **Policy:** View Detail; Clone (existing hook); Activate Version (existing); View Assigned SIMs → `/sims?policy_version_id=:id`.
    - **Audit:** View Entity → EntityLink route; Copy Entry ID; Filter by Entity → `/audit?entity_id=...`; Filter by User → `/audit?user_id=...`.
    - **Session:** View Detail; Force Disconnect (POST existing); View SIM; Copy Session ID.
    - **Job:** View Detail; Retry (existing); Cancel (existing); Download Error Report (existing endpoint).
    - **Alert:** View Detail; Acknowledge (existing); Resolve (existing); Copy Alert ID.
  - Toast via existing `sonner` or equivalent used elsewhere (check `web/src/lib/toast.ts` or similar).
  - j/k receiver: each list page adds a `useEffect` listener for `argus:row-nav`/`argus:row-open`/`argus:row-select-toggle`; maintains local `highlightedIndex`; rows get `data-row-index` + `aria-selected`. Enter navigates via row's id→detail URL. `x` toggles existing row selection state.
- **Verify:**
  - `grep -n '#[0-9a-fA-F]\{3,\}' web/src/components/shared/row-actions-menu.tsx` → zero hits.
  - `bun run typecheck` passes.
  - Manual: on `/sims`, hover row → ⋮ appears → click "Copy ICCID" → clipboard contains the ICCID (browser test).
- **Note:** Invoke `frontend-design` skill.

### Task 7: Row Quick-Peek + Favorite Toggle + New-Detail-Page Integration + Filtered-List `?q=` Support
- **Files:** Create `web/src/components/shared/row-quick-peek.tsx`, `web/src/components/shared/favorite-toggle.tsx`; Modify `web/src/components/shared/index.ts`; Modify `web/src/pages/sessions/detail.tsx`, `web/src/pages/settings/user-detail.tsx`, `web/src/pages/alerts/detail.tsx`, `web/src/pages/violations/detail.tsx`, `web/src/pages/system/tenant-detail.tsx` (addRecentItem + FavoriteToggle in header); Modify `web/src/pages/sims/detail.tsx`, `web/src/pages/apns/detail.tsx`, `web/src/pages/operators/detail.tsx`, `web/src/pages/policies/editor.tsx` (FavoriteToggle in header — they already have addRecentItem); Modify list pages that do not yet support `?q=` (apns/operators/policies/users) to accept `q` query param and filter client-side or via existing list endpoint if supported.
- **Depends on:** Task 2 (store caps), Task 6 (row actions layout on list pages).
- **Complexity:** medium
- **Pattern ref:** `web/src/components/ui/tooltip.tsx` for hover delay pattern (or implement own 500ms setTimeout); `web/src/pages/sims/detail.tsx` lines 646–652 for addRecentItem pattern.
- **Context refs:** `Architecture Context > Components Involved`, `Screen Mockups > Row Quick-Peek` + `Detail page header`, `Design Token Map`.
- **What:**
  - `RowQuickPeek`: wrapper around `<tr>` children providing 500ms hover timer → floating card (absolute-positioned or portal). Dismiss on `mouseleave` or `Esc`. Card slots: `title`, `fields: { label, value }[]`, optional `footerHref` → "View detail →".
  - `FavoriteToggle`: button with star icon (`Star` outline ↔ filled). Reads `favorites` from store; onClick calls `toggleFavorite({ type, id, label, path })`. Hide when store at cap and not already favorited — show warning tooltip "20 favorites max — unstar one first".
  - Integrate `RowQuickPeek` into 3 high-value list pages minimum: sims, operators, policies (AC-7 E2E mentions operator hover). Others optional in-scope.
  - In the 5 new-detail pages + 4 existing detail pages: add `<FavoriteToggle>` in header next to title / Edit button. Reuse label that addRecentItem uses.
  - In 5 new-detail pages (session, user, alert, violation, tenant): add `addRecentItem` on mount (same pattern as existing sim/apn/operator detail). Labels: `session` → `Session <id-prefix>`, `user` → `User <email>`, `alert` → `Alert <title>`, `violation` → `Violation <id-prefix>`, `tenant` → `Tenant <name>`.
  - For filtered-list `?q=` on apns/operators/policies/users lists: wire a `q` query-param reader + client-side `String.includes` filter (already-fetched data). This is minimal and sufficient for AC-2 "View all" links. No backend change.
- **Verify:**
  - `grep -n '#[0-9a-fA-F]\{3,\}' web/src/components/shared/{row-quick-peek,favorite-toggle}.tsx` → zero hits.
  - `bun run typecheck` passes.
  - Manual: hover operator row 600ms → peek card appears; click star on sim detail → sidebar Favorites shows it; refresh → still present.
- **Note:** Invoke `frontend-design` skill.

### Task 8: E2E Smoke + Lint + Build + Final Verification
- **Files:** Verification only; minor touch-ups if issues found.
- **Depends on:** Tasks 1–7.
- **Complexity:** low
- **Pattern ref:** None.
- **Context refs:** `Acceptance Criteria Mapping`, `Test Scenarios` (from story).
- **What:**
  - `go vet ./...` and `go build ./...` pass.
  - `make test` passes (at minimum the new search package tests).
  - `cd web && bun run typecheck && bun run lint && bun run build` all pass.
  - `grep -rn '#[0-9a-fA-F]\{3,\}' web/src/components/command-palette web/src/components/shared/row-actions-menu.tsx web/src/components/shared/row-quick-peek.tsx web/src/components/shared/favorite-toggle.tsx` → zero matches.
  - Smoke-test the 6 E2E scenarios from the story manually (Cmd+K search → SIM result; `?` shortcut modal; list `j` highlight; recent/favorite persistence; row action Copy ICCID; operator row hover peek).
- **Verify:** All commands green; all 6 scenarios pass.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 Backend search endpoint | Task 1 | Task 1 tests, Task 8 smoke |
| AC-2 Command palette entity search | Task 4 (consumes Task 3 hook, Task 1 endpoint) | Task 8 E2E #1 |
| AC-3 Keyboard shortcuts system | Task 5 | Task 8 E2E #2, #3 |
| AC-4 Recent items | Task 2 (store cap), Task 7 (new detail pages) | Task 8 E2E #4 |
| AC-5 Favorites | Task 2 (store cap), Task 7 (FavoriteToggle + headers) | Task 8 E2E #4 |
| AC-6 Contextual row action menus | Task 6 | Task 8 E2E #5 |
| AC-7 Row quick-peek | Task 7 | Task 8 E2E #6 |

## Story-Specific Compliance Rules

- **API: Standard envelope** — search response uses `{ status, data, meta }` envelope (per ARCHITECTURE conventions, CLAUDE.md rule).
- **Tenant scoping (CRITICAL)** — sims/apns/policies/users queries include `tenant_id = $1`. Operators query JOINs `operator_grants ON tenant_id = $1` (operators table is global — see Gotcha #1). Any query missing tenant filter = security bug.
- **Rate limiting** — search endpoint reuses existing read-endpoint rate-limit bucket; do NOT bypass.
- **Cursor pagination convention** — N/A (top-N results, not paginated).
- **UI: Design tokens only** — no hardcoded hex colors (`#...`) in any new/modified frontend file. Use classes from Design Token Map.
- **UI: Reuse primitives** — DropdownMenu, Tooltip, Button, EntityLink from existing paths; NEVER raw HTML menus.
- **UI: Turkish text** — N/A (English-only per CLAUDE.md).
- **Accessibility** — dropdown trigger focusable, aria-label, Enter/Space opens menu, Esc closes. Row-peek dismissible via Esc.
- **ADR-002 (JWT auth)** — search endpoint behind JWT middleware.
- **Audit logging** — search reads are NOT audited (read endpoints per existing convention); row actions that mutate (Suspend, Activate, Force Disconnect, etc.) reuse existing handlers which already audit.

## Bug Pattern Warnings

- **Key-conflict with input focus** — all new global shortcuts MUST check `target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable` before acting. Copy the exact check from existing `use-keyboard-nav.ts`. Missing this guard breaks every form on the page.
- **cmdk list behavior** — when input has text AND we render dynamic API results, clear the static commands so cmdk's built-in filter doesn't also filter static items. Render one set or the other based on `query.length >= 2`.
- **Operator tenant-scoping** — `OperatorStore.List` has no tenant param because operators are global. Search handler MUST add `JOIN operator_grants g ON g.operator_id = o.id WHERE g.tenant_id = $1`. A naive `SELECT FROM operators WHERE ...` leaks other tenants' operator names.
- **Clipboard API in non-HTTPS** — `navigator.clipboard.writeText` requires secure context. Fallback to `document.execCommand('copy')` with hidden textarea, OR show a toast if clipboard unavailable. Most browsers allow clipboard on localhost.
- **Recent/Favorites cap mismatch** — AC-4 says 20 recent, AC-5 says 20 favorites; store currently caps at 10 / 5. Task 2 MUST fix. Sidebar slice(0,5) display stays; store capacity changes.
- **Backspace navigation hazard** — `Backspace` as "back" fires when user hits backspace in any non-input context. The `INPUT/TEXTAREA/contentEditable` guard MUST cover this too. Some custom editors use `contentEditable`; test policy DSL editor before shipping.

## Tech Debt (from ROUTEMAP)

No tech debt items for this story. (Phase 10 tech debt is tracked separately; no open items reference STORY-076.)

## Mock Retirement

No mock retirement for this story. Frontend already uses real `/api` calls; search endpoint is new and immediately wired to real handler.

## Risks & Mitigations

- **Risk:** Search P50 latency >50ms on 10M-row sims without trigram indexes. **Mitigation:** 500ms context timeout; partial results OK if one type query times out (errgroup-per-type with per-type error suppressed to empty array + logged). If latency observed in Task 8 smoke, add a note in handoff but do not add schema migration this story.
- **Risk:** `Backspace` shortcut conflicts with policy DSL editor (contentEditable-adjacent). **Mitigation:** Task 5 guards; Task 8 smoke test on `/policies/:id` editor page.
- **Risk:** Palette rendering performance with debounced fetch. **Mitigation:** 200ms debounce + react-query `staleTime: 10_000` + cap results to 5 per type.
- **Risk:** Operator search through grants JOIN returns duplicates if grant rows repeat. **Mitigation:** `SELECT DISTINCT o.id,...` or `GROUP BY o.id`.
- **Risk:** cmdk internal filter conflicts with API-driven results. **Mitigation:** use `Command.Item value={unique-id}` and disable cmdk filtering via `shouldFilter={false}` on `<Command>` when in entity-search mode.

---

## Pre-Validation Summary

- **Minimum substance (L story ≥100 lines, ≥5 tasks):** PASS — ~260 lines, 8 tasks.
- **Required sections (Goal, Architecture Context, Tasks, Acceptance Criteria Mapping):** PASS.
- **Embedded specs (API, DB note, UI tokens):** PASS.
- **Task complexity (L → ≥1 high):** PASS — Tasks 1, 4, 6 are high.
- **Context refs point to real sections:** PASS.
- **Every new-file task has Pattern ref:** PASS.
- **Design Token Map populated:** PASS.
- **Component Reuse table populated:** PASS.
- **Bug patterns listed:** PASS.
- **Tenant-isolation spelled out:** PASS.
- **No implementation code in plan:** PASS.

Wave plan (dependency-driven):
- **Wave 1 (parallel):** Task 1 (backend), Task 2 (store caps).
- **Wave 2 (parallel):** Task 3 (hook), Task 5 (shortcuts) — both independent after Wave 1.
- **Wave 3:** Task 4 (palette rewrite — needs 2+3), Task 6 (row actions — needs 5).
- **Wave 4:** Task 7 (quick-peek + favorites + new-detail wiring — needs 2+6).
- **Wave 5:** Task 8 (verification — needs all).
