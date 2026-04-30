# Post-Story Review: STORY-092 — Dynamic IP Allocation pipeline + SEED FIX

> Date: 2026-04-18

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-090 | STORY-092 extended `sba.ServerDeps` and `diameter.ServerDeps` with optional `IPPoolStore` + `SIMStore` wiring, and added an `IPPoolOperations` / `SIMResolver` / `SIMUpdater` / `SIMCache` interface contract inside the SBA package. STORY-090's nested JSONB `adapter_config.{radius,diameter,sba,http,mock}` refactor can now target this consolidated dep surface (same 4 interfaces across RADIUS/Diameter/SBA hot paths). Plan-phase advisor still required for flat→nested migration decision per ROUTEMAP Advisor flag #3. No story-file edits by reviewer per protocol — Amil dispatch will update STORY-090 if needed. | NO_CHANGE (REPORT ONLY) |
| STORY-089 | The minimal Nsmf mock in `internal/aaa/sba/nsmf.go` (2 routes, Create + Release only) is a candidate for absorption into the new `cmd/operator-sim` container that STORY-089 will ship. Advisor confirms: STORY-089 planner can either (a) move the handler verbatim into the operator-sim binary and keep `internal/aaa/sba/nsmf.go` as a stub that dispatches through the adapter, or (b) retire the in-process mock once the operator-sim container is reachable. Interfaces `SIMResolver`/`IPPoolOperations`/`SIMUpdater`/`SIMCache` are stable contracts — either absorption path preserves the test harness. | NO_CHANGE (REPORT ONLY) |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| ROUTEMAP.md | STORY-092 row → DONE 2026-04-18; counter Runtime Alignment 0/3 → 1/3; Change Log entry added citing D-038 integration-level closure + 4 UI screenshots; Active Session line reset to STORY-090; D-038 row note amended to cite `enforcer_nilcache_integration_test.go` (integration-level closure); new D-039 added for pre-existing AUSF/UDM index gap targeting STORY-089 | UPDATED |
| CLAUDE.md | Active Session block: Story STORY-092 → STORY-090; Step stays Plan (new story) | UPDATED |
| docs/architecture/api/_index.md | New section `## 5G SBA — Nsmf Mock (2 endpoints) — STORY-092` with API-304 (POST /nsmf-pdusession/v1/sm-contexts) + API-305 (DELETE /nsmf-pdusession/v1/sm-contexts/{smContextRef}); annotated "minimal mock — Create + Release only"; footer total 241 → 243 intentionally left unchanged (stale counter, out of scope per advisor) | UPDATED |
| docs/architecture/db/_index.md | Appended `### SEED-06: Dynamic IP Reservation` subsection under `## Seed Files` covering seed 003's 13 pools + m2m.water pool + 129/129 reservations | UPDATED |
| docs/architecture/PROTOCOLS.md | Diameter Gx CCA-I flow: added `Framed-IP-Address (AVP 8)` bullet and CCR-T release bullet; 5G SBA section: added `### Nsmf (Session Management Function) — mock` subsection + cross-ref to STORY-089 long-term home | UPDATED |
| docs/architecture/ALGORITHMS.md | IP Allocation §1 header note updated: AllocateIP invoked from RADIUS Access-Accept + Diameter Gx CCA-I + SBA Nsmf CreateSMContext (in addition to existing admin/import paths); `used_addresses` counter D2-A app-level note preserved | UPDATED |
| docs/USERTEST.md | `## STORY-092` section appended: seed-reset idempotency repro, `/settings/ip-pools` capacity smoke, `/sessions` IP column check, SIM detail IP check, D-038 nil-cache DATABASE_URL-gated integration test invocation | UPDATED |
| docs/brainstorming/decisions.md | 3 new entries: DEV-244 (dynamic-alloc + release pipeline — order-of-ops on hot paths), DEV-245 (nil-cache enforcer integration closure + InvalidateIMSI nil-redis guard), DEV-246 (Nsmf mock scope boundaries — Create + Release only, no PATCH/QoS/PCF/UPF) | UPDATED |
| docs/brainstorming/bug-patterns.md | — (per dispatch: skip unless new pattern; gate found only dead-code which was fixed in-gate) | NO_CHANGE |
| docs/ARCHITECTURE.md | `## System Context` / Docker Architecture Nsmf mock endpoint annotation; simulator tree entry for `sba/` already covers STORY-084 — no change; IP allocation column narrative: added one sentence to reflect hot-path writers (RADIUS + Diameter + SBA) in addition to admin/import | UPDATED |
| docs/SCREENS.md | SCR-050 `/sessions` IP column already listed in the mockup (verified at `docs/screens/SCR-050-session-list.md:23`); no change | NO_CHANGE |
| docs/FUTURE.md | Added bullet under `## Future Phase: Digital Twin & Network Simulation` architecture-implications: "STORY-089 operator-SoR simulator will absorb the minimal Nsmf mock shipped in STORY-092 (`internal/aaa/sba/nsmf.go`)" | UPDATED |
| ROUTEMAP.md Tech Debt D-038 | Row note amended to cite `enforcer_nilcache_integration_test.go:196` as integration-level closure (previously note only mentioned unit-level guards) | UPDATED |
| ROUTEMAP.md Tech Debt D-039 | New row added: pre-existing AUSF/UDM/NRF endpoint indexing gap in `docs/architecture/api/_index.md` (STORY-020 never indexed, STORY-092 indexes only its own Nsmf mock). Targeting STORY-089. Reviewer-surfaced. | UPDATED (new) |

