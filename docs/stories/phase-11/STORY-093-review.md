# Post-Story Review: STORY-093 — IMEI Capture (RADIUS + Diameter S6a + 5G SBA)

> Date: 2026-05-01

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-094 | `session.Session.IMEI`/`SoftwareVersion` fields (Gate F-A2 fix) are the contract the STORY-094 S6a enricher reads. D-182 (Diameter listener caller-less) targets STORY-094. D-183 (PEIRaw retention) targets STORY-094/STORY-097. D-184 (1M-SIM bench) targets STORY-094. Handoff notes added to spec. | UPDATED |
| STORY-095 | Binding model established by STORY-093+094 chain. No direct field-level dependency on STORY-093 captures. | NO_CHANGE |
| STORY-096 | Enforcement engine reads `SessionContext.IMEI` populated by STORY-093. No spec drift; dependency already stated in STORY-096 "Blocked by STORY-094" chain. | NO_CHANGE |
| STORY-097 | D-183 (PEIRaw) is routed here as secondary target. Depends on `SessionContext.IMEI` from STORY-093 (AC-1 history pipeline). Handoff note added noting Diameter S6a listener wiring deferred to STORY-094. | UPDATED |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/brainstorming/decisions.md` | VAL-039, VAL-040, VAL-041 verified present (written by Gate Lead during fix). No new entries needed. | NO_CHANGE (verified) |
| `docs/GLOSSARY.md` | Added 3 missing terms: `Terminal-Information AVP`, `Software-Version AVP`, `3GPP-IMEISV VSA`. Existing IMEI, IMEISV, PEI, TAC, EIR already present. | UPDATED |
| `docs/USERTEST.md` | Added `## STORY-093:` section with 7 backend test scenarios (UT-093-01..07): RADIUS valid + malformed VSA, 5G SBA AUSF + UDM PEI capture, 5G SBA malformed counter, Diameter parser unit test, regression smoke. | UPDATED |
| `docs/brainstorming/bug-patterns.md` | Added PAT-017 RECURRENCE [STORY-093 Gate F-A1]: `MetricsReg` threaded into `ServerDeps`/`NewServer` but not forwarded to inner `NewAUSFHandler`/`NewUDMHandler` — nil-safe helper masked silent no-op. Cross-references PAT-017 original + PAT-011. | UPDATED |
| `docs/architecture/PROTOCOLS.md` | Gate Lead amended §SessionContext Population to document both flat (STORY-093 current) and nested (STORY-094 forward) shapes. Verified correct. | NO_CHANGE (verified, Gate-written) |
| `docs/stories/phase-11/STORY-094-sim-device-binding-model.md` | Added `## STORY-093 Handoff Notes` section: session.Session contract, D-182 Diameter listener wiring instruction, D-183 PEIRaw evaluation point, D-184 bench obligation. | UPDATED |
| `docs/stories/phase-11/STORY-097-imei-change-detection.md` | Added `## STORY-093 Handoff Notes` section: D-183 PEIRaw secondary target, SessionContext.IMEI Diameter coverage gap awareness. | UPDATED |
| `docs/ARCHITECTURE.md` | Backend-only story; no new services, ports, or components. | NO_CHANGE |
| `docs/SCREENS.md` | No new screens. SCR-021f and SCR-050 unchanged; they consume IMEI starting STORY-094/097. | NO_CHANGE |
| `docs/FRONTEND.md` | No frontend changes. | NO_CHANGE |
| `docs/FUTURE.md` | No new future extension points surfaced. EIR S13/N17 integration already documented as OOS. | NO_CHANGE |
| `Makefile` | No new services or scripts. | NO_CHANGE |
| `CLAUDE.md` | No port or URL changes. | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- PROTOCOLS.md §SessionContext Population now documents both flat and forward shapes — consistent with Gate Fix #9 (VAL-041).
- `internal/policy/dsl/evaluator.go` flat `IMEI`/`SoftwareVersion` fields match PROTOCOLS.md `STORY-093 (current)` block.
- `internal/aaa/session/session.go` `IMEI`/`SoftwareVersion` fields match gate fix #6 description and AC-9 invariant (in-memory + Redis only, zero audit-shape change).
- `docs/architecture/api/_index.md` endpoint count unchanged (no new external endpoints in STORY-093).
- `docs/architecture/db/_index.md` table count unchanged (no new tables or columns — STORY-093 is in-memory + Redis only).
- `docs/architecture/CONFIG.md`: no new env vars. `docs/architecture/ERROR_CODES.md`: no new error codes.
- ROUTEMAP D-182/D-183/D-184 rows confirmed present at lines 852–854 with correct target stories and OPEN status.

