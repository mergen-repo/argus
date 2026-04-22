# Implementation Plan: FIX-214 — CDR Explorer Page (Filter, Search, Session Timeline, Export)

- **Story**: `docs/stories/fix-ui-review/FIX-214-cdr-explorer-page.md`
- **Tier / Effort**: P1 · L · Wave 4
- **Findings addressed**: F-62 (CDR Explorer page missing)
- **Depends on (DONE)**: FIX-207 (CDR data integrity), FIX-208 (Aggregates facade), FIX-212 (envelope), FIX-213 (entity clickable patterns)
- **Mode**: FIX-NNN pre-release — architecture NOT frozen; extend existing CDR surfaces freely.
- **FE test policy**: build-only, no runtime tests (project convention per FIX-213). Type-level smoke + manual browser verification.

---

## Decisions (read first — pin these before implementation)

| # | Topic | Decision | Rationale |
|---|---|---|---|
| **D1** | Route path | **`/cdrs`** (plural, matches existing sidebar convention: `/sims`, `/apns`, `/sessions`, `/policies`). Sidebar entry under MANAGEMENT, positioned **between Sessions and Policies** per AC-9. Icon = `FileBarChart` from `lucide-react` (already imported in sidebar). | Consistency with existing routes. Sidebar reads cleanly: "Sessions → CDRs → Policies" (live traffic → billing records → rules). |
| **D2** | List endpoint: extend vs new | **EXTEND `GET /api/v1/cdrs`** — backend already has cursor pagination + sim_id/operator_id/from/to/min_cost. **Add**: `apn_id` (single, first iteration — multi via repeated `?apn_id=` if later needed), `record_type` (start/interim/stop/auth/auth_fail/reject), `session_id` (for AC-8 deep-link). Remove `min_cost` from FE (not in story scope). No breaking changes to existing callers. | Surgical extension per advisor. Keeps signature stable. Multi-select operator/APN deferred — story AC-2 says "multi-select" but UI can send a single ID and iterate on multi in a follow-up if UX insists. **Plan's FE renders as single-select dropdown** to match backend; note in risks. |
| **D3** | Text search (ICCID/IMSI/MSISDN) | **Two-phase client resolve**: SIM search uses existing `<SimSearch>` component (`web/src/components/ui/sim-search.tsx`) which hits `GET /api/v1/sims?q=...`. Selecting a SIM sets `sim_id` filter on the CDR list query. **No backend JOIN needed.** For CDR row rendering (showing ICCID/IMSI/MSISDN per row), add **FE-side batch resolution**: collect unique `sim_id` values from the page result, `GET /api/v1/sims?ids=<comma>` (one-shot, cached via TanStack Query), merge into DTO rows. | Advisor's option (b). Keeps hot `cdrs` query fast (no JOIN against 10M sims). TanStack cache staleTime 60s eliminates repeat lookups. **Requires**: verify `GET /api/v1/sims` already supports `ids=` param or adds it — see Task 3. Operators/APNs resolve via existing `NameCache` + list hooks (already loaded on app shell). |
| **D4** | Session timeline — new endpoint | **NEW: `GET /api/v1/cdrs/by-session/{session_id}`** — returns all CDRs for that session ordered by `timestamp ASC`. Tenant-scoped (filter by `tenant_id` server-side, 404 if cross-tenant). Uses `idx_cdrs_dedup` prefix `(session_id, timestamp, ...)` → fast. DTO identical to list endpoint. No pagination (max ~100 CDRs per session; interim updates cap at ~50 per day). | Advisor item 4. Session lookup by session_id UUID benefits from dedup index's leading column. Generic list endpoint via `?session_id=` would also work but a named endpoint is clearer, allows future per-session aggregates (bytes delta computed server-side in a follow-up). **Bytes delta is client-side** (current plan): FE computes `bytes_in[i] - bytes_in[i-1]` for each interim row. |
| **D5** | Stats card (AC-6) data source | **Use `Aggregates` facade** (`internal/analytics/aggregates/service.go`) to satisfy PAT-012. Add 4 new methods: `CDRCountInWindow(ctx, tenantID, from, to, filters) (int64, error)`, `CDRTotalBytesInWindow(ctx, tenantID, from, to, filters) (int64, int64, error)` (bytes_in, bytes_out), `CDRUniqueSIMsInWindow(ctx, tenantID, from, to, filters) (int64, error)`, `CDRUniqueSessionsInWindow(ctx, tenantID, from, to, filters) (int64, error)`. All take the same filter struct so numbers match the visible list. **No direct `SELECT COUNT` in handler.** New endpoint `GET /api/v1/cdrs/stats` returns all 4 in one envelope. | PAT-012 compliance — aggregate numbers shown next to a list MUST flow through the facade to prevent drift. 4 separate methods share a CTE-free filter expression; cache-invalidate on `cdrs.inserted` NATS event (same cache pattern as other facade methods). |
| **D6** | 30-day range cap + admin override | **Handler-level validation**: if `to - from > 30 days` AND user role != `super_admin`/`tenant_admin` → 422 VALIDATION_ERROR "Date range exceeds 30 days". Admins get no cap (for audits). Constant `MaxCDRQueryRange = 30 * 24 * time.Hour` in `internal/api/cdr/handler.go`. | Risk-1 mitigation from story. Role-based override keeps compliance auditors unblocked. 422 gives FE a structured error to show (not 400). |
| **D7** | Required date range | **FE enforces**: `TimeframeSelector` default = `24h`; form always submits a `from`/`to`. **Backend enforces**: if `from` missing → 422 "date range required". `to` defaults to `now()` when only `from` given. | AC-2: bounds hypertable chunk pruning. Without it, a naive `SELECT … FROM cdrs WHERE tenant_id = $1` scans the full 10M-row hypertable. |
| **D8** | Timezone handling | **Server stores/returns UTC RFC3339** (already the case). **FE displays in `Europe/Istanbul`** using `Intl.DateTimeFormat('tr-TR', { timeZone: 'Europe/Istanbul', ... })` via a new `web/src/lib/time.ts` `formatCDRTimestamp(iso)` helper. Follows PAT-style timezone fix already in `cdr.go:GetTrafficHeatmap7x24`. **TimeframeSelector "last 24h"** means `to = now()`, `from = now() - 24h` — both UTC on the wire; user sees Istanbul in UI. | Bug pattern warning: UTC/local timezone drift caused the heatmap bug. Centralize formatting in one helper to prevent regressions. Istanbul is Argus's deployment timezone per existing code. |
| **D9** | Performance — index audit | **Task 1 = DB EXPLAIN ANALYZE** on 1M-row fixture for the target query: `SELECT … FROM cdrs WHERE tenant_id=$1 AND timestamp >= $2 AND timestamp <= $3 AND operator_id=$4 ORDER BY timestamp DESC LIMIT 51`. **If p95 > 2s** → add migration `20260421000003_cdrs_explorer_indexes.up.sql` creating `CREATE INDEX IF NOT EXISTS idx_cdrs_tenant_ts_op ON cdrs(tenant_id, timestamp DESC, operator_id)` and `idx_cdrs_tenant_ts_sim ON cdrs(tenant_id, sim_id, timestamp DESC)`. Otherwise skip migration. **Hypertable chunk pruning** relies on `timestamp` predicate — already enforced by D6/D7. | AC-7 is load-bearing. Advisor flagged only dedup index exists today. Conditional migration — don't ship unnecessary indexes that slow inserts on the hot path. |
| **D10** | Export endpoint reuse | **REUSE existing `POST /api/v1/cdrs/export`** (creates `cdr_export` job via `internal/job/cdr_export.go`). **Extend** the payload to accept `sim_id`, `apn_id`, `record_type`, `session_id` (currently only `from`/`to`/`operator_id`/`format`). Job payload extended in `cdr_export.go`; `CDRStore.StreamForExport` extended with the new filter params so exported set matches the visible list. **Do NOT** build a new endpoint. **Do NOT** use the inline `GET /api/v1/cdrs/export.csv` for this UI — that's legacy for admin CLI; the user clicks "Export" → job queued → toast "Report queued, visible in /reports". | AC-5 + advisor item 1. The inline CSV streamer blocks the HTTP thread for large sets; the job approach is the correct path. Coordination note: FIX-248 refactors the reports subsystem — plan stays compatible by keeping the `cdr_export` job type name stable. |
| **D11** | FIX-248 coordination | **Not blocking.** FIX-248 refactors report *shape* (result envelope, delivery mechanism). This plan uses existing `POST /api/v1/cdrs/export` → `cdr_export` job → `/reports` page. When FIX-248 lands, FE's `/reports` link in the export toast will automatically benefit. If FIX-248 renames `cdr_export` → something else, that's a 1-line FE change documented in Risks. | Decouple. Advisor's last point. |
| **D12** | Deep link from Session Detail (AC-8) | **Session Detail page edit**: add an "Explore CDRs" button next to the existing session metadata card, routing to `/cdrs?session_id=<sess_id>&from=<started_at_iso>&to=<ended_at_iso+1h>`. FE parses `session_id` query param on `/cdrs` mount → filter bar auto-populates + triggers query. | AC-8. Query-string contract is stable; server rejects invalid UUIDs gracefully. +1h on `to` catches late interim/stop records. |
| **D13** | FE state management | **TanStack Query** (already the project pattern). New hook `web/src/hooks/use-cdrs.ts`: `useCDRList(filters)` (infinite query, cursor), `useCDRStats(filters)` (regular query), `useCDRSessionTimeline(sessionId)` (regular query, enabled when drawer open), `useSimBatch(ids[])` (resolves sim names, staleTime 60s). Filter state lives in page-local `useState` + `useSearchParams` (URL-serializable for bookmark/share per F-41 follow-up). | Consistency with `use-sessions.ts` / `use-sims.ts`. URL-synced filters = shareable links. No Zustand store needed (filters are page-scoped, not global). |
| **D14** | SlidePanel vs inline drawer | **Use `<SlidePanel>`** (already exists `web/src/components/ui/slide-panel.tsx`, width `lg` = `max-w-2xl`). Per Option C modal decision: SlidePanel for rich forms/views, Dialog for compact confirms. Session timeline = rich view → SlidePanel. | Project modal convention (CLAUDE.md "Modal decision: Option C"). |
| **D15** | Timeline chart | **Recharts `LineChart`** (`recharts@^3.8.0` already in `web/package.json`). X-axis = timestamp, Y-axis = cumulative bytes (sum of bytes_in + bytes_out per row). Render `<LineChart width={600} height={240}>` inside the SlidePanel. **Height of 240px max** to fit above the metadata/rows. | Reuse existing chart lib — no new deps. Cumulative bytes curve matches the session timeline mental model. |
| **D16** | Multi-select vs single-select (AC-2) | **First iteration = single-select** dropdowns for operator and APN (matches backend D2). **Record-type filter = multi-select chip group** (5-7 known values, fits inline, no backend change — repeated `record_type` params on URL, backend already single; change handler to accept CSV or first-value only → FE sends first selected, logs warning in console for the hidden multi case). **Mark as tech-debt item** D-214-001 in Gate to address in a P2 follow-up. | Story says "multi-select"; backend doesn't support it today and adding true multi requires a handler rewrite + index audit. First-value approximation is 80% of user need; full multi is a post-release polish. Explicit tech-debt prevents forgetting. |
| **D17** | Turkish UI | **Page chrome Turkish**: title "CDR Kayıtları", filter labels (`SIM`, `Operatör`, `APN`, `Kayıt Tipi`, `Zaman`), buttons (`Filtrele`, `Temizle`, `Dışa Aktar`). **Technical identifiers English**: column headers `ICCID`, `IMSI`, `MSISDN`, `SESSION ID`, `BYTES IN`, `BYTES OUT`, `TIMESTAMP`, record_type values (`start`, `interim`, `stop`). | Consistent with CLAUDE.md "Turkish UI chrome" + FIX-213 D14 pattern. |

