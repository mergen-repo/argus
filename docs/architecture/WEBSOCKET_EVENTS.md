# WebSocket Events — Argus

> Endpoint: `ws://host/ws/v1/events` (wss:// in production)
> Implementation: `internal/ws/`
> Transport: gorilla/websocket
> All events are tenant-scoped — clients only receive events for their own tenant.

## Connection

### Authentication

Two methods (in order of preference):

**Method 1: Query parameter (recommended for initial connection)**
```
wss://argus.example.com/ws/v1/events?token=<jwt_access_token>
```

**Method 2: First message authentication**
```
ws.connect("wss://argus.example.com/ws/v1/events")
// After connection, send auth message within 5 seconds:
ws.send(JSON.stringify({ "type": "auth", "token": "<jwt_access_token>" }))
// Server responds:
{ "type": "auth.ok", "data": { "tenant_id": "uuid", "user_id": "uuid", "role": "sim_manager" } }
// Or on failure:
{ "type": "auth.error", "data": { "code": "TOKEN_EXPIRED", "message": "Access token has expired" } }
// Connection is closed after auth failure.
```

If no auth message is received within 5 seconds (method 2) and no query param token was provided, the connection is closed with WebSocket close code 4001.

### Heartbeat

- **Ping/Pong**: Server sends WebSocket ping frame every 30 seconds.
- **Pong timeout**: If no pong received within 10 seconds, server closes the connection.
- **Client-side**: Standard WebSocket implementations handle pong automatically. If implementing custom client, respond to ping with pong.

### Reconnection Strategy

Clients should implement exponential backoff reconnection:

```
Attempt 1: wait 1s
Attempt 2: wait 2s
Attempt 3: wait 4s
Attempt 4: wait 8s
Attempt 5: wait 16s
Attempt N: wait min(2^N, 60s) seconds
```

On reconnect:
1. Re-authenticate (token may have been refreshed).
2. Fetch missed events via REST API if needed (e.g., `GET /api/v1/sessions?since=<last_event_timestamp>`).
3. Reset backoff counter on successful connection.

### Subscription Filtering (Optional)

After authentication, clients can subscribe to specific event types:

```json
// Subscribe to specific events only
{ "type": "subscribe", "events": ["session.started", "session.ended", "alert.new"] }

// Subscribe to all events (default behavior if no subscribe message sent)
{ "type": "subscribe", "events": ["*"] }

// Server confirms:
{ "type": "subscribe.ok", "data": { "events": ["session.started", "session.ended", "alert.new"] } }
```

---

## Event Envelope

All events share this structure:

```json
{
  "type": "event.name",
  "id": "evt_unique_id",
  "timestamp": "2026-03-18T14:02:00.123Z",
  "data": { ... }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Event type identifier (dot-separated) |
| `id` | string | Unique event ID (for deduplication on reconnect) |
| `timestamp` | string (ISO 8601) | Server-side event creation time |
| `data` | object | Event-specific payload (see below) |

---

## Event Types

### 1. session.started

Fired when a new AAA session is established (RADIUS Access-Accept or Diameter CCA-I sent).

```json
{
  "type": "session.started",
  "id": "evt_a1b2c3d4",
  "timestamp": "2026-03-18T14:02:00.123Z",
  "data": {
    "session_id": "550e8400-e29b-41d4-a716-446655440000",
    "sim_id": "660e8400-e29b-41d4-a716-446655440001",
    "iccid": "89901112345678901234",
    "imsi": "286010123456789",
    "msisdn": "+905321234567",
    "operator_id": "770e8400-e29b-41d4-a716-446655440002",
    "operator_name": "turkcell",
    "apn_id": "880e8400-e29b-41d4-a716-446655440003",
    "apn_name": "iot.fleet",
    "rat_type": "lte_m",
    "ip_address": "10.0.1.42",
    "ip_v6_address": null,
    "nas_ip": "192.168.1.100",
    "policy_name": "iot-fleet-standard",
    "policy_version": 3,
    "started_at": "2026-03-18T14:02:00.123Z"
  }
}
```

### 2. session.ended

Fired when an AAA session terminates (RADIUS Accounting-Stop or Diameter CCR-T).

```json
{
  "type": "session.ended",
  "id": "evt_e5f6g7h8",
  "timestamp": "2026-03-18T15:02:00.456Z",
  "data": {
    "session_id": "550e8400-e29b-41d4-a716-446655440000",
    "sim_id": "660e8400-e29b-41d4-a716-446655440001",
    "iccid": "89901112345678901234",
    "imsi": "286010123456789",
    "operator_name": "turkcell",
    "apn_name": "iot.fleet",
    "rat_type": "lte_m",
    "duration_sec": 3600,
    "bytes_in": 1234567,
    "bytes_out": 345678,
    "total_bytes": 1580245,
    "terminate_cause": "idle_timeout",
    "ip_address": "10.0.1.42",
    "started_at": "2026-03-18T14:02:00.123Z",
    "ended_at": "2026-03-18T15:02:00.456Z"
  }
}
```

`terminate_cause` values: `user_request`, `idle_timeout`, `session_timeout`, `admin_disconnect`, `policy_disconnect`, `nas_reboot`, `nas_error`, `operator_disconnect`, `lost_carrier`, `port_error`.

### 3. sim.state_changed

Fired when a SIM transitions between states.

```json
{
  "type": "sim.state_changed",
  "id": "evt_i9j0k1l2",
  "timestamp": "2026-03-18T14:05:00.789Z",
  "data": {
    "sim_id": "660e8400-e29b-41d4-a716-446655440001",
    "iccid": "89901112345678901234",
    "imsi": "286010123456789",
    "from_state": "active",
    "to_state": "suspended",
    "reason": "Quota exceeded - policy auto-suspend",
    "triggered_by": "policy",
    "user_id": null,
    "job_id": null,
    "operator_name": "turkcell",
    "apn_name": "iot.fleet"
  }
}
```

`triggered_by` values: `user`, `policy`, `system`, `bulk_job`.

### 4. operator.health_changed

Fired when operator health status changes (healthy/degraded/down transitions).

```json
{
  "type": "operator.health_changed",
  "id": "evt_m3n4o5p6",
  "timestamp": "2026-03-18T14:10:00.012Z",
  "data": {
    "operator_id": "770e8400-e29b-41d4-a716-446655440002",
    "operator_name": "turkcell",
    "previous_status": "healthy",
    "current_status": "degraded",
    "uptime_24h_pct": 99.2,
    "latency_ms_p95": 45,
    "consecutive_failures": 3,
    "circuit_breaker_state": "half_open",
    "last_successful_check": "2026-03-18T14:08:00.000Z",
    "last_failed_check": "2026-03-18T14:10:00.000Z",
    "failure_reason": "Connection timeout after 5000ms",
    "affected_sim_count": 234567
  }
}
```

`current_status` values: `healthy`, `degraded`, `down`.
`circuit_breaker_state` values: `closed` (normal), `open` (rejecting), `half_open` (testing recovery).

### 5. alert.new

Fired when a new alert/anomaly is detected.

```json
{
  "type": "alert.new",
  "id": "evt_q7r8s9t0",
  "timestamp": "2026-03-18T14:12:00.345Z",
  "data": {
    "alert_id": "990e8400-e29b-41d4-a716-446655440010",
    "alert_type": "anomaly_detected",
    "severity": "critical",
    "title": "Possible SIM cloning detected",
    "description": "IMSI 286010123456789 authenticated from 2 different NAS IPs (192.168.1.100, 10.20.30.40) within 3 minutes",
    "entity_type": "sim",
    "entity_id": "660e8400-e29b-41d4-a716-446655440001",
    "entity_identifier": "IMSI 286010123456789",
    "metadata": {
      "detection_rule": "sim_cloning",
      "nas_ips": ["192.168.1.100", "10.20.30.40"],
      "time_window_seconds": 180
    },
    "suggested_action": "Investigate SIM and consider suspending if confirmed"
  }
}
```

`alert_type` values: `anomaly_detected`, `sla_violation`, `quota_warning`, `quota_exceeded`, `pool_threshold`, `operator_down`, `compliance_alert`.
`severity` values: `info`, `warning`, `critical`.

### 6. job.progress

Fired periodically during job execution (every 1% progress or every 5 seconds, whichever comes first).

```json
{
  "type": "job.progress",
  "id": "evt_u1v2w3x4",
  "timestamp": "2026-03-18T14:15:00.678Z",
  "data": {
    "job_id": "aa0e8400-e29b-41d4-a716-446655440020",
    "job_type": "bulk_import",
    "state": "running",
    "total_items": 10000,
    "processed_items": 4523,
    "failed_items": 12,
    "progress_pct": 45.23,
    "estimated_remaining_sec": 120,
    "items_per_second": 37.7,
    "started_at": "2026-03-18T14:13:00.000Z"
  }
}
```

### 7. job.completed

Fired when a job finishes (success, failure, or cancellation).

```json
{
  "type": "job.completed",
  "id": "evt_y5z6a7b8",
  "timestamp": "2026-03-18T14:20:00.901Z",
  "data": {
    "job_id": "aa0e8400-e29b-41d4-a716-446655440020",
    "job_type": "bulk_import",
    "final_state": "completed",
    "total_items": 10000,
    "processed_items": 10000,
    "failed_items": 12,
    "success_items": 9988,
    "progress_pct": 100.0,
    "duration_sec": 420,
    "items_per_second": 23.8,
    "started_at": "2026-03-18T14:13:00.000Z",
    "completed_at": "2026-03-18T14:20:00.901Z",
    "error_report_available": true,
    "result_summary": "9,988 SIMs imported successfully. 12 failed (duplicate ICCID)."
  }
}
```

`final_state` values: `completed`, `failed`, `cancelled`.

### 8. notification.new

Fired when a new in-app notification is created for the connected user.

```json
{
  "type": "notification.new",
  "id": "evt_c9d0e1f2",
  "timestamp": "2026-03-18T14:22:00.234Z",
  "data": {
    "notification_id": "bb0e8400-e29b-41d4-a716-446655440030",
    "event_type": "quota_warning",
    "scope_type": "apn",
    "scope_ref_id": "880e8400-e29b-41d4-a716-446655440003",
    "scope_name": "iot.fleet",
    "title": "APN quota warning: iot.fleet at 80%",
    "body": "APN 'iot.fleet' has reached 80% of its monthly data quota. 234 SIMs affected.",
    "severity": "warning",
    "channels_sent": ["in_app", "email"],
    "created_at": "2026-03-18T14:22:00.234Z",
    "action_url": "/apns/880e8400-e29b-41d4-a716-446655440003"
  }
}
```

### 9. policy.rollout_progress

Fired when a policy staged rollout advances or completes a stage.

```json
{
  "type": "policy.rollout_progress",
  "id": "evt_g3h4i5j6",
  "timestamp": "2026-03-18T14:25:00.567Z",
  "data": {
    "rollout_id": "cc0e8400-e29b-41d4-a716-446655440040",
    "policy_id": "dd0e8400-e29b-41d4-a716-446655440050",
    "policy_name": "iot-fleet-standard",
    "from_version": 2,
    "to_version": 3,
    "state": "in_progress",
    "current_stage": 2,
    "total_stages": 3,
    "stages": [
      { "index": 0, "pct": 1, "status": "completed", "sim_count": 23456 },
      { "index": 1, "pct": 10, "status": "completed", "sim_count": 234560 },
      { "index": 2, "pct": 100, "status": "in_progress", "sim_count": 2345600, "migrated": 1500000 }
    ],
    "total_sims": 2345600,
    "migrated_sims": 1758016,
    "progress_pct": 74.97,
    "coa_sent_count": 1758016,
    "coa_acked_count": 1757800,
    "coa_failed_count": 216,
    "started_at": "2026-03-18T13:00:00.000Z"
  }
}
```

### 10. metrics.realtime

Fired every 1 second. Contains aggregated real-time metrics for the tenant dashboard.

```json
{
  "type": "metrics.realtime",
  "id": "evt_k7l8m9n0",
  "timestamp": "2026-03-18T14:30:01.000Z",
  "data": {
    "auth_requests_per_sec": 1234,
    "auth_success_per_sec": 1210,
    "auth_reject_per_sec": 24,
    "acct_requests_per_sec": 3456,
    "active_sessions": 4234567,
    "sessions_started_1m": 567,
    "sessions_ended_1m": 543,
    "avg_latency_ms": 4.2,
    "p95_latency_ms": 18.5,
    "p99_latency_ms": 42.3,
    "bytes_in_per_sec": 123456789,
    "bytes_out_per_sec": 34567890,
    "error_rate_pct": 0.02,
    "operator_health": {
      "turkcell": { "status": "healthy", "latency_ms": 12 },
      "vodafone": { "status": "healthy", "latency_ms": 8 },
      "turk_telekom": { "status": "degraded", "latency_ms": 89 }
    }
  }
}
```

---

## Server-to-Client Control Messages

| Type | Direction | Purpose |
|------|-----------|---------|
| `auth.ok` | Server → Client | Authentication successful |
| `auth.error` | Server → Client | Authentication failed |
| `subscribe.ok` | Server → Client | Subscription confirmed |
| `error` | Server → Client | General error (malformed message, etc.) |
| `reconnect` | Server → Client | Server requests client to reconnect (e.g., before maintenance) |

### Reconnect Message

```json
{
  "type": "reconnect",
  "data": {
    "reason": "Server maintenance scheduled",
    "delay_seconds": 5
  }
}
```

Client should close the connection and reconnect after the specified delay.

---

## Implementation Notes

### Server Architecture

```
NATS subscriber (per tenant)
    │
    ├─ Receives events from all services via NATS JetStream
    ├─ Filters by tenant_id
    ├─ Broadcasts to all WebSocket connections for that tenant
    └─ Further filters by user subscription preferences
```

### Connection Limits

| Metric | Limit |
|--------|-------|
| Max connections per tenant | 100 |
| Max connections per user | 5 |
| Message buffer per connection | 256 messages |
| Max message size (client → server) | 4 KB |
| Max message size (server → client) | 64 KB |

### Backpressure

If a client is slow to consume messages:
1. Messages are buffered (up to 256).
2. If buffer is full, oldest messages are dropped.
3. If client does not read for 60 seconds, connection is closed.

Dropped messages are logged server-side. Clients can detect gaps via sequential `id` fields and refetch via REST API.
