package dsl

import (
	"encoding/json"
	"testing"
)

func compileForEval(src string) *CompiledPolicy {
	compiled, errs, err := CompileSource(src)
	if err != nil {
		panic(err)
	}
	for _, e := range errs {
		if e.Severity == "error" {
			panic(e.Error())
		}
	}
	return compiled
}

func TestEvaluator_MatchingWhenReturnsAction(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
    RULES {
        bandwidth_down = 1mbps

        WHEN usage > 500MB {
            bandwidth_down = 64kbps
            ACTION notify(quota_warning, 80%)
        }
    }
}`
	compiled := compileForEval(src)
	ctx := SessionContext{
		APN:   "iot.data",
		Usage: 600 * 1048576,
	}

	eval := NewEvaluator()
	result, err := eval.Evaluate(ctx, compiled)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if result.QoSAttributes["bandwidth_down"] != float64(64000) {
		t.Errorf("expected bandwidth_down 64000, got %v", result.QoSAttributes["bandwidth_down"])
	}

	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(result.Actions))
	}
	if result.Actions[0].Type != "notify" {
		t.Errorf("expected notify action, got %q", result.Actions[0].Type)
	}
}

func TestEvaluator_NoMatchingWhenReturnsDefaults(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
    RULES {
        bandwidth_down = 1mbps

        WHEN usage > 1GB {
            bandwidth_down = 64kbps
        }
    }
}`
	compiled := compileForEval(src)
	ctx := SessionContext{
		APN:   "iot.data",
		Usage: 100 * 1048576,
	}

	eval := NewEvaluator()
	result, err := eval.Evaluate(ctx, compiled)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if result.QoSAttributes["bandwidth_down"] != float64(1000000) {
		t.Errorf("expected bandwidth_down 1000000 (default), got %v", result.QoSAttributes["bandwidth_down"])
	}
	if len(result.Actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(result.Actions))
	}
}

func TestEvaluator_LastMatchWinsForAssignments_AllActionsCollected(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
    RULES {
        bandwidth_down = 1mbps

        WHEN usage > 500MB {
            bandwidth_down = 256kbps
            ACTION notify(quota_warning, 80%)
        }

        WHEN usage > 1GB {
            bandwidth_down = 64kbps
            ACTION notify(quota_exceeded, 100%)
        }
    }
}`
	compiled := compileForEval(src)
	ctx := SessionContext{
		APN:   "iot.data",
		Usage: 1200 * 1048576,
	}

	eval := NewEvaluator()
	result, err := eval.Evaluate(ctx, compiled)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if result.QoSAttributes["bandwidth_down"] != float64(64000) {
		t.Errorf("expected bandwidth_down 64000 (last match wins), got %v", result.QoSAttributes["bandwidth_down"])
	}

	if len(result.Actions) != 2 {
		t.Fatalf("expected 2 actions (all collected), got %d", len(result.Actions))
	}
	if result.MatchedRules != 2 {
		t.Errorf("expected 2 matched rules, got %d", result.MatchedRules)
	}
}

func TestEvaluator_ChargingReturnsCorrectRateAndModel(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
    RULES {}
    CHARGING {
        model = postpaid
        rate_per_mb = 0.01
        billing_cycle = monthly
        quota = 1GB
        overage_action = throttle
    }
}`
	compiled := compileForEval(src)
	ctx := SessionContext{
		APN:     "iot.data",
		RATType: "lte",
	}

	eval := NewEvaluator()
	result, err := eval.Evaluate(ctx, compiled)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if result.ChargingParams == nil {
		t.Fatal("expected charging params")
	}
	if result.ChargingParams.Model != "postpaid" {
		t.Errorf("expected model 'postpaid', got %q", result.ChargingParams.Model)
	}
	if result.ChargingParams.RatePerMB != 0.01 {
		t.Errorf("expected rate_per_mb 0.01, got %f", result.ChargingParams.RatePerMB)
	}
}

func TestEvaluator_ComplexCondition(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
    RULES {
        WHEN usage > 500MB AND rat_type = lte {
            bandwidth_down = 128kbps
            ACTION notify(complex_match, 0%)
        }
    }
}`
	compiled := compileForEval(src)

	ctx := SessionContext{
		APN:     "iot.data",
		Usage:   600 * 1048576,
		RATType: "lte",
	}

	eval := NewEvaluator()
	result, err := eval.Evaluate(ctx, compiled)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if result.MatchedRules != 1 {
		t.Errorf("expected 1 matched rule, got %d", result.MatchedRules)
	}
	if result.QoSAttributes["bandwidth_down"] != float64(128000) {
		t.Errorf("expected bandwidth_down 128000, got %v", result.QoSAttributes["bandwidth_down"])
	}

	ctxNoMatch := SessionContext{
		APN:     "iot.data",
		Usage:   600 * 1048576,
		RATType: "nb_iot",
	}
	result2, err := eval.Evaluate(ctxNoMatch, compiled)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if result2.MatchedRules != 0 {
		t.Errorf("expected 0 matched rules for nb_iot, got %d", result2.MatchedRules)
	}
}

func TestEvaluator_EmptyRulesBlock(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
    RULES {}
}`
	compiled := compileForEval(src)
	ctx := SessionContext{
		APN: "iot.data",
	}

	eval := NewEvaluator()
	result, err := eval.Evaluate(ctx, compiled)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if !result.Allow {
		t.Error("expected allow=true with empty rules")
	}
	if len(result.Actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(result.Actions))
	}
}

