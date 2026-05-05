package job

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

type DataRetentionProcessor struct {
	jobs           *store.JobStore
	lifecycleStore *store.DataLifecycleStore
	storageStore   *store.StorageMonitorStore
	eventBus       *bus.EventBus
	defaultCDRDays int
	logger         zerolog.Logger
}

func NewDataRetentionProcessor(
	jobs *store.JobStore,
	lifecycleStore *store.DataLifecycleStore,
	storageStore *store.StorageMonitorStore,
	eventBus *bus.EventBus,
	defaultCDRDays int,
	logger zerolog.Logger,
) *DataRetentionProcessor {
	if defaultCDRDays <= 0 {
		defaultCDRDays = 365
	}
	return &DataRetentionProcessor{
		jobs:           jobs,
		lifecycleStore: lifecycleStore,
		storageStore:   storageStore,
		eventBus:       eventBus,
		defaultCDRDays: defaultCDRDays,
		logger:         logger.With().Str("processor", JobTypeDataRetention).Logger(),
	}
}

func (p *DataRetentionProcessor) Type() string {
	return JobTypeDataRetention
}

type dataRetentionResult struct {
	CDRChunksDropped     int64  `json:"cdr_chunks_dropped"`
	SessionChunksDropped int64  `json:"session_chunks_dropped"`
	TenantsProcessed     int    `json:"tenants_processed"`
	DefaultRetentionDays int    `json:"default_retention_days"`
	Status               string `json:"status"`
}

func (p *DataRetentionProcessor) Process(ctx context.Context, job *store.Job) error {
	p.logger.Info().
		Str("job_id", job.ID.String()).
		Int("default_cdr_days", p.defaultCDRDays).
		Msg("starting data retention sweep")

	configs, err := p.lifecycleStore.ListRetentionConfigs(ctx)
	if err != nil {
		p.logger.Warn().Err(err).Msg("list retention configs failed, using defaults")
		configs = nil
	}

	result := dataRetentionResult{
		DefaultRetentionDays: p.defaultCDRDays,
	}

	minCDROlderThan := time.Now().UTC().AddDate(0, 0, -p.defaultCDRDays)
	minSessionOlderThan := time.Now().UTC().AddDate(0, 0, -p.defaultCDRDays)

	for _, cfg := range configs {
		cdrOlder := time.Now().UTC().AddDate(0, 0, -cfg.CDRRetentionDays)
		if cdrOlder.Before(minCDROlderThan) {
			minCDROlderThan = cdrOlder
		}

		sessionDays := cfg.SessionRetentionDays
		if sessionDays <= 0 {
			sessionDays = p.defaultCDRDays
		}
		sessionOlder := time.Now().UTC().AddDate(0, 0, -sessionDays)
		if sessionOlder.Before(minSessionOlderThan) {
			minSessionOlderThan = sessionOlder
		}

		result.TenantsProcessed++
	}

	cdrDropped, err := p.storageStore.DropChunk(ctx, "cdrs", minCDROlderThan)
	if err != nil {
		p.logger.Error().Err(err).
			Time("older_than", minCDROlderThan).
			Msg("drop CDR chunks failed")
	} else {
		result.CDRChunksDropped = cdrDropped
		p.logger.Info().
			Int64("dropped", cdrDropped).
			Time("older_than", minCDROlderThan).
			Msg("CDR chunks dropped")
	}

	sessionDropped, err := p.storageStore.DropChunk(ctx, "sessions", minSessionOlderThan)
	if err != nil {
		p.logger.Error().Err(err).
			Time("older_than", minSessionOlderThan).
			Msg("drop session chunks failed")
	} else {
		result.SessionChunksDropped = sessionDropped
		p.logger.Info().
			Int64("dropped", sessionDropped).
			Time("older_than", minSessionOlderThan).
			Msg("session chunks dropped")
	}

	result.Status = "completed"

	resultJSON, _ := json.Marshal(result)
	if err := p.jobs.Complete(ctx, job.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("complete data retention job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":             job.ID.String(),
			"tenant_id":          job.TenantID.String(),
			"type":               JobTypeDataRetention,
			"state":              "completed",
			"cdr_chunks_dropped": result.CDRChunksDropped,
		})
	}

	p.logger.Info().
		Int64("cdr_dropped", result.CDRChunksDropped).
		Int64("session_dropped", result.SessionChunksDropped).
		Int("tenants", result.TenantsProcessed).
		Msg("data retention sweep completed")

	return nil
}
