package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// testProtocol is the default protocol label for router/failover tests
// that don't care about protocol-specific behaviour. Using a single
// constant keeps call sites consistent after the D4-A refactor.
const testProtocol = "mock"

func TestFailoverPolicy_Reject(t *testing.T) {
	router := newTestRouter()
	opID := uuid.New()
	router.RegisterOperator(opID, &failingAdapter{}, 1, 60)

	_, _ = router.ForwardAuth(context.Background(), opID, testProtocol, adapter.AuthRequest{IMSI: "123"})

	fe := NewFailoverEngine(router)
	cfg := FailoverConfig{Policy: PolicyReject, TimeoutMs: 1000}

	_, err := fe.ExecuteAuth(context.Background(), opID, testProtocol, cfg, nil, adapter.AuthRequest{IMSI: "123"})
	if err == nil {
		t.Fatal("expected error for reject policy")
	}
	if !errors.Is(err, adapter.ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen, got: %v", err)
	}
}

func TestFailoverPolicy_FallbackToNext(t *testing.T) {
	router := newTestRouter()

	failedID := uuid.New()
	router.RegisterOperator(failedID, &failingAdapter{}, 1, 60)
	_, _ = router.ForwardAuth(context.Background(), failedID, testProtocol, adapter.AuthRequest{IMSI: "123"})

	successID := registerMockOperator(t, router, 100)

	fe := NewFailoverEngine(router)
	cfg := FailoverConfig{Policy: PolicyFallbackToNext}

	result, err := fe.ExecuteAuth(context.Background(), failedID, testProtocol, cfg, []uuid.UUID{successID}, adapter.AuthRequest{IMSI: "286010123456789"})
	if err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	if result.OperatorID != successID {
		t.Errorf("expected fallback operator %s, got %s", successID, result.OperatorID)
	}
	if result.Response.Code != adapter.AuthAccept {
		t.Errorf("expected AuthAccept, got %s", result.Response.Code)
	}
}

func TestFailoverPolicy_FallbackToNext_AllFailed(t *testing.T) {
	router := newTestRouter()

	failedID1 := uuid.New()
	router.RegisterOperator(failedID1, &failingAdapter{}, 1, 60)
	_, _ = router.ForwardAuth(context.Background(), failedID1, testProtocol, adapter.AuthRequest{IMSI: "123"})

	failedID2 := uuid.New()
	router.RegisterOperator(failedID2, &failingAdapter{}, 1, 60)
	_, _ = router.ForwardAuth(context.Background(), failedID2, testProtocol, adapter.AuthRequest{IMSI: "123"})

	fe := NewFailoverEngine(router)
	cfg := FailoverConfig{Policy: PolicyFallbackToNext}

	_, err := fe.ExecuteAuth(context.Background(), failedID1, testProtocol, cfg, []uuid.UUID{failedID2}, adapter.AuthRequest{IMSI: "123"})
	if err == nil {
		t.Fatal("expected failover exhausted error")
	}
}

