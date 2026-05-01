package radius

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

// buildIMEISVVSA assembles the RFC 2865 Type 26 wire payload for a 3GPP
// vendor-specific attribute (vendor-id 10415, vendor-type 20, value).
func buildIMEISVVSA(value []byte) []byte {
	// Format: vendor-id (4) | vendor-type (1) | vendor-len (1) | value (N)
	// vendor-len covers itself + value bytes => 2 + len(value).
	buf := make([]byte, 0, 6+len(value))
	tmp := make([]byte, 4)
	binary.BigEndian.PutUint32(tmp, vendorID3GPP)
	buf = append(buf, tmp...)
	buf = append(buf, vendorType3GPPIMEISV)
	buf = append(buf, byte(2+len(value)))
	buf = append(buf, value...)
	return buf
}

// newPacketWithVSA returns an Access-Request packet containing the given
// raw VSA payload as a single Type 26 attribute, plus a User-Name for
// correlation.
func newPacketWithVSA(t *testing.T, vsa []byte, userName string) *radius.Packet {
	t.Helper()
	pkt := radius.New(radius.CodeAccessRequest, []byte("testing123"))
	if userName != "" {
		if err := rfc2865.UserName_SetString(pkt, userName); err != nil {
			t.Fatalf("UserName_SetString: %v", err)
		}
	}
	pkt.Attributes.Add(radius.Type(26), radius.Attribute(vsa))
	return pkt
}

// freshRegistry returns a fresh metrics Registry isolated from the global
// default — counters in other tests do not leak in.
func freshRegistry() *obsmetrics.Registry {
	return obsmetrics.NewRegistry()
}

func TestExtract3GPPIMEISV_ASCIIComma(t *testing.T) {
	vsa := buildIMEISVVSA([]byte("359211089765432,01"))
	pkt := newPacketWithVSA(t, vsa, "286010000000001")

	reg := freshRegistry()
	imei, sv, ok := Extract3GPPIMEISV(pkt, zerolog.Nop(), reg)
	if !ok {
		t.Fatalf("Extract3GPPIMEISV ok=false, want true")
	}
	if imei != "359211089765432" {
		t.Errorf("imei = %q, want %q", imei, "359211089765432")
	}
	if sv != "01" {
		t.Errorf("sv = %q, want %q", sv, "01")
	}
	if got := testutil.ToFloat64(reg.IMEICaptureParseErrorsTotal.WithLabelValues("radius")); got != 0 {
		t.Errorf("parse errors counter = %v, want 0 on happy path", got)
	}
}

func TestExtract3GPPIMEISV_ASCIIBare16(t *testing.T) {
	vsa := buildIMEISVVSA([]byte("3592110897654321"))
	pkt := newPacketWithVSA(t, vsa, "286010000000002")

	reg := freshRegistry()
	imei, sv, ok := Extract3GPPIMEISV(pkt, zerolog.Nop(), reg)
	if !ok {
		t.Fatalf("Extract3GPPIMEISV ok=false, want true")
	}
	if imei != "359211089765432" {
		t.Errorf("imei = %q, want %q", imei, "359211089765432")
	}
	if sv != "10" {
		t.Errorf("sv = %q, want %q (trailing digit right-padded with '0')", sv, "10")
	}
	if got := testutil.ToFloat64(reg.IMEICaptureParseErrorsTotal.WithLabelValues("radius")); got != 0 {
		t.Errorf("parse errors counter = %v, want 0 on happy path", got)
	}
}

