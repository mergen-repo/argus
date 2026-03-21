package metrics

import (
	"time"

	"github.com/google/uuid"
)

type SystemStatus string

const (
	StatusHealthy  SystemStatus = "healthy"
	StatusDegraded SystemStatus = "degraded"
	StatusCritical SystemStatus = "critical"
)

const (
	ErrorRateDegradedThreshold = 0.05
	ErrorRateCriticalThreshold = 0.20
)

type LatencyPercentiles struct {
	P50 int `json:"p50"`
	P95 int `json:"p95"`
	P99 int `json:"p99"`
}

type OperatorMetrics struct {
	OperatorID    uuid.UUID          `json:"operator_id"`
	AuthPerSec    int64              `json:"auth_per_sec"`
	AuthErrorRate float64            `json:"auth_error_rate"`
	Latency       LatencyPercentiles `json:"latency"`
}

type SystemMetrics struct {
	AuthPerSec     int64                       `json:"auth_per_sec"`
	AuthErrorRate  float64                     `json:"auth_error_rate"`
	Latency        LatencyPercentiles          `json:"latency"`
	ActiveSessions int64                       `json:"active_sessions"`
	ByOperator     map[string]*OperatorMetrics `json:"by_operator"`
	SystemStatus   SystemStatus                `json:"system_status"`
}

type RealtimePayload struct {
	AuthPerSec     int64        `json:"auth_per_sec"`
	ErrorRate      float64      `json:"error_rate"`
	LatencyP50     int          `json:"latency_p50"`
	LatencyP95     int          `json:"latency_p95"`
	ActiveSessions int64        `json:"active_sessions"`
	SystemStatus   SystemStatus `json:"system_status"`
	Timestamp      string       `json:"timestamp"`
}

func DeriveStatus(errorRate float64) SystemStatus {
	if errorRate >= ErrorRateCriticalThreshold {
		return StatusCritical
	}
	if errorRate >= ErrorRateDegradedThreshold {
		return StatusDegraded
	}
	return StatusHealthy
}

func ToRealtimePayload(m SystemMetrics) RealtimePayload {
	return RealtimePayload{
		AuthPerSec:     m.AuthPerSec,
		ErrorRate:      m.AuthErrorRate,
		LatencyP50:     m.Latency.P50,
		LatencyP95:     m.Latency.P95,
		ActiveSessions: m.ActiveSessions,
		SystemStatus:   m.SystemStatus,
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
	}
}
