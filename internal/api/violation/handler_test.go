package violation

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

// STORY-075: Remediate — pre-store validation paths (don't require DB)
func TestRemediate_MissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	id := uuid.New()

	router := chi.NewRouter()
	router.Post("/policy-violations/{id}/remediate", h.Remediate)

	req := httptest.NewRequest(http.MethodPost, "/policy-violations/"+id.String()+"/remediate", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestRemediate_InvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	router := chi.NewRouter()
	router.Post("/policy-violations/{id}/remediate", h.Remediate)

	req := httptest.NewRequest(http.MethodPost, "/policy-violations/not-a-uuid/remediate", nil)
	req = withCtx(req, uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRemediate_InvalidJSON(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	id := uuid.New()

	router := chi.NewRouter()
	router.Post("/policy-violations/{id}/remediate", h.Remediate)

	req := httptest.NewRequest(http.MethodPost, "/policy-violations/"+id.String()+"/remediate",
		strings.NewReader("{not valid json"))
	req.Header.Set("Content-Type", "application/json")
	req = withCtx(req, uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for invalid JSON", w.Code)
	}
}

func TestRemediate_UnknownAction(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	id := uuid.New()

	router := chi.NewRouter()
	router.Post("/policy-violations/{id}/remediate", h.Remediate)

	body := `{"action":"nuke_everything","reason":"nope"}`
	req := httptest.NewRequest(http.MethodPost, "/policy-violations/"+id.String()+"/remediate",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withCtx(req, uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unknown action", w.Code)
	}
}

func TestGet_MissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	id := uuid.New()

	router := chi.NewRouter()
	router.Get("/policy-violations/{id}", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/policy-violations/"+id.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestGet_InvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	router := chi.NewRouter()
	router.Get("/policy-violations/{id}", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/policy-violations/not-a-uuid", nil)
	req = withCtx(req, uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
