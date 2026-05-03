package diameter

import (
	"context"
	"testing"

	"github.com/btopcu/argus/internal/policy/binding"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// mockBindingGate records Evaluate + Apply calls for assertion in tests.
type mockBindingGate struct {
	evaluateVerdict binding.Verdict
	evaluateErr     error
	applyErr        error

	evaluateCalled bool
	applyCalled    bool
	applyVerdict   binding.Verdict
	applyProtocol  string
}

func (m *mockBindingGate) Evaluate(_ context.Context, _ binding.SessionContext, _ binding.SIMView) (binding.Verdict, error) {
	m.evaluateCalled = true
	return m.evaluateVerdict, m.evaluateErr
}

func (m *mockBindingGate) Apply(_ context.Context, v binding.Verdict, _ binding.SessionContext, _ binding.SIMView, protocol string) error {
	m.applyCalled = true
	m.applyVerdict = v
	m.applyProtocol = protocol
	return m.applyErr
}

// mockSIMResolver implements SIMResolver for tests, returning a
// pre-configured *store.SIM or an error.
type mockSIMResolver struct {
	sim *store.SIM
	err error
}

func (r *mockSIMResolver) GetByIMSI(_ context.Context, _ string) (*store.SIM, error) {
	return r.sim, r.err
}

func makeSIM() *store.SIM {
	mode := "strict"
	bound := "359211089765432"
	return &store.SIM{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		BindingMode: &mode,
		BoundIMEI:   &bound,
	}
}

// TestS6a_ULR_BindingPrecheck_AllowVerdict_ContinuesNormally verifies that
// when the binding gate returns VerdictAllow, HandleULR responds with
// ResultCode=2001 (success) — the normal flow is not interrupted.
func TestS6a_ULR_BindingPrecheck_AllowVerdict_ContinuesNormally(t *testing.T) {
	gate := &mockBindingGate{
		evaluateVerdict: binding.Verdict{Kind: binding.VerdictAllow},
	}
	sim := makeSIM()
	resolver := &mockSIMResolver{sim: sim}

	imsi := "286010123456789"
	h := NewS6aHandler(nil, nil, nil, zerolog.Nop(),
		WithBindingGate(gate),
		WithS6aSIMResolver(resolver),
	)

	msg := buildULRMsg("sess-ulr-allow-1",
		buildValidTerminalInfoAVP(*sim.BoundIMEI, "01"),
		buildSubscriptionIDIMSIAVP(imsi),
	)

	ans := h.HandleULR(msg)

	if rc := ans.GetResultCode(); rc != ResultCodeSuccess {
		t.Errorf("expected ResultCode %d (success), got %d", ResultCodeSuccess, rc)
	}
	if !gate.evaluateCalled {
		t.Error("expected Evaluate to be called")
	}
	if !gate.applyCalled {
		t.Error("expected Apply to be called")
	}
}

// TestS6a_ULR_BindingPrecheck_RejectVerdict_ULAFailure verifies that when
// the binding gate returns VerdictReject, HandleULR returns a ULA with
// Result-Code 5012 and an Error-Message AVP (281) containing the reason.
func TestS6a_ULR_BindingPrecheck_RejectVerdict_ULAFailure(t *testing.T) {
	const reason = "BINDING_MISMATCH_STRICT"
	gate := &mockBindingGate{
		evaluateVerdict: binding.Verdict{
			Kind:   binding.VerdictReject,
			Reason: reason,
		},
	}
	sim := makeSIM()
	resolver := &mockSIMResolver{sim: sim}

	imsi := "286010123456789"
	h := NewS6aHandler(nil, nil, nil, zerolog.Nop(),
		WithBindingGate(gate),
		WithS6aSIMResolver(resolver),
	)

	msg := buildULRMsg("sess-ulr-reject-1",
		buildValidTerminalInfoAVP("000000000000001", "00"),
		buildSubscriptionIDIMSIAVP(imsi),
	)

	ans := h.HandleULR(msg)

	if rc := ans.GetResultCode(); rc != ResultCodeUnableToComply {
		t.Errorf("expected ResultCode %d (5012), got %d", ResultCodeUnableToComply, rc)
	}
	errMsgAVP := FindAVP(ans.AVPs, AVPCodeErrorMessage)
	if errMsgAVP == nil {
		t.Fatal("expected Error-Message AVP (281) in ULA reject, got none")
	}
	if got := errMsgAVP.GetString(); got != reason {
		t.Errorf("Error-Message AVP: got %q, want %q", got, reason)
	}
	if !gate.applyCalled {
		t.Error("ULR Reject: Apply (sinks) must be called before wire reject (AC-16)")
	}
}

// TestS6a_NTR_BindingPrecheck_RejectVerdict_StillReturnsNTASuccess verifies
// the D-NTR signaling-only disposition: even when the gate returns
// VerdictReject, the NTA response is still ResultCode=2001, and Apply
// was called with the reject verdict (sinks fired).
func TestS6a_NTR_BindingPrecheck_RejectVerdict_StillReturnsNTASuccess(t *testing.T) {
	const reason = "BINDING_BLACKLIST"
	gate := &mockBindingGate{
		evaluateVerdict: binding.Verdict{
			Kind:   binding.VerdictReject,
			Reason: reason,
		},
	}
	sim := makeSIM()
	resolver := &mockSIMResolver{sim: sim}

	imsi := "286010123456789"
	h := NewS6aHandler(nil, nil, nil, zerolog.Nop(),
		WithBindingGate(gate),
		WithS6aSIMResolver(resolver),
	)

	msg := buildNTRMsg("sess-ntr-reject-1",
		buildValidTerminalInfoAVP("000000000000001", "00"),
		buildSubscriptionIDIMSIAVP(imsi),
	)

	ans := h.HandleNTR(msg)

	if rc := ans.GetResultCode(); rc != ResultCodeSuccess {
		t.Errorf("NTR: expected ResultCode %d (success) regardless of verdict, got %d", ResultCodeSuccess, rc)
	}
	if !gate.applyCalled {
		t.Error("NTR: expected Apply (sinks) to be called even on Reject verdict")
	}
	if gate.applyVerdict.Kind != binding.VerdictReject {
		t.Errorf("NTR: Apply called with verdict %v, want VerdictReject", gate.applyVerdict.Kind)
	}
}

// TestS6a_BindingPrecheck_NilGate_NoEffect verifies AC-17: when no
// BindingGate is configured, HandleULR behaves identically to STORY-094
// T7 (always returns ResultCode=2001; no gate calls).
func TestS6a_BindingPrecheck_NilGate_NoEffect(t *testing.T) {
	h := NewS6aHandler(nil, nil, nil, zerolog.Nop())

	imsi := "286010123456789"
	msg := buildULRMsg("sess-nil-gate-1",
		buildValidTerminalInfoAVP("359211089765432", "01"),
		buildSubscriptionIDIMSIAVP(imsi),
	)

	ans := h.HandleULR(msg)

	if rc := ans.GetResultCode(); rc != ResultCodeSuccess {
		t.Errorf("nil gate: expected ResultCode %d (success), got %d", ResultCodeSuccess, rc)
	}
}

// TestNewAVPErrorMessage verifies that NewAVPErrorMessage constructs
// AVP code 281 with a UTF8String payload that round-trips correctly.
func TestNewAVPErrorMessage(t *testing.T) {
	const msg = "BINDING_MISMATCH_STRICT"
	avp := NewAVPErrorMessage(msg)

	if avp.Code != AVPCodeErrorMessage {
		t.Errorf("code: got %d, want %d", avp.Code, AVPCodeErrorMessage)
	}
	if avp.Flags != 0 {
		t.Errorf("flags: got %d, want 0 (M-bit must NOT be set for Error-Message)", avp.Flags)
	}
	if avp.VendorID != 0 {
		t.Errorf("vendor id: got %d, want 0", avp.VendorID)
	}

	encoded := avp.Encode()
	decoded, consumed, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("round-trip decode: %v", err)
	}
	if consumed != len(encoded) {
		t.Errorf("consumed %d, expected %d", consumed, len(encoded))
	}
	if decoded.Code != AVPCodeErrorMessage {
		t.Errorf("decoded code: got %d, want %d", decoded.Code, AVPCodeErrorMessage)
	}
	if got := decoded.GetString(); got != msg {
		t.Errorf("decoded string: got %q, want %q", got, msg)
	}
}

// buildSubscriptionIDIMSIAVP builds a Subscription-ID AVP carrying an IMSI.
// Used by binding tests to plant an IMSI in ULR/NTR messages so the
// handler can look up the SIM.
func buildSubscriptionIDIMSIAVP(imsi string) *AVP {
	inner := []*AVP{
		NewAVPUint32(AVPCodeSubscriptionIDType, AVPFlagMandatory, 0, SubscriptionIDTypeIMSI),
		NewAVPString(AVPCodeSubscriptionIDData, AVPFlagMandatory, 0, imsi),
	}
	outer := NewAVPGrouped(AVPCodeSubscriptionID, AVPFlagMandatory, 0, inner)
	encoded := outer.Encode()
	decoded, _, err := DecodeAVP(encoded)
	if err != nil {
		panic("buildSubscriptionIDIMSIAVP: " + err.Error())
	}
	return decoded
}
