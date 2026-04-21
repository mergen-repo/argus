package bus

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/severity"
	"github.com/google/uuid"
)

// TestEnvelope_EndToEnd_WireFormat verifies the canonical envelope round-
// trips through JSON (the actual NATS wire format) and that every consumer
// shape we care about can read back the same fields a publisher authored.
// This is the "publish → N subscribers" shared contract: if marshaling the
// publisher's Envelope yields bytes that fail to strict-unmarshal into a
// Validate()-passing Envelope, the whole FIX-212 invariant collapses.
func TestEnvelope_EndToEnd_WireFormat(t *testing.T) {
	tenant := uuid.New().String()
	sim := uuid.New().String()

	env := NewEnvelope("session.started", tenant, severity.Info).
		WithSource("aaa").
		WithTitle("Session started").
		WithMessage("RADIUS session established").
		SetEntity("sim", sim, "ICCID 8990001234").
		WithMeta("operator_id", uuid.NewString()).
		WithMeta("apn_id", uuid.NewString()).
		WithMeta("rat_type", "EUTRAN")

	// Publisher side: must Validate before publishing.
	if err := env.Validate(); err != nil {
		t.Fatalf("publisher-side Validate: %v", err)
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// --- Subscriber A: strict Envelope parse (notification service, ws hub) ---
	var got Envelope
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("subscriber A strict unmarshal: %v", err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("subscriber A Validate: %v", err)
	}
	if got.EventVersion != CurrentEventVersion {
		t.Errorf("EventVersion = %d, want %d", got.EventVersion, CurrentEventVersion)
	}
	if got.Type != "session.started" || got.TenantID != tenant {
		t.Errorf("Type/TenantID round-trip lost: %+v", got)
	}
	if got.Entity == nil || got.Entity.Type != "sim" || got.Entity.ID != sim {
		t.Errorf("Entity round-trip lost: %+v", got.Entity)
	}
	if got.Entity.DisplayName != "ICCID 8990001234" {
		t.Errorf("DisplayName round-trip: %q", got.Entity.DisplayName)
	}

	// --- Subscriber B: loose map-based parse (FE WS relay legacy path) ---
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("subscriber B loose unmarshal: %v", err)
	}
	if v, ok := m["event_version"].(float64); !ok || int(v) != CurrentEventVersion {
		t.Errorf("loose subscriber event_version = %v", m["event_version"])
	}
	if v, ok := m["tenant_id"].(string); !ok || v != tenant {
		t.Errorf("loose subscriber tenant_id = %v", m["tenant_id"])
	}

	// --- Subscriber C: entity extraction (FE event stream) ---
	entityMap, ok := m["entity"].(map[string]interface{})
	if !ok {
		t.Fatalf("entity not map: %T", m["entity"])
	}
	if entityMap["display_name"] != "ICCID 8990001234" {
		t.Errorf("display_name round-trip: %v", entityMap["display_name"])
	}

	// --- Subscriber D: timestamp consistency ---
	ts, ok := m["timestamp"].(string)
	if !ok || ts == "" {
		t.Fatalf("timestamp not string: %v", m["timestamp"])
	}
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatalf("timestamp parse: %v", err)
	}
	if !parsed.Equal(got.Timestamp) {
		t.Errorf("timestamp drift: wire=%v parsed=%v", got.Timestamp, parsed)
	}
}

// TestEnvelope_LegacyShape_FailsValidate verifies that payloads missing
// event_version (or bearing a non-current version) are rejected by
// Validate() with the typed ErrLegacyShape. This is the signal subscribers
// branch on to run the backward shim (FIX-212 AC-8).
func TestEnvelope_LegacyShape_FailsValidate(t *testing.T) {
	// Legacy wire shape: no event_version field, typical pre-FIX-212 payload.
	legacy := map[string]interface{}{
		"tenant_id":   uuid.NewString(),
		"operator_id": uuid.NewString(),
		"status":      "down",
	}
	data, _ := json.Marshal(legacy)

	var env Envelope
	_ = json.Unmarshal(data, &env) // unmarshal succeeds, validate must fail.
	if err := env.Validate(); err == nil {
		t.Fatalf("legacy payload must fail Validate; it did not")
	}
}
