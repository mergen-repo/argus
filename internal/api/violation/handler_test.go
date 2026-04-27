package violation

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

// FIX-244 DEV-521: status filter input validation (handler-only — DB path
// not exercised here; integration suite covers the store-level translation).
func TestList_InvalidStatus(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodGet, "/policy-violations?status=banana", nil)
	req = withCtx(req, uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unknown status value", w.Code)
	}
}

func TestList_InvalidDateFrom(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodGet, "/policy-violations?date_from=not-a-date", nil)
	req = withCtx(req, uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for malformed date_from", w.Code)
	}
}

func TestList_DateRangeInverted(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	url := "/policy-violations?date_from=2026-04-27T00:00:00Z&date_to=2026-04-01T00:00:00Z"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req = withCtx(req, uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for inverted date range", w.Code)
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

// FIX-244 DEV-520: destructive actions require a meaningful reason.
func TestRemediate_Suspend_RequiresReasonMin3(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	id := uuid.New()

	router := chi.NewRouter()
	router.Post("/policy-violations/{id}/remediate", h.Remediate)

	cases := []struct{ name, body string }{
		{"missing", `{"action":"suspend_sim"}`},
		{"empty", `{"action":"suspend_sim","reason":""}`},
		{"whitespace", `{"action":"suspend_sim","reason":"   "}`},
		{"too_short", `{"action":"suspend_sim","reason":"x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/policy-violations/"+id.String()+"/remediate",
				strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = withCtx(req, uuid.New(), uuid.New())
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 for %s reason", w.Code, tc.name)
			}
			if !strings.Contains(w.Body.String(), "at least 3") {
				t.Errorf("body should mention 3-char minimum, got: %s", w.Body.String())
			}
		})
	}
}

func TestRemediate_Dismiss_RequiresReasonMin3(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	id := uuid.New()

	router := chi.NewRouter()
	router.Post("/policy-violations/{id}/remediate", h.Remediate)

	body := `{"action":"dismiss","reason":"no"}`
	req := httptest.NewRequest(http.MethodPost, "/policy-violations/"+id.String()+"/remediate",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withCtx(req, uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for short dismiss reason", w.Code)
	}
}

// FIX-244 DEV-522: bulk endpoints — pre-store validation paths.
func TestBulkAcknowledge_MissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/policy-violations/bulk/acknowledge",
		strings.NewReader(`{"ids":[]}`))
	w := httptest.NewRecorder()
	h.BulkAcknowledge(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestBulkAcknowledge_EmptyIDs(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/policy-violations/bulk/acknowledge",
		strings.NewReader(`{"ids":[]}`))
	req = withCtx(req, uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	h.BulkAcknowledge(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for empty ids", w.Code)
	}
}

func TestBulkAcknowledge_ExceedsCap(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	ids := make([]string, 101)
	for i := range ids {
		ids[i] = uuid.New().String()
	}
	body, _ := json.Marshal(map[string]interface{}{"ids": ids})

	req := httptest.NewRequest(http.MethodPost, "/policy-violations/bulk/acknowledge",
		strings.NewReader(string(body)))
	req = withCtx(req, uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	h.BulkAcknowledge(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for >100 ids", w.Code)
	}
	if !strings.Contains(w.Body.String(), "≤ 100") {
		t.Errorf("body should mention 100-id cap, got: %s", w.Body.String())
	}
}

func TestBulkDismiss_RequiresReasonMin3(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/policy-violations/bulk/dismiss",
		strings.NewReader(`{"ids":["`+uuid.New().String()+`"],"reason":"no"}`))
	req = withCtx(req, uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	h.BulkDismiss(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for short dismiss reason", w.Code)
	}
}

func TestBulkDismiss_MissingReason(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/policy-violations/bulk/dismiss",
		strings.NewReader(`{"ids":["`+uuid.New().String()+`"]}`))
	req = withCtx(req, uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	h.BulkDismiss(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing reason", w.Code)
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
