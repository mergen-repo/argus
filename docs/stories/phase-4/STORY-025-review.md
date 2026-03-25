# Review Report: STORY-025 — Policy Staged Rollout (Canary)

**Reviewer:** Amil Reviewer Agent
**Date:** 2026-03-21
**Phase:** 4 — Policy & Orchestration
**Story Status:** DONE (25 new tests, 1008 total passing)

---

## 1. Next Story Impact Analysis

### STORY-026 (Steering of Roaming Engine) — No impact

SoR engine operates on operator routing (SVC-06), independent of policy rollout. STORY-025 does not introduce operator-level changes. The rollout CoA mechanism dispatches per-session but does not modify operator routing decisions. No changes needed.

### STORY-027 (RAT-Type Awareness) — No impact

RAT-type awareness enriches policy evaluation, sessions, and analytics. STORY-025's rollout selects SIMs via `SelectSIMsForStage` which queries `policy_assignments` and `sims` tables. RAT-type is not a rollout selection criterion (rollout stages are percentage-based, not RAT-filtered). No conflict or dependency.

### STORY-046 (Frontend Policy Editor) — Unblocked

STORY-025 deliverable explicitly states: "STORY-046 (Frontend Policy Editor) -- rollout controls UI can now use these APIs." The 4 new endpoints (API-096 to API-099) are available for frontend consumption. The `policy.rollout_progress` WebSocket event enables live progress display.

---

## 2. Architecture Evolution

**Rollout sub-package established:** `internal/policy/rollout/` is the third sub-package under `internal/policy/` (after `dsl/` and `dryrun/`). This confirms the pattern of functional sub-packages within service packages.

**Interface-based dependency injection for cross-service calls:** `SessionProvider` and `CoADispatcher` interfaces are defined within the rollout package to avoid import cycles between `internal/policy/rollout/` and `internal/aaa/`. Adapters are wired in `cmd/argus/main.go`. This pattern (define interface in consumer, implement adapter in main.go) is a clean modular monolith approach and should be followed for future cross-service calls.

**Async threshold pattern reused:** STORY-025 uses the same 100K SIM threshold as STORY-024 for deciding between inline execution and background job creation (`asyncThreshold = 100000`). This confirms the pattern established in DEV-066/STORY-024 as a codebase convention.

