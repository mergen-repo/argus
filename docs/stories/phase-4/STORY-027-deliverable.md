# Deliverable: STORY-027 — RAT-Type Awareness (All Layers)

## Summary

Enriched all system layers with canonical RAT-type awareness. Created shared `rattype` package with canonical enum and mapping functions used by RADIUS, Diameter, 5G SBA, Policy DSL, SoR engine, and session management. RAT type extracted from protocol requests, normalized to canonical form, stored on sessions, and propagated to SIM records.

## What Was Built

### Canonical RAT Type Package (internal/aaa/rattype/rattype.go)
- Canonical enum: `utran` (3G), `geran` (2G), `lte` (4G), `nb_iot`, `lte_m`, `nr_5g` (5G SA), `nr_5g_nsa`, `unknown`
- `FromRADIUS(value uint32)`: Maps 3GPP-RAT-Type AVP numeric values (1=UTRAN, 2=GERAN, 6=EUTRAN, etc.)
- `FromDiameter(value uint32)`: Same mapping for Diameter RAT-Type AVP
- `FromSBA(value string)`: Maps 5G SBA string values ("NR", "nr_5g", "E-UTRA", etc.)
- `Normalize(value string)`: Normalizes any RAT string to canonical form
- `DisplayName(canonical string)`: Returns human-readable display name
- `IsValid(value string)`: Validates against canonical + alias set
- `AllCanonical()`: Returns all canonical values
- 9 tests with full coverage

### RADIUS RAT Extraction (internal/aaa/radius/server.go)
- `extract3GPPRATType()`: Parses 3GPP-RAT-Type from Vendor-Specific Attribute (vendor 10415, type 21)
- RAT type set on session and included in NATS event

### Diameter Normalization (internal/aaa/diameter/gx.go, gy.go)
- Replaced local mapDiameterRATType with shared `rattype.FromDiameter`
- Added RAT-Type AVP extraction to Gy handler (was only in Gx)

### 5G SBA Normalization (internal/aaa/sba/ausf.go, udm.go)
- Replaced hardcoded "nr_5g" with `rattype.NR5G` constant
- `rattype.FromSBA()` for proper normalization of AMF registration RATType

### Session → SIM Update (internal/aaa/session/session.go, internal/store/sim.go)
- Session manager passes RATType to DB store on creation
- `WithSIMStore` functional option for SIM last_rat_type update
- `UpdateLastRATType()` method on SIM store

### DSL Parser Extension (internal/policy/dsl/parser.go)
- Extended validRATTypes with all canonical values + common aliases
- Supports: `nb_iot`, `lte_m`, `lte`, `nr_5g`, `2g`, `3g`, `4g`, `5g`, `5g_sa`, `5g_nsa`, `cat_m1`, `utran`, `geran`, `nr_5g_nsa`

### SoR Engine Alignment (internal/operator/sor/types.go)
- `DefaultRATPreferenceOrder` changed from display names to canonical values
- Added `nr_5g_nsa` to preference order

## Architecture References Fulfilled
- SVC-04: AAA RAT extraction (RADIUS, Diameter, 5G SBA)
- SVC-05: Policy DSL RAT conditions
- SVC-06: SoR RAT filtering with canonical names
- TBL-17: session rat_type field
- TBL-10: SIM last_rat_type update

## Files Changed
```
internal/aaa/rattype/rattype.go      (new)
internal/aaa/rattype/rattype_test.go (new)
internal/aaa/radius/server.go        (modified)
internal/aaa/diameter/gx.go          (modified)
internal/aaa/diameter/gy.go          (modified)
internal/aaa/diameter/diameter_test.go (modified)
internal/aaa/sba/ausf.go             (modified)
internal/aaa/sba/udm.go              (modified)
internal/aaa/session/session.go      (modified)
internal/store/sim.go                (modified)
internal/policy/dsl/parser.go        (modified)
internal/operator/sor/types.go       (modified)
internal/operator/sor/engine_test.go (modified)
cmd/argus/main.go                    (modified)
```
