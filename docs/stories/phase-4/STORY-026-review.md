# Review: STORY-026 — Steering of Roaming (SoR) Engine

**Reviewer:** Amil Reviewer Agent
**Date:** 2026-03-21
**Phase:** 4
**Story Status:** DONE (gate PASS, 16 tests)

---

## 1. Next Story Impact (STORY-027)

STORY-027 (RAT-Type Awareness) lists STORY-026 as a dependency. Key integration points verified:

| STORY-027 AC | SoR Readiness | Notes |
|---|---|---|
| SoR engine: filter operators by RAT capability before cost/priority ranking | READY | `engine.go:filterByRAT` already filters by RAT, `sortCandidates` uses `bestRATRank`. STORY-027 can call `Evaluate()` with `RequestedRAT` directly. |
| Operator capability map: TBL-05.supported_rat_types | READY | Migration adds `supported_rat_types TEXT[]` to operator_grants; TBL-05 already has `supported_rat_types`. |
| RAT enum normalization | NEEDS ATTENTION | SoR uses `DefaultRATPreferenceOrder = ["5G","4G","3G","2G","NB-IoT","LTE-M"]` but DSL/sessions use `nb_iot, lte_m, lte, nr_5g`. STORY-027 must normalize. Post-STORY-022 note already flags this. No action needed now. |

**Recommendation for STORY-027:** Add a `MapRATToSoR(protocolRAT string) string` helper or centralize RAT enum constants shared between SoR and DSL. This avoids two parallel enum definitions.

---

## 2. Architecture Evolution

### 2a. ARCHITECTURE.md — Caching Strategy Table

**GAP FOUND:** The caching strategy table (line 296-306) does not include the SoR cache entry.

**Action Required:** Add row to caching table:

```
| SoR decision (per-SIM) | Redis | 1hr (configurable) | NATS on operator health change |
```

### 2b. ARCHITECTURE.md — Project Structure

VERIFIED: `internal/operator/sor/` already listed under SVC-06 Operator Router in the project structure tree (line 145). No change needed.

### 2c. ARCHITECTURE.md — Extension Points

VERIFIED: Auto-SoR extension point already documented: "SoR engine has pluggable strategy interface: RuleBased (v1), AIBased (future)." (line 313). Implementation uses `GrantProvider` and `CircuitBreakerChecker` interfaces, consistent with this provision.

### 2d. Architecture DB Schema Docs

**GAP FOUND:** `docs/architecture/db/operator.md` TBL-06 schema does NOT include the three new columns added by STORY-026 migration: `sor_priority`, `cost_per_mb`, `supported_rat_types`.

**Action Required:** Update TBL-06 schema in `docs/architecture/db/operator.md` to include:

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| sor_priority | INTEGER | NOT NULL, DEFAULT 100 | SoR routing priority (lower = preferred) |
| cost_per_mb | DECIMAL(10,6) | | Cost per MB for this operator-tenant grant |
| supported_rat_types | TEXT[] | NOT NULL, DEFAULT '{}' | RAT types supported under this grant |

### 2e. Architecture DB Schema Docs — TBL-17

**GAP FOUND:** `docs/architecture/db/aaa-analytics.md` TBL-17 sessions schema does NOT include the `sor_decision JSONB` column added by STORY-026 migration.

**Action Required:** Add `sor_decision` column to TBL-17 schema doc:

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| sor_decision | JSONB | | SoR engine decision record (operator, reason, fallbacks) |

---

## 3. GLOSSARY.md Updates

### Terms to Add

| Term | Definition | Context |
|------|-----------|---------|
| SoR Decision | Output of the Steering of Roaming evaluation: primary operator, fallback list, selection reason (IMSI prefix match, cost optimized, RAT preference, manual lock, default), and evaluation metadata. Cached in Redis per SIM with configurable TTL (default 1h). | SVC-06, STORY-026, `sor:result:{tenant_id}:{imsi}` |
| SoR Priority | Integer field on operator_grants (TBL-06) controlling operator preference order in SoR evaluation. Lower value = higher priority. Default 100. | SVC-06, STORY-026, TBL-06 |
| Operator Lock | Per-SIM override stored in `sims.metadata.operator_lock` (UUID) that bypasses SoR evaluation and forces routing to a specific operator. | SVC-06, STORY-026 |
| IMSI Prefix Routing | SoR routing method that matches the first 5-6 digits of an IMSI (MCC+MNC) against operator definitions to determine preferred operator(s). | SVC-06, STORY-026, F-021 |
| Cost-Based Selection | SoR tie-breaking strategy that selects the operator with lowest `cost_per_mb` when multiple operators have equal priority and RAT rank. | SVC-06, STORY-026, F-038 |

### Existing Terms — No Change Needed

- **SoR (Steering of Roaming)** — already in GLOSSARY.md Network Terms section: "Directing SIM to preferred network when multiple available". Sufficient as-is; detailed sub-terms above cover implementation specifics.
- **Circuit Breaker** — already defined, SoR integration (filtering by circuit state) is consistent.

---

## 4. FUTURE.md Relevance

**FTR-003 (Auto-SoR / Autonomous Steering)** is directly relevant:

> "AI replaces manual SoR rules. Real-time operator selection based on coverage + cost + latency optimization per device location."

