# STORY-024: Policy Dry-Run Simulation

## User Story
As a policy editor, I want to dry-run a policy version against the SIM fleet before activation, so that I can preview how many SIMs will be affected and what changes will occur per operator, APN, and RAT type.

## Description
Dry-run evaluates a policy version against the current SIM fleet without applying any changes. Returns affected SIM count broken down by operator, APN, and RAT type. Shows which SIMs would match new rules vs. existing rules, highlighting behavioral changes (e.g., QoS downgrade, new charging model). Results displayed in a preview pane on the policy editor.

## Architecture Reference
- Services: SVC-05 (Policy Engine), SVC-03 (Core API)
- API Endpoints: API-094
- Database Tables: TBL-14 (policy_versions), TBL-10 (sims), TBL-15 (policy_assignments)
- Source: docs/architecture/api/_index.md (Policies section)

## Screen Reference
- SCR-062: Policy Editor — dry-run preview pane with affected SIM breakdown charts

## Acceptance Criteria
- [ ] POST /api/v1/policy-versions/:id/dry-run evaluates version against SIM fleet
- [ ] Dry-run scoped to policy's MATCH block (operator, APN, RAT filters)
- [ ] Response includes: total_affected_sims, by_operator, by_apn, by_rat_type breakdowns
- [ ] Response includes: behavioral_changes (list of changes: QoS upgrade/downgrade, new charging, session limit changes)
- [ ] Response includes: sample_sims (first 10 affected SIMs with before/after policy result)
- [ ] Dry-run executes read-only (no DB writes, no CoA, no session changes)
- [ ] Dry-run for large fleets (>100K SIMs) runs async as job, returns 202 with job_id
- [ ] Small fleets (<100K) return synchronous 200 response
- [ ] Dry-run result cached for 5 minutes (invalidated on SIM fleet changes)
- [ ] Dry-run on invalid/unparseable DSL → 422 with compilation errors

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-094 | POST | /api/v1/policy-versions/:id/dry-run | `{segment_id?}` | `{total_affected,by_operator:{},by_apn:{},by_rat:{},behavioral_changes:[],sample_sims:[]}` | JWT(policy_editor+) | 200, 202, 404, 422 |

## Dependencies
- Blocked by: STORY-022 (DSL evaluator), STORY-023 (policy versioning), STORY-011 (SIM data)
- Blocks: STORY-025 (rollout uses dry-run data for stage planning)

## Test Scenarios
- [ ] Dry-run on policy matching 50 SIMs → returns breakdown with 50 total
- [ ] Dry-run shows QoS downgrade for SIMs currently on premium tier
- [ ] Dry-run with segment filter → only segment SIMs evaluated
- [ ] Dry-run on >100K SIMs → 202 with job_id, result available via job API
- [ ] Dry-run on invalid DSL → 422 with error details
- [ ] Dry-run result cached → second call within 5min returns cached result
- [ ] Dry-run does not modify any database records (verified by before/after row count)
- [ ] Sample SIMs include before/after policy evaluation results

## Effort Estimate
- Size: M
- Complexity: Medium
