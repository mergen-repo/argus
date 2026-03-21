package dsl

import (
	"fmt"
	"strconv"
	"strings"
)

type SessionContext struct {
	SIMID           string            `json:"sim_id"`
	TenantID        string            `json:"tenant_id"`
	Operator        string            `json:"operator"`
	APN             string            `json:"apn"`
	RATType         string            `json:"rat_type"`
	Roaming         bool              `json:"roaming"`
	Usage           int64             `json:"usage"`
	TimeOfDay       string            `json:"time_of_day"`
	DayOfWeek       string            `json:"day_of_week"`
	SessionCount    int               `json:"session_count"`
	BandwidthUsed   int64             `json:"bandwidth_used"`
	SessionDuration int64             `json:"session_duration"`
	Metadata        map[string]string `json:"metadata"`
	SimType         string            `json:"sim_type"`
}

type PolicyResult struct {
	Allow         bool                   `json:"allow"`
	QoSAttributes map[string]interface{} `json:"qos_attributes"`
	ChargingParams *ChargingResult       `json:"charging_params,omitempty"`
	Actions       []ActionResult         `json:"actions"`
	MatchedRules  int                    `json:"matched_rules"`
}

type ChargingResult struct {
	Model           string  `json:"model"`
	RatePerMB       float64 `json:"rate_per_mb"`
	RatePerSession  float64 `json:"rate_per_session,omitempty"`
	BillingCycle    string  `json:"billing_cycle"`
	Quota           int64   `json:"quota"`
	OverageAction   string  `json:"overage_action,omitempty"`
	OverageRatePerMB float64 `json:"overage_rate_per_mb,omitempty"`
	RATMultiplier   float64 `json:"rat_multiplier"`
}

type ActionResult struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params,omitempty"`
}

type Evaluator struct{}

func NewEvaluator() *Evaluator {
	return &Evaluator{}
}

func (e *Evaluator) Evaluate(ctx SessionContext, compiled *CompiledPolicy) (*PolicyResult, error) {
	if compiled == nil {
		return nil, fmt.Errorf("nil compiled policy")
	}

	if !e.matchesPolicy(ctx, compiled) {
		return &PolicyResult{
			Allow:         true,
			QoSAttributes: make(map[string]interface{}),
			Actions:       []ActionResult{},
			MatchedRules:  0,
		}, nil
	}

	result := e.evaluateRules(ctx, &compiled.Rules)

	if compiled.Charging != nil {
		result.ChargingParams = e.evaluateCharging(ctx, compiled.Charging)
	}

	return result, nil
}

func (e *Evaluator) matchesPolicy(ctx SessionContext, compiled *CompiledPolicy) bool {
	for _, cond := range compiled.Match.Conditions {
		if !e.evaluateMatchCondition(ctx, cond) {
			return false
		}
	}
	return true
}

func (e *Evaluator) evaluateMatchCondition(ctx SessionContext, cond CompiledMatchCondition) bool {
	fieldVal := e.getMatchFieldValue(ctx, cond.Field)

	switch cond.Op {
	case "in":
		for _, v := range cond.Values {
			if matchValues(fieldVal, v) {
				return true
			}
		}
		return false
	case "eq":
		return matchValues(fieldVal, cond.Value)
	case "neq":
		return !matchValues(fieldVal, cond.Value)
	default:
		return false
	}
}

func (e *Evaluator) getMatchFieldValue(ctx SessionContext, field string) interface{} {
	switch field {
	case "apn":
		return ctx.APN
	case "operator":
		return ctx.Operator
	case "rat_type":
		return ctx.RATType
	case "sim_type":
		return ctx.SimType
	case "roaming":
		return ctx.Roaming
	default:
		if strings.HasPrefix(field, "metadata.") {
			key := strings.TrimPrefix(field, "metadata.")
			if ctx.Metadata != nil {
				return ctx.Metadata[key]
			}
			return ""
		}
		return nil
	}
}

func (e *Evaluator) evaluateRules(ctx SessionContext, rules *CompiledRules) *PolicyResult {
	result := &PolicyResult{
		Allow:         true,
		QoSAttributes: make(map[string]interface{}),
		Actions:       []ActionResult{},
	}

	for k, v := range rules.Defaults {
		result.QoSAttributes[k] = v
	}

	for _, wb := range rules.WhenBlocks {
		if e.evaluateCondition(ctx, wb.Condition) {
			result.MatchedRules++
			for k, v := range wb.Assignments {
				result.QoSAttributes[k] = v
			}
			for _, action := range wb.Actions {
				result.Actions = append(result.Actions, ActionResult{
					Type:   action.Type,
					Params: action.Params,
				})
			}
		}
	}

	for _, action := range result.Actions {
		if action.Type == "block" || action.Type == "disconnect" || action.Type == "suspend" {
			result.Allow = false
			break
		}
	}

	return result
}

