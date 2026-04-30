package adapterschema

import (
	"encoding/json"
	"fmt"
)

// UpConvert takes a plaintext raw blob of ANY recognised shape and
// returns a fully-parsed NestedConfig. The hintAdapterType is the
// pre-090 `operators.adapter_type` column value (may be empty). Used
// to disambiguate flat shapes that don't carry distinctive heuristic
// keys (e.g. an empty mock config `{}` with no `latency_ms`).
//
// Contract:
//   - Nested input → parsed unchanged.
//   - Flat input with a known heuristic key → wrapped into
//     `{<protocol>: {enabled: true, ...flatBody}}`.
//   - Flat input whose protocol cannot be derived from heuristics
//     AND hintAdapterType is a valid protocol → wrapped under the
//     hint.
//   - Flat input with no heuristic match and no hint →
//     ErrUpConvertMissingHint.
//   - Non-JSON input → ErrShapeInvalidJSON (unchanged from DetectShape).
//
// UpConvert is idempotent: calling it twice on a nested blob returns
// an equivalent NestedConfig. Callers that need the raw JSON bytes for
// re-persist should call MarshalNested on the returned struct.
func UpConvert(raw []byte, hintAdapterType string) (NestedConfig, error) {
	shape, err := DetectShape(raw)
	if err != nil {
		// ErrShapeInvalidJSON is terminal — caller surfaces it as a
		// typed error per advisor watch-out #1 (d).
		if err == ErrShapeInvalidJSON {
			return NestedConfig{}, err
		}
		// ErrShapeUnknown means parse-succeeded-but-no-heuristic-hit.
		// Fall through to the hint branch: an empty `{}` flat mock
		// config with a valid hint should still up-convert. If the
		// hint is absent/invalid, surface ErrUpConvertMissingHint.
		if hintAdapterType != "" && IsValidProtocol(hintAdapterType) {
			return wrapFlatIntoNested(raw, hintAdapterType)
		}
		return NestedConfig{}, ErrUpConvertMissingHint
	}

	switch shape {
	case ShapeNested:
		return ParseNested(raw)
	case ShapeFlatRadius, ShapeFlatDiameter, ShapeFlatSBA, ShapeFlatHTTP, ShapeFlatMock:
		protocol := shapeToProtocol(shape)
		return wrapFlatIntoNested(raw, protocol)
	default:
		// Should be unreachable — DetectShape returns a typed error
		// for the fall-through branches. Defensive only.
		if hintAdapterType != "" && IsValidProtocol(hintAdapterType) {
			return wrapFlatIntoNested(raw, hintAdapterType)
		}
		return NestedConfig{}, ErrUpConvertMissingHint
	}
}

// wrapFlatIntoNested constructs a NestedConfig holding a single
// enabled protocol whose Raw is the flat body with `enabled:true`
// merged in. Merging preserves every original field so the adapter
// factory receives the full config on next GetOrCreate.
func wrapFlatIntoNested(flatRaw []byte, protocol string) (NestedConfig, error) {
	// Round-trip through a generic map so we can inject `enabled:true`
	// without clobbering any protocol-specific fields.
	var body map[string]json.RawMessage
	if err := json.Unmarshal(flatRaw, &body); err != nil {
		return NestedConfig{}, fmt.Errorf("%w: %v", ErrShapeInvalidJSON, err)
	}
	if body == nil {
		body = make(map[string]json.RawMessage)
	}
	// Always set enabled:true — a flat blob represents the "one
	// configured protocol" by definition of the pre-090 single-
	// protocol schema.
	body["enabled"] = json.RawMessage(`true`)
	merged, err := json.Marshal(body)
	if err != nil {
		return NestedConfig{}, fmt.Errorf("%w: re-marshal: %v", ErrShapeInvalidJSON, err)
	}
	sub, err := parseProtocolConfig(merged)
	if err != nil {
		return NestedConfig{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	out := NestedConfig{}
	switch protocol {
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
	default:
		return NestedConfig{}, fmt.Errorf("%w: unknown protocol %q", ErrValidation, protocol)
	}
	return out, nil
}

// MarshalNested serialises a NestedConfig back to JSON bytes suitable
// for re-persist. Protocols absent from the struct are omitted (the
// struct field is nil). For each present protocol, the full sub-blob
// is emitted as-is (including any protocol-specific fields).
func MarshalNested(n NestedConfig) (json.RawMessage, error) {
	out := make(map[string]json.RawMessage, 5)
	if n.Radius != nil {
		out["radius"] = n.Radius.Raw
	}
	if n.Diameter != nil {
		out["diameter"] = n.Diameter.Raw
	}
	if n.SBA != nil {
		out["sba"] = n.SBA.Raw
	}
	if n.HTTP != nil {
		out["http"] = n.HTTP.Raw
	}
	if n.Mock != nil {
		out["mock"] = n.Mock.Raw
	}
	return json.Marshal(out)
}
