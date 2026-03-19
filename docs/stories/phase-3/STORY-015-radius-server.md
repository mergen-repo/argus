# STORY-015: RADIUS Authentication & Accounting Server

## User Story
As a platform operator, I want Argus to serve as a RADIUS authentication and accounting server, so that operator P-GWs can authenticate IoT SIMs and report session usage through standard AAA protocols.

## Description
Implement a RADIUS server listening on UDP :1812 (authentication) and :1813 (accounting) per RFC 2865/2866. Uses the layeh/radius Go library. Handles Access-Request → Access-Accept/Reject with Framed-IP and QoS attributes from policy. Handles Accounting-Request Start/Interim-Update/Stop to manage session lifecycle. Supports Change-of-Authorization (CoA) and Disconnect-Message (DM) for mid-session policy changes. Integrates with Redis for session cache and NATS for event publishing.

## Architecture Reference
- Services: SVC-04 (AAA Engine — internal/aaa)
- API Endpoints: API-180 (health check includes AAA status)
- Database Tables: TBL-17 (sessions), TBL-10 (sims), TBL-09 (ip_addresses)
- Data Flows: FLW-01 (RADIUS Authentication), FLW-02 (RADIUS Accounting)
- Packages: internal/aaa, internal/protocol/radius, internal/cache
- Source: docs/architecture/services/_index.md (SVC-04), docs/architecture/flows/_index.md (FLW-01, FLW-02)
- Spec: docs/architecture/PROTOCOLS.md (RADIUS section), docs/architecture/ALGORITHMS.md (Sections 1, 6, 8), docs/architecture/CONFIG.md (AAA section)

## Screen Reference
- SCR-050: Live Sessions (session created/updated by RADIUS events)
- SCR-120: System Health (AAA server status)

## Acceptance Criteria
- [ ] RADIUS server listens on UDP :1812 (auth) and :1813 (accounting)
- [ ] Access-Request: parse IMSI from User-Name, lookup SIM in Redis (fallback to DB)
- [ ] Access-Request: validate SIM state is ACTIVE, operator is healthy
- [ ] Access-Request: delegate to Policy Engine (SVC-05) for rule evaluation
- [ ] Access-Accept: include Framed-IP-Address, Session-Timeout, QoS attributes from policy
- [ ] Access-Reject: include Reply-Message with reject reason code
- [ ] Accounting-Request (Start): create session in Redis + TBL-17, publish session.started via NATS
- [ ] Accounting-Request (Interim-Update): update bytes_in/bytes_out in Redis session cache
- [ ] Accounting-Request (Stop): finalize session in TBL-17, remove from Redis, publish session.ended
- [ ] CoA (Change-of-Authorization): send mid-session policy update to NAS
- [ ] DM (Disconnect-Message): force disconnect active session from NAS
- [ ] Shared secret per operator (from TBL-05 operator config)
- [ ] RADIUS packet logging with correlation ID for debugging
- [ ] Health check reports AAA server status in API-180 response
- [ ] Graceful shutdown: stop accepting new requests, drain in-flight within 5s

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-180 | GET | /api/health | — | `{ db, redis, nats, aaa: { radius: "ok", sessions_active: int } }` | None | 200, 503 |

## Database Changes
- Migration: `20260318030001_create_sessions.up.sql`
- TBL-17 (sessions): TimescaleDB hypertable partitioned by started_at
- Indexes: idx_sessions_sim_id, idx_sessions_operator_id, idx_sessions_active (WHERE ended_at IS NULL)

## Dependencies
- Blocked by: STORY-001 (scaffold), STORY-002 (DB schema), STORY-011 (SIM CRUD), STORY-010 (APN/IP)
- Blocks: STORY-016 (EAP-SIM/AKA), STORY-017 (session management), STORY-018 (operator adapter), STORY-032 (CDR processing)

## Test Scenarios
- [ ] Valid Access-Request with known IMSI → Access-Accept with Framed-IP
- [ ] Access-Request with unknown IMSI → Access-Reject (SIM_NOT_FOUND)
- [ ] Access-Request with suspended SIM → Access-Reject (SIM_SUSPENDED)
- [ ] Accounting-Start → session created in Redis and TBL-17, WS event published
- [ ] Accounting-Interim → session counters updated in Redis
- [ ] Accounting-Stop → session finalized in TBL-17, removed from Redis
- [ ] CoA sent to NAS → session updated with new policy attributes
- [ ] DM sent to NAS → session terminated
- [ ] Invalid shared secret → silent drop (per RFC 2865)
- [ ] Malformed packet → discard with error log
- [ ] Concurrent Access-Requests for same IMSI handled correctly (no race)

## Effort Estimate
- Size: XL
- Complexity: High
