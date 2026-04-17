package dryrun

import (
	"encoding/json"
	"testing"

	"github.com/btopcu/argus/internal/policy/dsl"
)

func TestBuildFiltersFromMatch(t *testing.T) {
	svc := &Service{}

	tests := []struct {
		name     string
		compiled *dsl.CompiledPolicy
		wantOps  int
		wantAPNs int
		wantRATs int
	}{
		{
			name:     "nil compiled policy",
			compiled: nil,
			wantOps:  0,
			wantAPNs: 0,
			wantRATs: 0,
		},
		{
			name: "no match conditions",
			compiled: &dsl.CompiledPolicy{
				Match: dsl.CompiledMatch{
					Conditions: []dsl.CompiledMatchCondition{},
				},
			},
			wantOps:  0,
			wantAPNs: 0,
			wantRATs: 0,
		},
		{
			name: "apn and rat_type conditions",
			compiled: &dsl.CompiledPolicy{
				Match: dsl.CompiledMatch{
					Conditions: []dsl.CompiledMatchCondition{
						{
							Field:  "apn",
							Op:     "in",
							Values: []interface{}{"iot.meter", "m2m.fleet"},
						},
						{
							Field:  "rat_type",
							Op:     "eq",
							Value:  "4G",
						},
					},
				},
			},
			wantOps:  0,
			wantAPNs: 2,
			wantRATs: 1,
		},
		{
			name: "operator condition with uuid",
			compiled: &dsl.CompiledPolicy{
				Match: dsl.CompiledMatch{
					Conditions: []dsl.CompiledMatchCondition{
						{
							Field: "operator",
							Op:    "eq",
							Value: "550e8400-e29b-41d4-a716-446655440000",
						},
					},
				},
			},
			wantOps:  1,
			wantAPNs: 0,
			wantRATs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters := svc.buildFiltersFromMatch(tt.compiled)

			if len(filters.OperatorIDs) != tt.wantOps {
				t.Errorf("OperatorIDs count = %d, want %d", len(filters.OperatorIDs), tt.wantOps)
			}
			if len(filters.APNNames) != tt.wantAPNs {
				t.Errorf("APNNames count = %d, want %d", len(filters.APNNames), tt.wantAPNs)
			}
			if len(filters.RATTypes) != tt.wantRATs {
				t.Errorf("RATTypes count = %d, want %d", len(filters.RATTypes), tt.wantRATs)
			}
		})
	}
}

func TestDetectBehavioralChanges(t *testing.T) {
	tests := []struct {
		name    string
		before  *dsl.PolicyResult
		after   *dsl.PolicyResult
		wantLen int
		wantTypes []string
	}{
		{
			name:    "nil before",
			before:  nil,
			after:   &dsl.PolicyResult{},
			wantLen: 0,
		},
		{
			name:    "nil after",
			before:  &dsl.PolicyResult{},
			after:   nil,
			wantLen: 0,
		},
		{
			name: "no changes",
			before: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{"bandwidth_down": float64(10000000)},
			},
			after: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{"bandwidth_down": float64(10000000)},
			},
			wantLen: 0,
		},
		{
			name: "bandwidth downgrade",
			before: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{"bandwidth_down": float64(10000000)},
			},
			after: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{"bandwidth_down": float64(5000000)},
			},
			wantLen:   1,
			wantTypes: []string{"qos_downgrade"},
		},
		{
			name: "bandwidth upgrade",
			before: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{"bandwidth_down": float64(5000000)},
			},
			after: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{"bandwidth_down": float64(10000000)},
			},
			wantLen:   1,
			wantTypes: []string{"qos_upgrade"},
		},
		{
			name: "access denied",
			before: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{},
			},
			after: &dsl.PolicyResult{
				Allow:         false,
				QoSAttributes: map[string]interface{}{},
			},
			wantLen:   1,
			wantTypes: []string{"access_denied"},
		},
		{
			name: "new qos attribute added",
			before: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{},
			},
			after: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{"max_sessions": int64(5)},
			},
			wantLen:   1,
			wantTypes: []string{"qos_added"},
		},
		{
			name: "qos attribute removed",
			before: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{"max_sessions": int64(5)},
			},
			after: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{},
			},
			wantLen:   1,
			wantTypes: []string{"qos_removed"},
		},
		{
			name: "charging model change",
			before: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{},
				ChargingParams: &dsl.ChargingResult{
					Model:     "prepaid",
					RatePerMB: 0.01,
					Quota:     1073741824,
				},
			},
			after: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{},
				ChargingParams: &dsl.ChargingResult{
					Model:     "postpaid",
					RatePerMB: 0.02,
					Quota:     2147483648,
				},
			},
			wantLen: 3,
		},
		{
			name: "charging added",
			before: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{},
			},
			after: &dsl.PolicyResult{
				Allow:         true,
				QoSAttributes: map[string]interface{}{},
				ChargingParams: &dsl.ChargingResult{
					Model: "prepaid",
				},
			},
			wantLen:   1,
			wantTypes: []string{"charging_added"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := DetectBehavioralChanges(tt.before, tt.after)

			if len(changes) != tt.wantLen {
				t.Errorf("changes count = %d, want %d", len(changes), tt.wantLen)
				for _, ch := range changes {
					t.Logf("  change: type=%s field=%s desc=%s", ch.Type, ch.Field, ch.Description)
				}
				return
			}

			if tt.wantTypes != nil {
				for i, wantType := range tt.wantTypes {
					if i < len(changes) && changes[i].Type != wantType {
						t.Errorf("changes[%d].Type = %q, want %q", i, changes[i].Type, wantType)
					}
				}
			}
		})
	}
}