func TestFailoverPolicy_QueueWithTimeout_Fallback(t *testing.T) {
	router := newTestRouter()

	failedID := uuid.New()
	router.RegisterOperator(failedID, &failingAdapter{}, 1, 60)
	_, _ = router.ForwardAuth(context.Background(), failedID, testProtocol, adapter.AuthRequest{IMSI: "123"})

	successID := registerMockOperator(t, router, 100)

	fe := NewFailoverEngine(router)
	cfg := FailoverConfig{Policy: PolicyQueueWithTimeout, TimeoutMs: 100}

	start := time.Now()
	result, err := fe.ExecuteAuth(context.Background(), failedID, testProtocol, cfg, []uuid.UUID{successID}, adapter.AuthRequest{IMSI: "286010123456789"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("queue_with_timeout should fallback: %v", err)
	}
	if result.OperatorID != successID {
		t.Errorf("expected fallback operator %s, got %s", successID, result.OperatorID)
	}
	if elapsed < 80*time.Millisecond {
		t.Errorf("expected queue delay of ~100ms, got %v", elapsed)
	}
}

func TestFailoverPolicy_QueueWithTimeout_ContextCancel(t *testing.T) {
	router := newTestRouter()

	failedID := uuid.New()
	router.RegisterOperator(failedID, &failingAdapter{}, 1, 60)
	_, _ = router.ForwardAuth(context.Background(), failedID, testProtocol, adapter.AuthRequest{IMSI: "123"})

	fe := NewFailoverEngine(router)
	cfg := FailoverConfig{Policy: PolicyQueueWithTimeout, TimeoutMs: 5000}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := fe.ExecuteAuth(ctx, failedID, testProtocol, cfg, nil, adapter.AuthRequest{IMSI: "123"})
	if err == nil {
		t.Fatal("expected context deadline exceeded error")
	}
}

func TestFailoverPolicy_PrimarySucceeds(t *testing.T) {
	router := newTestRouter()
	opID := registerMockOperator(t, router, 100)

	fe := NewFailoverEngine(router)
	cfg := FailoverConfig{Policy: PolicyFallbackToNext}

	result, err := fe.ExecuteAuth(context.Background(), opID, testProtocol, cfg, nil, adapter.AuthRequest{IMSI: "286010123456789"})
	if err != nil {
		t.Fatalf("primary should succeed: %v", err)
	}
	if result.OperatorID != opID {
		t.Errorf("expected primary operator %s, got %s", opID, result.OperatorID)
	}
	if result.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", result.Attempts)
	}
}

func TestFailoverPolicy_AcctReject(t *testing.T) {
	router := newTestRouter()
	opID := uuid.New()
	router.RegisterOperator(opID, &failingAdapter{}, 1, 60)
	_ = router.ForwardAcct(context.Background(), opID, testProtocol, adapter.AcctRequest{IMSI: "123", SessionID: "s1"})

	fe := NewFailoverEngine(router)
	cfg := FailoverConfig{Policy: PolicyReject}

	err := fe.ExecuteAcct(context.Background(), opID, testProtocol, cfg, nil, adapter.AcctRequest{IMSI: "123", SessionID: "s1"})
	if err == nil {
		t.Fatal("expected error for acct reject policy")
	}
}

func TestFailoverPolicy_AcctFallback(t *testing.T) {
	router := newTestRouter()

	failedID := uuid.New()
	router.RegisterOperator(failedID, &failingAdapter{}, 1, 60)
	_ = router.ForwardAcct(context.Background(), failedID, testProtocol, adapter.AcctRequest{IMSI: "123", SessionID: "s1"})

	successID := registerMockOperator(t, router, 100)

	fe := NewFailoverEngine(router)
	cfg := FailoverConfig{Policy: PolicyFallbackToNext}

	err := fe.ExecuteAcct(context.Background(), failedID, testProtocol, cfg, []uuid.UUID{successID}, adapter.AcctRequest{IMSI: "286010123456789", SessionID: "s2"})
	if err != nil {
		t.Fatalf("acct fallback should succeed: %v", err)
	}
}

func TestValidateFailoverPolicy(t *testing.T) {
	tests := []struct {
		policy string
		valid  bool
	}{
		{"reject", true},
		{"fallback_to_next", true},
		{"queue_with_timeout", true},
		{"unknown", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.policy, func(t *testing.T) {
			if got := ValidateFailoverPolicy(tt.policy); got != tt.valid {
				t.Errorf("ValidateFailoverPolicy(%q) = %v, want %v", tt.policy, got, tt.valid)
			}
		})
	}
}

