package reports

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// --- mock implementations ---

type mockScheduledReportStore struct {
	createFn  func(ctx context.Context, tenantID uuid.UUID, createdBy *uuid.UUID, reportType, scheduleCron, format string, recipients []string, filters json.RawMessage, nextRunAt time.Time) (*store.ScheduledReport, error)
	getByIDFn func(ctx context.Context, id uuid.UUID) (*store.ScheduledReport, error)
	listFn    func(ctx context.Context, tenantID uuid.UUID, cursor string, limit int) ([]*store.ScheduledReport, string, error)
	updateFn  func(ctx context.Context, id uuid.UUID, patch store.ScheduledReportPatch) error
	deleteFn  func(ctx context.Context, id uuid.UUID) error
}

func (m *mockScheduledReportStore) Create(ctx context.Context, tenantID uuid.UUID, createdBy *uuid.UUID, reportType, scheduleCron, format string, recipients []string, filters json.RawMessage, nextRunAt time.Time) (*store.ScheduledReport, error) {
	return m.createFn(ctx, tenantID, createdBy, reportType, scheduleCron, format, recipients, filters, nextRunAt)
}
func (m *mockScheduledReportStore) GetByID(ctx context.Context, id uuid.UUID) (*store.ScheduledReport, error) {
	return m.getByIDFn(ctx, id)
}
func (m *mockScheduledReportStore) List(ctx context.Context, tenantID uuid.UUID, cursor string, limit int) ([]*store.ScheduledReport, string, error) {
	return m.listFn(ctx, tenantID, cursor, limit)
}
func (m *mockScheduledReportStore) Update(ctx context.Context, id uuid.UUID, patch store.ScheduledReportPatch) error {
	return m.updateFn(ctx, id, patch)
}
func (m *mockScheduledReportStore) Delete(ctx context.Context, id uuid.UUID) error {
	return m.deleteFn(ctx, id)
}

type mockJobEnqueuer struct {
	called  bool
	payload store.CreateJobParams
	returnJob *store.Job
	returnErr error
}

func (m *mockJobEnqueuer) CreateWithTenantID(_ context.Context, _ uuid.UUID, p store.CreateJobParams) (*store.Job, error) {
	m.called = true
	m.payload = p
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	if m.returnJob != nil {
		return m.returnJob, nil
	}
	return &store.Job{ID: uuid.New(), TenantID: uuid.New()}, nil
}

type mockEventPublisher struct {
	called bool
}

func (m *mockEventPublisher) Publish(_ context.Context, _ string, _ interface{}) error {
	m.called = true
	return nil
}

// --- helpers ---