func (e *Evaluator) evaluateCondition(ctx SessionContext, cond *CompiledCondition) bool {
	if cond == nil {
		return true
	}

	switch cond.Op {
	case "and":
		return e.evaluateCondition(ctx, cond.Left) && e.evaluateCondition(ctx, cond.Right)
	case "or":
		return e.evaluateCondition(ctx, cond.Left) || e.evaluateCondition(ctx, cond.Right)
	case "not":
		return !e.evaluateCondition(ctx, cond.Inner)
	default:
		return e.evaluateSimpleCondition(ctx, cond)
	}
}

func (e *Evaluator) evaluateSimpleCondition(ctx SessionContext, cond *CompiledCondition) bool {
	fieldVal := e.getConditionFieldValue(ctx, cond.Field)

	switch cond.Op {
	case "gt":
		return compareNumeric(fieldVal, cond.Value) > 0
	case "gte":
		return compareNumeric(fieldVal, cond.Value) >= 0
	case "lt":
		return compareNumeric(fieldVal, cond.Value) < 0
	case "lte":
		return compareNumeric(fieldVal, cond.Value) <= 0
	case "eq":
		return matchValues(fieldVal, cond.Value)
	case "neq":
		return !matchValues(fieldVal, cond.Value)
	case "in":
		if cond.Field == "time_of_day" {
			timeStr, ok := fieldVal.(string)
			if !ok {
				return false
			}
			for _, v := range cond.Values {
				rangeStr, ok := v.(string)
				if !ok {
					continue
				}
				if isTimeInRange(timeStr, rangeStr) {
					return true
				}
			}
			return false
		}
		for _, v := range cond.Values {
			if matchValues(fieldVal, v) {
				return true
			}
		}
		return false
	case "between":
		if cond.Value != nil && len(cond.Values) > 0 {
			lower := cond.Value
			upper := cond.Values[0]
			return compareNumeric(fieldVal, lower) >= 0 && compareNumeric(fieldVal, upper) <= 0
		}
		return false
	default:
		return false
	}
}

func (e *Evaluator) getConditionFieldValue(ctx SessionContext, field string) interface{} {
	switch field {
	case "usage":
		return ctx.Usage
	case "time_of_day":
		return ctx.TimeOfDay
	case "rat_type":
		return ctx.RATType
	case "apn":
		return ctx.APN
	case "operator":
		return ctx.Operator
	case "roaming":
		return ctx.Roaming
	case "session_count":
		return int64(ctx.SessionCount)
	case "bandwidth_used":
		return ctx.BandwidthUsed
	case "session_duration":
		return ctx.SessionDuration
	case "day_of_week":
		return ctx.DayOfWeek
	case "sim_type":
		return ctx.SimType
	default:
		if strings.HasPrefix(field, "metadata.") {
			key := strings.TrimPrefix(field, "metadata.")
			if ctx.Metadata != nil {
				return ctx.Metadata[key]
			}
			return ""
		}
		return nil
	}
}

func (e *Evaluator) evaluateCharging(ctx SessionContext, ch *CompiledCharging) *ChargingResult {
	result := &ChargingResult{
		Model:           ch.Model,
		RatePerMB:       ch.RatePerMB,
		RatePerSession:  ch.RatePerSession,
		BillingCycle:    ch.BillingCycle,
		Quota:           ch.Quota,
		OverageAction:   ch.OverageAction,
		OverageRatePerMB: ch.OverageRatePerMB,
		RATMultiplier:   1.0,
	}

	if ch.RATMultiplier != nil {
		if mult, ok := ch.RATMultiplier[ctx.RATType]; ok {
			result.RATMultiplier = mult
		}
	}

	return result
}

func matchValues(a, b interface{}) bool {
	if a == nil || b == nil {
		return a == b
	}

	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)
	return strings.EqualFold(aStr, bStr)
}

func compareNumeric(a, b interface{}) int {
	aNum := toFloat64(a)
	bNum := toFloat64(b)

	if aNum < bNum {
		return -1
	}
	if aNum > bNum {
		return 1
	}
	return 0
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int64:
		return float64(val)
	case int:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return 0
	}
}

func isTimeInRange(timeStr, rangeStr string) bool {
	parts := strings.SplitN(rangeStr, "-", 2)
	if len(parts) != 2 {
		return timeStr == rangeStr
	}
	start := strings.TrimSpace(parts[0])
	end := strings.TrimSpace(parts[1])

	if start <= end {
		return timeStr >= start && timeStr <= end
	}
	return timeStr >= start || timeStr <= end
}
