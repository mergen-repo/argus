# Implementation Plan: STORY-095 — IMEI Pool Management

## Goal
Land tenant-scoped IMEI pool tables (TBL-56 whitelist / TBL-57 greylist / TBL-58 blacklist), the five pool endpoints (API-331..335) including bulk CSV import via SVC-09 async job, the SCR-196 management page (4 tabs) + SCR-197 lookup modal/drawer, and replace STORY-094's `device.imei_in_pool` placeholder evaluator with a real lookup that applies both exact full-IMEI and TAC-range matching with a per-evaluation-pass cache.

## Architecture Context

### Components Involved
- **SVC-03 Core API** — `internal/api/imei_pool/handler.go` (NEW), `internal/api/imei_pool/bulk_handler.go` (NEW), `internal/api/imei_pool/lookup_handler.go` (NEW)
- **SVC-05 Policy Engine** — `internal/policy/dsl/evaluator.go` (replace `device.imei_in_pool(...)` placeholder with `IMEIPoolStore.Lookup` + per-evaluation cache)
- **SVC-09 Job system** — new processor `internal/job/imei_pool_import_worker.go` (mirrors `sim_bulk_device_binding_worker.go` shape from STORY-094)
- **store layer** — `internal/store/imei_pool.go` (NEW; one struct, three table-targeted method families)
- **migrations/** — single migration pair `20260508000001_imei_pools.{up,down}.sql` (all three tables in one file — shared shape, single round-trip)
- **gateway/router.go** — register API-331..335 routes under `/api/v1/imei-pools/...` with role gates
- **frontend** — `web/src/pages/settings/imei-pools/{index.tsx,whitelist-tab.tsx,greylist-tab.tsx,blacklist-tab.tsx,bulk-import-tab.tsx,add-entry-dialog.tsx,lookup-dialog.tsx,lookup-drawer.tsx}` (NEW), `web/src/hooks/use-imei-pools.ts` (NEW)

### Data Flow

```
GET /api/v1/imei-pools/{kind}?include_bound_count=1
  → ImeiPoolHandler.List
  → IMEIPoolStore.List(ctx, tenantID, kind, params)
  → optional bound_sims_count subquery against sims (full_imei: bound_imei = imei_or_tac;
                                                     tac_range: SUBSTRING(bound_imei,1,8) = imei_or_tac)
  → cursor envelope + meta

POST /api/v1/imei-pools/{kind}
  → ImeiPoolHandler.Add (validate kind/length/required-by-list + CSV-injection sanitize)
  → IMEIPoolStore.Add (UNIQUE violation → ErrPoolEntryDuplicate → 409)
  → audit.CreateEntry("imei_pool.entry_added", entity_type="imei_pool_entry",
                      before=null, after={kind,imei_or_tac,list:<kind>}, hash-chained)
  → 201 + envelope

DELETE /api/v1/imei-pools/{kind}/{id}
  → ImeiPoolHandler.Delete (tenant-scoped)
  → IMEIPoolStore.Delete → 204; cross-tenant → 404 POOL_ENTRY_NOT_FOUND
  → audit "imei_pool.entry_removed"

POST /api/v1/imei-pools/{kind}/import (multipart file=<csv>)
  → ImeiPoolBulkHandler.Import (max 10 MB / 100 000 rows)
  → header parse + per-row CSV-injection sanitize (=,+,-,@,\t prefix)
  → JobStore.Create + bus.Publish(SubjectJobQueue, JobMessage{Type=JobTypeBulkIMEIPoolImport})
  → 202 {job_id}
  → worker iterates rows: validate kind/length/required → IMEIPoolStore.Add (per row)
                          → on UNIQUE conflict accumulate row-level error
                          → progress + cancellation polling per bulkBatchSize
                          → final job.Complete(error_report, result_summary)
  → audit "imei_pool.bulk_imported" once on completion

GET /api/v1/imei-pools/lookup?imei=<15 digits>
  → ImeiPoolLookupHandler.Get (validate 15-digit)
  → IMEIPoolStore.Lookup(ctx, tenantID, imei) → list-of-(kind, entry_id, matched_via)
       — exact match: imei_or_tac = $imei AND kind='full_imei'
       — tac match: imei_or_tac = SUBSTR($imei,1,8) AND kind='tac_range'
       UNION across all three tables in a single query per table (3 queries total)
  → SIMStore.ListByBoundIMEI(ctx, tenantID, imei) → bound_sims slice
  → IMEIHistoryStore.ListByObservedIMEI(ctx, tenantID, imei, sinceLast30Days, limit50) → history
  → 200 envelope with {lists, bound_sims, history}

DSL evaluation (NOW functional — replaces placeholder):
  WHEN device.imei_in_pool('blacklist') THEN reject
  → evaluator.getConditionFieldValue dispatches "device.imei_in_pool(<pool>)"
  → consults per-evaluation cache (poolCache map[string]bool keyed by pool_name)
  → on miss: IMEIPoolStore.LookupKind(ctx, tenantID, ctx.IMEI, pool) → bool
  → cache write
  → repeated WHEN clauses for same pool_name in same Evaluate() pass → cache hit, zero extra queries
```

### API Specifications

All endpoints use envelope `{ status, data, meta?, error? }`. JWT auth via existing middleware. Tenant scoped via `apierr.TenantIDKey` context.

#### API-331: GET /api/v1/imei-pools/{kind}
- Path param: `kind` ∈ `{whitelist, greylist, blacklist}`
- Query: `cursor`, `limit` (default 50, max 200), `tac` (8-digit prefix filter), `imei` (exact-15 filter), `device_model` (ILIKE), `include_bound_count` (`1`/`0`)
- 200:
  ```json
  { "status":"success",
    "data":[{
      "id":"<uuid>", "kind":"full_imei|tac_range", "imei_or_tac":"35921108",
      "device_model":"Quectel BG95"|null, "description":"..."|null,
      "quarantine_reason":"..."|null,        // greylist only
      "block_reason":"..."|null,             // blacklist only
      "imported_from":"manual|gsma_ceir|operator_eir"|null,  // blacklist only
      "bound_sims_count": 12403,             // present only when include_bound_count=1
      "created_at":"...","updated_at":"...","created_by":"<uuid>"|null
    }],
    "meta":{"next_cursor":"...","limit":50,"has_more":true}
  }
  ```
- 400 `INVALID_PARAM` on bad kind / cursor; 403 `INSUFFICIENT_PERMISSIONS` if role < sim_manager.

#### API-332: POST /api/v1/imei-pools/{kind}
- Body:
  ```json
  { "kind":"full_imei|tac_range",
    "imei_or_tac":"<15 or 8 digits>",
    "device_model":"..."|null, "description":"..."|null,
    "quarantine_reason":"..."|null,    // required when {kind path}=greylist
    "block_reason":"..."|null,         // required when {kind path}=blacklist
    "imported_from":"manual|gsma_ceir|operator_eir"|null  // required when {kind path}=blacklist
  }
  ```
- Validation:
  - `kind=full_imei` → `imei_or_tac` MUST be 15 digits (regex `^[0-9]{15}$`)
  - `kind=tac_range` → `imei_or_tac` MUST be 8 digits (regex `^[0-9]{8}$`)
  - greylist requires `quarantine_reason` non-empty after trim
  - blacklist requires `block_reason` non-empty AND `imported_from` ∈ enum
  - all string fields run through CSV-injection sanitization (see Story-Specific Compliance Rules)
- 201 + DTO matching API-331 row shape.
- 422 `VALIDATION_ERROR` for shape/length/missing-required violations.
- 409 `IMEI_POOL_DUPLICATE` on UNIQUE (tenant_id, imei_or_tac) violation (returned for any of the three tables when the same combo already exists).
- Audit: `imei_pool.entry_added` (entity_type=`imei_pool_entry`, entity_id=`<id>`, before=null, after=row JSON).

#### API-333: DELETE /api/v1/imei-pools/{kind}/{id}
- 204 on success.
- 404 `POOL_ENTRY_NOT_FOUND` cross-tenant or non-existent (no leak).
- Audit: `imei_pool.entry_removed` (before=row JSON, after=null).

#### API-334: POST /api/v1/imei-pools/{kind}/import
- Multipart `file=<csv>` (max 10 MB; max 100 000 rows; reject `.txt` / non-`.csv` extensions).
- CSV header (case-insensitive): `imei_or_tac, kind, device_model, description, quarantine_reason, block_reason, imported_from`
- 202 `{ "status":"success","data":{"job_id":"<uuid>"} }`
- Async via SVC-09 (`JobTypeBulkIMEIPoolImport`); per-row outcomes (`success` / `invalid_kind` / `invalid_length` / `missing_required` / `duplicate` / `csv_injection_rejected` / `store_error`) accumulate in job result. Reuse existing `GET /api/v1/jobs/{id}/errors` endpoint for error CSV download — DO NOT add new endpoints.
- Audit: `imei_pool.bulk_imported` once on job completion (before=null, after=`{job_id, processed_count, failed_count, total_count, list:<kind>}`).

#### API-335: GET /api/v1/imei-pools/lookup?imei=<imei>
- Query: `imei` (REQUIRED, 15 digits) — TAC-only lookup is supported via SCR-197 client-side branching but the API requires the full 15-digit form to do exact + TAC-range simultaneously. (8-digit-only path NOT exposed in this story; SCR-197 8-digit input falls back to client-side filter against API-331 with `?tac=`.)
- 200:
  ```json
  { "status":"success", "data":{
    "imei":"359211089765432", "tac":"35921108",
    "lists":[
      {"kind":"whitelist","entry_id":"<uuid>","matched_via":"tac_range"},
      {"kind":"blacklist","entry_id":"<uuid>","matched_via":"exact"}
    ],
    "bound_sims":[
      {"sim_id":"<uuid>","iccid":"8990...","binding_mode":"strict"|null,
       "binding_status":"verified"|"pending"|"mismatch"|"unbound"|"disabled"|null}
    ],
    "history":[
      {"id":"<uuid>","sim_id":"<uuid>","observed_at":"...","capture_protocol":"radius|diameter_s6a|5g_sba",
       "iccid":"8990...","nas_ip_address":"..."|null}
    ]
  }}
  ```
- Empty arrays (NOT 404) when nothing matches.
- 422 `INVALID_IMEI` when `imei` is missing OR not exactly 15 digits.
- `history` = last 30 days of `imei_history` rows where `observed_imei = $imei` for any SIM in the same tenant, max 50, ORDER BY `observed_at DESC, id DESC`.

### Database Schema

> **Source: ARCHITECTURE.md `db/_index.md` TBL-56/57/58 (DESIGN — no migration yet). STORY-095 creates these tables.**
> RLS variable is `app.current_tenant` per landed `migrations/20260507000002_imei_history.up.sql:21` (NOT `app.current_tenant_id` — confirmed by Read against the actual file).

**Migration — `20260508000001_imei_pools.up.sql` (creates TBL-56, TBL-57, TBL-58 in one file):**

```sql
-- TBL-56: imei_whitelist
CREATE TABLE IF NOT EXISTS imei_whitelist (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kind VARCHAR(15) NOT NULL CHECK (kind IN ('full_imei','tac_range')),
  imei_or_tac VARCHAR(15) NOT NULL,
  device_model VARCHAR(255) NULL,
  description TEXT NULL,
  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT imei_whitelist_unique_entry UNIQUE (tenant_id, imei_or_tac)
);
CREATE INDEX IF NOT EXISTS idx_imei_whitelist_tenant_kind ON imei_whitelist (tenant_id, kind);
ALTER TABLE imei_whitelist ENABLE ROW LEVEL SECURITY;
CREATE POLICY imei_whitelist_tenant_isolation ON imei_whitelist
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- TBL-57: imei_greylist (adds quarantine_reason)
CREATE TABLE IF NOT EXISTS imei_greylist (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kind VARCHAR(15) NOT NULL CHECK (kind IN ('full_imei','tac_range')),
  imei_or_tac VARCHAR(15) NOT NULL,
  device_model VARCHAR(255) NULL,
  description TEXT NULL,
  quarantine_reason TEXT NOT NULL,
  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT imei_greylist_unique_entry UNIQUE (tenant_id, imei_or_tac)
);
CREATE INDEX IF NOT EXISTS idx_imei_greylist_tenant_kind ON imei_greylist (tenant_id, kind);
ALTER TABLE imei_greylist ENABLE ROW LEVEL SECURITY;
CREATE POLICY imei_greylist_tenant_isolation ON imei_greylist
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- TBL-58: imei_blacklist (adds block_reason + imported_from)
CREATE TABLE IF NOT EXISTS imei_blacklist (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kind VARCHAR(15) NOT NULL CHECK (kind IN ('full_imei','tac_range')),
  imei_or_tac VARCHAR(15) NOT NULL,
  device_model VARCHAR(255) NULL,
  description TEXT NULL,
  block_reason TEXT NOT NULL,
  imported_from VARCHAR(20) NOT NULL
    CHECK (imported_from IN ('manual','gsma_ceir','operator_eir')),
  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT imei_blacklist_unique_entry UNIQUE (tenant_id, imei_or_tac)
);
CREATE INDEX IF NOT EXISTS idx_imei_blacklist_tenant_kind ON imei_blacklist (tenant_id, kind);
ALTER TABLE imei_blacklist ENABLE ROW LEVEL SECURITY;
CREATE POLICY imei_blacklist_tenant_isolation ON imei_blacklist
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);
```

**Down migration — `20260508000001_imei_pools.down.sql`:**

```sql
DROP POLICY IF EXISTS imei_blacklist_tenant_isolation ON imei_blacklist;
DROP POLICY IF EXISTS imei_greylist_tenant_isolation ON imei_greylist;
DROP POLICY IF EXISTS imei_whitelist_tenant_isolation ON imei_whitelist;
DROP TABLE IF EXISTS imei_blacklist;
DROP TABLE IF EXISTS imei_greylist;
DROP TABLE IF EXISTS imei_whitelist;
```

> **PAT-022 discipline:** the Go const sets for `kind` (`full_imei`, `tac_range`) and `imported_from` (`manual`, `gsma_ceir`, `operator_eir`) MUST equal the SQL CHECK enums. Test `TestIMEIPoolEnumsMatchCheckConstraints` (no-DB; re-parses migration SQL) ships with the store package — mirrors `internal/store/sim_binding_consts_test.go`.

### Store Surface

`internal/store/imei_pool.go` (NEW — single store, three-table-aware methods):

```
type PoolKind string  // "whitelist" | "greylist" | "blacklist"

type IMEIPoolEntry struct {
    ID, TenantID         uuid.UUID
    Kind                 string  // "full_imei" | "tac_range"
    IMEIOrTAC            string
    DeviceModel          *string
    Description          *string
    QuarantineReason     *string  // greylist only (NULL elsewhere)
    BlockReason          *string  // blacklist only
    ImportedFrom         *string  // blacklist only
    CreatedBy            *uuid.UUID
    CreatedAt, UpdatedAt time.Time
    BoundSIMsCount       *int     // populated only when caller passes IncludeBoundCount=true
}

type LookupResult struct { Kind, EntryID, MatchedVia string }  // "exact" | "tac_range"

type IMEIPoolStore struct { pool *pgxpool.Pool; sims *SIMStore }

NewIMEIPoolStore(pool *pgxpool.Pool, sims *SIMStore) *IMEIPoolStore

// Methods (each dispatches to the table named by `pool` arg):
List(ctx, tenantID, pool PoolKind, params ListIMEIPoolParams) ([]IMEIPoolEntry, nextCursor string, err error)
Add(ctx, tenantID uuid.UUID, pool PoolKind, in AddIMEIPoolEntryParams) (*IMEIPoolEntry, error)  // returns ErrPoolEntryDuplicate on UNIQUE
Get(ctx, tenantID, id uuid.UUID, pool PoolKind) (*IMEIPoolEntry, error)
Delete(ctx, tenantID, id uuid.UUID, pool PoolKind) error  // ErrPoolEntryNotFound

// Lookup queries all 3 tables for an IMEI:
Lookup(ctx, tenantID uuid.UUID, imei string) ([]LookupResult, error)
// LookupKind queries ONE table — used by DSL evaluator with cache:
LookupKind(ctx, tenantID uuid.UUID, imei string, pool PoolKind) (bool, error)
```

`ListIMEIPoolParams`: `Cursor, Limit, Tac, IMEI, DeviceModel string; IncludeBoundCount bool`.
`AddIMEIPoolEntryParams`: all DTO fields + `CreatedBy *uuid.UUID`.

Errors: `ErrPoolEntryNotFound`, `ErrPoolEntryDuplicate`. Pattern after existing `internal/store/imei_history.go` errors.

`Lookup()` SQL shape (single per-table CTE that UNIONs exact + TAC):

```sql
SELECT 'whitelist' AS kind, id AS entry_id,
       CASE WHEN kind='full_imei' THEN 'exact' ELSE 'tac_range' END AS matched_via
  FROM imei_whitelist
 WHERE tenant_id = $1
   AND ((kind='full_imei' AND imei_or_tac = $2)
        OR (kind='tac_range' AND imei_or_tac = SUBSTRING($2,1,8)))
UNION ALL
... same for greylist, blacklist
```

`LookupKind()` is the same per-table query without UNION — returns `bool` (existence check). Used by DSL to keep query count = 3 max per evaluation pass even when many WHEN clauses reference the predicate.

`SIMStore.ListByBoundIMEI(ctx, tenantID, imei) ([]BoundSIMRow, error)` — NEW companion method on `SIMStore` that returns `{sim_id, iccid, binding_mode, binding_status}` for SIMs matching `bound_imei = $imei`. Tenant-scoped.

`IMEIHistoryStore.ListByObservedIMEI(ctx, tenantID, imei, since, limit)` — NEW companion method. Returns up to 50 rows where `observed_imei = $imei AND tenant_id = $tenant_id AND observed_at >= NOW() - INTERVAL '30 days'`, ORDER BY `observed_at DESC, id DESC`. JOIN to sims to surface `iccid`.

### DSL Evaluator: `device.imei_in_pool` Replacement (AC-9)

**Current (placeholder, evaluator.go:253-257):**
```go
if strings.HasPrefix(field, "device.imei_in_pool(") && strings.HasSuffix(field, ")") {
    return false  // placeholder
}
```

**Target shape:**
1. `Evaluator` struct gains an OPTIONAL `pools IMEIPoolLookuper` interface field (set via `NewEvaluatorWithPools(pools)`). When nil (existing call sites that never set it — e.g. in-memory tests), behavior reverts to `false` (preserves placeholder semantics, no crash).
2. **Per-evaluation cache:** the cache MUST NOT live on the evaluator struct (would persist across requests AND tenants). Instead, extend `SessionContext` with an unexported scratch slot `poolCache map[string]bool` that is reset to `nil` (or initialised lazily) at the top of `Evaluate()`. Cache key = pool name (`whitelist`/`greylist`/`blacklist`). Max 3 entries per pass. To preserve `SessionContext` JSON shape, the field is `json:"-"` and not exported — declared at the bottom of the struct with a `// runtime-only` comment.
   ```go
   // SessionContext additions (NOT JSON-serialised)
   poolCache map[string]bool `json:"-"`
   ```
3. Wire dispatch in `getConditionFieldValue`:
   ```go
   if strings.HasPrefix(field, "device.imei_in_pool(") && strings.HasSuffix(field, ")") {
       inner := field[len("device.imei_in_pool(") : len(field)-1]
       pool := strings.Trim(inner, `"' `)
       return e.lookupPoolCached(ctx, pool)
   }
   ```
4. `(e *Evaluator) lookupPoolCached(ctx SessionContext, pool string) bool`:
   - if `e.pools == nil` → return `false` (placeholder fallback).
   - if `len(ctx.IMEI) != 15` → return `false` (TAC alone cannot match against full_imei rows, and TAC-range matching needs 15-digit IMEI to compute prefix; spec says ctx.IMEI in 095 is the full IMEI from STORY-093 capture).
   - `ctx.poolCache` lazy-init (caller cannot mutate `ctx` because it's a value type — handled inside `Evaluate()` entry: `ctx.poolCache = map[string]bool{}` and the same `ctx` value is propagated through `evaluateRules` → `evaluateCondition` → `getConditionFieldValue`. Cache mutation is WRITES to the same map reference — Go map values are reference types, so the value-receiver method still mutates the underlying map).
   - cache hit → return cached bool.
   - cache miss → call `e.pools.LookupKind(ctx.Context, tenantUUID, ctx.IMEI, pool)`; on error log+return `false` (fail-open during evaluation; STORY-096 enforcement decides default-deny posture); cache the result; return.
5. Tenant resolution: `ctx.TenantID` is a string in `SessionContext` (line 11). Parse to UUID with `uuid.Parse(ctx.TenantID)`. On parse error → return `false`.
6. Plumbing: `cmd/argus/main.go` instantiates `imeiPoolStore := store.NewIMEIPoolStore(pg.Pool, simStore)`, threads it into `policyEvaluator := dsl.NewEvaluatorWithPools(imeiPoolStore)` everywhere `dsl.NewEvaluator()` is currently called for production paths (NOT test fixtures). Lookuper interface lives in `internal/policy/dsl/` (not `store`) — store provides the concrete impl.

**Interface declaration (`internal/policy/dsl/evaluator.go`):**
```go
type IMEIPoolLookuper interface {
    LookupKind(ctx context.Context, tenantID uuid.UUID, imei string, pool string) (bool, error)
}
```

> Bringing `context.Context` into the evaluator: today `Evaluate(ctx SessionContext, ...)` does NOT carry a `context.Context`. The store call needs one. Plan: add `context.Context` to `SessionContext` as an unexported `runtimeCtx` field (mirroring `poolCache`), populated by callers via a new `WithContext(c context.Context)` setter; default `context.Background()` when unset (test path). Avoids API breakage of `Evaluate()`.

### Screen Mockups

#### SCR-196: IMEI Pool Management (Settings tab page, 4-tab strip)

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  Settings > IMEI Pools                                     │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  [White List] [Grey List] [Black List] [Bulk Import]       │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  [TAC ▼] [Device Model ▼] [Type: full_imei|tac_range ▼]   │
│                    │  [⌘K Search]    [🔍 IMEI Lookup]    [+ Add Entry]         │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  ┌────────────────────────────────────────────────────────┐│
│                    │  │ TAC      │ Device Model    │ Type      │ Bound │ By    ││
│                    │  ├──────────┼─────────────────┼───────────┼───────┼───────┤│
│                    │  │ 35291108 │ Quectel BG95    │ tac_range │ 12,403│ Bora T││
│                    │  │ 86412 06…│ SIMCom SIM7600E │ full_imei │      1│ Selen ││
│                    │  └──────────┴─────────────────┴───────────┴───────┴───────┘│
│                    │  Showing 1-50 of 218     ◀ Prev  [1] 2 3 ... 5  Next ▶   │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

Grey List tab adds `Quarantine Reason` column. Black List tab adds `Block Reason` + `Imported From` columns.

#### SCR-196 Add Entry modal (compact)

```
┌──────────────────────────────────────────────────────────────┐
│  Add IMEI Entry — White List                          [×]    │
│  Type:   ( ) Full IMEI (15 digits)   (●) TAC range (8)        │
│  IMEI / TAC:        [ 35291108 ]                              │
│  Device Model:      [ Quectel BG95 ]                          │
│  Description:       [ Standard IoT modem fleet ]              │
│  [ Quarantine Reason ]  ← greylist tab only                   │
│  [ Block Reason ] [ Imported From ▼ ]  ← blacklist tab only   │
│                                       [Cancel] [Add Entry]   │
└──────────────────────────────────────────────────────────────┘
```

#### SCR-196 Bulk Import tab

```
│  Bulk Import — White List                                                │
│  ┌──────────────────────────────────────────────────────────────────────┐│
│  │  Drop CSV here  or  [Choose File]                                    ││
│  │  Schema: imei_or_tac, kind, device_model, description, quarantine_…  ││
│  │  Max: 10 MB / 100,000 rows                                           ││
│  └──────────────────────────────────────────────────────────────────────┘│
│  Preview (first 5 of 12,540):  [valid/warn icons per row]                │
│                                                       [Cancel] [Import] │
│  ─── Active Job ───                                                       │
│  ⏳ Job #312  Bulk Import (whitelist)            Started: 14:21         │
│  ████████████████░░░░ 78%   9,783/12,540   Failed: 4   ETA: ~1 min      │
│                                            [Cancel] [Logs]   [Errors CSV]│
```

#### SCR-197: IMEI Lookup — input modal + result drawer

```
┌──────────────────────────────────────────────────────────────┐  ┌───────────────────────────────┐
│  IMEI Lookup                                          [×]    │  │ IMEI Lookup — 359211089765432 │
│  Paste a full IMEI (15 digits) or TAC prefix (8 digits):     │  │ TAC: 35291108 · Device: Quec… │
│  [ 359211089765432 ]                                         │  │ ┌─ Pool Membership ─────────┐ │
│  ⚠ Cross-tenant lookup is restricted to your tenant scope. │  │ │ ✓ White List  matched_via  │ │
│                                       [Cancel] [Lookup]      │  │ │   tac_range                │ │
└──────────────────────────────────────────────────────────────┘  │ │ — Grey List  not present  │ │
                                                                   │ │ — Black List not present  │ │
                                                                   │ └────────────────────────────┘ │
                                                                   │ ┌─ Bound SIMs (3) ──────────┐ │
                                                                   │ │ ICCID │ Mode │ Status │ → │ │
                                                                   │ │ 8990… │ str. │ ✓ ver  │ → │ │
                                                                   │ └────────────────────────────┘ │
                                                                   │ ┌─ Recent Observations (14) ┐ │
                                                                   │ │ 2026-04-26 14:02 · S6a    │ │
                                                                   │ │  …+11 more   [View All]   │ │
                                                                   │ └────────────────────────────┘ │
                                                                   │ Last seen: 2026-04-26 14:02   │
                                                                   │              [Add to Pool ▼]  │
                                                                   └───────────────────────────────┘
```

- **Navigation:** SCR-196 (toolbar `🔍 IMEI Lookup`) → modal → Lookup → drawer.
- **Drill-downs:** Bound SIMs row → `/sims/<id>#device-binding`; Pool list pill → SCR-196 list filtered to that entry.
- **Empty state:** "No matches in this tenant. The IMEI has not been observed and is not in any pool." + `[Add to Pool ▼]`.
- **Error states:** invalid length → inline `Enter a 15-digit IMEI or 8-digit TAC.`; server error → toast + retry.

### Design Token Map (UI MANDATORY)

#### Color Tokens (from FRONTEND.md)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Primary text | `text-text-primary` | `text-[#…]`, `text-gray-900` |
| Secondary text | `text-text-secondary` | `text-gray-500` |
| Tertiary text / captions | `text-text-tertiary` | `text-gray-400` |
| Page surface (card bg) | `bg-bg-surface` | `bg-white` |
| Elevated surface (drawer/modal bg) | `bg-bg-elevated` | `bg-gray-100` |
| Page background | `bg-bg-primary` | `bg-[#06060B]` |
| Hover row bg | `bg-bg-hover` | `bg-gray-50` |
| Card / row border | `border-border` | `border-gray-200`, `border-[#…]` |
| Primary CTA bg | `bg-accent text-bg-primary` | `bg-blue-500`, `bg-[#…]` |
| Accent text/icon | `text-accent` | `text-blue-500` |
| Success status (verified) | `text-success`, `bg-success-dim` | `text-green-500` |
| Warning status (pending/quarantine) | `text-warning`, `bg-warning-dim` | `text-yellow-500` |
| Danger status (mismatch/blacklist) | `text-danger`, `bg-danger-dim` | `text-red-500` |
| Info badge (tac_range, exact) | `text-info`, `bg-info-dim` | `text-blue-300` |

> Per FRONTEND.md line 40: do NOT use Tailwind defaults (`text-red-500`, `bg-green-50`, etc.).

#### Typography Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page title (`Settings > IMEI Pools`) | `text-2xl font-semibold text-text-primary` | `text-[24px]` |
| Tab strip label | `text-sm font-medium` | `text-[13px]` |
| Table header | `text-xs uppercase tracking-[1px] text-text-tertiary` | hex/px |
| Table row primary | `font-mono text-sm text-text-primary` | hex/px |
| Body / description | `text-sm text-text-secondary` | hex/px |
| Caption (`Last seen…`) | `text-[10px] uppercase tracking-[1px] text-text-tertiary` | hex/px |

#### Spacing & Elevation
| Usage | Class | NEVER Use |
|-------|-------|-----------|
| Card | `<Card>` (organism — has `shadow-card` + `rounded-md` baked in) | `shadow-lg`, `rounded-xl` |
| Section padding | `p-4` / `p-6` per existing settings page convention | arbitrary `p-[20px]` |
| Drawer/Slide-panel | `<SlidePanel>` (handles bg, border, shadow) | raw `<div>` |

#### Existing Components to REUSE (DO NOT recreate)
| Component | Path | Use For |
|-----------|------|---------|
| `<Card>` / `<CardHeader>` / `<CardTitle>` / `<CardContent>` | `web/src/components/ui/card.tsx` | All card chrome |
| `<Button>` | `web/src/components/ui/button.tsx` | ALL buttons — never raw `<button>` |
| `<Input>` | `web/src/components/ui/input.tsx` | ALL form fields |
| `<Select>` | `web/src/components/ui/select.tsx` | imported_from + kind dropdowns |
| `<Tabs>` | `web/src/components/ui/tabs.tsx` | 4-tab strip on SCR-196 |
| `<Dialog>` | `web/src/components/ui/dialog.tsx` | Add Entry compact modal AND IMEI Lookup compact modal (Option C decision) |
| `<SlidePanel>` | `web/src/components/ui/slide-panel.tsx` | IMEI Lookup result drawer (rich form per Option C) |
| `<Table>` / `<Skeleton>` | `web/src/components/ui/table.tsx`, `skeleton.tsx` | Pool rows + loading rows |
| `<Badge>` | `web/src/components/ui/badge.tsx` | `kind` pill, `matched_via` pill, binding_status pill |
| `<EmptyState>` | `web/src/components/shared/empty-state.tsx` | Empty list / no-match drawer |
| `<RowActionsMenu>` | `web/src/components/shared/row-actions-menu.tsx` | Per-row ⋮ (Delete, Move, View Details) |
| `<EntityLink>` | `web/src/components/shared/entity-link.tsx` | Bound SIM rows linking to `/sims/<id>#device-binding` |
| `<FileInput>` | `web/src/components/ui/file-input.tsx` | Bulk Import drop zone |
| `<TableToolbar>` | `web/src/components/ui/table-toolbar.tsx` | Filter chip bar |
| `useHashTab` hook | `web/src/hooks/use-hash-tab.ts` | `#whitelist`/`#greylist`/`#blacklist`/`#bulk-import` URL sync |
| Job progress shape | `web/src/components/shared/progress-toast.tsx` + existing `use-jobs.ts` | Active job card on Bulk Import tab |
| `confirm`/`alert` | NEVER — use `<Dialog>` from ui/dialog.tsx | ALL confirm flows (delete entry) |

### D-187 Disposition (MANDATORY)

**Decision: (B) — Route D-187 to STORY-096; STORY-095 does NOT consume `simAllowlistStore`.**

Reasoning:
- TBL-60 `sim_imei_allowlist` is a **per-SIM** join table (PK = `(sim_id, imei)`) used when a SIM has `binding_mode='allowlist'` — i.e. "this SIM may auth from any IMEI in its personal allowlist". It is a SIM-scoped concept.
- TBL-56/57/58 are **org-wide tenant-scoped** pools (PK = `id`, UNIQUE on `(tenant_id, imei_or_tac)`). Different schema, different semantics, different enforcement path.
- STORY-095's API-331..335 surface is org-wide pools — there is no API-spec slot for per-SIM allowlist Add/Remove/List/IsAllowed in this story.
- STORY-096 is the natural consumer: it implements the AAA pre-check for `binding_mode='allowlist'`, which calls `simAllowlistStore.IsAllowed(simID, observedIMEI)` on every auth request. STORY-096 will also expose Add/Remove/List endpoints (per-SIM scope) to feed the allowlist UI.

**Action items in this story:**
1. Update comment in `cmd/argus/main.go:631` from `// simAllowlistStore: production consumer ships in STORY-095 (D-187)` to `// simAllowlistStore: production consumer ships in STORY-096 (D-187 re-targeted by STORY-095 plan)`.
2. Update `docs/ROUTEMAP.md` D-187 row: target `STORY-095` → `STORY-096`.
3. Append handoff note to `docs/stories/phase-11/STORY-096-binding-enforcement.md` acknowledging D-187 re-target.

### Threading Path (Up-Front — PAT-017 Mitigation)

New stores/handlers thread from `cmd/argus/main.go` to:
1. `cmd/argus/main.go` (~line 632, after `imeiHistoryStore`):
   ```
   imeiPoolStore := store.NewIMEIPoolStore(pg.Pool, simStore)
   imeiPoolHandler := imeipoolapi.NewHandler(imeiPoolStore, auditSvc, log.Logger)
   imeiPoolBulkHandler := imeipoolapi.NewBulkHandler(imeiPoolStore, jobStore, eventBus, auditSvc, log.Logger)
   imeiPoolLookupHandler := imeipoolapi.NewLookupHandler(imeiPoolStore, simStore, imeiHistoryStore, log.Logger)
   imeiPoolImportProcessor := job.NewBulkIMEIPoolImportProcessor(jobStore, imeiPoolStore, eventBus, log.Logger)
   imeiPoolImportProcessor.SetAuditor(auditSvc)
   jobDispatcher.Register(imeiPoolImportProcessor)  // wherever existing processors register
   ```
2. `internal/gateway/router.go` `Deps` struct — add `IMEIPoolHandler *imeipoolapi.Handler`, `IMEIPoolBulkHandler *imeipoolapi.BulkHandler`, `IMEIPoolLookupHandler *imeipoolapi.LookupHandler`.
3. `internal/gateway/router.go` route registration — five routes under `/api/v1/imei-pools/...` (see Task 4).
4. `internal/policy/dsl/evaluator.go` — `Evaluator` accepts `IMEIPoolLookuper` via `NewEvaluatorWithPools(pools)`; `main.go` constructs `dsl.NewEvaluatorWithPools(imeiPoolStore)` everywhere a production `dsl.NewEvaluator()` is currently used (test paths can keep the placeholder).

> RBAC: API-331 + API-335 use `RequireRole("sim_manager")`; API-332 + API-333 + API-334 use `RequireRole("tenant_admin")`. Two route groups inside `/api/v1/imei-pools` block.

### Audit
Action keys (one entry per mutation, all hash-chained via `audit.Auditor.CreateEntry`):
- `imei_pool.entry_added` — Add (single)
- `imei_pool.entry_removed` — Delete
- `imei_pool.bulk_imported` — bulk job completion (single audit, NOT per-row, per AC-5)

Payload shape: `before` and `after` are JSON of the row (or `null`). All include `kind` (whitelist/greylist/blacklist), `imei_or_tac`, `entry_id`. Bulk audit additionally includes `processed_count, failed_count, total_count, job_id`.

## Prerequisites
- [x] STORY-094 closed (commit `8b20650`) — provides binding columns on `sims`, `IMEIHistoryStore`, `simAllowlistStore` (dormant), DSL `device.imei_in_pool(...)` parser + placeholder evaluator.
- [x] STORY-013 (Job system) — `JobStore`, `JobMessage`, bulk-job pattern via `BulkDeviceBindingsProcessor` (`internal/job/sim_bulk_device_binding_worker.go`).
- [x] FRONTEND.md design tokens stable; `<SlidePanel>`, `<Dialog>`, `<Tabs>`, `<EntityLink>` available.

## Tasks

### Task 1: Migration — three IMEI pool tables in one file
- **Files:** Create `migrations/20260508000001_imei_pools.up.sql`, `migrations/20260508000001_imei_pools.down.sql`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `migrations/20260507000002_imei_history.up.sql` (RLS variable canon `app.current_tenant`, ENABLE ROW LEVEL SECURITY pattern, ON DELETE CASCADE FK to tenants); `migrations/20260412000006_rls_policies.up.sql` (pattern for multi-table RLS in one file).
- **Context refs:** "Database Schema".
- **What:** Create the two migration files verbatim from the embedded SQL. Critical: (a) ALL three tables share shape — keep them in one migration to keep schema-version bump minimal. (b) RLS variable is `app.current_tenant` (NOT `..._id`) per landed `imei_history` migration. (c) UNIQUE constraint named `imei_<list>_unique_entry` so 23505 detection can match by name. (d) FK `created_by → users(id) ON DELETE SET NULL` (matches users table's nullable-creator pattern). (e) Down drops policies first, then tables, with `IF EXISTS`. NO seed data — `make db-seed` produces zero rows in any pool by AC-14.
- **Verify:** `make db-migrate-up` succeeds; `psql -c "\d imei_whitelist"` `\d imei_greylist` `\d imei_blacklist` show columns/index/RLS; `psql -c "SELECT count(*) FROM imei_whitelist UNION ALL SELECT count(*) FROM imei_greylist UNION ALL SELECT count(*) FROM imei_blacklist"` after `make db-seed` → all zeros; round-trip `make db-migrate-down && make db-migrate-up` clean.

### Task 2: IMEIPoolStore — CRUD, Lookup, LookupKind + companion methods
- **Files:** Create `internal/store/imei_pool.go`, `internal/store/imei_pool_test.go`, `internal/store/imei_pool_consts_test.go` (PAT-022 structural test); Modify `internal/store/sim.go` (add `ListByBoundIMEI`); Modify `internal/store/imei_history.go` (add `ListByObservedIMEI`)
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/store/imei_history.go` (cursor-pagination + tenant-RLS pattern); `internal/store/sim.go:135-200` (column-list + scan pattern); `internal/store/sim_binding_consts_test.go` (PAT-022 SQL re-parse pattern).
- **Context refs:** "Store Surface", "Database Schema".
- **What:** (1) Implement `IMEIPoolStore` with all 6 methods (`List`, `Add`, `Get`, `Delete`, `Lookup`, `LookupKind`). Each table-aware method dispatches via a `tableName(pool)` helper (`whitelist→imei_whitelist`, etc.). `Add` returns `ErrPoolEntryDuplicate` when pgx returns SQLSTATE 23505 with constraint name matching `imei_*_unique_entry`. `Lookup` runs three queries (one per table) and merges; `LookupKind` runs ONE query against the named table — returns true/false existence. (2) `ListByBoundIMEI` on `SIMStore`: SELECT id, iccid, binding_mode, binding_status FROM sims WHERE tenant_id=$1 AND bound_imei=$2. PAT-006 RECURRENCE guard — verify `simColumns`/`scanSIM` is NOT extended (we're SELECTing a fixed shape into a dedicated DTO, not the full `SIM` struct). (3) `ListByObservedIMEI` on `IMEIHistoryStore`: SELECT … FROM imei_history JOIN sims … WHERE observed_imei=$1 AND tenant_id=$2 AND observed_at >= NOW() - INTERVAL '30 days' ORDER BY observed_at DESC LIMIT 50. Tenant filter applied in BOTH WHERE clauses. (4) `imei_pool_consts_test.go` re-parses migration SQL at runtime, asserts Go enum sets equal CHECK enums for `kind` (full_imei, tac_range) and `imported_from` (manual, gsma_ceir, operator_eir). Mirrors `sim_binding_consts_test.go` shape exactly.
- **Verify:** `go build ./internal/store/...`; `go test ./internal/store/... -run 'TestIMEIPool|TestSIMStoreListByBoundIMEI|TestIMEIHistoryListByObservedIMEI'` PASS; cross-tenant negative tests included (Add/Get/Delete from another tenant returns ErrPoolEntryNotFound or RLS-isolated empty).

### Task 3: Bulk import job processor (SVC-09)
- **Files:** Create `internal/job/imei_pool_import_worker.go`, `internal/job/imei_pool_import_worker_test.go`; Modify `internal/job/types.go` (add `JobTypeBulkIMEIPoolImport` + listing); Modify `internal/job/bulk_types.go` (add `BulkIMEIPoolImportPayload`, `BulkIMEIPoolImportRowSpec`, `IMEIPoolImportRowResult`)
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/sim_bulk_device_binding_worker.go` (the canonical 094 T5 pattern). Mirror exactly: `Process` shape, `processRow` returning `(outcome, errMsg)`, batch-cancellation via `bulkBatchSize`, `completeJob` pattern, single audit entry on completion.
- **Context refs:** "API Specifications > API-334", "Audit", "Story-Specific Compliance Rules" (CSV injection rule).
- **What:** New processor `BulkIMEIPoolImportProcessor` with `Type() = "bulk_imei_pool_import"`. Payload carries `Pool PoolKind` (`whitelist|greylist|blacklist`) + `Rows []BulkIMEIPoolImportRowSpec`. `processRow` validates: (a) `kind ∈ {full_imei, tac_range}` else `invalid_kind`; (b) length 15 vs 8 else `invalid_length`; (c) for greylist `quarantine_reason` non-empty after trim else `missing_required`; (d) for blacklist both `block_reason` and `imported_from ∈ enum` else `missing_required`; (e) **CSV injection sanitizer (MANDATORY)** — for any string field that, after trim, starts with one of `=`, `+`, `-`, `@`, `\t` → either reject (set outcome `csv_injection_rejected`) or prefix with `'`. **Decision: reject** with explicit row-level outcome — gives the operator clear feedback in the error CSV; sanitization-via-prefix risks silent data corruption if the operator later round-trips the export. (f) `IMEIPoolStore.Add` — on `ErrPoolEntryDuplicate` outcome `duplicate`, on other error `store_error`, success → outcome `success`. Single audit `imei_pool.bulk_imported` on completion (NOT per-row, matches AC-5 contract). Register in main.go alongside existing processors.
- **Verify:** `go test ./internal/job/... -run 'TestBulkIMEIPoolImport'` PASS; integration fixture: 6-row payload (1 valid full_imei, 1 valid tac_range, 1 invalid_length, 1 missing_required, 1 duplicate, 1 csv_injection `=cmd|'/c calc'!A1`) → 2 success / 4 failures with row-level outcomes preserved in error report.

### Task 4: IMEI Pool handler — API-331 List + API-332 Add + API-333 Delete
- **Files:** Create `internal/api/imei_pool/handler.go`, `internal/api/imei_pool/handler_test.go`; Modify `internal/gateway/router.go` (Deps struct + routes); Modify `cmd/argus/main.go` (instantiation)
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/sim/device_binding_handler.go` (handler struct + envelope + `decodeOptionalStringField` for nullable fields if ever needed); `internal/gateway/router.go:463-471` (route group with `RequireRole`).
- **Context refs:** "API Specifications > API-331/332/333", "Threading Path", "Audit", "Story-Specific Compliance Rules".
- **What:** `Handler` with `List`, `Add`, `Delete`. `List`: parse `kind` URL param + filters; reject unknown `kind` with 400; call `IMEIPoolStore.List`; envelope with `meta.next_cursor/limit/has_more`. `Add`: decode JSON body, run CSV-injection sanitizer on every string field (`device_model`, `description`, `quarantine_reason`, `block_reason` — 422 on hit, message "field contains forbidden formula prefix"); run kind/length/required validations per AC-3; `IMEIPoolStore.Add` → 409 on duplicate; emit `imei_pool.entry_added` audit. `Delete`: parse id+kind; call `Delete` → 204 on success, 404 on not found; emit `imei_pool.entry_removed` audit. Cross-tenant returns 404 (NOT 403) per envelope convention. Wire two route groups in router.go: `RequireRole("sim_manager")` for GET, `RequireRole("tenant_admin")` for POST/DELETE. main.go instantiates `imeiPoolStore` + handler + Deps wiring. PAT-031 guard NOT applicable here (no PATCH in this story; if a future move-between-lists endpoint is added as PATCH, follow `device_binding_handler.go::Patch` pattern).
- **Verify:** `go test ./internal/api/imei_pool/... -run 'TestHandler'` PASS; `go vet ./...`; integration test asserts UNIQUE conflict → 409 `IMEI_POOL_DUPLICATE`; greylist without `quarantine_reason` → 422; cross-tenant DELETE → 404; CSV-injection field → 422.

### Task 5: Bulk handler + Lookup handler — API-334 + API-335
- **Files:** Create `internal/api/imei_pool/bulk_handler.go`, `internal/api/imei_pool/lookup_handler.go`, `internal/api/imei_pool/bulk_handler_test.go`, `internal/api/imei_pool/lookup_handler_test.go`; Modify `internal/gateway/router.go`, `cmd/argus/main.go`
- **Depends on:** Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/sim/bulk_handler.go:1047-1185` (multipart CSV → JobStore.Create → bus.Publish → 202 envelope — the canonical pattern from STORY-094). Read `internal/api/sim/device_binding_handler.go::GetIMEIHistory` for query-param parsing pattern.
- **Context refs:** "API Specifications > API-334/335", "Story-Specific Compliance Rules".
- **What:** `BulkHandler.Import` (POST `/api/v1/imei-pools/{kind}/import`): parse `kind` URL param; validate via existing kill-switch helper; max 10 MB; require `.csv` extension; parse header `imei_or_tac, kind, device_model, description, quarantine_reason, block_reason, imported_from` (case-insensitive, missing required cols → 422 with `missing_columns` detail); enqueue `JobTypeBulkIMEIPoolImport` with the rows + the `Pool` value derived from URL; 202 `{job_id}`. Per-row CSV-injection check happens in the worker (see Task 3) — the handler's responsibility is shape and route-level validation only. Reuse existing `GET /api/v1/jobs/{id}/errors` for error CSV download — DO NOT add a new endpoint. `LookupHandler.Get` (GET `/api/v1/imei-pools/lookup`): require `imei` query param; 422 `INVALID_IMEI` when missing or `len != 15` or non-digit; call `IMEIPoolStore.Lookup` → list-of-(kind, entry_id, matched_via); call `SIMStore.ListByBoundIMEI` → bound_sims; call `IMEIHistoryStore.ListByObservedIMEI` (last 30 days, max 50) → history; envelope. Empty arrays (NOT 404) when no match. Wire routes under same `/api/v1/imei-pools` block in router.go — Bulk needs `tenant_admin`, Lookup needs `sim_manager`.
- **Verify:** `go test ./internal/api/imei_pool/... -run 'TestBulkHandler|TestLookupHandler'` PASS; integration: 1000-row CSV with 5 malformed rows → 202 `{job_id}` then job-result endpoint shows 995 success / 5 failures; lookup with 15-digit IMEI in whitelist + 2 bound SIMs → both arrays populated; lookup with 14-digit → 422; lookup with no match → 200 with empty arrays.

### Task 6: DSL evaluator — replace `device.imei_in_pool` placeholder with real lookup + per-pass cache
- **Files:** Modify `internal/policy/dsl/evaluator.go`; Create `internal/policy/dsl/evaluator_pool_test.go`; Modify `cmd/argus/main.go` (call `dsl.NewEvaluatorWithPools` for production paths)
- **Depends on:** Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/policy/dsl/evaluator.go:244-258` (current placeholder location); `internal/policy/dsl/evaluator.go:60-87` (`Evaluate` entry point — where cache lifecycle starts); `internal/policy/dsl/evaluator_imei_test.go` if it exists, else `evaluator_test.go` (synthetic-context test pattern).
- **Context refs:** "DSL Evaluator: device.imei_in_pool Replacement", "Validation Trace appendix V2/V4".
- **What:** (1) Define `IMEIPoolLookuper` interface in evaluator.go (`LookupKind(ctx context.Context, tenantID uuid.UUID, imei string, pool string) (bool, error)`). (2) `Evaluator` gains optional `pools IMEIPoolLookuper` field; ctor `NewEvaluatorWithPools(pools)` keeps existing `NewEvaluator()` placeholder-compatible. (3) Add unexported `poolCache map[string]bool` and `runtimeCtx context.Context` slots on `SessionContext` (`json:"-"`). (4) At top of `Evaluate()`: `ctx.poolCache = map[string]bool{}` (so the same map ref propagates through value-typed `ctx` copies via map reference semantics). Add `WithContext(c context.Context)` setter on SessionContext. (5) Replace placeholder branch in `getConditionFieldValue` to: parse pool name (strip quotes/whitespace from inner); guard `len(ctx.IMEI)==15`; cache get → return; cache miss → if `e.pools == nil` cache `false` else call `e.pools.LookupKind` with parsed `tenantID` and `pool`; on err log+cache `false` (fail-open in policy evaluation; STORY-096 enforcement decides default-deny posture); cache write; return. (6) Five tests in `evaluator_pool_test.go`: (a) ctx.IMEI not 15 → false (b) pools nil → false (c) full_imei exact hit → true; matched_via assert NOT carried into bool (we only return existence) (d) tac_range hit (different IMEI same TAC) → true (e) AC-9 cache trace: two WHEN clauses `device.imei_in_pool('blacklist') OR device.imei_in_pool('blacklist')` against a stub lookuper that counts calls — assert call count == 1 for blacklist (and 1 each for separate pools when `OR device.imei_in_pool('greylist')` added → call count == 2 total, NOT 4).
- **Verify:** `go test ./internal/policy/dsl/... -run 'TestIMEIInPool|TestPoolCache'` PASS; `go test ./internal/policy/dsl/...` (full suite green — placeholder regression check); cache-trace test asserts mock LookupKind invocation count.

### Task 7: SCR-196 — IMEI Pools page (4 tabs + Add Entry dialog + Bulk Import)
- **Files:** Create `web/src/pages/settings/imei-pools/index.tsx`, `web/src/pages/settings/imei-pools/pool-list-tab.tsx`, `web/src/pages/settings/imei-pools/add-entry-dialog.tsx`, `web/src/pages/settings/imei-pools/bulk-import-tab.tsx`, `web/src/hooks/use-imei-pools.ts`, `web/src/types/imei-pool.ts`; Modify `web/src/router.tsx` (route registration), `web/src/components/layout/sidebar.tsx` (Settings nav entry), `web/src/components/layout/page-header.tsx` (breadcrumb hint if applicable)
- **Depends on:** Task 4, Task 5
- **Complexity:** high
- **Pattern ref:** Read `web/src/pages/settings/ip-pools.tsx` (settings page with `<Card>`, `<SlidePanel>` for create form, `<Skeleton>` loading, hook pattern); `web/src/hooks/use-jobs.ts` (job-progress polling pattern for Bulk Import active-job card); `web/src/components/shared/empty-state.tsx` (empty list state); `web/src/components/shared/row-actions-menu.tsx` (per-row action menu).
- **Context refs:** "Screen Mockups > SCR-196", "Design Token Map", "API Specifications > API-331/332/333/334".
- **What:** Build `/settings/imei-pools` route with `<Tabs>` showing 4 tabs (`#whitelist`, `#greylist`, `#blacklist`, `#bulk-import`) — use `useHashTab`. Three list tabs share `<PoolListTab>` org-component parametrized by `kind`: filter chip bar (TAC, Device Model, Type), `<TableToolbar>` with `🔍 IMEI Lookup` button (opens lookup dialog from Task 8) and `+ Add Entry` button, paginated `<Table>` rendering rows from API-331 with `?include_bound_count=1`. Greylist tab adds `Quarantine Reason` column; Blacklist tab adds `Block Reason` + `Imported From` columns. Each row: `<RowActionsMenu>` with Delete (calls `useDeleteEntry` → API-333; confirm via `<Dialog>`), Move (TODO note: routes to Task 4 if a single move endpoint is wired, else POST new + DELETE old client-side; for v1 ship "Move" as DELETE+POST sequence). Bound count column: clickable → `/sims?bound_imei_filter=<imei_or_tac>` (uses tac_range vs full_imei branching). `<AddEntryDialog>` (compact `<Dialog>` per Option C): `<Tabs>`-internal toggle full_imei vs tac_range with length validator; greylist mode shows `quarantine_reason` field; blacklist mode shows `block_reason` + `imported_from <Select>` (manual/gsma_ceir/operator_eir). Validates client-side, submits via `useAddEntry`; on 409 IMEI_POOL_DUPLICATE shows toast + form error. `<BulkImportTab>`: drop zone via `<FileInput>`, schema explainer below, `[Cancel] [Import]` buttons. After submit: poll job via `use-jobs` hook → render active-job `<Card>` with progress bar (X% / Y of Z / Failed: N / ETA), `[Cancel] [Logs] [Errors CSV]` actions. Errors CSV downloads from existing `/api/v1/jobs/{id}/errors`. Empty/loading/error states ALL covered: empty list → `<EmptyState>` with `+ Add Entry` + `Bulk Import` CTAs; loading → `<Skeleton>` rows; API error → toast + retry. **Tokens:** Use ONLY classes from Design Token Map — zero hardcoded hex/px. **Components:** Reuse atoms/molecules from Component Reuse table — NEVER raw `<button>`/`<input>`/`alert()`. Add Settings sidebar nav entry between IP Pools and Knowledge Base. Use `frontend-design` skill for visual quality.
- **Verify:** `cd web && pnpm tsc --noEmit` clean; `pnpm vite build` succeeds; `grep -rn '#[0-9a-fA-F]\{3,\}' web/src/pages/settings/imei-pools/ web/src/hooks/use-imei-pools.ts` returns ZERO matches; manual smoke: `/settings/imei-pools#whitelist` loads, Add Entry → row appears; `#bulk-import` upload 5-row CSV → progress card + completed status; empty list shows `<EmptyState>`.

### Task 8: SCR-197 — IMEI Lookup modal + drawer
- **Files:** Create `web/src/pages/settings/imei-pools/lookup-dialog.tsx`, `web/src/pages/settings/imei-pools/lookup-drawer.tsx`; Modify `web/src/pages/settings/imei-pools/index.tsx` (toolbar `🔍 IMEI Lookup` wires lookup dialog), `web/src/pages/sessions/index.tsx` (SCR-050 toolbar adds lookup button), `web/src/pages/sims/index.tsx` (SCR-020 toolbar adds lookup button); Extend `web/src/hooks/use-imei-pools.ts` with `useLookup`
- **Depends on:** Task 7
- **Complexity:** medium
- **Pattern ref:** Read `web/src/components/ui/slide-panel.tsx` (drawer wrapper); `web/src/pages/settings/imei-pools/add-entry-dialog.tsx` (compact dialog skeleton from Task 7); `web/src/components/shared/entity-link.tsx` (cross-page link pattern for Bound SIMs rows).
- **Context refs:** "Screen Mockups > SCR-197", "Design Token Map", "API Specifications > API-335".
- **What:** `<IMEILookupDialog>` (compact `<Dialog>` per Option C): single `<Input>` accepting full IMEI (15) or TAC (8); inline length validation; submit calls `useLookup({imei})` for 15-digit, OR for 8-digit branch falls back to `useImeiPoolList({kind: 'whitelist|greylist|blacklist', tac})` aggregation client-side. On result, close dialog + open `<IMEILookupDrawer>` `<SlidePanel size="lg">`. Drawer renders three `<Card>` sections: (1) Pool Membership — list 3 pools with `✓` / `—` indicator, `matched_via` badge (`<Badge>` token: exact=`bg-info-dim text-info`, tac_range=`bg-warning-dim text-warning`); (2) Bound SIMs — `<Table>` rows (ICCID, Mode, Status badge using existing tone-map helper, `<EntityLink>` to `/sims/<id>#device-binding`); (3) Recent Observations — last 30 days from `history` array, ordered DESC, "View All" link → `/sims/<sim_id>/imei-history?observed_imei=<imei>` (or first SIM if multiple). Header line shows `TAC: <8>` and `Last seen: <ts> · via <protocol>` from latest history row. Footer: `[Add to Pool ▼]` `<DropdownMenu>` with three options that open the SCR-196 Add Entry dialog pre-populated with the looked-up IMEI + selected target list. Empty states: no IMEI matches → drawer shows `<EmptyState>` "No matches in this tenant. The IMEI has not been observed and is not in any pool." + `[Add to Pool ▼]` action. SCR-050 + SCR-020 toolbar buttons: just import `<IMEILookupDialog>` and add a button — open it from those pages too. **Tokens:** ONLY Design Token Map classes. **Components:** ALL reuses from Component Reuse table. Use `frontend-design` skill for visual quality.
- **Verify:** `pnpm tsc --noEmit` clean; `grep -rn '#[0-9a-fA-F]\{3,\}' web/src/pages/settings/imei-pools/lookup-*.tsx` ZERO matches; manual smoke: SCR-196 toolbar `🔍 IMEI Lookup` opens dialog → submit 15-digit IMEI in whitelist with 2 bound SIMs → drawer renders Pool Membership + Bound SIMs + Observations; click Bound SIM row → navigates to SIM Detail; SCR-050 (Live Sessions) toolbar `🔍 IMEI Lookup` button opens same dialog.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 (3 tables, RLS, UNIQUE, idx, down) | Task 1 | Task 1 verify; Task 2 const test |
| AC-2 (API-331 + filters + bound count) | Task 4 | Task 4 tests |
| AC-3 (API-332 + validation + 409 + audit) | Task 4 | Task 4 tests (UNIQUE conflict, missing required) |
| AC-4 (API-333 + 404 + audit) | Task 4 | Task 4 tests |
| AC-5 (API-334 bulk + per-row outcomes + audit) | Task 3, Task 5 | Task 3 + Task 5 integration |
| AC-6 (API-335 lookup + history) | Task 5 | Task 5 lookup tests |
| AC-7 (move-between-lists) | Task 7 | Manual UAT (DELETE+POST sequence) |
| AC-8 (TAC-range matching at lookup) | Task 2, Task 5, Task 6 | Task 2 lookup test, Task 6 evaluator test |
| AC-9 (DSL `device.imei_in_pool` real + cache) | Task 6 | Task 6 cache-count test |
| AC-10 (SCR-196 page + 4 tabs + bulk) | Task 7 | Manual UAT + tsc/build |
| AC-11 (SCR-197 modal + drawer + cross-screen) | Task 8 | Manual UAT |
| AC-12 (RBAC) | Task 4, Task 5 | Task 4/5 negative-role tests |
| AC-13 (audit hash chain) | Task 4, Task 3 | existing audit chain verifier |
| AC-14 (regression — `make test` + seed clean) | Task 1, all | `make test` final run; `make db-seed` smoke |

## Story-Specific Compliance Rules

- **API:** standard envelope on all 5 endpoints; cursor-based pagination on API-331; cross-tenant returns 404 (not 403) per existing convention; 422 `INVALID_IMEI` on lookup format; 409 `IMEI_POOL_DUPLICATE` on UNIQUE.
- **DB:** migration single-file (3 tables) at `20260508000001`; RLS variable `app.current_tenant` (NOT `..._id`); ON DELETE CASCADE on tenant FK; ON DELETE SET NULL on user FK; UNIQUE constraint named for SQLSTATE 23505 detection.
- **DSL:** `device.imei_in_pool('whitelist'|'greylist'|'blacklist')` becomes functional; per-evaluation cache via `SessionContext.poolCache map[string]bool` (not on Evaluator struct — per-tenant safety); evaluator falls back to `false` when `pools == nil` (preserves placeholder behavior for tests).
- **CSV injection (MANDATORY):** any string field in API-332 body OR API-334 CSV row that, after trim, starts with `=`, `+`, `-`, `@`, or tab MUST be REJECTED with explicit outcome (handler: 422 with message; worker: row-outcome `csv_injection_rejected`). NOT prefix-sanitized — silent corruption risk on round-trip export. Applies to: `device_model`, `description`, `quarantine_reason`, `block_reason`. Add unit test: malicious row `imei_or_tac="=cmd|'/c calc'!A1",kind="full_imei"` → rejected; `description="-2+5"` → rejected; benign descriptions starting with letters/digits → accepted.
- **UI:** Design tokens from FRONTEND.md — zero hex/px; reuse `<Card>`/`<Dialog>`/`<SlidePanel>`/`<Tabs>`/`<Table>`/`<Button>`/`<Input>`/`<Select>`/`<Badge>`/`<EmptyState>`/`<RowActionsMenu>`/`<EntityLink>`; `frontend-design` skill noted for Tasks 7 & 8.
- **Business:** `make db-seed` produces zero rows in any of the three pools by default; bulk import max 10 MB / 100 000 rows; lookup history capped at 30 days, 50 rows.
- **ADR-004:** local enforcement architecture; this story stops at parser/evaluator wiring + read-side lookup, NOT enforcement (STORY-096 territory).
- **D-187:** routed to STORY-096 (not consumed in this story); main.go comment + ROUTEMAP target updated.

## Bug Pattern Warnings

- **PAT-006 / PAT-026 (RECURRENCE risk):** new `IMEIPoolEntry` struct construction must audit ALL sites — handler Add path, lookup result mapping, bulk worker row-result mapping. After Task 4: `grep -rn 'IMEIPoolEntry{' internal/` to confirm no zero-valued required fields.
- **PAT-009:** `device_model`, `description`, `quarantine_reason`, `block_reason`, `imported_from`, `created_by` are all nullable → use `*string` / `*uuid.UUID` in Go struct; DTO marshals `null` (not empty string / zero UUID) when nil. The `quarantine_reason` and `block_reason` columns are `NOT NULL` at the DB layer for greylist/blacklist tables — but the Go struct keeps them as `*string` because the same struct holds whitelist rows where those columns are absent (NULL in unified DTO).
- **PAT-011 / PAT-017:** new stores + handlers + processor + DSL `pools` interface MUST thread from main.go through DI — see "Threading Path" section. Audit grep after Task 4: `grep -n 'imeiPoolStore' cmd/argus/main.go` should show ≥4 hits (instantiation + handler + bulk + lookup + DSL evaluator).
- **PAT-022:** `kind` (`full_imei`, `tac_range`) and `imported_from` (`manual`, `gsma_ceir`, `operator_eir`) CHECK enums must equal Go const sets. Task 2 ships `imei_pool_consts_test.go` re-parsing migration SQL at runtime — mirror `sim_binding_consts_test.go`.
- **PAT-023:** `migrate force` can lie. After landing migration, `schemacheck` boot-time guard must catch any drift — verify on first boot under `make up`. Round-trip down/up in Task 1 verify.
- **PAT-026:** no feature is being removed in this story; D-187 re-target updates a comment + ROUTEMAP entry only (no code dormancy moved).
- **PAT-031:** no PATCH endpoints land in this story (Add is POST, Delete is DELETE; Move is two ops). If a future revision introduces PATCH `/api/v1/imei-pools/{kind}/{id}` (e.g., update device_model), use non-pointer `json.RawMessage` + `decodeOptionalStringField` per `internal/api/sim/device_binding_handler.go::Patch`.

## Tech Debt (from ROUTEMAP)

- **D-187 (dormant `simAllowlistStore`):** **Re-targeted by this plan to STORY-096.** TBL-60 is per-SIM allowlist (semantically orthogonal to TBL-56/57/58 org-wide pools); STORY-096 enforcement consumes it. Action: Task 4 includes a one-line comment update in `cmd/argus/main.go:631`, ROUTEMAP D-187 row updated, STORY-096 file gets a handoff note appended.
- **D-183 (PROTOCOLS device.peri_raw):** still STORY-097 — no change.
- **D-184 (1M-SIM bench):** still STORY-096 — no change.

## Mock Retirement

N/A — frontend lands here for the first time, no prior mocks for the imei-pool endpoints. Future stories may add mocks; not applicable now.

## Risks & Mitigations

- **R1: DSL evaluator cache leaks across requests.** Mitigation: cache lives on `SessionContext` (per-call value), NOT on `Evaluator` struct; reset at top of `Evaluate()`. Multi-tenant test in Task 6 uses two different tenant ctx values + asserts no cross-cache pollution.
- **R2: Bulk worker race on duplicate inserts within same job.** If the same `(tenant_id, imei_or_tac)` appears twice in the CSV: first row succeeds, second triggers UNIQUE 23505 → outcome `duplicate`. Mitigation: this is by-design and tested. NO transaction wrapping — per-row commit allows partial success per AC-5.
- **R3: TAC-range matching SQL slow without index.** Mitigation: `idx_<list>_tenant_kind (tenant_id, kind)` is in the migration. The `LookupKind` query path does either `imei_or_tac = $imei AND kind='full_imei'` or `imei_or_tac = SUBSTRING($imei,1,8) AND kind='tac_range'` — both use the `(tenant_id, kind)` index for tenant + kind filter, then equality on the small remaining set.
- **R4: CSV injection sanitization rejects benign rows starting with `-`.** A row like `description=-foo` looks malicious to the regex but may be legitimate. Mitigation: outcome `csv_injection_rejected` is explicit in error CSV — operator sees "starts with forbidden formula prefix" and can re-quote. Better than silent prefix injection. Validation Trace V3 covers a benign-vs-malicious example.
- **R5: SCR-196/197 design drift from FRONTEND.md.** Mitigation: Task 7 + Task 8 explicitly invoke `frontend-design` skill; verify steps include `grep -rn '#[0-9a-fA-F]'` returning zero matches.

## Validation Trace (Planner Quality Gate appendix)

> Re-verifiable by Quality Gate before plan acceptance.

**V1 — TAC-range matching at lookup time (AC-8):**
- Sample IMEI: `"359211089765432"` (15 digits).
- TAC slice: `imei[0:8]` = `"35921108"`.
- TBL-56 row: `id='abc-…', kind='tac_range', imei_or_tac='35921108'`.
- `Lookup` SQL whitelist branch: `WHERE tenant_id=$1 AND ((kind='full_imei' AND imei_or_tac='359211089765432') OR (kind='tac_range' AND imei_or_tac=SUBSTRING('359211089765432',1,8)='35921108'))`.
- Match: row `id='abc-…'` returned with `matched_via='tac_range'`. ✅
- Same IMEI with no whitelist row: zero rows returned from whitelist branch; greylist + blacklist branches independently checked; `lists` array is empty (NOT 404, returns 200). ✅

**V2 — DSL evaluator cache trace (AC-9):**
- Stub `IMEIPoolLookuper` records call count per (tenantID, imei, pool) tuple.
- Policy: `WHEN device.imei_in_pool('blacklist') OR device.imei_in_pool('greylist') THEN reject`
- Evaluate() entry: `ctx.poolCache = map[string]bool{}` (fresh per call).
- First WHEN: `device.imei_in_pool('blacklist')` → cache miss → stub.LookupKind(blacklist) → returns false → cache `{blacklist:false}` → returns false.
- Second WHEN (same Evaluate() pass): `device.imei_in_pool('greylist')` → cache miss → stub.LookupKind(greylist) → returns true → cache `{blacklist:false, greylist:true}` → returns true.
- Total stub.LookupKind calls: 2 (one per pool, NOT 4). ✅
- Repeat the same WHEN twice (`OR device.imei_in_pool('blacklist')` appended): cache hit on second blacklist reference → stub.LookupKind calls remain at 2. ✅

**V3 — CSV injection sanitization (mandatory rule):**
- Malicious input row: `imei_or_tac=359211089765432, kind=full_imei, device_model="=cmd|'/c calc'!A1"`.
- Worker sees `device_model` starts with `=` → outcome `csv_injection_rejected`, errMsg "device_model contains forbidden formula prefix".
- Job result error report contains row index + outcome; downloadable via `/api/v1/jobs/{id}/errors` as CSV.
- Benign input row: `description="-foo"` (starts with `-`) → also rejected (conservative). Operator sees explicit reason and can re-quote in source CSV.
- Benign input row: `description="standard IoT modem"` (starts with letter) → accepted. ✅
- Handler path (API-332): same rule applies in single-add — 422 `VALIDATION_ERROR` with `field=device_model, reason=csv_injection_rejected`.

**V4 — DSL evaluator condition-field switch coverage (no regression):**
- All 8 STORY-094 condition fields (`device.imei`, `device.imeisv`, `device.software_version`, `device.tac`, `device.binding_status`, `sim.binding_mode`, `sim.bound_imei`, `sim.binding_verified_at`) preserved verbatim in the switch. ✅
- `tac(<field>)` placeholder logic preserved. ✅
- `device.imei_in_pool(<pool>)` branch REPLACED — old behavior (always false) survives via `pools == nil` fallback path; new behavior gates on `len(ctx.IMEI)==15` then store call. ✅

**V5 — Migration RLS variable canon:**
- Landed `migrations/20260507000002_imei_history.up.sql:21` uses `current_setting('app.current_tenant', true)::uuid`. ✅
- New `migrations/20260508000001_imei_pools.up.sql` uses the same `app.current_tenant` literal in all three policies. ✅ NO `app.current_tenant_id` typo.

**V6 — D-187 disposition (B) reconciles:**
- TBL-60 schema: PK `(sim_id, imei)` — per-SIM scope. (`migrations/20260507000003_sim_imei_allowlist.up.sql`).
- TBL-56/57/58 schema: PK `id`, UNIQUE `(tenant_id, imei_or_tac)` — org-wide tenant scope.
- API-331..335 in api/_index.md surface only org-wide endpoints; no per-SIM allowlist Add/Remove/IsAllowed entries.
- STORY-096 spec section ("AAA pre-check enforcement, allowlist mode") natural consumer.
- main.go:631 comment must change in Task 4: `// simAllowlistStore: production consumer ships in STORY-096 (D-187 re-targeted by STORY-095 plan)`. ✅

## decisions.md Entries (route to ROUTEMAP)

- **VAL-NNN-1:** D-187 disposition **(B)** — STORY-095 routes D-187 to STORY-096; per-SIM allowlist (TBL-60) is semantically distinct from org-wide pools (TBL-56/57/58); STORY-096's `binding_mode='allowlist'` enforcement is the natural consumer.
- **VAL-NNN-2:** `device.imei_in_pool` placeholder REPLACEMENT is a named task (Task 6) — evaluator gains optional `IMEIPoolLookuper` interface + `NewEvaluatorWithPools` ctor; per-evaluation cache lives on `SessionContext.poolCache` (not Evaluator struct) to prevent cross-tenant pollution.
- **VAL-NNN-3:** CSV injection sanitization MANDATORY in both API-332 (handler 422) AND API-334 (worker outcome `csv_injection_rejected`); REJECT (not prefix-quote) for explicit operator feedback; tested with both malicious and benign edge rows.
- **VAL-NNN-4:** Threading path documented up-front (5 instantiation lines in main.go + 3 Deps fields + 5 routes + 1 DSL ctor swap) — PAT-017 mitigation.
- **VAL-NNN-5:** RLS canon `app.current_tenant` (NOT `..._id`) — verified against landed `imei_history` migration; embedded verbatim in plan SQL.
- **VAL-NNN-6:** Migration shape — single file `20260508000001_imei_pools.{up,down}.sql` containing all 3 tables (shared shape; minimizes schema-version churn) — vs. three separate files (matches db/_index.md per-TBL grain). Picked single-file consciously per advisor.
- **VAL-NNN-7:** Worked-example independent computation rule applied — Validation Trace V1–V6 verifies each example (TAC matching, cache count, CSV injection, RLS canon, D-187 reasoning) before plan acceptance.

## USERTEST Scenarios (Turkish — backend + UI)

Backend/API:
- API-332 ile bir 15-haneli IMEI'yi whitelist'e ekle; aynı IMEI ile tekrar dene → 409 IMEI_POOL_DUPLICATE.
- API-332 ile greylist'e `quarantine_reason` olmadan POST → 422.
- API-332 ile blacklist'e `block_reason` veya `imported_from` olmadan POST → 422.
- API-334 ile 1000 satırlık CSV yükle (5 hatalı, 1 CSV-injection `=cmd…`); job tamamlandığında 994 success / 6 failure raporu görülmeli; error CSV indirilmeli.
- API-335 ile whitelist'te bulunan + 2 SIM'e bağlı IMEI'yi sorgula → `lists` + `bound_sims` dolu, `history` son 30 gün; 200 dön.
- API-335 ile 14 haneli geçersiz IMEI sorgula → 422 INVALID_IMEI.
- DSL `device.imei_in_pool('blacklist')` predicate'i blacklist'te bulunan bir IMEI ile değerlendir → true; aynı policy aynı pass'te `OR device.imei_in_pool('blacklist')` tekrarı → store 1 kez sorgulanmalı (cache trace).
- Cross-tenant: başka tenant'a ait entry üzerinde GET/POST/DELETE → 404 dön.

UI:
- Settings → IMEI Pools → White List sekmesi → `+ Add Entry` → `Type: TAC range`, `IMEI/TAC: 35291108`, `Device Model: Quectel BG95` → kaydet → tablo yeni satırı göstermeli.
- Bulk Import sekmesi → 100 satırlık CSV yükle → Preview ekranı → `Import` → progress kartı (X% / Y/Z / Failed) → Failed > 0 ise `[Errors CSV]` butonu indirilebilir.
- SIM List ekranı toolbar'ında `🔍 IMEI Lookup` butonu → modal açılır → 15 haneli IMEI gir → drawer açılır → `Pool Membership`, `Bound SIMs`, `Recent Observations` üç bölüm de doluysa görüntülenir.
- Drawer'da bir Bound SIM satırına tıkla → `/sims/<id>#device-binding` sayfasına gider.
- Lookup drawer'ında `[Add to Pool ▼]` → "Add to Black List" seç → `block_reason` ve `imported_from` alanları görünür → kaydet → blacklist sekmesinde yeni satır görünür.

## Pre-Validation Self-Check

- [x] Min plan lines ≥60 (M effort): well over (~720 lines).
- [x] Min task count ≥3 (M effort): 8 tasks.
- [x] At least one `Complexity: high` task: Tasks 1, 2, 6, 7.
- [x] Required sections: Goal, Architecture Context, Tasks, Acceptance Criteria Mapping.
- [x] API specs embedded for API-331..335 with status codes + bodies + error codes.
- [x] DB schema embedded with source noted (DESIGN — TBL-56/57/58 first migration).
- [x] UI compliance: Design Token Map populated, Component Reuse table populated, mockups embedded for SCR-196 + SCR-197, drill-down targets identified, empty/loading/error states embedded, `frontend-design` skill noted in Task 7 + Task 8.
- [x] Each task has Pattern ref (existing project file), Context refs (sections in this plan), Verify command.
- [x] All Context refs point to existing sections in this plan.
- [x] D-187 disposition explicit (B) with reasoning + action items.
- [x] `device.imei_in_pool` replacement is a NAMED TASK (Task 6).
- [x] CSV injection sanitization embedded in both Story-Specific Compliance Rules AND Task 3/4 What.
- [x] RLS canon `app.current_tenant` verified against landed migration; embedded verbatim.
- [x] Worked-example Validation Trace V1–V6 included; cache trace counts store calls.
- [x] PAT-006/009/011/017/022/023/026/031 warnings present.
- [x] BulkDeviceBindingsProcessor pattern reuse called out (Task 3 Pattern ref).
