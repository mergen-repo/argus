# STORY-017: Session Management & Concurrent Control

## User Story
As a SIM manager, I want to view, manage, and control active sessions with idle/hard timeouts and force disconnect, so that I can monitor real-time connectivity and enforce usage policies.

## Description
Session management layer on top of RADIUS accounting: concurrent session control (max N sessions per SIM), idle timeout detection, hard session timeout, Redis-backed session cache for O(1) lookup, force disconnect via CoA/DM, and REST API for session listing/stats/disconnect. WebSocket events for real-time session updates on the portal.

## Architecture Reference
- Services: SVC-04 (AAA Engine), SVC-02 (WebSocket)
- API Endpoints: API-100 to API-103
- Database Tables: TBL-17 (sessions)
- Data Flows: FLW-01, FLW-02
- Source: docs/architecture/api/_index.md (Sessions section)

## Screen Reference
- SCR-050: Live Sessions — real-time session list with status, duration, usage
- SCR-021b: SIM Detail — Sessions tab

## Acceptance Criteria
- [ ] GET /api/v1/sessions lists active sessions with filters (operator, APN, SIM, duration, usage)
- [ ] GET /api/v1/sessions supports cursor-based pagination
- [ ] GET /api/v1/sessions/stats returns: total_active, by_operator, by_apn, avg_duration, avg_usage
- [ ] POST /api/v1/sessions/:id/disconnect sends CoA/DM to NAS, terminates session
- [ ] POST /api/v1/sessions/bulk/disconnect disconnects all sessions in segment
- [ ] Concurrent session control: if max_sessions_per_sim (from policy) exceeded, reject new or disconnect oldest
- [ ] Idle timeout: session with no accounting interim for N minutes → auto-disconnect
- [ ] Hard timeout: session exceeding max_session_duration → auto-disconnect
- [ ] Redis session cache: session state stored with TTL = hard_timeout + grace_period
- [ ] Session events published via NATS: session.started, session.ended → WebSocket push
- [ ] Session duration and usage (bytes_in, bytes_out) tracked in real-time via Redis
- [ ] Force disconnect creates audit log entry with reason and user who initiated
- [ ] Bulk disconnect runs as background job if segment count > 100

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-100 | GET | /api/v1/sessions | `?cursor&limit&sim_id&operator_id&apn_id&min_duration&min_usage` | `[{id,sim,operator,apn,nas_ip,started_at,duration,bytes_in,bytes_out,ip_address}]` | JWT(sim_manager+) | 200 |
| API-101 | GET | /api/v1/sessions/stats | — | `{total_active,by_operator:{},by_apn:{},avg_duration_sec,avg_bytes}` | JWT(analyst+) | 200 |
| API-102 | POST | /api/v1/sessions/:id/disconnect | `{reason?}` | `{id,state:"terminated",terminated_by}` | JWT(sim_manager+) | 200, 404 |
| API-103 | POST | /api/v1/sessions/bulk/disconnect | `{segment_id?,sim_ids?,reason}` | `{job_id?,disconnected_count?}` | JWT(tenant_admin) | 200, 202 |

## Dependencies
- Blocked by: STORY-015 (RADIUS server)
- Blocks: STORY-025 (policy rollout uses CoA), STORY-033 (real-time metrics)

## Test Scenarios
- [ ] List active sessions → returns only sessions with ended_at IS NULL
- [ ] Session stats → correct counts by operator and APN
- [ ] Force disconnect → CoA/DM sent, session terminated, event published
- [ ] Bulk disconnect on segment with >100 SIMs → job created (202)
- [ ] Concurrent session limit reached → oldest session disconnected on new auth
- [ ] Idle timeout triggered → session auto-disconnected after inactivity
- [ ] Hard timeout triggered → session terminated at max duration
- [ ] Redis session cache miss → fallback to TBL-17 query
- [ ] WebSocket clients receive session.started / session.ended events

## Effort Estimate
- Size: L
- Complexity: High