---

## Problem Context

### Current state (verified files)

- `internal/api/cdr/handler.go`: list endpoint with cursor + filters (sim/operator/from/to/min_cost), export endpoint queuing `cdr_export` job.
- `internal/store/cdr.go`: `ListByTenant` with cursor + filter predicates, `StreamForExport` for job-backed CSV, `GetCumulativeSessionBytes` (single scalar).
- `internal/job/cdr_export.go`: streams CDRs in filter window, writes CSV, base64-encodes into job result.
- `internal/gateway/router.go:605-611`: routes `/api/v1/cdrs`, `/api/v1/cdrs/export`, `/api/v1/cdrs/export.csv`.
- `internal/analytics/aggregates/service.go`: facade interface — **no CDR methods yet** (to be added by this story).
- `internal/cache/name_cache.go`: operator/APN/pool name cache — **no SIM identifier cache**.
- `migrations/20260320000002_core_schema.up.sql:418-435`: `cdrs` table schema.
- `migrations/20260322000001_cdr_dedup_index.up.sql`: only non-chunk index today — `(session_id, timestamp, record_type)`.
- `web/src/router.tsx`: no `/cdrs` route.
- `web/src/components/layout/sidebar.tsx:88-99`: MANAGEMENT group without CDR entry.
- `web/src/pages/sessions/index.tsx`: existing list-page pattern — reuse.
- `web/src/components/ui/slide-panel.tsx`: exists, ready for timeline drawer.
- `web/src/components/ui/sim-search.tsx`: reusable SIM autocomplete — ideal for SIM filter.
- `web/src/components/ui/timeframe-selector.tsx`: ready-made "15m | 1h | 6h | 24h | 7d | 30d" control.
- `web/src/components/shared/entity-link.tsx`: reusable clickable entity cells for SIM/operator/APN columns.

### Drift vs finding F-62

F-62: "CDR data is the billing + forensic backbone; no user-facing UI to query, inspect, or export. Only dashboard aggregates + CSV job export." Target: operator/analyst can filter → inspect → drill → export without leaving the UI.

### Out of scope

