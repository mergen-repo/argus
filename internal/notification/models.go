package notification

import (
	"time"

	"github.com/google/uuid"
)

type ScopeType string

const (
	ScopeSystem   ScopeType = "system"
	ScopeSIM      ScopeType = "sim"
	ScopeAPN      ScopeType = "apn"
	ScopeOperator ScopeType = "operator"
)

type EventType string

const (
	EventOperatorDown        EventType = "operator.down"
	EventOperatorRecovered   EventType = "operator.recovered"
	EventSIMStateChanged     EventType = "sim.state_changed"
	EventJobCompleted        EventType = "job.completed"
	EventJobFailed           EventType = "job.failed"
	EventAlertNew            EventType = "alert.new"
	EventSLAViolation        EventType = "sla.violation"
	EventPolicyRollout       EventType = "policy.rollout_completed"
	EventQuotaWarning        EventType = "quota.warning"
	EventQuotaExceeded       EventType = "quota.exceeded"
	EventAnomalyDetected     EventType = "anomaly.detected"
	EventStorageAlert        EventType = "storage.alert"
)

type Notification struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	UserID       *uuid.UUID `json:"user_id,omitempty"`
	EventType    string     `json:"event_type"`
	ScopeType    string     `json:"scope_type"`
	ScopeRefID   *uuid.UUID `json:"scope_ref_id,omitempty"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	Severity     string     `json:"severity"`
	ChannelsSent []string   `json:"channels_sent"`
	State        string     `json:"state"`
	ReadAt       *time.Time `json:"read_at,omitempty"`
	SentAt       *time.Time `json:"sent_at,omitempty"`
	DeliveredAt  *time.Time `json:"delivered_at,omitempty"`
	FailedAt     *time.Time `json:"failed_at,omitempty"`
	RetryCount   int        `json:"retry_count"`
	DeliveryMeta map[string]interface{} `json:"delivery_meta,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type NotificationConfig struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	UserID         *uuid.UUID `json:"user_id,omitempty"`
	EventType      string     `json:"event_type"`
	ScopeType      string     `json:"scope_type"`
	ScopeRefID     *uuid.UUID `json:"scope_ref_id,omitempty"`
	Channels       map[string]bool `json:"channels"`
	ThresholdType  *string    `json:"threshold_type,omitempty"`
	ThresholdValue *float64   `json:"threshold_value,omitempty"`
	Enabled        bool       `json:"enabled"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type NotifyRequest struct {
	TenantID   uuid.UUID
	UserID     *uuid.UUID
	EventType  EventType
	ScopeType  ScopeType
	ScopeRefID *uuid.UUID
	Title      string
	Body       string
	Severity   string
}

type WebhookSender interface {
	Send(ctx interface{}, url, secret, payload string) error
}

type SMSSender interface {
	Send(ctx interface{}, phoneNumber, message string) error
}
