package cdr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestHandler_List_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cdrs", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_List_WithTenantContext(t *testing.T) {
	t.Skip("requires database connection")
}

func TestHandler_Export_InvalidJSON(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cdrs/export", strings.NewReader("not json"))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Export(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_Export_MissingDates(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	body := `{"format":"csv"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cdrs/export", strings.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Export(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandler_Export_InvalidFormat(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	body := `{"from":"2026-03-01T00:00:00Z","to":"2026-03-22T00:00:00Z","format":"xlsx"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cdrs/export", strings.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Export(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandler_Export_FromAfterTo(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	body := `{"from":"2026-03-22T00:00:00Z","to":"2026-03-01T00:00:00Z","format":"csv"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cdrs/export", strings.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Export(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandler_Export_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	body := `{"from":"2026-03-01T00:00:00Z","to":"2026-03-22T00:00:00Z","format":"csv"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cdrs/export", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.Export(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_List_InvalidFromDate(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cdrs?from=bad-date", nil)
	req = req.WithContext(ctx)
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
