# STORY-025: Staged Policy Rollout

## User Story
As a policy editor, I want to roll out a new policy version in stages (canary 1% → 10% → 100%) with the ability to rollback, so that I can safely deploy policy changes without risking the entire fleet.

## Description
Staged rollout of policy versions: start at 1% of affected SIMs, advance to 10%, then 100%. Each stage updates TBL-15 (policy_assignments) for selected SIMs and triggers CoA for active sessions. Supports concurrent policy versions during rollout (old + new). Rollback reverts all migrated SIMs to previous version with mass CoA. Progress tracked in TBL-16 (policy_rollouts) and pushed via WebSocket.

## Architecture Reference
- Services: SVC-05 (Policy Engine), SVC-04 (AAA — CoA), SVC-02 (WebSocket)
- API Endpoints: API-095 to API-099
- Database Tables: TBL-14 (policy_versions), TBL-15 (policy_assignments), TBL-16 (policy_rollouts)
- Data Flows: FLW-03 (Policy Staged Rollout)
- Source: docs/architecture/flows/_index.md (FLW-03)

## Screen Reference
- SCR-062: Policy Editor — rollout controls (start, advance, rollback), progress bar, stage indicator

## Acceptance Criteria
- [ ] POST /api/v1/policy-versions/:id/activate activates version immediately (100% rollout)
- [ ] POST /api/v1/policy-versions/:id/rollout starts staged rollout at 1%
- [ ] Rollout record created in TBL-16 with stages: [{pct:1,state:"pending"},{pct:10},{pct:100}]
- [ ] Stage 1 (1%): randomly select 1% of affected SIMs, update TBL-15, send CoA per active session
- [ ] POST /api/v1/policy-rollouts/:id/advance moves to next stage (10%, then 100%)
- [ ] Advance requires explicit user action (no auto-advance)
- [ ] Concurrent versions: during rollout, some SIMs on old version, some on new
- [ ] Policy evaluation at auth time uses SIM-specific version from TBL-15
- [ ] POST /api/v1/policy-rollouts/:id/rollback reverts ALL migrated SIMs to previous version
- [ ] Rollback triggers mass CoA for all reverted active sessions
- [ ] GET /api/v1/policy-rollouts/:id returns progress: current_stage, migrated_count, total_count, errors
- [ ] NATS: publish "policy.rollout_progress" on each stage completion
- [ ] WebSocket: push rollout progress to connected portal clients
- [ ] Rollout error handling: if CoA fails for a SIM, log error but continue (partial success)
- [ ] Only one active rollout per policy at a time

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-095 | POST | /api/v1/policy-versions/:id/activate | — | `{id,state:"active",activated_at}` | JWT(policy_editor+) | 200, 404, 422 |
| API-096 | POST | /api/v1/policy-versions/:id/rollout | `{stages?:[1,10,100]}` | `{rollout_id,version_id,stages,state:"in_progress"}` | JWT(policy_editor+) | 201, 404, 422 |
| API-097 | POST | /api/v1/policy-rollouts/:id/advance | — | `{rollout_id,current_stage_pct,migrated_count,total_count}` | JWT(policy_editor+) | 200, 422 |
| API-098 | POST | /api/v1/policy-rollouts/:id/rollback | `{reason?}` | `{rollout_id,state:"rolled_back",reverted_count}` | JWT(policy_editor+) | 200, 422 |
| API-099 | GET | /api/v1/policy-rollouts/:id | — | `{rollout_id,policy_id,version_id,stages,current_stage,migrated_count,total_count,errors,state}` | JWT(policy_editor+) | 200, 404 |

## Dependencies
- Blocked by: STORY-023 (policy versioning), STORY-017 (session CoA), STORY-022 (DSL evaluator)
- Blocks: STORY-046 (frontend policy editor rollout controls)

## Test Scenarios
- [ ] Start rollout → 1% of SIMs migrated to new version, TBL-15 updated
- [ ] Advance to 10% → additional 9% SIMs migrated
- [ ] Advance to 100% → all remaining SIMs migrated, rollout state=completed
- [ ] Rollback at 10% → all 10% reverted, CoA sent, rollout state=rolled_back
- [ ] CoA sent for each active session of migrated SIMs
- [ ] Concurrent auth: SIM on old version gets old policy, SIM on new version gets new policy
- [ ] Rollout progress event pushed via WebSocket
- [ ] Start rollout while another is in progress → 422 ROLLOUT_IN_PROGRESS
- [ ] Advance on completed rollout → 422 ROLLOUT_COMPLETED

## Effort Estimate
- Size: XL
- Complexity: Very High
