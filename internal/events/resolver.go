// Package events provides the publisher-side name resolver that embeds
// EntityRef.display_name into bus.Envelope payloads (FIX-212 AC-6).
//
// Design (FIX-212 plan §Name Resolution Strategy, D2 hybrid):
//   - Redis-backed LRU cache with 10-minute TTL per entity kind.
//   - Cache misses hit the store synchronously. Acceptable for non-AAA
//     publishers firing O(1-10)/sec.
//   - AAA hot-path session publishers MUST NOT invoke ResolveOperator or
//     ResolveAPN — they embed ICCID directly from pre-loaded SIM context.
//     Only ResolveICCID is safe for the hot path.
//   - Cache invalidation: subscribes to bus.SubjectCacheInvalidate for
//     "operator:<id>" / "apn:<id>" keys (FIX-202 precedent) and DELs the
//     corresponding resolver key.
//
// Graceful degradation: every Resolve* method returns ("", nil) when the
// underlying lookup errors (Redis down, DB error, row missing). Publishers
// proceed with empty display_name; FE falls back to entity.id.
package events

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"golang.org/x/sync/singleflight"
)

const (
	resolverTTL = 10 * time.Minute
	// resolverNegativeTTL is the short TTL written for rows the store reports
	// as "does not exist" — prevents a cold cache from repeatedly hitting the
	// DB for a missing id. Short enough that a freshly-created entity becomes
	// visible within one TTL window; long enough that burst storms coalesce.
	resolverNegativeTTL = 30 * time.Second
	// resolverNegativeSentinel marks a cached "not-found". Never returned to
	// callers — cacheGet filters it into a miss with reason="negative".
	resolverNegativeSentinel = "__neg__"

	keyPrefixSim      = "argus:resolve:sim:"
	keyPrefixOperator = "argus:resolve:operator:"
	keyPrefixAPN      = "argus:resolve:apn:"
)

// Resolver is the interface publishers depend on. Implementations must be
// safe for concurrent use.
type Resolver interface {
	ResolveICCID(ctx context.Context, simID uuid.UUID) (string, error)
	ResolveOperator(ctx context.Context, operatorID uuid.UUID) (string, error)
	ResolveAPN(ctx context.Context, tenantID, apnID uuid.UUID) (string, error)
}

// SimLookup is the narrow store dependency used by the resolver.
type SimLookup interface {
	GetByID(ctx context.Context, tenantID, id uuid.UUID) (*simRow, error)
}

// OperatorLookup is the narrow operator-store contract used by the resolver.
// Returns the canonical operator name or error.
type OperatorLookup interface {
	GetName(ctx context.Context, id uuid.UUID) (string, error)
}

// APNLookup is the narrow apn-store contract used by the resolver.
type APNLookup interface {
	GetName(ctx context.Context, tenantID, id uuid.UUID) (string, error)
}

// SimNameLookup resolves a SIM UUID to its ICCID (cross-tenant permitted —
// resolver is an infra component).
type SimNameLookup interface {
	GetICCID(ctx context.Context, id uuid.UUID) (string, error)
}

// simRow is a tiny shape used to isolate the resolver from store.SIM.
// Reserved for future GetByID-based SIM lookups; currently unused.
type simRow struct {
	ID    uuid.UUID
	ICCID string
}

// ResolverMetrics is the narrow counter contract the resolver emits into.
// Implementations typically delegate to prometheus CounterVec.
type ResolverMetrics interface {
	IncResolverHit(kind string)
	IncResolverMiss(kind, reason string)
}

// noopMetrics satisfies ResolverMetrics with empty methods; used when the
// Prometheus registry is not wired (tests, offline tooling).
type noopMetrics struct{}

func (noopMetrics) IncResolverHit(string)          {}
func (noopMetrics) IncResolverMiss(string, string) {}

// RedisResolver is the production Resolver implementation. It reads/writes a
// Redis cache with TTL and falls back to the configured store lookups on
// miss. All errors are logged and swallowed — the envelope keeps flowing.
//
// Storm protection:
//   - singleflight.Group coalesces concurrent misses for the same key into
//     ONE DB call (cold-restart burst shield).
//   - A short negative-TTL sentinel is written when the store reports a
//     missing row, so a missing id cannot trigger a DB hit every publish.
type RedisResolver struct {
	rdb     *redis.Client
	sims    SimNameLookup
	ops     OperatorLookup
	apns    APNLookup
	metrics ResolverMetrics
	logger  zerolog.Logger
	ttl     time.Duration
	negTTL  time.Duration
	sf      singleflight.Group
}

