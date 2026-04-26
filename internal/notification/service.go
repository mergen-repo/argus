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

	"github.com/btopcu/argus/internal/alertstate"
	"github.com/btopcu/argus/internal/api/events"
	"github.com/btopcu/argus/internal/bus"
	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// SystemTenantID is a re-export of bus.SystemTenantID kept for backward
// compatibility of notification package users. The canonical constant lives
// in internal/bus (FIX-212 D5).
//
// TODO(D-078): retire once every in-scope publisher is on bus.Envelope
// (FIX-212 AC-2). Today the legacy-shape shim (`handleAlertPersist`) still
// falls back to `bus.SystemTenantID` for infra-global subjects like
// consumer_lag. Removing this re-export is blocked on closing D-078 — all
// raw-map publishers listed in FIX-212 plan §Out of Scope (job/cache/backup,
// etc.) must migrate first. Tracked in ROUTEMAP Tech Debt D-078.
var SystemTenantID = bus.SystemTenantID

// truncateRaw returns the prefix of s capped at n bytes with a `…(truncated)`
// suffix when it had to trim. Used for debug-log snippets that may contain
// PII (raw NATS payloads) — keeps log forensics useful while bounding PII
// surface area. Safe on non-UTF8-aligned cuts because zerolog serializes the
// result as a JSON string (invalid runes become U+FFFD in the sink).
func truncateRaw(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}

type Channel string

