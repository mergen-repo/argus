// Package radius — IMEI / IMEISV capture from 3GPP RADIUS VSAs.
//
// STORY-093 Task 2: parser for the 3GPP-IMEISV vendor-specific attribute
// (vendor 10415, vendor-type 20) carried inside RFC 2865 Type 26.
//
// Three accepted wire shapes (auto-detected, deterministic order):
//
//  1. ASCII with comma  (modern, TS 29.061 §16.4.7) — value: "359211089765432,01"
//     length 18, comma-separated 15-digit IMEI + 2-digit SV.
//
//  2. ASCII bare 16     (legacy IMEISV)             — value: "3592110897654321"
//     length 16, no separator. SV is the trailing single digit, right-padded
//     to 2 digits with '0' (e.g. trailing "1" → sv "10") per the STORY-093
//     wire-format spec.
//
//  3. BCD-packed 8 byte (rare CDMA / legacy NAS)    — 8 bytes, per-octet
//     nibble swap (low nibble first, high nibble second), 0xF fill stripped
//     from the tail. The 16 BCD digits split into IMEI = digits[0:15] and
//     SV = digits[14:16] (the last IMEI digit overlaps with the SV high
//     digit per the wire-format spec example).
//
// Terminology (PAT-025 — keep these distinct):
//
//   - IMSI   — International Mobile Subscriber Identity (15 digits, identifies
//     the SIM/subscriber). Carried in User-Name on RADIUS.
//   - IMEI   — International Mobile Equipment Identity (15 digits, identifies
//     the handset/modem). Captured here.
//   - IMEISV — IMEI + 2-digit Software Version suffix. The combined wire form
//     transmitted by 3GPP networks; this parser splits it into (imei, sv).
package radius

