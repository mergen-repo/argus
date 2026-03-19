# STORY-038: Notification Service

## User Story
As a user, I want to receive notifications via in-app, email, Telegram, webhook, and SMS channels with configurable per-scope thresholds, so that I am alerted about important events without notification fatigue.

## Description
Multi-channel notification service: in-app (stored in TBL-21), email (SMTP), Telegram bot, webhook (HTTP POST), and SMS gateway. Notification scopes: per-SIM, per-APN, per-operator, and system-wide. Configurable percentage thresholds per event type (e.g., "alert me when operator error rate exceeds 5%"). User preferences stored in TBL-22 (notification_configs). Delivery tracking and retry for failed deliveries.

## Architecture Reference
- Services: SVC-08 (Notification Service — internal/notification)
- API Endpoints: API-130 to API-134, API-170 to API-171 (SMS Gateway)
- Database Tables: TBL-21 (notifications), TBL-22 (notification_configs)
- Source: docs/architecture/api/_index.md (Notifications section), docs/architecture/services/_index.md (SVC-08)

## Screen Reference
- SCR-100: Notifications — notification center drawer, unread count badge
- SCR-113: Notification Config — channel preferences, event subscriptions, threshold settings

## Acceptance Criteria
- [ ] In-app notifications: stored in TBL-21, unread count via badge, mark as read
- [ ] Email delivery: SMTP integration, HTML templates per event type, retry on failure
- [ ] Telegram bot: send messages to configured chat_id, support rich formatting
- [ ] Webhook delivery: HTTP POST to configured URL with JSON payload, HMAC signature
- [ ] SMS gateway: send SMS via configurable provider (Twilio/Vonage placeholder)
- [ ] GET /api/v1/notifications lists notifications (unread first, cursor pagination)
- [ ] PATCH /api/v1/notifications/:id/read marks single notification as read
- [ ] POST /api/v1/notifications/read-all marks all as read for user
- [ ] GET /api/v1/notification-configs returns user's notification preferences
- [ ] PUT /api/v1/notification-configs updates preferences (channels, events, thresholds)
- [ ] Notification scopes: per-SIM (usage threshold), per-APN (traffic threshold), per-operator (health), system (auth rate)
- [ ] Percentage thresholds: "notify when usage exceeds 80% of quota"
- [ ] Event types: operator.down, sim.state_changed, job.completed, job.failed, alert.new, sla.violation, policy.rollout_completed, quota.warning, quota.exceeded
- [ ] Delivery tracking: sent_at, delivered_at, failed_at, retry_count per notification per channel
- [ ] Retry: exponential backoff (1s, 5s, 30s, 5min) for failed deliveries, max 5 retries
- [ ] Rate limiting: max 10 notifications per user per minute (configurable, burst protection)
- [ ] WebSocket: push notification.new event for in-app real-time delivery

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-130 | GET | /api/v1/notifications | `?cursor&limit&unread_only` | `[{id,type,title,message,scope,read,created_at}]` | JWT(any) | 200 |
| API-131 | PATCH | /api/v1/notifications/:id/read | — | `{id,read:true}` | JWT(any) | 200, 404 |
| API-132 | POST | /api/v1/notifications/read-all | — | `{updated_count}` | JWT(any) | 200 |
| API-133 | GET | /api/v1/notification-configs | — | `{channels:{email,telegram,webhook,sms},events:{...},thresholds:{...}}` | JWT(any) | 200 |
| API-134 | PUT | /api/v1/notification-configs | `{channels:{...},events:{...},thresholds:{...}}` | `{updated_at}` | JWT(any) | 200, 400 |

## Dependencies
- Blocked by: STORY-002 (DB — TBL-21, TBL-22), STORY-003 (auth — user context)
- Blocks: STORY-036 (anomaly alerts), STORY-021 (operator failover alerts), STORY-050 (frontend notification center)

## Test Scenarios
- [ ] Create in-app notification → stored in TBL-21, WebSocket notification.new pushed
- [ ] List notifications → unread first, then read, cursor paginated
- [ ] Mark as read → read=true, unread count decremented
- [ ] Mark all as read → all user notifications marked read
- [ ] Email delivery → SMTP send, delivery tracked
- [ ] Telegram delivery → message sent to configured chat_id
- [ ] Webhook delivery → HTTP POST with HMAC signature, 200 response
- [ ] Webhook failure → retry with exponential backoff
- [ ] Rate limiting → 11th notification in 1 min → queued, not dropped
- [ ] Threshold notification: usage at 80% of quota → quota.warning sent
- [ ] Update preferences → only subscribed events trigger notifications

## Effort Estimate
- Size: XL
- Complexity: High
