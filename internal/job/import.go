package job

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	progressInterval = 100
	maxCSVColumns    = 5
)

var requiredHeaders = []string{"iccid", "imsi", "msisdn", "operator_code", "apn_name"}

type ImportPayload struct {
	CSVData         string `json:"csv_data"`
	FileName        string `json:"file_name"`
	ReserveStaticIP bool   `json:"reserve_static_ip,omitempty"`
}

type ImportRowError struct {
	Row          int    `json:"row"`
	ICCID        string `json:"iccid,omitempty"`
	ErrorMessage string `json:"error"`
}

type ImportResult struct {
	TotalRows     int `json:"total_rows"`
	SuccessCount  int `json:"success_count"`
	FailureCount  int `json:"failure_count"`
	CreatedSIMIDs []string `json:"created_sim_ids,omitempty"`
}

type BulkImportProcessor struct {
	jobs      *store.JobStore
	sims      *store.SIMStore
	operators *store.OperatorStore
	apns      *store.APNStore
	ipPools   *store.IPPoolStore
	eventBus  *bus.EventBus
	logger    zerolog.Logger
}

func NewBulkImportProcessor(
	jobs *store.JobStore,
	sims *store.SIMStore,
	operators *store.OperatorStore,
	apns *store.APNStore,
	ipPools *store.IPPoolStore,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *BulkImportProcessor {
	return &BulkImportProcessor{
		jobs:      jobs,
		sims:      sims,
		operators: operators,
		apns:      apns,
		ipPools:   ipPools,
		eventBus:  eventBus,
		logger:    logger.With().Str("processor", JobTypeBulkImport).Logger(),
	}
}

func (p *BulkImportProcessor) Type() string {
	return JobTypeBulkImport
}

func (p *BulkImportProcessor) Process(ctx context.Context, job *store.Job) error {
	var payload ImportPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal import payload: %w", err)
	}

	tenantCtx := context.WithValue(ctx, apierr.TenantIDKey, job.TenantID)

	reader := csv.NewReader(strings.NewReader(payload.CSVData))
	reader.TrimLeadingSpace = true

	headers, err := reader.Read()
	if err != nil {
		return fmt.Errorf("read csv header: %w", err)
	}

	colMap, err := mapColumns(headers)
	if err != nil {
		return err
	}

	var rows [][]string
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read csv: %w", err)
		}
		rows = append(rows, record)
	}

	totalRows := len(rows)
	if totalRows == 0 {
		result, _ := json.Marshal(ImportResult{TotalRows: 0})
		return p.jobs.Complete(ctx, job.ID, nil, result)
	}

	_ = p.jobs.UpdateProgress(ctx, job.ID, 0, 0, totalRows)

	var rowErrors []ImportRowError
	var createdIDs []string
	processed := 0
	failed := 0

	operatorCache := make(map[string]*store.Operator)
	apnCache := make(map[string]*store.APN)

	for i, row := range rows {
		rowNum := i + 2

		if (i+1)%progressInterval == 0 {
			cancelled, checkErr := p.jobs.CheckCancelled(ctx, job.ID)
			if checkErr == nil && cancelled {
				p.logger.Info().Int("row", rowNum).Msg("job cancelled, stopping import")
				break
			}
		}

		if len(row) < maxCSVColumns {
			rowErrors = append(rowErrors, ImportRowError{
				Row:          rowNum,
				ErrorMessage: fmt.Sprintf("expected %d columns, got %d", maxCSVColumns, len(row)),
			})
			failed++
			p.updateProgressPeriodic(ctx, job, processed, failed, totalRows, i)
			continue
		}

		iccid := strings.TrimSpace(row[colMap["iccid"]])
		imsi := strings.TrimSpace(row[colMap["imsi"]])
		msisdn := strings.TrimSpace(row[colMap["msisdn"]])
		operatorCode := strings.TrimSpace(row[colMap["operator_code"]])
		apnName := strings.TrimSpace(row[colMap["apn_name"]])

		if validationErr := validateRow(iccid, imsi, operatorCode, apnName); validationErr != "" {
			rowErrors = append(rowErrors, ImportRowError{
				Row:          rowNum,
				ICCID:        iccid,
				ErrorMessage: validationErr,
			})
			failed++
			p.updateProgressPeriodic(ctx, job, processed, failed, totalRows, i)
			continue
		}

		op, err := p.resolveOperator(tenantCtx, operatorCode, operatorCache)
		if err != nil {
			rowErrors = append(rowErrors, ImportRowError{
				Row:          rowNum,
				ICCID:        iccid,
				ErrorMessage: fmt.Sprintf("operator '%s' not found", operatorCode),
			})
			failed++
			p.updateProgressPeriodic(ctx, job, processed, failed, totalRows, i)
			continue
		}

		apn, err := p.resolveAPN(tenantCtx, job.TenantID, op.ID, apnName, apnCache)
		if err != nil {
			rowErrors = append(rowErrors, ImportRowError{
				Row:          rowNum,
				ICCID:        iccid,
				ErrorMessage: fmt.Sprintf("APN '%s' not found", apnName),
			})
			failed++
			p.updateProgressPeriodic(ctx, job, processed, failed, totalRows, i)
			continue
		}

		var msisdnPtr *string
		if msisdn != "" {
			msisdnPtr = &msisdn
		}

		sim, err := p.sims.Create(tenantCtx, job.TenantID, store.CreateSIMParams{
			OperatorID: op.ID,
			APNID:      apn.ID,
			ICCID:      iccid,
			IMSI:       imsi,
			MSISDN:     msisdnPtr,
			SimType:    "physical",
		})
		if err != nil {
			errMsg := "create SIM failed"
			if errors.Is(err, store.ErrICCIDExists) {
				errMsg = fmt.Sprintf("ICCID %s already exists", iccid)
			} else if errors.Is(err, store.ErrIMSIExists) {
				errMsg = fmt.Sprintf("IMSI %s already exists", imsi)
			}
			rowErrors = append(rowErrors, ImportRowError{
				Row:          rowNum,
				ICCID:        iccid,
				ErrorMessage: errMsg,
			})
			failed++
			p.updateProgressPeriodic(ctx, job, processed, failed, totalRows, i)
			continue
		}

		ordered := "ordered"
		_ = p.sims.InsertHistory(tenantCtx, sim.ID, nil, "ordered", "bulk_import", nil, nil)

		activatedSim, activateErr := p.sims.TransitionState(tenantCtx, sim.ID, "active", nil, "bulk_import", nil, 0)
		if activateErr != nil {
			p.logger.Warn().Err(activateErr).Str("sim_id", sim.ID.String()).Msg("auto-activate failed")
			rowErrors = append(rowErrors, ImportRowError{
				Row:          rowNum,
				ICCID:        iccid,
				ErrorMessage: fmt.Sprintf("created but auto-activation failed: %v", activateErr),
			})
			failed++
			p.updateProgressPeriodic(ctx, job, processed, failed, totalRows, i)
			continue
		}
		_ = ordered

		p.allocateIPAndPolicy(tenantCtx, activatedSim, &apn.ID, apn.DefaultPolicyID, payload.ReserveStaticIP)

		createdIDs = append(createdIDs, sim.ID.String())
		processed++
		p.updateProgressPeriodic(ctx, job, processed, failed, totalRows, i)
	}

	_ = p.jobs.UpdateProgress(ctx, job.ID, processed, failed, totalRows)

	var errorReportJSON json.RawMessage
	if len(rowErrors) > 0 {
		errorReportJSON, _ = json.Marshal(rowErrors)
	}

	resultJSON, _ := json.Marshal(ImportResult{
		TotalRows:     totalRows,
		SuccessCount:  processed,
		FailureCount:  failed,
		CreatedSIMIDs: createdIDs,
	})

	if err := p.jobs.Complete(ctx, job.ID, errorReportJSON, resultJSON); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}

	_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
		"job_id":         job.ID.String(),
		"tenant_id":      job.TenantID.String(),
		"type":           JobTypeBulkImport,
		"state":          "completed",
		"total_rows":     totalRows,
		"success_count":  processed,
		"failure_count":  failed,
	})

	return nil
}

