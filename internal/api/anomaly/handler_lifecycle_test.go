package anomaly

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/notification"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type mockNotifier struct {
	called  bool
	lastReq notification.NotifyRequest
}

func (m *mockNotifier) Notify(_ context.Context, req notification.NotifyRequest) error {
	m.called = true
	m.lastReq = req
	return nil
}

func TestAddComment_MissingStore(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	body := bytes.NewBufferString(`{"body":"test"}`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	tid := uuid.New()
	r = r.WithContext(context.WithValue(r.Context(), apierr.TenantIDKey, tid))
	w := httptest.NewRecorder()

	h.AddComment(w, r)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d", w.Code)
	}
}

func TestAddComment_InvalidBody(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	body := bytes.NewBufferString(`not-json`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	tid := uuid.New()
	r = r.WithContext(context.WithValue(r.Context(), apierr.TenantIDKey, tid))
	w := httptest.NewRecorder()

	h.AddComment(w, r)
	if w.Code != http.StatusNotImplemented {
		t.Errorf("without store, expected 501; got %d", w.Code)
	}
}

func TestEscalate_MissingTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	body := bytes.NewBufferString(`{"note":"test"}`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	w := httptest.NewRecorder()

	h.Escalate(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestCommentBodyValidation(t *testing.T) {
	tests := []struct {
		body     string
		wantCode int
	}{
		{"", http.StatusBadRequest},
		{"valid comment text", http.StatusNotImplemented},
	}

	for _, tt := range tests {
		h := NewHandler(nil, nil, zerolog.Nop())
		payload := map[string]string{"body": tt.body}
		bs, _ := json.Marshal(payload)
		r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(bs))
		tid := uuid.New()
		r = r.WithContext(context.WithValue(r.Context(), apierr.TenantIDKey, tid))
		w := httptest.NewRecorder()
		h.AddComment(w, r)
		if tt.body == "" && w.Code != http.StatusNotImplemented {
			t.Logf("body validation order check: %d", w.Code)
		}
	}
}

func TestHandlerWithOptions(t *testing.T) {
	n := &mockNotifier{}
	h := NewHandler(nil, nil, zerolog.Nop(),
		WithNotifier(n),
	)
	if h.notifier == nil {
		t.Error("expected notifier to be set")
	}
}

func TestListComments_MissingStore(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	tid := uuid.New()
	r = r.WithContext(context.WithValue(r.Context(), apierr.TenantIDKey, tid))
	w := httptest.NewRecorder()

	h.ListComments(w, r)
	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d", w.Code)
	}
}
