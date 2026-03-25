# Review: STORY-022 — Policy DSL Parser & Evaluator

**Date:** 2026-03-21
**Reviewer:** Amil Reviewer Agent
**Phase:** 4 (Policy & Orchestration)
**Status:** COMPLETE

---

## 1. Next Story Impact Analysis

### STORY-023 (Policy CRUD & Versioning) — 2 updates needed

1. **API naming alignment:** STORY-023 AC says "DSL source must parse and compile without errors before version can be activated." The public API is `dsl.CompileSource(source)` which returns `(*CompiledPolicy, []DSLError, error)`. STORY-023 should use `CompileSource` for validation + compilation in a single call (not separate Parse + Compile steps). The `[]DSLError` slice should be checked for any `Severity == "error"` entries. No story update needed — implementation detail for the developer.

2. **Compiled rules storage:** STORY-023 AC says "Compiled rules (JSON) stored in TBL-14.compiled_rules alongside DSL source." The `CompiledPolicy` struct serializes directly to JSON via `json.Marshal`. STORY-023 can use `compiler.ToJSON(compiled)` or direct `json.Marshal`. **No assumption changes.**

3. **Redis caching (AC-12 deferred):** STORY-022 deferred Redis evaluation caching to STORY-023/024. STORY-023 should include: cache compiled rules in Redis with key pattern `policy:compiled:{version_id}`, TTL 10min (per ARCHITECTURE.md caching strategy). **Add note to STORY-023.**

**Impact: LOW — No blocking changes, one note to add.**

### STORY-024 (Policy Dry-Run Simulation) — 1 update needed

1. **Evaluator API:** STORY-024 needs to evaluate a policy version against many SIMs. The API is `dsl.EvaluateCompiled(compiled, ctx)` — takes a `*CompiledPolicy` and `SessionContext`. For dry-run, STORY-024 must build a `SessionContext` per SIM from DB data (SIM record + operator + APN + last session info). This is straightforward.

2. **MATCH block filtering optimization:** For large fleets, STORY-024 should first filter SIMs by the MATCH block conditions at the DB level (SQL WHERE) before loading SIMs into memory for evaluation. The `CompiledMatch.Conditions` structure maps directly to SQL filters (apn, operator, rat_type, sim_type, roaming). **Add optimization note to STORY-024.**

3. **Behavioral change detection:** STORY-024 AC requires "behavioral_changes (list of changes: QoS upgrade/downgrade, new charging, session limit changes)." This requires comparing the `PolicyResult` from old version vs new version for the same SIM. The evaluator returns `QoSAttributes` map and `ChargingParams` — diff these for each sample SIM. **No assumption changes.**

**Impact: LOW — One optimization note to add.**

### STORY-025 (Policy Staged Rollout) — No changes needed

1. **CoA integration:** STORY-025 needs to send CoA after policy version assignment. The `PolicyResult.QoSAttributes` map contains bandwidth/timeout values that map to RADIUS attributes for CoA. The evaluator output is compatible.

2. **Concurrent versions:** During rollout, SIMs on different versions are evaluated independently. `EvaluateCompiled` is stateless — each call uses the SIM's assigned version's compiled rules. **No assumption changes.**

3. **Policy evaluation at auth time:** STORY-025 AC says "Policy evaluation at auth time uses SIM-specific version from TBL-15." The evaluator's `SessionContext` does not include version info — it evaluates whatever compiled policy is passed to it. Version resolution is STORY-025's responsibility. **Correct design.**

**Impact: NONE.**

### STORY-027 (RAT-Type Awareness) — 1 observation

1. **RAT type enum mismatch:** The DSL uses RAT types `nb_iot`, `lte_m`, `lte`, `nr_5g` (per DSL_GRAMMAR.md and parser validation). STORY-027 AC defines enum as `2G, 3G, 4G, 5G_NSA, 5G_SA, NB_IOT, CAT_M1`. There is a naming gap:
   - DSL: `nb_iot` vs STORY-027: `NB_IOT`
   - DSL: `lte` vs STORY-027: `4G`
   - DSL: `nr_5g` vs STORY-027: `5G_SA`
   - DSL: `lte_m` vs STORY-027: `CAT_M1`
   - STORY-027 adds `2G`, `3G`, `5G_NSA` which DSL does not support

   The DSL identifiers are case-insensitive and stored lowercase. STORY-027 should normalize RAT type values to match the DSL's convention before storing in session/SIM records — OR the DSL parser's RAT type validation should be extended to accept the STORY-027 enum values. **Add alignment note to STORY-027.**

