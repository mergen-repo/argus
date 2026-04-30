package sor

import (
	"time"

	"github.com/btopcu/argus/internal/aaa/rattype"
	"github.com/google/uuid"
)

type SoRDecision struct {
	PrimaryOperatorID   uuid.UUID   `json:"primary_operator_id"`
	FallbackOperatorIDs []uuid.UUID `json:"fallback_operator_ids"`
	Reason              string      `json:"reason"`
	IMSIPrefix          string      `json:"imsi_prefix,omitempty"`
	RATType             string      `json:"rat_type,omitempty"`
	CostPerMB           float64     `json:"cost_per_mb,omitempty"`
	EvaluatedAt         time.Time   `json:"evaluated_at"`
	Cached              bool        `json:"cached"`
}

type SoRRequest struct {
	IMSI        string
	TenantID    uuid.UUID
	RequestedRAT string
	SimID       uuid.UUID
	SimMetadata map[string]interface{}
}

type SoRConfig struct {
	CacheTTL          time.Duration
	RATPreferenceOrder []string
}

type CandidateOperator struct {
	OperatorID    uuid.UUID
	MCC           string
	MNC           string
	SupportedRATs []string
	SoRPriority   int
	CostPerMB     float64
	HealthStatus  string
}

const (
	ReasonIMSIPrefixMatch    = "imsi_prefix_match"
	ReasonCostOptimized      = "cost_optimized"
	ReasonRATPreference      = "rat_preference"
	ReasonManualLock         = "manual_lock"
	ReasonDefault            = "default"
)

var DefaultRATPreferenceOrder = []string{
	rattype.NR5G,
	rattype.NR5GNSA,
	rattype.LTE,
	rattype.UTRAN,
	rattype.GERAN,
	rattype.NBIOT,
	rattype.LTEM,
}

func DefaultConfig() SoRConfig {
	return SoRConfig{
		CacheTTL:          time.Hour,
		RATPreferenceOrder: DefaultRATPreferenceOrder,
	}
}
