package anomaly

import (
	"context"
	"encoding/json"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type AnomalyCreator interface {
	Create(ctx context.Context, p CreateParams) (*AnomalyRecord, error)
	HasRecentAnomaly(ctx context.Context, tenantID uuid.UUID, simID *uuid.UUID, anomalyType string, window time.Duration) (bool, error)
	FindDataSpikeCandidates(ctx context.Context, multiplier float64) ([]DataSpikeRow, error)
	GetSimICCID(ctx context.Context, simID uuid.UUID) (string, error)
}

type CreateParams struct {
	TenantID   uuid.UUID
	SimID      *uuid.UUID
	Type       string
	Severity   string
	Details    json.RawMessage
	Source     *string
	DetectedAt time.Time
}

type AnomalyRecord struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	SimID      *uuid.UUID
	Type       string
	Severity   string
	State      string
	DetectedAt time.Time
}

type DataSpikeRow struct {
	SimID      uuid.UUID
	TenantID   uuid.UUID
	TodayBytes int64
	AvgBytes   float64
}

type AlertPublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

type SIMSuspender interface {
	Suspend(ctx context.Context, tenantID, simID uuid.UUID, userID *uuid.UUID, reason *string) error
}

type BatchDetector struct {
	store          AnomalyCreator
	publisher      AlertPublisher
	suspender      SIMSuspender
	thresholds     ThresholdConfig
	logger         zerolog.Logger
	alertSubject   string
	anomalySubject string
}

func NewBatchDetector(
	store AnomalyCreator,
	publisher AlertPublisher,
	suspender SIMSuspender,
	thresholds ThresholdConfig,
	alertSubject string,
	anomalySubject string,
	logger zerolog.Logger,
) *BatchDetector {
	return &BatchDetector{
		store:          store,
		publisher:      publisher,
		suspender:      suspender,
		thresholds:     thresholds,
		alertSubject:   alertSubject,
		anomalySubject: anomalySubject,
		logger:         logger.With().Str("component", "anomaly_batch_detector").Logger(),
	}
}

func (d *BatchDetector) SetThresholds(t ThresholdConfig) {
	d.thresholds = t
}

func (d *BatchDetector) RunDataSpikeDetection(ctx context.Context) (int, error) {
	multiplier := d.thresholds.DataSpikeMultiplier
	if multiplier <= 0 {
		multiplier = 3.0
	}

	candidates, err := d.store.FindDataSpikeCandidates(ctx, multiplier)
	if err != nil {
		return 0, err
	}

	detected := 0
	for _, c := range candidates {
		has, err := d.store.HasRecentAnomaly(ctx, c.TenantID, &c.SimID, TypeDataSpike, 24*time.Hour)
		if err != nil {
			d.logger.Warn().Err(err).Str("sim_id", c.SimID.String()).Msg("check recent anomaly failed")
			continue
		}
		if has {
			continue
		}

		details := map[string]interface{}{
			"today_bytes": c.TodayBytes,
			"avg_bytes":   c.AvgBytes,
			"multiplier":  float64(c.TodayBytes) / c.AvgBytes,
			"threshold":   multiplier,
		}
		detailsJSON, _ := json.Marshal(details)

		batchSource := "batch"
		record, err := d.store.Create(ctx, CreateParams{
			TenantID:   c.TenantID,
			SimID:      &c.SimID,
			Type:       TypeDataSpike,
			Severity:   SeverityHigh,
			Details:    detailsJSON,
			Source:     &batchSource,
			DetectedAt: time.Now().UTC(),
		})
		if err != nil {
			d.logger.Error().Err(err).Str("sim_id", c.SimID.String()).Msg("create data spike anomaly failed")
			continue
		}

		iccid, _ := d.store.GetSimICCID(ctx, c.SimID)

		d.publishAlert(ctx, record, iccid, details)
		detected++
	}

	d.logger.Info().
		Int("candidates", len(candidates)).
		Int("detected", detected).
		Float64("multiplier", multiplier).
		Msg("data spike detection completed")

	return detected, nil
}

func (d *BatchDetector) publishAlert(ctx context.Context, record *AnomalyRecord, iccid string, details map[string]interface{}) {
	if d.publisher == nil {
		return
	}

	if d.anomalySubject != "" {
		anomalyEnv := bus.NewEnvelope("anomaly.detected", record.TenantID.String(), record.Severity).
			WithSource("analytics").
			WithTitle(anomalyTitle(record.Type, iccid)).
			WithMessage(anomalyDescription(record.Type, details)).
			WithMeta("anomaly_type", record.Type).
			WithMeta("anomaly_id", record.ID.String()).
			WithMeta("details", details)
		if record.SimID != nil {
			anomalyEnv.SetEntity("sim", record.SimID.String(), simDisplayName(iccid))
			anomalyEnv.WithMeta("sim_id", record.SimID.String())
		}
		if err := d.publisher.Publish(ctx, d.anomalySubject, anomalyEnv); err != nil {
			d.logger.Warn().Err(err).Msg("publish anomaly event failed")
		}
	}

	if d.alertSubject != "" {
		alertEnv := bus.NewEnvelope("anomaly_"+record.Type, record.TenantID.String(), record.Severity).
			WithSource("sim").
			WithTitle(anomalyTitle(record.Type, iccid)).
			WithMessage(anomalyDescription(record.Type, details)).
			WithMeta("anomaly_id", record.ID.String()).
			WithMeta("anomaly_type", record.Type)
		for k, v := range details {
			alertEnv.WithMeta(k, v)
		}
		if record.SimID != nil {
			alertEnv.SetEntity("sim", record.SimID.String(), simDisplayName(iccid))
			alertEnv.WithMeta("sim_id", record.SimID.String())
		}
		if err := d.publisher.Publish(ctx, d.alertSubject, alertEnv); err != nil {
			d.logger.Warn().Err(err).Msg("publish alert event failed")
		}
	}
}

func anomalyTitle(anomalyType, iccid string) string {
	simRef := iccid
	if simRef == "" {
		simRef = "unknown"
	}
	switch anomalyType {
	case TypeSIMCloning:
		return "SIM Cloning Detected — " + simRef
	case TypeDataSpike:
		return "Data Usage Spike — " + simRef
	case TypeAuthFlood:
		return "Auth Flood Detected — " + simRef
	case TypeNASFlood:
		return "NAS Auth Flood Detected"
	default:
		return "Anomaly Detected — " + anomalyType
	}
}

func anomalyDescription(anomalyType string, details map[string]interface{}) string {
	switch anomalyType {
	case TypeSIMCloning:
		return "Same IMSI authenticated from multiple NAS IPs within the cloning detection window."
	case TypeDataSpike:
		return "SIM daily data usage exceeds the configured multiplier of its 30-day average."
	case TypeAuthFlood:
		return "Excessive authentication requests from same IMSI within detection window."
	case TypeNASFlood:
		return "Excessive authentication requests from same NAS IP within detection window."
	default:
		return "An anomaly was detected."
	}
}