- Dashboard KPI recomputation (already exists via `cdrs_daily` continuous aggregate).
- True multi-select operator/APN filters (D16 tech-debt).
- Custom CSV column picker (reports subsystem / FIX-248 territory).
- Admin-only CDR deletion / purging (separate admin feature).
- Real-time CDR streaming (drawer is snapshot; not WS-backed). CDR events ALREADY flow to Event Stream drawer via `cdrs.inserted` NATS — not relevant here.

---

## Architecture Context

### Components Involved

| Layer | Component | Role | File path pattern |
|---|---|---|---|
| DB | `cdrs` hypertable (TBL-18) | Primary data (read) | `migrations/*core_schema*` |
| DB (new) | Optional perf index | Query speedup | `migrations/20260421000003_cdrs_explorer_indexes.*` |
| Store | `CDRStore` | Data access | `internal/store/cdr.go` |
| Service | `Aggregates` facade | Stats card counts | `internal/analytics/aggregates/service.go` |
| API | `cdrapi.Handler` | HTTP endpoints | `internal/api/cdr/handler.go` |
| Job | `CDRExportProcessor` | Background CSV export | `internal/job/cdr_export.go` |
| Gateway | `router.go` | Route registration | `internal/gateway/router.go` |
| FE page | `CDRExplorerPage` | List + filters + stats | `web/src/pages/cdrs/index.tsx` (NEW) |
| FE drawer | `SessionTimelineDrawer` | Session timeline view | `web/src/pages/cdrs/session-timeline.tsx` (NEW) |
| FE hook | `useCDRs*` | TanStack Query hooks | `web/src/hooks/use-cdrs.ts` (NEW) |
| FE router | — | Register `/cdrs` | `web/src/router.tsx` |
| FE sidebar | — | Nav entry | `web/src/components/layout/sidebar.tsx` |

### Data Flow

```
User /cdrs mount
  → useSearchParams reads {sim_id, operator_id, apn_id, record_type, session_id, from, to}
  → useCDRList(filters) infiniteQuery → GET /api/v1/cdrs?filters&cursor=…
        → router.go → cdrapi.Handler.List
        → handler validates 30d cap (D6) + required range (D7)
        → store.ListByTenant (extended with apn_id/record_type/session_id)
        → cdrDTO[] + next cursor
  → useCDRStats(filters) → GET /api/v1/cdrs/stats
        → Aggregates.CDRCount/Bytes/UniqueSIMs/UniqueSessions InWindow
  → useSimBatch(rowSimIds) → GET /api/v1/sims?ids=a,b,c
        → merges iccid/imsi/msisdn into row display
  → Table renders; click row → setActiveSession(session_id)
  → SessionTimelineDrawer open → useCDRSessionTimeline(session_id)
        → GET /api/v1/cdrs/by-session/{session_id}
        → handler fetches all CDRs via CDRStore.ListBySession (new method)
        → Recharts LineChart + per-row bytes delta

Export:
  User clicks "Dışa Aktar"
  → POST /api/v1/cdrs/export {from,to,sim_id?,operator_id?,apn_id?,record_type?,session_id?,format:csv}
  → handler queues cdr_export job with payload
  → returns 202 + job_id
  → toast "Rapor hazırlanıyor. İlerleme için → /reports"

Session-Detail deep link:
  User in /sessions/:id → clicks "Explore CDRs" button
  → navigate to /cdrs?session_id=X&from=Y&to=Z
  → page parses, filter bar auto-populates, list auto-fires
```

### API Specifications

**GET `/api/v1/cdrs`** (EXTEND existing)
- Query params (all optional except `from`/`to`):
  - `cursor` (string) — opaque id-based cursor
  - `limit` (int) — default 50, max 100
  - `from` (RFC3339, REQUIRED)
  - `to` (RFC3339, REQUIRED)
  - `sim_id` (UUID)
  - `operator_id` (UUID)
  - `apn_id` (UUID) — NEW
  - `record_type` (string: `start|interim|stop|auth|auth_fail|reject`) — NEW
  - `session_id` (UUID) — NEW (AC-8)
- Validation:
  - `from`/`to` missing → 422 `VALIDATION_ERROR` "date range required"
  - `to - from > 30d` AND role not in (super_admin, tenant_admin) → 422 "range exceeds 30 days"
  - invalid UUID → 400 `INVALID_FORMAT`
- Success (200): `{ status:"success", data: cdrDTO[], meta:{ cursor, limit, has_more } }`
- Error: standard envelope.

**GET `/api/v1/cdrs/by-session/{session_id}`** (NEW)
- Tenant-scoped (filters by `tenant_id = ctx.tenant_id`); returns 404 if session UUID belongs to another tenant.
- Response: `{ status:"success", data: { session_id, cdrs: cdrDTO[] } }` ordered `timestamp ASC`.
- No pagination (bounded ~100 rows per session).

**GET `/api/v1/cdrs/stats`** (NEW)
- Query params identical to `/api/v1/cdrs` (except `cursor`/`limit`). Same validation.
- Response: `{ status:"success", data: { total_count, total_bytes_in, total_bytes_out, unique_sims, unique_sessions } }`.
- Backing: `Aggregates.CDR*InWindow` methods (D5).

**POST `/api/v1/cdrs/export`** (EXTEND existing)
- Body: `{ from, to, format:"csv", operator_id?, sim_id?, apn_id?, record_type?, session_id? }`.
- Same validation as list endpoint. Queues `cdr_export` job.
- Response (202): `{ status:"success", data: { job_id, status:"queued" } }`.

**GET `/api/v1/sims?ids=a,b,c`** (VERIFY EXISTS — add if missing)
- If not supported by current SIM list handler → add `ids` (CSV of UUIDs) path. Returns only id/iccid/imsi/msisdn fields. Max 500 ids.
- Used by `useSimBatch` for row rendering.

### Database Schema

**Source: `migrations/20260320000002_core_schema.up.sql:418-435` (ACTUAL)**

```sql
CREATE TABLE IF NOT EXISTS cdrs (
    id BIGSERIAL,
    session_id UUID NOT NULL,
    sim_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,
    apn_id UUID,
    rat_type VARCHAR(10),
    record_type VARCHAR(20) NOT NULL,
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    duration_sec INTEGER NOT NULL DEFAULT 0,
    usage_cost DECIMAL(12,4),
    carrier_cost DECIMAL(12,4),
    rate_per_mb DECIMAL(8,4),
    rat_multiplier DECIMAL(4,2) DEFAULT 1.0,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Hypertable: via 20260320000003 (time_bucket partition on timestamp)
-- Indexes today:
--   idx_cdrs_dedup (session_id, timestamp, record_type)  [dedup + session lookup]
-- + TimescaleDB auto chunk indexes on timestamp
```

**Source: `migrations/20260320000002_core_schema.up.sql:390-413` (sessions table, for AC-8 deep-link context)** — not modified by this story.

**Proposed new migration (conditional on D9 perf audit):**

