package adapterschema

import (
	"encoding/json"
	"unicode"
)

// Heuristic key sets for legacy flat-shape detection. The sets are
// deliberately non-overlapping so a flat blob with any ONE key from a
// set confidently classifies the shape. When the caller has an
// explicit hint (the pre-090 `adapter_type` column value), the hint
// always wins over heuristics inside UpConvert.
var (
	flatRadiusKeys = map[string]struct{}{
		"shared_secret": {},
		"listen_addr":   {},
		"acct_port":     {},
	}
	flatDiameterKeys = map[string]struct{}{
		"origin_host":  {},
		"origin_realm": {},
		"peers":        {},
		"product_name": {},
	}
	flatSBAKeys = map[string]struct{}{
		"nrf_url":        {},
		"nf_instance_id": {},
	}
	flatHTTPKeys = map[string]struct{}{
		"base_url":  {},
		"auth_type": {},
		"auth_token": {},
	}
	flatMockKeys = map[string]struct{}{
		"latency_ms":           {},
		"simulated_imsi_count": {},
		"fail_rate":            {},
		"success_rate":         {},
		"healthy_after":        {},
		"error_type":           {},
		"timeout_ms":           {},
	}
)

// firstNonWSByte returns the first byte of raw that is not JSON
// whitespace (space / tab / \n / \r), or zero if there is none.
// Used as the primary discriminator between the encrypted-envelope
// JSON string (starts with `"`) and the plaintext object (starts
// with `{`) per plan §AES-GCM envelope. The detector itself only
// sees plaintext (callers decrypt first), so the first non-WS byte
// must be `{` — anything else is invalid JSON for our purposes.
func firstNonWSByte(raw []byte) byte {
	for _, b := range raw {
		if !unicode.IsSpace(rune(b)) {
			return b
		}
	}
	return 0
}

// DetectShape inspects plaintext raw bytes (post-decrypt) and returns
// the concrete shape. Returns ErrShapeInvalidJSON if the bytes are
// not parseable JSON at all (advisor watch-out #1 (d)). Returns
// ErrShapeUnknown if the JSON parses as an object but has no keys
// that match any known protocol heuristic AND no nested protocol
// sub-key — callers that still want a best-effort up-convert pass
// via UpConvert with an explicit hint.
//
// DetectShape does NOT call DecryptJSON — the caller is responsible
// for providing plaintext. This keeps the function pure and testable
// with no crypto key dependency.
func DetectShape(raw []byte) (Shape, error) {
	if len(raw) == 0 {
		return ShapeInvalid, ErrShapeInvalidJSON
	}
	if firstNonWSByte(raw) != '{' {
		// A post-decrypt, JSON-valid input MUST be an object (the
		// nested shape or any legacy flat shape is always an object).
		// A leading `"` indicates an encrypted envelope that was
		// never decrypted — callers must run DecryptJSON first. A
		// leading garbage byte is straight corruption.
		return ShapeInvalid, ErrShapeInvalidJSON
	}

	// Parse as a generic map so we can enumerate top-level keys.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return ShapeInvalid, ErrShapeInvalidJSON
	}

	// Nested shape: EVERY top-level key must be in the protocol set
	// AND at least one sub-object must look like a ProtocolConfig
	// (has "enabled" or is a valid JSON object). An empty object {}
	// is ambiguous — treat as ShapeUnknown.
	if len(top) == 0 {
		return ShapeInvalid, ErrShapeUnknown
	}
	allProtocolKeyed := true
	anyLookedLikeSub := false
	for k, v := range top {
		if !IsValidProtocol(k) {
			allProtocolKeyed = false
			break
		}
		// Sub-object must itself be a JSON object to count as nested.
		if firstNonWSByte(v) == '{' {
			anyLookedLikeSub = true
		}
	}
	if allProtocolKeyed && anyLookedLikeSub {
		return ShapeNested, nil
	}

	// Flat shape heuristics. Walk protocol-specific key sets in
	// priority order (radius before diameter before sba before http
	// before mock — chosen so distinctive fields win). A single
	// matching key is sufficient to classify.
	for key := range top {
		if _, ok := flatRadiusKeys[key]; ok {
			return ShapeFlatRadius, nil
		}
	}
	for key := range top {
		if _, ok := flatDiameterKeys[key]; ok {
			return ShapeFlatDiameter, nil
		}
	}
	for key := range top {
		if _, ok := flatSBAKeys[key]; ok {
			return ShapeFlatSBA, nil
		}
	}
	for key := range top {
		if _, ok := flatHTTPKeys[key]; ok {
			return ShapeFlatHTTP, nil
		}
	}
	for key := range top {
		if _, ok := flatMockKeys[key]; ok {
			return ShapeFlatMock, nil
		}
	}

	return ShapeInvalid, ErrShapeUnknown
}

// shapeToProtocol maps a flat shape to its implied protocol name.
// Returns "" for ShapeNested / ShapeInvalid.
func shapeToProtocol(s Shape) string {
	switch s {
	case ShapeFlatRadius:
		return "radius"
	case ShapeFlatDiameter:
		return "diameter"
	case ShapeFlatSBA:
		return "sba"
	case ShapeFlatHTTP:
		return "http"
	case ShapeFlatMock:
		return "mock"
	default:
		return ""
	}
}
