package diameter

import (
	"errors"
	"testing"
)

var (
	benchDiamIMEI string
	benchDiamSV   string
	benchDiamErr  error
)

func buildTerminalInformationAVP(subAVPs []*AVP) *AVP {
	return NewAVPGrouped(AVPCodeTerminalInformation, AVPFlagMandatory, VendorID3GPP, subAVPs)
}

func TestExtractTerminalInformation_FullPair(t *testing.T) {
	inner := []*AVP{
		NewAVPString(AVPCodeIMEI, AVPFlagMandatory, VendorID3GPP, "359211089765432"),
		NewAVPString(AVPCodeSoftwareVersion, AVPFlagMandatory, VendorID3GPP, "01"),
	}
	outer := buildTerminalInformationAVP(inner)
	encoded := outer.Encode()

	decoded, _, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("decode avp: %v", err)
	}

	imei, sv, err := ExtractTerminalInformation([]*AVP{decoded})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if imei != "359211089765432" {
		t.Fatalf("imei %q, expected %q", imei, "359211089765432")
	}
	if sv != "01" {
		t.Fatalf("sv %q, expected %q", sv, "01")
	}
}

func TestExtractTerminalInformation_IMEISVOnly(t *testing.T) {
	inner := []*AVP{
		NewAVPString(AVPCodeIMEISV, AVPFlagMandatory, VendorID3GPP, "3592110897654321"),
	}
	outer := buildTerminalInformationAVP(inner)
	encoded := outer.Encode()

	decoded, _, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("decode avp: %v", err)
	}

	imei, sv, err := ExtractTerminalInformation([]*AVP{decoded})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if imei != "359211089765432" {
		t.Fatalf("imei %q, expected %q", imei, "359211089765432")
	}
	if sv != "10" {
		t.Fatalf("sv %q, expected %q", sv, "10")
	}
}

func TestExtractTerminalInformation_MalformedGrouped(t *testing.T) {
	outer := &AVP{
		Code:     AVPCodeTerminalInformation,
		Flags:    AVPFlagMandatory | AVPFlagVendor,
		VendorID: VendorID3GPP,
		Data:     []byte{0xFF, 0xFE, 0xFD, 0x00, 0x01, 0x02, 0x03},
	}

	_, _, err := ExtractTerminalInformation([]*AVP{outer})
	if !errors.Is(err, ErrIMEICaptureMalformed) {
		t.Fatalf("expected ErrIMEICaptureMalformed, got %v", err)
	}
}

func TestExtractTerminalInformation_AbsentAVP(t *testing.T) {
	avps := []*AVP{
		NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, "sess-1"),
	}

	imei, sv, err := ExtractTerminalInformation(avps)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if imei != "" || sv != "" {
		t.Fatalf("expected empty strings, got imei=%q sv=%q", imei, sv)
	}
}

func TestExtractTerminalInformation_BadInnerLengths(t *testing.T) {
	inner := []*AVP{
		NewAVPString(AVPCodeIMEI, AVPFlagMandatory, VendorID3GPP, "abc"),
	}
	outer := buildTerminalInformationAVP(inner)
	encoded := outer.Encode()

	decoded, _, err := DecodeAVP(encoded)
	if err != nil {
		t.Fatalf("decode avp: %v", err)
	}

	_, _, err = ExtractTerminalInformation([]*AVP{decoded})
	if !errors.Is(err, ErrIMEICaptureMalformed) {
		t.Fatalf("expected ErrIMEICaptureMalformed, got %v", err)
	}
}

func BenchmarkExtractTerminalInformation_S6a(b *testing.B) {
	inner := []*AVP{
		NewAVPString(AVPCodeIMEI, AVPFlagMandatory, VendorID3GPP, "359211089765432"),
		NewAVPString(AVPCodeSoftwareVersion, AVPFlagMandatory, VendorID3GPP, "01"),
	}
	outer := buildTerminalInformationAVP(inner)
	encoded := outer.Encode()
	decoded, _, err := DecodeAVP(encoded)
	if err != nil {
		b.Fatalf("setup DecodeAVP: %v", err)
	}
	avps := []*AVP{decoded}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchDiamIMEI, benchDiamSV, benchDiamErr = ExtractTerminalInformation(avps)
	}
}