```sql
-- File: migrations/20260421000003_cdrs_explorer_indexes.up.sql
-- Only ship if Task 1 EXPLAIN ANALYZE shows p95 > 2s at 7d/1M-row fixture.
CREATE INDEX IF NOT EXISTS idx_cdrs_tenant_ts_op
    ON cdrs (tenant_id, timestamp DESC, operator_id);
CREATE INDEX IF NOT EXISTS idx_cdrs_tenant_ts_sim
    ON cdrs (tenant_id, sim_id, timestamp DESC);
```

```sql
-- File: migrations/20260421000003_cdrs_explorer_indexes.down.sql
DROP INDEX IF EXISTS idx_cdrs_tenant_ts_op;
DROP INDEX IF EXISTS idx_cdrs_tenant_ts_sim;
```

### Screen Mockup — `/cdrs` list page (ASCII, embedded)

```
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│ CDR Kayıtları                                                                  [•LIVE]   │
│ ────────────────────────────────────────────────────────────────────────────────────────  │
│ ┌─────────────────────────────── Filtreler ───────────────────────────────────────────┐ │
│ │ [ 🔍 SIM ara: ICCID/IMSI/MSISDN...    ⌨ ] [ Operatör ▾ ] [ APN ▾ ]  [ Tip: ● Tümü ] │ │
│ │ [ ● start  ○ interim  ○ stop  ○ auth  ○ fail  ○ reject ]                             │ │
│ │ [ 15m  1h  6h | 24h | 7d  30d  ✎ özel aralık ]       [Temizle]   [ ⬇ Dışa Aktar ]  │ │
│ └──────────────────────────────────────────────────────────────────────────────────────┘ │
│ ┌──────── Toplam ────────┐ ┌──── Toplam Bayt ────┐ ┌─── Tekil SIM ───┐ ┌── Oturum ──┐   │
│ │ 4,823,102               │ │ ↓ 284.3 GB  ↑ 12.7  │ │ 18,411           │ │ 42,307     │   │
│ │ son 24 saat             │ │ GB                   │ │                   │ │             │   │
│ └─────────────────────────┘ └──────────────────────┘ └───────────────────┘ └─────────────┘   │
│ ────────────────────────────────────────────────────────────────────────────────────────  │
│ ICCID                IMSI            MSISDN     OPERATÖR     APN       TİP     ↓ BYTES  ↑ BYTES  TIMESTAMP              │
│ ─────────────────────────────────────────────────────────────────────────────────────── │
│ 893520…34567  →  28601…67890  →  +90…1234    Turkcell     iot.m2m   interim  2.1 MB   48 KB   21.04.2026 14:23:07 TR  │
│ 893520…34568  →  28601…67891  →  +90…1235    Vodafone     apn.biz   stop     ∞        ∞       21.04.2026 14:22:55 TR  │
│ …                                                                                         │
│ ────────────────────────────────────────────────────────────────────────────────────────  │
│                                                        [Daha Fazla Yükle ⟳]             │
└──────────────────────────────────────────────────────────────────────────────────────────┘

• Row click → opens SessionTimelineDrawer (SlidePanel, right, lg width)
• ICCID/IMSI columns: EntityLink → /sims/:sim_id
• OPERATÖR column: EntityLink → /operators/:operator_id
• APN column: EntityLink → /apns/:apn_id  (nullable — plain "—" if null)
• TİP column: colored badge (start=accent, interim=info, stop=success, auth_fail/reject=danger)
• TIMESTAMP: formatted via Intl.DateTimeFormat('tr-TR', {timeZone:'Europe/Istanbul'})
• Toolbar right: ⬇ Dışa Aktar (export) → queues job → toast
• Empty state: "Bu filtre için CDR bulunamadı." + [Filtreleri Temizle]
• Loading: skeleton rows (reuse Skeleton atom)
• Error: error state card with retry button
```

### Screen Mockup — Session Timeline Drawer (SlidePanel)

```
┌────────────────────────── Oturum Zaman Çizelgesi ──────────────────────────┐
│ Session ID: 4f8c2a90-…-7b41  ×                                              │
│ SIM: 8935201234567890123 (IMSI 286011234567890)                             │
│ Operatör: Turkcell  ·  APN: iot.m2m  ·  RAT: NB-IoT                         │
│ Süre: 00:12:47  ·  Başlangıç: 14:10:20 TR  ·  Son: 14:23:07 TR             │
│ ────────────────────────────────────────────────────────────────────────── │
│ Kümülatif Bayt (start → stop)                                               │
│ ┌─────────────────────────────────────────────────────────────────────────┐ │
│ │ 2.5MB ┤                                                          ╭── ●│ │
│ │ 2.0MB ┤                                            ╭───────── ●       │ │
│ │ 1.5MB ┤                             ╭──── ●                            │ │
│ │ 1.0MB ┤              ╭── ●                                             │ │
│ │ 0.5MB ┤ ●                                                              │ │
│ │   0 ──┴──────────────────────────────────────────────────────────────  │ │
│ │        14:10   14:12   14:14   14:16   14:18   14:20   14:22   14:24  │ │
│ └─────────────────────────────────────────────────────────────────────────┘ │
│ ────────────────────────────────────────────────────────────────────────── │
│ ZAMAN      TİP       ↓ BYTES   Δ↓       ↑ BYTES   Δ↑      KÜMÜLATİF       │
│ 14:10:20   start     0         —        0         —       0               │
│ 14:13:45   interim   580 KB    +580 KB  12 KB     +12 KB  592 KB          │
│ 14:17:18   interim   1.4 MB    +820 KB  28 KB     +16 KB  1.4 MB          │
│ 14:23:07   stop      2.1 MB    +700 KB  48 KB     +20 KB  2.1 MB          │
│ ────────────────────────────────────────────────────────────────────────── │
│ [Oturum detayına git →] (→ /sessions/:session_id)                          │
└────────────────────────────────────────────────────────────────────────────┘
```

### Design Token Map

#### Color Tokens

| Usage | Token Class | NEVER Use |
|---|---|---|
| Page bg | `bg-bg-primary` | `bg-[#06060B]`, `bg-black` |
| Card/Panel bg | `bg-bg-surface` | `bg-[#0C0C14]`, `bg-white` |
| Elevated bg (dropdowns/modal) | `bg-bg-elevated` | `bg-[#12121C]` |
| Hover | `bg-bg-hover` | `bg-[#1A1A28]`, `bg-gray-800` |
| Active filter chip | `bg-accent-dim text-accent` | `bg-[#00D4FF]/15` |
| Primary border | `border-border` | `border-[#1E1E30]` |
| Subtle row separator | `border-border-subtle` | — |
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-white` |
| Secondary text | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-400` |
| Muted text, placeholders | `text-text-tertiary` | `text-gray-500` |
| Link / entity button | `text-accent` | `text-[#00D4FF]`, `text-cyan-400` |
| Success badge (stop) | `bg-success-dim text-success` | `bg-green-500/10 text-green-400` |
| Warning (suspended) | `bg-warning-dim text-warning` | — |
| Danger badge (auth_fail/reject) | `bg-danger-dim text-danger` | `bg-red-500/10` |
| Info badge (interim) | `text-info` + subtle bg | — |

#### Typography Tokens

