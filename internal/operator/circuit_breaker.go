package operator

import (
	"sync"
	"time"
)

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

// TransitionHook is invoked whenever the circuit breaker transitions to
// a new state. Hooks must be fast and non-blocking — they run while the
// breaker's internal mutex is held. A nil hook is a no-op.
type TransitionHook func(state CircuitState)

type CircuitBreaker struct {
	mu             sync.Mutex
	state          CircuitState
	failureCount   int
	threshold      int
	recoveryPeriod time.Duration
	lastFailure    time.Time
	onTransition   TransitionHook
}

func NewCircuitBreaker(threshold, recoverySec int) *CircuitBreaker {
	return &CircuitBreaker{
		state:          CircuitClosed,
		threshold:      threshold,
		recoveryPeriod: time.Duration(recoverySec) * time.Second,
	}
}

// SetTransitionHook registers a callback that fires on every state
// change. Passing nil clears the hook. The hook runs while the
// breaker's mutex is held so it must not call back into the breaker.
func (cb *CircuitBreaker) SetTransitionHook(hook TransitionHook) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onTransition = hook
}

// fireHook runs the hook if set. Caller must hold cb.mu.
func (cb *CircuitBreaker) fireHook(state CircuitState) {
	if cb.onTransition != nil {
		cb.onTransition(state)
	}
}

func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == CircuitOpen && time.Since(cb.lastFailure) >= cb.recoveryPeriod {
		cb.state = CircuitHalfOpen
		cb.fireHook(CircuitHalfOpen)
	}
	return cb.state
}

func (cb *CircuitBreaker) ShouldAllow() bool {
	state := cb.State()
	return state == CircuitClosed || state == CircuitHalfOpen
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failureCount = 0
	prev := cb.state
	cb.state = CircuitClosed
	if prev != CircuitClosed {
		cb.fireHook(CircuitClosed)
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failureCount++
	cb.lastFailure = time.Now()
	if cb.failureCount >= cb.threshold && cb.state != CircuitOpen {
		cb.state = CircuitOpen
		cb.fireHook(CircuitOpen)
	}
}
