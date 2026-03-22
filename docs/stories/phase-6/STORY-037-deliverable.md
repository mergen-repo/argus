# Deliverable: STORY-037 — SIM Connectivity Diagnostics

## Summary

Implemented automated 7-step SIM connectivity diagnostics. Checks SIM state, last auth/session, operator health, APN config, policy verification, IP pool availability, and optional test authentication. Results include per-step findings with pass/warn/fail status and remediation suggestions. Cached 1 minute via Redis.

## Files Changed

### New Files
| File | Purpose |
|------|---------|
| `internal/diagnostics/diagnostics.go` | Core diagnostics service — 7-step check engine |
| `internal/diagnostics/diagnostics_test.go` | 12 unit tests |
| `internal/api/diagnostics/handler.go` | POST /api/v1/sims/:id/diagnose handler with Redis cache |
| `internal/api/diagnostics/handler_test.go` | 5 handler tests |

### Modified Files
| File | Change |
|------|--------|
| `internal/store/session_radius.go` | Added GetLastSessionBySIM method |
| `internal/gateway/router.go` | Diagnostics route (sim_manager+) |
| `cmd/argus/main.go` | Wired diagnostics service and handler |

## API Endpoints
| Ref | Method | Path | Auth | Description |
|-----|--------|------|------|-------------|
| API-049 | POST | `/api/v1/sims/:id/diagnose` | sim_manager+ | Run 7-step connectivity diagnostics |

## Key Features
- Step 1: SIM state check (active/suspended/terminated)
- Step 2: Last auth analysis (never connected, rejected, inactive >24h)
- Step 3: Operator health (healthy/degraded/down with failover info)
- Step 4: APN config validation (exists, active, operator mapping)
- Step 5: Policy verification (active version, bandwidth check)
- Step 6: IP pool availability
- Step 7: Optional test auth via operator adapter (placeholder)
- Overall status: PASS/DEGRADED/FAIL
- Redis cache: 1-minute TTL per SIM

## Test Coverage
- 17 new tests across 2 test files
- 917 total tests passing, 0 regressions
