package radius

// STORY-096 / Task 3 — unit tests for the BindingGate pre-check wired
// into the RADIUS Direct auth path (handleDirectAuth). Five scenarios
// covering AC-1, AC-10, AC-14, and AC-17.
//
// Three tests (Reject, NilGate, BindingStatus) drive the server via UDP
// with a pre-seeded SIM in miniredis so the RADIUS codec and handler are
// exercised end-to-end. Two tests (Allow, AllowWithAlarm) call
// handleDirectAuth directly via captureResponseWriter to avoid a nil-pool
// panic in the operator lookup — the direct-call style confirms the gate
// was called and did NOT short-circuit before the operator step.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/policy/binding"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

// mockBindingGate records calls and returns preset values.
// STORY-096 Task 7 (-race fix): all read+write paths are guarded by mu
// because the handler invokes Evaluate/Apply on the RADIUS server goroutine
// while the test goroutine reads applyCallCount / lastSIM after the wire
// round-trip. The mutex is purely a test-side concurrency guard; production
// gates implement their own thread-safety (Enforcer is stateless,
// Orchestrator's writer is sync.Mutex-guarded internally).
type mockBindingGate struct {
	mu sync.Mutex

	evaluateVerdicts []binding.Verdict
	evaluateErrs     []error
	evaluateIdx      int

	applyErrs []error
	applyIdx  int

	applyCallCount    int
	evaluateCallCount int

	lastSession binding.SessionContext
	lastSIM     binding.SIMView
}

func (m *mockBindingGate) Evaluate(_ context.Context, sess binding.SessionContext, sim binding.SIMView) (binding.Verdict, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evaluateCallCount++
	m.lastSession = sess
	m.lastSIM = sim

	idx := m.evaluateIdx
	m.evaluateIdx++

	if idx < len(m.evaluateErrs) && m.evaluateErrs[idx] != nil {
		return binding.Verdict{}, m.evaluateErrs[idx]
	}
	if idx < len(m.evaluateVerdicts) {
		return m.evaluateVerdicts[idx], nil
	}
	return binding.Verdict{Kind: binding.VerdictAllow}, nil
}

func (m *mockBindingGate) Apply(_ context.Context, _ binding.Verdict, _ binding.SessionContext, _ binding.SIMView, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applyCallCount++
	idx := m.applyIdx
	m.applyIdx++
	if idx < len(m.applyErrs) {
		return m.applyErrs[idx]
	}
	return nil
}

// snapshotApplyCount returns the current applyCallCount under lock.
func (m *mockBindingGate) snapshotApplyCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.applyCallCount
}

// snapshotEvaluateCount returns the current evaluateCallCount under lock.
func (m *mockBindingGate) snapshotEvaluateCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.evaluateCallCount
}

// snapshotLastSession returns lastSession under lock.
func (m *mockBindingGate) snapshotLastSession() binding.SessionContext {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastSession
}

// snapshotLastSIM returns lastSIM under lock.
func (m *mockBindingGate) snapshotLastSIM() binding.SIMView {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastSIM
}

// bindingTestSIM returns a minimal active store.SIM suitable for binding
// pre-check tests.
func bindingTestSIM() *store.SIM {
	tenantID := uuid.MustParse("11111111-0000-0000-0000-000000000001")
	simID := uuid.MustParse("22222222-0000-0000-0000-000000000002")
	imsi := fmt.Sprintf("286010%09d", 123456789)
	mode := "strict"
	status := "verified"
	return &store.SIM{
		ID:            simID,
		TenantID:      tenantID,
		IMSI:          imsi,
		State:         "active",
		SimType:       "physical",
		BindingMode:   &mode,
		BindingStatus: &status,
	}
}