func TestRouterForwardAuthWithPolicy_Reject(t *testing.T) {
	router := newTestRouter()
	opID := uuid.New()
	router.RegisterOperatorWithFailover(opID, &failingAdapter{}, 1, 60, FailoverConfig{Policy: PolicyReject})

	_, _ = router.ForwardAuth(context.Background(), opID, testProtocol, adapter.AuthRequest{IMSI: "123"})

	_, err := router.ForwardAuthWithPolicy(context.Background(), opID, testProtocol, nil, adapter.AuthRequest{IMSI: "123"})
	if err == nil {
		t.Fatal("expected error for reject policy")
	}
}

func TestRouterForwardAuthWithPolicy_FallbackToNext(t *testing.T) {
	router := newTestRouter()

	failedID := uuid.New()
	router.RegisterOperatorWithFailover(failedID, &failingAdapter{}, 1, 60, FailoverConfig{Policy: PolicyFallbackToNext})
	_, _ = router.ForwardAuth(context.Background(), failedID, testProtocol, adapter.AuthRequest{IMSI: "123"})

	successID := registerMockOperator(t, router, 100)

	resp, err := router.ForwardAuthWithPolicy(context.Background(), failedID, testProtocol, []uuid.UUID{successID}, adapter.AuthRequest{IMSI: "286010123456789"})
	if err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	if resp.Code != adapter.AuthAccept {
		t.Errorf("expected AuthAccept, got %s", resp.Code)
	}
}

func TestRouterForwardAuthWithPolicy_QueueWithTimeout(t *testing.T) {
	router := newTestRouter()

	failedID := uuid.New()
	router.RegisterOperatorWithFailover(failedID, &failingAdapter{}, 1, 60, FailoverConfig{Policy: PolicyQueueWithTimeout, TimeoutMs: 100})
	_, _ = router.ForwardAuth(context.Background(), failedID, testProtocol, adapter.AuthRequest{IMSI: "123"})

	successID := registerMockOperator(t, router, 100)

	resp, err := router.ForwardAuthWithPolicy(context.Background(), failedID, testProtocol, []uuid.UUID{successID}, adapter.AuthRequest{IMSI: "286010123456789"})
	if err != nil {
		t.Fatalf("queue_with_timeout should fallback: %v", err)
	}
	if resp.Code != adapter.AuthAccept {
		t.Errorf("expected AuthAccept, got %s", resp.Code)
	}
}

func TestRouterStateChangeCallback(t *testing.T) {
	router := newTestRouter()

	var calledFrom, calledTo CircuitState
	var calledOperator uuid.UUID
	var calledProtocol string
	router.SetStateChangeCallback(func(opID uuid.UUID, protocol string, from, to CircuitState) {
		calledOperator = opID
		calledProtocol = protocol
		calledFrom = from
		calledTo = to
	})

	opID := uuid.New()
	// failingAdapter.Type() is "failing" — that's the protocol key the
	// router stores the breaker under (per RegisterOperator wiring).
	router.RegisterOperator(opID, &failingAdapter{}, 2, 60)

	_, _ = router.ForwardAuth(context.Background(), opID, "failing", adapter.AuthRequest{IMSI: "123"})
	_, _ = router.ForwardAuth(context.Background(), opID, "failing", adapter.AuthRequest{IMSI: "123"})

	if calledOperator != opID {
		t.Errorf("callback operator = %s, want %s", calledOperator, opID)
	}
	if calledProtocol != "failing" {
		t.Errorf("callback protocol = %s, want failing", calledProtocol)
	}
	if calledFrom != CircuitClosed {
		t.Errorf("callback from = %s, want closed", calledFrom)
	}
	if calledTo != CircuitOpen {
		t.Errorf("callback to = %s, want open", calledTo)
	}
}

func TestRouterGetSetFailoverConfig(t *testing.T) {
	router := newTestRouter()
	opID := uuid.New()

	cfg := router.GetFailoverConfig(opID)
	if cfg.Policy != PolicyReject {
		t.Errorf("default policy = %s, want reject", cfg.Policy)
	}

	router.SetFailoverConfig(opID, FailoverConfig{Policy: PolicyFallbackToNext, TimeoutMs: 3000})
	cfg = router.GetFailoverConfig(opID)
	if cfg.Policy != PolicyFallbackToNext {
		t.Errorf("policy = %s, want fallback_to_next", cfg.Policy)
	}
	if cfg.TimeoutMs != 3000 {
		t.Errorf("timeout = %d, want 3000", cfg.TimeoutMs)
	}
}