## Cross-Doc Consistency

- Contradictions found: 0
- D-038 note on ROUTEMAP Tech Debt row aligned with the integration test file cited in the gate report (`enforcer_nilcache_integration_test.go`); no drift.
- ROUTEMAP header counter (line 6) updated `Runtime Alignment 0/3 → 1/3`.
- Advisor-flag #1 (Wave 1 nil-cache must be INTEGRATION not unit) now verifiable in code via `internal/aaa/radius/enforcer_nilcache_integration_test.go:196` — enforcer literally constructed with nil positions 1 + 5 matching `cmd/argus/main.go:1067`. Gate report cited the evidence, reviewer-level check confirms file exists.

## Decision Tracing

- Decisions checked: 3 planner-phase decisions from plan §"Locked Decisions" (D1-A seed extension, D2-A app-level counter, D3-B Nsmf minimal scope).
  - D1-A: Reflected in `migrations/seed/006_reserve_sim_ips.sql` (extended to cover seed 003's 13 pools + m2m.water; reservation CTE untouched per plan). PASS.
  - D2-A: Reflected in `RecountUsedAddresses` (`internal/store/ippool.go:55-92`) with no new migration; `AllocateIP` / `ReleaseIP` remain the single app-level writers. PASS.
  - D3-B: Reflected in `internal/aaa/sba/nsmf.go` with only `POST /sm-contexts` + `DELETE /sm-contexts/{ref}` mounted at `internal/aaa/sba/server.go:126-127`. No PATCH, no QoS, no PCF, no UPF. PASS.
- DEV-244/DEV-245/DEV-246 added to decisions.md by this review for the reasons above + advisor-flag #1 / #2 / #3 closure traceability.
- Orphaned: 0

## USERTEST Completeness

- Entry exists: YES (added by this review)
- Type: backend — DB-state introspection + UI smoke (existing `/sessions`, `/settings/ip-pools`, `/sims/:id`) + DATABASE_URL-gated D-038 integration regression invocation. Includes copy-paste reproducers for seed-006 idempotency + argus-app nil-cache boot.

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 1 (D-038)
- Already ✓ RESOLVED by Gate: 1 (D-038 row was marked RESOLVED 2026-04-18 in the prior hotfix; STORY-092 adds the integration-level regression test that hard-closes the original reporting gap)
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0
- Reviewer also surfaced D-039 (pre-existing AUSF/UDM/NRF index gap in api/_index.md — untracked prior to this review). Deferred to STORY-089 per advisor-flag #1 (STORY-089 will absorb the SBA mock and can re-sweep the SBA section holistically).

## Mock Status (Frontend-First projects only)

- N/A — Argus is not a Frontend-First project. `src/mocks/` does not exist in this repo.

## Issues

> Every issue MUST have a Resolution. NEVER write an issue without one.

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | ROUTEMAP STORY-092 row still `[~] IN PROGRESS` after gate PASS | NON-BLOCKING | FIXED | Updated row Status `[~] IN PROGRESS` → `[x] DONE`, Step `Plan` → `—`, Completed `—` → `2026-04-18`; counter 0/3 → 1/3 |
| 2 | ROUTEMAP Change Log missing STORY-092 DONE entry | NON-BLOCKING | FIXED | Added full Change Log row with deliverables, D-038 integration closure reference, and 4 UI screenshots as AC-1 evidence |
| 3 | CLAUDE.md Active Session still points to STORY-092 | NON-BLOCKING | FIXED | Bumped Story STORY-092 → STORY-090; Step stays Plan (new story start) |
| 4 | api/_index.md: new Nsmf endpoints unindexed | NON-BLOCKING | FIXED | Added API-304 (POST /nsmf-pdusession/v1/sm-contexts) + API-305 (DELETE /nsmf-pdusession/v1/sm-contexts/{smContextRef}) in new `## 5G SBA — Nsmf Mock` section |
| 5 | api/_index.md: pre-existing AUSF/UDM/NRF index gap (STORY-020 never indexed) | NON-BLOCKING | DEFERRED D-039 | Surfaced during review; deferred to STORY-089 where the SBA section will be re-swept holistically. Reviewer confirms scope dispatch is strict to STORY-092 endpoints. |
| 6 | db/_index.md: Seed Files section only covers SEED-01 + SEED-02 | NON-BLOCKING | FIXED | Added `### SEED-06: Dynamic IP Reservation` subsection. Older seeds 003/004/005 are pre-existing gaps (observation only, not in scope per dispatch). |
| 7 | PROTOCOLS.md Gx section missing Framed-IP-Address flow notes on CCA-I / CCR-T | NON-BLOCKING | FIXED | Added 2 bullets under Gx Message Flow and a cross-reference to `AVPCodeFramedIPAddress = 8` per RFC 7155 §4.4.10.5.1 |
| 8 | PROTOCOLS.md SBA section missing Nsmf mock scope | NON-BLOCKING | FIXED | Added `### Nsmf (Session Management Function) — mock` subsection + cross-reference to STORY-089 long-term home |
| 9 | ALGORITHMS.md IP Allocation §1 did not mention AAA hot-path writers | NON-BLOCKING | FIXED | Added "Invocation Points" note: RADIUS Access-Accept / Diameter Gx CCA-I / SBA Nsmf CreateSMContext are now hot-path writers in addition to admin/import |
| 10 | USERTEST.md missing STORY-092 section | NON-BLOCKING | FIXED | Added section with 5 reproducers (seed idempotency, ip-pools capacity, sessions IP, SIM detail IP, D-038 integration invocation) |
| 11 | decisions.md missing STORY-092 entries | NON-BLOCKING | FIXED | Added DEV-244 / DEV-245 / DEV-246 (dynamic alloc pipeline / nil-cache integration closure + nil-redis guard / Nsmf minimal scope) |
| 12 | ARCHITECTURE.md Docker diagram labelled `5G SBA` but did not surface the Nsmf mock endpoint | NON-BLOCKING | FIXED | Added one-line annotation + updated IP-allocation narrative to reflect hot-path writers |
| 13 | FUTURE.md did not cross-reference STORY-089 absorption of the Nsmf mock | NON-BLOCKING | FIXED | Added bullet under Digital Twin / Simulation section |
| 14 | bug-patterns.md | NON-BLOCKING | NO_CHANGE | Per dispatch — gate found only dead code (fixed in-gate); no new architectural bug pattern warrants PAT-NNN entry |

## Project Health

- Stories completed in Runtime Alignment: 1/3 (STORY-092 DONE 2026-04-18; STORY-090 + STORY-089 PENDING)
- Current phase: Runtime Alignment [IN PROGRESS]
- Next story: STORY-090 (Multi-protocol operator adapter refactor)
- Blockers: None. STORY-090 can begin Plan step; ROUTEMAP Advisor flags #3 + #4 apply.
