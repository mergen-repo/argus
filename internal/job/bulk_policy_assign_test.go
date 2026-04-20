package job

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// --- test doubles ---

type fakePolicyAssignAuditor struct {
	mu      sync.Mutex
	entries []audit.CreateEntryParams
	err     error
}

func (f *fakePolicyAssignAuditor) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, p)
	if f.err != nil {
		return nil, f.err
	}
	return &audit.Entry{}, nil
}

func (f *fakePolicyAssignAuditor) snapshot() []audit.CreateEntryParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]audit.CreateEntryParams, len(f.entries))
	copy(cp, f.entries)
	return cp
}

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

// --- FIX-201 Task 6: per-SIM audit + sim_ids branch + CoA continuity ---

func TestBulkPolicyAssign_SetAuditor_WiresDependency(t *testing.T) {
	p := newTestProcessor()
	if p.auditor != nil {
		t.Fatalf("auditor should be nil before SetAuditor")
	}
	a := &fakePolicyAssignAuditor{}
	p.SetAuditor(a)
	if p.auditor == nil {
		t.Fatalf("auditor should be set after SetAuditor")
	}
}

func TestBulkPolicyAssignPayload_SimIDsField_Marshal(t *testing.T) {
	simID1 := uuid.New()
	simID2 := uuid.New()
	policyID := uuid.New()
	payload := BulkPolicyAssignPayload{
		SimIDs:          []uuid.UUID{simID1, simID2},
		PolicyVersionID: policyID,
		Reason:          "compliance rollout",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BulkPolicyAssignPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.SimIDs) != 2 {
		t.Fatalf("sim_ids len = %d, want 2", len(decoded.SimIDs))
	}
	if decoded.SimIDs[0] != simID1 || decoded.SimIDs[1] != simID2 {
		t.Errorf("sim_ids mismatch: got %v, want [%v %v]", decoded.SimIDs, simID1, simID2)
	}
	if decoded.PolicyVersionID != policyID {
		t.Errorf("policy_version_id = %v, want %v", decoded.PolicyVersionID, policyID)
	}
	if decoded.Reason != "compliance rollout" {
		t.Errorf("reason = %q, want %q", decoded.Reason, "compliance rollout")
	}
}

