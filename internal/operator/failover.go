package operator

import (
	"context"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/google/uuid"
)

type FailoverPolicy string

const (
	PolicyReject           FailoverPolicy = "reject"
	PolicyFallbackToNext   FailoverPolicy = "fallback_to_next"
	PolicyQueueWithTimeout FailoverPolicy = "queue_with_timeout"
)

func ValidateFailoverPolicy(p string) bool {
	switch FailoverPolicy(p) {
	case PolicyReject, PolicyFallbackToNext, PolicyQueueWithTimeout:
		return true
	}
	return false
}

type FailoverConfig struct {
	Policy    FailoverPolicy
	TimeoutMs int
}

type FailoverResult struct {
	OperatorID uuid.UUID
	Response   *adapter.AuthResponse
	UsedPolicy FailoverPolicy
	Attempts   int
	Elapsed    time.Duration
}

type FailoverEngine struct {
	router *OperatorRouter
}

func NewFailoverEngine(router *OperatorRouter) *FailoverEngine {
	return &FailoverEngine{router: router}
}

func (fe *FailoverEngine) ExecuteAuth(ctx context.Context, primaryID uuid.UUID, protocol string, config FailoverConfig, fallbackIDs []uuid.UUID, req adapter.AuthRequest) (*FailoverResult, error) {
	result := &FailoverResult{
		OperatorID: primaryID,
		UsedPolicy: config.Policy,
	}
	start := time.Now()

	resp, err := fe.router.ForwardAuth(ctx, primaryID, protocol, req)
	result.Attempts++
	if err == nil {
		result.Response = resp
		result.Elapsed = time.Since(start)
		return result, nil
	}

	switch config.Policy {
	case PolicyReject:
		result.Elapsed = time.Since(start)
		return nil, fmt.Errorf("%w: operator %s circuit open, policy=reject", adapter.ErrCircuitOpen, primaryID)

	case PolicyFallbackToNext:
		return fe.fallbackAuth(ctx, result, protocol, fallbackIDs, req, start)

	case PolicyQueueWithTimeout:
		return fe.queueThenFallback(ctx, result, primaryID, protocol, config.TimeoutMs, fallbackIDs, req, start)

	default:
		result.Elapsed = time.Since(start)
		return nil, fmt.Errorf("%w: operator %s, unknown policy %s", adapter.ErrCircuitOpen, primaryID, config.Policy)
	}
}

func (fe *FailoverEngine) fallbackAuth(ctx context.Context, result *FailoverResult, protocol string, fallbackIDs []uuid.UUID, req adapter.AuthRequest, start time.Time) (*FailoverResult, error) {
	for _, opID := range fallbackIDs {
		cb := fe.router.GetCircuitBreaker(opID, protocol)
		if cb != nil && !cb.ShouldAllow() {
			continue
		}

		resp, err := fe.router.ForwardAuth(ctx, opID, protocol, req)
		result.Attempts++
		if err == nil {
			result.OperatorID = opID
			result.Response = resp
			result.Elapsed = time.Since(start)
			return result, nil
		}
	}

	result.Elapsed = time.Since(start)
	return nil, fmt.Errorf("failover exhausted: all %d operators failed", result.Attempts)
}

func (fe *FailoverEngine) queueThenFallback(ctx context.Context, result *FailoverResult, primaryID uuid.UUID, protocol string, timeoutMs int, fallbackIDs []uuid.UUID, req adapter.AuthRequest, start time.Time) (*FailoverResult, error) {
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	retryInterval := timeout / 5
	if retryInterval < 100*time.Millisecond {
		retryInterval = 100 * time.Millisecond
	}
	retryTicker := time.NewTicker(retryInterval)
	defer retryTicker.Stop()

	for {
		select {
		case <-timer.C:
			return fe.fallbackAuth(ctx, result, protocol, fallbackIDs, req, start)
		case <-retryTicker.C:
			cb := fe.router.GetCircuitBreaker(primaryID, protocol)
			if cb != nil && cb.ShouldAllow() {
				resp, err := fe.router.ForwardAuth(ctx, primaryID, protocol, req)
				result.Attempts++
				if err == nil {
					result.Response = resp
					result.Elapsed = time.Since(start)
					return result, nil
				}
			}
		case <-ctx.Done():
			result.Elapsed = time.Since(start)
			return nil, ctx.Err()
		}
	}
}

func (fe *FailoverEngine) ExecuteAcct(ctx context.Context, primaryID uuid.UUID, protocol string, config FailoverConfig, fallbackIDs []uuid.UUID, req adapter.AcctRequest) error {
	err := fe.router.ForwardAcct(ctx, primaryID, protocol, req)
	if err == nil {
		return nil
	}

	switch config.Policy {
	case PolicyReject:
		return fmt.Errorf("%w: operator %s circuit open, policy=reject", adapter.ErrCircuitOpen, primaryID)

	case PolicyFallbackToNext:
		for _, opID := range fallbackIDs {
			cb := fe.router.GetCircuitBreaker(opID, protocol)
			if cb != nil && !cb.ShouldAllow() {
				continue
			}
			if err := fe.router.ForwardAcct(ctx, opID, protocol, req); err == nil {
				return nil
			}
		}
		return fmt.Errorf("failover exhausted: all operators failed for accounting")

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
			return ctx.Err()
		}
		for _, opID := range fallbackIDs {
			cb := fe.router.GetCircuitBreaker(opID, protocol)
			if cb != nil && !cb.ShouldAllow() {
				continue
			}
			if err := fe.router.ForwardAcct(ctx, opID, protocol, req); err == nil {
				return nil
			}
		}
		return fmt.Errorf("failover exhausted: all operators failed for accounting")

	default:
		return err
	}
}
