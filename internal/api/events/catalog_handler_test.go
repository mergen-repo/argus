package events

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func TestEventsCatalogHandler_List_ReturnsCatalog(t *testing.T) {
	h := NewHandler(zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/catalog", nil)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		Status string `json:"status"`
		Data   struct {
			Events []CatalogEntry `json:"events"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
	if len(resp.Data.Events) < 14 {
		t.Errorf("catalog entries = %d, want >= 14", len(resp.Data.Events))
	}
	// Spot-check one entry.
	var found bool
	for _, e := range resp.Data.Events {
		if e.Type == "session.started" {
			found = true
			if e.DefaultSeverity != "info" {
				t.Errorf("session.started default_severity = %q, want info", e.DefaultSeverity)
			}
			if e.EntityType != "sim" {
				t.Errorf("session.started entity_type = %q, want sim", e.EntityType)
			}
		}
	}
	if !found {
		t.Error("session.started entry missing from catalog response")
	}
}
