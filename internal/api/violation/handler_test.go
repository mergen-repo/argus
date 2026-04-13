package violation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func withCtx(r *http.Request, tenantID, userID uuid.UUID) *http.Request {
	ctx := r.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, userID)
	return r.WithContext(ctx)
}

func TestAcknowledge_MissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	id := uuid.New()

	router := chi.NewRouter()
	router.Post("/policy-violations/{id}/acknowledge", h.Acknowledge)

	req := httptest.NewRequest(http.MethodPost, "/policy-violations/"+id.String()+"/acknowledge", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestAcknowledge_InvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	router := chi.NewRouter()
	router.Post("/policy-violations/{id}/acknowledge", h.Acknowledge)

	req := httptest.NewRequest(http.MethodPost, "/policy-violations/not-a-uuid/acknowledge", nil)
	req = withCtx(req, uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestList_MissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodGet, "/policy-violations", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestCountByType_MissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodGet, "/policy-violations/counts", nil)
	w := httptest.NewRecorder()

	h.CountByType(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestNewHandler_WithOptions(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
	if h.auditSvc != nil {
		t.Error("auditSvc should be nil by default")
	}

	_ = context.Background()
}
