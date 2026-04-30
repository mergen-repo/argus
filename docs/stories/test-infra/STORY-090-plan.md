# Implementation Plan: STORY-090 — Multi-protocol operator adapter refactor (nested JSONB adapter_config)

> Plan drafted 2026-04-18 by the Amil Planner agent after code-state
> validation against the current tree (post-STORY-092 DONE 2026-04-18)
> and advisor pre-draft consultation that reframed the D1 decision
> around the pre-existing app-layer AES-GCM envelope on
> `operators.adapter_config` — a constraint the dispatch text missed.
>
> **Revised 2026-04-18 per user decision-point selection (LOCKED):**
> D1-A, **D2-B (user override of planner D2-A — drop `adapter_type`
> column)**, D3-B, D4-A. Wave 2 expanded with Task 3b (drop-column
> migration + store/seed/export reader sweep). Task 7c expanded to
> consolidate the dead `supported_protocols` retirement with the
> D2-B `adapter_type` UI removal. AC-12 and AC-13 added to cover
> the column-drop and the zero-reference guard. Risk 8 added to
> capture D2-B reader-sweep miss exposure. Estimate shifted L → XL.

## Goal

Move operators from a single-protocol model (`operators.adapter_type
VARCHAR + adapter_config JSONB` flat blob) to a multi-protocol model
where one operator can have RADIUS, Diameter, SBA, HTTP, and Mock
adapters configured concurrently via a nested `adapter_config` JSONB
with per-protocol sub-keys. Concretely: (1) reshape the
`adapter_config` JSONB from a flat per-type blob into a nested
`{protocol: {enabled, …fields}}` schema; (2) re-key the in-memory
`adapter.Registry` from `map[operatorID]Adapter` to
`map[(operatorID, protocol)]Adapter` so multiple adapters per operator
coexist; (3) update every adapter-resolution call site
(`OperatorHandler.TestConnection`, `HealthChecker.checkOperator`,
`OperatorRouter.*`) to resolve BY PROTOCOL not by operator alone; (4)
ship a per-protocol "Test Connection" API and a new Protocols tab in
the operator detail UI with per-protocol cards (Enable toggle,
protocol-specific fields, Test Connection button); (5) retire the dead
`supported_protocols` frontend field and replace it with derived
display sourced from the nested JSONB. STORY-092's `IPPoolStore` /
`SIMStore` wiring on `aaadiameter.ServerDeps` and `aaasba.ServerDeps`
stays intact and is regression-guarded (AC-9 below).

## Architecture Context

### Current deployed reality (verified 2026-04-18)

#### DB schema — `operators` table

- Source of truth: `migrations/20260320000002_core_schema.up.sql:96-117`.
- Columns relevant to this story:
  ```sql
  adapter_type   VARCHAR(30) NOT NULL,          -- line 102: 'mock'|'radius'|'diameter'|'sba'
  adapter_config JSONB       NOT NULL DEFAULT '{}',  -- line 103: opaque per-type blob
  ```
- **No `supported_protocols` column exists.** The frontend form field
  by that name (see UI section below) is purely cosmetic and never
  persisted.
- Current seeds (`migrations/seed/002_system_data.sql:5`,
  `003_comprehensive_seed.sql:117`, `005_multi_operator_seed.sql:40-49`)
  all INSERT `adapter_type='mock'` plus a plaintext flat
  `adapter_config` (e.g., `'{"latency_ms":12,"simulated_imsi_count":
  1000}'::jsonb` for the mock adapter, shared secrets for the RADIUS/
  Diameter test-only seeds).

#### App-layer encryption envelope on `adapter_config` (advisor flag #1 — dispatch missed this)

The `adapter_config` column is NOT plaintext at runtime.
`internal/api/operator/handler.go:354-363` (Create) and `:453-462`
(Update) wrap inbound plaintext JSON through
`crypto.EncryptJSON(adapterConfig, h.encryptionKey)` before handing it
to the store. `TestConnection` at `:580-587` decrypts via
`crypto.DecryptJSON` before passing to `adapter.Registry.GetOrCreate`.
`HealthChecker.Start` at `internal/operator/health.go:153-157`
similarly decrypts per operator before scheduling probes.

**Envelope shape** (verified from `internal/crypto/aes.go:68-103`):

- **Plaintext** (seed-inserted rows, or runtime when `cfg.EncryptionKey
  == ""`): `adapter_config` = JSON OBJECT, e.g.
  `{"latency_ms":12,"simulated_imsi_count":1000}` — first non-WS byte
  is `{`.
- **Encrypted** (API-inserted rows when `cfg.EncryptionKey != ""`):
  `adapter_config` = JSON STRING containing a base64-encoded
  AES-256-GCM ciphertext of the plaintext bytes. The RawMessage stored
  in the column is literally e.g.
  `"U2FsdGVkX1+…=="` (quoted string with base64 payload). First
  non-WS byte is `"` — clean discriminator.
- `crypto.EncryptJSON` returns `json.RawMessage(fmt.Sprintf("%q",
  string(encrypted)))` at `aes.go:80-81` — the outer JSON is always a
  string when encrypted, always an object when plaintext.

**Implication**: a pure SQL migration CANNOT reshape encrypted values
(no access to the hex key from SQL). Any in-place reshape MUST happen
in Go with `cfg.EncryptionKey` in scope. This kills the dispatch-text
framing of "in-place transform vs dual-write + flag vs shadow column"
as stated — see D1 below for the correct axis.

#### Backend — handler surface and validation

- `internal/api/operator/handler.go:20-25` —
  `validAdapterTypes = {mock, radius, diameter, sba}` (no `http`).
  Handler test at `handler_test.go:148` asserts `http` is NOT a valid
  type today.
- `:93` `operatorResponse.AdapterType string` serialised as
  `"adapter_type"`.
- `:135-151` `createOperatorRequest` accepts both
  `adapter_type string` AND `adapter_config json.RawMessage` — so the
  raw JSONB pass-through is already wired, just not exercised.
- `:153-166` `updateOperatorRequest` accepts `adapter_config
  json.RawMessage` for PATCH-like updates.
- `:341-345` Create validates `req.AdapterType` is non-empty and in
  the allowlist.
- `:475-501` Update persists `adapter_config` if provided; on change,
  `h.adapterRegistry.Remove(id)` at :500 invalidates the cached
  adapter so the next request re-creates it from the new config.
- `:560-605` `TestConnection`: resolves the operator's current
  `AdapterType` + decrypted `AdapterConfig`, calls
  `h.adapterRegistry.GetOrCreate(id, op.AdapterType, adapterConfig)`,
  then `a.HealthCheck(r.Context())`. Returns
  `{success, latency_ms, error}`. **Keyed by operator ID alone — no
  protocol concept.**

#### Backend — store layer

- `internal/store/operator.go:30` — `Operator.AdapterType string`
  scanned at :139, :288, :417.
- `:94` `CreateOperatorParams.AdapterType string` + `AdapterConfig
  json.RawMessage`.
- `:192` INSERT binds both columns.
- `UpdateOperatorParams.AdapterConfig` supports updating the JSONB in
  place via `UpdateOperatorFields`. No `adapter_type` update path
  (immutable after create) — developer must NOT introduce one in this
  story.

#### Backend — adapter registry (advisor flag #2 — structural blast radius)

- `internal/operator/adapter/registry.go:11-17` — the core type:
  ```go
  type Registry struct {
      mu        sync.RWMutex
      factories map[string]AdapterFactory         // protocol → factory
      adapters  map[uuid.UUID]Adapter             // operatorID → ONE adapter
  }
  ```
  **`adapters` keyed by operator alone** — this is the key structural
  change STORY-090 requires. Must become `map[adapterKey]Adapter`
  where `adapterKey = struct { OperatorID uuid.UUID; Protocol string }`
  or equivalent.
- `:59-82` `GetOrCreate(operatorID, adapterType, config)` — returns
  one adapter per operator; must become
  `GetOrCreate(operatorID, protocol, config)` (same shape, but the
  map key includes protocol).
- `:84-101` `Set`/`Get`/`Remove(operatorID)` — all single-key. All
  must be extended OR supplemented with a per-protocol variant.
- Factories at `:25-36` cover `mock|radius|diameter|sba` today. This
  story adds `http` (see D3-B decision below — same pattern).

#### Backend — adapter resolution call sites (advisor flag #2)

All of these break if the Registry key changes without coordinated
updates:

1. **`internal/api/operator/handler.go:500`** — `h.adapterRegistry.
   Remove(id)` on Update. Must be replaced with a loop over every
   enabled protocol in the NEW adapter_config AND every protocol in
   the BEFORE snapshot (handle the case where an update disables a
   protocol).
2. **`internal/api/operator/handler.go:589`** — `h.adapterRegistry.
   GetOrCreate(id, op.AdapterType, adapterConfig)` inside
   `TestConnection`. The endpoint itself must gain a protocol URL
   parameter (see D3 and API spec below).
3. **`internal/operator/health.go:174`** — currently seeds the health-
   check goroutine with `op.ID, op.AdapterType, adapterConfig`. Must
   fanout: one goroutine per enabled protocol per operator.
4. **`internal/operator/health.go:181`** — `hc.registry.GetOrCreate
   (opID, adapterType, config)` inside `checkOperator`. Becomes
   per-protocol.
5. **`internal/operator/router.go:72`** — `r.registry.Set(operatorID,
   a)`. Router must key breakers per `(operatorID, protocol)`.
6. **`internal/operator/router.go:93`** — `RemoveOperator` registry
   clear.
7. **`internal/operator/router.go:98-104`** — `GetAdapter(operatorID)`
   returns one adapter. Signature must become
   `GetAdapter(operatorID, protocol) (Adapter, error)`.
8. **`internal/operator/router.go:112-160, 254-304`** —
   `ForwardAuth`, `ForwardAcct`, `SendCoA`, `SendDM`,
   `Authenticate`, `AccountingUpdate`, `FetchAuthVectors` — all take
   `operatorID uuid.UUID` as the adapter key. Each must gain a
   `protocol string` parameter.
9. **`internal/operator/router.go:312, 338`** —
   `HealthCheck(ctx, operatorID)` and
   `resolveWithCircuitBreaker(operatorID)` use the single-key lookup.
10. **Circuit breakers keyed by operatorID** at `router.go:18` —
    `breakers map[uuid.UUID]*CircuitBreaker`. A breaker per
    `(operatorID, protocol)` is the honest shape, but (see D4 below)
    the router is currently dead code, so this decision is deferrable.

#### OperatorRouter is currently dead code (advisor flag #3)

`cmd/argus/main.go:813-814`:
```go
operatorRouter := operator.NewOperatorRouterFromConfig(cfg, adapterRegistry, log.Logger)
_ = operatorRouter
```

The router is instantiated but never passed to any AAA server. Every
hot path (RADIUS `server.go`, Diameter `server.go`+`gx.go`, SBA
`server.go`) talks to adapters directly through local wiring (or
doesn't talk to them at all — see STORY-089's scope). Failover tests
at `internal/operator/failover_test.go` exercise the router in
isolation, but it never runs against production traffic.

**Consequence**: STORY-090 can choose whether to (a) refactor the
router API to accept `protocol` and leave it still dead, or (b) leave
the router API unchanged and let the registry refactor below do the
work. Surfaced as **D4** below.

#### AAA server wiring — STORY-092 surface

Per the STORY-092 plan (shipped 2026-04-18, commit `b8c5c15`):

- `internal/aaa/diameter/server.go:26-31` — `ServerDeps` now contains
  `IPPoolStore *store.IPPoolStore` and `SIMStore *store.SIMStore`.
  `cmd/argus/main.go:987-996` (approximate post-092 line range — confirm
  in Wave 2 Task 5) passes `ippoolStore` and `simStore` into
  `aaadiameter.ServerDeps{...}`.
- `internal/aaa/sba/server.go` ServerDeps similarly threads
  `IPPoolStore`, `SIMStore`, and the shared radius `SIMCache`.
- **STORY-090 regression guard (AC-9 below)**: these ServerDeps stay
  intact. No field is removed or renamed; new adapter-resolution
  helpers (e.g., a per-protocol adapter resolver) slot into the
  existing ServerDeps shape OR land on a new deps struct wired
  alongside without touching the IPPoolStore/SIMStore plumbing.

The AAA servers themselves do NOT currently call `adapter.Registry`
directly — they resolve SIM → operator locally. The registry is used
only by (a) `operatorHandler.TestConnection`, (b) `healthChecker`
(for periodic probes), (c) the dead router. That is the entire blast
radius for the registry key change. STORY-090 is NOT expected to wire
the registry into the AAA hot paths; that is STORY-089's work.

#### Frontend — dead `supported_protocols` field (advisor hard flag #2)

- `web/src/pages/operators/index.tsx:192` declares
  `const PROTOCOL_OPTIONS = ['radius', 'diameter', 'sba'] as const`.
- `:197-205` Create-operator form state holds
  `supported_protocols: [] as string[]` alongside `adapter_type` and
  `supported_rat_types`.
- `:209-216` `toggleProtocol(proto)` mutates the local state.
- `:234-241` `handleSubmit` calls
  `createMutation.mutateAsync({ name, code, mcc, mnc, adapter_type,
  supported_rat_types })` — **`supported_protocols` is never sent.**
- `:242` state reset still tracks the dead field.
- `:301-325` UI renders protocol toggle chips that users can click but
  that do nothing.
- `git log -S "supported_protocols"` shows only commit `9ac750a`
  (batch UX commit) introducing the field cosmetically — no backend
  persistence was ever added.
- Additionally, the frontend types/hooks declare `adapter_type` only
  — `web/src/types/operator.ts:7` and `web/src/hooks/use-operators.ts:62`.

#### Frontend — operator detail form

- `web/src/pages/operators/detail.tsx:221` renders the current single-
  adapter display: `<InfoRow label="Protocol"
  value={ADAPTER_DISPLAY[operator.adapter_type] ?? operator.adapter_type}
  />`.
- `:1280-1319` Tabs list (10 existing tabs: overview, health, circuit,
  traffic, sessions, agreements, audit, alerts, notifications, sims).
  **No Protocols tab.** New tab slots in alongside these.
- `:832-837` `ADAPTER_OPTIONS` array duplicates the create-dialog copy.
- `:841-949` `EditOperatorDialog` has name/code/mcc/mnc/adapter_type/
  supported_rat_types — **no `adapter_config` editor field at all**.
  The encrypted blob is never surfaced in the UI today.
- `:1250` renders `ADAPTER_DISPLAY[operator.adapter_type]` in the
  header chip. Same display path as :221.

### Data flow (post-fix, operator create with two protocols enabled)

