package job

import (
	"context"
	"encoding/json"

	"github.com/btopcu/argus/internal/analytics/anomaly"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

type AnomalyBatchProcessor struct {
	batchDetector *anomaly.BatchDetector
	jobs          *store.JobStore
	eventBus      *bus.EventBus
	logger        zerolog.Logger
}

func NewAnomalyBatchProcessor(
	batchDetector *anomaly.BatchDetector,
	jobs *store.JobStore,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *AnomalyBatchProcessor {
	return &AnomalyBatchProcessor{
		batchDetector: batchDetector,
		jobs:          jobs,
		eventBus:      eventBus,
		logger:        logger.With().Str("component", "anomaly_batch_processor").Logger(),
	}
}

func (p *AnomalyBatchProcessor) Type() string {
	return JobTypeAnomalyBatch
}

func (p *AnomalyBatchProcessor) Process(ctx context.Context, job *store.Job) error {
	p.logger.Info().Str("job_id", job.ID.String()).Msg("starting anomaly batch detection")

	detected, err := p.batchDetector.RunDataSpikeDetection(ctx)
	if err != nil {
		p.logger.Error().Err(err).Msg("data spike detection failed")
		errReport, _ := json.Marshal(map[string]string{"error": err.Error()})
		_ = p.jobs.Fail(ctx, job.ID, errReport)
		return err
	}

	result, _ := json.Marshal(map[string]interface{}{
		"data_spikes_detected": detected,
	})

	if err := p.jobs.Complete(ctx, job.ID, nil, result); err != nil {
		p.logger.Error().Err(err).Msg("complete job failed")
		return err
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":    job.ID.String(),
			"tenant_id": job.TenantID.String(),
			"type":      JobTypeAnomalyBatch,
			"state":     "completed",
			"result":    map[string]int{"data_spikes_detected": detected},
		})
	}

	p.logger.Info().
		Int("detected", detected).
		Str("job_id", job.ID.String()).
		Msg("anomaly batch detection completed")

	return nil
}
