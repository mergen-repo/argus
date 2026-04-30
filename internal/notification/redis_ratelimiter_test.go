package notification

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisRateLimiter_Interface(t *testing.T) {
	var _ RateLimiter = (*RedisRateLimiter)(nil)
}

func TestRedisRateLimiter_61stRequestRejected(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL not set — skipping live Redis test")
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse REDIS_URL: %v", err)
	}
	client := redis.NewClient(opt)
	defer client.Close()

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not reachable: %v", err)
	}

	rl := NewRedisRateLimiter(client, 60)
	key := fmt.Sprintf("test:rl:%d", time.Now().UnixNano())

	defer client.Del(ctx, fmt.Sprintf("notif:rl:%s", key))

	for i := 1; i <= 60; i++ {
		allowed, err := rl.Allow(ctx, key, 60, time.Minute)
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if !allowed {
			t.Fatalf("request %d: expected allowed, got denied", i)
		}
	}

	allowed, err := rl.Allow(ctx, key, 60, time.Minute)
	if err != nil {
		t.Fatalf("61st request: unexpected error: %v", err)
	}
	if allowed {
		t.Error("61st request: expected denied, got allowed")
	}
}

func TestRedisRateLimiter_Allow_EnforcesLimit(t *testing.T) {
	var callCount int64
	limiter := &thresholdRateLimiter{count: &callCount}

	ctx := context.Background()

	for i := 0; i < 60; i++ {
		allowed, err := limiter.Allow(ctx, "user-abc", 60, time.Minute)
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		if !allowed {
			t.Fatalf("request %d: expected allowed", i+1)
		}
	}

	allowed, err := limiter.Allow(ctx, "user-abc", 60, time.Minute)
	if err != nil {
		t.Fatalf("61st request error: %v", err)
	}
	if allowed {
		t.Error("61st request: expected denied, got allowed")
	}
}

type thresholdRateLimiter struct {
	count *int64
}

func (l *thresholdRateLimiter) Allow(_ context.Context, _ string, limit int, _ time.Duration) (bool, error) {
	n := atomic.AddInt64(l.count, 1)
	return int(n) <= limit, nil
}