import (
	"bytes"
	"encoding/binary"
	"strings"

	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/rs/zerolog"
	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

// 3GPP IMEISV vendor-type constant (TS 29.061). Vendor-id 10415 is already
// declared at server.go (vendorID3GPP) — reused here.
const (
	vendorType3GPPIMEISV uint8 = 20
)

// Extract3GPPIMEISV walks every RFC 2865 Type 26 (Vendor-Specific) attribute
// in pkt and returns the first 3GPP-IMEISV value (vendor 10415, vendor-type
// 20) successfully decoded into a 15-digit IMEI and 2-digit Software Version.
//
// Return contract (stable for STORY-093 Task 5 wiring — do NOT change):
//
//	imei — 15 ASCII digits when ok==true, "" otherwise.
//	sv   — 2 ASCII digits when ok==true, "" otherwise.
//	ok   — true iff a valid IMEISV VSA was found AND parsed AND validated.
//
// Behaviour matrix:
//
//   - No matching VSA → ok=false, silent (no log, no counter).
//   - Malformed VSA value (wrong length / non-digit / unrecognized shape) →
//     ok=false, single WARN log, parse-error counter incremented once.
//
// Logger and registry are optional; passing zerolog.Nop() and nil is safe.
func Extract3GPPIMEISV(pkt *radius.Packet, logger zerolog.Logger, reg *obsmetrics.Registry) (imei, sv string, ok bool) {
	if pkt == nil {
		return "", "", false
	}

	const protocolLabel = "radius"

	var sawMalformed bool
	var lastValueLen int
	for _, avp := range pkt.Attributes {
		if avp == nil || avp.Type != radius.Type(26) {
			continue
		}
		raw := []byte(avp.Attribute)
		if len(raw) < 7 {
			continue
		}
		vendorID := binary.BigEndian.Uint32(raw[0:4])
		if vendorID != vendorID3GPP {
			continue
		}
		vendorType := raw[4]
		if vendorType != vendorType3GPPIMEISV {
			continue
		}
		vendorLen := int(raw[5])
		if vendorLen < 3 || 4+vendorLen > len(raw) {
			continue
		}
		valueBytes := raw[6 : 4+vendorLen]
		if len(valueBytes) == 0 {
			continue
		}

		decodedIMEI, decodedSV, decodeOK := decodeIMEISVValue(valueBytes)
		if decodeOK {
			return decodedIMEI, decodedSV, true
		}
		// remember last malformed value bytes for a single WARN log/counter
		// after the loop — avoid spamming on repeated malformed VSAs.
		sawMalformed = true
		lastValueLen = len(valueBytes)
	}

	if sawMalformed {
		correlationID := correlationIDFromPacket(pkt)
		logger.Warn().
			Str("protocol", protocolLabel).
			Str("correlation_id", correlationID).
			Int("value_len", lastValueLen).
			Msg("imei: malformed 3GPP-IMEISV VSA (could not auto-detect shape)")
		reg.IncIMEICaptureParseErrors(protocolLabel)
	}

	return "", "", false
}

// decodeIMEISVValue auto-detects the wire shape of a 3GPP-IMEISV VSA value
// and returns (imei, sv, true) on success, ("", "", false) on any malformation.
//
// Detection order (deterministic, must not be reordered — see package doc):
//  1. Any byte > 0x39 → BCD-packed (digits / nibbles, not ASCII).
//  2. Contains a ','  → ASCII with comma separator.
//  3. Length 16, all  → ASCII bare 16.
//     digits
//  4. Otherwise       → malformed.
func decodeIMEISVValue(value []byte) (imei, sv string, ok bool) {
	// Shape 1: BCD-packed (any non-ASCII-digit byte triggers this branch).
	for _, b := range value {
		if b > 0x39 {
			return decodeBCDIMEISV(value)
		}
	}

	// Shape 2: ASCII with comma.
	if bytes.IndexByte(value, ',') >= 0 {
		parts := strings.SplitN(string(value), ",", 2)
		if len(parts) != 2 {
			return "", "", false
		}
		imei, sv = parts[0], parts[1]
		if len(imei) != 15 || !isAllDigits(imei) {
			return "", "", false
		}
		if len(sv) != 2 || !isAllDigits(sv) {
			return "", "", false
		}
		return imei, sv, true
	}

	// Shape 3: ASCII bare 16.
	if len(value) == 16 && isAllDigits(string(value)) {
		imei = string(value[0:15])
		// SV is the trailing single digit, left-padded to 2 digits per
		// TS 23.003 §6.2.2 (e.g. "1" → "10").
		sv = string([]byte{value[15], '0'})
		return imei, sv, true
	}

	return "", "", false
}

// decodeBCDIMEISV decodes 8-byte BCD-packed IMEISV. Each octet stores two BCD
// digits with the low nibble first (TS 24.008 §10.5.1.4 swapped-BCD format).
// 0xF nibbles are fill (used when the digit count is odd) and are stripped
// from the tail before length validation.
//
// Per the wire-format spec, the 16 BCD digits split into:
//
//	imei = digits[0:15]
//	sv   = digits[14:16]   (last IMEI digit overlaps with SV high digit)
//
// For the canonical example "3592110897654321" → imei="359211089765432",
// sv="21".
func decodeBCDIMEISV(value []byte) (imei, sv string, ok bool) {
	if len(value) != 8 {
		return "", "", false
	}
	digits := make([]byte, 0, 16)
	for _, b := range value {
		low := b & 0x0F
		high := (b >> 4) & 0x0F
		digits = append(digits, low, high)
	}
	// Strip trailing 0xF fill nibbles.
	for len(digits) > 0 && digits[len(digits)-1] == 0x0F {
		digits = digits[:len(digits)-1]
	}
	if len(digits) != 16 {
		return "", "", false
	}
	for _, d := range digits {
		if d > 9 {
			return "", "", false
		}
	}
	out := make([]byte, 16)
	for i, d := range digits {
		out[i] = '0' + d
	}
	imei = string(out[0:15])
	sv = string(out[14:16])
	return imei, sv, true
}

// correlationIDFromPacket returns a best-effort correlation key for log
// emission. Prefer User-Name (matches the spec's "use User-Name" guidance);
// returns "" when no User-Name attribute is present.
func correlationIDFromPacket(pkt *radius.Packet) string {
	if name, err := rfc2865.UserName_LookupString(pkt); err == nil && name != "" {
		return name
	}
	return ""
}

// isAllDigits reports whether s is non-empty and consists solely of ASCII '0'..'9'.
func isAllDigits(s string) bool {
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
