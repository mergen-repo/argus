package dsl

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type CompiledPolicy struct {
	Name     string            `json:"name"`
	Version  string            `json:"version"`
	Match    CompiledMatch     `json:"match"`
	Rules    CompiledRules     `json:"rules"`
	Charging *CompiledCharging `json:"charging,omitempty"`
}

type CompiledMatch struct {
	Conditions []CompiledMatchCondition `json:"conditions"`
}

type CompiledMatchCondition struct {
	Field  string        `json:"field"`
	Op     string        `json:"op"`
	Value  interface{}   `json:"value,omitempty"`
	Values []interface{} `json:"values,omitempty"`
}

type CompiledRules struct {
	Defaults   map[string]interface{} `json:"defaults"`
	WhenBlocks []CompiledWhenBlock    `json:"when_blocks"`
}

type CompiledWhenBlock struct {
	Condition   *CompiledCondition     `json:"condition"`
	Assignments map[string]interface{} `json:"assignments,omitempty"`
	Actions     []CompiledAction       `json:"actions,omitempty"`
}

type CompiledCondition struct {
	Field  string             `json:"field,omitempty"`
	Op     string             `json:"op"`
	Value  interface{}        `json:"value,omitempty"`
	Values []interface{}      `json:"values,omitempty"`
	Left   *CompiledCondition `json:"left,omitempty"`
	Right  *CompiledCondition `json:"right,omitempty"`
	Inner  *CompiledCondition `json:"inner,omitempty"`
}

type CompiledAction struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params,omitempty"`
}

type CompiledCharging struct {
	Model            string             `json:"model"`
	RatePerMB        float64            `json:"rate_per_mb,omitempty"`
	RatePerSession   float64            `json:"rate_per_session,omitempty"`
	BillingCycle     string             `json:"billing_cycle,omitempty"`
	Quota            int64              `json:"quota,omitempty"`
	OverageAction    string             `json:"overage_action,omitempty"`
	OverageRatePerMB float64            `json:"overage_rate_per_mb,omitempty"`
	RATMultiplier    map[string]float64 `json:"rat_type_multiplier,omitempty"`
}

type Compiler struct{}

func NewCompiler() *Compiler {
	return &Compiler{}
}

func (c *Compiler) Compile(ast *Policy) (*CompiledPolicy, error) {
	if ast == nil {
		return nil, fmt.Errorf("nil AST")
	}

	cp := &CompiledPolicy{
		Name:    ast.Name,
		Version: DSLVersion(),
	}

	if ast.Match != nil {
		match, err := c.compileMatch(ast.Match)
		if err != nil {
			return nil, fmt.Errorf("compile match: %w", err)
		}
		cp.Match = *match
	}

	if ast.Rules != nil {
		rules, err := c.compileRules(ast.Rules)
		if err != nil {
			return nil, fmt.Errorf("compile rules: %w", err)
		}
		cp.Rules = *rules
	} else {
		cp.Rules = CompiledRules{
			Defaults:   make(map[string]interface{}),
			WhenBlocks: []CompiledWhenBlock{},
		}
	}

	if ast.Charging != nil {
		charging, err := c.compileCharging(ast.Charging)
		if err != nil {
			return nil, fmt.Errorf("compile charging: %w", err)
		}
		cp.Charging = charging
	}

	return cp, nil
}

func (c *Compiler) compileMatch(m *MatchBlock) (*CompiledMatch, error) {
	cm := &CompiledMatch{}
	for _, clause := range m.Clauses {
		cond := CompiledMatchCondition{
			Field: clause.Field,
			Op:    normalizeOp(clause.Operator),
		}
		if cond.Op == "in" {
			for _, v := range clause.Values {
				cond.Values = append(cond.Values, extractRawValue(v))
			}
		} else if len(clause.Values) == 1 {
			cond.Value = extractRawValue(clause.Values[0])
		}
		cm.Conditions = append(cm.Conditions, cond)
	}
	return cm, nil
}

func (c *Compiler) compileRules(r *RulesBlock) (*CompiledRules, error) {
	cr := &CompiledRules{
		Defaults:   make(map[string]interface{}),
		WhenBlocks: []CompiledWhenBlock{},
	}

	for _, stmt := range r.Statements {
		switch s := stmt.(type) {
		case *Assignment:
			val, err := normalizeAssignmentValue(s.Property, s.Val)
			if err != nil {
				return nil, fmt.Errorf("normalize %s: %w", s.Property, err)
			}
			cr.Defaults[s.Property] = val

		case *WhenBlock:
			wb, err := c.compileWhenBlock(s)
			if err != nil {
				return nil, fmt.Errorf("compile when block: %w", err)
			}
			cr.WhenBlocks = append(cr.WhenBlocks, *wb)
		}
	}

	return cr, nil
}

