package operator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestToOperatorResponse(t *testing.T) {
	now := time.Now()
	target := 99.90
	o := &store.Operator{
		ID:                        uuid.New(),
		Name:                      "Turkcell",
		Code:                      "turkcell",
		MCC:                       "286",
		MNC:                       "01",
		AdapterType:               "mock",
		SupportedRATTypes:         []string{"lte", "nr_5g"},
		HealthStatus:              "healthy",
		HealthCheckIntervalSec:    30,
		FailoverPolicy:            "reject",
		FailoverTimeoutMs:         5000,
		CircuitBreakerThreshold:   5,
		CircuitBreakerRecoverySec: 60,
		SLAUptimeTarget:           &target,
		State:                     "active",
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}

	resp := toOperatorResponse(o)

	if resp.ID != o.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, o.ID.String())
	}
	if resp.Name != "Turkcell" {
		t.Errorf("Name = %q, want %q", resp.Name, "Turkcell")
	}
	if resp.Code != "turkcell" {
		t.Errorf("Code = %q, want %q", resp.Code, "turkcell")
	}
	if resp.MCC != "286" {
		t.Errorf("MCC = %q, want %q", resp.MCC, "286")
	}
	if resp.MNC != "01" {
		t.Errorf("MNC = %q, want %q", resp.MNC, "01")
	}
	if resp.AdapterType != "mock" {
		t.Errorf("AdapterType = %q, want %q", resp.AdapterType, "mock")
	}
	if len(resp.SupportedRATTypes) != 2 {
		t.Errorf("SupportedRATTypes len = %d, want 2", len(resp.SupportedRATTypes))
	}
	if resp.HealthStatus != "healthy" {
		t.Errorf("HealthStatus = %q, want %q", resp.HealthStatus, "healthy")
	}
	if resp.FailoverPolicy != "reject" {
		t.Errorf("FailoverPolicy = %q, want %q", resp.FailoverPolicy, "reject")
	}
	if resp.State != "active" {
		t.Errorf("State = %q, want %q", resp.State, "active")
	}
	if resp.SLAUptimeTarget == nil || *resp.SLAUptimeTarget != 99.90 {
		t.Error("SLAUptimeTarget should be 99.90")
	}
}

func TestToOperatorResponseNilRATTypes(t *testing.T) {
	o := &store.Operator{
		ID:            uuid.New(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	resp := toOperatorResponse(o)
	if resp.SupportedRATTypes == nil {
		t.Error("SupportedRATTypes should never be nil in response")
	}
	if len(resp.SupportedRATTypes) != 0 {
		t.Errorf("SupportedRATTypes len = %d, want 0", len(resp.SupportedRATTypes))
	}
}

func TestToGrantResponse(t *testing.T) {
	now := time.Now()
	userID := uuid.New()
	g := &store.OperatorGrant{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		OperatorID: uuid.New(),
		Enabled:    true,
		GrantedAt:  now,
		GrantedBy:  &userID,
	}

	resp := toGrantResponse(g)

	if resp.ID != g.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, g.ID.String())
	}
	if resp.TenantID != g.TenantID.String() {
		t.Errorf("TenantID = %q, want %q", resp.TenantID, g.TenantID.String())
	}
	if resp.OperatorID != g.OperatorID.String() {
		t.Errorf("OperatorID = %q, want %q", resp.OperatorID, g.OperatorID.String())
	}
	if !resp.Enabled {
		t.Error("Enabled should be true")
	}
	if resp.GrantedBy == nil {
		t.Error("GrantedBy should not be nil")
	}
}

func TestToGrantResponseNoGrantedBy(t *testing.T) {
	g := &store.OperatorGrant{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		OperatorID: uuid.New(),
		Enabled:    true,
		GrantedAt:  time.Now(),
	}

	resp := toGrantResponse(g)
	if resp.GrantedBy != nil {
		t.Error("GrantedBy should be nil when not set")
	}
}

func TestValidAdapterTypes(t *testing.T) {
	valid := []string{"mock", "radius", "diameter", "sba"}
	for _, v := range valid {
		if !validAdapterTypes[v] {
			t.Errorf("%q should be valid adapter type", v)
		}
	}

	if validAdapterTypes["http"] {
		t.Error("http should not be valid adapter type")
	}
}

