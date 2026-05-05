package syslog

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/severity"
)

// DestConfig carries the per-destination knobs needed to format a single
// envelope. It is the only state injected into the pure formatters: no clocks
// beyond Now, no DB lookups, no global state.
//
// Hostname / PID / Enterprise are captured once at boot in main.go and reused
// across destinations.
type DestConfig struct {
	// Format selects RFC 3164 or RFC 5424 wire format. MUST be one of the
	// FormatRFCxxxx constants; otherwise Format returns an error.
	Format string
	// Hostname is the trimmed system hostname embedded in the HOSTNAME field.
	Hostname string
	// PID is os.Getpid() captured at boot.
	PID int
	// Facility is the syslog facility (0..23) embedded in PRI. Validated by
	// the API + DB CHECK constraint; the formatter trusts it.
	Facility int
	// Enterprise is the IANA Private Enterprise Number embedded in RFC 5424
	// SD-IDs as `argus@<Enterprise>`. Defaults to EnterprisePEN when zero.
	Enterprise int
	// Now is the timestamp injection point used by tests. When zero, the
	// formatter calls time.Now().UTC().
	Now time.Time
}

// syslogSeverity maps a canonical Argus severity string to the RFC 5424
// numeric severity (§6.2.1). Empty / unknown defaults to 5 (Notice) per
// plan V3. The mapping is intentionally one-way (no reverse lookup) because
// the canonical Argus taxonomy is the source of truth.
//
//	critical -> 2 (Critical)
//	high     -> 3 (Error)
//	medium   -> 4 (Warning)
//	low      -> 5 (Notice)
//	info     -> 6 (Informational)
//	(empty/unknown) -> 5 (Notice — defensive default)
func syslogSeverity(argus string) int {
	switch argus {
	case severity.Critical:
		return 2
	case severity.High:
		return 3
	case severity.Medium:
		return 4
	case severity.Low:
		return 5
	case severity.Info:
		return 6
	default:
		return 5
	}
}

// pri computes the RFC 5424 §6.2.1 PRI value: facility*8 + severity.
func pri(facility, sev int) int {
	return facility*8 + sev
}

// Format renders an envelope as bytes per cfg.Format. The returned slice is
// the unframed payload; transport.Frame applies RFC 6587 octet-counting for
// TCP/TLS callers.
//
// Format is pure: identical inputs produce identical bytes. Determinism
// guarantees:
//   - Map iteration over env.Meta is sorted by key alphabetically.
//   - cfg.Now is used verbatim when non-zero; otherwise time.Now().UTC().
//   - RFC 3164 timestamps render in cfg.Now.Location(); tests pin time.Local
//     to time.UTC via TestMain.
func Format(env *bus.Envelope, cfg DestConfig) ([]byte, error) {
	if env == nil {
		return nil, fmt.Errorf("syslog: nil envelope")
	}
	now := cfg.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	switch cfg.Format {
	case FormatRFC3164:
		return formatRFC3164(env, cfg, now), nil
	case FormatRFC5424:
		return formatRFC5424(env, cfg, now), nil
	default:
		return nil, fmt.Errorf("syslog: unknown format %q (want %s|%s)", cfg.Format, FormatRFC3164, FormatRFC5424)
	}
}

// formatRFC3164 implements the BSD legacy wire format (RFC 3164 §4.1).
//
//	<PRI>TIMESTAMP HOSTNAME TAG: MSG
//
// TIMESTAMP = "Mmm dd hh:mm:ss" with day space-padded to 2 chars
// ("May  4" not "May 04"). The Go layout `Jan _2 15:04:05` produces this.
//
// VAL-077 — Argus deployment runs in UTC; the device's local clock IS UTC.
// RFC 3164 §4.1.2 specifies "device local time", which for Argus = UTC by
// deployment convention. Predictable UTC timestamps simplify SIEM ingestion
// (rsyslog/Splunk index-time correlation) and match the RFC 5424 emitter
// which is mandatorily UTC. Tests pin time.Local=UTC via TestMain.
//
// TAG = APP-NAME[PID] — APP-NAME is hardcoded "argus".
// MSG = "<event_type> tenant=<tid> [<meta_key>=<value> ]+ severity=<sev> <env.Title>"
// Meta keys are emitted in alphabetical order for determinism.
func formatRFC3164(env *bus.Envelope, cfg DestConfig, now time.Time) []byte {
	var buf bytes.Buffer
	sev := syslogSeverity(env.Severity)
	buf.WriteByte('<')
	buf.WriteString(strconv.Itoa(pri(cfg.Facility, sev)))
	buf.WriteByte('>')
	// `Jan _2 15:04:05` — `_2` is Go's zero-value-less space-padded day-of-month.
	buf.WriteString(now.Format("Jan _2 15:04:05"))
	buf.WriteByte(' ')
	buf.WriteString(cfg.Hostname)
	buf.WriteString(" argus[")
	buf.WriteString(strconv.Itoa(cfg.PID))
	buf.WriteString("]: ")
	buf.WriteString(env.Type)
	buf.WriteString(" tenant=")
	buf.WriteString(env.TenantID)
	for _, k := range sortedMetaKeys(env.Meta) {
		buf.WriteByte(' ')
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(rfc3164ScrubValue(metaValueString(env.Meta[k])))
	}
	buf.WriteString(" severity=")
	buf.WriteString(env.Severity)
	if env.Title != "" {
		buf.WriteByte(' ')
		buf.WriteString(rfc3164ScrubValue(env.Title))
	}
	return buf.Bytes()
}

