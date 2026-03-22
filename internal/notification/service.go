package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Channel string

const (
	ChannelEmail    Channel = "email"
	ChannelTelegram Channel = "telegram"
	ChannelInApp    Channel = "in_app"
)

type AlertPayload struct {
	AlertID     string                 `json:"alert_id"`
	AlertType   string                 `json:"alert_type"`
	Severity    string                 `json:"severity"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	EntityType  string                 `json:"entity_type"`
	EntityID    uuid.UUID              `json:"entity_id"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Timestamp   time.Time              `json:"timestamp"`
}

type HealthChangedPayload struct {
	OperatorID     uuid.UUID `json:"operator_id"`
	OperatorName   string    `json:"operator_name,omitempty"`
	PreviousStatus string    `json:"previous_status"`
	CurrentStatus  string    `json:"current_status"`
	CircuitState   string    `json:"circuit_breaker_state"`
	LatencyMs      int       `json:"latency_ms,omitempty"`
	FailureReason  string    `json:"failure_reason,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

type EmailSender interface {
	SendAlert(ctx context.Context, subject, body string) error
}

type TelegramSender interface {
	SendMessage(ctx context.Context, message string) error
}

type InAppStore interface {
	CreateNotification(ctx context.Context, n InAppNotification) error
}

type InAppNotification struct {
	ID           uuid.UUID `json:"id"`
	AlertType    string    `json:"alert_type"`
	Severity     string    `json:"severity"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	EntityType   string    `json:"entity_type"`
	EntityID     uuid.UUID `json:"entity_id"`
	ChannelsSent []string  `json:"channels_sent"`
	CreatedAt    time.Time `json:"created_at"`
}

type Subscriber interface {
	QueueSubscribe(subject, queue string, handler func(string, []byte)) (Subscription, error)
}

type Subscription interface {
	Unsubscribe() error
}

type EventPublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

type NotifStore interface {
	Create(ctx context.Context, p NotifCreateParams) (*NotifRow, error)
	UpdateDelivery(ctx context.Context, id uuid.UUID, sentAt, deliveredAt, failedAt *time.Time, retryCount int, channelsSent []string) error
}

type NotifCreateParams struct {
	TenantID     uuid.UUID
	UserID       *uuid.UUID
	EventType    string
	ScopeType    string
	ScopeRefID   *uuid.UUID
	Title        string
	Body         string
	Severity     string
	ChannelsSent []string
}

type NotifRow struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	CreatedAt time.Time `json:"created_at"`
}

type WebhookDispatcher interface {
	SendWebhook(ctx context.Context, url, secret, payload string) error
}

type SMSDispatcher interface {
	SendSMS(ctx context.Context, phoneNumber, message string) error
}

type Config struct {
	Channels      []Channel
	HealthSubject string
	AlertSubject  string
}

type Service struct {
	email    EmailSender
	telegram TelegramSender
	inApp    InAppStore
	webhook  WebhookDispatcher
	sms      SMSDispatcher
	channels []Channel
	logger   zerolog.Logger

	notifStore     NotifStore
	eventPublisher EventPublisher
	delivery       *DeliveryTracker
	notifSubject   string

	mu   sync.Mutex
	subs []Subscription
}

func NewService(email EmailSender, telegram TelegramSender, inApp InAppStore, channels []Channel, logger zerolog.Logger) *Service {
	return &Service{
		email:    email,
		telegram: telegram,
		inApp:    inApp,
		channels: channels,
		logger:   logger.With().Str("component", "notification").Logger(),
	}
}

func (s *Service) SetWebhook(w WebhookDispatcher) {
	s.webhook = w
}

func (s *Service) SetSMS(sms SMSDispatcher) {
	s.sms = sms
}

func (s *Service) SetNotifStore(ns NotifStore) {
	s.notifStore = ns
}

func (s *Service) SetEventPublisher(ep EventPublisher, notifSubject string) {
	s.eventPublisher = ep
	s.notifSubject = notifSubject
}

func (s *Service) SetDeliveryTracker(dt *DeliveryTracker) {
	s.delivery = dt
}

