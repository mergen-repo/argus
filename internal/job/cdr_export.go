package job

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type CDRExportProcessor struct {
	jobs         *store.JobStore
	cdrStore     *store.CDRStore
	readCDRStore *store.CDRStore
	eventBus     *bus.EventBus
	logger       zerolog.Logger
}

func NewCDRExportProcessor(
	jobs *store.JobStore,
	cdrStore *store.CDRStore,
	readCDRStore *store.CDRStore,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *CDRExportProcessor {
	return &CDRExportProcessor{
		jobs:         jobs,
		cdrStore:     cdrStore,
		readCDRStore: readCDRStore,
		eventBus:     eventBus,
		logger:       logger.With().Str("processor", JobTypeCDRExport).Logger(),
	}
}

func (p *CDRExportProcessor) Type() string {
	return JobTypeCDRExport
}

type cdrExportPayload struct {
	From       string   `json:"from"`
	To         string   `json:"to"`
	OperatorID *string  `json:"operator_id,omitempty"`
	SimID      *string  `json:"sim_id,omitempty"`
	APNID      *string  `json:"apn_id,omitempty"`
	SessionID  *string  `json:"session_id,omitempty"`
	RecordType *string  `json:"record_type,omitempty"`
	RATType    *string  `json:"rat_type,omitempty"`
	MinCost    *float64 `json:"min_cost,omitempty"`
	Format     string   `json:"format"`
}

func parseUUIDPtr(s *string) *uuid.UUID {
	if s == nil || *s == "" {
		return nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return nil
	}
	return &id
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (p *CDRExportProcessor) Process(ctx context.Context, job *store.Job) error {
	var payload cdrExportPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal cdr export payload: %w", err)
	}

	fromTime, err := time.Parse(time.RFC3339, payload.From)
	if err != nil {
		return fmt.Errorf("parse from time: %w", err)
	}
	toTime, err := time.Parse(time.RFC3339, payload.To)
	if err != nil {
		return fmt.Errorf("parse to time: %w", err)
	}

	listParams := store.ListCDRParams{
		From:       &fromTime,
		To:         &toTime,
		OperatorID: parseUUIDPtr(payload.OperatorID),
		SimID:      parseUUIDPtr(payload.SimID),
		APNID:      parseUUIDPtr(payload.APNID),
		SessionID:  parseUUIDPtr(payload.SessionID),
		RecordType: derefString(payload.RecordType),
		RATType:    derefString(payload.RATType),
		MinCost:    payload.MinCost,
	}

	count, err := p.readCDRStore.CountForExport(ctx, job.TenantID, fromTime, toTime, listParams.OperatorID)
	if err != nil {
		return fmt.Errorf("count cdrs for export: %w", err)
	}

	_ = p.jobs.UpdateProgress(ctx, job.ID, 0, 0, int(count))

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	header := []string{
		"id", "session_id", "sim_id", "operator_id", "apn_id", "rat_type",
		"record_type", "bytes_in", "bytes_out", "duration_sec",
		"usage_cost", "carrier_cost", "rate_per_mb", "rat_multiplier", "timestamp",
	}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}

	processed := 0
	err = p.readCDRStore.StreamForExportFiltered(ctx, job.TenantID, listParams, func(c store.CDR) error {
		row := []string{
			fmt.Sprintf("%d", c.ID),
			c.SessionID.String(),
			c.SimID.String(),
			c.OperatorID.String(),
			uuidPtrString(c.APNID),
			stringPtrValue(c.RATType),
			c.RecordType,
			fmt.Sprintf("%d", c.BytesIn),
			fmt.Sprintf("%d", c.BytesOut),
			fmt.Sprintf("%d", c.DurationSec),
			floatPtrString(c.UsageCost),
			floatPtrString(c.CarrierCost),
			floatPtrString(c.RatePerMB),
			floatPtrString(c.RATMultiplier),
			c.Timestamp.Format(time.RFC3339),
		}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("write csv row: %w", err)
		}
		processed++
		if processed%1000 == 0 {
			_ = p.jobs.UpdateProgress(ctx, job.ID, processed, 0, int(count))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("stream cdrs for export: %w", err)
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}

	csvBase64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	result, _ := json.Marshal(map[string]interface{}{
		"format":      "csv",
		"total_rows":  processed,
		"csv_data":    csvBase64,
		"exported_at": time.Now().UTC().Format(time.RFC3339),
	})

	return p.jobs.Complete(ctx, job.ID, nil, result)
}

func uuidPtrString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func stringPtrValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func floatPtrString(f *float64) string {
	if f == nil {
		return ""
	}
	return fmt.Sprintf("%.4f", *f)
}
