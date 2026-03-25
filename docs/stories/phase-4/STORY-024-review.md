# Review Report: STORY-024 — Policy Dry-Run Simulation

**Reviewer:** Amil Reviewer Agent
**Date:** 2026-03-21
**Phase:** 4 — Policy & Orchestration
**Story Status:** DONE (26 new tests, 623 total passing)

---

## 1. Next Story Impact Analysis

### STORY-025 (Policy Staged Rollout) — 1 update needed

**Assumptions validated:**
- STORY-025 depends on dry-run data for stage planning (per STORY-024 deliverable). The `DryRunResult` struct provides `TotalAffected`, `ByOperator`, `ByAPN`, and `ByRATType` breakdowns that can inform rollout stage sizing (e.g., 1% of 2.3M = 23K SIMs).
- The dry-run `affected_sim_count` is persisted on TBL-14 `policy_versions` via `UpdateDryRunResult()`. STORY-025's rollout handler can read this to pre-calculate stage sizes without re-running the dry-run.
- The `SIMFleetFilters` and `CountByFilters` infrastructure in `store/sim.go` (added by STORY-024) can be reused by STORY-025 for selecting SIMs per rollout stage.

**Impact:**
- STORY-025 should consume the `SIMFleetFilters` builder and `FetchSample`/`AggregateByOperator` queries from `store/sim.go` for stage SIM selection — no need to rebuild fleet query infrastructure.
- The `dryrun.Service.buildFiltersFromMatch()` method extracts MATCH conditions into query filters. STORY-025 can call this (or reuse the same logic) when selecting SIMs for each rollout stage.
- **Note needed:** Add post-STORY-024 integration note to STORY-025 about reusable fleet query infrastructure.

### STORY-026 (Steering of Roaming Engine) — No impact

SoR engine operates on operator routing (SVC-06), independent of policy dry-run. No changes needed.

### STORY-027 (RAT-Type Awareness) — No impact

RAT-type awareness extends policy evaluation conditions. STORY-024's dry-run already aggregates by RAT type (`ByRATType` breakdown) using the existing `rat_type` field on sessions/SIMs. STORY-027 may enrich the data quality (normalized RAT enum) but does not require retroactive changes to dry-run.

---

## 2. Architecture Evolution

**New sub-package established:** `internal/policy/dryrun/` is the first sub-package under `internal/policy/` beyond `dsl/`. This establishes the pattern of functional sub-packages within a service package (dsl, dryrun, rollout for STORY-025).

**Sync/async split pattern:** The handler implements a count-based threshold (100K SIMs) to decide between synchronous (200) and asynchronous (202 + job) execution. This is a new pattern in the codebase — previous async operations (bulk import) were always async. STORY-025 should follow this pattern for stage execution (small stages inline, large stages as jobs).

**Store fleet query infrastructure:** `SIMFleetFilters` with dynamic WHERE clause builder in `store/sim.go` is a reusable pattern for any feature that queries SIMs by policy MATCH criteria. STORY-025 (rollout stage selection) and STORY-030 (bulk operations) can leverage this.

**No architectural changes required to existing docs.** ARCHITECTURE.md SVC-05 description already lists "dry-run simulation" as a capability.

---

## 3. Glossary Check

### Existing terms verified:
- "Dry-Run" -- already in GLOSSARY.md: "Policy simulation without applying changes | Shows impact before commit." Still accurate but could be enriched.

### Recommended update to existing term:

| Term | Current | Recommended | Context |
|------|---------|-------------|---------|
| Dry-Run | Policy simulation without applying changes. Shows impact before commit. | Policy simulation that evaluates a version against the SIM fleet without applying changes. Returns `total_affected_sims`, breakdowns by operator/APN/RAT, `behavioral_changes` (QoS upgrade/downgrade, charging, access), and `sample_sims` with before/after comparison. Sync for <100K SIMs, async job for >100K. Cached 5min in Redis. | SVC-05, STORY-024, API-094, F-034 |

### New terms to add:

| Term | Definition | Context |
|------|-----------|---------|
| Behavioral Change (Dry-Run) | Difference in policy evaluation outcome between current and candidate policy version for a SIM. Types: QoS upgrade/downgrade (bandwidth, priority), charging model change (prepaid/postpaid), access change (allow/deny). Detected by `DetectBehavioralChanges()`. | SVC-05, STORY-024 |
| Async Threshold | SIM count threshold (100K) determining whether a dry-run executes synchronously (HTTP 200) or asynchronously as a background job (HTTP 202 with job_id). Same pattern applies to staged rollout stage execution. | SVC-05/SVC-09, STORY-024, STORY-025 |
| SIM Fleet Filters | Dynamic query filter set derived from policy MATCH block conditions. Includes operator_id, APN names, RAT types, and optional segment_id. Used by dry-run and rollout to scope SIM queries. Built by `buildFiltersFromMatch()`. | SVC-05, STORY-024, store/sim.go |

---

## 4. FUTURE.md Relevance

**FTR-005 (Network Digital Twin)** alignment strengthened:
- FUTURE.md states: "Policy engine must support 'shadow mode' (evaluate rules without enforcement)." STORY-024's dry-run is exactly this shadow evaluation capability. The existing architecture provision is now partially realized. No update needed -- the provision already covers this.

**FTR-006 (What-If Scenarios)** alignment:
- "Interactive scenario builder" could leverage the dry-run infrastructure for policy-level what-if analysis. The `DryRunResult` with behavioral changes is the foundation for this future feature.

No updates to FUTURE.md needed.

---

## 5. New Decisions

