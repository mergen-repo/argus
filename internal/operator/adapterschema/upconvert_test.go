package adapterschema

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestUpConvert_FlatRadius_NoHint(t *testing.T) {
	raw := []byte(`{"shared_secret":"s3cr3t","listen_addr":":1812"}`)
	n, err := UpConvert(raw, "")
	if err != nil {
		t.Fatalf("UpConvert: %v", err)
	}
	if n.Radius == nil {
		t.Fatal("Radius sub is nil after up-convert")
	}
	if !n.Radius.Enabled {
		t.Fatal("Radius.Enabled should be true after up-convert (legacy flat implies enabled)")
	}
	// Sub Raw should carry the original fields + injected enabled:true.
	var subFields map[string]json.RawMessage
	if err := json.Unmarshal(n.Radius.Raw, &subFields); err != nil {
		t.Fatalf("unmarshal sub raw: %v", err)
	}
	if _, ok := subFields["shared_secret"]; !ok {
		t.Error("shared_secret dropped during up-convert")
	}
	if _, ok := subFields["enabled"]; !ok {
		t.Error("enabled not injected")
	}
}

func TestUpConvert_FlatMock_ByHintOnly(t *testing.T) {
	// An empty mock config with no heuristic key — must fall through
	// to the hint branch.
	raw := []byte(`{}`)
	_, err := UpConvert(raw, "mock")
	if err != nil {
		// Empty object triggers ErrShapeUnknown from DetectShape; but
		// UpConvert should use the hint to disambiguate.
		t.Fatalf("UpConvert with hint should succeed on empty flat mock: %v", err)
	}
}

func TestUpConvert_FlatDiameter_WithHint(t *testing.T) {
	raw := []byte(`{"origin_host":"h","origin_realm":"r","peers":["p1","p2"]}`)
	n, err := UpConvert(raw, "diameter")
	if err != nil {
		t.Fatalf("UpConvert: %v", err)
	}
	if n.Diameter == nil || !n.Diameter.Enabled {
		t.Fatal("Diameter sub missing/disabled after up-convert")
	}
}

func TestUpConvert_NestedInput_Preserved(t *testing.T) {
	raw := []byte(`{"radius":{"enabled":true,"shared_secret":"s"},"mock":{"enabled":false}}`)
	n, err := UpConvert(raw, "")
	if err != nil {
		t.Fatalf("UpConvert: %v", err)
	}
	if n.Radius == nil || !n.Radius.Enabled {
		t.Fatal("Radius should be enabled")
	}
	if n.Mock == nil || n.Mock.Enabled {
		t.Fatal("Mock should be present but disabled")
	}
}

func TestUpConvert_MissingHintOnAmbiguousEmpty(t *testing.T) {
	// Empty object + empty hint → ErrUpConvertMissingHint.
	_, err := UpConvert([]byte(`{}`), "")
	if !errors.Is(err, ErrUpConvertMissingHint) {
		t.Fatalf("err = %v, want ErrUpConvertMissingHint", err)
	}
}

func TestUpConvert_InvalidHintFallsThroughToMissingHint(t *testing.T) {
	_, err := UpConvert([]byte(`{}`), "nosuchproto")
	if !errors.Is(err, ErrUpConvertMissingHint) {
		t.Fatalf("err = %v, want ErrUpConvertMissingHint for invalid hint", err)
	}
}

func TestUpConvert_GarbageInput_TypedError(t *testing.T) {
	_, err := UpConvert([]byte(`not-json`), "radius")
	if !errors.Is(err, ErrShapeInvalidJSON) {
		t.Fatalf("err = %v, want ErrShapeInvalidJSON", err)
	}
}

func TestMarshalNested_RoundTrip(t *testing.T) {
	raw := []byte(`{"radius":{"enabled":true,"shared_secret":"s"},"mock":{"enabled":false,"latency_ms":10}}`)
	n, err := ParseNested(raw)
	if err != nil {
		t.Fatalf("ParseNested: %v", err)
	}
	if err := Validate(n); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	rtRaw, err := MarshalNested(n)
	if err != nil {
		t.Fatalf("MarshalNested: %v", err)
	}
	n2, err := ParseNested(rtRaw)
	if err != nil {
		t.Fatalf("ParseNested after round-trip: %v", err)
	}
	if n2.Radius == nil || !n2.Radius.Enabled {
		t.Fatal("radius lost in round-trip")
	}
	if n2.Mock == nil || n2.Mock.Enabled {
		t.Fatal("mock enabled flag flipped during round-trip")
	}
}

func TestSubConfigRaw_ReturnsSubBlob(t *testing.T) {
	raw := []byte(`{"radius":{"enabled":true,"shared_secret":"s"},"mock":{"enabled":false}}`)
	n, err := ParseNested(raw)
	if err != nil {
		t.Fatalf("ParseNested: %v", err)
	}
	sub := SubConfigRaw(n, "radius")
	if sub == nil {
		t.Fatal("SubConfigRaw(radius) returned nil")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(sub, &fields); err != nil {
		t.Fatalf("sub unmarshal: %v", err)
	}
	if _, ok := fields["shared_secret"]; !ok {
		t.Error("radius shared_secret missing from sub raw")
	}
	if SubConfigRaw(n, "sba") != nil {
		t.Error("SubConfigRaw(sba) should be nil when absent")
	}
}

func TestValidateRaw_RejectsUnknownProtocolKey(t *testing.T) {
	raw := []byte(`{"notaproto":{"enabled":true}}`)
	if err := ValidateRaw(raw); err == nil {
		t.Fatal("expected validation error on unknown protocol key")
	}
}

func TestValidateRaw_RejectsNonObjectInput(t *testing.T) {
	err := ValidateRaw([]byte(`[1,2,3]`))
	if !errors.Is(err, ErrShapeInvalidJSON) {
		t.Fatalf("err = %v, want ErrShapeInvalidJSON", err)
	}
}
