# STORY-038 Gate Review: Notification Service (Multi-Channel)

**Date:** 2026-03-22
**Reviewer:** Gate Agent
**Result:** PASS

---

## Pass 1 — Structural & Compilation

| Check | Result |
|-------|--------|
| All 12 new files present | PASS |
| All 5 modified files consistent | PASS |
| `go build ./internal/notification/...` | PASS |
| `go build ./internal/store/...` | PASS |
| `go build ./internal/api/notification/...` | PASS |
| `go build ./cmd/argus/...` | PASS |
| `go vet` on all affected packages | PASS |
| Migration up/down symmetry | PASS |
| No import cycles | PASS |

## Pass 2 — AC Verification

| # | Acceptance Criteria | Status | Evidence |
|---|---------------------|--------|----------|
| 1 | In-app notifications in TBL-21, unread count, mark read | PASS | `store/notification.go`: Create, UnreadCount, MarkRead, MarkAllRead. TBL-21 in core_schema.up.sql lines 512-526 |
| 2 | Email SMTP with templates | PASS | `notification/email.go`: SMTPEmailSender with HTML template, TLS support |
| 3 | Telegram bot | PASS | `notification/telegram.go`: TelegramBotSender with Markdown parse_mode, chat_id routing |
| 4 | Webhook with HMAC-SHA256 | PASS | `notification/webhook.go`: HMAC-SHA256 signature in X-Argus-Signature header, ComputeHMAC/VerifyHMAC |
| 5 | SMS placeholder | PASS | `notification/sms.go`: SMSGatewaySender with twilio/vonage stubs returning "not yet implemented" |
| 6 | GET /api/v1/notifications (API-130) | PASS | `api/notification/handler.go` List, router.go line 430 |
| 7 | PATCH /api/v1/notifications/:id/read (API-131) | PASS | handler.go MarkRead, router.go line 431 |
| 8 | POST /api/v1/notifications/read-all (API-132) | PASS | handler.go MarkAllRead, router.go line 432 |
| 9 | GET /api/v1/notification-configs (API-133) | PASS | handler.go GetConfigs, router.go line 433 |
| 10 | PUT /api/v1/notification-configs (API-134) | PASS | handler.go UpdateConfigs, router.go line 434 |
| 11 | Scopes: per-SIM, per-APN, per-operator, system | PASS | `models.go`: ScopeSystem/ScopeSIM/ScopeAPN/ScopeOperator; handler validates 4 scopes |
| 12 | Percentage thresholds | PASS | `NotificationConfig` has ThresholdType + ThresholdValue; TBL-22 has threshold_type/threshold_value columns |
| 13 | Event types (11 defined) | PASS | `models.go`: 11 event types including operator.down/recovered, sim.state_changed, job.completed/failed, alert.new, sla.violation, policy.rollout_completed, quota.warning/exceeded, anomaly.detected |
| 14 | Delivery tracking | PASS | Migration adds sent_at, delivered_at, failed_at, retry_count, delivery_meta columns. `store/notification.go` UpdateDelivery method |
| 15 | Retry with exponential backoff | PASS | `delivery.go`: retryBackoffs [1s, 5s, 30s, 5min, 5min], maxRetries=5, DeliveryTracker with goroutine loop |
| 16 | Rate limiting (10/user/min) | PASS | `delivery.go` CheckRateLimit with limit=10, window=1min; service.go Notify checks before dispatch |
| 17 | WebSocket notification.new | PASS | ws/hub.go maps "argus.events.notification.dispatch" -> "notification.new"; service.go publishes to SubjectNotification |

## Pass 3 — Test Results

| Suite | Tests | Result |
|-------|-------|--------|
| `internal/notification` | 22 (service: 11, webhook: 3, delivery: 6, models: 2) | ALL PASS |
| `internal/store` (notification tests) | 4 (constructors, params validation) | ALL PASS |
| Full suite | 949 passing, 1 flaky (pre-existing TestRecordAuth_ErrorRate in analytics/metrics) | PASS |

## Pass 4 — Wiring & Integration

| Check | Result |
|-------|--------|
| NotificationStore created in main.go | PASS (line 312) |
| NotificationConfigStore created in main.go | PASS (line 313) |
| NotifSvc wired with store adapter | PASS (line 334) |
| EventPublisher wired for WS push | PASS (line 335) |
| DeliveryTracker wired | PASS (line 337-338) |
| NATS subscription for health + alert subjects | PASS (line 340) |
| Handler wired to RouterDeps | PASS (line 509) |
| All 5 routes registered under JWT(api_user) | PASS (router.go lines 426-436) |
| Graceful shutdown calls notifSvc.Stop() | PASS (line 594) |
| WebSocket hub subscribed to SubjectNotification | PASS (fixed: added to subscription list) |

## Pass 5 — Quality & Edge Cases

| Check | Result | Notes |
|-------|--------|-------|
| Nil-safe channel dispatch | PASS | Each channel checked for nil before send |
| Rate limit nil-safety | PASS | DeliveryTracker.CheckRateLimit returns true when rateLimiter is nil |
| Cursor pagination | PASS | ListByUser uses cursor-based pagination with limit+1 pattern |
| Unread-first ordering | PASS | ORDER BY CASE WHEN state='unread' THEN 0 ELSE 1 END |
| Tenant scoping | PASS | All store queries include tenant_id filter |
| Config validation | PASS | Handler validates event_type and scope_type against known sets |
| HMAC timing-safe comparison | PASS | Uses hmac.Equal for constant-time comparison |
| Retry stop safety | PASS | ScheduleRetry checks dt.stopped before appending |
| Migration idempotent | PASS | Uses IF NOT EXISTS for indexes, IF NOT EXISTS for columns |

## Fix Applied

| # | Issue | Fix |
|---|-------|-----|
| 1 | WebSocket hub not subscribed to `bus.SubjectNotification` | Added `bus.SubjectNotification` to wsHub.SubscribeToNATS subject list in `cmd/argus/main.go` |

## Notes

- Webhook and SMS channels are implemented at the service layer but not wired in main.go (no config vars). This is by design: webhook URL/secret come from per-user notification_configs, and SMS is an explicit placeholder per the story spec.
- The flaky `TestRecordAuth_ErrorRate` failure in `internal/analytics/metrics` is pre-existing (passes when run in isolation, fails intermittently in full suite due to timing). Not related to STORY-038.
- Event types: story spec lists 9 event types but implementation has 11 (adds `operator.recovered` and `anomaly.detected`). This is a superset -- acceptable.

## Verdict

**PASS** -- All 17 ACs verified, 22 notification tests + 4 store tests passing, 1 fix applied (WebSocket subscription), no regressions.
