# STORY-020: 5G SBA HTTP/2 Proxy (AUSF/UDM)

## User Story
As a platform operator, I want Argus to support 5G standalone authentication via HTTP/2 SBA interfaces, so that 5G network operators can authenticate SIMs through AUSF and UDM protocols.

## Description
Implement an HTTP/2 proxy for 5G Service-Based Architecture (SBA) interfaces. Act as AUSF (Authentication Server Function) for UE authentication and UDM (Unified Data Management) for subscription data. Support 5G-AKA and EAP-AKA' authentication methods. Handle network slice (S-NSSAI) authentication. Packaged as internal/aaa/sba listening on :8443 (TLS).

## Architecture Reference
- Services: SVC-04 (AAA Engine — internal/aaa/sba)
- Database Tables: TBL-17 (sessions), TBL-10 (sims)
- Packages: internal/aaa/sba
- Source: docs/architecture/services/_index.md (SVC-04)

## Screen Reference
- SCR-050: Live Sessions (5G sessions with slice info)
- SCR-041: Operator Detail (SBA endpoint status)
- SCR-120: System Health (SBA proxy status)

## Acceptance Criteria
- [ ] HTTP/2 server listens on :8443 with TLS (mTLS optional)
- [ ] AUSF: POST /nausf-auth/v1/ue-authentications — initiate 5G-AKA authentication
- [ ] AUSF: PUT /nausf-auth/v1/ue-authentications/{authCtxId}/5g-aka-confirmation — confirm auth
- [ ] UDM: GET /nudm-ueau/v1/{supiOrSuci}/security-information — fetch auth vectors
- [ ] UDM: POST /nudm-ueau/v1/{supiOrSuci}/auth-events — record auth result
- [ ] Support SUPI/SUCI identifier resolution (IMSI-based)
- [ ] Network slice authentication: validate S-NSSAI (SST + SD) against policy
- [ ] 5G-AKA flow: SUCI → SUPI resolution → AV generation → Auth confirmation
- [ ] EAP-AKA' flow via SBA: proxy to internal EAP-AKA' implementation
- [ ] Session created in TBL-17 with protocol_type='5g_sba' and slice_info
- [ ] JSON-based request/response per 3GPP TS 29.509 (AUSF) and TS 29.503 (UDM)
- [ ] Service discovery: NRF registration placeholder for future NF discovery

## Dependencies
- Blocked by: STORY-015 (RADIUS server — shared session model), STORY-016 (EAP-AKA')
- Blocks: None (optional advanced feature)

## Test Scenarios
- [ ] 5G-AKA initiation → auth context created, challenge returned
- [ ] 5G-AKA confirmation with valid RES* → authentication success, KSEAF derived
- [ ] 5G-AKA confirmation with invalid RES* → authentication failure
- [ ] SUCI to SUPI resolution → correct IMSI extracted
- [ ] UDM auth vector request → valid 5G HE AV returned
- [ ] Network slice authentication with valid S-NSSAI → allowed
- [ ] Network slice authentication with unauthorized S-NSSAI → rejected
- [ ] HTTP/2 connection with invalid TLS cert → connection refused
- [ ] Concurrent 5G authentications → no state leakage

## Effort Estimate
- Size: XL
- Complexity: Very High
