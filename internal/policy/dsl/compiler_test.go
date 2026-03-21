package dsl

import (
	"encoding/json"
	"testing"
)

func compileSource(src string) (*CompiledPolicy, error) {
	policy, errs := Parse(src)
	for _, e := range errs {
		if e.Severity == "error" {
			return nil, &e
		}
	}
	return CompileAST(policy)
}

func TestCompiler_ASTToJSONRuleTree(t *testing.T) {
	src := `POLICY "iot-fleet-standard" {
    MATCH {
        apn IN ("iot.fleet", "iot.meter")
        rat_type IN (nb_iot, lte_m)
    }

    RULES {
        bandwidth_down = 1mbps
        bandwidth_up = 256kbps
        session_timeout = 24h
        idle_timeout = 1h
        max_sessions = 1

        WHEN usage > 800MB {
            ACTION notify(quota_warning, 80%)
        }

        WHEN usage > 1GB {
            bandwidth_down = 64kbps
            bandwidth_up = 32kbps
            ACTION notify(quota_exceeded, 100%)
            ACTION log("FUP throttle applied")
        }
    }

    CHARGING {
        model = postpaid
        rate_per_mb = 0.01
        billing_cycle = monthly
        quota = 1GB
        overage_action = throttle

        rat_type_multiplier {
            nb_iot = 0.5
            lte_m = 1.0
            lte = 2.0
            nr_5g = 3.0
        }
    }
}`

	compiled, err := compileSource(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if compiled.Name != "iot-fleet-standard" {
		t.Errorf("expected name 'iot-fleet-standard', got %q", compiled.Name)
	}

	if len(compiled.Match.Conditions) != 2 {
		t.Errorf("expected 2 match conditions, got %d", len(compiled.Match.Conditions))
	}

	if compiled.Match.Conditions[0].Op != "in" {
		t.Errorf("expected op 'in', got %q", compiled.Match.Conditions[0].Op)
	}

	if compiled.Rules.Defaults["bandwidth_down"] != float64(1000000) {
		t.Errorf("expected bandwidth_down 1000000, got %v", compiled.Rules.Defaults["bandwidth_down"])
	}

	if compiled.Rules.Defaults["bandwidth_up"] != float64(256000) {
		t.Errorf("expected bandwidth_up 256000, got %v", compiled.Rules.Defaults["bandwidth_up"])
	}

	if compiled.Rules.Defaults["session_timeout"] != float64(86400) {
		t.Errorf("expected session_timeout 86400, got %v", compiled.Rules.Defaults["session_timeout"])
	}

	if compiled.Rules.Defaults["idle_timeout"] != float64(3600) {
		t.Errorf("expected idle_timeout 3600, got %v", compiled.Rules.Defaults["idle_timeout"])
	}

	if len(compiled.Rules.WhenBlocks) != 2 {
		t.Fatalf("expected 2 when blocks, got %d", len(compiled.Rules.WhenBlocks))
	}

	if compiled.Charging == nil {
		t.Fatal("expected charging block")
	}
	if compiled.Charging.Model != "postpaid" {
		t.Errorf("expected model 'postpaid', got %q", compiled.Charging.Model)
	}
	if compiled.Charging.Quota != 1073741824 {
		t.Errorf("expected quota 1073741824, got %d", compiled.Charging.Quota)
	}
}

func TestCompiler_UnitNormalization(t *testing.T) {
	tests := []struct {
		name     string
		val      float64
		unit     string
		expected float64
	}{
		{"1mbps", 1, "mbps", 1000000},
		{"256kbps", 256, "kbps", 256000},
		{"1GB", 1, "gb", 1073741824},
		{"500MB", 500, "mb", 500 * 1048576},
		{"24h", 24, "h", 86400},
		{"1d", 1, "d", 86400},
		{"60min", 60, "min", 3600},
		{"1KB", 1, "kb", 1024},
		{"1TB", 1, "tb", 1099511627776},
		{"1gbps", 1, "gbps", 1000000000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeWithUnit(tt.val, tt.unit)
			if result != tt.expected {
				t.Errorf("normalizeWithUnit(%f, %q) = %f, want %f", tt.val, tt.unit, result, tt.expected)
			}
		})
	}
}

func TestCompiler_OperatorNormalization(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{">", "gt"},
		{">=", "gte"},
		{"<", "lt"},
		{"<=", "lte"},
		{"=", "eq"},
		{"!=", "neq"},
		{"IN", "in"},
		{"BETWEEN", "between"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeOp(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeOp(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCompiler_CompoundCondition(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {
        WHEN usage > 500MB AND rat_type = lte {
            bandwidth_down = 64kbps
        }
    }
}`

	compiled, err := compileSource(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if len(compiled.Rules.WhenBlocks) != 1 {
		t.Fatal("expected 1 when block")
	}

	cond := compiled.Rules.WhenBlocks[0].Condition
	if cond.Op != "and" {
		t.Errorf("expected op 'and', got %q", cond.Op)
	}
	if cond.Left == nil || cond.Right == nil {
		t.Fatal("expected left and right conditions")
	}
}

func TestCompiler_OptionalChargingBlock(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {}
}`

	compiled, err := compileSource(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if compiled.Charging != nil {
		t.Error("expected nil charging when omitted")
	}
}

func TestCompiler_JSONSerialization(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
    RULES {
        bandwidth_down = 1mbps
    }
}`

	compiled, err := compileSource(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	compiler := NewCompiler()
	data, err := compiler.ToJSON(compiled)
	if err != nil {
		t.Fatalf("json error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if parsed["name"] != "test" {
		t.Errorf("expected name 'test', got %v", parsed["name"])
	}
}

func TestCompiler_ChargingWithRATMultiplier(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {}
    CHARGING {
        model = postpaid
        rate_per_mb = 0.01
        rat_type_multiplier {
            nb_iot = 0.5
            lte_m = 1.0
            lte = 2.0
            nr_5g = 3.0
        }
    }
}`

	compiled, err := compileSource(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if compiled.Charging == nil {
		t.Fatal("expected charging block")
	}
	if compiled.Charging.RATMultiplier == nil {
		t.Fatal("expected RAT multiplier map")
	}
	if compiled.Charging.RATMultiplier["nb_iot"] != 0.5 {
		t.Errorf("expected nb_iot 0.5, got %f", compiled.Charging.RATMultiplier["nb_iot"])
	}
	if compiled.Charging.RATMultiplier["nr_5g"] != 3.0 {
		t.Errorf("expected nr_5g 3.0, got %f", compiled.Charging.RATMultiplier["nr_5g"])
	}
}

func TestCompiler_Version(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {}
}`

	compiled, err := compileSource(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if compiled.Version != "1.0" {
		t.Errorf("expected version '1.0', got %q", compiled.Version)
	}
}