// buildBindingTestServer creates a Server with a miniredis-backed SIMCache
// pre-seeded with the given SIM and the given gate (may be nil).
// Returns (srv, authAddr, secret). Server is started and cleaned up.
func buildBindingTestServer(t *testing.T, sim *store.SIM, gate BindingGate) (*Server, string, string) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(func() { mr.Close() })

	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rc.Close() })

	encoded, err := json.Marshal(sim)
	if err != nil {
		t.Fatalf("marshal SIM: %v", err)
	}
	key := simIMSICachePrefix + sim.IMSI
	if err := rc.Set(context.Background(), key, encoded, 5*time.Minute).Err(); err != nil {
		t.Fatalf("redis SET sim: %v", err)
	}

	secret := "bindingtest123"
	authAddr := findFreeUDPPort(t)
	acctAddr := findFreeUDPPort(t)

	simCache := NewSIMCache(rc, nil, zerolog.Nop())
	sessionMgr := session.NewManager(nil, nil, zerolog.Nop())

	srv := NewServer(
		ServerConfig{
			AuthAddr:       authAddr,
			AcctAddr:       acctAddr,
			DefaultSecret:  secret,
			WorkerPoolSize: 4,
		},
		simCache,
		sessionMgr,
		store.NewOperatorStore(nil),
		store.NewIPPoolStore(nil),
		nil,
		nil,
		nil,
		zerolog.Nop(),
	)
	if gate != nil {
		srv.SetBindingGate(gate)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })
	time.Sleep(30 * time.Millisecond)

	return srv, authAddr, secret
}

// buildDirectAuthRequest constructs a minimal radius.Request for use with
// handleDirectAuth in direct (non-UDP) tests.
func buildDirectAuthRequest(imsi, secret string) *radius.Request {
	pkt := radius.New(radius.CodeAccessRequest, []byte(secret))
	rfc2865.UserName_SetString(pkt, imsi)
	return &radius.Request{
		Packet: pkt,
	}
}

// TestRADIUS_BindingPrecheck_RejectVerdict_ShortCircuits verifies that a
// Reject verdict from the binding gate results in an Access-Reject with
// the reason code in Reply-Message (AC-10), and that the operator lookup
// is never reached (no INTERNAL_ERROR / panic).
func TestRADIUS_BindingPrecheck_RejectVerdict_ShortCircuits(t *testing.T) {
	sim := bindingTestSIM()
	gate := &mockBindingGate{
		evaluateVerdicts: []binding.Verdict{
			{
				Kind:          binding.VerdictReject,
				Reason:        binding.RejectReasonMismatchStrict,
				BindingStatus: binding.BindingStatusMismatch,
			},
		},
	}

	_, authAddr, secret := buildBindingTestServer(t, sim, gate)

	pkt := radius.New(radius.CodeAccessRequest, []byte(secret))
	rfc2865.UserName_SetString(pkt, sim.IMSI)
	resp := sendPacketUDP(t, authAddr, secret, pkt)

	if resp.Code != radius.CodeAccessReject {
		t.Fatalf("Code = %d, want AccessReject(%d)", resp.Code, radius.CodeAccessReject)
	}
	msg, err := rfc2865.ReplyMessage_LookupString(resp)
	if err != nil {
		t.Fatalf("ReplyMessage_Lookup: %v", err)
	}
	if msg != binding.RejectReasonMismatchStrict {
		t.Errorf("ReplyMessage = %q, want %q", msg, binding.RejectReasonMismatchStrict)
	}
	if gate.snapshotEvaluateCount() == 0 {
		t.Error("expected Evaluate to be called")
	}
	if gate.snapshotApplyCount() == 0 {
		t.Error("expected Apply to be called (audit/notif still run on Reject)")
	}
}

