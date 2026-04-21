package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"strings"
	"sync"
	texttemplate "text/template"
	"time"

	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Channel string

const (
	ChannelEmail    Channel = "email"
	ChannelTelegram Channel = "telegram"
	ChannelInApp    Channel = "in_app"
	ChannelWebhook  Channel = "webhook"
	ChannelSMS      Channel = "sms"
)

// systemTenantID is the sentinel tenant UUID used by parseAlertPayload when
// an inbound alert event arrives with no tenant_id (5 of 7 publishers today —
// see FIX-209 Gate F-A1 + AC-3 + PAT-006). FIX-212 will normalize publishers
// to carry tenant_id; until then infra/operator-without-context alerts are
// written under this row so dashboards are not silently blind.
// Matches migrations/seed/001_admin_user.sql demo tenant.
var systemTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

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

// AlertStoreWriter is the narrow contract notification.Service needs to persist
// inbound alert events before dispatching notifications. Kept narrow to avoid
// tight coupling to *store.AlertStore (mirrors NotifStore pattern).
// FIX-209: unified alerts table.
type AlertStoreWriter interface {
	Create(ctx context.Context, p store.CreateAlertParams) (*store.Alert, error)
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

// PreferenceStore is the read interface for notification_preferences.
type PreferenceStore interface {
	Get(ctx context.Context, tenantID uuid.UUID, eventType string) (*Preference, error)
}

// Preference mirrors store.NotificationPreference for loose coupling.
type Preference struct {
	Channels          []string
	SeverityThreshold string
	Enabled           bool
}

// TemplateStore is the read interface for notification_templates.
type TemplateStore interface {
	Get(ctx context.Context, eventType, locale string) (*Template, error)
}

// Template mirrors store.NotificationTemplate for loose coupling.
type Template struct {
	Subject  string
	BodyText string
	BodyHTML string
}

// TemplatePayload is the sanitized data struct passed to Go templates.
// Only whitelisted fields are exposed — no raw event data.
type TemplatePayload struct {
	TenantName  string
	UserName    string
	EventTime   string
	EntityID    string
	ExtraFields map[string]string
}

// ErrTemplateNotFound is returned by TemplateStore.Get when no template exists.
var ErrTemplateNotFound = errors.New("notification: template not found")

// severityOrdinal maps severity string to a comparable integer (AC-8 filtering).
// FIX-211: delegates to the canonical 5-level taxonomy (info=1 .. critical=5).
func severityOrdinal(s string) int {
	return severity.Ordinal(s)
}

type Config struct {
	Channels      []Channel
	HealthSubject string
	AlertSubject  string
}

// killSwitchChecker allows the service to check if external_notifications is disabled.
type killSwitchChecker interface {
	IsEnabled(key string) bool
}

type Service struct {
	email      EmailSender
	telegram   TelegramSender
	inApp      InAppStore
	webhook    WebhookDispatcher
	sms        SMSDispatcher
	channels   []Channel
	killSwitch killSwitchChecker
	logger     zerolog.Logger

	webhookURL    string
	webhookSecret string

	notifStore     NotifStore
	eventPublisher EventPublisher
	delivery       *DeliveryTracker
	notifSubject   string

	prefStore     PreferenceStore
	templateStore TemplateStore

	alertStore AlertStoreWriter

	mu   sync.Mutex
	subs []Subscription
}

func NewService(email EmailSender, telegram TelegramSender, inApp InAppStore, channels []Channel, logger zerolog.Logger) *Service {
	svc := &Service{
		email:    email,
		telegram: telegram,
		inApp:    inApp,
		channels: channels,
		logger:   logger.With().Str("component", "notification").Logger(),
	}
	svc.validateChannels()
	return svc
}

// SetKillSwitch attaches an optional kill-switch service.
func (s *Service) SetKillSwitch(ks killSwitchChecker) {
	s.killSwitch = ks
}

func (s *Service) senderFor(ch Channel) interface{} {
	switch ch {
	case ChannelEmail:
		return s.email
	case ChannelTelegram:
		return s.telegram
	case ChannelInApp:
		return s.inApp
	case ChannelWebhook:
		return s.webhook
	case ChannelSMS:
		return s.sms
	}
	return nil
}

func (s *Service) validateChannels() {
	for _, ch := range s.channels {
		if s.senderFor(ch) == nil {
			s.logger.Warn().Str("channel", string(ch)).Msg("channel configured but sender is nil; dispatches will skip")
		}
	}
}

func (s *Service) SetWebhook(w WebhookDispatcher) {
	s.webhook = w
}

func (s *Service) SetWebhookConfig(webhookURL, webhookSecret string) {
	s.webhookURL = webhookURL
	s.webhookSecret = webhookSecret
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

func (s *Service) SetPrefStore(ps PreferenceStore) {
	s.prefStore = ps
}

func (s *Service) SetTemplateStore(ts TemplateStore) {
	s.templateStore = ts
}

// SetAlertStore wires a writer for the unified alerts table (FIX-209).
// Optional: when nil, alert events are dispatched but NOT persisted — this
// keeps pre-FIX-209 tests and offline tooling working without the dependency.
func (s *Service) SetAlertStore(as AlertStoreWriter) {
	s.alertStore = as
}

func (s *Service) Start(subscriber Subscriber, healthSubject, alertSubject string) error {
	sub1, err := subscriber.QueueSubscribe(healthSubject, "notification-svc", func(subject string, data []byte) {
		s.handleHealthChanged(data)
	})
	if err != nil {
		return fmt.Errorf("notification: subscribe health: %w", err)
	}

	sub2, err := subscriber.QueueSubscribe(alertSubject, "notification-svc", func(subject string, data []byte) {
		s.handleAlertPersist(data)
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
	// Kill-switch: external_notifications — suppress all outbound dispatches.
	if s.killSwitch != nil && s.killSwitch.IsEnabled("external_notifications") {
		s.logger.Warn().
			Str("event_type", string(req.EventType)).
			Msg("notification suppressed: kill_switch external_notifications active")
		return nil
	}

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

	// Preference-aware dispatch: look up (tenant, event_type) preference row.
	// Falls back to legacy s.channels when prefStore is nil or row is absent.
	activeChannels := s.channels
	if s.prefStore != nil {
		pref, err := s.prefStore.Get(ctx, req.TenantID, string(req.EventType))
		if err != nil {
			s.logger.Warn().Err(err).Str("event_type", string(req.EventType)).Msg("preference lookup failed, using defaults")
		} else if pref != nil {
			if !pref.Enabled {
				s.logger.Debug().
					Str("event_type", string(req.EventType)).
					Msg("notification suppressed by preference (enabled=false)")
				return nil
			}
			if severityOrdinal(req.Severity) < severityOrdinal(pref.SeverityThreshold) {
				s.logger.Debug().
					Str("event_type", string(req.EventType)).
					Str("severity", req.Severity).
					Str("threshold", pref.SeverityThreshold).
					Msg("notification suppressed by severity threshold")
				return nil
			}
			prefChannels := make([]Channel, 0, len(pref.Channels))
			for _, ch := range pref.Channels {
				prefChannels = append(prefChannels, Channel(ch))
			}
			activeChannels = prefChannels
		}
	}

	title, bodyText := s.renderContent(ctx, req)

	channelsSent := s.dispatchToActiveChannels(ctx, activeChannels, req.Severity, title, bodyText)

	if s.notifStore != nil {
		created, err := s.notifStore.Create(ctx, NotifCreateParams{
			TenantID:     req.TenantID,
			UserID:       req.UserID,
			EventType:    string(req.EventType),
			ScopeType:    string(req.ScopeType),
			ScopeRefID:   req.ScopeRefID,
			Title:        title,
			Body:         bodyText,
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
					"title":      title,
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

// renderContent looks up a template for the notification event, renders it with
// a sanitized TemplatePayload, and returns subject+body. Falls back to a simple
// fmt.Sprintf default when no template is configured or found.
func (s *Service) renderContent(ctx context.Context, req NotifyRequest) (subject, bodyText string) {
	subject = req.Title
	bodyText = req.Body

	if s.templateStore == nil {
		return
	}

	locale := "en"
	if req.Locale != "" {
		locale = req.Locale
	}

	tmpl, err := s.templateStore.Get(ctx, string(req.EventType), locale)
	if err != nil {
		if !errors.Is(err, ErrTemplateNotFound) {
			s.logger.Warn().Err(err).Str("event_type", string(req.EventType)).Msg("template lookup failed, using default")
		}
		subject = fmt.Sprintf("Argus: %s event", string(req.EventType))
		bodyText = req.Body
		return
	}

	payload := TemplatePayload{
		TenantName:  req.TenantName,
		UserName:    req.UserName,
		EventTime:   time.Now().Format(time.RFC3339),
		ExtraFields: req.ExtraFields,
	}
	if req.ScopeRefID != nil {
		payload.EntityID = req.ScopeRefID.String()
	}

	if parsed, err := texttemplate.New("subject").Parse(tmpl.Subject); err == nil {
		var buf bytes.Buffer
		if execErr := parsed.Execute(&buf, payload); execErr == nil {
			subject = buf.String()
		}
	}

	if parsed, err := texttemplate.New("body_text").Parse(tmpl.BodyText); err == nil {
		var buf bytes.Buffer
		if execErr := parsed.Execute(&buf, payload); execErr == nil {
			bodyText = buf.String()
		}
	}

	return
}

// renderHTMLBody renders body_html via html/template for safe HTML output.
func renderHTMLBody(rawHTML string, payload TemplatePayload) string {
	parsed, err := htmltemplate.New("body_html").Parse(rawHTML)
	if err != nil {
		return rawHTML
	}
	var buf bytes.Buffer
	if err := parsed.Execute(&buf, payload); err != nil {
		return rawHTML
	}
	return buf.String()
}

// dispatchToActiveChannels dispatches to the provided channel list without mutating s.channels.
// This keeps the preference-resolved path thread-safe alongside the legacy path.
func (s *Service) dispatchToActiveChannels(ctx context.Context, channels []Channel, severity, title, body string) []string {
	var sent []string
	for _, ch := range channels {
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
				if err := ValidateWebhookConfig(s.webhookURL, s.webhookSecret); err != nil {
					s.logger.Error().
						Err(err).
						Str("channel", string(ChannelWebhook)).
						Msg("webhook config invalid, skipping dispatch")
					continue
				}
				p, _ := json.Marshal(map[string]string{
					"title":    title,
					"body":     body,
					"severity": severity,
				})
				webhookURL := s.webhookURL
				webhookSecret := s.webhookSecret
				if err := s.webhook.SendWebhook(ctx, webhookURL, webhookSecret, string(p)); err != nil {
					s.logger.Error().Err(err).Msg("send webhook notification")
					s.scheduleRetry(func() error {
						retryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						return s.webhook.SendWebhook(retryCtx, webhookURL, webhookSecret, string(p))
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

// publisherSourceMap is the canonical alert-type → source taxonomy for FIX-209.
// Used by resolveSource() to normalize source strings from heterogeneous
// publishers into the 4 canonical alert source enums (operator / sim / infra /
// policy / system). storage.* prefix handled before the map lookup.
var publisherSourceMap = map[string]string{
	// operator-scoped
	"operator_down":                 "operator",
	"operator_recovered":            "operator",
	"sla_violation":                 "operator",
	"roaming.agreement.renewal_due": "operator",
	// SIM anomaly prefixes (emitted by anomaly engine + batch detector)
	"anomaly_sim_cloning": "sim",
	"anomaly_data_spike":  "sim",
	"anomaly_auth_flood":  "sim",
	"anomaly_nas_flood":   "sim",
	"anomaly_velocity":    "sim",
	"anomaly_location":    "sim",
	// infrastructure
	"nats_consumer_lag":   "infra",
	"anomaly_batch_crash": "infra",
	// policy
	"policy_violation": "policy",
	// storage.* handled via prefix match, fallback "system" for unknown types
}

// alertEventFlexible tolerates the 4+ different payload shapes emitted by the
// publishers of argus.events.alert.triggered (FIX-209 plan §Publisher inventory).
// FIX-212 will normalize these shapes; until then we accept what comes and
// synthesize what is missing.
type alertEventFlexible struct {
	AlertID     string                 `json:"alert_id,omitempty"`
	AlertType   string                 `json:"alert_type,omitempty"`
	Type        string                 `json:"type,omitempty"`
	TenantID    string                 `json:"tenant_id,omitempty"`
	Severity    string                 `json:"severity,omitempty"`
	Title       string                 `json:"title,omitempty"`
	Message     string                 `json:"message,omitempty"`
	Description string                 `json:"description,omitempty"`
	Source      string                 `json:"source,omitempty"`
	EntityType  string                 `json:"entity_type,omitempty"`
	EntityID    string                 `json:"entity_id,omitempty"`
	SimID       string                 `json:"sim_id,omitempty"`
	OperatorID  string                 `json:"operator_id,omitempty"`
	APNID       string                 `json:"apn_id,omitempty"`
	Consumer    string                 `json:"consumer,omitempty"`
	Pending     int64                  `json:"pending,omitempty"`
	JobID       string                 `json:"job_id,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Timestamp   *time.Time             `json:"timestamp,omitempty"`
	DetectedAt  *time.Time             `json:"detected_at,omitempty"`
}

// parseAlertPayload normalizes an inbound alert event into store.CreateAlertParams.
// Tolerant of the publisher heterogeneity documented in FIX-209 §Publisher inventory.
func parseAlertPayload(data []byte) (store.CreateAlertParams, error) {
	var flex alertEventFlexible
	if err := json.Unmarshal(data, &flex); err != nil {
		return store.CreateAlertParams{}, err
	}

	alertType := firstNonEmpty(flex.AlertType, flex.Type, flex.Source, "unknown")

	sev := flex.Severity
	if sev == "" || severity.Validate(sev) != nil {
		sev = severity.Info
	}

	title := firstNonEmpty(flex.Title, flex.Message)
	if title == "" {
		title = synthesizeTitle(alertType, flex.Consumer, flex.Pending, flex.JobID, flex.Error, flex.Description)
	}

	description := flex.Description
	src := resolveSource(alertType)

	simID := firstUUID(
		flex.SimID,
		mapGetString(flex.Metadata, "sim_id"),
		mapGetString(flex.Details, "sim_id"),
		ifEntityType(flex.EntityType, "sim", flex.EntityID),
	)
	operatorID := firstUUID(
		flex.OperatorID,
		mapGetString(flex.Metadata, "operator_id"),
		mapGetString(flex.Details, "operator_id"),
		ifEntityType(flex.EntityType, "operator", flex.EntityID),
	)
	apnID := firstUUID(
		flex.APNID,
		mapGetString(flex.Metadata, "apn_id"),
		mapGetString(flex.Details, "apn_id"),
	)

	// FIX-209 Gate (F-A1): publishers for operator.AlertEvent, nats_consumer_lag,
	// anomaly_batch_crash, storage_monitor (explicit nil), and roaming_renewal
	// do NOT currently emit tenant_id — AC-3 + PAT-006 forbid rewriting
	// publisher payloads in this story. Full envelope unification lands in
	// FIX-212. Until then, fall back to the system-tenant sentinel so
	// operator/infra alerts still persist instead of being silently dropped.
	// Tracked as ROUTEMAP Tech Debt D-075 (system-tenant sentinel → FIX-212).
	tenantID, err := uuid.Parse(flex.TenantID)
	if err != nil {
		tenantID = systemTenantID
	}

	meta := mergeMaps(flex.Metadata, flex.Details)
	metaBytes, _ := json.Marshal(meta)

	firedAt := firstTime(flex.Timestamp, flex.DetectedAt, timePtr(time.Now().UTC()))

	return store.CreateAlertParams{
		TenantID:    tenantID,
		Type:        alertType,
		Severity:    sev,
		Source:      src,
		Title:       title,
		Description: description,
		Meta:        metaBytes,
		SimID:       simID,
		OperatorID:  operatorID,
		APNID:       apnID,
		DedupKey:    nil, // FIX-210 populates
		FiredAt:     *firedAt,
	}, nil
}

func synthesizeTitle(alertType, consumer string, pending int64, jobID, errMsg, description string) string {
	switch {
	case alertType == "nats_consumer_lag" && consumer != "":
		return fmt.Sprintf("NATS consumer lag: %s has %d pending", consumer, pending)
	case alertType == "anomaly_batch_crash":
		if jobID != "" {
			return fmt.Sprintf("Anomaly batch crashed: job %s", jobID)
		}
		return "Anomaly batch crashed"
	case description != "":
		if len(description) > 80 {
			return description[:80] + "..."
		}
		return description
	default:
		return fmt.Sprintf("Alert: %s", alertType)
	}
}

func resolveSource(alertType string) string {
	if strings.HasPrefix(alertType, "storage.") {
		return "infra"
	}
	if s, ok := publisherSourceMap[alertType]; ok {
		return s
	}
	return "system"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstUUID(vals ...string) *uuid.UUID {
	for _, v := range vals {
		if v == "" {
			continue
		}
		id, err := uuid.Parse(v)
		if err != nil {
			continue
		}
		if id == uuid.Nil {
			continue
		}
		return &id
	}
	return nil
}

func firstTime(vals ...*time.Time) *time.Time {
	for _, v := range vals {
		if v != nil && !v.IsZero() {
			return v
		}
	}
	now := time.Now().UTC()
	return &now
}

func mapGetString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func mergeMaps(primary, fallback map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(primary)+len(fallback))
	for k, v := range fallback {
		out[k] = v
	}
	for k, v := range primary {
		out[k] = v
	}
	return out
}

func ifEntityType(entityType, want, entityID string) string {
	if entityType == want {
		return entityID
	}
	return ""
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// handleAlertPersist is the NATS alert-event subscriber (FIX-209).
// Ordering: persist BEFORE dispatch — a persisted alert that fails to
// dispatch is recoverable via retry; a dispatched alert that fails to
// persist is lost from the UI. Availability > durability for dispatch,
// so persist errors DO NOT block the dispatch path.
func (s *Service) handleAlertPersist(data []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Parse into canonical CreateAlertParams (tolerant of publisher heterogeneity).
	params, parseErr := parseAlertPayload(data)
	if parseErr != nil {
		s.logger.Warn().
			Err(parseErr).
			Str("raw", string(data)).
			Msg("alert: parse into unified schema failed; dispatch will continue with legacy AlertPayload")
	} else if s.alertStore != nil {
		// 2. Persist (ordered before dispatch). Log-and-continue on error.
		if _, perr := s.alertStore.Create(ctx, params); perr != nil {
			s.logger.Error().
				Err(perr).
				Str("type", params.Type).
				Str("tenant_id", params.TenantID.String()).
				Msg("persist alert failed")
		}
	}

	// 3. Dispatch — preserve legacy behavior (never regress notification coverage).
	var payload AlertPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		s.logger.Error().Err(err).Msg("unmarshal alert event (dispatch path)")
		return
	}

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
				if err := ValidateWebhookConfig(s.webhookURL, s.webhookSecret); err != nil {
					s.logger.Error().
						Err(err).
						Str("channel", string(ChannelWebhook)).
						Msg("webhook config invalid, skipping dispatch")
					continue
				}
				payload, _ := json.Marshal(map[string]string{
					"title":    title,
					"body":     body,
					"severity": severity,
				})
				webhookURL := s.webhookURL
				webhookSecret := s.webhookSecret
				if err := s.webhook.SendWebhook(ctx, webhookURL, webhookSecret, string(payload)); err != nil {
					s.logger.Error().Err(err).Msg("send webhook notification")
					s.scheduleRetry(func() error {
						retryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						return s.webhook.SendWebhook(retryCtx, webhookURL, webhookSecret, string(payload))
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