func TestEvaluator_Roundtrip(t *testing.T) {
	src := `POLICY "roundtrip" {
    MATCH { apn = "iot.data" }
    RULES {
        bandwidth_down = 1mbps
        WHEN usage > 1GB {
            bandwidth_down = 64kbps
        }
    }
}`

	compiled1 := compileForEval(src)
	ctx := SessionContext{
		APN:   "iot.data",
		Usage: 1200 * 1048576,
	}

	eval := NewEvaluator()
	result1, err := eval.Evaluate(ctx, compiled1)
	if err != nil {
		t.Fatalf("first eval error: %v", err)
	}

	compiler := NewCompiler()
	jsonBytes, err := compiler.ToJSON(compiled1)
	if err != nil {
		t.Fatalf("json error: %v", err)
	}

	var compiled2 CompiledPolicy
	if err := json.Unmarshal(jsonBytes, &compiled2); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	result2, err := eval.Evaluate(ctx, &compiled2)
	if err != nil {
		t.Fatalf("second eval error: %v", err)
	}

	if result1.QoSAttributes["bandwidth_down"] != result2.QoSAttributes["bandwidth_down"] {
		t.Errorf("roundtrip mismatch: bandwidth_down %v vs %v",
			result1.QoSAttributes["bandwidth_down"], result2.QoSAttributes["bandwidth_down"])
	}
	if result1.MatchedRules != result2.MatchedRules {
		t.Errorf("roundtrip mismatch: matched_rules %d vs %d",
			result1.MatchedRules, result2.MatchedRules)
	}
}

func TestEvaluator_PolicyDoesNotMatchContext(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
    RULES {
        bandwidth_down = 1mbps
    }
}`
	compiled := compileForEval(src)
	ctx := SessionContext{
		APN: "other.apn",
	}

	eval := NewEvaluator()
	result, err := eval.Evaluate(ctx, compiled)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if !result.Allow {
		t.Error("non-matching policy should allow")
	}
	if len(result.QoSAttributes) != 0 {
		t.Errorf("non-matching policy should have empty QoS, got %v", result.QoSAttributes)
	}
}

func TestEvaluator_RATMultiplier(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
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
	compiled := compileForEval(src)

	tests := []struct {
		ratType    string
		multiplier float64
	}{
		{"nb_iot", 0.5},
		{"lte_m", 1.0},
		{"lte", 2.0},
		{"nr_5g", 3.0},
		{"unknown", 1.0},
	}

	eval := NewEvaluator()
	for _, tt := range tests {
		t.Run(tt.ratType, func(t *testing.T) {
			ctx := SessionContext{
				APN:     "iot.data",
				RATType: tt.ratType,
			}
			result, err := eval.Evaluate(ctx, compiled)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}
			if result.ChargingParams.RATMultiplier != tt.multiplier {
				t.Errorf("expected multiplier %f, got %f", tt.multiplier, result.ChargingParams.RATMultiplier)
			}
		})
	}
}

func TestEvaluator_AllActionTypes(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
    RULES {
        WHEN usage > 1GB {
            ACTION notify(quota_exceeded, 100%)
            ACTION throttle(64kbps)
            ACTION log("FUP applied")
            ACTION tag("throttled", "true")
        }

        WHEN session_count > 3 {
            ACTION block()
        }
    }
}`
	compiled := compileForEval(src)
	ctx := SessionContext{
		APN:          "iot.data",
		Usage:        2 * 1073741824,
		SessionCount: 5,
	}

	eval := NewEvaluator()
	result, err := eval.Evaluate(ctx, compiled)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if len(result.Actions) != 5 {
		t.Fatalf("expected 5 actions, got %d", len(result.Actions))
	}

	actionTypes := make(map[string]bool)
	for _, a := range result.Actions {
		actionTypes[a.Type] = true
	}

	for _, expected := range []string{"notify", "throttle", "log", "tag", "block"} {
		if !actionTypes[expected] {
			t.Errorf("missing action type %q", expected)
		}
	}

	if result.Allow {
		t.Error("expected allow=false when block action is triggered")
	}
}

