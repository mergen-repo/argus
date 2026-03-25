package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBR4_RolloutStageJSON(t *testing.T) {
	stages := []RolloutStage{
		{Pct: 1, Status: "completed"},
		{Pct: 10, Status: "in_progress"},
		{Pct: 100, Status: "pending"},
	}

	data, err := json.Marshal(stages)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed []RolloutStage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(parsed) != 3 {
		t.Fatalf("expected 3 stages, got %d", len(parsed))
	}
	if parsed[0].Pct != 1 || parsed[0].Status != "completed" {
		t.Errorf("stage 0: pct=%d status=%s", parsed[0].Pct, parsed[0].Status)
	}
	if parsed[1].Pct != 10 || parsed[1].Status != "in_progress" {
		t.Errorf("stage 1: pct=%d status=%s", parsed[1].Pct, parsed[1].Status)
	}
	if parsed[2].Pct != 100 || parsed[2].Status != "pending" {
		t.Errorf("stage 2: pct=%d status=%s", parsed[2].Pct, parsed[2].Status)
	}
}

func TestBR4_PolicyRolloutStruct(t *testing.T) {
	now := time.Now()
	ro := PolicyRollout{
		ID:              uuid.New(),
		PolicyVersionID: uuid.New(),
		Strategy:        "canary",
		Stages:          json.RawMessage(`[{"pct":1,"status":"pending"}]`),
		TotalSIMs:       10000,
		MigratedSIMs:    0,
		CurrentStage:    0,
		State:           "in_progress",
		StartedAt:       &now,
	}

	if ro.Strategy != "canary" {
		t.Errorf("Strategy = %q, want %q", ro.Strategy, "canary")
	}
	if ro.TotalSIMs != 10000 {
		t.Errorf("TotalSIMs = %d, want 10000", ro.TotalSIMs)
	}
	if ro.MigratedSIMs != 0 {
		t.Errorf("MigratedSIMs = %d, want 0", ro.MigratedSIMs)
	}
	if ro.State != "in_progress" {
		t.Errorf("State = %q, want %q", ro.State, "in_progress")
	}
	if ro.StartedAt == nil {
		t.Error("StartedAt should not be nil")
	}
}

func TestBR4_RolloutStageWithSimCount(t *testing.T) {
	simCount := 100
	migrated := 50
	stage := RolloutStage{
		Pct:      10,
		Status:   "in_progress",
		SimCount: &simCount,
		Migrated: &migrated,
	}

	data, err := json.Marshal(stage)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed RolloutStage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.SimCount == nil || *parsed.SimCount != 100 {
		t.Error("SimCount should be 100")
	}
	if parsed.Migrated == nil || *parsed.Migrated != 50 {
		t.Error("Migrated should be 50")
	}
}

func TestBR4_AllPolicyErrorSentinels(t *testing.T) {
	errors := map[string]error{
		"ErrPolicyNotFound":        ErrPolicyNotFound,
		"ErrPolicyNameExists":      ErrPolicyNameExists,
		"ErrPolicyVersionNotFound": ErrPolicyVersionNotFound,
		"ErrPolicyInUse":           ErrPolicyInUse,
		"ErrVersionNotDraft":       ErrVersionNotDraft,
		"ErrRolloutNotFound":       ErrRolloutNotFound,
		"ErrRolloutInProgress":     ErrRolloutInProgress,
		"ErrRolloutCompleted":      ErrRolloutCompleted,
		"ErrRolloutRolledBack":     ErrRolloutRolledBack,
		"ErrStageInProgress":       ErrStageInProgress,
		"ErrVersionNotActivatable": ErrVersionNotActivatable,
	}

	for name, err := range errors {
		if err == nil {
			t.Errorf("%s should not be nil", name)
		}
		if err.Error() == "" {
			t.Errorf("%s should have a non-empty message", name)
		}
	}

	names := make([]string, 0, len(errors))
	errs := make([]error, 0, len(errors))
	for name, err := range errors {
		names = append(names, name)
		errs = append(errs, err)
	}
	for i := 0; i < len(errs); i++ {
		for j := i + 1; j < len(errs); j++ {
			if errs[i] == errs[j] {
				t.Errorf("%s and %s should be distinct", names[i], names[j])
			}
		}
	}
}

func TestBR4_CreateRolloutParams(t *testing.T) {
	userID := uuid.New()
	prevID := uuid.New()
	p := CreateRolloutParams{
		PolicyVersionID:   uuid.New(),
		PreviousVersionID: &prevID,
		Strategy:          "canary",
		Stages:            json.RawMessage(`[{"pct":1,"status":"pending"}]`),
		TotalSIMs:         5000,
		CreatedBy:         &userID,
	}

	if p.Strategy != "canary" {
		t.Errorf("Strategy = %q, want %q", p.Strategy, "canary")
	}
	if p.TotalSIMs != 5000 {
		t.Errorf("TotalSIMs = %d, want 5000", p.TotalSIMs)
	}
	if p.PreviousVersionID == nil {
		t.Error("PreviousVersionID should not be nil")
	}
	if p.CreatedBy == nil {
		t.Error("CreatedBy should not be nil")
	}
}

func TestBR4_RolloutStageNilOptionalFields(t *testing.T) {
	stage := RolloutStage{
		Pct:    1,
		Status: "pending",
	}

	if stage.SimCount != nil {
		t.Error("SimCount should be nil for pending stage")
	}
	if stage.Migrated != nil {
		t.Error("Migrated should be nil for pending stage")
	}
}
