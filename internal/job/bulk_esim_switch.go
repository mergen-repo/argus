package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// isStockExhausted returns true when err is or wraps store.ErrStockExhausted.
func isStockExhausted(err error) bool {
	return errors.Is(err, store.ErrStockExhausted)
}

// bulkSwitchESimProfileStore is the minimal ESimProfileStore subset needed for bulk switch (PAT-019).
type bulkSwitchESimProfileStore interface {
	GetEnabledProfileForSIM(ctx context.Context, tenantID, simID uuid.UUID) (*store.ESimProfile, error)
	List(ctx context.Context, tenantID uuid.UUID, p store.ListESimProfilesParams) ([]store.ESimProfile, string, error)
}

// bulkSwitchOTACommandStore is the minimal EsimOTACommandStore subset needed for bulk switch (PAT-019).
type bulkSwitchOTACommandStore interface {
	BatchInsert(ctx context.Context, params []store.InsertEsimOTACommandParams) (int, error)
}

// bulkSwitchStockStore is the minimal EsimProfileStockStore subset needed for bulk switch (PAT-019).
type bulkSwitchStockStore interface {
	Allocate(ctx context.Context, tenantID, operatorID uuid.UUID) (*store.EsimProfileStock, error)
}

type BulkEsimSwitchProcessor struct {
	jobs         *store.JobStore
	sims         *store.SIMStore
	segments     *store.SegmentStore
	esimStore    bulkSwitchESimProfileStore
	commandStore bulkSwitchOTACommandStore
	stockStore   bulkSwitchStockStore
	distLock     *DistributedLock
	eventBus     *bus.EventBus
	auditor      audit.Auditor
	logger       zerolog.Logger
}

