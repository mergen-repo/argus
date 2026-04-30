package operator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/google/uuid"
)

type failingAdapter struct{}

func (f *failingAdapter) HealthCheck(_ context.Context) adapter.HealthResult {
	return adapter.HealthResult{Success: false, Error: "always fails"}
}
func (f *failingAdapter) ForwardAuth(_ context.Context, _ adapter.AuthRequest) (*adapter.AuthResponse, error) {
	return nil, fmt.Errorf("simulated connection error")
}
func (f *failingAdapter) ForwardAcct(_ context.Context, _ adapter.AcctRequest) error {
	return fmt.Errorf("simulated connection error")
}
func (f *failingAdapter) SendCoA(_ context.Context, _ adapter.CoARequest) error {
	return fmt.Errorf("simulated connection error")
}
func (f *failingAdapter) SendDM(_ context.Context, _ adapter.DMRequest) error {
	return fmt.Errorf("simulated connection error")
}
func (f *failingAdapter) Authenticate(_ context.Context, _ adapter.AuthenticateRequest) (*adapter.AuthenticateResponse, error) {
	return nil, fmt.Errorf("simulated connection error")
}
func (f *failingAdapter) AccountingUpdate(_ context.Context, _ adapter.AccountingUpdateRequest) error {
	return fmt.Errorf("simulated connection error")
}
func (f *failingAdapter) FetchAuthVectors(_ context.Context, _ string, _ int) ([]adapter.AuthVector, error) {
	return nil, fmt.Errorf("simulated connection error")
}
func (f *failingAdapter) Type() string { return "failing" }

func TestRouterForwardAuth(t *testing.T) {
	router := newTestRouter()
	opID := registerMockOperator(t, router, 100)

	resp, err := router.ForwardAuth(context.Background(), opID, testProtocol, adapter.AuthRequest{
		IMSI: "286010123456789",
	})
	if err != nil {
		t.Fatalf("forward auth: %v", err)
	}
	if resp.Code != adapter.AuthAccept {
		t.Errorf("code = %s, want AuthAccept", resp.Code)
	}
}

func TestRouterForwardAuth_NotFound(t *testing.T) {
	router := newTestRouter()
	opID := uuid.New()

	_, err := router.ForwardAuth(context.Background(), opID, testProtocol, adapter.AuthRequest{
		IMSI: "286010123456789",
	})
	if err == nil {
		t.Fatal("expected error for unknown operator")
	}
	var adapterErr *adapter.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("expected AdapterError, got %T: %v", err, err)
	}
	if !errors.Is(adapterErr.Err, adapter.ErrAdapterNotFound) {
		t.Errorf("inner error = %v, want ErrAdapterNotFound", adapterErr.Err)
	}
}

func TestRouterForwardAcct(t *testing.T) {
	router := newTestRouter()
	opID := registerMockOperator(t, router, 100)

	err := router.ForwardAcct(context.Background(), opID, testProtocol, adapter.AcctRequest{
		IMSI:       "286010123456789",
		SessionID:  "sess-001",
		StatusType: adapter.AcctStart,
	})
	if err != nil {
		t.Fatalf("forward acct: %v", err)
	}
}

func TestRouterSendCoA(t *testing.T) {
	router := newTestRouter()
	opID := registerMockOperator(t, router, 100)

	err := router.SendCoA(context.Background(), opID, testProtocol, adapter.CoARequest{
		IMSI:      "286010123456789",
		SessionID: "sess-001",
	})
	if err != nil {
		t.Fatalf("send coa: %v", err)
	}
}

func TestRouterSendDM(t *testing.T) {
	router := newTestRouter()
	opID := registerMockOperator(t, router, 100)

	err := router.SendDM(context.Background(), opID, testProtocol, adapter.DMRequest{
		IMSI:      "286010123456789",
		SessionID: "sess-001",
	})
	if err != nil {
		t.Fatalf("send dm: %v", err)
	}
}

func TestRouterCircuitBreaker(t *testing.T) {
	router := newTestRouter()
	opID := uuid.New()
	router.RegisterOperator(opID, &failingAdapter{}, 2, 60)

	_, _ = router.ForwardAuth(context.Background(), opID, "failing", adapter.AuthRequest{IMSI: "123"})
	_, _ = router.ForwardAuth(context.Background(), opID, "failing", adapter.AuthRequest{IMSI: "123"})

	cb := router.GetCircuitBreaker(opID, "failing")
	if cb == nil {
		t.Fatal("circuit breaker not found")
	}

	_, err := router.ForwardAuth(context.Background(), opID, "failing", adapter.AuthRequest{IMSI: "123"})
	if err == nil {
		t.Fatal("expected circuit open error")
	}
	var adapterErr *adapter.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("expected AdapterError, got %T: %v", err, err)
	}
	if !errors.Is(adapterErr.Err, adapter.ErrCircuitOpen) {
		t.Errorf("inner error = %v, want ErrCircuitOpen", adapterErr.Err)
	}
}