func (p *BulkImportProcessor) updateProgressPeriodic(ctx context.Context, job *store.Job, processed, failed, total, idx int) {
	if (idx+1)%progressInterval == 0 || idx == total-1 {
		_ = p.jobs.UpdateProgress(ctx, job.ID, processed, failed, total)
		_ = p.eventBus.Publish(ctx, bus.SubjectJobProgress, map[string]interface{}{
			"job_id":          job.ID.String(),
			"tenant_id":       job.TenantID.String(),
			"type":            JobTypeBulkImport,
			"processed_items": processed,
			"failed_items":    failed,
			"total_items":     total,
			"progress_pct":    float64(processed+failed) / float64(total) * 100.0,
		})
	}
}

func (p *BulkImportProcessor) resolveOperator(ctx context.Context, code string, cache map[string]*store.Operator) (*store.Operator, error) {
	if op, ok := cache[code]; ok {
		return op, nil
	}
	op, err := p.operators.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	cache[code] = op
	return op, nil
}

func (p *BulkImportProcessor) resolveAPN(ctx context.Context, tenantID, operatorID uuid.UUID, name string, cache map[string]*store.APN) (*store.APN, error) {
	if apn, ok := cache[name]; ok {
		return apn, nil
	}
	apn, err := p.apns.GetByName(ctx, tenantID, operatorID, name)
	if err != nil {
		return nil, err
	}
	cache[name] = apn
	return apn, nil
}

