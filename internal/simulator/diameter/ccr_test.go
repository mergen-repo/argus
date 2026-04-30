package diameter

import (
	"net"
	"testing"
	"time"

	argusdiameter "github.com/btopcu/argus/internal/aaa/diameter"
	"github.com/btopcu/argus/internal/simulator/discovery"
	"github.com/btopcu/argus/internal/simulator/radius"
)

// testMSISDN is a shared MSISDN value for the fixture.
const testMSISDN = "905551234567"

// newFixtureSC builds a deterministic SessionContext used across all CCR tests.
func newFixtureSC() *radius.SessionContext {
	msisdn := testMSISDN
	return &radius.SessionContext{
		SIM: discovery.SIM{
			ID:           "sim-0001",
			TenantID:     "tenant-0001",
			OperatorID:   "op-0001",
			OperatorCode: "turkcell",
			MCC:          "286",
			MNC:          "01",
			IMSI:         "286010000000001",
			MSISDN:       &msisdn,
			ICCID:        "8990011234567890123",
		},
		NASIP:         "10.0.0.1",
		NASIdentifier: "sim-nas-01",
		AcctSessionID: "sess-test-0001",
		FramedIP:      net.IPv4(10, 0, 0, 42),
		StartedAt:     time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC),
	}
}

const (
	testOriginHost  = "sim-turkcell.sim.argus.test"
	testOriginRealm = "sim.argus.test"
	testDestRealm   = "argus.local"
	testHopID       = uint32(0xDEAD1234)
	testEndID       = uint32(0xBEEF5678)
)

// roundTrip encodes msg to bytes then decodes it back — validates Encode+Decode.
func roundTrip(t *testing.T, msg *argusdiameter.Message) *argusdiameter.Message {
	t.Helper()
	data, err := msg.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	decoded, err := argusdiameter.DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage failed: %v", err)
	}
	return decoded
}

// assertHeader checks Diameter message header fields.
func assertHeader(t *testing.T, msg *argusdiameter.Message, cmdCode, appID, hopID, endID uint32) {
	t.Helper()
	if msg.CommandCode != cmdCode {
		t.Errorf("CommandCode: got %d, want %d", msg.CommandCode, cmdCode)
	}
	if msg.ApplicationID != appID {
		t.Errorf("ApplicationID: got %d, want %d", msg.ApplicationID, appID)
	}
	if msg.HopByHopID != hopID {
		t.Errorf("HopByHopID: got %d, want %d", msg.HopByHopID, hopID)
	}
	if msg.EndToEndID != endID {
		t.Errorf("EndToEndID: got %d, want %d", msg.EndToEndID, endID)
	}
	if !msg.IsRequest() {
		t.Errorf("expected Request flag set")
	}
}

// assertAVPString asserts an AVP with given code has expected string value.
func assertAVPString(t *testing.T, avps []*argusdiameter.AVP, code uint32, want string) {
	t.Helper()
	avp := argusdiameter.FindAVP(avps, code)
	if avp == nil {
		t.Errorf("AVP code %d not found", code)
		return
	}
	if got := avp.GetString(); got != want {
		t.Errorf("AVP %d string: got %q, want %q", code, got, want)
	}
}

// assertAVPUint32 asserts an AVP with given code has expected uint32 value.
func assertAVPUint32(t *testing.T, avps []*argusdiameter.AVP, code uint32, want uint32) {
	t.Helper()
	avp := argusdiameter.FindAVP(avps, code)
	if avp == nil {
		t.Errorf("AVP code %d not found", code)
		return
	}
	got, err := avp.GetUint32()
	if err != nil {
		t.Errorf("AVP %d GetUint32: %v", code, err)
		return
	}
	if got != want {
		t.Errorf("AVP %d uint32: got %d, want %d", code, got, want)
	}
}

