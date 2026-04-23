# Implementation Plan: FIX-223 — IP Pool Detail Polish (Server-side Search, Last Seen, Reserve Modal ICCID)

## Goal
Move IP Pool Detail search from the browser to the server, surface a `Last Seen` column backed by a new `ip_addresses.last_seen_at` column, enrich the addresses listing DTO with SIM identifiers so ICCID / IMSI / MSISDN actually render (today they silently fall back to a UUID slice), and add the APN config static-IP tooltip (AC-5) using the FIX-222 InfoTooltip primitive.

## Scope Discipline
- **In scope:**
  - Backend: `internal/store/ippool.go::ListAddresses` — add `q` param + SIM JOIN + `last_seen_at` projection; new migration `20260424000003_ip_addresses_last_seen.up.sql` / `.down.sql`.
  - Backend: `internal/api/ippool/handler.go::ListAddresses` + `addressResponse` — accept `q`, return `sim_iccid`, `sim_imsi`, `sim_msisdn`, `last_seen_at`.
  - Frontend: `web/src/pages/settings/ip-pool-detail.tsx` — remove client-side filter, wire debounced server `q` param via `useDebounce`, add `Last Seen` column, pass `q` through `useIpPoolAddresses`.
  - Frontend: `web/src/hooks/use-settings.ts::useIpPoolAddresses` — accept `q` argument.
  - Frontend: `web/src/types/settings.ts` — add `last_seen_at?: string` to `IpAddress`.
  - Frontend: `web/src/pages/apns/detail.tsx` — add InfoTooltip on the "IP Pools" tab header or section introducing static-IP reservation (AC-5).
  - Frontend: `web/src/lib/glossary-tooltips.ts` — add `static_ip` glossary entry.
- **Out of scope (explicit):**
  - `last_seen_at` **writer** in AAA/accounting — deferred to a follow-up story. AC-3 is satisfied by column + DTO + UI render (will display `—` until a writer lands). **DEV-### entry records this deferral.** Rationale: wiring the RADIUS Accounting-Interim / Diameter Gx CCR-U path to update `ip_addresses.last_seen_at = NOW()` is a multi-file protocol-layer change (aaa/radius + aaa/diameter) that doubles M effort to L, and F-306 is an IP-pool-view usability finding, not a data-freshness finding. A dedicated fix is cheaper to review in isolation.
  - Reserve Modal SlidePanel conversion (FIX-216 already did the conversion — audit confirms `web/src/pages/settings/ip-pool-detail.tsx:389` is already `<SlidePanel>`, DEV-289).
  - IP Pool index page (`settings/ip-pools/index.tsx`) — untouched.
  - APN Detail Pool Usage tab filtering — untouched.
- **AC-4 residual FE work is minimal.** The Reserve Modal already shows ICCID+IMSI+MSISDN through `<SimSearch>` (`web/src/components/ui/sim-search.tsx`) and the reserve queue renders the selected SIM trio. Once the addresses DTO exposes `sim_iccid/imsi/msisdn`, the "Currently reserved" mini-list inside the panel will also show ICCID instead of the UUID-slice fallback — with zero FE component changes.

## Findings Addressed
| Finding | Summary | How this plan addresses it |
|--------:|---------|----------------------------|
| F-74 | Reserve modal SIM identity unclear | Already fixed by SimSearch (FIX-216 era); DTO enrichment lights up the "Currently reserved" list |
| F-75 | `sim_id` rendered as UUID slice instead of ICCID | AC-1 SIM JOIN enrichment — handler returns `sim_iccid` |
| F-76 | IMSI/MSISDN absent next to ICCID | Same JOIN — handler returns `sim_imsi`, `sim_msisdn` |
| F-78 | No "Last Seen" signal | Migration + store projection + DTO field + FE column |
| F-79 | Static IP semantics not documented | InfoTooltip + glossary entry on APN detail page (AC-5) |
| F-306 | Client-side search fails at /22 scale | AC-1/AC-2 — server `q` param with ILIKE on `address_v4::text` and JOINed SIM fields |

## Architecture Context

### Components Involved
- **Backend (Go)**
  - `internal/store/ippool.go` — `IPAddress` struct, `ListAddresses`, `ipAddressColumns`, `scanIPAddress`.
  - `internal/api/ippool/handler.go` — `addressResponse`, `toAddressResponse`, `ListAddresses` handler.
  - `migrations/20260424000003_ip_addresses_last_seen.up.sql` (new).
