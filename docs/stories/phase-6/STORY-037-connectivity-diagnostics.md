# STORY-037: SIM Connectivity Diagnostics

## User Story
As a SIM manager, I want an automated connectivity diagnostics tool that checks SIM state, last auth, operator health, APN config, policy, and IP pool, so that I can quickly troubleshoot connection issues.

## Description
Auto-diagnosis performs a 6-step connectivity check for a SIM: (1) SIM state check, (2) last auth/session analysis, (3) operator health check, (4) APN configuration validation, (5) policy verification, (6) IP pool availability. Optional step 7: trigger test authentication through operator. Results include findings (pass/warn/fail per step) and suggested remediation actions.

## Architecture Reference
- Services: SVC-03 (Core API), SVC-04 (AAA — test auth), SVC-06 (Operator Router — health)
- API Endpoints: API-049
- Database Tables: TBL-10 (sims), TBL-17 (sessions), TBL-05 (operators), TBL-07 (apns), TBL-08 (ip_pools), TBL-14 (policy_versions)
- Data Flows: FLW-07 (Connectivity Diagnostics)
- Source: docs/architecture/flows/_index.md (FLW-07)

## Screen Reference
- SCR-021d: SIM Detail — Diagnostics tab with step-by-step results, troubleshooting wizard

## Acceptance Criteria
- [ ] POST /api/v1/sims/:id/diagnose runs 6-step diagnostic check
- [ ] Step 1 — SIM State: verify SIM is ACTIVE (fail if SUSPENDED/TERMINATED)
- [ ] Step 2 — Last Auth: check TBL-17 for most recent session
  - If never authenticated → warn "SIM has never connected"
  - If last auth rejected → fail with reject reason
  - If last auth >24h ago → warn "No recent activity"
- [ ] Step 3 — Operator Health: check TBL-05 health_status
  - If operator down → fail "Operator is down, failover: {policy}"
  - If degraded → warn "Operator experiencing issues"
- [ ] Step 4 — APN Config: verify APN exists, is active, mapped to SIM's operator
- [ ] Step 5 — Policy: verify policy version is active, not throttled to 0 bandwidth
- [ ] Step 6 — IP Pool: verify assigned pool has available addresses
- [ ] Step 7 (optional, via query param): trigger test authentication through operator adapter
  - Send test Access-Request, wait up to 5s for response
  - Pass if Access-Accept, fail if Access-Reject or timeout
- [ ] Response: ordered steps with {step, name, status (pass/warn/fail), message, suggestion}
- [ ] Overall result: PASS (all steps pass), DEGRADED (warnings), FAIL (any step fails)
- [ ] Diagnostic result cached for 1 minute (avoid repeated expensive checks)

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-049 | POST | /api/v1/sims/:id/diagnose | `{include_test_auth?: bool}` | `{sim_id,overall_status,steps:[{step,name,status,message,suggestion}],diagnosed_at}` | JWT(sim_manager+) | 200, 404 |

## Dependencies
- Blocked by: STORY-011 (SIM CRUD), STORY-015 (RADIUS for test auth), STORY-021 (operator health)
- Blocks: None

## Test Scenarios
- [ ] Active SIM, healthy operator, valid APN → all steps PASS, overall=PASS
- [ ] Suspended SIM → Step 1 FAIL, suggestion "Activate or resume SIM"
- [ ] Operator down → Step 3 FAIL, suggestion includes failover status
- [ ] APN not mapped to operator → Step 4 FAIL, suggestion "Check APN-operator mapping"
- [ ] Policy throttled to 0 → Step 5 FAIL, suggestion "Update policy bandwidth"
- [ ] IP pool exhausted → Step 6 FAIL, suggestion "Expand IP pool or reclaim IPs"
- [ ] Test auth enabled → Step 7 runs, Access-Accept → PASS
- [ ] Test auth timeout → Step 7 FAIL, suggestion "Check operator connectivity"
- [ ] Diagnostic cached → second call within 1min returns cached result

## Effort Estimate
- Size: M
- Complexity: Medium
