package store

import (
	"encoding/json"
	"testing"
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