func (c *Compiler) compileWhenBlock(w *WhenBlock) (*CompiledWhenBlock, error) {
	cwb := &CompiledWhenBlock{
		Assignments: make(map[string]interface{}),
	}

	cond, err := c.compileCondition(w.Cond)
	if err != nil {
		return nil, fmt.Errorf("compile condition: %w", err)
	}
	cwb.Condition = cond

	for _, body := range w.Body {
		switch b := body.(type) {
		case *Assignment:
			val, err := normalizeAssignmentValue(b.Property, b.Val)
			if err != nil {
				return nil, fmt.Errorf("normalize %s: %w", b.Property, err)
			}
			cwb.Assignments[b.Property] = val

		case *ActionCall:
			action := c.compileAction(b)
			cwb.Actions = append(cwb.Actions, action)
		}
	}

	if len(cwb.Assignments) == 0 {
		cwb.Assignments = nil
	}

	return cwb, nil
}

func (c *Compiler) compileCondition(cond Condition) (*CompiledCondition, error) {
	switch cd := cond.(type) {
	case *SimpleCondition:
		cc := &CompiledCondition{
			Field: cd.Field,
			Op:    normalizeOp(cd.Operator),
		}
		if cc.Op == "in" {
			for _, v := range cd.Values {
				cc.Values = append(cc.Values, normalizeConditionValue(cd.Field, v))
			}
		} else if len(cd.Values) == 1 {
			cc.Value = normalizeConditionValue(cd.Field, cd.Values[0])
		} else if len(cd.Values) == 2 && cc.Op == "between" {
			cc.Value = normalizeConditionValue(cd.Field, cd.Values[0])
			cc.Values = []interface{}{normalizeConditionValue(cd.Field, cd.Values[1])}
		}
		return cc, nil

	case *CompoundCondition:
		left, err := c.compileCondition(cd.Left)
		if err != nil {
			return nil, err
		}
		right, err := c.compileCondition(cd.Right)
		if err != nil {
			return nil, err
		}
		return &CompiledCondition{
			Op:    strings.ToLower(cd.Op),
			Left:  left,
			Right: right,
		}, nil

	case *NotCondition:
		inner, err := c.compileCondition(cd.Inner)
		if err != nil {
			return nil, err
		}
		return &CompiledCondition{
			Op:    "not",
			Inner: inner,
		}, nil

	case *GroupCondition:
		return c.compileCondition(cd.Inner)

	default:
		return nil, fmt.Errorf("unknown condition type: %T", cond)
	}
}

func (c *Compiler) compileAction(a *ActionCall) CompiledAction {
	ca := CompiledAction{
		Type:   a.Name,
		Params: make(map[string]interface{}),
	}

	switch a.Name {
	case "notify":
		if len(a.Args) >= 1 {
			ca.Params["event_type"] = extractRawValue(a.Args[0].Val)
		}
		if len(a.Args) >= 2 {
			ca.Params["threshold"] = extractRawValue(a.Args[1].Val)
		}
	case "throttle":
		if len(a.Args) >= 1 {
			ca.Params["rate"] = normalizeRateValue(a.Args[0].Val)
		}
	case "log":
		if len(a.Args) >= 1 {
			ca.Params["message"] = extractRawValue(a.Args[0].Val)
		}
	case "tag":
		if len(a.Args) >= 1 {
			ca.Params["key"] = extractRawValue(a.Args[0].Val)
		}
		if len(a.Args) >= 2 {
			ca.Params["value"] = extractRawValue(a.Args[1].Val)
		}
	}

	if len(ca.Params) == 0 {
		ca.Params = nil
	}

	return ca
}

