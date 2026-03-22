package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type BruteForceConfig struct {
	MaxAttempts    int
	WindowSeconds  int
	BaseDelaySec   int
	MaxDelaySec    int
}

func DefaultBruteForceConfig() BruteForceConfig {
	return BruteForceConfig{
		MaxAttempts:   10,
		WindowSeconds: 900,
		BaseDelaySec:  1,
		MaxDelaySec:   30,
	}
}

func BruteForceProtection(rdb *redis.Client, cfg BruteForceConfig, logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rdb == nil {
				next.ServeHTTP(w, r)
				return
			}

			if !isAuthEndpoint(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			ip := extractIP(r)
			ctx := r.Context()

			failKey := fmt.Sprintf("bf:fail:%s", ip)
			count, err := rdb.Get(ctx, failKey).Int()
			if err != nil && err != redis.Nil {
				logger.Warn().Err(err).Msg("brute force check failed, allowing request")
				next.ServeHTTP(w, r)
				return
			}

			if count >= cfg.MaxAttempts {
				delay := progressiveDelay(count, cfg.BaseDelaySec, cfg.MaxDelaySec)

				logger.Warn().
					Str("ip", ip).
					Int("attempts", count).
					Int("delay_sec", delay).
					Msg("brute force protection: too many failed attempts")

				retryAfter := fmt.Sprintf("%d", delay)
				w.Header().Set("Retry-After", retryAfter)
				apierr.WriteError(w, http.StatusTooManyRequests, apierr.CodeRateLimited,
					fmt.Sprintf("Too many failed authentication attempts. Retry after %d seconds.", delay))
				return
			}

			rc := &responseCaptureForBF{ResponseWriter: w, status: 200}
			next.ServeHTTP(rc, r)

			if rc.status == http.StatusUnauthorized || rc.status == http.StatusForbidden {
				recordFailedAttempt(ctx, rdb, ip, cfg.WindowSeconds)
			} else if rc.status == http.StatusOK {
				clearFailedAttempts(ctx, rdb, ip)
			}
		})
	}
}

func isAuthEndpoint(path string) bool {
	return strings.HasPrefix(path, "/api/v1/auth/login") ||
		strings.HasPrefix(path, "/api/v1/auth/2fa")
}

func extractIP(r *http.Request) string {
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

func progressiveDelay(attempts, baseSec, maxSec int) int {
	delay := baseSec
	extra := attempts - 5
	if extra < 0 {
		extra = 0
	}
	delay += extra * 2
	if delay > maxSec {
		delay = maxSec
	}
	return delay
}

func recordFailedAttempt(ctx context.Context, rdb *redis.Client, ip string, windowSec int) {
	key := fmt.Sprintf("bf:fail:%s", ip)
	pipe := rdb.Pipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, time.Duration(windowSec)*time.Second)
	pipe.Exec(ctx)
}

func clearFailedAttempts(ctx context.Context, rdb *redis.Client, ip string) {
	key := fmt.Sprintf("bf:fail:%s", ip)
	rdb.Del(ctx, key)
}

type responseCaptureForBF struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (rc *responseCaptureForBF) WriteHeader(code int) {
	if !rc.wroteHeader {
		rc.status = code
		rc.wroteHeader = true
	}
	rc.ResponseWriter.WriteHeader(code)
}

func (rc *responseCaptureForBF) Write(b []byte) (int, error) {
	if !rc.wroteHeader {
		rc.wroteHeader = true
	}
	return rc.ResponseWriter.Write(b)
}
