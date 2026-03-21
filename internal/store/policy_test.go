package store

import (
	"encoding/json"
	"testing"
)

func TestPolicyStruct(t *testing.T) {
	desc := "Test policy description"
	p := Policy{
		Name:        "test-policy",
		Description: &desc,
		Scope:       "global",
		State:       "active",
	}

	if p.Name != "test-policy" {
		t.Errorf("Name = %q, want %q", p.Name, "test-policy")
	}
	if p.Description == nil || *p.Description != desc {
		t.Errorf("Description = %v, want %q", p.Description, desc)
	}
	if p.Scope != "global" {
		t.Errorf("Scope = %q, want %q", p.Scope, "global")
	}
	if p.State != "active" {
		t.Errorf("State = %q, want %q", p.State, "active")
	}
	if p.CurrentVersionID != nil {
		t.Error("CurrentVersionID should be nil by default")
	}
	if p.ScopeRefID != nil {
		t.Error("ScopeRefID should be nil by default")
	}
}

func TestPolicyVersionStruct(t *testing.T) {
	compiled := json.RawMessage(`{"name":"test","version":"1.0","match":{},"rules":{}}`)
	v := PolicyVersion{
		Version:       1,
		DSLContent:    `POLICY "test" { RULES { bandwidth_down = 1mbps } }`,
		CompiledRules: compiled,
		State:         "draft",
	}

	if v.Version != 1 {
		t.Errorf("Version = %d, want %d", v.Version, 1)
	}
	if v.DSLContent == "" {
		t.Error("DSLContent should not be empty")
	}
	if v.CompiledRules == nil {
		t.Error("CompiledRules should not be nil")
	}
	if v.State != "draft" {
		t.Errorf("State = %q, want %q", v.State, "draft")
	}
	if v.ActivatedAt != nil {
		t.Error("ActivatedAt should be nil for draft")
	}
	if v.RolledBackAt != nil {
		t.Error("RolledBackAt should be nil for draft")
	}
	if v.AffectedSIMCount != nil {
		t.Error("AffectedSIMCount should be nil")
	}
}

func TestCreatePolicyParams(t *testing.T) {
	desc := "A policy"
	p := CreatePolicyParams{
		Name:          "my-policy",
		Description:   &desc,
		Scope:         "apn",
		DSLContent:    `POLICY "my-policy" {}`,
		CompiledRules: json.RawMessage(`{}`),
	}

	if p.Name != "my-policy" {
		t.Errorf("Name = %q, want %q", p.Name, "my-policy")
	}
	if p.Scope != "apn" {
		t.Errorf("Scope = %q, want %q", p.Scope, "apn")
	}
	if p.CreatedBy != nil {
		t.Error("CreatedBy should be nil by default")
	}
}

func TestUpdatePolicyParams(t *testing.T) {
	name := "updated-name"
	p := UpdatePolicyParams{
		Name: &name,
	}

	if p.Name == nil || *p.Name != "updated-name" {
		t.Error("Name should be set")
	}
	if p.Description != nil {
		t.Error("Description should be nil")
	}
	if p.State != nil {
		t.Error("State should be nil")
	}
}

func TestCreateVersionParams(t *testing.T) {
	p := CreateVersionParams{
		DSLContent:    `POLICY "test" {}`,
		CompiledRules: json.RawMessage(`{}`),
	}

	if p.DSLContent == "" {
		t.Error("DSLContent should not be empty")
	}
	if p.CompiledRules == nil {
		t.Error("CompiledRules should not be nil")
	}
}

func TestErrPolicyNotFound(t *testing.T) {
	if ErrPolicyNotFound.Error() != "store: policy not found" {
		t.Errorf("ErrPolicyNotFound = %q", ErrPolicyNotFound.Error())
	}
}

func TestErrPolicyNameExists(t *testing.T) {
	if ErrPolicyNameExists.Error() != "store: policy name already exists for this tenant" {
		t.Errorf("ErrPolicyNameExists = %q", ErrPolicyNameExists.Error())
	}
}

func TestErrPolicyVersionNotFound(t *testing.T) {
	if ErrPolicyVersionNotFound.Error() != "store: policy version not found" {
		t.Errorf("ErrPolicyVersionNotFound = %q", ErrPolicyVersionNotFound.Error())
	}
}

func TestErrPolicyInUse(t *testing.T) {
	if ErrPolicyInUse.Error() != "store: policy has assigned SIMs" {
		t.Errorf("ErrPolicyInUse = %q", ErrPolicyInUse.Error())
	}
}

func TestErrVersionNotDraft(t *testing.T) {
	if ErrVersionNotDraft.Error() != "store: version is not in draft state" {
		t.Errorf("ErrVersionNotDraft = %q", ErrVersionNotDraft.Error())
	}
}

func TestPolicyVersionCompiledRulesJSON(t *testing.T) {
	compiled := json.RawMessage(`{"name":"iot-fleet","version":"1.0","match":{"conditions":[{"field":"apn","op":"in","values":["iot.fleet"]}]},"rules":{"defaults":{"bandwidth_down":1000000},"when_blocks":[]}}`)
	v := PolicyVersion{
		CompiledRules: compiled,
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(v.CompiledRules, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal compiled rules: %v", err)
	}
	if parsed["name"] != "iot-fleet" {
		t.Errorf("parsed name = %v, want %q", parsed["name"], "iot-fleet")
	}
}

func TestPolicyVersionStates(t *testing.T) {
	validStates := []string{"draft", "active", "superseded", "archived"}
	for _, state := range validStates {
		v := PolicyVersion{State: state}
		if v.State != state {
			t.Errorf("State = %q, want %q", v.State, state)
		}
	}
}

func TestPolicyScopeValues(t *testing.T) {
	validScopes := []string{"global", "operator", "apn", "sim"}
	for _, scope := range validScopes {
		p := Policy{Scope: scope}
		if p.Scope != scope {
			t.Errorf("Scope = %q, want %q", p.Scope, scope)
		}
	}
}

func TestNewPolicyStore(t *testing.T) {
	s := NewPolicyStore(nil)
	if s == nil {
		t.Fatal("NewPolicyStore returned nil")
	}
}
