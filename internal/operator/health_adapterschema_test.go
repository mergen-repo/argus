package operator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/crypto"
	"github.com/btopcu/argus/internal/operator/adapterschema"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// TestHealthChecker_NormalizeAdapterConfig_NestedPassThrough confirms
// an already-nested plaintext is returned unchanged, with no
// re-persist side effect.
func TestHealthChecker_NormalizeAdapterConfig_NestedPassThrough(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	op := store.Operator{
		ID:            uuid.New(),
		AdapterConfig: json.RawMessage(`{"mock":{"enabled":true,"latency_ms":10}}`),
	}
	out := hc.normalizeAdapterConfig(context.Background(), op)
	n, err := adapterschema.ParseNested(out)
	if err != nil {
		t.Fatalf("ParseNested: %v", err)
	}
	if n.Mock == nil || !n.Mock.Enabled {
		t.Fatal("mock sub lost / disabled")
	}
}

// TestHealthChecker_NormalizeAdapterConfig_LegacyFlatUpConverts feeds
// a pre-090 flat mock config and asserts the nested shape comes back.
// Per advisor: this proves HealthChecker never sees a flat blob in
// the same run that Task 1's up-convert path rewrites.
func TestHealthChecker_NormalizeAdapterConfig_LegacyFlatUpConverts(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	op := store.Operator{
		ID:            uuid.New(),
		AdapterConfig: json.RawMessage(`{"latency_ms":12,"simulated_imsi_count":1000}`),
	}
	out := hc.normalizeAdapterConfig(context.Background(), op)
	if !strings.Contains(string(out), `"mock"`) {
		t.Fatalf("expected nested mock key in output, got %s", string(out))
	}
	n, err := adapterschema.ParseNested(out)
	if err != nil {
		t.Fatalf("ParseNested: %v", err)
	}
	if n.Mock == nil || !n.Mock.Enabled {
		t.Fatal("legacy flat mock did not up-convert to enabled nested")
	}
}

// TestHealthChecker_NormalizeAdapterConfig_LegacyFlatRadiusUpConverts
// covers the RADIUS flat path — the distinctive heuristic key
// (shared_secret) is sufficient to classify without a hint.
func TestHealthChecker_NormalizeAdapterConfig_LegacyFlatRadiusUpConverts(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	op := store.Operator{
		ID:            uuid.New(),
		AdapterConfig: json.RawMessage(`{"shared_secret":"s","listen_addr":":1812"}`),
	}
	out := hc.normalizeAdapterConfig(context.Background(), op)
	n, err := adapterschema.ParseNested(out)
	if err != nil {
		t.Fatalf("ParseNested: %v", err)
	}
	if n.Radius == nil || !n.Radius.Enabled {
		t.Fatal("radius sub not enabled after up-convert")
	}
}

// TestHealthChecker_NormalizeAdapterConfig_GarbageReturnsRawSafely
// feeds a post-decrypt blob that is not valid JSON. The helper MUST
// return the raw bytes (not nil) so the probe still runs through the
// adapter factory's own JSON handling — silence is not an option but
// a panic-free fallback is the correct Wave 1 behaviour.
func TestHealthChecker_NormalizeAdapterConfig_GarbageReturnsRawSafely(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	op := store.Operator{
		ID:            uuid.New(),
		AdapterConfig: json.RawMessage(`not-json-at-all`),
	}
	out := hc.normalizeAdapterConfig(context.Background(), op)
	if len(out) == 0 {
		t.Fatal("helper returned empty bytes on garbage plaintext; expected raw fallback")
	}
}

// TestHealthChecker_NormalizeAdapterConfig_EncryptedNestedRoundTrip
// covers the full encrypted-envelope path. Build an encrypted nested
// config and assert the helper decrypts + passes through.
func TestHealthChecker_NormalizeAdapterConfig_EncryptedNestedRoundTrip(t *testing.T) {
	hexKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	hc := NewHealthChecker(nil, nil, nil, hexKey, zerolog.Nop())

	nested := json.RawMessage(`{"mock":{"enabled":true,"latency_ms":10}}`)
	enc, err := crypto.EncryptJSON(nested, hexKey)
	if err != nil {
		t.Fatalf("EncryptJSON: %v", err)
	}
	op := store.Operator{
		ID:            uuid.New(),
		AdapterConfig: enc,
	}
	out := hc.normalizeAdapterConfig(context.Background(), op)
	if !strings.Contains(string(out), `"mock"`) {
		t.Fatalf("expected decrypted nested output, got %s", string(out))
	}
}
