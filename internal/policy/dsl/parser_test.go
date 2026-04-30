package dsl

import (
	"testing"
)

func parseSource(src string) (*Policy, []DSLError) {
	lexer := NewLexer(src)
	tokens := lexer.Tokenize()
	parser := NewParserWithSource(tokens, src)
	return parser.Parse()
}

func TestParser_ValidPolicyWithMatchRulesCharging(t *testing.T) {
	src := `POLICY "iot-fleet" {
    MATCH {
        apn IN ("iot.fleet", "iot.meter")
        rat_type IN (nb_iot, lte_m)
    }

    RULES {
        bandwidth_down = 1mbps
        bandwidth_up = 256kbps
        session_timeout = 24h

        WHEN usage > 1GB {
            bandwidth_down = 64kbps
            ACTION notify(quota_exceeded, 100%)
        }
    }

    CHARGING {
        model = postpaid
        rate_per_mb = 0.01
        billing_cycle = monthly
        quota = 1GB

        rat_type_multiplier {
            nb_iot = 0.5
            lte_m = 1.0
        }
    }
}`

	policy, errs := parseSource(src)

	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected error: %s", e.Error())
		}
	}

	if policy.Name != "iot-fleet" {
		t.Errorf("expected policy name 'iot-fleet', got %q", policy.Name)
	}

	if policy.Match == nil {
		t.Fatal("expected MATCH block")
	}
	if len(policy.Match.Clauses) != 2 {
		t.Errorf("expected 2 MATCH clauses, got %d", len(policy.Match.Clauses))
	}

	if policy.Rules == nil {
		t.Fatal("expected RULES block")
	}

	assignmentCount := 0
	whenCount := 0
	for _, stmt := range policy.Rules.Statements {
		switch stmt.(type) {
		case *Assignment:
			assignmentCount++
		case *WhenBlock:
			whenCount++
		}
	}
	if assignmentCount != 3 {
		t.Errorf("expected 3 default assignments, got %d", assignmentCount)
	}
	if whenCount != 1 {
		t.Errorf("expected 1 WHEN block, got %d", whenCount)
	}

	if policy.Charging == nil {
		t.Fatal("expected CHARGING block")
	}
	if len(policy.Charging.Assignments) < 3 {
		t.Errorf("expected at least 3 charging assignments, got %d", len(policy.Charging.Assignments))
	}
	if len(policy.Charging.RATMultiplier) != 2 {
		t.Errorf("expected 2 RAT multipliers, got %d", len(policy.Charging.RATMultiplier))
	}
}

func TestParser_SyntaxError_LineColumn(t *testing.T) {
	src := `POLICY "test" {
    MATCH
    RULES {
    }
}`

	_, errs := parseSource(src)

	hasError := false
	for _, e := range errs {
		if e.Severity == "error" && e.Line > 0 && e.Column > 0 {
			hasError = true
			break
		}
	}
	if !hasError {
		t.Error("expected at least one error with line and column info")
	}
}

func TestParser_MatchBlock_INOperator(t *testing.T) {
	src := `POLICY "test" {
    MATCH {
        apn IN ("iot.fleet", "iot.meter", "iot.data")
    }
    RULES {}
}`

	policy, errs := parseSource(src)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected error: %s", e.Error())
		}
	}

	if policy.Match == nil || len(policy.Match.Clauses) != 1 {
		t.Fatal("expected 1 MATCH clause")
	}

	clause := policy.Match.Clauses[0]
	if clause.Field != "apn" {
		t.Errorf("expected field 'apn', got %q", clause.Field)
	}
	if clause.Operator != "IN" {
		t.Errorf("expected operator IN, got %q", clause.Operator)
	}
	if len(clause.Values) != 3 {
		t.Errorf("expected 3 values, got %d", len(clause.Values))
	}
}

