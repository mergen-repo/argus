package notification

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestDeliveryTracker_ScheduleRetry_EventualSuccess(t *testing.T) {
	var callCount int32
	var completed int32

	dt := NewDeliveryTracker(nil, zerolog.Nop())
	defer dt.Stop()

	dt.ScheduleRetry(
		func() error {
			count := atomic.AddInt32(&callCount, 1)
			if count < 2 {
				return fmt.Errorf("transient error")
			}
			return nil
		},
		func(success bool, err error, attempt int) {
			if success {
				atomic.StoreInt32(&completed, 1)
			}
		},
	)

	time.Sleep(8 * time.Second)

	if atomic.LoadInt32(&completed) != 1 {
		t.Error("expected retry to eventually succeed")
	}

	count := atomic.LoadInt32(&callCount)
	if count < 2 {
		t.Errorf("call count = %d, want >= 2", count)
	}
}

func TestDeliveryTracker_PendingCount(t *testing.T) {
	dt := NewDeliveryTracker(nil, zerolog.Nop())
	defer dt.Stop()

	if dt.PendingCount() != 0 {
		t.Errorf("initial pending = %d, want 0", dt.PendingCount())
	}

	dt.ScheduleRetry(func() error { return fmt.Errorf("fail") }, nil)

	if dt.PendingCount() != 1 {
		t.Errorf("pending after schedule = %d, want 1", dt.PendingCount())
	}
}

func TestDeliveryTracker_RateLimit_Allowed(t *testing.T) {
	limiter := &mockRateLimiter{allowed: true}
	dt := NewDeliveryTracker(limiter, zerolog.Nop())
	defer dt.Stop()

	allowed, err := dt.CheckRateLimit(context.Background(), "user-123")
	if err != nil {
		t.Fatalf("rate limit check: %v", err)
	}
	if !allowed {
		t.Error("expected rate limit to allow")
	}
}

func TestDeliveryTracker_RateLimit_Denied(t *testing.T) {
	limiter := &mockRateLimiter{allowed: false}
	dt := NewDeliveryTracker(limiter, zerolog.Nop())
	defer dt.Stop()

	allowed, err := dt.CheckRateLimit(context.Background(), "user-123")
	if err != nil {
		t.Fatalf("rate limit check: %v", err)
	}
	if allowed {
		t.Error("expected rate limit to deny")
	}
}

func TestDeliveryTracker_NilRateLimiter_Allows(t *testing.T) {
	dt := NewDeliveryTracker(nil, zerolog.Nop())
	defer dt.Stop()

	allowed, err := dt.CheckRateLimit(context.Background(), "user-123")
	if err != nil {
		t.Fatalf("rate limit check: %v", err)
	}
	if !allowed {
		t.Error("nil rate limiter should allow all")
	}
}

func TestDeliveryTracker_Stop_PreventsNewRetries(t *testing.T) {
	dt := NewDeliveryTracker(nil, zerolog.Nop())
	dt.Stop()

	dt.ScheduleRetry(func() error { return nil }, nil)

	if dt.PendingCount() != 0 {
		t.Errorf("pending after stop = %d, want 0", dt.PendingCount())
	}
}
