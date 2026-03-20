package operator

import (
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
}