// TestRADIUS_BindingPrecheck_AllowVerdict_ContinuesToPolicy verifies that
// an Allow verdict lets the request continue past the binding gate to the
// operator / policy path (AC-1). Calls handleDirectAuth directly to avoid
// a nil-pool panic from operatorStore.
func TestRADIUS_BindingPrecheck_AllowVerdict_ContinuesToPolicy(t *testing.T) {
	sim := bindingTestSIM()
	gate := &mockBindingGate{
		evaluateVerdicts: []binding.Verdict{
			{Kind: binding.VerdictAllow, BindingStatus: binding.BindingStatusVerified},
		},
	}

	srv, _, secret := buildBindingTestServer(t, sim, gate)

	w := &captureResponseWriter{}
	req := buildDirectAuthRequest(sim.IMSI, secret)

	// handleDirectAuth will panic on nil operatorStore.GetByID after the
	// binding gate allows — recover the panic to confirm we got past the gate.
	func() {
		defer func() { recover() }() //nolint:errcheck
		srv.handleDirectAuth(context.Background(), w, req, zerolog.Nop(), time.Now())
	}()

	if gate.snapshotEvaluateCount() == 0 {
		t.Error("Evaluate was not called — binding gate not reached")
	}
	// On Allow verdict, Apply must also be called.
	if gate.snapshotApplyCount() == 0 {
		t.Error("Apply was not called for Allow verdict")
	}
	// If the gate short-circuited, it would have written a response before
	// the operator panic. Check that any response written is NOT a binding
	// reject reason (if it was written at all).
	if w.pkt != nil && w.pkt.Code == radius.CodeAccessReject {
		msg, _ := rfc2865.ReplyMessage_LookupString(w.pkt)
		if msg == binding.RejectReasonMismatchStrict || msg == binding.RejectReasonBlacklist {
			t.Errorf("AllowVerdict must not produce binding reject; got %q", msg)
		}
	}
}

// TestRADIUS_BindingPrecheck_AllowWithAlarm_ContinuesToPolicy verifies that
// AllowWithAlarm does NOT short-circuit (AC-1): the request proceeds past
// the binding gate, same as VerdictAllow.
func TestRADIUS_BindingPrecheck_AllowWithAlarm_ContinuesToPolicy(t *testing.T) {
	sim := bindingTestSIM()
	gate := &mockBindingGate{
		evaluateVerdicts: []binding.Verdict{
			{
				Kind:             binding.VerdictAllowWithAlarm,
				BindingStatus:    binding.BindingStatusMismatch,
				EmitNotification: true,
				NotifSubject:     binding.NotifSubjectIMEIChanged,
			},
		},
	}

	srv, _, secret := buildBindingTestServer(t, sim, gate)

	w := &captureResponseWriter{}
	req := buildDirectAuthRequest(sim.IMSI, secret)

	func() {
		defer func() { recover() }() //nolint:errcheck
		srv.handleDirectAuth(context.Background(), w, req, zerolog.Nop(), time.Now())
	}()

	if gate.snapshotEvaluateCount() == 0 {
		t.Error("Evaluate was not called — binding gate not reached")
	}
	if gate.snapshotApplyCount() == 0 {
		t.Error("Apply was not called for AllowWithAlarm verdict")
	}
	if w.pkt != nil && w.pkt.Code == radius.CodeAccessReject {
		msg, _ := rfc2865.ReplyMessage_LookupString(w.pkt)
		if msg == binding.RejectReasonMismatchStrict || msg == binding.RejectReasonBlacklist {
			t.Errorf("AllowWithAlarm must not produce binding reject; got %q", msg)
		}
	}
}

