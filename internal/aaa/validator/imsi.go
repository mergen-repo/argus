// Package validator provides input validators used at AAA and API boundaries.
package validator

import (
	"errors"
	"regexp"
)

// ErrEmptyIMSI is returned when an IMSI is the empty string (always invalid).
var ErrEmptyIMSI = errors.New("validator: imsi: empty")

// ErrInvalidIMSIFormat is returned when strict validation rejects a non-PLMN IMSI.
var ErrInvalidIMSIFormat = errors.New("validator: imsi: invalid format")

// imsiRE matches PLMN-compliant IMSI: MCC(3) + MNC(2-3) + MSIN(9-10) → 14 or 15 digits.
var imsiRE = regexp.MustCompile(`^\d{14,15}$`)

// ValidateIMSI returns nil when the IMSI is acceptable.
// Empty IMSIs are always invalid (strict or not).
// When strict=false (IMSI_STRICT_VALIDATION=false), any non-empty IMSI is accepted
// (supports test networks using non-PLMN IMSI values).
// When strict=true, IMSI must match ^\d{14,15}$.
func ValidateIMSI(imsi string, strict bool) error {
	if imsi == "" {
		return ErrEmptyIMSI
	}
	if !strict {
		return nil
	}
	if !imsiRE.MatchString(imsi) {
		return ErrInvalidIMSIFormat
	}
	return nil
}

// IsIMSIFormatValid is the strict-mode predicate (no bypass).
// Used by scan jobs and audit tooling that must always assert PLMN compliance.
func IsIMSIFormatValid(imsi string) bool {
	if imsi == "" {
		return false
	}
	return imsiRE.MatchString(imsi)
}
