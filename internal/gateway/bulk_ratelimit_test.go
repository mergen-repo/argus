package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
)

func requestWithTenantBulk(tenantID uuid.UUID) *http.Request {
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	return httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/state-change", nil).WithContext(ctx)
}

func newBulkPassthrough() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestBulkRateLimit_FirstRequest_Allowed(t *testing.T) {
	m := NewBulkRateLimiter(1.0, 2)
	defer m.Shutdown()

	rec := httptest.NewRecorder()
	m.Middleware()(newBulkPassthrough()).ServeHTTP(rec, requestWithTenantBulk(uuid.New()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestBulkRateLimit_SecondImmediate_429(t *testing.T) {
	m := NewBulkRateLimiter(1.0, 1)
	defer m.Shutdown()

	tenantID := uuid.New()
	mw := m.Middleware()(newBulkPassthrough())

	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, requestWithTenantBulk(tenantID))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want 200", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, requestWithTenantBulk(tenantID))
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second immediate request: status = %d, want 429", rec2.Code)
	}
}

func TestBulkRateLimit_DifferentTenants_Independent(t *testing.T) {
	m := NewBulkRateLimiter(1.0, 1)
	defer m.Shutdown()

	mw := m.Middleware()(newBulkPassthrough())
	tenantA := uuid.New()
	tenantB := uuid.New()

	recA := httptest.NewRecorder()
	mw.ServeHTTP(recA, requestWithTenantBulk(tenantA))
	if recA.Code != http.StatusOK {
		t.Fatalf("tenant A: status = %d, want 200", recA.Code)
	}

	recB := httptest.NewRecorder()
	mw.ServeHTTP(recB, requestWithTenantBulk(tenantB))
	if recB.Code != http.StatusOK {
		t.Fatalf("tenant B: status = %d, want 200", recB.Code)
	}
}

func TestBulkRateLimit_AfterOneSecond_Allowed(t *testing.T) {
	m := NewBulkRateLimiter(1.0, 1)
	defer m.Shutdown()

	tenantID := uuid.New()
	mw := m.Middleware()(newBulkPassthrough())

	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, requestWithTenantBulk(tenantID))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want 200", rec1.Code)
	}

	time.Sleep(1100 * time.Millisecond)

	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, requestWithTenantBulk(tenantID))
	if rec2.Code != http.StatusOK {
		t.Fatalf("after 1.1s: status = %d, want 200", rec2.Code)
	}
}

func TestBulkRateLimit_NoTenantContext_BypassedCleanly(t *testing.T) {
	m := NewBulkRateLimiter(1.0, 1)
	defer m.Shutdown()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/state-change", nil)
	rec := httptest.NewRecorder()
	m.Middleware()(newBulkPassthrough()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("no tenant context: status = %d, want 200 (bypass)", rec.Code)
	}
}

func TestBulkRateLimit_429Response_IncludesRetryAfter(t *testing.T) {
	m := NewBulkRateLimiter(1.0, 1)
	defer m.Shutdown()

	tenantID := uuid.New()
	mw := m.Middleware()(newBulkPassthrough())

	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, requestWithTenantBulk(tenantID))

	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, requestWithTenantBulk(tenantID))

	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec2.Code)
	}
	if got := rec2.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want \"1\"", got)
	}
}

func TestBulkRateLimit_429Response_EnvelopeShape(t *testing.T) {
	m := NewBulkRateLimiter(1.0, 1)
	defer m.Shutdown()

	tenantID := uuid.New()
	mw := m.Middleware()(newBulkPassthrough())

	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, requestWithTenantBulk(tenantID))

	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, requestWithTenantBulk(tenantID))

	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec2.Code)
	}

	var resp apierr.ErrorResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp.Status != "error" {
		t.Errorf("status = %q, want \"error\"", resp.Status)
	}
	if resp.Error.Code != apierr.CodeRateLimited {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeRateLimited)
	}
}
