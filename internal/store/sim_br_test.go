package store

import (
	"testing"
)

func TestBR1_AllValidTransitionsFromOrdered(t *testing.T) {
	allowed := validTransitions["ordered"]
	if len(allowed) != 1 {
		t.Fatalf("ordered should have exactly 1 valid transition, got %d", len(allowed))
	}
	if allowed[0] != "active" {
		t.Errorf("ordered should only transition to active, got %q", allowed[0])
	}
}

func TestBR1_AllValidTransitionsFromActive(t *testing.T) {
	allowed := validTransitions["active"]
	expected := map[string]bool{"suspended": true, "stolen_lost": true, "terminated": true}
	if len(allowed) != len(expected) {
		t.Fatalf("active should have %d valid transitions, got %d", len(expected), len(allowed))
	}
	for _, s := range allowed {
		if !expected[s] {
			t.Errorf("unexpected transition from active to %q", s)
		}
	}
}

func TestBR1_AllValidTransitionsFromSuspended(t *testing.T) {
	allowed := validTransitions["suspended"]
	expected := map[string]bool{"active": true, "terminated": true}
	if len(allowed) != len(expected) {
		t.Fatalf("suspended should have %d valid transitions, got %d", len(expected), len(allowed))
	}
	for _, s := range allowed {
		if !expected[s] {
			t.Errorf("unexpected transition from suspended to %q", s)
		}
	}
}

func TestBR1_StolenLostCanOnlyTerminate(t *testing.T) {
	allowed := validTransitions["stolen_lost"]
	if len(allowed) != 1 {
		t.Fatalf("stolen_lost should have exactly 1 valid transition (per BR-1), got %d: %v", len(allowed), allowed)
	}
	if allowed[0] != "terminated" {
		t.Errorf("stolen_lost should only transition to terminated, got %q", allowed[0])
	}
}

func TestBR1_TerminatedCanOnlyPurge(t *testing.T) {
	allowed := validTransitions["terminated"]
	if len(allowed) != 1 {
		t.Fatalf("terminated should have exactly 1 valid transition, got %d", len(allowed))
	}
	if allowed[0] != "purged" {
		t.Errorf("terminated should only transition to purged, got %q", allowed[0])
	}
}

func TestBR1_PurgedIsFinalState(t *testing.T) {
	allowed := validTransitions["purged"]
	if len(allowed) != 0 {
		t.Errorf("purged should have 0 transitions (final state), got %d: %v", len(allowed), allowed)
	}
}

func TestBR1_CannotSkipStatesToPurge(t *testing.T) {
	directToPurge := []string{"ordered", "active", "suspended", "stolen_lost"}
	for _, state := range directToPurge {
		t.Run(state+"->purged", func(t *testing.T) {
			err := validateTransition(state, "purged")
			if err == nil {
				t.Errorf("should not be able to transition directly from %s to purged", state)
			}
			if err != ErrInvalidStateTransition {
				t.Errorf("expected ErrInvalidStateTransition, got %v", err)
			}
		})
	}
}

func TestBR1_CannotGoBackToOrdered(t *testing.T) {
	states := []string{"active", "suspended", "stolen_lost", "terminated", "purged"}
	for _, state := range states {
		t.Run(state+"->ordered", func(t *testing.T) {
			err := validateTransition(state, "ordered")
			if err == nil {
				t.Errorf("should not be able to transition from %s back to ordered", state)
			}
		})
	}
}

func TestBR1_SuspendedCanReactivate(t *testing.T) {
	err := validateTransition("suspended", "active")
	if err != nil {
		t.Errorf("suspended->active should be valid (resume): %v", err)
	}
}

func TestBR1_ActiveToStolenLost(t *testing.T) {
	err := validateTransition("active", "stolen_lost")
	if err != nil {
		t.Errorf("active->stolen_lost should be valid: %v", err)
	}
}

