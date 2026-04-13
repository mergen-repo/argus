package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

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
	mu       sync.Mutex
	events   []publishedEvent
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
		Severity:    "warning",
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
		Severity:  "warning",
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

	// Threshold=warning, event severity=info → skip
	svc.SetPrefStore(&mockPrefStore{
		pref: &Preference{
			Channels:          []string{"email"},
			SeverityThreshold: "warning",
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

	// Threshold=warning, event severity=error → allow
	svc.SetPrefStore(&mockPrefStore{
		pref: &Preference{
			Channels:          []string{"email"},
			SeverityThreshold: "warning",
			Enabled:           true,
		},
	})

	ctx := context.Background()
	err := svc.Notify(ctx, NotifyRequest{
		TenantID:  uuid.New(),
		EventType: "anomaly.detected",
		ScopeType: ScopeSystem,
		Title:     "Error event",
		Body:      "above threshold",
		Severity:  "error",
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
