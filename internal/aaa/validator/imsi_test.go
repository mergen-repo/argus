package validator

import (
	"errors"
	"testing"
)

func TestValidateIMSI(t *testing.T) {
	tests := []struct {
		name   string
		imsi   string
		strict bool
		want   error
	}{
		{"empty strict", "", true, ErrEmptyIMSI},
		{"empty non-strict", "", false, ErrEmptyIMSI},
		{"14 digits strict", "28601123456789", true, nil},
		{"15 digits strict", "286011234567890", true, nil},
		{"13 digits strict", "2860112345678", true, ErrInvalidIMSIFormat},
		{"16 digits strict", "2860112345678901", true, ErrInvalidIMSIFormat},
		{"alpha strict", "ABC012345678901", true, ErrInvalidIMSIFormat},
		{"space strict", " 28601123456789", true, ErrInvalidIMSIFormat},
		{"non-PLMN non-strict accepts", "test-imsi-1", false, nil},
		{"alpha non-strict accepts", "ABC", false, nil},
		{"tab-padded strict rejects", "\t28601123456789", true, ErrInvalidIMSIFormat},
		{"unicode-digit strict rejects", "٢٨٦٠١١٢٣٤٥٦٧٨٩", true, ErrInvalidIMSIFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateIMSI(tt.imsi, tt.strict)
			if !errors.Is(got, tt.want) && !(got == nil && tt.want == nil) {
				t.Errorf("ValidateIMSI(%q, %v) = %v; want %v", tt.imsi, tt.strict, got, tt.want)
			}
		})
	}
}

func TestIsIMSIFormatValid(t *testing.T) {
	tests := []struct {
		imsi string
		want bool
	}{
		{"", false},
		{"28601123456789", true},
		{"286011234567890", true},
		{"2860112345678", false},
		{"ABC012345678901", false},
	}
	for _, tt := range tests {
		t.Run(tt.imsi, func(t *testing.T) {
			if got := IsIMSIFormatValid(tt.imsi); got != tt.want {
				t.Errorf("IsIMSIFormatValid(%q) = %v; want %v", tt.imsi, got, tt.want)
			}
		})
	}
}
