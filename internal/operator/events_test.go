package operator

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOperatorHealthEvent_Serialization(t *testing.T) {
	evt := OperatorHealthEvent{
		OperatorID:     uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		OperatorName:   "turkcell",
		PreviousStatus: "healthy",
		CurrentStatus:  "down",
		CircuitState:   "open",
		LatencyMs:      0,
		FailureReason:  "connection timeout",
		Timestamp:      time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded OperatorHealthEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.OperatorID != evt.OperatorID {
		t.Errorf("OperatorID = %s, want %s", decoded.OperatorID, evt.OperatorID)
	}
	if decoded.OperatorName != evt.OperatorName {
		t.Errorf("OperatorName = %s, want %s", decoded.OperatorName, evt.OperatorName)
	}
	if decoded.PreviousStatus != evt.PreviousStatus {
		t.Errorf("PreviousStatus = %s, want %s", decoded.PreviousStatus, evt.PreviousStatus)
	}
	if decoded.CurrentStatus != evt.CurrentStatus {
		t.Errorf("CurrentStatus = %s, want %s", decoded.CurrentStatus, evt.CurrentStatus)
	}
	if decoded.CircuitState != evt.CircuitState {
		t.Errorf("CircuitState = %s, want %s", decoded.CircuitState, evt.CircuitState)
	}
	if decoded.FailureReason != evt.FailureReason {
		t.Errorf("FailureReason = %s, want %s", decoded.FailureReason, evt.FailureReason)
	}
}

func TestAlertEvent_Serialization(t *testing.T) {
	evt := AlertEvent{
		AlertID:     "alert-001",
		AlertType:   AlertTypeOperatorDown,
		Severity:    SeverityCritical,
		Title:       "Operator turkcell is DOWN",
		Description: "Circuit breaker opened",
		EntityType:  "operator",
		EntityID:    uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		Metadata: map[string]interface{}{
			"operator_name": "turkcell",
		},
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded AlertEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.AlertType != AlertTypeOperatorDown {
		t.Errorf("AlertType = %s, want %s", decoded.AlertType, AlertTypeOperatorDown)
	}
	if decoded.Severity != SeverityCritical {
		t.Errorf("Severity = %s, want %s", decoded.Severity, SeverityCritical)
	}
	if decoded.EntityID != evt.EntityID {
		t.Errorf("EntityID = %s, want %s", decoded.EntityID, evt.EntityID)
	}
}

func TestAlertTypeConstants(t *testing.T) {
	if AlertTypeOperatorDown != "operator_down" {
		t.Errorf("AlertTypeOperatorDown = %s", AlertTypeOperatorDown)
	}
	if AlertTypeOperatorUp != "operator_recovered" {
		t.Errorf("AlertTypeOperatorUp = %s", AlertTypeOperatorUp)
	}
	if AlertTypeSLAViolation != "sla_violation" {
		t.Errorf("AlertTypeSLAViolation = %s", AlertTypeSLAViolation)
	}
}

func TestSeverityConstants(t *testing.T) {
	if SeverityCritical != "critical" {
		t.Errorf("SeverityCritical = %s", SeverityCritical)
	}
	if SeverityHigh != "high" {
		t.Errorf("SeverityHigh = %s", SeverityHigh)
	}
	if SeverityMedium != "medium" {
		t.Errorf("SeverityMedium = %s", SeverityMedium)
	}
	if SeverityLow != "low" {
		t.Errorf("SeverityLow = %s", SeverityLow)
	}
	if SeverityInfo != "info" {
		t.Errorf("SeverityInfo = %s", SeverityInfo)
	}
}
