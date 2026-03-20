package job

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	jobtypes "github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestToJobDTO(t *testing.T) {
	now := time.Now()
	userID := uuid.New()
	startedAt := now.Add(-5 * time.Minute)
	completedAt := now

	job := &store.Job{
		ID:             uuid.New(),
		TenantID:       uuid.New(),
		Type:           "bulk_sim_import",
		State:          "completed",
		Priority:       5,
		Payload:        json.RawMessage(`{}`),
		TotalItems:     100,
		ProcessedItems: 95,
		FailedItems:    5,
		ProgressPct:    100.0,
		MaxRetries:     3,
		RetryCount:     0,
		StartedAt:      &startedAt,
		CompletedAt:    &completedAt,
		CreatedAt:      now.Add(-10 * time.Minute),
		CreatedBy:      &userID,
	}

	dto := toJobDTO(job)

	if dto.ID != job.ID {
		t.Errorf("ID = %v, want %v", dto.ID, job.ID)
	}
	if dto.Type != "bulk_sim_import" {
		t.Errorf("Type = %s, want bulk_sim_import", dto.Type)
	}
	if dto.State != "completed" {
		t.Errorf("State = %s, want completed", dto.State)
	}
	if dto.TotalItems != 100 {
		t.Errorf("TotalItems = %d, want 100", dto.TotalItems)
	}
	if dto.ProcessedItems != 95 {
		t.Errorf("ProcessedItems = %d, want 95", dto.ProcessedItems)
	}
	if dto.FailedItems != 5 {
		t.Errorf("FailedItems = %d, want 5", dto.FailedItems)
	}
	if dto.StartedAt == nil {
		t.Error("StartedAt should not be nil")
	}
	if dto.CompletedAt == nil {
		t.Error("CompletedAt should not be nil")
	}
	if dto.CreatedBy == nil {
		t.Error("CreatedBy should not be nil")
	}
}

func TestToJobDTONilOptionals(t *testing.T) {
	job := &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Type:     "bulk_sim_import",
		State:    "queued",
		Priority: 5,
		Payload:  json.RawMessage(`{}`),
		CreatedAt: time.Now(),
	}

	dto := toJobDTO(job)

	if dto.StartedAt != nil {
		t.Error("StartedAt should be nil for queued job")
	}
	if dto.CompletedAt != nil {
		t.Error("CompletedAt should be nil for queued job")
	}
	if dto.CreatedBy != nil {
		t.Error("CreatedBy should be nil when not set")
	}
}

func TestWriteErrorReportCSV(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	errors := []jobtypes.ImportRowError{
		{Row: 2, ICCID: "8990111234567890123", ErrorMessage: "ICCID already exists"},
		{Row: 5, ICCID: "8990111234567890456", ErrorMessage: "operator 'xyz' not found"},
		{Row: 8, ICCID: "", ErrorMessage: "ICCID is required"},
	}

	report, _ := json.Marshal(errors)

	w := httptest.NewRecorder()
	h.writeErrorReportCSV(w, report)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/csv" {
		t.Errorf("Content-Type = %q, want text/csv", contentType)
	}

	disposition := w.Header().Get("Content-Disposition")
	if disposition != "attachment; filename=error_report.csv" {
		t.Errorf("Content-Disposition = %q, want attachment; filename=error_report.csv", disposition)
	}

	reader := csv.NewReader(bytes.NewReader(w.Body.Bytes()))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("csv read error: %v", err)
	}

	if len(records) != 4 {
		t.Fatalf("csv rows = %d, want 4 (1 header + 3 data)", len(records))
	}

	header := records[0]
	if header[0] != "row" || header[1] != "iccid" || header[2] != "error" {
		t.Errorf("csv header = %v, want [row, iccid, error]", header)
	}

	if records[1][0] != "2" || records[1][1] != "8990111234567890123" {
		t.Errorf("first data row = %v, unexpected", records[1])
	}
}

func TestToJobDTOProgressPct(t *testing.T) {
	job := &store.Job{
		ID:             uuid.New(),
		TenantID:       uuid.New(),
		Type:           "bulk_sim_import",
		State:          "running",
		Priority:       5,
		Payload:        json.RawMessage(`{}`),
		TotalItems:     1000,
		ProcessedItems: 500,
		FailedItems:    50,
		ProgressPct:    55.0,
		CreatedAt:      time.Now(),
	}

	dto := toJobDTO(job)

	if dto.ProgressPct != 55.0 {
		t.Errorf("ProgressPct = %f, want 55.0", dto.ProgressPct)
	}
	if dto.TotalItems != 1000 {
		t.Errorf("TotalItems = %d, want 1000", dto.TotalItems)
	}
	if dto.ProcessedItems != 500 {
		t.Errorf("ProcessedItems = %d, want 500", dto.ProcessedItems)
	}
}

func TestToJobDTOWithErrorReport(t *testing.T) {
	errReport := json.RawMessage(`[{"row":2,"iccid":"123","error":"duplicate"}]`)
	resultData := json.RawMessage(`{"total_rows":100,"success_count":99,"failure_count":1}`)

	job := &store.Job{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		Type:        "bulk_sim_import",
		State:       "completed",
		Priority:    5,
		Payload:     json.RawMessage(`{}`),
		ErrorReport: errReport,
		Result:      resultData,
		CreatedAt:   time.Now(),
	}

	dto := toJobDTO(job)

	if dto.ErrorReport == nil {
		t.Error("ErrorReport should not be nil")
	}
	if dto.Result == nil {
		t.Error("Result should not be nil")
	}
}
