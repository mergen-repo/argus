package sla

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/report"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type fakeEngine struct {
	artifact *report.Artifact
	err      error
}

func (f *fakeEngine) Build(_ context.Context, _ report.Request) (*report.Artifact, error) {
	return f.artifact, f.err
}

type fakeSLAStore struct {
	rows          []store.SLAReportRow
	err           error
	historyResult []store.MonthSummary
	monthResult   *store.MonthSummary
}

func (f *fakeSLAStore) ListByTenant(_ context.Context, _ uuid.UUID, _, _ time.Time, _ *uuid.UUID, _ string, _ int) ([]store.SLAReportRow, string, error) {
	return f.rows, "", f.err
}
func (f *fakeSLAStore) GetByID(_ context.Context, _, _ uuid.UUID) (*store.SLAReportRow, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeSLAStore) HistoryByMonth(_ context.Context, _ uuid.UUID, _, _ int, _ *uuid.UUID) ([]store.MonthSummary, error) {
	if f.historyResult != nil {
		return f.historyResult, nil
	}
	return nil, errors.New("not implemented")
}
func (f *fakeSLAStore) MonthDetail(_ context.Context, _ uuid.UUID, _, _ int) (*store.MonthSummary, error) {
	if f.monthResult != nil {
		return f.monthResult, nil
	}
	return nil, errors.New("not implemented")
}
func (f *fakeSLAStore) GetByTenantOperatorMonth(_ context.Context, _, _ uuid.UUID, _, _ int) (*store.SLAReportRow, error) {
	return nil, errors.New("not implemented")
}

func withTenantCtx(r *http.Request, tenantID uuid.UUID) *http.Request {
	ctx := context.WithValue(r.Context(), apierr.TenantIDKey, tenantID)
	return r.WithContext(ctx)
}

func TestHandler_List_NoTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla-reports", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_List_InvalidFrom(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla-reports?from=not-a-date", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidFormat {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidFormat)
	}
}

func TestHandler_List_InvalidTo(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla-reports?to=bad", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_List_FromAfterTo(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla-reports?from=2026-12-31T00:00:00Z&to=2026-01-01T00:00:00Z", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_List_InvalidOperatorID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla-reports?operator_id=not-uuid", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_List_InvalidLimit(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla-reports?limit=abc", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_Get_NoTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla-reports/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_Get_InvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()

	r := chi.NewRouter()
	r.Get("/api/v1/sla-reports/{id}", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla-reports/not-a-uuid", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_History_NoTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/history", nil)
	w := httptest.NewRecorder()
	h.History(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_History_InvalidYear(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/history?year=3000", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	h.History(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidYear {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidYear)
	}
}

func TestHandler_History_InvalidYear_TooLow(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/history?year=2019", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	h.History(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidYear {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidYear)
	}
}

func TestHandler_History_InvalidMonths_Zero(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/history?months=0", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	h.History(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidMonthsRange {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidMonthsRange)
	}
}

func TestHandler_History_InvalidMonths_TooHigh(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/history?months=25", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	h.History(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidMonthsRange {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidMonthsRange)
	}
}

func TestHandler_History_InvalidOperatorID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/history?operator_id=not-a-uuid", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	h.History(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidOperatorID {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidOperatorID)
	}
}