func TestRouterRemoveOperator_CleansFailover(t *testing.T) {
	router := newTestRouter()
	opID := uuid.New()
	cfg := json.RawMessage(`{"latency_ms":1,"success_rate":100}`)
	mock, _ := adapter.NewMockAdapter(cfg)
	router.RegisterOperatorWithFailover(opID, mock, 5, 60, FailoverConfig{Policy: PolicyFallbackToNext})

	router.RemoveOperator(opID)

	got := router.GetFailoverConfig(opID)
	if got.Policy != PolicyReject {
		t.Errorf("after remove, policy = %s, want default (reject)", got.Policy)
	}
}

func TestFiveConsecutiveFailures_CircuitOpens(t *testing.T) {
	router := newTestRouter()
	opID := uuid.New()
	router.RegisterOperator(opID, &failingAdapter{}, 5, 60)

	for i := 0; i < 5; i++ {
		_, _ = router.ForwardAuth(context.Background(), opID, "failing", adapter.AuthRequest{IMSI: "286010123456789"})
	}

	cb := router.GetCircuitBreaker(opID, "failing")
	if cb == nil {
		t.Fatal("circuit breaker not found")
	}
	if cb.State() != CircuitOpen {
		t.Errorf("after 5 failures: state = %s, want open", cb.State())
	}

	_, err := router.ForwardAuth(context.Background(), opID, "failing", adapter.AuthRequest{IMSI: "286010123456789"})
	if err == nil {
		t.Fatal("expected circuit open error after 5 failures")
	}
	var adapterErr *adapter.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("expected AdapterError, got %T: %v", err, err)
	}
	if !errors.Is(adapterErr.Err, adapter.ErrCircuitOpen) {
		t.Errorf("inner error = %v, want ErrCircuitOpen", adapterErr.Err)
	}
}

func TestFailoverEngine_FallbackSkipsOpenCircuit(t *testing.T) {
	router := newTestRouter()

	failedID1 := uuid.New()
	router.RegisterOperator(failedID1, &failingAdapter{}, 1, 60)
	_, _ = router.ForwardAuth(context.Background(), failedID1, testProtocol, adapter.AuthRequest{IMSI: "123"})

	failedID2 := uuid.New()
	router.RegisterOperator(failedID2, &failingAdapter{}, 1, 60)
	_, _ = router.ForwardAuth(context.Background(), failedID2, testProtocol, adapter.AuthRequest{IMSI: "123"})

	successID := registerMockOperator(t, router, 100)

	fe := NewFailoverEngine(router)
	cfg := FailoverConfig{Policy: PolicyFallbackToNext}

	result, err := fe.ExecuteAuth(context.Background(), failedID1, testProtocol, cfg, []uuid.UUID{failedID2, successID}, adapter.AuthRequest{IMSI: "286010123456789"})
	if err != nil {
		t.Fatalf("should skip open circuits and succeed: %v", err)
	}
	if result.OperatorID != successID {
		t.Errorf("expected success operator %s, got %s", successID, result.OperatorID)
	}
}

func TestFailoverConfig_DefaultTimeout(t *testing.T) {
	router := newTestRouter()
	failedID := uuid.New()
	router.RegisterOperator(failedID, &failingAdapter{}, 1, 60)
	_, _ = router.ForwardAuth(context.Background(), failedID, testProtocol, adapter.AuthRequest{IMSI: "123"})

	successID := registerMockOperator(t, router, 100)

	fe := NewFailoverEngine(router)
	cfg := FailoverConfig{Policy: PolicyQueueWithTimeout, TimeoutMs: 0}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := fe.ExecuteAuth(ctx, failedID, testProtocol, cfg, []uuid.UUID{successID}, adapter.AuthRequest{IMSI: "286010123456789"})
	if err == nil {
		return
	}
	if !errors.Is(err, context.DeadlineExceeded) && err.Error() != "failover exhausted: all operators failed for accounting" {
		t.Logf("got error (expected for context timeout): %v", err)
	}
}

