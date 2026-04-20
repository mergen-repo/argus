package aggregates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/btopcu/argus/internal/store"
)

const (
	keyPrefix  = "argus:aggregates:v1"
	defaultTTL = 60 * time.Second
	minTTL     = 10 * time.Second
)

// MetricsRecorder is the minimal interface that the cached aggregates decorator
// uses to report hit/miss ratios and call durations. Task 6 adds the three
// methods to *metrics.Registry so it satisfies this interface automatically.
type MetricsRecorder interface {
	IncAggregatesCacheHit(method string)
	IncAggregatesCacheMiss(method string)
	ObserveAggregatesCallDuration(method, cache string, d time.Duration)
}

// Options holds tunable knobs for the cache decorator.
type Options struct {
	TTL time.Duration
}

// Option is a functional option for the cache decorator.
type Option func(*Options)

// WithTTL sets the cache TTL. Values below minTTL are clamped up to minTTL.
func WithTTL(ttl time.Duration) Option {
	return func(o *Options) {
		if ttl < minTTL {
			ttl = minTTL
		}
		o.TTL = ttl
	}
}

// New returns an Aggregates backed by db delegation + Redis cache decorator.
// Pass rdb=nil to get a pass-through with no caching (e.g. in tests).
// Pass reg=nil to skip metrics recording.
func New(
	simStore *store.SIMStore,
	sessionStore *store.RadiusSessionStore,
	rdb *redis.Client,
	reg MetricsRecorder,
	logger zerolog.Logger,
	opts ...Option,
) Aggregates {
	options := Options{TTL: defaultTTL}
	for _, o := range opts {
		o(&options)
	}
	inner := NewDB(simStore, sessionStore, logger)
	return &cachedAggregates{
		inner:  inner,
		rdb:    rdb,
		ttl:    options.TTL,
		reg:    reg,
		logger: logger,
	}
}

type cachedAggregates struct {
	inner  Aggregates
	rdb    *redis.Client
	ttl    time.Duration
	reg    MetricsRecorder
	logger zerolog.Logger
}

func (c *cachedAggregates) SIMCountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	if tenantID == uuid.Nil {
		return c.inner.SIMCountByTenant(ctx, tenantID)
	}
	method := "sim_count_by_tenant"
	key := fmt.Sprintf("%s:%s:%s", keyPrefix, tenantID.String(), method)
	start := time.Now()

	if c.rdb != nil {
		var cached int
		hit, err := cacheGet(ctx, c.rdb, key, &cached)
		if err == nil && hit {
			c.recordHit(method, start)
			return cached, nil
		}
	}

	v, err := c.inner.SIMCountByTenant(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	if c.rdb != nil {
		_ = cacheSet(ctx, c.rdb, key, v, c.ttl)
	}
	c.recordMiss(method, start)
	return v, nil
}

