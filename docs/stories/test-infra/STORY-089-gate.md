# Gate Report: STORY-089 — Operator SoR Simulator

> Written: 2026-04-18
> Story: `docs/stories/test-infra/STORY-089-plan.md`
> Track: Runtime Alignment (3/3 final story)
> Executed by: Gate Team Lead (Opus 4.7) after 3 parallel scouts (Analysis, Test/Build, UI)
> UI story: NO — UI scout returned empty block as expected
> Maintenance mode: NO

## Summary

- Requirements Tracing: Fields 14/14, Endpoints 10/10, Workflows 14/14, Components N/A (no UI)
- Gap Analysis: 13/14 acceptance criteria passed (AC-14 PENDING — expected at post-ship phase boundary)
- Compliance: COMPLIANT
- Tests: 46/46 story tests passed (config 97.6%, server 94.1%), 3160/3160 full suite passed
- Test Coverage: 13/14 ACs have assertions (AC-14 is a post-ship counter update); all business rules (per-op routing, 404 validation, metrics label set, stub shape) covered
- Performance: 0 issues found (passive HTTP server; PAT-003 + PAT-004 compliant; 135 series max cardinality)
- Build: PASS (`go build ./...` clean; `go vet ./cmd/operator-sim/... ./internal/operatorsim/...` clean; docker compose config clean; integration binary compiles at 11.9 MB)
- Screen Mockup Compliance: N/A (no UI)
- UI Quality: N/A (no UI)
- Token Enforcement: N/A (no UI)
- Turkish Text: N/A (no UI; backend-only)
- Overall: **PASS**

## Team Composition

- Analysis Scout: 8 findings (F-A1…F-A8)
- Test/Build Scout: 0 findings (all verification commands green — tests pass, build clean, coverage 97.6% / 94.1%)
- UI Scout: N/A (UI story: NO)
- De-duplicated: 8 → 8 findings (no overlaps)

## Fixes Applied

| # | Finding | Category | File | Change | Verified |
|---|---------|----------|------|--------|----------|
| 1 | F-A3 | test coverage | `internal/operatorsim/server/health_test.go` | Replaced `avea` test case with `vodafone` + `mock`; updated `testConfig()` to match production registered set (turkcell, vodafone_tr, vodafone, turk_telekom, mock) | 46/46 tests pass; 94.1% coverage |
| 2 | F-A3 | test coverage | `internal/operatorsim/server/server_test.go` | Replaced `avea` in operator slice with `vodafone` + `mock` to match production config | 46/46 tests pass |
| 3 | F-A5 | correctness/HTTP | `internal/operatorsim/server/cdr.go` | Always drain request body (was gated on `s.cfg.Stubs.CDREcho`); HTTP keep-alive safety. `CDREcho` retained in config for future metadata-shape control; documented inline | `TestCDREcho_Disabled` still passes; no 202 regression |
| 4 | F-A8 | compliance/cleanup | `cmd/operator-sim/main.go` | Removed duplicate `ARGUS_OPERATOR_SIM_CONFIG` env read (config.Load is now the single authoritative entrypoint); replaced with explanatory comment | `TestLoad_EnvOverrideConfigPath` still passes inside config package |
| 5 | F-A1 | doc/compliance | `docs/stories/test-infra/STORY-089-plan.md` (AC-9 text + §Data flow gauge block) | Updated AC-9 to say gauge value ≥ 1 with `0=down / 1=degraded / 2=healthy` semantics per `internal/observability/metrics/metrics.go:321`; data-flow block now shows `= 2` for healthy | plan line numbers updated; integration test assertion at `test/e2e/operator_sim_test.go:215` already uses `GreaterOrEqualf(..., 1.0)` — matches |
| 6 | F-A2 | doc/compliance | `docs/stories/test-infra/STORY-089-plan.md` §Components involved | Noted that metrics are inlined in `server.go` (metricsRegistry type + newMetricsRegistry constructor) rather than a separate `internal/operatorsim/metrics/` subpackage; rationale captured; PAT-003 compliance preserved. Advisor-guided trade-off: plan doc update is cheaper than file-move refactor and the code is functionally equivalent | visual diff |
| 7 | F-A6 | compliance/verification | `docs/reports/test-infra-tech-debt-gate-spec.md` AC-5 verify command | Replaced brittle `grep -c ' t$'` (assumes default psql tuple format) with robust `psql -t -A ... \| tr -d '[:space:]' \| grep -qx '3'` using COUNT(\*) and `adapter_config->'http'->>'enabled')::boolean = true`. Added changelog entry for the hardening | visual review; works under `-x`, `-A`, and CI-customized psql configs |
| 8 | F-A7 | doc/design | `docs/stories/test-infra/STORY-089-plan.md` §Metrics surface | Documented the design intent that `instrumentMiddleware` only counts requests that pass `validateOperator` (known operators only). 404 unknown-operator responses intentionally bypass metrics to keep `{operator}` label cardinality bounded and DoS-resistant. Future-story path sketched (separate `operator_sim_invalid_operator_requests_total` counter) | doc update only; no code change |

