package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

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
func (f *failingAdapter) Type() string { return "failing" }

func TestRouterForwardAuth(t *testing.T) {
	router := newTestRouter()
	opID := registerMockOperator(t, router, 100)

	resp, err := router.ForwardAuth(context.Background(), opID, adapter.AuthRequest{
		IMSI: "286010123456789",
	})
	if err != nil {
		t.Fatalf("forward auth: %v", err)
	}
	if resp.Code != adapter.AuthAccept {
		t.Errorf("code = %d, want AuthAccept", resp.Code)
	}
}

func TestRouterForwardAuth_NotFound(t *testing.T) {
	router := newTestRouter()
	opID := uuid.New()

	_, err := router.ForwardAuth(context.Background(), opID, adapter.AuthRequest{
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

	err := router.ForwardAcct(context.Background(), opID, adapter.AcctRequest{
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

	err := router.SendCoA(context.Background(), opID, adapter.CoARequest{
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

	err := router.SendDM(context.Background(), opID, adapter.DMRequest{
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

	_, _ = router.ForwardAuth(context.Background(), opID, adapter.AuthRequest{IMSI: "123"})
	_, _ = router.ForwardAuth(context.Background(), opID, adapter.AuthRequest{IMSI: "123"})

	cb := router.GetCircuitBreaker(opID)
	if cb == nil {
		t.Fatal("circuit breaker not found")
	}

	_, err := router.ForwardAuth(context.Background(), opID, adapter.AuthRequest{IMSI: "123"})
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

	failedID := uuid.New()
	router.RegisterOperator(failedID, &failingAdapter{}, 100, 60)

	successID := registerMockOperator(t, router, 100)

	resp, err := router.ForwardAuthWithFailover(context.Background(),
		[]uuid.UUID{failedID, successID},
		adapter.AuthRequest{IMSI: "286010123456789"},
	)
	if err != nil {
		t.Fatalf("failover auth: %v", err)
	}
	if resp.Code != adapter.AuthAccept {
		t.Errorf("code = %d, want AuthAccept", resp.Code)
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
		adapter.AuthRequest{IMSI: "286010123456789"},
	)
	if err == nil {
		t.Fatal("expected failover exhausted error")
	}
}

func TestRouterFailover_CircuitOpenSkipped(t *testing.T) {
	router := newTestRouter()

	openID := uuid.New()
	router.RegisterOperator(openID, &failingAdapter{}, 1, 60)
	_, _ = router.ForwardAuth(context.Background(), openID, adapter.AuthRequest{IMSI: "123"})

	successID := registerMockOperator(t, router, 100)

	resp, err := router.ForwardAuthWithFailover(context.Background(),
		[]uuid.UUID{openID, successID},
		adapter.AuthRequest{IMSI: "286010123456789"},
	)
	if err != nil {
		t.Fatalf("failover auth: %v", err)
	}
	if resp.Code != adapter.AuthAccept {
		t.Errorf("code = %d, want AuthAccept", resp.Code)
	}
}

func TestRouterHealthCheck(t *testing.T) {
	router := newTestRouter()
	opID := registerMockOperator(t, router, 100)

	result := router.HealthCheck(context.Background(), opID)
	if !result.Success {
		t.Errorf("health check failed: %s", result.Error)
	}
}

func TestRouterHealthCheck_NotFound(t *testing.T) {
	router := newTestRouter()
	result := router.HealthCheck(context.Background(), uuid.New())
	if result.Success {
		t.Error("expected health check to fail for unknown operator")
	}
}

func TestRouterRemoveOperator(t *testing.T) {
	router := newTestRouter()
	opID := registerMockOperator(t, router, 100)

	router.RemoveOperator(opID)

	_, err := router.GetAdapter(opID)
	if err == nil {
		t.Error("expected error after removing operator")
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
			_, _ = router.ForwardAuth(context.Background(), opID, adapter.AuthRequest{IMSI: "286010123456789"})
			_ = router.ForwardAcct(context.Background(), opID, adapter.AcctRequest{IMSI: "286010123456789", SessionID: "s1"})
			_ = router.HealthCheck(context.Background(), opID)
		}(i)
	}
	wg.Wait()
}