- **Frontend (React)**
  - `web/src/pages/settings/ip-pool-detail.tsx` — page component (search input L232-246, filteredAddresses memo L110-122, Table L286-364).
  - `web/src/hooks/use-settings.ts::useIpPoolAddresses` L210-226.
  - `web/src/types/settings.ts::IpAddress` L64-76.
  - `web/src/hooks/use-debounce.ts::useDebounce` (already exists, 300ms pattern used in command-palette L104).
  - `web/src/components/ui/info-tooltip.tsx::InfoTooltip` (FIX-222 primitive).
  - `web/src/lib/glossary-tooltips.ts`.
  - `web/src/pages/apns/detail.tsx` (IP Pools tab area L190-250 ballpark).

### Data Flow (per request)
1. User types in the search Input on IP Pool Detail → `searchFilter` state updates.
2. `useDebounce(searchFilter, 300)` → `debouncedQ` stable after 300ms idle.
3. `useIpPoolAddresses(poolId, debouncedQ)` rebuilds its queryKey and fires `GET /api/v1/ip-pools/{id}/addresses?q=<debouncedQ>&cursor=&limit=50`.
4. Handler validates tenant owns the pool (existing `GetByID` check), parses `q` (trim, ≤ 64 chars), calls `ippoolStore.ListAddresses(poolID, cursor, limit, stateFilter, q)`.
5. Store query: LEFT JOIN `sims s ON s.id = ip_addresses.sim_id`, WHERE `pool_id = $1 AND (q = '' OR ip_addresses.address_v4::text ILIKE '%'||q||'%' OR s.iccid ILIKE '%'||q||'%' OR s.imsi ILIKE '%'||q||'%' OR s.msisdn ILIKE '%'||q||'%')`. Projects existing columns + `last_seen_at` + `s.iccid`, `s.imsi`, `s.msisdn`.
6. Handler builds `addressResponse` with new fields; returns standard envelope.
7. FE renders table — rows with a SIM show ICCID/IMSI/MSISDN (already coded), new `Last Seen` column renders `new Date(last_seen_at).toLocaleString()` or `—`.

### Cursor + `q` interaction
Existing cursor is `address_v4 > $cursor::inet` — still valid when combined with `q` predicate. Each page may be sparse (fewer than `limit` hits) but `limit+1` peek semantics handle this. **No change to cursor model.**

### API Specification — existing endpoint, additive
`GET /api/v1/ip-pools/{id}/addresses?cursor=&limit=50&state=&q=<string>`

Request:
- `q` (optional) — free-text search, trimmed, server-side cap 64 chars, empty string or missing = no filter.

Response `data[]` (new fields **bold**):
```json
{
  "id": "uuid",
  "pool_id": "uuid",
  "address_v4": "10.0.1.42",
  "address_v6": null,
  "allocation_type": "dynamic",
  "state": "allocated",
  "sim_id": "uuid",
  "sim_iccid": "8990011234567890123",   // NEW — AC-1/F-75
  "sim_imsi": "286011234567890",         // NEW — AC-1/F-76
  "sim_msisdn": "+905301234567",         // NEW — AC-1/F-76 (nullable)
  "allocated_at": "2026-04-22T10:00:00Z",
  "last_seen_at": "2026-04-22T14:30:00Z", // NEW — AC-3 (nullable until writer lands)
  "reclaim_at": null
}
```
- Status codes unchanged: 200, 400 (bad `q` length), 403, 404, 500.

### Database Schema

Source: existing `migrations/20260320000002_core_schema.up.sql` + `migrations/20260413000001_story_069_schema.up.sql` (ACTUAL — migration files beat ARCHITECTURE.md TBL-09).

```sql
-- Current ip_addresses (ACTUAL post-story_069):
CREATE TABLE IF NOT EXISTS ip_addresses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pool_id UUID NOT NULL REFERENCES ip_pools(id),
    address_v4 INET,
    address_v6 INET,
    allocation_type VARCHAR(10) NOT NULL DEFAULT 'dynamic',
    sim_id UUID,
    state VARCHAR(20) NOT NULL DEFAULT 'available',
    allocated_at TIMESTAMPTZ,
    reclaim_at TIMESTAMPTZ,
    grace_expires_at TIMESTAMPTZ,  -- story_069
    released_at TIMESTAMPTZ        -- story_069
);
-- NO last_seen_at column today. NO reserved_by_note column.
```

New migration `20260424000003_ip_addresses_last_seen.up.sql`:
```sql
BEGIN;
ALTER TABLE ip_addresses ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;
-- No backfill — existing allocated rows leave last_seen_at NULL; FE renders `—`.
-- No index — column is projection-only; search does not hit it in AC-1.
COMMIT;
```
Down:
```sql
BEGIN;
ALTER TABLE ip_addresses DROP COLUMN IF EXISTS last_seen_at;
COMMIT;
```