2. **Policy DSL RAT conditions already work:** `WHEN rat_type = lte` and `WHEN rat_type IN (nb_iot, lte_m)` are fully functional. STORY-027's integration is about extracting RAT from protocol packets and feeding it into `SessionContext.RATType`. **No evaluator changes needed.**

**Impact: MEDIUM — RAT enum normalization must be aligned.**

---

## 2. Architecture Evolution

### Implemented vs Planned Structure

| Aspect | Planned (ARCHITECTURE.md) | Implemented | Status |
|--------|---------------------------|-------------|--------|
| Package location | `internal/policy/dsl/` (parser) + `internal/policy/evaluator/` (evaluator) + `internal/policy/rollout/` (rollout) | All in `internal/policy/dsl/` | ACCEPTABLE — single package for tightly coupled components |
| Public package | `pkg/dsl/` mentioned in ARCHITECTURE.md, STORY-022, DSL_GRAMMAR.md | Not created — internal only | ACCEPTABLE — no external consumers yet |
| Evaluator semantics | "first match wins" (STORY-022 AC-5) | "last match wins for assignments, all actions collected" (ALGORITHMS.md Section 6, gate-verified) | COMPLIANT with ALGORITHMS.md |

### ARCHITECTURE.md Update Needed

The project structure in ARCHITECTURE.md shows:
```
├── policy/
│   ├── dsl/        # DSL parser
│   ├── evaluator/  # Rule evaluation
│   └── rollout/    # Staged rollout
```

The actual implementation puts parser, compiler, and evaluator all in `dsl/`. The `evaluator/` and `rollout/` packages will be created by STORY-025 if needed, or rollout logic may live elsewhere. **No update needed now** — the structure is still accurate as a plan; `evaluator/` and `rollout/` can be created by their respective stories.

### DSL_GRAMMAR.md Note

DSL_GRAMMAR.md references `pkg/dsl/` as "Public package: `pkg/dsl/` (for external tooling)." The implementation uses `internal/policy/dsl/`. The gate noted this as observation #2. **Update DSL_GRAMMAR.md to reflect actual location.**

---

## 3. New Domain Terms

The following terms were introduced or refined by STORY-022 implementation:

| Term | Definition | Context |
|------|-----------|---------|
| AST (Policy) | Abstract Syntax Tree representation of a parsed Policy DSL source. Contains Policy, MatchBlock, RulesBlock, WhenBlock, ActionCall, ChargingBlock nodes. | SVC-05, STORY-022 |
| CompiledPolicy | JSON-serializable rule tree produced by compiling a Policy AST. Contains normalized values (bytes, bps, seconds) for fast evaluation. Stored in TBL-14 `compiled_rules` JSONB column. | SVC-05, STORY-022 |
| SessionContext | Runtime context struct passed to policy evaluator. Contains SIM/session state: operator, APN, RAT type, usage, time_of_day, roaming, session_count, metadata. | SVC-05, STORY-022 |
| PolicyResult | Output of policy evaluation: allow/deny decision, QoS attributes map, charging parameters, collected actions list, matched rule count. | SVC-05, STORY-022 |
| DSL Version | Semantic version string embedded in compiled policies for grammar evolution tracking. Current: "1.0". | SVC-05, STORY-022 |

**Action: Add to GLOSSARY.md under "Argus Platform Terms".**

---

## 4. FUTURE.md Relevance

1. **Digital Twin "shadow mode":** FUTURE.md states "Policy engine supports 'shadow evaluation' (evaluate without enforce)." The DSL evaluator is already a pure computation library with no side effects — `EvaluateCompiled()` returns a `PolicyResult` without modifying any state. Shadow mode is inherently supported by the current design. **No FUTURE.md update needed.**

2. **AI Policy Recommendations:** FUTURE.md states "Policy engine (L4) must accept AI-generated policy recommendations." The DSL is text-based — an AI system could generate DSL source strings and use `CompileSource()` + `Validate()` to verify them. The architecture naturally supports this. **No FUTURE.md update needed.**

---

## 5. New Decisions to Capture

