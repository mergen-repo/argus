# Implementation Plan: STORY-027 - RAT-Type Awareness Across All Layers

## Goal
Create a shared `rattype` package with canonical RAT-type enum, mapping functions (protocol numeric -> canonical -> display), and integrate it across RADIUS, Diameter, 5G SBA, session management, SIM store, policy DSL, and SoR engine so the entire platform uses a single normalized RAT taxonomy.

## Architecture Context

### Components Involved
- **`internal/aaa/rattype/`** (NEW): Shared package — canonical enum, protocol-to-canonical mapping, display name mapping
- **`internal/aaa/radius/server.go`** (SVC-04): RADIUS auth/acct — extract 3GPP-RAT-Type VSA from Access-Request, set on session
- **`internal/aaa/diameter/gx.go`** (SVC-04): Diameter Gx — already extracts RAT-Type AVP (code 1032), uses local `mapDiameterRATType` — migrate to shared package
- **`internal/aaa/diameter/gy.go`** (SVC-04): Diameter Gy — extract RAT-Type AVP on CCR-I, set on session
- **`internal/aaa/sba/ausf.go`** (SVC-04): 5G AUSF — currently hardcodes `"nr_5g"`, normalize to canonical
- **`internal/aaa/sba/udm.go`** (SVC-04): 5G UDM — currently does `strings.ToLower(reg.RATType)`, normalize to canonical
- **`internal/aaa/session/session.go`** (SVC-04): Session struct has `RATType` field — pass RAT to session creation, update SIM last_rat_type
- **`internal/store/session_radius.go`**: DB session create already accepts `RATType *string` — pass it through
- **`internal/store/sim.go`**: SIM struct has `RATType *string` (mapped to `rat_type` column) — add `UpdateLastRATType` method
- **`internal/policy/dsl/parser.go`** (SVC-05): DSL parser `validRATTypes` map — extend with broader enum values
- **`internal/policy/dsl/evaluator.go`** (SVC-05): Already evaluates `rat_type` in match and when conditions — works as-is
- **`internal/operator/sor/types.go`** (SVC-06): `DefaultRATPreferenceOrder` uses display names — align to canonical enum
- **`internal/operator/sor/engine.go`** (SVC-06): `filterByRAT` uses case-insensitive comparison — works with canonical values

### Data Flow
```
RADIUS Access-Request (3GPP-RAT-Type VSA, vendor 10415, type 21)
  → radius/server.go: extract 3GPP-RAT-Type vendor-specific attribute
  → rattype.FromRADIUS(rawValue uint8) → canonical string ("lte", "nr_5g", etc.)
  → session.Session.RATType = canonical
  → store: INSERT sessions (..., rat_type = $N)
  → store: UPDATE sims SET rat_type = $1 WHERE id = $2

Diameter CCR (RAT-Type AVP code 1032, vendor 10415)
  → diameter/gx.go or gy.go: extract RAT-Type AVP
  → rattype.FromDiameter(rawValue uint32) → canonical string
  → session.Session.RATType = canonical
  → same store flow as above

5G SBA (AUSF confirmation / UDM registration)
  → sba/ausf.go: always "NR" radio access → rattype.Canonical5GSA → "nr_5g"
  → sba/udm.go: reg.RATType field → rattype.FromSBA(rawString) → canonical
  → same session/SIM update flow

Policy DSL evaluation:
  → SessionContext.RATType already set from session
  → WHEN rat_type = "lte" matches session with rat_type="lte"
  → CHARGING rat_type_multiplier uses canonical keys

SoR engine:
  → SoRRequest.RequestedRAT = canonical value
  → CandidateOperator.SupportedRATs from DB (operator.supported_rat_types)
  → filterByRAT compares canonical-to-canonical (case-insensitive, already works)
```

### Database Schema

