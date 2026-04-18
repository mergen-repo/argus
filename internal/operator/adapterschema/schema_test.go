package adapterschema

import (
	"testing"
)

func TestIsValidProtocol(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"mock", true},
		{"radius", true},
		{"diameter", true},
		{"sba", true},
		{"http", true},
		{"", false},
		{"unknown", false},
		{"RADIUS", false},
	}
	for _, tc := range cases {
		if got := IsValidProtocol(tc.in); got != tc.want {
			t.Errorf("IsValidProtocol(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestCanonicalProtocolOrder_IsDefensiveCopy(t *testing.T) {
	a := CanonicalProtocolOrder()
	a[0] = "mutated"
	b := CanonicalProtocolOrder()
	if b[0] == "mutated" {
		t.Fatal("CanonicalProtocolOrder returned a shared slice; mutation leaked")
	}
}

// Case (a) — both-protocols-enabled (nested, valid).
func TestValidate_BothProtocolsEnabled(t *testing.T) {
	raw := []byte(`{
		"radius":{"enabled":true,"shared_secret":"s"},
		"diameter":{"enabled":true,"origin_host":"h"}
	}`)
	n, err := ParseNested(raw)
	if err != nil {
		t.Fatalf("ParseNested: %v", err)
	}
	if err := Validate(n); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	enabled := DeriveEnabledProtocols(n)
	if len(enabled) != 2 || enabled[0] != "diameter" || enabled[1] != "radius" {
		t.Fatalf("DeriveEnabledProtocols = %v, want [diameter radius]", enabled)
	}
}

// Case (b) — all-disabled (nested, all enabled:false). Must be
// accepted at the schema layer per advisor watch-out #1 (b): zero
// adapters → operator visible but non-routable.
func TestValidate_AllDisabledIsAllowed(t *testing.T) {
	raw := []byte(`{
		"radius":{"enabled":false},
		"diameter":{"enabled":false},
		"sba":{"enabled":false}
	}`)
	n, err := ParseNested(raw)
	if err != nil {
		t.Fatalf("ParseNested: %v", err)
	}
	if err := Validate(n); err != nil {
		t.Fatalf("Validate should accept zero-enabled nested: %v", err)
	}
	if got := DeriveEnabledProtocols(n); len(got) != 0 {
		t.Fatalf("DeriveEnabledProtocols = %v, want []", got)
	}
	if got := DerivePrimaryProtocol(n); got != "" {
		t.Fatalf("DerivePrimaryProtocol = %q, want \"\"", got)
	}
}

// Case (c) — malformed JSON (garbage bytes). DetectShape MUST return
// ErrShapeInvalidJSON so callers don't silently fall through.
func TestDetectShape_GarbageJSON(t *testing.T) {
	inputs := [][]byte{
		[]byte(`{not json`),
		[]byte(`}}}garbage`),
		[]byte(`[1,2,3]`), // top-level array is not a valid operator config
		[]byte(`null`),
		[]byte(``),
	}
	for _, in := range inputs {
		shape, err := DetectShape(in)
		if shape != ShapeInvalid {
			t.Errorf("DetectShape(%q) shape = %v, want ShapeInvalid", in, shape)
		}
		if err == nil {
			t.Errorf("DetectShape(%q) returned nil error, want ErrShapeInvalidJSON", in)
		}
	}
}

// Case (d) — decrypt-returned-non-JSON (the silent-failure demo).
// The crypto layer falls through silently when the ciphertext cannot
// be unwrapped, so the detector is the chokepoint that MUST surface
// a TYPED error. Dispatch-named regression test.
func TestDetectShape_DecryptedNonJSON_TypedError(t *testing.T) {
	// Simulated post-decrypt corrupted blob. Bytes look plausible but
	// are not valid JSON at all (mimics key-rotation / corruption).
	corrupted := []byte("this-is-not-json-at-all\x00\x01garbage")
	shape, err := DetectShape(corrupted)
	if shape != ShapeInvalid {
		t.Fatalf("shape = %v, want ShapeInvalid", shape)
	}
	if err != ErrShapeInvalidJSON {
		t.Fatalf("err = %v, want ErrShapeInvalidJSON (typed); strings MUST NOT be matched", err)
	}

	// UpConvert must also propagate the same typed error — it's the
	// public entry point callers will hit first.
	_, upErr := UpConvert(corrupted, "radius")
	if upErr != ErrShapeInvalidJSON {
		t.Fatalf("UpConvert err = %v, want ErrShapeInvalidJSON", upErr)
	}
}

// Case (e) — legacy flat shape (pre-D1-A). Each legacy protocol must
// be classified by its heuristic key WITHOUT needing a hint.
func TestDetectShape_LegacyFlatShapes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Shape
	}{
		{"radius by shared_secret", `{"shared_secret":"x","listen_addr":":1812"}`, ShapeFlatRadius},
		{"radius by listen_addr alone", `{"listen_addr":":1812"}`, ShapeFlatRadius},
		{"diameter by origin_host", `{"origin_host":"h","origin_realm":"r"}`, ShapeFlatDiameter},
		{"sba by nrf_url", `{"nrf_url":"https://nrf"}`, ShapeFlatSBA},
		{"http by base_url", `{"base_url":"https://api","auth_type":"bearer"}`, ShapeFlatHTTP},
		{"mock by latency_ms", `{"latency_ms":12,"simulated_imsi_count":1000}`, ShapeFlatMock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shape, err := DetectShape([]byte(tc.raw))
			if err != nil {
				t.Fatalf("DetectShape err = %v, want nil", err)
			}
			if shape != tc.want {
				t.Fatalf("shape = %v, want %v", shape, tc.want)
			}
		})
	}
}

// Case (f) — already-nested after upconvert (re-read path). Calling
// UpConvert twice must be idempotent.
func TestUpConvert_NestedIdempotent(t *testing.T) {
	raw := []byte(`{"radius":{"enabled":true,"shared_secret":"s"}}`)
	first, err := UpConvert(raw, "")
	if err != nil {
		t.Fatalf("first UpConvert: %v", err)
	}
	reRaw, err := MarshalNested(first)
	if err != nil {
		t.Fatalf("MarshalNested: %v", err)
	}
	second, err := UpConvert(reRaw, "")
	if err != nil {
		t.Fatalf("second UpConvert: %v", err)
	}
	if first.Radius == nil || second.Radius == nil {
		t.Fatal("radius sub lost across round-trip")
	}
	if !first.Radius.Enabled || !second.Radius.Enabled {
		t.Fatal("enabled flag lost across round-trip")
	}
}
