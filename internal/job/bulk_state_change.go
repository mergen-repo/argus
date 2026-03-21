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

const (
	bulkBatchSize = 100
	lockTTL       = 30 * time.Second
)

type BulkStateChangeProcessor struct {
	jobs     *store.JobStore
	sims     *store.SIMStore
	segments *store.SegmentStore
	distLock *DistributedLock
	eventBus *bus.EventBus
	logger   zerolog.Logger
}

func NewBulkStateChangeProcessor(
	jobs *store.JobStore,
	sims *store.SIMStore,
	segments *store.SegmentStore,
	distLock *DistributedLock,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *BulkStateChangeProcessor {
	return &BulkStateChangeProcessor{
		jobs:     jobs,
		sims:     sims,
		segments: segments,
		distLock: distLock,
		eventBus: eventBus,
		logger:   logger.With().Str("processor", JobTypeBulkStateChange).Logger(),
	}
}

func (p *BulkStateChangeProcessor) Type() string {
	return JobTypeBulkStateChange
}

func (p *BulkStateChangeProcessor) Process(ctx context.Context, j *store.Job) error {
	var payload BulkStateChangePayload
	if err := json.Unmarshal(j.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal state change payload: %w", err)
	}

	if len(payload.UndoRecords) > 0 {
		return p.processUndo(ctx, j, payload)
	}
	return p.processForward(ctx, j, payload)
}

func (p *BulkStateChangeProcessor) processForward(ctx context.Context, j *store.Job, payload BulkStateChangePayload) error {
	simDetails, err := p.segments.ListMatchingSIMIDsWithDetails(ctx, payload.SegmentID)
	if err != nil {
		return fmt.Errorf("list segment sims: %w", err)
	}

	total := len(simDetails)
	if total == 0 {
		result, _ := json.Marshal(BulkResult{TotalCount: 0})
		return p.jobs.Complete(ctx, j.ID, nil, result)
	}

	_ = p.jobs.UpdateProgress(ctx, j.ID, 0, 0, total)

	var (
		processed   int
		failed      int
		errors      []BulkOpError
		undoRecords []StateUndoRecord
	)

	holderID := j.ID.String()

	for i, sim := range simDetails {
		if (i+1)%bulkBatchSize == 0 {
			cancelled, checkErr := p.jobs.CheckCancelled(ctx, j.ID)
			if checkErr == nil && cancelled {
				p.logger.Info().Int("index", i).Msg("job cancelled, stopping")
				break
			}
		}

		lockKey := p.distLock.SIMKey(sim.ID.String())
		acquired, lockErr := p.distLock.Acquire(ctx, lockKey, holderID, lockTTL)
		if lockErr != nil || !acquired {
			errors = append(errors, BulkOpError{
				SimID:        sim.ID.String(),
				ICCID:        sim.ICCID,
				ErrorCode:    "LOCK_FAILED",
				ErrorMessage: "could not acquire distributed lock for SIM",
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}

		previousState := sim.State
		var reason interface{}
		if payload.Reason != nil {
			reason = payload.Reason
		}

		_, transErr := p.sims.TransitionState(ctx, sim.ID, payload.TargetState, nil, "bulk_job", reason, 0)
		_ = p.distLock.Release(ctx, lockKey, holderID)

		if transErr != nil {
			errCode := "TRANSITION_FAILED"
			if transErr == store.ErrInvalidStateTransition {
				errCode = "INVALID_STATE_TRANSITION"
			}
			errors = append(errors, BulkOpError{
				SimID:        sim.ID.String(),
				ICCID:        sim.ICCID,
				ErrorCode:    errCode,
				ErrorMessage: transErr.Error(),
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}

		undoRecords = append(undoRecords, StateUndoRecord{
			SimID:         sim.ID,
			PreviousState: previousState,
		})
		processed++
		p.publishProgress(ctx, j, processed, failed, total, i)
	}

	return p.completeJob(ctx, j, processed, failed, total, errors, undoRecords)
}

func (p *BulkStateChangeProcessor) processUndo(ctx context.Context, j *store.Job, payload BulkStateChangePayload) error {
	total := len(payload.UndoRecords)
	_ = p.jobs.UpdateProgress(ctx, j.ID, 0, 0, total)

	var (
		processed int
		failed    int
		errors    []BulkOpError
	)

	holderID := j.ID.String()

	for i, rec := range payload.UndoRecords {
		if (i+1)%bulkBatchSize == 0 {
			cancelled, checkErr := p.jobs.CheckCancelled(ctx, j.ID)
			if checkErr == nil && cancelled {
				break
			}
		}

		lockKey := p.distLock.SIMKey(rec.SimID.String())
		acquired, lockErr := p.distLock.Acquire(ctx, lockKey, holderID, lockTTL)
		if lockErr != nil || !acquired {
			errors = append(errors, BulkOpError{
				SimID:        rec.SimID.String(),
				ErrorCode:    "LOCK_FAILED",
				ErrorMessage: "could not acquire distributed lock for SIM",
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}

		reason := "bulk_undo"
		_, transErr := p.sims.TransitionState(ctx, rec.SimID, rec.PreviousState, nil, "bulk_undo", &reason, 0)
		_ = p.distLock.Release(ctx, lockKey, holderID)

		if transErr != nil {
			errors = append(errors, BulkOpError{
				SimID:        rec.SimID.String(),
				ErrorCode:    "UNDO_FAILED",
				ErrorMessage: transErr.Error(),
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}

		processed++
		p.publishProgress(ctx, j, processed, failed, total, i)
	}

	return p.completeJob(ctx, j, processed, failed, total, errors, nil)
}

func (p *BulkStateChangeProcessor) completeJob(ctx context.Context, j *store.Job, processed, failed, total int, errors []BulkOpError, undoRecords []StateUndoRecord) error {
	_ = p.jobs.UpdateProgress(ctx, j.ID, processed, failed, total)

	var errorReportJSON json.RawMessage
	if len(errors) > 0 {
		errorReportJSON, _ = json.Marshal(errors)
	}

	resultJSON, _ := json.Marshal(BulkResult{
		ProcessedCount: processed,
		FailedCount:    failed,
		TotalCount:     total,
		UndoRecords:    undoRecords,
	})

	if err := p.jobs.Complete(ctx, j.ID, errorReportJSON, resultJSON); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}

	_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
		"job_id":          j.ID.String(),
		"tenant_id":       j.TenantID.String(),
		"type":            JobTypeBulkStateChange,
		"state":           "completed",
		"processed_count": processed,
		"failed_count":    failed,
	})

	return nil
}

func (p *BulkStateChangeProcessor) publishProgress(ctx context.Context, j *store.Job, processed, failed, total, idx int) {
	if (idx+1)%bulkBatchSize == 0 || idx == total-1 {
		_ = p.jobs.UpdateProgress(ctx, j.ID, processed, failed, total)
		_ = p.eventBus.Publish(ctx, bus.SubjectJobProgress, map[string]interface{}{
			"job_id":          j.ID.String(),
			"tenant_id":       j.TenantID.String(),
			"type":            JobTypeBulkStateChange,
			"processed_items": processed,
			"failed_items":    failed,
			"total_items":     total,
			"progress_pct":    float64(processed+failed) / float64(total) * 100.0,
		})
	}
}