func TestParser_WhenBlock_SimpleCondition(t *testing.T) {
	src := `POLICY "test" {
    MATCH {
        apn = "iot.data"
    }
    RULES {
        WHEN usage > 500MB {
            bandwidth_down = 64kbps
        }
    }
}`

	policy, errs := parseSource(src)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected error: %s", e.Error())
		}
	}

	if policy.Rules == nil || len(policy.Rules.Statements) != 1 {
		t.Fatal("expected 1 statement in RULES")
	}

	wb, ok := policy.Rules.Statements[0].(*WhenBlock)
	if !ok {
		t.Fatal("expected WhenBlock statement")
	}

	sc, ok := wb.Cond.(*SimpleCondition)
	if !ok {
		t.Fatal("expected SimpleCondition")
	}
	if sc.Field != "usage" {
		t.Errorf("expected field 'usage', got %q", sc.Field)
	}
	if sc.Operator != ">" {
		t.Errorf("expected operator '>', got %q", sc.Operator)
	}
}

func TestParser_CompoundConditions(t *testing.T) {
	tests := []struct {
		name string
		cond string
		op   string
	}{
		{
			name: "AND",
			cond: "usage > 500MB AND rat_type = lte",
			op:   "AND",
		},
		{
			name: "OR",
			cond: "rat_type = lte OR usage > 1GB",
			op:   "OR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {
        WHEN ` + tt.cond + ` {
            bandwidth_down = 64kbps
        }
    }
}`
			policy, errs := parseSource(src)
			for _, e := range errs {
				if e.Severity == "error" {
					t.Errorf("unexpected error: %s", e.Error())
				}
			}

			wb, ok := policy.Rules.Statements[0].(*WhenBlock)
			if !ok {
				t.Fatal("expected WhenBlock")
			}

			cc, ok := wb.Cond.(*CompoundCondition)
			if !ok {
				t.Fatalf("expected CompoundCondition, got %T", wb.Cond)
			}
			if cc.Op != tt.op {
				t.Errorf("expected op %q, got %q", tt.op, cc.Op)
			}
		})
	}
}

func TestParser_NotCondition(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {
        WHEN NOT rat_type = lte {
            bandwidth_down = 2mbps
        }
    }
}`

	policy, errs := parseSource(src)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected error: %s", e.Error())
		}
	}

	wb, ok := policy.Rules.Statements[0].(*WhenBlock)
	if !ok {
		t.Fatal("expected WhenBlock")
	}

	nc, ok := wb.Cond.(*NotCondition)
	if !ok {
		t.Fatalf("expected NotCondition, got %T", wb.Cond)
	}
	if nc.Inner == nil {
		t.Error("NotCondition.Inner is nil")
	}
}

func TestParser_ParenthesizedCondition(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {
        WHEN (sim_type = iot OR usage > 1GB) AND rat_type = lte {
            bandwidth_down = 64kbps
        }
    }
}`

	policy, errs := parseSource(src)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected error: %s", e.Error())
		}
	}

	wb, ok := policy.Rules.Statements[0].(*WhenBlock)
	if !ok {
		t.Fatal("expected WhenBlock")
	}

	cc, ok := wb.Cond.(*CompoundCondition)
	if !ok {
		t.Fatalf("expected CompoundCondition, got %T", wb.Cond)
	}
	if cc.Op != "AND" {
		t.Errorf("expected AND at top level, got %q", cc.Op)
	}

	_, leftIsGroup := cc.Left.(*GroupCondition)
	if !leftIsGroup {
		t.Errorf("expected left side to be GroupCondition, got %T", cc.Left)
	}
}

func TestParser_ActionCalls(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {
        WHEN usage > 1GB {
            ACTION notify(quota_exceeded, 100%)
            ACTION throttle(64kbps)
            ACTION log("FUP applied")
            ACTION disconnect()
        }
    }
}`

	policy, errs := parseSource(src)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected error: %s", e.Error())
		}
	}

	wb, ok := policy.Rules.Statements[0].(*WhenBlock)
	if !ok {
		t.Fatal("expected WhenBlock")
	}

	actionCount := 0
	for _, body := range wb.Body {
		if _, ok := body.(*ActionCall); ok {
			actionCount++
		}
	}
	if actionCount != 4 {
		t.Errorf("expected 4 actions, got %d", actionCount)
	}
}

func TestParser_ChargingBlock(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {}
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

	policy, errs := parseSource(src)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected error: %s", e.Error())
		}
	}

	if policy.Charging == nil {
		t.Fatal("expected CHARGING block")
	}

	if len(policy.Charging.RATMultiplier) != 4 {
		t.Errorf("expected 4 RAT multipliers, got %d", len(policy.Charging.RATMultiplier))
	}
	if policy.Charging.RATMultiplier["nb_iot"] != 0.5 {
		t.Errorf("expected nb_iot multiplier 0.5, got %f", policy.Charging.RATMultiplier["nb_iot"])
	}
}

