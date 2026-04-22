package cdr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/go-chi/chi/v5"
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

func TestHandler_List_MissingDateRange_422(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cdrs", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandler_List_Range30dCap_422(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cdrs?from=2026-01-01T00:00:00Z&to=2026-03-05T00:00:00Z", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandler_List_Range30dCap_OverrideRequiresAdmin(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.RoleKey, "analyst")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cdrs?from=2026-01-01T00:00:00Z&to=2026-03-05T00:00:00Z&override_range=true", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_List_Range30dCap_SuperAdminOverride(t *testing.T) {
	// The handler should NOT reject with 422/403 when super_admin overrides.
	// The store call panics on nil pool, so we catch that panic and treat it
	// as proof the validator let the request through.
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.RoleKey, "super_admin")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cdrs?from=2026-01-01T00:00:00Z&to=2026-03-05T00:00:00Z&override_range=true", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	defer func() {
		_ = recover() // expected: nil cdrStore will panic — that means validator passed
		if w.Code == http.StatusUnprocessableEntity || w.Code == http.StatusForbidden {
			t.Errorf("validator rejected super_admin override (status %d)", w.Code)
		}
	}()
	h.List(w, req)
}

func TestHandler_List_InvalidSimID_400(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cdrs?from=2026-04-01T00:00:00Z&to=2026-04-10T00:00:00Z&sim_id=not-a-uuid", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_BySession_InvalidID_400(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cdrs/by-session/not-a-uuid", nil)
	req = req.WithContext(ctx)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("session_id", "not-a-uuid")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.BySession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_Stats_MissingRange_422(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cdrs/stats", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Stats(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandler_Export_Range30dCap_422(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	body := `{"from":"2026-01-01T00:00:00Z","to":"2026-03-05T00:00:00Z","format":"csv"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cdrs/export", strings.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Export(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}