func (c *Compiler) compileCharging(ch *ChargingBlock) (*CompiledCharging, error) {
	cc := &CompiledCharging{}

	for _, a := range ch.Assignments {
		switch a.Property {
		case "model":
			cc.Model = extractStringValue(a.Val)
		case "rate_per_mb":
			cc.RatePerMB = extractFloatValue(a.Val)
		case "rate_per_session":
			cc.RatePerSession = extractFloatValue(a.Val)
		case "billing_cycle":
			cc.BillingCycle = extractStringValue(a.Val)
		case "quota":
			cc.Quota = normalizeDataSize(a.Val)
		case "overage_action":
			cc.OverageAction = extractStringValue(a.Val)
		case "overage_rate_per_mb":
			cc.OverageRatePerMB = extractFloatValue(a.Val)
		}
	}

	if len(ch.RATMultiplier) > 0 {
		cc.RATMultiplier = ch.RATMultiplier
	}

	return cc, nil
}

func (c *Compiler) ToJSON(cp *CompiledPolicy) ([]byte, error) {
	return json.Marshal(cp)
}

func normalizeOp(op string) string {
	switch op {
	case ">":
		return "gt"
	case ">=":
		return "gte"
	case "<":
		return "lt"
	case "<=":
		return "lte"
	case "=":
		return "eq"
	case "!=":
		return "neq"
	case "IN":
		return "in"
	case "BETWEEN":
		return "between"
	default:
		return strings.ToLower(op)
	}
}

func extractRawValue(v Value) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case *StringValue:
		return val.Val
	case *NumberValue:
		if val.Val == math.Trunc(val.Val) {
			return int64(val.Val)
		}
		return val.Val
	case *NumberWithUnit:
		return normalizeWithUnit(val.Val, val.Unit)
	case *IdentValue:
		return val.Val
	case *BoolValue:
		return val.Val
	case *PercentValue:
		return val.Val
	case *TimeRange:
		return val.Start + "-" + val.End
	default:
		return nil
	}
}

func extractStringValue(v Value) string {
	switch val := v.(type) {
	case *StringValue:
		return val.Val
	case *IdentValue:
		return val.Val
	default:
		return ""
	}
}

func extractFloatValue(v Value) float64 {
	switch val := v.(type) {
	case *NumberValue:
		return val.Val
	case *NumberWithUnit:
		return val.Val
	default:
		return 0
	}
}

func normalizeAssignmentValue(prop string, v Value) (interface{}, error) {
	switch prop {
	case "bandwidth_down", "bandwidth_up":
		return normalizeRateValue(v), nil
	case "session_timeout", "idle_timeout":
		return normalizeDuration(v), nil
	case "max_sessions", "qos_class", "priority":
		return extractIntValue(v), nil
	default:
		return extractRawValue(v), nil
	}
}

func normalizeConditionValue(field string, v Value) interface{} {
	switch field {
	case "usage":
		return normalizeDataSize(v)
	case "bandwidth_used":
		return normalizeRateValue(v)
	case "session_duration":
		return normalizeDuration(v)
	case "time_of_day":
		return extractRawValue(v)
	default:
		return extractRawValue(v)
	}
}

func normalizeRateValue(v Value) interface{} {
	if nwu, ok := v.(*NumberWithUnit); ok {
		return normalizeWithUnit(nwu.Val, nwu.Unit)
	}
	return extractRawValue(v)
}

func normalizeDataSize(v Value) int64 {
	switch val := v.(type) {
	case *NumberWithUnit:
		return int64(normalizeWithUnit(val.Val, val.Unit))
	case *NumberValue:
		return int64(val.Val)
	default:
		return 0
	}
}

func normalizeDuration(v Value) interface{} {
	if nwu, ok := v.(*NumberWithUnit); ok {
		return normalizeWithUnit(nwu.Val, nwu.Unit)
	}
	return extractRawValue(v)
}

func extractIntValue(v Value) int64 {
	switch val := v.(type) {
	case *NumberValue:
		return int64(val.Val)
	case *NumberWithUnit:
		return int64(val.Val)
	default:
		return 0
	}
}

func normalizeWithUnit(val float64, unit string) float64 {
	unit = strings.ToLower(unit)
	switch unit {
	case "b":
		return val
	case "kb":
		return val * 1024
	case "mb":
		return val * 1048576
	case "gb":
		return val * 1073741824
	case "tb":
		return val * 1099511627776

	case "bps":
		return val
	case "kbps":
		return val * 1000
	case "mbps":
		return val * 1000000
	case "gbps":
		return val * 1000000000

	case "ms":
		return val * 0.001
	case "s":
		return val
	case "min":
		return val * 60
	case "h":
		return val * 3600
	case "d":
		return val * 86400

	default:
		return val
	}
}