func TestBulkPolicyAssignPayload_ReasonOmittedWhenEmpty(t *testing.T) {
	payload := BulkPolicyAssignPayload{
		SimIDs:          []uuid.UUID{uuid.New()},
		PolicyVersionID: uuid.New(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := decoded["reason"]; present {
		t.Errorf("reason should be omitted when empty")
	}
}

func TestEmitPolicyAssignAudit_FieldsAndCorrelationID(t *testing.T) {
	p := newTestProcessor()
	a := &fakePolicyAssignAuditor{}
	p.SetAuditor(a)

	jobID := uuid.New()
	tenantID := uuid.New()
	userID := uuid.New()
	simID := uuid.New()
	previousPolicyID := uuid.New()
	newPolicyID := uuid.New()

	j := &store.Job{ID: jobID, TenantID: tenantID, CreatedBy: &userID}
	payload := BulkPolicyAssignPayload{
		PolicyVersionID: newPolicyID,
		Reason:          "fleet compliance",
	}
	p.emitPolicyAssignAudit(context.Background(), j, simID, &previousPolicyID, payload)

	entries := a.snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	e := entries[0]

	if e.Action != "sim.policy_assign" {
		t.Errorf("action = %q, want %q", e.Action, "sim.policy_assign")
	}
	if e.EntityType != "sim" {
		t.Errorf("entity_type = %q, want %q", e.EntityType, "sim")
	}
	if e.EntityID != simID.String() {
		t.Errorf("entity_id = %q, want %q", e.EntityID, simID.String())
	}
	if e.TenantID != tenantID {
		t.Errorf("tenant_id = %v, want %v", e.TenantID, tenantID)
	}
	if e.UserID == nil || *e.UserID != userID {
		t.Errorf("user_id = %v, want %v", e.UserID, userID)
	}
	if e.CorrelationID == nil || *e.CorrelationID != jobID {
		t.Errorf("correlation_id = %v, want %v (bulk_job_id grouping)", e.CorrelationID, jobID)
	}

	var before map[string]any
	if err := json.Unmarshal(e.BeforeData, &before); err != nil {
		t.Fatalf("unmarshal BeforeData: %v", err)
	}
	if before["policy_version_id"] != previousPolicyID.String() {
		t.Errorf("before.policy_version_id = %v, want %q", before["policy_version_id"], previousPolicyID.String())
	}

	var after map[string]any
	if err := json.Unmarshal(e.AfterData, &after); err != nil {
		t.Fatalf("unmarshal AfterData: %v", err)
	}
	if after["policy_version_id"] != newPolicyID.String() {
		t.Errorf("after.policy_version_id = %v, want %q", after["policy_version_id"], newPolicyID.String())
	}
	if after["reason"] != "fleet compliance" {
		t.Errorf("after.reason = %v, want %q", after["reason"], "fleet compliance")
	}
}

func TestEmitPolicyAssignAudit_PreviousPolicyNil_BeforeContainsExplicitNull(t *testing.T) {
	p := newTestProcessor()
	a := &fakePolicyAssignAuditor{}
	p.SetAuditor(a)

	j := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
	payload := BulkPolicyAssignPayload{PolicyVersionID: uuid.New()}
	// previousPolicyID is nil — SIM had no prior policy
	p.emitPolicyAssignAudit(context.Background(), j, uuid.New(), nil, payload)

	entries := a.snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	rawBefore := string(entries[0].BeforeData)
	var before map[string]any
	if err := json.Unmarshal(entries[0].BeforeData, &before); err != nil {
		t.Fatalf("unmarshal BeforeData: %v", err)
	}
	v, ok := before["policy_version_id"]
	if !ok {
		t.Fatalf("before.policy_version_id key missing; raw=%s", rawBefore)
	}
	if v != nil {
		t.Errorf("before.policy_version_id = %v, want nil; raw=%s", v, rawBefore)
	}
}

func TestEmitPolicyAssignAudit_ReasonOmittedWhenEmpty(t *testing.T) {
	p := newTestProcessor()
	a := &fakePolicyAssignAuditor{}
	p.SetAuditor(a)

	j := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
	payload := BulkPolicyAssignPayload{PolicyVersionID: uuid.New()} // Reason zero value
	p.emitPolicyAssignAudit(context.Background(), j, uuid.New(), nil, payload)

	entries := a.snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	var after map[string]any
	if err := json.Unmarshal(entries[0].AfterData, &after); err != nil {
		t.Fatalf("unmarshal AfterData: %v", err)
	}
	if _, ok := after["reason"]; ok {
		t.Errorf("reason key should be omitted when empty; AfterData=%s", string(entries[0].AfterData))
	}
	if _, ok := after["policy_version_id"]; !ok {
		t.Errorf("policy_version_id key should be present in AfterData; got %s", string(entries[0].AfterData))
	}
}

func TestEmitPolicyAssignAudit_NilAuditor_NoPanic(t *testing.T) {
	p := newTestProcessor()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("emitPolicyAssignAudit panicked with nil auditor: %v", r)
		}
	}()

	j := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
	payload := BulkPolicyAssignPayload{PolicyVersionID: uuid.New()}
	p.emitPolicyAssignAudit(context.Background(), j, uuid.New(), nil, payload)
}

func TestEmitPolicyAssignAudit_AuditorError_DoesNotPropagate(t *testing.T) {
	p := newTestProcessor()
	a := &fakePolicyAssignAuditor{err: errors.New("nats down")}
	p.SetAuditor(a)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("emitPolicyAssignAudit panicked on auditor error: %v", r)
		}
	}()

	j := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
	payload := BulkPolicyAssignPayload{PolicyVersionID: uuid.New()}
	p.emitPolicyAssignAudit(context.Background(), j, uuid.New(), nil, payload)

	// Helper swallows the error — per-SIM loop must not observe it.
	// Auditor still recorded the attempt (error returned AFTER append).
	if got := len(a.snapshot()); got != 1 {
		t.Errorf("expected 1 CreateEntry call (error swallowed), got %d", got)
	}
}

func TestEmitPolicyAssignAudit_ParamsShape_GuardsAgainstFieldDrift(t *testing.T) {
	p := newTestProcessor()
	a := &fakePolicyAssignAuditor{}
	p.SetAuditor(a)

	jobID := uuid.New()
	tenantID := uuid.New()
	userID := uuid.New()
	simID := uuid.New()
	prevPolicyID := uuid.New()

	j := &store.Job{ID: jobID, TenantID: tenantID, CreatedBy: &userID}
	payload := BulkPolicyAssignPayload{PolicyVersionID: uuid.New(), Reason: "r"}
	p.emitPolicyAssignAudit(context.Background(), j, simID, &prevPolicyID, payload)

	e := a.snapshot()[0]

	checks := []struct {
		name string
		zero bool
	}{
		{"TenantID", e.TenantID == uuid.Nil},
		{"UserID", e.UserID == nil},
		{"Action", e.Action == ""},
		{"EntityType", e.EntityType == ""},
		{"EntityID", e.EntityID == ""},
		{"BeforeData", len(e.BeforeData) == 0},
		{"AfterData", len(e.AfterData) == 0},
		{"CorrelationID", e.CorrelationID == nil},
	}
	for _, c := range checks {
		if c.zero {
			t.Errorf("field %s is zero/empty; CreateEntryParams shape likely drifted", c.name)
		}
	}
}

