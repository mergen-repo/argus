# Gate Report: FIX-226 — Simulator Coverage + Volume Realism

## Summary
- Requirements Tracing: ACs 8/9 addressed (AC-5 DEFERRED per DEV-316; AC-6 reinterpreted per DEV-317)
- Gap Analysis: 8/8 in-scope ACs passed (AC-1, AC-2, AC-3, AC-4, AC-6, AC-7, AC-8, AC-9)
- Compliance: COMPLIANT
- Tests: 153/153 simulator package, 3513/3513 full sweep — all PASS
- Test Coverage: 5 new env-knob unit tests (AC-9), PAT-001 single-writer invariants verified for both new metrics
- Performance: no concerns — aggressive_m2m scenario at 1% weight ≈ 2 active sessions peak; plan R1 sized for 10× headroom
- Build: PASS (`go build ./...`, `go vet ./...` both clean)
- Screen Mockup Compliance: N/A (backend-only)
- UI Quality: N/A (backend-only; `git diff --stat HEAD -- web/` = 0)
- Token Enforcement: N/A
- Turkish Text: N/A (English technical docs)
- Overall: PASS

## Team Composition
- Analysis Scout: 3 findings (F-A1..F-A3)
- Test/Build Scout: 1 finding (F-B1, informational)
- UI Scout: 0 findings (backend-only)
- De-duplicated: 4 → 4 (no overlap)

## Fixes Applied
| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Docs (seed header accuracy) | `migrations/seed/008_scale_sims.sql:6-12` | Corrected activation-date range comments (40-SIM groups: 60→21d, 20-SIM TT groups: 60→41d) — previous comment claimed newest = today, actual newest = 21d ago | `grep` verify; no SQL change |

## Escalated Issues
None.

## Deferred Items (tracked in ROUTEMAP → Tech Debt)
| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-126 | AC-5 geo-block scenario (requires `geo_blocked` enforcer + DSL keyword) | FIX-260-series | YES |
| D-127 | `ARGUS_SIM_VIOLATION_RATE_PCT` defensive clamp — currently relies on `Validate()` scenario-weight rejection when pct ≥ 69% drives `normal_browsing.Weight` negative | Future simulator polish | YES |
| D-128 | `aggressive_m2m` scenario APN alignment — 008-seeded SIMs don't match seed 003 `m2m.meter` / `iot.fleet` APN predicates directly; breach still occurs via `premium-v2` (50Mbps) catchall | Future simulator polish | YES |

## Acceptance Criteria Verdicts
| AC | Verdict | Evidence |
|----|---------|----------|
| AC-1 Gy CCR-U every 30s | PASS | `config.example.yaml:109` — `normal_browsing.interim_interval_seconds: 30` (was 60); `engine.go:405 UpdateGy()` pre-existing |
| AC-2 5G SBA ≥10% sessions | PASS | `config.example.yaml:89` — Turkcell `sba.rate: 0.2` (20% ≥ 10%) |
| AC-3 NAS-IP AVP set | PASS | `config.example.yaml:82,93,98` — 192.0.2.10/20/30 RFC 5737 IPv4; `radius/client.go:167-177` — single-writer increment tracks misses |
| AC-4 Bandwidth violations (real enforcer path) | PASS | `config.example.yaml:127-132` — `aggressive_m2m` at weight 0.01, 200-500MB/30s = 53-133 Mbps → breaches any policy with < 53 Mbps cap; APN-tight alignment deferred to D-128 |
| AC-5 Geo-block | DEFERRED | DEV-316 + D-126 (FIX-260-series) |
| AC-6 SIM growth stagger | PASS | `008_scale_sims.sql` — 6 sites replaced `NOW()` with 60-day stagger expression; range verified 60→21d (40-SIM groups) and 60→41d (20-SIM TT groups); ON CONFLICT preserved |
| AC-7 No simulator heartbeats | PASS | `grep -rn heartbeat cmd/simulator/ internal/simulator/` = 0 matches |
| AC-8 CoA ack <200ms | PASS | `SimulatorCoAAckLatencySeconds` histogram registered, buckets include 200ms boundary; `listener.go:162,168,187` — t0 at handleCoA entry, observed on both NAK and ACK paths post-write; single-writer (PAT-001) |
| AC-9 5 env knobs | PASS | `config.go:196-234` — all 5 wired; `Validate()` enforces rate>0 with actionable error; 5 unit tests pass; CONFIG.md table + compose passthrough |

## Performance Summary

### Queries Analyzed
| # | File:Line | Query/Pattern | Issue | Severity | Status |
|---|-----------|---------------|-------|----------|--------|
| 1 | `008_scale_sims.sql:105-181` | Seed `NOW() - INTERVAL '1 day' * (60 - (g-100))` | Expression is deterministic, monotonic, bounded; runs once per `make db-seed` | LOW | PASS |

### Caching Verdicts
| # | Data | Location | TTL | Decision | Status |
|---|------|----------|-----|----------|--------|
| 1 | N/A | N/A | N/A | N/A (simulator has no caching concerns) | N/A |

## Verification
- Tests after fixes: 153 simulator + 3513 full sweep, all PASS
- Build after fixes: PASS (`go build ./...`, `go vet ./...`)
- Compose syntax: PASS (`docker compose config --quiet`)
- Fix iterations: 1 (doc fix only; no code iterations required)
- Single-writer invariants (PAT-001): VERIFIED for both `SimulatorNASIPMissingTotal` and `SimulatorCoAAckLatencySeconds`
- Env > YAML precedence (PAT-017): VERIFIED — `applyEnvOverrides` runs after YAML parse, before `Validate()`
- PAT-011 (plan params threaded to construction site): VERIFIED — all 5 env vars mutate the `Config` struct BEFORE `main.go` consumes it

## Passed Items
- Env knob range validation (rate > 0 as error, pct 0-100 bound, interval > 0)
- Idempotency preserved on all 6 SIM inserts (`ON CONFLICT (imsi, operator_id) DO NOTHING`)
- Scenario weight sum exactly 1.00 (0.69 + 0.20 + 0.10 + 0.01)
- No UI surface touched (backend-only story scope honored)
- No heartbeat code in simulator (AC-7 architectural invariant honored)
- CoA histogram bucket choice (1ms..1s, matches AC-8 threshold)
- NAS-IP values are distinct per-operator (avoid AVP collisions in logs)
- RFC 5737 TEST-NET-1 discipline followed (no routable IPs in test config)
- Compose env passthroughs use `${VAR:-}` empty-fallback (YAML defaults honored when unset)
- CONFIG.md documents both wired knobs AND "NOT ADDED" follow-ups (SIM_COUNT_TARGET, SBA_USE_RATE_FLOOR)
- Three DEV entries + three Tech Debt rows close the scope-decision audit trail
