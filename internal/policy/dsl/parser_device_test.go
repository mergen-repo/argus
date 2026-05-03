package dsl

import (
	"testing"
)

// STORY-094 Phase 11 — Parser golden tests for device & SIM-binding
// condition fields and function-call shapes (tac, device.imei_in_pool).
//
// Function-call encoding contract:
//   tac(device.imei)            → cond.Field == "tac(device.imei)"
//   device.imei_in_pool("foo")  → cond.Field == "device.imei_in_pool(foo)"
// Standalone (no comparator) form synthesises Operator="=" with a
// BoolValue{true} on the RHS so the evaluator dispatch path is uniform.

func wrapWhen(condition string) string {
	return `POLICY "test" {
    MATCH { apn = "test" }
    RULES {
        WHEN ` + condition + ` {
            bandwidth_down = 64kbps
        }
    }
}`
}

func extractWhenSimpleCond(t *testing.T, p *Policy) *SimpleCondition {
	t.Helper()
	if p.Rules == nil || len(p.Rules.Statements) != 1 {
		t.Fatalf("expected 1 statement in RULES, got %d", len(p.Rules.Statements))
	}
	wb, ok := p.Rules.Statements[0].(*WhenBlock)
	if !ok {
		t.Fatalf("expected WhenBlock, got %T", p.Rules.Statements[0])
	}
	sc, ok := wb.Cond.(*SimpleCondition)
	if !ok {
		t.Fatalf("expected SimpleCondition, got %T", wb.Cond)
	}
	return sc
}

func assertNoParseErrors(t *testing.T, errs []DSLError) {
	t.Helper()
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected parse error: %s", e.Error())
		}
	}
}

// 1. WHEN device.binding_status == "mismatch"
func TestParser_Device_BindingStatus_Eq(t *testing.T) {
	src := wrapWhen(`device.binding_status = "mismatch"`)
	policy, errs := parseSource(src)
	assertNoParseErrors(t, errs)

	sc := extractWhenSimpleCond(t, policy)
	if sc.Field != "device.binding_status" {
		t.Errorf("Field: got %q, want %q", sc.Field, "device.binding_status")
	}
	if sc.Operator != "=" {
		t.Errorf("Operator: got %q, want %q", sc.Operator, "=")
	}
	if len(sc.Values) != 1 {
		t.Fatalf("Values: got %d, want 1", len(sc.Values))
	}
	sv, ok := sc.Values[0].(*StringValue)
	if !ok {
		t.Fatalf("Values[0]: got %T, want *StringValue", sc.Values[0])
	}
	if sv.Val != "mismatch" {
		t.Errorf("Values[0].Val: got %q, want %q", sv.Val, "mismatch")
	}
}

// 2. WHEN sim.binding_mode IN ("strict","tac-lock")
func TestParser_Sim_BindingMode_In(t *testing.T) {
	src := wrapWhen(`sim.binding_mode IN ("strict", "tac-lock")`)
	policy, errs := parseSource(src)
	assertNoParseErrors(t, errs)

	sc := extractWhenSimpleCond(t, policy)
	if sc.Field != "sim.binding_mode" {
		t.Errorf("Field: got %q, want %q", sc.Field, "sim.binding_mode")
	}
	if sc.Operator != "IN" {
		t.Errorf("Operator: got %q, want %q", sc.Operator, "IN")
	}
	if len(sc.Values) != 2 {
		t.Fatalf("Values: got %d, want 2", len(sc.Values))
	}
	want := []string{"strict", "tac-lock"}
	for i, v := range sc.Values {
		sv, ok := v.(*StringValue)
		if !ok {
			t.Fatalf("Values[%d]: got %T, want *StringValue", i, v)
		}
		if sv.Val != want[i] {
			t.Errorf("Values[%d].Val: got %q, want %q", i, sv.Val, want[i])
		}
	}
}

// 3. WHEN tac(device.imei) == "35921108"
func TestParser_FunctionCall_Tac_OnLHS(t *testing.T) {
	src := wrapWhen(`tac(device.imei) = "35921108"`)
	policy, errs := parseSource(src)
	assertNoParseErrors(t, errs)

	sc := extractWhenSimpleCond(t, policy)
	if sc.Field != "tac(device.imei)" {
		t.Errorf("Field encoding: got %q, want %q", sc.Field, "tac(device.imei)")
	}
	if sc.Operator != "=" {
		t.Errorf("Operator: got %q, want %q", sc.Operator, "=")
	}
	if len(sc.Values) != 1 {
		t.Fatalf("Values: got %d, want 1", len(sc.Values))
	}
	sv, ok := sc.Values[0].(*StringValue)
	if !ok {
		t.Fatalf("Values[0]: got %T, want *StringValue", sc.Values[0])
	}
	if sv.Val != "35921108" {
		t.Errorf("Values[0].Val: got %q, want %q", sv.Val, "35921108")
	}
}

