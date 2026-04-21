package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type mockEmailSender struct {
	mu      sync.Mutex
	calls   []emailCall
	failErr error
}

type emailCall struct {
	subject string
	body    string
}

func (m *mockEmailSender) SendAlert(_ context.Context, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return m.failErr
	}
	m.calls = append(m.calls, emailCall{subject, body})
	return nil
}

type mockTelegramSender struct {
	mu      sync.Mutex
	calls   []string
	failErr error
}

func (m *mockTelegramSender) SendMessage(_ context.Context, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return m.failErr
	}
	m.calls = append(m.calls, message)
	return nil
}

type mockInAppStore struct {
	mu    sync.Mutex
	items []InAppNotification
}

func (m *mockInAppStore) CreateNotification(_ context.Context, n InAppNotification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = append(m.items, n)
	return nil
}

type mockSubscription struct {
	unsubscribed bool
}

func (m *mockSubscription) Unsubscribe() error {
	m.unsubscribed = true
	return nil
}

type mockSubscriber struct {
	handlers map[string]func(string, []byte)
}

func newMockSubscriber() *mockSubscriber {
	return &mockSubscriber{handlers: make(map[string]func(string, []byte))}
}

func (m *mockSubscriber) QueueSubscribe(subject, queue string, handler func(string, []byte)) (Subscription, error) {
	m.handlers[subject] = handler
	return &mockSubscription{}, nil
}

func (m *mockSubscriber) Publish(subject string, data []byte) {
	if h, ok := m.handlers[subject]; ok {
		h(subject, data)
	}
}

type mockWebhookSender struct {
	mu    sync.Mutex
	calls []webhookCall
}

type webhookCall struct {
	url     string
	secret  string
	payload string
}

func (m *mockWebhookSender) SendWebhook(_ context.Context, url, secret, payload string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, webhookCall{url, secret, payload})
	return nil
}

type mockSMSSender struct {
	mu    sync.Mutex
	calls []smsCall
}

type smsCall struct {
	phone   string
	message string
}

func (m *mockSMSSender) SendSMS(_ context.Context, phone, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, smsCall{phone, message})
	return nil
}

type mockNotifStore struct {
	mu    sync.Mutex
	items []NotifCreateParams
}

func (m *mockNotifStore) Create(_ context.Context, p NotifCreateParams) (*NotifRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = append(m.items, p)
	return &NotifRow{
		ID:        uuid.New(),
		TenantID:  p.TenantID,
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockNotifStore) UpdateDelivery(_ context.Context, _ uuid.UUID, _, _, _ *time.Time, _ int, _ []string) error {
	return nil
}

type mockEventPublisher struct {
	mu     sync.Mutex
	events []publishedEvent
}

type publishedEvent struct {
	subject string
	payload interface{}
}

func (m *mockEventPublisher) Publish(_ context.Context, subject string, payload interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, publishedEvent{subject, payload})
	return nil
}

type mockRateLimiter struct {
	allowed bool
}

func (m *mockRateLimiter) Allow(_ context.Context, _ string, _ int, _ time.Duration) (bool, error) {
	return m.allowed, nil
}

