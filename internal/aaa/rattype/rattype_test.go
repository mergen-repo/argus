package rattype

import (
	"testing"
)

func TestFromRADIUS(t *testing.T) {
	tests := []struct {
		input    uint8
		expected string
	}{
		{1, UTRAN},
		{2, GERAN},
		{6, LTE},
		{7, NR5G},
		{8, NR5GNSA},
		{9, NBIOT},
		{10, LTEM},
		{0, Unknown},
		{99, Unknown},
		{255, Unknown},
	}

	for _, tt := range tests {
		got := FromRADIUS(tt.input)
		if got != tt.expected {
			t.Errorf("FromRADIUS(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFromDiameter(t *testing.T) {
	tests := []struct {
		input    uint32
		expected string
	}{
		{1000, UTRAN},
		{1001, GERAN},
		{1004, LTE},
		{1005, NBIOT},
		{1006, LTEM},
		{1008, NR5GNSA},
		{1009, NR5G},
		{0, Unknown},
		{9999, Unknown},
	}

	for _, tt := range tests {
		got := FromDiameter(tt.input)
		if got != tt.expected {
			t.Errorf("FromDiameter(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFromSBA(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"NR", NR5G},
		{"nr", NR5G},
		{"nr_5g", NR5G},
		{"NR_5G", NR5G},
		{"E-UTRA-NR", NR5GNSA},
		{"e-utra-nr", NR5GNSA},
		{"E-UTRA", LTE},
		{"e-utra", LTE},
		{"EUTRA", LTE},
		{"LTE", LTE},
		{"NB-IoT", NBIOT},
		{"nb_iot", NBIOT},
		{"LTE-M", LTEM},
		{"cat_m1", LTEM},
		{"garbage", Unknown},
		{"", Unknown},
	}

	for _, tt := range tests {
		got := FromSBA(tt.input)
		if got != tt.expected {
			t.Errorf("FromSBA(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"lte", LTE},
		{"LTE", LTE},
		{"4G", LTE},
		{"4g", LTE},
		{"5G", NR5G},
		{"5g_sa", NR5G},
		{"5G_SA", NR5G},
		{"5g_nsa", NR5GNSA},
		{"5G_NSA", NR5GNSA},
		{"CAT_M1", LTEM},
		{"cat_m1", LTEM},
		{"nb_iot", NBIOT},
		{"NB-IoT", NBIOT},
		{"2g", GERAN},
		{"3g", UTRAN},
		{"nr_5g", NR5G},
		{"utran", UTRAN},
		{"geran", GERAN},
		{"unknown", Unknown},
		{"garbage", Unknown},
		{"  lte  ", LTE},
	}

	for _, tt := range tests {
		got := Normalize(tt.input)
		if got != tt.expected {
			t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestDisplayName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{UTRAN, "3G"},
		{GERAN, "2G"},
		{LTE, "4G"},
		{NBIOT, "NB-IoT"},
		{LTEM, "LTE-M"},
		{NR5G, "5G"},
		{NR5GNSA, "5G-NSA"},
		{Unknown, "Unknown"},
		{"garbage", "Unknown"},
	}

	for _, tt := range tests {
		got := DisplayName(tt.input)
		if got != tt.expected {
			t.Errorf("DisplayName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsValid(t *testing.T) {
	for _, v := range AllCanonical() {
		if !IsValid(v) {
			t.Errorf("IsValid(%q) = false, want true", v)
		}
	}

	if IsValid("4g") {
		t.Error("IsValid(\"4g\") = true, want false (4g is an alias, not canonical)")
	}
	if IsValid("garbage") {
		t.Error("IsValid(\"garbage\") = true, want false")
	}
}

func TestAllCanonical(t *testing.T) {
	all := AllCanonical()
	if len(all) != 8 {
		t.Errorf("AllCanonical() has %d entries, want 8", len(all))
	}

	expected := map[string]bool{
		UTRAN: true, GERAN: true, LTE: true, NBIOT: true,
		LTEM: true, NR5G: true, NR5GNSA: true, Unknown: true,
	}
	for _, v := range all {
		if !expected[v] {
			t.Errorf("AllCanonical() contains unexpected %q", v)
		}
	}
}

func TestAllDisplayNames(t *testing.T) {
	names := AllDisplayNames()
	if len(names) != 8 {
		t.Errorf("AllDisplayNames() has %d entries, want 8", len(names))
	}
	if names[LTE] != "4G" {
		t.Errorf("AllDisplayNames()[LTE] = %q, want \"4G\"", names[LTE])
	}
}

func TestDisplayNameNormalizeRoundTrip(t *testing.T) {
	for _, canonical := range AllCanonical() {
		display := DisplayName(canonical)
		if display == "" {
			t.Errorf("DisplayName(%q) returned empty", canonical)
		}
		normalized := Normalize(display)
		if normalized != canonical {
			t.Errorf("Normalize(DisplayName(%q)) = %q, want %q (display=%q)", canonical, normalized, canonical, display)
		}
	}
}
