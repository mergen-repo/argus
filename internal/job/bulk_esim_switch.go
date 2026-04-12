package job

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

type BulkEsimSwitchProcessor struct {
	jobs      *store.JobStore
	sims      *store.SIMStore
	segments  *store.SegmentStore
	esimStore *store.ESimProfileStore
	distLock  *DistributedLock
	eventBus  *bus.EventBus
	logger    zerolog.Logger
}

func NewBulkEsimSwitchProcessor(
	jobs *store.JobStore,
	sims *store.SIMStore,
	segments *store.SegmentStore,
	esimStore *store.ESimProfileStore,
	distLock *DistributedLock,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *BulkEsimSwitchProcessor {
	return &BulkEsimSwitchProcessor{
		jobs:      jobs,
		sims:      sims,
		segments:  segments,
		esimStore: esimStore,
		distLock:  distLock,
		eventBus:  eventBus,
		logger:    logger.With().Str("processor", JobTypeBulkEsimSwitch).Logger(),
	}
}

func (p *BulkEsimSwitchProcessor) Type() string {
	return JobTypeBulkEsimSwitch
}

func (p *BulkEsimSwitchProcessor) Process(ctx context.Context, j *store.Job) error {
	var payload BulkEsimSwitchPayload
	if err := json.Unmarshal(j.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal esim switch payload: %w", err)
	}

	if len(payload.UndoRecords) > 0 {
		return p.processUndo(ctx, j, payload)
	}
	return p.processForward(ctx, j, payload)
}

func (p *BulkEsimSwitchProcessor) processForward(ctx context.Context, j *store.Job, payload BulkEsimSwitchPayload) error {
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
		undoRecords []EsimUndoRecord
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

		if sim.SimType != "esim" {
			p.logger.Debug().Str("sim_id", sim.ID.String()).Str("sim_type", sim.SimType).Msg("skipping non-eSIM")
			errors = append(errors, BulkOpError{
				SimID:        sim.ID.String(),
				ICCID:        sim.ICCID,
				ErrorCode:    "NOT_ESIM",
				ErrorMessage: "SIM is not an eSIM, skipping operator switch",
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
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

		enabledProfile, profErr := p.esimStore.GetEnabledProfileForSIM(ctx, j.TenantID, sim.ID)
		if profErr != nil {
			_ = p.distLock.Release(ctx, lockKey, holderID)
			errors = append(errors, BulkOpError{
				SimID:        sim.ID.String(),
				ICCID:        sim.ICCID,
				ErrorCode:    "PROFILE_LOOKUP_FAILED",
				ErrorMessage: profErr.Error(),
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}
		if enabledProfile == nil {
			_ = p.distLock.Release(ctx, lockKey, holderID)
			errors = append(errors, BulkOpError{
				SimID:        sim.ID.String(),
				ICCID:        sim.ICCID,
				ErrorCode:    "NO_ENABLED_PROFILE",
				ErrorMessage: "SIM has no enabled eSIM profile",
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}

		targetProfiles, _, listErr := p.esimStore.List(ctx, j.TenantID, store.ListESimProfilesParams{
			SimID:      &sim.ID,
			OperatorID: &payload.TargetOperatorID,
			State:      "disabled",
			Limit:      1,
		})
		if listErr != nil || len(targetProfiles) == 0 {
			_ = p.distLock.Release(ctx, lockKey, holderID)
			errMsg := "no disabled profile found for target operator"
			if listErr != nil {
				errMsg = listErr.Error()
			}
			errors = append(errors, BulkOpError{
				SimID:        sim.ID.String(),
				ICCID:        sim.ICCID,
				ErrorCode:    "NO_TARGET_PROFILE",
				ErrorMessage: errMsg,
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}

		targetProfile := targetProfiles[0]

		_, switchErr := p.esimStore.Switch(ctx, j.TenantID, enabledProfile.ID, targetProfile.ID, nil)
		_ = p.distLock.Release(ctx, lockKey, holderID)

		if switchErr != nil {
			errCode := "SWITCH_FAILED"
			if switchErr == store.ErrInvalidProfileState {
				errCode = "INVALID_PROFILE_STATE"
			}
			errors = append(errors, BulkOpError{
				SimID:        sim.ID.String(),
				ICCID:        sim.ICCID,
				ErrorCode:    errCode,
				ErrorMessage: switchErr.Error(),
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}

		undoRecords = append(undoRecords, EsimUndoRecord{
			SimID:              sim.ID,
			OldProfileID:       enabledProfile.ID,
			NewProfileID:       targetProfile.ID,
			PreviousOperatorID: sim.OperatorID,
		})
		processed++
		p.publishProgress(ctx, j, processed, failed, total, i)
	}

	return p.completeJob(ctx, j, processed, failed, total, errors, undoRecords)
}

func (p *BulkEsimSwitchProcessor) processUndo(ctx context.Context, j *store.Job, payload BulkEsimSwitchPayload) error {
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

		_, switchErr := p.esimStore.Switch(ctx, j.TenantID, rec.NewProfileID, rec.OldProfileID, nil)
		_ = p.distLock.Release(ctx, lockKey, holderID)

		if switchErr != nil {
			errors = append(errors, BulkOpError{
				SimID:        rec.SimID.String(),
				ErrorCode:    "UNDO_FAILED",
				ErrorMessage: switchErr.Error(),
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

func (p *BulkEsimSwitchProcessor) completeJob(ctx context.Context, j *store.Job, processed, failed, total int, errors []BulkOpError, undoRecords []EsimUndoRecord) error {
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
		"type":            JobTypeBulkEsimSwitch,
		"state":           "completed",
		"processed_count": processed,
		"failed_count":    failed,
	})

	return nil
}

func (p *BulkEsimSwitchProcessor) publishProgress(ctx context.Context, j *store.Job, processed, failed, total, idx int) {
	if (idx+1)%bulkBatchSize == 0 || idx == total-1 {
		_ = p.jobs.UpdateProgress(ctx, j.ID, processed, failed, total)
		_ = p.eventBus.Publish(ctx, bus.SubjectJobProgress, map[string]interface{}{
			"job_id":          j.ID.String(),
			"tenant_id":       j.TenantID.String(),
			"type":            JobTypeBulkEsimSwitch,
			"processed_items": processed,
			"failed_items":    failed,
			"total_items":     total,
			"progress_pct":    float64(processed+failed) / float64(total) * 100.0,
		})
	}
}