func TestRouterFailover(t *testing.T) {
	router := newTestRouter()

	// Register the primary with a genuinely-failing adapter under a
	// matching protocol label so the failover loop resolves it and
	// then transitions to the successful fallback.
	failedID := uuid.New()
	router.registry.Set(failedID, testProtocol, &failingAdapter{})
	router.breakers[adapterKey{OperatorID: failedID, Protocol: testProtocol}] = NewCircuitBreaker(100, 60)

	successID := registerMockOperator(t, router, 100)

	resp, err := router.ForwardAuthWithFailover(context.Background(),
		[]uuid.UUID{failedID, successID},
		testProtocol,
		adapter.AuthRequest{IMSI: "286010123456789"},
	)
	if err != nil {
		t.Fatalf("failover auth: %v", err)
	}
	if resp.Code != adapter.AuthAccept {
		t.Errorf("code = %s, want AuthAccept", resp.Code)
	}
}

func TestRouterFailover_AllFailed(t *testing.T) {
	router := newTestRouter()

	op1 := uuid.New()
	router.RegisterOperator(op1, &failingAdapter{}, 100, 60)

	op2 := uuid.New()
	router.RegisterOperator(op2, &failingAdapter{}, 100, 60)

	_, err := router.ForwardAuthWithFailover(context.Background(),
		[]uuid.UUID{op1, op2},
		"failing",
		adapter.AuthRequest{IMSI: "286010123456789"},
	)
	if err == nil {
		t.Fatal("expected failover exhausted error")
	}
}

func TestRouterFailover_CircuitOpenSkipped(t *testing.T) {
	router := newTestRouter()

	// Primary returns protocol errors (failingAdapter) under the mock
	// protocol label; failure count of 1 opens its breaker immediately.
	// Fallback is a normal mock that succeeds.
	openID := uuid.New()
	router.registry.Set(openID, testProtocol, &failingAdapter{})
	router.breakers[adapterKey{OperatorID: openID, Protocol: testProtocol}] = NewCircuitBreaker(1, 60)
	_, _ = router.ForwardAuth(context.Background(), openID, testProtocol, adapter.AuthRequest{IMSI: "123"})

	successID := registerMockOperator(t, router, 100)

	resp, err := router.ForwardAuthWithFailover(context.Background(),
		[]uuid.UUID{openID, successID},
		testProtocol,
		adapter.AuthRequest{IMSI: "286010123456789"},
	)
	if err != nil {
		t.Fatalf("failover auth: %v", err)
	}
	if resp.Code != adapter.AuthAccept {
		t.Errorf("code = %s, want AuthAccept", resp.Code)
	}
}

func TestRouterHealthCheck(t *testing.T) {
	router := newTestRouter()
	opID := registerMockOperator(t, router, 100)

	result := router.HealthCheck(context.Background(), opID, testProtocol)
	if !result.Success {
		t.Errorf("health check failed: %s", result.Error)
	}
}

func TestRouterHealthCheck_NotFound(t *testing.T) {
	router := newTestRouter()
	result := router.HealthCheck(context.Background(), uuid.New(), testProtocol)
	if result.Success {
		t.Error("expected health check to fail for unknown operator")
	}
}

func TestRouterRemoveOperator(t *testing.T) {
	router := newTestRouter()
	opID := registerMockOperator(t, router, 100)

	router.RemoveOperator(opID)

	_, err := router.GetAdapter(opID, testProtocol)
	if err == nil {
		t.Error("expected error after removing operator")
	}
}

func TestRouterAuthenticate(t *testing.T) {
	router := newTestRouter()
	opID := registerMockOperator(t, router, 100)

	resp, err := router.Authenticate(context.Background(), opID, testProtocol, adapter.AuthenticateRequest{
		IMSI:    "286010123456789",
		APN:     "internet",
		RATType: "LTE",
	})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	if resp.Code != adapter.AuthAccept {
		t.Errorf("code = %s, want AuthAccept", resp.Code)
	}
	if resp.SessionID == "" {
		t.Error("expected non-empty session ID")
	}
}

