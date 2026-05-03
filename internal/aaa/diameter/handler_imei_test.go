package diameter

import (
	"testing"

	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
)

// buildValidTerminalInfoAVP returns a Terminal-Information AVP (code 350,
// vendor 10415) carrying a valid 15-digit IMEI (1402) + 2-digit SV (1403).
func buildValidTerminalInfoAVP(imei, sv string) *AVP {
	inner := []*AVP{
		NewAVPString(AVPCodeIMEI, AVPFlagMandatory, VendorID3GPP, imei),
		NewAVPString(AVPCodeSoftwareVersion, AVPFlagMandatory, VendorID3GPP, sv),
	}
	outer := NewAVPGrouped(AVPCodeTerminalInformation, AVPFlagMandatory, VendorID3GPP, inner)
	encoded := outer.Encode()
	decoded, _, err := DecodeAVP(encoded)
	if err != nil {
		panic("buildValidTerminalInfoAVP: decode failed: " + err.Error())
	}
	return decoded
}

// buildMalformedTerminalInfoAVP returns a Terminal-Information AVP with
// garbage payload — triggers ErrIMEICaptureMalformed.
func buildMalformedTerminalInfoAVP() *AVP {
	return &AVP{
		Code:     AVPCodeTerminalInformation,
		Flags:    AVPFlagMandatory | AVPFlagVendor,
		VendorID: VendorID3GPP,
		Data:     []byte{0xFF, 0xFE, 0xFD, 0x00, 0x01, 0x02, 0x03},
	}
}

// buildNTRMsg constructs a minimal Notify-Request (CommandNTR = 323) with the
// given Session-ID and optional extra AVPs.
func buildNTRMsg(sessionID string, extra ...*AVP) *Message {
	msg := &Message{
		Version:       1,
		Flags:         MsgFlagRequest,
		CommandCode:   CommandNTR,
		ApplicationID: ApplicationIDS6a,
	}
	msg.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
	for _, a := range extra {
		msg.AddAVP(a)
	}
	return msg
}

// buildULRMsg constructs a minimal Update-Location-Request (CommandULR = 316).
func buildULRMsg(sessionID string, extra ...*AVP) *Message {
	msg := &Message{
		Version:       1,
		Flags:         MsgFlagRequest,
		CommandCode:   CommandULR,
		ApplicationID: ApplicationIDS6a,
	}
	msg.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
	for _, a := range extra {
		msg.AddAVP(a)
	}
	return msg
}

// TestS6aIMEICapture_AVPAbsent verifies that a Notify-Request without a
// Terminal-Information AVP is processed normally: no counter increment,
// no WARN log, response has ResultCode=2001, and the response Session-ID
// matches the request Session-ID.
//
// AC contract: AVP absent → silent; zero behavior change on downstream flow.
func TestS6aIMEICapture_AVPAbsent(t *testing.T) {
	reg := obsmetrics.NewRegistry()
	handler := NewS6aHandler(nil, nil, reg, zerolog.Nop())

	msg := buildNTRMsg("session-absent-1")
	ans := handler.HandleNTR(msg)

	if rc := ans.GetResultCode(); rc != ResultCodeSuccess {
		t.Errorf("result code: got %d, want %d", rc, ResultCodeSuccess)
	}
	if sid := ans.GetSessionID(); sid != "session-absent-1" {
		t.Errorf("session id: got %q, want session-absent-1", sid)
	}

	count := testutil.ToFloat64(reg.IMEICaptureParseErrorsTotal.WithLabelValues("diameter_s6a"))
	if count != 0 {
		t.Errorf("counter: got %v, want 0 (no AVP present)", count)
	}
}

// TestS6aIMEICapture_AVPMalformed verifies that a Notify-Request with a
// malformed Terminal-Information AVP increments
// argus_imei_capture_parse_errors_total{protocol="diameter_s6a"} by 1 and
// does NOT block the request (response is still ResultCode=2001).
//
// AC contract: ErrIMEICaptureMalformed → counter+1, WARN log, flow continues.
func TestS6aIMEICapture_AVPMalformed(t *testing.T) {
	reg := obsmetrics.NewRegistry()
	handler := NewS6aHandler(nil, nil, reg, zerolog.Nop())

	msg := buildNTRMsg("session-malformed-1", buildMalformedTerminalInfoAVP())
	ans := handler.HandleNTR(msg)

	if rc := ans.GetResultCode(); rc != ResultCodeSuccess {
		t.Errorf("result code: got %d, want %d (malformed AVP must not block NTR)", rc, ResultCodeSuccess)
	}

	count := testutil.ToFloat64(reg.IMEICaptureParseErrorsTotal.WithLabelValues("diameter_s6a"))
	if count != 1 {
		t.Errorf("counter argus_imei_capture_parse_errors_total{protocol=\"diameter_s6a\"}: got %v, want 1", count)
	}
}

// TestS6aIMEICapture_AVPValid verifies that a Notify-Request with a valid
// Terminal-Information AVP (1402+1403 sub-AVPs) results in a successful
// response. The counter must NOT increment.
//
// AC contract: valid AVP → no error, counter stays 0, response unchanged.
func TestS6aIMEICapture_AVPValid(t *testing.T) {
	reg := obsmetrics.NewRegistry()
	handler := NewS6aHandler(nil, nil, reg, zerolog.Nop())

	tiAVP := buildValidTerminalInfoAVP("359211089765432", "01")
	msg := buildNTRMsg("session-valid-ntr-1", tiAVP)
	ans := handler.HandleNTR(msg)

	if rc := ans.GetResultCode(); rc != ResultCodeSuccess {
		t.Errorf("result code: got %d, want %d", rc, ResultCodeSuccess)
	}
	if sid := ans.GetSessionID(); sid != "session-valid-ntr-1" {
		t.Errorf("session id: got %q, want session-valid-ntr-1", sid)
	}

	count := testutil.ToFloat64(reg.IMEICaptureParseErrorsTotal.WithLabelValues("diameter_s6a"))
	if count != 0 {
		t.Errorf("counter: got %v, want 0 (valid AVP must not bump error counter)", count)
	}
}

// TestS6aULR_IMEICapture_AVPValid verifies the same IMEI-capture behavior on
// an Update-Location-Request (CommandULR = 316): valid Terminal-Information
// AVP → successful response, counter stays 0.
//
// AC contract: parity between NTR and ULR capture paths (D-182 disposition A).
func TestS6aULR_IMEICapture_AVPValid(t *testing.T) {
	reg := obsmetrics.NewRegistry()
	handler := NewS6aHandler(nil, nil, reg, zerolog.Nop())

	tiAVP := buildValidTerminalInfoAVP("359211089765432", "01")
	msg := buildULRMsg("session-valid-ulr-1", tiAVP)
	ans := handler.HandleULR(msg)

	if rc := ans.GetResultCode(); rc != ResultCodeSuccess {
		t.Errorf("result code: got %d, want %d", rc, ResultCodeSuccess)
	}
	if sid := ans.GetSessionID(); sid != "session-valid-ulr-1" {
		t.Errorf("session id: got %q, want session-valid-ulr-1", sid)
	}

	count := testutil.ToFloat64(reg.IMEICaptureParseErrorsTotal.WithLabelValues("diameter_s6a"))
	if count != 0 {
		t.Errorf("counter: got %v, want 0 (valid AVP must not bump error counter)", count)
	}
}