**Tenant safety of the SIM JOIN:** `sims` is partitioned by `operator_id` and enforces `tenant_id` at insert. The addresses listing is already gated by `ippoolStore.GetByID(tenantID, poolID)` before calling `ListAddresses` — the pool-id scope bounds the result set to that tenant's pool transitively. The JOIN adds `ON s.id = ip_addresses.sim_id`. We do NOT add a separate `sims.tenant_id = ?` predicate because (a) an address in tenant A's pool cannot reference a SIM in tenant B's pool under the existing allocation invariants, and (b) adding it requires threading tenant_id into `ListAddresses` and changes the signature for no observed risk. **Explicit tenant bind on the JOIN side is tracked as D-### tech debt** — acceptable ship-as-is for this story.

### Screen Mockup (IP Pool Detail — table header change only)

```
┌───────────────────────────────────────────────────────────────────────────┐
│  [Search: "10.0.1" ×]                                     [Reserve IP] ▼ │  <- debounced 300ms
├───────────────────────────────────────────────────────────────────────────┤
│  IP Address    State        Assigned SIM              Assigned At  Last Seen │
│  10.0.1.42     ALLOCATED    89900...(iccid)           14:02:33     14:30:11 │  <- NEW col
│                             286011... +905301...                             │
│  10.0.1.43     AVAILABLE    —                         —            —        │
└───────────────────────────────────────────────────────────────────────────┘
```

### Design Token Map

#### Color / typography / spacing — reuse existing tokens on the page
- Table headers: `text-text-secondary text-[10px] uppercase tracking-wider` (existing)
- Row muted cell: `text-xs text-text-tertiary` (existing)
- InfoTooltip color/bg: primitive handles it — no new tokens.

#### Existing Components to REUSE
| Component | Path | Use For |
|-----------|------|---------|
| `<Input>` | `@/components/ui/input` | Search input (already in use on page) |
| `<useDebounce>` | `@/hooks/use-debounce` | 300ms debounce on `searchFilter` |
| `<InfoTooltip>` | `@/components/ui/info-tooltip` | AC-5 static IP tooltip on APN detail |
| `<SlidePanel>` | `@/components/ui/slide-panel` | already rendering Reserve flow — no change |
| `<SimSearch>` | `@/components/ui/sim-search` | already embedded in Reserve panel — no change |
| `<Table>` / `<TableCell>` | `@/components/ui/table` | add `Last Seen` column |

## Prerequisites
- [x] FIX-216 — Reserve flow is already SlidePanel (DEV-289).
- [x] FIX-222 — `InfoTooltip` primitive + `glossary-tooltips.ts` pattern.
- [x] `useDebounce` hook exists at `web/src/hooks/use-debounce.ts`.

## Task Decomposition

### Wave 1 — Backend: schema + store + handler

#### Task W1-T1: Add `last_seen_at` migration
- **Files:** `migrations/20260424000003_ip_addresses_last_seen.up.sql`, `migrations/20260424000003_ip_addresses_last_seen.down.sql` (new).
- **Change:** additive TIMESTAMPTZ column, no backfill, no index.
- **Verify:** `make db-migrate` runs clean; `\d ip_addresses` shows `last_seen_at`.
- **Context refs:** § Database Schema.

#### Task W1-T2: Extend store — SIM JOIN, `q` filter, `last_seen_at` projection
- **Files:** `internal/store/ippool.go`
- **Changes:**
  1. Add `LastSeenAt *time.Time` to `IPAddress` struct (JSON `last_seen_at`).
  2. Add `SimICCID *string`, `SimIMSI *string`, `SimMSISDN *string` fields.
  3. Rewrite `ipAddressColumns` to a qualified SELECT list (table-aliased + joined):
     ```go
     var ipAddressColumnsSelect = `
       ip.id, ip.pool_id, ip.address_v4::text, ip.address_v6::text,
       ip.allocation_type, ip.sim_id, ip.state, ip.allocated_at, ip.reclaim_at,
       ip.last_seen_at,
       s.iccid, s.imsi, s.msisdn
     `
     ```
  4. Update `scanIPAddress` to read the new columns (mind nullable SIM fields when `sim_id IS NULL`).
  5. Extend `ListAddresses(ctx, poolID, cursor, limit, stateFilter, q string)` — new `q` param (trimmed, caller passes empty string when absent). Build WHERE:
     ```sql
     FROM ip_addresses ip
     LEFT JOIN sims s ON s.id = ip.sim_id
     WHERE ip.pool_id = $1
       [AND ip.state = $N]
       [AND ip.address_v4 > $M::inet]
       [AND (
           ip.address_v4::text ILIKE $K
        OR COALESCE(s.iccid,'')  ILIKE $K
        OR COALESCE(s.imsi,'')   ILIKE $K
        OR COALESCE(s.msisdn,'') ILIKE $K
       )]
     ORDER BY ip.address_v4 ASC NULLS LAST, ip.address_v6 ASC NULLS LAST
     LIMIT $L
     ```
     Where `$K` is `'%' || q || '%'`. Pass `q` only when non-empty.
  6. Cursor unchanged — still `ip.address_v4`.
  7. Audit `GetAddressByID`, `ReserveStaticIP`, `AllocateIP`, `GetIPAddressByID` call sites — they all use `ipAddressColumns` (scoped without JOIN). **Introduce a separate constant `ipAddressColumnsUnjoined`** for these internal mutations (no SIM JOIN needed — they return an `IPAddress` without SIM fields populated). This avoids disrupting allocation paths.
