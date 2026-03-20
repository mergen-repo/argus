package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func withTenantContext(r *http.Request) *http.Request {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ctx := context.WithValue(r.Context(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	return r.WithContext(ctx)
}

func TestHandler_List_NoTenantContext(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
	w := httptest.NewRecorder()

	handler.List(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeForbidden {
		t.Fatalf("error code = %s, want %s", resp.Error.Code, apierr.CodeForbidden)
	}
}

func TestHandler_Verify_NoTenantContext(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs/verify", nil)
	w := httptest.NewRecorder()

	handler.Verify(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_Export_NoTenantContext(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit-logs/export", strings.NewReader(`{"from":"2026-03-01","to":"2026-03-20"}`))
	w := httptest.NewRecorder()

	handler.Export(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_Export_InvalidBody(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit-logs/export", strings.NewReader("not json"))
	req = withTenantContext(req)
	w := httptest.NewRecorder()

	handler.Export(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidFormat {
		t.Fatalf("error code = %s, want %s", resp.Error.Code, apierr.CodeInvalidFormat)
	}
}

func TestHandler_Export_MissingFields(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit-logs/export", strings.NewReader(`{}`))
	req = withTenantContext(req)
	w := httptest.NewRecorder()

	handler.Export(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Fatalf("error code = %s, want %s", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestHandler_Export_InvalidDateFormat(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit-logs/export", strings.NewReader(`{"from":"not-a-date","to":"2026-03-20"}`))
	req = withTenantContext(req)
	w := httptest.NewRecorder()

	handler.Export(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestToAuditLogResponse(t *testing.T) {
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	ip := "192.168.1.1"
	ts := time.Date(2026, 3, 18, 14, 2, 0, 123456789, time.UTC)

	entry := audit.Entry{
		ID:         42,
		UserID:     &userID,
		Action:     "create",
		EntityType: "sim",
		EntityID:   "sim-1",
		Diff:       json.RawMessage(`{"name":{"from":null,"to":"test"}}`),
		IPAddress:  &ip,
		CreatedAt:  ts,
	}

	resp := toAuditLogResponse(entry)

	if resp.ID != 42 {
		t.Fatalf("id = %d, want 42", resp.ID)
	}
	if resp.Action != "create" {
		t.Fatalf("action = %s, want create", resp.Action)
	}
	if resp.EntityType != "sim" {
		t.Fatalf("entity_type = %s, want sim", resp.EntityType)
	}
	if resp.EntityID != "sim-1" {
		t.Fatalf("entity_id = %s, want sim-1", resp.EntityID)
	}
	if *resp.IPAddress != "192.168.1.1" {
		t.Fatalf("ip_address = %s, want 192.168.1.1", *resp.IPAddress)
	}
	if resp.CreatedAt != ts.Format(time.RFC3339Nano) {
		t.Fatalf("created_at = %s, want %s", resp.CreatedAt, ts.Format(time.RFC3339Nano))
	}
}

func TestToAuditLogResponse_NilOptionalFields(t *testing.T) {
	ts := time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC)

	entry := audit.Entry{
		ID:         1,
		Action:     "delete",
		EntityType: "user",
		EntityID:   "user-1",
		CreatedAt:  ts,
	}

	resp := toAuditLogResponse(entry)

	if resp.UserID != nil {
		t.Fatal("user_id should be nil")
	}
	if resp.IPAddress != nil {
		t.Fatal("ip_address should be nil")
	}
	if resp.Diff != nil {
		t.Fatal("diff should be nil")
	}
}

func TestHandler_Export_InvalidToDate(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit-logs/export", strings.NewReader(`{"from":"2026-03-01","to":"not-a-date"}`))
	req = withTenantContext(req)
	w := httptest.NewRecorder()

	handler.Export(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}
