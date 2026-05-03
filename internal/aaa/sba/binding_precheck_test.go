package sba

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/policy/binding"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

// stubBindingGate implements BindingGate for testing.
type stubBindingGate struct {
	verdict  binding.Verdict
	evalErr  error
	applied  bool
	applyErr error
}

func (g *stubBindingGate) Evaluate(_ context.Context, _ binding.SessionContext, _ binding.SIMView) (binding.Verdict, error) {
	return g.verdict, g.evalErr
}

func (g *stubBindingGate) Apply(_ context.Context, _ binding.Verdict, _ binding.SessionContext, _ binding.SIMView, _ string) error {
	g.applied = true
	return g.applyErr
}

// stubSIMResolver implements SIMResolver returning a fixed SIM.
type stubSIMResolver struct {
	sim *store.SIM
	err error
}

func (r *stubSIMResolver) GetByIMSI(_ context.Context, _ string) (*store.SIM, error) {
	return r.sim, r.err
}

func makeSIM() *store.SIM {
	mode := "strict"
	imei := "359211089765432"
	return &store.SIM{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		BindingMode: &mode,
		BoundIMEI:   &imei,
	}
}

func makeAUSFWithGate(gate BindingGate, resolver SIMResolver) *AUSFHandler {
	h := NewAUSFHandler(nil, nil, nil, testLogger())
	h.SetBindingGate(gate)
	h.SetSIMResolver(resolver)
	return h
}

func makeUDMWithGate(gate BindingGate, resolver SIMResolver) *UDMHandler {
	h := NewUDMHandler(nil, nil, nil, testLogger())
	h.SetBindingGate(gate)
	h.SetSIMResolver(resolver)
	return h
}

// TestSBA_AUSF_BindingPrecheck_AllowVerdict_ContinuesNormally verifies that
// an Allow verdict does not interrupt the authentication flow — the handler
// returns 201 Created with the standard auth context URI.
func TestSBA_AUSF_BindingPrecheck_AllowVerdict_ContinuesNormally(t *testing.T) {
	gate := &stubBindingGate{verdict: binding.Verdict{Kind: binding.VerdictAllow}}
	h := makeAUSFWithGate(gate, &stubSIMResolver{sim: makeSIM()})

	body := `{"supiOrSuci":"imsi-286010123456789","servingNetworkName":"5G:mnc001.mcc286.3gppnetwork.org","pei":"imei-359211089765432"}`
	req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/ue-authentications", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleAuthentication(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if !gate.applied {
		t.Error("expected Apply to be called for Allow verdict")
	}
	var resp AuthenticationResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AuthType != AuthType5GAKA {
		t.Errorf("expected auth type %s, got %s", AuthType5GAKA, resp.AuthType)
	}
}

// TestSBA_AUSF_BindingPrecheck_RejectVerdict_403_ProblemDetails verifies that
// a Reject verdict causes the AUSF handler to return 403 Forbidden with a
// problem-details JSON body containing the reject reason in the `cause` field
// (AC-10).
func TestSBA_AUSF_BindingPrecheck_RejectVerdict_403_ProblemDetails(t *testing.T) {
	gate := &stubBindingGate{verdict: binding.Verdict{
		Kind:   binding.VerdictReject,
		Reason: binding.RejectReasonMismatchStrict,
	}}
	h := makeAUSFWithGate(gate, &stubSIMResolver{sim: makeSIM()})

	body := `{"supiOrSuci":"imsi-286010123456789","servingNetworkName":"5G:mnc001.mcc286.3gppnetwork.org","pei":"imei-359211089765432"}`
	req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/ue-authentications", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleAuthentication(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var pd ProblemDetails
	if err := json.NewDecoder(w.Body).Decode(&pd); err != nil {
		t.Fatalf("decode problem-details: %v", err)
	}
	if pd.Cause != binding.RejectReasonMismatchStrict {
		t.Errorf("cause: got %q, want %q", pd.Cause, binding.RejectReasonMismatchStrict)
	}
	if pd.Status != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", pd.Status)
	}
	if !gate.applied {
		t.Error("Apply must be called even on Reject verdict (side-effects: audit/notification/history)")
	}
}