### Verification

```
go test -race -cover ./internal/operatorsim/...
ok  github.com/btopcu/argus/internal/operatorsim/config  coverage: 97.6%
ok  github.com/btopcu/argus/internal/operatorsim/server  coverage: 94.1%

go vet ./cmd/operator-sim/... ./internal/operatorsim/...
(no issues)

go build ./...
(success)

go test ./... (full suite)
3160 passed in 102 packages, 0 failed

go test -tags=integration -c -o /tmp/argus_e2e_test ./test/e2e/
(11.9M binary produced)
```

## Escalated Issues

None. All findings were within Team Lead scope.

## Deferred Items (tracked in ROUTEMAP → Tech Debt)

| # | Finding | Target | Written to ROUTEMAP |
|---|---------|--------|---------------------|
| D-040 | F-A4 `cmd/operator-sim/main.go:27` — bootstrap `initLogger("info","console")` before `config.Load` with re-init after, standard two-phase pattern; if Load fails, intended log.level/format ignored for Fatal path. No correctness impact; consolidating into a single config-aware stderr logger is cleanup, not a runtime bug. | future log-hygiene story | YES (row D-040, OPEN) |

**On F-A7 reclassification**: the scout marked this `fixable: YES (optional)` with suggested fix "accept current behavior." Per gate-lead discipline (§Phase 2 HARD-GATE — no "Advisory/Observations" categories), this was materially addressed by updating the plan's §Metrics surface to document the intentional behavior with rationale (DoS-resistance + cardinality bound). The behavior IS correct; the plan just needed to state that correctness. Classified as FIXED via doc, not deferred.

## Performance Summary

### Queries Analyzed
| # | File:Line | Pattern | Issue | Severity | Status |
|---|-----------|---------|-------|----------|--------|
| 1 | `internal/operatorsim/*` | Zero DB queries | Stateless passive HTTP server | N/A | — |
| 2 | `migrations/seed/003_comprehensive_seed.sql:126-143` | `ON CONFLICT DO NOTHING` | Idempotent seed | LOW | Accepted |
| 3 | `migrations/seed/005_multi_operator_seed.sql:48-76` | Same | Same | LOW | Accepted |

### Caching Verdicts
| # | Data | Location | TTL | Decision | Status |
|---|------|----------|-----|----------|--------|
| CACHE-V-1 | Simulator response bodies | None | N/A | SKIP (sub-200-byte JSON, no upstream) | — |
| CACHE-V-2 | `operatorSet` lookup (validateOperator) | `map[string]struct{}` | Process | CACHE (already implemented) | — |
| CACHE-V-3 | Prometheus registry | `prometheus.Registry` | Process | CACHE (already implemented) | — |

### Frontend Performance
N/A — UI story: NO.

### API Performance
- Payload size: < 200 bytes; no over-fetching
- Pagination: N/A (no list endpoints)
- Compression: not configured (not needed for bridge-network JSON)
- Goroutines: **PAT-004 compliant** — zero periodic tickers; bounded by http.Server per-connection pool
- Metric cardinality: **PAT-003 compliant** — label set `{operator, path, method, status_code}` defined at registration time; max 5 operators × 3 paths × 3 methods × 3 status codes = **135 series** (upper bound)

## Token & Component Enforcement (UI stories)

