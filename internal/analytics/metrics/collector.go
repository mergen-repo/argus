package metrics

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	keyAuthTotal   = "metrics:auth:total"
	keyAuthSuccess = "metrics:auth:success"
	keyAuthFailure = "metrics:auth:failure"
	keyLatency     = "metrics:latency:global"
	counterTTL     = 5 * time.Second
	latencyWindow  = 60 * time.Second
	latencyTTL     = 120 * time.Second
)

type SessionCounter interface {
	CountActive(ctx context.Context) (int64, error)
}

type OperatorLister interface {
	ListActiveOperatorIDs(ctx context.Context) ([]uuid.UUID, error)
}

type Collector struct {
	redis          *redis.Client
	logger         zerolog.Logger
	sessionCounter SessionCounter
	operatorIDs    []uuid.UUID
}

func NewCollector(redisClient *redis.Client, logger zerolog.Logger) *Collector {
	return &Collector{
		redis:  redisClient,
		logger: logger.With().Str("component", "metrics_collector").Logger(),
	}
}

func (c *Collector) SetSessionCounter(sc SessionCounter) {
	c.sessionCounter = sc
}

func (c *Collector) SetOperatorIDs(ids []uuid.UUID) {
	c.operatorIDs = ids
}

func (c *Collector) RecordAuth(ctx context.Context, operatorID uuid.UUID, success bool, latencyMs int) {
	if c.redis == nil {
		return
	}

	epoch := time.Now().Unix()
	epochStr := strconv.FormatInt(epoch, 10)

	pipe := c.redis.Pipeline()

	totalKey := fmt.Sprintf("%s:%s", keyAuthTotal, epochStr)
	pipe.Incr(ctx, totalKey)
	pipe.Expire(ctx, totalKey, counterTTL)

	if success {
		successKey := fmt.Sprintf("%s:%s", keyAuthSuccess, epochStr)
		pipe.Incr(ctx, successKey)
		pipe.Expire(ctx, successKey, counterTTL)
	} else {
		failKey := fmt.Sprintf("%s:%s", keyAuthFailure, epochStr)
		pipe.Incr(ctx, failKey)
		pipe.Expire(ctx, failKey, counterTTL)
	}

	if operatorID != uuid.Nil {
		opID := operatorID.String()
		opTotalKey := fmt.Sprintf("%s:%s:%s", keyAuthTotal, opID, epochStr)
		pipe.Incr(ctx, opTotalKey)
		pipe.Expire(ctx, opTotalKey, counterTTL)

		if success {
			opSuccKey := fmt.Sprintf("%s:%s:%s", keyAuthSuccess, opID, epochStr)
			pipe.Incr(ctx, opSuccKey)
			pipe.Expire(ctx, opSuccKey, counterTTL)
		} else {
			opFailKey := fmt.Sprintf("%s:%s:%s", keyAuthFailure, opID, epochStr)
			pipe.Incr(ctx, opFailKey)
			pipe.Expire(ctx, opFailKey, counterTTL)
		}
	}

	nowMs := float64(time.Now().UnixNano())
	member := fmt.Sprintf("%d:%d", latencyMs, time.Now().UnixNano())
	pipe.ZAdd(ctx, keyLatency, redis.Z{Score: nowMs, Member: member})
	pipe.Expire(ctx, keyLatency, latencyTTL)

	cutoff := float64(time.Now().Add(-latencyWindow).UnixNano())
	pipe.ZRemRangeByScore(ctx, keyLatency, "0", fmt.Sprintf("%f", cutoff))

	if operatorID != uuid.Nil {
		opLatKey := fmt.Sprintf("%s:%s", "metrics:latency", operatorID.String())
		pipe.ZAdd(ctx, opLatKey, redis.Z{Score: nowMs, Member: member})
		pipe.Expire(ctx, opLatKey, latencyTTL)
		pipe.ZRemRangeByScore(ctx, opLatKey, "0", fmt.Sprintf("%f", cutoff))
	}

	if _, err := pipe.Exec(ctx); err != nil {
		c.logger.Warn().Err(err).Msg("failed to record auth metrics")
	}
}

func (c *Collector) GetMetrics(ctx context.Context) (SystemMetrics, error) {
	var m SystemMetrics
	m.ByOperator = make(map[string]*OperatorMetrics)

	epoch := time.Now().Unix() - 1
	epochStr := strconv.FormatInt(epoch, 10)

	total, _ := c.redis.Get(ctx, fmt.Sprintf("%s:%s", keyAuthTotal, epochStr)).Int64()
	failure, _ := c.redis.Get(ctx, fmt.Sprintf("%s:%s", keyAuthFailure, epochStr)).Int64()

	m.AuthPerSec = total
	if total > 0 {
		m.AuthErrorRate = math.Round(float64(failure)/float64(total)*10000) / 10000
	}

	m.Latency = c.computeLatencyPercentiles(ctx, keyLatency)

	if c.sessionCounter != nil {
		count, err := c.sessionCounter.CountActive(ctx)
		if err != nil {
			c.logger.Warn().Err(err).Msg("failed to get active session count")
		} else {
			m.ActiveSessions = count
		}
	}

	for _, opID := range c.operatorIDs {
		opIDStr := opID.String()
		opTotal, _ := c.redis.Get(ctx, fmt.Sprintf("%s:%s:%s", keyAuthTotal, opIDStr, epochStr)).Int64()
		opFailure, _ := c.redis.Get(ctx, fmt.Sprintf("%s:%s:%s", keyAuthFailure, opIDStr, epochStr)).Int64()

		opM := &OperatorMetrics{
			OperatorID: opID,
			AuthPerSec: opTotal,
		}
		if opTotal > 0 {
			opM.AuthErrorRate = math.Round(float64(opFailure)/float64(opTotal)*10000) / 10000
		}

		opLatKey := fmt.Sprintf("%s:%s", "metrics:latency", opIDStr)
		opM.Latency = c.computeLatencyPercentiles(ctx, opLatKey)

		m.ByOperator[opIDStr] = opM
	}

	m.SystemStatus = DeriveStatus(m.AuthErrorRate)

	return m, nil
}

func (c *Collector) computeLatencyPercentiles(ctx context.Context, key string) LatencyPercentiles {
	cutoff := float64(time.Now().Add(-latencyWindow).UnixNano())
	now := float64(time.Now().UnixNano())

	results, err := c.redis.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%f", cutoff),
		Max: fmt.Sprintf("%f", now),
	}).Result()
	if err != nil || len(results) == 0 {
		return LatencyPercentiles{}
	}

	latencies := make([]int, 0, len(results))
	for _, r := range results {
		parts := strings.SplitN(r, ":", 2)
		if len(parts) >= 1 {
			if v, err := strconv.Atoi(parts[0]); err == nil {
				latencies = append(latencies, v)
			}
		}
	}

	if len(latencies) == 0 {
		return LatencyPercentiles{}
	}

	sort.Ints(latencies)

	return LatencyPercentiles{
		P50: percentile(latencies, 50),
		P95: percentile(latencies, 95),
		P99: percentile(latencies, 99),
	}
}

func percentile(sorted []int, pct int) int {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(pct)/100.0*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
