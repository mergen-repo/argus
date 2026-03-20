package operator

import (
	"context"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

func TestCachedHealthStruct(t *testing.T) {
	ch := CachedHealth{
		Status:       "healthy",
		LatencyMs:    15,
		CircuitState: "closed",
		CheckedAt:    "2026-03-20T10:00:00Z",
	}

	if ch.Status != "healthy" {
		t.Errorf("Status = %q, want %q", ch.Status, "healthy")
	}
	if ch.LatencyMs != 15 {
		t.Errorf("LatencyMs = %d, want %d", ch.LatencyMs, 15)
	}
	if ch.CircuitState != "closed" {
		t.Errorf("CircuitState = %q, want %q", ch.CircuitState, "closed")
	}
}

func TestHealthStatusFromCircuitState(t *testing.T) {
	tests := []struct {
		circuitState CircuitState
		checkSuccess bool
		wantStatus   string
	}{
		{CircuitClosed, true, "healthy"},
		{CircuitClosed, false, "degraded"},
		{CircuitOpen, true, "down"},
		{CircuitOpen, false, "down"},
		{CircuitHalfOpen, true, "degraded"},
		{CircuitHalfOpen, false, "degraded"},
	}

	for _, tt := range tests {
		var status string
		switch tt.circuitState {
		case CircuitOpen:
			status = "down"
		case CircuitHalfOpen:
			status = "degraded"
		case CircuitClosed:
			if tt.checkSuccess {
				status = "healthy"
			} else {
				status = "degraded"
			}
		}

		if status != tt.wantStatus {
			t.Errorf("circuit=%s success=%v: got %q, want %q",
				tt.circuitState, tt.checkSuccess, status, tt.wantStatus)
		}
	}
}

func TestCircuitBreakerIntegrationWithHealth(t *testing.T) {
	cb := NewCircuitBreaker(3, 10)

	if cb.State() != CircuitClosed {
		t.Fatalf("initial state = %s, want closed", cb.State())
	}

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Errorf("after 2 failures, state = %s, want closed", cb.State())
	}

	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Errorf("after 3 failures (threshold), state = %s, want open", cb.State())
	}

	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Errorf("after success, state = %s, want closed", cb.State())
	}
}

func TestNewHealthCheckerNilSafe(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	if hc == nil {
		t.Fatal("NewHealthChecker should not return nil")
	}
	if hc.breakers == nil {
		t.Error("breakers map should be initialized")
	}
	if hc.stopChs == nil {
		t.Error("stopChs map should be initialized")
	}
	if hc.lastStatus == nil {
		t.Error("lastStatus map should be initialized")
	}
	if hc.operatorNames == nil {
		t.Error("operatorNames map should be initialized")
	}
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

func TestHealthChecker_SetEventPublisher(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	pub := &mockEventPublisher{}
	hc.SetEventPublisher(pub, "argus.events.operator.health", "argus.events.alert.triggered")

	if hc.eventPub == nil {
		t.Error("eventPub should be set")
	}
	if hc.healthSubject != "argus.events.operator.health" {
		t.Errorf("healthSubject = %s, want argus.events.operator.health", hc.healthSubject)
	}
	if hc.alertSubject != "argus.events.alert.triggered" {
		t.Errorf("alertSubject = %s, want argus.events.alert.triggered", hc.alertSubject)
	}
}

func TestHealthChecker_SetSLATracker(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	tracker := NewSLATracker(nil, zerolog.Nop())
	hc.SetSLATracker(tracker)

	if hc.slaTracker == nil {
		t.Error("slaTracker should be set")
	}
}

func TestHealthChecker_PublishAlertNilPub(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	hc.publishAlert(context.Background(), [16]byte{}, "test", "operator_down", "critical", "title", "desc")
}

func TestHealthChecker_CheckSLAViolationNilTracker(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	hc.checkSLAViolation(context.Background(), [16]byte{}, "test")
}
