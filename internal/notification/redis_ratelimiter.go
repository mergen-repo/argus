package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisRateLimiter struct {
	client      *redis.Client
	limitPerMin int
}

func NewRedisRateLimiter(client *redis.Client, limitPerMin int) *RedisRateLimiter {
	return &RedisRateLimiter{
		client:      client,
		limitPerMin: limitPerMin,
	}
}

func (r *RedisRateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	now := time.Now().UnixNano()
	windowNs := window.Nanoseconds()
	windowStart := now - windowNs
	redisKey := fmt.Sprintf("notif:rl:%s", key)

	pipe := r.client.Pipeline()
	pipe.ZRemRangeByScore(ctx, redisKey, "0", fmt.Sprintf("%d", windowStart))
	countCmd := pipe.ZCard(ctx, redisKey)
	pipe.ZAdd(ctx, redisKey, redis.Z{Score: float64(now), Member: now})
	pipe.Expire(ctx, redisKey, window+time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, fmt.Errorf("redis rate limiter pipeline: %w", err)
	}

	count := countCmd.Val()
	if count >= int64(limit) {
		r.client.ZRem(ctx, redisKey, now)
		return false, nil
	}

	return true, nil
}