// NewRedisResolver constructs a resolver wired to Redis + store lookups.
// Pass nil metrics to use a no-op counter.
func NewRedisResolver(rdb *redis.Client, sims SimNameLookup, ops OperatorLookup, apns APNLookup, m ResolverMetrics, logger zerolog.Logger) *RedisResolver {
	if m == nil {
		m = noopMetrics{}
	}
	return &RedisResolver{
		rdb:     rdb,
		sims:    sims,
		ops:     ops,
		apns:    apns,
		metrics: m,
		logger:  logger.With().Str("component", "event_resolver").Logger(),
		ttl:     resolverTTL,
		negTTL:  resolverNegativeTTL,
	}
}

// ResolveICCID looks up sim.iccid by SIM UUID.
func (r *RedisResolver) ResolveICCID(ctx context.Context, simID uuid.UUID) (string, error) {
	if simID == uuid.Nil || r.sims == nil {
		return "", nil
	}
	key := keyPrefixSim + simID.String()
	name, _, _ := r.sf.Do(key, func() (interface{}, error) {
		if cached, ok := r.cacheGet(ctx, key); ok {
			if cached == resolverNegativeSentinel {
				r.metrics.IncResolverMiss("sim", "negative")
				return "", nil
			}
			r.metrics.IncResolverHit("sim")
			return cached, nil
		}
		r.metrics.IncResolverMiss("sim", "cold")
		v, err := r.sims.GetICCID(ctx, simID)
		if err != nil {
			r.metrics.IncResolverMiss("sim", "db_error")
			r.logger.Debug().Err(err).Str("sim_id", simID.String()).Msg("resolve iccid: store lookup failed")
			return "", nil
		}
		if v != "" {
			r.cacheSet(ctx, key, v, r.ttl)
		} else {
			r.cacheSet(ctx, key, resolverNegativeSentinel, r.negTTL)
		}
		return v, nil
	})
	s, _ := name.(string)
	return s, nil
}

// ResolveOperator looks up operator.name by operator UUID.
func (r *RedisResolver) ResolveOperator(ctx context.Context, opID uuid.UUID) (string, error) {
	if opID == uuid.Nil || r.ops == nil {
		return "", nil
	}
	key := keyPrefixOperator + opID.String()
	name, _, _ := r.sf.Do(key, func() (interface{}, error) {
		if cached, ok := r.cacheGet(ctx, key); ok {
			if cached == resolverNegativeSentinel {
				r.metrics.IncResolverMiss("operator", "negative")
				return "", nil
			}
			r.metrics.IncResolverHit("operator")
			return cached, nil
		}
		r.metrics.IncResolverMiss("operator", "cold")
		v, err := r.ops.GetName(ctx, opID)
		if err != nil {
			r.metrics.IncResolverMiss("operator", "db_error")
			r.logger.Debug().Err(err).Str("operator_id", opID.String()).Msg("resolve operator: store lookup failed")
			return "", nil
		}
		if v != "" {
			r.cacheSet(ctx, key, v, r.ttl)
		} else {
			r.cacheSet(ctx, key, resolverNegativeSentinel, r.negTTL)
		}
		return v, nil
	})
	s, _ := name.(string)
	return s, nil
}

// ResolveAPN looks up apn.name by tenant + APN UUID. Key is tenant-scoped
// because APN.name is a tenant-owned resource (two tenants can legitimately
// reuse the same apnID in test harnesses and fixtures — scoping prevents
// cross-tenant display-name bleed).
func (r *RedisResolver) ResolveAPN(ctx context.Context, tenantID, apnID uuid.UUID) (string, error) {
	if apnID == uuid.Nil || r.apns == nil {
		return "", nil
	}
	key := keyPrefixAPN + tenantID.String() + ":" + apnID.String()
	name, _, _ := r.sf.Do(key, func() (interface{}, error) {
		if cached, ok := r.cacheGet(ctx, key); ok {
			if cached == resolverNegativeSentinel {
				r.metrics.IncResolverMiss("apn", "negative")
				return "", nil
			}
			r.metrics.IncResolverHit("apn")
			return cached, nil
		}
		r.metrics.IncResolverMiss("apn", "cold")
		v, err := r.apns.GetName(ctx, tenantID, apnID)
		if err != nil {
			r.metrics.IncResolverMiss("apn", "db_error")
			r.logger.Debug().Err(err).Str("apn_id", apnID.String()).Msg("resolve apn: store lookup failed")
			return "", nil
		}
		if v != "" {
			r.cacheSet(ctx, key, v, r.ttl)
		} else {
			r.cacheSet(ctx, key, resolverNegativeSentinel, r.negTTL)
		}
		return v, nil
	})
	s, _ := name.(string)
	return s, nil
}

