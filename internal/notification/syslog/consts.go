// Package syslog implements the Argus native syslog forwarder (STORY-098).
// It emits canonical bus.Envelope events as RFC 3164 (BSD legacy) or
// RFC 5424 frames over UDP/TCP/TLS to one or more configured destinations.
//
// This file holds the closed-set string enums used by both the migration
// CHECK constraints (TBL-61) and the API validators. The shape mirrors
// internal/severity/severity.go (PAT-022 — string enums must have a Go
// const set + Validate helper paired with the SQL CHECK constraint).
package syslog

// Transport identifies the syslog wire protocol.
const (
	TransportUDP = "udp"
	TransportTCP = "tcp"
	TransportTLS = "tls"
)

// Transports lists every valid transport. Used by API validators and
// FE dropdowns. MUST match the TBL-61 transport CHECK constraint exactly.
var Transports = []string{TransportUDP, TransportTCP, TransportTLS}

// ValidTransport reports whether s is a canonical transport value.
func ValidTransport(s string) bool {
	for _, v := range Transports {
		if v == s {
			return true
		}
	}
	return false
}

// Format identifies the wire-format encoding of a syslog message.
const (
	FormatRFC3164 = "rfc3164"
	FormatRFC5424 = "rfc5424"
)

// Formats lists every valid format. MUST match the TBL-61 format CHECK
// constraint exactly.
var Formats = []string{FormatRFC3164, FormatRFC5424}

// ValidFormat reports whether s is a canonical format value.
func ValidFormat(s string) bool {
	for _, v := range Formats {
		if v == s {
			return true
		}
	}
	return false
}

// Canonical event categories (advisor Brief #3 + plan Bus Subject → Category
// Mapping). MUST match the canonical 7-set used by the API validator and the
// FE checkbox group.
const (
	CategoryAuth    = "auth"
	CategoryAudit   = "audit"
	CategoryAlert   = "alert"
	CategorySession = "session"
	CategoryPolicy  = "policy"
	CategoryIMEI    = "imei"
	CategorySystem  = "system"
)

// Categories lists every canonical event category.
var Categories = []string{
	CategoryAuth,
	CategoryAudit,
	CategoryAlert,
	CategorySession,
	CategoryPolicy,
	CategoryIMEI,
	CategorySystem,
}

// ValidCategory reports whether s is a canonical category value.
func ValidCategory(s string) bool {
	for _, v := range Categories {
		if v == s {
			return true
		}
	}
	return false
}

// EnterprisePEN is the IANA Private Enterprise Number embedded in RFC 5424
// SD-IDs as `argus@<EnterprisePEN>`. The value 32473 is the IANA-reserved
// "for documentation and examples only" PEN (RFC 5612) — VAL-098-03
// placeholder pending registration of an Argus PEN (D-198-01).
const EnterprisePEN = 32473
