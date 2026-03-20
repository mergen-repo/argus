# Post-Story Review: STORY-020 — 5G SBA HTTP/2 Proxy (AUSF/UDM)

> Date: 2026-03-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-021 (Operator Failover) | No impact. STORY-021 focuses on NATS event publishing, notification alerts, WebSocket push, and SLA violation detection. SBA is a separate protocol listener, not routed through OperatorRouter failover. | NO_CHANGE |
| STORY-027 (RAT-Type Awareness) | 5G SBA already sets `rat_type='nr_5g'` on sessions (ausf.go L211, udm.go L158). AC "5G SBA: extract RAT type from authentication context" is partially done. RAT type normalization needed (nr_5g -> 5G_SA enum). | UPDATED |
| STORY-032 (CDR Processing) | 5G SBA publishes session events to same NATS topics with `protocol: "5g_sba"`. CDR consumer should handle 5G SBA sessions. Note: no interim accounting updates from SBA (auth-only sessions). | UPDATED |
| STORY-033 (Real-Time Metrics) | SBA health checker already integrated into /api/health. Metrics collection should count SBA auth events alongside RADIUS/Diameter. | NO_CHANGE |
| STORY-052 (Perf Tuning) | SBA uses in-memory auth context map with 30s TTL (not Redis). Performance characteristics differ from RADIUS/Diameter hot path. Benchmark suite should include SBA throughput test. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| GLOSSARY.md | Added 7 terms: NRF, SUPI, SUCI, S-NSSAI, 5G-AKA, KSEAF, KAUSF | UPDATED |
| ARCHITECTURE.md | Added SBA (SVC-04 :8443) to system architecture diagram | UPDATED |
| CLAUDE.md | Added :8443 (5G SBA) to Docker services table | UPDATED |
| CONFIG.md | Added `SBA_ENABLE_MTLS` env var entry + .env.example section | UPDATED |
| ERROR_CODES.md | Added note about SBA using `application/problem+json` format (3GPP TS 29.500) | UPDATED |
| services/_index.md | Fixed shared package path: `internal/protocol/sba` -> `internal/aaa/sba` | UPDATED |
| STORY-020 spec | Fixed architecture reference: removed non-existent `internal/protocol/sba` package | UPDATED |
| STORY-027 spec | Added post-STORY-020 note about existing RAT-type handling in SBA | UPDATED |
| STORY-032 spec | Added post-STORY-020 note about 5G SBA session events and CDR implications | UPDATED |
| ROUTEMAP.md | STORY-020 marked DONE (20/55, 36%), changelog entry, next story STORY-021 | UPDATED |
| decisions.md | No changes needed — VAL-011/012/013, TEST-014, PERF-021 already captured | NO_CHANGE |
| SCREENS.md | No changes needed — backend-only story | NO_CHANGE |
| FRONTEND.md | No changes needed — backend-only story | NO_CHANGE |
| FUTURE.md | No changes needed — no new future opportunities identified | NO_CHANGE |
| Makefile | No changes needed — no new make targets | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 2 (both fixed)
  1. **services/_index.md** listed `internal/protocol/sba` as a shared package, but this directory does not exist. The actual implementation is in `internal/aaa/sba`. Fixed to `internal/aaa/sba`.
  2. **CLAUDE.md** Docker services table did not list port 8443 (5G SBA), while ARCHITECTURE.md and docker-compose.yml both expose it. Fixed CLAUDE.md to include `:8443 (5G SBA)`.

## Observations

### Implementation Quality
- Clean 5-file structure: server.go (orchestration), ausf.go (5G-AKA), udm.go (subscriber data), eap_proxy.go (EAP-AKA' delegation), nrf.go (NRF placeholder)
- Correct use of `application/problem+json` for 3GPP NF-to-NF errors (not Argus envelope)
- Auth context expiry via goroutine + sleep (30s TTL) is simple and correct for single-node. Cluster mode would need Redis-based contexts.
- Deterministic AV generation (SHA256-based) is acceptable for mock/proxy mode per VAL-011
- SBA_ENABLED feature gate with graceful NRF deregistration on shutdown is well-implemented

### Minor Notes (Not Blocking)
- `internal/protocol/sba` was planned during architecture but implementation landed in `internal/aaa/sba` (consistent with RADIUS/Diameter placement). The orphan reference has been cleaned up.
- CONFIG.md .env.example section was missing `SBA_ENABLE_MTLS` (added by gate fix to .env.example but CONFIG.md docs were not updated). Now fixed.
- Phase 3 has 1 remaining story: STORY-021 (Operator Failover, M effort, scope reduced by prior stories)

## Project Health

- Stories completed: 20/55 (36%)
- Current phase: Phase 3 (AAA Engine) — 6/7 stories done
- Next story: STORY-021 (Operator Failover & Circuit Breaker)
- Phase 3 remaining: STORY-021 only (M effort)
- Blockers: None
