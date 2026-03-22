package notification

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

var retryBackoffs = []time.Duration{
	1 * time.Second,
	5 * time.Second,
	30 * time.Second,
	5 * time.Minute,
	5 * time.Minute,
}

const maxRetries = 5

type DeliveryResult struct {
	Channel    Channel
	Success    bool
	Error      error
	SentAt     time.Time
	RetryCount int
}

type RateLimiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

type DeliveryTracker struct {
	logger      zerolog.Logger
	rateLimiter RateLimiter

	mu      sync.Mutex
	pending []retryEntry
	stopCh  chan struct{}
	stopped bool
}

type retryEntry struct {
	fn         func() error
	attempt    int
	nextRetry  time.Time
	onComplete func(success bool, err error, attempt int)
}

func NewDeliveryTracker(rateLimiter RateLimiter, logger zerolog.Logger) *DeliveryTracker {
	dt := &DeliveryTracker{
		logger:      logger.With().Str("component", "delivery_tracker").Logger(),
		rateLimiter: rateLimiter,
		stopCh:      make(chan struct{}),
	}
	go dt.processRetries()
	return dt
}

func (dt *DeliveryTracker) CheckRateLimit(ctx context.Context, userID string) (bool, error) {
	if dt.rateLimiter == nil {
		return true, nil
	}
	return dt.rateLimiter.Allow(ctx, fmt.Sprintf("notif:rate:%s", userID), 10, time.Minute)
}

func (dt *DeliveryTracker) ScheduleRetry(fn func() error, onComplete func(success bool, err error, attempt int)) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	if dt.stopped {
		return
	}

	dt.pending = append(dt.pending, retryEntry{
		fn:         fn,
		attempt:    0,
		nextRetry:  time.Now().Add(retryBackoffs[0]),
		onComplete: onComplete,
	})
}

func (dt *DeliveryTracker) processRetries() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-dt.stopCh:
			return
		case <-ticker.C:
			dt.tickRetries()
		}
	}
}

func (dt *DeliveryTracker) tickRetries() {
	dt.mu.Lock()
	now := time.Now()
	var ready []retryEntry
	var remaining []retryEntry

	for _, e := range dt.pending {
		if now.After(e.nextRetry) {
			ready = append(ready, e)
		} else {
			remaining = append(remaining, e)
		}
	}
	dt.pending = remaining
	dt.mu.Unlock()

	for _, e := range ready {
		e.attempt++
		err := e.fn()
		if err == nil {
			if e.onComplete != nil {
				e.onComplete(true, nil, e.attempt)
			}
			dt.logger.Info().Int("attempt", e.attempt).Msg("retry delivery succeeded")
			continue
		}

		dt.logger.Warn().Err(err).Int("attempt", e.attempt).Msg("retry delivery failed")

		if e.attempt >= maxRetries {
			if e.onComplete != nil {
				e.onComplete(false, err, e.attempt)
			}
			continue
		}

		backoffIdx := e.attempt
		if backoffIdx >= len(retryBackoffs) {
			backoffIdx = len(retryBackoffs) - 1
		}

		dt.mu.Lock()
		dt.pending = append(dt.pending, retryEntry{
			fn:         e.fn,
			attempt:    e.attempt,
			nextRetry:  time.Now().Add(retryBackoffs[backoffIdx]),
			onComplete: e.onComplete,
		})
		dt.mu.Unlock()
	}
}

func (dt *DeliveryTracker) Stop() {
	dt.mu.Lock()
	dt.stopped = true
	dt.mu.Unlock()

	close(dt.stopCh)
}

func (dt *DeliveryTracker) PendingCount() int {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return len(dt.pending)
}
