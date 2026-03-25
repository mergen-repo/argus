# Review Report: STORY-023 — Policy CRUD & Versioning

**Reviewer:** Amil Reviewer Agent
**Date:** 2026-03-21
**Phase:** 4 — Policy & Orchestration
**Story Status:** DONE (28 tests, 613 total passing)

---

## 1. Next Story Impact Analysis

### STORY-024 (Policy Dry-Run Simulation) — 1 update needed

**Assumptions validated:**
- STORY-024 assumes it can load policy versions and their compiled rules. STORY-023 provides `GetVersionByID()` and `GetVersionsByPolicyID()` which return `PolicyVersion` with `CompiledRules json.RawMessage` and `DSLContent`. Confirmed.
- STORY-024 needs to evaluate compiled rules against SIM fleet. The `dsl.EvaluateCompiled()` function from STORY-022 + compiled rules storage from STORY-023 provide the full chain.

**Impact:**
- STORY-024 spec references `POST /api/v1/policy-versions/:id/dry-run`. STORY-023 already implemented `POST /api/v1/policy-versions/:id/activate` on the same path pattern (`policy-versions/{id}`). Dry-run handler can follow the same routing pattern — no conflict.
- STORY-024 references TBL-14 `dry_run_result JSONB` and `affected_sim_count` columns. STORY-023's `PolicyVersion` struct already includes both fields (`DryRunResult json.RawMessage`, `AffectedSIMCount *int`). The store layer is ready.
- **Note:** STORY-024 should add a `StoreDryRunResult(ctx, versionID, result, simCount)` method to `PolicyStore` rather than building a separate store.

### STORY-025 (Policy Staged Rollout) — 1 update needed

**Assumptions validated:**
- STORY-025 assumes version activation, which STORY-023 implements via `ActivateVersion()` with transactional supersede logic.
- STORY-025 references TBL-15 (policy_assignments) and TBL-16 (policy_rollouts) — these are separate tables not touched by STORY-023, as expected.
- STORY-025's `POST /api/v1/policy-versions/:id/rollout` will need to coexist with `POST /api/v1/policy-versions/:id/activate`. STORY-023 registered activate under `RequireRole("policy_editor")` — rollout should use the same role group.

**Impact:**
- STORY-025 AC states `POST /api/v1/policy-versions/:id/activate activates version immediately (100% rollout)`. STORY-023 already implements this endpoint. STORY-025 may need to extend (not replace) the existing handler to add rollout-awareness. **Update needed:** Add note to STORY-025 that `ActivateVersion` handler already exists and should be extended, not recreated.
- The version state machine in STORY-023 uses `draft → active → superseded → archived`. STORY-025 will need a `rolling_out` state. The `PolicyVersion.State` field is a `VARCHAR(20)` — sufficient. But the `validPolicyStates` map in handler.go only includes `active/disabled/archived` for policy-level states. Version states are validated in the store layer. STORY-025 should add `rolling_out` as a valid version state.

### STORY-026 (SoR Engine) — No impact

SoR engine operates on operator routing (SVC-06), independent of policy CRUD. No changes needed.

### STORY-027 (RAT-Type Awareness) — No impact

RAT-type awareness extends policy evaluation (WHEN conditions), which was implemented in STORY-022 DSL package. STORY-023's CRUD layer stores/retrieves DSL content without interpreting RAT conditions. No changes needed.

---

## 2. Architecture Evolution

**No architectural changes required.** STORY-023 followed established patterns precisely:
- Store pattern matches `operator.go` (struct with `*pgxpool.Pool`, sentinel errors, scan helpers)
- Handler pattern matches `operator/handler.go` (request/response types, validation, audit logging)
- Route registration pattern matches existing groups in `router.go`
- DSL integration uses `dsl.CompileSource()` / `dsl.Validate()` as designed

**Observation:** The `PolicyStore` is the first store to use `SELECT FOR UPDATE` within a transaction (in `ActivateVersion`). This establishes a precedent for concurrent-safe state transitions that STORY-025 (rollout) and STORY-030 (bulk operations) should follow.

---

## 3. Glossary Check

### New terms to add:

| Term | Definition | Context |
|------|-----------|---------|
| Policy Scope | Classification of policy applicability: `global` (tenant-wide), `operator` (per-operator), `apn` (per-APN), `sim` (per-SIM). Determines evaluation precedence (BR-4). | SVC-05, STORY-023, TBL-13 |
| Version State Machine (Policy) | State transitions for policy versions: `draft` (editable) -> `active` (in use, one per policy) -> `superseded` (replaced by newer active version) -> `archived` (manually deactivated). Only draft versions can be edited or activated. | SVC-05, STORY-023, TBL-14 |
| Policy Soft-Delete | Setting policy state to `archived` instead of physical deletion. Blocked if any SIMs are assigned to the policy's versions. Archived policies are read-only. | SVC-05, STORY-023 |

### Existing terms verified:
- "Policy Version" — already in GLOSSARY.md, definition still accurate
- "Policy DSL" — already in GLOSSARY.md, definition still accurate
- "CompiledPolicy" — already in GLOSSARY.md from STORY-022, still accurate

---

## 4. FUTURE.md Relevance

**No new future opportunities identified.** STORY-023 is an infrastructure story (CRUD + versioning). The existing FUTURE.md entries remain relevant:
- FTR-005 (Network Digital Twin) — "Policy engine supports shadow evaluation" — STORY-023's version system enables storing shadow versions (draft) alongside active versions, which aligns with this future capability.
- FTR-006 (What-If Scenarios) — Policy version comparison (diff endpoint) could be extended for what-if analysis in the future.

No updates to FUTURE.md needed.

---

## 5. New Decisions