func (p *BulkImportProcessor) allocateIPAndPolicy(ctx context.Context, sim *store.SIM, apnID *uuid.UUID, defaultPolicyID *uuid.UUID, reserveStatic bool) {
	if apnID == nil {
		return
	}

	pools, _, err := p.ipPools.List(ctx, sim.TenantID, "", 1, apnID)
	if err != nil || len(pools) == 0 {
		return
	}

	var result *store.IPAddress
	if reserveStatic {
		result, err = p.ipPools.ReserveStaticIP(ctx, pools[0].ID, sim.ID, nil)
	} else {
		result, err = p.ipPools.AllocateIP(ctx, pools[0].ID, sim.ID)
	}
	if err != nil {
		p.logger.Warn().Err(err).Str("sim_id", sim.ID.String()).Msg("ip allocation failed during import")
		return
	}

	_ = p.sims.SetIPAndPolicy(ctx, sim.ID, &result.ID, defaultPolicyID)
}

func mapColumns(headers []string) (map[string]int, error) {
	normalized := make([]string, len(headers))
	for i, h := range headers {
		normalized[i] = strings.ToLower(strings.TrimSpace(h))
	}

	colMap := make(map[string]int)
	for _, required := range requiredHeaders {
		found := false
		for i, h := range normalized {
			if h == required {
				colMap[required] = i
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("missing required CSV column: %s", required)
		}
	}
	return colMap, nil
}

func validateRow(iccid, imsi, operatorCode, apnName string) string {
	var errs []string

	if iccid == "" {
		errs = append(errs, "ICCID is required")
	} else if len(iccid) < 19 || len(iccid) > 22 {
		errs = append(errs, "ICCID must be 19-22 characters")
	}

	if imsi == "" {
		errs = append(errs, "IMSI is required")
	} else if len(imsi) != 15 {
		errs = append(errs, "IMSI must be 15 digits")
	}

	if operatorCode == "" {
		errs = append(errs, "operator_code is required")
	}

	if apnName == "" {
		errs = append(errs, "apn_name is required")
	}

	if len(errs) > 0 {
		return strings.Join(errs, "; ")
	}
	return ""
}