func TestService_OperatorDown_DispatchesToAllChannels(t *testing.T) {
	email := &mockEmailSender{}
	telegram := &mockTelegramSender{}
	inApp := &mockInAppStore{}

	svc := NewService(email, telegram, inApp, []Channel{ChannelEmail, ChannelTelegram, ChannelInApp}, zerolog.Nop())

	sub := newMockSubscriber()
	if err := svc.Start(sub, "argus.events.operator.health", "argus.events.alert.triggered"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	payload := HealthChangedPayload{
		OperatorID:     uuid.New(),
		OperatorName:   "turkcell",
		PreviousStatus: "healthy",
		CurrentStatus:  "down",
		CircuitState:   "open",
		FailureReason:  "timeout",
		Timestamp:      time.Now(),
	}

	data, _ := json.Marshal(payload)
	sub.Publish("argus.events.operator.health", data)

	time.Sleep(50 * time.Millisecond)

	email.mu.Lock()
	if len(email.calls) != 1 {
		t.Errorf("email calls = %d, want 1", len(email.calls))
	} else if email.calls[0].subject == "" {
		t.Error("email subject is empty")
	}
	email.mu.Unlock()

	telegram.mu.Lock()
	if len(telegram.calls) != 1 {
		t.Errorf("telegram calls = %d, want 1", len(telegram.calls))
	}
	telegram.mu.Unlock()

	inApp.mu.Lock()
	if len(inApp.items) != 1 {
		t.Errorf("in-app items = %d, want 1", len(inApp.items))
	}
	inApp.mu.Unlock()
}

func TestService_OperatorRecovery_DispatchesNotification(t *testing.T) {
	email := &mockEmailSender{}

	svc := NewService(email, nil, nil, []Channel{ChannelEmail}, zerolog.Nop())

	sub := newMockSubscriber()
	if err := svc.Start(sub, "argus.events.operator.health", "argus.events.alert.triggered"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	payload := HealthChangedPayload{
		OperatorID:     uuid.New(),
		OperatorName:   "vodafone",
		PreviousStatus: "down",
		CurrentStatus:  "healthy",
		CircuitState:   "closed",
		Timestamp:      time.Now(),
	}

	data, _ := json.Marshal(payload)
	sub.Publish("argus.events.operator.health", data)

	time.Sleep(50 * time.Millisecond)

	email.mu.Lock()
	if len(email.calls) != 1 {
		t.Errorf("email calls = %d, want 1", len(email.calls))
	} else {
		if email.calls[0].subject == "" {
			t.Error("recovery email subject is empty")
		}
	}
	email.mu.Unlock()
}

func TestService_HealthyToHealthy_NoDispatch(t *testing.T) {
	email := &mockEmailSender{}

	svc := NewService(email, nil, nil, []Channel{ChannelEmail}, zerolog.Nop())

	sub := newMockSubscriber()
	if err := svc.Start(sub, "argus.events.operator.health", "argus.events.alert.triggered"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	payload := HealthChangedPayload{
		OperatorID:     uuid.New(),
		OperatorName:   "turkcell",
		PreviousStatus: "healthy",
		CurrentStatus:  "degraded",
		CircuitState:   "closed",
		Timestamp:      time.Now(),
	}

	data, _ := json.Marshal(payload)
	sub.Publish("argus.events.operator.health", data)

	time.Sleep(50 * time.Millisecond)

	email.mu.Lock()
	if len(email.calls) != 0 {
		t.Errorf("email calls = %d, want 0 (no dispatch for healthy->degraded)", len(email.calls))
	}
	email.mu.Unlock()
}

func TestService_AlertEvent_Dispatches(t *testing.T) {
	email := &mockEmailSender{}
	telegram := &mockTelegramSender{}

	svc := NewService(email, telegram, nil, []Channel{ChannelEmail, ChannelTelegram}, zerolog.Nop())

	sub := newMockSubscriber()
	if err := svc.Start(sub, "argus.events.operator.health", "argus.events.alert.triggered"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	alert := AlertPayload{
		AlertID:     "alert-sla-001",
		AlertType:   "sla_violation",
		Severity:    "medium",
		Title:       "SLA violation for turkcell",
		Description: "Uptime 98.5% below target 99.9%",
		EntityType:  "operator",
		EntityID:    uuid.New(),
		Timestamp:   time.Now(),
	}

	data, _ := json.Marshal(alert)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(50 * time.Millisecond)

	email.mu.Lock()
	if len(email.calls) != 1 {
		t.Errorf("email calls = %d, want 1", len(email.calls))
	}
	email.mu.Unlock()

	telegram.mu.Lock()
	if len(telegram.calls) != 1 {
		t.Errorf("telegram calls = %d, want 1", len(telegram.calls))
	}
	telegram.mu.Unlock()
}

func TestService_Stop_UnsubscribesAll(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, zerolog.Nop())

	sub := newMockSubscriber()
	if err := svc.Start(sub, "health", "alert"); err != nil {
		t.Fatalf("start: %v", err)
	}

	svc.Stop()

	svc.mu.Lock()
	if len(svc.subs) != 0 {
		t.Errorf("subs after stop = %d, want 0", len(svc.subs))
	}
	svc.mu.Unlock()
}

func TestService_NilChannels_NoDispatch(t *testing.T) {
	svc := NewService(nil, nil, nil, []Channel{ChannelEmail, ChannelTelegram, ChannelInApp}, zerolog.Nop())

	sub := newMockSubscriber()
	if err := svc.Start(sub, "health", "alert"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	payload := HealthChangedPayload{
		OperatorID:     uuid.New(),
		OperatorName:   "test",
		PreviousStatus: "healthy",
		CurrentStatus:  "down",
		CircuitState:   "open",
		Timestamp:      time.Now(),
	}

	data, _ := json.Marshal(payload)
	sub.Publish("health", data)

	time.Sleep(50 * time.Millisecond)
}

func TestService_Webhook_Dispatches(t *testing.T) {
	webhook := &mockWebhookSender{}

	svc := NewService(nil, nil, nil, []Channel{ChannelWebhook}, zerolog.Nop())
	svc.SetWebhook(webhook)
	svc.SetWebhookConfig("https://example.com/hook", "test-secret")

	sub := newMockSubscriber()
	if err := svc.Start(sub, "health", "alert"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	payload := HealthChangedPayload{
		OperatorID:     uuid.New(),
		OperatorName:   "turkcell",
		PreviousStatus: "healthy",
		CurrentStatus:  "down",
		CircuitState:   "open",
		Timestamp:      time.Now(),
	}

	data, _ := json.Marshal(payload)
	sub.Publish("health", data)

	time.Sleep(50 * time.Millisecond)

	webhook.mu.Lock()
	if len(webhook.calls) != 1 {
		t.Errorf("webhook calls = %d, want 1", len(webhook.calls))
	} else {
		call := webhook.calls[0]
		if call.url != "https://example.com/hook" {
			t.Errorf("webhook url = %q, want https://example.com/hook", call.url)
		}
		if call.secret != "test-secret" {
			t.Errorf("webhook secret = %q, want test-secret", call.secret)
		}
	}
	webhook.mu.Unlock()
}

func TestWebhookValidation_EmptyURLRejectedNoDispatch(t *testing.T) {
	webhook := &mockWebhookSender{}

	svc := NewService(nil, nil, nil, []Channel{ChannelWebhook}, zerolog.Nop())
	svc.SetWebhook(webhook)

	sub := newMockSubscriber()
	if err := svc.Start(sub, "health", "alert"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	payload := HealthChangedPayload{
		OperatorID:     uuid.New(),
		OperatorName:   "turkcell",
		PreviousStatus: "healthy",
		CurrentStatus:  "down",
		CircuitState:   "open",
		Timestamp:      time.Now(),
	}

	data, _ := json.Marshal(payload)
	sub.Publish("health", data)

	time.Sleep(50 * time.Millisecond)

	webhook.mu.Lock()
	if len(webhook.calls) != 0 {
		t.Errorf("webhook calls = %d, want 0 — empty config must not attempt partial delivery", len(webhook.calls))
	}
	webhook.mu.Unlock()
}

func TestWebhookValidation_HTTPURLRejectedNoDispatch(t *testing.T) {
	webhook := &mockWebhookSender{}

	svc := NewService(nil, nil, nil, []Channel{ChannelWebhook}, zerolog.Nop())
	svc.SetWebhook(webhook)
	svc.SetWebhookConfig("http://insecure.example.com/hook", "some-secret")

	sub := newMockSubscriber()
	if err := svc.Start(sub, "health", "alert"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	payload := HealthChangedPayload{
		OperatorID:     uuid.New(),
		OperatorName:   "vodafone",
		PreviousStatus: "healthy",
		CurrentStatus:  "down",
		CircuitState:   "open",
		Timestamp:      time.Now(),
	}

	data, _ := json.Marshal(payload)
	sub.Publish("health", data)

	time.Sleep(50 * time.Millisecond)

	webhook.mu.Lock()
	if len(webhook.calls) != 0 {
		t.Errorf("webhook calls = %d, want 0 — http scheme must not attempt dispatch", len(webhook.calls))
	}
	webhook.mu.Unlock()
}

func TestWebhookValidation_EmptySecretRejectedNoDispatch(t *testing.T) {
	webhook := &mockWebhookSender{}

	svc := NewService(nil, nil, nil, []Channel{ChannelWebhook}, zerolog.Nop())
	svc.SetWebhook(webhook)
	svc.SetWebhookConfig("https://example.com/hook", "")

	sub := newMockSubscriber()
	if err := svc.Start(sub, "health", "alert"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	payload := HealthChangedPayload{
		OperatorID:     uuid.New(),
		OperatorName:   "turkcell",
		PreviousStatus: "healthy",
		CurrentStatus:  "down",
		CircuitState:   "open",
		Timestamp:      time.Now(),
	}

	data, _ := json.Marshal(payload)
	sub.Publish("health", data)

	time.Sleep(50 * time.Millisecond)

	webhook.mu.Lock()
	if len(webhook.calls) != 0 {
		t.Errorf("webhook calls = %d, want 0 — empty secret must not attempt dispatch", len(webhook.calls))
	}
	webhook.mu.Unlock()
}

func TestService_SMS_Dispatches(t *testing.T) {
	sms := &mockSMSSender{}

	svc := NewService(nil, nil, nil, []Channel{ChannelSMS}, zerolog.Nop())
	svc.SetSMS(sms)

	sub := newMockSubscriber()
	if err := svc.Start(sub, "health", "alert"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	payload := HealthChangedPayload{
		OperatorID:     uuid.New(),
		OperatorName:   "vodafone",
		PreviousStatus: "healthy",
		CurrentStatus:  "down",
		CircuitState:   "open",
		Timestamp:      time.Now(),
	}

	data, _ := json.Marshal(payload)
	sub.Publish("health", data)

	time.Sleep(50 * time.Millisecond)

	sms.mu.Lock()
	if len(sms.calls) != 1 {
		t.Errorf("sms calls = %d, want 1", len(sms.calls))
	}
	sms.mu.Unlock()
}

func TestService_Notify_PersistsToStore(t *testing.T) {
	notifStore := &mockNotifStore{}
	publisher := &mockEventPublisher{}

	svc := NewService(nil, nil, nil, []Channel{}, zerolog.Nop())
	svc.SetNotifStore(notifStore)
	svc.SetEventPublisher(publisher, "argus.events.notification.dispatch")

	ctx := context.Background()
	tenantID := uuid.New()
	userID := uuid.New()

	err := svc.Notify(ctx, NotifyRequest{
		TenantID:  tenantID,
		UserID:    &userID,
		EventType: EventOperatorDown,
		ScopeType: ScopeOperator,
		Title:     "Test notification",
		Body:      "Test body",
		Severity:  "critical",
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}

	notifStore.mu.Lock()
	if len(notifStore.items) != 1 {
		t.Errorf("store items = %d, want 1", len(notifStore.items))
	} else {
		item := notifStore.items[0]
		if item.TenantID != tenantID {
			t.Errorf("tenant_id = %s, want %s", item.TenantID, tenantID)
		}
		if item.EventType != string(EventOperatorDown) {
			t.Errorf("event_type = %s, want %s", item.EventType, EventOperatorDown)
		}
	}
	notifStore.mu.Unlock()

	publisher.mu.Lock()
	if len(publisher.events) != 1 {
		t.Errorf("published events = %d, want 1", len(publisher.events))
	}
	publisher.mu.Unlock()
}

func TestService_Notify_RateLimited(t *testing.T) {
	svc := NewService(nil, nil, nil, []Channel{}, zerolog.Nop())

	limiter := &mockRateLimiter{allowed: false}
	dt := NewDeliveryTracker(limiter, zerolog.Nop())
	defer dt.Stop()
	svc.SetDeliveryTracker(dt)

	ctx := context.Background()
	userID := uuid.New()

	err := svc.Notify(ctx, NotifyRequest{
		TenantID:  uuid.New(),
		UserID:    &userID,
		EventType: EventOperatorDown,
		ScopeType: ScopeSystem,
		Title:     "Should be rate limited",
		Body:      "test",
		Severity:  "info",
	})

	if err == nil {
		t.Error("expected rate limit error, got nil")
	}
}

func TestService_Notify_RateLimitAllowed(t *testing.T) {
	notifStore := &mockNotifStore{}
	svc := NewService(nil, nil, nil, []Channel{}, zerolog.Nop())
	svc.SetNotifStore(notifStore)

	limiter := &mockRateLimiter{allowed: true}
	dt := NewDeliveryTracker(limiter, zerolog.Nop())
	defer dt.Stop()
	svc.SetDeliveryTracker(dt)

	ctx := context.Background()
	userID := uuid.New()

	err := svc.Notify(ctx, NotifyRequest{
		TenantID:  uuid.New(),
		UserID:    &userID,
		EventType: EventAlertNew,
		ScopeType: ScopeSystem,
		Title:     "Allowed notification",
		Body:      "test",
		Severity:  "info",
	})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	notifStore.mu.Lock()
	if len(notifStore.items) != 1 {
		t.Errorf("store items = %d, want 1", len(notifStore.items))
	}
	notifStore.mu.Unlock()
}

func TestService_ValidateChannels_WarnsNilSenders(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	_ = NewService(nil, nil, nil, []Channel{ChannelWebhook, ChannelSMS, ChannelEmail}, logger)

	logged := buf.String()

	if !strings.Contains(logged, "channel configured but sender is nil") {
		t.Errorf("expected warn log for nil sender, got: %q", logged)
	}

	if strings.Contains(logged, string(ChannelInApp)) {
		t.Errorf("unexpected warn for in_app channel which was not configured")
	}
}

// -- Preference & template dispatch integration tests (AC-7, AC-8) --

type mockPrefStore struct {
	pref *Preference
	err  error
}

func (m *mockPrefStore) Get(_ context.Context, _ uuid.UUID, _ string) (*Preference, error) {
	return m.pref, m.err
}

type mockTemplateStore struct {
	tmpl *Template
	err  error
}

func (m *mockTemplateStore) Get(_ context.Context, _, _ string) (*Template, error) {
	return m.tmpl, m.err
}

func TestService_Notify_TurkishTemplate_RenderedSubject(t *testing.T) {
	email := &mockEmailSender{}
	svc := NewService(email, nil, nil, []Channel{ChannelEmail}, zerolog.Nop())

	svc.SetTemplateStore(&mockTemplateStore{
		tmpl: &Template{
			Subject:  "Merhaba {{.UserName}}, hoş geldiniz!",
			BodyText: "Güzel bir gün dileriz.",
		},
	})

	ctx := context.Background()
	err := svc.Notify(ctx, NotifyRequest{
		TenantID:  uuid.New(),
		EventType: "welcome",
		ScopeType: ScopeSystem,
		Title:     "fallback",
		Body:      "fallback body",
		Severity:  "info",
		Locale:    "tr",
		UserName:  "Ahmet",
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}

	email.mu.Lock()
	defer email.mu.Unlock()
	if len(email.calls) != 1 {
		t.Fatalf("email calls = %d, want 1", len(email.calls))
	}
	if email.calls[0].subject != "Merhaba Ahmet, hoş geldiniz!" {
		t.Errorf("subject = %q, want Turkish rendered subject", email.calls[0].subject)
	}
}

func TestService_Notify_PreferenceChannelFilter_SkipsWebhook(t *testing.T) {
	email := &mockEmailSender{}
	webhook := &mockWebhookSender{}

	svc := NewService(email, nil, nil, []Channel{ChannelEmail, ChannelWebhook}, zerolog.Nop())
	svc.SetWebhook(webhook)
	svc.SetWebhookConfig("https://example.com/hook", "secret")

	// Preference: only email channel
	svc.SetPrefStore(&mockPrefStore{
		pref: &Preference{
			Channels:          []string{"email"},
			SeverityThreshold: "info",
			Enabled:           true,
		},
	})

	ctx := context.Background()
	err := svc.Notify(ctx, NotifyRequest{
		TenantID:  uuid.New(),
		EventType: "anomaly.detected",
		ScopeType: ScopeSystem,
		Title:     "Anomaly",
		Body:      "Anomaly detected",
		Severity:  "medium",
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}

	email.mu.Lock()
	if len(email.calls) != 1 {
		t.Errorf("email calls = %d, want 1", len(email.calls))
	}
	email.mu.Unlock()

	webhook.mu.Lock()
	if len(webhook.calls) != 0 {
		t.Errorf("webhook calls = %d, want 0 (not in preference channels)", len(webhook.calls))
	}
	webhook.mu.Unlock()
}

func TestService_Notify_PreferenceDisabled_NoSend(t *testing.T) {
	email := &mockEmailSender{}
	svc := NewService(email, nil, nil, []Channel{ChannelEmail}, zerolog.Nop())

	svc.SetPrefStore(&mockPrefStore{
		pref: &Preference{
			Channels:          []string{"email"},
			SeverityThreshold: "info",
			Enabled:           false,
		},
	})

	ctx := context.Background()
	err := svc.Notify(ctx, NotifyRequest{
		TenantID:  uuid.New(),
		EventType: "anomaly.detected",
		ScopeType: ScopeSystem,
		Title:     "Should not send",
		Body:      "disabled",
		Severity:  "critical",
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}

	email.mu.Lock()
	if len(email.calls) != 0 {
		t.Errorf("email calls = %d, want 0 (preference disabled)", len(email.calls))
	}
	email.mu.Unlock()
}

func TestService_Notify_SeverityThreshold_Skip(t *testing.T) {
	email := &mockEmailSender{}
	svc := NewService(email, nil, nil, []Channel{ChannelEmail}, zerolog.Nop())

	// Threshold=medium, event severity=info → skip
	svc.SetPrefStore(&mockPrefStore{
		pref: &Preference{
			Channels:          []string{"email"},
			SeverityThreshold: "medium",
			Enabled:           true,
		},
	})

	ctx := context.Background()
	err := svc.Notify(ctx, NotifyRequest{
		TenantID:  uuid.New(),
		EventType: "anomaly.detected",
		ScopeType: ScopeSystem,
		Title:     "Info event",
		Body:      "below threshold",
		Severity:  "info",
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}

	email.mu.Lock()
	if len(email.calls) != 0 {
		t.Errorf("email calls = %d, want 0 (severity below threshold)", len(email.calls))
	}
	email.mu.Unlock()
}

func TestService_Notify_SeverityThreshold_Allow(t *testing.T) {
	email := &mockEmailSender{}
	svc := NewService(email, nil, nil, []Channel{ChannelEmail}, zerolog.Nop())

	// Threshold=medium, event severity=high → allow
	svc.SetPrefStore(&mockPrefStore{
		pref: &Preference{
			Channels:          []string{"email"},
			SeverityThreshold: "medium",
			Enabled:           true,
		},
	})

	ctx := context.Background()
	err := svc.Notify(ctx, NotifyRequest{
		TenantID:  uuid.New(),
		EventType: "anomaly.detected",
		ScopeType: ScopeSystem,
		Title:     "High event",
		Body:      "above threshold",
		Severity:  "high",
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}

	email.mu.Lock()
	if len(email.calls) != 1 {
		t.Errorf("email calls = %d, want 1 (severity above threshold)", len(email.calls))
	}
	email.mu.Unlock()
}

// TestNotifySeverityThreshold_5Level verifies the canonical 5-level severity
// ordinal comparison (FIX-211). Event severity must be >= threshold to dispatch.
// Ordinal: info=1 < low=2 < medium=3 < high=4 < critical=5.
func TestNotifySeverityThreshold_5Level(t *testing.T) {
	cases := []struct {
		name      string
		threshold string
		eventSev  string
		wantCalls int
	}{
		{"high_threshold_suppresses_medium", "high", "medium", 0},
		{"medium_threshold_allows_high", "medium", "high", 1},
		{"info_threshold_allows_info", "info", "info", 1},
		{"critical_threshold_allows_critical", "critical", "critical", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			email := &mockEmailSender{}
			svc := NewService(email, nil, nil, []Channel{ChannelEmail}, zerolog.Nop())

			svc.SetPrefStore(&mockPrefStore{
				pref: &Preference{
					Channels:          []string{"email"},
					SeverityThreshold: tc.threshold,
					Enabled:           true,
				},
			})

			err := svc.Notify(context.Background(), NotifyRequest{
				TenantID:  uuid.New(),
				EventType: "anomaly.detected",
				ScopeType: ScopeSystem,
				Title:     "5-level severity test",
				Body:      "canonical taxonomy",
				Severity:  tc.eventSev,
			})
			if err != nil {
				t.Fatalf("notify: %v", err)
			}

			email.mu.Lock()
			defer email.mu.Unlock()
			if len(email.calls) != tc.wantCalls {
				t.Errorf("email calls = %d, want %d (threshold=%s, event=%s)",
					len(email.calls), tc.wantCalls, tc.threshold, tc.eventSev)
			}
		})
	}
}

func TestService_Notify_NoPrefStore_UsesLegacyChannels(t *testing.T) {
	email := &mockEmailSender{}
	svc := NewService(email, nil, nil, []Channel{ChannelEmail}, zerolog.Nop())

	ctx := context.Background()
	err := svc.Notify(ctx, NotifyRequest{
		TenantID:  uuid.New(),
		EventType: EventAlertNew,
		ScopeType: ScopeSystem,
		Title:     "Alert",
		Body:      "test",
		Severity:  "info",
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}

	email.mu.Lock()
	if len(email.calls) != 1 {
		t.Errorf("email calls = %d, want 1 (legacy channels)", len(email.calls))
	}
	email.mu.Unlock()
}

// -- FIX-209: handleAlertPersist — unified alerts persist + dispatch tests --
// -- FIX-210: extended with dedup + cooldown outcome programming --

// mockAlertStore is a fake AlertStoreWriter for FIX-209 / FIX-210 tests.
// FIX-210: implements UpsertWithDedup; outcomes is a FIFO queue of
// UpsertResult values the mock returns per call (defaults to
// UpsertInserted when the queue is empty, matching legacy behaviour).
type mockAlertStore struct {
	mu       sync.Mutex
	calls    []store.CreateAlertParams
	outcomes []store.UpsertResult
	failErr  error
}

func (m *mockAlertStore) UpsertWithDedup(_ context.Context, p store.CreateAlertParams, _ int) (*store.Alert, store.UpsertResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return nil, store.UpsertInserted, m.failErr
	}
	m.calls = append(m.calls, p)

	result := store.UpsertInserted
	if len(m.outcomes) > 0 {
		result = m.outcomes[0]
		m.outcomes = m.outcomes[1:]
	}

	// UpsertCoolingDown does not return a row (mirrors the real store).
	if result == store.UpsertCoolingDown {
		return nil, result, nil
	}
	return &store.Alert{
		ID:       uuid.New(),
		TenantID: p.TenantID,
		Type:     p.Type,
		Severity: p.Severity,
		Source:   p.Source,
		Title:    p.Title,
		FiredAt:  p.FiredAt,
	}, result, nil
}

func newPersistSvc(t *testing.T, email *mockEmailSender, alertStore *mockAlertStore) (*Service, *mockSubscriber) {
	t.Helper()
	svc := NewService(email, nil, nil, []Channel{ChannelEmail}, zerolog.Nop())
	if alertStore != nil {
		svc.SetAlertStore(alertStore)
	}
	sub := newMockSubscriber()
	if err := svc.Start(sub, "argus.events.operator.health", "argus.events.alert.triggered"); err != nil {
		t.Fatalf("start: %v", err)
	}
	return svc, sub
}

// newPersistSvcWithMetrics is newPersistSvc + a wired metrics registry.
// Used by FIX-210 tests that need to assert dedup / cooldown counter
// increments end-to-end through handleAlertPersist.
func newPersistSvcWithMetrics(t *testing.T, email *mockEmailSender, alertStore *mockAlertStore) (*Service, *mockSubscriber, *obsmetrics.Registry) {
	t.Helper()
	svc := NewService(email, nil, nil, []Channel{ChannelEmail}, zerolog.Nop())
	if alertStore != nil {
		svc.SetAlertStore(alertStore)
	}
	reg := obsmetrics.NewRegistry()
	svc.SetMetricsRegistry(reg)
	sub := newMockSubscriber()
	if err := svc.Start(sub, "argus.events.operator.health", "argus.events.alert.triggered"); err != nil {
		t.Fatalf("start: %v", err)
	}
	return svc, sub, reg
}

// scrapeNotifMetrics fetches the /metrics body from the supplied registry.
// Mirrors the helper in internal/operator/health_test.go.
func scrapeNotifMetrics(t *testing.T, reg *obsmetrics.Registry) string {
	t.Helper()
	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

// TestHandleAlertPersist_FullAlertEvent_PersistsAllFields — shape A (operator.AlertEvent JSON).
func TestHandleAlertPersist_FullAlertEvent_PersistsAllFields(t *testing.T) {
	email := &mockEmailSender{}
	alertStore := &mockAlertStore{}
	svc, sub := newPersistSvc(t, email, alertStore)
	defer svc.Stop()

	tenantID := uuid.New()
	opID := uuid.New()
	payload := map[string]interface{}{
		"alert_id":    "alert-op-001",
		"alert_type":  "operator_down",
		"tenant_id":   tenantID.String(),
		"severity":    "critical",
		"title":       "Operator turkcell is DOWN",
		"description": "Circuit breaker opened",
		"entity_type": "operator",
		"entity_id":   opID.String(),
		"metadata":    map[string]interface{}{"operator_name": "turkcell"},
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(payload)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(50 * time.Millisecond)

	alertStore.mu.Lock()
	defer alertStore.mu.Unlock()
	if len(alertStore.calls) != 1 {
		t.Fatalf("alert persist calls = %d, want 1", len(alertStore.calls))
	}
	p := alertStore.calls[0]
	if p.TenantID != tenantID {
		t.Errorf("tenant_id = %s, want %s", p.TenantID, tenantID)
	}
	if p.Type != "operator_down" {
		t.Errorf("type = %q, want operator_down", p.Type)
	}
	if p.Severity != "critical" {
		t.Errorf("severity = %q, want critical", p.Severity)
	}
	if p.Source != "operator" {
		t.Errorf("source = %q, want operator", p.Source)
	}
	if p.OperatorID == nil || *p.OperatorID != opID {
		t.Errorf("operator_id = %v, want %s (derived from entity_type=operator)", p.OperatorID, opID)
	}
}

// TestHandleAlertPersist_AnomalyMapPayload_LinksAnomalyID — shape C (anomaly engine map).
func TestHandleAlertPersist_AnomalyMapPayload_LinksAnomalyID(t *testing.T) {
	email := &mockEmailSender{}
	alertStore := &mockAlertStore{}
	svc, sub := newPersistSvc(t, email, alertStore)
	defer svc.Stop()

	tenantID := uuid.New()
	simID := uuid.New()
	anomalyID := uuid.New()
	payload := map[string]interface{}{
		"alert_id":    anomalyID.String(),
		"tenant_id":   tenantID.String(),
		"alert_type":  "anomaly_data_spike",
		"severity":    "high",
		"title":       "Data Usage Spike — 89012345",
		"description": "SIM data spike",
		"entity_type": "anomaly",
		"entity_id":   anomalyID.String(),
		"sim_id":      simID.String(),
		"metadata": map[string]interface{}{
			"anomaly_id":  anomalyID.String(),
			"sim_id":      simID.String(),
			"today_bytes": float64(1000000),
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(payload)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(50 * time.Millisecond)

	alertStore.mu.Lock()
	defer alertStore.mu.Unlock()
	if len(alertStore.calls) != 1 {
		t.Fatalf("alert persist calls = %d, want 1", len(alertStore.calls))
	}
	p := alertStore.calls[0]
	if p.SimID == nil || *p.SimID != simID {
		t.Errorf("sim_id = %v, want %s", p.SimID, simID)
	}
	if p.Source != "sim" {
		t.Errorf("source = %q, want sim", p.Source)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(p.Meta, &meta); err != nil {
		t.Fatalf("meta unmarshal: %v", err)
	}
	if meta["anomaly_id"] != anomalyID.String() {
		t.Errorf("meta.anomaly_id = %v, want %s", meta["anomaly_id"], anomalyID)
	}
	if meta["sim_id"] != simID.String() {
		t.Errorf("meta.sim_id = %v, want %s", meta["sim_id"], simID)
	}
}

// TestHandleAlertPersist_LagAlert_SynthesizesTitle — shape D (consumer_lag).
// NOTE: real publisher omits tenant_id; FIX-212 will add. Test injects tenant_id
// to exercise the shape-D title synthesis path.
func TestHandleAlertPersist_LagAlert_SynthesizesTitle(t *testing.T) {
	email := &mockEmailSender{}
	alertStore := &mockAlertStore{}
	svc, sub := newPersistSvc(t, email, alertStore)
	defer svc.Stop()

	tenantID := uuid.New()
	payload := map[string]interface{}{
		"tenant_id": tenantID.String(),
		"severity":  "medium",
		"source":    "nats_consumer_lag",
		"consumer":  "cdr-worker",
		"pending":   520,
	}
	data, _ := json.Marshal(payload)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(50 * time.Millisecond)

	alertStore.mu.Lock()
	defer alertStore.mu.Unlock()
	if len(alertStore.calls) != 1 {
		t.Fatalf("alert persist calls = %d, want 1", len(alertStore.calls))
	}
	p := alertStore.calls[0]
	if p.Type != "nats_consumer_lag" {
		t.Errorf("type = %q, want nats_consumer_lag (resolved from source field)", p.Type)
	}
	if p.Source != "infra" {
		t.Errorf("source = %q, want infra", p.Source)
	}
	wantTitle := "NATS consumer lag: cdr-worker has 520 pending"
	if p.Title != wantTitle {
		t.Errorf("title = %q, want %q", p.Title, wantTitle)
	}
}

// TestHandleAlertPersist_StorageMonitor_MapsToInfra — shape B (storage monitor).
// NOTE: real publisher emits tenant_id: nil; FIX-212 will add a real tenant.
// Test injects tenant_id to exercise the storage.* prefix mapping.
func TestHandleAlertPersist_StorageMonitor_MapsToInfra(t *testing.T) {
	email := &mockEmailSender{}
	alertStore := &mockAlertStore{}
	svc, sub := newPersistSvc(t, email, alertStore)
	defer svc.Stop()

	tenantID := uuid.New()
	payload := map[string]interface{}{
		"alert_type":  "storage.hypertable_growth",
		"tenant_id":   tenantID.String(),
		"severity":    "high",
		"title":       "Storage Alert: hypertable_growth",
		"description": "CDR hypertable grew by 80% in 24h",
		"entity_type": "system",
	}
	data, _ := json.Marshal(payload)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(50 * time.Millisecond)

	alertStore.mu.Lock()
	defer alertStore.mu.Unlock()
	if len(alertStore.calls) != 1 {
		t.Fatalf("alert persist calls = %d, want 1", len(alertStore.calls))
	}
	p := alertStore.calls[0]
	if p.Source != "infra" {
		t.Errorf("source = %q, want infra (storage.* prefix)", p.Source)
	}
	if p.Type != "storage.hypertable_growth" {
		t.Errorf("type = %q, want storage.hypertable_growth", p.Type)
	}
}

// TestHandleAlertPersist_PolicyViolation_MessageBecomesTitle — shape C (enforcer).
func TestHandleAlertPersist_PolicyViolation_MessageBecomesTitle(t *testing.T) {
	email := &mockEmailSender{}
	alertStore := &mockAlertStore{}
	svc, sub := newPersistSvc(t, email, alertStore)
	defer svc.Stop()

	tenantID := uuid.New()
	simID := uuid.New()
	payload := map[string]interface{}{
		"id":          uuid.New().String(),
		"tenant_id":   tenantID.String(),
		"type":        "policy_violation",
		"severity":    "high",
		"state":       "open",
		"message":     "Policy violation: usage_cap on SIM 8901234567890123",
		"sim_id":      simID.String(),
		"entity_type": "sim",
		"entity_id":   simID.String(),
		"detected_at": time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(payload)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(50 * time.Millisecond)

	alertStore.mu.Lock()
	defer alertStore.mu.Unlock()
	if len(alertStore.calls) != 1 {
		t.Fatalf("alert persist calls = %d, want 1", len(alertStore.calls))
	}
	p := alertStore.calls[0]
	if p.Type != "policy_violation" {
		t.Errorf("type = %q, want policy_violation (resolved from 'type' field)", p.Type)
	}
	if p.Source != "policy" {
		t.Errorf("source = %q, want policy", p.Source)
	}
	if p.Title != "Policy violation: usage_cap on SIM 8901234567890123" {
		t.Errorf("title = %q, want message-derived", p.Title)
	}
	if p.SimID == nil || *p.SimID != simID {
		t.Errorf("sim_id = %v, want %s", p.SimID, simID)
	}
}

// TestHandleAlertPersist_AnomalyBatchCrash_NoTitleSynthesizes — shape D (supervisor).
// NOTE: real publisher omits tenant_id; test injects to exercise synthesis path.
func TestHandleAlertPersist_AnomalyBatchCrash_NoTitleSynthesizes(t *testing.T) {
	email := &mockEmailSender{}
	alertStore := &mockAlertStore{}
	svc, sub := newPersistSvc(t, email, alertStore)
	defer svc.Stop()

	tenantID := uuid.New()
	jobID := uuid.New()
	payload := map[string]interface{}{
		"tenant_id": tenantID.String(),
		"severity":  "high",
		"source":    "anomaly_batch_crash",
		"job_id":    jobID.String(),
		"error":     "context deadline exceeded",
	}
	data, _ := json.Marshal(payload)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(50 * time.Millisecond)

	alertStore.mu.Lock()
	defer alertStore.mu.Unlock()
	if len(alertStore.calls) != 1 {
		t.Fatalf("alert persist calls = %d, want 1", len(alertStore.calls))
	}
	p := alertStore.calls[0]
	if p.Source != "infra" {
		t.Errorf("source = %q, want infra", p.Source)
	}
	wantTitle := "Anomaly batch crashed: job " + jobID.String()
	if p.Title != wantTitle {
		t.Errorf("title = %q, want %q", p.Title, wantTitle)
	}
}

// TestHandleAlertPersist_RoamingRenewal_FullEnvelope — shape A (AlertPayload JSON).
// NOTE: roaming_renewal publisher emits notification.AlertPayload which has no
// TenantID field; test injects to exercise the source='operator' path.
func TestHandleAlertPersist_RoamingRenewal_FullEnvelope(t *testing.T) {
	email := &mockEmailSender{}
	alertStore := &mockAlertStore{}
	svc, sub := newPersistSvc(t, email, alertStore)
	defer svc.Stop()

	tenantID := uuid.New()
	agreementID := uuid.New()
	payload := map[string]interface{}{
		"alert_id":    "roaming-renewal-xyz",
		"alert_type":  "roaming.agreement.renewal_due",
		"tenant_id":   tenantID.String(),
		"severity":    "medium",
		"title":       "Roaming agreement expiring in 30 days",
		"description": "Agreement with VodafoneDE expires on 2026-05-21",
		"entity_type": "roaming_agreement",
		"entity_id":   agreementID.String(),
		"metadata": map[string]interface{}{
			"partner_operator_name": "VodafoneDE",
			"days_to_expiry":        float64(30),
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(payload)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(50 * time.Millisecond)

	alertStore.mu.Lock()
	defer alertStore.mu.Unlock()
	if len(alertStore.calls) != 1 {
		t.Fatalf("alert persist calls = %d, want 1", len(alertStore.calls))
	}
	p := alertStore.calls[0]
	if p.Source != "operator" {
		t.Errorf("source = %q, want operator", p.Source)
	}
	if p.Type != "roaming.agreement.renewal_due" {
		t.Errorf("type = %q, want roaming.agreement.renewal_due", p.Type)
	}
}

// TestHandleAlertPersist_InvalidSeverity_CoercesToInfo — severity drift must not drop the event.
func TestHandleAlertPersist_InvalidSeverity_CoercesToInfo(t *testing.T) {
	email := &mockEmailSender{}
	alertStore := &mockAlertStore{}
	svc, sub := newPersistSvc(t, email, alertStore)
	defer svc.Stop()

	tenantID := uuid.New()
	payload := map[string]interface{}{
		"alert_type": "sla_violation",
		"tenant_id":  tenantID.String(),
		"severity":   "warning", // not a canonical severity
		"title":      "stale publisher emitted 'warning'",
	}
	data, _ := json.Marshal(payload)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(50 * time.Millisecond)

	alertStore.mu.Lock()
	defer alertStore.mu.Unlock()
	if len(alertStore.calls) != 1 {
		t.Fatalf("alert persist calls = %d, want 1 (event must NOT be dropped on invalid severity)", len(alertStore.calls))
	}
	if alertStore.calls[0].Severity != "info" {
		t.Errorf("severity = %q, want info (coerced)", alertStore.calls[0].Severity)
	}
}

// TestHandleAlertPersist_PersistFails_DispatchStillRuns — persist error must
// not block the dispatch path (availability > durability for notifications).
func TestHandleAlertPersist_PersistFails_DispatchStillRuns(t *testing.T) {
	email := &mockEmailSender{}
	alertStore := &mockAlertStore{failErr: errors.New("db is down")}
	svc, sub := newPersistSvc(t, email, alertStore)
	defer svc.Stop()

	tenantID := uuid.New()
	payload := AlertPayload{
		AlertID:     "x",
		AlertType:   "sla_violation",
		Severity:    "high",
		Title:       "SLA violation",
		Description: "uptime below target",
		EntityType:  "operator",
		EntityID:    uuid.New(),
		Metadata:    map[string]interface{}{"tenant_id": tenantID.String()}, // inject tenant_id so parse succeeds
		Timestamp:   time.Now(),
	}
	// Build raw JSON with tenant_id at top-level so parse succeeds AND persist hits the failErr.
	raw := map[string]interface{}{
		"alert_id":    payload.AlertID,
		"alert_type":  payload.AlertType,
		"tenant_id":   tenantID.String(),
		"severity":    payload.Severity,
		"title":       payload.Title,
		"description": payload.Description,
		"entity_type": payload.EntityType,
		"entity_id":   payload.EntityID.String(),
		"timestamp":   payload.Timestamp.Format(time.RFC3339),
	}
	data, _ := json.Marshal(raw)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(50 * time.Millisecond)

	// Persist was attempted (and returned an error via failErr).
	alertStore.mu.Lock()
	if len(alertStore.calls) != 0 {
		t.Errorf("alert persist calls = %d, want 0 (failErr set — no row appended)", len(alertStore.calls))
	}
	alertStore.mu.Unlock()

	// Dispatch must still have happened (email sent).
	email.mu.Lock()
	defer email.mu.Unlock()
	if len(email.calls) != 1 {
		t.Errorf("email calls = %d, want 1 (dispatch must not be blocked by persist failure)", len(email.calls))
	}
}

// TestHandleAlertPersist_DispatchFails_PersistStillCommitted — persist happens
// BEFORE dispatch, so even if dispatch errors the persist call is committed.
func TestHandleAlertPersist_DispatchFails_PersistStillCommitted(t *testing.T) {
	email := &mockEmailSender{failErr: errors.New("smtp unreachable")}
	alertStore := &mockAlertStore{}
	svc, sub := newPersistSvc(t, email, alertStore)
	defer svc.Stop()

	tenantID := uuid.New()
	raw := map[string]interface{}{
		"alert_id":    "x",
		"alert_type":  "sla_violation",
		"tenant_id":   tenantID.String(),
		"severity":    "high",
		"title":       "SLA violation",
		"description": "uptime below target",
		"entity_type": "operator",
		"entity_id":   uuid.New().String(),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(raw)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(50 * time.Millisecond)

	// Persist must be committed despite dispatch error.
	alertStore.mu.Lock()
	defer alertStore.mu.Unlock()
	if len(alertStore.calls) != 1 {
		t.Errorf("alert persist calls = %d, want 1 (persist runs BEFORE dispatch)", len(alertStore.calls))
	}
}

// TestHandleAlertPersist_NilAlertStore_DispatchStillRuns — FIX-209 Gate F-A10.
// When alertStore is never wired (SetAlertStore unused), handleAlertPersist must
// still dispatch without panicking.
func TestHandleAlertPersist_NilAlertStore_DispatchStillRuns(t *testing.T) {
	email := &mockEmailSender{}
	// nil alertStore explicitly — do NOT call SetAlertStore.
	svc, sub := newPersistSvc(t, email, nil)
	defer svc.Stop()

	tenantID := uuid.New()
	raw := map[string]interface{}{
		"alert_id":    "x",
		"alert_type":  "operator_down",
		"tenant_id":   tenantID.String(),
		"severity":    "critical",
		"title":       "Operator is DOWN",
		"description": "cb opened",
		"entity_type": "operator",
		"entity_id":   uuid.New().String(),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(raw)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(50 * time.Millisecond)

	// Dispatch must have happened despite no alertStore wired.
	email.mu.Lock()
	defer email.mu.Unlock()
	if len(email.calls) != 1 {
		t.Errorf("email calls = %d, want 1 (nil alertStore must not block dispatch)", len(email.calls))
	}
}

// TestParseAlertPayload_MissingTenantID_UsesSentinel — FIX-209 Gate F-A1.
// Five of seven publishers (operator.AlertEvent, nats_consumer_lag,
// anomaly_batch_crash, storage_monitor explicit-nil, roaming_renewal
// notification.AlertPayload) do NOT emit tenant_id — plan AC-3 + PAT-006
// forbid rewriting publishers in this story. parseAlertPayload must fall
// back to the system-tenant sentinel so these alerts still persist.
func TestParseAlertPayload_MissingTenantID_UsesSentinel(t *testing.T) {
	// Shape D — consumer_lag (no tenant_id, no title, no description).
	raw := map[string]interface{}{
		"severity": "medium",
		"source":   "nats_consumer_lag",
		"consumer": "argus-anomaly-events",
		"pending":  float64(150),
	}
	data, _ := json.Marshal(raw)

	p, err := parseAlertPayload(data)
	if err != nil {
		t.Fatalf("parseAlertPayload returned err with missing tenant_id (should fall back): %v", err)
	}
	if p.TenantID != systemTenantID {
		t.Errorf("tenant_id = %s, want system sentinel %s", p.TenantID, systemTenantID)
	}
	if p.Source != "infra" {
		t.Errorf("source = %q, want infra (resolved via publisherSourceMap)", p.Source)
	}
	if p.Title == "" {
		t.Errorf("title must be synthesized for payloads without explicit title")
	}
}

// TestParseAlertPayload_DedupKeyAlwaysPopulated — FIX-210 Task 4.
// Every parsed event must carry a 64-char hex dedup_key so
// handleAlertPersist always exercises the upsert path. This is the
// runtime invariant documented in plan D2.
func TestParseAlertPayload_DedupKeyAlwaysPopulated(t *testing.T) {
	// Shape A — operator.AlertEvent with an explicit tenant_id.
	tenantID := uuid.New()
	opID := uuid.New()
	raw := map[string]interface{}{
		"alert_type":  "operator_down",
		"tenant_id":   tenantID.String(),
		"severity":    "critical",
		"title":       "Operator turkcell is DOWN",
		"entity_type": "operator",
		"entity_id":   opID.String(),
	}
	data, _ := json.Marshal(raw)

	p, err := parseAlertPayload(data)
	if err != nil {
		t.Fatalf("parseAlertPayload: %v", err)
	}
	if p.DedupKey == nil {
		t.Fatal("dedup_key must be non-nil post-FIX-210")
	}
	if len(*p.DedupKey) != 64 {
		t.Errorf("dedup_key length = %d, want 64 (sha256 hex)", len(*p.DedupKey))
	}

	// Shape D — consumer_lag event with no entity triple → "-" prefix,
	// key still non-nil and 64 chars.
	raw2 := map[string]interface{}{
		"source":   "nats_consumer_lag",
		"consumer": "argus-anomaly-events",
		"pending":  float64(150),
	}
	data2, _ := json.Marshal(raw2)
	p2, err := parseAlertPayload(data2)
	if err != nil {
		t.Fatalf("parseAlertPayload (no tenant): %v", err)
	}
	if p2.DedupKey == nil || len(*p2.DedupKey) != 64 {
		t.Error("dedup_key must be non-nil 64-char hex even for no-entity / no-tenant events")
	}
}

// -- FIX-210: handleAlertPersist — dedup + cooldown outcome tests --

// TestHandleAlertPersist_SecondEvent_Deduplicates — two identical events
// must result in one UpsertInserted then one UpsertDeduplicated. Dispatch
// runs both times (availability); persist count reflects both attempts
// (mock still records both calls — dedup is about the STORE outcome, not
// the publisher suppressing the event). Also asserts the dedup metric
// increments exactly once through the end-to-end handleAlertPersist path.
func TestHandleAlertPersist_SecondEvent_Deduplicates(t *testing.T) {
	email := &mockEmailSender{}
	alertStore := &mockAlertStore{
		outcomes: []store.UpsertResult{store.UpsertInserted, store.UpsertDeduplicated},
	}
	svc, sub, reg := newPersistSvcWithMetrics(t, email, alertStore)
	defer svc.Stop()

	tenantID := uuid.New()
	simID := uuid.New()
	payload := map[string]interface{}{
		"alert_type":  "anomaly_data_spike",
		"tenant_id":   tenantID.String(),
		"severity":    "high",
		"title":       "Data spike on SIM",
		"sim_id":      simID.String(),
		"entity_type": "sim",
		"entity_id":   simID.String(),
	}
	data, _ := json.Marshal(payload)

	sub.Publish("argus.events.alert.triggered", data)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(80 * time.Millisecond)

	alertStore.mu.Lock()
	if len(alertStore.calls) != 2 {
		t.Errorf("UpsertWithDedup calls = %d, want 2 (both attempts reach the store)", len(alertStore.calls))
	}
	// Both params must carry identical dedup_keys — deterministic.
	if alertStore.calls[0].DedupKey == nil || alertStore.calls[1].DedupKey == nil {
		t.Fatal("dedup_key must be non-nil on every call")
	}
	if *alertStore.calls[0].DedupKey != *alertStore.calls[1].DedupKey {
		t.Errorf("dedup_key not stable across identical events: %q vs %q",
			*alertStore.calls[0].DedupKey, *alertStore.calls[1].DedupKey)
	}
	alertStore.mu.Unlock()

	// Dispatch MUST run twice — availability > durability.
	email.mu.Lock()
	if len(email.calls) != 2 {
		t.Errorf("dispatch calls = %d, want 2 (dispatch runs regardless of persist outcome)", len(email.calls))
	}
	email.mu.Unlock()

	// Dedup metric must have incremented exactly once for the
	// deduplicated event. The first event (UpsertInserted) does not
	// touch this counter.
	text := scrapeNotifMetrics(t, reg)
	want := `argus_alerts_deduplicated_total{source="sim",type="anomaly_data_spike"} 1`
	if !strings.Contains(text, want) {
		t.Errorf("missing dedup counter line %q\n%s", want, text)
	}
	// The cooldown counter for this label set must NOT appear (zero value
	// ⇒ Prometheus omits the series unless it was emitted at least once).
	if strings.Contains(text, `argus_alerts_cooldown_dropped_total{source="sim",type="anomaly_data_spike"}`) {
		t.Errorf("cooldown counter should not have been incremented during a pure-dedup test:\n%s", text)
	}
}

// TestHandleAlertPersist_Cooldown_DropsEvent — UpsertCoolingDown path
// logs + emits the cooldown metric, and dispatch still runs (events are
// never silently lost from notifications even when dropped from the DB).
func TestHandleAlertPersist_Cooldown_DropsEvent(t *testing.T) {
	email := &mockEmailSender{}
	alertStore := &mockAlertStore{
		outcomes: []store.UpsertResult{store.UpsertCoolingDown},
	}
	svc, sub, reg := newPersistSvcWithMetrics(t, email, alertStore)
	defer svc.Stop()

	tenantID := uuid.New()
	payload := map[string]interface{}{
		"alert_type": "operator_down",
		"tenant_id":  tenantID.String(),
		"severity":   "critical",
		"title":      "Operator DOWN (cooldown window)",
	}
	data, _ := json.Marshal(payload)
	sub.Publish("argus.events.alert.triggered", data)

	time.Sleep(50 * time.Millisecond)

	alertStore.mu.Lock()
	if len(alertStore.calls) != 1 {
		t.Errorf("UpsertWithDedup calls = %d, want 1", len(alertStore.calls))
	}
	alertStore.mu.Unlock()

	// Dispatch MUST still run — availability guarantee.
	email.mu.Lock()
	if len(email.calls) != 1 {
		t.Errorf("dispatch calls = %d, want 1 (cooldown drops persist, never dispatch)", len(email.calls))
	}
	email.mu.Unlock()

	// Cooldown counter must have incremented exactly once.
	text := scrapeNotifMetrics(t, reg)
	want := `argus_alerts_cooldown_dropped_total{source="operator",type="operator_down"} 1`
	if !strings.Contains(text, want) {
		t.Errorf("missing cooldown counter line %q\n%s", want, text)
	}
	// And the dedup counter for this label set must NOT appear.
	if strings.Contains(text, `argus_alerts_deduplicated_total{source="operator",type="operator_down"}`) {
		t.Errorf("dedup counter should not have been incremented during a pure-cooldown test:\n%s", text)
	}
}

// TestHandleAlertPersist_CooldownExpired_InsertsFresh — after the
// cooldown window expires the store switches back to UpsertInserted
// (new alert row). No special metric is emitted for a plain insert.
func TestHandleAlertPersist_CooldownExpired_InsertsFresh(t *testing.T) {
	email := &mockEmailSender{}
	alertStore := &mockAlertStore{
		outcomes: []store.UpsertResult{store.UpsertCoolingDown, store.UpsertInserted},
	}
	svc, sub := newPersistSvc(t, email, alertStore)
	defer svc.Stop()

	tenantID := uuid.New()
	payload := map[string]interface{}{
		"alert_type": "operator_down",
		"tenant_id":  tenantID.String(),
		"severity":   "critical",
		"title":      "Operator DOWN (cooldown → reopen)",
	}
	data, _ := json.Marshal(payload)

	sub.Publish("argus.events.alert.triggered", data) // during cooldown
	sub.Publish("argus.events.alert.triggered", data) // after cooldown

	time.Sleep(80 * time.Millisecond)

	alertStore.mu.Lock()
	if len(alertStore.calls) != 2 {
		t.Errorf("UpsertWithDedup calls = %d, want 2 (both attempts reach the store)", len(alertStore.calls))
	}
	alertStore.mu.Unlock()

	email.mu.Lock()
	if len(email.calls) != 2 {
		t.Errorf("dispatch calls = %d, want 2", len(email.calls))
	}
	email.mu.Unlock()
}
