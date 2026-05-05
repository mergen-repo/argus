package operator

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/google/uuid"
)

// slowFailAdapter sleeps briefly then returns an error, simulating a slow
// downstream operator that never succeeds. The calls counter allows asserting
// that the adapter is NOT reached after the circuit opens.
type slowFailAdapter struct {
	calls   atomic.Int64
	delayMs int
}

func (s *slowFailAdapter) HealthCheck(_ context.Context) adapter.HealthResult {
	return adapter.HealthResult{Success: false, Error: "always fails"}
}

func (s *slowFailAdapter) ForwardAuth(_ context.Context, _ adapter.AuthRequest) (*adapter.AuthResponse, error) {
	s.calls.Add(1)
	time.Sleep(time.Duration(s.delayMs) * time.Millisecond)
	return nil, errors.New("OPERATOR_UNAVAILABLE")
}

func (s *slowFailAdapter) ForwardAcct(_ context.Context, _ adapter.AcctRequest) error {
	s.calls.Add(1)
	time.Sleep(time.Duration(s.delayMs) * time.Millisecond)
	return errors.New("OPERATOR_UNAVAILABLE")
}

func (s *slowFailAdapter) SendCoA(_ context.Context, _ adapter.CoARequest) error {
	s.calls.Add(1)
	return errors.New("OPERATOR_UNAVAILABLE")
}

func (s *slowFailAdapter) SendDM(_ context.Context, _ adapter.DMRequest) error {
	s.calls.Add(1)
	return errors.New("OPERATOR_UNAVAILABLE")
}

func (s *slowFailAdapter) Authenticate(_ context.Context, _ adapter.AuthenticateRequest) (*adapter.AuthenticateResponse, error) {
	s.calls.Add(1)
	return nil, errors.New("OPERATOR_UNAVAILABLE")
}

func (s *slowFailAdapter) AccountingUpdate(_ context.Context, _ adapter.AccountingUpdateRequest) error {
	s.calls.Add(1)
	return errors.New("OPERATOR_UNAVAILABLE")
}

func (s *slowFailAdapter) FetchAuthVectors(_ context.Context, _ string, _ int) ([]adapter.AuthVector, error) {
	s.calls.Add(1)
	return nil, errors.New("OPERATOR_UNAVAILABLE")
}

func (s *slowFailAdapter) Type() string { return "slow_fail" }

// TestCircuitBreaker_Integration_OpensAfter5Failures verifies:
//   - 5 calls to a slow (200ms) failing adapter trip the breaker (threshold=5)
//   - The 6th call returns OPERATOR_UNAVAILABLE in <50ms (breaker short-circuits)
//   - The underlying adapter is NOT invoked on the 6th call (spy counter stays at 5)
//
// Gate: skip in -short mode so `make test` (unit-only) stays fast.
func TestCircuitBreaker_Integration_OpensAfter5Failures(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in short mode")
	}
	t.Parallel()

	const (
		threshold   = 5
		delayMs     = 20 // kept small for test speed; enough to detect short-circuit
		recoverySec = 300
	)

	spy := &slowFailAdapter{delayMs: delayMs}
	router := newTestRouter()
	opID := uuid.New()
	router.RegisterOperator(opID, spy, threshold, recoverySec)

	// Fire exactly threshold calls to trip the breaker.
	for i := 0; i < threshold; i++ {
		_, _ = router.ForwardAuth(context.Background(), opID, "slow_fail", adapter.AuthRequest{IMSI: "286010000000001"})
	}

	if got := spy.calls.Load(); got != threshold {
		t.Fatalf("expected %d adapter calls before open, got %d", threshold, got)
	}

	cb := router.GetCircuitBreaker(opID, "slow_fail")
	if cb == nil {
		t.Fatal("circuit breaker not found for operator")
	}
	if cb.State() != CircuitOpen {
		t.Fatalf("expected circuit to be open after %d failures, got %s", threshold, cb.State())
	}

	// 6th call: measure round-trip time; adapter must NOT be reached.
	start := time.Now()
	_, err := router.ForwardAuth(context.Background(), opID, "slow_fail", adapter.AuthRequest{IMSI: "286010000000001"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when circuit is open, got nil")
	}

	// Adapter must not have been called again.
	if got := spy.calls.Load(); got != threshold {
		t.Errorf("adapter called after circuit open: got %d calls, want %d", got, threshold)
	}

	// Circuit-open path must be fast (< 50ms) — no slow adapter overhead.
	if elapsed >= 50*time.Millisecond {
		t.Errorf("6th call took %v, want <50ms (circuit open must not block)", elapsed)
	}

	// Error must be a wrapped AdapterError containing ErrCircuitOpen.
	var adapterErr *adapter.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("expected *adapter.AdapterError, got %T: %v", err, err)
	}
	if !errors.Is(adapterErr.Err, adapter.ErrCircuitOpen) {
		t.Errorf("inner error = %v, want adapter.ErrCircuitOpen", adapterErr.Err)
	}
}
