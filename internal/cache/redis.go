package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Redis struct {
	Client *redis.Client
}

func NewRedis(ctx context.Context, url string, maxConns int, readTimeout, writeTimeout time.Duration) (*Redis, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("cache: parse url: %w", err)
	}

	opts.PoolSize = maxConns
	opts.ReadTimeout = readTimeout
	opts.WriteTimeout = writeTimeout

	client := redis.NewClient(opts)

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("cache: ping: %w", err)
	}

	return &Redis{Client: client}, nil
}

func (r *Redis) HealthCheck(ctx context.Context) error {
	return r.Client.Ping(ctx).Err()
}

func (r *Redis) Close() error {
	return r.Client.Close()
}
