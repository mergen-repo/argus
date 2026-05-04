// Package sba — PEI (Permanent Equipment Identifier) parsers for 5G SBA.
//
// Two sibling helpers are exported:
//
//   - ParsePEI (STORY-093 Task 4): decodes the pei JSON field and returns
//     structured IMEI + SoftwareVersion for 3GPP forms.
//   - ExtractPEIRaw (STORY-097 Task 1): returns the verbatim PEI URI for
//     non-3GPP forms (mac-, eui64-) for forensic retention in SessionContext.
//
// ParsePEI accepts:
//
//   - "imei-"   + 15 ASCII digits (total length 20)  → IMEI, no SV.
//   - "imeisv-" + 16 ASCII digits (total length 23)  → IMEI (first 15) + SV (last digit, zero-padded to 2).
//   - "mac-"    / "eui64-"                           → silently ignored (non-3GPP access), ok=true.
//   - empty                                          → ok=false, no log.
//   - any other / malformed                          → ok=false, WARN log + counter.
//
// Terminology (PAT-025 — keep IMEI and IMSI distinct):
//
//   - IMSI — International Mobile Subscriber Identity (15 digits, identifies the SIM/subscriber).
//     Carried in supiOrSuci as "imsi-..." on the 5G SBA wire.
//   - IMEI — International Mobile Equipment Identity (15 digits, identifies the handset/modem).
//     Carried in the pei field as "imei-..." or "imeisv-..." on the 5G SBA wire.
package sba

import (
	"strings"

	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/rs/zerolog"
)

const protocolLabel5GSBA = "5g_sba"

// ParsePEI decodes the PEI tagged-URI string from a 5G SBA request body and
// returns the extracted IMEI and Software Version.
//
// Return contract (stable for STORY-093 Task 6 wiring — do NOT change):
//
//	imei — 15 ASCII digits when ok==true AND a 3GPP identity was present, "" otherwise.
//	sv   — 2 ASCII digits when imeisv was decoded, "" otherwise.
//	ok   — true iff pei was absent/empty OR a valid (or silently-ignored) PEI was found.
//	       false iff pei is present but malformed (WARN log + counter incremented).
//
// Logger and registry are optional; passing zerolog.Nop() and nil is safe.
func ParsePEI(pei string, logger zerolog.Logger, reg *obsmetrics.Registry) (imei, sv string, ok bool) {
	if pei == "" {
		return "", "", false
	}

	switch {
	case strings.HasPrefix(pei, "imei-"):
		if len(pei) != 20 {
			warnMalformed(pei, logger, reg)
			return "", "", false
		}
		suffix := pei[5:]
		if !allDigits(suffix) {
			warnMalformed(pei, logger, reg)
			return "", "", false
		}
		return suffix, "", true

	case strings.HasPrefix(pei, "imeisv-"):
		if len(pei) != 23 {
			warnMalformed(pei, logger, reg)
			return "", "", false
		}
		suffix := pei[7:]
		if !allDigits(suffix) {
			warnMalformed(pei, logger, reg)
			return "", "", false
		}
		return suffix[:15], suffix[15:16] + "0", true

	case strings.HasPrefix(pei, "mac-"), strings.HasPrefix(pei, "eui64-"):
		return "", "", true

	default:
		warnMalformed(pei, logger, reg)
		return "", "", false
	}
}

// ExtractPEIRaw returns the raw PEI URI for non-3GPP forms (mac-, eui64-).
// Returns empty string for 3GPP forms (imei-, imeisv-) — those values are
// captured in the structured IMEI/SV fields and the raw URI is not retained.
// Returns empty string for unknown prefixes or empty input.
//
// Per PROTOCOLS.md §PEI: forensic retention contract for non-3GPP forms
// requires the original URI to flow into SessionContext.PEIRaw.
// In-memory only: not persisted to DB (no TBL-59 column for PEIRaw in v1).
func ExtractPEIRaw(pei string) string {
	if pei == "" {
		return ""
	}
	if strings.HasPrefix(pei, "mac-") || strings.HasPrefix(pei, "eui64-") {
		return pei
	}
	return ""
}

// warnMalformed emits a single WARN log and increments the parse-error counter.
func warnMalformed(pei string, logger zerolog.Logger, reg *obsmetrics.Registry) {
	logger.Warn().
		Str("protocol", protocolLabel5GSBA).
		Str("pei_prefix", safePEIPrefix(pei)).
		Int("pei_len", len(pei)).
		Msg("imei: malformed PEI field in 5G SBA request")
	reg.IncIMEICaptureParseErrors(protocolLabel5GSBA)
}

// safePEIPrefix returns the first up to 12 characters of pei for logging
// without exposing a full IMEI/device identity in log lines.
func safePEIPrefix(pei string) string {
	if len(pei) <= 12 {
		return pei
	}
	return pei[:12]
}

// allDigits reports whether s is non-empty and consists solely of ASCII '0'..'9'.
func allDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