| # | Date | Decision | Status |
|---|------|----------|--------|
| DEV-061 | 2026-03-21 | STORY-023: PolicyStore uses `SELECT FOR UPDATE` in `ActivateVersion` transaction to prevent race conditions on concurrent activations. This is the first use of row-level locking in a store method. Pattern should be followed for STORY-025 (rollout state transitions) and any future concurrent state machine operations. | ACCEPTED |
| DEV-062 | 2026-03-21 | STORY-023: `HasAssignedSIMs` uses `EXISTS` query instead of `COUNT(*)` for deletion check. Provides early termination on large datasets. Same pattern should be used wherever a "has any?" check is needed (not "how many?"). | ACCEPTED |
| DEV-063 | 2026-03-21 | STORY-023: Policy list endpoint returns `sim_count: 0` as placeholder. Efficient population requires LEFT JOIN to policy_assignments (TBL-15), which doesn't exist yet. Will be populated when STORY-025 implements policy assignments. | ACCEPTED |
| DEV-064 | 2026-03-21 | STORY-023: `active_version` summary in list response not populated to avoid N+1 queries. Recommended fix: LEFT JOIN in List query when performance becomes a concern. Deferred as functional requirement is met. | ACCEPTED |
| DEV-065 | 2026-03-21 | STORY-023: Version state uses `superseded` (not `archived`) when a new version is activated. `archived` is reserved for manual deactivation. This distinguishes between "replaced by newer version" and "deliberately disabled". | ACCEPTED |
| PERF-020 | 2026-03-21 | STORY-023: Policy list and detail endpoints not cached in Redis — admin/policy_editor endpoint, state changes happen on version activation. Same rationale as PERF-001/003/005. Policy compiled rules caching (for AAA hot path) deferred to STORY-024/025 integration. | ACCEPTED |

---

## 6. Cross-Document Consistency Check

| Check | Status | Notes |
|-------|--------|-------|
| PRODUCT.md F-033 (Policy versioning with rollback) | CONSISTENT | Versioning implemented. Rollback is STORY-025 scope. |
| PRODUCT.md F-032 (Policy DSL / rule engine) | CONSISTENT | DSL compilation integrated into create/update/activate flows. |
| PRODUCT.md BR-4 (Policy evaluation order) | CONSISTENT | Scope field (global/operator/apn/sim) stored per policy. Evaluation order implementation deferred to runtime (STORY-024/025). |
| PRODUCT.md Data Model (Policy → PolicyVersion) | CONSISTENT | 1:N relationship implemented correctly. |
| ARCHITECTURE.md RBAC Matrix (policy_editor manages policies) | CONSISTENT | Routes use `RequireRole("policy_editor")`. |
| ARCHITECTURE.md Caching Strategy (Policy compiled rules: Redis, 10min) | CONSISTENT | Caching not yet implemented but compiled_rules stored in DB ready for cache layer. |
| SCOPE.md L4 Policy & Charging | CONSISTENT | "Policy versioning + rollback" listed, versioning implemented. |
| GLOSSARY.md PolicyVersion | CONSISTENT | Definition matches implementation. |
| GLOSSARY.md PolicyVersion states | NEEDS UPDATE | GLOSSARY says `DRAFT/ACTIVE/ROLLING_OUT/ROLLED_BACK`. Implementation uses `draft/active/superseded/archived`. `ROLLING_OUT` and `ROLLED_BACK` are STORY-025 scope. `superseded` is new — not in GLOSSARY. |
| DSL_GRAMMAR.md | CONSISTENT | `dsl.CompileSource()` and `dsl.Validate()` used correctly per grammar spec. |

**1 inconsistency found:** GLOSSARY.md `PolicyVersion` states definition lists `ROLLING_OUT/ROLLED_BACK` but not `superseded`. The `superseded` state was introduced by STORY-023 for "replaced by newer activation." GLOSSARY update recommended.

---

## 7. Document Updates

### GLOSSARY.md — Update PolicyVersion definition

Current:
> Policy Version: Immutable snapshot of a policy rule set | Versioned for rollback/staged rollout

Recommended update to add `superseded` state and reference STORY-023:
> Policy Version: Immutable snapshot of a policy rule set. States: `draft` (editable), `active` (in use), `superseded` (replaced by newer active version), `rolling_out` (staged rollout in progress, STORY-025), `rolled_back` (reverted, STORY-025), `archived` (manually deactivated). Only one active version per policy. | SVC-05, STORY-023

### ROUTEMAP.md — Mark STORY-023 as DONE

STORY-023 should be updated from `[~] IN PROGRESS` to `[x] DONE` with completion date.

### STORY-025 — Add integration note

Add note about existing `ActivateVersion` handler that should be extended for rollout-awareness.

---

## Summary

| Category | Result |
|----------|--------|
| Next story impact | 2 stories affected (STORY-024, STORY-025), minor integration notes |
| Architecture evolution | No changes. SELECT FOR UPDATE pattern established as precedent. |
| New glossary terms | 3 recommended additions (Policy Scope, Version State Machine, Policy Soft-Delete) |
| FUTURE.md | No updates needed |
| New decisions | 6 captured (DEV-061 to DEV-065, PERF-020) |
| Cross-doc consistency | 1 inconsistency (GLOSSARY.md PolicyVersion states missing `superseded`) |
| Story updates | ROUTEMAP.md (STORY-023 DONE), GLOSSARY.md (states), STORY-025 (integration note) |

---

## Project Progress

- Stories completed: 23/55 (42%)
- Phase 4 progress: 2/6 stories done (STORY-022, STORY-023)
- Next story: STORY-024 (Policy Dry-Run Simulation)
- Test count: 613 passing
