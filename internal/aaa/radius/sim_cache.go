package radius

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	simIMSICachePrefix = "sim:imsi:"
	simCacheTTL        = 5 * time.Minute
)

type SIMCache struct {
	redis    *redis.Client
	simStore *store.SIMStore
	logger   zerolog.Logger
}

func NewSIMCache(redisClient *redis.Client, simStore *store.SIMStore, logger zerolog.Logger) *SIMCache {
	return &SIMCache{
		redis:    redisClient,
		simStore: simStore,
		logger:   logger.With().Str("component", "sim_cache").Logger(),
	}
}

func (c *SIMCache) GetByIMSI(ctx context.Context, imsi string) (*store.SIM, error) {
	key := simIMSICachePrefix + imsi

	if c.redis == nil {
		if c.simStore == nil {
			return nil, store.ErrSIMNotFound
		}
		return c.simStore.GetByIMSI(ctx, imsi)
	}

	data, err := c.redis.Get(ctx, key).Bytes()
	if err == nil {
		var sim store.SIM
		if err := json.Unmarshal(data, &sim); err == nil {
			return &sim, nil
		}
		c.logger.Warn().Str("imsi", imsi).Msg("failed to unmarshal cached SIM, falling through to DB")
	} else if err != redis.Nil {
		c.logger.Warn().Err(err).Str("imsi", imsi).Msg("redis GET error, falling through to DB")
	}

	if c.simStore == nil {
		return nil, store.ErrSIMNotFound
	}

	sim, err := c.simStore.GetByIMSI(ctx, imsi)
	if err != nil {
		return nil, err
	}

	if c.redis != nil {
		if encoded, err := json.Marshal(sim); err == nil {
			if err := c.redis.Set(ctx, key, encoded, simCacheTTL).Err(); err != nil {
				c.logger.Warn().Err(err).Str("imsi", imsi).Msg("failed to cache SIM in Redis")
			}
		}
	}

	return sim, nil
}

func (c *SIMCache) InvalidateIMSI(ctx context.Context, imsi string) error {
	// Mirror the nil-redis guard in GetByIMSI (sim_cache.go:36) — STORY-092
	// D-038 closure keeps enforcer/SIMCache usable with nil redis on the
	// RADIUS happy path. Without this guard the DB-fall-through auth path
	// would panic on first dynamic IP allocation.
	if c.redis == nil {
		return nil
	}
	key := simIMSICachePrefix + imsi
	if err := c.redis.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("sim_cache: invalidate %s: %w", imsi, err)
	}
	return nil
}