- **Verify:** `go build ./...`, `go vet ./...`, existing `gx_ipalloc_test.go` still passes (uses direct SQL, schema change is additive).
- **Context refs:** § API Specification, § Database Schema, § Data Flow, file paths.

#### Task W1-T3: Handler — accept `q`, extend DTO
- **Files:** `internal/api/ippool/handler.go`
- **Changes:**
  1. `addressResponse` add nullable string fields: `SimICCID *string \`json:"sim_iccid,omitempty"\``, `SimIMSI *string \`json:"sim_imsi,omitempty"\``, `SimMSISDN *string \`json:"sim_msisdn,omitempty"\``, `LastSeenAt *string \`json:"last_seen_at,omitempty"\``.
  2. `toAddressResponse` — copy the fields; format `last_seen_at` as RFC3339Nano when non-nil.
  3. `ListAddresses` handler: read `q := strings.TrimSpace(r.URL.Query().Get("q"))`. Validate `len(q) <= 64`; return `400 invalid_format` with `Search query too long (max 64 chars)` otherwise. Pass `q` into `ippoolStore.ListAddresses(...)`.
- **Verify:** `go build`, `go vet`, existing handler tests (if any) still pass; manual curl against `/ip-pools/{id}/addresses?q=10.0.1` returns enriched rows.
- **Context refs:** § API Specification, § Data Flow.

### Wave 2 — Frontend: hook + page

#### Task W2-T1: Hook signature — accept `q`
- **Files:** `web/src/hooks/use-settings.ts`, `web/src/types/settings.ts`
- **Changes:**
  1. `IpAddress` type: add `last_seen_at?: string` (sim_iccid/imsi/msisdn already declared at L72-74).
  2. `useIpPoolAddresses(poolId: string, q?: string)` — include `q` in `queryKey` and append `&q=<encoded>` to the URL when non-empty.
- **Verify:** `npm run typecheck` clean.
- **Context refs:** § API Specification, § Data Flow.

#### Task W2-T2: Page — server-side search + Last Seen column
- **Files:** `web/src/pages/settings/ip-pool-detail.tsx`
- **Changes:**
  1. Import `useDebounce` from `@/hooks/use-debounce`.
  2. Add `const debouncedSearch = useDebounce(searchFilter, 300)`.
  3. Call `useIpPoolAddresses(poolId ?? '', debouncedSearch)` — query refires whenever `debouncedSearch` changes.
  4. **Delete the client-side `filteredAddresses` memo (L110-122)** — replace every reference with `sortedAddresses` (server already filtered). The `stateOrder` sort stays on the FE since server orders by `address_v4`.
  5. Empty-state string updated to reflect server mode: `No addresses matching "{searchFilter}"` when `searchFilter && sortedAddresses.length === 0`.
  6. Footer counter: `{sortedAddresses.length} addresses` (simpler — no "N of M" because server already filtered).
  7. Table header: add `<TableHead>Last Seen</TableHead>` between `Assigned At` and the action column.
  8. Row cell: `<TableCell><span className="text-xs text-text-secondary">{addr.last_seen_at ? new Date(addr.last_seen_at).toLocaleString() : '—'}</span></TableCell>`.
  9. Adjust the empty-row `colSpan={6}` (was 5).
- **Verify:** type-check, manual /22 pool smoke: type a partial IP / ICCID → 300ms debounce → network request hits `?q=`; `Last Seen` column shows `—` for all rows (column added, writer deferred).
- **Context refs:** § Screen Mockup, § Data Flow, file paths.