func (s *Service) Start(subscriber Subscriber, healthSubject, alertSubject string) error {
	sub1, err := subscriber.QueueSubscribe(healthSubject, "notification-svc", func(subject string, data []byte) {
		s.handleHealthChanged(data)
	})
	if err != nil {
		return fmt.Errorf("notification: subscribe health: %w", err)
	}

	sub2, err := subscriber.QueueSubscribe(alertSubject, "notification-svc", func(subject string, data []byte) {
		s.handleAlert(data)
	})
	if err != nil {
		sub1.Unsubscribe()
		return fmt.Errorf("notification: subscribe alert: %w", err)
	}

	s.mu.Lock()
	s.subs = append(s.subs, sub1, sub2)
	s.mu.Unlock()

	s.logger.Info().
		Str("health_subject", healthSubject).
		Str("alert_subject", alertSubject).
		Int("channels", len(s.channels)).
		Msg("notification service started")

	return nil
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.subs {
		sub.Unsubscribe()
	}
	s.subs = nil

	if s.delivery != nil {
		s.delivery.Stop()
	}

	s.logger.Info().Msg("notification service stopped")
}

func (s *Service) Notify(ctx context.Context, req NotifyRequest) error {
	if s.delivery != nil && req.UserID != nil {
		allowed, err := s.delivery.CheckRateLimit(ctx, req.UserID.String())
		if err != nil {
			s.logger.Warn().Err(err).Msg("rate limit check failed, allowing")
		} else if !allowed {
			s.logger.Warn().
				Str("user_id", req.UserID.String()).
				Str("event_type", string(req.EventType)).
				Msg("notification rate limited")
			return fmt.Errorf("notification: rate limited for user %s", req.UserID)
		}
	}

	channelsSent := s.dispatchToChannels(ctx, req.Severity, req.Title, req.Body)

	if s.notifStore != nil {
		created, err := s.notifStore.Create(ctx, NotifCreateParams{
			TenantID:     req.TenantID,
			UserID:       req.UserID,
			EventType:    string(req.EventType),
			ScopeType:    string(req.ScopeType),
			ScopeRefID:   req.ScopeRefID,
			Title:        req.Title,
			Body:         req.Body,
			Severity:     req.Severity,
			ChannelsSent: channelsSent,
		})
		if err != nil {
			s.logger.Error().Err(err).Msg("persist notification to store")
		} else {
			now := time.Now()
			_ = s.notifStore.UpdateDelivery(ctx, created.ID, &now, nil, nil, 0, channelsSent)

			if s.eventPublisher != nil && s.notifSubject != "" {
				wsPayload := map[string]interface{}{
					"id":         created.ID.String(),
					"tenant_id":  created.TenantID.String(),
					"event_type": string(req.EventType),
					"title":      req.Title,
					"severity":   req.Severity,
					"created_at": created.CreatedAt.Format(time.RFC3339),
				}
				if pubErr := s.eventPublisher.Publish(ctx, s.notifSubject, wsPayload); pubErr != nil {
					s.logger.Warn().Err(pubErr).Msg("publish notification.new event")
				}
			}
		}
	}

	s.logger.Info().
		Str("event_type", string(req.EventType)).
		Str("severity", req.Severity).
		Strs("channels", channelsSent).
		Msg("notification dispatched")

	return nil
}

func (s *Service) handleHealthChanged(data []byte) {
	var payload HealthChangedPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		s.logger.Error().Err(err).Msg("unmarshal health changed event")
		return
	}

	if payload.CurrentStatus != "down" {
		if payload.PreviousStatus == "down" {
			s.dispatchRecovery(payload)
		}
		return
	}

	s.dispatchOperatorDown(payload)
}

func (s *Service) handleAlert(data []byte) {
	var payload AlertPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		s.logger.Error().Err(err).Msg("unmarshal alert event")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	channelsSent := s.dispatchToChannels(ctx, payload.Severity, payload.Title, payload.Description)

	s.logger.Info().
		Str("alert_type", payload.AlertType).
		Str("severity", payload.Severity).
		Strs("channels", channelsSent).
		Msg("alert dispatched")
}

