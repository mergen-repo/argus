package webhooks

import (
	"bytes"
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

func withTenantCtx(ctx context.Context, tenantID uuid.UUID) context.Context {
	return context.WithValue(ctx, apierr.TenantIDKey, tenantID)
}

func withRouteID(ctx context.Context, key, val string) context.Context {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return context.WithValue(ctx, chi.RouteCtxKey, rctx)
}

func newNilHandler() *Handler {
	return NewHandler(nil, nil, nil, nil, zerolog.Nop())
}

func TestHandler_List_NoTenant(t *testing.T) {
	h := newNilHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks", nil)
	w := httptest.NewRecorder()
	h.List(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_Create_NoTenant(t *testing.T) {
	h := newNilHandler()
	body := `{"url":"https://example.com","secret":"s"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_Create_MissingURL(t *testing.T) {
	h := newNilHandler()
	tenantID := uuid.New()
	body := `{"secret":"s","event_types":[]}`
	ctx := withTenantCtx(context.Background(), tenantID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", bytes.NewBufferString(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandler_Create_MissingSecret(t *testing.T) {
	h := newNilHandler()
	tenantID := uuid.New()
	body := `{"url":"https://example.com/hook","event_types":[]}`
	ctx := withTenantCtx(context.Background(), tenantID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", bytes.NewBufferString(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandler_Create_BadJSON(t *testing.T) {
	h := newNilHandler()
	tenantID := uuid.New()
	ctx := withTenantCtx(context.Background(), tenantID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", bytes.NewBufferString("not-json")).WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_Update_NoTenant(t *testing.T) {
	h := newNilHandler()
	ctx := withRouteID(context.Background(), "id", uuid.New().String())
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/webhooks/"+uuid.New().String(), bytes.NewBufferString("{}")).WithContext(ctx)
	w := httptest.NewRecorder()
	h.Update(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_Update_InvalidID(t *testing.T) {
	h := newNilHandler()
	tenantID := uuid.New()
	ctx := withTenantCtx(context.Background(), tenantID)
	ctx = withRouteID(ctx, "id", "bad-uuid")
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/webhooks/bad-uuid", bytes.NewBufferString("{}")).WithContext(ctx)
	w := httptest.NewRecorder()
	h.Update(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_Delete_NoTenant(t *testing.T) {
	h := newNilHandler()
	ctx := withRouteID(context.Background(), "id", uuid.New().String())
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/webhooks/"+uuid.New().String(), nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.Delete(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_Delete_InvalidID(t *testing.T) {
	h := newNilHandler()
	tenantID := uuid.New()
	ctx := withTenantCtx(context.Background(), tenantID)
	ctx = withRouteID(ctx, "id", "not-a-uuid")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/webhooks/not-a-uuid", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.Delete(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_ListDeliveries_NoTenant(t *testing.T) {
	h := newNilHandler()
	ctx := withRouteID(context.Background(), "id", uuid.New().String())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/x/deliveries", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListDeliveries(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_ListDeliveries_InvalidID(t *testing.T) {
	h := newNilHandler()
	tenantID := uuid.New()
	ctx := withTenantCtx(context.Background(), tenantID)
	ctx = withRouteID(ctx, "id", "bad-id")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/bad-id/deliveries", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListDeliveries(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_RetryDelivery_InvalidConfigID(t *testing.T) {
	h := newNilHandler()
	tenantID := uuid.New()
	ctx := withTenantCtx(context.Background(), tenantID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "bad-id")
	rctx.URLParams.Add("delivery_id", uuid.New().String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bad-id/deliveries/x/retry", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.RetryDelivery(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_RetryDelivery_InvalidDeliveryID(t *testing.T) {
	h := newNilHandler()
	tenantID := uuid.New()
	ctx := withTenantCtx(context.Background(), tenantID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuid.New().String())
	rctx.URLParams.Add("delivery_id", "bad-delivery-id")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/x/deliveries/bad/retry", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.RetryDelivery(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_Create_SecretNotInValidationError(t *testing.T) {
	h := newNilHandler()
	tenantID := uuid.New()
	body := `{"url":"","secret":"my-very-secret-value"}`
	ctx := withTenantCtx(context.Background(), tenantID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", bytes.NewBufferString(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	respStr, _ := json.Marshal(resp)
	if bytes.Contains(respStr, []byte("my-very-secret-value")) {
		t.Error("secret value must not appear in error response")
	}
}
