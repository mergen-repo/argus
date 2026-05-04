package sba

import (
	"encoding/json"
	"strings"
	"testing"

	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
)

var (
	benchSBAIMEI string
	benchSBASV   string
	benchSBAOK   bool
)

func nopLogger() zerolog.Logger { return zerolog.Nop() }

func newTestRegistry() *obsmetrics.Registry { return obsmetrics.NewRegistry() }

func TestParsePEI_IMEI15(t *testing.T) {
	imei, sv, ok := ParsePEI("imei-359211089765432", nopLogger(), nil)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if imei != "359211089765432" {
		t.Errorf("imei: got %q, want %q", imei, "359211089765432")
	}
	if sv != "" {
		t.Errorf("sv: got %q, want %q", sv, "")
	}
}

func TestParsePEI_IMEISV16(t *testing.T) {
	imei, sv, ok := ParsePEI("imeisv-3592110897654321", nopLogger(), nil)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if imei != "359211089765432" {
		t.Errorf("imei: got %q, want %q", imei, "359211089765432")
	}
	if sv != "10" {
		t.Errorf("sv: got %q, want %q (zero-padded per TS 23.003)", sv, "10")
	}
}

func TestParsePEI_MAC_Ignored(t *testing.T) {
	imei, sv, ok := ParsePEI("mac-aabbccddeeff", nopLogger(), nil)
	if !ok {
		t.Fatal("expected ok=true for mac- prefix (silently ignored)")
	}
	if imei != "" {
		t.Errorf("imei: got %q, want %q", imei, "")
	}
	if sv != "" {
		t.Errorf("sv: got %q, want %q", sv, "")
	}
}

func TestParsePEI_EUI64_Ignored(t *testing.T) {
	imei, sv, ok := ParsePEI("eui64-0123456789abcdef", nopLogger(), nil)
	if !ok {
		t.Fatal("expected ok=true for eui64- prefix (silently ignored)")
	}
	if imei != "" {
		t.Errorf("imei: got %q, want %q", imei, "")
	}
	if sv != "" {
		t.Errorf("sv: got %q, want %q", sv, "")
	}
}

func TestParsePEI_Malformed_BadDigits(t *testing.T) {
	reg := newTestRegistry()
	imei, sv, ok := ParsePEI("imei-abc211089765432", nopLogger(), reg)
	if ok {
		t.Fatal("expected ok=false for bad digits")
	}
	if imei != "" || sv != "" {
		t.Errorf("expected empty imei/sv, got imei=%q sv=%q", imei, sv)
	}
	count := testutil.ToFloat64(reg.IMEICaptureParseErrorsTotal.WithLabelValues("5g_sba"))
	if count != 1 {
		t.Errorf("parse error counter: got %v, want 1", count)
	}
}

func TestParsePEI_Empty(t *testing.T) {
	imei, sv, ok := ParsePEI("", nopLogger(), nil)
	if ok {
		t.Fatal("expected ok=false for empty pei")
	}
	if imei != "" || sv != "" {
		t.Errorf("expected empty imei/sv, got imei=%q sv=%q", imei, sv)
	}
}

func TestAuthenticationRequest_PEIOmitEmpty(t *testing.T) {
	b, err := json.Marshal(AuthenticationRequest{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"pei"`) {
		t.Errorf("zero-value AuthenticationRequest should not emit pei key (omitempty), got: %s", b)
	}
}

func TestAmf3GppAccessRegistration_PEIOmitEmpty(t *testing.T) {
	b, err := json.Marshal(Amf3GppAccessRegistration{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"pei"`) {
		t.Errorf("zero-value Amf3GppAccessRegistration should not emit pei key (omitempty), got: %s", b)
	}
}

func TestExtractPEIRaw_IMEI3GPP_ReturnsEmpty(t *testing.T) {
	if got := ExtractPEIRaw("imei-359211089765432"); got != "" {
		t.Errorf("got %q, want empty for 3GPP imei- prefix", got)
	}
}

func TestExtractPEIRaw_IMEISV3GPP_ReturnsEmpty(t *testing.T) {
	if got := ExtractPEIRaw("imeisv-3592110897654321"); got != "" {
		t.Errorf("got %q, want empty for 3GPP imeisv- prefix", got)
	}
}

func TestExtractPEIRaw_MAC_ReturnsRaw(t *testing.T) {
	const input = "mac-aabbccddeeff"
	if got := ExtractPEIRaw(input); got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestExtractPEIRaw_EUI64_ReturnsRaw(t *testing.T) {
	const input = "eui64-0011223344556677"
	if got := ExtractPEIRaw(input); got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestExtractPEIRaw_Unknown_ReturnsEmpty(t *testing.T) {
	if got := ExtractPEIRaw("xyz-something"); got != "" {
		t.Errorf("got %q, want empty for unknown prefix", got)
	}
}

func TestExtractPEIRaw_Empty_ReturnsEmpty(t *testing.T) {
	if got := ExtractPEIRaw(""); got != "" {
		t.Errorf("got %q, want empty for empty input", got)
	}
}

func BenchmarkParsePEI_5G(b *testing.B) {
	nop := zerolog.Nop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSBAIMEI, benchSBASV, benchSBAOK = ParsePEI("imeisv-3592110897654321", nop, nil)
	}
}
