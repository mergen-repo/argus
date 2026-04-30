# FIX-230: Rollout DSL Match Integration — `SelectSIMsForStage` DSL Predicate Filter + `total_sims` Accurate Count

## Problem Statement
Rollout pipeline has TWO critical correctness bugs:

**Bug 1 — DSL match ignored:** `SelectSIMsForStage` SQL:
```sql
WHERE tenant_id=X AND state='active' 
  AND id NOT IN (already in rollout)
  AND policy_version_id = $prev
ORDER BY random() LIMIT $target
```
Missing: DSL `MATCH { apn = "data.demo" }` clause never applied to SIM selection. Result: rollout migrates RANDOM sims from prev version, not DSL-matching sims. Argus core value proposition broken.

**Bug 2 — `total_sims` wrong:** `StartRollout` computes:
```go
if version.AffectedSIMCount != nil { totalSIMs = *version.AffectedSIMCount }
if totalSIMs == 0 {
    count, _ := s.simStore.CountByFilters(tenant, SIMFleetFilters{}) // ALL TENANT SIMs!
    totalSIMs = count
}
```
Fallback counts ALL tenant SIMs (153) — not matching SIMs (7). Stage target calculated wrong: `ceil(153 × 1%) = 2` instead of `ceil(7 × 1%) = 1`. Progress bar + completion detection broken.

## User Story
As a policy administrator, I want rollouts to apply the policy's DSL MATCH criteria when selecting SIMs to migrate, and compute stage targets based on the actual matching SIM count, so rollout semantics are correct.

## Architecture Reference
- Backend: `internal/policy/rollout/service.go::StartRollout, ExecuteStage`
- Store: `internal/store/policy.go::SelectSIMsForStage`
- DSL: `internal/policy/dsl/` — need predicate → SQL translator

## Findings Addressed
F-142 (DSL match ignore), F-143 (total_sims fallback bug)

## Acceptance Criteria
- [ ] **AC-1:** New utility `internal/policy/dsl/sql_predicate.go` — translates DSL `MATCH { ... }` AST to SQL WHERE clause:
  - `apn = "data.demo"` → `apn_id = (SELECT id FROM apns WHERE name='data.demo')` (with parameterized binding)
  - `operator IN ("turkcell", "vodafone_tr")` → `operator_id IN (SELECT id FROM operators WHERE code = ANY($1))`
  - `imsi_prefix = "28601"` → `imsi LIKE '28601%'`
  - `rat_type = "lte"` → `rat_type = $1`
  - Nested AND/OR/NOT supported
  - Returns `(sql string, args []interface{}, err error)`
- [ ] **AC-2:** `SelectSIMsForStage` accepts DSL predicate, injects into WHERE:
  ```go
  func SelectSIMsForStage(tenantID, rolloutID, dslPredicate string, args []any, targetCount int) ([]uuid.UUID, error)
  ```
  Query: `SELECT id FROM sims WHERE <base> AND policy_version_id=$prev AND <dsl_predicate> ORDER BY random() LIMIT target FOR UPDATE SKIP LOCKED`
- [ ] **AC-3:** `StartRollout` computes `totalSIMs` = count of SIMs matching DSL (via same predicate + tenant filter), NOT all-tenant fallback:
  ```go
  predicate, args := dsl.ToSQLPredicate(compiledDSL.Match)
  totalSIMs := s.simStore.CountWithPredicate(ctx, tenantID, predicate, args)
  ```
- [ ] **AC-4:** Migration: `policy_versions.affected_sim_count` auto-computed at version create (CompileDSL → count → cache). Recomputed if DSL changes.
- [ ] **AC-5:** Edge case — DSL MATCH empty (apply to all SIMs): predicate = `TRUE`, totalSIMs = all tenant SIMs. Explicit design — rare but valid.
- [ ] **AC-6:** `ExecuteStage` unchanged — inherits correct targetMigrated = ceil(totalSIMs × pct / 100) from AC-3.
- [ ] **AC-7:** Integration test: Policy with `MATCH { apn = "data.demo" }` → rollout stage 1% → 1 SIM migrated (from 7 matching, not 153 tenant total).
- [ ] **AC-8:** Regression: existing rollouts without DSL match still work (fallback predicate = TRUE).
- [ ] **AC-9:** SQL injection safety — all values parameterized ($1, $2); DSL strings never concatenated raw.

## Files to Touch
- `internal/policy/dsl/sql_predicate.go` (NEW) — predicate translator
- `internal/policy/dsl/sql_predicate_test.go` (NEW) — each match type
- `internal/policy/rollout/service.go` — StartRollout + ExecuteStage
- `internal/store/policy.go::SelectSIMsForStage` — predicate param
- `internal/store/sim.go` — CountWithPredicate helper
- `migrations/YYYYMMDDHHMMSS_policy_versions_affected_sim_count.up.sql` — column if missing

## Risks & Regression
- **Risk 1 — Predicate translator bug injects bad SQL:** Strong test coverage per match type; fuzz tests; read-only role for production DSL eval.
- **Risk 2 — Existing rollouts assume old behavior:** In-progress rollouts finish via ExecuteStage — if prev version computed wrong totalSIMs, behavior changes mid-flight. Acceptance: complete in-progress using old total, new rollouts use correct predicate.
- **Risk 3 — Performance of predicate count:** COUNT(*) with JOIN apns + complex predicate can be slow. Mitigation: index on `sims.apn_id` + `sims.imsi`; cache affected_sim_count in policy_versions (AC-4).

## Test Plan
- Unit: predicate translator for 10 DSL scenarios (apn, operator, imsi_prefix, rat, AND/OR combos)
- Integration: rollout with apn=data.demo → exactly 7 matching SIMs selected
- SQL injection: inject `\\' OR 1=1 \\--` in DSL value → safely parameterized

## Plan Reference
Priority: P0 · Effort: L · Wave: 2.5 · Depends: FIX-231 (version state machine)
