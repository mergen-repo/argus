package rollout

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type mockCoAStatusUpdater struct {
	calls  []string
	simIDs []uuid.UUID
	err    error
}

func (m *mockCoAStatusUpdater) UpdateAssignmentCoAStatus(_ context.Context, simID uuid.UUID, status string) error {
	m.calls = append(m.calls, status)
	m.simIDs = append(m.simIDs, simID)
	return m.err
}

type mockSessionProvider struct {
	sessions []SessionInfo
	err      error
}

func (m *mockSessionProvider) GetSessionsForSIM(ctx context.Context, simID string) ([]SessionInfo, error) {
	return m.sessions, m.err
}

type mockCoADispatcher struct {
	result *CoAResult
	err    error
	calls  int
}

func (m *mockCoADispatcher) SendCoA(ctx context.Context, req CoARequest) (*CoAResult, error) {
	m.calls++
	return m.result, m.err
}

func TestNewService(t *testing.T) {
	logger := zerolog.Nop()
	svc := NewService(nil, nil, nil, nil, nil, nil, logger)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

func TestSetSessionProvider(t *testing.T) {
	logger := zerolog.Nop()
	svc := NewService(nil, nil, nil, nil, nil, nil, logger)
	mock := &mockSessionProvider{}
	svc.SetSessionProvider(mock)
	if svc.sessionProvider != mock {
		t.Error("SetSessionProvider did not set the provider")
	}
}

func TestSetCoADispatcher(t *testing.T) {
	logger := zerolog.Nop()
	svc := NewService(nil, nil, nil, nil, nil, nil, logger)
	mock := &mockCoADispatcher{}
	svc.SetCoADispatcher(mock)
	if svc.coaDispatcher != mock {
		t.Error("SetCoADispatcher did not set the dispatcher")
	}
}

func TestSendCoAForSIM_NilProviders(t *testing.T) {
	logger := zerolog.Nop()
	mu := &mockCoAStatusUpdater{}
	svc := NewService(nil, nil, nil, nil, nil, nil, logger)
	svc.coaStatusUpdater = mu
	svc.sendCoAForSIM(context.Background(), uuid.New())
	if len(mu.calls) != 1 || mu.calls[0] != CoAStatusNoSession {
		t.Errorf("expected UpdateAssignmentCoAStatus(%q), got %v", CoAStatusNoSession, mu.calls)
	}
}

func TestSendCoAForSIM_NoSessions(t *testing.T) {
	logger := zerolog.Nop()
	sp := &mockSessionProvider{sessions: nil}
	cd := &mockCoADispatcher{result: &CoAResult{Status: "ack"}}
	mu := &mockCoAStatusUpdater{}
	svc := NewService(nil, nil, sp, cd, nil, nil, logger)
	svc.coaStatusUpdater = mu
	svc.sendCoAForSIM(context.Background(), uuid.New())
	if cd.calls != 0 {
		t.Errorf("expected 0 CoA calls, got %d", cd.calls)
	}
	if len(mu.calls) != 1 || mu.calls[0] != CoAStatusNoSession {
		t.Errorf("expected UpdateAssignmentCoAStatus(%q), got %v", CoAStatusNoSession, mu.calls)
	}
}

func TestSendCoAForSIM_WithSessions(t *testing.T) {
	logger := zerolog.Nop()
	sp := &mockSessionProvider{
		sessions: []SessionInfo{
			{ID: "s1", SimID: "sim1", NASIP: "10.0.0.1", AcctSessionID: "acct1", IMSI: "123456"},
			{ID: "s2", SimID: "sim1", NASIP: "10.0.0.2", AcctSessionID: "acct2", IMSI: "123456"},
		},
	}
	cd := &mockCoADispatcher{result: &CoAResult{Status: "ack"}}
	mu := &mockCoAStatusUpdater{}
	svc := NewService(nil, nil, sp, cd, nil, nil, logger)
	svc.coaStatusUpdater = mu
	svc.sendCoAForSIM(context.Background(), uuid.New())
	if cd.calls != 2 {
		t.Errorf("expected 2 CoA calls, got %d", cd.calls)
	}
	// expect: queued (before loop) + acked (session 1) + acked (session 2)
	want := []string{CoAStatusQueued, CoAStatusAcked, CoAStatusAcked}
	if len(mu.calls) != len(want) {
		t.Fatalf("expected %d status writes, got %d: %v", len(want), len(mu.calls), mu.calls)
	}
	for i, w := range want {
		if mu.calls[i] != w {
			t.Errorf("status[%d]: want %q, got %q", i, w, mu.calls[i])
		}
	}
}

func TestSendCoAForSIM_DispatchError(t *testing.T) {
	logger := zerolog.Nop()
	sp := &mockSessionProvider{
		sessions: []SessionInfo{
			{ID: "s1", SimID: "sim1", NASIP: "10.0.0.1", AcctSessionID: "acct1", IMSI: "123456"},
		},
	}
	cd := &mockCoADispatcher{err: errors.New("radius timeout")}
	mu := &mockCoAStatusUpdater{}
	svc := NewService(nil, nil, sp, cd, nil, nil, logger)
	svc.coaStatusUpdater = mu
	svc.sendCoAForSIM(context.Background(), uuid.New())
	// expect: queued (before loop) + failed (dispatch returned error)
	want := []string{CoAStatusQueued, CoAStatusFailed}
	if len(mu.calls) != len(want) {
		t.Fatalf("expected %d status writes, got %d: %v", len(want), len(mu.calls), mu.calls)
	}
	for i, w := range want {
		if mu.calls[i] != w {
			t.Errorf("status[%d]: want %q, got %q", i, w, mu.calls[i])
		}
	}
}

func TestSendCoAForSIM_NonAckResult(t *testing.T) {
	logger := zerolog.Nop()
	sp := &mockSessionProvider{
		sessions: []SessionInfo{
			{ID: "s1", SimID: "sim1", NASIP: "10.0.0.1", AcctSessionID: "acct1", IMSI: "123456"},
		},
	}
	cd := &mockCoADispatcher{result: &CoAResult{Status: "nak"}}
	mu := &mockCoAStatusUpdater{}
	svc := NewService(nil, nil, sp, cd, nil, nil, logger)
	svc.coaStatusUpdater = mu
	svc.sendCoAForSIM(context.Background(), uuid.New())
	// expect: queued (before loop) + failed (result.Status != "ack")
	want := []string{CoAStatusQueued, CoAStatusFailed}
	if len(mu.calls) != len(want) {
		t.Fatalf("expected %d status writes, got %d: %v", len(want), len(mu.calls), mu.calls)
	}
	for i, w := range want {
		if mu.calls[i] != w {
			t.Errorf("status[%d]: want %q, got %q", i, w, mu.calls[i])
		}
	}
}

func TestPublishProgress_NilEventBus(t *testing.T) {
	logger := zerolog.Nop()
	svc := NewService(nil, nil, nil, nil, nil, nil, logger)
	ro := &store.PolicyRollout{
		ID:              uuid.New(),
		PolicyVersionID: uuid.New(),
		TotalSIMs:       100,
		State:           "in_progress",
	}
	stages := []store.RolloutStage{
		{Pct: 1, Status: "completed"},
		{Pct: 10, Status: "in_progress"},
	}
	svc.publishProgress(context.Background(), ro, stages, 10, 1)
}

func TestRolloutStageJSON(t *testing.T) {
	stages := []store.RolloutStage{
		{Pct: 1, Status: "pending"},
		{Pct: 10, Status: "pending"},
		{Pct: 100, Status: "pending"},
	}
	data, err := json.Marshal(stages)
	if err != nil {
		t.Fatalf("marshal stages: %v", err)
	}

	var parsed []store.RolloutStage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal stages: %v", err)
	}

	if len(parsed) != 3 {
		t.Errorf("expected 3 stages, got %d", len(parsed))
	}
	if parsed[0].Pct != 1 {
		t.Errorf("expected first stage pct=1, got %d", parsed[0].Pct)
	}
	if parsed[2].Pct != 100 {
		t.Errorf("expected last stage pct=100, got %d", parsed[2].Pct)
	}
}

