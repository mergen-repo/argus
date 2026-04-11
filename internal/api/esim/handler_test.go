package esim

import (
	"context"
	"errors"
	"testing"
	"time"

	aaasession "github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// mockSessionLister implements activeSessionLister for tests.
type mockSessionLister struct {
	sessions []store.RadiusSession
	err      error
	called   bool
	gotSimID uuid.UUID
}

func (m *mockSessionLister) ListActiveBySIM(ctx context.Context, simID uuid.UUID) ([]store.RadiusSession, error) {
	m.called = true
	m.gotSimID = simID
	return m.sessions, m.err
}

// mockDMSender implements dmDispatcher for tests.
type mockDMSender struct {
	result    *aaasession.DMResult
	err       error
	callCount int
	lastReq   aaasession.DMRequest
}

func (m *mockDMSender) SendDM(ctx context.Context, req aaasession.DMRequest) (*aaasession.DMResult, error) {
	m.callCount++
	m.lastReq = req
	return m.result, m.err
}

func strPtr(s string) *string { return &s }

func newTestHandler(lister *mockSessionLister, sender *mockDMSender) *Handler {
	h := &Handler{logger: zerolog.Nop()}
	if lister != nil || sender != nil {
		var ls activeSessionLister
		var ds dmDispatcher
		if lister != nil {
			ls = lister
		}
		if sender != nil {
			ds = sender
		}
		h.sessionStore = ls
		h.dmSender = ds
	}
	return h
}

func TestToProfileResponse(t *testing.T) {
	now := time.Now()
	smdpID := "smdp-plus-123"
	iccidOnProfile := "8990100000000000001"

	p := &store.ESimProfile{
		ID:                uuid.New(),
		SimID:             uuid.New(),
		EID:               "89044000000000000000000000000001",
		SMDPPlusID:        &smdpID,
		OperatorID:        uuid.New(),
		ProfileState:      "enabled",
		ICCIDOnProfile:    &iccidOnProfile,
		LastProvisionedAt: &now,
		LastError:         nil,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	resp := toProfileResponse(p)

	if resp.ID != p.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, p.ID.String())
	}
	if resp.SimID != p.SimID.String() {
		t.Errorf("SimID = %q, want %q", resp.SimID, p.SimID.String())
	}
	if resp.EID != "89044000000000000000000000000001" {
		t.Errorf("EID = %q, want %q", resp.EID, "89044000000000000000000000000001")
	}
	if resp.OperatorID != p.OperatorID.String() {
		t.Errorf("OperatorID = %q, want %q", resp.OperatorID, p.OperatorID.String())
	}
	if resp.ProfileState != "enabled" {
		t.Errorf("ProfileState = %q, want %q", resp.ProfileState, "enabled")
	}
	if resp.SMDPPlusID == nil || *resp.SMDPPlusID != "smdp-plus-123" {
		t.Error("SMDPPlusID should be 'smdp-plus-123'")
	}
	if resp.ICCIDOnProfile == nil || *resp.ICCIDOnProfile != "8990100000000000001" {
		t.Error("ICCIDOnProfile should match")
	}
	if resp.LastProvisionedAt == nil {
		t.Error("LastProvisionedAt should not be nil")
	}
	if resp.LastError != nil {
		t.Error("LastError should be nil")
	}
	if resp.CreatedAt == "" {
		t.Error("CreatedAt should not be empty")
	}
	if resp.UpdatedAt == "" {
		t.Error("UpdatedAt should not be empty")
	}
}