func TestBR1_SuspendedCannotGoToStolenLost(t *testing.T) {
	err := validateTransition("suspended", "stolen_lost")
	if err == nil {
		t.Error("suspended->stolen_lost should be invalid per BR-1 rules")
	}
}

func TestBR1_StolenLostCanTerminate(t *testing.T) {
	err := validateTransition("stolen_lost", "terminated")
	if err != nil {
		t.Errorf("stolen_lost->terminated should be valid per BR-1 (manual terminate after investigation): %v", err)
	}
}

func TestBR1_AllStatesExistInTransitionMap(t *testing.T) {
	allStates := []string{"ordered", "active", "suspended", "stolen_lost", "terminated", "purged"}
	for _, state := range allStates {
		if _, ok := validTransitions[state]; !ok {
			t.Errorf("state %q not in validTransitions map", state)
		}
	}
}

func TestBR1_ExhaustiveInvalidTransitions(t *testing.T) {
	allStates := []string{"ordered", "active", "suspended", "stolen_lost", "terminated", "purged"}

	for _, from := range allStates {
		allowed := make(map[string]bool)
		for _, to := range validTransitions[from] {
			allowed[to] = true
		}
		for _, to := range allStates {
			if from == to {
				continue
			}
			if allowed[to] {
				continue
			}
			t.Run(from+"->"+to+"_invalid", func(t *testing.T) {
				err := validateTransition(from, to)
				if err == nil {
					t.Errorf("expected %s->%s to be invalid", from, to)
				}
			})
		}
	}
}

func TestBR2_APNErrorSentinels_Distinct(t *testing.T) {
	if ErrAPNNotFound == ErrAPNHasActiveSIMs {
		t.Error("ErrAPNNotFound and ErrAPNHasActiveSIMs must be distinct")
	}
	if ErrAPNNotFound == ErrAPNNameExists {
		t.Error("ErrAPNNotFound and ErrAPNNameExists must be distinct")
	}
	if ErrAPNHasActiveSIMs == ErrAPNNameExists {
		t.Error("ErrAPNHasActiveSIMs and ErrAPNNameExists must be distinct")
	}
}

func TestBR2_APNHasActiveSIMsErrorMessage(t *testing.T) {
	expected := "store: apn has active sims"
	if ErrAPNHasActiveSIMs.Error() != expected {
		t.Errorf("ErrAPNHasActiveSIMs.Error() = %q, want %q", ErrAPNHasActiveSIMs.Error(), expected)
	}
}

func TestBR3_IPPoolErrorSentinels_Distinct(t *testing.T) {
	if ErrPoolExhausted == ErrIPPoolNotFound {
		t.Error("ErrPoolExhausted and ErrIPPoolNotFound must be distinct")
	}
	if ErrIPAlreadyAllocated == ErrIPPoolNotFound {
		t.Error("ErrIPAlreadyAllocated and ErrIPPoolNotFound must be distinct")
	}
	if ErrIPNotFound == ErrIPPoolNotFound {
		t.Error("ErrIPNotFound and ErrIPPoolNotFound must be distinct")
	}
}

func TestBR3_PoolExhaustedErrorMessage(t *testing.T) {
	expected := "store: ip pool exhausted"
	if ErrPoolExhausted.Error() != expected {
		t.Errorf("ErrPoolExhausted.Error() = %q, want %q", ErrPoolExhausted.Error(), expected)
	}
}

func TestSIMErrorSentinels_Distinct(t *testing.T) {
	if ErrSIMNotFound == ErrICCIDExists {
		t.Error("ErrSIMNotFound and ErrICCIDExists must be distinct")
	}
	if ErrSIMNotFound == ErrIMSIExists {
		t.Error("ErrSIMNotFound and ErrIMSIExists must be distinct")
	}
	if ErrICCIDExists == ErrIMSIExists {
		t.Error("ErrICCIDExists and ErrIMSIExists must be distinct")
	}
	if ErrSIMNotFound == ErrInvalidStateTransition {
		t.Error("ErrSIMNotFound and ErrInvalidStateTransition must be distinct")
	}
}
