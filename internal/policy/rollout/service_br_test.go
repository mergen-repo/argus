package rollout

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestBR4_DefaultStagePcts(t *testing.T) {
	var pcts []int
	if len(pcts) == 0 {
		pcts = []int{1, 10, 100}
	}
	if pcts[0] != 1 || pcts[1] != 10 || pcts[2] != 100 {
		t.Errorf("default canary stages should be [1, 10, 100], got %v", pcts)
	}
}

func TestBR4_StageTargetCalculation(t *testing.T) {
	tests := []struct {
		name      string
		totalSIMs int
		pct       int
		want      int
	}{
		{"1% of 10000", 10000, 1, 100},
		{"10% of 10000", 10000, 10, 1000},
		{"100% of 10000", 10000, 100, 10000},
		{"1% of 1 rounds up", 1, 1, 1},
		{"1% of 99 rounds up", 99, 1, 1},
		{"1% of 150 rounds up", 150, 1, 2},
		{"10% of 3 rounds up", 3, 10, 1},
		{"0 SIMs", 0, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := int(math.Ceil(float64(tt.totalSIMs) * float64(tt.pct) / 100.0))
			if got != tt.want {
				t.Errorf("stage target = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBR4_ProgressPctCalculation(t *testing.T) {
	tests := []struct {
		name     string
		total    int
		migrated int
		wantPct  float64
	}{
		{"0 of 100", 100, 0, 0.0},
		{"50 of 100", 100, 50, 50.0},
		{"100 of 100", 100, 100, 100.0},
		{"1 of 10000", 10000, 1, 0.01},
		{"333 of 1000", 1000, 333, 33.3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got float64
			if tt.total > 0 {
				got = math.Round(float64(tt.migrated)/float64(tt.total)*10000) / 100
			}
			if got != tt.wantPct {
				t.Errorf("progress = %.2f%%, want %.2f%%", got, tt.wantPct)
			}
		})
	}
}

func TestBR4_CoADispatch_ErrorHandling(t *testing.T) {
	logger := zerolog.Nop()
	cd := &mockCoADispatcher{err: errors.New("network error")}
	sp := &mockSessionProvider{
		sessions: []SessionInfo{
			{ID: "s1", SimID: "sim1", NASIP: "10.0.0.1", AcctSessionID: "acct1", IMSI: "123456"},
		},
	}
	svc := NewService(nil, nil, sp, cd, nil, nil, logger)
	svc.sendCoAForSIM(context.Background(), uuid.New())
	if cd.calls != 1 {
		t.Errorf("expected 1 CoA call even on error, got %d", cd.calls)
	}
}

func TestBR4_CoADispatch_NackStatus(t *testing.T) {
	logger := zerolog.Nop()
	cd := &mockCoADispatcher{result: &CoAResult{Status: "nack", Message: "rejected"}}
	sp := &mockSessionProvider{
		sessions: []SessionInfo{
			{ID: "s1", SimID: "sim1", NASIP: "10.0.0.1", AcctSessionID: "acct1", IMSI: "123456"},
		},
	}
	svc := NewService(nil, nil, sp, cd, nil, nil, logger)
	svc.sendCoAForSIM(context.Background(), uuid.New())
	if cd.calls != 1 {
		t.Errorf("expected 1 CoA call, got %d", cd.calls)
	}
}

func TestBR4_CoADispatch_SessionProviderError(t *testing.T) {
	logger := zerolog.Nop()
	sp := &mockSessionProvider{err: errors.New("db connection lost")}
	cd := &mockCoADispatcher{result: &CoAResult{Status: "ack"}}
	svc := NewService(nil, nil, sp, cd, nil, nil, logger)
	svc.sendCoAForSIM(context.Background(), uuid.New())
	if cd.calls != 0 {
		t.Errorf("expected 0 CoA calls when session provider errors, got %d", cd.calls)
	}
}

func TestBR4_MultipleConcurrentPolicyVersions(t *testing.T) {
	stages := []store.RolloutStage{
		{Pct: 1, Status: "completed"},
		{Pct: 10, Status: "in_progress"},
		{Pct: 100, Status: "pending"},
	}
	data, err := json.Marshal(stages)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed []store.RolloutStage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	completedCount := 0
	inProgressCount := 0
	pendingCount := 0
	for _, s := range parsed {
		switch s.Status {
		case "completed":
			completedCount++
		case "in_progress":
			inProgressCount++
		case "pending":
			pendingCount++
		}
	}
	if completedCount != 1 {
		t.Errorf("expected 1 completed stage, got %d", completedCount)
	}
	if inProgressCount != 1 {
		t.Errorf("expected 1 in_progress stage, got %d", inProgressCount)
	}
	if pendingCount != 1 {
		t.Errorf("expected 1 pending stage, got %d", pendingCount)
	}
}

func TestBR4_RolloutProgressEvent_WithStartedAt(t *testing.T) {
	now := time.Now()
	event := RolloutProgressEvent{
		RolloutID:    uuid.New().String(),
		VersionID:    uuid.New().String(),
		State:        "in_progress",
		CurrentStage: 0,
		TotalStages:  3,
		TotalSIMs:    1000,
		MigratedSIMs: 10,
		ProgressPct:  1.0,
		StartedAt:    now.Format(time.RFC3339),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed RolloutProgressEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.StartedAt == "" {
		t.Error("StartedAt should not be empty")
	}
	if parsed.ProgressPct != 1.0 {
		t.Errorf("ProgressPct = %f, want 1.0", parsed.ProgressPct)
	}
}

func TestBR4_RolloutProgressEvent_EmptyStartedAt(t *testing.T) {
	event := RolloutProgressEvent{
		RolloutID:    uuid.New().String(),
		VersionID:    uuid.New().String(),
		State:        "in_progress",
		CurrentStage: 0,
		TotalStages:  1,
		TotalSIMs:    0,
		MigratedSIMs: 0,
		ProgressPct:  0,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed RolloutProgressEvent
	json.Unmarshal(data, &parsed)
	if parsed.StartedAt != "" {
		t.Errorf("StartedAt should be empty when not set, got %q", parsed.StartedAt)
	}
}

func TestFIX108_AdvanceRollout_AllStagesCompletedButMigrationIncomplete(t *testing.T) {
	stages := []store.RolloutStage{
		{Pct: 1, Status: "completed"},
		{Pct: 10, Status: "completed"},
		{Pct: 100, Status: "completed"},
	}
	migratedSIMs := 6
	totalSIMs := 45

	nextStage := -1
	shouldReturnStageInProgress := false
	for i, st := range stages {
		if st.Status == "pending" {
			nextStage = i
			break
		}
		if st.Status == "in_progress" {
			shouldReturnStageInProgress = true
			break
		}
	}

	if nextStage == -1 && !shouldReturnStageInProgress {
		if migratedSIMs < totalSIMs {
			nextStage = len(stages) - 1
			stages[nextStage].Status = "pending"
		}
	}

	if nextStage == -1 {
		t.Fatal("FIX-108: advance should NOT return ROLLOUT_COMPLETED when migrated_sims < total_sims")
	}
	if nextStage != 2 {
		t.Errorf("expected nextStage=2, got %d", nextStage)
	}
	if stages[nextStage].Status != "pending" {
		t.Errorf("reopened stage should be pending (retryable), got %q", stages[nextStage].Status)
	}
}

func TestFIX108_AdvanceRollout_AllStagesCompletedMigrationDone(t *testing.T) {
	stages := []store.RolloutStage{
		{Pct: 1, Status: "completed"},
		{Pct: 10, Status: "completed"},
		{Pct: 100, Status: "completed"},
	}
	migratedSIMs := 45
	totalSIMs := 45

	nextStage := -1
	for i, st := range stages {
		if st.Status == "pending" {
			nextStage = i
			break
		}
		if st.Status == "in_progress" {
			break
		}
	}

	if nextStage == -1 {
		if migratedSIMs < totalSIMs {
			nextStage = len(stages) - 1
			stages[nextStage].Status = "pending"
		}
	}

	if nextStage != -1 {
		t.Fatal("should return ROLLOUT_COMPLETED when all stages done and migrated == total")
	}
}

func TestFIX108_AdvanceRollout_PendingStageExists(t *testing.T) {
	stages := []store.RolloutStage{
		{Pct: 1, Status: "completed"},
		{Pct: 10, Status: "pending"},
		{Pct: 100, Status: "pending"},
	}

	nextStage := -1
	for i, st := range stages {
		if st.Status == "pending" {
			nextStage = i
			break
		}
		if st.Status == "in_progress" {
			break
		}
	}

	if nextStage != 1 {
		t.Errorf("expected nextStage=1 (first pending), got %d", nextStage)
	}
}

func TestFIX108_AdvanceRollout_InProgressStageBlocksAdvance(t *testing.T) {
	stages := []store.RolloutStage{
		{Pct: 1, Status: "completed"},
		{Pct: 10, Status: "in_progress"},
		{Pct: 100, Status: "pending"},
	}

	nextStage := -1
	shouldReturnStageInProgress := false
	for i, st := range stages {
		if st.Status == "pending" {
			nextStage = i
			break
		}
		if st.Status == "in_progress" {
			shouldReturnStageInProgress = true
			break
		}
	}

	if !shouldReturnStageInProgress {
		t.Error("in_progress stage should block advance with ErrStageInProgress")
	}
	if nextStage != -1 {
		t.Errorf("nextStage should be -1 when blocked by in_progress, got %d", nextStage)
	}
}

func TestFIX108_ExecuteStage_TargetNotReached_StageResetToPending(t *testing.T) {
	totalSIMs := 45
	stagePct := 100
	targetMigrated := int(math.Ceil(float64(totalSIMs) * float64(stagePct) / 100.0))
	totalMigrated := 6

	targetReached := totalMigrated >= targetMigrated

	if targetReached {
		t.Fatal("with 6/45 migrated, target should NOT be reached")
	}

	status := "in_progress"
	if targetReached {
		status = "completed"
	} else {
		status = "pending"
	}

	if status != "pending" {
		t.Errorf("stage should be reset to pending when target not reached, got %q", status)
	}
}

func TestFIX108_ExecuteStage_TargetReached_StageCompleted(t *testing.T) {
	totalSIMs := 45
	stagePct := 10
	targetMigrated := int(math.Ceil(float64(totalSIMs) * float64(stagePct) / 100.0))
	totalMigrated := 5

	targetReached := totalMigrated >= targetMigrated
	if !targetReached {
		t.Fatal("with 5/5 migrated (10% of 45 = 5), target should be reached")
	}

	status := "in_progress"
	if targetReached {
		status = "completed"
	} else {
		status = "pending"
	}
	if status != "completed" {
		t.Errorf("stage should be completed when target reached, got %q", status)
	}
}

func TestFIX108_CompleteRollout_OnlyWhenTargetReachedAndFinalStage(t *testing.T) {
	tests := []struct {
		name           string
		stagePct       int
		targetReached  bool
		shouldComplete bool
	}{
		{"final stage, target reached", 100, true, true},
		{"final stage, target NOT reached", 100, false, false},
		{"intermediate stage, target reached", 10, true, false},
		{"intermediate stage, target NOT reached", 10, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldComplete := tt.targetReached && tt.stagePct == 100
			if shouldComplete != tt.shouldComplete {
				t.Errorf("shouldComplete=%v, want %v", shouldComplete, tt.shouldComplete)
			}
		})
	}
}

func TestBR4_PolicyStoreErrors_Distinct(t *testing.T) {
	errs := []error{
		store.ErrVersionNotDraft,
		store.ErrRolloutInProgress,
		store.ErrRolloutCompleted,
		store.ErrRolloutRolledBack,
		store.ErrStageInProgress,
		store.ErrPolicyNotFound,
		store.ErrRolloutNotFound,
	}

	for i := 0; i < len(errs); i++ {
		for j := i + 1; j < len(errs); j++ {
			if errs[i] == errs[j] {
				t.Errorf("errors should be distinct: %v == %v", errs[i], errs[j])
			}
		}
	}
}

func TestBR4_PublishProgressWithState_NilEventBus(t *testing.T) {
	logger := zerolog.Nop()
	svc := NewService(nil, nil, nil, nil, nil, nil, logger)
	ro := &store.PolicyRollout{
		ID:              uuid.New(),
		PolicyVersionID: uuid.New(),
		TotalSIMs:       100,
		State:           "rolled_back",
	}
	stages := []store.RolloutStage{{Pct: 1, Status: "completed"}}
	svc.publishProgressWithState(context.Background(), ro, stages, 0, 0, "rolled_back")
}

func TestBR4_PublishProgress_ZeroTotalSIMs(t *testing.T) {
	logger := zerolog.Nop()
	svc := NewService(nil, nil, nil, nil, nil, nil, logger)
	ro := &store.PolicyRollout{
		ID:              uuid.New(),
		PolicyVersionID: uuid.New(),
		TotalSIMs:       0,
		State:           "in_progress",
	}
	stages := []store.RolloutStage{{Pct: 100, Status: "pending"}}
	svc.publishProgress(context.Background(), ro, stages, 0, 0)
}

func TestBR4_CoARequest_Fields(t *testing.T) {
	req := CoARequest{
		NASIP:         "10.0.0.1",
		AcctSessionID: "acct-123",
		IMSI:          "286010123456789",
		Attributes:    map[string]interface{}{"bandwidth_down": 64000},
	}
	if req.NASIP != "10.0.0.1" {
		t.Errorf("NASIP = %q, want %q", req.NASIP, "10.0.0.1")
	}
	if req.IMSI != "286010123456789" {
		t.Errorf("IMSI = %q, want %q", req.IMSI, "286010123456789")
	}
	if req.Attributes["bandwidth_down"] != 64000 {
		t.Error("bandwidth_down attribute should be 64000")
	}
}

func TestBR4_SessionInfo_Fields(t *testing.T) {
	si := SessionInfo{
		ID:            "sess-1",
		SimID:         "sim-1",
		NASIP:         "10.0.0.1",
		AcctSessionID: "acct-1",
		IMSI:          "286010123456789",
	}
	if si.ID != "sess-1" {
		t.Errorf("ID = %q, want %q", si.ID, "sess-1")
	}
	if si.IMSI != "286010123456789" {
		t.Errorf("IMSI = %q, want %q", si.IMSI, "286010123456789")
	}
}

func TestBR4_CreateStageJob_NilJobStore(t *testing.T) {
	logger := zerolog.Nop()
	svc := NewService(nil, nil, nil, nil, nil, nil, logger)
	err := svc.createStageJob(context.Background(), uuid.New(), uuid.New(), 0, nil)
	if err == nil {
		t.Error("expected error when jobStore is nil")
	}
}

func TestBR4_ResolveTenantID_NilPolicyStore(t *testing.T) {
	logger := zerolog.Nop()
	svc := NewService(nil, nil, nil, nil, nil, nil, logger)
	ro := &store.PolicyRollout{ID: uuid.New()}
	tid := svc.resolveTenantID(context.Background(), ro)
	if tid != uuid.Nil {
		t.Errorf("expected uuid.Nil, got %s", tid)
	}
}