func TestToProfileResponseNilFields(t *testing.T) {
	now := time.Now()

	p := &store.ESimProfile{
		ID:           uuid.New(),
		SimID:        uuid.New(),
		EID:          "89044000000000000000000000000002",
		OperatorID:   uuid.New(),
		ProfileState: "disabled",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	resp := toProfileResponse(p)

	if resp.SMDPPlusID != nil {
		t.Error("SMDPPlusID should be nil when not set")
	}
	if resp.ICCIDOnProfile != nil {
		t.Error("ICCIDOnProfile should be nil when not set")
	}
	if resp.LastProvisionedAt != nil {
		t.Error("LastProvisionedAt should be nil when not set")
	}
	if resp.LastError != nil {
		t.Error("LastError should be nil when not set")
	}
}

func TestSwitchResponseFormat(t *testing.T) {
	simID := uuid.New()
	opID := uuid.New()
	now := time.Now()

	old := &store.ESimProfile{
		ID:           uuid.New(),
		SimID:        simID,
		EID:          "eid-1",
		OperatorID:   uuid.New(),
		ProfileState: "disabled",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	newP := &store.ESimProfile{
		ID:           uuid.New(),
		SimID:        simID,
		EID:          "eid-2",
		OperatorID:   opID,
		ProfileState: "enabled",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	resp := switchResponse{
		SimID:         simID.String(),
		OldProfile:    toProfileResponse(old),
		NewProfile:    toProfileResponse(newP),
		NewOperatorID: opID.String(),
	}

	if resp.SimID != simID.String() {
		t.Errorf("SimID = %q, want %q", resp.SimID, simID.String())
	}
	if resp.OldProfile.ProfileState != "disabled" {
		t.Errorf("OldProfile state = %q, want 'disabled'", resp.OldProfile.ProfileState)
	}
	if resp.NewProfile.ProfileState != "enabled" {
		t.Errorf("NewProfile state = %q, want 'enabled'", resp.NewProfile.ProfileState)
	}
	if resp.NewOperatorID != opID.String() {
		t.Errorf("NewOperatorID = %q, want %q", resp.NewOperatorID, opID.String())
	}
}

func TestProfileResponseJSONTags(t *testing.T) {
	now := time.Now()

	p := &store.ESimProfile{
		ID:           uuid.New(),
		SimID:        uuid.New(),
		EID:          "eid-test",
		OperatorID:   uuid.New(),
		ProfileState: "disabled",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	resp := toProfileResponse(p)

	if resp.ProfileState != "disabled" {
		t.Errorf("ProfileState = %q, want %q", resp.ProfileState, "disabled")
	}
	if resp.EID != "eid-test" {
		t.Errorf("EID = %q, want %q", resp.EID, "eid-test")
	}
}

func TestSwitchRequestStruct(t *testing.T) {
	req := switchRequest{
		TargetProfileID: uuid.New().String(),
	}

	if req.TargetProfileID == "" {
		t.Error("TargetProfileID should not be empty")
	}

	_, err := uuid.Parse(req.TargetProfileID)
	if err != nil {
		t.Errorf("TargetProfileID should be a valid UUID: %v", err)
	}
}

// --- AC-6: DM dispatch before eSIM profile switch ---

func TestDisconnectActiveSessionsForSwitch_NoActiveSessions(t *testing.T) {
	lister := &mockSessionLister{sessions: nil}
	sender := &mockDMSender{}
	h := newTestHandler(lister, sender)

	simID := uuid.New()
	results, nak, err := h.disconnectActiveSessionsForSwitch(context.Background(), simID, "imsi-1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nak != "" {
		t.Errorf("expected empty nak session id, got %q", nak)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 dm results, got %d", len(results))
	}
	if !lister.called {
		t.Error("session lister should have been called")
	}
	if lister.gotSimID != simID {
		t.Errorf("lister got sim id %s, want %s", lister.gotSimID, simID)
	}
	if sender.callCount != 0 {
		t.Errorf("DMSender should not have been called when no sessions; got %d calls", sender.callCount)
	}
}

func TestDisconnectActiveSessionsForSwitch_ActiveSessionAck(t *testing.T) {
	sessID := uuid.New()
	lister := &mockSessionLister{sessions: []store.RadiusSession{
		{
			ID:            sessID,
			NASIP:         strPtr("10.0.0.1"),
			AcctSessionID: strPtr("acct-123"),
		},
	}}
	sender := &mockDMSender{result: &aaasession.DMResult{Status: aaasession.DMResultACK}}
	h := newTestHandler(lister, sender)

	results, nak, err := h.disconnectActiveSessionsForSwitch(context.Background(), uuid.New(), "imsi-42", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nak != "" {
		t.Errorf("expected empty nak on ACK, got %q", nak)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 dm result, got %d", len(results))
	}
	if results[0]["dm_status"] != aaasession.DMResultACK {
		t.Errorf("dm_status = %v, want %q", results[0]["dm_status"], aaasession.DMResultACK)
	}
	if results[0]["session_id"] != sessID.String() {
		t.Errorf("session_id = %v, want %q", results[0]["session_id"], sessID.String())
	}
	if results[0]["acct_session_id"] != "acct-123" {
		t.Errorf("acct_session_id = %v, want 'acct-123'", results[0]["acct_session_id"])
	}
	if sender.callCount != 1 {
		t.Errorf("DMSender call count = %d, want 1", sender.callCount)
	}
	if sender.lastReq.IMSI != "imsi-42" {
		t.Errorf("DMRequest IMSI = %q, want 'imsi-42'", sender.lastReq.IMSI)
	}
	if sender.lastReq.NASIP != "10.0.0.1" {
		t.Errorf("DMRequest NASIP = %q, want '10.0.0.1'", sender.lastReq.NASIP)
	}
	if sender.lastReq.AcctSessionID != "acct-123" {
		t.Errorf("DMRequest AcctSessionID = %q, want 'acct-123'", sender.lastReq.AcctSessionID)
	}
}

func TestDisconnectActiveSessionsForSwitch_NAK_NoForce(t *testing.T) {
	sessID := uuid.New()
	lister := &mockSessionLister{sessions: []store.RadiusSession{
		{
			ID:            sessID,
			NASIP:         strPtr("10.0.0.1"),
			AcctSessionID: strPtr("acct-nak"),
		},
	}}
	sender := &mockDMSender{result: &aaasession.DMResult{Status: aaasession.DMResultNAK, Message: "refused"}}
	h := newTestHandler(lister, sender)

	results, nak, err := h.disconnectActiveSessionsForSwitch(context.Background(), uuid.New(), "imsi-1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nak != "acct-nak" {
		t.Errorf("expected nak session id 'acct-nak', got %q", nak)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 dm result even on NAK, got %d", len(results))
	}
	if results[0]["dm_status"] != aaasession.DMResultNAK {
		t.Errorf("dm_status = %v, want %q", results[0]["dm_status"], aaasession.DMResultNAK)
	}
}

func TestDisconnectActiveSessionsForSwitch_NAK_Force(t *testing.T) {
	lister := &mockSessionLister{sessions: []store.RadiusSession{
		{
			ID:            uuid.New(),
			NASIP:         strPtr("10.0.0.1"),
			AcctSessionID: strPtr("acct-nak"),
		},
	}}
	sender := &mockDMSender{result: &aaasession.DMResult{Status: aaasession.DMResultNAK}}
	h := newTestHandler(lister, sender)

	results, nak, err := h.disconnectActiveSessionsForSwitch(context.Background(), uuid.New(), "imsi-1", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nak != "" {
		t.Errorf("force=true must bypass NAK handling, got nak=%q", nak)
	}
	if len(results) != 0 {
		t.Errorf("force=true must return no dm results, got %d", len(results))
	}
	if lister.called {
		t.Error("force=true must not query session store")
	}
	if sender.callCount != 0 {
		t.Error("force=true must not invoke DMSender")
	}
}

func TestDisconnectActiveSessionsForSwitch_NilDeps(t *testing.T) {
	h := newTestHandler(nil, nil)
	results, nak, err := h.disconnectActiveSessionsForSwitch(context.Background(), uuid.New(), "imsi-1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nak != "" || len(results) != 0 {
		t.Errorf("nil deps must be a no-op, got nak=%q results=%v", nak, results)
	}
}

func TestDisconnectActiveSessionsForSwitch_ListError(t *testing.T) {
	lister := &mockSessionLister{err: errors.New("db down")}
	sender := &mockDMSender{}
	h := newTestHandler(lister, sender)

	results, nak, err := h.disconnectActiveSessionsForSwitch(context.Background(), uuid.New(), "imsi-1", false)
	if err == nil {
		t.Fatal("expected error propagated from lister")
	}
	if nak != "" || len(results) != 0 {
		t.Errorf("on list error, expected empty results; got nak=%q results=%v", nak, results)
	}
	if sender.callCount != 0 {
		t.Errorf("DMSender must not be called when list fails; got %d calls", sender.callCount)
	}
}

func TestDisconnectActiveSessionsForSwitch_SkipIncompleteSession(t *testing.T) {
	lister := &mockSessionLister{sessions: []store.RadiusSession{
		{ID: uuid.New(), NASIP: nil, AcctSessionID: strPtr("x")},
		{ID: uuid.New(), NASIP: strPtr(""), AcctSessionID: strPtr("x")},
		{ID: uuid.New(), NASIP: strPtr("10.0.0.2"), AcctSessionID: nil},
	}}
	sender := &mockDMSender{result: &aaasession.DMResult{Status: aaasession.DMResultACK}}
	h := newTestHandler(lister, sender)

	_, nak, err := h.disconnectActiveSessionsForSwitch(context.Background(), uuid.New(), "imsi-1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nak != "" {
		t.Errorf("incomplete sessions skipped; nak should be empty, got %q", nak)
	}
	if sender.callCount != 0 {
		t.Errorf("incomplete sessions must not trigger DM; got %d calls", sender.callCount)
	}
}

func TestDisconnectActiveSessionsForSwitch_StripsNASIPPort(t *testing.T) {
	lister := &mockSessionLister{sessions: []store.RadiusSession{
		{
			ID:            uuid.New(),
			NASIP:         strPtr("10.0.0.1:1812"),
			AcctSessionID: strPtr("acct-1"),
		},
	}}
	sender := &mockDMSender{result: &aaasession.DMResult{Status: aaasession.DMResultACK}}
	h := newTestHandler(lister, sender)

	_, _, err := h.disconnectActiveSessionsForSwitch(context.Background(), uuid.New(), "imsi-1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.lastReq.NASIP != "10.0.0.1" {
		t.Errorf("NASIP port not stripped: %q", sender.lastReq.NASIP)
	}
}

func TestDisconnectActiveSessionsForSwitch_DMSenderError(t *testing.T) {
	lister := &mockSessionLister{sessions: []store.RadiusSession{
		{
			ID:            uuid.New(),
			NASIP:         strPtr("10.0.0.1"),
			AcctSessionID: strPtr("acct-err"),
		},
		{
			ID:            uuid.New(),
			NASIP:         strPtr("10.0.0.2"),
			AcctSessionID: strPtr("acct-ok"),
		},
	}}
	// First call errors, but we proceed; status is recorded as "error".
	sender := &mockDMSender{err: errors.New("dial timeout")}
	h := newTestHandler(lister, sender)

	results, nak, err := h.disconnectActiveSessionsForSwitch(context.Background(), uuid.New(), "imsi-1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nak != "" {
		t.Errorf("errors are non-blocking (no NAK); got %q", nak)
	}
	if len(results) != 2 {
		t.Fatalf("expected both sessions recorded, got %d", len(results))
	}
	for _, r := range results {
		if r["dm_status"] != aaasession.DMResultError {
			t.Errorf("dm_status = %v, want %q on send error", r["dm_status"], aaasession.DMResultError)
		}
	}
}

func TestSwitchResponseIncludesDisconnectedSessions(t *testing.T) {
	simID := uuid.New()
	opID := uuid.New()
	now := time.Now()

	old := &store.ESimProfile{
		ID: uuid.New(), SimID: simID, EID: "eid-1", OperatorID: uuid.New(),
		ProfileState: "disabled", CreatedAt: now, UpdatedAt: now,
	}
	newP := &store.ESimProfile{
		ID: uuid.New(), SimID: simID, EID: "eid-2", OperatorID: opID,
		ProfileState: "enabled", CreatedAt: now, UpdatedAt: now,
	}

	resp := switchResponse{
		SimID:         simID.String(),
		OldProfile:    toProfileResponse(old),
		NewProfile:    toProfileResponse(newP),
		NewOperatorID: opID.String(),
		DisconnectedSessions: []map[string]interface{}{
			{"session_id": "s1", "acct_session_id": "a1", "dm_status": aaasession.DMResultACK},
		},
	}

	if len(resp.DisconnectedSessions) != 1 {
		t.Errorf("DisconnectedSessions length = %d, want 1", len(resp.DisconnectedSessions))
	}
	if resp.DisconnectedSessions[0]["dm_status"] != aaasession.DMResultACK {
		t.Errorf("dm_status = %v, want %q", resp.DisconnectedSessions[0]["dm_status"], aaasession.DMResultACK)
	}
}

func TestSetSessionDeps(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	if h.sessionStore != nil || h.dmSender != nil {
		t.Fatal("handler fields should start nil")
	}
	lister := &mockSessionLister{}
	sender := &mockDMSender{}
	h.SetSessionDeps(lister, sender)
	if h.sessionStore == nil {
		t.Error("SetSessionDeps did not set sessionStore")
	}
	if h.dmSender == nil {
		t.Error("SetSessionDeps did not set dmSender")
	}
}
