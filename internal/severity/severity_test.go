package severity

import (
	"errors"
	"testing"
)

func TestValidate_AcceptsCanonicalValues(t *testing.T) {
	for _, v := range Values {
		if err := Validate(v); err != nil {
			t.Errorf("Validate(%q) = %v; want nil", v, err)
		}
	}
}

func TestValidate_RejectsOldValues(t *testing.T) {
	for _, v := range []string{"warning", "error"} {
		err := Validate(v)
		if !errors.Is(err, ErrInvalidSeverity) {
			t.Errorf("Validate(%q) = %v; want ErrInvalidSeverity", v, err)
		}
	}
}

func TestValidate_RejectsUppercase(t *testing.T) {
	for _, v := range []string{"Critical", "HIGH", "Info"} {
		err := Validate(v)
		if !errors.Is(err, ErrInvalidSeverity) {
			t.Errorf("Validate(%q) = %v; want ErrInvalidSeverity", v, err)
		}
	}
}

func TestValidate_RejectsEmpty(t *testing.T) {
	err := Validate("")
	if !errors.Is(err, ErrInvalidSeverity) {
		t.Errorf("Validate(\"\") = %v; want ErrInvalidSeverity", err)
	}
}

func TestOrdinal_StrictOrder(t *testing.T) {
	if !(Ordinal(Info) < Ordinal(Low)) {
		t.Errorf("expected Ordinal(info) < Ordinal(low)")
	}
	if !(Ordinal(Low) < Ordinal(Medium)) {
		t.Errorf("expected Ordinal(low) < Ordinal(medium)")
	}
	if !(Ordinal(Medium) < Ordinal(High)) {
		t.Errorf("expected Ordinal(medium) < Ordinal(high)")
	}
	if !(Ordinal(High) < Ordinal(Critical)) {
		t.Errorf("expected Ordinal(high) < Ordinal(critical)")
	}
}

func TestOrdinal_InvalidReturnsZero(t *testing.T) {
	for _, v := range []string{"warning", "", "foo"} {
		if got := Ordinal(v); got != 0 {
			t.Errorf("Ordinal(%q) = %d; want 0", v, got)
		}
	}
}

func TestIsValid(t *testing.T) {
	for _, v := range Values {
		if !IsValid(v) {
			t.Errorf("IsValid(%q) = false; want true", v)
		}
	}
	for _, v := range []string{"warning", "error", "Critical", "HIGH", ""} {
		if IsValid(v) {
			t.Errorf("IsValid(%q) = true; want false", v)
		}
	}
}
