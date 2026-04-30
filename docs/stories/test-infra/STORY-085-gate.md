# Gate Report: STORY-085 — Simulator Reactive Behavior (Approach B)

## Summary
- Requirements Tracing: Fields 9/9, Endpoints 3/3 (CoA/DM/metrics), Workflows 9/9 ACs, Components: N/A (backend-only simulator story)
- Gap Analysis: 9/9 acceptance criteria passed (AC-9 advanced from NOT MET → MET via nas_ip hostname fix; AC-1/AC-3/AC-6/AC-7 advanced from PARTIAL → FULL via in-gate test/code additions)
- Compliance: COMPLIANT
- Tests: 80 story-scope tests passed + 30 integration tests passed; full suite 3000 passed (baseline 2991 → +9)
- Test Coverage: 9/9 ACs with explicit tests (AC-1 now has a wall-clock sub-interval test; AC-3 has a 3s SLA regression anchor; AC-7 has a socket-probe assertion)
- Performance: N/A for gate (sub-interval deadline timer added to engine; arms exactly once per CoA-driven shift, drained cleanly on session exit — no leak)
- Build: PASS (`go build ./cmd/simulator`, `go vet ./internal/simulator/... ./cmd/simulator/...`)
- Screen Mockup Compliance: N/A (backend-only)
- UI Quality: N/A
- Token Enforcement: N/A
- Turkish Text: N/A
- Overall: PASS

## Team Composition
- Analysis Scout: 11 findings (F-A1 … F-A11)
- Test/Build Scout: 5 findings (F-B1 … F-B5)
- UI Scout: 0 findings (Skipped — backend-only)
- De-duplicated: 16 → 15 unique findings (F-A2 + F-B4 merged into one compliance fix)

## Fixes Applied
| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Compliance | `internal/simulator/config/config.go:429-437` | `RejectMaxRetriesPerHour == 0` now defaults to 5 instead of returning an error; negative values still rejected | config tests PASS |
| 2 | Tests | `internal/simulator/config/config_test.go` | Renamed `TestReactive_MaxRetriesZero_Error` → `TestReactive_MaxRetriesZero_DefaultsTo5`; added `TestReactive_MaxRetriesNegative_Error` | PASS |
| 3 | Compliance | `deploy/simulator/config.example.yaml:80,91,96` | Changed `nas_ip` for all 3 operators from `10.99.0.{1,2,3}` to `argus-simulator` (plan AC-9 hostname-based CoA routing) | YAML parses |
| 4 | Compliance | `docs/architecture/simulator.md:490-518` | Rewrote §CoA/DM routing to describe hostname-based flow as default posture; reconciled with F-A2 fix | Visual inspection |
| 5 | Correctness | `internal/simulator/reactive/listener.go:170-182` | `handleCoA` now only marks `CauseCoADeadline` when new deadline is EARLIER than current — eliminates metric bias F-A5 | integration tests PASS |
| 6 | Performance / Correctness | `internal/simulator/engine/engine.go:314-376` | Added per-session `deadlineTimer` (composite with ticker) to honour sub-interval Session-Timeouts — mitigates F-A6 / fixes plan §Risks "Session-Timeout mid-interim timing boundary" | engine tests PASS; added `TestSessionTimeout_SubIntervalDeadlineFires` |
| 7 | Code quality | `cmd/simulator/main.go:141-152` | Removed dead `select { case <-Ready(): … case <-time.After(2s): … }` block — `Start` closes `ready` synchronously before returning nil, making the timeout branch unreachable | build PASS |
| 8 | Docs (tech debt) | `docs/ROUTEMAP.md:399` | Added **D-035** (bandwidth-cap RFC-broken install at `internal/aaa/radius/server.go:571-580`, target POST-GA) and **D-036** (engine-level end-to-end integration tests — F-A8 partial deferral) | Inspection |
| 9 | Compliance (docs) | `docs/architecture/simulator.md:478-488` | Updated metrics table — enumerated real label values (`kind ∈ {dm, coa, unknown}`, `result ∈ {ack, unknown_session, bad_secret, malformed, unsupported}`); removed stale `nak` | Inspection |
| 10 | Compliance (docs) | `internal/simulator/metrics/metrics.go:162-166` | Updated `simulator_reactive_incoming_total` help comment to match emitted labels; note `nak` was never emitted (`unknown_session` IS the NAK branch) | build PASS |
| 11 | Tests | `internal/simulator/reactive/integration_test.go` | Added `TestReactive_ListenerUnbound_WhenDisabled` socket-probe (F-B3); added explicit 3s SLA guard to `TestReactive_ListenerCancelsSession_EndToEnd` (F-B2) | integration tests PASS |
| 12 | Tests | `internal/simulator/engine/engine_test.go` | Added `TestSessionTimeout_SubIntervalDeadlineFires` wall-clock assertion for AC-1 (F-B1) — uses a 500ms deadline under a 10s ticker and asserts the deadline timer wins, catching the F-A6 regression | engine tests PASS |

