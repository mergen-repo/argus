# Deliverable: STORY-023 — Policy CRUD & Versioning

## Summary

Implemented Policy CRUD with immutable versioning. Policies (TBL-13) serve as containers with metadata, while policy versions (TBL-14) hold DSL source and compiled rules. Version state machine: draft → active → archived. Only one active version per policy at any time.

## What Was Built

### Store Layer (internal/store/policy.go)
- PolicyStore with full CRUD for policies and versions
- Create, GetByID, List (cursor pagination, state filter, search), Update, SoftDelete
- CreateVersion (auto-increment), GetVersionByID, GetVersionsByPolicyID
- UpdateVersion (draft-only check), ActivateVersion (transactional)
- HasAssignedSIMs (EXISTS-based), CountAssignedSIMs
- GetActiveVersionSummary
- All queries scoped by tenant_id
- 5 sentinel errors for domain validation

### API Handlers (internal/api/policy/handler.go)
- 9 HTTP endpoints covering API-090 through API-095 + version management
- List policies (cursor pagination, status filter, search)
- Create policy with initial draft version (DSL compilation via dsl.CompileSource)
- Get policy with all versions
- Update policy metadata
- Delete policy (soft, only if no assigned SIMs)
- Create new version (clone from active or specified version)
- Activate version (validates DSL, archives previous active)
- Update draft version (recompile DSL)
- Diff two versions (DSL source comparison)
- Standard envelope responses, audit logging

### Routing & Wiring
- Routes registered under RequireRole("policy_editor") in gateway/router.go
- PolicyStore and PolicyHandler initialized in cmd/argus/main.go

### Tests
- 5 store tests + 23 handler tests = 28 story-specific tests
- Full suite: 613 tests passing, zero regressions

## Architecture References Fulfilled
- API-090: GET /api/v1/policies (list with pagination)
- API-091: POST /api/v1/policies (create with initial draft)
- API-092: GET /api/v1/policies/:id (get with versions)
- API-093: POST /api/v1/policies/:id/versions (create version)
- TBL-13: policies table operations
- TBL-14: policy_versions table operations
- SVC-05: Policy Engine store and API layer

## Gate Fixes Applied
1. HasAssignedSIMs using EXISTS (not COUNT) for deletion check
2. Added sim_count field to policy list response per API-090 spec

## Files Changed
```
internal/store/policy.go           (new)
internal/store/policy_test.go      (new)
internal/api/policy/handler.go     (new)
internal/api/policy/handler_test.go (new)
internal/gateway/router.go         (modified)
cmd/argus/main.go                  (modified)
```

## Dependencies Unblocked
- STORY-024 (Policy Dry-Run Simulation) — can now load policies and versions
- STORY-025 (Policy Staged Rollout) — can now activate/manage policy versions
