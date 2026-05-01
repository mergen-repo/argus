# Gate Report: STORY-093 — IMEI Capture

## Summary

- **Requirements Tracing:** Fields 2/2 (`SessionContext.IMEI`, `SessionContext.SoftwareVersion`), Endpoints N/A (backend-only AAA hot path), Workflows 3/3 (RADIUS auth-path VSA, Diameter S6a Terminal-Information AVP parser, 5G SBA PEI on `Nausf`/`Nudm`), Components 0/0 (no UI).
- **Gap Analysis:** 10/10 acceptance criteria pass post-fix (AC-1..AC-10). AC-3 narrowed scope (parser-only, listener deferred to STORY-094) and AC-10 perf evidence via microbench ratio acknowledged via D-182 / D-184.
- **Compliance:** COMPLIANT.
- **Tests:** 3841/3841 full suite pass (was 3838 pre-fix; +3 new regression tests for F-A1 / F-A2). Story-scoped: 24/24 pass (21 pre-existing STORY-093 tests + 3 new gate-fix regressions).
- **Test Coverage:** AC-6 negative tests present (RADIUS malformed BCD, Diameter malformed grouped AVP, 5G PEI bad digits) + new AUSF + UDM malformed-PEI counter regression tests cover the wire-handler surface (previously only the parser was exercised).
- **Performance:** 1 issue found (F-A7 perf-rig), 0 fixed in code (deferred D-184 to STORY-094 1M-SIM bench). Microbench evidence still strong: aggregate 169 ns/op against 200 µs target = 1183× margin.
- **Build:** PASS (`go build ./...` clean).
- **Vet:** PASS (`go vet ./...` clean).
- **AC-9 audit shape:** `git diff -- internal/audit/` empty AND `git diff -- internal/store/` empty — DB schema and audit payload preserved.
- **Overall:** PASS.

## Team Composition

- Analysis Scout: 7 findings (F-A1..F-A7)
- Test/Build Scout: 0 findings (all tests/build/bench green)
- UI Scout: SKIPPED (backend-only story per dispatch)
- De-duplicated: 7 → 7 (no overlap)

## Fixes Applied

| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Build / labels | `internal/observability/metrics/metrics.go:101,328,594` | Harmonized `IMEICaptureParseErrorsTotal` protocol-label set to canonical `radius \| diameter_s6a \| 5g_sba` across struct comment + Prom Help + helper docstring | `go vet` clean, full test suite passes |
| 2 | Compliance / metrics wiring | `internal/aaa/sba/ausf.go` (added `reg *obsmetrics.Registry`, threaded into `NewAUSFHandler`, replaced `nil` at `ParsePEI` call site) | F-A1 — production AUSF path now increments `argus_imei_capture_parse_errors_total{protocol="5g_sba"}` on malformed PEI | `TestAUSFAuthenticationInitiation_PEIMalformed_CounterIncrements` PASS |
| 3 | Compliance / metrics wiring | `internal/aaa/sba/udm.go` (same pattern as #2) | F-A1 — production UDM Registration path now increments the counter on malformed PEI | `TestUDMRegistration_PEIMalformed_CounterIncrements` PASS |
| 4 | Compliance / DI surface | `internal/aaa/sba/server.go` (`ServerDeps.MetricsReg` new field, threaded through `NewServer`) + `cmd/argus/main.go:1350-1358` (production wires `MetricsReg: metricsReg`) | F-A1 — single source of truth for the registry; nil-safe so optional in tests | `go build ./...` PASS |
| 5 | Test fan-out | `internal/aaa/sba/nsmf_integration_test.go:183-184`, `internal/simulator/sba/{ausf,udm,integration}_test.go` (5 call sites) | Pass `nil` registry to new constructor signature; tests stay green via nil-safe counter helper | full suite PASS |
| 6 | Compliance / Session propagation | `internal/aaa/session/session.go` (Session struct extended with `IMEI` + `SoftwareVersion` strings, JSON-tagged `omitempty`, comment marks AC-9 invariant: in-memory + Redis only, NOT in `CreateRadiusSessionParams`, NOT in audit) | F-A2 — captured device identity now reaches downstream consumers | `git diff -- internal/store/` empty + `git diff -- internal/audit/` empty + new test passes |
| 7 | Compliance / Session populate | `internal/aaa/sba/ausf.go:220` (HandleConfirmation literal) + `internal/aaa/sba/udm.go:165` (HandleRegistration literal) | F-A2 — populate the two SBA `&session.Session{}` literals with parsed IMEI/SV. Maintains PAT-006 audit. | both regression tests PASS |
| 8 | Test | `internal/aaa/sba/imei_handler_test.go` (NEW — 3 tests) | F-A1 + F-A2 regression coverage at the wire-handler layer (was only at parser layer pre-fix) | 3/3 PASS |
| 9 | Documentation | `docs/architecture/PROTOCOLS.md` §SessionContext Population | F-A6 — amend doc to describe both the *flat* shape that STORY-093 ships (`SessionContext.IMEI` / `SoftwareVersion`) and the forward-looking nested `SessionContext.Device { ... }` form that STORY-094 will introduce | doc-only edit; PROTOCOLS.md grep verifies both shapes documented |

## Escalated Issues

NONE.

## Deferred Items (tracked in ROUTEMAP → Tech Debt)

| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|--------------|---------------------|
| D-182 | F-A3 — `internal/aaa/diameter/imei.go` `ExtractTerminalInformation` ships parser-only with zero production callers. PAT-026 inverse risk (parser without consumer). Plan T3 narrowed scope per ADR-004 §Out-of-Scope and DEV-409. STORY-094 S6a enricher is the intended caller. | STORY-094 | YES |
| D-183 | F-A5 — PROTOCOLS.md:556 forensic-retention contract for non-3GPP PEI forms (`mac-` / `eui64-`) unimplemented; `internal/aaa/sba/imei.go:73-74` returns `("", "", true)`; no `SessionContext.PEIRaw` field. Forward gap, not a regression. | STORY-094 / STORY-097 | YES |
| D-184 | F-A7 — AC-10 1M-SIM bench evidence currently substituted with parser microbench ratio (1183× margin). Real 1M-SIM rig out of CI. Schedule literal AC-10 run with STORY-094 binding pre-check perf budget. | STORY-094 | YES |

## Performance Summary

### Queries Analyzed
N/A — STORY-093 is parser-only on the hot path. No new SQL.

### Caching Verdicts
N/A — captured device identity rides on the existing Redis session blob (no new keys, no new TTLs). Redis-cached `Session.IMEI` / `SoftwareVersion` survive Acct-Interim updates by virtue of the existing JSON marshal/unmarshal round-trip in `Manager.UpdateCounters`.

### Microbench Evidence (story plan §AC-10 Perf Addendum)

| Parser | ns/op | B/op | allocs/op | Source |
|--------|------:|-----:|----------:|--------|
| `BenchmarkExtract3GPPIMEISV_RADIUS-16` | 43.05 | 56 | 2 | `internal/aaa/radius/imei_test.go` |
| `BenchmarkExtractTerminalInformation_S6a-16` | 104.4 | 156 | 8 | `internal/aaa/diameter/imei_test.go` |
| `BenchmarkParsePEI_5G-16` | 22.40 | 2 | 1 | `internal/aaa/sba/imei_test.go` |
| **Aggregate** | **~169** | — | — | — |
| **AC-10 budget** | 200,000 | — | — | spec |
| **Margin** | 1183× | — | — | — |

Real 1M-SIM rig run scheduled with STORY-094 (D-184).

## Token & Component Enforcement (UI stories)

N/A — backend-only story.

## Verification

- Tests after fixes: 3841/3841 PASS (109 packages); 0 FAIL, 0 SKIP, 0 RACE
- New tests added: 3 (`TestAUSFAuthenticationInitiation_PEIMalformed_CounterIncrements`, `TestUDMRegistration_PEIMalformed_CounterIncrements`, `TestUDMRegistration_PEIPopulatesSession`)
- Build after fixes: PASS (`go build ./...`)
- `go vet ./...`: clean
- `grep -rn 'ParsePEI' internal/aaa/sba/*.go | grep -v _test`: both call sites now pass `h.reg` (no more `nil`)
- `grep -rn 'protocol="' internal/aaa/ internal/observability/`: unified label set verified (`radius` | `diameter_s6a` | `5g_sba`)
- `git diff -- internal/audit/`: 0 lines (AC-9 preserved)
- `git diff -- internal/store/`: 0 lines (DB schema unchanged)
- All 5 test call sites for `NewAUSFHandler` / `NewUDMHandler` now match the 4-arg signature (pass `nil` for the registry; nil-safe)
- Fix iterations: 1 (no second pass needed; first-pass green)

## Maintenance Mode — Pass 0 Regression

N/A — `maintenance_mode: NO` per dispatch.

## Passed Items

- AC-1 ✓ — `dsl.SessionContext.IMEI` + `SoftwareVersion` exported, zero-value safe (existing).
- AC-2 ✓ — RADIUS `Extract3GPPIMEISV` wired at server.go:492 + :653 with `s.reg` (no change required).
- AC-3 ✓ (narrowed) — Diameter S6a parser + 5/5 unit tests; listener deferred per ADR-004 to STORY-094 (D-182).
- AC-4 ✓ — 5G SBA `ParsePEI` wired at AUSF Authentication + UDM Registration; both now thread `h.reg` (post-fix).
- AC-5 ✓ — capture is read-only / null-safe; new tests assert PEI parse failure produces 201 (does NOT block auth/registration).
- AC-6 ✓ — malformed input → WARN log + counter increment confirmed via 5G regression tests post-fix; RADIUS / Diameter coverage was pre-existing.
- AC-7 ✓ — golden-byte fixture coverage: RADIUS 5/5, Diameter 5/5, SBA ParsePEI 6/6, plus 3 new wire-handler regression tests.
- AC-8 ✓ — full test matrix 3841/3841 PASS; zero behavioural regression in radius/diameter/sba/dsl/observability.
- AC-9 ✓ — `git diff -- internal/audit/` empty; `git diff -- internal/store/` empty; Session struct extension is in-memory + Redis only (rationale captured in field comment + VAL-040).
- AC-10 ✓ (microbench evidence; literal 1M-SIM run scheduled D-184).

## Decisions Recorded

- VAL-039 — Constructor-level Registry injection on AUSF/UDM via `ServerDeps.MetricsReg`.
- VAL-040 — Chose `session.Session` struct extension (option a) over per-request context-value stash for UDM IMEI propagation; AC-9 audit shape preserved by NOT extending `CreateRadiusSessionParams` or audit payload.
- VAL-041 — PROTOCOLS.md amended to document both flat (current) and nested (forward) SessionContext shapes.

## Tech Debt Routed

- D-182 → STORY-094 (Diameter parser caller-less)
- D-183 → STORY-094 / STORY-097 (5G non-3GPP PEI raw retention)
- D-184 → STORY-094 (AC-10 literal 1M-SIM bench)

## Files Modified During Fix

Source:
- `internal/observability/metrics/metrics.go`
- `internal/aaa/sba/server.go`
- `internal/aaa/sba/ausf.go`
- `internal/aaa/sba/udm.go`
- `internal/aaa/session/session.go`
- `cmd/argus/main.go`

Tests (call-site updates):
- `internal/aaa/sba/nsmf_integration_test.go`
- `internal/simulator/sba/ausf_test.go`
- `internal/simulator/sba/udm_test.go`
- `internal/simulator/sba/integration_test.go`

Docs:
- `docs/architecture/PROTOCOLS.md`
- `docs/brainstorming/decisions.md`
- `docs/ROUTEMAP.md`

## Files Created During Fix

- `internal/aaa/sba/imei_handler_test.go` — 3 wire-handler regression tests covering F-A1 (counter) and F-A2 (Session population).
- `docs/stories/phase-11/STORY-093-gate.md` — this report.

## Final Gate Verdict

**PASS** — All 7 findings resolved (3 FIXED in code, 1 FIXED in docs, 3 DEFERRED with target stories and ROUTEMAP entries). Full test matrix green, build clean, AC-9 audit shape preserved, DB schema untouched.
