package analytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/analytics/cost"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestHandler_GetUsage_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/usage", nil)
	w := httptest.NewRecorder()

	h.GetUsage(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_GetUsage_InvalidPeriod(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/usage?period=invalid", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetUsage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidFormat {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidFormat)
	}
}

func TestHandler_GetUsage_CustomPeriodMissingDates(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/usage?period=custom", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetUsage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetUsage_CustomPeriodInvalidFrom(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/usage?period=custom&from=bad&to=2026-03-22T00:00:00Z", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetUsage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetUsage_CustomPeriodFromAfterTo(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/usage?period=custom&from=2026-03-22T00:00:00Z&to=2026-03-01T00:00:00Z", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetUsage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetUsage_InvalidGroupBy(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/usage?group_by=invalid_column", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetUsage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetUsage_InvalidOperatorID(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/usage?operator_id=not-a-uuid", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetUsage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetUsage_InvalidAPNID(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/usage?apn_id=not-a-uuid", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetUsage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetUsage_DefaultPeriod(t *testing.T) {
	t.Skip("requires database connection")
}

func TestDeltaPercent(t *testing.T) {
	tests := []struct {
		name     string
		current  int64
		previous int64
		want     float64
	}{
		{"zero_to_zero", 0, 0, 0},
		{"zero_to_positive", 100, 0, 100.0},
		{"double", 200, 100, 100.0},
		{"half", 50, 100, -50.0},
		{"same", 100, 100, 0},
		{"decrease_to_zero", 0, 100, -100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deltaPercent(tt.current, tt.previous)
			if got != tt.want {
				t.Errorf("deltaPercent(%d, %d) = %v, want %v", tt.current, tt.previous, got, tt.want)
			}
		})
	}
}

func TestValidPeriods(t *testing.T) {
	for _, p := range []string{"1h", "24h", "7d", "30d", "custom"} {
		if !validPeriods[p] {
			t.Errorf("validPeriods[%q] = false, want true", p)
		}
	}
	if validPeriods["invalid"] {
		t.Error("validPeriods[invalid] = true, want false")
	}
}

func TestValidGroupBy(t *testing.T) {
	for _, g := range []string{"operator", "operator_id", "apn", "apn_id", "rat_type"} {
		if !validGroupBy[g] {
			t.Errorf("validGroupBy[%q] = false, want true", g)
		}
	}
	if validGroupBy["invalid"] {
		t.Error("validGroupBy[invalid] = true, want false")
	}
}

func TestHandler_GetCost_NoCostService(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cost", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetCost(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestHandler_GetCost_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())
	h.costService = &cost.Service{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cost", nil)
	w := httptest.NewRecorder()

	h.GetCost(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_GetCost_InvalidPeriod(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())
	h.costService = &cost.Service{}

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cost?period=invalid", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetCost(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetCost_CustomPeriodMissingDates(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())
	h.costService = &cost.Service{}

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cost?period=custom", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetCost(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetCost_CustomPeriodFromAfterTo(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())
	h.costService = &cost.Service{}

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cost?period=custom&from=2026-03-22T00:00:00Z&to=2026-03-01T00:00:00Z", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetCost(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetCost_InvalidOperatorID(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())
	h.costService = &cost.Service{}

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cost?operator_id=not-a-uuid", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetCost(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetCost_InvalidAPNID(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())
	h.costService = &cost.Service{}

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cost?apn_id=not-a-uuid", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetCost(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetCost_InvalidFromFormat(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())
	h.costService = &cost.Service{}

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cost?period=custom&from=bad&to=2026-03-22T00:00:00Z", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetCost(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetCost_InvalidToFormat(t *testing.T) {
	h := NewHandler(nil, zerolog.Nop())
	h.costService = &cost.Service{}

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cost?period=custom&from=2026-03-01T00:00:00Z&to=bad", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetCost(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetCost_DefaultPeriod(t *testing.T) {
	t.Skip("requires database connection")
}