// 4. WHEN device.imei_in_pool("blacklist")
func TestParser_FunctionCall_ImeiInPool_Standalone(t *testing.T) {
	src := wrapWhen(`device.imei_in_pool("blacklist")`)
	policy, errs := parseSource(src)
	assertNoParseErrors(t, errs)

	sc := extractWhenSimpleCond(t, policy)
	if sc.Field != "device.imei_in_pool(blacklist)" {
		t.Errorf("Field encoding: got %q, want %q", sc.Field, "device.imei_in_pool(blacklist)")
	}
	// Standalone (truthy) form — synthesised "=" + true.
	if sc.Operator != "=" {
		t.Errorf("Operator (synthesised): got %q, want %q", sc.Operator, "=")
	}
	if len(sc.Values) != 1 {
		t.Fatalf("Values: got %d, want 1", len(sc.Values))
	}
	bv, ok := sc.Values[0].(*BoolValue)
	if !ok {
		t.Fatalf("Values[0]: got %T, want *BoolValue (synthesised)", sc.Values[0])
	}
	if bv.Val != true {
		t.Errorf("Values[0].Val: got %v, want true", bv.Val)
	}
}

// 5. WHEN device.software_version != "00"
func TestParser_Device_SoftwareVersion_Neq(t *testing.T) {
	src := wrapWhen(`device.software_version != "00"`)
	policy, errs := parseSource(src)
	assertNoParseErrors(t, errs)

	sc := extractWhenSimpleCond(t, policy)
	if sc.Field != "device.software_version" {
		t.Errorf("Field: got %q, want %q", sc.Field, "device.software_version")
	}
	if sc.Operator != "!=" {
		t.Errorf("Operator: got %q, want %q", sc.Operator, "!=")
	}
	if len(sc.Values) != 1 {
		t.Fatalf("Values: got %d, want 1", len(sc.Values))
	}
	sv, ok := sc.Values[0].(*StringValue)
	if !ok {
		t.Fatalf("Values[0]: got %T, want *StringValue", sc.Values[0])
	}
	if sv.Val != "00" {
		t.Errorf("Values[0].Val: got %q, want %q", sv.Val, "00")
	}
}

//  6. AC-13 combined: WHEN device.binding_status == "mismatch" AND
//     sim.binding_mode IN ("strict", "tac-lock") THEN reject (block).
func TestParser_AC13_CombinedPolicy(t *testing.T) {
	src := `POLICY "ac-13-binding-enforce" {
    MATCH { apn = "iot.data" }
    RULES {
        WHEN device.binding_status = "mismatch" AND sim.binding_mode IN ("strict", "tac-lock") {
            ACTION block()
        }
    }
}`
	policy, errs := parseSource(src)
	assertNoParseErrors(t, errs)

	if policy.Rules == nil || len(policy.Rules.Statements) != 1 {
		t.Fatalf("expected 1 statement in RULES, got %d", len(policy.Rules.Statements))
	}
	wb, ok := policy.Rules.Statements[0].(*WhenBlock)
	if !ok {
		t.Fatalf("expected WhenBlock, got %T", policy.Rules.Statements[0])
	}
	cc, ok := wb.Cond.(*CompoundCondition)
	if !ok {
		t.Fatalf("expected CompoundCondition at top, got %T", wb.Cond)
	}
	if cc.Op != "AND" {
		t.Errorf("top-level Op: got %q, want AND", cc.Op)
	}

	leftSC, ok := cc.Left.(*SimpleCondition)
	if !ok {
		t.Fatalf("Left: got %T, want *SimpleCondition", cc.Left)
	}
	if leftSC.Field != "device.binding_status" || leftSC.Operator != "=" {
		t.Errorf("Left simple cond: field=%q op=%q (want device.binding_status / =)",
			leftSC.Field, leftSC.Operator)
	}

	rightSC, ok := cc.Right.(*SimpleCondition)
	if !ok {
		t.Fatalf("Right: got %T, want *SimpleCondition", cc.Right)
	}
	if rightSC.Field != "sim.binding_mode" || rightSC.Operator != "IN" {
		t.Errorf("Right simple cond: field=%q op=%q (want sim.binding_mode / IN)",
			rightSC.Field, rightSC.Operator)
	}
	if len(rightSC.Values) != 2 {
		t.Errorf("Right Values: got %d, want 2", len(rightSC.Values))
	}

	// Body has the block() action.
	if len(wb.Body) != 1 {
		t.Fatalf("expected 1 body item, got %d", len(wb.Body))
	}
	ac, ok := wb.Body[0].(*ActionCall)
	if !ok {
		t.Fatalf("Body[0]: got %T, want *ActionCall", wb.Body[0])
	}
	if ac.Name != "block" {
		t.Errorf("ActionCall.Name: got %q, want block", ac.Name)
	}
}
