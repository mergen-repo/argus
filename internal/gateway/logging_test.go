package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestTenantLabel_UnknownWhenMissing(t *testing.T) {
	got := TenantLabel(context.Background())
	if got != "unknown" {
		t.Errorf("expected %q, got %q", "unknown", got)
	}
}

func TestTenantLabel_PresentFromCtx(t *testing.T) {
	tid := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tid)
	got := TenantLabel(ctx)
	if got != tid.String() {
		t.Errorf("expected %q, got %q", tid.String(), got)
	}
}

func TestZerologRequestLogger_IncludesTenantID(t *testing.T) {
	tid := uuid.New()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	stubAuth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), apierr.TenantIDKey, tid)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	handler := stubAuth(
		ZerologRequestLogger(logger)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/sims", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("log output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	gotTenant, ok := logEntry["tenant_id"].(string)
	if !ok {
		t.Fatalf("expected tenant_id field in log entry, got: %s", buf.String())
	}
	if gotTenant != tid.String() {
		t.Errorf("expected tenant_id %q, got %q", tid.String(), gotTenant)
	}
}
