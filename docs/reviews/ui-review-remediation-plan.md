# UI Review Remediation Plan — FIX-201..FIX-228

**Scope:** `docs/reviews/ui-review-2026-04-19.md` (107 aktif finding)
**Triage date:** 2026-04-19
**Change type:** Mid-project CHANGE (post-Phase 10, pre-Documentation)
**User decisions locked:** Modal pattern = Option C (Dialog compact confirm + SlidePanel rich form)

## Priority Tiers

| Tier | Criteria | Action |
|------|----------|--------|
| **P0 — Critical** | Data corruption, broken features, FE↔BE contract mismatch, user workflow blockers | Fix first, blocks everything else |
| **P1 — High** | Architectural fixes, missing major features | Fix after P0 |
| **P2 — Medium** | UI consistency, polish, UX enhancement | Can ship incrementally |
| **P3 — Low** | Nice-to-have additions | Optional / post-release |

## Story Groupings

### P0 — Backend Contract + Data Integrity (8 stories)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-201** | Bulk Actions Contract Fix — accept sim_ids (state-change + policy-assign + operator-switch) | F-103, F-104, F-105, F-106, F-107, F-108 | L |
| **FIX-202** | SIM List & Dashboard DTO — Operator Name Resolution Everywhere | F-82, F-97, F-84, F-14 (partial), F-21, F-102 | M |
| **FIX-203** | Dashboard Operator Health — Uptime/Latency/Activity + WS Push | F-03, F-04, F-45, F-50, F-55, F-80 | L |
| **FIX-204** | Analytics group_by NULL Scan Bug + APN Orphan Sessions | F-28, F-22 | S |
| **FIX-205** | Token Refresh Auto-retry on 401 | F-35 | S |
| **FIX-206** | Orphan Operator IDs Cleanup + FK Constraints + Seed Fix | F-83, F-22, F-63, F-81, F-93 | M |
| **FIX-207** | Session/CDR Data Integrity — negative duration, cross-pool IP, IMSI format | F-98, F-99, F-100, F-101 (verify), F-34 | M |
| **FIX-208** | Cross-Tab Data Aggregation Unify (SIM usage/cost/sessions + Operator/APN counts) | F-95, F-96, F-65, F-51, F-24, F-25 | L |

**FIX-203 Scope Cuts (shipped):**
- AC-9: no virtualization; slice(0,50) + Show all → /operators link (deferred: virtual scroll for tenants with >50 operators)
- AC-7 SLA threshold: hardcoded 500ms default (per-operator config column deferred to future story)
- Sparkline: BE provides 12 × 5-min buckets for 1h latency trend (session-activity sparkline retained as deferred polish)

### P0 — Alert System Architecture (3 stories)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-209** | Unified `alerts` Table + Operator/Infra Alert Persistence | F-36, F-40, F-44, F-10 | XL |
| **FIX-210** | Alert Deduplication + State Machine (edge-triggered, cooldown) | F-08 | M |
| **FIX-211** | Severity Taxonomy Unification (critical/high/medium/low/info) | F-37 | M |

### P1 — Event System (2 stories)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-212** | Unified Event Envelope + Name Resolution + Missing Publishers | F-13, F-14, F-11, F-17, F-18, F-15, F-16, F-19, F-21, F-102 | XL |
| **FIX-213** | Live Event Stream UX — filter chips, usage display, alert body | F-09, F-12, F-20, F-19 | M |

### P1 — Missing Major Features (2 stories)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-214** | CDR Explorer Page (filter, search, session timeline, export) | F-62 | L |
| **FIX-215** | SLA Historical Reports + PDF Export + Drill-down | F-44, F-46, F-47, F-48, F-49 | L |

### P2 — UI Consistency Standardization (4 stories)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-216** | Modal Pattern Standardization — Dialog vs SlidePanel semantik ayrım | F-109 | M |
| **FIX-217** | Timeframe Selector Pill Toggle Unification | F-61, F-73 | S |
| **FIX-218** | Views Button Global Removal + Checkbox Cleanup (Operators) | F-59, F-60, F-66, F-71, F-90 | S |
| **FIX-219** | Name Resolution + Clickable Cells Everywhere (global audit) | F-14, F-31, F-67, F-75, F-78 | M |

