package sla

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

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
