# Post-Story Review: STORY-085 — Simulator Reactive Behavior (Approach B)

> Date: 2026-04-17

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-087 | No simulator dependency. STORY-087 targets D-032 (pre-069 sims-compat FK shim) — pure DB migration work. STORY-085 adds no new tables, no migration changes. | REPORT ONLY / NO_CHANGE |
| STORY-088 | No relation. STORY-088 targets D-033 (`internal/policy/dryrun/service_test.go:333` go-vet non-pointer Unmarshal). STORY-085 does not touch the policy package. | REPORT ONLY / NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/stories/test-infra/STORY-085-review.md` | This review report | UPDATED |
| `docs/ARCHITECTURE.md` | Updated `cmd/simulator/` and `internal/simulator/` annotations STORY-082/083/084 → 082/083/084/085; added `reactive/` sub-dir entry; updated metrics comment to include `simulator_reactive_*`; updated config/ comment to include Reactive defaults | UPDATED |
| `docs/architecture/simulator.md` | Updated header package list to include `sba` + `reactive`; updated introduced-in line to cover STORY-084/085 | UPDATED |
| `docs/USERTEST.md` | Added `## STORY-085:` section with 7 test scenarios | UPDATED |
| `docs/ROUTEMAP.md` | STORY-085 marked DONE; counter updated 3/5 → 5/5; Change Log DONE + REVIEW entries added | UPDATED |
| `docs/brainstorming/bug-patterns.md` | Added PAT-002 (composite-timer pattern for externally-mutable deadlines) | UPDATED |
| `docs/brainstorming/decisions.md` | VAL-024..027 already present from gate — verified | NO_CHANGE |
| `deploy/simulator/config.example.yaml` | Gate already updated (`nas_ip: argus-simulator` all 3 operators, reactive block) — verified | NO_CHANGE |
| `docs/GLOSSARY.md` | Existing terms cover STORY-085 domain: CoA/DM (lines 13-14), Session-Timeout / Hard Timeout (lines 179-180). New simulator-internal terms (BackingOff state, RejectTracker, deadlineTimer) are implementation details not domain vocabulary — deliberately excluded per reviewer judgement | NO_CHANGE |
| `Makefile` | No new targets added by STORY-085 | NO_CHANGE |
| `CLAUDE.md` | No Docker URL/port changes; no architecture scale bump (no new APIs/tables/screens — simulator is dev-tool only) | NO_CHANGE |
| `docs/FUTURE.md` | No new opportunities or invalidations identified; D-035/D-036 are tech-debt upgrades not FUTURE.md extension points | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- `simulator.md` §Reactive Behavior metrics table verified: real label values enumerated (`kind` ∈ {dm, coa, unknown}; `result` ∈ {ack, unknown_session, bad_secret, malformed, unsupported}; `nak` absent and explained). Matches `metrics.go:163-165`.
- `simulator.md` §CoA/DM routing verified: hostname-based flow described as default posture; no IP addresses; explains AVP omission per RFC 2865 §5.4.
- `simulator.md` §Configuration table verified: `reject_max_retries_per_hour` default 5 (matches gate F-A1 fix in `config.go:429-437`).
- `config.example.yaml` verified: `nas_ip: argus-simulator` on all three operators (turkcell, vodafone, turk_telekom); reactive block present; `reject_max_retries_per_hour: 5`; `coa_listener.shared_secret: ""` (inherits from `argus.radius_shared_secret`).
- `ARCHITECTURE.md` simulator tree: **FIXED** — STORY-085 package and `reactive/` sub-dir were missing; also caught stale STORY-083/084 annotation that didn't reflect 084.
- `simulator.md` header: **FIXED** — package list omitted `sba` and `reactive`; introduced-in line omitted STORY-084 and STORY-085.
- ROUTEMAP Tech Debt: D-035 (bandwidth-cap RFC bug) + D-036 (engine integration tests) both present and `[ ] PENDING` — verified.
- VAL-024..027 in `decisions.md` lines 288-291 — all present, correct, tagged ACCEPTED.

## Decision Tracing

