# STORY-023: Policy CRUD & Versioning

## User Story
As a policy editor, I want to create, edit, and version policies, so that I can iterate on rules safely with full change history and the ability to compare versions.

## Description
Policy CRUD with immutable versioning. A policy (TBL-13) has metadata (name, description, scope). Each edit creates a new policy_version (TBL-14) with states: draft → active → archived. Only one version can be active at a time per policy. Version comparison shows diff between two versions. DSL source is stored alongside compiled JSON rule tree.

## Architecture Reference
- Services: SVC-03 (Core API), SVC-05 (Policy Engine)
- API Endpoints: API-090 to API-093
- Database Tables: TBL-13 (policies), TBL-14 (policy_versions)
- Source: docs/architecture/api/_index.md (Policies section)

## Screen Reference
- SCR-060: Policy List — policies with active version, assignment count, status
- SCR-062: Policy Editor — version tabs, DSL editor, version diff view

## Acceptance Criteria
- [ ] POST /api/v1/policies creates policy with initial draft version
- [ ] GET /api/v1/policies lists policies with active version summary, SIM count, last modified
- [ ] GET /api/v1/policies/:id returns policy with all versions (draft, active, archived)
- [ ] POST /api/v1/policies/:id/versions creates new draft version (clones from active or specified version)
- [ ] Draft version can be edited (DSL source updated, recompiled)
- [ ] Version state machine: draft → active (activation archives previous active)
- [ ] Only one active version per policy at any time
- [ ] Archived versions are read-only but viewable
- [ ] Version comparison: diff two versions showing DSL source changes + affected SIM count delta
- [ ] Policy deletion: soft-delete, only if no SIMs assigned to any version
- [ ] Compiled rules (JSON) stored in TBL-14.compiled_rules alongside DSL source
- [ ] Validation: DSL source must parse and compile without errors before version can be activated
- [ ] Audit log entry for every policy/version create, update, activate, archive

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-090 | GET | /api/v1/policies | `?cursor&limit&status&q` | `[{id,name,description,active_version,sim_count,updated_at}]` | JWT(policy_editor+) | 200 |
| API-091 | POST | /api/v1/policies | `{name,description,scope,dsl_source}` | `{id,name,versions:[{id,version:1,state:"draft"}]}` | JWT(policy_editor+) | 201, 400 |
| API-092 | GET | /api/v1/policies/:id | — | `{id,name,description,scope,versions:[...],active_version_id}` | JWT(policy_editor+) | 200, 404 |
| API-093 | POST | /api/v1/policies/:id/versions | `{dsl_source,clone_from_version_id?}` | `{id,version,state:"draft",dsl_source,compiled_rules}` | JWT(policy_editor+) | 201, 400, 422 |

## Dependencies
- Blocked by: STORY-022 (policy DSL parser), STORY-002 (DB schema)
- Blocks: STORY-024 (dry-run), STORY-025 (rollout)

## Test Scenarios
- [ ] Create policy → policy created with v1 draft
- [ ] Create new version → v2 draft created, v1 unchanged
- [ ] Activate draft → version state=active, previous active → archived
- [ ] Activate with DSL syntax error → 422 INVALID_DSL
- [ ] Get policy → all versions returned in order
- [ ] Delete policy with assigned SIMs → 422 POLICY_IN_USE
- [ ] Version comparison → DSL diff returned
- [ ] List policies with filter by status → correct results

## Effort Estimate
- Size: L
- Complexity: Medium
