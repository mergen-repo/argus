package syslog

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/bus"
)

// TestMain pins time.Local = time.UTC so RFC 3164 timestamps (which use
// LOCAL time per §4.1.2) are deterministic across CI runners regardless of
// the host TZ. Plan §V1 line 355 mandates this.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

// sampleEnvelope returns the canonical worked-example envelope from the
// plan §"RFC 3164 / RFC 5424 Byte-Trace Worked Examples". Its byte output
// is pinned by TestRFC3164_GoldenBytes and TestRFC5424_GoldenBytes.
func sampleEnvelope() *bus.Envelope {
	ts := time.Date(2026, time.May, 4, 10, 0, 0, 0, time.UTC)
	return &bus.Envelope{
		EventVersion: bus.CurrentEventVersion,
		ID:           "00000000-0000-0000-0000-0000000000ee",
		Type:         "sim.binding_mismatch",
		Timestamp:    ts,
		TenantID:     "00000000-0000-0000-0000-000000000001",
		Severity:     "medium",
		Source:       "aaa",
		Title:        "IMEI mismatch detected",
		Message:      "Mismatch summary",
		Entity: &bus.EntityRef{
			Type: "sim",
			ID:   "00000000-0000-0000-0000-000000000abc",
		},
		Meta: map[string]interface{}{
			"sim_id": "00000000-0000-0000-0000-000000000abc",
		},
	}
}

// sampleConfig returns the canonical destination config from the plan worked
// example: facility=16 (local0), hostname="argus-host", PID=12345, the
// reserved enterprise number 32473 (VAL-098-03 placeholder), and the fixed
// timestamp 2026-05-04T10:00:00Z.
func sampleConfig(format string) DestConfig {
	return DestConfig{
		Format:     format,
		Hostname:   "argus-host",
		PID:        12345,
		Facility:   16,
		Enterprise: EnterprisePEN,
		Now:        time.Date(2026, time.May, 4, 10, 0, 0, 0, time.UTC),
	}
}

// TestRFC3164_GoldenBytes locks the unframed payload to plan §V1 byte-for-byte.
// PRI = facility(16)*8 + severity(medium=4) = 132 → "<132>".
// Day 4 is space-padded to "May  4" (two spaces, RFC 3164 §4.1.2).
func TestRFC3164_GoldenBytes(t *testing.T) {
	env := sampleEnvelope()
	got, err := Format(env, sampleConfig(FormatRFC3164))
	if err != nil {
		t.Fatalf("Format returned error: %v", err)
	}
	want := []byte("<132>May  4 10:00:00 argus-host argus[12345]: sim.binding_mismatch tenant=00000000-0000-0000-0000-000000000001 sim_id=00000000-0000-0000-0000-000000000abc severity=medium IMEI mismatch detected")
	if !bytes.Equal(got, want) {
		t.Fatalf("RFC3164 byte mismatch\n  got:  %q\n  want: %q", got, want)
	}
}

// TestRFC5424_GoldenBytes locks the unframed payload to plan §V2 byte-for-byte,
// including the UTF-8 BOM (EF BB BF) preceding MSG.
// SD-PARAM order is fixed: tenant_id → sorted meta keys → severity.
func TestRFC5424_GoldenBytes(t *testing.T) {
	env := sampleEnvelope()
	got, err := Format(env, sampleConfig(FormatRFC5424))
	if err != nil {
		t.Fatalf("Format returned error: %v", err)
	}
	bom := []byte{0xEF, 0xBB, 0xBF}
	var want bytes.Buffer
	want.WriteString(`<132>1 2026-05-04T10:00:00.000Z argus-host argus 12345 sim.binding_mismatch [argus@32473 tenant_id="00000000-0000-0000-0000-000000000001" sim_id="00000000-0000-0000-0000-000000000abc" severity="medium"] `)
	want.Write(bom)
	want.WriteString("IMEI mismatch detected")
	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("RFC5424 byte mismatch\n  got:  %q\n  want: %q", got, want.Bytes())
	}
}

// TestRFC5424_BOMPresent asserts the BOM appears immediately after the
// STRUCTURED-DATA closing `]` and a single space, per RFC 5424 §6.4.
func TestRFC5424_BOMPresent(t *testing.T) {
	got, err := Format(sampleEnvelope(), sampleConfig(FormatRFC5424))
	if err != nil {
		t.Fatalf("Format returned error: %v", err)
	}
	bom := []byte{0xEF, 0xBB, 0xBF}
	idx := bytes.Index(got, bom)
	if idx < 0 {
		t.Fatalf("BOM (EF BB BF) not found in payload: %q", got)
	}
	// Byte before BOM must be SP, byte before that must be `]`.
	if idx < 2 || got[idx-1] != ' ' || got[idx-2] != ']' {
		t.Fatalf("BOM context unexpected: ...%q[BOM]...", got[max0(idx-4):idx])
	}
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// TestRFC3164_DayPadding asserts that single-digit days produce a
// space-padded "Mmm  d" string ("May  4", not "May 04"). Two-digit days
// render unchanged ("May 14").
func TestRFC3164_DayPadding(t *testing.T) {
	cases := []struct {
		name string
		when time.Time
		want string
	}{
		{"single-digit-day", time.Date(2026, time.May, 4, 10, 0, 0, 0, time.UTC), "May  4 10:00:00"},
		{"two-digit-day", time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC), "May 14 10:00:00"},
		{"single-digit-day-jan", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "Jan  1 00:00:00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := sampleEnvelope()
			cfg := sampleConfig(FormatRFC3164)
			cfg.Now = tc.when
			got, err := Format(env, cfg)
			if err != nil {
				t.Fatalf("Format error: %v", err)
			}
			if !bytes.Contains(got, []byte(tc.want)) {
				t.Fatalf("expected timestamp %q in payload\n  got: %q", tc.want, got)
			}
		})
	}
}