// assertAVPVendorUint32 asserts a vendor AVP has expected uint32 value.
func assertAVPVendorUint32(t *testing.T, avps []*argusdiameter.AVP, code, vendorID, want uint32) {
	t.Helper()
	avp := argusdiameter.FindAVPVendor(avps, code, vendorID)
	if avp == nil {
		t.Errorf("vendor AVP code %d vendorID %d not found", code, vendorID)
		return
	}
	got, err := avp.GetUint32()
	if err != nil {
		t.Errorf("vendor AVP %d GetUint32: %v", code, err)
		return
	}
	if got != want {
		t.Errorf("vendor AVP %d uint32: got %d, want %d", code, got, want)
	}
}

// assertSubscriptionID verifies at least one Subscription-Id grouped AVP
// with SubscriptionIDTypeIMSI and the expected IMSI value.
func assertSubscriptionID(t *testing.T, avps []*argusdiameter.AVP, wantIMSI string) {
	t.Helper()
	for _, a := range avps {
		if a.Code != argusdiameter.AVPCodeSubscriptionID {
			continue
		}
		grouped, err := a.GetGrouped()
		if err != nil {
			continue
		}
		typeAVP := argusdiameter.FindAVP(grouped, argusdiameter.AVPCodeSubscriptionIDType)
		dataAVP := argusdiameter.FindAVP(grouped, argusdiameter.AVPCodeSubscriptionIDData)
		if typeAVP == nil || dataAVP == nil {
			continue
		}
		subType, _ := typeAVP.GetUint32()
		if subType == argusdiameter.SubscriptionIDTypeIMSI && dataAVP.GetString() == wantIMSI {
			return
		}
	}
	t.Errorf("no Subscription-Id AVP found with IMSI=%q", wantIMSI)
}

// assertCommonBase checks AVPs present in every CCR variant.
func assertCommonBase(t *testing.T, msg *argusdiameter.Message, sc *radius.SessionContext, appID uint32) {
	t.Helper()
	assertAVPString(t, msg.AVPs, argusdiameter.AVPCodeSessionID, sc.AcctSessionID)
	assertAVPString(t, msg.AVPs, argusdiameter.AVPCodeOriginHost, testOriginHost)
	assertAVPString(t, msg.AVPs, argusdiameter.AVPCodeOriginRealm, testOriginRealm)
	assertAVPString(t, msg.AVPs, argusdiameter.AVPCodeDestinationRealm, testDestRealm)
	assertAVPUint32(t, msg.AVPs, argusdiameter.AVPCodeAuthApplicationID, appID)
	assertSubscriptionID(t, msg.AVPs, sc.SIM.IMSI)
}

// TestBuildGxCCRI validates the Gx CCR-I builder.
func TestBuildGxCCRI(t *testing.T) {
	sc := newFixtureSC()
	msg := BuildGxCCRI(sc, testOriginHost, testOriginRealm, testDestRealm, testHopID, testEndID)

	decoded := roundTrip(t, msg)

	assertHeader(t, decoded, argusdiameter.CommandCCR, argusdiameter.ApplicationIDGx, testHopID, testEndID)
	assertCommonBase(t, decoded, sc, argusdiameter.ApplicationIDGx)
	assertAVPUint32(t, decoded.AVPs, argusdiameter.AVPCodeCCRequestType, argusdiameter.CCRequestTypeInitial)
	assertAVPUint32(t, decoded.AVPs, argusdiameter.AVPCodeCCRequestNumber, 0)

	if argusdiameter.FindAVP(decoded.AVPs, avpCodeFramedIPAddress) == nil {
		t.Error("Framed-IP-Address (code 8) AVP missing")
	}
	// IP-CAN-Type = 0 (3GPP-GPRS) — F-A6 alignment with PROTOCOLS.md.
	assertAVPVendorUint32(t, decoded.AVPs, argusdiameter.AVPCodeIPCANType, argusdiameter.VendorID3GPP, 0)
	assertAVPVendorUint32(t, decoded.AVPs, argusdiameter.AVPCodeRATType3GPP, argusdiameter.VendorID3GPP, 1004)
}

