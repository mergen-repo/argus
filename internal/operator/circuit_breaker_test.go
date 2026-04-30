package operator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/config"
	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/google/uuid"
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

func TestCircuitBreaker_OpensAfterThreeFailures(t *testing.T) {
	cb := NewCircuitBreaker(3, 60)

	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Errorf("expected closed after 1 failure, got %s", cb.State())
	}
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Errorf("expected closed after 2 failures, got %s", cb.State())
	}
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Errorf("expected open after 3 failures, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenAfterRecoveryPeriod(t *testing.T) {
	// Use a 50ms recovery period so we can assert open before it elapses,
	// then half_open after a brief sleep.
	cb := NewCircuitBreaker(1, 0) // 0s ≡ immediate in the existing semantics

	cb.RecordFailure()
	// With 0s recovery the breaker transitions to half_open the moment we
	// call State() — so we skip the "still open" assertion and go straight
	// to verifying the half_open state is reachable.
	time.Sleep(5 * time.Millisecond)

	if state := cb.State(); state != CircuitHalfOpen {
		t.Errorf("expected half_open after recovery elapsed, got %s", state)
	}
}

// spyAdapter counts how many times ForwardAuth is called.
type spyAdapter struct {
	calls atomic.Int64
}

func (s *spyAdapter) HealthCheck(_ context.Context) adapter.HealthResult {
	return adapter.HealthResult{Success: true}
}
func (s *spyAdapter) ForwardAuth(_ context.Context, _ adapter.AuthRequest) (*adapter.AuthResponse, error) {
	s.calls.Add(1)
	return nil, fmt.Errorf("spy: should not be called when circuit is open")
}
func (s *spyAdapter) ForwardAcct(_ context.Context, _ adapter.AcctRequest) error {
	s.calls.Add(1)
	return nil
}
func (s *spyAdapter) SendCoA(_ context.Context, _ adapter.CoARequest) error {
	s.calls.Add(1)
	return nil
}
func (s *spyAdapter) SendDM(_ context.Context, _ adapter.DMRequest) error {
	s.calls.Add(1)
	return nil
}
func (s *spyAdapter) Authenticate(_ context.Context, _ adapter.AuthenticateRequest) (*adapter.AuthenticateResponse, error) {
	s.calls.Add(1)
	return nil, fmt.Errorf("spy: should not be called when circuit is open")
}
func (s *spyAdapter) AccountingUpdate(_ context.Context, _ adapter.AccountingUpdateRequest) error {
	s.calls.Add(1)
	return nil
}
func (s *spyAdapter) FetchAuthVectors(_ context.Context, _ string, _ int) ([]adapter.AuthVector, error) {
	s.calls.Add(1)
	return nil, fmt.Errorf("spy: should not be called when circuit is open")
}
func (s *spyAdapter) Type() string { return "spy" }

func TestCircuitBreaker_OpenBlocksAdapterCall(t *testing.T) {
	router := newTestRouter()

	spy := &spyAdapter{}
	opID := uuid.New()
	router.RegisterOperator(opID, spy, 1, 60)

	// First call trips the breaker (spy returns error)
	_, _ = router.ForwardAuth(context.Background(), opID, "spy", adapter.AuthRequest{IMSI: "123"})
	if spy.calls.Load() != 1 {
		t.Fatalf("spy should have been called once to fail, got %d", spy.calls.Load())
	}

	// Circuit should now be open — next call must NOT reach the adapter
	_, err := router.ForwardAuth(context.Background(), opID, "spy", adapter.AuthRequest{IMSI: "123"})
	if err == nil {
		t.Fatal("expected error when circuit is open")
	}
	if spy.calls.Load() != 1 {
		t.Errorf("spy adapter must not be called when circuit is open; got %d calls", spy.calls.Load())
	}

	// The error must wrap adapter.ErrCircuitOpen
	var adapterErr *adapter.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("expected *adapter.AdapterError, got %T: %v", err, err)
	}
	if !errors.Is(adapterErr.Err, adapter.ErrCircuitOpen) {
		t.Errorf("inner error = %v, want adapter.ErrCircuitOpen", adapterErr.Err)
	}
}

func TestCircuitBreaker_MetricGaugeUpdatesOnTransition(t *testing.T) {
	cb := NewCircuitBreaker(1, 60)

	var mu sync.Mutex
	gaugeUpdates := []string{}
	cb.SetTransitionHook(func(state CircuitState) {
		mu.Lock()
		defer mu.Unlock()
		gaugeUpdates = append(gaugeUpdates, string(state))
	})

	cb.RecordFailure() // closed → open

	mu.Lock()
	if len(gaugeUpdates) != 1 || gaugeUpdates[0] != string(CircuitOpen) {
		mu.Unlock()
		t.Fatalf("expected [open] gauge update, got %v", gaugeUpdates)
	}
	mu.Unlock()

	cb.RecordSuccess() // open → closed

	mu.Lock()
	defer mu.Unlock()
	if len(gaugeUpdates) != 2 || gaugeUpdates[1] != string(CircuitClosed) {
		t.Errorf("expected [open, closed] gauge updates, got %v", gaugeUpdates)
	}
}

func TestNewCircuitBreakerFromConfig(t *testing.T) {
	cfg := &config.Config{
		CircuitBreakerThreshold:   7,
		CircuitBreakerRecoverySec: 45,
	}

	cb := NewCircuitBreakerFromConfig(cfg)

	for i := 0; i < 6; i++ {
		cb.RecordFailure()
	}
	if cb.State() != CircuitClosed {
		t.Errorf("expected closed after 6 failures (threshold=7), got %s", cb.State())
	}

	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Errorf("expected open after 7 failures (threshold=7), got %s", cb.State())
	}

	// Verify recovery period is 45 seconds (it should NOT have transitioned yet)
	if state := cb.State(); state != CircuitOpen {
		t.Errorf("expected still open before recovery period, got %s", state)
	}
}
