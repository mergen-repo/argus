package job

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

var (
	releaseScript = redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		end
		return 0
	`)

	renewScript = redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("PEXPIRE", KEYS[1], ARGV[2])
		end
		return 0
	`)
)

type DistributedLock struct {
	client *redis.Client
	logger zerolog.Logger
}

func NewDistributedLock(client *redis.Client, logger zerolog.Logger) *DistributedLock {
	return &DistributedLock{
		client: client,
		logger: logger.With().Str("component", "dist_lock").Logger(),
	}
}

func (dl *DistributedLock) keyFor(key string) string {
	return fmt.Sprintf("argus:lock:%s", key)
}

func (dl *DistributedLock) SIMKey(simID string) string {
	return fmt.Sprintf("sim:%s", simID)
}

func (dl *DistributedLock) Acquire(ctx context.Context, key string, holderID string, ttl time.Duration) (bool, error) {
	ok, err := dl.client.SetNX(ctx, dl.keyFor(key), holderID, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("acquire lock %s: %w", key, err)
	}
	if ok {
		dl.logger.Debug().Str("key", key).Str("holder", holderID).Dur("ttl", ttl).Msg("lock acquired")
	}
	return ok, nil
}

func (dl *DistributedLock) Release(ctx context.Context, key string, holderID string) error {
	result, err := releaseScript.Run(ctx, dl.client, []string{dl.keyFor(key)}, holderID).Int64()
	if err != nil {
		return fmt.Errorf("release lock %s: %w", key, err)
	}
	if result == 1 {
		dl.logger.Debug().Str("key", key).Str("holder", holderID).Msg("lock released")
	}
	return nil
}

func (dl *DistributedLock) Renew(ctx context.Context, key string, holderID string, ttl time.Duration) (bool, error) {
	result, err := renewScript.Run(ctx, dl.client, []string{dl.keyFor(key)}, holderID, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, fmt.Errorf("renew lock %s: %w", key, err)
	}
	return result == 1, nil
}

func (dl *DistributedLock) IsHeld(ctx context.Context, key string) (bool, error) {
	result, err := dl.client.Exists(ctx, dl.keyFor(key)).Result()
	if err != nil {
		return false, fmt.Errorf("check lock %s: %w", key, err)
	}
	return result > 0, nil
}
