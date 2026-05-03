package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// imeiDigitsRegexp matches a string consisting solely of ASCII digits.
var imeiDigitsRegexp = regexp.MustCompile(`^[0-9]+$`)

// imeiPoolAdder is the minimal interface BulkIMEIPoolImportProcessor needs from
// the store. Using an interface rather than a concrete *store.IMEIPoolStore allows
// pure in-process unit tests (no Postgres required) for all validation paths.
type imeiPoolAdder interface {
	Add(ctx context.Context, tenantID uuid.UUID, pool store.PoolKind, p store.AddEntryParams) (*store.PoolEntry, error)
}

// BulkIMEIPoolImportProcessor handles JobTypeBulkIMEIPoolImport jobs.
// Each row in the payload represents one IMEI pool entry to insert.
type BulkIMEIPoolImportProcessor struct {
	jobs     *store.JobStore
	pool     imeiPoolAdder
	eventBus *bus.EventBus
	auditor  audit.Auditor
	logger   zerolog.Logger
}

// NewBulkIMEIPoolImportProcessor constructs a BulkIMEIPoolImportProcessor.
func NewBulkIMEIPoolImportProcessor(
	jobs *store.JobStore,
	pool *store.IMEIPoolStore,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *BulkIMEIPoolImportProcessor {
	return &BulkIMEIPoolImportProcessor{
		jobs:     jobs,
		pool:     pool,
		eventBus: eventBus,
		logger:   logger.With().Str("processor", JobTypeBulkIMEIPoolImport).Logger(),
	}
}

// SetAuditor wires an optional audit.Auditor. Processor degrades gracefully
// (no audit entries) when the auditor is nil (e.g. in unit tests).
func (p *BulkIMEIPoolImportProcessor) SetAuditor(a audit.Auditor) {
	p.auditor = a
}

func (p *BulkIMEIPoolImportProcessor) Type() string {
	return JobTypeBulkIMEIPoolImport
}

func (p *BulkIMEIPoolImportProcessor) Process(ctx context.Context, j *store.Job) error {
	var payload BulkIMEIPoolImportPayload
	if err := json.Unmarshal(j.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal bulk imei pool import payload: %w", err)
	}

	poolKind := store.PoolKind(payload.Pool)
	total := len(payload.Rows)
	if total == 0 {
		result, _ := json.Marshal(BulkIMEIPoolImportResult{Total: 0, Rows: []IMEIPoolImportRowResult{}})
		return p.jobs.Complete(ctx, j.ID, nil, result)
	}

	_ = p.jobs.UpdateProgress(ctx, j.ID, 0, 0, total)

	var (
		successCount int
		failedCount  int
		rowResults   []IMEIPoolImportRowResult
	)

	for i, row := range payload.Rows {
		if (i+1)%bulkBatchSize == 0 {
			cancelled, checkErr := p.jobs.CheckCancelled(ctx, j.ID)
			if checkErr == nil && cancelled {
				p.logger.Info().Int("index", i).Msg("job cancelled, stopping")
				break
			}
		}

		outcome, msg := p.processRow(ctx, j, poolKind, row)
		if outcome == "success" {
			successCount++
		} else {
			failedCount++
		}
		rowResults = append(rowResults, IMEIPoolImportRowResult{
			RowNumber: i + 1,
			Outcome:   outcome,
			Message:   msg,
		})

		if (i+1)%bulkBatchSize == 0 || i == total-1 {
			_ = p.jobs.UpdateProgress(ctx, j.ID, successCount, failedCount, total)
			_ = p.eventBus.Publish(ctx, bus.SubjectJobProgress, map[string]interface{}{
				"job_id":          j.ID.String(),
				"tenant_id":       j.TenantID.String(),
				"type":            JobTypeBulkIMEIPoolImport,
				"processed_items": successCount,
				"failed_items":    failedCount,
				"total_items":     total,
				"progress_pct":    float64(successCount+failedCount) / float64(total) * 100.0,
			})
		}
	}

	return p.completeJob(ctx, j, poolKind, successCount, failedCount, total, rowResults)
}

// processRow validates and inserts a single CSV row. Returns outcome code and
// an optional human-readable message.
func (p *BulkIMEIPoolImportProcessor) processRow(
	ctx context.Context,
	j *store.Job,
	poolKind store.PoolKind,
	row IMEIPoolImportRowSpec,
) (outcome, msg string) {
	// CSV injection guard runs first on all operator-supplied string fields (plan §Risks R4).
	for _, field := range []string{row.IMEIOrTAC, row.DeviceModel, row.Description, row.QuarantineReason, row.BlockReason, row.ImportedFrom} {
		if hasCSVInjectionPrefix(field) {
			return "invalid_csv_injection", fmt.Sprintf("field value starts with formula-injection character: %q", field[:1])
		}
	}

	// kind validation before length check (kind determines the expected length).
	if !store.IsValidEntryKind(row.Kind) {
		return "invalid_kind", fmt.Sprintf("kind %q is not valid (expected full_imei or tac_range)", row.Kind)
	}

	// IMEI/TAC length and digit validation.
	trimmed := strings.TrimSpace(row.IMEIOrTAC)
	switch row.Kind {
	case string(store.EntryKindFullIMEI):
		if len(trimmed) != 15 || !imeiDigitsRegexp.MatchString(trimmed) {
			return "invalid_imei_length", fmt.Sprintf("full_imei must be exactly 15 digits, got %q", trimmed)
		}
	case string(store.EntryKindTACRange):
		if len(trimmed) != 8 || !imeiDigitsRegexp.MatchString(trimmed) {
			return "invalid_imei_length", fmt.Sprintf("tac_range must be exactly 8 digits, got %q", trimmed)
		}
	}

	// Pool-specific required field validation.
	switch poolKind {
	case store.PoolGreylist:
		if strings.TrimSpace(row.QuarantineReason) == "" {
			return "missing_quarantine_reason", "greylist entry requires quarantine_reason"
		}
	case store.PoolBlacklist:
		if strings.TrimSpace(row.BlockReason) == "" {
			return "missing_block_reason", "blacklist entry requires block_reason"
		}
		if strings.TrimSpace(row.ImportedFrom) == "" {
			return "missing_imported_from", "blacklist entry requires imported_from"
		}
		if !store.IsValidImportedFrom(row.ImportedFrom) {
			return "invalid_imported_from", fmt.Sprintf("imported_from %q is not valid (expected manual, gsma_ceir, or operator_eir)", row.ImportedFrom)
		}
	}

	params := store.AddEntryParams{
		Kind:      row.Kind,
		IMEIOrTAC: trimmed,
	}
	if row.DeviceModel != "" {
		params.DeviceModel = &row.DeviceModel
	}
	if row.Description != "" {
		params.Description = &row.Description
	}
	if row.QuarantineReason != "" {
		params.QuarantineReason = &row.QuarantineReason
	}
	if row.BlockReason != "" {
		params.BlockReason = &row.BlockReason
	}
	if row.ImportedFrom != "" {
		params.ImportedFrom = &row.ImportedFrom
	}
	if j.CreatedBy != nil {
		params.CreatedBy = j.CreatedBy
	}

	_, err := p.pool.Add(ctx, j.TenantID, poolKind, params)
	if err != nil {
		if errors.Is(err, store.ErrPoolEntryDuplicate) {
			return "imei_pool_duplicate", fmt.Sprintf("entry %q already exists in pool", trimmed)
		}
		return "store_error", fmt.Sprintf("add pool entry: %v", err)
	}

	return "success", ""
}

func (p *BulkIMEIPoolImportProcessor) completeJob(
	ctx context.Context,
	j *store.Job,
	poolKind store.PoolKind,
	successCount, failedCount, total int,
	rowResults []IMEIPoolImportRowResult,
) error {
	_ = p.jobs.UpdateProgress(ctx, j.ID, successCount, failedCount, total)

	if rowResults == nil {
		rowResults = []IMEIPoolImportRowResult{}
	}

	resultJSON, _ := json.Marshal(BulkIMEIPoolImportResult{
		Total:        total,
		SuccessCount: successCount,
		FailedCount:  failedCount,
		Rows:         rowResults,
	})

	if err := p.jobs.Complete(ctx, j.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}

	// AC-13: emit a single audit event per completed job (not per row).
	if p.auditor != nil {
		jobID := j.ID
		afterPayload, _ := json.Marshal(map[string]interface{}{
			"pool":          string(poolKind),
			"total":         total,
			"success_count": successCount,
			"failed_count":  failedCount,
		})
		_, auditErr := p.auditor.CreateEntry(ctx, audit.CreateEntryParams{
			TenantID:      j.TenantID,
			UserID:        j.CreatedBy,
			Action:        "imei_pool.bulk_imported",
			EntityType:    "imei_pool",
			EntityID:      string(poolKind),
			AfterData:     json.RawMessage(afterPayload),
			CorrelationID: &jobID,
		})
		if auditErr != nil {
			p.logger.Warn().Err(auditErr).Str("job_id", j.ID.String()).Msg("failed to emit imei_pool.bulk_imported audit entry")
		}
	}

	subject, env := buildBulkJobEvent(JobTypeBulkIMEIPoolImport, j.ID.String(), j.TenantID.String(), successCount, failedCount, total)
	if err := p.eventBus.Publish(ctx, subject, env); err != nil {
		p.logger.Warn().Err(err).Str("bulk_job_id", j.ID.String()).Msg("failed to publish bulk_job event")
	}

	return nil
}

// hasCSVInjectionPrefix returns true if s starts with a character that some
// spreadsheet apps interpret as a formula. Per STORY-095 plan §Risks R4 we
// REJECT (not prefix-quote) such rows so the operator sees a clear error.
func hasCSVInjectionPrefix(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t':
		return true
	}
	return false
}