### P2 — UX Polish (5 stories)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-220** | Analytics Polish — MSISDN kolonu, IN/OUT split, tooltip, delta cap, capitalization | F-23, F-26, F-29, F-30, F-32, F-33 | M |
| **FIX-221** | Dashboard Polish — Heatmap tooltip, IP pool KPI clarity | F-05 | S |
| **FIX-222** | Operator/APN Detail Polish — KPI row, tab consolidation, tooltips | F-52, F-53, F-54, F-56, F-57, F-58, F-68, F-69, F-70, F-88 | M |
| **FIX-223** | IP Pool Detail Polish — search backend, last_seen, reserve modal ICCID | F-74, F-75, F-76, F-78, F-79 | M |
| **FIX-224** | SIM List/Detail Polish — state filter, Created datetime, bulk bar sticky, compare limit, import validation | F-85, F-86, F-87, F-91, F-92 | M |

### P2 — Simulator / Infra (2 stories)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-225** | Docker Restart Policy + Infra Stability | F-02, F-07 | S |
| **FIX-226** | Simulator Coverage + Volume Realism | F-06 (revised), F-27, F-64, F-93 | M |

### P2 — APN Connected SIMs Panel (1 story)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-227** | APN Connected SIMs SlidePanel — CDR + Usage graph + quick stats | F-72 | S |

### P3 — Nice-to-have (2 stories)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-228** | Login — Forgot Password Flow + version footer | F-01 | M |
| **FIX-229** | Alert Feature Enhancements (Mute All UX, Export format, Similar clustering, retention) | F-38, F-39, F-41, F-42, F-43 | M |

### P0 — Policy Rollout System Deep-Fix (4 stories — added 2026-04-19 after rollout deep-dive)

Scope: `docs/reviews/ui-review-2026-04-19.md` "Rollout System — Architecture Analysis" bölümündeki tüm F-141..F-148 bulgularını kapsar. Dependency chain: FIX-231 ⇒ FIX-230 ⇒ FIX-232 ⇒ FIX-233.

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-230** | Rollout DSL Match Integration — `SelectSIMsForStage` DSL predicate filter + `total_sims` accurate count | F-142, F-143 | L |
| **FIX-231** | Policy Version State Machine — atomic rolling_out→active→superseded transitions + 1-active-rollout constraint + F-146a dual-source fix | F-144, F-146a, F-146b | XL |
| **FIX-232** | Rollout UI Active State — progress bar, advance/rollback/abort buttons, WS `policy.rollout.progressed` subscription, correct endpoint paths | F-145, F-146 | L |
| **FIX-233** | SIM List Policy column + Rollout Cohort filter — "Policy v{N}" column, filter chips (policy/version/rollout-stage), DTO extension | F-147, F-148 | M |

### P2 — CoA Status Enum Extension (1 story)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-234** | `coa_status` enum genişletme (pending|queued|acked|failed|no_session|skipped) + idle SIM handling + UI counters | (Rollout CoA gap) | S |

---

## Phase 2 Review Additions (FIX-235..FIX-248) — Added 2026-04-19

**Scope:** Phase 2 review (17 remaining pages) + retrospective implementation audit sonrasında eklenen story'ler. Kapsam: F-141..F-329 finding'ler.

### P0 — Global Pattern / Critical Bugs (3 stories)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-241** | Global API nil-slice fix — `WriteList` helper normalize nil → `[]` (F-243/F-277 crash root cause) | F-302, F-243, F-277, F-328 | XS (1-line fix + test) |
| **FIX-242** | Session Detail extended DTO populate — SoR/Policy/Quota fields backend populate kodu | F-299, F-159, F-161 | M |
| **FIX-237** | M2M-centric Event Taxonomy + Notification Redesign — per-SIM event'leri kaldır, aggregate/digest event'ler ekle | F-217, F-227, F-236 | XL |

