# STORY-014: MSISDN Number Pool Management

## User Story
As a SIM manager, I want to manage a pool of phone numbers and assign them to SIMs.

## Architecture Reference
- Services: SVC-03
- API Endpoints: API-160 to API-162
- Database Tables: TBL-24 (msisdn_pool)

## Acceptance Criteria
- [ ] POST /api/v1/msisdn-pool/import accepts CSV of MSISDNs per operator
- [ ] GET /api/v1/msisdn-pool lists numbers with state (available, assigned, reserved)
- [ ] POST /api/v1/msisdn-pool/:id/assign assigns MSISDN to SIM
- [ ] MSISDN globally unique across all tenants/operators
- [ ] Released on SIM termination (after grace period)

## Dependencies
- Blocked by: STORY-011
- Blocks: None

## Effort Estimate
- Size: S
- Complexity: Low
