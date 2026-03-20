package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
)

func TestCorrelationID_SetsHeader(t *testing.T) {
	handler := CorrelationID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	id := rec.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("expected X-Request-ID header to be set")
	}
	if len(id) != 36 {
		t.Fatalf("expected UUID format (36 chars), got %d chars: %s", len(id), id)
	}
}

func TestCorrelationID_InjectsIntoContext(t *testing.T) {
	var ctxID string
	handler := CorrelationID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = GetCorrelationID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if ctxID == "" {
		t.Fatal("expected correlation_id in context")
	}
	headerID := rec.Header().Get("X-Request-ID")
	if ctxID != headerID {
		t.Fatalf("context ID %q != header ID %q", ctxID, headerID)
	}
}

func TestGetCorrelationID_EmptyContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	id := GetCorrelationID(req.Context())
	if id != "" {
		t.Fatalf("expected empty string for missing correlation_id, got %q", id)
	}
}

func TestCorrelationID_UniquePerRequest(t *testing.T) {
	var ids []string
	handler := CorrelationID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, r.Context().Value(apierr.CorrelationIDKey).(string))
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate correlation_id: %s", id)
		}
		seen[id] = true
	}
}