**SELECT FOR UPDATE SKIP LOCKED for concurrent SIM selection:** `SelectSIMsForStage` uses `ORDER BY random() LIMIT $N FOR UPDATE SKIP LOCKED` to select random SIMs without blocking concurrent transactions. This builds on DEV-061 (STORY-023's SELECT FOR UPDATE pattern) with the addition of SKIP LOCKED for batch operations.

**No architectural changes required to existing docs.** ARCHITECTURE.md SVC-05 already lists "staged rollout" and the project structure already shows `internal/policy/rollout/`.

---

## 3. Glossary Check

### Existing terms verified:
- "Staged Rollout" -- exists in GLOSSARY.md: "Gradual policy deployment: 1% -> 10% -> 100% of affected SIMs | Canary deployment for policies." Needs enrichment with implementation details.
- "Policy Version" -- already includes `rolling_out` and `rolled_back` states from STORY-023 review. Accurate.
- "Async Threshold" -- already references STORY-025. Accurate.

### Recommended enrichment to existing term:

| Term | Current | Recommended | Context |
|------|---------|-------------|---------|
| Staged Rollout | Gradual policy deployment: 1% -> 10% -> 100% of affected SIMs. Canary deployment for policies. | Gradual policy deployment in configurable stages (default: 1% -> 10% -> 100%). Each stage selects SIMs via `SELECT ... ORDER BY random() FOR UPDATE SKIP LOCKED`, updates `policy_assignments` (TBL-15), sends CoA for active sessions, and publishes progress via NATS `policy.rollout_progress`. Only one active rollout per policy at a time. Stages >100K SIMs execute as background jobs. Rollback reverts all migrated SIMs to previous version with mass CoA. | SVC-05, STORY-025, API-096..099, TBL-15/16, F-035 |

### New terms to add:

| Term | Definition | Context |
|------|-----------|---------|
| Policy Rollout | Record in TBL-16 tracking a staged policy deployment. Contains stage definitions (percentages), current stage index, state (`in_progress`/`completed`/`rolled_back`), migrated/total SIM counts, and error log. Created by `StartRollout()`, advanced by `AdvanceRollout()`. | SVC-05, STORY-025, TBL-16 |
| Policy Assignment | Per-SIM policy version mapping in TBL-15 (`policy_assignments`). Tracks which policy version is assigned to each SIM during and after a rollout. Enables concurrent policy versions during staged rollout (some SIMs on old version, others on new). | SVC-05, STORY-025, TBL-15, BR-4 |
| CoA Dispatch (Rollout) | Process of sending Change of Authorization messages to active sessions during a rollout stage or rollback. Executed per-SIM in batches of 1000. CoA failures are logged but do not halt the rollout (fault-tolerant). | SVC-04/SVC-05, STORY-025, F-006 |

---

## 4. FUTURE.md Relevance

**FTR-005 (Network Digital Twin)** alignment:
- FUTURE.md states: "Policy engine must support 'shadow mode' (evaluate rules without enforcement)." STORY-024's dry-run provides shadow evaluation. STORY-025's staged rollout builds on this by enabling partial enforcement (1% -> 10%) before full deployment. The progression from shadow evaluation (dry-run) to gradual enforcement (rollout) to full enforcement (100% stage) is a natural pipeline that a digital twin could leverage. No update needed.

**FTR-006 (What-If Scenarios)** alignment:
- What-if scenarios could use rollout history data (stage results, error rates, CoA success rates) to simulate "what if we rolled out policy X to Turkcell SIMs?" using historical patterns. No update needed.

No updates to FUTURE.md required.

---

## 5. New Decisions

| # | Date | Decision | Status |
|---|------|----------|--------|
| DEV-070 | 2026-03-21 | STORY-025: `SessionProvider` and `CoADispatcher` interfaces defined in `internal/policy/rollout/` to avoid import cycles with `internal/aaa/`. Adapters wired in `main.go` via `SetSessionProvider()`/`SetCoADispatcher()`. This interface-in-consumer pattern should be used for all cross-service dependencies in the modular monolith. | ACCEPTED |
| DEV-071 | 2026-03-21 | STORY-025: Gate fixed critical bug -- `ExecuteStage` passed `uuid.Nil` as tenantID to `SelectSIMsForStage`, meaning no SIMs would be selected in production (WHERE tenant_id = uuid.Nil matches nothing). Fixed with `resolveTenantID()` + `GetTenantIDForRollout()` store method (joins rollouts -> versions -> policies for tenant). | ACCEPTED |
| DEV-072 | 2026-03-21 | STORY-025: Only one active rollout per policy enforced at two levels: (1) `GetActiveRolloutForPolicy` check in service layer, (2) transaction-level COUNT check in `CreateRollout` store method. Belt-and-suspenders approach for data integrity. | ACCEPTED |
| DEV-073 | 2026-03-21 | STORY-025: CoA dispatch failure in rollout is non-fatal -- `sendCoAForSIM` logs warning and sets `coa_status = "failed"` but continues with remaining SIMs. This matches AC-14 and PRODUCT.md BR-4 (CoA enforcement on policy changes). Retry of failed CoAs is not implemented; manual re-advance or re-rollout is the recovery path. | ACCEPTED |
| DEV-074 | 2026-03-21 | STORY-025: `rolloutResponse` struct initially missing `PolicyID` and `Errors` fields despite API-099 spec requiring them. Gate fix added `GetPolicyIDForRollout()` store method and `Errors []string` field. API response completeness should be validated against spec during development, not deferred to gate. | ACCEPTED |
| PERF-026 | 2026-03-21 | STORY-025: Rollout stage SIM selection uses `ORDER BY random() LIMIT $N FOR UPDATE SKIP LOCKED`. `random()` is not cryptographically random but sufficient for canary selection. `SKIP LOCKED` prevents deadlocks when concurrent operations access the same SIMs. Indexes: `idx_policy_assignments_sim` (unique), `idx_policy_assignments_rollout`, `idx_policy_rollouts_state`. | ACCEPTED |
| PERF-027 | 2026-03-21 | STORY-025: CoA dispatch batch size = 1000 SIMs. Balances memory usage and throughput. No Redis pipeline for CoA -- each SIM's active sessions are queried individually. Acceptable because CoA is not on the hot path (admin-initiated, not per-auth-request). | ACCEPTED |

---

## 6. Cross-Document Consistency Check

| Check | Status | Notes |
|-------|--------|-------|
| PRODUCT.md F-035 (Staged rollout) | CONSISTENT | "Staged rollout -- canary 1% -> 10% -> 100%, concurrent policy versions, CoA on each stage" -- all implemented. |
| PRODUCT.md WF-2 (Policy Staged Rollout) | CONSISTENT | Workflow steps 3-7 (stage selection, dashboard progress, reviewer approval, next stage, rollback) all implemented via API-096..099 + WebSocket progress. |
| PRODUCT.md BR-4 (Policy Enforcement) | CONSISTENT | "Staged rollout: SIMs track their assigned policy version" (TBL-15), "Multiple policy versions can coexist" (per-SIM assignment), "Rollback: mass CoA to revert all SIMs" (RollbackRollout). |
| SCOPE.md L4 (Staged rollout) | CONSISTENT | "Staged rollout (canary: 1% -> 10% -> 100%) with concurrent policy versions" listed and implemented. |
| ARCHITECTURE.md SVC-05 | CONSISTENT | "staged rollout" listed as SVC-05 capability. `internal/policy/rollout/` in project structure. |
| ARCHITECTURE.md project structure | CONSISTENT | `internal/policy/rollout/` listed under SVC-05. |
| GLOSSARY.md "Staged Rollout" | NEEDS ENRICHMENT | Current definition is minimal. Should reference API endpoints, store tables, and operational details. |
| GLOSSARY.md "Policy Version" | CONSISTENT | Already includes `rolling_out` and `rolled_back` states. |
| API architecture (API-096..099) | CONSISTENT | All 4 endpoints match architecture API index: POST rollout (096), POST advance (097), POST rollback (098), GET progress (099). |
| STORY-025 story spec vs implementation | CONSISTENT | All 15 ACs pass per gate report. |
| decisions.md G-018 | CONSISTENT | "Policy versioning + rollback + dry-run simulation + staged rollout" -- staged rollout now implemented. |
| decisions.md G-030 | CONSISTENT | "Concurrent policy versions allowed during staged rollout. Each SIM tracks assigned policy version. Rollout progresses SIM-by-SIM with CoA trigger. Dashboard shows rollout progress. Rollback = mass revert + CoA." -- all implemented exactly as decided. |
| ALGORITHMS.md Section 6 (Rollout) | CONSISTENT | Algorithm description matches implementation (random selection, batch CoA, async threshold). |

**0 inconsistencies found.** 1 enrichment recommended (GLOSSARY.md staged rollout definition).

---

## 7. Document Updates

### GLOSSARY.md -- Enrich Staged Rollout definition + add 3 new terms

Current:
> Staged Rollout | Gradual policy deployment: 1% -> 10% -> 100% of affected SIMs | Canary deployment for policies

Recommended:
> Staged Rollout | Gradual policy deployment in configurable stages (default: 1% -> 10% -> 100%). Each stage selects SIMs randomly, updates policy_assignments (TBL-15), sends CoA for active sessions, publishes progress via NATS. Only one active rollout per policy. Stages >100K SIMs run as background jobs. Rollback reverts all SIMs to previous version with mass CoA. | SVC-05, STORY-025, API-096..099, TBL-15/16, F-035

New terms: Policy Rollout, Policy Assignment, CoA Dispatch (Rollout) -- see Section 3 above.

### ROUTEMAP.md -- Mark STORY-025 as DONE

STORY-025 should be updated from `[~] IN PROGRESS | Review` to `[x] DONE` with completion date 2026-03-21.

Progress counter should update from 24/55 (44%) to 25/55 (45%).

### decisions.md -- Add new decisions

Add DEV-070 through DEV-074 and PERF-026, PERF-027.

---

## Summary

| Category | Result |
|----------|--------|
| Next story impact | 0 stories require changes. STORY-046 unblocked for frontend rollout UI. |
| Architecture evolution | Interface-based DI pattern for cross-service calls confirmed. Async threshold pattern reused. SELECT FOR UPDATE SKIP LOCKED for batch operations. |
| New glossary terms | 3 new terms + 1 enrichment (Policy Rollout, Policy Assignment, CoA Dispatch, Staged Rollout update) |
| FUTURE.md | No updates needed. FTR-005/006 alignment confirmed. |
| New decisions | 7 captured (DEV-070 to DEV-074, PERF-026, PERF-027) |
| Cross-doc consistency | 0 inconsistencies, 1 enrichment recommended |
| Story updates | ROUTEMAP.md (STORY-025 DONE), GLOSSARY.md (enrich + 3 terms), decisions.md (7 new) |

---

## Project Progress

- Stories completed: 25/55 (45%)
- Phase 4 progress: 4/6 stories done (STORY-022, STORY-023, STORY-024, STORY-025)
- Next story: STORY-026 (Steering of Roaming Engine)
- Test count: 1008 passing