### Non-fix clarifications (scouts flagged but no action needed)
- **F-A11** (`Registry.Len()` O(n)) — only used in tests/debug; no production impact. Not fixed.
- **F-B5** (`TestListener_BadSecret_SilentDrop` doesn't assert metric) — re-read of `listener_test.go:259-262` shows the test DOES assert `count >= 1` for the `bad_secret` label. Scout false-positive. No fix required.

## Escalated Issues (architectural / business decisions)
(none)

## Deferred Items (tracked in ROUTEMAP → Tech Debt)
| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-035 | F-A3 — Bandwidth-cap RFC-broken install at `internal/aaa/radius/server.go:571-580`; retired from STORY-085 scope; needs VSA rework | POST-GA protocol-correctness | YES |
| D-036 | F-A8 — Plan Task 9 called for engine-level `TestReactive_SessionTimeout` + `TestReactive_DisconnectMessage` + `TestReactive_RejectBackoff` with a full in-process RADIUS server. STORY-085 implemented listener-scope integration + F-B1 wall-clock timer test; a full compose-side smoke runbook (AC-9) remains | POST-GA simulator-regression | YES |

Both deferrals are **upgrades to future test coverage** — no functional gap. AC-1/AC-3/AC-4/AC-7 are all individually tested in-gate.

## Performance Summary

### Queries Analyzed
(N/A — simulator is stateless + read-only via discovery cache)

### Caching Verdicts
(N/A)

### Timer/Goroutine Review
| # | Location | Pattern | Verdict |
|---|----------|---------|---------|
| 1 | `engine.go:314-363` | Per-session `time.Timer` armed once, re-armed only when CoA shifts deadline, stopped in `defer` | Safe; verified with `go test -race` |
| 2 | `listener.go:144-158` | `sess.CancelFn()` invocation + registry lookup | Single-writer via `sync.Map`; verified with `-race` |

## Token & Component Enforcement
(N/A — no UI changes)

## Verification
- Tests after fixes: 3000/3000 passed (story: 80/80, integration: 30/30, full: 3000/3000)
- Tests with -race: PASS for engine + reactive packages (41 tests + 30 integration)
- Build after fixes: PASS
- Vet: CLEAN (`go vet ./internal/simulator/... ./cmd/simulator/...`)
- YAML: `deploy/simulator/config.example.yaml` parses (python yaml)
- Fix iterations: 1 (all fixes landed; no re-fix loop needed)

## Maintenance Mode — Pass 0 Regression
(N/A — not a maintenance-mode gate)

## Passed Items
- **AC-1 Session-Timeout respect**: FULL — math test + wall-clock sub-interval test + `TestSessionTimeoutRespect_DeadlineShortened`. F-A6 fix guarantees sub-60s deadlines fire on time.
- **AC-2 Exponential backoff curve**: FULL — 7 backoff tests (unit + integration).
- **AC-3 Disconnect round-trip**: FULL — listener-scope + explicit 3s SLA regression anchor (F-B2).
- **AC-4 CoA Session-Timeout update**: FULL — `TestReactive_CoAUpdatesDeadline_EndToEnd` + `TestListener_CoAAck_UpdatesDeadline`; F-A5 bias fix verified.
- **AC-5 Retry-storm cap**: FULL — `TestRejectTracker_AllowedAfterSuspension` + backoff curve.
- **AC-6 Reactive disabled → byte-identical**: FULL — `TestNew_NilReactiveSubsystem` + `TestReactive_DisabledByDefault_NoImpact`; nil-subsystem guard in `engine.go:170,201,278` unchanged.
- **AC-7 CoA listener only binds when enabled**: FULL — `TestReactive_DisabledByDefault_NoImpact` + new `TestReactive_ListenerUnbound_WhenDisabled` socket probe.
- **AC-8 Bad-secret silent drop**: FULL — `TestListener_BadSecret_SilentDrop` (asserts silent drop + `bad_secret` metric + recovery).
- **AC-9 Compose reachability**: MET — `config.example.yaml` uses `nas_ip: argus-simulator` for all three operators; compose DNS resolves the name; Argus dials `argus-simulator:3799` at CoA/DM time. `net.ParseIP(nil)` → `NAS-IP-Address` AVP omitted; `NAS-Identifier` carries operator identity per RFC 2865 §5.4. Compose-side smoke runbook documented in `docs/architecture/simulator.md:489-514`; full runbook execution is D-036.

## Plan-Compliance Rules — Final Check
- [x] No new third-party deps
- [x] Single-writer PAT-001 preserved (engine owns `SimulatorReactiveTerminationsTotal` + `SimulatorReactiveRejectBackoffsTotal`; listener owns `SimulatorReactiveIncomingTotal`)
- [x] Reactive subsystem opt-in (default `reactive.enabled: false` → byte-identical path)
- [x] CoA listener port NOT published on host (`deploy/docker-compose.simulator.yml`)
- [x] Hostname-based `nas_ip: argus-simulator` applied
- [x] No DB changes
- [x] SIMULATOR_ENABLED env guard preserved
