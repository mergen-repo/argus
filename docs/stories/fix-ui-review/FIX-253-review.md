# Post-Story Review: FIX-253 — Suspend IP release + Activate empty-pool guard + audit-on-failure

> **RETROACTIVE REVIEW filed 2026-04-30** (original was INLINE 2026-04-26).
> Reconstructs the 14-check post-story review against the as-shipped code (commit `95856fb`).

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-241 (Wave 8 P0, completed 2026-04-26) | Suspend semantics changed — Suspend now releases dynamic IP. FIX-241's null-slice WriteList fix did not touch Suspend/Activate/Resume code paths. | NO_CHANGE |
| FIX-237 (M2M event taxonomy) | Adds `sim.activate.failed` and `sim.resume.failed` audit categories — should be classified into appropriate tier per FIX-237 taxonomy. Current implementation writes to `audit_logs` (audit pipeline), NOT to notification tiers — so no immediate notification-side refactor needed. | NO_CHANGE |
| Future story consuming sim state transitions | Resume signature now `*uuid.UUID` for ipAddressID — any future caller must pass either a fresh allocation or nil for static SIMs. | DOCUMENTED in plan; no orphan callers exist (verified via `grep -n "simStore.Resume\|simStore.Activate" internal/`). |
| Future test infra story | New tech debt D-149 (SIM store interface for handler-level error injection) — exposes 3 audit reasons currently untested end-to-end. | TRACKED in ROUTEMAP Tech Debt (this review) |

## Documents Updated (verified against commit 95856fb)

| Document | Change | Status |
|----------|--------|--------|
| docs/brainstorming/decisions.md | Added DEV-390/391/392/393 (all ACCEPTED) | UPDATED (per step-log line 7) |
| docs/USERTEST.md | Added "FIX-253:" section with 8 test scenarios (verified at /docs/USERTEST.md:4780+) | UPDATED |
| docs/ROUTEMAP.md | FIX-253 marked DONE + activity log entry | UPDATED |
| CLAUDE.md | Active session updated to advance to FIX-241 | UPDATED |
| GLOSSARY | No new domain terms introduced | NO_CHANGE |
| ARCHITECTURE | No architecture-level changes (handler/store internals only) | NO_CHANGE |
| SCREENS | BE-only story; no screen changes | NO_CHANGE |
| FRONTEND | No design-token changes | NO_CHANGE |
| FUTURE | No future opportunities/invalidations | NO_CHANGE |
| Makefile | No new targets/services | NO_CHANGE |

## Cross-Doc Consistency
- Contradictions found: 0
- ARCHITECTURE.md states "every state-changing operation creates an audit log entry" — FIX-253 implements this for failure paths too, ALIGNS with stated policy (was a gap previously).

## 14-Check Review (RETROACTIVE)

### 1. Next-story impact — PASS
Verified above. No upcoming stories require updates.

### 2. Architecture evolution — PASS
No new layers/services. Suspend-releases-IP semantic added at store layer, hidden behind existing `SIMStore.Suspend` API; transparent to upstream callers.

### 3. New domain terms — PASS
None. "POOL_EXHAUSTED" already in error-code catalog (used pre-existing `apierr.CodePoolExhausted`).

### 4. Screen updates — N/A
BE-only.

### 5. FUTURE.md relevance — PASS
No new future opportunities. Atomic IP release was always the implicit expectation; FIX-253 simply made it real.

### 6. New decisions — PASS
DEV-390 (POOL_EXHAUSTED 422 restated), DEV-391 (atomic IP release w/ static preservation), DEV-392 (Resume re-allocates per same flow as Activate), DEV-393 (audit on every failure branch). All 4 ACCEPTED in decisions.md.

### 7. Makefile / .env.example — PASS
No new env vars or services.

### 8. CLAUDE.md consistency — PASS
No port/URL changes.

### 9. Cross-doc consistency — PASS
Already covered above; no contradictions.

### 10. Story updates needed — PASS
No upcoming story needs modification.

