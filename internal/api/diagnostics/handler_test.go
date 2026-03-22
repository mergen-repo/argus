package diagnostics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	diag "github.com/btopcu/argus/internal/diagnostics"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestDiagnose_InvalidSimID(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/invalid/diagnose", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "invalid")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = req.WithContext(context.WithValue(req.Context(), apierr.TenantIDKey, uuid.New()))

	w := httptest.NewRecorder()
	h.Diagnose(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want=%d", w.Code, http.StatusBadRequest)
	}
}

func TestDiagnose_MissingTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/"+uuid.New().String()+"/diagnose", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuid.New().String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.Diagnose(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want=%d", w.Code, http.StatusBadRequest)
	}
}

func TestDiagnose_InvalidRequestBody(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())

	body := strings.NewReader("{invalid json")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/"+uuid.New().String()+"/diagnose", body)
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuid.New().String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = req.WithContext(context.WithValue(req.Context(), apierr.TenantIDKey, uuid.New()))

	w := httptest.NewRecorder()
	h.Diagnose(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want=%d", w.Code, http.StatusBadRequest)
	}
}

func TestDiagnosticResponseStructure(t *testing.T) {
	result := diag.DiagnosticResult{
		SimID:         uuid.New().String(),
		OverallStatus: diag.OverallPass,
		Steps: []diag.StepResult{
			{Step: 1, Name: "SIM State", Status: diag.StatusPass, Message: "SIM is active"},
			{Step: 2, Name: "Last Authentication", Status: diag.StatusPass, Message: "OK"},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["overall_status"] != "PASS" {
		t.Errorf("overall_status=%v, want=PASS", m["overall_status"])
	}

	steps, ok := m["steps"].([]interface{})
	if !ok {
		t.Fatal("steps not array")
	}
	if len(steps) != 2 {
		t.Errorf("steps len=%d, want=2", len(steps))
	}

	s1 := steps[0].(map[string]interface{})
	if s1["step"].(float64) != 1 {
		t.Error("step 1 number mismatch")
	}
	if s1["name"] != "SIM State" {
		t.Error("step 1 name mismatch")
	}
	if s1["status"] != "pass" {
		t.Error("step 1 status mismatch")
	}
}

func TestCacheKeyFormat(t *testing.T) {
	tenantID := uuid.New()
	simID := uuid.New()
	key := cacheKeyPrefix + tenantID.String() + ":" + simID.String() + ":false"
	if !strings.HasPrefix(key, "diag:") {
		t.Errorf("key=%q, want prefix 'diag:'", key)
	}
	if !strings.Contains(key, tenantID.String()) {
		t.Error("key should contain tenant ID")
	}
	if !strings.Contains(key, simID.String()) {
		t.Error("key should contain sim ID")
	}
}
