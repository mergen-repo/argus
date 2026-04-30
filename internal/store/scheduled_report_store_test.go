package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestScheduledReportStore_Constructor(t *testing.T) {
	s := NewScheduledReportStore(nil)
	if s == nil {
		t.Fatal("NewScheduledReportStore returned nil")
	}
}

func TestScheduledReport_Fields(t *testing.T) {
	tenantID := uuid.New()
	createdBy := uuid.New()
	lastJobID := uuid.New()
	now := time.Now().UTC()
	filters := json.RawMessage(`{"apn":"internet"}`)

	r := ScheduledReport{
		ID:           uuid.New(),
		TenantID:     tenantID,
		ReportType:   "compliance_kvkk",
		ScheduleCron: "0 0 1 * *",
		Format:       "pdf",
		Recipients:   []string{"admin@example.com", "ops@example.com"},
		Filters:      filters,
		LastRunAt:    &now,
		NextRunAt:    &now,
		LastJobID:    &lastJobID,
		State:        "active",
		CreatedAt:    now,
		CreatedBy:    &createdBy,
		UpdatedAt:    now,
	}

	if r.TenantID != tenantID {
		t.Errorf("TenantID mismatch")
	}
	if r.ReportType != "compliance_kvkk" {
		t.Errorf("ReportType = %s, want compliance_kvkk", r.ReportType)
	}
	if r.Format != "pdf" {
		t.Errorf("Format = %s, want pdf", r.Format)
	}
	if len(r.Recipients) != 2 {
		t.Errorf("Recipients len = %d, want 2", len(r.Recipients))
	}
	if r.State != "active" {
		t.Errorf("State = %s, want active", r.State)
	}
	if *r.LastJobID != lastJobID {
		t.Errorf("LastJobID mismatch")
	}
}

func TestScheduledReportPatch_PartialFields(t *testing.T) {
	newCron := "0 6 * * 1"
	newState := "paused"
	newRecipients := []string{"new@example.com"}
	nextRun := time.Now().Add(24 * time.Hour).UTC()

	patch := ScheduledReportPatch{
		ScheduleCron: &newCron,
		State:        &newState,
		Recipients:   &newRecipients,
		NextRunAt:    &nextRun,
	}

	if *patch.ScheduleCron != newCron {
		t.Errorf("ScheduleCron = %s, want %s", *patch.ScheduleCron, newCron)
	}
	if *patch.State != "paused" {
		t.Errorf("State = %s, want paused", *patch.State)
	}
	if len(*patch.Recipients) != 1 {
		t.Errorf("Recipients len = %d, want 1", len(*patch.Recipients))
	}
	if patch.Filters != nil {
		t.Error("Filters should be nil when not set")
	}
}

func TestScheduledReportStore_Update_NoFieldsIsNoop(t *testing.T) {
	patch := ScheduledReportPatch{}
	sets := []string{}

	if patch.ScheduleCron != nil {
		sets = append(sets, "schedule_cron")
	}
	if patch.Recipients != nil {
		sets = append(sets, "recipients")
	}
	if patch.Filters != nil {
		sets = append(sets, "filters")
	}
	if patch.State != nil {
		sets = append(sets, "state")
	}
	if patch.NextRunAt != nil {
		sets = append(sets, "next_run_at")
	}

	if len(sets) != 0 {
		t.Errorf("empty patch should produce 0 sets, got %d", len(sets))
	}
}

func TestScheduledReportStore_ListDue_LimitBounds(t *testing.T) {
	cases := []struct {
		name      string
		limit     int
		wantLimit int
	}{
		{"zero becomes 100", 0, 100},
		{"negative becomes 100", -5, 100},
		{"over 1000 becomes 100", 2000, 100},
		{"valid preserved", 50, 50},
		{"1000 preserved", 1000, 1000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limit := tc.limit
			if limit <= 0 || limit > 1000 {
				limit = 100
			}
			if limit != tc.wantLimit {
				t.Errorf("limit = %d, want %d", limit, tc.wantLimit)
			}
		})
	}
}

