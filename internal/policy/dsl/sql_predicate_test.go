package dsl

import (
	"reflect"
	"strings"
	"testing"
)

func TestToSQLPredicate(t *testing.T) {
	tests := []struct {
		name         string
		match        *CompiledMatch
		tenantArgIdx int
		startArgIdx  int
		wantSQL      string
		wantArgs     []interface{}
		wantNextIdx  int
		wantErr      bool
		errSubstr    string
	}{
		{
			name: "apn eq",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "apn", Op: "eq", Value: "data.demo"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.apn_id = (SELECT id FROM apns WHERE tenant_id = $1 AND name = $2)",
			wantArgs:     []interface{}{"data.demo"},
			wantNextIdx:  3,
		},
		{
			name: "apn IN list",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "apn", Op: "in", Values: []interface{}{"a", "b"}},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.apn_id IN (SELECT id FROM apns WHERE tenant_id = $1 AND name = ANY($2))",
			wantArgs:     []interface{}{[]string{"a", "b"}},
			wantNextIdx:  3,
		},
		{
			name: "operator eq",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "operator", Op: "eq", Value: "turkcell"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.operator_id = (SELECT id FROM operators WHERE code = $2)",
			wantArgs:     []interface{}{"turkcell"},
			wantNextIdx:  3,
		},
		{
			name: "operator IN list",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "operator", Op: "in", Values: []interface{}{"turkcell", "vodafone_tr"}},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.operator_id IN (SELECT id FROM operators WHERE code = ANY($2))",
			wantArgs:     []interface{}{[]string{"turkcell", "vodafone_tr"}},
			wantNextIdx:  3,
		},
		{
			name: "imsi_prefix eq appends %",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "imsi_prefix", Op: "eq", Value: "28601"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.imsi LIKE $2",
			wantArgs:     []interface{}{"28601%"},
			wantNextIdx:  3,
		},
		{
			name: "imsi_prefix IN expands to OR-LIKE",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "imsi_prefix", Op: "in", Values: []interface{}{"28601", "28602"}},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "(s.imsi LIKE $2 OR s.imsi LIKE $3)",
			wantArgs:     []interface{}{"28601%", "28602%"},
			wantNextIdx:  4,
		},
		{
			name: "rat_type eq",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "rat_type", Op: "eq", Value: "lte"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.rat_type = $2",
			wantArgs:     []interface{}{"lte"},
			wantNextIdx:  3,
		},
		{
			name: "sim_type eq",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "sim_type", Op: "eq", Value: "physical"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.sim_type = $2",
			wantArgs:     []interface{}{"physical"},
			wantNextIdx:  3,
		},
		{
			name: "rat_type IN list uses ANY",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "rat_type", Op: "in", Values: []interface{}{"lte", "nr_5g"}},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.rat_type = ANY($2)",
			wantArgs:     []interface{}{[]string{"lte", "nr_5g"}},
			wantNextIdx:  3,
		},
		{
			name: "sim_type IN list uses ANY",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "sim_type", Op: "in", Values: []interface{}{"physical", "esim"}},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.sim_type = ANY($2)",
			wantArgs:     []interface{}{[]string{"physical", "esim"}},
			wantNextIdx:  3,
		},
		{
			name: "apn neq wraps with NOT",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "apn", Op: "neq", Value: "data.demo"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "NOT (s.apn_id = (SELECT id FROM apns WHERE tenant_id = $1 AND name = $2))",
			wantArgs:     []interface{}{"data.demo"},
			wantNextIdx:  3,
		},
		{
			name: "rat_type neq uses <>",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "rat_type", Op: "neq", Value: "lte"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.rat_type <> $2",
			wantArgs:     []interface{}{"lte"},
			wantNextIdx:  3,
		},
		{
			name: "imsi_prefix neq uses NOT LIKE",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "imsi_prefix", Op: "neq", Value: "28601"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.imsi NOT LIKE $2",
			wantArgs:     []interface{}{"28601%"},
			wantNextIdx:  3,
		},
		// FIX-230 Gate F-B1: matrix coverage for operator-neq and sim_type-neq.
		{
			name: "operator neq wraps with NOT",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "operator", Op: "neq", Value: "turkcell"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "NOT (s.operator_id = (SELECT id FROM operators WHERE code = $2))",
			wantArgs:     []interface{}{"turkcell"},
			wantNextIdx:  3,
		},
		{
			name: "sim_type neq uses <>",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "sim_type", Op: "neq", Value: "esim"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.sim_type <> $2",
			wantArgs:     []interface{}{"esim"},
			wantNextIdx:  3,
		},
		{
			name: "compound apn AND operator",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "apn", Op: "eq", Value: "x"},
				{Field: "operator", Op: "eq", Value: "y"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.apn_id = (SELECT id FROM apns WHERE tenant_id = $1 AND name = $2) AND s.operator_id = (SELECT id FROM operators WHERE code = $3)",
			wantArgs:     []interface{}{"x", "y"},
			wantNextIdx:  4,
		},
		{
			name:         "empty match returns TRUE",
			match:        &CompiledMatch{},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "TRUE",
			wantArgs:     nil,
			wantNextIdx:  2,
		},
		{
			name:         "nil match returns TRUE",
			match:        nil,
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "TRUE",
			wantArgs:     nil,
			wantNextIdx:  2,
		},
		{
			name: "unknown field rejected",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "foo", Op: "eq", Value: "bar"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantErr:      true,
			errSubstr:    "field \"foo\" not allowed",
		},
		{
			name: "unknown field rejected (in)",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "secret_col", Op: "in", Values: []interface{}{"a"}},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantErr:      true,
			errSubstr:    "field \"secret_col\" not allowed",
		},
		{
			name: "unknown op rejected",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "apn", Op: "regex", Value: "^x.*"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantErr:      true,
			errSubstr:    "operator \"regex\" not allowed",
		},
		{
			name: "imsi_prefix non-string rejected",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "imsi_prefix", Op: "eq", Value: int64(123)},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantErr:      true,
			errSubstr:    "imsi_prefix value must be string",
		},
		{
			name: "apn IN with non-string element rejected",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "apn", Op: "in", Values: []interface{}{"a", int64(7)}},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantErr:      true,
			errSubstr:    "must be string",
		},
		{
			name: "empty IN list rejected",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "apn", Op: "in", Values: nil},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantErr:      true,
			errSubstr:    "IN list",
		},
		{
			name: "arg numbering chains across multiple clauses",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "apn", Op: "eq", Value: "iot.fleet"},
				{Field: "imsi_prefix", Op: "in", Values: []interface{}{"28601", "28602"}},
				{Field: "rat_type", Op: "eq", Value: "lte"},
			}},
			tenantArgIdx: 1,
			startArgIdx:  2,
			wantSQL:      "s.apn_id = (SELECT id FROM apns WHERE tenant_id = $1 AND name = $2) AND (s.imsi LIKE $3 OR s.imsi LIKE $4) AND s.rat_type = $5",
			wantArgs:     []interface{}{"iot.fleet", "28601%", "28602%", "lte"},
			wantNextIdx:  6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotArgs, gotIdx, err := ToSQLPredicate(tt.match, tt.tenantArgIdx, tt.startArgIdx)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil; sql=%q", tt.errSubstr, gotSQL)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotSQL != tt.wantSQL {
				t.Errorf("sql mismatch\n  got:  %q\n  want: %q", gotSQL, tt.wantSQL)
			}
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Errorf("args mismatch\n  got:  %#v\n  want: %#v", gotArgs, tt.wantArgs)
			}
			if gotIdx != tt.wantNextIdx {
				t.Errorf("nextArgIdx: got %d want %d", gotIdx, tt.wantNextIdx)
			}
		})
	}
}