#### TBL-17: sessions (ACTUAL — from migrations/20260320000002_core_schema.up.sql)
```sql
-- Source: migrations/20260320000002_core_schema.up.sql (ACTUAL)
CREATE TABLE IF NOT EXISTS sessions (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,
    apn_id UUID,
    nas_ip INET,
    framed_ip INET,
    calling_station_id VARCHAR(50),
    called_station_id VARCHAR(100),
    rat_type VARCHAR(10),            -- already exists
    session_state VARCHAR(20) NOT NULL DEFAULT 'active',
    auth_method VARCHAR(20),
    policy_version_id UUID,
    acct_session_id VARCHAR(100),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    terminate_cause VARCHAR(50),
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    packets_in BIGINT NOT NULL DEFAULT 0,
    packets_out BIGINT NOT NULL DEFAULT 0,
    last_interim_at TIMESTAMPTZ
);
-- sor_decision JSONB added by migrations/20260321000001_sor_fields.up.sql
-- protocol_type TEXT added by migrations/20260320000006_session_protocol_type.up.sql
```

#### TBL-10: sims (ACTUAL — from migrations/20260320000002_core_schema.up.sql)
```sql
-- Source: migrations/20260320000002_core_schema.up.sql (ACTUAL)
-- sims table already has: rat_type VARCHAR(10)
-- This is `last_rat_type` equivalent — updated when a new session starts
```

#### TBL-05: operators (ACTUAL)
```sql
-- Source: migrations/20260320000002_core_schema.up.sql (ACTUAL)
-- operators table already has: supported_rat_types VARCHAR[] NOT NULL DEFAULT '{}'
```

#### TBL-18: cdrs (ACTUAL)
```sql
-- Source: migrations/20260320000002_core_schema.up.sql (ACTUAL)
-- cdrs table already has: rat_type VARCHAR(10), rat_multiplier DECIMAL(4,2) DEFAULT 1.0
```

**No new migration needed.** All required columns already exist in the schema.

### Canonical RAT-Type Enum

The shared `rattype` package defines canonical values used across all layers:

| Canonical (stored) | Display Name | RADIUS 3GPP-RAT-Type | Diameter RAT-Type (AVP 1032) | 5G SBA | DSL Alias |
|---|---|---|---|---|---|
| `utran` | 3G | 1 | 1000 | - | `utran`, `3g` |
| `geran` | 2G | 2 | 1001 | - | `geran`, `2g` |
| `lte` | 4G | 6 | 1004 | - | `lte`, `4g` |
| `nb_iot` | NB-IoT | 9 (vendor-specific) | 1005 | - | `nb_iot` |
| `lte_m` | LTE-M/CAT-M1 | 10 (vendor-specific) | 1006 | - | `lte_m`, `cat_m1` |
| `nr_5g` | 5G | 7 (NR from 3GPP TS 29.061) | 1009 | NR, nr_5g | `nr_5g`, `5g` |
| `nr_5g_nsa` | 5G-NSA | 8 | 1008 | E-UTRA-NR | `nr_5g_nsa`, `5g_nsa` |
| `unknown` | Unknown | any other | any other | any other | `unknown` |

**Design decisions:**
- Canonical values match existing DSL conventions (`nb_iot`, `lte_m`, `lte`, `nr_5g`)
- This is Option A from story notes: normalize protocol values to DSL conventions
- Display names for UI/SoR: separate `DisplayName(canonical) string` function
- DSL parser extended to accept broader aliases (e.g., `4G` maps to `lte` internally)
- SoR `DefaultRATPreferenceOrder` updated to use canonical values

### RADIUS 3GPP-RAT-Type Extraction

The 3GPP-RAT-Type is a Vendor-Specific Attribute (VSA):
- **Vendor ID**: 10415 (3GPP)
- **Vendor Type**: 21 (3GPP-RAT-Type)
- **Format**: 4-byte integer (Unsigned32 in RADIUS VSA encoding)
- **Standard**: 3GPP TS 29.061

In `layeh/radius`, VSAs are accessed via the raw `Attr(26)` (Vendor-Specific attribute type 26) and must be manually decoded: parse vendor ID (4 bytes), then vendor type (1 byte), then vendor length (1 byte), then value.

