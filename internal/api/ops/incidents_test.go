package ops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/rs/zerolog"
)

func TestParseIncidentFilters_Defaults(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	f, err := parseIncidentFilters(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.limit != 50 {
		t.Errorf("expected default limit=50, got %d", f.limit)
	}
	if f.from != nil || f.to != nil {
		t.Error("expected nil from/to by default")
	}
}

func TestParseIncidentFilters_ValidRange(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?from=2026-04-01T00:00:00Z&to=2026-04-15T23:59:59Z&severity=high&limit=20", nil)
	f, err := parseIncidentFilters(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.limit != 20 {
		t.Errorf("expected limit=20, got %d", f.limit)
	}
	if f.severity != "high" {
		t.Errorf("expected severity=high, got %q", f.severity)
	}
	if f.from == nil {
		t.Error("expected non-nil from")
	} else if f.from.Year() != 2026 {
		t.Errorf("expected from year 2026, got %d", f.from.Year())
	}
}

func TestParseIncidentFilters_InvalidFrom(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?from=not-a-date", nil)
	_, err := parseIncidentFilters(r)
	if err == nil {
		t.Error("expected error for invalid from date")
	}
}

func TestParseIncidentFilters_LimitCap(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?limit=999", nil)
	f, err := parseIncidentFilters(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.limit != 50 {
		t.Errorf("limit over 200 should fall back to default 50, got %d", f.limit)
	}
}

func TestIncidents_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())
	r := httptest.NewRequest(http.MethodGet, "/api/v1/ops/incidents", nil)
	w := httptest.NewRecorder()
	h.Incidents(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestIncidents_SuperAdminNoTenant(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())
	r := httptest.NewRequest(http.MethodGet, "/api/v1/ops/incidents", nil)
	ctx := context.WithValue(r.Context(), apierr.RoleKey, "super_admin")
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Incidents(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("super_admin should get 200, got %d", w.Code)
	}
}

func TestBuildIncidentTimeline_Empty(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	filters := incidentFilters{limit: 50}
	events := h.buildIncidentTimeline(r, nil, filters)
	if len(events) != 0 {
		t.Errorf("expected empty events with no stores, got %d", len(events))
	}
}

// TestIncidentTimeline_SortOrder verifies that the inline sort.Slice in
// buildIncidentTimeline produces strict descending chronological order.
// We replicate the same comparator here to guard against regressions in
// the timestamp parsing / comparator direction.
func TestIncidentTimeline_SortOrder(t *testing.T) {
	events := []incidentEvent{
		{TS: "2026-04-01T10:00:00Z", Action: "detected"},
		{TS: "2026-04-01T12:00:00Z", Action: "acknowledged"},
		{TS: "2026-04-01T11:00:00Z", Action: "resolved"},
	}

	sortIncidents(events)

	for i := 0; i < len(events)-1; i++ {
		ti, _ := time.Parse(time.RFC3339, events[i].TS)
		tj, _ := time.Parse(time.RFC3339, events[i+1].TS)
		if !ti.After(tj) {
			t.Fatalf("expected DESC order: events[%d]=%s should be after events[%d]=%s",
				i, events[i].TS, i+1, events[i+1].TS)
		}
	}

	if events[0].Action != "acknowledged" {
		t.Errorf("expected newest action 'acknowledged' first, got %q", events[0].Action)
	}
	if events[2].Action != "detected" {
		t.Errorf("expected oldest action 'detected' last, got %q", events[2].Action)
	}
}
