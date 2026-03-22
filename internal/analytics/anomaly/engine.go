package anomaly

import (
	"context"
	"encoding/json"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type MessageSubscriber interface {
	QueueSubscribe(subject, queue string, handler func(subject string, data []byte)) (Subscription, error)
}

type Subscription interface {
	Unsubscribe() error
}

type Engine struct {
	detector       *RealtimeDetector
	anomalyStore   *store.AnomalyStore
	simStore       SIMSuspender
	publisher      AlertPublisher
	thresholds     ThresholdConfig
	logger         zerolog.Logger
	subs           []Subscription
	alertSubject   string
	anomalySubject string
}

type EngineConfig struct {
	Thresholds     ThresholdConfig
	AlertSubject   string
	AnomalySubject string
}

func NewEngine(
	detector *RealtimeDetector,
	anomalyStore *store.AnomalyStore,
	simStore SIMSuspender,
	publisher AlertPublisher,
	cfg EngineConfig,
	logger zerolog.Logger,
) *Engine {
	return &Engine{
		detector:       detector,
		anomalyStore:   anomalyStore,
		simStore:       simStore,
		publisher:      publisher,
		thresholds:     cfg.Thresholds,
		alertSubject:   cfg.AlertSubject,
		anomalySubject: cfg.AnomalySubject,
		logger:         logger.With().Str("component", "anomaly_engine").Logger(),
	}
}

func (e *Engine) Start(subscriber MessageSubscriber) error {
	sub, err := subscriber.QueueSubscribe("argus.events.auth.attempt", "anomaly-engine", func(subject string, data []byte) {
		e.handleAuthEvent(data)
	})
	if err != nil {
		return err
	}
	e.subs = append(e.subs, sub)

	e.logger.Info().Msg("anomaly engine started")
	return nil
}

func (e *Engine) Stop() {
	for _, sub := range e.subs {
		sub.Unsubscribe()
	}
	e.subs = nil
	e.logger.Info().Msg("anomaly engine stopped")
}

func (e *Engine) handleAuthEvent(data []byte) {
	var evt AuthEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		e.logger.Error().Err(err).Msg("unmarshal auth event")
		return
	}

	if e.thresholds.FilterBulkJobs && evt.Source == "bulk_job" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := e.detector.CheckAuth(ctx, evt)
	if err != nil {
		e.logger.Error().Err(err).Str("imsi", evt.IMSI).Msg("check auth failed")
		return
	}

	for _, result := range results {
		e.processDetection(ctx, result)
	}
}

func (e *Engine) processDetection(ctx context.Context, result DetectionResult) {
	dedupWindow := 5 * time.Minute
	if result.Type == TypeDataSpike {
		dedupWindow = 24 * time.Hour
	}

	has, err := e.anomalyStore.HasRecentAnomaly(ctx, result.TenantID, result.SimID, result.Type, dedupWindow)
	if err != nil {
		e.logger.Warn().Err(err).Msg("check recent anomaly failed")
		return
	}
	if has {
		return
	}

	detailsJSON, _ := json.Marshal(result.Details)
	realtimeSource := "realtime"

	record, err := e.anomalyStore.Create(ctx, store.CreateAnomalyParams{
		TenantID:   result.TenantID,
		SimID:      result.SimID,
		Type:       result.Type,
		Severity:   result.Severity,
		Details:    detailsJSON,
		Source:     &realtimeSource,
		DetectedAt: time.Now().UTC(),
	})
	if err != nil {
		e.logger.Error().Err(err).Str("type", result.Type).Msg("create anomaly record failed")
		return
	}

	var iccid string
	if result.SimID != nil {
		iccid, _ = e.anomalyStore.GetSimICCID(ctx, *result.SimID)
	}

	e.publishEvents(ctx, record, iccid, result.Details)

	if result.Severity == SeverityCritical && result.Type == TypeSIMCloning {
		e.handleCriticalAnomaly(ctx, result, record)
	}

	e.logger.Warn().
		Str("type", result.Type).
		Str("severity", result.Severity).
		Str("anomaly_id", record.ID.String()).
		Msg("anomaly detected")
}