// TestSBA_UDM_BindingPrecheck_RejectVerdict_403_ProblemDetails verifies that
// a Reject verdict on the UDM registration path returns 403 with cause in the
// problem-details body (AC-10).
func TestSBA_UDM_BindingPrecheck_RejectVerdict_403_ProblemDetails(t *testing.T) {
	gate := &stubBindingGate{verdict: binding.Verdict{
		Kind:   binding.VerdictReject,
		Reason: binding.RejectReasonMismatchStrict,
	}}
	h := makeUDMWithGate(gate, &stubSIMResolver{sim: makeSIM()})

	body := `{"amfInstanceId":"amf-01","deregCallbackUri":"http://x.invalid/dereg","guami":{"plmnId":{"mcc":"286","mnc":"01"},"amfId":"abc123"},"ratType":"NR","initialRegistrationInd":false,"pei":"imei-359211089765432"}`
	req := httptest.NewRequest(http.MethodPut, "/nudm-uecm/v1/imsi-286010123456789/registrations/amf-3gpp-access", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleRegistration(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var pd ProblemDetails
	if err := json.NewDecoder(w.Body).Decode(&pd); err != nil {
		t.Fatalf("decode problem-details: %v", err)
	}
	if pd.Cause != binding.RejectReasonMismatchStrict {
		t.Errorf("cause: got %q, want %q", pd.Cause, binding.RejectReasonMismatchStrict)
	}
	if pd.Status != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", pd.Status)
	}
}

// TestSBA_BindingPrecheck_NilGate_NoEffect verifies that when bindingGate is
// nil (the default), the AUSF and UDM handlers behave exactly as they did
// before STORY-096 (AC-17 regression guard).
func TestSBA_BindingPrecheck_NilGate_NoEffect(t *testing.T) {
	t.Run("AUSF", func(t *testing.T) {
		srv := newTestServer()
		body := `{"supiOrSuci":"imsi-286010123456789","servingNetworkName":"5G:mnc001.mcc286.3gppnetwork.org"}`
		req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/ue-authentications", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.ausfHandler.HandleAuthentication(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("AUSF nil gate: expected 201, got %d", w.Code)
		}
	})

	t.Run("UDM", func(t *testing.T) {
		srv := newTestServer()
		body := `{"amfInstanceId":"amf-01","deregCallbackUri":"http://x.invalid/dereg","guami":{"plmnId":{"mcc":"286","mnc":"01"},"amfId":"abc123"},"ratType":"NR","initialRegistrationInd":false}`
		req := httptest.NewRequest(http.MethodPut, "/nudm-uecm/v1/imsi-286010123456789/registrations/amf-3gpp-access", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.udmHandler.HandleRegistration(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("UDM nil gate: expected 201, got %d", w.Code)
		}
	})
}

// TestSBA_AUSF_BindingPrecheck_AllowWithAlarm_ContinuesNormally verifies that
// an AllowWithAlarm verdict does NOT short-circuit the auth response — the
// handler returns 201 (not 403) even though Apply is called to record the alarm.
func TestSBA_AUSF_BindingPrecheck_AllowWithAlarm_ContinuesNormally(t *testing.T) {
	gate := &stubBindingGate{verdict: binding.Verdict{
		Kind:     binding.VerdictAllowWithAlarm,
		Severity: binding.SeverityMedium,
	}}
	h := makeAUSFWithGate(gate, &stubSIMResolver{sim: makeSIM()})

	body := `{"supiOrSuci":"imsi-286010123456789","servingNetworkName":"5G:mnc001.mcc286.3gppnetwork.org","pei":"imei-359211089765432"}`
	req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/ue-authentications", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleAuthentication(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 (AllowWithAlarm must NOT reject), got %d: %s", w.Code, w.Body.String())
	}
	if !gate.applied {
		t.Error("expected Apply to be called for AllowWithAlarm verdict")
	}
}

// TestSBA_AUSF_BindingPrecheck_EvalError_FailOpen verifies that an Evaluate
// error is treated as fail-open — auth proceeds normally (log + continue).
func TestSBA_AUSF_BindingPrecheck_EvalError_FailOpen(t *testing.T) {
	gate := &stubBindingGate{evalErr: errors.New("store unavailable")}
	h := makeAUSFWithGate(gate, &stubSIMResolver{sim: makeSIM()})

	body := `{"supiOrSuci":"imsi-286010123456789","servingNetworkName":"5G:mnc001.mcc286.3gppnetwork.org","pei":"imei-359211089765432"}`
	req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/ue-authentications", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleAuthentication(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 (eval error must be fail-open), got %d: %s", w.Code, w.Body.String())
	}
}
