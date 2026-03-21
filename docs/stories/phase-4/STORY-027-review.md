# Review: STORY-027 — RAT-Type Awareness (All Layers)

**Reviewer:** Amil Reviewer Agent
**Date:** 2026-03-21
**Phase:** 4 (Policy & Orchestration) -- LAST STORY IN PHASE
**Story Status:** DONE (gate PASS, 672 tests, 14 files changed)

---

## 1. Next Story Impact (Phase 5 Stories)

Phase 4 is complete. Phase 5 (eSIM & Advanced Ops) contains STORY-028, STORY-029, STORY-030, STORY-031. Impact analysis:

| Phase 5 Story | RAT-Type Relevance | Impact |
|---|---|---|
| STORY-028 (eSIM Profiles) | LOW | eSIM profile switch triggers operator/APN/IP changes. RAT type is extracted from protocol requests (not from eSIM switch itself). No direct dependency on `rattype` package. However, after an eSIM switch, the next session will use `rattype.FromRADIUS/FromDiameter/FromSBA` to determine the new RAT -- already handled. |
| STORY-029 (OTA SIM) | NONE | OTA commands are management-plane (APDU/SMS-PP). No RAT extraction needed. |
| STORY-030 (Bulk Operations) | LOW | Bulk policy assign sends CoA to active sessions. The CoA path does not extract RAT type; it modifies policy. `SIM Fleet Filters` from STORY-024 can filter by `rat_type` -- already works with canonical values via `rattype.Normalize`. |
| STORY-031 (Job Runner) | NONE | Job infrastructure. No RAT-type interaction. |

**Verdict:** No Phase 5 stories have direct dependencies on STORY-027 deliverables. The `rattype` package is fully self-contained. No action items.

---

## 2. Architecture Evolution

### 2a. ARCHITECTURE.md — Project Structure

**GAP FOUND:** The project structure tree (line 133-138) lists `internal/aaa/` sub-packages but does NOT include `rattype/`:

```
│   ├── aaa/                      # SVC-04: AAA engine
│   │   ├── radius/               # RADIUS server
│   │   ├── diameter/             # Diameter server
│   │   ├── sba/                  # 5G SBA proxy
│   │   ├── eap/                  # EAP-SIM/AKA handlers
│   │   └── session/              # Session management
```

**Action Required:** Add `rattype/` line:

```
│   │   ├── rattype/              # RAT type canonical enum & mapping
```

### 2b. ARCHITECTURE.md — Caching & Performance

No new caching introduced by STORY-027. `rattype` functions are pure in-memory O(1) lookups (switch/map). No Redis, no DB. Consistent with the AAA Hot Path performance architecture.

### 2c. ARCHITECTURE.md — General

No new API endpoints, no new DB tables, no new Docker services. Architecture references (SVC-04, SVC-05, SVC-06) remain accurate. No changes needed beyond the project structure gap.

---

## 3. GLOSSARY.md Updates

### Terms to Add

| Term | Definition | Context |
|------|-----------|---------|
| RAT Type (Canonical) | Normalized string identifier for Radio Access Technology. Canonical values: `utran` (3G), `geran` (2G), `lte` (4G), `nb_iot` (NB-IoT), `lte_m` (LTE-M/Cat-M1), `nr_5g` (5G SA), `nr_5g_nsa` (5G NSA), `unknown`. Protocol-specific values (RADIUS numeric codes, Diameter AVP values, 5G SBA strings) are mapped to canonical form via the `rattype` package. | SVC-04, STORY-027, `internal/aaa/rattype/` |

### Existing Terms — Verified OK

- **RAT** (Radio Access Technology) — already in GLOSSARY.md Network Terms: "NB-IoT, LTE-M, 4G LTE, 5G NR". Sufficient.
- **SoR** — already references RAT preference. Updated in STORY-026 review.
- **SessionContext** — already lists "RAT type" as a field. OK.
- **CDR** — already mentions "RAT-type per session". OK.

---

## 4. FUTURE.md Relevance

**FTR-003 (Auto-SoR)** benefits from STORY-027:
- `rattype` package provides the canonical enum that any AI-based SoR strategy would use
- `rattype.Normalize()` ensures consistent inputs regardless of protocol source
- No FUTURE.md changes needed -- the description "real-time operator selection based on coverage + cost + latency optimization per device location" implicitly includes RAT awareness

**FTR-004 (Network Quality Scoring)** could benefit from RAT-based quality segmentation. However, the current FUTURE.md text is sufficient -- RAT-type data availability is an implementation detail, not an architectural provision.

**No FUTURE.md changes needed.**

---

## 5. Decisions (decisions.md)

### New Decisions to Record

| # | Date | Decision | Status |
|---|------|----------|--------|
| DEV-075 | 2026-03-21 | STORY-027: Canonical RAT type naming follows DSL conventions (`utran`, `geran`, `lte`, `nb_iot`, `lte_m`, `nr_5g`, `nr_5g_nsa`) not display names (`2G`, `3G`, `4G`). Display names mapped via `rattype.DisplayName()` for UI layer. This resolves the Option A vs Option B question from STORY-027 post-notes. | ACCEPTED |
| DEV-076 | 2026-03-21 | STORY-027: `rattype` package has zero internal imports (only stdlib `strings`). Deliberately designed as a pure-function library with no DB, Redis, NATS, or HTTP dependencies. Can be used by any layer without circular import risk. | ACCEPTED |
| DEV-077 | 2026-03-21 | STORY-027: SIM `last_rat_type` update on session creation is fault-tolerant -- `UpdateLastRATType` failure is logged as warning but does not block session creation. Rationale: RAT tracking is informational, not on the critical auth path. | ACCEPTED |
| DEV-078 | 2026-03-21 | STORY-027: `FromRADIUS` takes `uint8` (not `uint32` as in gate doc AC-1/AC-2 comments). 3GPP-RAT-Type VSA value is a single byte in RADIUS (TS 29.061). `FromDiameter` takes `uint32` matching Diameter's Unsigned32 AVP data type. | ACCEPTED |

