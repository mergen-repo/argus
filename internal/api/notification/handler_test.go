package notification

import (
	"context"
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
	h := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_List_NoUserContext(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_MarkRead_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/notifications/"+uuid.New().String()+"/read", nil)
	w := httptest.NewRecorder()

	h.MarkRead(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_MarkRead_InvalidID(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "invalid-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/notifications/invalid-uuid/read", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.MarkRead(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_MarkAllRead_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/read-all", nil)
	w := httptest.NewRecorder()

	h.MarkAllRead(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_MarkAllRead_NoUserContext(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/read-all", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.MarkAllRead(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_GetConfigs_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notification-configs", nil)
	w := httptest.NewRecorder()

	h.GetConfigs(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_UpdateConfigs_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-configs", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	h.UpdateConfigs(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_UpdateConfigs_InvalidBody(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	userID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, userID)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-configs", strings.NewReader(`invalid`))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpdateConfigs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_UpdateConfigs_EmptyConfigs(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	userID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, userID)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-configs", strings.NewReader(`{"configs":[]}`))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpdateConfigs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_UpdateConfigs_InvalidEventType(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	userID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, userID)

	body := `{"configs":[{"event_type":"invalid.event","scope_type":"system","channels":{"email":true},"enabled":true}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-configs", strings.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpdateConfigs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_UpdateConfigs_InvalidScopeType(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	userID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, userID)

	body := `{"configs":[{"event_type":"operator.down","scope_type":"invalid","channels":{"email":true},"enabled":true}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-configs", strings.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpdateConfigs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