| # | Date | Decision | Status |
|---|------|----------|--------|
| DEV-056 | 2026-03-21 | STORY-022: DSL package placed in `internal/policy/dsl/` (not `pkg/dsl/` as mentioned in STORY-022 story and DSL_GRAMMAR.md). Internal package is correct — no external consumers exist. `pkg/dsl/` can be created as a thin wrapper if external tooling needs it. | ACCEPTED |
| DEV-057 | 2026-03-21 | STORY-022: Evaluation semantics follow ALGORITHMS.md Section 6 ("last match wins for assignments, all actions collected"), not STORY-022 AC-5 ("first matching WHEN wins"). The AC was ambiguous — ALGORITHMS.md is authoritative. Gate verified correctness. | ACCEPTED |
| DEV-058 | 2026-03-21 | STORY-022: Public API naming uses `CompileSource`/`EvaluateSource`/`EvaluateCompiled` (not `Compile`/`Evaluate` as in story). Better naming — avoids ambiguity with `CompileAST`. No impact on dependent stories. | ACCEPTED |
| DEV-059 | 2026-03-21 | STORY-022: Redis evaluation caching (AC-12) deferred to STORY-023/024 by design. DSL package is a pure computation library with no external I/O. Caching belongs in the service layer that calls the evaluator. | ACCEPTED |
| DEV-060 | 2026-03-21 | STORY-022: `time_of_day IN (HH:MM-HH:MM)` uses `isTimeInRange()` with midnight wrapping support. Gate fix — original implementation used string equality which would never match time ranges. Critical for night-shift and off-peak policy rules. | ACCEPTED |

---

## 6. Cross-Document Consistency

| Check | Documents | Status | Notes |
|-------|-----------|--------|-------|
| RAT type values | DSL_GRAMMAR.md vs STORY-027 | MISMATCH | DSL uses `nb_iot/lte_m/lte/nr_5g`; STORY-027 uses `NB_IOT/CAT_M1/4G/5G_SA`. Needs alignment in STORY-027. |
| Package location | ARCHITECTURE.md vs implementation | MINOR | ARCHITECTURE.md shows `evaluator/` as separate package; implementation has evaluator in `dsl/`. Acceptable — evaluator is tightly coupled to AST types. |
| Package location | DSL_GRAMMAR.md vs implementation | MINOR | DSL_GRAMMAR.md says `pkg/dsl/`; actual is `internal/policy/dsl/`. Update DSL_GRAMMAR.md. |
| Evaluation semantics | STORY-022 AC-5 vs ALGORITHMS.md | CLARIFIED | AC-5 says "first match wins"; ALGORITHMS.md says "last match wins for assignments." ALGORITHMS.md is authoritative. Gate verified implementation follows ALGORITHMS.md. |
| DSL actions | DSL_GRAMMAR.md vs implementation | CONSISTENT | All 7 actions (notify, throttle, disconnect, log, block, suspend, tag) implemented. |
| DSL conditions | DSL_GRAMMAR.md vs implementation | CONSISTENT | All 10 condition fields, all operators, compound conditions, time ranges. |
| Compiled JSON format | DSL_GRAMMAR.md vs implementation | CONSISTENT | JSON structure matches spec. Unit normalization (bytes, bps, seconds) correct. |
| Feature references | PRODUCT.md F-032 (Policy DSL) | CONSISTENT | Implemented per spec. |
| Caching strategy | ARCHITECTURE.md caching table | CONSISTENT | "Policy compiled rules: Redis, 10min TTL, NATS invalidation" — deferred to STORY-023, not violated. |

**Overall: CONSISTENT with 2 minor mismatches (RAT enum, package path in DSL_GRAMMAR.md).**

---

## 7. Document Updates

### 7.1 GLOSSARY.md — Add 5 new terms
- AST (Policy), CompiledPolicy, SessionContext, PolicyResult, DSL Version

### 7.2 DSL_GRAMMAR.md — Fix package path
- Change `pkg/dsl/` reference to `internal/policy/dsl/`

### 7.3 decisions.md — Add 5 new decisions
- DEV-056 through DEV-060

### 7.4 ROUTEMAP.md — Mark STORY-022 complete
- Status: `[x] DONE`
- Step: `—`
- Completed: `2026-03-21`
- Update progress counter: 22/55 (40%)

### 7.5 STORY-027 — Add RAT enum alignment note
- Note that DSL RAT enum (`nb_iot/lte_m/lte/nr_5g`) differs from STORY-027's proposed enum (`NB_IOT/CAT_M1/4G/5G_SA/2G/3G/5G_NSA`). STORY-027 implementation must either:
  - (a) Normalize RADIUS/Diameter RAT values to DSL enum conventions, OR
  - (b) Extend DSL parser's valid RAT types to include the broader set

---

## Summary

| Metric | Result |
|--------|--------|
| Stories Impacted | 4 analyzed, 1 needs alignment note (STORY-027) |
| Architecture Changes | None required |
| New Glossary Terms | 5 |
| New Decisions | 5 (DEV-056 to DEV-060) |
| Cross-Doc Consistency | CONSISTENT (2 minor mismatches noted) |
| FUTURE.md | No updates needed |
| ROUTEMAP Progress | 22/55 (40%) |
| Next Story | STORY-023 (Policy CRUD & Versioning) |