// TestBuildGxCCRT validates the Gx CCR-T builder.
// Gx CCR-T must NOT carry Used-Service-Unit (Gx is policy-only).
func TestBuildGxCCRT(t *testing.T) {
	sc := newFixtureSC()
	const reqNum uint32 = 1
	msg := BuildGxCCRT(sc, testOriginHost, testOriginRealm, testDestRealm, testHopID, testEndID, reqNum)

	decoded := roundTrip(t, msg)

	assertHeader(t, decoded, argusdiameter.CommandCCR, argusdiameter.ApplicationIDGx, testHopID, testEndID)
	assertCommonBase(t, decoded, sc, argusdiameter.ApplicationIDGx)
	assertAVPUint32(t, decoded.AVPs, argusdiameter.AVPCodeCCRequestType, argusdiameter.CCRequestTypeTermination)
	assertAVPUint32(t, decoded.AVPs, argusdiameter.AVPCodeCCRequestNumber, reqNum)

	if argusdiameter.FindAVP(decoded.AVPs, argusdiameter.AVPCodeUsedServiceUnit) != nil {
		t.Error("Gx CCR-T must not carry Used-Service-Unit")
	}
}

// TestBuildGyCCRI validates the Gy CCR-I builder.
func TestBuildGyCCRI(t *testing.T) {
	sc := newFixtureSC()
	const requestedOctets uint64 = 100 * 1024 * 1024
	msg := BuildGyCCRI(sc, testOriginHost, testOriginRealm, testDestRealm, testHopID, testEndID, requestedOctets)

	decoded := roundTrip(t, msg)

	assertHeader(t, decoded, argusdiameter.CommandCCR, argusdiameter.ApplicationIDGy, testHopID, testEndID)
	assertCommonBase(t, decoded, sc, argusdiameter.ApplicationIDGy)
	assertAVPUint32(t, decoded.AVPs, argusdiameter.AVPCodeCCRequestType, argusdiameter.CCRequestTypeInitial)
	assertAVPUint32(t, decoded.AVPs, argusdiameter.AVPCodeCCRequestNumber, 0)

	if argusdiameter.FindAVP(decoded.AVPs, avpCodeFramedIPAddress) == nil {
		t.Error("Framed-IP-Address (code 8) AVP missing in Gy CCR-I")
	}
	// IP-CAN-Type = 0 (3GPP-GPRS) — F-A6 alignment with PROTOCOLS.md.
	assertAVPVendorUint32(t, decoded.AVPs, argusdiameter.AVPCodeIPCANType, argusdiameter.VendorID3GPP, 0)
	assertAVPVendorUint32(t, decoded.AVPs, argusdiameter.AVPCodeRATType3GPP, argusdiameter.VendorID3GPP, 1004)

	rsuAVP := argusdiameter.FindAVP(decoded.AVPs, argusdiameter.AVPCodeRequestedServiceUnit)
	if rsuAVP == nil {
		t.Fatal("Requested-Service-Unit AVP missing in Gy CCR-I")
	}
	rsuInner, err := rsuAVP.GetGrouped()
	if err != nil {
		t.Fatalf("RSU GetGrouped: %v", err)
	}
	totalAVP := argusdiameter.FindAVP(rsuInner, argusdiameter.AVPCodeCCTotalOctets)
	if totalAVP == nil {
		t.Fatal("CC-Total-Octets missing in Requested-Service-Unit")
	}
	got, err := totalAVP.GetUint64()
	if err != nil {
		t.Fatalf("CC-Total-Octets GetUint64: %v", err)
	}
	if got != requestedOctets {
		t.Errorf("CC-Total-Octets: got %d, want %d", got, requestedOctets)
	}
}

