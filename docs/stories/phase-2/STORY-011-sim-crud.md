# STORY-011: SIM CRUD & State Machine

## User Story
As a SIM manager, I want to create, search, and manage SIM lifecycle states, so that I can control the entire SIM fleet.

## Description
SIM CRUD with full state machine (ORDERED→ACTIVE↔SUSPENDED→TERMINATED→PURGED + STOLEN/LOST), state transition validation, history logging, IP allocation on activation, and auto-purge scheduling.

## Architecture Reference
- Services: SVC-03 (Core API)
- API Endpoints: API-040 to API-052
- Database Tables: TBL-10 (sims), TBL-11 (sim_state_history), TBL-09 (ip_addresses)
- Source: docs/architecture/db/sim-apn.md

## Screen Reference
- SCR-020: SIM List (docs/screens/SCR-020-sim-list.md)
- SCR-021: SIM Detail (docs/screens/SCR-021-sim-detail.md)
- SCR-021e: SIM History (docs/screens/SCR-021e-sim-history.md)

## Acceptance Criteria
- [ ] POST /api/v1/sims creates SIM in ORDERED state
- [ ] POST /api/v1/sims/:id/activate → ORDERED→ACTIVE, allocates IP, assigns default policy
- [ ] POST /api/v1/sims/:id/suspend → ACTIVE→SUSPENDED, CoA placeholder, retains IP
- [ ] POST /api/v1/sims/:id/resume → SUSPENDED→ACTIVE
- [ ] POST /api/v1/sims/:id/terminate → ACTIVE/SUSPENDED→TERMINATED, schedules IP reclaim + purge
- [ ] POST /api/v1/sims/:id/report-lost → ACTIVE→STOLEN_LOST
- [ ] Invalid transitions return 422 (e.g., ORDERED→SUSPENDED)
- [ ] Every state transition creates entry in TBL-11 (sim_state_history)
- [ ] GET /api/v1/sims supports combo search: ICCID, IMSI, MSISDN, operator, APN, state, RAT
- [ ] GET /api/v1/sims uses cursor-based pagination (not offset)
- [ ] GET /api/v1/sims/:id returns full detail with current session, policy, IP, eSIM profile
- [ ] ICCID and IMSI are globally unique (unique index enforced)
- [ ] purge_at set to terminated_at + tenant.purge_retention_days
- [ ] Audit log entry for every state change

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-042 | POST | /api/v1/sims | `{iccid,imsi,msisdn?,operator_id,apn_id,sim_type,metadata?}` | `{id,iccid,imsi,state:"ordered",...}` | JWT(sim_manager+) | 201, 400, 409 |
| API-040 | GET | /api/v1/sims | `?cursor&limit&iccid&imsi&msisdn&operator_id&apn_id&state&rat_type&q` | `[{id,iccid,imsi,msisdn,operator,apn,state,rat_type,data_30d}]` | JWT(sim_manager+) | 200 |
| API-044 | POST | /api/v1/sims/:id/activate | — | `{id,state:"active",ip_address,policy_version}` | JWT(sim_manager+) | 200, 404, 422 |
| API-045 | POST | /api/v1/sims/:id/suspend | `{reason?}` | `{id,state:"suspended"}` | JWT(sim_manager+) | 200, 422 |
| API-047 | POST | /api/v1/sims/:id/terminate | `{reason?}` | `{id,state:"terminated",purge_at}` | JWT(tenant_admin) | 200, 422 |
| API-050 | GET | /api/v1/sims/:id/history | `?cursor&limit` | `[{from_state,to_state,reason,triggered_by,user,created_at}]` | JWT(sim_manager+) | 200 |

## Dependencies
- Blocked by: STORY-010 (APN + IP pool)
- Blocks: STORY-012 (segments), STORY-013 (bulk import)

## Test Scenarios
- [ ] Create SIM with unique ICCID/IMSI → 201, state=ordered
- [ ] Duplicate ICCID → 409 ICCID_EXISTS
- [ ] Activate SIM → state=active, IP allocated, history entry created
- [ ] Suspend active SIM → state=suspended, IP retained
- [ ] Resume suspended SIM → state=active
- [ ] Terminate SIM → state=terminated, purge_at calculated
- [ ] ORDERED→SUSPENDED → 422 INVALID_STATE_TRANSITION
- [ ] Search by IMSI prefix → returns matching SIMs
- [ ] Cursor pagination: first page returns cursor, second page uses it

## Effort Estimate
- Size: XL
- Complexity: High
