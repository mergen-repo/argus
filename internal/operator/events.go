package operator

import (
	"time"

	"github.com/google/uuid"
)

type OperatorHealthEvent struct {
	OperatorID     uuid.UUID `json:"operator_id"`
	OperatorName   string    `json:"operator_name,omitempty"`
	PreviousStatus string    `json:"previous_status"`
	CurrentStatus  string    `json:"current_status"`
	CircuitState   string    `json:"circuit_breaker_state"`
	LatencyMs      int       `json:"latency_ms,omitempty"`
	FailureReason  string    `json:"failure_reason,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

type AlertEvent struct {
	AlertID     string    `json:"alert_id"`
	AlertType   string    `json:"alert_type"`
	Severity    string    `json:"severity"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	EntityType  string    `json:"entity_type"`
	EntityID    uuid.UUID `json:"entity_id"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

const (
	AlertTypeOperatorDown  = "operator_down"
	AlertTypeOperatorUp    = "operator_recovered"
	AlertTypeSLAViolation  = "sla_violation"

	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)
