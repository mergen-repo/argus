# Deliverable: STORY-038 — Notification Service (Multi-Channel)

## Summary

Extended STORY-021's foundational notification service into a full multi-channel notification engine. 5 delivery channels (in-app, email, Telegram, webhook, SMS), per-user preferences with scope/threshold configuration, delivery tracking with exponential backoff retry, rate limiting, and WebSocket real-time push. 5 REST API endpoints for notification management and preferences.

## Files Changed

### New Files
| File | Purpose |
|------|---------|
| `internal/notification/models.go` | Domain types: Channel, ScopeType, EventType, NotifyRequest |
| `internal/notification/email.go` | SMTP email sender with HTML templates |
| `internal/notification/telegram.go` | Telegram bot sender via Bot API |
| `internal/notification/webhook.go` | HTTP webhook sender with HMAC-SHA256 signature |
| `internal/notification/sms.go` | SMS gateway sender (Twilio/Vonage placeholder) |
| `internal/notification/delivery.go` | Delivery tracker with exponential backoff retry + rate limiter |
| `internal/store/notification.go` | NotificationStore (TBL-21) + NotificationConfigStore (TBL-22) |
| `internal/store/notification_test.go` | Store tests |
| `internal/api/notification/handler.go` | REST handler: API-130 to API-134 |
| `internal/notification/webhook_test.go` | HMAC + webhook tests |
| `internal/notification/delivery_test.go` | Delivery tracker + rate limiter tests |
| `migrations/20260322000004_notification_delivery.up.sql` | Delivery tracking columns |
| `migrations/20260322000004_notification_delivery.down.sql` | Down migration |

### Modified Files
| File | Change |
|------|--------|
| `internal/notification/service.go` | Extended with webhook/SMS, store, retry, rate limit, Notify() |
| `internal/notification/service_test.go` | 6 new tests |
| `internal/gateway/router.go` | 5 notification routes |
| `cmd/argus/main.go` | Wired stores, senders, delivery tracker, handler |

## API Endpoints
| Ref | Method | Path | Auth | Description |
|-----|--------|------|------|-------------|
| API-130 | GET | `/api/v1/notifications` | any | List notifications (unread first) |
| API-131 | PATCH | `/api/v1/notifications/:id/read` | any | Mark as read |
| API-132 | POST | `/api/v1/notifications/read-all` | any | Mark all as read |
| API-133 | GET | `/api/v1/notification-configs` | any | Get preferences |
| API-134 | PUT | `/api/v1/notification-configs` | any | Update preferences |

## Test Coverage
- 26 new tests, 949 total passing, 0 regressions
- Gate fix: SubjectNotification added to WS hub subscription list
