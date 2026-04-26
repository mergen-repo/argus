package dsl

import "testing"

func TestVocab_NonEmpty(t *testing.T) {
	v := Vocab()
	if len(v.MatchFields) == 0 {
		t.Error("MatchFields should not be empty")
	}
	if len(v.ChargingModels) == 0 {
		t.Error("ChargingModels should not be empty")
	}
	if len(v.OverageActions) == 0 {
		t.Error("OverageActions should not be empty")
	}
	if len(v.BillingCycles) == 0 {
		t.Error("BillingCycles should not be empty")
	}
	if len(v.Units) == 0 {
		t.Error("Units should not be empty")
	}
	if len(v.RuleKeywords) == 0 {
		t.Error("RuleKeywords should not be empty")
	}
	if len(v.Actions) == 0 {
		t.Error("Actions should not be empty")
	}
}

func TestVocab_Sorted(t *testing.T) {
	v := Vocab()
	for _, list := range [][]string{
		v.MatchFields, v.ChargingModels, v.OverageActions,
		v.BillingCycles, v.Units, v.RuleKeywords, v.Actions,
	} {
		for i := 1; i < len(list); i++ {
			if list[i-1] > list[i] {
				t.Errorf("vocab list is not sorted: %v", list)
				break
			}
		}
	}
}

func TestVocab_ContainsKnownEntries(t *testing.T) {
	v := Vocab()
	containsStr := func(haystack []string, needle string) bool {
		for _, h := range haystack {
			if h == needle {
				return true
			}
		}
		return false
	}
	if !containsStr(v.MatchFields, "apn") {
		t.Errorf("MatchFields should contain 'apn', got %v", v.MatchFields)
	}
	if !containsStr(v.ChargingModels, "prepaid") {
		t.Errorf("ChargingModels should contain 'prepaid', got %v", v.ChargingModels)
	}
	if !containsStr(v.OverageActions, "throttle") {
		t.Errorf("OverageActions should contain 'throttle', got %v", v.OverageActions)
	}
	if !containsStr(v.BillingCycles, "monthly") {
		t.Errorf("BillingCycles should contain 'monthly', got %v", v.BillingCycles)
	}
	if !containsStr(v.Units, "mb") {
		t.Errorf("Units should contain 'mb', got %v", v.Units)
	}
	if !containsStr(v.RuleKeywords, "POLICY") {
		t.Errorf("RuleKeywords should contain 'POLICY', got %v", v.RuleKeywords)
	}
	if !containsStr(v.Actions, "notify") {
		t.Errorf("Actions should contain 'notify', got %v", v.Actions)
	}
}
