# STORY-027: RAT-Type Awareness Across All Layers

## User Story
As a platform operator, I want RAT-type (2G/3G/4G/5G) awareness throughout the system — in policies, analytics, operator routing, and cost calculations — so that I can make radio-technology-specific decisions.

## Description
Enrich all system layers with RAT-type information: RADIUS attributes (3GPP-RAT-Type AVP), policy WHEN conditions per RAT, analytics breakdown by RAT, operator capability mapping (which operators support which RATs), and CDR cost differentiation by RAT. RAT type extracted from RADIUS/Diameter requests and stored on session and SIM records.

## Architecture Reference
- Services: SVC-04 (AAA — RAT extraction), SVC-05 (Policy — RAT conditions), SVC-06 (Operator — RAT capabilities), SVC-07 (Analytics — RAT breakdown)
- Database Tables: TBL-17 (sessions — rat_type), TBL-10 (sims — last_rat_type), TBL-05 (operators — supported_rat_types), TBL-18 (cdrs — rat_type)
- Source: docs/architecture/services/_index.md

## Screen Reference
- SCR-010: Main Dashboard — SIM distribution includes RAT breakdown
- SCR-011: Analytics Usage — filter/group by RAT type
- SCR-012: Analytics Cost — cost per RAT type
- SCR-041: Operator Detail — supported RAT types
- SCR-050: Live Sessions — RAT type column

## Acceptance Criteria
- [ ] RADIUS: extract 3GPP-RAT-Type from Access-Request vendor-specific attributes
- [ ] Diameter: extract RAT-Type AVP from CCR messages
- [ ] 5G SBA: extract RAT type from authentication context
- [ ] Session record (TBL-17): rat_type field populated on session creation
- [ ] SIM record (TBL-10): last_rat_type updated on each new session
- [ ] Policy DSL: WHEN conditions support `rat_type = "4G"`, `rat_type IN ("4G","5G")`
- [ ] Policy evaluation includes RAT-type matching for rule selection
- [ ] Operator capability map: TBL-05.supported_rat_types (JSONB array)
- [ ] SoR engine: filter operators by RAT capability before cost/priority ranking
- [ ] CDR (TBL-18): rat_type field for cost differentiation
- [ ] Analytics: all usage/cost queries support group_by=rat_type
- [ ] Dashboard: SIM distribution pie chart includes RAT breakdown option
- [ ] Enum: RAT types = [2G, 3G, 4G, 5G_NSA, 5G_SA, NB_IOT, CAT_M1]

## Dependencies
- Blocked by: STORY-015 (RADIUS), STORY-022 (policy DSL), STORY-026 (SoR engine)
- Blocks: None (enrichment across existing features)

## Test Scenarios
- [ ] RADIUS Access-Request with 3GPP-RAT-Type=6 (EUTRAN) → session.rat_type = "4G"
- [ ] Policy with `WHEN rat_type = "4G"` matches 4G session → correct action applied
- [ ] Policy with `WHEN rat_type = "3G"` does not match 4G session → next rule evaluated
- [ ] Analytics usage query with group_by=rat_type → breakdown returned
- [ ] Operator with supported_rat_types=["4G","5G"] → SoR includes for 4G request
- [ ] Operator with supported_rat_types=["3G"] → SoR excludes for 4G request
- [ ] CDR with rat_type=4G uses 4G cost rate, not 3G rate
- [ ] Unknown RAT type value → mapped to "UNKNOWN", session still created

> **Note (post-STORY-020):** STORY-020 (5G SBA) already sets `rat_type='nr_5g'` on sessions created via AUSF 5G-AKA confirmation (ausf.go L211) and `rat_type` from UDM AMF registration (udm.go L158, lowercased from RATType field). The AC "5G SBA: extract RAT type from authentication context" is partially done. This story should verify and normalize the mapping (e.g., "NR" -> "5G_SA", "nr_5g" -> "5G_SA") to match the enum in AC (5G_SA). Also, the `session.Session` struct and TBL-17 `rat_type` column already exist from STORY-015/017 — no migration needed for this field.

> **Note (post-STORY-022, RAT enum alignment):** The Policy DSL parser (STORY-022, `internal/policy/dsl/`) validates RAT types as `nb_iot`, `lte_m`, `lte`, `nr_5g` (per DSL_GRAMMAR.md). This story's AC defines a broader enum: `2G, 3G, 4G, 5G_NSA, 5G_SA, NB_IOT, CAT_M1`. There is a naming mismatch — e.g., DSL uses `lte` but STORY-027 uses `4G`. Two options: (a) Normalize protocol-extracted RAT values to DSL conventions (`nb_iot/lte_m/lte/nr_5g`) before storing in session/SIM records and `SessionContext.RATType`, OR (b) Extend the DSL parser's valid RAT type list to include the broader STORY-027 enum. Option (a) is recommended — fewer values, DSL already works, and RAT display names can be mapped in the UI layer.

> **Note (post-STORY-026, SoR integration):** STORY-026 implemented the SoR engine at `internal/operator/sor/`. Key integration points for STORY-027: (1) `SoREngine.Evaluate(ctx, SoRRequest)` is the single entry point — set `SoRRequest.RequestedRAT` to trigger RAT filtering, (2) `CandidateOperator.SupportedRATs []string` carries per-grant RAT capabilities (from TBL-06 `supported_rat_types`), (3) `engine.go:filterByRAT` already filters candidates by requested RAT, (4) `DefaultRATPreferenceOrder = ["5G","4G","3G","2G","NB-IoT","LTE-M"]` uses display names not DSL conventions (`lte`, `nr_5g`). STORY-027 should normalize RAT values before passing to SoR or align SoR's preference order to DSL conventions. Recommendation: create a shared `rattype` package with canonical enum + mapping functions used by both DSL and SoR.

## Effort Estimate
- Size: M
- Complexity: Medium