// TestBuildGyCCRU validates the Gy CCR-U builder.
func TestBuildGyCCRU(t *testing.T) {
	sc := newFixtureSC()
	const (
		reqNum   uint32 = 2
		deltaIn  uint64 = 512 * 1024
		deltaOut uint64 = 128 * 1024
		deltaSec uint32 = 30
	)
	msg := BuildGyCCRU(sc, testOriginHost, testOriginRealm, testDestRealm, testHopID, testEndID, reqNum, deltaIn, deltaOut, deltaSec)

	decoded := roundTrip(t, msg)

	assertHeader(t, decoded, argusdiameter.CommandCCR, argusdiameter.ApplicationIDGy, testHopID, testEndID)
	assertCommonBase(t, decoded, sc, argusdiameter.ApplicationIDGy)
	assertAVPUint32(t, decoded.AVPs, argusdiameter.AVPCodeCCRequestType, argusdiameter.CCRequestTypeUpdate)
	assertAVPUint32(t, decoded.AVPs, argusdiameter.AVPCodeCCRequestNumber, reqNum)

	usuAVP := argusdiameter.FindAVP(decoded.AVPs, argusdiameter.AVPCodeUsedServiceUnit)
	if usuAVP == nil {
		t.Fatal("Used-Service-Unit AVP missing in Gy CCR-U")
	}
	usuInner, err := usuAVP.GetGrouped()
	if err != nil {
		t.Fatalf("USU GetGrouped: %v", err)
	}
	assertUint64AVP(t, usuInner, argusdiameter.AVPCodeCCInputOctets, deltaIn)
	assertUint64AVP(t, usuInner, argusdiameter.AVPCodeCCOutputOctets, deltaOut)
	assertUint32AVPInner(t, usuInner, argusdiameter.AVPCodeCCTime, deltaSec)

	if argusdiameter.FindAVP(decoded.AVPs, argusdiameter.AVPCodeRequestedServiceUnit) == nil {
		t.Error("Requested-Service-Unit missing in Gy CCR-U")
	}
}

// TestBuildGyCCRT validates the Gy CCR-T builder.
func TestBuildGyCCRT(t *testing.T) {
	sc := newFixtureSC()
	const (
		reqNum   uint32 = 5
		finalIn  uint64 = 2 * 1024 * 1024
		finalOut uint64 = 512 * 1024
		finalSec uint32 = 300
	)
	msg := BuildGyCCRT(sc, testOriginHost, testOriginRealm, testDestRealm, testHopID, testEndID, reqNum, finalIn, finalOut, finalSec)

	decoded := roundTrip(t, msg)

	assertHeader(t, decoded, argusdiameter.CommandCCR, argusdiameter.ApplicationIDGy, testHopID, testEndID)
	assertCommonBase(t, decoded, sc, argusdiameter.ApplicationIDGy)
	assertAVPUint32(t, decoded.AVPs, argusdiameter.AVPCodeCCRequestType, argusdiameter.CCRequestTypeTermination)
	assertAVPUint32(t, decoded.AVPs, argusdiameter.AVPCodeCCRequestNumber, reqNum)

	usuAVP := argusdiameter.FindAVP(decoded.AVPs, argusdiameter.AVPCodeUsedServiceUnit)
	if usuAVP == nil {
		t.Fatal("Used-Service-Unit AVP missing in Gy CCR-T")
	}
	usuInner, err := usuAVP.GetGrouped()
	if err != nil {
		t.Fatalf("USU GetGrouped: %v", err)
	}
	assertUint64AVP(t, usuInner, argusdiameter.AVPCodeCCInputOctets, finalIn)
	assertUint64AVP(t, usuInner, argusdiameter.AVPCodeCCOutputOctets, finalOut)
	assertUint32AVPInner(t, usuInner, argusdiameter.AVPCodeCCTime, finalSec)

	if argusdiameter.FindAVP(decoded.AVPs, argusdiameter.AVPCodeRequestedServiceUnit) != nil {
		t.Error("Gy CCR-T must not carry Requested-Service-Unit")
	}
}