STORY-026 implementation supports this future path:
- `GrantProvider` interface enables swapping the data source (rule-based -> AI model)
- `CandidateOperator` struct carries all selection criteria
- `SoRConfig.RATPreferenceOrder` is configurable (can be replaced by ML output)
- `SoRDecision` result struct is generic enough for AI-generated decisions

**Architecture Implications note** in FUTURE.md states: "SoR engine (L3) must support pluggable decision strategies (rule-based -> AI-based)." This is achievable: extract `Evaluate()` logic into a `Strategy` interface, register `RuleBasedStrategy` (current) as default.

**No FUTURE.md changes needed.** Current text accurately describes the future direction.

---

## 5. Decisions (decisions.md)

File does not exist (`docs/decisions.md` not found). No action needed unless the project adopts a decisions log. Key decisions from STORY-026 worth recording if created:

- **DEC-026-01:** SoR uses lazy cache invalidation (delete cache on operator health event, re-evaluate on next auth) rather than eager re-evaluation. Rationale: avoids thundering herd on operator failover.
- **DEC-026-02:** SoR sorting order is priority ASC -> RAT rank -> cost_per_mb ASC (`sort.SliceStable`). This is deterministic and auditable.
- **DEC-026-03:** SoR fields placed on `operator_grants` (not `operators`) to allow per-tenant cost/priority configuration for the same operator.

---

## 6. Cross-Document Consistency

| Document | Check | Status | Detail |
|----------|-------|--------|--------|
| SCOPE.md | F-021 (IMSI routing), F-022 (SoR), F-023 (failover) | OK | All covered by STORY-026 implementation |
| PRODUCT.md | F-022 "SoR engine with RAT-type preference" | OK | RAT filtering and preference ordering implemented |
| PRODUCT.md | BR-5 "Operator Failover" | OK | SoR integrates with circuit breaker per BR-5 |
| PRODUCT.md | WF-3 "Operator Failover" — "fallback-to-next: route to..." | OK | SoR provides fallback operator list consumed by failover engine |
| ARCHITECTURE.md | SVC-06 description | OK | "IMSI routing, SoR engine" listed |
| ARCHITECTURE.md | Project structure | OK | `sor/` listed under `internal/operator/` |
| ARCHITECTURE.md | Caching table | GAP | Missing SoR cache entry (see section 2a) |
| ARCHITECTURE.md | Extension Points | OK | Auto-SoR provision documented |
| ARCHITECTURE.md | AAA Hot Path | OK | "Operator route (in-memory IMSI prefix table, ~0.01ms)" — SoR cache adds Redis hop but only on cache miss |
| db/_index.md | TBL-06 description | OK | Listed but schema doc needs column update |
| db/operator.md | TBL-06 columns | GAP | Missing sor_priority, cost_per_mb, supported_rat_types (see section 2d) |
| db/aaa-analytics.md | TBL-17 columns | GAP | Missing sor_decision JSONB column (see section 2e) |
| services/_index.md | SVC-06 | OK | "SoR engine" listed in responsibility |
| ROUTEMAP.md | STORY-026 status | OK | Marked `[x] DONE`, date 2026-03-21 |
| ROUTEMAP.md | STORY-027 deps | OK | Lists STORY-026 as dependency |
| GLOSSARY.md | SoR term | OK | Present but implementation sub-terms missing (see section 3) |
| FUTURE.md | FTR-003 Auto-SoR | OK | Consistent with pluggable interface design |

---

## 7. Story File Updates

| File | Status | Notes |
|------|--------|-------|
| STORY-026-sor-engine.md | OK | All 11 ACs traceable to implementation |
| STORY-026-gate.md | OK | All passes clear, no fixes needed |
| STORY-026-deliverable.md | OK | File list matches actual implementation |
| STORY-026-plan.md | OK | Plan exists |
| STORY-027 post-note | RECOMMENDED | Add post-STORY-026 note about SoR integration: `SoREngine.Evaluate()` entry point, `CandidateOperator.SupportedRATs` field, and RAT enum normalization consideration |

---

## 8. Action Items Summary

| # | Priority | Action | Target File |
|---|----------|--------|-------------|
| 1 | HIGH | Add SoR cache row to Caching Strategy table | `docs/ARCHITECTURE.md` (line ~306) |
| 2 | HIGH | Add sor_priority, cost_per_mb, supported_rat_types columns to TBL-06 | `docs/architecture/db/operator.md` |
| 3 | HIGH | Add sor_decision JSONB column to TBL-17 | `docs/architecture/db/aaa-analytics.md` |
| 4 | MEDIUM | Add 5 new glossary terms (SoR Decision, SoR Priority, Operator Lock, IMSI Prefix Routing, Cost-Based Selection) | `docs/GLOSSARY.md` |
| 5 | LOW | Add post-STORY-026 note to STORY-027 about SoR entry point and RAT enum | `docs/stories/phase-4/STORY-027-rat-awareness.md` |

---

## 9. Verdict

**STORY-026 is well-implemented and architecturally sound.** The SoR engine cleanly separates concerns via interfaces (`GrantProvider`, `CircuitBreakerChecker`), integrates properly with the existing circuit breaker and NATS event system, and leaves clear extension points for FTR-003 (Auto-SoR). All 11 acceptance criteria are met with 16 tests covering edge cases.

**3 documentation gaps identified** (caching table, TBL-06 schema, TBL-17 schema) — all are schema doc updates that should be applied before STORY-027 starts to maintain architecture doc accuracy.
