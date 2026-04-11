package job

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	BulkCoAStatusAcked  = "acked"
	BulkCoAStatusFailed = "failed"
)

type BulkSessionInfo struct {
	ID            string
	SimID         string
	NASIP         string
	AcctSessionID string
	IMSI          string
}

type BulkCoARequest struct {
	NASIP         string
	AcctSessionID string
	IMSI          string
	Attributes    map[string]interface{}
}

type BulkCoAResult struct {
	Status  string
	Message string
}

type BulkSessionProvider interface {
	GetSessionsForSIM(ctx context.Context, simID string) ([]BulkSessionInfo, error)
}

type BulkCoADispatcher interface {
	SendCoA(ctx context.Context, req BulkCoARequest) (*BulkCoAResult, error)
}

type BulkPolicyCoAUpdater interface {
	UpdateAssignmentCoAStatus(ctx context.Context, simID uuid.UUID, status string) error
}

type BulkPolicyAssignProcessor struct {
	jobs            *store.JobStore
	sims            *store.SIMStore
	segments        *store.SegmentStore
	distLock        *DistributedLock
	eventBus        *bus.EventBus
	sessionProvider BulkSessionProvider
	coaDispatcher   BulkCoADispatcher
	policyUpdater   BulkPolicyCoAUpdater
	logger          zerolog.Logger
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

func (p *BulkPolicyAssignProcessor) SetSessionProvider(sp BulkSessionProvider) {
	p.sessionProvider = sp
}

func (p *BulkPolicyAssignProcessor) SetCoADispatcher(cd BulkCoADispatcher) {
	p.coaDispatcher = cd
}

func (p *BulkPolicyAssignProcessor) SetPolicyCoAUpdater(u BulkPolicyCoAUpdater) {
	p.policyUpdater = u
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
		coaSent     int
		coaAcked    int
		coaFailed   int
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

		// AC-7: dispatch CoA to active sessions on the affected SIM.
		// Outside the distributed lock to avoid blocking other SIM ops during UDP I/O.
		sent, acked, failedCoA := p.dispatchCoAForSIM(ctx, sim.ID)
		coaSent += sent
		coaAcked += acked
		coaFailed += failedCoA

		processed++
		p.publishProgress(ctx, j, processed, failed, total, i)
	}

	return p.completeJob(ctx, j, processed, failed, total, coaSent, coaAcked, coaFailed, errors, undoRecords)
}

// dispatchCoAForSIM sends CoA to all active sessions on the given SIM and returns
// (sent, acked, failed) counts. Degrades gracefully if session/CoA deps are nil.
func (p *BulkPolicyAssignProcessor) dispatchCoAForSIM(ctx context.Context, simID uuid.UUID) (int, int, int) {
	if p.sessionProvider == nil || p.coaDispatcher == nil {
		return 0, 0, 0
	}

	sessions, err := p.sessionProvider.GetSessionsForSIM(ctx, simID.String())
	if err != nil {
		p.logger.Warn().Err(err).Str("sim_id", simID.String()).Msg("get sessions for CoA")
		return 0, 0, 0
	}

	var sent, acked, failedCoA int
	for _, sess := range sessions {
		sent++
		result, coaErr := p.coaDispatcher.SendCoA(ctx, BulkCoARequest{
			NASIP:         sess.NASIP,
			AcctSessionID: sess.AcctSessionID,
			IMSI:          sess.IMSI,
			Attributes:    map[string]interface{}{},
		})

		status := BulkCoAStatusFailed
		if coaErr == nil && result != nil && result.Status == "ack" {
			acked++
			status = BulkCoAStatusAcked
		} else {
			failedCoA++
			if coaErr != nil {
				p.logger.Warn().Err(coaErr).
					Str("sim_id", simID.String()).
					Str("session_id", sess.ID).
					Msg("CoA send failed")
			}
		}

		if p.policyUpdater != nil {
			if updateErr := p.policyUpdater.UpdateAssignmentCoAStatus(ctx, simID, status); updateErr != nil {
				p.logger.Warn().Err(updateErr).Str("sim_id", simID.String()).Msg("update CoA status")
			}
		}
	}
	return sent, acked, failedCoA
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

	return p.completeJob(ctx, j, processed, failed, total, 0, 0, 0, errors, nil)
}

func (p *BulkPolicyAssignProcessor) completeJob(ctx context.Context, j *store.Job, processed, failed, total, coaSent, coaAcked, coaFailed int, errors []BulkOpError, undoRecords []PolicyUndoRecord) error {
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
		CoASentCount:   coaSent,
		CoAAckedCount:  coaAcked,
		CoAFailedCount: coaFailed,
	})

	if err := p.jobs.Complete(ctx, j.ID, errorReportJSON, resultJSON); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}

	_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
		"job_id":           j.ID.String(),
		"tenant_id":        j.TenantID.String(),
		"type":             JobTypeBulkPolicyAssign,
		"state":            "completed",
		"processed_count":  processed,
		"failed_count":     failed,
		"coa_sent_count":   coaSent,
		"coa_acked_count":  coaAcked,
		"coa_failed_count": coaFailed,
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