// cacheGet returns (value, true) on hit, ("", false) on miss or Redis error.
func (r *RedisResolver) cacheGet(ctx context.Context, key string) (string, bool) {
	if r.rdb == nil {
		return "", false
	}
	val, err := r.rdb.Get(ctx, key).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			r.logger.Debug().Err(err).Str("key", key).Msg("resolver: redis GET failed")
		}
		return "", false
	}
	return val, true
}

// cacheSet writes the value under the given TTL. Errors are logged and
// swallowed.
func (r *RedisResolver) cacheSet(ctx context.Context, key, value string, ttl time.Duration) {
	if r.rdb == nil {
		return
	}
	if ttl <= 0 {
		ttl = r.ttl
	}
	if err := r.rdb.Set(ctx, key, value, ttl).Err(); err != nil {
		r.logger.Debug().Err(err).Str("key", key).Msg("resolver: redis SET failed")
	}
}

// HandleCacheInvalidate is invoked by the bus subscriber on
// SubjectCacheInvalidate events. The payload is a simple
// map[string]interface{} with a "key" field formatted "operator:<id>" or
// "apn:<id>" (matches FIX-202 precedent). Unknown key shapes are ignored.
//
// APN invalidate: since APN keys are tenant-scoped ("apn:<tenant>:<apnID>"),
// a bare "apn:<apnID>" hint triggers a SCAN-DEL across all tenant-scoped
// keys for that APN id. APN renames are rare (ops event), so SCAN cost is
// acceptable. Hints that already carry the tenant ("apn:<tenant>:<apnID>")
// are DEL'd directly.
func (r *RedisResolver) HandleCacheInvalidate(ctx context.Context, keyHint string) {
	if r.rdb == nil || keyHint == "" {
		return
	}
	switch {
	case strings.HasPrefix(keyHint, "operator:"):
		target := keyPrefixOperator + strings.TrimPrefix(keyHint, "operator:")
		if err := r.rdb.Del(ctx, target).Err(); err != nil {
			r.logger.Debug().Err(err).Str("key", target).Msg("resolver: cache invalidate DEL failed")
		}
	case strings.HasPrefix(keyHint, "apn:"):
		rest := strings.TrimPrefix(keyHint, "apn:")
		if strings.Contains(rest, ":") {
			// Hint already carries tenant prefix → direct DEL.
			target := keyPrefixAPN + rest
			if err := r.rdb.Del(ctx, target).Err(); err != nil {
				r.logger.Debug().Err(err).Str("key", target).Msg("resolver: cache invalidate DEL failed")
			}
			return
		}
		// Fallback: bare "apn:<id>" → SCAN across tenant prefixes.
		pattern := keyPrefixAPN + "*:" + rest
		iter := r.rdb.Scan(ctx, 0, pattern, 256).Iterator()
		for iter.Next(ctx) {
			if err := r.rdb.Del(ctx, iter.Val()).Err(); err != nil {
				r.logger.Debug().Err(err).Str("key", iter.Val()).Msg("resolver: cache invalidate DEL failed")
			}
		}
		if err := iter.Err(); err != nil {
			r.logger.Debug().Err(err).Str("pattern", pattern).Msg("resolver: cache invalidate SCAN failed")
		}
	case strings.HasPrefix(keyHint, "sim:"):
		target := keyPrefixSim + strings.TrimPrefix(keyHint, "sim:")
		if err := r.rdb.Del(ctx, target).Err(); err != nil {
			r.logger.Debug().Err(err).Str("key", target).Msg("resolver: cache invalidate DEL failed")
		}
	default:
		return
	}
}

// Formatters --------------------------------------------------------------

// FormatSimDisplayName returns the canonical display_name used in
// entity:{type:"sim"} envelopes. Empty iccid yields empty display_name so FE
// falls back to entity.id.
func FormatSimDisplayName(iccid string) string {
	if iccid == "" {
		return ""
	}
	return fmt.Sprintf("ICCID %s", iccid)
}
