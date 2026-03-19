# STORY-016: EAP-SIM/AKA/AKA' Authentication Methods

## User Story
As a platform operator, I want Argus to support EAP-SIM, EAP-AKA, and EAP-AKA' authentication methods, so that SIMs can authenticate using standard 3GPP challenge-response protocols.

## Description
Implement EAP-SIM (RFC 4186), EAP-AKA (RFC 4187), and EAP-AKA' (RFC 5448) as authentication method plugins within the AAA Engine. These handle the multi-round-trip challenge-response flow embedded in RADIUS Access-Request/Challenge cycles. The EAP state machine manages identity exchange, challenge generation (via operator HLR/AuC), and key derivation. Packaged as internal/aaa/eap with method-specific sub-packages.

## Architecture Reference
- Services: SVC-04 (AAA Engine — internal/aaa/eap)
- Database Tables: TBL-17 (sessions), TBL-10 (sims)
- Packages: internal/aaa/eap, internal/aaa/eap/sim, internal/aaa/eap/aka, internal/aaa/eap/akaprime
- Source: docs/architecture/services/_index.md (SVC-04)

## Screen Reference
- SCR-050: Live Sessions (auth method shown per session)
- SCR-041: Operator Detail (supported auth methods per operator)

## Acceptance Criteria
- [ ] EAP-SIM: handle EAP-Request/Identity → SIM-Start → SIM-Challenge → SIM-Success flow
- [ ] EAP-AKA: handle AKA-Identity → AKA-Challenge (RAND+AUTN) → AKA-Success flow
- [ ] EAP-AKA': handle AKA'-Challenge with AT_KDF and AT_KDF_INPUT attributes
- [ ] EAP state machine tracks pending challenges in Redis (TTL 30s)
- [ ] Challenge vectors (triplets for SIM, quintets for AKA) fetched from operator adapter (SVC-06)
- [ ] Authentication vector caching: pre-fetch batches to reduce latency
- [ ] EAP-Failure sent on AUTN mismatch (sync failure) or MAC verification failure
- [ ] Session key (MSK/EMSK) derived and included in Access-Accept for encryption
- [ ] EAP method selection based on SIM type (physical SIM → EAP-SIM, USIM → EAP-AKA')
- [ ] Auth method recorded in TBL-17 session record (eap_method column)
- [ ] Pluggable method registry: new EAP methods can be added without changing core

## Dependencies
- Blocked by: STORY-015 (RADIUS server), STORY-018 (operator adapter for vector fetch)
- Blocks: STORY-020 (5G SBA uses AKA')

## Test Scenarios
- [ ] EAP-SIM full flow: Identity → Start → Challenge → Success with valid triplets
- [ ] EAP-AKA full flow: Identity → Challenge → Success with valid quintets
- [ ] EAP-AKA' full flow with KDF negotiation
- [ ] SIM authentication failure (wrong SRES) → EAP-Failure
- [ ] AKA sync failure (AUTN mismatch) → Synchronization-Failure notification
- [ ] Challenge timeout (>30s) → EAP-Failure, pending state cleaned from Redis
- [ ] Multiple concurrent EAP sessions for different SIMs (no state leakage)
- [ ] Unknown EAP method type → EAP-NAK with supported methods list

## Effort Estimate
- Size: XL
- Complexity: Very High
