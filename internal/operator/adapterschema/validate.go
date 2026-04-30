package adapterschema

import (
	"encoding/json"
	"fmt"
)

// ParseNested parses raw plaintext bytes into a NestedConfig. Returns
// ErrShapeInvalidJSON if the bytes are not a JSON object, or
// ErrValidation wrapped with detail if any sub-object fails to parse
// as a ProtocolConfig (must at least have an `enabled` bool).
//
// ParseNested does NOT run the full Validate semantics — it's a low-
// level parser. Callers that need full validation should follow with
// Validate(parsed).
func ParseNested(raw []byte) (NestedConfig, error) {
	var out NestedConfig
	if firstNonWSByte(raw) != '{' {
		return out, ErrShapeInvalidJSON
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return out, ErrShapeInvalidJSON
	}
	for k, v := range top {
		if !IsValidProtocol(k) {
			return out, fmt.Errorf("%w: unknown protocol key %q", ErrValidation, k)
		}
		sub, subErr := parseProtocolConfig(v)
		if subErr != nil {
			return out, fmt.Errorf("%w: protocol %q: %v", ErrValidation, k, subErr)
		}
		switch k {
		case "radius":
			out.Radius = sub
		case "diameter":
			out.Diameter = sub
		case "sba":
			out.SBA = sub
		case "http":
			out.HTTP = sub
		case "mock":
			out.Mock = sub
		}
	}
	return out, nil
}

// parseProtocolConfig parses a single per-protocol sub-blob. Requires
// `enabled` to be a bool (absent is allowed — defaults to false, which
// is friendly to admin-authored JSON that omits the flag for the
// disabled case; Validate tightens this for stricter call sites).
func parseProtocolConfig(raw json.RawMessage) (*ProtocolConfig, error) {
	if firstNonWSByte(raw) != '{' {
		return nil, fmt.Errorf("sub-object must be a JSON object")
	}
	// Use a local struct to carry just the Enabled field so the JSON
	// decoder doesn't type-error on protocol-specific fields we don't
	// care about at the schema layer.
	var probe struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	sub := &ProtocolConfig{Raw: append(json.RawMessage(nil), raw...)}
	if probe.Enabled != nil {
		sub.Enabled = *probe.Enabled
	}
	return sub, nil
}

// Validate enforces structural rules on a parsed NestedConfig. Rules:
//
//  1. At least one protocol key must be present (otherwise the blob
//     is indistinguishable from an empty {} — rejected by ParseNested).
//  2. Zero-enabled is ALLOWED (per dispatch advisor watch-out #1 (b)).
//     Operators with zero enabled protocols are persisted-but-non-
//     routable — the adapter registry returns zero adapters; the
//     operator is visible in UI but no protocol traffic can be handled.
//     This is a known semantic, documented here so it is not surfaced
//     as an error at the persistence layer.
//  3. For each enabled=true sub-object, the sub-blob must be valid
//     JSON (ParseNested already enforces this). Protocol-specific
//     required-field checks (e.g. RADIUS shared_secret) live on the
//     adapter factory — keeping field-level validation out of the
//     schema layer avoids double-maintenance with the adapter
//     packages. Future stories may lift required-field rules here.
//
// Validate returns nil on success, ErrValidation wrapped with detail
// on failure.
func Validate(n NestedConfig) error {
	total := 0
	if n.Radius != nil {
		total++
	}
	if n.Diameter != nil {
		total++
	}
	if n.SBA != nil {
		total++
	}
	if n.HTTP != nil {
		total++
	}
	if n.Mock != nil {
		total++
	}
	if total == 0 {
		return fmt.Errorf("%w: at least one protocol key required", ErrValidation)
	}
	return nil
}

// ValidateRaw is a convenience wrapper that parses plaintext raw bytes
// and runs Validate in one step.
func ValidateRaw(raw []byte) error {
	n, err := ParseNested(raw)
	if err != nil {
		return err
	}
	return Validate(n)
}

// DeriveEnabledProtocols returns the sorted enabled-protocol slice in
// canonical order. Used to populate the `enabled_protocols` response
// field added by Wave 2 (plan §API Specifications > GET detail).
// Safe to call on a NestedConfig produced by ParseNested — protocols
// absent from the parsed struct contribute nothing.
func DeriveEnabledProtocols(n NestedConfig) []string {
	out := make([]string, 0, 5)
	for _, p := range canonicalProtocolOrder {
		sub := subForProtocol(&n, p)
		if sub != nil && sub.Enabled {
			out = append(out, p)
		}
	}
	return out
}

// DerivePrimaryProtocol returns the first enabled protocol in
// canonical order, or "" if none are enabled. Wave 3 uses this for
// the legacy single-protocol TestConnection alias.
func DerivePrimaryProtocol(n NestedConfig) string {
	enabled := DeriveEnabledProtocols(n)
	if len(enabled) == 0 {
		return ""
	}
	return enabled[0]
}

// subForProtocol returns the sub-ProtocolConfig pointer for the given
// protocol name, or nil if the protocol is not present in the struct.
func subForProtocol(n *NestedConfig, protocol string) *ProtocolConfig {
	switch protocol {
	case "radius":
		return n.Radius
	case "diameter":
		return n.Diameter
	case "sba":
		return n.SBA
	case "http":
		return n.HTTP
	case "mock":
		return n.Mock
	default:
		return nil
	}
}

// SubConfigRaw returns the raw JSON for a given protocol's sub-blob
// from a parsed NestedConfig, or nil if the protocol is absent. The
// returned bytes are suitable to pass to adapter.Registry.GetOrCreate
// as the per-protocol factory config.
func SubConfigRaw(n NestedConfig, protocol string) json.RawMessage {
	sub := subForProtocol(&n, protocol)
	if sub == nil {
		return nil
	}
	return sub.Raw
}