| # | Date | Decision | Status |
|---|------|----------|--------|
| DEV-066 | 2026-03-21 | STORY-024: Dry-run `Execute()` writes to `policy_versions.dry_run_result` and `affected_sim_count` columns despite being labeled "read-only." AC-6 intent is "no side effects on SIM fleet" -- updating version metadata is acceptable. Clarified in gate observation #3. | ACCEPTED |
| DEV-067 | 2026-03-21 | STORY-024: `CountMatchingSIMs` does not check DSL error list when `CompileSource` returns nil compiled policy with nil error (DSL has severity=error diagnostics). `Execute()` catches this correctly downstream. Non-blocking observation per gate. | ACCEPTED |
| DEV-068 | 2026-03-21 | STORY-024: Per-sample SIM operator/APN name resolution uses individual DB queries (max 10 queries for 10 sample SIMs). Could be batched but acceptable at scale=10. Future optimization if sample size increases. | ACCEPTED |
| DEV-069 | 2026-03-21 | STORY-024: `bus.PublishRaw()` added for pre-serialized NATS messages (dry-run job progress). Complements existing `bus.Publish()` which JSON-marshals. Minimal addition (7 lines). | ACCEPTED |
| PERF-024 | 2026-03-21 | STORY-024: Dry-run Redis cache with 5min TTL, keyed by `dryrun:{version_id}:{segment_id_or_all}`. Checked before DB aggregation queries. Cache invalidation on SIM fleet changes is TTL-based only (no explicit invalidation via NATS). Acceptable because dry-run is an approximation and 5min staleness is tolerable. | ACCEPTED |
| PERF-025 | 2026-03-21 | STORY-024: Fleet aggregation queries use GROUP BY (not per-SIM queries) for operator/APN/RAT breakdowns. Existing `idx_sims_tenant_state` index applies. N+1 avoided by design. | ACCEPTED |

---

## 6. Cross-Document Consistency Check

| Check | Status | Notes |
|-------|--------|-------|
| PRODUCT.md F-034 (Dry-run simulation) | CONSISTENT | "Dry-run simulation -- 'this rule affects N SIMs' preview" implemented with richer data (breakdowns, behavioral changes, samples). |
| PRODUCT.md WF-2 (Policy Staged Rollout) | CONSISTENT | WF-2 step 2: "Dry-run: 'This policy affects 2.3M SIMs across 4 APNs'" -- dry-run returns total_affected + by_apn breakdown. |
| SCOPE.md L4 (Dry-run simulation) | CONSISTENT | "Dry-run simulation ('affects N SIMs')" listed and implemented. |
| ARCHITECTURE.md SVC-05 | CONSISTENT | "dry-run simulation" listed as SVC-05 capability. |
| ARCHITECTURE.md Caching Strategy | CONSISTENT | Dry-run cached in Redis with 5min TTL. Not listed in caching strategy table but consistent with pattern. |
| GLOSSARY.md "Dry-Run" | NEEDS ENRICHMENT | Current definition is minimal ("simulation without applying changes"). Should reference actual output fields and sync/async behavior. |
| API architecture (API-094) | CONSISTENT | `POST /api/v1/policy-versions/:id/dry-run` matches architecture API index entry exactly. |
| STORY-024 story spec vs implementation | CONSISTENT | All 10 ACs pass per gate report. |
| STORY-025 dependency | CONSISTENT | STORY-024 deliverable confirms "STORY-025 can use dry-run data for stage planning." |
| decisions.md G-018 | CONSISTENT | "Policy versioning + rollback + dry-run simulation + staged rollout" -- dry-run now implemented. |

**0 inconsistencies found.** 1 enrichment recommended (GLOSSARY.md dry-run definition).

---

## 7. Document Updates

### GLOSSARY.md -- Enrich Dry-Run definition

Current:
> Dry-Run | Policy simulation without applying changes | Shows impact before commit

Recommended:
> Dry-Run | Policy simulation that evaluates a version against the SIM fleet without applying changes. Returns total_affected_sims, breakdowns by operator/APN/RAT, behavioral_changes (QoS/charging/access), and sample_sims with before/after. Sync (<100K SIMs) or async job (>100K). Cached 5min in Redis. | SVC-05, STORY-024, API-094, F-034

### ROUTEMAP.md -- Mark STORY-024 as DONE

STORY-024 should be updated from `[~] IN PROGRESS | Review` to `[x] DONE` with completion date 2026-03-21.

### STORY-025 -- Add post-STORY-024 integration note

Add note about reusable fleet query infrastructure (SIMFleetFilters, buildFiltersFromMatch, aggregation queries) and dry-run result availability on policy_versions table.

### decisions.md -- Add new decisions

Add DEV-066 through DEV-069 and PERF-024, PERF-025.

---

## Summary

| Category | Result |
|----------|--------|
| Next story impact | 1 story affected (STORY-025), reusable fleet query infrastructure note |
| Architecture evolution | New sub-package pattern (`policy/dryrun/`), sync/async threshold pattern established |
| New glossary terms | 3 new terms + 1 enrichment (Behavioral Change, Async Threshold, SIM Fleet Filters, Dry-Run update) |
| FUTURE.md | No updates needed. FTR-005/006 alignment confirmed. |
| New decisions | 6 captured (DEV-066 to DEV-069, PERF-024, PERF-025) |
| Cross-doc consistency | 0 inconsistencies, 1 enrichment recommended |
| Story updates | ROUTEMAP.md (STORY-024 DONE), GLOSSARY.md (enrich), STORY-025 (integration note), decisions.md (6 new) |

---

## Project Progress

- Stories completed: 24/55 (44%)
- Phase 4 progress: 3/6 stories done (STORY-022, STORY-023, STORY-024)
- Next story: STORY-025 (Policy Staged Rollout)
- Test count: 623 passing
