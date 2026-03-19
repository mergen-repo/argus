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

type CircuitBreaker struct {
	mu             sync.Mutex
	state          CircuitState
	failureCount   int
	threshold      int
	recoveryPeriod time.Duration
	lastFailure    time.Time
}

func NewCircuitBreaker(threshold, recoverySec int) *CircuitBreaker {
	return &CircuitBreaker{
		state:          CircuitClosed,
		threshold:      threshold,
		recoveryPeriod: time.Duration(recoverySec) * time.Second,
	}
}

func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == CircuitOpen && time.Since(cb.lastFailure) >= cb.recoveryPeriod {
		cb.state = CircuitHalfOpen
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
	cb.state = CircuitClosed
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failureCount++
	cb.lastFailure = time.Now()
	if cb.failureCount >= cb.threshold {
		cb.state = CircuitOpen
	}
}