---

## 6. Cross-Document Consistency

| Document | Check | Status | Detail |
|----------|-------|--------|--------|
| SCOPE.md | L3 "RAT-type awareness across all layers" | OK | F-026 fully delivered by STORY-027 |
| SCOPE.md | L4 "Dynamic policy rules — RAT-type conditions" | OK | DSL parser extended with all RAT aliases |
| SCOPE.md | L5 "per RAT-type cost differentiation" | OK | Pre-existing TBL-18 rat_type + rat_multiplier columns. CDR processing (STORY-032) will consume these. |
| PRODUCT.md | F-022 "SoR with RAT-type preference" | OK | SoR `filterByRAT` uses canonical rattype constants |
| PRODUCT.md | F-026 "RAT-type awareness across all layers" | OK | RADIUS, Diameter, 5G SBA, DSL, SoR, session, SIM all RAT-aware |
| PRODUCT.md | F-028 "Dynamic policy rules — RAT-type" | OK | DSL `validRATTypes` extended with 14 entries |
| PRODUCT.md | F-040 "RAT-type cost differentiation" | OK | Schema ready (TBL-18), awaits STORY-032 (CDR) |
| ARCHITECTURE.md | Project structure | GAP | Missing `rattype/` under `internal/aaa/` (see section 2a) |
| ARCHITECTURE.md | AAA Hot Path | OK | `rattype` functions are O(1) pure mappings, no latency impact |
| ARCHITECTURE.md | Extension Points | OK | SoR pluggable strategy still valid with canonical RAT enum |
| GLOSSARY.md | RAT term | OK | Present in Network Terms |
| GLOSSARY.md | RAT Type (Canonical) | GAP | New term needed for canonical enum definition (see section 3) |
| ROUTEMAP.md | Phase 4 status | GAP | Header says `[PENDING]`, should be `[DONE]` (see section 7) |
| ROUTEMAP.md | Progress counter | GAP | Says "27/55 (49%)", should still be 27/55 but phase header needs update |
| ROUTEMAP.md | STORY-027 row | OK | Marked `[x] DONE`, date 2026-03-21 |
| db/_index.md | TBL-10 (sims) | OK | `last_rat_type` was pre-existing |
| db/_index.md | TBL-17 (sessions) | OK | `rat_type` was pre-existing |
| DSL_GRAMMAR.md | RAT type values | NEEDS CHECK | Should list all canonical values + aliases accepted by parser |
| decisions.md | STORY-027 entries | GAP | No DEV-075..078 entries yet (see section 5) |

---

## 7. Phase 4 Completion Status

All 6 stories in Phase 4 (Policy & Orchestration) are DONE:

| # | Story | Status | Completed |
|---|-------|--------|-----------|
| STORY-022 | Policy DSL Parser & Evaluator | DONE | 2026-03-21 |
| STORY-023 | Policy CRUD & Versioning | DONE | 2026-03-21 |
| STORY-024 | Policy Dry-Run Simulation | DONE | 2026-03-21 |
| STORY-025 | Policy Staged Rollout (Canary) | DONE | 2026-03-21 |
| STORY-026 | Steering of Roaming Engine | DONE | 2026-03-21 |
| STORY-027 | RAT-Type Awareness (All Layers) | DONE | 2026-03-21 |

**Action Required:** Mark Phase 4 as `[DONE]` in ROUTEMAP.md header and phase title.

---

## 8. Action Items Summary

| # | Priority | Action | Target File |
|---|----------|--------|-------------|
| 1 | HIGH | Mark Phase 4 as [DONE] in phase header and update current phase to Phase 5 | `docs/ROUTEMAP.md` |
| 2 | MEDIUM | Add `rattype/` to project structure tree under `internal/aaa/` | `docs/ARCHITECTURE.md` |
| 3 | MEDIUM | Add "RAT Type (Canonical)" glossary term | `docs/GLOSSARY.md` |
| 4 | LOW | Add DEV-075..078 decisions | `docs/brainstorming/decisions.md` |
| 5 | LOW | Add ROUTEMAP.md change log entry for Phase 4 completion and STORY-027 | `docs/ROUTEMAP.md` |

---

## 9. Verdict

**STORY-027 is well-implemented and architecturally clean.** The `rattype` package is an exemplary shared utility: zero dependencies, pure functions, O(1) lookups, comprehensive aliases, and consistent usage across all AAA protocols (RADIUS, Diameter, 5G SBA), policy DSL, SoR engine, and session management. The functional-options pattern (`WithSIMStore`) for cross-service DI is clean and backward-compatible.

**Phase 4 (Policy & Orchestration) is COMPLETE.** All 6 stories delivered a coherent policy and orchestration layer: DSL parser/evaluator, CRUD with versioning, dry-run simulation, staged rollout with CoA, steering of roaming, and RAT-type awareness. The system now has end-to-end support from protocol-level RAT extraction through policy evaluation to operator routing decisions.

**2 documentation gaps identified** (ARCHITECTURE.md project structure, GLOSSARY.md canonical RAT type term) -- both are minor doc updates. No code changes needed.
