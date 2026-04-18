package adapterschema

import (
	"errors"
	"testing"
)

func TestDetectShape_Nested_Valid(t *testing.T) {
	cases := []string{
		`{"radius":{"enabled":true,"shared_secret":"s"}}`,
		`{"mock":{"enabled":false}}`,
		`{"radius":{"enabled":true},"diameter":{"enabled":false}}`,
		`  {"radius":{"enabled":true}}  `, // leading whitespace tolerated
	}
	for _, raw := range cases {
		shape, err := DetectShape([]byte(raw))
		if err != nil {
			t.Errorf("DetectShape(%q) err = %v", raw, err)
			continue
		}
		if shape != ShapeNested {
			t.Errorf("DetectShape(%q) = %v, want ShapeNested", raw, shape)
		}
	}
}

func TestDetectShape_EmptyObject_Unknown(t *testing.T) {
	shape, err := DetectShape([]byte(`{}`))
	if shape != ShapeInvalid {
		t.Errorf("shape = %v, want ShapeInvalid", shape)
	}
	if !errors.Is(err, ErrShapeUnknown) {
		t.Errorf("err = %v, want ErrShapeUnknown", err)
	}
}

func TestDetectShape_EncryptedEnvelopeRejected(t *testing.T) {
	// A RawMessage that is a JSON STRING (starts with `"`) is the
	// encrypted envelope per crypto/aes.go. DetectShape MUST refuse —
	// callers must decrypt first.
	shape, err := DetectShape([]byte(`"U2FsdGVkX1+=="`))
	if shape != ShapeInvalid {
		t.Errorf("shape = %v, want ShapeInvalid for encrypted envelope", shape)
	}
	if err != ErrShapeInvalidJSON {
		t.Errorf("err = %v, want ErrShapeInvalidJSON", err)
	}
}

func TestDetectShape_UnknownShape(t *testing.T) {
	// Valid JSON object but no recognised protocol keys and no legacy
	// heuristic match.
	raw := []byte(`{"foo":"bar","baz":42}`)
	shape, err := DetectShape(raw)
	if shape != ShapeInvalid {
		t.Errorf("shape = %v, want ShapeInvalid", shape)
	}
	if !errors.Is(err, ErrShapeUnknown) {
		t.Errorf("err = %v, want ErrShapeUnknown", err)
	}
}

func TestDetectShape_NestedButSomeKeysInvalid_FailsAllProtocolKeyedCheck(t *testing.T) {
	// Mixing a valid protocol key with a random key must NOT classify
	// as nested — it forces the heuristic path, which then picks up
	// the radius flat-key because of `shared_secret`.
	raw := []byte(`{"radius":{"enabled":true},"shared_secret":"x"}`)
	shape, err := DetectShape(raw)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if shape != ShapeFlatRadius {
		t.Errorf("shape = %v, want ShapeFlatRadius (mixed keys falls to heuristic)", shape)
	}
}

func TestShape_String(t *testing.T) {
	cases := []struct {
		in   Shape
		want string
	}{
		{ShapeNested, "nested"},
		{ShapeFlatRadius, "flat_radius"},
		{ShapeFlatDiameter, "flat_diameter"},
		{ShapeFlatSBA, "flat_sba"},
		{ShapeFlatHTTP, "flat_http"},
		{ShapeFlatMock, "flat_mock"},
		{ShapeInvalid, "invalid"},
	}
	for _, tc := range cases {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("Shape(%d).String() = %q, want %q", tc.in, got, tc.want)
		}
	}
}
