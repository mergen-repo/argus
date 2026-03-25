# STORY-051: End-to-End Integration Test — Full Auth Flow

## User Story
As a QA engineer, I want a comprehensive end-to-end test covering the full user journey from login to session verification, so that I can validate all system components work together correctly.

## Description
End-to-end test scenario: login → dashboard loads → create APN → bulk import SIMs → verify SIMs in list → apply policy → check session created via RADIUS. Tests the full stack: frontend → API Gateway → Core API → AAA Engine → Policy Engine → Database → Redis → NATS → WebSocket. Uses Playwright for browser automation and Go test client for RADIUS simulation.

## Architecture Reference
- Services: All services (SVC-01 through SVC-10)
- API Endpoints: API-001, API-110, API-031, API-063, API-040, API-091, API-095, API-100
- Data Flows: FLW-01 (RADIUS Auth), FLW-04 (Bulk Import)
- Source: All architecture documents

## Screen Reference
- SCR-001: Login
- SCR-010: Dashboard
- SCR-030: APN List (create APN)
- SCR-020: SIM List (verify imported SIMs)
- SCR-060: Policy List (create and apply policy)
- SCR-050: Live Sessions (verify session)

## Acceptance Criteria
- [x] Test 1 — Login: POST /api/v1/auth/login → JWT obtained
- [x] Test 2 — Dashboard: GET /api/v1/dashboard → 200, metrics present
- [x] Test 3 — Create APN: POST /api/v1/apns → 201, APN with IP pool created
- [x] Test 4 — Bulk Import: POST /api/v1/sims/bulk/import (CSV with 10 SIMs) → 202, job created
- [x] Test 5 — Wait for job: poll GET /api/v1/jobs/:id until state=completed
- [x] Test 6 — Verify SIMs: GET /api/v1/sims → 10 SIMs in active state, IPs assigned
- [x] Test 7 — Create Policy: POST /api/v1/policies → policy with DSL rules created
- [x] Test 8 — Activate Policy: POST /api/v1/policy-versions/:id/activate → policy active
- [x] Test 9 — Assign Policy: POST /api/v1/sims/bulk/policy-assign → policy assigned to SIMs
- [x] Test 10 — RADIUS Auth: send Access-Request for imported SIM → Access-Accept with policy QoS
- [x] Test 11 — Verify Session: GET /api/v1/sessions → session for authenticated SIM exists
- [x] Test 12 — WebSocket: connect to WS, verify session.started event received
- [x] All tests run in isolated Docker environment (docker compose up)
- [x] Test cleanup: teardown created resources after test suite
- [x] Test runs in CI pipeline (GitHub Actions) — gated by E2E=1 env var
- [x] Total test execution time < 60 seconds

## Dependencies
- Blocked by: All Phase 1-7 stories (requires full backend), STORY-041 (frontend scaffold)
- Blocks: None

## Test Scenarios
- [ ] Full flow succeeds end-to-end in clean environment
- [ ] Full flow succeeds after docker compose restart (state persisted)
- [ ] Individual test steps can run independently (for debugging)
- [ ] Test with invalid CSV → job completes with errors, valid SIMs still imported
- [ ] Test with suspended SIM → RADIUS Access-Reject returned
- [ ] Concurrent RADIUS auth from multiple SIMs → all succeed

## Effort Estimate
- Size: L
- Complexity: High