// TestPRI_Combinations exercises the PRI table from plan §V3:
// PRI = facility*8 + severity_numeric.
func TestPRI_Combinations(t *testing.T) {
	cases := []struct {
		name     string
		severity string
		facility int
		wantPRI  int
	}{
		// local0 (facility 16)
		{"critical_local0", "critical", 16, 130},
		{"high_local0", "high", 16, 131},
		{"medium_local0", "medium", 16, 132},
		{"low_local0", "low", 16, 133},
		{"info_local0", "info", 16, 134},
		// local7 (facility 23)
		{"critical_local7", "critical", 23, 186},
		{"high_local7", "high", 23, 187},
		{"medium_local7", "medium", 23, 188},
		{"low_local7", "low", 23, 189},
		{"info_local7", "info", 23, 190},
		// user (facility 1)
		{"critical_user", "critical", 1, 10},
		{"high_user", "high", 1, 11},
		{"medium_user", "medium", 1, 12},
		{"low_user", "low", 1, 13},
		{"info_user", "info", 1, 14},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pri(tc.facility, syslogSeverity(tc.severity))
			if got != tc.wantPRI {
				t.Fatalf("PRI(facility=%d, sev=%s)=%d; want %d", tc.facility, tc.severity, got, tc.wantPRI)
			}
		})
	}
}

// TestPRI_Info_Local0 is the dispatch-named worked-example check: info on
// local0 → 134.
func TestPRI_Info_Local0(t *testing.T) {
	got := pri(16, syslogSeverity("info"))
	if got != 134 {
		t.Fatalf("info on local0: got PRI=%d, want 134", got)
	}
}

// TestSeverityMapping asserts the canonical 5-level table + the defensive
// default (5 / Notice) for empty + unknown values.
func TestSeverityMapping(t *testing.T) {
	cases := []struct {
		argus    string
		wantNum  int
		wantName string
	}{
		{"critical", 2, "Critical"},
		{"high", 3, "Error"},
		{"medium", 4, "Warning"},
		{"low", 5, "Notice"},
		{"info", 6, "Informational"},
		{"", 5, "Notice (default)"},
		{"bogus", 5, "Notice (default)"},
	}
	for _, tc := range cases {
		t.Run(tc.argus, func(t *testing.T) {
			got := syslogSeverity(tc.argus)
			if got != tc.wantNum {
				t.Fatalf("syslogSeverity(%q)=%d (%s expected); want %d", tc.argus, got, tc.wantName, tc.wantNum)
			}
		})
	}
}

// TestSeverityMapping_DefaultsToNotice locks the empty/unknown defensive
// default at 5 (Notice) per plan V3.
func TestSeverityMapping_DefaultsToNotice(t *testing.T) {
	if syslogSeverity("") != 5 {
		t.Fatalf("empty severity: want 5 (Notice)")
	}
	if syslogSeverity("not-a-real-severity") != 5 {
		t.Fatalf("unknown severity: want 5 (Notice)")
	}
}

