package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// TestOperatorHealthBroadcast_StatusFlipRelayed verifies that publishing an
// argus.events.operator.health NATS payload (prev=healthy, curr=degraded)
// causes the hub to relay an operator.health_changed WS event to all connected
// clients containing the correct operator_id, current_status, and latency_ms.
func TestOperatorHealthBroadcast_StatusFlipRelayed(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	conn := &Connection{
		TenantID: uuid.New(),
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 16),
	}
	hub.Register(conn)

	operatorID := uuid.New()
	payload := map[string]interface{}{
		"operator_id":           operatorID.String(),
		"operator_name":         "TestNet",
		"previous_status":       "healthy",
		"current_status":        "degraded",
		"circuit_breaker_state": "open",
		"latency_ms":            850,
		"timestamp":             time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	hub.relayNATSEvent("argus.events.operator.health", data)

	select {
	case msg := <-conn.SendCh:
		var env EventEnvelope
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatalf("unmarshal envelope: %v", err)
		}
		if env.Type != "operator.health_changed" {
			t.Errorf("type = %q, want %q", env.Type, "operator.health_changed")
		}

		inner, ok := env.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("Data is not map[string]interface{}: %T", env.Data)
		}
		if got := inner["operator_id"]; got != operatorID.String() {
			t.Errorf("operator_id = %v, want %v", got, operatorID.String())
		}
		if got := inner["current_status"]; got != "degraded" {
			t.Errorf("current_status = %v, want degraded", got)
		}
		if got := inner["previous_status"]; got != "healthy" {
			t.Errorf("previous_status = %v, want healthy", got)
		}
		if got, ok := inner["latency_ms"].(float64); !ok || int(got) != 850 {
			t.Errorf("latency_ms = %v, want 850", inner["latency_ms"])
		}

	case <-time.After(2 * time.Second):
		t.Fatal("timeout: operator.health_changed event not received within 2s")
	}
}

// TestOperatorHealthBroadcast_LatencyOnlyRelayed verifies that a health event
// with an unchanged status but a different latency_ms is still relayed to WS
// clients (the hub does not suppress events that lack a status transition).
func TestOperatorHealthBroadcast_LatencyOnlyRelayed(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	conn := &Connection{
		TenantID: uuid.New(),
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 16),
	}
	hub.Register(conn)

	operatorID := uuid.New()
	payload := map[string]interface{}{
		"operator_id":           operatorID.String(),
		"previous_status":       "healthy",
		"current_status":        "healthy",
		"circuit_breaker_state": "closed",
		"latency_ms":            1200,
		"timestamp":             time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	hub.relayNATSEvent("argus.events.operator.health", data)

	select {
	case msg := <-conn.SendCh:
		var env EventEnvelope
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatalf("unmarshal envelope: %v", err)
		}
		if env.Type != "operator.health_changed" {
			t.Errorf("type = %q, want %q", env.Type, "operator.health_changed")
		}

		inner, ok := env.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("Data is not map[string]interface{}: %T", env.Data)
		}
		if got, ok := inner["latency_ms"].(float64); !ok || int(got) != 1200 {
			t.Errorf("latency_ms = %v, want 1200", inner["latency_ms"])
		}
		if got := inner["current_status"]; got != "healthy" {
			t.Errorf("current_status = %v, want healthy", got)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("timeout: operator.health_changed event not received within 2s")
	}
}
