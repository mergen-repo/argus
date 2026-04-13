package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func nopLogger() zerolog.Logger {
	return zerolog.Nop()
}

func TestGetDashboard_MissingTenant(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nopLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	w := httptest.NewRecorder()

	h.GetDashboard(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestTrafficHeatmapCell_JSONShape(t *testing.T) {
	cell := trafficHeatmapCell{Day: 3, Hour: 14, Value: 0.75}

	b, err := json.Marshal(cell)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"day", "hour", "value"} {
		if out[key] == nil {
			t.Errorf("field %q missing from trafficHeatmapCell JSON", key)
		}
	}

	if int(out["day"].(float64)) != 3 {
		t.Errorf("day = %v, want 3", out["day"])
	}
	if int(out["hour"].(float64)) != 14 {
		t.Errorf("hour = %v, want 14", out["hour"])
	}
}

func TestDashboardDTO_TrafficHeatmapField(t *testing.T) {
	data := dashboardDTO{
		TotalSIMs:      100,
		TrafficHeatmap: []trafficHeatmapCell{{Day: 0, Hour: 0, Value: 0.5}},
	}

	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal dashboardDTO: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := out["traffic_heatmap"]; !ok {
		t.Error("traffic_heatmap field missing from dashboardDTO JSON")
	}

	cells, ok := out["traffic_heatmap"].([]interface{})
	if !ok {
		t.Fatal("traffic_heatmap is not an array")
	}
	if len(cells) != 1 {
		t.Errorf("expected 1 cell, got %d", len(cells))
	}

	_ = apierr.TenantIDKey
	_ = uuid.Nil
}