### 11. Decision tracing — PASS
- DEV-390: implemented at handler.go:1025-1033 (empty-pool guard returning 422 POOL_EXHAUSTED)
- DEV-391: implemented at sim.go:632-680 (atomic IP release with static preserve)
- DEV-392: implemented at handler.go:1183-1320 + sim.go:696-754 (Resume mirror flow)
- DEV-393: implemented at handler.go:997-1088 (Activate audit) + 1206-1319 (Resume audit) + sim.go:538 signature change to `*uuid.UUID`
All 4 decisions reflected in code. NO orphans.

### 12. USERTEST completeness — PASS
Verified at `docs/USERTEST.md:4780-4870+` — section "FIX-253: Suspend IP Release + Activate Empty-Pool Guard + Audit-on-Failure" with 8 scenarios:
- Senaryo 1: Suspend dynamic IP release (AC-1)
- Senaryo 2: Suspend static IP preserve (AC-1)
- Senaryo 3: Activate empty pool 422 (AC-2)
- Senaryo 4: Resume static IP allocation skip (AC-5)
- Senaryo 5: Resume dynamic IP re-allocation (AC-5)
- Senaryo 6: Round-trip Suspend → Activate (AC-1 + AC-2)
- Senaryo 7: Audit log on every failure branch (AC-3)
- Senaryo 8: Static-allocation full lifecycle preserved through Suspend → Resume

### 13. Tech Debt pickup — PASS, with NEW item D-149
- No OPEN tech debt items target FIX-253 directly (FIX-253 was a spinoff of FIX-252 closing those concerns).
- Gate retroactively routes D-149: "SIM store interface for handler-level error injection — covers `get_sim_failed`/`list_pools_failed`/`allocate_failed` audit reasons currently uncovered by handler tests due to concrete-store mid-call error injection infeasibility."

### 14. Mock sweep — N/A
Not a frontend-first project.

## Decision Tracing
- Decisions checked: 4 (DEV-390, DEV-391, DEV-392, DEV-393)
- Orphaned (approved but not applied): 0
- All 4 traced to specific file:line evidence (see check 11 above)

## USERTEST Completeness
- Entry exists: YES
- Type: backend-integration scenarios (8 senaryo, written in Turkish per project convention)

## Tech Debt Pickup
- Items targeting FIX-253: 0 pre-existing
- Already RESOLVED by Gate: 0
- Resolved by Reviewer: 0
- NOT addressed: 0
- **NEW deferred (this retro)**: 1 → D-149 (test infra, future story)

## PAT-006 / PAT-023 Cross-References (from spec §"Plan Reference")
- **PAT-006 (inline-scan vs scanSIM drift)**: NOT triggered by FIX-253 — all three state transitions reuse `scanSIM` helper with `simColumns` constant. Verified at sim.go:578, :627, :739. PAT-006 RECURRENCE filed under FIX-251 (separate root-class fix).
- **PAT-023 (schema_migrations lying / schema drift)**: NOT triggered — FIX-253 introduces NO new migrations; pure handler/store logic on existing schema. The story's spec correctly identifies PAT-023 as "context for why this story exists at all" (FIX-252's full schema reset surfaced these latent defects).

## Mock Status
N/A.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | 3 audit reasons (`get_sim_failed`, `list_pools_failed`, `allocate_failed`) lack handler-level test coverage; only verified via code path inspection + go vet + live-verify smoke | NON-BLOCKING | DEFERRED D-149 | Routed to future test-infra story; concrete `*store.SIMStore` does not expose interface for mid-call error injection. Acceptable trade-off; 4 of 7 audit reasons covered by tests, all 7 verified by code review. |
| 2 | Original Gate skipped 3-scout dispatch per Ana Amil judgment call | NON-BLOCKING | FIXED | This retro reconstructs full Gate Team output; verdict matches original INLINE PASS. Future XL/L BE-only stories should run full Gate Team unless context budget is critical. |

## Project Health
- Stories completed up to FIX-253: many (Wave 9 P1 5/5 done, Wave 8 P0 closed, etc.)
- Current phase: UI Review Remediation (in progress)
- FIX-253 status: DONE (commit 95856fb)
- Blockers: None

## Verdict
**PASS (RETROACTIVE)** — implementation is sound, all 14 checks satisfied, 1 new tech debt item routed (D-149). Original INLINE review on 2026-04-26 was defensible; this retro confirms no regressions or missed findings of consequence.
