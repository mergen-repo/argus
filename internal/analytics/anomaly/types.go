package anomaly

import (
	"time"

	"github.com/google/uuid"
)

const (
	TypeSIMCloning = "sim_cloning"
	TypeDataSpike  = "data_spike"
	TypeAuthFlood  = "auth_flood"
	TypeNASFlood   = "nas_flood"
)

const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
	SeverityInfo     = "info"
)

const (
	StateOpen          = "open"
	StateAcknowledged  = "acknowledged"
	StateResolved      = "resolved"
	StateFalsePositive = "false_positive"
)

type ThresholdConfig struct {
	CloningWindowSec     int     `json:"cloning_window_sec"`
	DataSpikeMultiplier  float64 `json:"data_spike_multiplier"`
	DataSpikeAvgDays     int     `json:"data_spike_avg_days"`
	AuthFloodMax         int     `json:"auth_flood_max"`
	AuthFloodWindowSec   int     `json:"auth_flood_window_sec"`
	NASFloodMax          int     `json:"nas_flood_max"`
	NASFloodWindowSec    int     `json:"nas_flood_window_sec"`
	AutoSuspendOnCloning bool    `json:"auto_suspend_on_cloning"`
	FilterBulkJobs       bool    `json:"filter_bulk_jobs"`
}

func DefaultThresholds() ThresholdConfig {
	return ThresholdConfig{
		CloningWindowSec:     300,
		DataSpikeMultiplier:  3.0,
		DataSpikeAvgDays:     30,
		AuthFloodMax:         100,
		AuthFloodWindowSec:   60,
		NASFloodMax:          1000,
		NASFloodWindowSec:    60,
		AutoSuspendOnCloning: true,
		FilterBulkJobs:       true,
	}
}

type AuthEvent struct {
	IMSI       string    `json:"imsi"`
	SimID      uuid.UUID `json:"sim_id"`
	TenantID   uuid.UUID `json:"tenant_id"`
	OperatorID uuid.UUID `json:"operator_id"`
	NASIP      string    `json:"nas_ip"`
	Success    bool      `json:"success"`
	Source     string    `json:"source,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

type AnomalyEvent struct {
	ID         uuid.UUID              `json:"id"`
	TenantID   uuid.UUID              `json:"tenant_id"`
	SimID      *uuid.UUID             `json:"sim_id,omitempty"`
	SimICCID   string                 `json:"sim_iccid,omitempty"`
	Type       string                 `json:"type"`
	Severity   string                 `json:"severity"`
	Details    map[string]interface{} `json:"details"`
	DetectedAt time.Time              `json:"detected_at"`
}