func (e *Engine) handleCriticalAnomaly(ctx context.Context, result DetectionResult, record *store.Anomaly) {
	if !e.thresholds.AutoSuspendOnCloning {
		return
	}

	if result.SimID == nil || e.simStore == nil {
		return
	}

	reason := "Auto-suspended: SIM cloning detected"
	if err := e.simStore.Suspend(ctx, result.TenantID, *result.SimID, nil, &reason); err != nil {
		e.logger.Error().Err(err).
			Str("sim_id", result.SimID.String()).
			Msg("auto-suspend SIM failed")
	} else {
		e.logger.Warn().
			Str("sim_id", result.SimID.String()).
			Str("anomaly_id", record.ID.String()).
			Msg("SIM auto-suspended due to cloning detection")
	}
}

func (e *Engine) publishEvents(ctx context.Context, record *store.Anomaly, iccid string, details map[string]interface{}) {
	if e.publisher == nil {
		return
	}

	evt := AnomalyEvent{
		ID:         record.ID,
		TenantID:   record.TenantID,
		SimID:      record.SimID,
		SimICCID:   iccid,
		Type:       record.Type,
		Severity:   record.Severity,
		Details:    details,
		DetectedAt: record.DetectedAt,
	}

	if e.anomalySubject != "" {
		if err := e.publisher.Publish(ctx, e.anomalySubject, evt); err != nil {
			e.logger.Warn().Err(err).Msg("publish anomaly event failed")
		}
	}

	if e.alertSubject != "" {
		alert := map[string]interface{}{
			"alert_id":    record.ID.String(),
			"alert_type":  "anomaly_" + record.Type,
			"severity":    record.Severity,
			"title":       anomalyTitle(record.Type, iccid),
			"description": anomalyDescription(record.Type, details),
			"entity_type": "anomaly",
			"entity_id":   record.ID.String(),
			"metadata":    details,
			"timestamp":   record.DetectedAt.Format(time.RFC3339),
		}
		if err := e.publisher.Publish(ctx, e.alertSubject, alert); err != nil {
			e.logger.Warn().Err(err).Msg("publish alert event failed")
		}
	}
}

func (e *Engine) ProcessAuthEvent(ctx context.Context, evt AuthEvent) error {
	results, err := e.detector.CheckAuth(ctx, evt)
	if err != nil {
		return err
	}
	for _, result := range results {
		e.processDetection(ctx, result)
	}
	return nil
}

type AnomalyStoreAdapter struct {
	s *store.AnomalyStore
}

func NewAnomalyStoreAdapter(s *store.AnomalyStore) *AnomalyStoreAdapter {
	return &AnomalyStoreAdapter{s: s}
}

func (a *AnomalyStoreAdapter) Create(ctx context.Context, p CreateParams) (*AnomalyRecord, error) {
	record, err := a.s.Create(ctx, store.CreateAnomalyParams{
		TenantID:   p.TenantID,
		SimID:      p.SimID,
		Type:       p.Type,
		Severity:   p.Severity,
		Details:    p.Details,
		Source:     p.Source,
		DetectedAt: p.DetectedAt,
	})
	if err != nil {
		return nil, err
	}
	return &AnomalyRecord{
		ID:         record.ID,
		TenantID:   record.TenantID,
		SimID:      record.SimID,
		Type:       record.Type,
		Severity:   record.Severity,
		State:      record.State,
		DetectedAt: record.DetectedAt,
	}, nil
}

func (a *AnomalyStoreAdapter) HasRecentAnomaly(ctx context.Context, tenantID uuid.UUID, simID *uuid.UUID, anomalyType string, window time.Duration) (bool, error) {
	return a.s.HasRecentAnomaly(ctx, tenantID, simID, anomalyType, window)
}

func (a *AnomalyStoreAdapter) FindDataSpikeCandidates(ctx context.Context, multiplier float64) ([]DataSpikeRow, error) {
	candidates, err := a.s.FindDataSpikeCandidates(ctx, multiplier)
	if err != nil {
		return nil, err
	}
	rows := make([]DataSpikeRow, len(candidates))
	for i, c := range candidates {
		rows[i] = DataSpikeRow{
			SimID:      c.SimID,
			TenantID:   c.TenantID,
			TodayBytes: c.TodayBytes,
			AvgBytes:   c.AvgBytes,
		}
	}
	return rows, nil
}

func (a *AnomalyStoreAdapter) GetSimICCID(ctx context.Context, simID uuid.UUID) (string, error) {
	return a.s.GetSimICCID(ctx, simID)
}