| Usage | Token Class | NEVER Use |
|---|---|---|
| Page title "CDR Kayıtları" | `text-[15px] font-semibold text-text-primary` | `text-xl`, arbitrary |
| Stat card value | `font-mono text-lg font-bold` | — |
| Stat card label | `text-[10px] uppercase tracking-[1.5px] text-text-secondary` | — |
| Table header | `text-[10px] uppercase tracking-[0.5px] text-text-tertiary` | — |
| Table cell (data, text) | `text-[13px]` | `text-sm` |
| Table cell (mono — ICCID/IMSI/IP/timestamp) | `font-mono text-[12px]` | — |
| Filter chip label | `text-xs font-medium` | — |

#### Spacing & Elevation

| Usage | Token | NEVER Use |
|---|---|---|
| Card radius | `rounded-[var(--radius-md)]` | `rounded-md`, `rounded-lg` |
| Button radius | `rounded-[var(--radius-sm)]` | — |
| Shadow | (none on cards by default; hover = `shadow-glow`) | `shadow-lg` arbitrary |
| Content padding | `p-6` (matches 24px content-padding) | `p-[23px]` |
| Card inner padding | `p-4` or `p-5` | `p-[17px]` |

#### Components to REUSE (do NOT recreate)

| Component | Path | Use For |
|---|---|---|
| `<Card>` | `web/src/components/ui/card.tsx` | Stat cards + page containers |
| `<Table>` + parts | `web/src/components/ui/table.tsx` | All tables — NEVER raw `<table>` |
| `<Button>` | `web/src/components/ui/button.tsx` | All buttons |
| `<Badge>` | `web/src/components/ui/badge.tsx` | record_type badge |
| `<Input>` | `web/src/components/ui/input.tsx` | Never raw `<input>` |
| `<Select>` | `web/src/components/ui/select.tsx` | Operator/APN dropdowns |
| `<SimSearch>` | `web/src/components/ui/sim-search.tsx` | SIM autocomplete filter |
| `<TimeframeSelector>` | `web/src/components/ui/timeframe-selector.tsx` | Time range |
| `<SlidePanel>` + `<SlidePanelFooter>` | `web/src/components/ui/slide-panel.tsx` | Session timeline drawer |
| `<Skeleton>` | `web/src/components/ui/skeleton.tsx` | Loading rows |
| `<Spinner>` | `web/src/components/ui/spinner.tsx` | Inline loading |
| `<EmptyState>` | `web/src/components/shared/empty-state.tsx` | No results |
| `<EntityLink>` | `web/src/components/shared/entity-link.tsx` | SIM/operator/APN clickable cells |
| `<CopyableId>` | `web/src/components/shared/copyable-id.tsx` | session_id, full UUID on drawer |
| `<TableToolbar>` | `web/src/components/ui/table-toolbar.tsx` | Top-of-table action row |
| `formatBytes()`, `formatDuration()`, `formatNumber()` | `web/src/lib/format.ts` | Bytes/duration rendering |
| `formatCDRTimestamp()` (NEW) | `web/src/lib/time.ts` | Timestamp formatting (D8) |
| `<LineChart>`, `<XAxis>`, `<YAxis>`, `<Tooltip>`, `<Line>`, `<ResponsiveContainer>` | `recharts` | Timeline chart |

---

## Prerequisites

