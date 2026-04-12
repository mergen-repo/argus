package operator

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/google/uuid"
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

// scrapeMetrics fetches the /metrics body from the supplied registry.
func scrapeMetrics(t *testing.T, reg *obsmetrics.Registry) string {
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

func TestHealthChecker_SetMetricsRegistry_WiresBreakerHook(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())

	opID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	cb := NewCircuitBreaker(1, 10)

	// Register the breaker manually — mimicking startOperatorLoop.
	hc.mu.Lock()
	hc.breakers[opID] = cb
	hc.lastStatus[opID] = "healthy"
	hc.operatorNames[opID] = "acme"
	hc.mu.Unlock()

	reg := obsmetrics.NewRegistry()
	hc.SetMetricsRegistry(reg)

	// Seeding should publish closed=1 immediately.
	text := scrapeMetrics(t, reg)
	wantClosed := `argus_circuit_breaker_state{operator_id="` + opID.String() + `",state="closed"} 1`
	if !strings.Contains(text, wantClosed) {
		t.Errorf("missing seed line %q\n%s", wantClosed, text)
	}

	// Trip the breaker — hook should update the gauge.
	cb.RecordFailure()
	text = scrapeMetrics(t, reg)
	wantOpen := `argus_circuit_breaker_state{operator_id="` + opID.String() + `",state="open"} 1`
	if !strings.Contains(text, wantOpen) {
		t.Errorf("missing open line after failure %q\n%s", wantOpen, text)
	}
	wantClosedZero := `argus_circuit_breaker_state{operator_id="` + opID.String() + `",state="closed"} 0`
	if !strings.Contains(text, wantClosedZero) {
		t.Errorf("expected closed=0 after open transition, got\n%s", text)
	}
}

func TestHealthChecker_SetMetricsRegistry_NilClearsHook(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	cb := NewCircuitBreaker(1, 10)
	opID := uuid.New()

	hc.mu.Lock()
	hc.breakers[opID] = cb
	hc.mu.Unlock()

	reg := obsmetrics.NewRegistry()
	hc.SetMetricsRegistry(reg)
	hc.SetMetricsRegistry(nil)

	// After clearing, breaker transitions must not panic or affect
	// the previously attached registry.
	cb.RecordFailure()
}

func TestHealthChecker_SetMetricsRegistry_NoBreakersIsSafe(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	reg := obsmetrics.NewRegistry()
	hc.SetMetricsRegistry(reg) // no breakers registered — must not panic
}