// TestSDParamEscape verifies the RFC 5424 §6.3.3 escaping rules: the three
// reserved chars `"`, `\`, `]` are escaped as `\"`, `\\`, `\]`.
func TestSDParamEscape(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`plain`, `plain`},
		{`with"quote`, `with\"quote`},
		{`with\backslash`, `with\\backslash`},
		{`with]bracket`, `with\]bracket`},
		{`all"\]three`, `all\"\\\]three`},
		// Order matters: backslash must be escaped first.
		{`a\b"c]d`, `a\\b\"c\]d`},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := rfc5424EscapeSDValue(tc.in)
			if got != tc.want {
				t.Fatalf("rfc5424EscapeSDValue(%q)=%q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRFC5424_StructuredDataEscaping is the end-to-end version of
// TestSDParamEscape: a Meta value containing all three reserved characters
// must round-trip through Format with proper escaping.
func TestRFC5424_StructuredDataEscaping(t *testing.T) {
	env := sampleEnvelope()
	env.Meta = map[string]interface{}{
		"trick": `a\b"c]d`,
	}
	got, err := Format(env, sampleConfig(FormatRFC5424))
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}
	want := []byte(`trick="a\\b\"c\]d"`)
	if !bytes.Contains(got, want) {
		t.Fatalf("escaped SD-PARAM not found\n  got: %q\n  want substring: %q", got, want)
	}
}

// TestRFC5424_NoStructuredData_FallbackHyphen asserts that an envelope with
// no tenant, no meta, no severity emits SD-DATA as the NILVALUE "-" per
// RFC 5424 §6.3.2.
func TestRFC5424_NoStructuredData_FallbackHyphen(t *testing.T) {
	env := &bus.Envelope{
		EventVersion: bus.CurrentEventVersion,
		Type:         "minimal",
		Timestamp:    time.Date(2026, time.May, 4, 10, 0, 0, 0, time.UTC),
		Title:        "minimal event",
		// TenantID, Severity, Meta intentionally empty.
	}
	got, err := Format(env, sampleConfig(FormatRFC5424))
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}
	// SD-DATA segment should be exactly " - " between MSGID and BOM.
	bom := []byte{0xEF, 0xBB, 0xBF}
	bomIdx := bytes.Index(got, bom)
	if bomIdx < 0 {
		t.Fatalf("BOM missing: %q", got)
	}
	if bomIdx < 3 || got[bomIdx-1] != ' ' || got[bomIdx-2] != '-' || got[bomIdx-3] != ' ' {
		t.Fatalf("expected NILVALUE '-' before BOM; got bytes ...%q[BOM]", got[max0(bomIdx-6):bomIdx])
	}
}

// TestFormat_UnknownFormat returns an explicit error rather than a malformed
// payload. This keeps the worker layer's error-counter path well-defined.
func TestFormat_UnknownFormat(t *testing.T) {
	cfg := sampleConfig("not-a-format")
	_, err := Format(sampleEnvelope(), cfg)
	if err == nil {
		t.Fatal("expected error for unknown format, got nil")
	}
}

// TestFormat_NilEnvelope returns an explicit error.
func TestFormat_NilEnvelope(t *testing.T) {
	_, err := Format(nil, sampleConfig(FormatRFC3164))
	if err == nil {
		t.Fatal("expected error for nil envelope, got nil")
	}
}

// TestRFC5424_DeterministicMetaOrder asserts the SD-PARAM key sequence is
// alphabetical regardless of map insertion order. Without this guarantee
// golden tests would flake.
func TestRFC5424_DeterministicMetaOrder(t *testing.T) {
	env := sampleEnvelope()
	env.Meta = map[string]interface{}{
		"zebra": "z",
		"apple": "a",
		"mango": "m",
	}
	// Run multiple times; bytes must be byte-identical.
	cfg := sampleConfig(FormatRFC5424)
	first, err := Format(env, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		got, err := Format(env, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, first) {
			t.Fatalf("nondeterministic Format output\n  run0: %q\n  runN: %q", first, got)
		}
	}
	// Also assert the alphabetical order.
	wantSeq := []byte(`apple="a" mango="m" zebra="z"`)
	if !bytes.Contains(first, wantSeq) {
		t.Fatalf("meta keys not alphabetical\n  got: %q\n  want substring: %q", first, wantSeq)
	}
}

// TestValidTransport / TestValidFormat lock the consts.go validators.
func TestValidTransport(t *testing.T) {
	for _, ok := range Transports {
		if !ValidTransport(ok) {
			t.Errorf("ValidTransport(%q)=false; want true", ok)
		}
	}
	for _, bad := range []string{"", "UDP", "rcp", "tlss", "ftp"} {
		if ValidTransport(bad) {
			t.Errorf("ValidTransport(%q)=true; want false", bad)
		}
	}
}

func TestValidFormat(t *testing.T) {
	for _, ok := range Formats {
		if !ValidFormat(ok) {
			t.Errorf("ValidFormat(%q)=false; want true", ok)
		}
	}
	for _, bad := range []string{"", "RFC3164", "rfc3146", "json"} {
		if ValidFormat(bad) {
			t.Errorf("ValidFormat(%q)=true; want false", bad)
		}
	}
}

func TestValidCategory(t *testing.T) {
	for _, ok := range Categories {
		if !ValidCategory(ok) {
			t.Errorf("ValidCategory(%q)=false; want true", ok)
		}
	}
	for _, bad := range []string{"", "AUDIT", "session.created", "unknown"} {
		if ValidCategory(bad) {
			t.Errorf("ValidCategory(%q)=true; want false", bad)
		}
	}
}

// TestEnterprisePEN_DefaultedWhenZero ensures the formatter substitutes the
// canonical PEN when cfg.Enterprise is left at the zero value.
func TestEnterprisePEN_DefaultedWhenZero(t *testing.T) {
	cfg := sampleConfig(FormatRFC5424)
	cfg.Enterprise = 0
	got, err := Format(sampleEnvelope(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("argus@32473")) {
		t.Fatalf("expected default PEN 32473 in payload: %q", got)
	}
}
