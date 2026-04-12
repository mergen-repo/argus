package operator

import (
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_TransitionHookOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 10)

	var mu sync.Mutex
	transitions := []CircuitState{}
	cb.SetTransitionHook(func(state CircuitState) {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, state)
	})

	cb.RecordFailure()
	// Below threshold — no transition
	mu.Lock()
	if len(transitions) != 0 {
		t.Errorf("expected 0 transitions after 1 failure, got %v", transitions)
	}
	mu.Unlock()

	cb.RecordFailure()
	// At threshold — closed → open
	mu.Lock()
	if len(transitions) != 1 || transitions[0] != CircuitOpen {
		t.Errorf("expected [open], got %v", transitions)
	}
	mu.Unlock()

	// Further failures should not re-fire for the same state.
	cb.RecordFailure()
	mu.Lock()
	if len(transitions) != 1 {
		t.Errorf("expected no new transition for repeated open, got %v", transitions)
	}
	mu.Unlock()
}

func TestCircuitBreaker_TransitionHookSuccessAfterOpen(t *testing.T) {
	cb := NewCircuitBreaker(1, 10)

	var mu sync.Mutex
	transitions := []CircuitState{}
	cb.SetTransitionHook(func(state CircuitState) {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, state)
	})

	cb.RecordFailure() // closed -> open
	cb.RecordSuccess() // open -> closed

	mu.Lock()
	defer mu.Unlock()
	if len(transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %v", transitions)
	}
	if transitions[0] != CircuitOpen {
		t.Errorf("expected first transition open, got %s", transitions[0])
	}
	if transitions[1] != CircuitClosed {
		t.Errorf("expected second transition closed, got %s", transitions[1])
	}
}

func TestCircuitBreaker_TransitionHookOpenToHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(1, 0) // 0s recovery for test speed

	var mu sync.Mutex
	transitions := []CircuitState{}
	cb.SetTransitionHook(func(state CircuitState) {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, state)
	})

	cb.RecordFailure() // closed -> open
	time.Sleep(5 * time.Millisecond)
	_ = cb.State() // should transition open -> half_open

	mu.Lock()
	defer mu.Unlock()
	if len(transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %v", transitions)
	}
	if transitions[1] != CircuitHalfOpen {
		t.Errorf("expected half_open transition, got %s", transitions[1])
	}
}

func TestCircuitBreaker_NilHookIsSafe(t *testing.T) {
	cb := NewCircuitBreaker(1, 10)
	cb.SetTransitionHook(nil)
	cb.RecordFailure() // must not panic
	if cb.State() != CircuitOpen {
		t.Errorf("expected open, got %s", cb.State())
	}
}

func TestCircuitBreaker_SuccessAtClosedDoesNotFire(t *testing.T) {
	cb := NewCircuitBreaker(3, 10)

	var mu sync.Mutex
	transitions := []CircuitState{}
	cb.SetTransitionHook(func(state CircuitState) {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, state)
	})

	cb.RecordSuccess() // already closed — no transition

	mu.Lock()
	defer mu.Unlock()
	if len(transitions) != 0 {
		t.Errorf("expected no transitions for closed->closed, got %v", transitions)
	}
}
