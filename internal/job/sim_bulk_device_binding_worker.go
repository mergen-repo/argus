package job

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var bulkIMEIRegexp = regexp.MustCompile(`^[0-9]{15}$`)

// bulkBindingAuditPayload mirrors bindingAuditPayload from the device_binding_handler
// package and is used for before/after audit entries.
type bulkBindingAuditPayload struct {
	BindingMode   *string `json:"binding_mode"`
	BoundIMEI     *string `json:"bound_imei"`
	BindingStatus *string `json:"binding_status"`
}

// BulkDeviceBindingsProcessor handles JobTypeBulkDeviceBindings jobs.
// Each row in the payload is an ICCID+IMEI+mode triple from the uploaded CSV.
type BulkDeviceBindingsProcessor struct {
	jobs     *store.JobStore
	sims     *store.SIMStore
	eventBus *bus.EventBus
	auditor  audit.Auditor
	logger   zerolog.Logger
}

func NewBulkDeviceBindingsProcessor(
	jobs *store.JobStore,
	sims *store.SIMStore,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *BulkDeviceBindingsProcessor {
	return &BulkDeviceBindingsProcessor{
		jobs:     jobs,
		sims:     sims,
		eventBus: eventBus,
		logger:   logger.With().Str("processor", JobTypeBulkDeviceBindings).Logger(),
	}
}

// SetAuditor wires an optional audit.Auditor. Processor degrades gracefully
// (no audit entries) when the auditor is nil (e.g. in unit tests).
func (p *BulkDeviceBindingsProcessor) SetAuditor(a audit.Auditor) {
	p.auditor = a
}

func (p *BulkDeviceBindingsProcessor) Type() string {
	return JobTypeBulkDeviceBindings
}

func (p *BulkDeviceBindingsProcessor) Process(ctx context.Context, j *store.Job) error {
	var payload BulkDeviceBindingsPayload
	if err := json.Unmarshal(j.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal bulk device bindings payload: %w", err)
	}

	total := len(payload.Rows)
	if total == 0 {
		result, _ := json.Marshal(BulkResult{TotalCount: 0})
		return p.jobs.Complete(ctx, j.ID, nil, result)
	}

	_ = p.jobs.UpdateProgress(ctx, j.ID, 0, 0, total)

	var (
		processed int
		failed    int
		rowErrors []DeviceBindingsBulkRowResult
	)

	for i, row := range payload.Rows {
		if (i+1)%bulkBatchSize == 0 {
			cancelled, checkErr := p.jobs.CheckCancelled(ctx, j.ID)
			if checkErr == nil && cancelled {
				p.logger.Info().Int("index", i).Msg("job cancelled, stopping")
				break
			}
		}

		outcome, errMsg := p.processRow(ctx, j, row)
		if outcome == "success" {
			processed++
		} else {
			failed++
			rowErrors = append(rowErrors, DeviceBindingsBulkRowResult{
				ICCID:    row.ICCID,
				Outcome:  outcome,
				ErrorMsg: errMsg,
			})
		}

		if (i+1)%bulkBatchSize == 0 || i == total-1 {
			_ = p.jobs.UpdateProgress(ctx, j.ID, processed, failed, total)
			_ = p.eventBus.Publish(ctx, bus.SubjectJobProgress, map[string]interface{}{
				"job_id":          j.ID.String(),
				"tenant_id":       j.TenantID.String(),
				"type":            JobTypeBulkDeviceBindings,
				"processed_items": processed,
				"failed_items":    failed,
				"total_items":     total,
				"progress_pct":    float64(processed+failed) / float64(total) * 100.0,
			})
		}
	}

	return p.completeJob(ctx, j, processed, failed, total, rowErrors)
}

// processRow validates and applies a single CSV row. Returns outcome code and
// an optional human-readable error message.
func (p *BulkDeviceBindingsProcessor) processRow(ctx context.Context, j *store.Job, row DeviceBindingsBulkRowSpec) (outcome, errMsg string) {
	if !store.IsValidBindingMode(row.BindingMode) {
		return "invalid_mode", fmt.Sprintf("binding_mode %q is not valid", row.BindingMode)
	}

	if row.BoundIMEI != "" && !bulkIMEIRegexp.MatchString(row.BoundIMEI) {
		return "invalid_imei", fmt.Sprintf("bound_imei %q must be 15 digits", row.BoundIMEI)
	}

	ids, _, err := p.sims.ListIDsByFilter(ctx, j.TenantID, store.ListSIMsParams{ICCID: row.ICCID}, 1)
	if err != nil {
		return "store_error", fmt.Sprintf("lookup iccid: %v", err)
	}
	if len(ids) == 0 {
		return "unknown_iccid", fmt.Sprintf("no SIM found with ICCID %q", row.ICCID)
	}
	simID := ids[0]

	mode := row.BindingMode
	var boundIMEI *string
	if row.BoundIMEI != "" {
		boundIMEI = &row.BoundIMEI
	}

	// F-A2 (STORY-094 Gate): GetDeviceBinding runs unconditionally so we can
	// preserve existing binding_status during the bulk update. SetDeviceBinding
	// writes binding_status = $5 unconditionally, so passing nil would clobber
	// any existing 'verified'/'pending'/'mismatch' status to NULL — a latent
	// integrity bug for STORY-096+ when AAA enforcement starts populating it.
	// before is also reused by the auditor branch.
	before, _ := p.sims.GetDeviceBinding(ctx, j.TenantID, simID)
	var statusOverride *string
	if before != nil {
		statusOverride = before.BindingStatus
	}

	updated, err := p.sims.SetDeviceBinding(ctx, j.TenantID, simID, &mode, boundIMEI, statusOverride)
	if err != nil {
		return "store_error", fmt.Sprintf("set device binding: %v", err)
	}

	if p.auditor != nil {
		p.emitBindingAudit(ctx, j, simID, before, updated)
	}

	return "success", ""
}

func (p *BulkDeviceBindingsProcessor) emitBindingAudit(
	ctx context.Context,
	j *store.Job,
	simID uuid.UUID,
	before *store.DeviceBinding,
	after *store.DeviceBinding,
) {
	var beforeJSON, afterJSON json.RawMessage

	if before != nil {
		bp := bulkBindingAuditPayload{
			BindingMode:   before.BindingMode,
			BoundIMEI:     before.BoundIMEI,
			BindingStatus: before.BindingStatus,
		}
		beforeJSON, _ = json.Marshal(bp)
	}

	if after != nil {
		ap := bulkBindingAuditPayload{
			BindingMode:   after.BindingMode,
			BoundIMEI:     after.BoundIMEI,
			BindingStatus: after.BindingStatus,
		}
		afterJSON, _ = json.Marshal(ap)
	}

	jobID := j.ID
	_, auditErr := p.auditor.CreateEntry(ctx, audit.CreateEntryParams{
		TenantID:      j.TenantID,
		UserID:        j.CreatedBy,
		Action:        "sim.binding_mode_changed",
		EntityType:    "sim",
		EntityID:      simID.String(),
		BeforeData:    beforeJSON,
		AfterData:     afterJSON,
		CorrelationID: &jobID,
	})
	if auditErr != nil {
		p.logger.Warn().Err(auditErr).Str("sim_id", simID.String()).Msg("failed to emit binding audit entry")
	}
}

func (p *BulkDeviceBindingsProcessor) completeJob(
	ctx context.Context,
	j *store.Job,
	processed, failed, total int,
	rowErrors []DeviceBindingsBulkRowResult,
) error {
	_ = p.jobs.UpdateProgress(ctx, j.ID, processed, failed, total)

	var errorReportJSON json.RawMessage
	if len(rowErrors) > 0 {
		errorReportJSON, _ = json.Marshal(rowErrors)
	}

	resultJSON, _ := json.Marshal(BulkResult{
		ProcessedCount: processed,
		FailedCount:    failed,
		TotalCount:     total,
	})

	if err := p.jobs.Complete(ctx, j.ID, errorReportJSON, resultJSON); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}

	subject, env := buildBulkJobEvent(JobTypeBulkDeviceBindings, j.ID.String(), j.TenantID.String(), processed, failed, total)
	if err := p.eventBus.Publish(ctx, subject, env); err != nil {
		p.logger.Warn().Err(err).Str("bulk_job_id", j.ID.String()).Msg("failed to publish bulk_job event")
	}

	return nil
}
