package ota

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	DefaultMaxOTAPerSimPerHour = 10
	otaRateLimitKeyPrefix      = "ota:ratelimit:"
)

type RateLimiter struct {
	client     *redis.Client
	maxPerHour int
}

func NewRateLimiter(client *redis.Client, maxPerHour int) *RateLimiter {
	if maxPerHour <= 0 {
		maxPerHour = DefaultMaxOTAPerSimPerHour
	}
	return &RateLimiter{
		client:     client,
		maxPerHour: maxPerHour,
	}
}

func (rl *RateLimiter) Allow(ctx context.Context, simID uuid.UUID) (bool, int, error) {
	key := fmt.Sprintf("%s%s", otaRateLimitKeyPrefix, simID.String())

	pipe := rl.client.Pipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, time.Hour)

	if _, err := pipe.Exec(ctx); err != nil {
		return false, 0, fmt.Errorf("rate limit check: %w", err)
	}

	count := int(incrCmd.Val())
	if count > rl.maxPerHour {
		return false, rl.maxPerHour - count + 1, nil
	}

	return true, rl.maxPerHour - count, nil
}

func (rl *RateLimiter) Remaining(ctx context.Context, simID uuid.UUID) (int, error) {
	key := fmt.Sprintf("%s%s", otaRateLimitKeyPrefix, simID.String())

	val, err := rl.client.Get(ctx, key).Int()
	if err == redis.Nil {
		return rl.maxPerHour, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get rate limit: %w", err)
	}

	remaining := rl.maxPerHour - val
	if remaining < 0 {
		remaining = 0
	}
	return remaining, nil
}

func (rl *RateLimiter) MaxPerHour() int {
	return rl.maxPerHour
}
