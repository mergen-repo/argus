package store

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestSegmentFilterParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid full filter",
			input:   `{"operator_id":"550e8400-e29b-41d4-a716-446655440000","state":"active","apn_id":"660e8400-e29b-41d4-a716-446655440000","rat_type":"lte_m"}`,
			wantErr: false,
		},
		{
			name:    "partial filter state only",
			input:   `{"state":"suspended"}`,
			wantErr: false,
		},
		{
			name:    "empty filter",
			input:   `{}`,
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `{invalid}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var filter SegmentFilter
			err := json.Unmarshal([]byte(tc.input), &filter)
			if (err != nil) != tc.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestBuildFilterConditions(t *testing.T) {
	opID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	apnID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440000")
	tenantID := uuid.MustParse("770e8400-e29b-41d4-a716-446655440000")

	tests := []struct {
		name           string
		filter         SegmentFilter
		wantConditions int
		wantArgs       int
	}{
		{
			name:           "empty filter",
			filter:         SegmentFilter{},
			wantConditions: 1,
			wantArgs:       1,
		},
		{
			name:           "all fields set",
			filter:         SegmentFilter{OperatorID: &opID, State: "active", APNID: &apnID, RATType: "lte_m"},
			wantConditions: 5,
			wantArgs:       5,
		},
		{
			name:           "operator only",
			filter:         SegmentFilter{OperatorID: &opID},
			wantConditions: 2,
			wantArgs:       2,
		},
		{
			name:           "state only",
			filter:         SegmentFilter{State: "suspended"},
			wantConditions: 2,
			wantArgs:       2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conditions, args, _ := buildFilterConditions(&tc.filter, tenantID)
			if len(conditions) != tc.wantConditions {
				t.Errorf("conditions count = %d, want %d", len(conditions), tc.wantConditions)
			}
			if len(args) != tc.wantArgs {
				t.Errorf("args count = %d, want %d", len(args), tc.wantArgs)
			}
		})
	}
}

func TestStateSummaryResultMarshal(t *testing.T) {
	byState := map[string]int64{
		"active":    1000,
		"suspended": 200,
		"ordered":   50,
	}

	data, err := json.Marshal(byState)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]int64
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if result["active"] != 1000 {
		t.Errorf("active = %d, want 1000", result["active"])
	}
	if result["suspended"] != 200 {
		t.Errorf("suspended = %d, want 200", result["suspended"])
	}
	if result["ordered"] != 50 {
		t.Errorf("ordered = %d, want 50", result["ordered"])
	}
}

func TestSegmentFilterFields(t *testing.T) {
	input := `{"operator_id":"550e8400-e29b-41d4-a716-446655440000","state":"active","rat_type":"lte_m"}`

	var filter SegmentFilter
	if err := json.Unmarshal([]byte(input), &filter); err != nil {
		t.Fatal(err)
	}

	if filter.OperatorID == nil {
		t.Error("expected OperatorID to be set")
	}
	if filter.State != "active" {
		t.Errorf("State = %q, want %q", filter.State, "active")
	}
	if filter.RATType != "lte_m" {
		t.Errorf("RATType = %q, want %q", filter.RATType, "lte_m")
	}
	if filter.APNID != nil {
		t.Error("expected APNID to be nil")
	}
}