N/A — UI story: NO.

## Verification

- Tests after fixes: 46/46 story tests PASS, 3160/3160 full suite PASS
- Build after fixes: PASS (`go build ./...`, `go vet`, `docker compose config`, integration binary)
- Token enforcement: N/A (no UI)
- Fix iterations: 1 (within max 2)

## Maintenance Mode — Pass 0 Regression

N/A — maintenance_mode: NO. Not a hotfix/bugfix/enhance story.

## Passed Items

**AC-1 — Binary compiles / container builds**: `go build ./cmd/operator-sim` succeeds; Dockerfile multi-stage present (`deploy/operator-sim/Dockerfile`); Makefile targets `operator-sim-build` + `operator-sim-logs` present.

**AC-2 — Container healthy via docker-compose**: `docker compose -f deploy/docker-compose.yml config` parses clean; healthcheck wired (`wget -qO- http://localhost:9596/-/health`); `argus-app` has `depends_on: operator-sim: condition: service_healthy`.

**AC-3 — Per-operator health endpoints**: all 5 operator codes registered in `deploy/operator-sim/config.example.yaml` (turkcell, vodafone_tr, vodafone, turk_telekom, mock); `validateOperator` returns 404 on unknown operator with `{"error":"unknown operator","operator":"<code>"}`.

**AC-4 — Per-operator stubs**: subscriber returns stub `{imsi, operator, plan=default, status=active}`; CDR returns 202 with `{received, ingested_at}`. Post-fix: body always drained.

**AC-5 — Seed 003 http sub-key**: `migrations/seed/003_comprehensive_seed.sql` populates `adapter_config.http` on turkcell, vodafone_tr, turk_telekom with `base_url=http://argus-operator-sim:9595/<code>`, `health_path=/health`, `timeout_ms=2000`, `enabled=true`.

**AC-6 — Seed 005 http sub-key**: `migrations/seed/005_multi_operator_seed.sql` parallel coverage on 3 operators using `vodafone` alias.

**AC-7 — API-307 /test/http**: integration-verifiable end-to-end at `test/e2e/operator_sim_test.go`; path math correct (`BaseURL + HealthPath = http://argus-operator-sim:9595/<operator>/health`).

**AC-8 — enabled_protocols array**: STORY-090 canonical-order derivation + seed `http.enabled=true` → `["radius","http","mock"]` expected for the 3 real operators.

**AC-9 — HealthChecker http gauge**: assertion uses `GreaterOrEqualf(..., 1.0)` matching gauge semantics (`0=down / 1=degraded / 2=healthy`). Plan text corrected post-F-A1.

**AC-10 — D-039 closed**: 5 new rows API-308..API-312 in `docs/architecture/api/_index.md`; pending-note removed; total 241→246; ROUTEMAP D-039 row flipped to `✓ RESOLVED`.

**AC-11 — Metrics populated after 1 request**: `instrumentMiddleware` increments `operator_sim_requests_total{...}` counter + `operator_sim_request_duration_seconds{...}` histogram on every known-operator request. Per F-A7 post-fix doc: 404s intentionally bypass this counter for cardinality safety.

**AC-12 — go vet/race/coverage ≥ 70%**: `go vet` clean; `go test -race` clean; coverage 97.6% (config) / 94.1% (server) — both well above 70% floor.

**AC-13 — Mini Phase Gate spec EXTENDED**: `docs/reports/test-infra-tech-debt-gate-spec.md` §STORY-089 present; changelog line added; only additions — no deletions to any STORY-080/082/083/084/085/087/088/090/092 section. AC-5 psql verify command additionally hardened (F-A6).

**AC-14 — Runtime Alignment 3/3**: PENDING (post-ship). ROUTEMAP counter advances at phase-boundary step after this gate report ships.

## Final Status

**GATE: PASS**

All 13 in-scope ACs MET. AC-14 is a post-ship ROUTEMAP counter update — expected at the Mini Phase Gate step. 8 findings from the Analysis scout — 7 fixed directly, 1 deferred to a future log-hygiene story (D-040). 0 findings from Test/Build and UI scouts. Tests + build + vet + full suite all green. Coverage well above the 70% AC-12 floor. Zero regressions introduced.
