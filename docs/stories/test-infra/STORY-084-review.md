# Post-Story Review: STORY-084 — Simulator 5G SBA Client (AUSF/UDM)

> Date: 2026-04-17

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-085 | Plan line 167 explicitly states "STORY-083 and STORY-084 are independent — reactive behavior extends RADIUS path; Diameter/5G equivalents are future follow-ups if needed." No AC changes, no dependency assumptions changed. ProtocolSelector pattern available as reuse reference for STORY-085 if it ever adds SBA-reactive behaviour. | REPORT ONLY / NO_CHANGE |
| STORY-087 | No impact — STORY-084 has no DB migrations. | REPORT ONLY / NO_CHANGE |
| STORY-088 | No impact — STORY-084 does not touch `internal/policy/`. | REPORT ONLY / NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/stories/test-infra/STORY-084-review.md` | This review report | UPDATED |
| `docs/ARCHITECTURE.md` | Added `sba/` to simulator tree; updated STORY-082/083 → 082/083/084 annotations | UPDATED |
| `docs/USERTEST.md` | Added `## STORY-084:` section with 4 test scenarios | UPDATED |
| `docs/ROUTEMAP.md` | STORY-084 marked DONE; Completed date and step updated; Change Log entry added | UPDATED |
| `docs/brainstorming/decisions.md` | VAL-020..023 already present from gate — no new entries needed | NO_CHANGE |
| `docs/architecture/simulator.md` | Gate already updated (5G SBA section, correct labels, deregister exclusion, prod_guard) — verified | NO_CHANGE |
| `deploy/simulator/config.example.yaml` | Gate already updated (prod_guard: true, sba block, rate: 0.2, no deregister) — verified | NO_CHANGE |
| `docs/GLOSSARY.md` | AUSF, UDM, SUPI, SUCI, 5G-AKA, NSSAI, GUAMI terms already present from STORY-020 era — verified | NO_CHANGE |
| `Makefile` | No new simulator targets added by STORY-084 | NO_CHANGE |
| `CLAUDE.md` | No Docker URL/port changes; no architecture scale bump (no new APIs/tables/screens) | NO_CHANGE |
| `docs/FUTURE.md` | No new opportunities or invalidations identified | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- simulator.md metric tables verified: `endpoint`/`cause` labels correct, result enum `success|error_4xx|error_5xx|timeout|transport` correct.
- simulator.md config field table verified: `prod_guard` (not `prod_guard_disabled`), default `true`, `*bool` pointer semantics documented.
- simulator.md Data Flow diagram verified: deregister exclusion section present and accurate.
- config.example.yaml verified: `prod_guard: true`, `sba:` block present, `rate: 0.2` on turkcell, no deregister references.
- ARCHITECTURE.md simulator tree: **FIXED** — `sba/` sub-dir missing, STORY-082/083 annotations did not include 084.
- ROUTEMAP Tech Debt: confirmed no new D-NNN from STORY-084 (gate had 0 deferrals); D-034 (from STORY-083 review) is unrelated to STORY-084.

## Decision Tracing

- Decisions checked: 4 (VAL-020..023 all tagged STORY-084)
- Orphaned (approved but not applied): 0
- VAL-020: response-bucket enum + single-writer invariant — reflected in `ausf.go classifyStatusCode`, `engine.go runSBASession`, metrics test.
- VAL-021: `ProdGuard *bool` pointer default-true — reflected in `config.go SBADefaults`, config tests `TestSBA_ProdGuardDefaultIsOn`.
- VAL-022: Deregister removal — reflected in `udm.go` (no DeregisterViaUDM), `simulator.md` scope exclusion section.
- VAL-023: EAP_AKA_PRIME rejection at validation — reflected in `validateSBA()` + `TestSBA_ValidationErrors`.
- All 4 decisions verified in code and doc.

## USERTEST Completeness

- Entry exists: YES (added by this review)
- Type: Backend/dev-tool scenarios — unit test commands, SBA session smoke, metrics verification, prod-guard env injection, failover restart

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting STORY-084: 0 (confirmed by STORY-084 plan §Tech Debt section: "No tech debt items target STORY-084 specifically")
- Already ✓ RESOLVED by Gate: 0
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0

## Mock Status

- N/A — simulator is a traffic generator; no `src/mocks/` directory involved.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | `internal/simulator/sba/` package missing from `docs/ARCHITECTURE.md` project structure tree | NON-BLOCKING | FIXED | Gate added the `5G SBA Client` section to `docs/architecture/simulator.md` but did not update `docs/ARCHITECTURE.md` tree. Same gap as STORY-082/083 (picked up in STORY-083 review). Reviewer added `sba/` sub-dir entry and updated the STORY-083 → STORY-082/083/084 annotation on both the `cmd/simulator/` and `internal/simulator/` lines. |
| 2 | STORY-084 missing from `docs/USERTEST.md` | NON-BLOCKING | FIXED | Added `## STORY-084:` section with 4 scenarios: unit/integration test commands, SBA-enabled operator smoke, metrics scrape verification, prod-guard env injection test, and failover kill/restart scenario. |
| 3 | STORY-084 ROUTEMAP step/status stale at Review (should be DONE post-review) | NON-BLOCKING | FIXED | Updated STORY-084 row: Status `[x] DONE`, Step `—`, Completed `2026-04-17`. Added REVIEW Change Log entry. Updated header counter from 2/5 to 3/5 DONE. |

## Project Health

- Stories completed: Phase 10 DONE (24/24); Test Infra 3/5 DONE (080, 082, 083, 084)
- Current phase: Test Infrastructure + Tech Debt Cleanup
- Next story: STORY-085 — Reactive Behavior (approach B): state machine, CoA listener, Session-Timeout respect, reject backoff, bandwidth cap reaction
- Blockers: None
