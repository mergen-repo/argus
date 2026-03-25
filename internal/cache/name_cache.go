package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const nameCacheTTL = 5 * time.Minute

type NameCache struct {
	rdb *redis.Client
}

func NewNameCache(rdb *redis.Client) *NameCache {
	return &NameCache{rdb: rdb}
}

func (c *NameCache) GetOperatorName(ctx context.Context, id uuid.UUID) (string, bool) {
	if c.rdb == nil {
		return "", false
	}
	val, err := c.rdb.Get(ctx, fmt.Sprintf("name:op:%s", id.String())).Result()
	if err != nil {
		return "", false
	}
	return val, true
}

func (c *NameCache) SetOperatorName(ctx context.Context, id uuid.UUID, name string) {
	if c.rdb == nil {
		return
	}
	c.rdb.Set(ctx, fmt.Sprintf("name:op:%s", id.String()), name, nameCacheTTL)
}

func (c *NameCache) GetAPNName(ctx context.Context, id uuid.UUID) (string, bool) {
	if c.rdb == nil {
		return "", false
	}
	val, err := c.rdb.Get(ctx, fmt.Sprintf("name:apn:%s", id.String())).Result()
	if err != nil {
		return "", false
	}
	return val, true
}

func (c *NameCache) SetAPNName(ctx context.Context, id uuid.UUID, name string) {
	if c.rdb == nil {
		return
	}
	c.rdb.Set(ctx, fmt.Sprintf("name:apn:%s", id.String()), name, nameCacheTTL)
}

func (c *NameCache) GetPoolName(ctx context.Context, id uuid.UUID) (string, bool) {
	if c.rdb == nil {
		return "", false
	}
	val, err := c.rdb.Get(ctx, fmt.Sprintf("name:pool:%s", id.String())).Result()
	if err != nil {
		return "", false
	}
	return val, true
}

func (c *NameCache) SetPoolName(ctx context.Context, id uuid.UUID, name string) {
	if c.rdb == nil {
		return
	}
	c.rdb.Set(ctx, fmt.Sprintf("name:pool:%s", id.String()), name, nameCacheTTL)
}
