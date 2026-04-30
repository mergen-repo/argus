package bus

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/severity"
	"github.com/google/uuid"
)

const testTenantID = "00000000-0000-0000-0000-000000000001"

func TestEnvelope_NewEnvelope_SetsVersionIDTimestamp(t *testing.T) {
	before := time.Now().UTC()
	env := NewEnvelope("session.started", testTenantID, severity.Info)
	after := time.Now().UTC()

	if env.EventVersion != CurrentEventVersion {
		t.Fatalf("EventVersion = %d, want %d", env.EventVersion, CurrentEventVersion)
	}
	if env.ID == "" {
		t.Fatal("ID should be non-empty")
	}
	if _, err := uuid.Parse(env.ID); err != nil {
		t.Fatalf("ID is not a valid UUID: %v", err)
	}
	if env.Type != "session.started" {
		t.Fatalf("Type = %q, want session.started", env.Type)
	}
	if env.TenantID != testTenantID {
		t.Fatalf("TenantID mismatch: got %q", env.TenantID)
	}
	if env.Severity != severity.Info {
		t.Fatalf("Severity = %q, want info", env.Severity)
	}
	if env.Timestamp.Before(before) || env.Timestamp.After(after) {
		t.Fatalf("Timestamp %v not within [%v, %v]", env.Timestamp, before, after)
	}
	if env.Meta == nil {
		t.Fatal("Meta should be initialized to empty map, not nil")
	}
}

func TestEnvelope_Validate_AcceptsValid(t *testing.T) {
	env := NewEnvelope("session.started", testTenantID, severity.Info)
	env.Source = "aaa"
	env.Title = "Session started"
	if err := env.Validate(); err != nil {
		t.Fatalf("Validate should pass for complete envelope: %v", err)
	}
}

func TestEnvelope_Validate_RejectsMissingFields(t *testing.T) {
	build := func() *Envelope {
		env := NewEnvelope("t", testTenantID, severity.Info)
		env.Source = "system"
		env.Title = "x"
		return env
	}

	cases := []struct {
		name    string
		mutate  func(e *Envelope)
		wantErr error
	}{
		{"missing id", func(e *Envelope) { e.ID = "" }, ErrMissingField},
		{"missing type", func(e *Envelope) { e.Type = "" }, ErrMissingField},
		{"zero timestamp", func(e *Envelope) { e.Timestamp = time.Time{} }, ErrMissingField},
		{"missing tenant_id", func(e *Envelope) { e.TenantID = "" }, ErrMissingField},
		{"missing severity", func(e *Envelope) { e.Severity = "" }, ErrMissingField},
		{"missing source", func(e *Envelope) { e.Source = "" }, ErrMissingField},
		{"missing title", func(e *Envelope) { e.Title = "" }, ErrMissingField},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := build()
			tc.mutate(env)
			err := env.Validate()
			if err == nil {
				t.Fatal("expected Validate to fail")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected errors.Is(err, %v), got %v", tc.wantErr, err)
			}
		})
	}
}

func TestEnvelope_Validate_RejectsInvalidSeverity(t *testing.T) {
	env := NewEnvelope("t", testTenantID, "catastrophic")
	env.Source = "sys"
	env.Title = "x"
	err := env.Validate()
	if !errors.Is(err, ErrInvalidSeverity) {
		t.Fatalf("expected ErrInvalidSeverity, got %v", err)
	}
}

func TestEnvelope_Validate_RejectsInvalidTenantUUID(t *testing.T) {
	env := NewEnvelope("t", "not-a-uuid", severity.Info)
	env.Source = "sys"
	env.Title = "x"
	err := env.Validate()
	if !errors.Is(err, ErrInvalidTenant) {
		t.Fatalf("expected ErrInvalidTenant, got %v", err)
	}
}

func TestEnvelope_Validate_AcceptsLegacyVersionAsErrLegacyShape(t *testing.T) {
	env := NewEnvelope("t", testTenantID, severity.Info)
	env.Source = "sys"
	env.Title = "x"
	env.EventVersion = 0
	err := env.Validate()
	if !errors.Is(err, ErrLegacyShape) {
		t.Fatalf("expected ErrLegacyShape for version 0, got %v", err)
	}
}

