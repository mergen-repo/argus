package job

import (
	"encoding/json"
	"testing"
)

func TestRolloutStageProcessor_Type(t *testing.T) {
	p := &RolloutStageProcessor{}
	if p.Type() != "policy_rollout_stage" {
		t.Errorf("Type() = %q, want %q", p.Type(), "policy_rollout_stage")
	}
}

func TestRolloutStagePayload_Unmarshal(t *testing.T) {
	raw := `{"rollout_id":"550e8400-e29b-41d4-a716-446655440000","stage_index":1,"tenant_id":"660e8400-e29b-41d4-a716-446655440000"}`
	var payload rolloutStagePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.RolloutID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("rollout_id = %q", payload.RolloutID)
	}
	if payload.StageIndex != 1 {
		t.Errorf("stage_index = %d", payload.StageIndex)
	}
	if payload.TenantID != "660e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("tenant_id = %q", payload.TenantID)
	}
}

func TestRolloutStagePayload_MissingFields(t *testing.T) {
	raw := `{}`
	var payload rolloutStagePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.RolloutID != "" {
		t.Errorf("expected empty rollout_id, got %q", payload.RolloutID)
	}
	if payload.StageIndex != 0 {
		t.Errorf("expected stage_index=0, got %d", payload.StageIndex)
	}
}