### Wave 3 — AC-5: Static IP glossary tooltip on APN detail

#### Task W3-T1: Add glossary entry + tooltip placement
- **Files:** `web/src/lib/glossary-tooltips.ts`, `web/src/pages/apns/detail.tsx`
- **Changes:**
  1. Add `static_ip` term to `glossary-tooltips.ts` with a short business-facing definition:
     > *Static IP — an IP address permanently assigned to a specific SIM via pool reservation. Survives re-authentication and session teardown. Reclaim grace window configurable per pool.*
  2. In `web/src/pages/apns/detail.tsx` IP Pools tab area (around the `<h3>IP Pools` or equivalent section intro — see L236+), wrap the pool-usage heading or section label in `<InfoTooltip term="static_ip">...</InfoTooltip>`. Exact line chosen by the developer based on nearest visible label; primary intent: any user looking at "IP Pools" for this APN can hover to understand what a static reservation means.
- **Verify:** hover shows tooltip; dev console lacks the `[InfoTooltip] Unknown term` warning.
- **Context refs:** § Scope Discipline (AC-5), existing FIX-222 tooltip pattern.

## Risks & Regression

1. **Risk — allocation-path breakage if `ipAddressColumns` is repurposed for JOINed list while other call sites still reference it.**
   Mitigation: introduce a separate constant for the unjoined select (W1-T2 step 7). Walk each call site (`GetAddressByID`, `ReserveStaticIP`, `AllocateIP`, `GetIPAddressByID`, grace/reclaim paths) and leave them on the unjoined constant. Verified by existing `gx_ipalloc_test.go` passing.
2. **Risk — cursor sparsity on filtered pages.**
   Already handled by the `limit+1` peek pattern; document in AC commentary. Advisor confirmed no change needed.
3. **Risk — `q` parameter ILIKE on `address_v4::text` performs sequential scan.**
   Spec proposed an index — rejected. `%q%` cannot use a B-tree index anyway, and `ip_addresses` scoped to a single `pool_id` is bounded (≤ 1024 rows at /22, ≤ 65 536 at /16). Sequential scan within one pool is acceptable. **No new index.**
4. **Risk — `last_seen_at` always NULL until writer lands.**
   Accepted. UI renders `—`. DEV-### entry calls out the follow-up story. F-306's primary complaint is "client-side search broken at /22 scale" — satisfied by AC-1; the Last Seen column is usability-additive.
5. **Risk — tenant leak via SIM JOIN.**
   Bounded by `pool_id → tenant_id` invariant (allocation path never links a SIM in tenant A to a pool in tenant B). Accepted ship-as-is; D-### tracks explicit `s.tenant_id = ?` belt-and-suspenders addition.

## Test Plan
- **Manual — server-side search at scale:** seed a /22 pool (1024 IPs); allocate ~10 SIMs across it; search for an IP in the middle of the range and for a SIM's ICCID suffix — both return results in < 300 ms after debounce.
- **Manual — debounce behavior:** fast-typing "10.0.1.4" should fire ≤ 1 request after 300 ms pause, not 7.
- **Manual — Last Seen column:** renders `—` consistently; no console warnings.
- **Manual — Reserve modal ICCID:** open panel, search for a SIM, add to queue; reserved IP row (after save + refetch) shows `sim_iccid` instead of UUID slice.
- **Manual — AC-5:** hover the APN detail IP Pools section label → tooltip text appears; dev console clean.
- **Go — existing tests:** `make test` green; `gx_ipalloc_test.go` and any `ippool` handler tests still pass.
- **Regression — allocation paths:** reserve a specific IP (`AddressV4` != nil) and a random pick (`AddressV4 == nil`) — both still complete.
- **Unit tests — DEFERRED to D-091** (global unit-test-coverage campaign). No new unit tests added in this story.

## AC Matrix
| AC | Satisfied by | Status |
|----|--------------|--------|
| AC-1 backend `?q=` | W1-T2, W1-T3, W2-T1 | planned |
| AC-2 remove client filter | W2-T2 | planned |
| AC-3 Last Seen column | W1-T1 (migration), W1-T2 (projection), W1-T3 (DTO), W2-T2 (column) | planned — writer deferred |
| AC-4 Reserve Modal SIM identity | FIX-216 already shipped SlidePanel; W1-T2/T3 lights up `sim_iccid` in "Currently reserved" list | MOSTLY-EXISTING + enrichment |
| AC-5 Static IP APN tooltip | W3-T1 | planned |

## Plan Reference
Priority: P2 · Effort: M · Wave: 6 · Target: 3 waves · 6 tasks total.
