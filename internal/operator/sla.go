package operator

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type SLAMetrics struct {
	Uptime24h    float64 `json:"uptime_24h"`
	TotalChecks  int64   `json:"total_checks"`
	FailedChecks int64   `json:"failed_checks"`
	LatencyP50Ms int     `json:"latency_p50_ms"`
	LatencyP95Ms int     `json:"latency_p95_ms"`
	LatencyP99Ms int     `json:"latency_p99_ms"`
	SLATarget    float64 `json:"sla_target"`
	SLAViolation bool    `json:"sla_violation"`
}

type SLATracker struct {
	redis  *redis.Client
	logger zerolog.Logger
}

func NewSLATracker(redisClient *redis.Client, logger zerolog.Logger) *SLATracker {
	return &SLATracker{
		redis:  redisClient,
		logger: logger,
	}
}

func CalculateUptime(totalChecks, failedChecks int64) float64 {
	if totalChecks == 0 {
		return 100.0
	}
	uptime := float64(totalChecks-failedChecks) / float64(totalChecks) * 100.0
	return math.Round(uptime*100) / 100
}

func CheckSLAViolation(uptime float64, target *float64) bool {
	if target == nil {
		return false
	}
	return uptime < *target
}

func (st *SLATracker) RecordLatency(ctx context.Context, operatorID uuid.UUID, latencyMs int) {
	if st.redis == nil {
		return
	}
	key := fmt.Sprintf("operator:latency:%s", operatorID.String())
	now := float64(time.Now().UnixMilli())

	st.redis.ZAdd(ctx, key, redis.Z{
		Score:  now,
		Member: latencyMs,
	})

	oneHourAgo := float64(time.Now().Add(-1 * time.Hour).UnixMilli())
	st.redis.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%f", oneHourAgo))

	st.redis.Expire(ctx, key, 2*time.Hour)
}

func (st *SLATracker) GetLatencyPercentiles(ctx context.Context, operatorID uuid.UUID) (p50, p95, p99 int) {
	if st.redis == nil {
		return 0, 0, 0
	}

	cacheKey := fmt.Sprintf("operator:latency:p:%s", operatorID.String())
	vals, cErr := st.redis.HGetAll(ctx, cacheKey).Result()
	if cErr == nil && len(vals) >= 3 {
		v50, _ := strconv.Atoi(vals["p50"])
		v95, _ := strconv.Atoi(vals["p95"])
		v99, _ := strconv.Atoi(vals["p99"])
		return v50, v95, v99
	}

	key := fmt.Sprintf("operator:latency:%s", operatorID.String())
	oneHourAgo := float64(time.Now().Add(-1 * time.Hour).UnixMilli())
	now := float64(time.Now().UnixMilli())

	results, err := st.redis.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%f", oneHourAgo),
		Max: fmt.Sprintf("%f", now),
	}).Result()
	if err != nil || len(results) == 0 {
		return 0, 0, 0
	}

	latencies := make([]int, 0, len(results))
	for _, r := range results {
		var v int
		if _, err := fmt.Sscanf(r, "%d", &v); err == nil {
			latencies = append(latencies, v)
		}
	}

	if len(latencies) == 0 {
		return 0, 0, 0
	}

	sort.Ints(latencies)

	p50 = percentile(latencies, 50)
	p95 = percentile(latencies, 95)
	p99 = percentile(latencies, 99)

	pipe := st.redis.Pipeline()
	pipe.HSet(ctx, cacheKey, "p50", strconv.Itoa(p50), "p95", strconv.Itoa(p95), "p99", strconv.Itoa(p99))
	pipe.Expire(ctx, cacheKey, 10*time.Second)
	pipe.Exec(ctx)

	return
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

func (st *SLATracker) ComputeMetrics(ctx context.Context, operatorID uuid.UUID, totalChecks, failedChecks int64, slaTarget *float64) SLAMetrics {
	uptime := CalculateUptime(totalChecks, failedChecks)
	p50, p95, p99 := st.GetLatencyPercentiles(ctx, operatorID)

	target := 0.0
	if slaTarget != nil {
		target = *slaTarget
	}

	return SLAMetrics{
		Uptime24h:    uptime,
		TotalChecks:  totalChecks,
		FailedChecks: failedChecks,
		LatencyP50Ms: p50,
		LatencyP95Ms: p95,
		LatencyP99Ms: p99,
		SLATarget:    target,
		SLAViolation: CheckSLAViolation(uptime, slaTarget),
	}
}