- [x] FIX-207 DONE — CDR data integrity (no orphan rows for exports)
- [x] FIX-208 DONE — Aggregates facade exists (extend, don't create)
- [x] FIX-212 DONE — envelope types available (not consumed directly in this story but context)
- [x] FIX-213 DONE — entity clickable pattern (`EntityLink`) proven
- [x] `recharts@^3.8.0` installed in `web/package.json`
- [x] `<SlidePanel>`, `<TimeframeSelector>`, `<SimSearch>` exist
- [ ] Verify `GET /api/v1/sims?ids=...` support — else Task 3 adds it

---

## Tasks

### Task 1: DB Perf Audit + Conditional Index Migration
- **Files:** (maybe) `migrations/20260421000003_cdrs_explorer_indexes.up.sql`, `.down.sql`
- **Depends on:** —
- **Complexity:** **high**
- **Pattern ref:** Read `migrations/20260322000001_cdr_dedup_index.up.sql` for migration structure; `migrations/20260323000002_perf_indexes.up.sql` for IF NOT EXISTS pattern.
- **Context refs:** "Decisions D9", "Database Schema", "Bug Pattern Warnings"
- **What:** Run `EXPLAIN ANALYZE SELECT id,…,timestamp FROM cdrs WHERE tenant_id=$1 AND timestamp >= NOW()-INTERVAL '7 days' ORDER BY timestamp DESC LIMIT 51;` against a 1M-row fixture (seed or load from existing environment). If execution time > 2s (p95 target from AC-7) → author the migration above and run. Also verify `operator_id`/`sim_id`/`apn_id` predicate variants don't trigger sequential scan. Document findings in `docs/stories/fix-ui-review/FIX-214-step-log.txt`.
- **Verify:** EXPLAIN uses `Index Scan` on `cdrs_timestamp_idx` (Timescale-auto) or the new composite; query time < 2s at 7d/1M rows. Migration up + down pass `make db-migrate` roundtrip.

### Task 2: Store — extend `ListCDRParams` + new `ListBySession` + filter widen
- **Files:** Modify `internal/store/cdr.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Existing `ListByTenant` (cdr.go:175-254) is the exact shape — extend conditions.
- **Context refs:** "API Specifications", "Database Schema", "Decisions D2/D4"
- **What:**
  - Add fields to `ListCDRParams`: `APNID *uuid.UUID`, `RecordType *string`, `SessionID *uuid.UUID`.
  - Extend `ListByTenant` WHERE-builder with those three predicates.
  - Add method `ListBySession(ctx, tenantID, sessionID uuid.UUID) ([]CDR, error)` — simple `SELECT … FROM cdrs WHERE tenant_id=$1 AND session_id=$2 ORDER BY timestamp ASC`. Uses `idx_cdrs_dedup`.
  - Extend `StreamForExport` signature with the same three new params (reuse `ListCDRParams` or a shared `CDRFilter` struct — choose the struct to keep signature clean). Propagate into WHERE clause.
- **Verify:** `go build ./…` clean. Unit test in `internal/store/cdr_test.go` covers `ListBySession` returning 0 rows for wrong tenant + correct rows when scoped; extended filter round-trip tested. No changes to existing passing tests.

### Task 3: Aggregates Facade — CDR methods
- **Files:** Modify `internal/analytics/aggregates/service.go`, `internal/analytics/aggregates/cache.go`, `internal/analytics/aggregates/invalidator.go`. Add `internal/store/cdr.go` helpers.
- **Depends on:** Task 2
- **Complexity:** **high**
- **Pattern ref:** Existing `TrafficByOperator` method (service.go) + its cache wrapping.
- **Context refs:** "Decisions D5", "API Specifications — GET /stats", "Bug Pattern Warnings — PAT-012"
- **What:**
  - Introduce `type CDRStatsFilter struct { From, To time.Time; SimID, OperatorID, APNID, SessionID *uuid.UUID; RecordType *string }`.
  - Add to `Aggregates` interface: `CDRStatsInWindow(ctx, tenantID, CDRStatsFilter) (*CDRStats, error)` returning `{ TotalCount, TotalBytesIn, TotalBytesOut, UniqueSims, UniqueSessions }`. One method, one envelope (matches the single `/stats` endpoint) — simpler than 4 separate methods.
  - DB implementation: single query `SELECT COUNT(*), SUM(bytes_in), SUM(bytes_out), COUNT(DISTINCT sim_id), COUNT(DISTINCT session_id) FROM cdrs WHERE tenant_id=$1 AND timestamp BETWEEN $2 AND $3 [+ filter predicates]`. **Do NOT** use `cdrs_daily` aggregate — filters (operator/APN/record_type/session/sim) need raw rows, and the window is bounded ≤ 30d so perf is acceptable with the timestamp index.
  - Cache wrapper: `cache.go` key `agg:cdr_stats:<tenant>:<filter_hash>`, TTL 30s. Invalidate on subject `argus.events.cdrs.inserted` (already published by cdr consumer per `internal/analytics/cdr/consumer.go`).
- **Verify:** `go test ./internal/analytics/aggregates/...` passes; new test covers filter variants. PAT-012 check: query matches the list endpoint's WHERE clause for identical numbers.

### Task 4: Handler — List filter widen + 30d cap + required range
- **Files:** Modify `internal/api/cdr/handler.go`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Existing `Handler.List` (handler.go:91-162).
- **Context refs:** "API Specifications — GET /api/v1/cdrs", "Decisions D6/D7/D2"
- **What:**
  - Parse new query params: `apn_id` (UUID), `record_type` (string, whitelist validate), `session_id` (UUID).
  - Add 422 if `from`/`to` missing.
  - `MaxCDRQueryRange = 30 * 24 * time.Hour` constant. Reject `to - from > cap` for non-admin roles (read role from `r.Context()` per existing RBAC middleware).
  - Pass new params to `store.ListCDRParams`.
- **Verify:** `go build` clean; `go test ./internal/api/cdr/...` — new tests cover 422 for missing range, 422 for > 30d as analyst, 200 for admin with 60d, new filter echoes in query results. `handler_test.go` pattern established.

### Task 5: Handler — Session timeline + stats endpoints
- **Files:** Modify `internal/api/cdr/handler.go`, `internal/gateway/router.go`, `cmd/argus/main.go` (wire Aggregates dep)
- **Depends on:** Tasks 2, 3, 4
- **Complexity:** **high**
- **Pattern ref:** `handler.go` existing handlers; `router.go:605-611` existing routes.
- **Context refs:** "API Specifications — /by-session, /stats", "Decisions D4/D5", "Data Flow"
- **What:**
  - New handler `ListBySession(w,r)` reading `chi.URLParam("session_id")`, parsing UUID, calling `CDRStore.ListBySession`, returning `{ session_id, cdrs: cdrDTO[] }`.
  - New handler `Stats(w,r)` parsing same filter params as List, calling `aggregates.CDRStatsInWindow`, returning `CDRStats` DTO.
  - Wire `Aggregates` into `Handler` constructor (add param); update `main.go` wiring.
  - Register routes in `router.go`: `r.Get("/api/v1/cdrs/by-session/{session_id}", deps.CDRHandler.ListBySession)` and `r.Get("/api/v1/cdrs/stats", deps.CDRHandler.Stats)` — inside the existing auth group.
- **Verify:** `go build ./…`. Handler tests for both endpoints: tenant-scoping (wrong tenant → 404), happy path, filter validation. `curl` smoke (via make test or manual).

### Task 6: Export — extend job payload + filter passthrough
- **Files:** Modify `internal/api/cdr/handler.go` (Export method), `internal/job/cdr_export.go`, `internal/store/cdr.go` (StreamForExport)
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Existing `cdr_export.go` + `StreamForExport`.
- **Context refs:** "API Specifications — POST /export", "Decisions D10"
- **What:**
  - Extend `exportRequest` struct with: `SimID`, `APNID`, `RecordType`, `SessionID` (all pointer, all optional).
  - Extend `cdrExportPayload` in `cdr_export.go` identically; propagate into `StreamForExport` filter.
  - `StreamForExport` signature change: accept a single `CDRFilter` struct instead of ad-hoc args (cleanup). Existing callers in `s3_archival.go` etc. updated to wrap in struct.
- **Verify:** `go build ./…`. Existing `cdr_export_test.go` still passes; new test covers filter passthrough — exported file matches visible list for same filter.

### Task 7: FE hook — `use-cdrs.ts`
- **Files:** Create `web/src/hooks/use-cdrs.ts`, `web/src/types/cdr.ts` (new DTO types)
- **Depends on:** Tasks 4, 5
- **Complexity:** medium
- **Pattern ref:** Read `web/src/hooks/use-sessions.ts` — follow infinite query + detail query pattern.
- **Context refs:** "API Specifications", "Decisions D13", "Data Flow"
- **What:**
  - Types: `CDRRow { id, session_id, sim_id, operator_id, apn_id?, rat_type?, record_type, bytes_in, bytes_out, duration_sec, timestamp }` + `CDRStats { total_count, total_bytes_in, total_bytes_out, unique_sims, unique_sessions }` + `CDRFilter { from, to, sim_id?, operator_id?, apn_id?, record_type?, session_id? }` + `SessionTimeline { session_id, cdrs: CDRRow[] }`.
  - Hooks: `useCDRList(filter)` → `useInfiniteQuery`, page size 50, cursor pagination via `meta.cursor`/`meta.has_more`. `useCDRStats(filter)` → `useQuery`, staleTime 30s. `useCDRSessionTimeline(sessionId)` → `useQuery`, enabled on truthy session id. `useSimBatch(simIds)` → `useQuery(['sims','batch',ids.sorted().join(',')])`, staleTime 60s, disabled on empty list.
  - `useCDRExport()` → `useMutation` posting `/cdrs/export`, returns `{ jobId }` on success.
- **Verify:** `npm --prefix web run typecheck` clean. No runtime tests (project FE convention).

### Task 8: FE page — `/cdrs` index
- **Files:** Create `web/src/pages/cdrs/index.tsx`
- **Depends on:** Task 7
- **Complexity:** **high**
- **Pattern ref:** Read `web/src/pages/sessions/index.tsx` — follow the exact shell, stat cards, table, row click, filter toolbar composition.
- **Context refs:** "Screen Mockup — /cdrs list", "Design Token Map", "Decisions D1/D2/D3/D7/D8/D12/D13/D16/D17", "Data Flow"
- **What:**
  - URL-synced filter state via `useSearchParams`.
  - Filter bar: `SimSearch` (sim_id), operator `Select` (populated by `useOperators()`), APN `Select` (populated by `useApns()`), record_type chip group (multi-UI but sends first via D16), `TimeframeSelector` + custom range `[from, to]` for admin override.
  - 4 stat cards at top (from `useCDRStats`): total count, total bytes (in+out), unique sims, unique sessions. Skeleton while loading.
  - Table: 8 columns per mockup. Mono font for ICCID/IMSI/IP/timestamp cells. Row hover `bg-bg-hover`. EntityLink for SIM/operator/APN. record_type Badge.
  - Load-more pagination button (reuse existing `useInfiniteQuery` fetchNextPage pattern).
  - Row click → `setActiveSessionId(row.session_id)` → opens `SessionTimelineDrawer` (Task 9).
  - Empty state: `EmptyState` component with "Bu filtre için CDR bulunamadı" + "Filtreleri Temizle" CTA.
  - Export button → `useCDRExport.mutate({ …filters, format:'csv' })` → on success, show toast "Rapor kuyruğa alındı. İlerleme için /reports." using existing toast system.
  - Deep-link: on mount, read `session_id` URL param → auto-apply filter.
- **Tokens:** Use ONLY classes from Design Token Map. Zero hardcoded hex/px.
- **Components:** Reuse all from Components to REUSE table.
- **Note:** Invoke `frontend-design` skill for row styling + stat card tuning.
- **Verify:** `grep -rn '#[0-9a-fA-F]\{3,6\}' web/src/pages/cdrs/` → ZERO matches. Manual browser: filter combos, row click, empty state, error state, export button.

### Task 9: FE drawer — Session Timeline
- **Files:** Create `web/src/pages/cdrs/session-timeline.tsx`
- **Depends on:** Tasks 7, 8
- **Complexity:** medium
- **Pattern ref:** Read `web/src/components/ui/slide-panel.tsx` for API; for chart reference check existing recharts use at `web/src/pages/dashboard/analytics.tsx` or `web/src/pages/sims/detail.tsx` (whichever exists).
- **Context refs:** "Screen Mockup — Session Timeline Drawer", "Design Token Map", "Decisions D14/D15"
- **What:**
  - Props: `{ open, onOpenChange, sessionId?: string }`.
  - `useCDRSessionTimeline(sessionId)` loads `{cdrs[]}`. Compute `cumulativeBytes = cdrs.reduce((acc,c)=>acc + c.bytes_in + c.bytes_out, 0)` per row for chart series.
  - Layout: metadata card (session_id copyable, sim link, operator link, apn, rat, duration) → Recharts `<ResponsiveContainer height={240}><LineChart>…</LineChart></ResponsiveContainer>` → bytes-delta table.
  - Delta computation: `delta_in = row.bytes_in - prev.bytes_in` (min 0; handles counter reset on stop). Same for delta_out.
  - Footer: `<SlidePanelFooter>` with "Oturum detayına git →" button → `navigate('/sessions/'+sessionId)`.
- **Tokens:** Same discipline as Task 8. Chart colors via CSS vars: line = `var(--accent)`, grid = `var(--border-subtle)`.
- **Verify:** Manual browser: open drawer, chart renders, deltas correct, metadata correct, go-to-session works. `npm typecheck` clean.

### Task 10: Router + Sidebar + Session Detail deep-link
- **Files:** Modify `web/src/router.tsx`, `web/src/components/layout/sidebar.tsx`, `web/src/pages/sessions/detail.tsx`
- **Depends on:** Task 8
- **Complexity:** low
- **Pattern ref:** Existing sidebar item format (sidebar.tsx:88-99), router lazy-load pattern (router.tsx:15-30).
- **Context refs:** "Decisions D1/D12", "Architecture Context — Components Involved"
- **What:**
  - `router.tsx`: add `const CDRExplorerPage = lazy(() => import('@/pages/cdrs/index'))` and `{ path: '/cdrs', element: lazySuspense(CDRExplorerPage) }` inside the MANAGEMENT/protected group.
  - `sidebar.tsx`: add `{ label: 'CDRs', icon: FileBarChart, path: '/cdrs', minRole: 'analyst' }` positioned AFTER the `Sessions` item and BEFORE `Policies`. Import `FileBarChart` if not already.
  - `pages/sessions/detail.tsx`: add button "Explore CDRs" in the session metadata header section, `onClick = navigate('/cdrs?session_id='+s.id+'&from='+s.started_at+'&to='+(s.ended_at ?? new Date().toISOString()))`.
- **Verify:** `npm run build` clean. Manual: Sidebar nav shows CDRs under Sessions, route loads, session-detail button deep-links with correct query.

### Task 11: FE util — `web/src/lib/time.ts` (timestamp helper)
- **Files:** Create `web/src/lib/time.ts`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `web/src/lib/format.ts` — follow same pure-function style.
- **Context refs:** "Decisions D8", "Bug Pattern Warnings"
- **What:** Export `formatCDRTimestamp(iso: string): string` — uses `new Intl.DateTimeFormat('tr-TR', { timeZone: 'Europe/Istanbul', year:'numeric', month:'2-digit', day:'2-digit', hour:'2-digit', minute:'2-digit', second:'2-digit' }).format(new Date(iso))` + a trailing `"TR"` marker. Export `formatCDRTimestampShort(iso)` without date (HH:mm:ss TR) for drawer rows.
- **Verify:** `npm typecheck` clean. Manual: verify displayed time matches Istanbul wall-clock for a known UTC timestamp.

### Task 12: Backend tests — handler + store + aggregates
- **Files:** Modify `internal/api/cdr/handler_test.go`, `internal/store/cdr_test.go`, `internal/analytics/aggregates/service_test.go` (or `integration_test.go`)
- **Depends on:** Tasks 2, 3, 4, 5, 6
- **Complexity:** medium
- **Pattern ref:** Existing `handler_test.go` + `cdr_test.go` structures.
- **Context refs:** "Acceptance Criteria Mapping", "API Specifications", "Decisions D6/D7"
- **What:** Cover AC scenarios:
  - AC-2: list with each filter combination returns correct rows
  - AC-3: cursor pagination across 3 pages, limit=2
  - AC-4: `ListBySession` returns rows ordered ASC, excludes other sessions, 404 cross-tenant
  - AC-5: export POST with full filter set → job payload contains all params
  - AC-6: `CDRStatsInWindow` totals match list sum for same filter (PAT-012 consistency check)
  - AC-7: (documentation test) add a perf-gate comment + a benchmark stub `BenchmarkListByTenant_7d_1M` — skipped on CI but runnable manually
  - 422 on missing date range, > 30d range, invalid UUID
- **Verify:** `make test` green. All tests pass including new assertions.

---

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|---|---|---|
| AC-1 — Page with table (ICCID/IMSI/MSISDN/Operator/APN/Record Type/Bytes/Session/Timestamp) | Task 8 | Manual browser; Task 12 handler tests confirm DTO shape |
| AC-2 — Filter bar (SIM search, operator, APN, record_type, date range required) | Tasks 4, 8 | Task 12 (filter passthrough), manual |
| AC-3 — Server-side cursor pagination (default 50, max 100) | Tasks 2, 4 | Task 12 pagination test |
| AC-4 — Row click → drawer with session timeline | Tasks 5, 9 | Task 12 `/by-session` test + manual |
| AC-5 — Export CSV via report job | Task 6 | Task 12 export test |
| AC-6 — Aggregate stats card | Tasks 3, 5, 8 | Task 12 aggregate-matches-list-sum test (PAT-012) |
| AC-7 — p95 < 2s for 7d / 1M rows | Tasks 1, 2 | EXPLAIN ANALYZE in Task 1 step log; benchmark stub in Task 12 |
| AC-8 — Session Detail → CDR Explorer deep link | Task 10 | Manual browser click-through |
| AC-9 — Sidebar entry between Sessions and Policies | Task 10 | Manual sidebar inspection |

---

## Story-Specific Compliance Rules

- **API**: All new endpoints use standard envelope `{ status, data, meta?, error? }`. Standard error codes: `VALIDATION_ERROR`, `INVALID_FORMAT`, `FORBIDDEN`, `INTERNAL_ERROR`.
- **DB**: Migrations are additive-only (index creation with IF NOT EXISTS); no column changes. Both `up` and `down` required. **Only ship migration if Task 1 justifies it.**
- **Tenant Scoping**: Every query filters by `tenant_id = ctx.tenant_id`. `ListBySession` returns 404 (not row) on cross-tenant ID.
- **Cursor pagination**: `(timestamp DESC, id DESC)` cursor format `"<id>"` (reuse existing pattern). Never use OFFSET.
- **UI Tokens**: Only classes from Design Token Map; zero hardcoded hex/px. Grep gate in Task 8 Verify.
- **Entity Clickability**: EntityLink for every ICCID/IMSI/MSISDN/Operator/APN cell.
- **FE Turkish**: Labels/buttons/headers Turkish; technical identifiers English.
- **Timezone**: All server times UTC; display via `formatCDRTimestamp` (D8).
- **Aggregates**: Stats card values ONLY via `aggregates` facade (PAT-012).
- **Severity** (if showing warnings for range > 30d): use FIX-211 5-level taxonomy (info/low/medium/high/critical).
- **ADRs**: ADR-001 (modular monolith — handlers in `internal/api/cdr`), ADR-002 (tenant scoping), ADR-003 (audit every state change — export triggers audit event `cdr.export`, already in place).

---

## Bug Pattern Warnings

Read `docs/brainstorming/bug-patterns.md` before implementation.

- **PAT-009 (FIX-204) — Nullable FK in aggregations**: `cdrs.apn_id` is nullable. `COUNT(DISTINCT apn_id)` silently excludes NULLs; don't claim "unique APNs" in stats (not in AC-6, but double-check if you add it).
- **PAT-012 (FIX-208) — Cross-surface count drift**: the stats card numbers MUST match the list endpoint's filtered count for the same filter. Task 3 + Task 12's "aggregate-matches-list-sum" test guards against this. Never SELECT COUNT in the handler.
- **PAT-013 (FIX-211) — Enum constraint normalization**: if you add a check constraint on `record_type` (not in scope, mentioned for awareness), use `= ANY(ARRAY['start','interim','stop',…])` form; Postgres will canonicalize `IN (...)`.
- **PAT-015 (FIX-209) — Dead-code UI**: Task 8 page must be mounted via router (Task 10). Task 10's Verify step checks via manual browser walk.
- **PAT-016 (FIX-209) — PK ID confusion**: `cdrs.id` is `BIGSERIAL` (int), `cdrs.session_id` is UUID. Don't mix in cursor or deep links.
- **PAT-017 (FIX-210) — Config threaded to store but not used**: if Task 3 adds a cache TTL config, verify the cached path actually reads it (not just accepts the option).
- **Timezone drift (existing PAT in cdr.go:GetTrafficHeatmap7x24 comment)**: UTC on server, Europe/Istanbul on display. Centralize in `lib/time.ts`.

---

## Tech Debt (from ROUTEMAP)

No Tech Debt items target this story directly. **Proposed new tech-debt** (record in Gate):
- D-214-001 — Single-select operator/APN filter (D16): add true multi-select backend support + UI in a P2 follow-up.

## Mock Retirement

No mocks directory in project. N/A.

---

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| R1 — Hypertable full scan on filter absent date range | D6+D7 make range required; handler returns 422. Task 12 tests the validator. |
| R2 — Perf regression on insert from new indexes | Task 1 audit gates the migration — only ship if list query is genuinely slow. Composite indexes (tenant_id, timestamp DESC, operator_id) add minor insert cost but don't block writes. |
| R3 — Stats card drift from list (PAT-012) | Task 3 uses identical filter predicates as list; Task 12 cross-checks the numbers. |
| R4 — SIM batch lookup N+1 if page size grows | `useSimBatch` is called once per page (not per row); TanStack Query dedupes identical id sets across pages. |
| R5 — FIX-248 reshape of `cdr_export` job | Coordination note: keep job type name `cdr_export` stable; result envelope will be harmonized by FIX-248. This plan is FIX-248-compatible. |
| R6 — CSV export memory blow-up for large sets | AC-5 delegates to streaming job (`CDRExportProcessor`). In-handler inline stream path (`/export.csv`) unchanged — analyst-use only, narrow audience. Job-based path scales. |
| R7 — Timezone drift from UTC↔Istanbul | Centralize in `lib/time.ts` (Task 11). Grep check: no inline `toLocaleString` without `timeZone:'Europe/Istanbul'`. |
| R8 — D16 single-select vs story "multi-select" | Explicit tech-debt D-214-001 + Gate note. First-iteration covers 80% of user need. |
| R9 — `/api/v1/sims?ids=` may not exist | Task 7 Prerequisites check. If missing, add to SIM handler as 1-file extension (not separate task — scope creep manageable). |
| R10 — EXPLAIN ANALYZE on prod-scale fixture | Seed a dev tenant with 1M synthetic CDRs (script in Task 1 step log); or use existing canary/staging environment. Do not test on prod. |

---

## Summary

L story, 12 tasks, 3 marked `high` (Tasks 1, 3, 5, 8). Wave structure:
- **Wave 1 (serial)**: Task 1 (DB audit)
- **Wave 2 (parallel)**: Task 2 (store) + Task 11 (FE time helper)
- **Wave 3 (parallel)**: Task 3 (aggregates) + Task 4 (handler filter widen) + Task 6 (export extend)
- **Wave 4 (serial)**: Task 5 (new endpoints wired — needs Task 3)
- **Wave 5 (parallel)**: Task 7 (FE hook) + Task 12 (backend tests)
- **Wave 6 (serial)**: Task 8 (FE page)
- **Wave 7 (parallel)**: Task 9 (drawer) + Task 10 (router/sidebar/session-detail)

New endpoints: 2 (`GET /cdrs/by-session/{id}`, `GET /cdrs/stats`). Endpoint extensions: 2 (`GET /cdrs`, `POST /cdrs/export`). New migration: 1 (conditional). New FE pages: 2 (`/cdrs`, SlidePanel drawer). New FE hooks: 4 in one file. Sidebar + router + session-detail updated.

Pre-Validation: all checks pass (≥5 tasks, ≥1 high complexity, API/DB/UI specs embedded, context refs map to existing sections, patterns referenced per task, tests map to ACs).
