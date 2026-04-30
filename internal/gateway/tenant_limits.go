package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// LimitKey identifies the resource being limited. Using a dedicated type
// prevents accidental cross-use with raw strings at call sites.
type LimitKey string

const (
	LimitSIMs    LimitKey = "sims"
	LimitAPNs    LimitKey = "apns"
	LimitUsers   LimitKey = "users"
	LimitAPIKeys LimitKey = "api_keys"
)

// CountFn returns the current count of a resource for a tenant. One is
// registered per LimitKey so the middleware can dispatch based on the
// resource being created.
type CountFn func(ctx context.Context, tenantID uuid.UUID) (int, error)

// TenantLookup is the minimum tenant-store shape the middleware needs.
// *store.TenantStore satisfies this; a mock is used in tests.
type TenantLookup interface {
	GetByID(ctx context.Context, id uuid.UUID) (*store.Tenant, error)
}

// cachedLimits is the minimal projection cached in Redis. Caching the
// whole Tenant (with settings JSON, timestamps, etc.) would waste memory
// and include PII-adjacent fields for no benefit.
type cachedLimits struct {
	MaxSims    int `json:"max_sims"`
	MaxApns    int `json:"max_apns"`
	MaxUsers   int `json:"max_users"`
	MaxAPIKeys int `json:"max_api_keys"`
}

func limitsFromTenant(t *store.Tenant) cachedLimits {
	return cachedLimits{
		MaxSims:    t.MaxSims,
		MaxApns:    t.MaxApns,
		MaxUsers:   t.MaxUsers,
		MaxAPIKeys: t.MaxAPIKeys,
	}
}

func (c cachedLimits) forResource(resource LimitKey) (int, bool) {
	switch resource {
	case LimitSIMs:
		return c.MaxSims, true
	case LimitAPNs:
		return c.MaxApns, true
	case LimitUsers:
		return c.MaxUsers, true
	case LimitAPIKeys:
		return c.MaxAPIKeys, true
	default:
		return 0, false
	}
}

// TenantLimitsMiddleware enforces per-tenant resource caps (max_sims,
// max_apns, max_users, max_api_keys) on create endpoints. It is a soft,
// pre-insert check — a concurrent create could race the count and
// briefly exceed the limit. Hard enforcement requires a DB trigger,
// tracked as future work.
type TenantLimitsMiddleware struct {
	tenants  TenantLookup
	counters map[LimitKey]CountFn
	rdb      *redis.Client
	cacheTTL time.Duration
	logger   zerolog.Logger
}

// NewTenantLimitsMiddleware builds the middleware. cacheTTL ≤ 0 defaults
// to 5 minutes. rdb may be nil: every request then hits the DB, which is
// acceptable for tests and for deployments without Redis.
func NewTenantLimitsMiddleware(
	tenants TenantLookup,
	counters map[LimitKey]CountFn,
	rdb *redis.Client,
	cacheTTL time.Duration,
	logger zerolog.Logger,
) *TenantLimitsMiddleware {
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Minute
	}
	if counters == nil {
		counters = map[LimitKey]CountFn{}
	}
	return &TenantLimitsMiddleware{
		tenants:  tenants,
		counters: counters,
		rdb:      rdb,
		cacheTTL: cacheTTL,
		logger:   logger,
	}
}

// Enforce returns a middleware that applies the limit for the given
// resource. Wrap it around POST create endpoints only.
func (m *TenantLimitsMiddleware) Enforce(resource LimitKey) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			tenantID, ok := ctx.Value(apierr.TenantIDKey).(uuid.UUID)
			if !ok || tenantID == uuid.Nil {
				// No authenticated tenant — let downstream auth
				// layers handle the 401. Do not mask it with a
				// tenant-limit error.
				next.ServeHTTP(w, r)
				return
			}

			counter, ok := m.counters[resource]
			if !ok {
				// Defensive: a misconfigured route registered a
				// LimitKey we don't have a counter for. Log and
				// allow the request rather than blackholing it.
				m.logger.Error().Str("resource", string(resource)).
					Msg("tenant limits: no counter registered for resource; bypassing check")
				next.ServeHTTP(w, r)
				return
			}

			limits, err := m.loadLimits(ctx, tenantID)
			if err != nil {
				m.logger.Error().Err(err).Str("tenant_id", tenantID.String()).
					Msg("tenant limits: lookup failed")
				apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
					"Unable to enforce tenant resource limits")
				return
			}

			limit, known := limits.forResource(resource)
			if !known {
				m.logger.Error().Str("resource", string(resource)).
					Msg("tenant limits: unknown resource key; bypassing check")
				next.ServeHTTP(w, r)
				return
			}

			// limit == 0 means unlimited by policy (tenants may be
			// provisioned without a cap). Negative values are treated
			// the same way for safety.
			if limit <= 0 {
				next.ServeHTTP(w, r)
				return
			}

			current, err := counter(ctx, tenantID)
			if err != nil {
				m.logger.Error().Err(err).Str("tenant_id", tenantID.String()).
					Str("resource", string(resource)).
					Msg("tenant limits: count failed")
				apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
					"Unable to enforce tenant resource limits")
				return
			}

			if current >= limit {
				apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeTenantLimitExceeded,
					"Tenant resource limit exceeded",
					[]map[string]interface{}{
						{"resource": string(resource), "current": current, "limit": limit},
					})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// loadLimits returns the cached limits projection, filling the cache on
// miss. Redis errors do not fail the request — we fall through to the
// DB. Unmarshal errors are treated like a miss so a corrupt entry
// heals itself.
func (m *TenantLimitsMiddleware) loadLimits(ctx context.Context, tenantID uuid.UUID) (cachedLimits, error) {
	cacheKey := "tenant:limits:" + tenantID.String()

	if m.rdb != nil {
		raw, err := m.rdb.Get(ctx, cacheKey).Bytes()
		if err == nil {
			var cached cachedLimits
			if jerr := json.Unmarshal(raw, &cached); jerr == nil {
				return cached, nil
			}
			m.logger.Warn().Str("tenant_id", tenantID.String()).
				Msg("tenant limits: corrupt cache entry, refreshing")
		} else if !errors.Is(err, redis.Nil) {
			m.logger.Warn().Err(err).Str("tenant_id", tenantID.String()).
				Msg("tenant limits: redis get failed, falling back to DB")
		}
	}

	tenant, err := m.tenants.GetByID(ctx, tenantID)
	if err != nil {
		return cachedLimits{}, fmt.Errorf("tenant lookup: %w", err)
	}
	limits := limitsFromTenant(tenant)

	if m.rdb != nil {
		if payload, jerr := json.Marshal(limits); jerr == nil {
			if serr := m.rdb.Set(ctx, cacheKey, payload, m.cacheTTL).Err(); serr != nil {
				m.logger.Warn().Err(serr).Str("tenant_id", tenantID.String()).
					Msg("tenant limits: redis set failed, continuing without cache")
			}
		}
	}

	return limits, nil
}
