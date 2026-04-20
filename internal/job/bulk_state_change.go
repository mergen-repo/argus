package job

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	bulkBatchSize = 100
	lockTTL       = 30 * time.Second
)

// simForStateChange is a local normalization of both the segment-resolution
// path (store.SIMBulkInfo) and the sim_ids path (store.SIMSummary). The state
// change processor only needs ID/ICCID/State so we strip the rest.
type simForStateChange struct {
	ID    uuid.UUID
	ICCID string
	State string
}

type BulkStateChangeProcessor struct {
	jobs         *store.JobStore
	sims         *store.SIMStore
	segments     *store.SegmentStore
	readSegments *store.SegmentStore
	distLock     *DistributedLock
	eventBus     *bus.EventBus
	auditor      audit.Auditor
	logger       zerolog.Logger
}

func NewBulkStateChangeProcessor(
	jobs *store.JobStore,
	sims *store.SIMStore,
	segments *store.SegmentStore,
	readSegments *store.SegmentStore,
	distLock *DistributedLock,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *BulkStateChangeProcessor {
	return &BulkStateChangeProcessor{
		jobs:         jobs,
		sims:         sims,
		segments:     segments,
		readSegments: readSegments,
		distLock:     distLock,
		eventBus:     eventBus,
		logger:       logger.With().Str("processor", JobTypeBulkStateChange).Logger(),
	}
}

// SetAuditor wires an audit.Auditor after construction. Mirrors the optional
// dependency pattern used by BulkPolicyAssignProcessor.SetCoADispatcher so the
// processor degrades gracefully when the auditor is not injected (e.g. tests).
func (p *BulkStateChangeProcessor) SetAuditor(a audit.Auditor) {
	p.auditor = a
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

// resolveSIMs fans out to either the sim_ids batch fetch (explicit list path)
// or the segment resolution path. Both return a normalized slice so the main
// loop stays branch-free. The sim_ids path MUST pre-filter via
// sims.GetSIMsByIDs (which is tenant-scoped) to keep 10K-SIM jobs from doing
// per-SIM round trips inside the loop. If both SimIDs and SegmentID are set
// in the payload the sim_ids path wins (defensive precedence — the enqueue
// validator is expected to enforce "exactly one of", but the processor is
// safe either way).
func (p *BulkStateChangeProcessor) resolveSIMs(ctx context.Context, j *store.Job, payload BulkStateChangePayload) ([]simForStateChange, error) {
	if len(payload.SimIDs) > 0 {
		rows, err := p.sims.GetSIMsByIDs(ctx, j.TenantID, payload.SimIDs)
		if err != nil {
			return nil, fmt.Errorf("get sims by ids: %w", err)
		}
		out := make([]simForStateChange, 0, len(rows))
		for _, r := range rows {
			out = append(out, simForStateChange{ID: r.ID, ICCID: r.ICCID, State: r.State})
		}
		return out, nil
	}

	details, err := p.readSegments.ListMatchingSIMIDsWithDetails(ctx, payload.SegmentID)
	if err != nil {
		return nil, fmt.Errorf("list segment sims: %w", err)
	}
	out := make([]simForStateChange, 0, len(details))
	for _, d := range details {
		out = append(out, simForStateChange{ID: d.ID, ICCID: d.ICCID, State: d.State})
	}
	return out, nil
}

func (p *BulkStateChangeProcessor) processForward(ctx context.Context, j *store.Job, payload BulkStateChangePayload) error {
	sims, err := p.resolveSIMs(ctx, j, payload)
	if err != nil {
		return err
	}

	total := len(sims)
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

	for i, sim := range sims {
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

		// AC-8: per-SIM audit with bulk_job_id stored in correlation_id.
		// Only emitted on successful TransitionState — failed transitions are
		// recorded via error_report, not audit.
		p.emitStateChangeAudit(ctx, j, sim.ID, previousState, payload.TargetState, payload.Reason)

		undoRecords = append(undoRecords, StateUndoRecord{
			SimID:         sim.ID,
			PreviousState: previousState,
		})
		processed++
		p.publishProgress(ctx, j, processed, failed, total, i)
	}

	return p.completeJob(ctx, j, processed, failed, total, errors, undoRecords)
}

// emitStateChangeAudit records a sim.state_change entry with the bulk job ID
// stored in correlation_id (groups all per-SIM entries by bulk run).
// Degrades gracefully: nil auditor is a no-op; write failures are logged but
// never propagated — the state transition already succeeded and a failed
// audit write should not block legitimate mutations.
func (p *BulkStateChangeProcessor) emitStateChangeAudit(
	ctx context.Context,
	j *store.Job,
	simID uuid.UUID,
	previousState, targetState string,
	reason *string,
) {
	if p.auditor == nil {
		return
	}

	beforeJSON, beforeErr := json.Marshal(map[string]any{"state": previousState})
	if beforeErr != nil {
		p.logger.Warn().Err(beforeErr).Str("sim_id", simID.String()).Msg("marshal audit before state failed")
		return
	}

	after := map[string]any{"state": targetState}
	if reason != nil && *reason != "" {
		after["reason"] = *reason
	}
	afterJSON, afterErr := json.Marshal(after)
	if afterErr != nil {
		p.logger.Warn().Err(afterErr).Str("sim_id", simID.String()).Msg("marshal audit after state failed")
		return
	}

	jobID := j.ID
	_, auditErr := p.auditor.CreateEntry(ctx, audit.CreateEntryParams{
		TenantID:      j.TenantID,
		UserID:        j.CreatedBy,
		Action:        "sim.state_change",
		EntityType:    "sim",
		EntityID:      simID.String(),
		BeforeData:    beforeJSON,
		AfterData:     afterJSON,
		CorrelationID: &jobID,
	})
	if auditErr != nil {
		p.logger.Warn().
			Err(auditErr).
			Str("sim_id", simID.String()).
			Str("job_id", j.ID.String()).
			Msg("audit write failed for bulk state change — continuing")
	}
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