func TestParser_EmptyRulesBlock(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {}
}`

	policy, errs := parseSource(src)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected error: %s", e.Error())
		}
	}

	if policy.Rules == nil {
		t.Fatal("expected RULES block")
	}
	if len(policy.Rules.Statements) != 0 {
		t.Errorf("expected 0 statements, got %d", len(policy.Rules.Statements))
	}
}

func TestParser_ErrorRecovery_MultipleErrors(t *testing.T) {
	src := `POLICY "test" {
    MATCH {
    }
    RULES {
        bandwidth_down = 1mbps
        bandwidth_down = 2mbps
    }
}`

	_, errs := parseSource(src)

	errorCount := 0
	for _, e := range errs {
		if e.Severity == "error" {
			errorCount++
		}
	}

	if errorCount < 2 {
		t.Errorf("expected at least 2 errors (empty match + duplicate), got %d", errorCount)
	}
}

func TestParser_DuplicateAssignment(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {
        bandwidth_down = 1mbps
        bandwidth_down = 2mbps
    }
}`

	_, errs := parseSource(src)

	found := false
	for _, e := range errs {
		if e.Code == "DSL_DUPLICATE_ASSIGNMENT" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected DSL_DUPLICATE_ASSIGNMENT error")
	}
}

func TestParser_InvalidRATType(t *testing.T) {
	src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {}
    CHARGING {
        model = postpaid
        rat_type_multiplier {
            invalid_rat = 1.0
        }
    }
}`

	_, errs := parseSource(src)

	found := false
	for _, e := range errs {
		if e.Code == "DSL_INVALID_RAT_TYPE" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected DSL_INVALID_RAT_TYPE error")
	}
}

func TestParser_RATTypeAliasesAccepted(t *testing.T) {
	// Only aliases that the DSL lexer can tokenize as a single identifier:
	// must start with a letter/underscore; hyphens and digit-prefixes are not valid DSL identifier chars.
	aliases := []string{
		"nb_iot", "nbiot",
		"lte_m", "cat_m1",
		"lte", "eutran",
		"nr_5g", "nr_5g_nsa",
		"utran", "geran",
		"unknown",
	}

	for _, alias := range aliases {
		t.Run(alias, func(t *testing.T) {
			src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {}
    CHARGING {
        model = postpaid
        rat_type_multiplier {
            ` + alias + ` = 1.0
        }
    }
}`
			_, errs := parseSource(src)
			for _, e := range errs {
				if e.Code == "DSL_INVALID_RAT_TYPE" {
					t.Errorf("alias %q rejected: %s", alias, e.Message)
				}
			}
		})
	}
}

func TestParser_ActionParameterValidation(t *testing.T) {
	tests := []struct {
		name    string
		action  string
		hasErr  bool
	}{
		{"notify_valid", "ACTION notify(quota_exceeded, 100%)", false},
		{"notify_invalid", "ACTION notify(quota_exceeded)", true},
		{"throttle_valid", "ACTION throttle(64kbps)", false},
		{"disconnect_valid", "ACTION disconnect()", false},
		{"disconnect_invalid", "ACTION disconnect(123)", true},
		{"log_valid", `ACTION log("message")`, false},
		{"tag_valid", `ACTION tag("key", "value")`, false},
		{"unknown_action", "ACTION unknown_action()", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := `POLICY "test" {
    MATCH { apn = "test" }
    RULES {
        WHEN usage > 1GB {
            ` + tt.action + `
        }
    }
}`
			_, errs := parseSource(src)

			hasError := false
			for _, e := range errs {
				if e.Severity == "error" && (e.Code == "DSL_ACTION_PARAMS" || e.Code == "DSL_UNKNOWN_ACTION") {
					hasError = true
					break
				}
			}
			if tt.hasErr && !hasError {
				t.Error("expected action parameter error")
			}
			if !tt.hasErr && hasError {
				t.Error("unexpected action parameter error")
			}
		})
	}
}
