package auth

import (
	"regexp"
	"strings"
	"testing"
)

var backupCodeRegex = regexp.MustCompile(`^[A-Z0-9]{4}-[A-Z0-9]{4}$`)

func TestGenerateBackupCodeFormat_Format(t *testing.T) {
	for i := 0; i < 20; i++ {
		code, err := GenerateBackupCodeFormat()
		if err != nil {
			t.Fatalf("GenerateBackupCodeFormat() error: %v", err)
		}
		if !backupCodeRegex.MatchString(code) {
			t.Errorf("code %q does not match pattern ^[A-Z0-9]{4}-[A-Z0-9]{4}$", code)
		}
	}
}

func TestGenerateBackupCodeFormat_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		code, err := GenerateBackupCodeFormat()
		if err != nil {
			t.Fatalf("GenerateBackupCodeFormat() error: %v", err)
		}
		if seen[code] {
			t.Errorf("duplicate code generated: %q", code)
		}
		seen[code] = true
	}
}

func TestNormalizeBackupCode_LowercaseSpaceSeparated(t *testing.T) {
	got := NormalizeBackupCode("abcd efgh")
	want := "ABCD-EFGH"
	if got != want {
		t.Errorf("NormalizeBackupCode(%q) = %q, want %q", "abcd efgh", got, want)
	}
}

func TestNormalizeBackupCode_LowercaseDashSeparated(t *testing.T) {
	got := NormalizeBackupCode("abcd-efgh")
	want := "ABCD-EFGH"
	if got != want {
		t.Errorf("NormalizeBackupCode(%q) = %q, want %q", "abcd-efgh", got, want)
	}
}

func TestNormalizeBackupCode_AlreadyNormalized(t *testing.T) {
	got := NormalizeBackupCode("ABCD-EFGH")
	want := "ABCD-EFGH"
	if got != want {
		t.Errorf("NormalizeBackupCode(%q) = %q, want %q", "ABCD-EFGH", got, want)
	}
}

func TestNormalizeBackupCode_NoDash(t *testing.T) {
	got := NormalizeBackupCode("abcdefgh")
	want := "ABCD-EFGH"
	if got != want {
		t.Errorf("NormalizeBackupCode(%q) = %q, want %q", "abcdefgh", got, want)
	}
}

func TestNormalizeBackupCode_UppercaseNoDash(t *testing.T) {
	got := NormalizeBackupCode("ABCDEFGH")
	want := "ABCD-EFGH"
	if got != want {
		t.Errorf("NormalizeBackupCode(%q) = %q, want %q", "ABCDEFGH", got, want)
	}
}

func TestNormalizeBackupCode_MixedCase(t *testing.T) {
	got := NormalizeBackupCode("Ab1C-eF2G")
	want := "AB1C-EF2G"
	if got != want {
		t.Errorf("NormalizeBackupCode(%q) = %q, want %q", "Ab1C-eF2G", got, want)
	}
}

func TestNormalizeBackupCode_LeadingTrailingSpaces(t *testing.T) {
	got := NormalizeBackupCode("  abcd-efgh  ")
	want := "ABCD-EFGH"
	if got != want {
		t.Errorf("NormalizeBackupCode(%q) = %q, want %q", "  abcd-efgh  ", got, want)
	}
}

func TestNormalizeBackupCode_GeneratedCodeRoundTrip(t *testing.T) {
	code, err := GenerateBackupCodeFormat()
	if err != nil {
		t.Fatalf("GenerateBackupCodeFormat() error: %v", err)
	}
	lower := strings.ToLower(code)
	normalized := NormalizeBackupCode(lower)
	if normalized != code {
		t.Errorf("round-trip failed: generated %q, lowercased to %q, normalized to %q", code, lower, normalized)
	}
}