```
User clicks "Create Operator" → fills form:
  name="Turkcell", code="TCELL", mcc="286", mnc="01",
  protocols:
    radius  = {enabled: true, shared_secret: "…", listen: ":1812"}
    diameter= {enabled: true, origin_host: "argus.local", peers: [...]}
    sba     = {enabled: false}
    http    = {enabled: false}
    mock    = {enabled: false}
  ↓ POST /api/v1/operators
  { name, code, mcc, mnc,
    adapter_config: { radius:{…}, diameter:{…}, sba:{enabled:false},
                      http:{enabled:false}, mock:{enabled:false} } }
  (NO top-level adapter_type field — the column is DROPPED under
   D2-B; enabled_protocols is computed server-side for responses)
  ↓ handler.go::Create
  validate adapter_config shape (new validator — see Task 2):
    each sub-key is one of {radius,diameter,sba,http,mock}
    each sub-key has `enabled: bool`
    for each enabled sub-key, protocol-specific fields match schema
    at least one sub-key must have enabled=true
  compute enabled_protocols (canonical order)
    [diameter, radius, sba, http, mock] — returned on response DTO
    (NO adapter_type derivation — column is dropped per D2-B)
  encrypt adapter_config as a whole (unchanged from today)
  persist via operatorStore.Create (INSERT no longer binds adapter_type)
  ↓ registry wiring
  (NO eager registration — registry is lazy via GetOrCreate)
  ↓ response 201 Created with adapter_config echoed back (decrypted
    for the caller per existing Create/Update pattern)

User clicks "Test Connection" on the Diameter card in Protocols tab:
  ↓ POST /api/v1/operators/{id}/test-connection/{protocol=diameter}
  handler.go::TestConnectionForProtocol (new):
    op = operatorStore.GetByID(id)
    adapterConfig = crypto.DecryptJSON(op.AdapterConfig, key)
    sub = adapterConfig[protocol]
    if sub == nil || !sub.enabled:
      422 {error: "protocol not configured or disabled"}
    a = adapterRegistry.GetOrCreate(id, protocol, sub-json-only)
    result = a.HealthCheck(ctx)
    return {success, latency_ms, error}
```

### API Specifications

#### Existing — POST `/api/v1/operators` (Create) — schema change

Request body change (nested shape):
```json
{
  "name": "Turkcell",
  "code": "TCELL",
  "mcc": "286",
  "mnc": "01",
  "adapter_config": {
    "radius":   { "enabled": true,  "shared_secret": "…", "listen_addr": ":1812" },
    "diameter": { "enabled": true,  "origin_host": "argus.local", "origin_realm": "argus.local", "peers": [...] },
    "sba":      { "enabled": false },
    "http":     { "enabled": false },
    "mock":     { "enabled": false }
  },
  "supported_rat_types": ["lte","nr_5g"]
}
```

- `adapter_type` field **HARD-REMOVED** from request and response
  DTOs per D2-B (selected). Clients sending `adapter_type` in a
  request body have it ignored by the JSON decoder (no 4xx is
  raised, but no side-effect on the persisted record either); the
  response DTO does NOT echo an `adapter_type` field at all — UI
  reads `enabled_protocols[0]` via `ADAPTER_DISPLAY` for the
  primary-protocol label.
- Backward-compat tolerance for LEGACY flat request shape:
  `adapter_type: "radius", adapter_config: {shared_secret:"…"}` — the
  decoder up-converts to the nested shape `adapter_config: {radius:
  {enabled:true, shared_secret:"…"}}` on the fly. See D1-A below.

Response 201 shape: existing `operatorResponse` fields remain, plus a
new `adapter_config` field (decrypted JSON) exposed on the write path
to match typical PUT/POST idiom. GET /List response does NOT include
`adapter_config` (keep list payloads slim) — per-operator detail GET
returns it.

Error responses:
- 422 `VALIDATION_ERROR` on malformed nested shape. Validation rules
  listed in Task 2.
- 422 if zero protocols enabled (at least one MUST be enabled).

#### Existing — PATCH `/api/v1/operators/{id}` (Update)

Same `adapter_config` shape. Partial sub-key updates are explicit:
clients send the FULL `adapter_config` object; the server does NOT
merge at sub-key granularity. Rationale: matches the existing write
semantics where `adapter_config` is replaced wholesale, and avoids
ambiguity about how to disable a sub-key (send `{…, sba: {enabled:
false}}` explicitly).

Legacy flat requests up-converted identically to Create.

#### Existing — GET `/api/v1/operators/{id}` (Detail)

Response includes:
```json
{
  "id": "…",
  "name": "…",
  // NO adapter_type field — column dropped per D2-B
  "adapter_config": {           // decrypted, nested, secrets masked
    "radius":   {"enabled":true,  …},
    "diameter": {"enabled":true,  …},
    …
  },
  "enabled_protocols": ["diameter","radius"],  // canonical order
  …
}
```

Secrets handling: `shared_secret`, `auth_token`, `client_key`, etc.
are ALWAYS masked in GET responses (return `"****"` or `null`) —
follow the existing `api_keys` GET masking precedent at
`internal/api/apikey/handler.go`. TestConnection uses the server-
side decrypted full config, not the response payload.

#### New — POST `/api/v1/operators/{id}/test-connection/{protocol}`

Replaces the single-protocol `TestConnection` at handler.go:560-605.

- Path params: `id` UUID, `protocol ∈ {radius, diameter, sba, http,
  mock}`.
- Request body: empty (MAY accept `{overrides: {...}}` in a future
  story — explicitly out of scope).
- Success 200:
  ```json
  { "status": "success",
    "data": { "success": true, "latency_ms": 42, "error": "" } }
  ```
- Error 422 if `protocol` is not configured or `adapter_config[protocol].
  enabled != true`:
  ```json
  { "status": "error",
    "error": { "code": "PROTOCOL_NOT_CONFIGURED",
               "message": "protocol diameter is not enabled on this operator" } }
  ```
- Error 400 for invalid protocol name.
- Error 404 for unknown operator.

**Test depth** (per D3 decision below): protocol-native handshake —
RADIUS Status-Server (RFC 5997), Diameter DWR/DWA, SBA NRF
heartbeat, HTTP GET base_url+"/health", Mock noop (returns ok
immediately).

#### Backwards-compat — legacy single-protocol endpoint

`POST /api/v1/operators/{id}/test-connection` (no protocol in path) is
kept as a thin alias under D2-B: since `op.AdapterType` no longer
exists, the legacy handler derives `protocol :=
DerivePrimaryProtocol(decryptedConfig)` (first enabled in canonical
order) and delegates to the new per-protocol handler. Retire in a
follow-up story after all frontends migrate. Documented in plan but
NOT a hard requirement for Wave 3.

### Database Schema

Source: `migrations/20260320000002_core_schema.up.sql:96-117` (ACTUAL).

Current columns (relevant subset):
```sql
adapter_type   VARCHAR(30) NOT NULL,
adapter_config JSONB       NOT NULL DEFAULT '{}',
```

**Post-STORY-090 columns** (D2-B SELECTED — drop the column):
- `adapter_type` column **DROPPED** via a new migration pair (see
  file names below). All reads in store/handler/seed layers stop
  referencing it. Response DTOs expose `enabled_protocols
  []string` (canonical order) instead; the primary-protocol label
  is derived client-side as `enabled_protocols[0]` via
  `ADAPTER_DISPLAY`.
- `adapter_config` column **retained**. Shape changes from flat per-
  type blob to nested `{protocol: {enabled, …fields}}` JSONB. Outer
  envelope (plaintext object vs encrypted base64 string) is
  **unchanged** — the reshape happens at the plaintext JSON level
  before encryption.

**Post-STORY-090 columns** (D2-A NOT SELECTED — was planner
recommendation):
- `adapter_type` column **retained** as a derived legacy alias —
  populated via backfill to the first-enabled-protocol. Stays NOT
  NULL to avoid breaking existing indexes/queries.
- Higher blast radius than D2-A (touches every Operator.AdapterType
  reference in Go code).

NO new columns introduced. No `supported_protocols` column is added
(the feature lives entirely in the nested JSONB `enabled` flags).

Migration file(s) — under the LOCKED selection (D1-A + D2-B):

- **For D1-A (adapter_config reshape): ZERO new SQL migrations.**
  All flat→nested reshape happens in application code at write time.
  Legacy flat rows coexist with nested rows until they are naturally
  touched by Update or a one-time admin rewrite task.

- **For D2-B (drop `adapter_type` column): ONE new migration pair.**
  Filenames (follow project convention
  `YYYYMMDDHHMMSS_description.{up,down}.sql`; 2026-04-18 per user
  direction):
  - `migrations/20260418120000_drop_operators_adapter_type.up.sql`
  - `migrations/20260418120000_drop_operators_adapter_type.down.sql`

  Up-migration contents:
  ```sql
  ALTER TABLE operators DROP COLUMN IF EXISTS adapter_type;
  ```
  (`IF EXISTS` makes the up idempotent across environments where the
  column may or may not still be present.)

  Down-migration contents (emergency rollback — restores the column
  as NULLABLE so an emergency rollback does NOT require a data-
  backfill gate; existing rows will have NULL `adapter_type` which
  the pre-090 code MUST tolerate OR a DB admin must backfill
  manually via the re-derivation SQL documented in §Rollback plan):
  ```sql
  ALTER TABLE operators
    ADD COLUMN IF NOT EXISTS adapter_type VARCHAR(30);
  -- Re-added as NULLABLE (not NOT NULL) to allow an emergency
  -- rollback without requiring a synchronous data backfill. The
  -- pre-090 binary reads adapter_type from the operators table;
  -- for a clean rollback to the pre-090 state, a DB admin MUST run
  -- the re-derivation UPDATE documented in §Rollback plan BEFORE
  -- re-asserting the NOT NULL constraint. This non-NOT-NULL down
  -- is a deliberate safety tradeoff.
  ```

  Both up and down use `IF EXISTS` / `IF NOT EXISTS` guards; running
  either twice is safe.

### Screen Mockups

New **Protocols** tab in `/operators/:id`:

```
┌─────────────────────────────────────────────────────────────────┐
│  Turkcell · TCELL · 286/01                  [Edit] [Disable]    │
├─────────────────────────────────────────────────────────────────┤
│ [Overview] [Health] [Circuit] [Traffic] [Sessions]              │
│ [Agreements] [Audit] [Alerts] [Notifications] [SIMs]            │
│ [Protocols ←NEW]                                                │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  RADIUS                                       [●] Enabled       │
│  ─────────────────────────────────────────────────              │
│  Shared Secret  [ ****** ]                 [Edit]               │
│  Listen Addr    [ :1812  ]                                      │
│                                          [Test Connection] →    │
│  Last probe: 2026-04-18 12:03:42 · 41ms · OK                    │
│                                                                 │
│  Diameter                                     [●] Enabled       │
│  ─────────────────────────────────────────────────              │
│  Origin Host    [ argus.local ]                                 │
│  Origin Realm   [ argus.local ]                                 │
│  Peers          [ 10.0.1.10:3868 (dwa ok) · + Add peer ]        │
│                                          [Test Connection] →    │
│  Last probe: 2026-04-18 12:03:55 · 128ms · OK                   │
│                                                                 │
│  5G SBA                                       [○] Disabled      │
│  ─────────────────────────────────────────────────              │
│  (collapsed; click header to enable and configure)              │
│                                                                 │
│  HTTP                                         [○] Disabled      │
│  Mock                                         [●] Enabled       │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

Per-protocol card structure:
- Header row: protocol name + enable/disable toggle (switch) +
  optional "Last probe" summary chip on the right.
- Body (only when enabled): per-protocol fields, each reusing
  `<Input>`, `<Select>`, `<Textarea>`.
- Footer row: "Test Connection" button (outline variant) on the right;
  disabled if the card has unsaved dirty state.
- Collapsed card (disabled protocols): header-only row with
  enable toggle; clicking the toggle reveals the body.

Protocol-specific field sets:
- **RADIUS**: `shared_secret` (password input, masked), `listen_addr`
  (e.g., ":1812"), optional `acct_port` (":1813").
- **Diameter**: `origin_host`, `origin_realm`, `peers` (list of
  `host:port` strings with add/remove buttons), optional
  `product_name`.
- **SBA**: `nrf_url`, `nf_instance_id` (optional UUID), optional TLS
  section `{cert_path, key_path, ca_path}`.
- **HTTP**: `base_url`, `auth_type` (select: none/bearer/basic/
  mtls), optional `auth_token` (masked) or
  `{username, password}` depending on auth_type.
- **Mock**: `latency_ms` (number), optional `simulated_imsi_count`.

Operators list page (`/operators`) — protocol chip derivation:

```
┌──────────────────────────────────────────────────────────────┐
│ Name          Code     MCC/MNC  Protocols             Health │
├──────────────────────────────────────────────────────────────┤
│ Turkcell      TCELL    286/01   [radius] [diameter]  [ OK ]  │
│ Vodafone TR   VFTR     286/02   [diameter] [sba]    [ OK ]   │
│ Mock XYZ      XYZ      286/99   [mock]              [ OK ]   │
└──────────────────────────────────────────────────────────────┘
```

Chip list derived from `operator.enabled_protocols` (server-computed
array on the response DTO — see API spec). NOT sourced from a local
`supported_protocols` form field (that field is retired).

### Design Token Map (UI story — MANDATORY)

#### Color Tokens (from existing operator pages — observed)

| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Card background | `bg-bg-elevated` | `bg-white`, `bg-[#ffffff]` |
| Card border | `border-border` | `border-[#e2e8f0]`, `border-gray-200` |
| Primary text | `text-text-primary` | `text-[#0f172a]`, `text-gray-900` |
| Secondary text | `text-text-secondary` | `text-[#64748b]`, `text-gray-500` |
| Tertiary / muted | `text-text-tertiary` | `text-[#94a3b8]` |
| Danger text | `text-danger` | `text-[#dc2626]`, `text-red-500` |
| Success text | `text-success` | `text-[#16a34a]`, `text-green-500` |
| Accent highlight bg | `bg-accent-dim` | `bg-blue-100` |
| Accent highlight text | `text-accent` | `text-[#2563eb]` |

#### Typography Tokens

| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Card section title | `text-sm font-medium text-text-primary` | `text-[14px]`, custom px |
| Card body field label | `text-xs font-medium text-text-secondary` | `text-[12px]`, custom px |
| Monospace chip | `font-mono text-xs` | `text-[11px] font-mono` (arbitrary) |

#### Spacing & Elevation Tokens

| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Card padding | `p-4` or `p-6` (match existing sibling cards) | `p-[20px]` |
| Card radius | existing Card component's rounded | custom `rounded-xl` |
| Card border | `border border-border` | `border-2`, custom color |

#### Existing Components to REUSE (DO NOT recreate)

| Component | Path | Use For |
|-----------|------|---------|
| `<Input>` | `web/src/components/ui/input.tsx` | ALL text/number fields |
| `<Select>` | `web/src/components/ui/select.tsx` | ALL dropdowns (auth_type, etc.) |
| `<Button>` | `web/src/components/ui/button.tsx` | ALL actions (Test Connection, Add peer) |
| `<Card>/<CardHeader>/<CardContent>/<CardTitle>` | `web/src/components/ui/card.tsx` | Per-protocol cards |
| `<Badge>` | `web/src/components/ui/badge.tsx` | Protocol chips in list view |
| `<Switch>` / `<Tabs>` | existing ui pkg | enable toggle + tab navigation |
| `<Skeleton>` | `web/src/components/ui/skeleton.tsx` | Loading state |
| `<EmptyState>` | `web/src/components/shared/EmptyState.tsx` | Disabled-protocol empty body |

## Decision Points

> **All four decisions LOCKED 2026-04-18 per user selection.**
> Locked set: **D1-A, D2-B, D3-B, D4-A**. D1/D3/D4 match planner
> recommendations; **D2 is a user override — chose D2-B (drop
> `adapter_type` column) despite planner recommendation D2-A (keep as
> legacy alias). Rationale: clean removal preferred over legacy alias
> bloat; single source of truth in nested `adapter_config` is worth the
> one-time blast-radius cost of updating every reader.**
>
> The selection triggers scope changes documented inline in each
> section below and expands Wave 2 with a drop-column migration plus an
> exhaustive `adapter_type` reader sweep.

### D1. Migration strategy (advisor flag #1 — dispatch framing invalid)

**LOCKED 2026-04-18 per user selection → D1-A (dual-read + lazy
rewrite; no backfill migration for `adapter_config`).** Note: D1-A
applies specifically to the **adapter_config encryption-envelope
reshape** (flat→nested JSON) — there is NO SQL needed for that step.
A SEPARATE single-column-drop migration is introduced under D2-B
(below) to remove the `adapter_type` column. The two mechanisms do
not conflict: D1-A is app-layer lazy; D2-B is a standalone DDL
migration narrowly scoped to one column.

Dispatch framing ("in-place transform vs dual-write + flag vs shadow
column") does NOT work because `adapter_config` is AES-GCM encrypted at
the app layer before it reaches the DB (see §Current deployed reality
> App-layer encryption envelope). SQL has no key. Real axis:

- **D1-A (SELECTED): dual-read tolerance + lazy rewrite, no
  backfill migration.**
  1. `store/operator.go` reading code stays unchanged (still decrypts
     via `DecryptJSON`).
  2. Handler layer adds a `normalizeAdapterConfig(raw) (nested
     json.RawMessage, error)` pre-write step: if raw is a flat blob
     (detected by absence of all five protocol sub-keys at top level),
     up-convert to `{<adapter_type>: {enabled: true, …raw}}` nested
     shape, using the request's `adapter_type` (Create) or the
     stored Operator.AdapterType (Update).
  3. Handler layer adds a `denormalizeForRead(nested)` step on GET
     responses (no-op for already-nested rows; ensures API shape
     consistency).
  4. Read paths (TestConnection, HealthChecker) add a SHAPE DETECTOR:
     `if topLevelHasKeysLike("mock","radius","diameter","sba","http")
     → nested path; else → legacy flat path`. Legacy flat rows are
     transparently wrapped into `{<adapter_type>: {enabled:true,
     ...flatBody}}` at read time.
  5. Natural rewrite on the first Update of any legacy operator — the
     normalized nested JSON is re-encrypted and persisted.
  6. Optional admin CLI task (`argus admin migrate-adapter-config`)
     for fleet-wide rewrite — OUT of Wave 1; can be Wave 3 or a
     follow-up.

  Pros: zero new SQL migrations; zero boot-time backfill; legacy and
  new shapes coexist indefinitely without service disruption; no
  schema_version column pollution; minimal rollback.
  Cons: normalize/denormalize code paths live forever until the
  optional admin task retires them; unit tests must cover both shapes.

- **D1-B: Go one-shot boot-time backfill with schema_version column.**
  1. New migration adds `operators.adapter_config_schema_version INT
     NOT NULL DEFAULT 1 CHECK (IN (1,2))`.
  2. New boot-time task in `cmd/argus/main.go` walks rows with
     `schema_version=1`, decrypts, reshapes flat→nested, re-encrypts,
     writes back, sets `schema_version=2`.
  3. Post-backfill, all runtime read/write paths handle nested only.
  4. Backfill is idempotent: re-running sees no rows with
     schema_version=1.
  5. A `--skip-adapter-backfill` flag covers emergency bypass.

  Pros: clean post-backfill state; no dual-read code paths in the
  steady state; clear migration audit trail via the version column.
  Cons: boot-time DB write (startup risk); needs downtime coordination
  if operator row count is large (row count is ~5-20 today per
  deployment — low risk in practice); more code than D1-A.

- **D1-C: new sidecar column `adapter_config_v2` + dual-write + cutover
  flag.**
  1. New migration adds `adapter_config_v2 JSONB` nullable.
  2. App-layer dual-writes both columns on every Create/Update.
  3. Reads are gated by `cfg.UseAdapterConfigV2` boolean flag —
     default true in production after Wave 2 ships.
  4. Old column dropped in a follow-up story after flag has been true
     for N releases.

  Pros: instant rollback via flag flip; no data loss risk; parallel
  read paths can be A/B compared.
  Cons: two encrypted columns instead of one (storage doubled for
  ~seconds until drop); D1-A already provides effectively the same
  rollback via the dual-read logic with zero schema change; adds
  operational complexity the tiny operator table doesn't warrant.

**Planner recommendation**: **D1-A**. The operators table is tiny
(current deployments ship ~5-20 rows), dual-read tolerance costs ~80
LOC of Go, and lazy rewrite clears legacy rows on the next natural
edit. The D1-B one-shot is tempting for cleanliness but introduces a
new column AND a boot-time write step for an operation that can
happen lazily without anyone noticing. D1-C is overkill.

**Advisor verdict (pre-draft consultation)**: confirmed D1-A for a
~5-20-row table. Specifically ruled out "in-place SQL transform" as
infeasible given the encryption constraint. Recommended citing the
actual discriminator byte (`"` for encrypted, `{` for plaintext) in
the decoder pseudocode so Wave 1 tasks have an unambiguous spec.

### D2. `adapter_type` column fate (retire vs keep as derived alias)

**LOCKED 2026-04-18 per user selection → D2-B (DROP the
`adapter_type` column entirely). USER OVERRIDE** — chose B despite
planner recommendation A. Rationale (user-recorded): clean removal
preferred over legacy alias bloat; a single source of truth in the
nested `adapter_config` is worth the one-time blast-radius cost of
updating every reader in the same PR.

**Scope expansion triggered by D2-B** (new Wave-2 work — see Tasks 3b
and 7d below):
1. New SQL migration pair drops the column (see §Database Schema and
   Task 3b).
2. Go store layer (`internal/store/operator.go`) removes the
   `AdapterType` field from the struct, all SELECT column lists, the
   INSERT at `:192`, the UPDATE path, and all scan destinations at
   `:139, :288, :417`.
3. Go API layer (`internal/api/operator/handler.go`) hard-removes
   `AdapterType` from `operatorResponse` (:93), hard-removes from
   `createOperatorRequest` (:135-151), hard-removes from
   `updateOperatorRequest` (:153-166). The handler DOES NOT accept
   `adapter_type` as an ignored legacy field — **the decision is
   hard-remove, NOT soft-deprecate for one release**. Recorded here
   per planner obligation to choose-and-document: the operators API
   is internal-only (served behind the admin UI); no external
   consumer contract to honor. A request that sends `adapter_type`
   will 422 under the JSON decoder's `DisallowUnknownFields` path if
   enabled, OR be silently ignored by the JSON decoder if not — in
   either case the server never persists or reads it.
4. Seeds (`migrations/seed/002_system_data.sql`,
   `003_comprehensive_seed.sql`, `005_multi_operator_seed.sql`)
   strip the `adapter_type` column from every INSERT — this is a
   seed edit that lands IN THE SAME PR as the drop-column migration
   to keep fresh-DB bootstrap green.
5. Export handler (`internal/api/operator/export.go:29`) removes
   `adapter_type` from the CSV header + row build.
6. UI (`web/src/pages/operators/detail.tsx:850-940`
   EditOperatorDialog) removes the `adapter_type` dropdown
   entirely; `web/src/pages/operators/index.tsx` CreateOperator form
   removes the `adapter_type` selector (consolidated with the dead
   `supported_protocols` retirement into one Task 7c/7d sweep).
7. TypeScript types (`web/src/types/operator.ts:7`,
   `web/src/hooks/use-operators.ts:62`) drop
   `adapter_type: string` field; replace with
   `enabled_protocols: string[]` (primary display label derived
   client-side from `enabled_protocols[0]` via
   `ADAPTER_DISPLAY[enabled_protocols[0]]`).
8. All existing Go tests that reference `adapter_type` are updated
   in the same commit — use grep to find them: `internal/api/
   operator/handler_test.go`, `internal/store/operator_test.go`,
   any operator fixtures in test helpers.
9. Compare page (`web/src/pages/operators/compare.tsx:26`) migrates
   the `adapter_type` column to `enabled_protocols` chip display.

- **D2-A (NOT SELECTED — was planner recommendation): keep `adapter_type` as a derived legacy alias.**
  On every Create/Update, the handler computes the column value from
  the enabled sub-keys in canonical priority order:
  `diameter > radius > sba > http > mock`. The column stays in seeds,
  stays in DTOs for list/detail responses (chip rendering), stays
  readable by any external tooling that grep-queries it. STORY-089
  and downstream can retire it when nothing reads it.

  Pros: zero breaking change for anything reading `adapter_type`
  today (CDR reports, audit logs, existing tests); stays compatible
  with DB reporting tools.
  Cons: one more line in the handler compute path; "legacy alias"
  semantics must be documented so nobody writes NEW code that treats
  it as source of truth.

- **D2-B (SELECTED — USER OVERRIDE): drop the `adapter_type` column in a new migration.**
  Every Go reference (store, handler, seeds, tests, export) is
  updated to derive from `adapter_config`. DTO drops `adapter_type`
  and relies on `enabled_protocols []string` (computed server-side
  via `DeriveEnabledProtocols`) as the sole protocol-identity
  surface on responses. UI renders `ADAPTER_DISPLAY[enabled_
  protocols[0]]` for the primary-protocol label.

  Pros: no legacy surface; a single source of truth.
  Cons: larger blast radius — 6+ Go files touched, 3+ seed files
  touched (seeds 002, 003, 005 all reference the column), export
  handler at `internal/api/operator/export.go:29` updated, handler
  tests updated, existing audit entries in the DB retain the old
  column name in `before_data`/`after_data` JSONB (historical
  artifact — acceptable).

**Planner recommendation**: **D2-A** (NOT SELECTED). User selected
**D2-B** and accepted the larger blast radius in exchange for a
single-source-of-truth surface.

### D3. Test-Connection depth (minimal reachability vs protocol-native handshake)

**LOCKED 2026-04-18 per user selection → D3-B (protocol-native
handshake: RFC 5997 Status-Server for RADIUS, RFC 6733 DWR/DWA for
Diameter, 3GPP TS 29.510 NRF heartbeat for SBA, HTTP `/health` for
HTTP adapter).** Matches planner recommendation.

- **D3-A (NOT SELECTED): minimal reachability probe** — RADIUS/Diameter:
  `net.DialTimeout(udp, addr, 2s)` then close; SBA: HTTP GET
  `nrf_url` with 2s timeout; HTTP: HTTP HEAD `base_url` with 2s
  timeout; Mock: return `{success:true, latency_ms:0}` immediately.

  Pros: zero new protocol code; uses existing net stdlib; fast.
  Cons: "reachable" ≠ "spoken to": a misconfigured shared_secret on
  RADIUS or a wrong origin-host on Diameter still reports success.
  Weak signal for the UI "Test" button.

- **D3-B (SELECTED): protocol-native handshake.** Reuses each
  adapter's existing `HealthCheck(ctx)` method at
  `internal/operator/adapter/{mock,radius,diameter,sba}.go`:
  - `Mock.HealthCheck` → existing behavior.
  - `RADIUS.HealthCheck` → implement Status-Server (RFC 5997, type
    code 12) if not already present; otherwise fall back to a
    minimal Access-Request with a fixed synthetic IMSI and expect
    Reject-or-Accept (not a timeout).
  - `Diameter.HealthCheck` → Device-Watchdog-Request (DWR, command
    code 280) expecting DWA back. Existing Diameter adapter in
    `internal/operator/adapter/diameter.go` likely already has a CER/
    CEA path — reuse if so, else add DWR.
  - `SBA.HealthCheck` → NRF heartbeat: PATCH on
    `/nnrf-nfm/v1/nf-instances/{nfInstanceId}` with a minimal
    `[{op:"replace", path:"/nfStatus", value:"REGISTERED"}]` payload
    per TS 29.510, or simpler GET on the same path.
  - `HTTP.HealthCheck` → GET `base_url + "/health"` (configurable
    path via `health_path` adapter_config field, default "/health").
  - Latency captured from request-start to first-response-byte,
    returned in the `latency_ms` field.

  Pros: the "Test" button actually proves the adapter can talk the
  protocol, not just reach the IP:port. Real operational signal.
  Cons: Status-Server + DWR + NRF heartbeat implementations add
  protocol code (but most is a single round-trip — mitigated by the
  fact that the adapter interface already declares `HealthCheck` and
  each adapter has at least a stub today).

**Planner recommendation**: **D3-B**. This is a test-platform and the
UI "Test" button MUST actually test — a reachability probe
misleads operators. The implementation cost is bounded because the
Adapter interface already declares `HealthCheck(ctx) HealthResult` at
`internal/operator/adapter/types.go:40` and each of mock/radius/
diameter/sba has a file — we extend, not invent.

### D4. OperatorRouter refactor scope (advisor flag #3 — dead code)

**LOCKED 2026-04-18 per user selection → D4-A (refactor router API
in THIS story).** Matches planner recommendation. Router signatures
gain `protocol string` alongside `operatorID`; breakers re-keyed by
`(operatorID, protocol)`; `failover_test.go` updated in-story.

Current state: `OperatorRouter` is instantiated at `cmd/argus/main.go:
813` and immediately discarded at `:814` (`_ = operatorRouter`). All
its methods (`ForwardAuth`, `ForwardAcct`, …) take `operatorID
uuid.UUID` as the single adapter key. Unused in production; used
only by `internal/operator/failover_test.go` and similar in-package
tests.

- **D4-A (SELECTED): refactor router API in this story.**
  Extend every router method signature to accept `protocol string`
  alongside `operatorID`. Breakers re-keyed by `(operatorID,
  protocol)`. Tests at `failover_test.go` updated in the same task.

  Pros: future-proofs the router for when STORY-089 wires it into
  the hot paths — those changes won't need a second refactor; test
  coverage of the multi-protocol semantics lands in-story.
  Cons: larger task surface — 3 files touched (router.go,
  failover_test.go, circuit_breaker.go).

- **D4-B (NOT SELECTED): leave router API single-keyed; STORY-089 deals with it.**
  Add a `// TODO STORY-089: refactor for per-protocol` comment at
  `router.go:98` and ship STORY-090 without touching router signatures.

  Pros: smaller task surface; no touch of dead code.
  Cons: STORY-089 inherits a router refactor on top of its own scope;
  registry/router key-shape drift (registry is per-protocol but
  router still per-operator).

**Planner recommendation**: **D4-A**. The cost delta is small and
keeps the layer shapes consistent post-STORY-090. Leaving it for
STORY-089 guarantees a second refactor pass.

## Acceptance Criteria

Each AC is testable; test tasks cover them individually.

### AC-1: Multi-protocol operator create round-trips end-to-end

Test: `POST /api/v1/operators` with nested `adapter_config` having
BOTH `radius.enabled=true` AND `diameter.enabled=true` (plus `sba.
enabled=false`, `http.enabled=false`, `mock.enabled=false`). Expect:
- 201 Created with echoed nested `adapter_config` (decrypted,
  per-response).
- `enabled_protocols` array in response is `["diameter","radius"]`
  (canonical order).
- Response JSON does NOT contain an `adapter_type` field (per D2-B;
  this is enforced by marshaling from a Go struct that has no
  `AdapterType` field — verified via assertion `json.RawMessage ∌
  "adapter_type"`).
- DB: `operators.adapter_config` is encrypted (first non-WS byte `"`);
  after `crypto.DecryptJSON` with the test key, decoded JSON matches
  the sent nested shape exactly. The `operators` table has NO
  `adapter_type` column after the D2-B migration runs.

### AC-2: Registry returns independent adapters for each enabled protocol

Test: after AC-1's operator exists, call `adapter.Registry.GetOrCreate(
opID, "radius", <radius-sub-config>)` and `.GetOrCreate(opID,
"diameter", <diameter-sub-config>)`. Expect two DISTINCT Adapter
instances returned (different `Type()` values). Calling `GetOrCreate`
again with the same (opID, protocol) pair returns the same instance
(idempotent). `Registry.Remove(opID, "radius")` removes ONLY the
radius adapter — the diameter entry survives.

### AC-3: Test-Connection endpoint works per-protocol

Test:
1. `POST /api/v1/operators/{id}/test-connection/mock` on AC-1's
   operator → 200, `{success: true, latency_ms ≥ 0}`.
2. `POST .../test-connection/radius` → 200 on a live RADIUS peer
   (or predictable error on a misconfigured one — see Risk 4). The
   `HealthCheck` invocation exercises the D3-B path (Status-Server or
   minimal Access-Request, not pure DialTimeout).
3. `POST .../test-connection/sba` → 422 `PROTOCOL_NOT_CONFIGURED`
   because `sba.enabled=false`.
4. `POST .../test-connection/nonsense` → 400 invalid protocol.

### AC-4: Protocols tab renders per-protocol cards with Enable toggles

UI smoke test: navigate to `/operators/:id` → Protocols tab. Assert
at least 5 cards render (mock, radius, diameter, sba, http). Each
card's enable toggle reflects `adapter_config[protocol].enabled`.
Enabled cards show protocol-specific fields; disabled cards show
header only. Test Connection button is present on every enabled
card.

### AC-5: Migration preserves existing single-protocol operators (D1-A lazy rewrite)

Pre-condition: seed DB has AC-0 legacy operators
(`adapter_type='mock'` with flat `adapter_config = {latency_ms:12}`
plaintext per seed 002).

Test 1 (read path): `GET /api/v1/operators/{seed-op-id}` returns a
response where `adapter_config` is the UP-CONVERTED nested shape
`{"mock":{"enabled":true,"latency_ms":12}, …}`, and DB row is still
the legacy flat shape (NOT rewritten on read).

Test 2 (write path, lazy rewrite): `PATCH /api/v1/operators/{seed-
op-id}` with any change (e.g., `{name: "New Name"}`). Post-update DB
row for `adapter_config` is re-encrypted with the nested shape. Re-
fetching returns the nested shape directly (no more up-conversion
needed). No `Operator.AdapterType` column exists — the D2-B drop-
column migration removed it before this test runs; the seed data
reshape into nested form is the sole persistence side-effect of
Test 2.

Test 3 (TestConnection on unrewritten legacy row): before Test 2
rewrites the row, `POST .../test-connection/mock` still works
(read-path shape detection handles the flat blob transparently).

### AC-6: Retired `supported_protocols` UI field is gone and replaced by derived chips

Test: `grep -rn 'supported_protocols' web/src/` returns ZERO matches
after the refactor. `web/src/pages/operators/index.tsx` list rows
render protocol chips derived from `operator.enabled_protocols`
array. `web/src/pages/operators/compare.tsx` is updated under D2-B
to render `enabled_protocols` chips in place of the removed
`adapter_type` column heading. `CreateOperatorDialog` no longer has
a `toggleProtocol` chip group — the nested Protocols editor is the
sole protocol source. The `EditOperatorDialog` no longer has an
`adapter_type` dropdown (the field is gone; the primary-protocol
label renders read-only as `ADAPTER_DISPLAY[enabled_protocols[0]]`).

### AC-7: Existing single-protocol operators keep working unchanged (regression)

Every existing backend integration test that creates or updates an
operator with flat `adapter_type + adapter_config` continues to pass
after the test is updated to match the new response shape. Under
D2-B, the handler still ACCEPTS `adapter_type` in the request body
as an **input-only hint for legacy-flat up-conversion** (used by
`UpConvertFlatToNested` to determine which sub-key to wrap the flat
blob into), but does NOT persist it and does NOT echo it back.
Specifically the tests updated:
`internal/api/operator/handler_test.go`, existing SIM/RADIUS/
Diameter/SBA tests that touch operators. Any test that previously
asserted `response.adapter_type == "…"` is updated to assert
`response.enabled_protocols[0] == "…"` instead.

### AC-8: Per-protocol adapter resolution on the registry AND the router

Test: after D4-A refactor,
- `adapter.Registry.GetOrCreate(opID, "radius", cfg)` returns the
  radius adapter.
- `adapter.Registry.GetOrCreate(opID, "diameter", cfg)` returns the
  diameter adapter (different instance).
- `OperatorRouter.ForwardAuth(ctx, opID, "radius", authReq)`
  dispatches to the radius adapter. Attempting
  `ForwardAuth(ctx, opID, "sba", authReq)` on an operator where SBA
  is disabled returns `adapter.ErrAdapterNotFound`.
- `OperatorRouter.GetCircuitBreaker(opID, "radius")` and `("diameter")`
  return distinct breakers.

### AC-9: STORY-092 wiring preserved (regression — advisor hard flag)

Test: `internal/aaa/diameter/server.go::ServerDeps` still has
`IPPoolStore` and `SIMStore` fields. `cmd/argus/main.go` still passes
the shared `ippoolStore` and `simStore` into `aaadiameter.ServerDeps{
...}` and `aaasba.ServerDeps{...}`. `go test ./internal/aaa/diameter/
-run TestGxCCAInitial_FramedIPAddress` and `go test
./internal/aaa/sba/ -run TestSBAFullFlow_NsmfAllocates` both still
pass (exercising the STORY-092 hot paths through the post-090
wiring). No STORY-092 AC regresses.

**Explicit D2-B regression guard** (user direction): the new
registry-resolution surface introduced in Tasks 3+3b MUST still
thread `IPPoolStore` + `SIMStore` into `aaadiameter.ServerDeps` and
`aaasba.ServerDeps` exactly as STORY-092 wired them. If the registry
lookup previously went through `adapter_type`, the new per-protocol
lookup must replace that wiring without dropping either Store field.
Nil-cache enforcer (STORY-092 AC-9) + RADIUS dynamic allocation path
must remain green — Task 8 runs `TestEnforcerNilCacheIntegration_
STORY092` as an explicit D2-B crossover regression probe.

### AC-10: HealthChecker fans out per enabled protocol

Test: operator with 3 enabled protocols (e.g., radius+diameter+mock).
After `HealthChecker.Start`, Prometheus gauge
`argus_operator_adapter_health_status{operator=…, protocol=…}` has
THREE distinct label sets (one per protocol), each ticking on the
per-operator interval. Disabling a protocol via PATCH removes its
gauge within one health-check cycle.

### AC-11: Baseline test suite remains green (guard)

`go test ./...` ≥ 3000 PASS, 0 FAIL. `go vet ./...` exit 0. No new
package SKIPs beyond the pre-existing DATABASE_URL-gated set.

### AC-12: `adapter_type` column is absent from the live DB post-migration (D2-B)

Test: against a fresh `make db-migrate` on a disposable Postgres:
```sql
SELECT column_name FROM information_schema.columns
  WHERE table_name = 'operators' AND column_name = 'adapter_type';
```
returns ZERO rows. `\d operators` in `psql` does NOT list
`adapter_type`. The migration pair
`20260418120000_drop_operators_adapter_type.{up,down}.sql` is
present in the `migrations/` directory and the `schema_migrations`
table records its version after `make db-migrate` completes
successfully. Down-migration `make db-migrate-down STEPS=1` restores
the column as NULLABLE (not NOT NULL) without requiring a data
backfill.

### AC-13: Zero `adapter_type` references remain in source (D2-B reader-sweep guard)

Test (run from repo root):
```bash
rg -n --type go '\badapter_type\b|\bAdapterType\b' \
   internal/ cmd/ | grep -v '_test.go' | grep -v 'migrations/'
```
returns **ZERO** matches in non-test, non-migration Go sources after
the refactor. A small bounded set of matches is acceptable and
expected ONLY in:
- `migrations/20260320000002_core_schema.up.sql` (historical; the
  original CREATE TABLE; left alone — the DROP COLUMN migration
  supersedes it at runtime).
- `migrations/20260418120000_drop_operators_adapter_type.up.sql` and
  `.down.sql` (the new migration themselves reference the column
  name).
- Tests covering the request-body up-convert-hint path (legacy
  `adapter_type` input accepted for wrapping; tests assert the path
  does not persist the value).

Frontend guard (run from `web/`):
```bash
rg -n 'adapter_type' src/
```
returns **ZERO** matches after the refactor. All TypeScript types,
hooks, components, and pages use `enabled_protocols` as the sole
protocol-identity surface.

### Adapter registry current shape

`internal/operator/adapter/registry.go:11-17` — `Registry.adapters
map[uuid.UUID]Adapter`. Mutex-protected. Single-instance-per-operator
by construction today. Post-090: key becomes
`struct {OperatorID uuid.UUID; Protocol string}` or a string
composite `opID+":"+protocol`. Factories at `:25-36` stay — only the
call sites that invoke them change.

### AES-GCM envelope (`internal/crypto/aes.go:68-103`)

- `EncryptJSON` returns `json.RawMessage(fmt.Sprintf("%q", string(
  encrypted)))` — always a JSON STRING (starts with `"`).
- `DecryptJSON` short-circuits for empty key (plaintext passthrough).
  Non-empty key unmarshal to string; if unmarshal fails, returns
  data unchanged (at line 95-96 — idempotent handling of plaintext
  data with a non-empty key configured).
- **Discriminator for Wave 1 decoder**: first non-whitespace byte of
  the RawMessage.
  - `"` → encrypted → `DecryptJSON` → examine JSON object inside.
  - `{` → plaintext → direct JSON object examination.
  This is unambiguous and used by the shape detector in Task 3.

### RFC 5997 Status-Server (for D3-B RADIUS HealthCheck)

Message type code 12, reserved for server health checks. Shared
secret protects the Response-Authenticator. Response is a normal
Access-Accept or Access-Reject (depending on server policy) with no
requirement to include user-specific attributes. `layeh.com/radius/
rfc2865` does not include Status-Server; implement minimally in the
RADIUS adapter's `HealthCheck` by constructing a type-12 packet
manually or by falling back to an Access-Request with a reserved
synthetic IMSI.

### Diameter DWR/DWA (RFC 6733 §5.5, for D3-B Diameter HealthCheck)

Command Code 280, application 0 (Diameter common). Expected back:
DWA with `Result-Code = 2001 (DIAMETER_SUCCESS)`. Existing Diameter
adapter already maintains a peer connection with CER/CEA — DWR is
the natural keepalive.

### 3GPP TS 29.510 (for D3-B SBA HealthCheck)

NRF NF profile heartbeat per TS 29.510 §5.2.2.4: `PATCH
/nnrf-nfm/v1/nf-instances/{nfInstanceId}` with JSON Patch `[{op:
"replace", path:"/nfStatus", value:"REGISTERED"}]` or simpler GET on
the same path. Simpler GET is acceptable for the mock case.

### Existing STORY-092 surface to preserve

- `internal/aaa/diameter/server.go:26-31` ServerDeps with
  `IPPoolStore *store.IPPoolStore` and `SIMStore *store.SIMStore`.
- `internal/aaa/sba/server.go` ServerDeps with the same two stores
  plus the shared radius `SIMCache`.
- `cmd/argus/main.go` construction sites for both — STORY-090 wave 2
  touches `main.go` adjacent to these sites but must NOT remove/
  rename any STORY-092 field or call.

## Tasks

Dependency-ordered. Each task touches ≤3 files where possible; two
tightly-coupled refactors exceed this (Task 3 touches 4 for the
registry shape change; Task 3b touches 8+ for the D2-B
migration + reader sweep — acceptable as single logical units per
the Task Decomposition Rules "ideally 1-2 but max 3" guidance,
with the caveat that splitting is acceptable if the developer
hits mental-context overflow). Wave breakdown: Wave 1 = schema
shape / validator / backend up-converter; Wave 2 = registry
refactor + hot-path resolution + **D2-B drop-column migration +
store/seed/export reader sweep** + router D4-A; Wave 3 = UI
Protocols tab + test-connection endpoint + dead-code removal
(consolidated with D2-B `adapter_type` UI retirement) + evidence
+ regression guard. **Total tasks: 12** (0, 1, 2, 3, 3b, 4, 5, 6,
7a, 7b, 7c, 8).

### Wave 1 — schema shape and backend up-conversion

#### Task 0: Nested `adapter_config` validator + shape detector

- **What**: Add a new package `internal/api/operator/adapterschema/`
  (or a single file if small) exposing:
  1. `ValidateNestedAdapterConfig(raw json.RawMessage) error` —
     asserts: is JSON object; every top-level key ∈ {mock, radius,
     diameter, sba, http}; every sub-object has `enabled: bool`;
     for each `enabled=true` sub-object, protocol-specific required
     fields are present and typed correctly (per §Screen Mockups >
     Protocol-specific field sets).
  2. `IsNestedShape(raw json.RawMessage) bool` — true if ≥1 top-
     level key matches the protocol set, false otherwise.
  3. `IsLegacyFlatShape(raw json.RawMessage) bool` — true if none of
     the top-level keys are in the protocol set (heuristic sibling
     to `IsNestedShape`).
  4. `DeriveEnabledProtocols(raw json.RawMessage) []string` — returns
     enabled sub-keys in canonical order (`diameter`, `radius`, `sba`,
     `http`, `mock`).
  5. `DerivePrimaryProtocol(raw json.RawMessage) string` — returns
     the first enabled per canonical order, or `""` if none.
  6. `UpConvertFlatToNested(flatRaw json.RawMessage, adapterType
     string) (json.RawMessage, error)` — wraps a legacy flat blob
     into `{adapterType: {enabled: true, …flatBody}}`.
  Unit tests for each function covering: nested-valid, nested-
  invalid (missing enabled flag, unknown protocol key, wrong type),
  legacy flat, encryption-envelope passthrough (test helper that
  calls `crypto.EncryptJSON`/`DecryptJSON` to ensure shape logic
  operates on plaintext only).
- **Files**:
  - NEW `internal/api/operator/adapterschema/schema.go`
  - NEW `internal/api/operator/adapterschema/schema_test.go`
- **Pattern ref**: Read `internal/api/apn/validator.go` (or the
  closest validator in `internal/api/`) for validator package layout;
  read `internal/crypto/aes.go` to understand encryption envelope
  shape.
- **Context refs**: Architecture Context > App-layer encryption
  envelope; API Specifications; Decision Points > D1.
- **Verify**: `go test ./internal/api/operator/adapterschema/...` —
  full green.
- **Complexity**: medium.
- **Depends on**: —

#### Task 1: Handler Create/Update accept nested `adapter_config` + compute enabled_protocols (D2-B)

- **What**: Update `internal/api/operator/handler.go`:
  1. In `Create` at `:317-414`: after JSON decode at `:319`, if
     `req.AdapterConfig` is present AND `IsNestedShape(req.
     AdapterConfig) == false`: up-convert using
     `UpConvertFlatToNested(req.AdapterConfig, req.AdapterType)`
     (backward-compat — `req.AdapterType` is an INPUT-ONLY hint here,
     used to pick the wrapping sub-key; under D2-B it is NOT
     persisted and NOT echoed back). If `req.AdapterType` is empty
     AND `req.AdapterConfig` is flat, return 422 with message
     "legacy flat request requires adapter_type hint".
  2. Validate via `adapterschema.ValidateNestedAdapterConfig(…)`
     — reject 422 on failure.
  3. Compute `enabledProtocols := DeriveEnabledProtocols(nestedRaw)`
     (canonical order slice). Store this in the audit
     `after_data` payload.
  4. Encrypt as today, pass to store as `AdapterConfig=encryptedNestedRaw`
     ONLY — **no `AdapterType` field is passed to the store under
     D2-B** (store signature changes per Task 3b's reader sweep).
  5. In `Update` at `:416-506`: same normalize path. If request
     omits `adapter_config`, no change. If request provides
     `adapter_config`, validate and persist via `operatorStore.
     Update` (no `AdapterType` write path exists under D2-B). If
     the request sends `adapter_type`, the JSON decoder either
     silently ignores it OR 422s under `DisallowUnknownFields`
     depending on decoder setup — document the chosen behavior
     inline.
  6. Remove the `validAdapterTypes` map at `:20-25` entirely —
     adapter-type allowlist validation was coupled to the
     now-dropped column. Replace any remaining usages with
     `adapterschema.IsValidProtocol(string) bool` (a new helper in
     Task 0) which the nested-config validator already calls
     transitively. Add `"http"` to the protocol allowlist in the
     new helper (per D3-B, HTTP adapter is now a legal protocol).
  7. In `toOperatorResponse` at `:174-198`: **REMOVE the
     `AdapterType string` field entirely** (D2-B). Add
     `EnabledProtocols []string` field populated from the operator's
     (decrypted) `adapter_config` by calling
     `adapterschema.DeriveEnabledProtocols`. Add `AdapterConfig
     json.RawMessage` field populated from decrypted adapter_config
     with secrets masked (see §API Specifications > GET detail
     secrets masking).
  8. Remove the `AdapterType` field from `createOperatorRequest`
     and `updateOperatorRequest` structs. Under D2-B the hard-
     removal is the chosen path — see D2 decision block for the
     documented rationale (handler returns JSON decoder behavior
     for an unexpected `adapter_type` field: ignored by default;
     accepted as an input-only hint ONLY inside the up-convert
     helper when the payload is a flat blob, as per step 1). Since
     the request struct no longer has the field, the up-convert
     helper now takes an explicit `hintType string` param populated
     from a local variable inside Create (parsed out of the raw
     request body if flat-shape detected). Document the decoder
     behavior inline.
- **Files**:
  - MOD `internal/api/operator/handler.go`
  - MOD `internal/api/operator/handler_test.go` (extend existing
    tests for the new nested-shape paths; update tests that assert
    `response.adapter_type` to assert `response.enabled_protocols[0]`)
- **Pattern ref**: Existing `createOperator` flow in handler.go lines
  317-414 — preserve structure; insert normalize+validate BEFORE
  the encryption step at :354.
- **Context refs**: Architecture Context > Current deployed reality >
  Backend handler surface; API Specifications; Decision Points > D1,
  D2 (LOCKED → D2-B).
- **Verify**: `go test ./internal/api/operator -run TestHandlerCreate_
  NestedAdapterConfig` + `TestHandlerCreate_LegacyFlatStillWorks` +
  `TestHandlerResponse_NoAdapterTypeField`.
- **Complexity**: high (touches Create+Update+DTO+validator +
  response-DTO field removal on the same high-traffic handler).
- **Depends on**: Task 0.

#### Task 2: Read-path shape detector for TestConnection + HealthChecker (D1-A lazy rewrite core)

- **What**:
  1. In `internal/api/operator/handler.go::TestConnection` at
     `:560-605`, AFTER the decrypt at `:580-587`, instead of calling
     `adapterRegistry.GetOrCreate(id, op.AdapterType, adapterConfig)`
     directly: detect shape via `adapterschema.IsNestedShape`. If
     nested: extract `sub := adapterConfig[protocol]` (where `protocol`
     comes from the URL — see Task 7a). If legacy flat: call
     `UpConvertFlatToNested(adapterConfig, op.AdapterType)` and pass
     that single sub-object to the registry keyed by protocol.
     Reject 422 if the protocol sub-key doesn't exist or `!enabled`.
  2. In `internal/operator/health.go::Start` at `:140-175`: instead
     of `go func(opID, adapterType, cfg, ...)` once per operator,
     call `adapterschema.DeriveEnabledProtocols(decryptedConfig)` and
     spawn ONE goroutine per enabled protocol. The goroutine arg
     changes from `(opID, adapterType, cfg)` to
     `(opID, protocol, subCfg)` where `subCfg` is the protocol's
     sub-object JSON.
  3. In `checkOperator` at `:177-217`: signature becomes
     `checkOperator(opID uuid.UUID, protocol string, subCfg
     json.RawMessage, cb *CircuitBreaker, intervalSec int)`. The
     `registry.GetOrCreate(opID, protocol, subCfg)` call gets the
     protocol key.
  4. Update `HealthChecker.Stop`/`Remove` to deregister per-
     protocol entries; prometheus gauge labels include `protocol`.
- **Files**:
  - MOD `internal/api/operator/handler.go` (TestConnection only —
    the URL routing change goes in Task 7a)
  - MOD `internal/operator/health.go`
  - MOD `internal/operator/health_test.go`
- **Pattern ref**: Read Task 0 + Task 1 output for up-convert
  pattern; read existing `checkOperator` for health-probe loop
  shape.
- **Context refs**: Architecture Context > Current deployed reality >
  Adapter resolution call sites; Decision Points > D1.
- **Verify**: `go test ./internal/operator -run TestHealthChecker_
  FansOutPerProtocol`; `go test ./internal/api/operator -run
  TestHandlerTestConnection_LegacyFlatOperator`.
- **Complexity**: high (health-check fanout semantics, deregister
  lifecycle, prometheus label schema change).
- **Depends on**: Task 1.

### Wave 2 — registry refactor + router D4-A

#### Task 3: Registry key change — `map[uuid.UUID]Adapter` → `map[adapterKey]Adapter`

- **What**: In `internal/operator/adapter/registry.go`:
  1. Define `type adapterKey struct { OperatorID uuid.UUID; Protocol
     string }` (unexported).
  2. `Registry.adapters` becomes `map[adapterKey]Adapter`.
  3. `GetOrCreate(operatorID, protocol, config)` — signature
     unchanged (protocol arg was already named `adapterType` but used
     interchangeably) but internally keyed by `adapterKey{operatorID,
     protocol}`.
  4. `Set(operatorID, protocol, a Adapter)` — signature GAINS
     protocol. All call sites updated.
  5. `Get(operatorID, protocol) (Adapter, bool)` — signature GAINS
     protocol.
  6. `Remove(operatorID)` — KEEP as "remove all protocols for
     operator" (convenient for operator-delete path); ADD
     `RemoveProtocol(operatorID, protocol)` for per-protocol
     invalidation.
  7. `HasFactory(protocol)` — unchanged (already keyed by protocol
     type).
  8. Existing factories at `:25-36` — unchanged. Add `http` factory
     stub (new Task 4 wires the full implementation).
- **Files**:
  - MOD `internal/operator/adapter/registry.go`
  - MOD `internal/operator/adapter/registry_test.go`
  - MOD `internal/api/operator/handler.go` (call site update at
    `:500` — `Remove(id)` semantics unchanged; call site at `:589`
    moved to Task 7a)
  - MOD `internal/operator/health.go` (Task 2's `GetOrCreate` call
    passes protocol correctly — confirm signature alignment)
- **Pattern ref**: Read existing `Registry` impl top-to-bottom — this
  is a pure shape change, not a semantic one; preserve all
  mutex/idempotency invariants.
- **Context refs**: Architecture Context > Adapter registry current
  shape; AC-2, AC-8.
- **Verify**: `go test ./internal/operator/adapter/... -v` — all
  existing tests pass after update; new test
  `TestRegistryMultiProtocolPerOperator` asserts AC-2.
- **Complexity**: high (central data structure change; every caller
  must update in lockstep; concurrency correctness must be
  preserved).
- **Depends on**: Task 2 (health.go already expects per-protocol
  call shape).

#### Task 3b: Drop `adapter_type` column migration + store/seed/export reader sweep (D2-B)

- **What**: Comprehensive D2-B column-drop in one tightly-coupled
  task so the repo stays green from the first commit of this
  task to the last.
  1. **Create migration pair** (both under `migrations/`):
     - `20260418120000_drop_operators_adapter_type.up.sql` —
       `ALTER TABLE operators DROP COLUMN IF EXISTS adapter_type;`
     - `20260418120000_drop_operators_adapter_type.down.sql` —
       `ALTER TABLE operators ADD COLUMN IF NOT EXISTS adapter_type
       VARCHAR(30);` (nullable per D2-B rollback-safety spec; see
       §Database Schema for the annotated header comment to include).
  2. **Store layer** (`internal/store/operator.go`):
     - Remove `AdapterType string` field from the `Operator` struct
       (line ~30).
     - Remove `AdapterType` from `CreateOperatorParams` (line ~94)
       and from `UpdateOperatorParams`.
     - Remove `adapter_type` column from every SELECT column list
       (lines ~139, ~288, ~417 scan destinations — drop the
       `AdapterType` arg from each `rows.Scan` / `row.Scan` call).
     - Remove `adapter_type` column binding from INSERT at
       line ~192; drop the corresponding parameter placeholder.
     - Remove any `WHERE adapter_type = …` queries if present
       (grep confirms none exist today — but run a final sweep as
       part of this task).
  3. **Seeds** (`migrations/seed/002_system_data.sql`,
     `003_comprehensive_seed.sql`, `005_multi_operator_seed.sql`):
     - Strip `adapter_type` from every `INSERT INTO operators (…)
       VALUES (…)` statement's column list AND the corresponding
       value tuple. The seeds will then insert ONLY
       `(id, name, code, mcc, mnc, adapter_config,
       supported_rat_types, …)` without `adapter_type`.
     - Reshape seed `adapter_config` values from flat to nested in
       the same edit: e.g., `{"latency_ms":12,"simulated_imsi_count":
       1000}` becomes `{"mock":{"enabled":true,"latency_ms":12,
       "simulated_imsi_count":1000}}`. This keeps a fresh `make
       db-seed` green against the post-090 code.
  4. **Export handler** (`internal/api/operator/export.go:29`):
     - Remove `adapter_type` from the CSV header array.
     - Remove the corresponding value from every exported row
       (replace with a derived `strings.Join(enabledProtocols, ",")`
       field under a new column heading `"enabled_protocols"`).
  5. **Audit handler**: existing audit `before_data`/`after_data`
     JSONB entries recorded pre-090 are historical artifacts — no
     backfill. New audit entries post-090 include
     `"enabled_protocols": [...]` instead of `"adapter_type": "…"`.
     Document this delta in the migration's up.sql header comment.
  6. **Go tests**: exhaustive grep + fix:
     ```bash
     rg -n --type go '\badapter_type\b|\bAdapterType\b' \
        internal/ cmd/
     ```
     - Remove or update every match in non-test Go sources.
     - In `_test.go` sources, update assertions: `assert.Equal(…,
       op.AdapterType)` → `assert.Equal(…, op.EnabledProtocols[0])`.
     - Specific files known to reference: `internal/api/operator/
       handler_test.go`, `internal/store/operator_test.go` (if
       exists), operator fixtures in test helpers (e.g.,
       `internal/testutil/` if any).
  7. **CI guard in this task**: include a `go vet ./...` + `go
     build ./...` + `go test ./...` run as the verify step.
- **Files**:
  - NEW `migrations/20260418120000_drop_operators_adapter_type.up.sql`
  - NEW `migrations/20260418120000_drop_operators_adapter_type.down.sql`
  - MOD `internal/store/operator.go`
  - MOD `internal/store/operator_test.go` (if the file exists;
    otherwise create a minimal test covering the new Operator
    shape)
  - MOD `migrations/seed/002_system_data.sql`
  - MOD `migrations/seed/003_comprehensive_seed.sql`
  - MOD `migrations/seed/005_multi_operator_seed.sql`
  - MOD `internal/api/operator/export.go`
  - MOD `internal/api/operator/handler_test.go` (assertion updates
    — may overlap with Task 1; land Task 1 first)
- **Pattern ref**: Read existing `migrations/` folder for golang-
  migrate file-naming convention (YYYYMMDDHHMMSS_description.
  {up,down}.sql). Read `internal/store/operator.go` top-to-bottom
  to enumerate every reference. Follow ADR-002 (golang-migrate).
- **Context refs**: Decision Points > D2 (LOCKED → D2-B); AC-12,
  AC-13.
- **Verify**:
  - `make db-migrate` succeeds against a disposable Postgres.
  - `make db-migrate-down STEPS=1` restores column as NULLABLE and
    succeeds.
  - `rg -n --type go '\badapter_type\b|\bAdapterType\b' internal/
     cmd/ | grep -v '_test.go' | grep -v 'migrations/'` returns
    ZERO matches.
  - `go test ./...` full suite stays green.
- **Complexity**: high (cross-cutting: migration + store + seeds +
  export + tests; one-commit landing keeps the tree building).
- **Depends on**: Tasks 1, 2, 3 (Task 1 removes response DTO
  field; Tasks 2-3 de-couple the registry call sites; Task 3b
  finishes the sweep at the store/seed/export layer).

#### Task 4: HTTP adapter implementation + factory registration

- **What**: New file `internal/operator/adapter/http.go` implementing
  the `Adapter` interface for operator HTTP-based SoR calls. Minimal
  scope:
  1. `type HTTPAdapter struct { config struct { BaseURL string;
     AuthType string; BearerToken string; BasicUser string;
     BasicPass string; HealthPath string; }; client *http.Client }`.
  2. `NewHTTPAdapter(cfg json.RawMessage) (Adapter, error)` —
     unmarshal config; validate `base_url` parses; set defaults
     (health_path="/health", timeout=2s).
  3. `Type() string` → "http".
  4. `HealthCheck(ctx)` → HTTP GET `BaseURL + HealthPath` with
     Authorization header per AuthType. Returns
     `HealthResult{Success, LatencyMs, Error}`.
  5. Remaining `Adapter` interface methods
     (`ForwardAuth`, `ForwardAcct`, `SendCoA`, `SendDM`,
     `Authenticate`, `AccountingUpdate`, `FetchAuthVectors`) return
     `ErrUnsupportedProtocol` — HTTP is a "health-checkable"
     operator for metadata/config sync, not an AAA adapter. Document
     via file header comment.
  6. Register in `registry.go::NewRegistry` factories block
     (`r.factories["http"] = …`) and in `NewAdapter` switch at
     `:110-123`.
- **Files**:
  - NEW `internal/operator/adapter/http.go`
  - NEW `internal/operator/adapter/http_test.go`
  - MOD `internal/operator/adapter/registry.go` (factory
    registration + NewAdapter switch)
- **Pattern ref**: Read `internal/operator/adapter/mock.go` for
  HealthResult-only adapter shape; read `internal/esim/smdp_http.go`
  (STORY-015 precedent) for HTTP client + Auth header pattern.
- **Context refs**: Decision Points > D3 (field set); Architecture
  Context > Screen Mockups > Protocol-specific field sets.
- **Verify**: `go test ./internal/operator/adapter -run
  TestHTTPAdapter_HealthCheck` — happy + 404 + timeout + basic-auth
  paths.
- **Complexity**: medium.
- **Depends on**: Task 3 (registration shape).

#### Task 5: Protocol-native HealthCheck for RADIUS/Diameter/SBA (D3-B)

- **What**: Extend the existing `HealthCheck` on each adapter:
  1. `internal/operator/adapter/radius.go::RADIUSAdapter.HealthCheck`
     — implement RFC 5997 Status-Server OR a minimal Access-Request
     with synthetic IMSI `001010000000000` (a reserved test IMSI).
     Expect any response (Accept/Reject) within 2s. Timeout or
     transport error → `Success=false`.
  2. `internal/operator/adapter/diameter.go::DiameterAdapter.
     HealthCheck` — send DWR (command code 280, application 0) to
     each configured peer; expect DWA with Result-Code=2001 within
     2s. Short-circuit true on first DWA, false if all peers fail.
  3. `internal/operator/adapter/sba.go::SBAAdapter.HealthCheck`
     — GET `nrf_url + "/nnrf-nfm/v1/nf-instances"` with 2s timeout;
     200 → Success=true. 4xx/5xx/timeout → Success=false.
  4. `Mock.HealthCheck` (`mock.go`) — unchanged; reuse existing
     latency-sim.
  5. `HTTP.HealthCheck` — already in Task 4.
- **Files**:
  - MOD `internal/operator/adapter/radius.go`
  - MOD `internal/operator/adapter/diameter.go`
  - MOD `internal/operator/adapter/sba.go`
- **Pattern ref**: Read each adapter's existing `HealthCheck` impl
  (likely a placeholder returning static success). Read
  `internal/aaa/diameter/diameter.go` for DWR construction
  primitives; read `internal/aaa/radius/server.go` for RADIUS
  packet marshaling.
- **Context refs**: Decision Points > D3 (D3-B); Architecture
  references > RFC 5997 / RFC 6733 / TS 29.510.
- **Verify**: Per-adapter tests —
  `TestRADIUSAdapter_HealthCheck_LivePeerResponds`,
  `TestDiameterAdapter_HealthCheck_DWR_DWA`,
  `TestSBAAdapter_HealthCheck_NRF_Reachable`.
- **Complexity**: high (protocol-correctness across three protocols;
  RFC/TS citations required inline in each function's doc comment).
- **Depends on**: Task 3.

#### Task 6: OperatorRouter D4-A refactor — protocol arg everywhere

- **What**: In `internal/operator/router.go`:
  1. `breakers map[uuid.UUID]*CircuitBreaker` → `map[adapterKey]
     *CircuitBreaker` with the same `adapterKey` struct (or share
     from adapter pkg).
  2. Every method signature that takes `operatorID uuid.UUID` gains
     `protocol string` alongside: `RegisterOperator`,
     `RegisterOperatorWithFailover`, `RemoveOperator` (keeps both
     shapes — fleet-remove plus per-protocol), `GetAdapter`,
     `GetCircuitBreaker`, `ForwardAuth`, `ForwardAcct`, `SendCoA`,
     `SendDM`, `Authenticate`, `AccountingUpdate`,
     `FetchAuthVectors`, `HealthCheck`,
     `resolveWithCircuitBreaker`, `ForwardAuthWithPolicy`,
     `ForwardAcctWithPolicy` (latter two in `failover.go`).
  3. Test file `internal/operator/failover_test.go` updated in the
     same commit — existing test helpers (`newTestRouter`,
     `registerMockOperator`) gain `protocol` param.
  4. `cmd/argus/main.go:813` call (`_ = operatorRouter`) stays as-is
     — the router remains dead in production, but its API is now
     consistent with the registry.
- **Files**:
  - MOD `internal/operator/router.go`
  - MOD `internal/operator/failover.go`
  - MOD `internal/operator/failover_test.go`
- **Pattern ref**: The D4-A refactor is a pure function-signature
  change — no semantic logic changes. Read `router.go` top-to-bottom
  and mechanically add the new param.
- **Context refs**: Architecture Context > OperatorRouter is
  currently dead code; Decision Points > D4.
- **Verify**: `go test ./internal/operator/... -run 'TestRouter|
  TestFailover'` — all pass after update.
- **Complexity**: medium (mechanical refactor; no behavior change).
- **Depends on**: Task 3.

### Wave 3 — UI + test-connection endpoint + cleanups + evidence

#### Task 7a: Per-protocol Test-Connection endpoint (backend)

- **What**: In `internal/api/operator/handler.go`:
  1. Extract the existing `TestConnection` body at `:560-605` into a
     new private helper `testConnectionForProtocol(ctx, op *store.
     Operator, protocol string, decryptedConfig json.RawMessage)
     (testResponse, int, error)`. `int` is the HTTP status, `error`
     carries the error code.
  2. Rename the existing route handler to `TestConnectionLegacy` —
     it derives `protocol = op.AdapterType` and delegates.
  3. Add a new handler `TestConnectionForProtocol` that reads
     `protocol` from URL path, validates against the protocol set,
     and delegates.
  4. Wire new route in `cmd/argus/main.go` (or wherever operator
     routes are mounted — verify location; the handler package
     itself may expose a `Routes(r chi.Router)` method):
     `r.Post("/operators/{id}/test-connection/{protocol}",
     h.TestConnectionForProtocol)`. Keep the legacy route
     `r.Post("/operators/{id}/test-connection", h.TestConnectionLegacy)`.
  5. Shape detector logic from Task 2 lives in the helper — both
     paths call the same per-protocol resolver.
- **Files**:
  - MOD `internal/api/operator/handler.go`
  - MOD `cmd/argus/main.go` (route mount — one new line)
  - MOD `internal/api/operator/handler_test.go` (new test cases)
- **Pattern ref**: Existing `TestConnection` at `:560-605`; existing
  route patterns for detail/sub-resources
  (`/operators/{id}/sessions`, `/operators/{id}/traffic` are
  examples of {id}-scoped sub-routes).
- **Context refs**: API Specifications > POST .../test-connection/
  {protocol}; Decision Points > D1 (shape detection), D3 (native
  handshake).
- **Verify**: `go test ./internal/api/operator -run
  TestTestConnection_PerProtocol` — happy path for each protocol;
  422 for disabled protocol; 400 for invalid name; 404 for unknown
  operator.
- **Complexity**: medium.
- **Depends on**: Tasks 2, 3, 4, 5.

#### Task 7b: Protocols tab UI — per-protocol cards + Test Connection buttons

- **What**: In `web/src/pages/operators/detail.tsx`:
  1. Add a new `TabsTrigger value="protocols"` between `overview`
     and `health` at `:1280-1283`. Tab label "Protocols", icon
     `Radio` (already imported at `:24`).
  2. Add a new `TabsContent value="protocols"` sibling to the
     existing 10 content blocks at `:1322+`. Content: a React
     component `<ProtocolsPanel operator={operator} onUpdate={…}/>`
     rendering 5 cards (mock/radius/diameter/sba/http).
  3. New component file `web/src/components/operators/ProtocolsPanel.
     tsx` implementing:
     - `ProtocolsPanel(operator, onUpdate)` — reads
       `operator.adapter_config` (already on the DTO after Task 1).
     - Renders 5 `<ProtocolCard protocol={p} config={operator.
       adapter_config[p]} onSave={…} onTest={…}/>` children in
       canonical order.
     - Each card: enable toggle (Switch), expanded body with
       per-protocol fields (per §Screen Mockups), "Test Connection"
       button calling `useTestConnectionPerProtocol(id, protocol)`
       hook.
     - Local state for dirty form; `Save` button at the panel
       bottom calls `useUpdateOperator` with the merged
       adapter_config.
  4. New hook `web/src/hooks/use-operators.ts::useTestConnectionPerProtocol(id,
     protocol)` — calls `POST /api/v1/operators/{id}/test-connection/
     {protocol}`.
  5. Secrets masking: password fields use `<Input type="password">`
     (already a pattern in the project).
  6. Reuse atoms from §Design Token Map — NEVER raw `<input>` or
     hex colors.
- **Files**:
  - MOD `web/src/pages/operators/detail.tsx`
  - NEW `web/src/components/operators/ProtocolsPanel.tsx` (or
    `web/src/components/operators/protocols/ProtocolsPanel.tsx`
    + per-protocol `RadiusCard.tsx`, `DiameterCard.tsx`, etc. —
    developer judgment; default single file if <300 LOC)
  - MOD `web/src/hooks/use-operators.ts` (new hook)
- **Pattern ref**: Existing TabsContent panels in detail.tsx (e.g.,
  `value="health"` block) for structure; existing `Card`/`CardHeader`/
  `CardContent` usage elsewhere in operator pages.
- **Context refs**: Screen Mockups > Protocols tab; Design Token
  Map; API Specifications > POST .../test-connection/{protocol}.
- **Tokens**: Use ONLY classes from §Design Token Map — zero
  hardcoded hex/px.
- **Components**: Reuse atoms/molecules from §Design Token Map >
  Existing Components — NEVER raw HTML elements.
- **Note**: Invoke `frontend-design` skill for professional quality.
- **Verify**: `grep -rE '#[0-9a-fA-F]{3,6}|text-\[|bg-\[' web/src/
  components/operators/` → ZERO matches. UI smoke: navigate to
  `/operators/:id` → Protocols tab visible → 5 cards render → Test
  Connection shows latency for enabled protocols, 422 for disabled.
- **Complexity**: high (largest UI work in the story; 5 per-
  protocol field layouts + secrets masking + save/dirty state +
  test-connection button integration).
- **Depends on**: Task 7a (endpoint must exist for hook to hit).

#### Task 7c: Retire dead `supported_protocols` AND `adapter_type` UI fields + derive chips (D2-B UI sweep)

- **What**: Consolidated UI sweep — retires BOTH the dead
  `supported_protocols` field (AC-6) AND the `adapter_type`
  dropdown / type (D2-B). Every TypeScript and React surface that
  references `adapter_type` is updated in this single task.
  1. **`web/src/pages/operators/index.tsx`**:
     - Remove `supported_protocols: [] as string[]` from form state
       at `:203`.
     - Remove `toggleProtocol` at `:209-216`.
     - Remove the `PROTOCOL_OPTIONS` constant at `:192` (if still
       used elsewhere, keep but re-audit).
     - Remove the UI chip block at `:301-325` that renders the dead
       toggles.
     - Remove `supported_protocols` AND `adapter_type` from the
       `setForm({…})` reset at `:242`.
     - Remove the `adapter_type` dropdown from the Create form
       (consolidated with `supported_protocols` removal — ONE form
       edit covers both). Create dialog no longer asks the user to
       pick a type; users configure protocols via the Protocols tab
       post-creation (see step 4 hint below).
     - Remove `adapter_type` from `handleSubmit`'s
       `createMutation.mutateAsync({…})` payload at `:234-241`.
     - List row protocol chips: REPLACE
       `ADAPTER_DISPLAY[operator.adapter_type]` at `:131` (which is
       a dead reference post-D2-B) with a `<Badge>` list rendered
       from `operator.enabled_protocols` (new DTO field from Task
       1). Keep the header label as `ADAPTER_DISPLAY[operator.
       enabled_protocols[0]]` for the "primary" display.
  2. **`web/src/pages/operators/detail.tsx::EditOperatorDialog`**
     at `:841-949`:
     - Remove the `adapter_type` dropdown at `:913-915` entirely
       (the dropdown no longer has any persistence target under
       D2-B).
     - Replace with a read-only "Primary Protocol:
       {ADAPTER_DISPLAY[enabled_protocols[0]]}" display.
     - Remove `adapter_type` from the local form state and the
       mutation payload.
     - Header chip at `:1250` — replace
       `ADAPTER_DISPLAY[operator.adapter_type]` with
       `ADAPTER_DISPLAY[operator.enabled_protocols[0]]`.
     - InfoRow at `:221` — same replacement.
  3. **`web/src/pages/operators/compare.tsx:26`** — replace the
     `adapter_type` column heading + cell render with an
     `enabled_protocols` Badge-list render.
  4. **Create dialog hint**: After Create succeeds, show a toast
     that says "Operator created. Configure protocols in the
     Protocols tab." (or route the user directly to
     `/operators/{id}?tab=protocols`). The full editor lives in
     Task 7b's Protocols tab.
  5. **`web/src/hooks/use-operators.ts:62`** — REMOVE `adapter_type:
     string` field from the Operator type; ADD
     `enabled_protocols: string[]`.
  6. **`web/src/types/operator.ts:7`** — REMOVE
     `adapter_type: string` field; ADD
     `enabled_protocols: string[]`.
  7. **Create-mutation & Update-mutation payload types** — drop
     `adapter_type` from both; the create/update API payload no
     longer sends this field.
  8. **Exhaustive grep guard** (run as verify step):
     `rg -n 'adapter_type' web/src/` returns ZERO matches;
     `rg -n 'supported_protocols' web/src/` returns ZERO matches.
- **Files**:
  - MOD `web/src/pages/operators/index.tsx`
  - MOD `web/src/pages/operators/detail.tsx` (EditOperatorDialog +
    header chip + InfoRow only — tab changes live in Task 7b)
  - MOD `web/src/pages/operators/compare.tsx`
  - MOD `web/src/types/operator.ts`
  - MOD `web/src/hooks/use-operators.ts`
- **Pattern ref**: Existing Badge usage in the same file at the
  protocol-chip rendering site; existing `ADAPTER_DISPLAY` constant
  at `web/src/lib/constants.ts` for label strings.
- **Context refs**: Architecture Context > Current deployed reality >
  Frontend > Dead supported_protocols field; Decision Points > D2
  (LOCKED → D2-B); AC-6, AC-13.
- **Tokens**: Use ONLY classes from §Design Token Map — zero
  hardcoded hex/px.
- **Verify**: `rg -n 'supported_protocols' web/src/` → ZERO
  matches; `rg -n 'adapter_type' web/src/` → ZERO matches. UI
  smoke: Create dialog no longer shows the chip group or the
  adapter type dropdown; list view shows derived protocol chips;
  detail-page header shows derived-primary-protocol label.
- **Complexity**: medium-high (expanded from medium due to D2-B
  consolidation — single task now covers both retirements).
- **Depends on**: Task 1 (DTO has `enabled_protocols` field), Task
  3b (DB column dropped, seeds fixed), Task 7b (detail page
  Protocols tab exists so users have somewhere to configure).

#### Task 8: STORY-092 regression guard + evidence collection

- **What**:
  1. Run the full STORY-092 integration test suite against the
     post-090 code: `go test ./internal/aaa/radius/... ./internal/aaa/
     diameter/... ./internal/aaa/sba/... -run 'TestRADIUSAccess|
     TestGxCCAInitial|TestSBAFullFlow|TestEnforcerNilCacheIntegration'`.
     All must pass unchanged. If any fails, that's a blocking bug
     — fix before marking the gate.
  2. `go test ./...` full suite — ≥ 3000 PASS, 0 FAIL. `go vet
     ./...` exit 0.
  3. Manual smoke: `make up` + `make db-seed` → navigate to
     `/operators/:id` → Protocols tab renders → enable a new
     protocol, Save, verify DB row has nested shape after the write
     (`psql -c "SELECT adapter_config FROM operators WHERE …"` and
     decrypt in a Go REPL or via the API).
  4. Capture 6 screenshots under
     `docs/stories/test-infra/STORY-090-evidence/`:
     - /operators list (chips derived from enabled_protocols)
     - /operators/:id Protocols tab — 5 cards visible
     - /operators/:id Protocols tab — RADIUS card expanded with
       Test Connection success
     - /operators/:id Protocols tab — Diameter card expanded with
       Test Connection failure-case error message
     - /operators/:id/test-connection 422 response for disabled
       protocol (browser network tab screenshot or curl output)
     - Legacy operator (seed 002) viewed post-update showing the
       nested shape round-tripped
- **Files**:
  - NEW `docs/stories/test-infra/STORY-090-evidence/*.png` (6 PNGs)
- **Pattern ref**: STORY-092 `docs/stories/test-infra/STORY-092-
  evidence/` layout.
- **Context refs**: AC-9 (STORY-092 preserve); AC-11 (baseline
  green).
- **Verify**: tests green; screenshots attached.
- **Complexity**: low (manual + test-suite run).
- **Depends on**: Tasks 7a, 7b, 7c.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 (nested round-trip, no `adapter_type` in response) | Tasks 0, 1 | `TestHandlerCreate_NestedAdapterConfig`, `TestHandlerResponse_NoAdapterTypeField` |
| AC-2 (registry per-protocol) | Task 3 | `TestRegistryMultiProtocolPerOperator` |
| AC-3 (per-protocol test-connection) | Tasks 5, 7a | `TestTestConnection_PerProtocol` |
| AC-4 (Protocols tab UI) | Task 7b | Manual smoke, Task 8 screenshots |
| AC-5 (legacy lazy rewrite) | Tasks 1, 2 | `TestHandlerCreate_LegacyFlatStillWorks`, `TestHandlerUpdate_RewritesLegacyShape` |
| AC-6 (supported_protocols + adapter_type UI retired) | Task 7c | `rg -n 'supported_protocols\|adapter_type' web/src/` = 0 matches |
| AC-7 (single-protocol regression) | Tasks 1, 2 | existing `handler_test.go` suite, updated for `enabled_protocols[0]` assertions |
| AC-8 (router D4-A) | Tasks 3, 6 | updated `failover_test.go` |
| AC-9 (STORY-092 preserved) | Task 8 | STORY-092 integration tests still pass |
| AC-10 (HealthChecker fanout) | Task 2 | `TestHealthChecker_FansOutPerProtocol` |
| AC-11 (baseline green) | all | `go test ./...`, `go vet ./...` |
| AC-12 (`adapter_type` column absent post-migration) | Task 3b | `information_schema.columns` query, `\d operators` in psql |
| AC-13 (zero `adapter_type` references in source) | Tasks 1, 3b, 7c | `rg -n 'adapter_type\|AdapterType' internal/ cmd/ web/src/` guard |

## Test Strategy

- **Unit tests**: Tasks 0, 1, 3, 4, 5, 6, 7a, 7c each add or extend
  a `_test.go` covering new behavior. Target: **+18 unit test cases**
  total across `internal/api/operator/adapterschema/`,
  `internal/api/operator/`, `internal/operator/`,
  `internal/operator/adapter/`, and the frontend (RTL tests for the
  Protocols tab if the codebase already has them — verify existing
  frontend test infra before committing to this).
- **Integration tests**:
  - New: `TestHealthChecker_FansOutPerProtocol` — spawns a real
    `HealthChecker` with a test registry against an in-memory fake
    store; asserts per-protocol goroutine count and prometheus
    label fanout.
  - Preserved: all STORY-092 integration tests (`TestGxCCAInitial_
    FramedIPAddress`, `TestSBAFullFlow_NsmfAllocates`,
    `TestEnforcerNilCacheIntegration_STORY092`) — run post-090 as
    regression guard (Task 8).
- **Smoke / manual** (Task 8): Create a new operator with 2 enabled
  protocols via the UI; Test Connection each; verify DB row is
  encrypted + nested; disable one protocol; verify the health-check
  fanout deregisters within one cycle.
- **Regression guard**: `go test ./...` ≥ 3000 PASS; `go vet ./...`
  exit 0. No new package SKIPs.

## Rollback plan

- **D1-A (LOCKED)**: no new SQL migrations for the adapter_config
  reshape — rollback is pure `git revert` of Wave 1 + Wave 2 +
  Wave 3 task commits. Any legacy-flat rows that got lazily
  rewritten to nested are ALSO readable by the pre-090 code (the
  pre-090 code expects flat; the nested shape will fail validation
  at pre-090 handler on the NEXT Update attempt). Operationally:
  if rollback is needed AFTER some rows have been rewritten, an
  operator running the pre-090 binary sees those rows reject
  updates until a manual re-flattening. Acceptable risk window:
  the operators table is small (~5-20 rows); a DB admin can
  `UPDATE operators SET adapter_config=…` for each rewritten row
  in <15 min. Document this in the pull-request body.
- **D2-B (LOCKED)**: the drop-column migration has a down.sql
  that restores the column as NULLABLE. Rollback recipe:
  1. `make db-migrate-down STEPS=1` re-adds `operators.adapter_
     type VARCHAR(30)` as NULL-allowed.
  2. `git revert` the Task 1, Task 3b, Task 7c commits. The
     pre-090 binary now sees NULL `adapter_type` across all rows;
     depending on how strictly the pre-090 binary enforces
     NOT-NULL in Go, it may need a data backfill.
  3. Emergency backfill (run against a psql session with the app
     paused, OR include in a follow-up migration written
     post-rollback):
     ```sql
     -- PLAINTEXT rows only (seeds). For encrypted rows the Go
     -- layer must decrypt via crypto.DecryptJSON — see the
     -- rollback helper script sketched below.
     UPDATE operators
       SET adapter_type = (
         CASE
           WHEN adapter_config->'diameter'->>'enabled' = 'true' THEN 'diameter'
           WHEN adapter_config->'radius'->>'enabled'   = 'true' THEN 'radius'
           WHEN adapter_config->'sba'->>'enabled'      = 'true' THEN 'sba'
           WHEN adapter_config->'http'->>'enabled'     = 'true' THEN 'http'
           WHEN adapter_config->'mock'->>'enabled'     = 'true' THEN 'mock'
           ELSE 'mock'
         END
       )
       WHERE adapter_type IS NULL;
     ALTER TABLE operators
       ALTER COLUMN adapter_type SET NOT NULL;
     ```
  4. Encrypted-row backfill helper (Go one-shot script — required
     for production rollback, since `jsonb->'radius'->>'enabled'`
     returns NULL on encrypted JSON strings):
     - Path: `cmd/migrate-rollback-adapter-type/main.go` (written
       post-rollback if needed; NOT in-story).
     - Loop rows, decrypt `adapter_config`, derive primary
       protocol, write back `adapter_type`. Idempotent.

  Operational risk window for D2-B rollback: longer than D1-A
  because it requires at minimum a down-migration + a backfill
  step. For the current deployment footprint (~5-20 operator
  rows) this is ~15-30 min of DBA work. Document clearly in the
  PR body.
- **D3-B (LOCKED)**: `HealthCheck` method body changes only.
  Pre-090 adapters had stub/placeholder `HealthCheck`; post-090 has
  native handshake. Rollback reverts the implementations. No data
  migration.
- **D4-A (LOCKED)**: router API signature change. Rollback
  reverts; since the router is dead code, operational blast radius
  = zero.
- Task 7b UI: pure new component + hook; rollback deletes them and
  reverts the TabsTrigger addition.
- Task 7c UI: dead-field removal + `adapter_type` UI retirement;
  rollback re-adds both fields. Under D2-B the frontend type system
  no longer has `adapter_type` at all — rollback requires
  re-introducing the field in `web/src/types/operator.ts` and all
  consumers.

## Wave plan

### Wave 1 (3 tasks, largely sequential)

- Task 0 runs first — no dependencies. `go test ./internal/api/
  operator/adapterschema/` must be green before Task 1 opens.
- Task 1 after Task 0. `go test ./internal/api/operator/...` must
  be green before Task 2.
- Task 2 after Task 1. No further Wave 1 work.

### Wave 2 (5 tasks, partly parallel — adds Task 3b for D2-B)

- Task 3 after Wave 1 (registry shape change).
- Task 3b after Task 3 (drop-column migration + store/seed/export
  reader sweep — D2-B). MUST land after Task 1 (response DTO field
  removal) and Task 3 (registry no longer keys by adapter_type) to
  keep the tree compiling in-flight. See Task 3b "Depends on".
- Task 4 (HTTP adapter) can run in parallel with Task 5 (native
  HealthCheck for radius/diameter/sba) — disjoint files.
- Task 5 after Task 3 (registry keys the call shape).
- Task 6 (router D4-A refactor) after Task 3 (shares `adapterKey`
  concept; developer may lift it from adapter package into a shared
  type or duplicate — prefer lift).

### Wave 3 (4 tasks, mostly sequential due to UI-endpoint coupling)

- Task 7a (endpoint) after Wave 2.
- Task 7b (UI) after Task 7a (hook targets the new endpoint).
- Task 7c (dead-code cleanup) in parallel with Task 7b (different
  files mostly — minor merge in `detail.tsx` at the Edit dialog).
- Task 8 last — runs the full gate and captures evidence.

## Story-Specific Compliance Rules

- **API**: two new/modified endpoints. `POST /api/v1/operators/{id}/
  test-connection/{protocol}` uses standard envelope (existing
  `apierr.WriteSuccess`/`WriteError`). Standard envelope applies to
  BOTH the new per-protocol route and the legacy single-protocol
  route (unchanged). Error codes: `PROTOCOL_NOT_CONFIGURED` (new)
  mapped to HTTP 422 with a clear message. Existing `VALIDATION_ERROR`
  covers nested shape validation.
- **DB**: ONE new migration pair under D2-B (LOCKED):
  `20260418120000_drop_operators_adapter_type.{up,down}.sql`.
  `operators.adapter_config` shape is reshaped purely at the app
  layer under D1-A (no SQL for that step). `operators.adapter_type`
  is DROPPED via the new migration; down-migration restores it as
  NULLABLE. ADR-002 (golang-migrate) triggered by the new
  migration pair — follow the convention.
- **UI**:
  - Design tokens from §Design Token Map — zero hardcoded hex / px.
  - Atomic reuse — no raw `<input>`, `<button>`, or inline SVG.
  - `frontend-design` skill invoked for Task 7b.
  - All secret fields (shared_secret, auth_token, client_key) masked
    via `<Input type="password">` in the form AND via the server-
    side masking in GET responses.
- **Business**:
  - At least one protocol MUST be `enabled=true` on every operator
    (validator hard-fails otherwise).
  - `enabled_protocols` is returned in canonical order (diameter →
    radius → sba → http → mock) on every response DTO. UI shows
    `enabled_protocols[0]` as the primary-protocol label via
    `ADAPTER_DISPLAY`.
  - Under D2-B the `adapter_type` column no longer exists — there
    is no legacy alias. Audit log `after_data` records the full
    enabled-protocols array; pre-090 audit entries keep their
    historical `adapter_type` field as a read-only artifact.
  - When an Update disables the previously-primary protocol, the
    next-enabled-protocol becomes the UI primary; audit captures
    both before and after `enabled_protocols` arrays.
  - TestConnection against a disabled protocol is a HARD 422 — no
    silent fallback to the enabled primary.
- **ADR**: ADR-002 (golang-migrate) — TRIGGERED by the D2-B
  drop-column migration pair. Follow the standard naming
  convention. ADR-003 (bcrypt / encryption) — preserved: the
  existing AES-GCM envelope stays unchanged.

## Bug Pattern Warnings

- **PAT-001 (SIM hot-path cache coherence)** — NOT applicable here;
  adapter_config changes do not touch the SIM cache. But adjacent
  concern: if the Registry key change (Task 3) introduces a subtle
  re-keying bug that causes the wrong adapter to be returned for
  (opID, protocol), it will manifest as a cross-protocol contamination
  (a RADIUS handler getting a Diameter adapter). Task 3 unit tests
  must include a cross-pollution assertion: register radius and
  diameter adapters for the same opID, then retrieve each and assert
  `Type()` matches the requested protocol.
- **PAT-004 (FK-to-partitioned-parent)** — NOT applicable; no new
  FKs.
- **No new pattern added by this story.**

## Tech Debt (from ROUTEMAP)

- **D-029** (STORY-079 Gate F-A4 — no CI guard against seed drift)
  — **PARTIALLY ADDRESSED** under D2-B: Task 3b edits seeds
  002/003/005 in lockstep with the schema migration, bringing the
  seeds into alignment with the post-090 schema. This does NOT
  install a general CI guard against seed drift — that remains
  D-029's concern. Flag Task 3b as proof-of-need for D-029's
  eventual CI guard (a test that runs `make db-seed` against a
  fresh `make db-migrate` and fails on any row error).
- **D-038** (Enforcer nil-cache) — closed by STORY-092; preserved
  here via AC-9 regression guard.
- **D-040** (potential new: "Admin CLI for fleet adapter_config
  rewrite") — OPEN after STORY-090 lands under D1-A. Captures the
  optional admin task that retires the dual-read code path. NOT
  part of STORY-090 scope. Add to ROUTEMAP Tech Debt table during
  the review step post-gate.
- **D-041** (potential new: "Encrypted-row rollback helper for
  D2-B") — CONDITIONALLY OPEN. If STORY-090 lands cleanly and no
  rollback is needed, this stays closed. If a rollback IS needed,
  a one-shot Go tool (`cmd/migrate-rollback-adapter-type/`) must
  be written post-rollback per §Rollback plan. NOT in STORY-090
  scope; captured here for traceability.

## Mock Retirement

No backend mocks retired. Frontend `supported_protocols` form field
is not a mock — it's cosmetic dead code and is retired under AC-6.

## Dependencies

- **STORY-092 (DONE, 2026-04-18)** — `IPPoolStore`+`SIMStore` on
  `aaadiameter.ServerDeps` and `aaasba.ServerDeps`. STORY-090 must
  preserve this wiring (AC-9).
- **STORY-086 (DONE)** + **STORY-087 (DONE)** — unrelated; no
  interaction surface.

## Blocking

- **STORY-089** (Operator SoR simulator) is unblocked by STORY-090
  DONE. STORY-089 targets the new nested adapter_config surface and
  will wire the registry into AAA hot paths.

## Out of Scope

- Admin CLI fleet-wide `adapter_config` rewrite (D1-A optional
  follow-up). Tracked under new tech debt D-040.
- Registry wiring into AAA hot paths (RADIUS server, Diameter
  server, SBA server calling `adapter.Registry.GetOrCreate` on the
  request path). This is STORY-089's work.
- PCF / UPF / QoS flow work (out of scope for any story in this
  track — these are SMF concerns tracked separately).
- RADIUS Proxy chaining (advisor explicitly flagged this as not part
  of STORY-090).
- IPv6 / dual-stack adapter_config keys — if an adapter needs v6,
  it's an internal field of that sub-config; no top-level schema
  change.
- Per-operator `circuit_breaker_threshold` and per-protocol breaker
  tuning — the existing per-operator knobs on the Operator table
  stay as-is. Per-protocol breakers under D4-A inherit the same
  threshold (acceptable simplification; can be refined in a
  follow-up).
- Mini Phase Gate spec extension is STORY-089 post-processing,
  per dispatch advisor hard flag #3.

## Risks & Mitigations

### Risk 1: Hot-path regression if adapter resolution breaks

- **Description**: Task 3's registry key change is touched by
  HealthChecker, TestConnection, and (when STORY-089 wires it)
  AAA hot paths. A mis-keyed map lookup silently returns nil or the
  wrong adapter.
- **Likelihood**: Medium (central data structure).
- **Mitigation**: Task 3 adds a cross-pollution test (see Bug
  Pattern Warnings). AC-8 asserts registry returns distinct
  instances. Task 8 runs the full STORY-092 integration suite as
  regression.

### Risk 2: Legacy operator can't be read after Wave 1 if shape detector is buggy

- **Description**: If `IsNestedShape` mis-classifies a legacy flat
  blob as nested (or vice versa), every read on that row returns
  malformed adapter_config. Users see blank Protocols tab.
- **Likelihood**: Low-Medium. The discriminator (presence of any of
  `mock|radius|diameter|sba|http` as top-level key) is
  unambiguous UNLESS an external tool or a future story adds a
  protocol field with a collision name.
- **Mitigation**: Task 0 unit tests explicitly cover edge cases —
  empty object `{}` (valid nested-no-protocols, which FAILS
  validation per the "at least one enabled" rule), flat blob with
  the literal word "radius" as a VALUE (not a key), encrypted vs
  plaintext envelopes, mixed-case protocol names (reject via
  normalization to lowercase).

### Risk 3: Migration data loss on lazy rewrite

- **Description**: If the Update handler's up-convert step
  accidentally drops sub-fields of the legacy flat blob, rewritten
  rows lose data silently.
- **Likelihood**: Low. The up-convert is a pure wrap
  (`{<type>: {enabled: true, …allFields}}`) — nothing is dropped.
- **Mitigation**: Task 1 test asserts a round-trip through
  UpConvertFlatToNested → re-serialize → decrypt → re-read matches
  the original sub-field set byte-for-byte (modulo the wrapping
  `{enabled:true}` addition).

### Risk 4: UI test-connection UX if protocol fails noisily

- **Description**: D3-B means the "Test" button makes a real
  protocol handshake that CAN be slow (2s timeout × 4 peers for
  Diameter = 8s worst case) or return cryptic errors ("DWR timeout
  peer 10.0.1.10:3868"). Users see "still loading…" for seconds
  or an error message they don't parse.
- **Likelihood**: Medium.
- **Mitigation**:
  1. Every `HealthCheck` has a hard 3s-wall-clock cap (Task 5 spec).
  2. Error strings sanitized server-side via a whitelist of common
     cases ("peer unreachable", "auth rejected", "tls handshake
     failed", "timeout") before reaching the UI.
  3. UI button shows a spinner + elapsed-time counter ("Testing…
     2.1s") so users see progress.
  4. After failure, a "Show details" disclosure reveals the raw
     error for operators who want it.

### Risk 5: Circuit breaker key collision under D4-A

- **Description**: If the `adapterKey` struct is not stable across
  package boundaries (adapter vs operator pkg), map lookups may
  miss on value-equal but import-path-different types. Go's
  structural typing handles this IF both pkgs use identical struct
  shape, but a subtle field-order drift breaks map equality.
- **Likelihood**: Low.
- **Mitigation**: Define `adapterKey` in ONE package (adapter
  package preferred) and have the operator package import it
  (`adapter.Key{OperatorID, Protocol}` — exported). Task 6 spec
  mandates the shared type.

### Risk 6: Frontend-backend type drift for nested shape

- **Description**: TypeScript types in `web/src/types/operator.ts`
  fall out of sync with Go DTOs in
  `internal/api/operator/handler.go`. The Protocols tab UI silently
  mishandles a sub-field type mismatch.
- **Likelihood**: Medium (this happens across the codebase).
- **Mitigation**: Task 7b defines the TypeScript types for nested
  adapter_config in one place (`web/src/types/operator.ts`) with a
  top-level `AdapterConfig` union type per protocol; Task 7b
  comments cite the Go struct lines. A runtime smoke test in Task
  8 creates an operator via API and re-reads it via UI, verifying
  the round-trip decodes cleanly.

### Risk 7: STORY-092 ServerDeps drift during Wave 2 main.go edits

- **Description**: Task 7a modifies `cmd/argus/main.go` for the
  new route. Adjacent edits may accidentally remove/rename the
  STORY-092 `IPPoolStore`/`SIMStore` fields in the Diameter or SBA
  ServerDeps construction block.
- **Likelihood**: Low-Medium.
- **Mitigation**: AC-9 explicit regression test asserts the
  STORY-092 integration tests still pass. Task 8 runs them as part
  of the gate. Code reviewer (planner/advisor) cross-checks the
  main.go diff against the STORY-092 plan's wiring assertions at
  §Current deployed reality > AAA server wiring. **Additional D2-B
  specific guard**: the new registry-resolution surface in Tasks
  3+3b threads ServerDeps through the same `IPPoolStore` +
  `SIMStore` fields it did before — Task 3b's reader sweep must
  NOT remove those fields. The pre-PR grep (`rg 'IPPoolStore\|
  SIMStore' internal/aaa/diameter/ internal/aaa/sba/`) serves as a
  checklist: the hit count must be identical before and after
  Task 3b.

### Risk 8: D2-B reader-sweep miss breaks production at runtime (NEW — added per D2-B selection)

- **Description**: D2-B drops a column referenced by Go code,
  SQL seeds, TypeScript types, and Go tests. If any reference is
  missed, the production binary will fail at runtime with a
  PostgreSQL `column "adapter_type" does not exist` error on the
  first query that touches the column — a clearly observable
  failure, but one that hits production traffic (operator CRUD
  endpoints, HealthChecker probes, audit writes).
- **Likelihood**: Medium. The blast radius is large: `rg -n
  '\badapter_type\b|\bAdapterType\b' internal/ cmd/ web/src/ seeds/
  migrations/seed/` currently returns 20+ matches across
  handler, store, seeds, export, tests, audit, UI.
- **Mitigation**:
  1. **Pre-PR grep sweep** as a hard gate: before marking Task 3b
     complete, the developer runs the grep above and confirms
     every non-test/non-migration match has been surgically
     removed or replaced.
  2. **CI green gate on full test suite** (AC-11): the
     `go test ./...` + `go vet ./...` run must pass with the new
     migration applied. Any leftover `Operator.AdapterType`
     reference causes a compile error — this is a BENEFIT of
     hard-removing the field from the Go struct (compile-time
     safety).
  3. **Staging smoke test**: Task 8 includes a manual smoke on a
     fresh DB (`make db-migrate` + `make db-seed` + seed some
     operators via the UI + verify list/detail render). A runtime
     error here blocks the gate.
  4. **Down-migration safety** (per §Database Schema): the down-
     migration restores the column as NULLABLE, so an emergency
     rollback does NOT require a synchronous data backfill. The
     backfill can happen asynchronously behind the restored
     pre-090 binary.
  5. **Audit log compatibility**: pre-090 audit entries preserve
     `"adapter_type": "…"` in their `before_data`/`after_data`
     JSONB — historical artifact, no migration. New audit entries
     write `"enabled_protocols": [...]`. The audit API reader
     tolerates both shapes (add a small shape-detector in the
     audit handler if it doesn't already — verify during Task 3b).

## Quality Gate (plan self-validation)

### Substance

- Goal stated (5-sentence summary with concrete deliverables).
- Root cause/target traced to exact lines (handler `:20-25, 135-155,
  317-505, 560-605`; registry `:11-17`; router `:15-356`;
  detail.tsx `:1280-1319, 832-914`; index.tsx `:185-325`; store
  `:30, :94, :139, :192, :288, :417`; export `:29`).
- Every advisor pre-draft flag surfaced and placed into task spec
  (encryption constraint → D1-A / Tasks 0+1+2; registry key change
  → Task 3; dead-code router → D4 / Task 6; envelope discriminator
  byte → §Architecture references).
- 4 explicit decision points (D1, D2, D3, D4) — **ALL LOCKED
  2026-04-18 per user selection: D1-A, D2-B (user override), D3-B,
  D4-A**.

### Required sections

- Goal ✓
- Architecture Context (Current deployed reality, Data flow, API
  specs, DB schema, Screen Mockups, Design Token Map) ✓
- Decision Points (D1–D4, ALL LOCKED) ✓
- Acceptance Criteria (AC-1 … AC-13) ✓
- Architecture references (registry shape, AES envelope, RFC 5997,
  RFC 6733, TS 29.510, STORY-092 surface) ✓
- Tasks (0, 1, 2, 3, 3b, 4, 5, 6, 7a, 7b, 7c, 8 — **12 numbered**,
  wave-grouped; Task 3b added under D2-B) ✓
- Acceptance Criteria Mapping ✓
- Test Strategy ✓
- Rollback plan ✓
- Wave plan ✓
- Story-Specific Compliance Rules ✓
- Bug Pattern Warnings ✓
- Tech Debt ✓
- Mock Retirement ✓
- Dependencies ✓
- Blocking ✓
- Out of Scope ✓
- Risks & Mitigations (8 — Risk 8 added for D2-B reader-sweep) ✓
- Quality Gate self-validation ✓

### Embedded specs

- Request/response shapes written out inline with example JSON.
- DB column source cited with migration file line numbers.
- AES envelope discriminator cited (first non-WS byte `"` vs `{`).
- Registry struct shape cited with line numbers.
- Screen mockup in ASCII with protocol-specific field sets
  enumerated per protocol.
- Design Token Map populated with exact class names.

### Effort confirmation

- Dispatch estimate: **L-XL**.
- Task count: **12** (0, 1, 2, 3, 3b, 4, 5, 6, 7a, 7b, 7c, 8) —
  adds Task 3b under D2-B for the drop-column migration + store/
  seed/export reader sweep.
- AC count: **13** (AC-1 … AC-13) — adds AC-12 (column absent
  post-migration) and AC-13 (zero `adapter_type` references in
  source) under D2-B.
- High-complexity tasks: **6** (Task 1 handler refactor, Task 2
  health-check fanout, Task 3 registry key change, Task 3b D2-B
  reader sweep, Task 5 native protocol HealthCheck across three
  protocols, Task 7b Protocols tab UI) — firmly in the "XL =
  multiple high" range.
- **Revised estimate: XL** (shifted up from L under D2-A because
  the D2-B reader sweep expands blast radius: migration + store
  rewrite + seed edits + export update + exhaustive test fixups
  in Task 3b, plus Task 7c grows to cover `adapter_type` UI
  retirement in addition to `supported_protocols`).
- Pre-Validation: PASS (all quality gate checks pass).
