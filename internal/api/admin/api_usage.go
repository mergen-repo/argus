package admin

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
)

// apiKeyUsageItem mirrors FE APIKeyUsageItem (web/src/types/admin.ts).
type apiKeyUsageItem struct {
	KeyID          uuid.UUID     `json:"key_id"`
	KeyName        string        `json:"key_name"`
	Prefix         string        `json:"prefix"`
	TenantID       uuid.UUID     `json:"tenant_id"`
	TenantName     string        `json:"tenant_name"`
	Requests       int64         `json:"requests"`
	RateLimit      int           `json:"rate_limit"`
	ConsumptionPct float64       `json:"consumption_pct"`
	ErrorRate      float64       `json:"error_rate"`
	TopEndpoints   []endpointHit `json:"top_endpoints"`
	Anomaly        bool          `json:"anomaly"`
	EMA            float64       `json:"ema"`
}

type endpointHit struct {
	Path  string `json:"path"`
	Count int64  `json:"count"`
}

func windowBuckets(window string) (int, string) {
	switch window {
	case "1h":
		return 60, "per_minute"
	case "7d":
		return 60 * 24 * 7, "per_minute"
	default: // 24h
		return 60 * 24, "per_minute"
	}
}

// windowDuration is defined in delivery_status.go and reused here.

// ListAPIKeyUsage GET /api/v1/admin/api-keys/usage (super_admin)
func (h *Handler) ListAPIKeyUsage(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
		limit = v
	}

	type keyRow struct {
		ID                 uuid.UUID
		Name               string
		KeyPrefix          string
		RateLimitPerMinute int
		RateLimitPerHour   int
		TenantID           uuid.UUID
		TenantName         string
	}

	rows, err := h.db.Query(r.Context(), `
		SELECT ak.id, ak.name, ak.key_prefix, ak.rate_limit_per_minute, ak.rate_limit_per_hour,
			ak.tenant_id, COALESCE(t.name, '')
		FROM api_keys ak
		LEFT JOIN tenants t ON t.id = ak.tenant_id
		WHERE ak.revoked_at IS NULL
		ORDER BY ak.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list api keys for usage")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	defer rows.Close()

	var keys []keyRow
	for rows.Next() {
		var k keyRow
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.RateLimitPerMinute, &k.RateLimitPerHour,
			&k.TenantID, &k.TenantName); err != nil {
			continue
		}
		keys = append(keys, k)
	}

	items := make([]apiKeyUsageItem, 0, len(keys))
	for _, k := range keys {
		requests := h.sumRedisRequests(r.Context(), k.ID.String(), window)
		rateLimit := k.RateLimitPerMinute
		if rateLimit <= 0 {
			rateLimit = 1000
		}

		consumptionPct := 0.0
		if rateLimit > 0 {
			consumptionPct = float64(requests) / float64(rateLimit) * 100
			if consumptionPct > 100 {
				consumptionPct = 100
			}
		}

		items = append(items, apiKeyUsageItem{
			KeyID:          k.ID,
			KeyName:        k.Name,
			Prefix:         k.KeyPrefix,
			TenantID:       k.TenantID,
			TenantName:     k.TenantName,
			Requests:       requests,
			RateLimit:      rateLimit,
			ConsumptionPct: consumptionPct,
			TopEndpoints:   []endpointHit{},
			Anomaly:        consumptionPct > 90,
			EMA:            float64(requests),
		})
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Limit:   limit,
		HasMore: len(items) == limit,
	})
}

func (h *Handler) sumRedisRequests(ctx context.Context, keyID, window string) int64 {
	if h.redis == nil {
		return 0
	}

	now := time.Now().Unix()
	buckets, _ := windowBuckets(window)

	var total int64
	windowSec := int64(60)
	for i := 0; i < buckets; i++ {
		bucketStart := now - int64(i)*windowSec - (now % windowSec)
		redisKey := fmt.Sprintf("ratelimit:apikey:%s:per_minute:%d", keyID, bucketStart)
		v, err := h.redis.Get(ctx, redisKey).Int64()
		if err == nil {
			total += v
		}
	}
	return total
}