func TestEvaluator_BoolCondition(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
    RULES {
        WHEN roaming = true {
            bandwidth_down = 256kbps
        }
    }
}`
	compiled := compileForEval(src)

	evalTrue := NewEvaluator()
	resultTrue, _ := evalTrue.Evaluate(SessionContext{APN: "iot.data", Roaming: true}, compiled)
	if resultTrue.MatchedRules != 1 {
		t.Error("expected roaming=true to match")
	}

	resultFalse, _ := evalTrue.Evaluate(SessionContext{APN: "iot.data", Roaming: false}, compiled)
	if resultFalse.MatchedRules != 0 {
		t.Error("expected roaming=false to not match")
	}
}

func TestEvaluator_MatchIN(t *testing.T) {
	src := `POLICY "test" {
    MATCH {
        apn IN ("iot.fleet", "iot.meter", "iot.data")
        rat_type IN (nb_iot, lte_m)
    }
    RULES {
        bandwidth_down = 1mbps
    }
}`
	compiled := compileForEval(src)
	eval := NewEvaluator()

	result1, _ := eval.Evaluate(SessionContext{APN: "iot.fleet", RATType: "nb_iot"}, compiled)
	if result1.QoSAttributes["bandwidth_down"] == nil {
		t.Error("expected match for iot.fleet + nb_iot")
	}

	result2, _ := eval.Evaluate(SessionContext{APN: "other.apn", RATType: "nb_iot"}, compiled)
	if len(result2.QoSAttributes) != 0 {
		t.Error("expected no match for other.apn")
	}

	result3, _ := eval.Evaluate(SessionContext{APN: "iot.fleet", RATType: "lte"}, compiled)
	if len(result3.QoSAttributes) != 0 {
		t.Error("expected no match for lte (not in rat_type list)")
	}
}

func TestEvaluator_DSLVersionField(t *testing.T) {
	v := DSLVersion()
	if v == "" {
		t.Error("DSLVersion() should not be empty")
	}
	if v != "1.0" {
		t.Errorf("expected DSLVersion() = '1.0', got %q", v)
	}
}

func TestEvaluator_TimeOfDayRange(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
    RULES {
        WHEN time_of_day IN (00:00-06:00) {
            bandwidth_down = 2mbps
        }
    }
}`
	compiled := compileForEval(src)
	eval := NewEvaluator()

	tests := []struct {
		name    string
		time    string
		matches bool
	}{
		{"inside_range", "03:00", true},
		{"at_start", "00:00", true},
		{"at_end", "06:00", true},
		{"outside_after", "08:00", false},
		{"outside_before_midnight", "23:00", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := SessionContext{APN: "iot.data", TimeOfDay: tt.time}
			result, err := eval.Evaluate(ctx, compiled)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}
			if tt.matches && result.MatchedRules != 1 {
				t.Errorf("expected match for time %s, got %d matched rules", tt.time, result.MatchedRules)
			}
			if !tt.matches && result.MatchedRules != 0 {
				t.Errorf("expected no match for time %s, got %d matched rules", tt.time, result.MatchedRules)
			}
		})
	}
}

func TestEvaluator_TimeOfDayMidnightWrap(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
    RULES {
        WHEN time_of_day IN (22:00-06:00) {
            bandwidth_down = 4mbps
        }
    }
}`
	compiled := compileForEval(src)
	eval := NewEvaluator()

	tests := []struct {
		name    string
		time    string
		matches bool
	}{
		{"before_midnight", "23:00", true},
		{"at_start", "22:00", true},
		{"midnight", "00:00", true},
		{"after_midnight", "03:00", true},
		{"at_end", "06:00", true},
		{"daytime", "12:00", false},
		{"evening", "20:00", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := SessionContext{APN: "iot.data", TimeOfDay: tt.time}
			result, err := eval.Evaluate(ctx, compiled)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}
			if tt.matches && result.MatchedRules != 1 {
				t.Errorf("expected match for time %s, got %d matched rules", tt.time, result.MatchedRules)
			}
			if !tt.matches && result.MatchedRules != 0 {
				t.Errorf("expected no match for time %s, got %d matched rules", tt.time, result.MatchedRules)
			}
		})
	}
}

func TestEvaluator_SuspendAction(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "iot.data" }
    RULES {
        WHEN usage > 2GB {
            ACTION suspend()
        }
    }
}`
	compiled := compileForEval(src)
	ctx := SessionContext{
		APN:   "iot.data",
		Usage: 3 * 1073741824,
	}

	eval := NewEvaluator()
	result, err := eval.Evaluate(ctx, compiled)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if result.Allow {
		t.Error("expected allow=false when suspend action triggered")
	}
	if len(result.Actions) != 1 || result.Actions[0].Type != "suspend" {
		t.Errorf("expected suspend action, got %v", result.Actions)
	}
}