func TestRolloutProgressEvent_Serialization(t *testing.T) {
	event := RolloutProgressEvent{
		RolloutID:    uuid.New().String(),
		VersionID:    uuid.New().String(),
		State:        "in_progress",
		CurrentStage: 1,
		TotalStages:  3,
		Stages: []store.RolloutStage{
			{Pct: 1, Status: "completed"},
			{Pct: 10, Status: "in_progress"},
			{Pct: 100, Status: "pending"},
		},
		TotalSIMs:    10000,
		MigratedSIMs: 100,
		ProgressPct:  1.0,
		StartedAt:    "2026-03-21T10:00:00Z",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	var parsed RolloutProgressEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	if parsed.State != "in_progress" {
		t.Errorf("expected state=in_progress, got %s", parsed.State)
	}
	if parsed.TotalSIMs != 10000 {
		t.Errorf("expected total_sims=10000, got %d", parsed.TotalSIMs)
	}
	if parsed.MigratedSIMs != 100 {
		t.Errorf("expected migrated_sims=100, got %d", parsed.MigratedSIMs)
	}
	if len(parsed.Stages) != 3 {
		t.Errorf("expected 3 stages, got %d", len(parsed.Stages))
	}
}

func TestDefaultStages(t *testing.T) {
	var stagePcts []int
	if len(stagePcts) == 0 {
		stagePcts = []int{1, 10, 100}
	}

	if len(stagePcts) != 3 {
		t.Errorf("expected 3 default stages, got %d", len(stagePcts))
	}
	if stagePcts[0] != 1 || stagePcts[1] != 10 || stagePcts[2] != 100 {
		t.Errorf("unexpected default stages: %v", stagePcts)
	}
}