- Decisions checked: 4 (VAL-024..027 all tagged STORY-085 Gate)
- Orphaned (approved but not applied): 0
- VAL-024: `RejectMaxRetriesPerHour` zero-value defaults to 5 — reflected in `config.go validateReactive()` and `TestReactive_MaxRetriesZero_DefaultsTo5`.
- VAL-025: `nas_ip: argus-simulator` on all 3 operators — reflected in `config.example.yaml` lines 80, 91, 96 and `simulator.md` §CoA/DM routing.
- VAL-026: CoA handler `CauseCoADeadline` only when deadline shortens — reflected in `listener.go:170-182` and `TestReactive_CoAUpdatesDeadline_EndToEnd`.
- VAL-027: Composite `deadlineTimer` in engine interim loop — reflected in `engine.go:314-376` and `TestSessionTimeout_SubIntervalDeadlineFires`.
- All 4 decisions verified in code and doc.

## USERTEST Completeness

- Entry exists: YES (added by this review)
- Type: Backend/dev-tool scenarios — reactive toggle, metrics scrape, Session-Timeout unit test, reject-backoff unit test, DM round-trip via API, CoA integration test, listener socket probe, clean shutdown

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting STORY-085: 0 (no pre-existing D-NNN had STORY-085 as target)
- Already ✓ RESOLVED by Gate: 0
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0

Note: D-035 and D-036 were CREATED by this story's gate (not targeted at it). Both correctly appear in ROUTEMAP as `[ ] PENDING` targeting POST-GA.

## Mock Status

- N/A — simulator is a traffic generator; no `src/mocks/` directory involved.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | `internal/simulator/reactive/` package and STORY-085 annotation missing from `docs/ARCHITECTURE.md` project structure tree | NON-BLOCKING | FIXED | Gate updated `docs/architecture/simulator.md` but did not update `docs/ARCHITECTURE.md` tree. Reviewer added `reactive/` sub-dir entry; updated `sba/` from `└──` to `├──`; updated STORY-082/083/084 → 082/083/084/085 on both `cmd/simulator/` and `internal/simulator/` lines; updated config/ and metrics/ comments to include Reactive. |
| 2 | `docs/architecture/simulator.md` header stale — package list and story list omitted STORY-084 (sba) and STORY-085 (reactive) | NON-BLOCKING | FIXED | Reviewer updated the `> Implementation packages:` line to include `sba` and `reactive`; updated the introduced-in sentence to cover STORY-082 through STORY-085. Gate had rewritten internal sections but left the 4-line header intact. |
| 3 | STORY-085 missing from `docs/USERTEST.md` | NON-BLOCKING | FIXED | Added `## STORY-085:` section with 7 manual test scenarios covering all 9 ACs. |
| 4 | STORY-085 ROUTEMAP row stale (`[~] IN PROGRESS \| Review`) and header counter wrong (`3/5`) | NON-BLOCKING | FIXED | Updated STORY-085 row to `[x] DONE \| — \| 2026-04-17`. Fixed header counter: `3/5 DONE (080, 082, 083, 084)` → `5/5 DONE (080, 082, 083, 084, 085)`. Added DONE + REVIEW Change Log entries. |
| 5 | PAT-002 pattern not captured in `docs/brainstorming/bug-patterns.md` | NON-BLOCKING | FIXED | Gate F-A6 (VAL-027) describes a generalisable anti-pattern: single-clock polling loop that misses dynamically-shortened deadlines. Added PAT-002 with root cause, prevention rule (composite `select` with `deadlineTimer`), test reference, and affected-scope annotation. |
| 6 | GLOSSARY decision: new simulator-reactive terms not documented | NON-BLOCKING | FIXED (explicit exclusion) | Checked: CoA (line 13), DM (line 14), Session-Timeout / Hard Timeout (lines 179-180) already cover the domain concepts used by STORY-085. Simulator-internal terms (BackingOff state, RejectTracker, deadlineTimer, Suspended state) are implementation details of a dev/test tool — not domain vocabulary that end-users or operators encounter. Deliberately not added to GLOSSARY. Decision recorded here. |

## Project Health

- Stories completed: Phase 10 DONE (24/24); Test Infra 5/5 DONE (080, 082, 083, 084, 085)
- Current phase: Test Infrastructure + Tech Debt Cleanup
- Next story: STORY-087 — [TECH-DEBT] D-032: Pre-069 sims-compat shim or no-transaction safeguard for `20260413000001_story_069_schema.up.sql` FK-to-partitioned-parent defect
- Blockers: None
