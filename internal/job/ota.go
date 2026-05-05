package job

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/ota"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type OTAProcessor struct {
	jobs        *store.JobStore
	otaStore    *store.OTAStore
	simStore    *store.SIMStore
	rateLimiter *ota.RateLimiter
	eventBus    *bus.EventBus
	logger      zerolog.Logger
}

func NewOTAProcessor(
	jobs *store.JobStore,
	otaStore *store.OTAStore,
	simStore *store.SIMStore,
	rateLimiter *ota.RateLimiter,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *OTAProcessor {
	return &OTAProcessor{
		jobs:        jobs,
		otaStore:    otaStore,
		simStore:    simStore,
		rateLimiter: rateLimiter,
		eventBus:    eventBus,
		logger:      logger.With().Str("processor", JobTypeOTACommand).Logger(),
	}
}

func (p *OTAProcessor) Type() string {
	return JobTypeOTACommand
}

func (p *OTAProcessor) Process(ctx context.Context, job *store.Job) error {
	var payload ota.BulkOTAPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal ota payload: %w", err)
	}

	totalSIMs := len(payload.SimIDs)
	if totalSIMs == 0 {
		result, _ := json.Marshal(ota.BulkOTAResult{TotalSIMs: 0})
		return p.jobs.Complete(ctx, job.ID, nil, result)
	}

	_ = p.jobs.UpdateProgress(ctx, job.ID, 0, 0, totalSIMs)

	queued := 0
	failed := 0

	type otaError struct {
		SimID string `json:"sim_id"`
		Error string `json:"error"`
	}
	var errs []otaError

	for i, simIDStr := range payload.SimIDs {
		if (i+1)%100 == 0 {
			cancelled, checkErr := p.jobs.CheckCancelled(ctx, job.ID)
			if checkErr == nil && cancelled {
				p.logger.Info().Int("index", i).Msg("job cancelled, stopping OTA")
				break
			}
		}

		simID, parseErr := uuid.Parse(simIDStr)
		if parseErr != nil {
			errs = append(errs, otaError{SimID: simIDStr, Error: "invalid SIM ID format"})
			failed++
			p.updateProgressPeriodic(ctx, job, queued, failed, totalSIMs, i)
			continue
		}

		if p.rateLimiter != nil {
			allowed, _, limErr := p.rateLimiter.Allow(ctx, simID)
			if limErr != nil {
				p.logger.Warn().Err(limErr).Str("sim_id", simIDStr).Msg("rate limit check failed")
			}
			if !allowed {
				errs = append(errs, otaError{SimID: simIDStr, Error: "OTA rate limit exceeded"})
				failed++
				p.updateProgressPeriodic(ctx, job, queued, failed, totalSIMs, i)
				continue
			}
		}

		apduData, buildErr := ota.BuildAPDU(payload.CommandType, payload.Payload)
		if buildErr != nil {
			errs = append(errs, otaError{SimID: simIDStr, Error: buildErr.Error()})
			failed++
			p.updateProgressPeriodic(ctx, job, queued, failed, totalSIMs, i)
			continue
		}

		maxRetries := payload.MaxRetries
		if maxRetries <= 0 {
			maxRetries = 3
		}

		_, createErr := p.otaStore.Create(ctx, job.TenantID, store.CreateOTACommandParams{
			SimID:        simID,
			CommandType:  string(payload.CommandType),
			Channel:      string(payload.Channel),
			SecurityMode: string(payload.SecurityMode),
			APDUData:     apduData,
			Payload:      payload.Payload,
			MaxRetries:   maxRetries,
			JobID:        &job.ID,
		})
		if createErr != nil {
			errs = append(errs, otaError{SimID: simIDStr, Error: createErr.Error()})
			failed++
			p.updateProgressPeriodic(ctx, job, queued, failed, totalSIMs, i)
			continue
		}

		queued++
		p.updateProgressPeriodic(ctx, job, queued, failed, totalSIMs, i)
	}

	_ = p.jobs.UpdateProgress(ctx, job.ID, queued, failed, totalSIMs)

	var errorReportJSON json.RawMessage
	if len(errs) > 0 {
		errorReportJSON, _ = json.Marshal(errs)
	}

	resultJSON, _ := json.Marshal(ota.BulkOTAResult{
		TotalSIMs:   totalSIMs,
		QueuedCount: queued,
		FailedCount: failed,
	})

	if err := p.jobs.Complete(ctx, job.ID, errorReportJSON, resultJSON); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}

	_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
		"job_id":       job.ID.String(),
		"tenant_id":    job.TenantID.String(),
		"type":         JobTypeOTACommand,
		"state":        "completed",
		"queued_count": queued,
		"failed_count": failed,
	})

	return nil
}

func (p *OTAProcessor) updateProgressPeriodic(ctx context.Context, job *store.Job, queued, failed, total, idx int) {
	if (idx+1)%100 == 0 || idx == total-1 {
		_ = p.jobs.UpdateProgress(ctx, job.ID, queued, failed, total)
		_ = p.eventBus.Publish(ctx, bus.SubjectJobProgress, map[string]interface{}{
			"job_id":          job.ID.String(),
			"tenant_id":       job.TenantID.String(),
			"type":            JobTypeOTACommand,
			"processed_items": queued,
			"failed_items":    failed,
			"total_items":     total,
			"progress_pct":    float64(queued+failed) / float64(total) * 100.0,
		})
	}
}
