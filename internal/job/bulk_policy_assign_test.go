package job

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// --- test doubles ---

type fakeBulkSessionProvider struct {
	mu       sync.Mutex
	byID     map[string][]BulkSessionInfo
	errOnID  map[string]error
	callLog  []string
}

func newFakeBulkSessionProvider() *fakeBulkSessionProvider {
	return &fakeBulkSessionProvider{
		byID:    make(map[string][]BulkSessionInfo),
		errOnID: make(map[string]error),
	}
}

func (f *fakeBulkSessionProvider) GetSessionsForSIM(_ context.Context, simID string) ([]BulkSessionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callLog = append(f.callLog, simID)
	if err, ok := f.errOnID[simID]; ok {
		return nil, err
	}
	return f.byID[simID], nil
}

type fakeBulkCoADispatcher struct {
	mu       sync.Mutex
	status   string // "ack", "nak", "timeout"
	returnErr error
	sent     []BulkCoARequest
}

func (f *fakeBulkCoADispatcher) SendCoA(_ context.Context, req BulkCoARequest) (*BulkCoAResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, req)
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	status := f.status
	if status == "" {
		status = "ack"
	}
	return &BulkCoAResult{Status: status}, nil
}

type fakeBulkPolicyUpdater struct {
	mu      sync.Mutex
	updates map[string]string // simID -> status
}

func newFakeBulkPolicyUpdater() *fakeBulkPolicyUpdater {
	return &fakeBulkPolicyUpdater{updates: make(map[string]string)}
}

func (f *fakeBulkPolicyUpdater) UpdateAssignmentCoAStatus(_ context.Context, simID uuid.UUID, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates[simID.String()] = status
	return nil
}

// --- helpers ---

func newTestProcessor() *BulkPolicyAssignProcessor {
	return &BulkPolicyAssignProcessor{
		logger: zerolog.New(io.Discard),
	}
}

// --- tests ---

func TestBulkPolicyAssignProcessorType(t *testing.T) {
	p := newTestProcessor()
	if p.Type() != JobTypeBulkPolicyAssign {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeBulkPolicyAssign)
	}
}

func TestBulkPolicyAssign_DispatchCoA_MixedSessions(t *testing.T) {
	simWithSessions := uuid.New()
	simWithoutSessions := uuid.New()
	simWithSessions2 := uuid.New()

	sp := newFakeBulkSessionProvider()
	sp.byID[simWithSessions.String()] = []BulkSessionInfo{
		{ID: "s1", SimID: simWithSessions.String(), NASIP: "10.0.0.1", AcctSessionID: "acct-1", IMSI: "imsi-1"},
	}
	sp.byID[simWithSessions2.String()] = []BulkSessionInfo{
		{ID: "s2", SimID: simWithSessions2.String(), NASIP: "10.0.0.2", AcctSessionID: "acct-2", IMSI: "imsi-2"},
	}
	// simWithoutSessions has no entry — returns nil

	coa := &fakeBulkCoADispatcher{status: "ack"}
	pu := newFakeBulkPolicyUpdater()

	p := newTestProcessor()
	p.SetSessionProvider(sp)
	p.SetCoADispatcher(coa)
	p.SetPolicyCoAUpdater(pu)

	ctx := context.Background()

	var totalSent, totalAcked, totalFailed int
	for _, sim := range []uuid.UUID{simWithSessions, simWithoutSessions, simWithSessions2} {
		s, a, f := p.dispatchCoAForSIM(ctx, sim)
		totalSent += s
		totalAcked += a
		totalFailed += f
	}

	if totalSent != 2 {
		t.Errorf("coaSent = %d, want 2", totalSent)
	}
	if totalAcked != 2 {
		t.Errorf("coaAcked = %d, want 2", totalAcked)
	}
	if totalFailed != 0 {
		t.Errorf("coaFailed = %d, want 0", totalFailed)
	}
	if len(coa.sent) != 2 {
		t.Errorf("dispatcher calls = %d, want 2", len(coa.sent))
	}
	if got := pu.updates[simWithSessions.String()]; got != BulkCoAStatusAcked {
		t.Errorf("policy updater status for sim1 = %q, want %q", got, BulkCoAStatusAcked)
	}
	if got := pu.updates[simWithSessions2.String()]; got != BulkCoAStatusAcked {
		t.Errorf("policy updater status for sim2 = %q, want %q", got, BulkCoAStatusAcked)
	}
	if _, ok := pu.updates[simWithoutSessions.String()]; ok {
		t.Errorf("policy updater should not be called for sim without sessions")
	}
}

func TestBulkPolicyAssign_NoSessionStore_GracefulDegradation(t *testing.T) {
	p := newTestProcessor()
	// sessionProvider is nil — should not panic and should return all zero counts
	sent, acked, failed := p.dispatchCoAForSIM(context.Background(), uuid.New())
	if sent != 0 || acked != 0 || failed != 0 {
		t.Errorf("expected all zero counts, got sent=%d acked=%d failed=%d", sent, acked, failed)
	}
}

func TestBulkPolicyAssign_NoCoADispatcher_GracefulDegradation(t *testing.T) {
	sp := newFakeBulkSessionProvider()
	sp.byID["any"] = []BulkSessionInfo{{ID: "s1"}}

	p := newTestProcessor()
	p.SetSessionProvider(sp)
	// coaDispatcher intentionally left nil

	sent, acked, failed := p.dispatchCoAForSIM(context.Background(), uuid.New())
	if sent != 0 || acked != 0 || failed != 0 {
		t.Errorf("expected all zero counts when dispatcher nil, got sent=%d acked=%d failed=%d", sent, acked, failed)
	}
}