## Decision Tracing

- Decisions checked: 3 (VAL-039, VAL-040, VAL-041)
- Orphaned (approved but not applied): 0
- VAL-039 (constructor-level registry injection via `ServerDeps.MetricsReg`): reflected in `internal/aaa/sba/server.go` + `cmd/argus/main.go:1350-1358`. PASS.
- VAL-040 (`session.Session` struct extension over per-request context-value stash; AC-9 audit shape preserved): reflected in `internal/aaa/session/session.go:89-90` + `git diff -- internal/store/` empty + `git diff -- internal/audit/` empty. PASS.
- VAL-041 (PROTOCOLS.md flat + forward SessionContext shapes): reflected in PROTOCOLS.md §SessionContext Population. PASS.
- DEV-411 (all three protocols in single story, gated by ADR-004): STORY-093 implements all three parsers + wires RADIUS + 5G SBA; Diameter listener deferred per ADR-004 §Out-of-Scope (D-182). PASS.

## USERTEST Completeness

- Entry exists: YES (`## STORY-093:` section appended to `docs/USERTEST.md`)
- Type: Backend test scenarios (7 scenarios covering RADIUS, 5G SBA, Diameter parser, regression smoke)
- ui_story: NO — backend-only story per dispatch; no UI walkthrough required.

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 0 (D-182/D-183/D-184 were created BY this story's Gate, not targeting it)
- Already ✓ RESOLVED by Gate: 0 (these are forward-looking debts)
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0

Note: D-182/D-183/D-184 were routed by Gate Lead during fix. All three confirmed present in ROUTEMAP with correct target stories (STORY-094 / STORY-097). Status: OPEN — correct, as STORY-094 has not yet run.

## Mock Status

N/A — `maintenance_mode: NO` + backend-only story. No mock files to retire (STORY-093 is AAA hot-path; no frontend mocks involved).

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | STORY-094 spec had zero references to `session.Session.IMEI` contract, D-182 (Diameter listener deferral), D-183 (PEIRaw), or D-184 (bench obligation). STORY-094 developer would encounter silent spec gap. | NON-BLOCKING | FIXED | Added `## STORY-093 Handoff Notes` section to `STORY-094-sim-device-binding-model.md` with explicit contract + D-182/183/184 instructions. |
| 2 | STORY-097 spec had zero references to D-183 (PEIRaw secondary target) and no awareness that Diameter S6a listener is deferred. | NON-BLOCKING | FIXED | Added `## STORY-093 Handoff Notes` section to `STORY-097-imei-change-detection.md`. |
| 3 | GLOSSARY missing three STORY-093-introduced protocol terms: `Terminal-Information AVP` (350), `Software-Version AVP` (1403), `3GPP-IMEISV VSA` (vendor 10415, attr 20). | NON-BLOCKING | FIXED | Added all three entries to `docs/GLOSSARY.md` after the `TAC` row with full spec references. |
| 4 | USERTEST.md had no `## STORY-093:` section — backend story without test scenarios note. | NON-BLOCKING | FIXED | Appended 7 backend test scenarios (UT-093-01..07) to `docs/USERTEST.md`. |
| 5 | bug-patterns.md had no annotation capturing PAT-017 shape recurrence: `MetricsReg` plumbed into `ServerDeps` but not forwarded to inner handler constructors — nil-safe helper masked the no-op silently. | NON-BLOCKING | FIXED | Added `PAT-017 RECURRENCE [STORY-093 Gate F-A1]` entry to `docs/brainstorming/bug-patterns.md`. |

## Project Health

- Stories completed: ~13 DONE + STORY-093 = ~14 (Phase 11 in progress; full count across all phases)
- Current phase: Phase 11 — Enterprise Readiness Pack (STORY-093..098)
- Next story: STORY-094 (SIM-Device Binding Model + Policy DSL Extension)
- Blockers: None. STORY-093 Gate PASS. D-182/D-183/D-184 routed with target stories. Full test suite 3841/3841 green.
