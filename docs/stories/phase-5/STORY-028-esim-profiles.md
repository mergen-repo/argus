# STORY-028: eSIM Profile Management

## User Story
As a SIM manager, I want to manage eSIM profiles with enable/disable/switch operations and SM-DP+ integration readiness, so that I can remotely control eSIM-capable devices.

## Description
eSIM profile CRUD with lifecycle management. Each eSIM SIM can have multiple profiles (one per operator) stored in TBL-12. Operations include enable, disable, and switch (activate a different operator profile). SM-DP+ API integration is a placeholder interface for future operator-specific implementations. Profile state machine: available → enabled ↔ disabled → deleted.

## Architecture Reference
- Services: SVC-03 (Core API)
- API Endpoints: API-070 to API-074
- Database Tables: TBL-12 (esim_profiles), TBL-10 (sims — sim_type='esim')
- Source: docs/architecture/api/_index.md (eSIM section)

## Screen Reference
- SCR-070: eSIM Profiles — profile list, status, operator, actions

## Acceptance Criteria
- [ ] GET /api/v1/esim-profiles lists profiles with filters (sim_id, operator, state)
- [ ] GET /api/v1/esim-profiles/:id returns profile detail with SIM info, operator, ICCID, state
- [ ] POST /api/v1/esim-profiles/:id/enable enables profile (available/disabled → enabled)
- [ ] POST /api/v1/esim-profiles/:id/disable disables profile (enabled → disabled)
- [ ] POST /api/v1/esim-profiles/:id/switch switches to different operator profile (disables current, enables target)
- [ ] Only one profile per SIM can be enabled at a time
- [ ] Switch operation: atomic — disable old + enable new in single transaction
- [ ] Switch triggers: operator change on TBL-10, APN reassignment, IP reallocation, policy reassignment
- [ ] Switch triggers CoA/DM if SIM has active session
- [ ] SM-DP+ adapter interface defined: `DownloadProfile()`, `EnableProfile()`, `DisableProfile()`, `DeleteProfile()`
- [ ] SM-DP+ mock adapter for development (simulates profile operations)
- [ ] Profile operations create audit log entries
- [ ] Profile operations create SIM state history entries in TBL-11

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-070 | GET | /api/v1/esim-profiles | `?cursor&limit&sim_id&operator_id&state` | `[{id,sim_id,iccid,operator,state,created_at}]` | JWT(sim_manager+) | 200 |
| API-071 | GET | /api/v1/esim-profiles/:id | — | `{id,sim_id,iccid,operator,state,smdp_id,metadata}` | JWT(sim_manager+) | 200, 404 |
| API-072 | POST | /api/v1/esim-profiles/:id/enable | — | `{id,state:"enabled"}` | JWT(sim_manager+) | 200, 404, 422 |
| API-073 | POST | /api/v1/esim-profiles/:id/disable | — | `{id,state:"disabled"}` | JWT(sim_manager+) | 200, 404, 422 |
| API-074 | POST | /api/v1/esim-profiles/:id/switch | `{target_profile_id}` | `{sim_id,old_profile,new_profile,new_operator,new_apn}` | JWT(sim_manager+) | 200, 404, 422 |

## Dependencies
- Blocked by: STORY-011 (SIM CRUD), STORY-010 (APN/IP pool)
- Blocks: STORY-030 (bulk operator switch uses eSIM profiles)

## Test Scenarios
- [ ] List eSIM profiles filtered by operator → correct results
- [ ] Enable available profile → state=enabled, only one enabled per SIM
- [ ] Enable when another profile is enabled → 422 PROFILE_ALREADY_ENABLED
- [ ] Disable enabled profile → state=disabled
- [ ] Switch profiles → old disabled, new enabled, operator/APN/IP updated on SIM
- [ ] Switch with active session → CoA/DM triggered
- [ ] Enable on non-eSIM SIM → 422 NOT_ESIM
- [ ] Profile operations logged in audit log

## Effort Estimate
- Size: L
- Complexity: High