func TestEnvelope_Validate_RejectsInvalidEntity(t *testing.T) {
	env := NewEnvelope("t", testTenantID, severity.Info)
	env.Source = "sys"
	env.Title = "x"
	env.Entity = &EntityRef{Type: "", ID: "abc"}
	if err := env.Validate(); !errors.Is(err, ErrInvalidEntity) {
		t.Fatalf("expected ErrInvalidEntity, got %v", err)
	}
	env.Entity = &EntityRef{Type: "sim", ID: ""}
	if err := env.Validate(); !errors.Is(err, ErrInvalidEntity) {
		t.Fatalf("expected ErrInvalidEntity for empty id, got %v", err)
	}
}

func TestEnvelope_Validate_RejectsOversizedDedupKey(t *testing.T) {
	env := NewEnvelope("t", testTenantID, severity.Info)
	env.Source = "sys"
	env.Title = "x"
	oversized := strings.Repeat("x", MaxDedupKeyLength+1)
	env.DedupKey = &oversized
	if err := env.Validate(); !errors.Is(err, ErrDedupKeyTooLong) {
		t.Fatalf("expected ErrDedupKeyTooLong, got %v", err)
	}
}

func TestEnvelope_RoundTrip_JSONMarshalUnmarshal(t *testing.T) {
	env := NewEnvelope("session.started", testTenantID, severity.Info)
	env.Source = "aaa"
	env.Title = "Session started"
	env.Message = "RADIUS session established"
	env.SetEntity("sim", "550e8400-e29b-41d4-a716-446655440000", "ICCID 8990")
	env.WithMeta("operator_id", "11111111-1111-1111-1111-111111111111")
	env.WithMeta("framed_ip", "10.20.30.40")
	dk := "dedup-key-123"
	env.DedupKey = &dk

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	body := string(data)
	for _, key := range []string{"event_version", "tenant_id", "display_name", "dedup_key"} {
		if !strings.Contains(body, `"`+key+`":`) {
			t.Fatalf("snake_case tag %q missing in JSON: %s", key, body)
		}
	}

	var back Envelope
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if back.EventVersion != env.EventVersion {
		t.Fatalf("EventVersion mismatch")
	}
	if back.Entity == nil || back.Entity.ID != env.Entity.ID {
		t.Fatalf("Entity roundtrip failed")
	}
	if back.Entity.DisplayName != "ICCID 8990" {
		t.Fatalf("display_name roundtrip failed: %q", back.Entity.DisplayName)
	}
	if back.DedupKey == nil || *back.DedupKey != dk {
		t.Fatalf("DedupKey roundtrip failed")
	}
	if back.Meta["framed_ip"] != "10.20.30.40" {
		t.Fatalf("Meta roundtrip failed: %+v", back.Meta)
	}
}

func TestEnvelope_RoundTrip_NilEntityOmitted(t *testing.T) {
	env := NewEnvelope("notification.dispatch", testTenantID, severity.Info)
	env.Source = "notification"
	env.Title = "x"
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"entity"`) {
		t.Fatalf("nil entity should be omitted, got: %s", data)
	}
}

func TestEnvelope_SetEntity_Chainable(t *testing.T) {
	env := NewEnvelope("t", testTenantID, severity.Info).
		SetEntity("sim", "550e8400-e29b-41d4-a716-446655440000", "ICCID 89").
		WithMeta("k", "v").
		WithSource("aaa").
		WithTitle("x").
		WithMessage("m")
	if env.Entity == nil || env.Entity.Type != "sim" {
		t.Fatal("SetEntity failed")
	}
	if env.Meta["k"] != "v" {
		t.Fatal("WithMeta failed")
	}
	if env.Source != "aaa" || env.Title != "x" || env.Message != "m" {
		t.Fatal("builder chain failed")
	}
}

func TestEnvelope_WithDedupKey_EmptyClears(t *testing.T) {
	env := NewEnvelope("t", testTenantID, severity.Info).WithDedupKey("xx")
	if env.DedupKey == nil || *env.DedupKey != "xx" {
		t.Fatal("WithDedupKey(non-empty) failed")
	}
	env.WithDedupKey("")
	if env.DedupKey != nil {
		t.Fatal("WithDedupKey(\"\") should clear")
	}
}