func TestBulkPolicyAssign_CoA_NAK_CountsAsFailed(t *testing.T) {
	simID := uuid.New()
	sp := newFakeBulkSessionProvider()
	sp.byID[simID.String()] = []BulkSessionInfo{
		{ID: "s1", SimID: simID.String(), NASIP: "10.0.0.1", AcctSessionID: "acct-1", IMSI: "imsi-1"},
	}
	coa := &fakeBulkCoADispatcher{status: "nak"}
	pu := newFakeBulkPolicyUpdater()

	p := newTestProcessor()
	p.SetSessionProvider(sp)
	p.SetCoADispatcher(coa)
	p.SetPolicyCoAUpdater(pu)

	sent, acked, failed := p.dispatchCoAForSIM(context.Background(), simID)
	if sent != 1 {
		t.Errorf("sent = %d, want 1", sent)
	}
	if acked != 0 {
		t.Errorf("acked = %d, want 0", acked)
	}
	if failed != 1 {
		t.Errorf("failed = %d, want 1", failed)
	}
	if got := pu.updates[simID.String()]; got != BulkCoAStatusFailed {
		t.Errorf("policy status = %q, want %q", got, BulkCoAStatusFailed)
	}
}

func TestBulkPolicyAssign_CoA_DispatcherError_CountsAsFailed(t *testing.T) {
	simID := uuid.New()
	sp := newFakeBulkSessionProvider()
	sp.byID[simID.String()] = []BulkSessionInfo{
		{ID: "s1", SimID: simID.String(), NASIP: "10.0.0.1", AcctSessionID: "acct-1", IMSI: "imsi-1"},
	}
	coa := &fakeBulkCoADispatcher{returnErr: errors.New("network down")}
	pu := newFakeBulkPolicyUpdater()

	p := newTestProcessor()
	p.SetSessionProvider(sp)
	p.SetCoADispatcher(coa)
	p.SetPolicyCoAUpdater(pu)

	sent, acked, failed := p.dispatchCoAForSIM(context.Background(), simID)
	if sent != 1 {
		t.Errorf("sent = %d, want 1", sent)
	}
	if acked != 0 {
		t.Errorf("acked = %d, want 0", acked)
	}
	if failed != 1 {
		t.Errorf("failed = %d, want 1", failed)
	}
	if got := pu.updates[simID.String()]; got != BulkCoAStatusFailed {
		t.Errorf("policy status = %q, want %q", got, BulkCoAStatusFailed)
	}
}

func TestBulkPolicyAssign_CoA_SessionProviderError_Returns_Zero(t *testing.T) {
	simID := uuid.New()
	sp := newFakeBulkSessionProvider()
	sp.errOnID[simID.String()] = errors.New("db unavailable")

	p := newTestProcessor()
	p.SetSessionProvider(sp)
	p.SetCoADispatcher(&fakeBulkCoADispatcher{status: "ack"})

	sent, acked, failed := p.dispatchCoAForSIM(context.Background(), simID)
	if sent != 0 || acked != 0 || failed != 0 {
		t.Errorf("expected zero counts on session provider error, got sent=%d acked=%d failed=%d", sent, acked, failed)
	}
}

func TestBulkPolicyAssign_NilPolicyUpdater_DoesNotPanic(t *testing.T) {
	simID := uuid.New()
	sp := newFakeBulkSessionProvider()
	sp.byID[simID.String()] = []BulkSessionInfo{
		{ID: "s1", SimID: simID.String(), NASIP: "10.0.0.1", AcctSessionID: "acct-1", IMSI: "imsi-1"},
	}

	p := newTestProcessor()
	p.SetSessionProvider(sp)
	p.SetCoADispatcher(&fakeBulkCoADispatcher{status: "ack"})
	// policyUpdater intentionally left nil

	sent, acked, failed := p.dispatchCoAForSIM(context.Background(), simID)
	if sent != 1 || acked != 1 || failed != 0 {
		t.Errorf("expected sent=1 acked=1 failed=0, got sent=%d acked=%d failed=%d", sent, acked, failed)
	}
}

func TestBulkResult_CoACountersJSON(t *testing.T) {
	result := BulkResult{
		ProcessedCount: 3,
		FailedCount:    0,
		TotalCount:     3,
		CoASentCount:   2,
		CoAAckedCount:  2,
		CoAFailedCount: 0,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if v := int(decoded["coa_sent_count"].(float64)); v != 2 {
		t.Errorf("coa_sent_count = %d, want 2", v)
	}
	if v := int(decoded["coa_acked_count"].(float64)); v != 2 {
		t.Errorf("coa_acked_count = %d, want 2", v)
	}
	// zero CoAFailedCount should be omitted due to omitempty
	if _, present := decoded["coa_failed_count"]; present {
		t.Errorf("coa_failed_count should be omitted when zero")
	}
}

func TestBulkResult_CoACountersOmittedWhenZero(t *testing.T) {
	result := BulkResult{
		ProcessedCount: 10,
		FailedCount:    0,
		TotalCount:     10,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"coa_sent_count", "coa_acked_count", "coa_failed_count"} {
		if _, present := decoded[k]; present {
			t.Errorf("%s should be omitted when zero", k)
		}
	}
}
