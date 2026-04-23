# FIX-221 Gate Scout — Test & Build

Scope: compilation, vet, tests, build, lint enforcement re-verification.

## Build / Vet
| Check | Command | Result |
|-------|---------|--------|
| Go build | `go build ./...` | PASS |
| Go vet (full) | `go vet ./internal/store/... ./internal/api/dashboard/...` | PASS (0 issues) |
| Frontend type-check | `cd web && npx tsc --noEmit` | PASS (0 errors) |
| Frontend build | `cd web && npm run build` | PASS (vite 2.47s, 14 chunks emitted) |

## Test Suite
| Check | Command | Result |
|-------|---------|--------|
| Targeted | `go test ./internal/store/... ./internal/api/dashboard/...` | PASS — 457 tests / 3 packages |
| Full suite | `go test ./...` | PASS — 3531 tests / 109 packages |
| Legacy heatmap test | `TestCDRStore_GetTrafficHeatmap7x24` (cdr_test.go:329) | PASS (wrapper preserved) |

## Lint Enforcement (UI scope of FIX-221)
| Check | Command | Before | After |
|-------|---------|--------|-------|
| Raw `<button>` in dashboard | `grep -cE '<button' web/src/pages/dashboard/index.tsx` | 0 | 0 |
| Hex color literals | `grep -cE '#[0-9a-fA-F]{3,8}' web/src/pages/dashboard/index.tsx` | 0 | 0 |
| `rgba(...)` usages | `grep -cE 'rgba\(' web/src/pages/dashboard/index.tsx` | 12 | 12 (all pre-existing heatmap cell colors; out of FIX-221 scope) |
| New `text-white` / `text-cyan-…` defaults | grep | 0 | 0 |

## Test Coverage of New Surface Area

### F-B1 MED — No unit tests for `GetTrafficHeatmap7x24WithRaw`
- Evidence: `internal/store/cdr_test.go:329` only covers the legacy `GetTrafficHeatmap7x24`. The new method + `TrafficHeatmapCellRaw` type are untested.
- Per plan decision (D-091 open; AUTOPILOT precedent: "unit tests deferred — manual smoke"), this is an ACCEPTED miss for FIX-21x story cadence.
- Classification: DEFERRED (already tracked by D-091 "Deferred unit tests").

### F-B2 MED — No unit tests for `TopPoolUsage`
- Evidence: no `internal/store/ippool_test.go` file exists in the repo (searched); `TopPoolUsage` empty-tenant (`pgx.ErrNoRows → nil,nil`) path is untested.
- Classification: DEFERRED under D-091 (same rationale as F-B1).

### F-B3 MED — No handler test for new `raw_bytes` / `top_ip_pool` DTO shape
- Evidence: `internal/api/dashboard/handler.go` ships additive JSON fields; existing handler tests that don't use `DisallowUnknownFields` pass trivially but don't assert the new fields' presence or shape.
- Classification: DEFERRED under D-091.

## Manual Smoke (per plan Test Plan)
Per plan Task 3 + Task 5 + Task 6 Verify, manual browser/curl smoke is the sign-off gate. Manual smoke NOT performed inside the gate subagent (no running stack). Reviewer relies on:
- Type/shape correctness validated via tsc + go build.
- DOW/TZ semantics identical to existing tested method (F-A6).
- FE defaults (`?? 0`, `subtitle={undefined}` for null case) proven by tsc + build.

## Summary
Build + type-check + full Go test suite PASS. No regression. Three MED test-coverage gaps all fall under existing D-091 deferral policy; no new D-### needed for these. No BLOCKER/HIGH findings in this scout.
