package gateway

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type contextKeyRateLimit string

const (
	RateLimitPerMinuteKey contextKeyRateLimit = "rate_limit_per_minute"
	RateLimitPerHourKey   contextKeyRateLimit = "rate_limit_per_hour"
)

type RateLimitResult struct {
	Allowed   bool
	Remaining int
	ResetAt   int64
	Limit     int
}

func RateLimiter(redisClient *redis.Client, defaultPerMin, defaultPerHour int, logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/health") {
				next.ServeHTTP(w, r)
				return
			}

			if redisClient == nil {
				next.ServeHTTP(w, r)
				return
			}

			identifier := resolveIdentifier(r)
			perMin, perHour := resolveLimits(r, defaultPerMin, defaultPerHour)

			ctx := r.Context()
			minResult, err := checkRateLimit(ctx, redisClient, identifier, "per_minute", perMin, 60)
			if err != nil {
				logger.Warn().Err(err).Str("identifier", identifier).Msg("rate limit check failed, allowing request")
				next.ServeHTTP(w, r)
				return
			}

			if !minResult.Allowed {
				writeRateLimitResponse(w, minResult, "per_minute")
				return
			}

			hourResult, err := checkRateLimit(ctx, redisClient, identifier, "per_hour", perHour, 3600)
			if err != nil {
				logger.Warn().Err(err).Str("identifier", identifier).Msg("rate limit check failed, allowing request")
				next.ServeHTTP(w, r)
				return
			}

			if !hourResult.Allowed {
				writeRateLimitResponse(w, hourResult, "per_hour")
				return
			}

			setRateLimitHeaders(w, minResult)
			next.ServeHTTP(w, r)
		})
	}
}

func resolveIdentifier(r *http.Request) string {
	if authType, ok := r.Context().Value(apierr.AuthTypeKey).(string); ok && authType == "api_key" {
		if id, ok := r.Context().Value(apierr.APIKeyIDKey).(string); ok {
			return "apikey:" + id
		}
	}

	if tenantID, ok := r.Context().Value(apierr.TenantIDKey).(interface{ String() string }); ok {
		if userID, ok := r.Context().Value(apierr.UserIDKey).(interface{ String() string }); ok {
			return "user:" + tenantID.String() + ":" + userID.String()
		}
	}

	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return "ip:" + ip
}

func resolveLimits(r *http.Request, defaultPerMin, defaultPerHour int) (int, int) {
	perMin := defaultPerMin
	perHour := defaultPerHour

	if v, ok := r.Context().Value(RateLimitPerMinuteKey).(int); ok && v > 0 {
		perMin = v
	}
	if v, ok := r.Context().Value(RateLimitPerHourKey).(int); ok && v > 0 {
		perHour = v
	}

	return perMin, perHour
}

func checkRateLimit(ctx context.Context, client *redis.Client, identifier, window string, limit, windowSec int) (*RateLimitResult, error) {
	now := time.Now().Unix()
	windowStart := now - (now % int64(windowSec))
	currentKey := fmt.Sprintf("ratelimit:%s:%s:%d", identifier, window, windowStart)
	previousKey := fmt.Sprintf("ratelimit:%s:%s:%d", identifier, window, windowStart-int64(windowSec))

	pipe := client.Pipeline()
	prevCmd := pipe.Get(ctx, previousKey)
	incrCmd := pipe.Incr(ctx, currentKey)
	pipe.Expire(ctx, currentKey, time.Duration(windowSec*2)*time.Second)
	_, err := pipe.Exec(ctx)
	if err != nil && !isRedisNil(err) {
		return nil, fmt.Errorf("redis pipeline: %w", err)
	}

	prevCount := int64(0)
	if v, pErr := prevCmd.Int64(); pErr == nil {
		prevCount = v
	}
	currCount := incrCmd.Val()

	elapsed := now - windowStart
	weight := float64(int64(windowSec)-elapsed) / float64(windowSec)
	weightedCount := float64(prevCount)*weight + float64(currCount)

	if weightedCount > float64(limit) {
		client.Decr(ctx, currentKey)
		return &RateLimitResult{
			Allowed:   false,
			Remaining: 0,
			ResetAt:   windowStart + int64(windowSec),
			Limit:     limit,
		}, nil
	}

	remaining := limit - int(math.Ceil(weightedCount))
	if remaining < 0 {
		remaining = 0
	}

	return &RateLimitResult{
		Allowed:   true,
		Remaining: remaining,
		ResetAt:   windowStart + int64(windowSec),
		Limit:     limit,
	}, nil
}

func isRedisNil(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, redis.Nil) || strings.Contains(err.Error(), "redis: nil")
}

func writeRateLimitResponse(w http.ResponseWriter, result *RateLimitResult, window string) {
	retryAfter := result.ResetAt - time.Now().Unix()
	if retryAfter < 1 {
		retryAfter = 1
	}

	w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(result.Limit))
	w.Header().Set("X-RateLimit-Remaining", "0")
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetAt, 10))

	apierr.WriteError(w, http.StatusTooManyRequests, apierr.CodeRateLimited,
		fmt.Sprintf("Rate limit exceeded. Retry after %d seconds.", retryAfter),
		[]map[string]interface{}{
			{
				"limit":               result.Limit,
				"window":              window,
				"retry_after_seconds": retryAfter,
			},
		})
}

func setRateLimitHeaders(w http.ResponseWriter, result *RateLimitResult) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(result.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetAt, 10))
}