// TestHandler_History_BodyShape is the PAT-006 regression canary introduced by
// FIX-215 Gate (F-A1). Asserts that MonthSummary JSON shape uses lowercase keys
// (year/month/overall/operators) — NOT Go-default capitalized names that would
// make the frontend render empty cards and crash on `.toFixed(3)`.
func TestHandler_History_BodyShape(t *testing.T) {
	tid := uuid.New()
	fake := &fakeSLAStore{historyResult: []store.MonthSummary{{
		Year:  2026,
		Month: 3,
		Overall: store.OperatorMonthAgg{
			UptimePct:     99.82,
			IncidentCount: 4,
			BreachMinutes: 71,
			LatencyP95Ms:  142,
			SessionsTotal: 482113,
		},
		Operators: []store.OperatorMonthAgg{},
	}}}
	h := &Handler{store: fake, logger: zerolog.Nop()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/history?months=6", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	h.History(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Status string `json:"status"`
		Data   []struct {
			Year      int   `json:"year"`
			Month     int   `json:"month"`
			Overall   any   `json:"overall"`
			Operators []any `json:"operators"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v; body=%s", err, w.Body.String())
	}
	if envelope.Status != "success" {
		t.Errorf("status=%q, want 'success'", envelope.Status)
	}
	if len(envelope.Data) != 1 {
		t.Fatalf("data length = %d, want 1", len(envelope.Data))
	}
	if envelope.Data[0].Year != 2026 || envelope.Data[0].Month != 3 {
		t.Errorf("got year=%d month=%d, want 2026/3 — JSON tags regression?",
			envelope.Data[0].Year, envelope.Data[0].Month)
	}
	if envelope.Data[0].Overall == nil {
		t.Errorf("overall is nil — JSON tag missing on Overall field")
	}
	// Guard against capitalized-key regression (Go default marshalling).
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err == nil {
		var dataArr []map[string]json.RawMessage
		if err2 := json.Unmarshal(raw["data"], &dataArr); err2 == nil && len(dataArr) == 1 {
			for _, forbidden := range []string{"Year", "Month", "Overall", "Operators"} {
				if _, exists := dataArr[0][forbidden]; exists {
					t.Errorf("MonthSummary leaked capitalized key %q — JSON tags missing (PAT-006)", forbidden)
				}
			}
		}
	}
}

func TestHandler_MonthDetail_NoTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	router := chi.NewRouter()
	router.Get("/api/v1/sla/months/{year}/{month}", h.MonthDetail)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/months/2026/4", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_MonthDetail_InvalidYear(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()
	router := chi.NewRouter()
	router.Get("/api/v1/sla/months/{year}/{month}", h.MonthDetail)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/months/2019/4", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidYear {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidYear)
	}
}

func TestHandler_MonthDetail_InvalidMonth_TooHigh(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()
	router := chi.NewRouter()
	router.Get("/api/v1/sla/months/{year}/{month}", h.MonthDetail)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/months/2026/13", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidMonth {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidMonth)
	}
}

func TestHandler_MonthDetail_InvalidMonth_Zero(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()
	router := chi.NewRouter()
	router.Get("/api/v1/sla/months/{year}/{month}", h.MonthDetail)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/months/2026/0", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidMonth {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidMonth)
	}
}

func TestHandler_OperatorMonthBreaches_NoTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	router := chi.NewRouter()
	router.Get("/api/v1/sla/operators/{operatorId}/months/{year}/{month}/breaches", h.OperatorMonthBreaches)

	opID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/sla/operators/%s/months/2026/4/breaches", opID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_OperatorMonthBreaches_InvalidOperatorID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()
	router := chi.NewRouter()
	router.Get("/api/v1/sla/operators/{operatorId}/months/{year}/{month}/breaches", h.OperatorMonthBreaches)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/operators/not-a-uuid/months/2026/4/breaches", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidOperatorID {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidOperatorID)
	}
}

func TestHandler_OperatorMonthBreaches_InvalidYear(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()
	router := chi.NewRouter()
	router.Get("/api/v1/sla/operators/{operatorId}/months/{year}/{month}/breaches", h.OperatorMonthBreaches)

	opID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/sla/operators/%s/months/2019/4/breaches", opID), nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidYear {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidYear)
	}
}

func TestHandler_OperatorMonthBreaches_InvalidMonth(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tid := uuid.New()
	router := chi.NewRouter()
	router.Get("/api/v1/sla/operators/{operatorId}/months/{year}/{month}/breaches", h.OperatorMonthBreaches)

	opID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/sla/operators/%s/months/2026/13/breaches", opID), nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidMonth {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidMonth)
	}
}

func TestHandler_ParseBreachesFromDetails_Empty(t *testing.T) {
	result := parseBreachesFromDetails(nil)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}

func TestHandler_ParseBreachesFromDetails_ValidJSON(t *testing.T) {
	raw := []byte(`{"breaches":[{"started_at":"2023-01-01T00:00:00Z","ended_at":"2023-01-01T01:00:00Z","duration_sec":3600,"cause":"down","samples_count":12}]}`)
	result := parseBreachesFromDetails(raw)
	if len(result) != 1 {
		t.Fatalf("expected 1 breach, got %d", len(result))
	}
	if result[0].Cause != "down" {
		t.Errorf("cause = %q, want %q", result[0].Cause, "down")
	}
}

func makeDownloadPDFHandler(rows []store.SLAReportRow, storeErr error, artifact *report.Artifact, engineErr error) *Handler {
	return &Handler{
		store:  &fakeSLAStore{rows: rows, err: storeErr},
		engine: &fakeEngine{artifact: artifact, err: engineErr},
		logger: zerolog.Nop(),
	}
}

func slaRow() store.SLAReportRow {
	return store.SLAReportRow{
		ID:          uuid.New(),
		WindowStart: time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC),
		UptimePct:   99.5,
	}
}

func TestHandler_DownloadPDF_MissingYear(t *testing.T) {
	h := makeDownloadPDFHandler(nil, nil, nil, nil)
	tid := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/pdf?month=3", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	h.DownloadPDF(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidYear {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeInvalidYear)
	}
}

func TestHandler_DownloadPDF_MissingMonth(t *testing.T) {
	h := makeDownloadPDFHandler(nil, nil, nil, nil)
	tid := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/pdf?year=2025", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	h.DownloadPDF(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidMonth {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeInvalidMonth)
	}
}

func TestHandler_DownloadPDF_InvalidOperatorID(t *testing.T) {
	h := makeDownloadPDFHandler(nil, nil, nil, nil)
	tid := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/pdf?year=2025&month=3&operator_id=not-a-uuid", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	h.DownloadPDF(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidOperatorID {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeInvalidOperatorID)
	}
}

func TestHandler_DownloadPDF_EmptyMonth(t *testing.T) {
	h := makeDownloadPDFHandler(nil, nil, nil, nil)
	tid := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/pdf?year=2025&month=3", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	h.DownloadPDF(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeSLAMonthNotAvailable {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeSLAMonthNotAvailable)
	}
}

func TestHandler_DownloadPDF_HappyPath_AllTenant(t *testing.T) {
	pdfBytes := []byte("%PDF-1.4 fake content")
	h := makeDownloadPDFHandler(
		[]store.SLAReportRow{slaRow()},
		nil,
		&report.Artifact{Bytes: pdfBytes, MIME: "application/pdf"},
		nil,
	)
	tid := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/pdf?year=2025&month=3", nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	h.DownloadPDF(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/pdf")
	}
	cd := w.Header().Get("Content-Disposition")
	wantCD := `attachment; filename="sla-2025-03-all.pdf"`
	if cd != wantCD {
		t.Errorf("Content-Disposition = %q, want %q", cd, wantCD)
	}
	if len(w.Body.Bytes()) == 0 {
		t.Error("expected non-empty body")
	}
}

func TestHandler_DownloadPDF_HappyPath_PerOperator_NoCode(t *testing.T) {
	pdfBytes := []byte("%PDF-1.4 fake content per op")
	opID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	h := makeDownloadPDFHandler(
		[]store.SLAReportRow{slaRow()},
		nil,
		&report.Artifact{Bytes: pdfBytes, MIME: "application/pdf"},
		nil,
	)
	tid := uuid.New()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/sla/pdf?year=2025&month=3&operator_id=%s", opID), nil)
	req = withTenantCtx(req, tid)
	w := httptest.NewRecorder()
	h.DownloadPDF(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	cd := w.Header().Get("Content-Disposition")
	wantFallback := fmt.Sprintf(`attachment; filename="sla-2025-03-%s.pdf"`, opID.String()[:8])
	if cd != wantFallback {
		t.Errorf("Content-Disposition = %q, want %q", cd, wantFallback)
	}
}
