package notification

import (
	"context"
	"encoding/json"
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