func TestExtract3GPPIMEISV_BCDLegacy(t *testing.T) {
	// TS 24.008 swapped-BCD: each byte stores low nibble first, high nibble
	// second. Bytes below decode (low,high) per byte to:
	//   0x53→"35"  0x29→"92"  0x11→"11"  0x80→"08"
	//   0x79→"97"  0x56→"65"  0x34→"43"  0x12→"21"
	// concatenated: "3592110897654321" → imei="359211089765432", sv="21".
	//
	// NOTE: the embedded spec's input bytes contained typos at idx 2 (0x12
	// instead of 0x11) and idx 6 (0x43 instead of 0x34) which contradicted
	// its own stated expected output. The bytes here are corrected to the
	// canonical TS 24.008 inputs that produce the spec's expected output.
	bcd := []byte{0x53, 0x29, 0x11, 0x80, 0x79, 0x56, 0x34, 0x12}
	vsa := buildIMEISVVSA(bcd)
	pkt := newPacketWithVSA(t, vsa, "286010000000003")

	reg := freshRegistry()
	imei, sv, ok := Extract3GPPIMEISV(pkt, zerolog.Nop(), reg)
	if !ok {
		t.Fatalf("Extract3GPPIMEISV ok=false, want true")
	}
	if imei != "359211089765432" {
		t.Errorf("imei = %q, want %q", imei, "359211089765432")
	}
	if sv != "21" {
		t.Errorf("sv = %q, want %q", sv, "21")
	}
	if got := testutil.ToFloat64(reg.IMEICaptureParseErrorsTotal.WithLabelValues("radius")); got != 0 {
		t.Errorf("parse errors counter = %v, want 0 on happy path", got)
	}
}

func TestExtract3GPPIMEISV_AbsentVSA(t *testing.T) {
	// Build a packet with NO Type 26 attribute at all.
	pkt := radius.New(radius.CodeAccessRequest, []byte("testing123"))
	if err := rfc2865.UserName_SetString(pkt, "286010000000004"); err != nil {
		t.Fatalf("UserName_SetString: %v", err)
	}

	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf)
	reg := freshRegistry()

	imei, sv, ok := Extract3GPPIMEISV(pkt, logger, reg)
	if ok {
		t.Fatalf("Extract3GPPIMEISV ok=true, want false (no VSA present)")
	}
	if imei != "" || sv != "" {
		t.Errorf("imei=%q sv=%q, want both empty", imei, sv)
	}
	if logBuf.Len() != 0 {
		t.Errorf("absent VSA must not emit a log; got %q", logBuf.String())
	}
	if got := testutil.ToFloat64(reg.IMEICaptureParseErrorsTotal.WithLabelValues("radius")); got != 0 {
		t.Errorf("parse errors counter = %v, want 0 on absent VSA", got)
	}
}

func TestExtract3GPPIMEISV_MalformedShortValue(t *testing.T) {
	// Value "abc" — 3 ASCII bytes, no digits, no comma, wrong length →
	// fails all three shape detectors → malformed.
	vsa := buildIMEISVVSA([]byte("abc"))
	pkt := newPacketWithVSA(t, vsa, "286010000000005")

	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf)
	reg := freshRegistry()

	imei, sv, ok := Extract3GPPIMEISV(pkt, logger, reg)
	if ok {
		t.Fatalf("Extract3GPPIMEISV ok=true, want false (malformed value)")
	}
	if imei != "" || sv != "" {
		t.Errorf("imei=%q sv=%q, want both empty on malformed", imei, sv)
	}

	// WARN log emitted exactly once with required structured fields.
	logged := logBuf.String()
	if logged == "" {
		t.Fatalf("malformed VSA must emit a WARN log; got empty buffer")
	}
	if !strings.Contains(logged, `"level":"warn"`) {
		t.Errorf("log level not WARN: %s", logged)
	}
	if !strings.Contains(logged, `"protocol":"radius"`) {
		t.Errorf("log missing protocol=radius: %s", logged)
	}
	if !strings.Contains(logged, `"correlation_id":"286010000000005"`) {
		t.Errorf("log missing correlation_id from User-Name: %s", logged)
	}

	// Counter incremented exactly once.
	got := testutil.ToFloat64(reg.IMEICaptureParseErrorsTotal.WithLabelValues("radius"))
	if got != 1 {
		t.Errorf("parse errors counter = %v, want 1 on single malformed VSA", got)
	}
}