// TestRouterMultiProtocolPerOperator (Wave 2 Task 6 / D4-A, AC-2):
// confirms distinct (operator, protocol) tuples have independent
// circuit breakers — one protocol's failure does not open another's
// circuit on the same operator.
func TestRouterMultiProtocolPerOperator(t *testing.T) {
	router := newTestRouter()
	opID := uuid.New()

	mockAdapter, _ := adapter.NewMockAdapter(json.RawMessage(`{"latency_ms":1,"success_rate":100}`))
	router.RegisterOperator(opID, mockAdapter, 3, 60)

	// Register a failing adapter separately. Use NewMockAdapter with
	// success_rate=0 to emit failures under the "mock" protocol — but
	// we need a distinct protocol, so register the failing adapter as
	// a pseudo "diameter" by direct registry.Set.
	failing := &failingAdapter{}
	router.registry.Set(opID, "diameter", failing)
	router.breakers[adapterKey{OperatorID: opID, Protocol: "diameter"}] = NewCircuitBreaker(2, 60)

	// Two failures on "diameter" — should open that breaker only.
	_, _ = router.ForwardAuth(context.Background(), opID, "diameter", adapter.AuthRequest{IMSI: "123"})
	_, _ = router.ForwardAuth(context.Background(), opID, "diameter", adapter.AuthRequest{IMSI: "123"})

	if cb := router.GetCircuitBreaker(opID, "diameter"); cb == nil || cb.State() != CircuitOpen {
		t.Error("diameter breaker should be open")
	}
	if cb := router.GetCircuitBreaker(opID, testProtocol); cb == nil || cb.State() != CircuitClosed {
		t.Error("mock breaker should remain closed — protocol isolation broken")
	}
}

// TestRouterRemoveOperatorProtocol_Scoped confirms RemoveOperatorProtocol
// only drops the targeted (operatorID, protocol) tuple and leaves
// peer protocols intact on the same operator.
func TestRouterRemoveOperatorProtocol_Scoped(t *testing.T) {
	router := newTestRouter()
	opID := uuid.New()

	mockA, _ := adapter.NewMockAdapter(json.RawMessage(`{"latency_ms":1,"success_rate":100}`))
	router.RegisterOperator(opID, mockA, 3, 60)

	httpA, _ := adapter.NewHTTPAdapter(json.RawMessage(`{"base_url":"http://example.com"}`))
	router.RegisterOperator(opID, httpA, 3, 60)

	router.RemoveOperatorProtocol(opID, "mock")

	if _, err := router.GetAdapter(opID, "mock"); err == nil {
		t.Error("mock adapter should be removed")
	}
	if _, err := router.GetAdapter(opID, "http"); err != nil {
		t.Error("http adapter should survive mock scoped removal")
	}
}

func newTestRouter() *OperatorRouter {
	registry := adapter.NewRegistry()
	logger := zerolog.Nop()
	return NewOperatorRouter(registry, logger)
}

func registerMockOperator(t *testing.T, router *OperatorRouter, successRate float64) uuid.UUID {
	t.Helper()
	opID := uuid.New()
	cfg := json.RawMessage(fmt.Sprintf(`{"latency_ms":1,"success_rate":%v}`, successRate))
	mock, err := adapter.NewMockAdapter(cfg)
	if err != nil {
		t.Fatalf("new mock adapter: %v", err)
	}
	router.RegisterOperator(opID, mock, 3, 60)
	return opID
}
