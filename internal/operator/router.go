package operator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/config"
	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// adapterKey mirrors the unexported key shape from adapter/registry.go.
// The router keys circuit breakers per (operator, protocol) tuple —
// STORY-090 Wave 2 Task 6 (D4-A): a single operator may host multiple
// protocols, each with its own failure history.
type adapterKey struct {
	OperatorID uuid.UUID
	Protocol   string
}

type OperatorRouter struct {
	mu              sync.RWMutex
	registry        *adapter.Registry
	breakers        map[adapterKey]*CircuitBreaker
	failoverConfigs map[uuid.UUID]FailoverConfig
	logger          zerolog.Logger
	onStateChange   func(operatorID uuid.UUID, protocol string, from, to CircuitState)

	defaultThreshold   int
	defaultRecoverySec int
}

func NewOperatorRouter(registry *adapter.Registry, logger zerolog.Logger) *OperatorRouter {
	return &OperatorRouter{
		registry:           registry,
		breakers:           make(map[adapterKey]*CircuitBreaker),
		failoverConfigs:    make(map[uuid.UUID]FailoverConfig),
		logger:             logger,
		defaultThreshold:   5,
		defaultRecoverySec: 30,
	}
}

// NewOperatorRouterFromConfig builds a router seeded with circuit breaker defaults
// from global config. Per-operator override is future work (tracked in STORY-066 decisions).
func NewOperatorRouterFromConfig(cfg *config.Config, registry *adapter.Registry, logger zerolog.Logger) *OperatorRouter {
	r := NewOperatorRouter(registry, logger)
	r.defaultThreshold = cfg.CircuitBreakerThreshold
	r.defaultRecoverySec = cfg.CircuitBreakerRecoverySec
	return r
}

// SetStateChangeCallback registers a callback that fires on every
// circuit-breaker state transition. STORY-090 Wave 2: the callback
// receives the protocol alongside the operator ID so downstream
// consumers (metrics, alerts) can distinguish per-protocol failures
// within the same operator.
func (r *OperatorRouter) SetStateChangeCallback(fn func(operatorID uuid.UUID, protocol string, from, to CircuitState)) {
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

// RegisterOperator binds an adapter for (operatorID, protocol) and
// creates the associated circuit breaker. STORY-090 Wave 2: protocol
// is required (the adapter's Type() is authoritative).
func (r *OperatorRouter) RegisterOperator(operatorID uuid.UUID, a adapter.Adapter, cbThreshold, cbRecoverySec int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	protocol := a.Type()
	r.registry.Set(operatorID, protocol, a)

	if cbThreshold <= 0 {
		cbThreshold = r.defaultThreshold
	}
	if cbRecoverySec <= 0 {
		cbRecoverySec = r.defaultRecoverySec
	}
	r.breakers[adapterKey{OperatorID: operatorID, Protocol: protocol}] = NewCircuitBreaker(cbThreshold, cbRecoverySec)
}

func (r *OperatorRouter) RegisterOperatorWithFailover(operatorID uuid.UUID, a adapter.Adapter, cbThreshold, cbRecoverySec int, config FailoverConfig) {
	r.RegisterOperator(operatorID, a, cbThreshold, cbRecoverySec)
	r.mu.Lock()
	r.failoverConfigs[operatorID] = config
	r.mu.Unlock()
}

// RemoveOperator drops every adapter + breaker tuple for the operator,
// regardless of protocol. Used on operator-delete paths.
func (r *OperatorRouter) RemoveOperator(operatorID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registry.Remove(operatorID)
	for k := range r.breakers {
		if k.OperatorID == operatorID {
			delete(r.breakers, k)
		}
	}
	delete(r.failoverConfigs, operatorID)
}

// RemoveOperatorProtocol drops a single (operatorID, protocol) pair.
func (r *OperatorRouter) RemoveOperatorProtocol(operatorID uuid.UUID, protocol string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registry.RemoveProtocol(operatorID, protocol)
	delete(r.breakers, adapterKey{OperatorID: operatorID, Protocol: protocol})
}

// GetAdapter returns the adapter registered for (operatorID, protocol).
func (r *OperatorRouter) GetAdapter(operatorID uuid.UUID, protocol string) (adapter.Adapter, error) {
	a, ok := r.registry.Get(operatorID, protocol)
	if !ok {
		return nil, adapter.ErrAdapterNotFound
	}
	return a, nil
}

// GetCircuitBreaker returns the breaker for (operatorID, protocol), or
// nil if none is registered.
func (r *OperatorRouter) GetCircuitBreaker(operatorID uuid.UUID, protocol string) *CircuitBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.breakers[adapterKey{OperatorID: operatorID, Protocol: protocol}]
}

func (r *OperatorRouter) ForwardAuth(ctx context.Context, operatorID uuid.UUID, protocol string, req adapter.AuthRequest) (*adapter.AuthResponse, error) {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID, protocol)
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