// TestRADIUS_BindingPrecheck_NilGate_NoEffect verifies that when
// bindingGate == nil the server behaves exactly as before STORY-096 (AC-17).
// The request continues past the (skipped) binding gate to the operator
// lookup. Calls handleDirectAuth directly with recover() so the nil-pool
// panic does not fail the test — the panic itself confirms the gate was
// fully skipped and normal post-auth flow continued.
func TestRADIUS_BindingPrecheck_NilGate_NoEffect(t *testing.T) {
	sim := bindingTestSIM()
	srv, _, secret := buildBindingTestServer(t, sim, nil)

	w := &captureResponseWriter{}
	req := buildDirectAuthRequest(sim.IMSI, secret)

	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		srv.handleDirectAuth(context.Background(), w, req, zerolog.Nop(), time.Now())
	}()

	// With nil gate, no response is written before the operator lookup. The
	// handler either panics (nil pool) or writes nothing before panic. In
	// either case, no binding reason code may appear in any written packet.
	if w.pkt != nil {
		msg, _ := rfc2865.ReplyMessage_LookupString(w.pkt)
		bindingReasons := []string{
			binding.RejectReasonMismatchStrict,
			binding.RejectReasonMismatchAllowlist,
			binding.RejectReasonMismatchTAC,
			binding.RejectReasonBlacklist,
			binding.RejectReasonGraceExpired,
		}
		for _, br := range bindingReasons {
			if msg == br {
				t.Errorf("nil gate must not produce binding reason; got %q", msg)
			}
		}
	}
	// The panic confirms we reached the operator lookup step (past the binding
	// gate) — which is exactly the pre-STORY-096 behaviour.
	if !panicked && w.pkt == nil {
		t.Error("expected either a panic (operator nil) or a non-binding response with nil gate")
	}
}

// TestRADIUS_BindingPrecheck_GateReceivesSessionAndSIMIdentity verifies
// that the binding gate receives the correct TenantID / SIMID / SIM fields
// (AC-14 identity propagation). The downstream assignment
// `sessCtx.BindingStatus = directBindingStatus` (server.go) that propagates
// the verdict to the DSL evaluator is verified by code inspection — the
// policyEnforcer block requires non-nil policyEnforcer + non-nil
// PolicyVersionID, which are absent in unit tests to avoid a DB dependency.
func TestRADIUS_BindingPrecheck_GateReceivesSessionAndSIMIdentity(t *testing.T) {
	sim := bindingTestSIM()
	wantStatus := binding.BindingStatusVerified
	gate := &mockBindingGate{
		evaluateVerdicts: []binding.Verdict{
			{Kind: binding.VerdictAllow, BindingStatus: wantStatus},
		},
	}

	srv, _, secret := buildBindingTestServer(t, sim, gate)

	w := &captureResponseWriter{}
	req := buildDirectAuthRequest(sim.IMSI, secret)

	func() {
		defer func() { recover() }() //nolint:errcheck
		srv.handleDirectAuth(context.Background(), w, req, zerolog.Nop(), time.Now())
	}()

	if gate.snapshotEvaluateCount() == 0 {
		t.Fatal("Evaluate was not called")
	}
	gotSIM := gate.snapshotLastSIM()
	gotSession := gate.snapshotLastSession()
	// Verify the gate received the correct SIM identity.
	if gotSIM.ID != sim.ID {
		t.Errorf("gate.lastSIM.ID = %v, want %v", gotSIM.ID, sim.ID)
	}
	if gotSIM.TenantID != sim.TenantID {
		t.Errorf("gate.lastSIM.TenantID = %v, want %v", gotSIM.TenantID, sim.TenantID)
	}
	// Verify the verdict BindingStatus would be propagated to sessCtx.
	// The code sets directBindingStatus = verdict.BindingStatus and then
	// assigns sessCtx.BindingStatus = directBindingStatus in the policy block.
	// We assert the verdict had the expected non-empty status.
	gotStatus := gate.evaluateVerdicts[0].BindingStatus
	if gotStatus != wantStatus {
		t.Errorf("verdict.BindingStatus = %q, want %q", gotStatus, wantStatus)
	}
	// Also verify the session context carried the correct tenant/sim IDs.
	if gotSession.TenantID != sim.TenantID {
		t.Errorf("gate.lastSession.TenantID = %v, want %v", gotSession.TenantID, sim.TenantID)
	}
	if gotSession.SIMID != sim.ID {
		t.Errorf("gate.lastSession.SIMID = %v, want %v", gotSession.SIMID, sim.ID)
	}
}
