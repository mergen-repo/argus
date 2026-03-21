package job

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

type BulkPolicyAssignProcessor struct {
	jobs     *store.JobStore
	sims     *store.SIMStore
	segments *store.SegmentStore
	distLock *DistributedLock
	eventBus *bus.EventBus
	logger   zerolog.Logger
}

func NewBulkPolicyAssignProcessor(
	jobs *store.JobStore,
	sims *store.SIMStore,
	segments *store.SegmentStore,
	distLock *DistributedLock,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *BulkPolicyAssignProcessor {
	return &BulkPolicyAssignProcessor{
		jobs:     jobs,
		sims:     sims,
		segments: segments,
		distLock: distLock,
		eventBus: eventBus,
		logger:   logger.With().Str("processor", JobTypeBulkPolicyAssign).Logger(),
	}
}

func (p *BulkPolicyAssignProcessor) Type() string {
	return JobTypeBulkPolicyAssign
}

func (p *BulkPolicyAssignProcessor) Process(ctx context.Context, j *store.Job) error {
	var payload BulkPolicyAssignPayload
	if err := json.Unmarshal(j.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal policy assign payload: %w", err)
	}

	if len(payload.UndoRecords) > 0 {
		return p.processUndo(ctx, j, payload)
	}
	return p.processForward(ctx, j, payload)
}

func (p *BulkPolicyAssignProcessor) processForward(ctx context.Context, j *store.Job, payload BulkPolicyAssignPayload) error {
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
		undoRecords []PolicyUndoRecord
	)

	holderID := j.ID.String()
	policyID := payload.PolicyVersionID

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

		undoRecords = append(undoRecords, PolicyUndoRecord{
			SimID:                   sim.ID,
			PreviousPolicyVersionID: sim.PolicyVersionID,
		})

		setErr := p.sims.SetIPAndPolicy(ctx, sim.ID, nil, &policyID)
		_ = p.distLock.Release(ctx, lockKey, holderID)

		if setErr != nil {
			undoRecords = undoRecords[:len(undoRecords)-1]
			errors = append(errors, BulkOpError{
				SimID:        sim.ID.String(),
				ICCID:        sim.ICCID,
				ErrorCode:    "POLICY_ASSIGN_FAILED",
				ErrorMessage: setErr.Error(),
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}

		processed++
		p.publishProgress(ctx, j, processed, failed, total, i)
	}

	return p.completeJob(ctx, j, processed, failed, total, errors, undoRecords)
}

func (p *BulkPolicyAssignProcessor) processUndo(ctx context.Context, j *store.Job, payload BulkPolicyAssignPayload) error {
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

		setErr := p.sims.SetIPAndPolicy(ctx, rec.SimID, nil, rec.PreviousPolicyVersionID)
		_ = p.distLock.Release(ctx, lockKey, holderID)

		if setErr != nil {
			errors = append(errors, BulkOpError{
				SimID:        rec.SimID.String(),
				ErrorCode:    "UNDO_FAILED",
				ErrorMessage: setErr.Error(),
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

func (p *BulkPolicyAssignProcessor) completeJob(ctx context.Context, j *store.Job, processed, failed, total int, errors []BulkOpError, undoRecords []PolicyUndoRecord) error {
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
		"type":            JobTypeBulkPolicyAssign,
		"state":           "completed",
		"processed_count": processed,
		"failed_count":    failed,
	})

	return nil
}

func (p *BulkPolicyAssignProcessor) publishProgress(ctx context.Context, j *store.Job, processed, failed, total, idx int) {
	if (idx+1)%bulkBatchSize == 0 || idx == total-1 {
		_ = p.jobs.UpdateProgress(ctx, j.ID, processed, failed, total)
		_ = p.eventBus.Publish(ctx, bus.SubjectJobProgress, map[string]interface{}{
			"job_id":          j.ID.String(),
			"tenant_id":       j.TenantID.String(),
			"type":            JobTypeBulkPolicyAssign,
			"processed_items": processed,
			"failed_items":    failed,
			"total_items":     total,
			"progress_pct":    float64(processed+failed) / float64(total) * 100.0,
		})
	}
}