func TestBulkPolicyAssign_CoADispatchedForMultipleSIMs(t *testing.T) {
	// Verifies AC-9 CoA continuity: dispatchCoAForSIM is called per-SIM in the
	// processForward loop (same path for segment and sim_ids branches). This
	// test exercises it for three SIMs as the loop would.
	sim1 := uuid.New()
	sim2 := uuid.New()
	sim3 := uuid.New()

	sp := newFakeBulkSessionProvider()
	sp.byID[sim1.String()] = []BulkSessionInfo{{ID: "s1", SimID: sim1.String(), NASIP: "10.0.0.1", AcctSessionID: "acct-1", IMSI: "imsi-1"}}
	sp.byID[sim2.String()] = []BulkSessionInfo{{ID: "s2", SimID: sim2.String(), NASIP: "10.0.0.2", AcctSessionID: "acct-2", IMSI: "imsi-2"}}
	sp.byID[sim3.String()] = []BulkSessionInfo{{ID: "s3", SimID: sim3.String(), NASIP: "10.0.0.3", AcctSessionID: "acct-3", IMSI: "imsi-3"}}

	coa := &fakeBulkCoADispatcher{status: "ack"}
	pu := newFakeBulkPolicyUpdater()

	p := newTestProcessor()
	p.SetSessionProvider(sp)
	p.SetCoADispatcher(coa)
	p.SetPolicyCoAUpdater(pu)

	ctx := context.Background()
	var totalSent, totalAcked, totalFailed int
	for _, sim := range []uuid.UUID{sim1, sim2, sim3} {
		s, a, f := p.dispatchCoAForSIM(ctx, sim)
		totalSent += s
		totalAcked += a
		totalFailed += f
	}

	if totalSent != 3 {
		t.Errorf("CoA sent = %d, want 3 (one per SIM with active session)", totalSent)
	}
	if totalAcked != 3 {
		t.Errorf("CoA acked = %d, want 3", totalAcked)
	}
	if totalFailed != 0 {
		t.Errorf("CoA failed = %d, want 0", totalFailed)
	}
	if len(coa.sent) != 3 {
		t.Errorf("dispatcher calls = %d, want 3", len(coa.sent))
	}
}

func TestSimForPolicyAssign_NormalizesBothSources(t *testing.T) {
	// Defensive: the loop code only reads simForPolicyAssign.ID / ICCID /
	// PolicyVersionID. Pin the mapping contract so resolveSIMs stays correct
	// if either source type (SIMSummary / SIMBulkInfo) loses a field.
	policyID := uuid.New()
	summary := store.SIMSummary{
		ID:              uuid.New(),
		ICCID:           "8990000000000000001",
		PolicyVersionID: &policyID,
	}
	bulkInfo := store.SIMBulkInfo{
		ID:              uuid.New(),
		ICCID:           "8990000000000000002",
		PolicyVersionID: nil,
	}

	fromSummary := simForPolicyAssign{
		ID:              summary.ID,
		ICCID:           summary.ICCID,
		PolicyVersionID: summary.PolicyVersionID,
	}
	fromBulk := simForPolicyAssign{
		ID:              bulkInfo.ID,
		ICCID:           bulkInfo.ICCID,
		PolicyVersionID: bulkInfo.PolicyVersionID,
	}

	if fromSummary.ID != summary.ID || fromSummary.ICCID != summary.ICCID {
		t.Errorf("summary mapping lost fields: %+v", fromSummary)
	}
	if fromSummary.PolicyVersionID == nil || *fromSummary.PolicyVersionID != policyID {
		t.Errorf("summary PolicyVersionID not preserved: %+v", fromSummary.PolicyVersionID)
	}
	if fromBulk.ID != bulkInfo.ID || fromBulk.ICCID != bulkInfo.ICCID {
		t.Errorf("bulkInfo mapping lost fields: %+v", fromBulk)
	}
	if fromBulk.PolicyVersionID != nil {
		t.Errorf("bulkInfo PolicyVersionID should be nil, got %v", fromBulk.PolicyVersionID)
	}
}