const (
	ChannelEmail    Channel = "email"
	ChannelTelegram Channel = "telegram"
	ChannelInApp    Channel = "in_app"
	ChannelWebhook  Channel = "webhook"
	ChannelSMS      Channel = "sms"
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

// AlertStoreWriter is the narrow contract notification.Service needs to persist
// inbound alert events before dispatching notifications. Kept narrow to avoid
// tight coupling to *store.AlertStore (mirrors NotifStore pattern).
// FIX-209: unified alerts table.
// FIX-210: swapped Create → UpsertWithDedup for dedup + cooldown. *store.AlertStore
// implements both methods, so main.go wiring is unchanged.
type AlertStoreWriter interface {
	UpsertWithDedup(ctx context.Context, p store.CreateAlertParams, severityOrdinal int) (*store.Alert, store.UpsertResult, error)
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
	metricsReg *obsmetrics.Registry

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

// SetMetricsRegistry wires the Prometheus registry used to emit FIX-210
// dedup / cooldown counters. Safe to leave unset (all increments become
// no-ops when the registry is nil — mirrors SetAlertStore semantics).
func (s *Service) SetMetricsRegistry(reg *obsmetrics.Registry) {
	s.metricsReg = reg
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

	// FIX-237 — Cross-tier safety invariant: classify event_type and short-circuit
	// before any preference lookup or persistence.
	//
	// Tier 1 (internal): NEVER persist a notification row, even if a misconfigured
	//   user has a preference for it. Pure metric/WS-stream events.
	// Tier 2 (digest): only the digest worker (Source="digest") may emit these
	//   directly; raw publishers are rejected (digest is the aggregation gate).
	// Tier 3 (operational): proceed to existing flow.
	tier := events.TierFor(string(req.EventType))
	switch tier {
	case events.TierInternal:
		s.logger.Debug().
			Str("event_type", string(req.EventType)).
			Msg("notification suppressed: tier=internal")
		if s.metricsReg != nil {
			s.metricsReg.IncEventsTierFiltered(string(req.EventType), "internal")
		}
		return nil
	case events.TierDigest:
		if req.Source != "digest" {
			s.logger.Warn().
				Str("event_type", string(req.EventType)).
				Str("source", req.Source).
				Msg("notification suppressed: tier=digest but source!=digest (raw publisher must route through digest worker)")
			if s.metricsReg != nil {
				s.metricsReg.IncEventsTierFiltered(string(req.EventType), "digest_no_source")
			}
			return nil
		}
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
		// AC-6 (FIX-237) audit: this is a valid direct insert. The tier guard
		// above ensures only Tier 3 events (and digest-sourced Tier 2 events)
		// reach this point, satisfying the spec's "OR kept as valid direct
		// insert for system-initiated notifications" exception. No event-driven
		// refactor needed for this call site.
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
				env := bus.NewEnvelope("notification.dispatch", created.TenantID.String(), req.Severity).
					WithSource("notification").
					WithTitle(title).
					WithMessage(bodyText).
					WithMeta("notification_id", created.ID.String()).
					WithMeta("event_type", string(req.EventType)).
					WithMeta("channels_sent", channelsSent).
					WithMeta("created_at", created.CreatedAt.Format(time.RFC3339))
				if pubErr := s.eventPublisher.Publish(ctx, s.notifSubject, env); pubErr != nil {
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
	// FIX-212: prefer envelope path; fall back to legacy shape for 1 release
	// grace window (D-078). Envelope meta carries previous_status / current_status;
	// entity.display_name carries operator name.
	var env bus.Envelope
	if err := json.Unmarshal(data, &env); err == nil && env.EventVersion == bus.CurrentEventVersion {
		payload := HealthChangedPayload{
			PreviousStatus: mapGetString(env.Meta, "previous_status"),
			CurrentStatus:  mapGetString(env.Meta, "current_status"),
			CircuitState:   mapGetString(env.Meta, "circuit_state"),
			FailureReason:  mapGetString(env.Meta, "failure_reason"),
			Timestamp:      env.Timestamp,
		}
		if env.Entity != nil {
			if id, perr := uuid.Parse(env.Entity.ID); perr == nil {
				payload.OperatorID = id
			}
			payload.OperatorName = env.Entity.DisplayName
		}
		if latRaw, ok := env.Meta["latency_ms"]; ok {
			switch v := latRaw.(type) {
			case float64:
				payload.LatencyMs = int(v)
			case int:
				payload.LatencyMs = v
			}
		}
		if payload.CurrentStatus != "down" {
			if payload.PreviousStatus == "down" {
				s.dispatchRecovery(payload)
			}
			return
		}
		s.dispatchOperatorDown(payload)
		return
	}

	// Legacy path.
	s.metricsReg.IncEventsLegacyShape("operator.health")
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

// parseAlertPayloadLegacy is the legacy tolerant parser used for pre-FIX-212
// envelope-less alert payloads. Kept for the 1-release grace window per
// FIX-212 D3 (D-078). When an inbound alert arrives with event_version != 1,
// handleAlertPersist routes through this shim and increments
// argus_events_legacy_shape_total{subject=alert.triggered}.
//
// DO NOT use as the primary path — strict alertParamsFromEnvelope is
// authoritative. Remove this function when the metric stays at 0 across a
// full release cycle (D-078).
func parseAlertPayloadLegacy(data []byte) (store.CreateAlertParams, error) {
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

	// FIX-212: legacy path — if tenant_id absent, fall back to SystemTenantID
	// sentinel. New publishers author tenant_id themselves (D5).
	tenantID, err := uuid.Parse(flex.TenantID)
	if err != nil {
		tenantID = SystemTenantID
	}

	meta := mergeMaps(flex.Metadata, flex.Details)
	metaBytes, _ := json.Marshal(meta)

	firedAt := firstTime(flex.Timestamp, flex.DetectedAt, timePtr(time.Now().UTC()))

	dedupKey := alertstate.DedupKey(tenantID, alertType, src, simID, operatorID, apnID)

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
		DedupKey:    &dedupKey,
		FiredAt:     *firedAt,
	}, nil
}

// alertParamsFromEnvelope is the FIX-212 primary path — converts a validated
// bus.Envelope into store.CreateAlertParams. Dedup key is computed here
// (FIX-210 PAT-006 single compute point) unless the publisher pre-authored
// one on the envelope (FIX-212 D4 optional override).
func alertParamsFromEnvelope(env *bus.Envelope) (store.CreateAlertParams, error) {
	tenantID, err := uuid.Parse(env.TenantID)
	if err != nil {
		return store.CreateAlertParams{}, fmt.Errorf("notification: envelope tenant_id invalid: %w", err)
	}

	alertType := env.Type
	if alertType == "" {
		alertType = "unknown"
	}

	src := env.Source
	if src == "" {
		src = resolveSource(alertType)
	}

	// Extract tri-entity IDs: prefer envelope.entity when its type matches
	// one of the alert scopes; also probe meta for operator_id/sim_id/apn_id
	// so cross-entity alerts (sim alert with operator context) resolve.
	var simID, operatorID, apnID *uuid.UUID
	if env.Entity != nil {
		switch env.Entity.Type {
		case "sim":
			if id, perr := uuid.Parse(env.Entity.ID); perr == nil && id != uuid.Nil {
				simID = &id
			}
		case "operator":
			if id, perr := uuid.Parse(env.Entity.ID); perr == nil && id != uuid.Nil {
				operatorID = &id
			}
		case "apn":
			if id, perr := uuid.Parse(env.Entity.ID); perr == nil && id != uuid.Nil {
				apnID = &id
			}
		}
	}
	if simID == nil {
		simID = firstUUID(mapGetString(env.Meta, "sim_id"))
	}
	if operatorID == nil {
		operatorID = firstUUID(mapGetString(env.Meta, "operator_id"))
	}
	if apnID == nil {
		apnID = firstUUID(mapGetString(env.Meta, "apn_id"))
	}

	metaBytes, _ := json.Marshal(env.Meta)

	firedAt := env.Timestamp
	if firedAt.IsZero() {
		firedAt = time.Now().UTC()
	}

	// FIX-212 D4: publisher may optionally pre-author dedup_key.
	var dkPtr *string
	if env.DedupKey != nil && *env.DedupKey != "" {
		dk := *env.DedupKey
		dkPtr = &dk
	} else {
		dk := alertstate.DedupKey(tenantID, alertType, src, simID, operatorID, apnID)
		dkPtr = &dk
	}

	return store.CreateAlertParams{
		TenantID:    tenantID,
		Type:        alertType,
		Severity:    env.Severity,
		Source:      src,
		Title:       env.Title,
		Description: env.Message,
		Meta:        metaBytes,
		SimID:       simID,
		OperatorID:  operatorID,
		APNID:       apnID,
		DedupKey:    dkPtr,
		FiredAt:     firedAt,
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

// validationReason maps an envelope Validate error onto the canonical
// argus_events_invalid_total{reason} label (FIX-212 Task 6 metric).
func validationReason(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, bus.ErrLegacyShape):
		return "legacy_shape"
	case errors.Is(err, bus.ErrInvalidSeverity):
		return "invalid_severity"
	case errors.Is(err, bus.ErrInvalidTenant):
		return "invalid_tenant"
	case errors.Is(err, bus.ErrMissingField):
		return "missing_field"
	case errors.Is(err, bus.ErrInvalidEntity):
		return "invalid_entity"
	case errors.Is(err, bus.ErrDedupKeyTooLong):
		return "dedup_key_too_long"
	default:
		return "unknown"
	}
}

// handleAlertPersist is the NATS alert-event subscriber (FIX-209 / FIX-210).
// Ordering: persist BEFORE dispatch — a persisted alert that fails to
// dispatch is recoverable via retry; a dispatched alert that fails to
// persist is lost from the UI. Availability > durability for dispatch,
// so persist errors DO NOT block the dispatch path.
//
// FIX-210: the persist step routes through UpsertWithDedup which either
// inserts a fresh row, increments occurrence_count on the existing active
// alert, or drops the event when a matching dedup_key is still in cooldown.
// Metrics are emitted per-outcome so operators can see dedup/cooldown
// effectiveness without sampling.
func (s *Service) handleAlertPersist(data []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. FIX-212: parse into canonical bus.Envelope first; fall back to the
	//    legacy tolerant parser when the payload predates the migration
	//    (event_version missing or != 1). Legacy hits increment the per-subject
	//    metric so D-078 (shim removal) has an observability signal.
	var env bus.Envelope
	unmarshalErr := json.Unmarshal(data, &env)
	var params store.CreateAlertParams
	var parseErr error
	useLegacy := unmarshalErr != nil || env.EventVersion != bus.CurrentEventVersion
	if !useLegacy {
		if verr := env.Validate(); verr != nil {
			if errors.Is(verr, bus.ErrLegacyShape) {
				useLegacy = true
			} else {
				s.metricsReg.IncEventsInvalid("alert.triggered", validationReason(verr))
				s.logger.Warn().
					Err(verr).
					Str("subject", "alert.triggered").
					Str("raw", truncateRaw(string(data), 512)).
					Msg("alert: envelope validation failed; dropping")
				return
			}
		}
	}
	if useLegacy {
		s.metricsReg.IncEventsLegacyShape("alert.triggered")
		s.logger.Debug().
			Str("subject", "alert.triggered").
			Msg("alert: legacy-shape payload routed through FIX-212 shim (D-078)")
		params, parseErr = parseAlertPayloadLegacy(data)
	} else {
		params, parseErr = alertParamsFromEnvelope(&env)
	}

	if parseErr != nil {
		s.logger.Warn().
			Err(parseErr).
			Str("raw", truncateRaw(string(data), 512)).
			Msg("alert: parse into unified schema failed; dispatch will continue with legacy AlertPayload")
	} else if s.alertStore != nil {
		// 2. Persist (ordered before dispatch). Log-and-continue on error.
		//    FIX-210: UpsertWithDedup replaces Create. Outcome drives metrics.
		ordinal := severity.Ordinal(params.Severity)
		_, result, perr := s.alertStore.UpsertWithDedup(ctx, params, ordinal)
		if perr != nil {
			s.logger.Error().
				Err(perr).
				Str("type", params.Type).
				Str("tenant_id", params.TenantID.String()).
				Msg("persist alert failed")
		} else {
			switch result {
			case store.UpsertInserted:
				// Normal path — fresh row created. No extra metric.
			case store.UpsertDeduplicated:
				s.metricsReg.IncAlertsDeduplicated(params.Type, params.Source)
				dk := ""
				if params.DedupKey != nil {
					dk = *params.DedupKey
				}
				s.logger.Debug().
					Str("type", params.Type).
					Str("source", params.Source).
					Str("dedup_key", dk).
					Msg("alert deduplicated (occurrence_count incremented)")
			case store.UpsertCoolingDown:
				s.metricsReg.IncAlertsCooldownDropped(params.Type, params.Source)
				dk := ""
				if params.DedupKey != nil {
					k := *params.DedupKey
					if len(k) > 8 {
						dk = k[:8]
					} else {
						dk = k
					}
				}
				// FIX-210 Gate F-A4 — demoted from Warn to Debug; cooldown drops are
				// expected (not anomalous) under flapping publishers, and the metric
				// argus_alerts_cooldown_dropped_total is the primary ops signal.
				// dedup_key truncated to 8 chars to reduce log volume under burst.
				s.logger.Debug().
					Str("type", params.Type).
					Str("source", params.Source).
					Str("dedup_key_prefix", dk).
					Msg("alert dropped: dedup_key still in cooldown window")
			}
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