// formatRFC5424 implements RFC 5424 §6.
//
//	<PRI>1 TIMESTAMP HOSTNAME APP-NAME PROCID MSGID STRUCTURED-DATA BOM MSG
//
//	VERSION       = "1"
//	TIMESTAMP     = RFC 3339 with millisecond precision + `Z` (UTC mandatory).
//	APP-NAME      = "argus"
//	PROCID        = pid as decimal
//	MSGID         = env.Type truncated to 32 chars
//	STRUCTURED-DATA = "[argus@<PEN> tenant_id=\"...\" <sorted-meta>... severity=\"...\"]"
//	                  When env has no tenant/meta/severity, falls back to "-" (NILVALUE).
//	BOM           = bytes EF BB BF (RFC 5424 §6.4 — UTF-8 BOM precedes MSG).
//	MSG           = env.Title (or env.Message if Title empty). Newlines space-replaced.
//
// SD-PARAM ordering (deterministic): tenant_id first, then sorted meta keys, then severity last.
func formatRFC5424(env *bus.Envelope, cfg DestConfig, now time.Time) []byte {
	var buf bytes.Buffer
	sev := syslogSeverity(env.Severity)
	enterprise := cfg.Enterprise
	if enterprise == 0 {
		enterprise = EnterprisePEN
	}
	buf.WriteByte('<')
	buf.WriteString(strconv.Itoa(pri(cfg.Facility, sev)))
	buf.WriteString(">1 ")
	buf.WriteString(now.UTC().Format("2006-01-02T15:04:05.000Z07:00"))
	buf.WriteByte(' ')
	buf.WriteString(cfg.Hostname)
	buf.WriteString(" argus ")
	buf.WriteString(strconv.Itoa(cfg.PID))
	buf.WriteByte(' ')
	buf.WriteString(truncate(env.Type, 32))
	buf.WriteByte(' ')
	buf.Write(buildStructuredData(env, enterprise))
	buf.WriteByte(' ')
	// UTF-8 BOM (RFC 5424 §6.4).
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	msg := env.Title
	if msg == "" {
		msg = env.Message
	}
	buf.WriteString(rfc3164ScrubValue(msg))
	return buf.Bytes()
}

// buildStructuredData renders the RFC 5424 STRUCTURED-DATA element. When the
// envelope carries no useful context (no tenant, no meta, no severity), the
// NILVALUE "-" is returned per RFC 5424 §6.3.2.
func buildStructuredData(env *bus.Envelope, enterprise int) []byte {
	hasContent := env.TenantID != "" || env.Severity != "" || len(env.Meta) > 0
	if !hasContent {
		return []byte{'-'}
	}
	var sd bytes.Buffer
	sd.WriteByte('[')
	sd.WriteString("argus@")
	sd.WriteString(strconv.Itoa(enterprise))
	if env.TenantID != "" {
		sd.WriteString(` tenant_id="`)
		sd.WriteString(rfc5424EscapeSDValue(env.TenantID))
		sd.WriteByte('"')
	}
	for _, k := range sortedMetaKeys(env.Meta) {
		sd.WriteByte(' ')
		sd.WriteString(k)
		sd.WriteString(`="`)
		sd.WriteString(rfc5424EscapeSDValue(metaValueString(env.Meta[k])))
		sd.WriteByte('"')
	}
	if env.Severity != "" {
		sd.WriteString(` severity="`)
		sd.WriteString(rfc5424EscapeSDValue(env.Severity))
		sd.WriteByte('"')
	}
	sd.WriteByte(']')
	return sd.Bytes()
}

// rfc5424EscapeSDValue escapes the three reserved chars in SD-PARAM values
// per RFC 5424 §6.3.5: `"`, `\`, `]` -> `\"`, `\\`, `\]`. Order matters: the
// backslash MUST be escaped first so the replacement backslashes in the
// later steps are not double-escaped.
func rfc5424EscapeSDValue(s string) string {
	if !strings.ContainsAny(s, `"\]`) {
		return s
	}
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		`]`, `\]`,
	)
	return r.Replace(s)
}

// rfc3164ScrubValue replaces newlines with spaces; RFC 3164 messages MUST be
// single-line. CR/LF are the only forbidden control chars in practice.
func rfc3164ScrubValue(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	r := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")
	return r.Replace(s)
}

// metaValueString renders a Meta value as a deterministic string. JSON scalar
// types are emitted via fmt; nil renders as empty.
func metaValueString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
}

// sortedMetaKeys returns Meta keys in lexicographic order. Iteration over
// map[string]interface{} is otherwise non-deterministic, which would break
// byte-level golden tests.
func sortedMetaKeys(m map[string]interface{}) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// truncate clips s to n bytes (UTF-8 safe at the byte level — used only on
// MSGID which is ASCII event-type identifiers like "sim.binding_mismatch").
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
