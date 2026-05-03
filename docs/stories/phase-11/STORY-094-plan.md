# Implementation Plan: STORY-094 — SIM-Device Binding Model + Policy DSL Extension

## Goal
Land the SIM-Device binding data model (TBL-10 column extension + TBL-59 imei_history + TBL-60 sim_imei_allowlist), the four backend endpoints (API-327/328/330/336), the Policy DSL `device.*` namespace + `tac()` + `device.imei_in_pool()` predicate, and complete STORY-093's parser→listener gap by wiring `ExtractTerminalInformation` at the Diameter S6a Notify-Request / ULR listen path (capture only — no enforcement).

## Architecture Context

### Components Involved
- **SVC-03 Core API** — handlers under `internal/api/sim/` (existing handler/bulk_handler patterns)
- **SVC-05 Policy Engine** — `internal/policy/dsl/{lexer,parser,evaluator}.go` (extend reserved tokens + condition fields)
- **SVC-04 AAA Engine** — `internal/aaa/diameter/handler.go` (S6a Notify/ULR listener; ZERO new SBA/RADIUS edits)
- **store layer** — extend `internal/store/sim.go` (existing `SIMStore`, NOT a new store), add new `internal/store/sim_imei_allowlist.go`
- **migrations/** — three new SQL pairs at `20260507000001`, `20260507000002`, `20260507000003`
- **gateway/router.go** — register API-327/328/330/336 routes

### Data Flow
```
PATCH /sims/{id}/device-binding
  → sim handler (validate enum + 15-digit IMEI)
  → SIMStore.SetDeviceBinding (UPDATE on partition-aware sims, RLS via tenant_id)
  → audit.CreateEntry(action="sim.binding_mode_changed", hash-chained)
  → 200 + envelope

POST /sims/bulk/device-bindings
  → BulkHandler ingests multipart CSV (iccid, bound_imei, binding_mode)
  → enqueues SVC-09 job (reuse STORY-013 job infra) → 202 {job_id}
  → worker iterates rows: ICCID lookup → SetDeviceBinding → per-row audit
  → results retrievable via existing job-result endpoint

GET /sims/{id}/device-binding
  → SIMStore.GetDeviceBinding → DTO with nullable fields + history_count from imei_history

GET /sims/{id}/imei-history
  → cursor + since(RFC3339) + protocol filter → imei_history rows

DSL evaluation (NOT on hot AAA path in this story):
  → SessionContext flat fields (IMEI, SoftwareVersion existing; +BindingMode, +BoundIMEI, +BindingStatus, +TAC computed by tac())
  → evaluator.getConditionFieldValue("device.imei", ...) returns ctx.IMEI
  → tac(device.imei) → first 8 chars of IMEI when len==15, else ""
  → device.imei_in_pool('whitelist'|'greylist'|'blacklist') → false placeholder (STORY-095 wires real lookup)

Diameter S6a listener (capture only, completes 093):
  → diameter handler dispatches CommandNotifyRequest / CommandUpdateLocationRequest
  → ExtractTerminalInformation(avps) → (imei, sv, err)
  → on ErrIMEICaptureMalformed: reg.IncIMEICaptureParseErrors("diameter_s6a") + WARN log
  → on success: write IMEI/SV onto SessionContext + Session.IMEI / Session.SoftwareVersion (in-memory + Redis only — NO DB column, AC-9/F-A2 contract preserved)
```

### API Specifications

All endpoints use the standard envelope `{ status, data, meta?, error? }`. Authenticated via existing JWT/api-key middleware. Tenant-scoped via existing `tenant_id` middleware.

#### API-327: GET /api/v1/sims/{id}/device-binding
- Path param: `id` (UUID)
- Success 200:
  ```json
  { "status": "success", "data": {
      "bound_imei": "359211089765432" | null,
      "binding_mode": "strict" | null,
      "binding_status": "verified" | "pending" | "mismatch" | "unbound" | "disabled" | null,
      "binding_verified_at": "2026-05-07T10:00:00Z" | null,
      "last_imei_seen_at": "..." | null,
      "binding_grace_expires_at": "..." | null,
      "history_count": 0
  }}
  ```
- 404 `SIM_NOT_FOUND` cross-tenant or missing.

#### API-328: PATCH /api/v1/sims/{id}/device-binding
- Body: `{ "binding_mode"?: "<enum>"|null, "bound_imei"?: "<15 digits>"|null, "binding_status_override"?: "<enum>"|null }`
- 200 with same DTO as API-327.
- 422 `INVALID_BINDING_MODE` if `binding_mode` not in `{strict,allowlist,first-use,tac-lock,grace-period,soft}` (NULL allowed and clears related fields).
- 422 `INVALID_IMEI` if `bound_imei` is not exactly 15 ASCII digits.
- Audit: `sim.binding_mode_changed` (entity_type=sim, entity_id=simID, before/after JSON), hash-chain via existing `audit.Auditor.CreateEntry`.

#### API-330: GET /api/v1/sims/{id}/imei-history
- Query: `cursor`, `limit` (default 50, max 200), `since` (RFC3339), `protocol` (`radius|diameter_s6a|5g_sba`).
- 200: `{ "status":"success", "data":[ { "id","sim_id","tenant_id","observed_imei","observed_software_version","observed_at","capture_protocol","nas_ip_address","was_mismatch","alarm_raised" } ], "meta":{"next_cursor":"...","limit":N,"has_more":bool} }`
  - `sim_id` and `tenant_id` are echoed for debug/correlation. Both are derivable from the route + request tenant; included verbatim per F-A5 gate decision (doc-match implementation, not a leak).
- 404 cross-tenant.

#### API-336: POST /api/v1/sims/bulk/device-bindings
- Multipart form-data: `file=<csv>` with header `iccid,bound_imei,binding_mode`.
- 202 `{"status":"success","data":{"job_id":"<uuid>"}}` enqueues an SVC-09 job (reuse STORY-013 `JobStore.Enqueue` + worker registration).
- Per-row outcomes (success / unknown_iccid / invalid_imei / invalid_mode) accumulate in the existing job-result endpoint.
- One audit row per successful row (`sim.binding_mode_changed`).

### Database Schema

> Source for `sims` table: ACTUAL — `migrations/20260320000002_core_schema.up.sql:275-312`. The table is `PARTITION BY LIST (operator_id)` with a `sims_default` catch-all partition.
> Source for TBL-59/TBL-60: DESIGN — `docs/architecture/db/_index.md` rows TBL-59/TBL-60. No prior migration. STORY-094 creates these tables.

**Migration 1 — `20260507000001_sim_device_binding_columns.up.sql` (extends TBL-10):**

```sql
-- AC-1: six new nullable columns on parent partitioned table.
-- Per DEV-410, existing rows keep binding_mode IS NULL (no backfill).
-- Per AC-1, ALL six columns are NULLABLE permanently — single-step additive.
ALTER TABLE sims
  ADD COLUMN bound_imei VARCHAR(15) NULL,
  ADD COLUMN binding_mode VARCHAR(20) NULL
    CHECK (binding_mode IN ('strict','allowlist','first-use','tac-lock','grace-period','soft')),
  ADD COLUMN binding_status VARCHAR(20) NULL
    CHECK (binding_status IN ('verified','pending','mismatch','unbound','disabled')),
  ADD COLUMN binding_verified_at TIMESTAMPTZ NULL,
  ADD COLUMN last_imei_seen_at TIMESTAMPTZ NULL,
  ADD COLUMN binding_grace_expires_at TIMESTAMPTZ NULL;

-- Partial index — created on the parent partitioned table; PG auto-propagates
-- to existing + future partitions.
CREATE INDEX IF NOT EXISTS idx_sims_binding_mode
  ON sims (binding_mode)
  WHERE binding_mode IS NOT NULL;
```

**Migration 1 down:**
```sql
DROP INDEX IF EXISTS idx_sims_binding_mode;
ALTER TABLE sims
  DROP COLUMN IF EXISTS binding_grace_expires_at,
  DROP COLUMN IF EXISTS last_imei_seen_at,
  DROP COLUMN IF EXISTS binding_verified_at,
  DROP COLUMN IF EXISTS binding_status,
  DROP COLUMN IF EXISTS binding_mode,
  DROP COLUMN IF EXISTS bound_imei;
```

**Migration 2 — `20260507000002_imei_history.up.sql` (creates TBL-59):**

```sql
CREATE TABLE IF NOT EXISTS imei_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  sim_id UUID NOT NULL,
  observed_imei VARCHAR(15) NOT NULL,
  observed_software_version VARCHAR(2) NULL,
  observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  capture_protocol VARCHAR(20) NOT NULL
    CHECK (capture_protocol IN ('radius','diameter_s6a','5g_sba')),
  nas_ip_address INET NULL,
  was_mismatch BOOLEAN NOT NULL DEFAULT FALSE,
  alarm_raised BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX IF NOT EXISTS idx_imei_history_sim_observed ON imei_history (sim_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_imei_history_tenant ON imei_history (tenant_id);

ALTER TABLE imei_history ENABLE ROW LEVEL SECURITY;
CREATE POLICY imei_history_tenant_isolation ON imei_history
  USING (tenant_id = current_setting('app.current_tenant_id', true)::uuid);
```
Down: `DROP POLICY ... ; DROP TABLE imei_history;` (idempotent guards).

**Migration 3 — `20260507000003_sim_imei_allowlist.up.sql` (creates TBL-60):**

```sql
CREATE TABLE IF NOT EXISTS sim_imei_allowlist (
  sim_id UUID NOT NULL,
  imei VARCHAR(15) NOT NULL,
  added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  added_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  PRIMARY KEY (sim_id, imei)
);
-- NOTE: cannot FK directly to sims(id) because sims PK is composite
-- (id, operator_id) due to LIST partitioning. RLS is enforced via the
-- parent SIM lookup at store layer (see AC-6). Cleanup on SIM delete is
-- handled via store-layer logic + tenant scoping check.

ALTER TABLE sim_imei_allowlist ENABLE ROW LEVEL SECURITY;
-- RLS is "deny by default unless sim_id resolves to current tenant".
-- Cross-tenant Add/Remove/IsAllowed must be guarded at store layer
-- (lookup sims first, verify tenant_id match before issuing the query).
```
Down: `DROP TABLE sim_imei_allowlist;`.

> **PAT-022 discipline:** the Go const set for `binding_mode` and `binding_status` must match the SQL CHECK exactly. Add `TestBindingModeConstSetMatchesCheckConstraint` (no-DB structural test parsing the migration file).

### Store Surface

`internal/store/sim.go` extensions (AC-5):
- `simColumns` constant: append `, bound_imei, binding_mode, binding_status, binding_verified_at, last_imei_seen_at, binding_grace_expires_at` (six new fields after `updated_at`).
- `SIM` struct: add six fields with pointer types where nullable (`*string`, `*time.Time`).
- `scanSIM`: extend Scan args (count must equal column count — PAT-006 RECURRENCE #3).
- New methods:
  - `GetDeviceBinding(ctx, tenantID, simID) (*DeviceBinding, error)`
  - `SetDeviceBinding(ctx, tenantID, simID, mode *string, boundIMEI *string, statusOverride *string) (*DeviceBinding, error)`
  - `ClearBoundIMEI(ctx, tenantID, simID) error`
- All UPDATE call sites that use `RETURNING simColumns` must be re-verified after `simColumns` extension. Audit list — exact grep:
  ```
  grep -n 'RETURNING.*simColumns\|scanSIM(' internal/store/sim.go
  ```

`internal/store/sim_imei_allowlist.go` (new — AC-6):
- `Add(ctx, tenantID, simID uuid.UUID, imei string) error`
- `Remove(ctx, tenantID, simID uuid.UUID, imei string) error`
- `List(ctx, tenantID, simID uuid.UUID) ([]string, error)`
- `IsAllowed(ctx, tenantID, simID uuid.UUID, imei string) (bool, error)`
- All four methods MUST first verify `simID` belongs to `tenantID` via a `sims` lookup (returns `false`/`ErrSIMNotFound` cross-tenant, never leaks data).

`internal/store/imei_history.go` (new — read side for AC-10):
- `List(ctx, tenantID, simID uuid.UUID, params ListIMEIHistoryParams) ([]IMEIHistoryRow, nextCursor string, err error)` with `Cursor`, `Limit`, `Since *time.Time`, `Protocol *string`.
- `Append(ctx, params AppendIMEIHistoryParams)` reserved for STORY-096 — out of scope for STORY-094 except as a stub method; integration tests can insert rows directly via SQL fixture.

### DSL Extension (AC-11/12/13)

**Lexer:** no change required — `readIdentifier` already accepts `.` (line 217). `device.imei`, `sim.binding_mode`, etc. tokenize as `TokenIdent` with the literal preserved as-is. Function call `tac(...)` and `device.imei_in_pool(...)` use the existing `(`/`,`/`)` handling.

**Parser:** the parser uses `validMatchFields` (line 30) for `MATCH` clause field validation but the condition parser (`parseSimpleCondition`, line 526) does NOT validate field names — it accepts arbitrary identifiers and leaves resolution to the evaluator. Therefore reserved-token discipline is enforced by:
1. Adding the new identifiers to `validMatchFields` is NOT desired (these are condition fields, not match fields).
2. Add a new `validConditionFields` whitelist (or extend the evaluator's `getConditionFieldValue` switch and let unknown identifiers evaluate to `nil`/false). RECOMMENDED: extend evaluator without parser-level whitelist for v1, matching existing condition-field handling. Add to `Vocab()` snapshot for FE autocomplete (extend `vocab.go`).

**Function-call surface:** `tac(device.imei)` and `device.imei_in_pool('whitelist')` are NEW shapes. The current `parseSimpleCondition` reads `Field = identifier`, `Op = comparator`, `Values = parseValueList()`. For `tac(...)` the LHS is a function call, not a bare identifier — this is a parser-level extension. Plan: extend `parseSimpleCondition` to detect `IDENT '(' ... ')'` and emit a function-call `Field` form (e.g. literal `"tac:device.imei"` for evaluator dispatch). For `device.imei_in_pool('whitelist') = true` the function form already produces a boolean, so accept the `=` comparator with `true`/`false` literal RHS, OR sugar as `device.imei_in_pool('whitelist')` standalone (treat as truthy). RECOMMENDED: encode as `Field = "device.imei_in_pool"`, `Values = [pool_name]` parsed via the existing `parseValueList` after a function-call shape detection.

**Evaluator (`evaluator.go:236 getConditionFieldValue`):** add cases:
- `"device.imei"` → `ctx.IMEI`
- `"device.imeisv"` → `ctx.IMEI + ctx.SoftwareVersion` when both present, else `""`
- `"device.software_version"` → `ctx.SoftwareVersion`
- `"device.tac"` → first 8 chars of `ctx.IMEI` when `len(ctx.IMEI) == 15`, else `""`
- `"device.binding_status"` → `ctx.BindingStatus` (NEW field)
- `"sim.binding_mode"` → `ctx.BindingMode` (NEW field)
- `"sim.bound_imei"` → `ctx.BoundIMEI` (NEW field)
- `"sim.binding_verified_at"` → `ctx.BindingVerifiedAt` (NEW field)

**Function dispatch:**
- `tac(<field-ref>)`: compute first 8 chars when input value is exactly 15 ASCII digits, else `""`.
- `device.imei_in_pool(<pool>)`: returns `false` always (placeholder until STORY-095 wires the pool tables).

**SessionContext extension (`evaluator.go:9-26`):**
```go
type SessionContext struct {
    // ... existing fields ...
    IMEI               string `json:"imei,omitempty"`
    SoftwareVersion    string `json:"software_version,omitempty"`
    // STORY-094 — flat binding fields. Hot-path AAA WILL NOT populate
    // BindingStatus/BindingVerifiedAt in this story (no enforcement); they
    // remain zero-valued so dry-run policies that reference them evaluate
    // to "" against synthetic ctx.
    BindingMode        string    `json:"binding_mode,omitempty"`
    BoundIMEI          string    `json:"bound_imei,omitempty"`
    BindingStatus      string    `json:"binding_status,omitempty"`
    BindingVerifiedAt  string    `json:"binding_verified_at,omitempty"` // RFC3339 string for DSL string compare
}
```

> **D-183 fold-in: NO.** Story is flat-fielded throughout AC-11/12; nested `Device{}` is a separate refactor flagged in PROTOCOLS.md. PEIRaw is 5G-SBA-only forensic. Re-target D-183 to STORY-097 (re-pair forensic flow) in ROUTEMAP.

### D-182 Disposition: **(A) Wire S6a Notify-Request / ULR listener as part of STORY-094**

Justification — capture-wiring is NOT enforcement:
- ADR-004 places enforcement (Access-Reject on `binding_status='mismatch'` + strict mode) on the AAA hot path — that is STORY-096 territory.
- STORY-093 reviewer handoff explicitly assigned the listener wire-up to STORY-094 (handoff note 2: "STORY-094 S6a enricher is the intended first consumer — wire `ExtractTerminalInformation` at the Diameter Notify-Request / ULR listen path in this story").
- The listener writes IMEI onto in-memory Session + Redis cache + SessionContext only. It does not call `SetDeviceBinding`, does not insert into `imei_history`, does not change `binding_status`. Pure read-side capture, parity with the RADIUS+SBA wiring shipped in STORY-093.

Files touched: `internal/aaa/diameter/handler.go` (locate Notify-Request / ULR dispatch — call `diameter.ExtractTerminalInformation(req.AVPs)` after request validation, on success write `sess.IMEI = imei; sess.SoftwareVersion = sv` and persist via existing Redis update path; on `ErrIMEICaptureMalformed` increment `reg.IncIMEICaptureParseErrors("diameter_s6a")` + WARN log). NO new metrics, NO new audit rows, NO DB writes.

### Threading Path (Up-Front — PAT-017 Mitigation)

The new `simAllowlistStore` (and `imeiHistoryStore`) must thread from `cmd/argus/main.go` to:
1. `cmd/argus/main.go` — instantiate alongside existing `simStore := store.NewSIMStore(pg.Pool)` (~line 629). Add `simAllowlistStore := store.NewSIMIMEIAllowlistStore(pg.Pool)` and `imeiHistoryStore := store.NewIMEIHistoryStore(pg.Pool)`.
2. `internal/api/sim/device_binding_handler.go` (NEW) — accept both new stores + existing `simStore` + `audit.Auditor` via constructor.
3. `internal/gateway/router.go` (line ~444) — register the four routes after the existing `/api/v1/sims/{id}/...` block (AC-7/8/9/10):
   - `r.Get("/api/v1/sims/{id}/device-binding", deps.SIMDeviceBindingHandler.Get)`
   - `r.Patch("/api/v1/sims/{id}/device-binding", deps.SIMDeviceBindingHandler.Patch)`
   - `r.Get("/api/v1/sims/{id}/imei-history", deps.SIMDeviceBindingHandler.History)`
   - `r.Post("/api/v1/sims/bulk/device-bindings", deps.SIMBulkHandler.DeviceBindingsCSV)` (extend existing BulkHandler)
4. `internal/gateway/router.go` `Deps` struct — add `SIMDeviceBindingHandler *simapi.DeviceBindingHandler`.

> Existing `simStore` is already threaded everywhere (16 hits in main.go); no change there. Bulk handler already exists and only needs a new method.

### Audit
- Action key: `sim.binding_mode_changed`.
- Payload before/after JSON: `{ "binding_mode", "bound_imei", "binding_status" }` (plus pre-existing audit framework hash chain — automatic via `audit.Auditor.CreateEntry`).
- Bulk path: per-row audit emit (matches STORY-013 / FIX-244 pattern with `bulk: true` flag).

### Components Reuse / Mock Retirement / Design Tokens
N/A — backend-only story.

## Prerequisites
- [x] STORY-093 closed (commit `42b70c5`) — provides `Session.IMEI`, `Session.SoftwareVersion`, `SessionContext.IMEI`, `SessionContext.SoftwareVersion`, RADIUS + 5G SBA capture wiring. Verified: `internal/aaa/session/session.go:89-90`, `internal/policy/dsl/evaluator.go:24-25`.
- [x] STORY-022 (Policy DSL baseline) — `internal/policy/dsl/{lexer,parser,evaluator}.go` exist and are stable.

## Tasks

### Task 1: Migration trio — sims columns + imei_history + sim_imei_allowlist
- **Files:** Create `migrations/20260507000001_sim_device_binding_columns.{up,down}.sql`, `migrations/20260507000002_imei_history.{up,down}.sql`, `migrations/20260507000003_sim_imei_allowlist.{up,down}.sql`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `migrations/20260420000002_sims_fk_constraints.up.sql` (recent ALTER TABLE on `sims` parent partition — confirms partition-aware ALTER pattern); `migrations/20260412000006_rls_policies.up.sql` (RLS pattern with `app.current_tenant_id` setting).
- **Context refs:** "Database Schema" section (full SQL embedded above).
- **What:** Create the three migration pairs verbatim from the embedded SQL. Critical points: (a) ALL six new columns on `sims` are NULLABLE permanently — single-step additive, NO backfill, NO three-step ladder; existing rows untouched per DEV-410. (b) Partial index created on parent partitioned table; PG auto-propagates. (c) `imei_history` and `sim_imei_allowlist` are unpartitioned tables. (d) Both new tables enable RLS; `imei_history` has direct policy; `sim_imei_allowlist` lacks a direct policy (FK is composite — RLS enforced via store-layer SIM lookup, documented in migration comment). (e) Down migrations use `IF EXISTS` and reverse-order DROP.
- **Verify:** `make db-migrate-up` succeeds; `psql -c "\d sims"` shows all 6 new columns; `psql -c "\d imei_history"` and `\d sim_imei_allowlist` show new tables; `psql -c "SELECT COUNT(*) FROM sims WHERE binding_mode IS NOT NULL"` returns 0; round-trip `make db-migrate-down && make db-migrate-up` succeeds.

### Task 2: SIMStore extensions + binding-mode constants
- **Files:** Modify `internal/store/sim.go`; Create `internal/store/sim_binding_consts.go`; Create `internal/store/sim_binding_consts_test.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/store/sim.go:135-200` (existing `simColumns` + `scanSIM` + `Create` + `GetByID`). Mirror the column-extension pattern from FIX-251 (PAT-006 RECURRENCE #3).
- **Context refs:** "Store Surface", "Database Schema".
- **What:** (1) Extend `simColumns` constant to include the six new column names in DECLARED ORDER. (2) Add six fields to `SIM` struct (pointers for nullable). (3) Extend `scanSIM` Scan args to match column count exactly. (4) Audit ALL `RETURNING simColumns` call sites (use exact grep `grep -n 'RETURNING.*simColumns\|scanSIM(' internal/store/sim.go` — every call site uses the helper, so the three edits cover all ~12 sites). (5) Add `GetDeviceBinding`, `SetDeviceBinding`, `ClearBoundIMEI` methods (UPDATE with `WHERE id = $1 AND tenant_id = $2`, return new `DeviceBinding` DTO). (6) New file `sim_binding_consts.go` exports `ValidBindingModes = []string{"strict","allowlist","first-use","tac-lock","grace-period","soft"}` and `ValidBindingStatuses = []string{"verified","pending","mismatch","unbound","disabled"}`. (7) Test `sim_binding_consts_test.go` parses the migration file at `migrations/20260507000001_sim_device_binding_columns.up.sql`, extracts the CHECK enums via regex, and asserts the Go const set equals the SQL set (PAT-022 discipline).
- **Verify:** `go build ./...`; `go test ./internal/store/... -run TestBindingMode` PASS; manual `grep -c '&s\.' internal/store/sim.go` after edit equals `simColumns` field count.

### Task 3: New stores — sim_imei_allowlist + imei_history
- **Files:** Create `internal/store/sim_imei_allowlist.go`, `internal/store/sim_imei_allowlist_test.go`, `internal/store/imei_history.go`, `internal/store/imei_history_test.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/audit.go` (or nearest existing `*Store` struct + `New*Store` factory + RLS-aware tenant lookup). `internal/store/sim.go:187-200` (`GetByID` tenant-scope pattern).
- **Context refs:** "Store Surface", "Database Schema".
- **What:** Implement `SIMIMEIAllowlistStore` with `Add/Remove/List/IsAllowed`; each method first calls `SIMStore.GetByID(ctx, tenantID, simID)` (or equivalent ICCID lookup that asserts tenant) — return `ErrSIMNotFound` cross-tenant. Implement `IMEIHistoryStore.List(ctx, tenantID, simID, params)` with cursor-based pagination (default 50, max 200), `since` and `protocol` filters, ORDER BY `observed_at DESC, id DESC`. Tests use existing pgxmock or the in-process Postgres test harness — follow whatever the closest store test uses.
- **Verify:** `go test ./internal/store/... -run 'TestSIMIMEIAllowlist|TestIMEIHistory'` PASS; tenant-isolation negative tests included.

### Task 4: Device-binding handlers + router wiring (API-327, API-328, API-330)
- **Files:** Create `internal/api/sim/device_binding_handler.go`, `internal/api/sim/device_binding_handler_test.go`; Modify `internal/gateway/router.go`, `cmd/argus/main.go`
- **Depends on:** Task 2, Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/sim/handler.go` (existing `Get`, `Patch` SIM handlers; envelope writers; tenant resolution via context). `internal/gateway/router.go:444-460` (existing `/api/v1/sims/{id}/...` route block).
- **Context refs:** "API Specifications" (327/328/330), "Threading Path", "Audit".
- **What:** Build `DeviceBindingHandler` with three methods. `Get` reads `SIMStore.GetDeviceBinding` + `IMEIHistoryStore` count. `Patch` validates `binding_mode` against `store.ValidBindingModes` (422 `INVALID_BINDING_MODE` on miss), validates `bound_imei` is exactly 15 ASCII digits when non-null (422 `INVALID_IMEI`), calls `SIMStore.SetDeviceBinding`, emits audit `sim.binding_mode_changed` via `audit.Auditor.CreateEntry`. `History` calls `IMEIHistoryStore.List` with cursor+filter parsing. Cross-tenant access returns 404 (NOT 403 — match existing pattern). Wire deps in `Deps` struct + register the three routes. `main.go`: add the two new stores after the existing `simStore` instantiation and pass them into the new handler constructor.
- **Verify:** `go test ./internal/api/sim/... -run 'TestDeviceBinding'` PASS; `go vet ./...` clean; `go build ./...` succeeds.

### Task 5: Bulk CSV ingest (API-336) — BulkHandler extension
- **Files:** Modify `internal/api/sim/bulk_handler.go`, `internal/api/sim/bulk_integration_test.go`; Create new job-worker registration in `internal/job/`
- **Depends on:** Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/sim/bulk_handler.go:851+` (existing bulk preview-count / state-change-by-filter patterns) and one existing job worker (e.g., the policy-rollout worker). FIX-244 audit-fanout pattern (DEV-528) for the per-row audit.
- **Context refs:** "API Specifications > API-336", "Audit".
- **What:** Add `DeviceBindingsCSV` method on `BulkHandler` accepting multipart form `file=<csv>`. Parse header `iccid,bound_imei,binding_mode`, enqueue via existing `JobStore.Enqueue` with a new job kind `sim.device_bindings_bulk`. Worker iterates rows: ICCID lookup via `SIMStore.GetByICCID` (existing) → `SetDeviceBinding` → per-row audit on success → `unknown_iccid` / `invalid_imei` / `invalid_mode` error rows accumulated in job result JSON. 202 with `{job_id}`. Reuse the existing job-result endpoint for outcome retrieval — DO NOT add a new endpoint.
- **Verify:** `go test ./internal/api/sim/... -run 'TestDeviceBindingsBulk'` PASS; integration test fixture: 3-row CSV (1 valid, 1 unknown ICCID, 1 invalid IMEI) → job result reports 1 success + 2 failures.

### Task 6: DSL extension — SessionContext fields + condition fields + tac() + device.imei_in_pool() + parser shape
- **Files:** Modify `internal/policy/dsl/evaluator.go`, `internal/policy/dsl/parser.go`, `internal/policy/dsl/vocab.go`; Create `internal/policy/dsl/evaluator_device_test.go`, `internal/policy/dsl/parser_device_test.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/policy/dsl/evaluator.go:236-268` (`getConditionFieldValue` switch — extension target). `internal/policy/dsl/parser.go:526-568` (`parseSimpleCondition` — function-call extension target). `internal/policy/dsl/evaluator_imei_test.go` (STORY-093 test pattern for IMEI-related evaluator tests). `internal/policy/dsl/parser_test.go` (golden parser test pattern).
- **Context refs:** "DSL Extension", "Validation Trace appendix".
- **What:** (1) Extend `SessionContext` with `BindingMode`, `BoundIMEI`, `BindingStatus`, `BindingVerifiedAt` flat string fields. (2) Extend `getConditionFieldValue` switch with the eight new identifiers (see "DSL Extension" section). (3) Add `tac()` helper: 15-digit guard then `s[0:8]`, else `""`. (4) Add function-call detection in `parseSimpleCondition` — when current token is `IDENT` followed by `LPAREN`, parse arg list, encode as `Field = "<funcname>(<arg-literal>)"` so the evaluator dispatches via a separate switch. (5) `tac(device.imei)` resolves at evaluator time — inner ident resolved via `getConditionFieldValue`, then `tac()` applied. (6) `device.imei_in_pool('whitelist'|'greylist'|'blacklist')` — placeholder returning `false`. (7) Vocab snapshot: extend `Vocab()` to include the new condition fields under a new `ConditionFields []string` slice + the new function names under `Functions []string`. (8) Six golden parser tests: `WHEN device.binding_status == "mismatch"`, `WHEN sim.binding_mode IN ("strict","tac-lock")`, `WHEN tac(device.imei) == "35921108"`, `WHEN device.imei_in_pool("blacklist")`, `WHEN device.software_version != "00"`, combined `WHEN device.binding_status == "mismatch" AND sim.binding_mode IN ("strict","tac-lock") THEN reject` (AC-13 exact policy). (9) Evaluator unit tests: `tac("359211089765432") == "35921108"`, `tac("") == ""`, `tac("123") == ""`, AC-13 evaluation against synthetic ctx (mismatch+strict → reject; verified+strict → allow).
- **Verify:** `go test ./internal/policy/dsl/... -run 'TestDevice|TestTac|TestBindingStatus'` PASS; vocab snapshot test green.

### Task 7: Diameter S6a listener wiring (D-182 closure) + seed verify
- **Files:** Modify `internal/aaa/diameter/handler.go` (or whichever file dispatches Notify-Request / ULR — locate via `grep -rn "CommandNotify\|UpdateLocationRequest\|CommandULR" internal/aaa/diameter/`); Create `internal/aaa/diameter/handler_imei_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/radius/handler.go` IMEI capture wiring shipped in STORY-093 (search for `extractIMEISV` or similar + `sess.IMEI = ...`). Read `internal/aaa/sba/imei_handler_test.go` for capture-wiring test pattern.
- **Context refs:** "D-182 Disposition", "Data Flow > Diameter S6a listener".
- **What:** Locate the Diameter handler dispatch for `Notify-Request` and `Update-Location-Request` (S6a). Immediately after request validation but before any response writing, call `imei, sv, err := diameter.ExtractTerminalInformation(req.AVPs)`. On `errors.Is(err, diameter.ErrIMEICaptureMalformed)`: `reg.IncIMEICaptureParseErrors("diameter_s6a")` + `logger.Warn().Err(err).Msg(...)`. On success: write `sess.IMEI = imei; sess.SoftwareVersion = sv` and re-persist the Redis-cached blob via the existing session update path (do NOT call any DB write — AC-9 / F-A2 contract preserved). Tests cover three cases: AVP absent (no fields set, no counter inc), AVP malformed (counter inc + fields stay empty), AVP valid (fields set on session). After landing: `make db-seed` smoke check — must succeed and produce zero rows with non-NULL `binding_mode` per AC-15.
- **Verify:** `go test ./internal/aaa/diameter/... -run TestS6aIMEICapture` PASS; `make db-seed` succeeds; `psql -c "SELECT COUNT(*) FROM sims WHERE binding_mode IS NOT NULL"` returns 0.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 (sims columns + partial index, NULL default) | Task 1 | Task 1 verify; Task 2 const-set test |
| AC-2 (down/up roundtrip) | Task 1 | Task 1 verify (round-trip step) |
| AC-3 (imei_history TBL-59) | Task 1 | Task 1 verify; Task 3 tests |
| AC-4 (sim_imei_allowlist TBL-60) | Task 1 | Task 1 verify; Task 3 tests |
| AC-5 (SIMStore CRUD + DTO surfacing) | Task 2 | Task 2 build + Task 4 handler tests |
| AC-6 (sim_imei_allowlist store ops) | Task 3 | Task 3 tests (cross-tenant negative) |
| AC-7 (API-327 GET) | Task 4 | Task 4 handler tests |
| AC-8 (API-328 PATCH + 422 + audit) | Task 4 | Task 4 handler tests + audit chain check |
| AC-9 (API-336 bulk CSV) | Task 5 | Task 5 integration test |
| AC-10 (API-330 history + filters) | Task 4 | Task 4 handler tests |
| AC-11 (DSL tokens + 6 golden tests) | Task 6 | Task 6 parser tests |
| AC-12 (evaluator wiring + tac + imei_in_pool placeholder) | Task 6 | Task 6 evaluator tests |
| AC-13 (synthetic ctx full policy) | Task 6 | Task 6 evaluator tests |
| AC-14 (audit hash chain) | Task 4, Task 5 | existing audit verifier exercised in tests |
| AC-15 (regression — full test + db-seed) | Task 7 | Task 7 `make db-seed` step + full `make test` |

## Story-Specific Compliance Rules

- API: standard envelope `{ status, data, meta?, error? }` for all four endpoints; cursor-based pagination on API-330; cross-tenant returns 404 (NOT 403) per existing convention.
- DB: migrations require both `.up.sql` and `.down.sql`; partial index on parent partition; RLS enforced (direct policy on `imei_history`; via store-layer SIM lookup for `sim_imei_allowlist`).
- DSL: `device.*` and `sim.binding_*` are reserved tokens (DSL_GRAMMAR.md). `tac(<field>)` and `device.imei_in_pool(<pool>)` are function-call shapes new to this story.
- Business (DEV-410): existing SIMs must remain `binding_mode IS NULL`; seed must produce zero rows with non-NULL `binding_mode`.
- AAA/F-A2 (STORY-093 Gate): `Session.IMEI` / `SoftwareVersion` persist in-memory + Redis ONLY — STORY-094 introduces NEW DB columns (`bound_imei`, `binding_mode`, etc.) on `sims` directly; no extension of `radius_sessions` / sessions DB rows in this story.
- ADR: ADR-004 — local enforcement architecture; this story stops at parser/evaluator wiring + capture listener, NOT enforcement (STORY-096 territory).

## Bug Pattern Warnings

- **PAT-006 RECURRENCE #3 (FIX-251):** Extending `simColumns` requires three coordinated edits (constant + struct + scan) — Task 2 makes them as a single atomic edit. Audit grep `grep -n 'RETURNING.*simColumns\|scanSIM(' internal/store/sim.go` after edit must show all sites still using the helper.
- **PAT-009:** New nullable columns (`bound_imei`, `binding_mode`, `binding_status`, `binding_verified_at`, `last_imei_seen_at`, `binding_grace_expires_at`) require pointer types (`*string`, `*time.Time`) in the Go struct. The DTO-shaping in Task 4 must marshal `null` (not empty string / zero time) when pointer is nil.
- **PAT-011 / PAT-017 RECURRENCE:** New `simAllowlistStore` + `imeiHistoryStore` must be threaded from main.go → Deps → handler constructor → router. Up-front threading-path documented in "Threading Path (Up-Front — PAT-017 Mitigation)". Three call sites only: instantiation, handler constructor, router registration.
- **PAT-022:** `binding_mode` and `binding_status` CHECK enums must equal the Go const sets in `internal/store/sim_binding_consts.go`. Task 2 ships a structural test (`TestBindingModeConstSetMatchesCheckConstraint`) that re-parses the migration SQL at runtime to assert equality.
- **PAT-023:** `schema_migrations` can lie. After landing the migration trio, post-merge boot-time `schemacheck` guard must catch any drift — verify on first server boot under `make up`.
- **PAT-025:** IMEI / IMEISV / IMSI all string-typed. `bound_imei VARCHAR(15)` is the IMEI (equipment); existing `imsi VARCHAR(15)` is the subscriber. Confirm column-name discipline; Task 4 PATCH validation rejects non-15-digit values explicitly.
- **PAT-026:** No feature is being removed, but the new `sim_imei_allowlist` table has NO direct PG-level FK to `sims` (composite-PK partition issue). Cleanup-on-SIM-delete must be store-layer logic, not assumed CASCADE. Document in migration comment.

## Tech Debt (from ROUTEMAP)

- **D-182 (Diameter listener orphan):** CLOSED by this story (Task 7) — disposition (A) per advisor: capture-only wiring at the S6a Notify-Request / ULR path. Update ROUTEMAP D-182 status to `RESOLVED [STORY-094 Task 7]`.
- **D-183 (PROTOCOLS device.peri_raw / `SessionContext.PEIRaw`):** Re-targeted to STORY-097 (re-pair forensic flow). Reasoning: STORY-094 is fully flat-fielded per AC-12; the nested `Device{}` refactor PROTOCOLS.md hints at is its own cross-protocol scope and PEIRaw is 5G-SBA-only forensic. Update ROUTEMAP D-183 target → STORY-097.
- **D-184 (1M-SIM bench):** Re-targeted to STORY-096. Reasoning: STORY-094 has explicit no-enforcement-on-AAA-path scope; nothing measurable. The bench should run when STORY-096 introduces the binding pre-check on the auth hot path. Update ROUTEMAP D-184 target → STORY-096.
- **D-185, D-186:** out of scope.

## Mock Retirement
N/A — backend story with no FE mocks for these endpoints (UI controls land in STORY-097).

## Risks & Mitigations
- **R1: Partial index on parent partitioned table.** PG ≥11 supports partitioned indexes; ours is PG16 — confirmed safe. Mitigation: Task 1 verify step explicitly checks `\d sims` after migration shows the index propagated.
- **R2: `sim_imei_allowlist` lacks direct PG FK to `sims` (composite PK).** Mitigation: store-layer guards every Add/Remove/List/IsAllowed with a `SIMStore.GetByID` tenant check. Documented in migration SQL comment.
- **R3: Parser function-call extension may break existing policies.** Mitigation: Task 6 runs the full `make test` against the policy DSL test suite; vocab snapshot test compares before/after. Function-call detection is gated on `IDENT '(' ` lookahead — a bare identifier still parses as today.
- **R4: D-182 wiring on the Diameter handler may regress unrelated S6a paths.** Mitigation: Task 7 wraps the call in a single `if`/`else` branch immediately after request validation; on any error path the existing flow runs unchanged. Negative-path test covers AVP-absent (zero behavior change).

## Validation Trace (Planner Quality Gate appendix)

> Re-verifiable by Quality Gate before plan acceptance.

**Trace V1 — `tac()` semantics (AC-12):**
- Input `"359211089765432"` (15 digits) → `len==15` guard passes → `s[0:8]` → `"35921108"`. ✅
- Input `""` → `len==0`, guard fails → `""`. ✅
- Input `"123"` → `len==3`, guard fails → `""`. ✅
- Input `"35921108976543A"` (14 digits + letter) — guard is `len==15`-only OR `len==15 && all-digits`? Spec says "first 8 digits of `device.imei`, or empty string when `device.imei` is empty". RECOMMENDED implementation: `len==15` only (matches spec literally; non-digit IMEIs cannot reach evaluator because `ExtractTerminalInformation` already validates digits — defensive but spec-aligned). Edge case documented for Reviewer.

**Trace V2 — AC-13 evaluation against synthetic SessionContext:**
- Policy: `WHEN device.binding_status == "mismatch" AND sim.binding_mode IN ("strict","tac-lock") THEN reject`
- Synthetic ctx 1: `BindingStatus="mismatch", BindingMode="strict"` → `device.binding_status == "mismatch"` → true; `sim.binding_mode IN ("strict","tac-lock")` → true; AND → true → action `reject` triggers; `result.Allow=false`. ✅
- Synthetic ctx 2: `BindingStatus="verified", BindingMode="strict"` → first comparator → false; AND short-circuits → false; no action; `result.Allow=true`. ✅
- Synthetic ctx 3: `BindingStatus="mismatch", BindingMode="soft"` → first → true; second `IN ("strict","tac-lock")` against `"soft"` → false; AND → false; no action; `result.Allow=true`. ✅

**Trace V3 — Migration default (AC-1 / DEV-410):**
- Pre-migration row example: `iccid='8990123456789012345', operator_id='<uuid>'`, no `bound_imei` column.
- Post-migration: `ALTER TABLE sims ADD COLUMN bound_imei VARCHAR(15) NULL, ...` → row gets `bound_imei IS NULL, binding_mode IS NULL, binding_status IS NULL, ...` (PG default for new nullable column = NULL on existing rows). ✅
- AC-15 verification: `SELECT COUNT(*) FROM sims WHERE binding_mode IS NOT NULL;` post-`make db-seed` returns `0`. ✅

**Trace V4 — DSL evaluator condition-field switch coverage:**
- Eight new identifiers: `device.imei` → `ctx.IMEI`; `device.tac` → `tac(ctx.IMEI)` (= first 8 of IMEI); `device.imeisv` → `ctx.IMEI + ctx.SoftwareVersion`; `device.software_version` → `ctx.SoftwareVersion`; `device.binding_status` → `ctx.BindingStatus`; `sim.binding_mode` → `ctx.BindingMode`; `sim.bound_imei` → `ctx.BoundIMEI`; `sim.binding_verified_at` → `ctx.BindingVerifiedAt`. ✅ Each maps to one of the four NEW or two EXISTING flat fields.

**Trace V5 — `device.imei_in_pool` placeholder:**
- Any pool name (`whitelist|greylist|blacklist`) → returns `false`. STORY-095 wires real lookup via `imei_whitelist/greylist/blacklist` tables. ✅

**Trace V6 — Migration schema enum vs. Go const equality (PAT-022):**
- SQL `binding_mode` CHECK: `('strict','allowlist','first-use','tac-lock','grace-period','soft')` — 6 values.
- Go `ValidBindingModes`: `["strict","allowlist","first-use","tac-lock","grace-period","soft"]` — same 6 values, same order (test asserts set equality, not order).
- SQL `binding_status` CHECK: `('verified','pending','mismatch','unbound','disabled')` — 5 values.
- Go `ValidBindingStatuses`: `["verified","pending","mismatch","unbound","disabled"]` — same 5 values.

## decisions.md Entries (route to ROUTEMAP)

- **VAL-NNN-1:** D-182 disposition (A) — STORY-094 wires the S6a Notify/ULR listener as capture-only completion of STORY-093's parser→listener gap. NOT enforcement (STORY-096 owns that).
- **VAL-NNN-2:** D-183 fold-in NO — STORY-094 keeps SessionContext flat per AC-12; PEIRaw is 5G-SBA-only forensic; re-target D-183 → STORY-097.
- **VAL-NNN-3:** D-184 re-target — STORY-094 has no binding pre-check on auth hot path (no enforcement); 1M-SIM bench moves to STORY-096.
- **VAL-NNN-4:** Migration is single-step additive (NULL permanent) per AC-1 — NO three-step ladder, contradicts AC-1 "existing rows untouched".
- **VAL-NNN-5:** PAT-022 const-set discipline embedded in Task 2 via `sim_binding_consts_test.go` runtime SQL re-parse.
- **VAL-NNN-6:** Worked-example independent computation rule applied — Validation Trace appendix (V1–V6) verifies each example before plan acceptance.

## Pre-Validation Self-Check
- [x] Min plan lines ≥60 (M effort): well over.
- [x] Min task count ≥3 (M effort): 7 tasks landed.
- [x] At least one `Complexity: high` task (Tasks 1, 2, 6).
- [x] Required sections present: Goal, Architecture Context, Tasks, Acceptance Criteria Mapping.
- [x] API specs embedded (327/328/330/336).
- [x] DB schema embedded with source noted (mix of ACTUAL `sims` migration + DESIGN for new tables).
- [x] No UI surface — Design Token Map / Component Reuse / Mock Retirement marked N/A.
- [x] Each task has `Pattern ref`, `Context refs`, `Verify`.
- [x] All `Context refs` point to sections that exist in this plan.
- [x] D-182 disposition explicit (A) with reasoning; D-183 explicit NO; D-184 re-target.
- [x] Validation Trace appendix V1–V6 included.
