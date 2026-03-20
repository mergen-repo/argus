package eap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	vectorCacheKeyPrefix = "eap:vectors:"
	defaultVectorTTL     = 5 * time.Minute
	defaultBatchSize     = 3
)

type CacheOption func(*CachedVectorProvider)

func WithVectorTTL(d time.Duration) CacheOption {
	return func(c *CachedVectorProvider) {
		c.ttl = d
	}
}

func WithBatchSize(n int) CacheOption {
	return func(c *CachedVectorProvider) {
		if n > 0 {
			c.batchSize = n
		}
	}
}

type CachedVectorProvider struct {
	inner     AuthVectorProvider
	redis     *redis.Client
	logger    zerolog.Logger
	ttl       time.Duration
	batchSize int
}

func NewCachedVectorProvider(inner AuthVectorProvider, redisClient *redis.Client, logger zerolog.Logger, opts ...CacheOption) *CachedVectorProvider {
	c := &CachedVectorProvider{
		inner:     inner,
		redis:     redisClient,
		logger:    logger.With().Str("component", "eap_vector_cache").Logger(),
		ttl:       defaultVectorTTL,
		batchSize: defaultBatchSize,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *CachedVectorProvider) GetSIMTriplets(ctx context.Context, imsi string) (*SIMTriplets, error) {
	if c.redis == nil {
		return c.inner.GetSIMTriplets(ctx, imsi)
	}

	key := vectorCacheKeyPrefix + imsi + ":triplet"

	data, err := c.redis.LPop(ctx, key).Bytes()
	if err == nil {
		var triplets SIMTriplets
		if err := json.Unmarshal(data, &triplets); err == nil {
			c.logger.Debug().Str("imsi", imsi).Msg("SIM triplets cache hit")
			return &triplets, nil
		}
		c.logger.Warn().Err(err).Str("imsi", imsi).Msg("failed to unmarshal cached triplets")
	} else if err != redis.Nil {
		c.logger.Warn().Err(err).Str("imsi", imsi).Msg("redis LPOP error for triplets")
	}

	tripletSets := make([]*SIMTriplets, 0, c.batchSize)
	for i := 0; i < c.batchSize; i++ {
		t, err := c.inner.GetSIMTriplets(ctx, imsi)
		if err != nil {
			if i == 0 {
				return nil, fmt.Errorf("fetch SIM triplets batch: %w", err)
			}
			break
		}
		tripletSets = append(tripletSets, t)
	}

	if len(tripletSets) > 1 {
		for _, t := range tripletSets[1:] {
			encoded, err := json.Marshal(t)
			if err != nil {
				continue
			}
			if err := c.redis.RPush(ctx, key, encoded).Err(); err != nil {
				c.logger.Warn().Err(err).Str("imsi", imsi).Msg("failed to cache triplet set")
			}
		}
		c.redis.Expire(ctx, key, c.ttl)
	}

	c.logger.Debug().
		Str("imsi", imsi).
		Int("batch_fetched", len(tripletSets)).
		Msg("SIM triplets fetched and cached")

	return tripletSets[0], nil
}

func (c *CachedVectorProvider) GetAKAQuintets(ctx context.Context, imsi string) (*AKAQuintets, error) {
	if c.redis == nil {
		return c.inner.GetAKAQuintets(ctx, imsi)
	}

	key := vectorCacheKeyPrefix + imsi + ":quintet"

	data, err := c.redis.LPop(ctx, key).Bytes()
	if err == nil {
		var quintets AKAQuintets
		if err := json.Unmarshal(data, &quintets); err == nil {
			c.logger.Debug().Str("imsi", imsi).Msg("AKA quintets cache hit")
			return &quintets, nil
		}
		c.logger.Warn().Err(err).Str("imsi", imsi).Msg("failed to unmarshal cached quintets")
	} else if err != redis.Nil {
		c.logger.Warn().Err(err).Str("imsi", imsi).Msg("redis LPOP error for quintets")
	}

	quintetSets := make([]*AKAQuintets, 0, c.batchSize)
	for i := 0; i < c.batchSize; i++ {
		q, err := c.inner.GetAKAQuintets(ctx, imsi)
		if err != nil {
			if i == 0 {
				return nil, fmt.Errorf("fetch AKA quintets batch: %w", err)
			}
			break
		}
		quintetSets = append(quintetSets, q)
	}

	if len(quintetSets) > 1 {
		for _, q := range quintetSets[1:] {
			encoded, err := json.Marshal(q)
			if err != nil {
				continue
			}
			if err := c.redis.RPush(ctx, key, encoded).Err(); err != nil {
				c.logger.Warn().Err(err).Str("imsi", imsi).Msg("failed to cache quintet set")
			}
		}
		c.redis.Expire(ctx, key, c.ttl)
	}

	c.logger.Debug().
		Str("imsi", imsi).
		Int("batch_fetched", len(quintetSets)).
		Msg("AKA quintets fetched and cached")

	return quintetSets[0], nil
}