## Prerequisites
- [x] STORY-015 (RADIUS server) — provides `internal/aaa/radius/server.go`, session creation flow
- [x] STORY-019 (Diameter server) — provides `internal/aaa/diameter/`, Gx/Gy handlers with RAT-Type extraction
- [x] STORY-020 (5G SBA) — provides `internal/aaa/sba/`, already sets `rat_type='nr_5g'`
- [x] STORY-022 (Policy DSL) — provides `internal/policy/dsl/`, parser validates RAT types
- [x] STORY-026 (SoR engine) — provides `internal/operator/sor/`, `filterByRAT` exists

## Tasks

### Task 1: Create shared rattype package
- **Files:** Create `internal/aaa/rattype/rattype.go`, Create `internal/aaa/rattype/rattype_test.go`
- **Depends on:** — (none)
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/session/session.go` — follow same package structure (constants, types, functions)
- **Context refs:** Canonical RAT-Type Enum, RADIUS 3GPP-RAT-Type Extraction
- **What:**
  Create `internal/aaa/rattype/rattype.go` with:
  1. String constants for canonical RAT types: `UTRAN = "utran"`, `GERAN = "geran"`, `LTE = "lte"`, `NBIOT = "nb_iot"`, `LTEM = "lte_m"`, `NR5G = "nr_5g"`, `NR5GNSA = "nr_5g_nsa"`, `Unknown = "unknown"`
  2. `FromRADIUS(rawValue uint8) string` — maps RADIUS 3GPP-RAT-Type numeric values to canonical:
     - 1 → utran, 2 → geran, 6 → lte, 7 → nr_5g, 8 → nr_5g_nsa, 9 → nb_iot, 10 → lte_m, else → unknown
  3. `FromDiameter(rawValue uint32) string` — maps Diameter RAT-Type AVP values to canonical:
     - 1000 → utran, 1001 → geran, 1004 → lte, 1005 → nb_iot, 1006 → lte_m, 1008 → nr_5g_nsa, 1009 → nr_5g, else → unknown
  4. `FromSBA(rawString string) string` — normalizes 5G SBA RAT-Type strings:
     - "NR", "nr", "nr_5g", "NR_5G" → nr_5g; "E-UTRA-NR", "e-utra-nr" → nr_5g_nsa; "E-UTRA", "e-utra", "EUTRA", "LTE" → lte; else → lowercase of input if it matches a known canonical, otherwise unknown
  5. `Normalize(raw string) string` — normalizes any free-form string to canonical:
     - Case-insensitive comparison against canonical values and aliases
     - Alias map: "2g"→geran, "3g"→utran, "4g"→lte, "5g"→nr_5g, "5g_sa"→nr_5g, "5g_nsa"→nr_5g_nsa, "cat_m1"→lte_m, "NB-IoT"→nb_iot, plus all canonical values
  6. `DisplayName(canonical string) string` — returns display name: utran→"3G", geran→"2G", lte→"4G", nb_iot→"NB-IoT", lte_m→"LTE-M", nr_5g→"5G", nr_5g_nsa→"5G-NSA", unknown→"Unknown"
  7. `IsValid(value string) bool` — returns true if value is a recognized canonical RAT type
  8. `AllCanonical() []string` — returns all canonical values
  9. `AllDisplayNames() map[string]string` — returns canonical→display map

  Create `internal/aaa/rattype/rattype_test.go` with tests for:
  - FromRADIUS: each known value maps correctly, unknown value returns "unknown"
  - FromDiameter: each known value maps correctly, unknown value returns "unknown"
  - FromSBA: "NR"→nr_5g, "E-UTRA-NR"→nr_5g_nsa, "LTE"→lte, unknown string
  - Normalize: "4G"→lte, "5G_SA"→nr_5g, "CAT_M1"→lte_m, "nb_iot"→nb_iot, unknown→unknown
  - DisplayName: each canonical→correct display name
  - IsValid: canonical values return true, junk returns false
- **Verify:** `go test ./internal/aaa/rattype/...`

### Task 2: RADIUS RAT-Type extraction
- **Files:** Modify `internal/aaa/radius/server.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/diameter/gx.go` lines 111-117 — follow same pattern of extracting RAT AVP and mapping
- **Context refs:** Architecture Context > Data Flow, RADIUS 3GPP-RAT-Type Extraction
- **What:**
  In `internal/aaa/radius/server.go`, modify `handleAcctStart`:
  1. Import `"github.com/btopcu/argus/internal/aaa/rattype"`
  2. After the NAS-IP and Framed-IP extraction (around line 518), before session creation:
     - Extract 3GPP-RAT-Type from the RADIUS packet's Vendor-Specific Attribute (VSA)
     - VSA is attribute type 26, vendor ID 10415 (3GPP), vendor type 21
     - Use `r.Packet.Lookup(radius.Type(26))` to get raw VSA bytes
     - Parse: first 4 bytes = vendor ID (10415), next 1 byte = vendor type (21), next 1 byte = vendor length, remaining bytes = value (uint32 big-endian, but typically fits in uint8)
     - Call `rattype.FromRADIUS(value)` to get canonical string
     - If no VSA present or parsing fails, leave rat_type empty
  3. Set `sess.RATType = ratTypeCanonical` on the session being created
  4. Include `rat_type` in the session.started event payload published to NATS

  Also modify `handleDirectAuth` to extract and log the RAT type (even though Access-Accept doesn't create a session, the RAT type is useful for future policy evaluation in the auth path).
- **Verify:** `go build ./internal/aaa/radius/...`

### Task 3: Diameter RAT-Type normalization and Gy extraction
- **Files:** Modify `internal/aaa/diameter/gx.go`, Modify `internal/aaa/diameter/gy.go`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `internal/aaa/diameter/gx.go` lines 111-117 and 266-281 — existing RAT extraction pattern to refactor
- **Context refs:** Architecture Context > Components Involved, Canonical RAT-Type Enum
- **What:**
  1. In `gx.go`:
     - Import `"github.com/btopcu/argus/internal/aaa/rattype"`
     - Replace the local `mapDiameterRATType` function call (line 115) with `rattype.FromDiameter(ratVal)`
     - Delete the `mapDiameterRATType` function (lines 266-281) — it's replaced by the shared package

  2. In `gy.go`:
     - Import `"github.com/btopcu/argus/internal/aaa/rattype"`
     - In `handleInitial` (line 69), after the SIM resolver block (around line 119):
       - Extract RAT-Type AVP: `ratAVP := msg.FindAVPVendor(AVPCodeRATType3GPP, VendorID3GPP)` (same pattern as Gx)
       - If found, `sess.RATType = rattype.FromDiameter(ratVal)`
- **Verify:** `go build ./internal/aaa/diameter/...`

### Task 4: 5G SBA RAT-Type normalization
- **Files:** Modify `internal/aaa/sba/ausf.go`, Modify `internal/aaa/sba/udm.go`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `internal/aaa/sba/ausf.go` lines 208-216 — current hardcoded "nr_5g" to replace
- **Context refs:** Architecture Context > Components Involved, Canonical RAT-Type Enum
- **What:**
  1. In `ausf.go`:
     - Import `"github.com/btopcu/argus/internal/aaa/rattype"`
     - Line 211: Replace `RATType: "nr_5g"` with `RATType: rattype.NR5G`
     - This is a constant reference, not a functional change, but ensures consistency

  2. In `udm.go`:
     - Import `"github.com/btopcu/argus/internal/aaa/rattype"`
     - Line 158: Replace `RATType: strings.ToLower(reg.RATType)` with `RATType: rattype.FromSBA(reg.RATType)`
     - This normalizes arbitrary RATType strings from AMF registration to canonical values
     - Remove the `"strings"` import if it's no longer used (check other usages first)
- **Verify:** `go build ./internal/aaa/sba/...`

### Task 5: Session manager — pass RAT to store and update SIM last_rat_type
- **Files:** Modify `internal/aaa/session/session.go`, Modify `internal/store/sim.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/session/session.go` lines 94-155 — session Create method
- **Context refs:** Architecture Context > Data Flow, Database Schema
- **What:**
  1. In `session.go`, modify `Create` method:
     - Currently, the `CreateRadiusSessionParams` does NOT pass `RATType` from the session. Fix this:
     - Add `RATType: nilIfEmpty(sess.RATType)` to the `CreateRadiusSessionParams` struct initialization (around line 122, after `AuthMethod`)
     - The `CreateRadiusSessionParams` struct in `store/session_radius.go` already has `RATType *string` field and the INSERT query already includes `rat_type`

  2. In `store/sim.go`, add a new method `UpdateLastRATType`:
     ```
     func (s *SIMStore) UpdateLastRATType(ctx context.Context, simID uuid.UUID, operatorID uuid.UUID, ratType string) error
     ```
     - Executes: `UPDATE sims SET rat_type = $3, updated_at = NOW() WHERE id = $1 AND operator_id = $2`
     - The `operator_id` is needed because `sims` is partitioned by `operator_id` (composite PK)
     - Returns error if no rows affected (SIM not found)

  3. In `session.go`, modify `Create` method:
     - After successful session creation in the DB (after line 131), if `sess.RATType != ""` and `sess.SimID != ""`:
       - Parse simID and operatorID as UUIDs
       - Call `m.sessionStore.SIMStore().UpdateLastRATType(ctx, simID, operatorID, sess.RATType)`
       - Since `sessionStore` is `*store.RadiusSessionStore` and doesn't have SIM store access, instead add the UpdateLastRATType functionality directly. Alternative approach: add `simStore *store.SIMStore` to the Manager struct, or handle the SIM update at the caller level (RADIUS/Diameter handler).
       - **Better approach**: Do NOT add SIM store to session Manager. Instead, add the SIM RAT update to each protocol handler after session creation. This keeps the session manager focused and avoids circular dependencies.
       - So: skip the SIM update in session manager. The SIM update will be done in Task 6.
- **Verify:** `go build ./internal/aaa/session/... && go build ./internal/store/...`

### Task 6: SIM last_rat_type update in protocol handlers + tests
- **Files:** Modify `internal/aaa/radius/server.go`, Modify `internal/aaa/diameter/gx.go`, Modify `internal/aaa/diameter/gy.go`, Modify `internal/aaa/sba/ausf.go`, Modify `internal/aaa/sba/udm.go`
- **Depends on:** Task 2, Task 3, Task 4, Task 5
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/radius/server.go` lines 469-573 — handleAcctStart session creation flow
- **Context refs:** Architecture Context > Data Flow, Database Schema
- **What:**
  This task is about updating the SIM's `rat_type` (last known RAT) whenever a session is created. Since the SIM store is not accessible from the session manager, each protocol handler should update it.

  However, looking at the existing code more carefully:
  - RADIUS server has `simCache` (which is a SIMCache, not SIMStore)
  - Diameter handlers have `simResolver` interface
  - SBA handlers don't have SIM store access

  **Revised approach**: The simplest and cleanest approach is to add a `SIMStore` field to the session `Manager` struct and call `UpdateLastRATType` in the `Create` method. This is a clean cross-cutting concern — the session manager already knows about sessions and SIMs.

  1. In `internal/store/sim.go`, add method:
     ```go
     func (s *SIMStore) UpdateLastRATType(ctx, simID, operatorID uuid.UUID, ratType string) error
     ```
     SQL: `UPDATE sims SET rat_type = $3, updated_at = NOW() WHERE id = $1 AND operator_id = $2`

  2. In `internal/aaa/session/session.go`:
     - Add `simStore` field to `Manager` struct (optional, nil-safe)
     - Update `NewManager` to accept `*store.SIMStore` parameter
     - In `Create`, after successful DB insert, if `sess.RATType != ""` and simID/operatorID are valid UUIDs and `m.simStore != nil`:
       - Call `m.simStore.UpdateLastRATType(ctx, simID, operatorID, sess.RATType)`
       - Log warning on error, don't fail session creation

  3. Update all callers of `NewManager` — check `cmd/argus/main.go` or wherever session.Manager is instantiated:
     - Pass `simStore` or `nil` as needed
     - This is a signature change, so all callers must be updated
