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

## Effort Estimate
- Size: M
- Complexity: Medium
