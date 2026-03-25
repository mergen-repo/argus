package sim

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

func withTenantCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
}

func TestActivateInvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/not-uuid/activate", nil)
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Activate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Activate(invalid id) status = %d, want 400", w.Code)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidFormat {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidFormat)
	}
}

func TestSuspendInvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/not-uuid/suspend", nil)
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Suspend(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Suspend(invalid id) status = %d, want 400", w.Code)
	}
}

func TestResumeInvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/not-uuid/resume", nil)
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Resume(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Resume(invalid id) status = %d, want 400", w.Code)
	}
}

func TestTerminateInvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/not-uuid/terminate", nil)
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Terminate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Terminate(invalid id) status = %d, want 400", w.Code)
	}
}

func TestReportLostInvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/not-uuid/report-lost", nil)
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ReportLost(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ReportLost(invalid id) status = %d, want 400", w.Code)
	}
}

func TestActivateNoTenantContext(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/"+uuid.New().String()+"/activate", nil)
	w := httptest.NewRecorder()

	h.Activate(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Activate(no tenant) status = %d, want 403", w.Code)
	}
}

func TestSuspendNoTenantContext(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/"+uuid.New().String()+"/suspend", nil)
	w := httptest.NewRecorder()

	h.Suspend(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Suspend(no tenant) status = %d, want 403", w.Code)
	}
}

func TestResumeNoTenantContext(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/"+uuid.New().String()+"/resume", nil)
	w := httptest.NewRecorder()

	h.Resume(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Resume(no tenant) status = %d, want 403", w.Code)
	}
}

func TestTerminateNoTenantContext(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/"+uuid.New().String()+"/terminate", nil)
	w := httptest.NewRecorder()

	h.Terminate(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Terminate(no tenant) status = %d, want 403", w.Code)
	}
}

func TestReportLostNoTenantContext(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/"+uuid.New().String()+"/report-lost", nil)
	w := httptest.NewRecorder()

	h.ReportLost(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("ReportLost(no tenant) status = %d, want 403", w.Code)
	}
}

func TestCreateSIMInvalidJSON(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	ctx := withTenantCtx(req.Context())
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Create(invalid json) status = %d, want 400", w.Code)
	}
}

func TestCreateSIMNoTenantContext(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	body := `{"iccid":"8990123456789012345","imsi":"286010123456789","operator_id":"` + uuid.New().String() + `","apn_id":"` + uuid.New().String() + `","sim_type":"physical"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Create(no tenant) status = %d, want 403", w.Code)
	}
}

func TestUserIDFromCtx_NoUserID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	uid := userIDFromCtx(req)
	if uid != nil {
		t.Error("expected nil userID when not set in context")
	}
}

func TestUserIDFromCtx_NilUUID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := context.WithValue(req.Context(), apierr.UserIDKey, uuid.Nil)
	req = req.WithContext(ctx)
	uid := userIDFromCtx(req)
	if uid != nil {
		t.Error("expected nil for uuid.Nil")
	}
}

func TestUserIDFromCtx_ValidUUID(t *testing.T) {
	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := context.WithValue(req.Context(), apierr.UserIDKey, id)
	req = req.WithContext(ctx)
	uid := userIDFromCtx(req)
	if uid == nil {
		t.Fatal("expected non-nil userID")
	}
	if *uid != id {
		t.Errorf("userID = %s, want %s", uid, id)
	}
}

func TestReasonRequestParsing(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantNil    bool
		wantReason string
	}{
		{"with reason", `{"reason":"quota exceeded"}`, false, "quota exceeded"},
		{"empty body", `{}`, true, ""},
		{"null reason", `{"reason":null}`, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req reasonRequest
			json.NewDecoder(strings.NewReader(tt.body)).Decode(&req)

			if tt.wantNil && req.Reason != nil {
				t.Error("expected nil reason")
			}
			if !tt.wantNil && (req.Reason == nil || *req.Reason != tt.wantReason) {
				t.Errorf("reason = %v, want %q", req.Reason, tt.wantReason)
			}
		})
	}
}

func TestNewHandler(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
}

func TestCreateAuditEntry_NilAuditSvc(t *testing.T) {
	h := &Handler{logger: zerolog.Nop(), auditSvc: nil}
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	h.createAuditEntry(req, "sim.create", "test-id", nil, nil, nil)
}