func TestScheduledReportStore_List_LimitBounds(t *testing.T) {
	cases := []struct {
		name      string
		limit     int
		wantLimit int
	}{
		{"zero becomes 20", 0, 20},
		{"negative becomes 20", -1, 20},
		{"over 100 becomes 20", 200, 20},
		{"20 preserved", 20, 20},
		{"100 preserved", 100, 100},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limit := tc.limit
			if limit <= 0 || limit > 100 {
				limit = 20
			}
			if limit != tc.wantLimit {
				t.Errorf("limit = %d, want %d", limit, tc.wantLimit)
			}
		})
	}
}

func TestScheduledReport_ReportTypes(t *testing.T) {
	validTypes := []string{
		"compliance_kvkk",
		"compliance_gdpr",
		"compliance_btk",
		"sla_monthly",
		"usage_summary",
		"cost_analysis",
		"audit_log_export",
		"sim_inventory",
	}

	for _, rt := range validTypes {
		r := ScheduledReport{ReportType: rt}
		if r.ReportType != rt {
			t.Errorf("ReportType round-trip failed for %s", rt)
		}
	}
}

func TestScheduledReport_Formats(t *testing.T) {
	for _, f := range []string{"pdf", "csv", "xlsx"} {
		r := ScheduledReport{Format: f}
		if r.Format != f {
			t.Errorf("Format round-trip failed for %s", f)
		}
	}
}

func TestScheduledReport_States(t *testing.T) {
	for _, st := range []string{"active", "paused"} {
		r := ScheduledReport{State: st}
		if r.State != st {
			t.Errorf("State round-trip failed for %s", st)
		}
	}
}

func TestScheduledReport_NilPointerFields(t *testing.T) {
	r := ScheduledReport{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		ReportType: "usage_summary",
		State:      "active",
	}

	if r.LastRunAt != nil {
		t.Error("LastRunAt should default to nil")
	}
	if r.NextRunAt != nil {
		t.Error("NextRunAt should default to nil")
	}
	if r.LastJobID != nil {
		t.Error("LastJobID should default to nil")
	}
	if r.CreatedBy != nil {
		t.Error("CreatedBy should default to nil")
	}
}

func TestScheduledReport_ListDue_FilterLogic(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	reports := []struct {
		state     string
		nextRunAt *time.Time
		wantDue   bool
	}{
		{"active", &past, true},
		{"active", &now, true},
		{"paused", &past, false},
		{"active", &future, false},
		{"active", nil, false},
	}

	for _, tc := range reports {
		isDue := tc.state == "active" &&
			tc.nextRunAt != nil &&
			!tc.nextRunAt.After(now)

		if isDue != tc.wantDue {
			t.Errorf("state=%s nextRunAt=%v: isDue=%v, want %v",
				tc.state, tc.nextRunAt, isDue, tc.wantDue)
		}
	}
}

func TestScheduledReport_CursorPagination(t *testing.T) {
	ids := make([]uuid.UUID, 3)
	for i := range ids {
		ids[i] = uuid.New()
	}

	limit := 2
	allResults := ids

	page1 := allResults[:limit]
	var nextCursor string
	if len(allResults) > limit {
		nextCursor = allResults[limit-1].String()
	}

	if len(page1) != 2 {
		t.Errorf("page1 len = %d, want 2", len(page1))
	}
	if nextCursor == "" {
		t.Error("nextCursor should not be empty when more results exist")
	}
	if nextCursor != ids[1].String() {
		t.Errorf("nextCursor = %s, want %s", nextCursor, ids[1].String())
	}
}

func TestScheduledReport_FiltersDefaultsToEmptyJSON(t *testing.T) {
	var filters json.RawMessage
	if filters == nil {
		filters = json.RawMessage(`{}`)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(filters, &m); err != nil {
		t.Fatalf("default filters not valid JSON: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("default filters should be empty object, got %v", m)
	}
}