- **Verify:** `go build ./...`

### Task 7: DSL parser — extend valid RAT types
- **Files:** Modify `internal/policy/dsl/parser.go`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `internal/policy/dsl/parser.go` lines 28-30 — existing `validRATTypes` map
- **Context refs:** Canonical RAT-Type Enum
- **What:**
  In `internal/policy/dsl/parser.go`:
  1. Extend `validRATTypes` map to include all canonical values AND common aliases:
     ```go
     var validRATTypes = map[string]bool{
         "nb_iot":    true,
         "lte_m":     true,
         "lte":       true,
         "nr_5g":     true,
         "utran":     true,
         "geran":     true,
         "nr_5g_nsa": true,
         "cat_m1":    true,
         "2g":        true,
         "3g":        true,
         "4g":        true,
         "5g":        true,
         "5g_sa":     true,
         "5g_nsa":    true,
         "unknown":   true,
     }
     ```
  2. The DSL compiler and evaluator will work as-is because they use string comparison
  3. Users can write `WHEN rat_type = "4g"` or `WHEN rat_type = "lte"` — both are valid
  4. The evaluator's `matchValues` function uses `strings.EqualFold` so case-insensitive matching already works
- **Verify:** `go build ./internal/policy/dsl/...`

### Task 8: SoR engine — align RAT preference to canonical values
- **Files:** Modify `internal/operator/sor/types.go`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `internal/operator/sor/types.go` lines 43-58 — existing DefaultRATPreferenceOrder
- **Context refs:** Architecture Context > Components Involved, Canonical RAT-Type Enum
- **What:**
  In `internal/operator/sor/types.go`:
  1. Import `"github.com/btopcu/argus/internal/aaa/rattype"`
  2. Update `DefaultRATPreferenceOrder` from display names to canonical values:
     ```go
     var DefaultRATPreferenceOrder = []string{
         rattype.NR5G,     // "nr_5g" (was "5G")
         rattype.NR5GNSA,  // "nr_5g_nsa" (new)
         rattype.LTE,      // "lte" (was "4G")
         rattype.UTRAN,    // "utran" (was "3G")
         rattype.GERAN,    // "geran" (was "2G")
         rattype.NBIOT,    // "nb_iot" (was "NB-IoT")
         rattype.LTEM,     // "lte_m" (was "LTE-M")
     }
     ```
  3. The `filterByRAT` in `engine.go` already uses `strings.EqualFold` for comparison, so the SoR filtering will work with both canonical and display name values in operator `supported_rat_types` column. However, for consistency, operators should store canonical values.
