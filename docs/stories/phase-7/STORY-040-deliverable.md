# Deliverable: STORY-040 — WebSocket Server & Real-Time Event Push

## Summary

Extended STORY-021's WS hub into a full WebSocket server with gorilla/websocket upgrade handler, JWT authentication (query param + first-message), ping/pong heartbeat, per-tenant max connections, client event subscription filtering, and graceful shutdown. 10 event types relayed from NATS to connected portal clients with tenant isolation.

## Files Changed

### New Files
| File | Purpose |
|------|---------|
| `internal/ws/server.go` | WS upgrade handler, JWT auth, read/write pumps, heartbeat |
| `internal/ws/server_test.go` | 28 comprehensive tests |

### Modified Files
| File | Change |
|------|--------|
| `internal/ws/hub.go` | TenantConnectionCount, Connection fields, policy subject mapping |
| `internal/config/config.go` | WSMaxConnsPerTenant config (default 100) |
| `cmd/argus/main.go` | WS server on :8081, NATS subscriptions, graceful shutdown |
| `go.mod` | gorilla/websocket v1.5.3 |

## Key Features
- Server: `ws://host:8081/ws/v1/events`
- JWT auth: query param `?token=` or first-message within 5s
- 10 event types: session.started/ended, sim.state_changed, operator.health_changed, alert.new, job.progress/completed, notification.new, policy.rollout_progress, metrics.realtime
- Tenant isolation: events filtered by JWT tenant_id
- Ping/pong heartbeat: 30s interval, 40s pong timeout
- Backpressure: buffer 256 events, drop on overflow
- Max 100 connections per tenant (configurable)
- Client subscribe message: filter event types
- Graceful shutdown: CloseGoingAway frames
- Custom close codes: 4001 (unauthorized), 4002 (max conns), 4003 (auth timeout)

## Test Coverage
- 28 new tests + 11 existing = 39 total WS tests
- All packages passing, 0 regressions