// TestToSQLPredicate_InjectionProbe is the AC-9 SQL injection defense check.
// A malicious value like `x' OR 1=1 --` MUST flow through to the args slice
// untouched, and MUST NOT appear inline in the produced SQL fragment.
func TestToSQLPredicate_InjectionProbe(t *testing.T) {
	probe := "x' OR 1=1 --"

	cases := []struct {
		name  string
		match *CompiledMatch
	}{
		{
			name: "apn eq",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "apn", Op: "eq", Value: probe},
			}},
		},
		{
			name: "operator eq",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "operator", Op: "eq", Value: probe},
			}},
		},
		{
			name: "rat_type eq",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "rat_type", Op: "eq", Value: probe},
			}},
		},
		{
			name: "sim_type eq",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "sim_type", Op: "eq", Value: probe},
			}},
		},
		{
			name: "imsi_prefix eq",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "imsi_prefix", Op: "eq", Value: probe},
			}},
		},
		{
			name: "apn IN",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "apn", Op: "in", Values: []interface{}{probe, "ok"}},
			}},
		},
		{
			name: "imsi_prefix IN",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "imsi_prefix", Op: "in", Values: []interface{}{probe, "28601"}},
			}},
		},
		// FIX-230 Gate F-B2: IN-list injection probes for operator, rat_type, sim_type.
		{
			name: "operator IN",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "operator", Op: "in", Values: []interface{}{probe, "turkcell"}},
			}},
		},
		{
			name: "rat_type IN",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "rat_type", Op: "in", Values: []interface{}{probe, "lte"}},
			}},
		},
		{
			name: "sim_type IN",
			match: &CompiledMatch{Conditions: []CompiledMatchCondition{
				{Field: "sim_type", Op: "in", Values: []interface{}{probe, "physical"}},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sqlFrag, args, _, err := ToSQLPredicate(tc.match, 1, 2)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(sqlFrag, "'") {
				t.Errorf("SQL fragment contains a single quote — value inlined? frag=%q", sqlFrag)
			}
			if strings.Contains(sqlFrag, "--") {
				t.Errorf("SQL fragment contains '--' — value inlined? frag=%q", sqlFrag)
			}
			if strings.Contains(sqlFrag, "OR 1=1") {
				t.Errorf("SQL fragment contains injection payload — value inlined? frag=%q", sqlFrag)
			}
			// Probe value must reach the args slice somewhere (possibly suffixed
			// with %% for imsi_prefix, or wrapped in []string for IN lists).
			if !argsContainsProbe(args, probe) {
				t.Errorf("probe value %q not found in args slice; args=%#v", probe, args)
			}
		})
	}
}

func argsContainsProbe(args []interface{}, probe string) bool {
	for _, a := range args {
		switch v := a.(type) {
		case string:
			if v == probe || strings.HasPrefix(v, probe) {
				return true
			}
		case []string:
			for _, s := range v {
				if s == probe || strings.HasPrefix(s, probe) {
					return true
				}
			}
		}
	}
	return false
}