- **Verify:** `go build ./internal/operator/sor/...`

### Task 9: Comprehensive tests for RAT-type integration
- **Files:** Create `internal/aaa/rattype/integration_test.go`
- **Depends on:** Task 1, Task 7, Task 8
- **Complexity:** medium
- **Pattern ref:** Read `internal/policy/dsl/evaluator_test.go` — follow same test structure with helper functions
- **Context refs:** Canonical RAT-Type Enum, Architecture Context > Data Flow
- **What:**
  Create `internal/aaa/rattype/integration_test.go` with cross-layer scenario tests:

  1. **TestRADIUSRATMapping**: RADIUS 3GPP-RAT-Type=6 (EUTRAN) → `FromRADIUS(6)` → "lte" → `DisplayName("lte")` → "4G"
  2. **TestRADIUSRATMappingUnknown**: Unknown value 99 → "unknown"
  3. **TestDiameterRATMapping**: Diameter RAT-Type=1004 → `FromDiameter(1004)` → "lte"
  4. **Test5GSBARATMapping**: "NR" → `FromSBA("NR")` → "nr_5g"
  5. **TestNormalizeAliases**: "4G" → `Normalize("4G")` → "lte", "CAT_M1" → "lte_m", "5G_SA" → "nr_5g"
  6. **TestPolicyEvaluatorWithRAT**: Compile a policy with `WHEN rat_type = "lte"`, evaluate with SessionContext{RATType: "lte"} → matches; evaluate with {RATType: "utran"} → does not match
  7. **TestPolicyEvaluatorWithRATAlias**: Policy `WHEN rat_type = "4g"`, SessionContext{RATType: "lte"} → should match because matchValues uses EqualFold. Actually "4g" != "lte" with EqualFold. This is a known issue — DSL users must use canonical values in policies, and sessions store canonical values. Document this.
  8. **TestSoRPreferenceOrder**: Verify DefaultRATPreferenceOrder contains canonical values and `IsValid` returns true for each
  9. **TestAllCanonicalRATTypesValid**: Iterate AllCanonical(), verify IsValid returns true for each
  10. **TestDisplayNameRoundTrip**: For each canonical, DisplayName returns non-empty, Normalize(DisplayName) maps back correctly
