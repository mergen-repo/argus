package system

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
)

func TestCapacityHandler_MissingTenant(t *testing.T) {
	h := NewCapacityHandler(
		CapacityConfig{SIMs: 15_000_000, Sessions: 2_000_000, AuthPerSec: 5_000, MonthlyGrowth: 72_000},
		nil, nil, nil, nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/capacity", nil)
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestCapacityHandler_ConfigDefaults(t *testing.T) {
	cfg := CapacityConfig{
		SIMs:          15_000_000,
		Sessions:      2_000_000,
		AuthPerSec:    5_000,
		MonthlyGrowth: 72_000,
	}

	if cfg.SIMs != 15_000_000 {
		t.Errorf("CapacitySIMs = %d, want 15000000", cfg.SIMs)
	}
	if cfg.Sessions != 2_000_000 {
		t.Errorf("CapacitySessions = %d, want 2000000", cfg.Sessions)
	}
	if cfg.AuthPerSec != 5_000 {
		t.Errorf("CapacityAuthPerSec = %d, want 5000", cfg.AuthPerSec)
	}
}

func TestCapacityData_Marshaling(t *testing.T) {
	tenantID := uuid.New()
	_ = tenantID

	data := capacityData{
		TotalSIMs:         10_200_000,
		ActiveSessions:    847_000,
		AuthPerSec:        1247.0,
		SIMCapacity:       15_000_000,
		SessionCapacity:   2_000_000,
		AuthCapacity:      5_000,
		MonthlyGrowthSIMs: 72_000,
		IPPools:           []capacityIPPool{},
	}

	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal capacityData: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	requiredKeys := []string{"total_sims", "active_sessions", "sim_capacity", "session_capacity", "auth_capacity", "monthly_growth_sims", "ip_pools"}
	for _, k := range requiredKeys {
		if _, ok := out[k]; !ok {
			t.Errorf("response missing field %q", k)
		}
	}

	_ = apierr.CodeForbidden
}