### P1 — Feature Completion (5 stories)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-243** | Policy DSL realtime validate endpoint + FE linter integration | F-309, F-135 | M |
| **FIX-244** | Violations lifecycle UI — acknowledge + remediate actions wired | F-310, F-169 | S |
| **FIX-239** | Knowledge Base Ops Runbook Redesign — 9 bölüm operasyonel perspektif + interactive request/response popup | F-230 | L |
| **FIX-236** | 10M SIM Scale Readiness Audit — filter-based bulk selection, async batch pattern, streaming export, virtual scrolling | F-183 | XL |
| **FIX-248** | Reports Subsystem Refactor — 4 kaldır (BTK/KVKK/GDPR/Cost) + 5 yeni (fleet_health/policy_rollout_audit/ip_pool_forecast/coa_enforcement/traffic_trend) + Local FS storage | F-325..F-329, F-326 | XL |

### P2 — UX Redesign / Consolidation (4 stories)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-240** | Unified Settings Page + Tabbed Reorganization — Security/Sessions/Reliability/Notifications tab altına | F-231, F-232, F-233, F-234 | M |
| **FIX-246** | Merge Quotas + Resources → unified "Tenant Usage" dashboard (tenant card grid + threshold alerts) | F-314, F-315, F-316, F-317 | M |
| **FIX-235** | M2M eSIM Provisioning Pipeline — SGP.22 → SGP.02 refactor + SM-SR integration + bulk operations | F-172, F-178 | XL |
| **FIX-245** | Remove 5 Admin Sub-pages + migrate Kill Switches to env vars — Cost/Compliance/DSAR/Maintenance/Kill Switches | F-313 | L |

### P2 — Scope Reduction (3 stories)

| Story | Title | Findings | Effort |
|-------|-------|----------|--------|
| **FIX-238** | Remove Roaming Feature (full stack — UI + BE + DB + DSL grammar) | F-229 | L |
| **FIX-247** | Remove Admin Global Sessions UI (backend retain for user-centric revoke) | F-320 | S |
| — | (DSAR event `data_portability.ready` cleanup bundled into FIX-245) | — | — |

---

## Updated Summary (Phase 1 + Phase 2)

| Tier | Story count | Total effort |
|------|-------------|--------------|
| P0 | 18 | ~10-12 weeks |
| P1 | 9 | ~6-8 weeks |
| P2 | 21 | ~7-9 weeks |
| P3 | 2 | ~1 week |
| **TOTAL** | **50 stories** (FIX-201..FIX-248) | **~24-30 weeks** |

## Phase 2 Dependencies

```
FIX-241 (nil-slice) — P0, XS, standalone — unblocks F-243/F-277 UI crashes
FIX-242 (session DTO) ─→ FIX-233 (SIM cohort view — needs session policy)
FIX-237 (event taxonomy) ─→ FIX-212 (event envelope) — same scope

FIX-243 (DSL validate) ─→ FIX-135 (seed DSL fix)
FIX-244 (violations lifecycle) ─→ F-165 filter fix (backend ready)

FIX-246 (quotas+resources merge) — standalone
FIX-240 (unified settings) — standalone but affects navigation
FIX-245 (admin scope reduction) — touches migrations, separate from rest

FIX-236 (10M scale) — cross-cutting, touches bulk/virtual-scroll/async patterns
FIX-238 (remove roaming) — delete operation, careful with DSL grammar
FIX-247 (admin sessions UI removal) — trivial
FIX-248 (reports refactor) — new storage backend + new builders
```

## Wave Order (Updated)

**Wave 1:** FIX-241 (global null-slice — **do this FIRST**, unblocks crashes) + Wave 1 previous items
**Wave 2-7:** Previous waves as defined above
**Wave 8 (Phase 2 P0):** FIX-237, FIX-242
**Wave 9 (Phase 2 P1):** FIX-243, FIX-244, FIX-239, FIX-236, FIX-248
**Wave 10 (Phase 2 P2 — UX/scope):** FIX-240, FIX-246, FIX-235, FIX-245, FIX-238, FIX-247

## Critical Path (Original + Phase 2)

