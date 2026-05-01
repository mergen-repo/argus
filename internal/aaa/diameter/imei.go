// Package diameter — S6a Terminal-Information AVP parser (TS 29.272 §7.3.3).
//
// PAT-025: IMEI and IMSI are both string type. IMEI is 15 digits (equipment
// identity), IMSI is 15 digits (subscriber identity). Do not confuse them.
//
// Counter/log call-site note: ExtractTerminalInformation is a pure function
// (no *Registry, no zerolog.Logger parameter) consistent with PAT-006 and the
// ExtractSubscriptionID shape. The caller (STORY-094 S6a enricher) is
// responsible for invoking reg.IncIMEICaptureParseErrors("diameter_s6a") and
// emitting a WARN log on ErrIMEICaptureMalformed. The counter is already
// registered centrally at internal/observability/metrics/metrics.go:326.
package diameter

import "errors"

// ErrIMEICaptureMalformed is returned when AVP 350 is present but its
// grouped content cannot be decoded or does not carry a valid IMEI.
var ErrIMEICaptureMalformed = errors.New("imei capture: malformed Terminal-Information AVP")

// ExtractTerminalInformation extracts IMEI and Software-Version strings from
// Diameter S6a Terminal-Information AVP (code 350, vendor 10415, TS 29.272
// §7.3.3).
//
// Return contract:
//   - outer AVP 350 absent → ("", "", nil)  — silent absence, not an error
//   - outer present, inner malformed → ("", "", ErrIMEICaptureMalformed)
//   - 1402+1403 valid → (imei, sv, nil)
//   - 1404 valid (16 chars) → (imei=first15, sv=char16+"0", nil)
//   - none of the above → ("", "", ErrIMEICaptureMalformed)
func ExtractTerminalInformation(avps []*AVP) (imei, sv string, err error) {
	outer := FindAVPVendor(avps, AVPCodeTerminalInformation, VendorID3GPP)
	if outer == nil {
		return "", "", nil
	}

	inner, gErr := outer.GetGrouped()
	if gErr != nil {
		return "", "", ErrIMEICaptureMalformed
	}

	if a := FindAVP(inner, AVPCodeIMEI); a != nil && validateDigits(a.GetString(), 15) {
		imei = a.GetString()
		if b := FindAVP(inner, AVPCodeSoftwareVersion); b != nil && validateDigits(b.GetString(), 2) {
			sv = b.GetString()
		}
		return imei, sv, nil
	}

	if a := FindAVP(inner, AVPCodeIMEISV); a != nil && validateDigits(a.GetString(), 16) {
		s := a.GetString()
		return s[0:15], s[15:16] + "0", nil
	}

	return "", "", ErrIMEICaptureMalformed
}

// validateDigits returns true if s consists entirely of ASCII digits and has
// exactly want characters. Uses byte comparison to avoid unicode.IsDigit
// surprises with non-ASCII codepoints.
func validateDigits(s string, want int) bool {
	if len(s) != want {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
