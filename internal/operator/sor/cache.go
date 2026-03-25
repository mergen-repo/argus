package sor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type SoRCache struct {
	client     *redis.Client
	defaultTTL time.Duration
}

func NewSoRCache(client *redis.Client, defaultTTL time.Duration) *SoRCache {
	if defaultTTL <= 0 {
		defaultTTL = time.Hour
	}
	return &SoRCache{
		client:     client,
		defaultTTL: defaultTTL,
	}
}

func (c *SoRCache) Get(ctx context.Context, tenantID uuid.UUID, imsi string) (*SoRDecision, error) {
	if c.client == nil {
		return nil, nil
	}

	data, err := c.client.Get(ctx, c.cacheKey(tenantID, imsi)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sor cache get: %w", err)
	}

	var decision SoRDecision
	if err := json.Unmarshal(data, &decision); err != nil {
		return nil, fmt.Errorf("sor cache unmarshal: %w", err)
	}
	decision.Cached = true
	return &decision, nil
}

func (c *SoRCache) Set(ctx context.Context, tenantID uuid.UUID, imsi string, decision *SoRDecision, ttl time.Duration) error {
	if c.client == nil {
		return nil
	}

	if ttl <= 0 {
		ttl = c.defaultTTL
	}

	data, err := json.Marshal(decision)
	if err != nil {
		return fmt.Errorf("sor cache marshal: %w", err)
	}

	if err := c.client.Set(ctx, c.cacheKey(tenantID, imsi), data, ttl).Err(); err != nil {
		return err
	}

	indexKey := fmt.Sprintf("sor:index:%s:%s", tenantID.String(), decision.PrimaryOperatorID.String())
	c.client.SAdd(ctx, indexKey, imsi)
	c.client.Expire(ctx, indexKey, ttl)

	return nil
}

func (c *SoRCache) Delete(ctx context.Context, tenantID uuid.UUID, imsi string) error {
	if c.client == nil {
		return nil
	}
	return c.client.Del(ctx, c.cacheKey(tenantID, imsi)).Err()
}

func (c *SoRCache) DeleteByOperator(ctx context.Context, tenantID, operatorID uuid.UUID) error {
	if c.client == nil {
		return nil
	}

	indexKey := fmt.Sprintf("sor:index:%s:%s", tenantID.String(), operatorID.String())
	members, err := c.client.SMembers(ctx, indexKey).Result()
	if err != nil || len(members) == 0 {
		return nil
	}

	pipe := c.client.Pipeline()
	for _, imsi := range members {
		key := fmt.Sprintf("sor:result:%s:%s", tenantID.String(), imsi)
		pipe.Del(ctx, key)
	}
	pipe.Del(ctx, indexKey)
	_, err = pipe.Exec(ctx)
	return err
}

func (c *SoRCache) DeleteAllForTenant(ctx context.Context, tenantID uuid.UUID) error {
	if c.client == nil {
		return nil
	}

	pattern := fmt.Sprintf("sor:result:%s:*", tenantID.String())
	var cursor uint64

	for {
		keys, nextCursor, err := c.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("sor cache scan: %w", err)
		}

		if len(keys) > 0 {
			c.client.Del(ctx, keys...)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}

func (c *SoRCache) cacheKey(tenantID uuid.UUID, imsi string) string {
	return fmt.Sprintf("sor:result:%s:%s", tenantID.String(), imsi)
}
