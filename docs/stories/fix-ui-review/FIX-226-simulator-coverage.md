# FIX-226: Simulator Coverage + Volume Realism

## Problem Statement
Simulator gaps surface repeatedly in findings:
- Diameter CCR/CCA live traffic = 0 (F-06) — only initial Gy CCR, no steady-state accounting
- 5G SBA AUSF/UDM = 0 traffic (F-282) — not exercised
- RADIUS NAS-IP AVP absent (F-160)
- Policy violations never triggered (F-154) — no bandwidth/quota breach simulation
- Heartbeat events flood notifications (F-227)
- Growth rate unrealistic (+73.3%/day — F-221)
- 5 missing kill_switch key (F-313 via env — kill switches remove, so skip that part)

## User Story
As a test/demo environment user, I want the simulator to generate realistic, diverse M2M traffic (all protocols, anomalies, violations) so the UI exercises real code paths.

## Findings Addressed
F-06 (revised), F-27, F-64, F-93, F-160, F-154, F-221, F-227, F-266, F-279, F-282

## Acceptance Criteria
- [ ] **AC-1:** Diameter CCR-U (interim update) generated every 30s per active session — realistic accounting traffic.
- [ ] **AC-2:** 5G SBA AUSF Nausf_UEAuthentication + UDM Nudm_SDM exercised — at least 10% of session creates via 5G SBA path.
- [ ] **AC-3:** RADIUS simulator includes NAS-IP-Address AVP in Access-Request.
- [ ] **AC-4:** Bandwidth overshoot scenarios: 1% of sessions exceed their policy `bandwidth_down` — triggers `policy_violations` insert + throttle action.
- [ ] **AC-5:** Geo-block scenarios: occasional foreign IMSI attempts → `geo_blocked` violation.
- [ ] **AC-6:** SIM growth rate realistic: default 5 SIMs/day, env configurable.
- [ ] **AC-7:** Heartbeat events — REMOVED from simulator (F-217 M2M event taxonomy scope). Internal health monitoring is metric, not notification.
- [ ] **AC-8:** Policy rollout CoA simulation — when rollout fires, simulator acks CoA within 200ms.
- [ ] **AC-9:** Config knobs: `SIM_COUNT_TARGET`, `SESSION_RATE_PER_SEC`, `VIOLATION_RATE_PCT`, `DIAMETER_ENABLED`, `SBA_ENABLED` in simulator env.

## Files to Touch
- `cmd/operator-sim/` or equivalent simulator binary
- `internal/aaa/bench/loadgen.go` (if co-located)
- Simulator docker-compose config

## Risks & Regression
- **Risk 1 — Simulator noise breaks real tests:** Keep simulator separable — prod deploys don't include it.
- **Risk 2 — DB fill from simulator:** Retention policy (F-267 backup + cleanup) handles.

## Test Plan
- 24h run: observe Dashboard — all 3 protocols active, violations accumulate, auth/s > 0

## Plan Reference
Priority: P2 · Effort: M · Wave: 6