func NewBulkEsimSwitchProcessor(
	jobs *store.JobStore,
	sims *store.SIMStore,
	segments *store.SegmentStore,
	esimStore bulkSwitchESimProfileStore,
	commandStore bulkSwitchOTACommandStore,
	stockStore bulkSwitchStockStore,
	distLock *DistributedLock,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *BulkEsimSwitchProcessor {
	return &BulkEsimSwitchProcessor{
		jobs:         jobs,
		sims:         sims,
		segments:     segments,
		esimStore:    esimStore,
		commandStore: commandStore,
		stockStore:   stockStore,
		distLock:     distLock,
		eventBus:     eventBus,
		logger:       logger.With().Str("processor", JobTypeBulkEsimSwitch).Logger(),
	}
}

func (p *BulkEsimSwitchProcessor) SetAuditor(a audit.Auditor) {
	p.auditor = a
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

// esimSwitchSIM is a unified view of a SIM used inside the forward loop,
// sourced from either SIMBulkInfo (segment branch) or SIMSummary (sim_ids branch).
type esimSwitchSIM struct {
	ID         uuid.UUID
	ICCID      string
	SimType    string
	OperatorID uuid.UUID
}

func (p *BulkEsimSwitchProcessor) processForward(ctx context.Context, j *store.Job, payload BulkEsimSwitchPayload) error {
	var targetSIMs []esimSwitchSIM

	if len(payload.SimIDs) > 0 {
		summaries, err := p.sims.GetSIMsByIDs(ctx, j.TenantID, payload.SimIDs)
		if err != nil {
			return fmt.Errorf("get sims by ids: %w", err)
		}
		targetSIMs = make([]esimSwitchSIM, len(summaries))
		for i, s := range summaries {
			targetSIMs[i] = esimSwitchSIM{
				ID:         s.ID,
				ICCID:      s.ICCID,
				SimType:    s.SimType,
				OperatorID: s.OperatorID,
			}
		}
	} else {
		simDetails, err := p.segments.ListMatchingSIMIDsWithDetails(ctx, payload.SegmentID)
		if err != nil {
			return fmt.Errorf("list segment sims: %w", err)
		}
		targetSIMs = make([]esimSwitchSIM, len(simDetails))
		for i, s := range simDetails {
			targetSIMs[i] = esimSwitchSIM{
				ID:         s.ID,
				ICCID:      s.ICCID,
				SimType:    s.SimType,
				OperatorID: s.OperatorID,
			}
		}
	}

	total := len(targetSIMs)
	if total == 0 {
		result, _ := json.Marshal(BulkResult{TotalCount: 0})
		return p.jobs.Complete(ctx, j.ID, nil, result)
	}

	_ = p.jobs.UpdateProgress(ctx, j.ID, 0, 0, total)

	var (
		processed   int
		failed      int
		errs        []BulkOpError
		undoRecords []EsimUndoRecord
	)

	holderID := j.ID.String()

	for i, sim := range targetSIMs {
		if (i+1)%bulkBatchSize == 0 {
			cancelled, checkErr := p.jobs.CheckCancelled(ctx, j.ID)
			if checkErr == nil && cancelled {
				p.logger.Info().Int("index", i).Msg("job cancelled, stopping")
				break
			}
		}

		if sim.SimType != "esim" {
			p.logger.Debug().Str("sim_id", sim.ID.String()).Str("sim_type", sim.SimType).Msg("skipping non-eSIM")
			errs = append(errs, BulkOpError{
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
			errs = append(errs, BulkOpError{
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
			errs = append(errs, BulkOpError{
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
			errs = append(errs, BulkOpError{
				SimID:        sim.ID.String(),
				ICCID:        sim.ICCID,
				ErrorCode:    "NO_ENABLED_PROFILE",
				ErrorMessage: "SIM has no enabled eSIM profile",
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}

		previousOperatorID := sim.OperatorID
		previousEnabledProfileID := enabledProfile.ID

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
			errs = append(errs, BulkOpError{
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

		// Allocate stock for the target operator (atomic).
		_, allocErr := p.stockStore.Allocate(ctx, j.TenantID, payload.TargetOperatorID)
		if allocErr != nil {
			_ = p.distLock.Release(ctx, lockKey, holderID)
			errCode := "STOCK_ALLOC_FAILED"
			if isStockExhausted(allocErr) {
				errCode = "STOCK_EXHAUSTED"
			}
			errs = append(errs, BulkOpError{
				SimID:        sim.ID.String(),
				ICCID:        sim.ICCID,
				ErrorCode:    errCode,
				ErrorMessage: allocErr.Error(),
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}

		// Enqueue OTA command (no synchronous Switch call).
		// EID is the eUICC identifier from the enabled profile — NOT sim.ICCID.
		// SM-SR Push routing, callback resolution and ListByEID/OTAHistory all key off this column.
		otaParams := store.InsertEsimOTACommandParams{
			TenantID:         j.TenantID,
			EID:              enabledProfile.EID,
			ProfileID:        &enabledProfile.ID,
			CommandType:      "switch",
			TargetOperatorID: &payload.TargetOperatorID,
			SourceProfileID:  &enabledProfile.ID,
			TargetProfileID:  &targetProfile.ID,
			JobID:            &j.ID,
		}

		// Single-row batch insert per SIM. The BatchInsert call is inside a transaction
		// in the store layer; future optimisation can collect a page-level slice and
		// call BatchInsert once — but correctness is maintained either way (no N+1 reads).
		if _, insertErr := p.commandStore.BatchInsert(ctx, []store.InsertEsimOTACommandParams{otaParams}); insertErr != nil {
			_ = p.distLock.Release(ctx, lockKey, holderID)
			errs = append(errs, BulkOpError{
				SimID:        sim.ID.String(),
				ICCID:        sim.ICCID,
				ErrorCode:    "OTA_ENQUEUE_FAILED",
				ErrorMessage: insertErr.Error(),
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}

		_ = p.distLock.Release(ctx, lockKey, holderID)

		undoRecords = append(undoRecords, EsimUndoRecord{
			SimID:              sim.ID,
			EID:                enabledProfile.EID,
			OldProfileID:       enabledProfile.ID,
			NewProfileID:       targetProfile.ID,
			PreviousOperatorID: sim.OperatorID,
		})

		p.emitEnqueueAudit(ctx, j, sim.ID, previousOperatorID, previousEnabledProfileID, payload.TargetOperatorID, targetProfile.ID, payload.Reason)

		processed++
		p.publishProgress(ctx, j, processed, failed, total, i)
	}

	return p.completeJob(ctx, j, processed, failed, total, errs, undoRecords)
}

// emitSwitchAudit is preserved as a thin alias over emitEnqueueAudit for tests
// asserting the audit row content (action="bulk.ota_enqueue").
func (p *BulkEsimSwitchProcessor) emitSwitchAudit(
	ctx context.Context,
	j *store.Job,
	simID uuid.UUID,
	previousOperatorID uuid.UUID,
	previousProfileID uuid.UUID,
	targetOperatorID uuid.UUID,
	targetProfileID uuid.UUID,
	reason string,
) {
	p.emitEnqueueAudit(ctx, j, simID, previousOperatorID, previousProfileID, targetOperatorID, targetProfileID, reason)
}

func (p *BulkEsimSwitchProcessor) emitEnqueueAudit(
	ctx context.Context,
	j *store.Job,
	simID uuid.UUID,
	previousOperatorID uuid.UUID,
	previousProfileID uuid.UUID,
	targetOperatorID uuid.UUID,
	targetProfileID uuid.UUID,
	reason string,
) {
	if p.auditor == nil {
		return
	}

	beforeData, _ := json.Marshal(map[string]any{
		"operator_id": previousOperatorID.String(),
		"profile_id":  previousProfileID.String(),
	})

	afterMap := map[string]any{
		"operator_id": targetOperatorID.String(),
		"profile_id":  targetProfileID.String(),
		"mode":        "ota_enqueue",
	}
	if reason != "" {
		afterMap["reason"] = reason
	}
	afterData, _ := json.Marshal(afterMap)

	corrID := j.ID
	_, auditErr := p.auditor.CreateEntry(ctx, audit.CreateEntryParams{
		TenantID:      j.TenantID,
		UserID:        j.CreatedBy,
		Action:        "bulk.ota_enqueue",
		EntityType:    "sim",
		EntityID:      simID.String(),
		BeforeData:    beforeData,
		AfterData:     afterData,
		CorrelationID: &corrID,
	})
	if auditErr != nil {
		p.logger.Warn().Err(auditErr).
			Str("sim_id", simID.String()).
			Str("job_id", j.ID.String()).
			Msg("audit write failed for bulk esim switch ota_enqueue — continuing")
	}
}

func (p *BulkEsimSwitchProcessor) processUndo(ctx context.Context, j *store.Job, payload BulkEsimSwitchPayload) error {
	total := len(payload.UndoRecords)
	_ = p.jobs.UpdateProgress(ctx, j.ID, 0, 0, total)

	var (
		processed int
		failed    int
		undoErrs  []BulkOpError
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
			undoErrs = append(undoErrs, BulkOpError{
				SimID:        rec.SimID.String(),
				ErrorCode:    "LOCK_FAILED",
				ErrorMessage: "could not acquire distributed lock for SIM",
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}

		// Undo enqueues a reverse OTA command (switch from new → old profile).
		// EID is the eUICC identifier carried over from the forward record — NOT the SIM UUID.
		undoParams := store.InsertEsimOTACommandParams{
			TenantID:         j.TenantID,
			EID:              rec.EID,
			CommandType:      "switch",
			TargetOperatorID: &rec.PreviousOperatorID,
			SourceProfileID:  &rec.NewProfileID,
			TargetProfileID:  &rec.OldProfileID,
			JobID:            &j.ID,
		}
		_, insertErr := p.commandStore.BatchInsert(ctx, []store.InsertEsimOTACommandParams{undoParams})
		_ = p.distLock.Release(ctx, lockKey, holderID)

		if insertErr != nil {
			undoErrs = append(undoErrs, BulkOpError{
				SimID:        rec.SimID.String(),
				ErrorCode:    "UNDO_ENQUEUE_FAILED",
				ErrorMessage: insertErr.Error(),
			})
			failed++
			p.publishProgress(ctx, j, processed, failed, total, i)
			continue
		}

		processed++
		p.publishProgress(ctx, j, processed, failed, total, i)
	}

	return p.completeJob(ctx, j, processed, failed, total, undoErrs, nil)
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

	subject, env := buildBulkJobEvent(JobTypeBulkEsimSwitch, j.ID.String(), j.TenantID.String(), processed, failed, total)
	if err := p.eventBus.Publish(ctx, subject, env); err != nil {
		p.logger.Warn().Err(err).Str("bulk_job_id", j.ID.String()).Msg("failed to publish bulk_job event")
	}

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
