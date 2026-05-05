package job

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/compliance"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

type PurgeSweepProcessor struct {
	jobs          *store.JobStore
	complianceSvc *compliance.Service
	eventBus      *bus.EventBus
	logger        zerolog.Logger
}

func NewPurgeSweepProcessor(
	jobs *store.JobStore,
	complianceSvc *compliance.Service,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *PurgeSweepProcessor {
	return &PurgeSweepProcessor{
		jobs:          jobs,
		complianceSvc: complianceSvc,
		eventBus:      eventBus,
		logger:        logger.With().Str("processor", JobTypePurgeSweep).Logger(),
	}
}

func (p *PurgeSweepProcessor) Type() string {
	return JobTypePurgeSweep
}

func (p *PurgeSweepProcessor) Process(ctx context.Context, job *store.Job) error {
	p.logger.Info().
		Str("job_id", job.ID.String()).
		Str("tenant_id", job.TenantID.String()).
		Msg("starting purge sweep")

	batchSize := 500
	totalPurged := 0
	totalFailed := 0
	totalPseudonymized := 0

	for {
		result, err := p.complianceSvc.RunPurgeSweep(ctx, batchSize)
		if err != nil {
			p.logger.Error().Err(err).Msg("purge sweep batch error")
			break
		}

		totalPurged += result.TotalPurged
		totalFailed += result.FailedCount
		totalPseudonymized += result.PseudonymizedLogs

		_ = p.jobs.UpdateProgress(ctx, job.ID, totalPurged, totalFailed, totalPurged+totalFailed)

		if result.TotalPurged == 0 && result.FailedCount == 0 {
			break
		}

		cancelled, checkErr := p.jobs.CheckCancelled(ctx, job.ID)
		if checkErr == nil && cancelled {
			p.logger.Info().Msg("purge sweep cancelled")
			break
		}
	}

	resultJSON, _ := json.Marshal(map[string]interface{}{
		"total_purged":       totalPurged,
		"total_failed":       totalFailed,
		"pseudonymized_logs": totalPseudonymized,
	})

	if err := p.jobs.Complete(ctx, job.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("complete purge sweep job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":       job.ID.String(),
			"tenant_id":    job.TenantID.String(),
			"type":         JobTypePurgeSweep,
			"state":        "completed",
			"total_purged": totalPurged,
			"total_failed": totalFailed,
		})
	}

	p.logger.Info().
		Int("purged", totalPurged).
		Int("failed", totalFailed).
		Int("pseudonymized", totalPseudonymized).
		Msg("purge sweep completed")

	return nil
}