func (r *OperatorRouter) ForwardAuthWithFailover(ctx context.Context, operatorIDs []uuid.UUID, protocol string, req adapter.AuthRequest) (*adapter.AuthResponse, error) {
	for _, opID := range operatorIDs {
		a, cb, err := r.resolveWithCircuitBreaker(opID, protocol)
		if err != nil {
			r.logger.Debug().
				Str("operator_id", opID.String()).
				Str("protocol", protocol).
				Err(err).
				Msg("skipping operator in failover")
			continue
		}

		resp, err := a.ForwardAuth(ctx, req)
		r.recordResult(cb, opID, a.Type(), err)
		if err != nil {
			r.logger.Warn().
				Str("operator_id", opID.String()).
				Str("protocol", protocol).
				Err(err).
				Msg("operator auth failed, trying failover")
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("failover exhausted: all %d operators failed", len(operatorIDs))
}

func (r *OperatorRouter) ForwardAuthWithPolicy(ctx context.Context, primaryID uuid.UUID, protocol string, fallbackIDs []uuid.UUID, req adapter.AuthRequest) (*adapter.AuthResponse, error) {
	config := r.GetFailoverConfig(primaryID)

	resp, err := r.ForwardAuth(ctx, primaryID, protocol, req)
	if err == nil {
		return resp, nil
	}

	switch config.Policy {
	case PolicyReject:
		return nil, fmt.Errorf("%w: operator %s, policy=reject", adapter.ErrCircuitOpen, primaryID)

	case PolicyFallbackToNext:
		for _, opID := range fallbackIDs {
			cb := r.GetCircuitBreaker(opID, protocol)
			if cb != nil && !cb.ShouldAllow() {
				continue
			}
			resp, err := r.ForwardAuth(ctx, opID, protocol, req)
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

		cb := r.GetCircuitBreaker(primaryID, protocol)
		if cb != nil && cb.ShouldAllow() {
			resp, err := r.ForwardAuth(ctx, primaryID, protocol, req)
			if err == nil {
				return resp, nil
			}
		}
		for _, opID := range fallbackIDs {
			cb := r.GetCircuitBreaker(opID, protocol)
			if cb != nil && !cb.ShouldAllow() {
				continue
			}
			resp, err := r.ForwardAuth(ctx, opID, protocol, req)
			if err == nil {
				return resp, nil
			}
		}
		return nil, fmt.Errorf("failover exhausted: all operators failed after queue timeout")

	default:
		return nil, err
	}
}

func (r *OperatorRouter) ForwardAcct(ctx context.Context, operatorID uuid.UUID, protocol string, req adapter.AcctRequest) error {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID, protocol)
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

func (r *OperatorRouter) SendCoA(ctx context.Context, operatorID uuid.UUID, protocol string, req adapter.CoARequest) error {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID, protocol)
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

func (r *OperatorRouter) SendDM(ctx context.Context, operatorID uuid.UUID, protocol string, req adapter.DMRequest) error {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID, protocol)
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

func (r *OperatorRouter) Authenticate(ctx context.Context, operatorID uuid.UUID, protocol string, req adapter.AuthenticateRequest) (*adapter.AuthenticateResponse, error) {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID, protocol)
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

func (r *OperatorRouter) AccountingUpdate(ctx context.Context, operatorID uuid.UUID, protocol string, req adapter.AccountingUpdateRequest) error {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID, protocol)
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

func (r *OperatorRouter) FetchAuthVectors(ctx context.Context, operatorID uuid.UUID, protocol string, imsi string, count int) ([]adapter.AuthVector, error) {
	a, cb, err := r.resolveWithCircuitBreaker(operatorID, protocol)
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

// HealthCheck bypasses the circuit breaker's ShouldAllow gate intentionally:
// health probes MUST execute even when the circuit is open so the breaker can
// detect operator recovery. Results are still recorded on the breaker so that
// a successful probe transitions it back to half-open/closed.
func (r *OperatorRouter) HealthCheck(ctx context.Context, operatorID uuid.UUID, protocol string) adapter.HealthResult {
	a, ok := r.registry.Get(operatorID, protocol)
	if !ok {
		return adapter.HealthResult{
			Success: false,
			Error:   "adapter not found for operator",
		}
	}

	result := a.HealthCheck(ctx)

	r.mu.RLock()
	cb := r.breakers[adapterKey{OperatorID: operatorID, Protocol: protocol}]
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

func (r *OperatorRouter) resolveWithCircuitBreaker(operatorID uuid.UUID, protocol string) (adapter.Adapter, *CircuitBreaker, error) {
	a, ok := r.registry.Get(operatorID, protocol)
	if !ok {
		return nil, nil, adapter.WrapError(operatorID, protocol, adapter.ErrAdapterNotFound)
	}

	r.mu.RLock()
	cb := r.breakers[adapterKey{OperatorID: operatorID, Protocol: protocol}]
	r.mu.RUnlock()

	if cb != nil && !cb.ShouldAllow() {
		// Returns apierr.CodeOperatorUnavailable ("OPERATOR_UNAVAILABLE") at the HTTP layer
		// when translated by the gateway error handler. The adapter layer wraps
		// adapter.ErrCircuitOpen so protocol-level callers (RADIUS, Diameter, 5G) can
		// inspect the sentinel directly without importing apierr.
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
			fn(operatorID, protocolType, prevState, newState)
		}
	}
}