- **Verify:** `go test ./internal/aaa/rattype/...`

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| RADIUS: extract 3GPP-RAT-Type from Access-Request VSA | Task 2 | Task 9 (TestRADIUSRATMapping) |
| Diameter: extract RAT-Type AVP from CCR messages | Task 3 (Gx already done, Gy added) | Task 9 (TestDiameterRATMapping) |
| 5G SBA: extract RAT type from authentication context | Task 4 (normalize existing) | Task 9 (Test5GSBARATMapping) |
| Session record: rat_type field populated | Task 5 (pass to store) | Build verify |
| SIM record: last_rat_type updated | Task 5, Task 6 | Build verify |
| Policy DSL: WHEN conditions support rat_type | Task 7 (extend valid types) | Task 9 (TestPolicyEvaluatorWithRAT) |
| Policy evaluation includes RAT-type matching | Already works (evaluator.go) | Task 9 |
| Operator capability map: supported_rat_types | Already exists (TBL-05) | N/A (schema already present) |
| SoR engine: filter operators by RAT capability | Already works (engine.go filterByRAT) | Task 9 (TestSoRPreferenceOrder) |
| CDR: rat_type field | Already exists (TBL-18) | N/A (schema already present) |
| Analytics: group_by=rat_type | Already works (sim.go AggregateByRATType) | N/A |
| Dashboard: RAT breakdown option | Session stats already has ByRATType | N/A |
| Enum: RAT types per AC | Task 1 (canonical enum) | Task 9 (TestAllCanonicalRATTypesValid) |

## Story-Specific Compliance Rules

- **API**: No new endpoints — this enriches existing data flows
- **DB**: No new migrations — all columns already exist
- **Business**: RAT-type is informational; unknown RAT type must not reject sessions (AC: "session still created")
- **ADR-003**: Custom AAA engine — all protocol handling is in Go, consistent with ADR
- **Convention**: Store canonical lowercase values in DB (`lte`, `nr_5g`), display names (`4G`, `5G`) only in UI layer

## Risks & Mitigations
- **Risk**: RADIUS VSA parsing edge cases (different vendor encodings) → Mitigation: Graceful fallback to empty rat_type; log warning, never reject session
- **Risk**: SoR `DefaultRATPreferenceOrder` change from display names to canonical could break existing SoR configurations if operators store display names in `supported_rat_types` → Mitigation: `filterByRAT` uses `strings.EqualFold` which is case-insensitive; document that operators should use canonical values
- **Risk**: `NewManager` signature change breaks callers → Mitigation: Use optional `*store.SIMStore` parameter, check all callers
