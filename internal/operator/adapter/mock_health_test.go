package adapter

import (
	"context"
	"encoding/json"
	"testing"
)

// TestMockAdapter_HealthCheck_DefaultReturnsSuccess verifies the Mock
// adapter's HealthCheck remains trivially successful in its default
// configuration. Task 5 intentionally leaves the mock adapter's
// protocol-native behaviour alone (it was already correct for its
// purpose: simulated latency + pass-through).
func TestMockAdapter_HealthCheck_DefaultReturnsSuccess(t *testing.T) {
	a, err := NewMockAdapter(json.RawMessage(`{"latency_ms":1}`))
	if err != nil {
		t.Fatalf("new mock adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if !result.Success {
		t.Errorf("mock health check should succeed by default: %s", result.Error)
	}
}

// TestMockAdapter_HealthCheck_HealthyAfterFailsEarly verifies the
// healthy_after simulation still works — the first N probes fail to
// emulate an operator that takes a while to warm up.
func TestMockAdapter_HealthCheck_HealthyAfterFailsEarly(t *testing.T) {
	a, err := NewMockAdapter(json.RawMessage(`{"latency_ms":1,"healthy_after":3}`))
	if err != nil {
		t.Fatalf("new mock adapter: %v", err)
	}
	// Probes 1, 2, 3 should fail; 4+ should succeed.
	for i := 1; i <= 3; i++ {
		r := a.HealthCheck(context.Background())
		if r.Success {
			t.Errorf("probe %d should fail per healthy_after=3", i)
		}
	}
	r := a.HealthCheck(context.Background())
	if !r.Success {
		t.Errorf("probe 4 should succeed after healthy_after threshold")
	}
}
