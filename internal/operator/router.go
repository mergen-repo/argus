package operator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type OperatorRouter struct {
	mu              sync.RWMutex
	registry        *adapter.Registry
	breakers        map[uuid.UUID]*CircuitBreaker
	failoverConfigs map[uuid.UUID]FailoverConfig
	logger          zerolog.Logger
	onStateChange   func(operatorID uuid.UUID, from, to CircuitState)
}

func NewOperatorRouter(registry *adapter.Registry, logger zerolog.Logger) *OperatorRouter {
	return &OperatorRouter{
		registry:        registry,
		breakers:        make(map[uuid.UUID]*CircuitBreaker),
		failoverConfigs: make(map[uuid.UUID]FailoverConfig),
		logger:          logger,
	}
}

func (r *OperatorRouter) SetStateChangeCallback(fn func(operatorID uuid.UUID, from, to CircuitState)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onStateChange = fn
}

func (r *OperatorRouter) SetFailoverConfig(operatorID uuid.UUID, config FailoverConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failoverConfigs[operatorID] = config
}

func (r *OperatorRouter) GetFailoverConfig(operatorID uuid.UUID) FailoverConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if cfg, ok := r.failoverConfigs[operatorID]; ok {
		return cfg
	}
	return FailoverConfig{Policy: PolicyReject, TimeoutMs: 5000}
}

func (r *OperatorRouter) RegisterOperator(operatorID uuid.UUID, a adapter.Adapter, cbThreshold, cbRecoverySec int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.registry.Set(operatorID, a)

	if cbThreshold <= 0 {
		cbThreshold = 5
	}
	if cbRecoverySec <= 0 {
		cbRecoverySec = 60
	}
	r.breakers[operatorID] = NewCircuitBreaker(cbThreshold, cbRecoverySec)
}

func (r *OperatorRouter) RegisterOperatorWithFailover(operatorID uuid.UUID, a adapter.Adapter, cbThreshold, cbRecoverySec int, config FailoverConfig) {
	r.RegisterOperator(operatorID, a, cbThreshold, cbRecoverySec)
	r.mu.Lock()
	r.failoverConfigs[operatorID] = config
	r.mu.Unlock()
}

func (r *OperatorRouter) RemoveOperator(operatorID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registry.Remove(operatorID)
	delete(r.breakers, operatorID)
	delete(r.failoverConfigs, operatorID)
}

func (r *OperatorRouter) GetAdapter(operatorID uuid.UUID) (adapter.Adapter, error) {
	a, ok := r.registry.Get(operatorID)
	if !ok {
		return nil, adapter.ErrAdapterNotFound
	}
	return a, nil
}

func (r *OperatorRouter) GetCircuitBreaker(operatorID uuid.UUID) *CircuitBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.breakers[operatorID]
}

func (r *OperatorRouter) ForwardAuth(ctx context.Context, operatorID uuid.UUID, req adapter.AuthRequest) (*adapter.AuthResponse, error) {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID)
	if err != nil {
		return nil, err
	}

	resp, err := a.ForwardAuth(ctx, req)
	r.recordResult(cb, operatorID, a.Type(), err)
	if err != nil {
		return nil, adapter.WrapError(operatorID, a.Type(), err)
	}

	return resp, nil
}

func (r *OperatorRouter) ForwardAuthWithFailover(ctx context.Context, operatorIDs []uuid.UUID, req adapter.AuthRequest) (*adapter.AuthResponse, error) {
	for _, opID := range operatorIDs {
		a, cb, err := r.resolveWithCircuitBreaker(opID)
		if err != nil {
			r.logger.Debug().
				Str("operator_id", opID.String()).
				Err(err).
				Msg("skipping operator in failover")
			continue
		}

		resp, err := a.ForwardAuth(ctx, req)
		r.recordResult(cb, opID, a.Type(), err)
		if err != nil {
			r.logger.Warn().
				Str("operator_id", opID.String()).
				Err(err).
				Msg("operator auth failed, trying failover")
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("failover exhausted: all %d operators failed", len(operatorIDs))
}

func (r *OperatorRouter) ForwardAuthWithPolicy(ctx context.Context, primaryID uuid.UUID, fallbackIDs []uuid.UUID, req adapter.AuthRequest) (*adapter.AuthResponse, error) {
	config := r.GetFailoverConfig(primaryID)

	resp, err := r.ForwardAuth(ctx, primaryID, req)
	if err == nil {
		return resp, nil
	}

	switch config.Policy {
	case PolicyReject:
		return nil, fmt.Errorf("%w: operator %s, policy=reject", adapter.ErrCircuitOpen, primaryID)

	case PolicyFallbackToNext:
		for _, opID := range fallbackIDs {
			cb := r.GetCircuitBreaker(opID)
			if cb != nil && !cb.ShouldAllow() {
				continue
			}
			resp, err := r.ForwardAuth(ctx, opID, req)
			if err == nil {
				return resp, nil
			}
		}
		return nil, fmt.Errorf("failover exhausted: all operators failed")

	case PolicyQueueWithTimeout:
		timeoutMs := config.TimeoutMs
		if timeoutMs <= 0 {
			timeoutMs = 5000
		}
		timer := time.NewTimer(time.Duration(timeoutMs) * time.Millisecond)
		defer timer.Stop()

		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		cb := r.GetCircuitBreaker(primaryID)
		if cb != nil && cb.ShouldAllow() {
			resp, err := r.ForwardAuth(ctx, primaryID, req)
			if err == nil {
				return resp, nil
			}
		}
		for _, opID := range fallbackIDs {
			cb := r.GetCircuitBreaker(opID)
			if cb != nil && !cb.ShouldAllow() {
				continue
			}
			resp, err := r.ForwardAuth(ctx, opID, req)
			if err == nil {
				return resp, nil
			}
		}
		return nil, fmt.Errorf("failover exhausted: all operators failed after queue timeout")

	default:
		return nil, err
	}
}

func (r *OperatorRouter) ForwardAcct(ctx context.Context, operatorID uuid.UUID, req adapter.AcctRequest) error {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID)
	if err != nil {
		return err
	}

	err = a.ForwardAcct(ctx, req)
	r.recordResult(cb, operatorID, a.Type(), err)
	if err != nil {
		return adapter.WrapError(operatorID, a.Type(), err)
	}

	return nil
}

func (r *OperatorRouter) SendCoA(ctx context.Context, operatorID uuid.UUID, req adapter.CoARequest) error {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID)
	if err != nil {
		return err
	}

	err = a.SendCoA(ctx, req)
	r.recordResult(cb, operatorID, a.Type(), err)
	if err != nil {
		return adapter.WrapError(operatorID, a.Type(), err)
	}

	return nil
}