// TestBuildGxCCRI_NilFramedIP verifies that nil FramedIP omits the AVP.
func TestBuildGxCCRI_NilFramedIP(t *testing.T) {
	sc := newFixtureSC()
	sc.FramedIP = nil

	msg := BuildGxCCRI(sc, testOriginHost, testOriginRealm, testDestRealm, testHopID, testEndID)
	decoded := roundTrip(t, msg)

	if argusdiameter.FindAVP(decoded.AVPs, avpCodeFramedIPAddress) != nil {
		t.Error("Framed-IP-Address should be absent when FramedIP is nil")
	}
}

// TestBuildGxCCRI_NoMSISDN verifies that nil MSISDN produces only IMSI Subscription-Id.
func TestBuildGxCCRI_NoMSISDN(t *testing.T) {
	sc := newFixtureSC()
	sc.SIM.MSISDN = nil

	msg := BuildGxCCRI(sc, testOriginHost, testOriginRealm, testDestRealm, testHopID, testEndID)
	decoded := roundTrip(t, msg)

	assertSubscriptionID(t, decoded.AVPs, sc.SIM.IMSI)

	var msisdnFound bool
	for _, a := range decoded.AVPs {
		if a.Code != argusdiameter.AVPCodeSubscriptionID {
			continue
		}
		grouped, _ := a.GetGrouped()
		typeAVP := argusdiameter.FindAVP(grouped, argusdiameter.AVPCodeSubscriptionIDType)
		if typeAVP == nil {
			continue
		}
		subType, _ := typeAVP.GetUint32()
		if subType == argusdiameter.SubscriptionIDTypeMSISDN {
			msisdnFound = true
		}
	}
	if msisdnFound {
		t.Error("MSISDN Subscription-Id should be absent when sc.SIM.MSISDN is nil")
	}
}

// TestBuildGyCCRU_IncrementingReqNum verifies reqNum propagates correctly.
func TestBuildGyCCRU_IncrementingReqNum(t *testing.T) {
	sc := newFixtureSC()

	for _, reqNum := range []uint32{1, 2, 3, 100} {
		msg := BuildGyCCRU(sc, testOriginHost, testOriginRealm, testDestRealm, testHopID+reqNum, testEndID+reqNum, reqNum, 0, 0, 0)
		decoded := roundTrip(t, msg)
		assertAVPUint32(t, decoded.AVPs, argusdiameter.AVPCodeCCRequestNumber, reqNum)
	}
}

// assertUint64AVP is a helper to assert a uint64 AVP in an inner grouped list.
func assertUint64AVP(t *testing.T, avps []*argusdiameter.AVP, code uint32, want uint64) {
	t.Helper()
	avp := argusdiameter.FindAVP(avps, code)
	if avp == nil {
		t.Errorf("inner AVP code %d not found", code)
		return
	}
	got, err := avp.GetUint64()
	if err != nil {
		t.Errorf("inner AVP %d GetUint64: %v", code, err)
		return
	}
	if got != want {
		t.Errorf("inner AVP %d uint64: got %d, want %d", code, got, want)
	}
}

// assertUint32AVPInner is a helper to assert a uint32 AVP in an inner grouped list.
func assertUint32AVPInner(t *testing.T, avps []*argusdiameter.AVP, code uint32, want uint32) {
	t.Helper()
	avp := argusdiameter.FindAVP(avps, code)
	if avp == nil {
		t.Errorf("inner AVP code %d not found", code)
		return
	}
	got, err := avp.GetUint32()
	if err != nil {
		t.Errorf("inner AVP %d GetUint32: %v", code, err)
		return
	}
	if got != want {
		t.Errorf("inner AVP %d uint32: got %d, want %d", code, got, want)
	}
}