func TestValidFailoverPolicies(t *testing.T) {
	valid := []string{"reject", "fallback_to_next", "queue_with_timeout"}
	for _, v := range valid {
		if !validFailoverPolicies[v] {
			t.Errorf("%q should be valid failover policy", v)
		}
	}

	if validFailoverPolicies["retry"] {
		t.Error("retry should not be valid failover policy")
	}
}

func TestValidOperatorStates(t *testing.T) {
	if !validOperatorStates["active"] {
		t.Error("active should be valid")
	}
	if !validOperatorStates["disabled"] {
		t.Error("disabled should be valid")
	}
	if validOperatorStates["deleted"] {
		t.Error("deleted should not be valid")
	}
}

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantCode int
	}{
		{
			name:     "empty body",
			body:     `{}`,
			wantCode: 422,
		},
		{
			name:     "missing name",
			body:     `{"code":"tc","mcc":"286","mnc":"01","adapter_type":"mock"}`,
			wantCode: 422,
		},
		{
			name:     "missing code",
			body:     `{"name":"TC","mcc":"286","mnc":"01","adapter_type":"mock"}`,
			wantCode: 422,
		},
		{
			name:     "missing mcc",
			body:     `{"name":"TC","code":"tc","mnc":"01","adapter_type":"mock"}`,
			wantCode: 422,
		},
		{
			name:     "invalid mcc length",
			body:     `{"name":"TC","code":"tc","mcc":"28","mnc":"01","adapter_type":"mock"}`,
			wantCode: 422,
		},
		{
			name:     "invalid mnc length",
			body:     `{"name":"TC","code":"tc","mcc":"286","mnc":"0","adapter_type":"mock"}`,
			wantCode: 422,
		},
		{
			name:     "missing adapter_type",
			body:     `{"name":"TC","code":"tc","mcc":"286","mnc":"01"}`,
			wantCode: 422,
		},
		{
			name:     "invalid adapter_type",
			body:     `{"name":"TC","code":"tc","mcc":"286","mnc":"01","adapter_type":"unknown"}`,
			wantCode: 422,
		},
		{
			name:     "invalid failover_policy",
			body:     `{"name":"TC","code":"tc","mcc":"286","mnc":"01","adapter_type":"mock","failover_policy":"invalid"}`,
			wantCode: 422,
		},
		{
			name:     "invalid json",
			body:     `not json`,
			wantCode: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				logger: zerolog.Nop(),
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/operators", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.Create(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Create(%s) status = %d, want %d, body: %s", tt.name, w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestUpdateValidation(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		body     string
		wantCode int
	}{
		{
			name:     "invalid id format",
			id:       "not-a-uuid",
			body:     `{"name":"test"}`,
			wantCode: 400,
		},
		{
			name:     "invalid json",
			id:       uuid.New().String(),
			body:     `not json`,
			wantCode: 400,
		},
		{
			name:     "invalid failover_policy",
			id:       uuid.New().String(),
			body:     `{"failover_policy":"bad"}`,
			wantCode: 422,
		},
		{
			name:     "invalid state",
			id:       uuid.New().String(),
			body:     `{"state":"deleted"}`,
			wantCode: 422,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				logger: zerolog.Nop(),
			}

			req := httptest.NewRequest(http.MethodPatch, "/api/v1/operators/"+tt.id, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.id)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			w := httptest.NewRecorder()

			h.Update(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Update(%s) status = %d, want %d, body: %s", tt.name, w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestCreateGrantValidation(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{
			name:     "empty body",
			body:     `{}`,
			wantCode: 422,
		},
		{
			name:     "missing tenant_id",
			body:     `{"operator_id":"` + uuid.New().String() + `"}`,
			wantCode: 422,
		},
		{
			name:     "missing operator_id",
			body:     `{"tenant_id":"` + uuid.New().String() + `"}`,
			wantCode: 422,
		},
		{
			name:     "invalid tenant_id format",
			body:     `{"tenant_id":"bad","operator_id":"` + uuid.New().String() + `"}`,
			wantCode: 400,
		},
		{
			name:     "invalid json",
			body:     `not json`,
			wantCode: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				logger: zerolog.Nop(),
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/operator-grants", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.CreateGrant(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("CreateGrant(%s) status = %d, want %d, body: %s", tt.name, w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestDeleteGrantInvalidID(t *testing.T) {
	h := &Handler{
		logger: zerolog.Nop(),
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/operator-grants/not-uuid", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-uuid")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.DeleteGrant(w, req)

	if w.Code != 400 {
		t.Errorf("DeleteGrant(invalid id) status = %d, want 400", w.Code)
	}
}

func TestGetHealthInvalidID(t *testing.T) {
	h := &Handler{
		logger: zerolog.Nop(),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/operators/not-uuid/health", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-uuid")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.GetHealth(w, req)

	if w.Code != 400 {
		t.Errorf("GetHealth(invalid id) status = %d, want 400", w.Code)
	}
}

func TestTestConnectionInvalidID(t *testing.T) {
	h := &Handler{
		logger: zerolog.Nop(),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/operators/not-uuid/test", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-uuid")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.TestConnection(w, req)

	if w.Code != 400 {
		t.Errorf("TestConnection(invalid id) status = %d, want 400", w.Code)
	}
}

func TestHealthResponseStructure(t *testing.T) {
	resp := healthResponse{
		HealthStatus: "healthy",
		LatencyMs:    intPtr(15),
		CircuitState: "closed",
		Uptime24h:    99.5,
		FailureCount: 2,
	}

	if resp.HealthStatus != "healthy" {
		t.Errorf("HealthStatus = %q, want %q", resp.HealthStatus, "healthy")
	}
	if resp.LatencyMs == nil || *resp.LatencyMs != 15 {
		t.Error("LatencyMs should be 15")
	}
	if resp.Uptime24h != 99.5 {
		t.Errorf("Uptime24h = %f, want 99.5", resp.Uptime24h)
	}
	if resp.FailureCount != 2 {
		t.Errorf("FailureCount = %d, want 2", resp.FailureCount)
	}
}

func TestTestResponseStructure(t *testing.T) {
	resp := testResponse{
		Success:   true,
		LatencyMs: 42,
	}

	if !resp.Success {
		t.Error("Success should be true")
	}
	if resp.LatencyMs != 42 {
		t.Errorf("LatencyMs = %d, want 42", resp.LatencyMs)
	}
	if resp.Error != "" {
		t.Errorf("Error = %q, want empty", resp.Error)
	}

	errResp := testResponse{
		Success:   false,
		LatencyMs: 100,
		Error:     "connection refused",
	}
	if errResp.Success {
		t.Error("Success should be false")
	}
	if errResp.Error != "connection refused" {
		t.Errorf("Error = %q, want %q", errResp.Error, "connection refused")
	}
}

func intPtr(v int) *int {
	return &v
}

func TestGetHealthHistory_InvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	router := chi.NewRouter()
	router.Get("/operators/{id}/health-history", h.GetHealthHistory)

	req := httptest.NewRequest(http.MethodGet, "/operators/not-a-uuid/health-history", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGetHealthHistory_ValidHoursParam(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	router := chi.NewRouter()
	router.Get("/operators/{id}/health-history", h.GetHealthHistory)

	req := httptest.NewRequest(http.MethodGet, "/operators/"+uuid.New().String()+"/health-history?hours=invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status for invalid hours = %d, want 400", w.Code)
	}
}

func TestGetMetrics_MissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	router := chi.NewRouter()
	router.Get("/operators/{id}/metrics", h.GetMetrics)

	req := httptest.NewRequest(http.MethodGet, "/operators/"+uuid.New().String()+"/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestGetMetrics_InvalidWindow(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	router := chi.NewRouter()
	router.Get("/operators/{id}/metrics", h.GetMetrics)

	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, uuid.New())
	req := httptest.NewRequest(http.MethodGet, "/operators/"+uuid.New().String()+"/metrics?window=invalid", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