func (r *OperatorRouter) SendDM(ctx context.Context, operatorID uuid.UUID, req adapter.DMRequest) error {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID)
	if err != nil {
		return err
	}

	err = a.SendDM(ctx, req)
	r.recordResult(cb, operatorID, a.Type(), err)
	if err != nil {
		return adapter.WrapError(operatorID, a.Type(), err)
	}

	return nil
}

func (r *OperatorRouter) Authenticate(ctx context.Context, operatorID uuid.UUID, req adapter.AuthenticateRequest) (*adapter.AuthenticateResponse, error) {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID)
	if err != nil {
		return nil, err
	}

	resp, err := a.Authenticate(ctx, req)
	r.recordResult(cb, operatorID, a.Type(), err)
	if err != nil {
		return nil, adapter.WrapError(operatorID, a.Type(), err)
	}

	return resp, nil
}

func (r *OperatorRouter) AccountingUpdate(ctx context.Context, operatorID uuid.UUID, req adapter.AccountingUpdateRequest) error {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID)
	if err != nil {
		return err
	}

	err = a.AccountingUpdate(ctx, req)
	r.recordResult(cb, operatorID, a.Type(), err)
	if err != nil {
		return adapter.WrapError(operatorID, a.Type(), err)
	}

	return nil
}

func (r *OperatorRouter) FetchAuthVectors(ctx context.Context, operatorID uuid.UUID, imsi string, count int) ([]adapter.AuthVector, error) {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID)
	if err != nil {
		return nil, err
	}

	vectors, err := a.FetchAuthVectors(ctx, imsi, count)
	r.recordResult(cb, operatorID, a.Type(), err)
	if err != nil {
		return nil, adapter.WrapError(operatorID, a.Type(), err)
	}

	return vectors, nil
}

func (r *OperatorRouter) HealthCheck(ctx context.Context, operatorID uuid.UUID) adapter.HealthResult {
	a, ok := r.registry.Get(operatorID)
	if !ok {
		return adapter.HealthResult{
			Success: false,
			Error:   "adapter not found for operator",
		}
	}

	result := a.HealthCheck(ctx)

	r.mu.RLock()
	cb := r.breakers[operatorID]
	r.mu.RUnlock()

	if cb != nil {
		if result.Success {
			cb.RecordSuccess()
		} else {
			cb.RecordFailure()
		}
	}

	return result
}

func (r *OperatorRouter) resolveWithCircuitBreaker(operatorID uuid.UUID) (adapter.Adapter, *CircuitBreaker, error) {
	a, ok := r.registry.Get(operatorID)
	if !ok {
		return nil, nil, adapter.WrapError(operatorID, "unknown", adapter.ErrAdapterNotFound)
	}

	r.mu.RLock()
	cb := r.breakers[operatorID]
	r.mu.RUnlock()

	if cb != nil && !cb.ShouldAllow() {
		return nil, nil, adapter.WrapError(operatorID, a.Type(), adapter.ErrCircuitOpen)
	}

	return a, cb, nil
}

func (r *OperatorRouter) recordResult(cb *CircuitBreaker, operatorID uuid.UUID, protocolType string, err error) {
	if cb == nil {
		return
	}
	prevState := cb.State()
	if err != nil {
		cb.RecordFailure()
		r.logger.Debug().
			Str("operator_id", operatorID.String()).
			Str("protocol", protocolType).
			Err(err).
			Msg("adapter call failed, circuit breaker failure recorded")
	} else {
		cb.RecordSuccess()
	}
	newState := cb.State()

	if prevState != newState {
		r.mu.RLock()
		fn := r.onStateChange
		r.mu.RUnlock()
		if fn != nil {
			fn(operatorID, prevState, newState)
		}
	}
}