```
FIX-206 (orphan cleanup) ─┬─→ FIX-202 (SIM DTO)
                          ├─→ FIX-208 (cross-tab aggregation)
                          └─→ FIX-231 (dual-source fix overlap)

FIX-211 (taxonomy) ─→ FIX-209 (alerts table) ─→ FIX-213 (alert UX)
FIX-210 (dedup) ────→ FIX-209

FIX-212 (event envelope) ─→ FIX-213 (event stream UX)
                         ├─→ FIX-203 (dashboard WS operator health)
                         └─→ FIX-232 (rollout progressed event)

FIX-216 (modal) ─→ FIX-224 (SIM bulk UX)
FIX-218 (views removal) parallel
FIX-219 (name resolution) ─→ many polish stories

# Rollout deep-fix chain (P0, blocks SIM cohort observability):
FIX-231 (version state machine + dual source) ─→ FIX-230 (DSL match)
                                              └─→ FIX-232 (UI active state)
                                              └─→ FIX-233 (SIM list policy column)
FIX-230 ─→ FIX-233 (cohort filtering needs correct version assignment)
FIX-234 (CoA enum) parallel to above
```

## Suggested Execution Order

**Wave 1 (P0 Critical — bulk blockers):** FIX-201, FIX-202, FIX-204, FIX-205, FIX-206, FIX-207
**Wave 2 (P0 Data + Architecture):** FIX-208, FIX-211, FIX-210, FIX-209
**Wave 2.5 (P0 Rollout foundation):** FIX-231, FIX-230 (sequential — state machine first)
**Wave 3 (P0 + P1 UI fundamental):** FIX-203, FIX-212, FIX-213, FIX-232
**Wave 4 (P1 missing features + SIM cohort):** FIX-214, FIX-215, FIX-233
**Wave 5 (P2 standardization):** FIX-216, FIX-217, FIX-218, FIX-219
**Wave 6 (P2 polish):** FIX-220..FIX-227, FIX-234
**Wave 7 (P3):** FIX-228, FIX-229

## Execution Recommendations

1. **Each FIX story follows normal `/amil dev` pipeline:** Plan → Dev → Lint → Gate → Review → Commit → Handoff
2. **Gate MUST include regression tests** — mevcut UAT senaryoları kırılmamalı
3. **Waves 1-2 sequential** (data integrity foundation gerek)
4. **Waves 3+ parallelizable** (AUTOPILOT mode uygundur Wave 3 sonrası)
5. **F-109 modal decision `docs/FRONTEND.md`'e yazılsın** (FIX-216'nın bir parçası)

## Coverage Matrix

107 aktif finding → 30 story.
- Tüm finding'ler story'lerden birine atanmış (yukarıdaki tablolar)
- "ayrıntı yok" olanlar: positive finding'ler (F-89, F-94) — story yok, zaten doğru
- F-77 skipped (historical util chart — scope dışı)

## Open Decision Points for User

Bu planı uygulamaya geçmeden önce kullanıcı karar vermeli:

1. **Scope onayı:** 30 story fazla mı? Daha minimal MVP remediation (P0 only = 11 story) tercih edilir mi?
2. **Execution mode:** Sequential manual review mi, AUTOPILOT batch mi?
3. **Parallel vs Wave:** Wave'ler sırayla mı (güvenli), yoksa Wave 3+'dan sonra parallel AUTOPILOT mu (hızlı)?
4. **Modal decision Option C onay:** FIX-216 için kesin olarak Option C devam mı?
5. **Sessions/Policies/Topology etc. remaining sayfalar inceleme:** Bu plan sonrasında mı (daha fazla finding ekleme olasılığı) yoksa önce inceleme → birleşik plan mı?

## Post-Approval Actions

User approval'dan sonra:
1. Update `docs/ROUTEMAP.md` — new "UI Review Remediation" track başlığı + 30 FIX story satırı
2. Update `docs/brainstorming/decisions.md` — modal pattern decision + triage rationale
3. Create individual story files: `docs/stories/fix-ui-review/FIX-NNN-*.md` (30 files)
4. Dispatch first story to Planner agent