func TestDryRunResultJSON(t *testing.T) {
	result := &DryRunResult{
		VersionID:     "test-version-id",
		TotalAffected: 1500,
		ByOperator:    map[string]int{"Turkcell": 1000, "Vodafone": 500},
		ByAPN:         map[string]int{"iot.meter": 1500},
		ByRAT:         map[string]int{"4G": 1200, "NB-IoT": 300},
		BehavioralChanges: []BehavioralChange{
			{
				Type:          "qos_downgrade",
				Description:   "bandwidth_down reduced from 10Mbps to 5Mbps",
				AffectedCount: 500,
				Field:         "bandwidth_down",
				OldValue:      float64(10000000),
				NewValue:      float64(5000000),
			},
		},
		SampleSIMs:  []SampleSIM{},
		EvaluatedAt: "2026-03-21T10:00:00Z",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal DryRunResult: %v", err)
	}

	var decoded DryRunResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal DryRunResult: %v", err)
	}

	if decoded.TotalAffected != 1500 {
		t.Errorf("TotalAffected = %d, want 1500", decoded.TotalAffected)
	}
	if decoded.ByOperator["Turkcell"] != 1000 {
		t.Errorf("ByOperator[Turkcell] = %d, want 1000", decoded.ByOperator["Turkcell"])
	}
	if len(decoded.BehavioralChanges) != 1 {
		t.Errorf("BehavioralChanges count = %d, want 1", len(decoded.BehavioralChanges))
	}
	if decoded.BehavioralChanges[0].Type != "qos_downgrade" {
		t.Errorf("BehavioralChanges[0].Type = %q, want qos_downgrade", decoded.BehavioralChanges[0].Type)
	}
}

func TestClassifyQoSChange(t *testing.T) {
	tests := []struct {
		field    string
		oldVal   interface{}
		newVal   interface{}
		wantType string
	}{
		{"bandwidth_down", float64(10000000), float64(5000000), "qos_downgrade"},
		{"bandwidth_down", float64(5000000), float64(10000000), "qos_upgrade"},
		{"bandwidth_up", float64(1000000), float64(500000), "qos_downgrade"},
		{"max_sessions", int64(5), int64(10), "qos_upgrade"},
		{"priority", int64(3), int64(1), "qos_downgrade"},
		{"session_timeout", float64(3600), float64(7200), "qos_upgrade"},
		{"qos_class", int64(5), int64(9), "qos_change"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got := classifyQoSChange(tt.field, tt.oldVal, tt.newVal)
			if got != tt.wantType {
				t.Errorf("classifyQoSChange(%q, %v, %v) = %q, want %q", tt.field, tt.oldVal, tt.newVal, got, tt.wantType)
			}
		})
	}
}

func TestIsDSLError(t *testing.T) {
	dslErr := &DSLError{Message: "test error"}
	if !IsDSLError(dslErr) {
		t.Error("IsDSLError should return true for *DSLError")
	}

	var target any
	regularErr := json.Unmarshal([]byte("invalid"), &target)
	if IsDSLError(regularErr) {
		t.Error("IsDSLError should return false for non-DSLError")
	}
}

func TestAsyncThreshold(t *testing.T) {
	threshold := AsyncThreshold()
	if threshold != 100000 {
		t.Errorf("AsyncThreshold() = %d, want 100000", threshold)
	}
}
