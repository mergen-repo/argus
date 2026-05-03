package dsl

import "sort"

// VocabSnapshot is the public, FE-consumable snapshot of the DSL grammar's
// whitelisted vocabulary. Built from the parser's internal whitelists
// (validMatchFields, validChargingModels, validOverageActions, validBillingCycles,
// unitSet) plus a hard-coded list of rule keywords from token.go. Adding a
// new keyword to the parser auto-propagates to the FE autocomplete.
//
// FIX-243 Wave A — DSL real-time validate endpoint.
type VocabSnapshot struct {
	MatchFields     []string `json:"match_fields"`
	ConditionFields []string `json:"condition_fields"`
	Functions       []string `json:"functions"`
	ChargingModels  []string `json:"charging_models"`
	OverageActions  []string `json:"overage_actions"`
	BillingCycles   []string `json:"billing_cycles"`
	Units           []string `json:"units"`
	RuleKeywords    []string `json:"rule_keywords"`
	Actions         []string `json:"actions"`
}

// Vocab returns the snapshot of the DSL's whitelisted vocabulary.
// All lists are returned alphabetically sorted for deterministic output
// (FE autocomplete relies on stable ordering).
func Vocab() VocabSnapshot {
	return VocabSnapshot{
		MatchFields:     sortedKeysBool(validMatchFields),
		ConditionFields: conditionFieldList(),
		Functions:       functionList(),
		ChargingModels:  sortedKeysBool(validChargingModels),
		OverageActions:  sortedKeysBool(validOverageActions),
		BillingCycles:   sortedKeysBool(validBillingCycles),
		Units:           sortedKeysBool(unitSet),
		RuleKeywords:    ruleKeywordList(),
		Actions:         validActionList(),
	}
}

// sortedKeysBool returns the keys of a map[string]bool, alphabetically sorted.
func sortedKeysBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ruleKeywordList returns the canonical DSL block/operator keywords from
// token.go. Hard-coded to keep the FE-facing surface stable.
func ruleKeywordList() []string {
	return []string{
		"ACTION", "AND", "BETWEEN", "CHARGING", "IN",
		"MATCH", "NOT", "OR", "POLICY", "RULES", "WHEN",
	}
}

// validActionList returns the action names recognized by validateAction
// in parser.go. Kept in sync with the switch statement in
// (*Parser).validateAction.
func validActionList() []string {
	return []string{"block", "disconnect", "log", "notify", "suspend", "tag", "throttle"}
}

// conditionFieldList returns the canonical field names valid in WHEN
// conditions. Kept in sync with the switch statement in
// (*Evaluator).getConditionFieldValue. STORY-094 added the device.* and
// sim.* fields. Returned alphabetically sorted.
func conditionFieldList() []string {
	return []string{
		"device.binding_status",
		"device.imei",
		"device.imeisv",
		"device.software_version",
		"device.tac",
		"sim.binding_mode",
		"sim.binding_verified_at",
		"sim.bound_imei",
	}
}

// functionList returns the function names callable in WHEN conditions.
// STORY-094 added tac() and device.imei_in_pool(). Returned
// alphabetically sorted.
func functionList() []string {
	return []string{"device.imei_in_pool", "tac"}
}
