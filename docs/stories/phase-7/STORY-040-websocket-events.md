# STORY-040: WebSocket Server & Real-Time Event Push

## User Story
As a portal user, I want real-time updates pushed to my browser via WebSocket, so that I see live session changes, alerts, job progress, and metrics without manual page refresh.

## Description
WebSocket server (SVC-02) on :8081 with JWT authentication on connection. Supports 10 event types pushed from NATS subscriptions to connected portal clients. Per-tenant event isolation (users only see their tenant's events). Connection management with heartbeat/ping, automatic reconnection guidance, and backpressure handling.

## Architecture Reference
- Services: SVC-02 (WebSocket Server — internal/ws)
- Packages: internal/ws
- Source: docs/architecture/api/_index.md (WebSocket Events section), docs/architecture/services/_index.md (SVC-02)

## Screen Reference
- SCR-010: Main Dashboard (live alert feed, metrics)
- SCR-050: Live Sessions (session.started, session.ended)
- SCR-080: Job List (job.progress, job.completed)
- SCR-100: Notifications (notification.new)
- SCR-062: Policy Editor (policy.rollout_progress)
- SCR-120: System Health (metrics.realtime)

## Acceptance Criteria
- [ ] WebSocket server listens on ws://host:8081/ws/v1/events
- [ ] JWT authentication: token passed as query param or first message, validated before event delivery
- [ ] Tenant isolation: events filtered by user's tenant_id (from JWT)
- [ ] 10 event types supported:
  - session.started: new AAA session created
  - session.ended: AAA session terminated
  - sim.state_changed: SIM state transition
  - operator.health_changed: operator health status change
  - alert.new: new anomaly or SLA alert
  - job.progress: job progress update (pct, processed, failed)
  - job.completed: job finished (success/failure)
  - notification.new: new notification for user
  - policy.rollout_progress: rollout stage advancement
  - metrics.realtime: auth/s, session count, latency (1s interval)
- [ ] Event payload: `{type, tenant_id, data, timestamp}`
- [ ] NATS subscription: SVC-02 subscribes to NATS topics, fan-out to connected clients
- [ ] Ping/pong heartbeat: every 30s, disconnect idle clients after 90s
- [ ] Backpressure: if client is slow, buffer up to 100 events, then drop oldest
- [ ] Connection count tracking: expose active_ws_connections metric
- [ ] Graceful shutdown: send close frame, drain pending events
- [ ] Client subscription: optional event type filter (subscribe only to desired events)
- [ ] Max connections per tenant: configurable (default 100)

## Dependencies
- Blocked by: STORY-001 (scaffold — NATS), STORY-003 (JWT auth)
- Blocks: STORY-041 (frontend scaffold — WS client), STORY-043 (frontend dashboard — live data)

## Test Scenarios
- [ ] Connect with valid JWT → connection accepted, events start flowing
- [ ] Connect with invalid JWT → connection rejected (4001 Unauthorized)
- [ ] Connect with expired JWT → connection rejected, reconnect with fresh token
- [ ] Tenant A event → only Tenant A clients receive it
- [ ] Session.started event → connected clients receive it within 100ms
- [ ] Metrics.realtime → received every 1 second
- [ ] Client subscribes to ["session.started","alert.new"] → only those events received
- [ ] Slow client → events buffered, oldest dropped when buffer full
- [ ] Ping timeout (90s no pong) → connection closed
- [ ] 101st connection for tenant → rejected (MAX_CONNECTIONS)
- [ ] Server shutdown → all clients receive close frame

## Effort Estimate
- Size: L
- Complexity: Medium
