package anomaly

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestHandler_List_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/anomalies", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_Get_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/anomalies/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_UpdateState_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	body := `{"state":"acknowledged"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/analytics/anomalies/"+uuid.New().String(), strings.NewReader(body))
	w := httptest.NewRecorder()

	h.UpdateState(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_UpdateState_InvalidState(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	tenantID := uuid.New()
	anomalyID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", anomalyID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	body := `{"state":"invalid"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/analytics/anomalies/"+anomalyID.String(), strings.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpdateState(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestHandler_List_InvalidSimID(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/anomalies?sim_id=invalid", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_List_InvalidFrom(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/anomalies?from=notadate", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_Get_InvalidID(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/anomalies/not-a-uuid", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestToAnomalyDTO(t *testing.T) {
	simID := uuid.New()

	dto := toAnomalyDTO(storeAnomalyFixture(simID), "8901234567890123456")

	if dto.Type != "sim_cloning" {
		t.Errorf("type = %q, want %q", dto.Type, "sim_cloning")
	}
	if dto.SimICCID != "8901234567890123456" {
		t.Errorf("sim_iccid = %q, want %q", dto.SimICCID, "8901234567890123456")
	}
	if dto.SimID == nil {
		t.Error("expected sim_id to be set")
	}
}

func TestHandler_Get_WithChiContext(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-a-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/anomalies/not-a-uuid", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func storeAnomalyFixture(simID uuid.UUID) store.Anomaly {
	now := time.Now().UTC()
	return store.Anomaly{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		SimID:      &simID,
		Type:       "sim_cloning",
		Severity:   "critical",
		State:      "open",
		Details:    json.RawMessage(`{"imsi":"001010000000001"}`),
		DetectedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}