func (c *cachedAggregates) SIMCountByOperator(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]int, error) {
	if tenantID == uuid.Nil {
		return c.inner.SIMCountByOperator(ctx, tenantID)
	}
	method := "sim_count_by_operator"
	key := fmt.Sprintf("%s:%s:%s", keyPrefix, tenantID.String(), method)
	start := time.Now()

	if c.rdb != nil {
		var wire map[string]int
		hit, err := cacheGet(ctx, c.rdb, key, &wire)
		if err == nil && hit {
			result, decErr := decodeUUIDMap[int](wire)
			if decErr == nil {
				c.recordHit(method, start)
				return result, nil
			}
		}
	}

	v, err := c.inner.SIMCountByOperator(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if c.rdb != nil {
		_ = cacheSet(ctx, c.rdb, key, encodeUUIDMap[int](v), c.ttl)
	}
	c.recordMiss(method, start)
	return v, nil
}

func (c *cachedAggregates) SIMCountByAPN(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]int64, error) {
	if tenantID == uuid.Nil {
		return c.inner.SIMCountByAPN(ctx, tenantID)
	}
	method := "sim_count_by_apn"
	key := fmt.Sprintf("%s:%s:%s", keyPrefix, tenantID.String(), method)
	start := time.Now()

	if c.rdb != nil {
		var wire map[string]int64
		hit, err := cacheGet(ctx, c.rdb, key, &wire)
		if err == nil && hit {
			result, decErr := decodeUUIDMap[int64](wire)
			if decErr == nil {
				c.recordHit(method, start)
				return result, nil
			}
		}
	}

	v, err := c.inner.SIMCountByAPN(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if c.rdb != nil {
		_ = cacheSet(ctx, c.rdb, key, encodeUUIDMap[int64](v), c.ttl)
	}
	c.recordMiss(method, start)
	return v, nil
}

func (c *cachedAggregates) SIMCountByPolicy(ctx context.Context, tenantID, policyID uuid.UUID) (int, error) {
	if tenantID == uuid.Nil {
		return c.inner.SIMCountByPolicy(ctx, tenantID, policyID)
	}
	method := "sim_count_by_policy"
	key := fmt.Sprintf("%s:%s:%s:%s", keyPrefix, tenantID.String(), method, policyID.String())
	start := time.Now()

	if c.rdb != nil {
		var cached int
		hit, err := cacheGet(ctx, c.rdb, key, &cached)
		if err == nil && hit {
			c.recordHit(method, start)
			return cached, nil
		}
	}

	v, err := c.inner.SIMCountByPolicy(ctx, tenantID, policyID)
	if err != nil {
		return 0, err
	}
	if c.rdb != nil {
		_ = cacheSet(ctx, c.rdb, key, v, c.ttl)
	}
	c.recordMiss(method, start)
	return v, nil
}

type simCountByStateCache struct {
	Total   int                    `json:"total"`
	ByState []store.SIMStateCount  `json:"by_state"`
}

func (c *cachedAggregates) SIMCountByState(ctx context.Context, tenantID uuid.UUID) (int, []store.SIMStateCount, error) {
	if tenantID == uuid.Nil {
		return c.inner.SIMCountByState(ctx, tenantID)
	}
	method := "sim_count_by_state"
	key := fmt.Sprintf("%s:%s:%s", keyPrefix, tenantID.String(), method)
	start := time.Now()

	if c.rdb != nil {
		var cached simCountByStateCache
		hit, err := cacheGet(ctx, c.rdb, key, &cached)
		if err == nil && hit {
			c.recordHit(method, start)
			return cached.Total, cached.ByState, nil
		}
	}

	total, byState, err := c.inner.SIMCountByState(ctx, tenantID)
	if err != nil {
		return 0, nil, err
	}
	if c.rdb != nil {
		_ = cacheSet(ctx, c.rdb, key, simCountByStateCache{Total: total, ByState: byState}, c.ttl)
	}
	c.recordMiss(method, start)
	return total, byState, nil
}

func (c *cachedAggregates) ActiveSessionStats(ctx context.Context, tenantID uuid.UUID) (*store.SessionStatsResult, error) {
	if tenantID == uuid.Nil {
		return c.inner.ActiveSessionStats(ctx, tenantID)
	}
	method := "active_session_stats"
	key := fmt.Sprintf("%s:%s:%s", keyPrefix, tenantID.String(), method)
	start := time.Now()

	if c.rdb != nil {
		var cached store.SessionStatsResult
		hit, err := cacheGet(ctx, c.rdb, key, &cached)
		if err == nil && hit {
			c.recordHit(method, start)
			return &cached, nil
		}
	}

	v, err := c.inner.ActiveSessionStats(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if c.rdb != nil && v != nil {
		_ = cacheSet(ctx, c.rdb, key, *v, c.ttl)
	}
	c.recordMiss(method, start)
	return v, nil
}

func (c *cachedAggregates) TrafficByOperator(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]int64, error) {
	if tenantID == uuid.Nil {
		return c.inner.TrafficByOperator(ctx, tenantID)
	}
	method := "traffic_by_operator"
	key := fmt.Sprintf("%s:%s:%s", keyPrefix, tenantID.String(), method)
	start := time.Now()

	if c.rdb != nil {
		var wire map[string]int64
		hit, err := cacheGet(ctx, c.rdb, key, &wire)
		if err == nil && hit {
			result, decErr := decodeUUIDMap[int64](wire)
			if decErr == nil {
				c.recordHit(method, start)
				return result, nil
			}
		}
	}

	v, err := c.inner.TrafficByOperator(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if c.rdb != nil {
		_ = cacheSet(ctx, c.rdb, key, encodeUUIDMap[int64](v), c.ttl)
	}
	c.recordMiss(method, start)
	return v, nil
}

func (c *cachedAggregates) recordHit(method string, start time.Time) {
	if c.reg == nil {
		return
	}
	c.reg.IncAggregatesCacheHit(method)
	c.reg.ObserveAggregatesCallDuration(method, "hit", time.Since(start))
}

func (c *cachedAggregates) recordMiss(method string, start time.Time) {
	if c.reg == nil {
		return
	}
	c.reg.IncAggregatesCacheMiss(method)
	c.reg.ObserveAggregatesCallDuration(method, "miss", time.Since(start))
}

// cacheGet retrieves and JSON-decodes a value from Redis.
// Returns (true, nil) on hit, (false, nil) on key-not-found, (false, err) on error.
func cacheGet(ctx context.Context, rdb *redis.Client, key string, dst any) (bool, error) {
	data, err := rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return false, err
	}
	return true, nil
}

// cacheSet JSON-encodes and stores a value in Redis with the given TTL.
func cacheSet(ctx context.Context, rdb *redis.Client, key string, val any, ttl time.Duration) error {
	data, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return rdb.Set(ctx, key, data, ttl).Err()
}

// encodeUUIDMap converts map[uuid.UUID]T to map[string]T for JSON serialization.
func encodeUUIDMap[T any](m map[uuid.UUID]T) map[string]T {
	out := make(map[string]T, len(m))
	for k, v := range m {
		out[k.String()] = v
	}
	return out
}

// decodeUUIDMap converts map[string]T back to map[uuid.UUID]T after JSON deserialization.
func decodeUUIDMap[T any](m map[string]T) (map[uuid.UUID]T, error) {
	out := make(map[uuid.UUID]T, len(m))
	for k, v := range m {
		uid, err := uuid.Parse(k)
		if err != nil {
			return nil, err
		}
		out[uid] = v
	}
	return out, nil
}