func (s *Service) dispatchOperatorDown(payload HealthChangedPayload) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	title := fmt.Sprintf("CRITICAL: Operator %s is DOWN", payload.OperatorName)
	body := fmt.Sprintf(
		"Operator %s (ID: %s) is DOWN.\nCircuit breaker state: %s\nReason: %s\nTime: %s",
		payload.OperatorName, payload.OperatorID, payload.CircuitState,
		payload.FailureReason, payload.Timestamp.Format(time.RFC3339),
	)

	channelsSent := s.dispatchToChannels(ctx, "critical", title, body)

	s.logger.Warn().
		Str("operator_id", payload.OperatorID.String()).
		Str("operator_name", payload.OperatorName).
		Strs("channels", channelsSent).
		Msg("operator down notification dispatched")
}

func (s *Service) dispatchRecovery(payload HealthChangedPayload) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	title := fmt.Sprintf("RECOVERED: Operator %s is back", payload.OperatorName)
	body := fmt.Sprintf(
		"Operator %s (ID: %s) recovered.\nNew status: %s\nCircuit state: %s\nTime: %s",
		payload.OperatorName, payload.OperatorID, payload.CurrentStatus,
		payload.CircuitState, payload.Timestamp.Format(time.RFC3339),
	)

	channelsSent := s.dispatchToChannels(ctx, "info", title, body)

	s.logger.Info().
		Str("operator_id", payload.OperatorID.String()).
		Str("operator_name", payload.OperatorName).
		Strs("channels", channelsSent).
		Msg("operator recovery notification dispatched")
}

func (s *Service) dispatchToChannels(ctx context.Context, severity, title, body string) []string {
	var sent []string
	for _, ch := range s.channels {
		switch ch {
		case ChannelEmail:
			if s.email != nil {
				if err := s.email.SendAlert(ctx, title, body); err != nil {
					s.logger.Error().Err(err).Msg("send email notification")
					s.scheduleRetry(func() error {
						retryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						return s.email.SendAlert(retryCtx, title, body)
					})
				} else {
					sent = append(sent, string(ChannelEmail))
				}
			}
		case ChannelTelegram:
			if s.telegram != nil {
				msg := fmt.Sprintf("*%s*\n\n%s", title, body)
				if err := s.telegram.SendMessage(ctx, msg); err != nil {
					s.logger.Error().Err(err).Msg("send telegram notification")
					s.scheduleRetry(func() error {
						retryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						return s.telegram.SendMessage(retryCtx, msg)
					})
				} else {
					sent = append(sent, string(ChannelTelegram))
				}
			}
		case ChannelInApp:
			if s.inApp != nil {
				n := InAppNotification{
					ID:           uuid.New(),
					AlertType:    severity,
					Severity:     severity,
					Title:        title,
					Body:         body,
					ChannelsSent: sent,
					CreatedAt:    time.Now(),
				}
				if err := s.inApp.CreateNotification(ctx, n); err != nil {
					s.logger.Error().Err(err).Msg("create in-app notification")
				} else {
					sent = append(sent, string(ChannelInApp))
				}
			}
		case ChannelWebhook:
			if s.webhook != nil {
				payload, _ := json.Marshal(map[string]string{
					"title":    title,
					"body":     body,
					"severity": severity,
				})
				if err := s.webhook.SendWebhook(ctx, "", "", string(payload)); err != nil {
					s.logger.Error().Err(err).Msg("send webhook notification")
					s.scheduleRetry(func() error {
						retryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						return s.webhook.SendWebhook(retryCtx, "", "", string(payload))
					})
				} else {
					sent = append(sent, string(ChannelWebhook))
				}
			}
		case ChannelSMS:
			if s.sms != nil {
				if err := s.sms.SendSMS(ctx, "", fmt.Sprintf("%s: %s", title, body)); err != nil {
					s.logger.Error().Err(err).Msg("send sms notification")
				} else {
					sent = append(sent, string(ChannelSMS))
				}
			}
		}
	}
	return sent
}

func (s *Service) scheduleRetry(fn func() error) {
	if s.delivery != nil {
		s.delivery.ScheduleRetry(fn, func(success bool, err error, attempt int) {
			if success {
				s.logger.Info().Int("attempt", attempt).Msg("retry delivery succeeded")
			} else {
				s.logger.Error().Err(err).Int("attempt", attempt).Msg("retry delivery exhausted")
			}
		})
	}
}
