// Package adapterschema provides shape detection, validation, and
// flat→nested up-conversion for the encrypted `operators.adapter_config`
// column. Introduced in STORY-090 Wave 1 (D1-A) to support multi-protocol
// operators while preserving dual-read compatibility with pre-090 rows.
//
// The nested shape is the canonical post-090 plaintext:
//
//	{
//	  "radius":   {"enabled": true, "shared_secret": "...", "listen_addr": ":1812"},
//	  "diameter": {"enabled": true, "origin_host": "...", ...},
//	  "sba":      {"enabled": false},
//	  "http":     {"enabled": false},
//	  "mock":     {"enabled": false}
//	}
//
// Legacy flat shapes (pre-090) are single-protocol bodies keyed by the
// protocol-specific fields themselves, e.g. {"shared_secret":"x"} for
// RADIUS, {"origin_host":"x"} for Diameter, {"latency_ms":12} for mock.
package adapterschema

import (
	"encoding/json"
	"errors"
)

// Canonical protocol order — used by DeriveEnabledProtocols and for
// canonical-primary-protocol derivation. Keep in sync with plan §Data
// flow / API Specifications.
var canonicalProtocolOrder = []string{"diameter", "radius", "sba", "http", "mock"}

// validProtocolSet is the post-090 protocol allowlist. Includes "http"
// per D3-B (Wave 2). Kept distinct from the legacy handler-side
// validAdapterTypes map that gates pre-090 request bodies — see plan
// Task 1 §point 6.
var validProtocolSet = map[string]struct{}{
	"mock":     {},
	"radius":   {},
	"diameter": {},
	"sba":      {},
	"http":     {},
}

// Typed errors surface decoder / validation failures with a discriminated
// return. Callers MUST branch on errors.Is, not on string compare.
var (
	// ErrShapeInvalidJSON is returned by DetectShape and UpConvert when
	// the input bytes are not valid JSON at all. Most commonly surfaces
	// when a post-decrypt blob is corrupted (key rotation, truncated
	// ciphertext, wrong key). Distinct from ErrShapeUnknown to avoid
	// silent fall-through per STORY-090 advisor watch-out #1 (d).
	ErrShapeInvalidJSON = errors.New("adapterschema: input is not valid JSON")

	// ErrShapeUnknown is returned when JSON parses but matches neither
	// the nested shape nor any known legacy flat shape (no protocol
	// heuristic keys found and no nested protocol sub-key present).
	ErrShapeUnknown = errors.New("adapterschema: input shape is neither nested nor a known legacy flat shape")

	// ErrValidation is returned by Validate when the nested shape is
	// structurally sound but violates field rules.
	ErrValidation = errors.New("adapterschema: validation failed")

	// ErrUpConvertMissingHint is returned by UpConvert when the input
	// is flat-shaped but the caller did not supply a protocol hint and
	// the heuristic keys did not disambiguate.
	ErrUpConvertMissingHint = errors.New("adapterschema: flat shape requires protocol hint (legacy adapter_type)")
)

// Shape enumerates the concrete shapes DetectShape recognises.
type Shape int

const (
	// ShapeInvalid is returned together with ErrShapeInvalidJSON or
	// ErrShapeUnknown — never emitted on its own by a successful call.
	ShapeInvalid Shape = iota

	// ShapeNested is the canonical post-090 shape: a JSON object whose
	// keys are ALL in the protocol set. At least one sub-object must
	// contain an `"enabled"` field (bool) for the shape to qualify —
	// otherwise it is treated as ambiguously-flat.
	ShapeNested

	// ShapeFlatRadius / ShapeFlatDiameter / ShapeFlatSBA /
	// ShapeFlatHTTP / ShapeFlatMock are the five legacy per-protocol
	// flat shapes. Detected via protocol-specific heuristic keys.
	ShapeFlatRadius
	ShapeFlatDiameter
	ShapeFlatSBA
	ShapeFlatHTTP
	ShapeFlatMock
)

// String renders Shape for log lines; the label is load-bearing in the
// `operator_id=... old_shape=... new_shape=nested` up-convert log line
// per dispatch Task 1 body.
func (s Shape) String() string {
	switch s {
	case ShapeNested:
		return "nested"
	case ShapeFlatRadius:
		return "flat_radius"
	case ShapeFlatDiameter:
		return "flat_diameter"
	case ShapeFlatSBA:
		return "flat_sba"
	case ShapeFlatHTTP:
		return "flat_http"
	case ShapeFlatMock:
		return "flat_mock"
	default:
		return "invalid"
	}
}

// ProtocolConfig is the per-protocol sub-object inside NestedConfig.
// Fields are intentionally permissive JSON-level (json.RawMessage) —
// each adapter factory unmarshals its own fields from the sub-blob,
// so adapterschema deliberately avoids mirroring every protocol's
// struct. Validate() checks the minimum surface: Enabled is a bool,
// and for enabled=true sub-objects, a small set of "at least one of"
// required fields per protocol (per plan §Screen Mockups).
type ProtocolConfig struct {
	Enabled bool            `json:"enabled"`
	Raw     json.RawMessage `json:"-"`
}

// NestedConfig is the full plaintext nested adapter_config. Fields are
// pointer-typed so `omitempty` behaves correctly: a protocol absent
// from the JSON is nil, distinct from a protocol present-but-disabled.
type NestedConfig struct {
	Radius   *ProtocolConfig `json:"radius,omitempty"`
	Diameter *ProtocolConfig `json:"diameter,omitempty"`
	SBA      *ProtocolConfig `json:"sba,omitempty"`
	HTTP     *ProtocolConfig `json:"http,omitempty"`
	Mock     *ProtocolConfig `json:"mock,omitempty"`
}

// IsValidProtocol reports whether name is in the post-090 protocol
// allowlist (mock|radius|diameter|sba|http). Used by future PATCH
// endpoints that accept a protocol URL parameter. See plan Task 1
// §point 6 for why this is a separate surface from the handler's
// legacy validAdapterTypes map.
func IsValidProtocol(name string) bool {
	_, ok := validProtocolSet[name]
	return ok
}

// CanonicalProtocolOrder returns the canonical order slice. The
// returned slice is a defensive copy — mutate freely.
func CanonicalProtocolOrder() []string {
	out := make([]string, len(canonicalProtocolOrder))
	copy(out, canonicalProtocolOrder)
	return out
}