func TestRouterAuthenticate_NotFound(t *testing.T) {
	router := newTestRouter()
	_, err := router.Authenticate(context.Background(), uuid.New(), testProtocol, adapter.AuthenticateRequest{IMSI: "123"})
	if err == nil {
		t.Fatal("expected error for unknown operator")
	}
	var adapterErr *adapter.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("expected AdapterError, got %T: %v", err, err)
	}
	if !errors.Is(adapterErr.Err, adapter.ErrAdapterNotFound) {
		t.Errorf("inner error = %v, want ErrAdapterNotFound", adapterErr.Err)
	}
}

func TestRouterAuthenticate_ErrorWrapping(t *testing.T) {
	router := newTestRouter()
	opID := uuid.New()
	router.RegisterOperator(opID, &failingAdapter{}, 100, 60)

	_, err := router.Authenticate(context.Background(), opID, "failing", adapter.AuthenticateRequest{IMSI: "123"})
	if err == nil {
		t.Fatal("expected error from failing adapter")
	}
	var adapterErr *adapter.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("expected AdapterError, got %T: %v", err, err)
	}
	if adapterErr.OperatorID != opID {
		t.Errorf("operator_id = %s, want %s", adapterErr.OperatorID, opID)
	}
	if adapterErr.ProtocolType != "failing" {
		t.Errorf("protocol = %s, want failing", adapterErr.ProtocolType)
	}
}

func TestRouterAccountingUpdate(t *testing.T) {
	router := newTestRouter()
	opID := registerMockOperator(t, router, 100)

	err := router.AccountingUpdate(context.Background(), opID, testProtocol, adapter.AccountingUpdateRequest{
		IMSI:         "286010123456789",
		SessionID:    "sess-001",
		StatusType:   adapter.AcctStart,
		InputOctets:  1024,
		OutputOctets: 2048,
		SessionTime:  60,
		RATType:      "LTE",
	})
	if err != nil {
		t.Fatalf("accounting update: %v", err)
	}
}

func TestRouterAccountingUpdate_NotFound(t *testing.T) {
	router := newTestRouter()
	err := router.AccountingUpdate(context.Background(), uuid.New(), testProtocol, adapter.AccountingUpdateRequest{IMSI: "123"})
	if err == nil {
		t.Fatal("expected error for unknown operator")
	}
}

func TestRouterFetchAuthVectors(t *testing.T) {
	router := newTestRouter()
	opID := registerMockOperator(t, router, 100)

	vectors, err := router.FetchAuthVectors(context.Background(), opID, testProtocol, "286010123456789", 3)
	if err != nil {
		t.Fatalf("fetch auth vectors: %v", err)
	}
	if len(vectors) != 3 {
		t.Fatalf("vector count = %d, want 3", len(vectors))
	}
	for _, v := range vectors {
		if v.Type != adapter.VectorTypeTriplet && v.Type != adapter.VectorTypeQuintet {
			t.Errorf("unexpected vector type: %s", v.Type)
		}
	}
}

func TestRouterFetchAuthVectors_NotFound(t *testing.T) {
	router := newTestRouter()
	_, err := router.FetchAuthVectors(context.Background(), uuid.New(), testProtocol, "123", 1)
	if err == nil {
		t.Fatal("expected error for unknown operator")
	}
}

func TestRouterConcurrentAccess(t *testing.T) {
	router := newTestRouter()

	var operators []uuid.UUID
	for i := 0; i < 5; i++ {
		opID := registerMockOperator(t, router, 100)
		operators = append(operators, opID)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			opID := operators[idx%len(operators)]
			_, _ = router.ForwardAuth(context.Background(), opID, testProtocol, adapter.AuthRequest{IMSI: "286010123456789"})
			_ = router.ForwardAcct(context.Background(), opID, testProtocol, adapter.AcctRequest{IMSI: "286010123456789", SessionID: "s1"})
			_ = router.HealthCheck(context.Background(), opID, testProtocol)
			_, _ = router.Authenticate(context.Background(), opID, testProtocol, adapter.AuthenticateRequest{IMSI: "286010123456789", APN: "iot"})
			_ = router.AccountingUpdate(context.Background(), opID, testProtocol, adapter.AccountingUpdateRequest{IMSI: "286010123456789", SessionID: "s1", StatusType: adapter.AcctInterim})
			_, _ = router.FetchAuthVectors(context.Background(), opID, testProtocol, "286010123456789", 2)
		}(i)
	}
	wg.Wait()
}