func withCtx(r *http.Request, tenantID, userID uuid.UUID) *http.Request {
	ctx := context.WithValue(r.Context(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, userID)
	return r.WithContext(ctx)
}

func newHandler(s ScheduledReportStore, j JobEnqueuer, eb EventPublisher) *Handler {
	return NewHandler(s, j, eb, nil, zerolog.Nop())
}

// --- Generate tests ---

func TestGenerate_InvalidReportType(t *testing.T) {
	h := newHandler(nil, nil, nil)
	tid := uuid.New()

	body, _ := json.Marshal(map[string]string{"report_type": "bad_type", "format": "csv"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/generate", bytes.NewReader(body))
	req = withCtx(req, tid, uuid.New())
	w := httptest.NewRecorder()

	h.Generate(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestGenerate_InvalidFormat(t *testing.T) {
	h := newHandler(nil, nil, nil)
	tid := uuid.New()

	body, _ := json.Marshal(map[string]string{"report_type": "usage_summary", "format": "docx"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/generate", bytes.NewReader(body))
	req = withCtx(req, tid, uuid.New())
	w := httptest.NewRecorder()

	h.Generate(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestGenerate_ValidAsync_Returns202(t *testing.T) {
	jobID := uuid.New()
	tenantID := uuid.New()

	enqueuer := &mockJobEnqueuer{
		returnJob: &store.Job{ID: jobID, TenantID: tenantID},
	}
	pub := &mockEventPublisher{}
	h := newHandler(nil, enqueuer, pub)

	body, _ := json.Marshal(map[string]any{
		"report_type": "usage_summary",
		"format":      "csv",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/generate", bytes.NewReader(body))
	req = withCtx(req, tenantID, uuid.New())
	w := httptest.NewRecorder()

	h.Generate(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	if !enqueuer.called {
		t.Error("expected job enqueuer to be called")
	}
	if enqueuer.payload.Type != "scheduled_report_run" {
		t.Errorf("job type = %q, want %q", enqueuer.payload.Type, "scheduled_report_run")
	}
	if !pub.called {
		t.Error("expected event publisher to be called")
	}

	var resp apierr.SuccessResponse
	json.NewDecoder(w.Body).Decode(&resp)
	data, _ := json.Marshal(resp.Data)
	var result map[string]string
	json.Unmarshal(data, &result)
	if result["job_id"] != jobID.String() {
		t.Errorf("job_id = %q, want %q", result["job_id"], jobID.String())
	}
	if result["status"] != "queued" {
		t.Errorf("status = %q, want %q", result["status"], "queued")
	}
}

func TestGenerate_NoTenant_Returns401(t *testing.T) {
	h := newHandler(nil, nil, nil)

	body, _ := json.Marshal(map[string]string{"report_type": "usage_summary", "format": "csv"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/generate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Generate(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// --- CreateScheduled tests ---

func TestCreateScheduled_Valid_Returns201(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()
	reportID := uuid.New()
	now := time.Now().UTC()

	s := &mockScheduledReportStore{
		createFn: func(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _, _, _ string, _ []string, _ json.RawMessage, _ time.Time) (*store.ScheduledReport, error) {
			return &store.ScheduledReport{
				ID:           reportID,
				TenantID:     tenantID,
				ReportType:   "usage_summary",
				ScheduleCron: "@daily",
				Format:       "csv",
				Recipients:   []string{"admin@example.com"},
				State:        "active",
				CreatedAt:    now,
				UpdatedAt:    now,
			}, nil
		},
	}
	h := newHandler(s, nil, nil)

	body, _ := json.Marshal(scheduledReportCreateRequest{
		ReportType:   "usage_summary",
		ScheduleCron: "@daily",
		Format:       "csv",
		Recipients:   []string{"admin@example.com"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/scheduled", bytes.NewReader(body))
	req = withCtx(req, tenantID, userID)
	w := httptest.NewRecorder()

	h.CreateScheduled(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestCreateScheduled_InvalidCron_Returns422(t *testing.T) {
	h := newHandler(nil, nil, nil)
	tid := uuid.New()

	body, _ := json.Marshal(scheduledReportCreateRequest{
		ReportType:   "usage_summary",
		ScheduleCron: "not a cron",
		Format:       "csv",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/scheduled", bytes.NewReader(body))
	req = withCtx(req, tid, uuid.New())
	w := httptest.NewRecorder()

	h.CreateScheduled(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestCreateScheduled_NextRunComputed(t *testing.T) {
	tenantID := uuid.New()
	var capturedNextRun time.Time

	s := &mockScheduledReportStore{
		createFn: func(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _, _, _ string, _ []string, _ json.RawMessage, nextRunAt time.Time) (*store.ScheduledReport, error) {
			capturedNextRun = nextRunAt
			return &store.ScheduledReport{
				ID:           uuid.New(),
				TenantID:     tenantID,
				ScheduleCron: "@hourly",
				NextRunAt:    &nextRunAt,
				State:        "active",
			}, nil
		},
	}
	h := newHandler(s, nil, nil)

	body, _ := json.Marshal(scheduledReportCreateRequest{
		ReportType:   "sim_inventory",
		ScheduleCron: "@hourly",
		Format:       "xlsx",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/scheduled", bytes.NewReader(body))
	req = withCtx(req, tenantID, uuid.New())
	w := httptest.NewRecorder()

	h.CreateScheduled(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if capturedNextRun.IsZero() {
		t.Error("next_run_at was not computed")
	}
	if capturedNextRun.Before(time.Now()) {
		t.Errorf("next_run_at %v is in the past", capturedNextRun)
	}
}

// --- ListScheduled tests ---

func TestListScheduled_Pagination(t *testing.T) {
	tenantID := uuid.New()
	cursor := uuid.New().String()

	s := &mockScheduledReportStore{
		listFn: func(_ context.Context, _ uuid.UUID, cur string, lim int) ([]*store.ScheduledReport, string, error) {
			if cur != "" {
				return []*store.ScheduledReport{}, "", nil
			}
			return []*store.ScheduledReport{
				{ID: uuid.New(), TenantID: tenantID},
				{ID: uuid.New(), TenantID: tenantID},
			}, cursor, nil
		},
	}
	h := newHandler(s, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/scheduled?limit=2", nil)
	req = withCtx(req, tenantID, uuid.New())
	w := httptest.NewRecorder()

	h.ListScheduled(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp apierr.ListResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Meta.Cursor != cursor {
		t.Errorf("next_cursor = %q, want %q", resp.Meta.Cursor, cursor)
	}
	if !resp.Meta.HasMore {
		t.Error("expected has_more = true")
	}
}

// --- PatchScheduled tests ---

func TestPatchScheduled_StateToggle(t *testing.T) {
	tenantID := uuid.New()
	reportID := uuid.New()

	row := &store.ScheduledReport{
		ID:       reportID,
		TenantID: tenantID,
		State:    "active",
	}

	s := &mockScheduledReportStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*store.ScheduledReport, error) {
			return row, nil
		},
		updateFn: func(_ context.Context, _ uuid.UUID, _ store.ScheduledReportPatch) error {
			return nil
		},
	}
	h := newHandler(s, nil, nil)

	router := chi.NewRouter()
	router.Patch("/api/v1/reports/scheduled/{id}", h.PatchScheduled)

	paused := "paused"
	body, _ := json.Marshal(scheduledReportPatchRequest{State: &paused})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/reports/scheduled/"+reportID.String(), bytes.NewReader(body))
	req = withCtx(req, tenantID, uuid.New())
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestPatchScheduled_CronChange_RecomputesNextRun(t *testing.T) {
	tenantID := uuid.New()
	reportID := uuid.New()

	var capturedPatch store.ScheduledReportPatch
	row := &store.ScheduledReport{
		ID:       reportID,
		TenantID: tenantID,
		State:    "active",
	}

	s := &mockScheduledReportStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*store.ScheduledReport, error) {
			return row, nil
		},
		updateFn: func(_ context.Context, _ uuid.UUID, patch store.ScheduledReportPatch) error {
			capturedPatch = patch
			return nil
		},
	}
	h := newHandler(s, nil, nil)

	router := chi.NewRouter()
	router.Patch("/api/v1/reports/scheduled/{id}", h.PatchScheduled)

	newCron := "@weekly"
	body, _ := json.Marshal(scheduledReportPatchRequest{ScheduleCron: &newCron})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/reports/scheduled/"+reportID.String(), bytes.NewReader(body))
	req = withCtx(req, tenantID, uuid.New())
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if capturedPatch.NextRunAt == nil {
		t.Error("expected NextRunAt to be set when cron changes")
	}
	if capturedPatch.NextRunAt != nil && capturedPatch.NextRunAt.Before(time.Now()) {
		t.Errorf("recomputed next_run_at %v is in the past", *capturedPatch.NextRunAt)
	}
}

// --- DeleteScheduled tests ---

func TestDeleteScheduled_Returns204(t *testing.T) {
	tenantID := uuid.New()
	reportID := uuid.New()

	row := &store.ScheduledReport{ID: reportID, TenantID: tenantID}

	s := &mockScheduledReportStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*store.ScheduledReport, error) {
			return row, nil
		},
		deleteFn: func(_ context.Context, _ uuid.UUID) error {
			return nil
		},
	}
	h := newHandler(s, nil, nil)

	router := chi.NewRouter()
	router.Delete("/api/v1/reports/scheduled/{id}", h.DeleteScheduled)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/reports/scheduled/"+reportID.String(), nil)
	req = withCtx(req, tenantID, uuid.New())
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestDeleteScheduled_WrongTenant_Returns404(t *testing.T) {
	ownerTenant := uuid.New()
	callerTenant := uuid.New()
	reportID := uuid.New()

	row := &store.ScheduledReport{ID: reportID, TenantID: ownerTenant}

	s := &mockScheduledReportStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*store.ScheduledReport, error) {
			return row, nil
		},
	}
	h := newHandler(s, nil, nil)

	router := chi.NewRouter()
	router.Delete("/api/v1/reports/scheduled/{id}", h.DeleteScheduled)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/reports/scheduled/"+reportID.String(), nil)
	req = withCtx(req, callerTenant, uuid.New())
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestListDefinitions_ReturnsAll8Types(t *testing.T) {
	h := newHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/definitions", nil)
	w := httptest.NewRecorder()

	h.ListDefinitions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		Status string             `json:"status"`
		Data   []reportDefinition `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Data) != 8 {
		t.Errorf("definitions count = %d, want 8", len(resp.Data))
	}

	expectedIDs := []string{
		"compliance_btk", "compliance_kvkk", "compliance_gdpr",
		"sla_monthly", "usage_summary", "cost_analysis",
		"audit_log_export", "sim_inventory",
	}

	idSet := make(map[string]bool)
	for _, d := range resp.Data {
		if d.Name == "" {
			t.Errorf("definition %q has empty name", d.ID)
		}
		if d.Description == "" {
			t.Errorf("definition %q has empty description", d.ID)
		}
		if len(d.FormatOptions) == 0 {
			t.Errorf("definition %q has no format_options", d.ID)
		}
		idSet[d.ID] = true
	}

	for _, id := range expectedIDs {
		if !idSet[id] {
			t.Errorf("missing report definition %q", id)
		}
	}
}
